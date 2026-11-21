package ffmpeg

import (
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

type captionLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *captionLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *captionLog) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// The caption changes on screen while the compositor keeps running: nothing restarts, and the
// frames show the background, each line in turn and the background again once it is cleared.
func TestCaptionsChangeInPlaceWithoutRecomposing(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	logs := &captionLog{}
	dir := t.TempDir()
	c, err := New(Config{Binary: "ffmpeg", WorkDir: dir, RTPPortMin: 47800, RTPPortMax: 47900},
		noTap{}, slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.cfg.FontFile == "" {
		t.Skip("no font available for drawtext")
	}
	out := dir + "/out.flv"
	small := domain.OutputSettings{Width: 640, Height: 360, Framerate: 10, VideoBitrate: 400_000, AudioBitrate: 64_000}
	plan := domain.CompositionPlan{Output: small, Title: "Demo", Captions: true}
	if err := c.Apply("demo", plan, []ports.Output{{ID: "file", URL: out}}); err != nil {
		t.Fatal(err)
	}

	// The publisher's input analysis keeps the first ~5 s of video from reaching the file, so the
	// background is observed from then on: wait for it, then change the caption.
	time.Sleep(6500 * time.Millisecond)
	c.SetCaption("demo", "Ana: primera linea\nsegunda linea de prueba")
	time.Sleep(2500 * time.Millisecond)
	c.SetCaption("demo", "Beto: corto")
	time.Sleep(2500 * time.Millisecond)
	c.SetCaption("demo", "")
	time.Sleep(2500 * time.Millisecond)
	c.Stop("demo")

	if n := strings.Count(logs.String(), "compositor starting"); n != 1 {
		t.Fatalf("captions must not recompose: %d runs\n%s", n, logs.String())
	}
	// Mean luma of the bottom band, frame by frame: text and its box change it, an empty caption
	// leaves the plain background. Runs of equal values are the moments of the test.
	runs := lumaRuns(t, out)
	if len(runs) < 4 {
		t.Fatalf("expected background, first line, second line, background again; got runs %v", runs)
	}
	base, first, second, last := runs[0], runs[1], runs[2], runs[len(runs)-1]
	const distinct = 0.3
	if abs(first-base) < distinct || abs(second-base) < distinct || abs(first-second) < distinct {
		t.Errorf("each caption must change the picture: base=%.2f first=%.2f second=%.2f (all runs %v)", base, first, second, runs)
	}
	if abs(last-base) >= distinct {
		t.Errorf("a cleared caption must leave nothing behind (no box): base=%.2f last=%.2f (all runs %v)", base, last, runs)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// lumaRuns returns the mean luma of the picture's bottom band as runs of similar frames (a run needs
// at least three frames, which drops the transitions between captions).
func lumaRuns(t *testing.T, file string) []float64 {
	t.Helper()
	b, err := exec.Command("ffmpeg", "-v", "error", "-i", file, "-vf",
		"crop=640:110:0:250,signalstats,metadata=print:key=lavfi.signalstats.YAVG:file=-", "-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("measuring the band: %v\n%s", err, b)
	}
	var values []float64
	for _, line := range strings.Split(string(b), "\n") {
		if _, v, ok := strings.Cut(line, "lavfi.signalstats.YAVG="); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				values = append(values, f)
			}
		}
	}
	var runs []float64
	count, sum := 0, 0.0
	flush := func() {
		if count >= 3 {
			runs = append(runs, sum/float64(count))
		}
		count, sum = 0, 0
	}
	for _, v := range values {
		if count > 0 && abs(v-sum/float64(count)) > 0.3 {
			flush()
		}
		count++
		sum += v
	}
	flush()
	return runs
}
