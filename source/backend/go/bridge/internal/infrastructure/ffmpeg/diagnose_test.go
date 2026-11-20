package ffmpeg

import (
	"strings"
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

func TestClassifyExit(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		kind  string
		slave int
	}{
		{"the real YouTube cut", []string{"[out#0/tee @ 0x55588121ec80] Terminating thread with return code -32 (Broken pipe)"}, kindRemoteClosed, -1},
		{"reset on one destination", []string{"[tee @ 0x1] Slave muxer #1 failed: Connection reset by peer, continuing with 1/2 slaves."}, kindRemoteClosed, 1},
		{"no dns", []string{"[rtmp @ 0x1] Cannot open connection tcp://a.rtmp.example:1935", "Failed to resolve hostname a.rtmp.example: Name or service not known"}, kindDNS, -1},
		{"refused", []string{"[tcp @ 0x1] Connection to tcp://x:1935 failed: Connection refused"}, kindRefused, -1},
		{"bad key", []string{"[rtmp @ 0x1] Server returned 403 Forbidden"}, kindRejected, -1},
		{"timeout", []string{"[tcp @ 0x1] Connection to tcp://x:1935 failed: Connection timed out"}, kindTimeout, -1},
		{"encoder", []string{"Error while decoding stream #0:0: Invalid data found when processing input"}, kindEncoder, -1},
		{"nothing recognisable", []string{"something odd"}, kindUnknown, -1},
		{"no lines", nil, kindUnknown, -1},
		{"newest evidence wins", []string{"Connection timed out", "write: Broken pipe"}, kindRemoteClosed, -1},
	}
	for _, c := range cases {
		d := classifyExit(c.lines)
		if d.kind != c.kind || d.slave != c.slave {
			t.Errorf("%s: got kind=%s slave=%d, want kind=%s slave=%d", c.name, d.kind, d.slave, c.kind, c.slave)
		}
	}
}

func TestRestartBackoffGrowsAndIsCapped(t *testing.T) {
	want := []time.Duration{2, 2, 4, 8, 16, 30, 30}
	for i, w := range want {
		if got := restartDelay(i); got != w*time.Second {
			t.Errorf("streak %d: %v, want %vs", i, got, w)
		}
	}
}

func TestAHealthyPublisherResetsTheStreak(t *testing.T) {
	if got := nextStreak(4, 5*time.Second); got != 5 {
		t.Errorf("a quick failure must continue the streak, got %d", got)
	}
	if got := nextStreak(4, 2*time.Minute); got != 1 {
		t.Errorf("a publisher that lived a minute must start over, got %d", got)
	}
}

func TestWatchdogOnlyRepairsAFrozenOutputThatIsOnAir(t *testing.T) {
	cases := []struct {
		name        string
		onAir       bool
		tagAge      time.Duration
		sinceRepair time.Duration
		attempts    int
		want        watchdogAction
	}{
		{"fine", true, time.Second, time.Hour, 0, watchOK},
		{"starting up is not a stall", false, time.Minute, time.Hour, 0, watchOK},
		{"stalled: new run", true, 11 * time.Second, time.Hour, 0, watchRestartRun},
		{"too soon after the last repair", true, 30 * time.Second, 5 * time.Second, 1, watchOK},
		{"second attempt", true, 30 * time.Second, 20 * time.Second, 1, watchRestartRun},
		{"escalates to the publisher", true, 50 * time.Second, 20 * time.Second, 2, watchRestartAll},
	}
	for _, c := range cases {
		if got := watchdog(c.onAir, c.tagAge, c.sinceRepair, c.attempts); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func videoPlan(tiles ...domain.SourceRef) domain.CompositionPlan {
	p := domain.CompositionPlan{}
	for _, ref := range tiles {
		p.Tiles = append(p.Tiles, domain.Tile{Kind: domain.TileVideo, Source: ref})
	}
	return p
}

func TestPlanDiffNamesWhatChanged(t *testing.T) {
	cam := domain.SourceRef{Participant: "p-1", Source: domain.SourceCamera}
	screen := domain.SourceRef{Participant: "p-1", Source: domain.SourceScreen}
	got := planDiff(videoPlan(cam), videoPlan(cam, screen))
	if !strings.Contains(got, "video+=p-1/screen") || !strings.Contains(got, "tiles=1->2") {
		t.Errorf("sharing the screen: %q", got)
	}
	got = planDiff(videoPlan(cam, screen), videoPlan(cam))
	if !strings.Contains(got, "video-=p-1/screen") {
		t.Errorf("stopping the screen: %q", got)
	}
}
