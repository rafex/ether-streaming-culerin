package sfu

import (
	"errors"
	"testing"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/rtcerr"
)

// A guest leaving while an offer is being made closes the connection under it: that is not a
// warning. A failure on a live peer still is.
func TestAClosedPeerIsNotAWarning(t *testing.T) {
	closedErr := &rtcerr.InvalidStateError{Err: webrtc.ErrConnectionClosed}
	other := errors.New("sdp: bad")

	live := &peer{}
	if !live.closing(closedErr) {
		t.Error("InvalidStateError{ErrConnectionClosed} on a live peer must count as closing")
	}
	if live.closing(other) {
		t.Error("an unrelated failure on a live peer must stay a warning")
	}

	gone := &peer{closed: true}
	if !gone.closing(other) {
		t.Error("anything failing on a peer that was closed is expected")
	}
}
