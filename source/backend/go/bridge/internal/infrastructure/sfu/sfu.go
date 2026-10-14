// Package sfu is the Pion-based selective forwarding unit: every participant
// publishes its tracks once and receives everybody else's, following the
// webrelay's two-connection model (publish offered by the client, subscribe
// offered by the bridge). It also lets the compositor tap the tracks.
package sfu

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

// Config tunes the WebRTC stack.
type Config struct {
	// UDPPort, when non-zero, multiplexes all ICE traffic on one UDP port
	// (what a container publishes). Zero uses ephemeral ports.
	UDPPort int
	// PublicIPs replaces the host candidates' addresses (1:1 NAT, containers).
	PublicIPs []string
	// IncludeLoopback gathers 127.0.0.1 candidates (same-host tests).
	IncludeLoopback bool
}

// SFU implements ports.SFU and ports.MediaTap.
type SFU struct {
	api *webrtc.API
	log *slog.Logger
	mux net.PacketConn

	mu       sync.Mutex
	rooms    map[domain.RoomID]*room
	listener ports.MediaListener
}

type room struct {
	id      domain.RoomID
	peers   map[domain.ParticipantID]*peer
	sources map[domain.SourceRef]*source
}

var (
	_ ports.SFU      = (*SFU)(nil)
	_ ports.MediaTap = (*SFU)(nil)
)

// New builds the WebRTC API: Opus, VP8 and H.264 (the codecs every browser
// and FFmpeg handle), NACK, RTCP reports and TWCC.
func New(cfg Config, log *slog.Logger) (*SFU, error) {
	media := &webrtc.MediaEngine{}
	videoFeedback := []webrtc.RTCPFeedback{
		{Type: "goog-remb"}, {Type: "ccm", Parameter: "fir"}, {Type: "nack"}, {Type: "nack", Parameter: "pli"},
	}
	codecs := []struct {
		params webrtc.RTPCodecParameters
		kind   webrtc.RTPCodecType
	}{
		{webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1",
		}, PayloadType: 111}, webrtc.RTPCodecTypeAudio},
		{webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeVP8, ClockRate: 90000, RTCPFeedback: videoFeedback,
		}, PayloadType: 96}, webrtc.RTPCodecTypeVideo},
		{webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeH264, ClockRate: 90000, RTCPFeedback: videoFeedback,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		}, PayloadType: 102}, webrtc.RTPCodecTypeVideo},
	}
	for _, c := range codecs {
		if err := media.RegisterCodec(c.params, c.kind); err != nil {
			return nil, fmt.Errorf("register codec %s: %w", c.params.MimeType, err)
		}
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(media, registry); err != nil {
		return nil, fmt.Errorf("register interceptors: %w", err)
	}

	settings := webrtc.SettingEngine{}
	settings.SetIncludeLoopbackCandidate(cfg.IncludeLoopback)
	if len(cfg.PublicIPs) > 0 {
		if err := settings.SetICEAddressRewriteRules(webrtc.ICEAddressRewriteRule{
			External: cfg.PublicIPs, AsCandidateType: webrtc.ICECandidateTypeHost, Mode: webrtc.ICEAddressRewriteReplace,
		}); err != nil {
			return nil, fmt.Errorf("public ips: %w", err)
		}
	}
	s := &SFU{log: log, rooms: map[domain.RoomID]*room{}}
	if cfg.UDPPort != 0 {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: cfg.UDPPort})
		if err != nil {
			return nil, fmt.Errorf("listen ICE UDP port %d: %w", cfg.UDPPort, err)
		}
		settings.SetICEUDPMux(webrtc.NewICEUDPMux(nil, conn))
		settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
		s.mux = conn
	}
	s.api = webrtc.NewAPI(webrtc.WithMediaEngine(media), webrtc.WithInterceptorRegistry(registry), webrtc.WithSettingEngine(settings))
	return s, nil
}

// SetListener implements ports.SFU.
func (s *SFU) SetListener(l ports.MediaListener) {
	s.mu.Lock()
	s.listener = l
	s.mu.Unlock()
}

// Close stops the shared UDP port.
func (s *SFU) Close() error {
	if s.mux != nil {
		return s.mux.Close()
	}
	return nil
}

