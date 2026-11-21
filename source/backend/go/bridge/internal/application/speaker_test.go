package application

import (
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

func mic(p domain.ParticipantID) domain.SourceRef {
	return domain.SourceRef{Participant: p, Source: domain.SourceMicrophone}
}

// twoOnStage builds a voice-activated room with Ana (p-1) and Beto (p-2) on stage, live mics and
// cameras, and a broadcaster that is not running its own loops (the test drives followSpeaker).
func twoOnStage(t *testing.T, layout domain.LayoutMode) (*fixture, *Broadcaster, *fakeTap, *domain.SpeakerTracker) {
	t.Helper()
	f, b, _, tap := newBroadcastFixture(t)
	f.guest(t, "demo", "Ana")
	f.guest(t, "demo", "Beto")
	for _, p := range []domain.ParticipantID{"p-1", "p-2"} {
		f.hub.SourcePublished("demo", p, domain.SourceCamera)
		f.hub.SourcePublished("demo", p, domain.SourceMicrophone)
	}
	if _, err := f.hub.UpdateRoom("demo", RoomPatch{Layout: &layout}); err != nil {
		t.Fatal(err)
	}
	return f, b, tap, domain.NewSpeakerTracker(domain.SpeakerParams{})
}

// converse lets `seconds` pass in 250 ms steps, sampling who is talking each time.
func converse(f *fixture, b *Broadcaster, tr *domain.SpeakerTracker, seconds float64) {
	for i := 0; i < int(seconds*4); i++ {
		f.clock.Advance(250 * time.Millisecond)
		b.followSpeaker("demo", tr)
	}
}

func focusLabel(t *testing.T, b *Broadcaster) string {
	t.Helper()
	plan, err := b.Plan("demo")
	if err != nil {
		t.Fatal(err)
	}
	for _, tile := range plan.Tiles {
		if tile.Focus {
			return tile.Label
		}
	}
	return ""
}

func TestVoiceActivatedLayoutPutsTheSpeakerInFocus(t *testing.T) {
	f, b, tap, tr := twoOnStage(t, domain.LayoutActive)
	if got := focusLabel(t, b); got != "Ana" {
		t.Fatalf("before anyone talks the first guest has the focus, got %q", got)
	}
	tap.say(mic("p-2"), 0.9)
	converse(f, b, tr, 3)
	if got := focusLabel(t, b); got != "Beto" {
		t.Fatalf("Beto talks, focus = %q", got)
	}
}

func TestVoiceActivatedLayoutDoesNotFlipOnAShortInterruption(t *testing.T) {
	f, b, tap, tr := twoOnStage(t, domain.LayoutActive)
	tap.say(mic("p-1"), 0.9)
	converse(f, b, tr, 3)
	if got := focusLabel(t, b); got != "Ana" {
		t.Fatalf("focus = %q, want Ana", got)
	}
	// Ana stops, Beto says one word (0.5 s), Ana goes on.
	tap.say(mic("p-1"), 0)
	tap.say(mic("p-2"), 0.9)
	f.clock.Advance(10 * time.Second) // well past the hold time
	converse(f, b, tr, 0.5)
	tap.say(mic("p-2"), 0)
	tap.say(mic("p-1"), 0.9)
	converse(f, b, tr, 3)
	if got := focusLabel(t, b); got != "Ana" {
		t.Fatalf("a word from Beto moved the focus to %q", got)
	}
}

func TestOtherLayoutsIgnoreWhoTalks(t *testing.T) {
	for _, layout := range []domain.LayoutMode{domain.LayoutSpeaker, domain.LayoutAuto, domain.LayoutGrid} {
		f, b, tap, tr := twoOnStage(t, layout)
		tap.say(mic("p-2"), 0.95)
		converse(f, b, tr, 5)
		want := ""
		if layout == domain.LayoutSpeaker {
			want = "Ana" // the first guest on stage, as always
		}
		if got := focusLabel(t, b); got != want {
			t.Errorf("layout %s: focus %q, want %q", layout, got, want)
		}
		if tr.Current() != "" {
			t.Errorf("layout %s: tracker follows voices", layout)
		}
	}
}

func TestLeavingTheVoiceLayoutForgetsTheSpeaker(t *testing.T) {
	f, b, tap, tr := twoOnStage(t, domain.LayoutActive)
	tap.say(mic("p-2"), 0.9)
	converse(f, b, tr, 3)
	grid := domain.LayoutGrid
	if _, err := f.hub.UpdateRoom("demo", RoomPatch{Layout: &grid}); err != nil {
		t.Fatal(err)
	}
	converse(f, b, tr, 0.5)
	active := domain.LayoutActive
	if _, err := f.hub.UpdateRoom("demo", RoomPatch{Layout: &active}); err != nil {
		t.Fatal(err)
	}
	tap.say(mic("p-2"), 0)
	if got := focusLabel(t, b); got != "Ana" {
		t.Fatalf("after leaving and coming back the focus is %q, want the first guest again", got)
	}
}

func TestTheBroadcastRecomposesWhenTheFocusMoves(t *testing.T) {
	f, b, comp, tap := newBroadcastFixture(t)
	f.hub.AddDestination("demo", domain.Destination{Name: "M", URL: "rtmp://localhost/live", StreamKey: "k", Enabled: true})
	f.guest(t, "demo", "Ana")
	f.guest(t, "demo", "Beto")
	for _, p := range []domain.ParticipantID{"p-1", "p-2"} {
		f.hub.SourcePublished("demo", p, domain.SourceCamera)
		f.hub.SourcePublished("demo", p, domain.SourceMicrophone)
	}
	layout := domain.LayoutActive
	f.hub.UpdateRoom("demo", RoomPatch{Layout: &layout})
	b.cfg.SpeakerEvery = 5 * time.Millisecond
	if err := b.Start("demo"); err != nil {
		t.Fatal(err)
	}
	tap.say(mic("p-2"), 0.9)
	go func() { // the fake clock only moves when told to
		for i := 0; i < 200; i++ {
			f.clock.Advance(250 * time.Millisecond)
			time.Sleep(3 * time.Millisecond)
		}
	}()
	eventually(t, "a plan with Beto in focus", func() bool {
		for _, tile := range comp.lastPlan().Tiles {
			if tile.Focus && tile.Label == "Beto" {
				return true
			}
		}
		return false
	})
}
