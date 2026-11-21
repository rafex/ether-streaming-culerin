package application

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

type captionLine struct {
	speaker domain.ParticipantID
	name    string
	text    string
}

func captureCaptions(f *fixture) *[]captionLine {
	var got []captionLine
	f.hub.OnCaption(func(_ domain.RoomID, speaker domain.ParticipantID, name, text string) {
		got = append(got, captionLine{speaker, name, text})
	})
	return &got
}

func enableCaptions(t *testing.T, f *fixture, on bool) {
	t.Helper()
	if _, err := f.hub.UpdateRoom("demo", RoomPatch{Captions: &on}); err != nil {
		t.Fatal(err)
	}
}

func send(s *Session, text string) { s.Handle([]byte(`{"type":"caption","text":` + quote(text) + `}`)) }

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestCaptionsAreDroppedUntilTheHostTurnsThemOn(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	got := captureCaptions(f)
	ana, _ := f.guest(t, "demo", "Ana")

	send(ana, "hola")
	if len(*got) != 0 {
		t.Fatalf("captions off: %v", *got)
	}
	enableCaptions(t, f, true)
	send(ana, "hola a todos")
	if len(*got) != 1 || (*got)[0].name != "Ana" || (*got)[0].text != "hola a todos" {
		t.Fatalf("captions on: %v", *got)
	}
}

func TestEveryGuestIsToldWhenCaptionsChangeAndNewcomersWelcomedWithTheFlag(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	_, annaConn := f.guest(t, "demo", "Ana")
	enableCaptions(t, f, true)

	last := annaConn.sent[len(annaConn.sent)-1]
	if m, ok := last.(domain.CaptionsMessage); !ok || !m.Enabled {
		t.Fatalf("Ana was not told captions are on: %s", annaConn.types())
	}
	_, betoConn := f.guest(t, "demo", "Beto")
	var welcome *domain.WelcomeMessage
	for _, m := range betoConn.sent {
		if w, ok := m.(domain.WelcomeMessage); ok {
			welcome = &w
		}
	}
	if welcome == nil || !welcome.Captions {
		t.Fatalf("a newcomer's welcome must carry captions=true: %s", betoConn.types())
	}

	n := len(annaConn.sent)
	enableCaptions(t, f, true) // no change: nothing to announce
	if len(annaConn.sent) != n {
		t.Fatal("an unchanged setting was announced again")
	}
	enableCaptions(t, f, false)
	if m, ok := annaConn.sent[len(annaConn.sent)-1].(domain.CaptionsMessage); !ok || m.Enabled {
		t.Fatalf("turning off was not announced: %s", annaConn.types())
	}
}

func TestOnlyGuestsOnStageAreCaptioned(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	enableCaptions(t, f, true)
	got := captureCaptions(f)
	ana, _ := f.guest(t, "demo", "Ana")
	if err := f.hub.SetOnStage(*ana.self, false); err != nil {
		t.Fatal(err)
	}
	send(ana, "no me ve nadie")
	if len(*got) != 0 {
		t.Fatalf("a guest off stage was captioned: %v", *got)
	}
	if err := f.hub.SetOnStage(*ana.self, true); err != nil {
		t.Fatal(err)
	}
	send(ana, "ahora sí")
	if len(*got) != 1 {
		t.Fatalf("on stage: %v", *got)
	}
}

func TestCaptionTextIsCleanedAndFloodingIsLimited(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	enableCaptions(t, f, true)
	got := captureCaptions(f)
	ana, _ := f.guest(t, "demo", "Ana")

	send(ana, "  hola\t‮mundo\u0007   feliz \n ")
	if len(*got) != 1 || (*got)[0].text != "hola mundo feliz" {
		t.Fatalf("not cleaned: %q", *got)
	}

	// A burst inside the minimum interval keeps only the first line...
	send(ana, "dos")
	if len(*got) != 1 {
		t.Fatalf("flooding passed: %v", *got)
	}
	// ...but a clear always goes through, and so does the next line after the interval.
	send(ana, "")
	if len(*got) != 2 || (*got)[1].text != "" {
		t.Fatalf("a clear was held back: %v", *got)
	}
	f.clock.Advance(captionMinInterval + time.Millisecond)
	send(ana, "tres")
	if len(*got) != 3 || (*got)[2].text != "tres" {
		t.Fatalf("after the interval: %v", *got)
	}
}

