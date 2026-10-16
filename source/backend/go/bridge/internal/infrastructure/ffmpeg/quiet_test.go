package ffmpeg

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
)

func TestOnlyOurOwnPipeCloseIsQuiet(t *testing.T) {
	closed := &os.PathError{Op: "read", Path: "|0", Err: os.ErrClosed}
	if got := feedFailureLevel(closed); got != slog.LevelDebug {
		t.Errorf("a pipe we closed: level %v, want debug", got)
	}
	if got := feedFailureLevel(fmt.Errorf("splice: %w", closed)); got != slog.LevelDebug {
		t.Errorf("wrapped: level %v, want debug", got)
	}
	for _, err := range []error{io.ErrUnexpectedEOF, errors.New("bad FLV tag"), io.EOF} {
		if got := feedFailureLevel(err); got != slog.LevelWarn {
			t.Errorf("%v: level %v, want warn", err, got)
		}
	}
}
