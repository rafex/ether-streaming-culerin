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
)

// browserLikeAPI offers Opus with the audio-level extension, as Chrome, Firefox and Safari do.
func browserLikeAPI(t *testing.T) *webrtc.API {
	t.Helper()
	media := &webrtc.MediaEngine{}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		PayloadType:        111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		t.Fatal(err)
	}
	if err := media.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: audioLevelURI}, webrtc.RTPCodecTypeAudio); err != nil {
		t.Fatal(err)
	}
	return webrtc.NewAPI(webrtc.WithMediaEngine(media))
}

// TestTheSFUMeasuresWhoIsTalkingFromTheAudioLevelExtension publishes a microphone whose packets
// report a loud voice, then silence, and reads the activity the bridge derives from it.
func TestTheSFUMeasuresWhoIsTalkingFromTheAudioLevelExtension(t *testing.T) {
	s, err := New(Config{IncludeLoopback: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stop := make(chan struct{})
	var pumps sync.WaitGroup
	defer func() { close(stop); pumps.Wait() }()

	ana := newClientWith(t, browserLikeAPI(t))
	if ana.peer, err = s.Attach("demo", "p-1", ana.sink); err != nil {
		t.Fatal(err)
	}
	pumps.Add(1)
	go func() { defer pumps.Done(); ana.pump(stop) }()

	track, _ := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "mic", "ana")
	sender, err := ana.pub.AddTrack(track)
	if err != nil {
		t.Fatal(err)
	}
	offer, _ := ana.pub.CreateOffer(nil)
	gathered := webrtc.GatheringCompletePromise(ana.pub)
	_ = ana.pub.SetLocalDescription(offer)
	<-gathered
	mid := ana.pub.GetTransceivers()[0].Mid()
	answer, err := ana.peer.Publish(ana.pub.LocalDescription().SDP, map[string]domain.MediaSource{mid: domain.SourceMicrophone})
	if err != nil {
		t.Fatal(err)
	}
	if err := ana.pub.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatal(err)
	}

	var levelID uint8
	for _, ext := range sender.GetParameters().HeaderExtensions {
		if ext.URI == audioLevelURI {
			levelID = uint8(ext.ID)
		}
	}
	if levelID == 0 {
		t.Fatal("the bridge did not negotiate the audio-level extension")
	}

	var level byte = 0x80 | 20 // a clear voice
	var mu sync.Mutex
	setLevel := func(b byte) { mu.Lock(); level = b; mu.Unlock() }
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
				mu.Lock()
				b := level
				mu.Unlock()
				pkt := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 960}, Payload: []byte{0xfc, 0xff, 0xfe}}
				if err := pkt.Header.SetExtension(levelID, []byte{b}); err != nil {
					return
				}
				_ = track.WriteRTP(pkt)
			}
		}
	}()

	ref := domain.SourceRef{Participant: "p-1", Source: domain.SourceMicrophone}
	waitFor := func(what string, cond func(float64) bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for !cond(s.Activity("demo", ref)) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s (activity %.2f)", what, s.Activity("demo", ref))
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitFor("a talking microphone to read as active", func(a float64) bool { return a > 0.8 })
	setLevel(127) // digital silence
	waitFor("a silent microphone to read as inactive", func(a float64) bool { return a < 0.1 })

	if got := s.Activity("demo", domain.SourceRef{Participant: "p-9", Source: domain.SourceMicrophone}); got != 0 {
		t.Fatalf("unknown source activity = %v, want 0", got)
	}
}
