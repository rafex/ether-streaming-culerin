package domain

import (
	"unicode"
)

// checkLabel rejects text a person types (names, titles) that would be shown to
// everyone, including on the live stream, but cannot be read as what it says:
// control characters, line/paragraph separators and the bidirectional
// embedding/override/isolate characters (which reorder the text around them,
// letting "Anfitrión" be spelled by someone else). Zero-width joiners stay
// allowed because emoji sequences need them. A label made only of invisible
// characters is rejected as blank.
func checkLabel(field, s string) error {
	visible := false
	for _, r := range s {
		switch {
		case unicode.IsControl(r), r == ' ', r == ' ',
			r >= '‪' && r <= '‮', r >= '⁦' && r <= '⁩':
			return Errorf(CodeBadRequest, "%s must not contain control or text-direction characters", field)
		case !unicode.Is(unicode.Cf, r) && !unicode.IsSpace(r):
			visible = true
		}
	}
	if s != "" && !visible {
		return Errorf(CodeBadRequest, "%s must have visible characters", field)
	}
	return nil
}