// Attach implements ports.SFU.
func (s *SFU) Attach(roomID domain.RoomID, id domain.ParticipantID, sink ports.SignalSink) (ports.MediaPeer, error) {
	sub, err := s.api.NewPeerConnection(webrtc.Configuration{BundlePolicy: webrtc.BundlePolicyMaxBundle})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	r := s.rooms[roomID]
	if r == nil {
		r = &room{id: roomID, peers: map[domain.ParticipantID]*peer{}, sources: map[domain.SourceRef]*source{}}
		s.rooms[roomID] = r
	}
	p := newPeer(s, r, id, sink, sub)
	r.peers[id] = p
	existing := make([]*source, 0, len(r.sources))
	for _, src := range r.sources {
		existing = append(existing, src)
	}
	s.mu.Unlock()

	for _, src := range existing {
		p.forward(src)
	}
	p.renegotiate(false)
	return p, nil
}

// Tap implements ports.MediaTap.
func (s *SFU) Tap(roomID domain.RoomID, ref domain.SourceRef, fn func([]byte)) (ports.Codec, func(), error) {
	src := s.source(roomID, ref)
	if src == nil {
		return ports.Codec{}, nil, fmt.Errorf("source %s not published in %s", ref, roomID)
	}
	return src.codec(), src.addTap(fn), nil
}

// RequestKeyframe implements ports.MediaTap.
func (s *SFU) RequestKeyframe(roomID domain.RoomID, ref domain.SourceRef) {
	if src := s.source(roomID, ref); src != nil {
		src.requestKeyframe()
	}
}

// LastPacket implements ports.MediaTap.
func (s *SFU) LastPacket(roomID domain.RoomID, ref domain.SourceRef) time.Time {
	if src := s.source(roomID, ref); src != nil {
		return src.lastPacket()
	}
	return time.Time{}
}

func (s *SFU) source(roomID domain.RoomID, ref domain.SourceRef) *source {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.rooms[roomID]; r != nil {
		return r.sources[ref]
	}
	return nil
}

// publish registers a new source and forwards it to every other peer.
func (s *SFU) publish(r *room, src *source) {
	s.mu.Lock()
	if old := r.sources[src.ref]; old != nil && old != src {
		s.mu.Unlock()
		s.unpublish(r, old)
		s.mu.Lock()
	}
	r.sources[src.ref] = src
	subscribers := make([]*peer, 0, len(r.peers))
	for id, p := range r.peers {
		if id != src.ref.Participant {
			subscribers = append(subscribers, p)
		}
	}
	listener := s.listener
	s.mu.Unlock()

	s.log.Info("track published", "room", r.id, "source", src.ref.String(), "codec", src.remote.Codec().MimeType)
	for _, p := range subscribers {
		p.forward(src)
		p.renegotiate(false)
	}
	if listener != nil {
		go listener.SourcePublished(r.id, src.ref.Participant, src.ref.Source)
	}
}

// unpublish removes a source everywhere.
func (s *SFU) unpublish(r *room, src *source) {
	s.mu.Lock()
	if r.sources[src.ref] != src {
		s.mu.Unlock()
		return
	}
	delete(r.sources, src.ref)
	peers := make([]*peer, 0, len(r.peers))
	for _, p := range r.peers {
		peers = append(peers, p)
	}
	listener := s.listener
	s.mu.Unlock()

	src.close()
	for _, p := range peers {
		if p.unforward(src) {
			p.renegotiate(false)
		}
	}
	s.log.Info("track unpublished", "room", r.id, "source", src.ref.String())
	if listener != nil {
		go listener.SourceUnpublished(r.id, src.ref.Participant, src.ref.Source)
	}
}

// detach removes a peer and everything it published.
func (s *SFU) detach(r *room, p *peer) {
	s.mu.Lock()
	if r.peers[p.id] == p {
		delete(r.peers, p.id)
	}
	var owned []*source
	for ref, src := range r.sources {
		if ref.Participant == p.id {
			owned = append(owned, src)
		}
	}
	empty := len(r.peers) == 0
	if empty && len(r.sources) == len(owned) {
		delete(s.rooms, r.id)
	}
	s.mu.Unlock()
	for _, src := range owned {
		s.unpublish(r, src)
	}
}

var errClosed = errors.New("media peer closed")
