package ffmpeg

import (
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

type logBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuf) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *logBuf) String() string              { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// A destination that cannot be reached kills the publisher. The log must say why (kind,
// destination, what FFmpeg printed), back off between restarts, expose the failure in the status,
// and never print the stream key.
func TestPublisherFailureIsDiagnosedBacksOffAndHidesTheKey(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	// A port nobody listens on: connection refused, right away.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	logs := &logBuf{}
	c, err := New(Config{Binary: "ffmpeg", WorkDir: t.TempDir(), RTPPortMin: 47400, RTPPortMax: 47500},
		noTap{}, slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	const key = "SECRETKEY-1234"
	outputs := []ports.Output{{ID: "yt", URL: "rtmp://" + addr + "/live/" + key}}
	small := domain.OutputSettings{Width: 320, Height: 180, Framerate: 15, VideoBitrate: 300_000, AudioBitrate: 64_000}
	if err := c.Apply("demo", domain.CompositionPlan{Output: small, Title: "Demo"}, outputs); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(12 * time.Second)
	for !strings.Contains(logs.String(), "fail_streak=2") {
		if time.Now().After(deadline) {
			t.Fatalf("no second failure logged:\n%s", logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	out := logs.String()
	for _, want := range []string{
		"publisher exited; restarting", "kind=refused", "destination=yt", "fail_streak=1", "retry_in=2s",
		"fail_streak=2", "retry_in=4s", "tail=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, key) {
		t.Errorf("the stream key leaked into the log:\n%s", out)
	}
	st := c.Status("demo")
	if st.LastErrorKind != "refused" || st.LastErrorAt.IsZero() || st.Restarts < 2 {
		t.Errorf("status = %+v", st)
	}
	if strings.Contains(st.LastError, key) {
		t.Errorf("the stream key leaked into the status: %s", st.LastError)
	}
}

// A compositor that freezes (here: SIGSTOP) while on air must be replaced by the watchdog without
// anyone touching the room, and the repair must be visible in the log and the status.
func TestAFrozenCompositorIsReplacedByTheWatchdog(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	oldEvery, oldSummary, oldStall, oldCool := healthEvery, healthSummary, stallAfter, stallCooldown
	healthEvery, healthSummary, stallAfter, stallCooldown = 200*time.Millisecond, time.Second, 1500*time.Millisecond, time.Second
	t.Cleanup(func() {
		healthEvery, healthSummary, stallAfter, stallCooldown = oldEvery, oldSummary, oldStall, oldCool
	})

	logs := &logBuf{}
	dir := t.TempDir()
	c, err := New(Config{Binary: "ffmpeg", WorkDir: dir, RTPPortMin: 47600, RTPPortMax: 47700},
		noTap{}, slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	small := domain.OutputSettings{Width: 320, Height: 180, Framerate: 15, VideoBitrate: 300_000, AudioBitrate: 64_000}
	if err := c.Apply("demo", domain.CompositionPlan{Output: small, Title: "Demo"},
		[]ports.Output{{ID: "file", URL: dir + "/out.flv"}}); err != nil {
		t.Fatal(err)
	}

	wait := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s:\n%s", what, logs.String())
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	wait("the first run on air", func() bool { return strings.Contains(logs.String(), "compositor on air") })
	wait("a healthy summary", func() bool { return strings.Contains(logs.String(), "broadcast health") })
	if strings.Contains(logs.String(), "broadcast unhealthy") {
		t.Fatalf("a healthy broadcast was flagged:\n%s", logs.String())
	}

	c.mu.Lock()
	p := c.rooms["demo"]
	c.mu.Unlock()
	p.mu.Lock()
	frozen := p.runs[p.onAir]
	p.mu.Unlock()
	if err := frozen.cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}

	wait("the stall to be reported", func() bool { return strings.Contains(logs.String(), "broadcast unhealthy") })
	wait("the watchdog to start a new run", func() bool {
		return strings.Contains(logs.String(), "output stalled; restarting the compositor run")
	})
	wait("the replacement on air", func() bool { return strings.Count(logs.String(), "compositor on air") >= 2 })
	st := c.Status("demo")
	if st.LastErrorKind != "stall" || st.Restarts < 1 {
		t.Errorf("status = %+v", st)
	}
}
