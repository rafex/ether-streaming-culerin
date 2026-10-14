package ffmpeg

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// encodeRun produces a short FLV the way a compositor run does.
func encodeRun(t *testing.T, dir, name, video string) []byte {
	t.Helper()
	out := filepath.Join(dir, name+".flv")
	args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", video + "=s=640x360:r=30:d=3",
		"-f", "lavfi", "-i", "sine=f=440:r=48000:d=3",
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-pix_fmt", "yuv420p",
		"-b:v", "800k", "-g", "60", "-keyint_min", "60", "-sc_threshold", "0", "-bf", "0",
		"-c:a", "aac", "-b:a", "128k", "-ar", "48000", "-ac", "2",
		"-f", "flv", "-flvflags", "no_duration_filesize", "-y", out}
	if b, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, b)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSplicedRunsDecodeCleanly(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	run1 := encodeRun(t, dir, "run1", "testsrc2")
	run2 := encodeRun(t, dir, "run2", "smptebars")

	clock := time.Unix(0, 0)
	now := func() time.Time { return clock }
	var out bytes.Buffer
	s := newSplicer(&out, now)
	if err := s.feed(1, bytes.NewReader(run1), func() {}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(3 * time.Second)
	if err := s.feed(2, bytes.NewReader(run2), func() {}); err != nil {
		t.Fatal(err)
	}
	spliced := filepath.Join(dir, "spliced.flv")
	if err := os.WriteFile(spliced, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := exec.Command("ffmpeg", "-v", "warning", "-i", spliced, "-f", "null", "-").CombinedOutput()
	if err != nil || strings.TrimSpace(string(b)) != "" {
		t.Fatalf("spliced stream does not decode cleanly: %v\n%s", err, b)
	}
	probe, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", spliced).Output()
	if err != nil {
		t.Fatal(err)
	}
	if d := strings.TrimSpace(string(probe)); !strings.HasPrefix(d, "5.") && !strings.HasPrefix(d, "6.") {
		t.Fatalf("spliced duration = %s, want about 6 s", d)
	}
}
