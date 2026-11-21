package domain

import (
	"slices"
	"time"
)

// SpeakerParams tune how the voice-activated layout follows the conversation. Every cut is a
// recomposition of the broadcast (a ~2 s stall), so the tracker is deliberately slow and sticky:
// it would rather keep the wrong person on screen a moment than flip between two who interrupt
// each other.
type SpeakerParams struct {
	// Enter is the activity (0..1, the share of the last ~0.5 s that carried speech) a guest needs
	// to be a candidate for the focus.
	Enter float64
	// Leave is the activity under which the person in focus counts as having stopped talking.
	Leave float64
	// Confirm is how long a candidate must keep out-talking the others before the focus moves.
	Confirm time.Duration
	// Hold is the shortest time between two changes of focus.
	Hold time.Duration
}

// DefaultSpeakerParams: 1.2 s of clear speech to take the focus, never more than one cut per 8 s.
var DefaultSpeakerParams = SpeakerParams{
	Enter:   0.35,
	Leave:   0.12,
	Confirm: 1200 * time.Millisecond,
	Hold:    8 * time.Second,
}

// SpeakerTracker decides who has the focus from how much each guest is talking. It holds no
// clock: callers pass the time, so it is deterministic under test.
type SpeakerTracker struct {
	params SpeakerParams

	current   ParticipantID
	changedAt time.Time
	candidate ParticipantID
	since     time.Time
}

// NewSpeakerTracker builds a tracker; zero params fall back to DefaultSpeakerParams.
func NewSpeakerTracker(p SpeakerParams) *SpeakerTracker {
	if p == (SpeakerParams{}) {
		p = DefaultSpeakerParams
	}
	return &SpeakerTracker{params: p}
}

// Current is who has the focus ("" until somebody has spoken).
func (t *SpeakerTracker) Current() ParticipantID { return t.current }

// Reset forgets everything (the layout stopped following voices).
func (t *SpeakerTracker) Reset() { *t = SpeakerTracker{params: t.params} }

// Update takes how much each guest on stage is talking at now and returns the guest in focus and
// whether it changed. Guests missing from the map are not on stage (or not eligible).
func (t *SpeakerTracker) Update(now time.Time, activity map[ParticipantID]float64) (ParticipantID, bool) {
	currentGone := false
	if _, ok := activity[t.current]; t.current != "" && !ok {
		currentGone = true
	}

	best, bestLevel := t.loudest(activity)
	if best == "" || bestLevel < t.params.Enter || best == t.current {
		t.candidate = ""
		return t.current, t.dropIfGone(currentGone, now)
	}

	if t.candidate != best {
		t.candidate, t.since = best, now
	}
	if now.Sub(t.since) < t.params.Confirm {
		return t.current, t.dropIfGone(currentGone, now)
	}
	// The person in focus keeps it while still talking, and a fresh cut waits for Hold; a focus
	// whose owner left the stage (or nobody yet) is replaced at once.
	free := t.current == "" || currentGone
	if !free {
		if activity[t.current] >= t.params.Leave || now.Sub(t.changedAt) < t.params.Hold {
			return t.current, false
		}
	}
	t.current, t.changedAt, t.candidate = best, now, ""
	return t.current, true
}

// dropIfGone clears a focus whose owner left the stage; it reports whether the focus changed.
func (t *SpeakerTracker) dropIfGone(gone bool, now time.Time) bool {
	if !gone {
		return false
	}
	t.current, t.changedAt = "", now
	return true
}

// loudest is the guest with the highest activity (ties go to the earliest id, for determinism).
func (t *SpeakerTracker) loudest(activity map[ParticipantID]float64) (ParticipantID, float64) {
	ids := make([]ParticipantID, 0, len(activity))
	for id := range activity {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var best ParticipantID
	level := -1.0
	for _, id := range ids {
		if activity[id] > level {
			best, level = id, activity[id]
		}
	}
	return best, level
}
