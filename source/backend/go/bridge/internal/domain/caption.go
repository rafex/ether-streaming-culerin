package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Live captions: each guest transcribes their own microphone in the browser and sends the
// line being said (`caption`). The bridge draws the latest line on the broadcast. Nothing here
// stores a transcript: only the last line lives, and it expires on its own.

const (
	// MaxCaptionRunes bounds one line of transcription (a few seconds of speech).
	MaxCaptionRunes = 240
	// CaptionLines is how many lines the overlay uses at most.
	CaptionLines = 2
)

// CleanCaption turns what a client sent into something safe to draw on the stream: control and
// text-direction characters are dropped (they would reorder or corrupt the text), every kind of
// whitespace becomes one space, and a long line keeps its END (the newest words).
func CleanCaption(s string) string {
	var b strings.Builder
	space := true // swallow leading spaces
	for _, r := range s {
		switch {
		case r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069':
			continue // text-direction embedding / override / isolate
		case unicode.IsSpace(r) || r == '\u2028' || r == '\u2029':
			if !space {
				b.WriteByte(' ')
				space = true
			}
		case unicode.IsControl(r) || r == utf8.RuneError:
			continue
		default:
			b.WriteRune(r)
			space = false
		}
	}
	out := strings.TrimRight(b.String(), " ")
	if n := utf8.RuneCountInString(out); n > MaxCaptionRunes {
		runes := []rune(out)
		out = string(runes[n-MaxCaptionRunes:])
		if i := strings.IndexByte(out, ' '); i > 0 && i < 20 {
			out = out[i+1:] // do not start in the middle of a word
		}
	}
	return out
}

// CaptionLayout picks the font size and how many characters fit on a line for an output size.
func CaptionLayout(out OutputSettings) (fontSize, lineRunes int) {
	fontSize = clampInt(out.Height/22, 16, 56)
	fontSize -= fontSize % 2
	// The text may use ~88 % of the width; an average glyph is ~0.6 em wide.
	lineRunes = max(12, int(float64(out.Width)*0.88/(0.6*float64(fontSize))))
	return fontSize, lineRunes
}

func clampInt(v, lo, hi int) int { return max(lo, min(hi, v)) }

// WrapCaption lays "speaker: text" out in at most `lines` lines of at most `width` characters,
// breaking at spaces. If it does not fit it keeps the newest words behind an ellipsis, so what is
// being said right now is always on screen. An empty text gives an empty caption (nothing drawn).
func WrapCaption(speaker, text string, width, lines int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	prefix := speaker + ": "
	for dropped := 0; ; dropped++ {
		head := prefix
		if dropped > 0 {
			head += "… "
		}
		wrapped := wrapWords(head, words[dropped:], width)
		if len(wrapped) <= lines || dropped == len(words)-1 {
			if len(wrapped) > lines { // a single huge word: cut it
				wrapped = wrapped[len(wrapped)-lines:]
			}
			return strings.Join(wrapped, "\n")
		}
	}
}

func wrapWords(head string, words []string, width int) []string {
	var lines []string
	current := head
	empty := true // nothing but the head on the line
	for _, w := range words {
		sep := " "
		if empty {
			sep = ""
		}
		if !empty && utf8.RuneCountInString(current)+len(sep)+utf8.RuneCountInString(w) > width {
			lines = append(lines, current)
			current, empty = w, false
			continue
		}
		current += sep + w
		empty = false
	}
	return append(lines, current)
}
