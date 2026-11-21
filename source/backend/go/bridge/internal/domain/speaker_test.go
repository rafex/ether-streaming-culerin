package domain

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func at(sec float64) time.Time { return t0.Add(time.Duration(sec * float64(time.Second))) }

func act(a, b float64) map[ParticipantID]float64 {
	return map[ParticipantID]float64{"p-1": a, "p-2": b}
}

// run feeds the same activity every 250 ms between from and to (seconds) and returns the last focus
// and every change seen.
func run(t *SpeakerTracker, from, to float64, a map[ParticipantID]float64) (ParticipantID, []ParticipantID) {
	var changes []ParticipantID
	var cur ParticipantID
	for s := from; s <= to+1e-9; s += 0.25 {
		var changed bool
		cur, changed = t.Update(at(s), a)
		if changed {
			changes = append(changes, cur)
		}
	}
	return cur, changes
}

func TestNobodyTalkingMeansNoFocus(t *testing.T) {
	tr := NewSpeakerTracker(SpeakerParams{})
	cur, changes := run(tr, 0, 10, act(0.02, 0.01))
	if cur != "" || len(changes) != 0 {
		t.Fatalf("focus %q changes %v, want none", cur, changes)
	}
}

func TestFirstVoiceTakesTheFocusAfterConfirm(t *testing.T) {
	tr := NewSpeakerTracker(SpeakerParams{})
	if cur, _ := run(tr, 0, 1.0, act(0.9, 0)); cur != "" {
		t.Fatalf("focus %q after 1 s of speech, want none yet (Confirm = 1.2 s)", cur)
	}
	cur, changes := run(tr, 1.25, 3, act(0.9, 0))
	if cur != "p-1" || len(changes) != 1 {
		t.Fatalf("focus %q changes %v, want p-1 once", cur, changes)
	}
}

func TestACoughDoesNotMoveTheFocus(t *testing.T) {
	tr := NewSpeakerTracker(SpeakerParams{})
	run(tr, 0, 3, act(0.9, 0)) // p-1 has the focus
	cur, changes := run(tr, 12, 12.75, act(0, 0.9))
	if cur != "p-1" || len(changes) != 0 {
		t.Fatalf("0.75 s of p-2 moved the focus: %q %v", cur, changes)
	}
}

func TestFocusMovesToWhoKeepsTalkingOnceThePreviousStopped(t *testing.T) {
	tr := NewSpeakerTracker(SpeakerParams{})
	run(tr, 0, 3, act(0.9, 0))
	cur, changes := run(tr, 12, 14, act(0.02, 0.9))
	if cur != "p-2" || len(changes) != 1 || changes[0] != "p-2" {
		t.Fatalf("focus %q changes %v, want p-2", cur, changes)
	}
}

func TestTheSpeakerKeepsTheFocusWhileStillTalking(t *testing.T) {
	tr := NewSpeakerTracker(SpeakerParams{})
	run(tr, 0, 3, act(0.9, 0))
	// p-2 interrupts for a long time, but p-1 never stops: no cut.
	cur, changes := run(tr, 12, 30, act(0.5, 0.95))
	if cur != "p-1" || len(changes) != 0 {
		t.Fatalf("an interruption stole the focus: %q %v", cur, changes)
	}
}

func TestChangesAreSpacedByTheHoldTime(t *testing.T) {
	tr := NewSpeakerTracker(SpeakerParams{})
	run(tr, 0, 3, act(0.9, 0)) // p-1 at ~1.5 s
	// p-1 stops and p-2 talks right away: still held until Hold (8 s) after the first cut.
	cur, _ := run(tr, 3.25, 7, act(0, 0.9))
	if cur != "p-1" {
		t.Fatalf("focus %q moved before the hold time", cur)
	}
	cur, changes := run(tr, 7.25, 10, act(0, 0.9))
	if cur != "p-2" || len(changes) != 1 {
		t.Fatalf("focus %q changes %v, want p-2 after the hold", cur, changes)
	}
}

func TestAFocusWhoseOwnerLeftTheStageIsClearedAndReplacedAtOnce(t *testing.T) {
	tr := NewSpeakerTracker(SpeakerParams{})
	run(tr, 0, 3, act(0.9, 0)) // p-1 in focus
	// p-1 leaves the stage: gone from the map, nobody else talking → cleared.
	cur, changed := tr.Update(at(3.5), map[ParticipantID]float64{"p-2": 0})
	if cur != "" || !changed {
		t.Fatalf("got %q changed=%v, want cleared", cur, changed)
	}
	// p-2 talks: takes the focus with only the confirm delay (no hold, nobody to replace).
	cur, _ = run(tr, 4, 6, map[ParticipantID]float64{"p-2": 0.9})
	if cur != "p-2" {
		t.Fatalf("focus %q, want p-2", cur)
	}
}

func TestEqualActivityBreaksTiesByID(t *testing.T) {
	tr := NewSpeakerTracker(SpeakerParams{})
	cur, _ := run(tr, 0, 3, act(0.8, 0.8))
	if cur != "p-1" {
		t.Fatalf("focus %q, want p-1 (earliest id on a tie)", cur)
	}
}

func TestResetForgetsTheFocus(t *testing.T) {
	tr := NewSpeakerTracker(SpeakerParams{})
	run(tr, 0, 3, act(0.9, 0))
	tr.Reset()
	if tr.Current() != "" {
		t.Fatalf("focus %q after reset", tr.Current())
	}
}
