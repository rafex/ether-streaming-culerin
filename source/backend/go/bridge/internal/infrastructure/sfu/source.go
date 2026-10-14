package sfu

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

// keyframeInterval rate-limits PLIs sent to a publisher.
const keyframeInterval = 500 * time.Millisecond

// source is one published track: a reader on the publisher's connection
// fanning out to a shared local track (bound to every subscriber) and to
// compositor taps.
type source struct {
	ref       domain.SourceRef
	remote    *webrtc.TrackRemote
	local     *webrtc.TrackLocalStaticRTP
	publisher *webrtc.PeerConnection

	last     atomic.Int64 // unix nanos of the last packet
	lastPLI  atomic.Int64
	mu       sync.Mutex
	taps     map[int]func([]byte)
	nextTap  int
	closed   chan struct{}
	closeOne sync.Once
}

func newSource(ref domain.SourceRef, remote *webrtc.TrackRemote, publisher *webrtc.PeerConnection) (*source, error) {
	local, err := webrtc.NewTrackLocalStaticRTP(remote.Codec().RTPCodecCapability,
		string(ref.Participant)+"-"+string(ref.Source), string(ref.Participant))
	if err != nil {
		return nil, err
	}
	return &source{ref: ref, remote: remote, local: local, publisher: publisher,
		taps: map[int]func([]byte){}, closed: make(chan struct{})}, nil
}

// run copies RTP until the remote track ends. Header extensions are dropped:
// their ids were negotiated on the publish connection and mean something
// else (or nothing) on each subscribe connection.
func (s *source) run(onEnd func()) {
	defer onEnd()
	buf := make([]byte, 1500)
	var pkt rtp.Packet
	for {
		n, _, err := s.remote.Read(buf)
		if err != nil {
			return
		}
		if err := pkt.Unmarshal(buf[:n]); err != nil {
			continue
		}
		pkt.Header.Extension = false
		pkt.Header.Extensions = nil
		pkt.Header.ExtensionProfile = 0
		s.last.Store(time.Now().UnixNano())
		if err := s.local.WriteRTP(&pkt); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			return
		}
		s.mu.Lock()
		taps := len(s.taps)
		s.mu.Unlock()
		if taps == 0 {
			continue
		}
		raw, err := pkt.Marshal()
		if err != nil {
			continue
		}
		s.mu.Lock()
		for _, tap := range s.taps {
			tap(raw)
		}
		s.mu.Unlock()
	}
}

func (s *source) codec() ports.Codec {
	c := s.remote.Codec()
	return ports.Codec{
		MimeType: c.MimeType, ClockRate: c.ClockRate, Channels: c.Channels,
		PayloadType: uint8(c.PayloadType), Fmtp: c.SDPFmtpLine,
	}
}

func (s *source) addTap(fn func([]byte)) func() {
	s.mu.Lock()
	id := s.nextTap
	s.nextTap++
	s.taps[id] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.taps, id)
		s.mu.Unlock()
	}
}

func (s *source) requestKeyframe() {
	if !s.ref.Source.IsVideo() {
		return
	}
	now := time.Now().UnixNano()
	if now-s.lastPLI.Load() < int64(keyframeInterval) {
		return
	}
	s.lastPLI.Store(now)
	_ = s.publisher.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(s.remote.SSRC())}})
}

func (s *source) lastPacket() time.Time {
	if v := s.last.Load(); v != 0 {
		return time.Unix(0, v)
	}
	return time.Time{}
}

func (s *source) close() {
	s.closeOne.Do(func() { close(s.closed) })
}
