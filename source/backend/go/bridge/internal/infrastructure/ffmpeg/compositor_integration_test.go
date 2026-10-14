package ffmpeg

import (
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

// noTap has no published tracks: every tile renders as a card.
type noTap struct{}

func (noTap) Tap(domain.RoomID, domain.SourceRef, func([]byte)) (ports.Codec, func(), error) {
	return ports.Codec{}, nil, errors.New("not published")
}
func (noTap) RequestKeyframe(domain.RoomID, domain.SourceRef)      {}
func (noTap) LastPacket(domain.RoomID, domain.SourceRef) time.Time { return time.Time{} }

func TestCompositorKeepsOneStreamAcrossLayoutChanges(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	c, err := New(Config{Binary: "ffmpeg", WorkDir: dir, RTPPortMin: 47000, RTPPortMax: 47100},
		noTap{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	out := filepath.Join(dir, "broadcast.flv")
	outputs := []ports.Output{{ID: "file", URL: out}}
	small := domain.OutputSettings{Width: 320, Height: 180, Framerate: 15, VideoBitrate: 300_000, AudioBitrate: 64_000}
	empty := domain.CompositionPlan{Output: small, Title: "Demo"}
	if err := c.Apply("demo", empty, outputs); err != nil {
		t.Fatal(err)
	}
	if err := c.Apply("demo", empty, outputs); err != nil {
		t.Fatal("re-applying the same plan must be a no-op")
	}
	time.Sleep(2 * time.Second)
	if st := c.Status("demo"); !st.Live || st.Since.IsZero() {
		t.Fatalf("status = %+v", st)
	}

	cards := empty
	cards.Tiles = []domain.Tile{{Rect: domain.Rect{X: 10, Y: 10, W: 140, H: 80}, Kind: domain.TileVideo,
		Source: domain.SourceRef{Participant: "p-1", Source: domain.SourceCamera}, Label: "Ana"}}
	if err := c.Apply("demo", cards, outputs); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	c.Stop("demo")
	if c.Status("demo").Live {
		t.Fatal("stopped broadcast still live")
	}

	b, err := exec.Command("ffmpeg", "-v", "warning", "-i", out, "-f", "null", "-").CombinedOutput()
	if err != nil || strings.Contains(string(b), "Invalid") || strings.Contains(string(b), "rror") {
		t.Fatalf("broadcast does not decode cleanly: %v\n%s", err, b)
	}
	probe, _ := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", out).Output()
	if d, _ := strconv.ParseFloat(strings.TrimSpace(string(probe)), 64); d < 3 {
		t.Fatalf("expected one continuous stream of ~4 s across both runs, got %.2f s", d)
	}
}

func TestCompositorRejectsBadOutputs(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, err := New(Config{Binary: "ffmpeg", WorkDir: t.TempDir(), RTPPortMin: 47200, RTPPortMax: 47300},
		noTap{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Apply("demo", domain.CompositionPlan{Output: domain.DefaultOutput}, nil); err == nil {
		t.Fatal("no outputs accepted")
	}
	if _, err := New(Config{Binary: "ffmpeg-does-not-exist"}, noTap{}, nil); err == nil {
		t.Fatal("missing binary accepted")
	}
}

func TestPortPool(t *testing.T) {
	pool := newPortPool(47401, 47420)
	seen := map[int]bool{}
	for range 5 {
		port, err := pool.acquire()
		if err != nil || port%2 != 0 || seen[port] {
			t.Fatalf("port %d err %v", port, err)
		}
		seen[port] = true
	}
	for port := range seen {
		pool.release(port)
	}
	if _, err := newPortPool(47500, 47501).acquire(); err == nil {
		t.Fatal("an exhausted pool must fail")
	}
}
