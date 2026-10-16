package ffmpeg

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hostile are names a guest could pick to make drawtext do something other than
// print them. With its default text expansion a `%` starts a function and anything
// invalid (even a lone "100% real") aborts the filter, which keeps a composition
// from ever going on air. `%%` is not an escape, so labels are printed with
// expansion=none.
var hostile = []string{
	"%{n}", "%{localtime}", "%{metadata:title}", "%{eif:1+1:d}", "100% real", "%", "%%", "%{", "}%{",
	`a\b`, "it's", "a:b", "x,y;z", "[a]", "#{b}", "ñandú 山田",
}

func TestLabelTextIsWrittenLiterally(t *testing.T) {
	r := &composeRun{dir: t.TempDir()}
	for _, name := range hostile {
		path := r.text("label-0", name)
		if filepath.Dir(path) != r.dir || filepath.Base(path) != "label-0.txt" {
			t.Fatalf("label file escaped the run directory: %s", path)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// drawtext runs with expansion=none: what it prints is exactly the file.
		if string(got) != name {
			t.Errorf("%q written as %q", name, got)
		}
	}
}

// With a real FFmpeg: none of the hostile names may stop drawtext from starting.
func TestDrawtextAcceptsHostileNames(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	font := firstExisting(
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", "/usr/share/fonts/dejavu/DejaVuSans.ttf",
		"/System/Library/Fonts/Supplemental/Arial.ttf", "/Library/Fonts/Arial.ttf")
	if font == "" {
		t.Skip("no font available")
	}
	r := &composeRun{dir: t.TempDir()}
	for _, name := range hostile {
		file := r.text("label-0", name)
		filter := drawtext(font, file, 24, "10", "10", true)
		out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi",
			"-i", "color=c=black:s=320x120:d=0.2:r=5", "-vf", filter, "-frames:v", "1", "-f", "null", "-").CombinedOutput()
		if err != nil {
			t.Errorf("drawtext failed for %q: %v\n%s", name, err, out)
		}
	}
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func TestEveryLabelFilterTurnsExpansionOff(t *testing.T) {
	args, err := composeArgs(graphParams{plan: plan(), inputs: nil, font: "/f/a.ttf", text: text})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(args, " ")
	n := strings.Count(line, "drawtext=")
	if n == 0 || n != strings.Count(line, ":expansion=none:") {
		t.Fatalf("%d drawtext filters, %d with expansion=none:\n%s", n, strings.Count(line, ":expansion=none:"), line)
	}
}
