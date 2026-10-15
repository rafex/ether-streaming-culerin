package sfu

import (
	"fmt"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

// peer is one participant: its publish connection (created on the first
// publish.offer) and its subscribe connection (created on join).
type peer struct {
	sfu  *SFU
	room *room
	id   domain.ParticipantID
	sink ports.SignalSink

	mu        sync.Mutex
	closed    bool
	pub       *webrtc.PeerConnection
	sub       *webrtc.PeerConnection
	sources   map[string]domain.MediaSource // publish mid → source
	forwarded map[domain.SourceRef]*forward
	pending   map[domain.PeerRole][]webrtc.ICECandidateInit

	// subscribe negotiation: one offer in flight; changes meanwhile set again.
	// negMu serializes the calls that change the subscribe connection's SDP
	// state (SetRemoteDescription of the answer, CreateOffer/SetLocalDescription,
	// rollback): Pion does not support them running concurrently, and Resume
	// (on reconnect) can overlap with an answer or a track-driven renegotiation.
	// It is never held while calling p.sink or taking p.mu for long.
	negMu       sync.Mutex
	negotiating bool
	again       bool
	restart     bool
}

type forward struct {
	src    *source
	sender *webrtc.RTPSender
}

var _ ports.MediaPeer = (*peer)(nil)

func newPeer(s *SFU, r *room, id domain.ParticipantID, sink ports.SignalSink, sub *webrtc.PeerConnection) *peer {
	p := &peer{sfu: s, room: r, id: id, sink: sink, sub: sub,
		sources: map[string]domain.MediaSource{}, forwarded: map[domain.SourceRef]*forward{},
		pending: map[domain.PeerRole][]webrtc.ICECandidateInit{}}
	sub.OnICECandidate(p.trickle(domain.RoleSubscribe))
	sub.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.log.Debug("subscribe connection", "room", r.id, "participant", id, "state", state.String())
	})
	return p
}

// Publish implements ports.MediaPeer.
func (p *peer) Publish(sdp string, sources map[string]domain.MediaSource) (string, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return "", errClosed
	}
	for mid, source := range sources {
		p.sources[mid] = source
	}
	pub := p.pub
	p.mu.Unlock()

	if pub == nil {
		var err error
		pub, err = p.newPublishConnection()
		if err != nil {
			return "", err
		}
	}
	if err := pub.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
		return "", fmt.Errorf("set publish offer: %w", err)
	}
	p.flushCandidates(domain.RolePublish, pub)
	answer, err := pub.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("create publish answer: %w", err)
	}
	if err := pub.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("set publish answer: %w", err)
	}
	return pub.LocalDescription().SDP, nil
}

func (p *peer) newPublishConnection() (*webrtc.PeerConnection, error) {
	pub, err := p.sfu.api.NewPeerConnection(webrtc.Configuration{BundlePolicy: webrtc.BundlePolicyMaxBundle})
	if err != nil {
		return nil, err
	}
	pub.OnICECandidate(p.trickle(domain.RolePublish))
	pub.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		mid := ""
		for _, t := range pub.GetTransceivers() {
			if t.Receiver() == receiver {
				mid = t.Mid()
			}
		}
		p.mu.Lock()
		kind, ok := p.sources[mid]
		closed := p.closed
		p.mu.Unlock()
		if !ok || closed {
			p.sfu.log.Warn("track without declared source", "room", p.room.id, "participant", p.id, "mid", mid)
			return
		}
		src, err := newSource(domain.SourceRef{Participant: p.id, Source: kind}, remote, pub)
		if err != nil {
			p.sfu.log.Error("cannot relay track", "err", err)
			return
		}
		p.sfu.publish(p.room, src)
		go src.run(func() { p.sfu.unpublish(p.room, src) })
	})
	pub.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		p.sfu.log.Debug("publish connection", "room", p.room.id, "participant", p.id, "state", state.String())
	})
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = pub.Close()
		return nil, errClosed
	}
	p.pub = pub
	p.mu.Unlock()
	return pub, nil
}

// AnswerSubscribe implements ports.MediaPeer.
func (p *peer) AnswerSubscribe(sdp string) error {
	p.negMu.Lock()
	err := p.sub.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp})
	p.negMu.Unlock()
	if err != nil {
		return fmt.Errorf("set subscribe answer: %w", err)
	}
	p.flushCandidates(domain.RoleSubscribe, p.sub)
	p.mu.Lock()
	p.negotiating = false
	again, restart := p.again, p.restart
	p.again, p.restart = false, false
	var video []*source
	for _, f := range p.forwarded {
		if f.src.ref.Source.IsVideo() {
			video = append(video, f.src)
		}
	}
	p.mu.Unlock()
	// A new viewer cannot decode until the next keyframe.
	time.AfterFunc(200*time.Millisecond, func() {
		for _, src := range video {
			src.requestKeyframe()
		}
	})
	if again {
		p.renegotiate(restart)
	}
	return nil
}

