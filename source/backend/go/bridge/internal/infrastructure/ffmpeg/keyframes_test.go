package ffmpeg

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

type countingTap struct {
	mu    sync.Mutex
	times []time.Time
}

func (t *countingTap) Tap(domain.RoomID, domain.SourceRef, func([]byte)) (ports.Codec, func(), error) {
	return ports.Codec{}, func() {}, nil
}
func (t *countingTap) LastPacket(domain.RoomID, domain.SourceRef) time.Time { return time.Time{} }
func (t *countingTap) RequestKeyframe(domain.RoomID, domain.SourceRef) {
	t.mu.Lock()
	t.times = append(t.times, time.Now())
	t.mu.Unlock()
}

// A starting run must ask for keyframes at once (not after a tick) and keep
// asking every keyframeRetry, because FFmpeg opens its inputs one by one and
// each needs a keyframe that arrives after its socket exists.
func TestStartingRunRequestsKeyframesImmediatelyAndRepeatedly(t *testing.T) {
	tap := &countingTap{}
	c := &Compositor{tap: tap, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p := &pipeline{c: c, room: "r"}
	run := &composeRun{done: make(chan struct{})}
	in := rtpInput{ref: domain.SourceRef{Participant: "p-1", Source: domain.SourceCamera}}

	start := time.Now()
	go p.requestKeyframes(run, 1, []rtpInput{in})
	time.Sleep(3*keyframeRetry + keyframeRetry/2)
	close(run.done)

	tap.mu.Lock()
	defer tap.mu.Unlock()
	if len(tap.times) < 4 {
		t.Fatalf("got %d keyframe requests in %v, want at least 4 (immediate + every %v)", len(tap.times), time.Since(start), keyframeRetry)
	}
	if first := tap.times[0].Sub(start); first > keyframeRetry/2 {
		t.Fatalf("first keyframe request after %v, want immediately", first)
	}
}