func TestAClosedRoomTakesNoCaptions(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	enableCaptions(t, f, true)
	got := captureCaptions(f)
	ana, _ := f.guest(t, "demo", "Ana")
	if err := f.hub.CloseRoom("demo"); err != nil {
		t.Fatal(err)
	}
	send(ana, "hola")
	if len(*got) != 0 {
		t.Fatalf("closed room: %v", *got)
	}
}

func TestBroadcasterDrawsTheLineWrappedAndLetsItExpire(t *testing.T) {
	f, b, comp, _ := newBroadcastFixture(t)
	f.hub.AddDestination("demo", domain.Destination{Name: "M", URL: "rtmp://localhost/live", StreamKey: "k", Enabled: true})
	enableCaptions(t, f, true)
	ana, _ := f.guest(t, "demo", "Ana")
	if err := b.Start("demo"); err != nil {
		t.Fatal(err)
	}
	if !comp.lastPlan().Captions {
		t.Fatal("the plan must reserve the captions overlay")
	}

	send(ana, "hola a todos")
	if line, _ := comp.lastCaption(); line != "Ana: hola a todos" {
		t.Fatalf("caption = %q", line)
	}
	f.clock.Advance(captionHold - time.Second)
	if line, _ := comp.lastCaption(); line == "" {
		t.Fatal("the line expired too early")
	}
	f.clock.Advance(2 * time.Second)
	if line, _ := comp.lastCaption(); line != "" {
		t.Fatalf("the line must clear itself, got %q", line)
	}

	// A newer line restarts the clock instead of being cleared by the old timer.
	f.clock.Advance(captionMinInterval)
	send(ana, "primera")
	f.clock.Advance(captionHold - time.Second)
	f.clock.Advance(captionMinInterval)
	send(ana, "segunda")
	f.clock.Advance(2 * time.Second)
	if line, _ := comp.lastCaption(); line != "Ana: segunda" {
		t.Fatalf("a stale timer cleared the newer line: %q", line)
	}
}

func TestBroadcasterIgnoresCaptionsOffAir(t *testing.T) {
	f, _, comp, _ := newBroadcastFixture(t)
	enableCaptions(t, f, true)
	ana, _ := f.guest(t, "demo", "Ana")
	send(ana, "hola")
	if _, ok := comp.lastCaption(); ok {
		t.Fatal("a caption reached the compositor while the room is off air")
	}
}

func TestTogglingCaptionsRecomposesOnlyThen(t *testing.T) {
	f, b, comp, _ := newBroadcastFixture(t)
	f.hub.AddDestination("demo", domain.Destination{Name: "M", URL: "rtmp://localhost/live", StreamKey: "k", Enabled: true})
	ana, _ := f.guest(t, "demo", "Ana")
	if err := b.Start("demo"); err != nil {
		t.Fatal(err)
	}
	applied := comp.count()
	enableCaptions(t, f, true)
	eventually(t, "a new plan with captions", func() bool { return comp.count() > applied && comp.lastPlan().Captions })
	applied = comp.count()
	for _, text := range []string{"a", "b", "c"} {
		f.clock.Advance(time.Second)
		send(ana, text)
	}
	time.Sleep(40 * time.Millisecond)
	if comp.count() != applied {
		t.Fatal("caption text changes must not recompose the broadcast")
	}
}

func TestTheHostCanTypeACaptionAndItNeedsCaptionsOn(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	got := captureCaptions(f)

	var de *domain.Error
	if err := f.hub.HostCaption("demo", "hola"); !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("captions off must be a conflict, got %v", err)
	}
	if err := f.hub.HostCaption("ghost", "hola"); !errors.As(err, &de) || de.Code != domain.CodeNotFound {
		t.Fatalf("unknown room: %v", err)
	}
	enableCaptions(t, f, true)
	if err := f.hub.HostCaption("demo", "  hola\u202e a todos "); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 || (*got)[0].name != "Rafex" || (*got)[0].text != "hola a todos" {
		t.Fatalf("host caption = %v", *got)
	}
	// Clearing goes through, and a closed room refuses.
	if err := f.hub.HostCaption("demo", ""); err != nil || (*got)[1].text != "" {
		t.Fatalf("clear: %v %v", err, *got)
	}
	if err := f.hub.CloseRoom("demo"); err != nil {
		t.Fatal(err)
	}
	if err := f.hub.HostCaption("demo", "tarde"); !errors.As(err, &de) || de.Code != domain.CodeRoomClosed {
		t.Fatalf("closed room: %v", err)
	}
}