// AddCandidate implements ports.MediaPeer; candidates that arrive before the
// remote description are queued.
func (p *peer) AddCandidate(role domain.PeerRole, c domain.IceCandidate) error {
	init := webrtc.ICECandidateInit{Candidate: c.Candidate, SDPMid: c.SDPMid, SDPMLineIndex: c.SDPMLineIndex}
	p.mu.Lock()
	pc := p.sub
	if role == domain.RolePublish {
		pc = p.pub
	}
	if pc == nil || pc.RemoteDescription() == nil {
		p.pending[role] = append(p.pending[role], init)
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	return pc.AddICECandidate(init)
}

func (p *peer) flushCandidates(role domain.PeerRole, pc *webrtc.PeerConnection) {
	p.mu.Lock()
	queued := p.pending[role]
	delete(p.pending, role)
	p.mu.Unlock()
	for _, c := range queued {
		if err := pc.AddICECandidate(c); err != nil {
			p.sfu.log.Debug("queued candidate rejected", "role", role, "err", err)
		}
	}
}

// Resume implements ports.MediaPeer: messages sent while the socket was down
// were lost, so the subscribe side is offered again (with an ICE restart if
// media dropped too).
func (p *peer) Resume() {
	p.mu.Lock()
	inFlight := p.negotiating
	p.negotiating = false
	p.mu.Unlock()
	p.negMu.Lock()
	if inFlight && p.sub.SignalingState() == webrtc.SignalingStateHaveLocalOffer {
		_ = p.sub.SetLocalDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeRollback})
	}
	p.negMu.Unlock()
	state := p.sub.ICEConnectionState()
	restart := state == webrtc.ICEConnectionStateFailed || state == webrtc.ICEConnectionStateDisconnected
	p.renegotiate(restart)
}

// Close implements ports.MediaPeer.
func (p *peer) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	pub, sub := p.pub, p.sub
	p.mu.Unlock()
	p.sfu.detach(p.room, p)
	if pub != nil {
		_ = pub.Close()
	}
	_ = sub.Close()
}

// forward adds src to this peer's subscribe connection (once).
func (p *peer) forward(src *source) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || src.ref.Participant == p.id {
		return
	}
	if _, ok := p.forwarded[src.ref]; ok {
		return
	}
	sender, err := p.sub.AddTrack(src.local)
	if err != nil {
		p.sfu.log.Warn("cannot forward track", "to", p.id, "source", src.ref.String(), "err", err)
		return
	}
	p.forwarded[src.ref] = &forward{src: src, sender: sender}
	go p.readRTCP(sender, src)
}

// unforward removes src; it reports whether anything changed.
func (p *peer) unforward(src *source) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	f, ok := p.forwarded[src.ref]
	if !ok || f.src != src {
		return false
	}
	delete(p.forwarded, src.ref)
	if !p.closed {
		_ = p.sub.RemoveTrack(f.sender)
	}
	return !p.closed
}

// readRTCP relays the subscriber's keyframe requests to the publisher.
func (p *peer) readRTCP(sender *webrtc.RTPSender, src *source) {
	for {
		packets, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, pkt := range packets {
			switch pkt.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				src.requestKeyframe()
			}
		}
	}
}

// renegotiate sends `tracks` and a new `subscribe.offer` (never an empty one:
// browsers using max-bundle reject offers without media sections).
func (p *peer) renegotiate(iceRestart bool) {
	p.mu.Lock()
	if p.closed || len(p.forwarded) == 0 && !hasTransceivers(p.sub) {
		p.mu.Unlock()
		return
	}
	if p.negotiating {
		p.again = true
		p.restart = p.restart || iceRestart
		p.mu.Unlock()
		return
	}
	p.negotiating = true
	p.mu.Unlock()

	p.negMu.Lock()
	offer, err := p.sub.CreateOffer(&webrtc.OfferOptions{ICERestart: iceRestart})
	if err == nil {
		err = p.sub.SetLocalDescription(offer)
	}
	p.negMu.Unlock()
	if err != nil {
		p.sfu.log.Warn("subscribe offer failed", "participant", p.id, "err", err)
		p.mu.Lock()
		p.negotiating = false
		p.mu.Unlock()
		return
	}
	tracks := map[string]domain.TrackInfo{}
	p.mu.Lock()
	for ref, f := range p.forwarded {
		for _, t := range p.sub.GetTransceivers() {
			if t.Sender() == f.sender && t.Mid() != "" {
				tracks[t.Mid()] = domain.TrackInfo{Participant: ref.Participant, Source: ref.Source}
			}
		}
	}
	p.mu.Unlock()
	p.sink(domain.NewTracks(tracks))
	p.sink(domain.NewSubscribeOffer(p.sub.LocalDescription().SDP))
}

func hasTransceivers(pc *webrtc.PeerConnection) bool { return len(pc.GetTransceivers()) > 0 }

func (p *peer) trickle(role domain.PeerRole) func(*webrtc.ICECandidate) {
	return func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		p.sink(domain.NewIce(role, domain.IceCandidate{
			Candidate: init.Candidate, SDPMid: init.SDPMid, SDPMLineIndex: init.SDPMLineIndex,
		}))
	}
}
