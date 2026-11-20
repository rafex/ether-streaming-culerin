package domain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanCaption(t *testing.T) {
	long := strings.Repeat("palabra ", 60) + "final" // 485 runes
	cases := []struct{ name, in, want string }{
		{"plain", "hola a todos", "hola a todos"},
		{"whitespace collapses", "  hola \t\n  mundo  ", "hola mundo"},
		{"control characters go", "ho\u0007la\u0000", "hola"},
		{"text direction characters go", "ho\u202ela \u2066mundo\u2069", "hola mundo"},
		{"line separators are spaces", "uno\u2028dos\u2029tres", "uno dos tres"},
		{"accents and emoji stay", "¿Qué tal? 😀", "¿Qué tal? 😀"},
		{"empty", "", ""},
		{"only invisible", " \t\u202e\n", ""},
	}
	for _, c := range cases {
		if got := CleanCaption(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	got := CleanCaption(long)
	if n := utf8.RuneCountInString(got); n > MaxCaptionRunes {
		t.Errorf("a long line must be bounded: %d runes", n)
	}
	if !strings.HasSuffix(got, "final") {
		t.Errorf("a long line keeps its newest words: %q", got[len(got)-30:])
	}
	if strings.HasPrefix(got, "alabra") {
		t.Errorf("must not start in the middle of a word: %q", got[:20])
	}
}

func TestCaptionLayoutScalesWithTheOutput(t *testing.T) {
	size, width := CaptionLayout(OutputSettings{Width: 1280, Height: 720})
	if size != 32 || width < 50 || width > 70 {
		t.Errorf("720p: size=%d width=%d", size, width)
	}
	smallSize, smallWidth := CaptionLayout(OutputSettings{Width: 640, Height: 360})
	// A smaller picture gets smaller text and, being proportional, the same characters per line.
	if smallSize >= size || smallWidth != width {
		t.Errorf("a smaller picture needs smaller text: %d/%d vs %d/%d", smallSize, smallWidth, size, width)
	}
	if s, w := CaptionLayout(OutputSettings{Width: 320, Height: 180}); s < 16 || w < 12 {
		t.Errorf("floor: %d/%d", s, w)
	}
	if s, _ := CaptionLayout(OutputSettings{Width: 3840, Height: 2160}); s > 56 || s%2 != 0 {
		t.Errorf("ceiling: %d", s)
	}
}

func TestWrapCaption(t *testing.T) {
	cases := []struct {
		name         string
		speaker, in  string
		width, lines int
		want         string
	}{
		{"fits on one line", "Ana", "hola a todos", 40, 2, "Ana: hola a todos"},
		{"wraps to a second line", "Ana", "esto es una frase un poco mas larga", 24, 2,
			"Ana: esto es una frase\nun poco mas larga"},
		{"keeps the newest words behind an ellipsis", "Ana", "uno dos tres cuatro cinco seis siete ocho nueve diez", 20, 2,
			"Ana: … seis siete\nocho nueve diez"},
		{"two full lines need no ellipsis", "Ana", "uno dos tres cuatro cinco seis siete ocho nueve diez", 40, 2,
			"Ana: uno dos tres cuatro cinco seis\nsiete ocho nueve diez"},
		{"nothing to say", "Ana", "   ", 40, 2, ""},
		{"no text", "Ana", "", 40, 2, ""},
	}
	for _, c := range cases {
		got := WrapCaption(c.speaker, c.in, c.width, c.lines)
		if got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
		if got != "" {
			if n := len(strings.Split(got, "\n")); n > c.lines {
				t.Errorf("%s: %d lines, max %d", c.name, n, c.lines)
			}
		}
	}
}

func TestWrapCaptionNeverReturnsMoreLinesThanAllowed(t *testing.T) {
	// A single word longer than the line cannot be broken; it still must not grow the block.
	got := WrapCaption("Ana", strings.Repeat("x", 200), 20, 2)
	if n := len(strings.Split(got, "\n")); n > 2 {
		t.Fatalf("lines = %d: %q", n, got)
	}
	got = WrapCaption("Ana", strings.Repeat("palabra ", 100), 30, 2)
	if n := len(strings.Split(got, "\n")); n != 2 || !strings.HasPrefix(got, "Ana: … ") {
		t.Fatalf("long text: %q", got)
	}
}
