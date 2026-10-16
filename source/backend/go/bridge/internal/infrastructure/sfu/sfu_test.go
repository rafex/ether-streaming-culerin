package sfu

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

type recordingListener struct {
	mu          sync.Mutex
	published   []domain.SourceRef
	unpublished []domain.SourceRef
}

func (l *recordingListener) SourcePublished(_ domain.RoomID, p domain.ParticipantID, s domain.MediaSource) {
	l.mu.Lock()
	l.published = append(l.published, domain.SourceRef{Participant: p, Source: s})
	l.mu.Unlock()
}

func (l *recordingListener) SourceUnpublished(_ domain.RoomID, p domain.ParticipantID, s domain.MediaSource) {
	l.mu.Lock()
	l.unpublished = append(l.unpublished, domain.SourceRef{Participant: p, Source: s})
	l.mu.Unlock()
}

func (l *recordingListener) counts() (int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.published), len(l.unpublished)
}

// client plays the webrelay: it applies everything the bridge sends.
type client struct {
	t    *testing.T
	pub  *webrtc.PeerConnection
	sub  *webrtc.PeerConnection
	peer ports.MediaPeer
	msgs chan domain.ServerMessage
	got  chan *webrtc.TrackRemote
}

func newClient(t *testing.T) *client {
	t.Helper()
	pub, err := webrtc.NewPeerConnection(webrtc.Configuration{BundlePolicy: webrtc.BundlePolicyMaxBundle})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := webrtc.NewPeerConnection(webrtc.Configuration{BundlePolicy: webrtc.BundlePolicyMaxBundle})
	if err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, pub: pub, sub: sub, msgs: make(chan domain.ServerMessage, 64), got: make(chan *webrtc.TrackRemote, 4)}
	sub.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) { c.got <- track })
	t.Cleanup(func() { _ = pub.Close(); _ = sub.Close() })
	return c
}

func (c *client) sink(m domain.ServerMessage) { c.msgs <- m }

// pump applies bridge messages until stop is closed.
func (c *client) pump(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case m := <-c.msgs:
			switch msg := m.(type) {
			case domain.ServerIceMessage:
				pc := c.sub
				if msg.PC == domain.RolePublish {
					pc = c.pub
				}
				_ = pc.AddICECandidate(webrtc.ICECandidateInit{Candidate: msg.Candidate.Candidate, SDPMid: msg.Candidate.SDPMid, SDPMLineIndex: msg.Candidate.SDPMLineIndex})
			case domain.SDPMessage:
				if msg.Type != "subscribe.offer" {
					continue
				}
				if err := c.sub.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: msg.SDP}); err != nil {
					c.t.Errorf("subscribe offer: %v", err)
					continue
				}
				answer, _ := c.sub.CreateAnswer(nil)
				gathered := webrtc.GatheringCompletePromise(c.sub)
				_ = c.sub.SetLocalDescription(answer)
				<-gathered
				if err := c.peer.AnswerSubscribe(c.sub.LocalDescription().SDP); err != nil {
					c.t.Logf("answer subscribe (peer may be closed): %v", err)
				}
			}
		}
	}
}

func TestTwoParticipantsThroughTheSFU(t *testing.T) {
	s, err := New(Config{IncludeLoopback: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	listener := &recordingListener{}
	s.SetListener(listener)
	// The pumps report through t, so the test must not return (and t.Cleanup must not
	// close the clients' connections) while one is still handling a message.
	stop := make(chan struct{})
	var pumps sync.WaitGroup
	defer func() { close(stop); pumps.Wait() }()

	ana, beto := newClient(t), newClient(t)
	if ana.peer, err = s.Attach("demo", "p-1", ana.sink); err != nil {
		t.Fatal(err)
	}
	if beto.peer, err = s.Attach("demo", "p-2", beto.sink); err != nil {
		t.Fatal(err)
	}
	pumps.Add(2)
	go func() { defer pumps.Done(); ana.pump(stop) }()
	go func() { defer pumps.Done(); beto.pump(stop) }()

	// Ana publishes a VP8 camera track.
	track, _ := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}, "cam", "ana")
	if _, err := ana.pub.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
		t.Fatal(err)
	}
	offer, _ := ana.pub.CreateOffer(nil)
	gathered := webrtc.GatheringCompletePromise(ana.pub)
	_ = ana.pub.SetLocalDescription(offer)
	<-gathered
	mid := ana.pub.GetTransceivers()[0].Mid()
	answer, err := ana.peer.Publish(ana.pub.LocalDescription().SDP, map[string]domain.MediaSource{mid: domain.SourceCamera})
	if err != nil {
		t.Fatal(err)
	}
	if err := ana.pub.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatal(err)
	}

	go func() {
		seq := uint16(0)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				seq++
				// VP8 payload descriptor (S bit) + a keyframe-looking byte.
				_ = track.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, Marker: true, SequenceNumber: seq, Timestamp: uint32(seq) * 1800},
					Payload: []byte{0x10, 0x00, 0x9d, 0x01, 0x2a}})
			}
		}
	}()

	// Beto receives Ana's camera through his subscribe connection.
	select {
	case remote := <-beto.got:
		if remote.Kind() != webrtc.RTPCodecTypeVideo {
			t.Fatalf("got %s track", remote.Kind())
		}
		_ = remote.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, _, err := remote.ReadRTP(); err != nil {
			t.Fatalf("no RTP relayed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Beto never received Ana's track")
	}

	ref := domain.SourceRef{Participant: "p-1", Source: domain.SourceCamera}
	if pub, _ := listener.counts(); pub != 1 {
		t.Fatalf("published events = %d", pub)
	}
	if s.LastPacket("demo", ref).IsZero() {
		t.Fatal("LastPacket not tracked")
	}
	packets := make(chan []byte, 8)
	codec, stopTap, err := s.Tap("demo", ref, func(b []byte) {
		select {
		case packets <- b:
		default:
		}
	})
	if err != nil || codec.MimeType != webrtc.MimeTypeVP8 || codec.PayloadType == 0 {
		t.Fatalf("tap: %+v %v", codec, err)
	}
	select {
	case b := <-packets:
		var p rtp.Packet
		if p.Unmarshal(b) != nil || p.Header.Extension {
			t.Fatal("tapped packets must be valid RTP without header extensions")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tap received nothing")
	}
	stopTap()
	s.RequestKeyframe("demo", ref)
	if _, _, err := s.Tap("demo", domain.SourceRef{Participant: "p-9", Source: domain.SourceCamera}, nil); err == nil {
		t.Fatal("tap of an unknown source")
	}

	// Beto's tracks message names the publisher.
	ana.peer.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, unpub := listener.counts(); unpub == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closing the publisher must unpublish its track")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !s.LastPacket("demo", ref).IsZero() {
		t.Fatal("unpublished source still reported")
	}
	if _, err := ana.peer.Publish("v=0", nil); err == nil {
		t.Fatal("publishing on a closed peer")
	}
	beto.peer.Resume()
	beto.peer.Close()
}

func TestCandidatesBeforeTheOfferAreQueued(t *testing.T) {
	s, err := New(Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mp, err := s.Attach("demo", "p-1", func(domain.ServerMessage) {})
	if err != nil {
		t.Fatal(err)
	}
	defer mp.Close()
	if err := mp.AddCandidate(domain.RolePublish, domain.IceCandidate{Candidate: "candidate:1 1 udp 1 127.0.0.1 9 typ host"}); err != nil {
		t.Fatalf("early candidate must be queued: %v", err)
	}
	p := mp.(*peer)
	if len(p.pending[domain.RolePublish]) != 1 {
		t.Fatal("candidate not queued")
	}
	if _, err := New(Config{PublicIPs: []string{"203.0.113.7"}, UDPPort: 0}, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("public ip config: %v", err)
	}
}
