package application

import (
	"strings"
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

func approvalRoom(t *testing.T, f *fixture, id domain.RoomID) {
	t.Helper()
	if _, err := f.hub.CreateRoom(id, domain.RoomSettings{Title: "T", Host: domain.HostInfo{Name: "H"}, Capacity: 2, Admission: domain.AdmissionApproval}); err != nil {
		t.Fatal(err)
	}
}

// arrive opens a lobby with the room's invitation and presses Join.
func arrive(t *testing.T, f *fixture, room domain.RoomID, name string) (*Session, *fakeConn) {
	t.Helper()
	conn := &fakeConn{}
	s := NewSession(f.hub, conn, quiet)
	s.Handle([]byte(`{"type":"room.peek","room":"` + string(room) + `","token":"` + f.token(room) + `"}`))
	s.Handle([]byte(`{"type":"join","name":"` + name + `"}`))
	return s, conn
}

func waitingOf(t *testing.T, f *fixture, room domain.RoomID) []WaitingView {
	t.Helper()
	view, err := f.hub.Room(room)
	if err != nil {
		t.Fatal(err)
	}
	return view.Waiting
}

func TestApprovalRoomParksTheGuestUntilTheHostAdmits(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	s, conn := arrive(t, f, "demo", "Ana")

	if s.self != nil || conn.types() != "room.info,waiting" {
		t.Fatalf("a waiting guest must not be seated: self=%v sent=%s", s.self, conn.types())
	}
	view, _ := f.hub.Room("demo")
	if len(view.Participants) != 0 || len(view.Waiting) != 1 || view.Waiting[0].Name != "Ana" || view.Waiting[0].Admitted || view.Admission != domain.AdmissionApproval {
		t.Fatalf("view = %+v", view)
	}
	// Pressing Join again while waiting does nothing.
	s.Handle([]byte(`{"type":"join","name":"Ana"}`))
	if len(waitingOf(t, f, "demo")) != 1 {
		t.Fatal("a second join created a second waiting entry")
	}

	if err := f.hub.Admit("demo", view.Waiting[0].ID); err != nil {
		t.Fatal(err)
	}
	token := conn.last(t, "admitted").(domain.AdmittedMessage).Token
	if len(token) < 16 {
		t.Fatalf("admission token = %q", token)
	}
	if w := waitingOf(t, f, "demo"); len(w) != 1 || !w[0].Admitted {
		t.Fatalf("admitted guest should still be listed until they come back: %+v", w)
	}

	s.Handle([]byte(`{"type":"join","name":"Ana","admit":"` + token + `"}`))
	if s.self == nil {
		t.Fatalf("not seated after coming back: %s", conn.types())
	}
	conn.last(t, "welcome")
	view, _ = f.hub.Room("demo")
	if len(view.Participants) != 1 || len(view.Waiting) != 0 {
		t.Fatalf("view = %+v", view)
	}
}

func TestAnAdmissionTokenWorksOnceAndOnlyForItsSocket(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	ana, anaConn := arrive(t, f, "demo", "Ana")
	_, mallory := arrive(t, f, "demo", "Mallory")
	view, _ := f.hub.Room("demo")
	var anaID string
	for _, w := range view.Waiting {
		if w.Name == "Ana" {
			anaID = w.ID
		}
	}
	_ = f.hub.Admit("demo", anaID)
	token := anaConn.last(t, "admitted").(domain.AdmittedMessage).Token

	// Mallory is also waiting and learns Ana's token somehow: it is not hers.
	malloryS := NewSession(f.hub, mallory, quiet)
	malloryS.room, malloryS.peeked = "demo", true
	malloryS.Handle([]byte(`{"type":"join","name":"Mallory","admit":"` + token + `"}`))
	if e := mallory.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeDenied || !e.Fatal {
		t.Fatalf("stolen token: %+v", e)
	}
	if malloryS.self != nil {
		t.Fatal("Mallory got a seat with Ana's token")
	}

	ana.Handle([]byte(`{"type":"join","name":"Ana","admit":"` + token + `"}`))
	if ana.self == nil {
		t.Fatal("Ana could not use her own token")
	}
	// Used up: replaying it on a new socket gets nothing.
	replay := &fakeConn{}
	r := NewSession(f.hub, replay, quiet)
	r.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + f.token("demo") + `"}`))
	r.Handle([]byte(`{"type":"join","name":"Ana2","admit":"` + token + `"}`))
	if r.self != nil {
		t.Fatal("an admission token was reused")
	}
	if e := replay.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeDenied {
		t.Fatalf("replay: %+v", e)
	}
}

func TestGuestsCannotSkipTheWaitingRoomWithAMadeUpToken(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	for _, admit := range []string{"x", strings.Repeat("a", 32), "0123456789abcdef0123456789abcdef"} {
		conn := &fakeConn{}
		s := NewSession(f.hub, conn, quiet)
		s.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + f.token("demo") + `"}`))
		s.Handle([]byte(`{"type":"join","name":"Eve","admit":"` + admit + `"}`))
		if s.self != nil {
			t.Fatalf("seated with the made-up token %q", admit)
		}
		if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeDenied || !e.Fatal || conn.closeCode != 4405 {
			t.Fatalf("%q: %+v close=%d", admit, e, conn.closeCode)
		}
	}
	if view, _ := f.hub.Room("demo"); len(view.Participants) != 0 {
		t.Fatal("a guest got in without the host")
	}
}

func TestDenyTurnsTheGuestAway(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	_, conn := arrive(t, f, "demo", "Ana")
	id := waitingOf(t, f, "demo")[0].ID
	if err := f.hub.Deny("demo", id); err != nil {
		t.Fatal(err)
	}
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeDenied || !e.Fatal || conn.closeCode != 4405 {
		t.Fatalf("denied: %+v close=%d", e, conn.closeCode)
	}
	if len(waitingOf(t, f, "demo")) != 0 {
		t.Fatal("denied guest still listed")
	}
	if err := f.hub.Deny("demo", id); err == nil {
		t.Fatal("denying twice succeeded")
	}
	if err := f.hub.Admit("demo", id); err == nil {
		t.Fatal("admitting a guest who was turned away succeeded")
	}
	if err := f.hub.Admit("ghost", id); err == nil {
		t.Fatal("admitting in an unknown room succeeded")
	}
}

func TestWaitingGuestsLeaveTheListWhenTheyGoAway(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	closes, _ := arrive(t, f, "demo", "Ana")
	leaves, _ := arrive(t, f, "demo", "Beto")
	if len(waitingOf(t, f, "demo")) != 2 {
		t.Fatal("expected two waiting guests")
	}
	closes.Closed()
	leaves.Handle([]byte(`{"type":"leave"}`))
	if w := waitingOf(t, f, "demo"); len(w) != 0 {
		t.Fatalf("still waiting: %+v", w)
	}
}

func TestNobodyAnsweringEndsTheWait(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	_, conn := arrive(t, f, "demo", "Ana")
	f.clock.Advance(WaitingTimeout - time.Second)
	if len(waitingOf(t, f, "demo")) != 1 {
		t.Fatal("the guest was dropped too early")
	}
	f.clock.Advance(2 * time.Second)
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeDenied || conn.closeCode != 4405 {
		t.Fatalf("timeout: %+v close=%d", e, conn.closeCode)
	}
	if len(waitingOf(t, f, "demo")) != 0 {
		t.Fatal("expired guest still listed")
	}
}

func TestAnAdmittedGuestWhoNeverComesBackIsForgotten(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	arrive(t, f, "demo", "Ana")
	_ = f.hub.Admit("demo", waitingOf(t, f, "demo")[0].ID)
	f.clock.Advance(AdmitClaimTimeout + time.Second)
	if len(waitingOf(t, f, "demo")) != 0 {
		t.Fatal("an unclaimed admission lingers")
	}
}

func TestOpeningTheRoomAdmitsEveryoneWaiting(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	a, ac := arrive(t, f, "demo", "Ana")
	b, bc := arrive(t, f, "demo", "Beto")
	open := domain.AdmissionOpen
	if _, err := f.hub.UpdateRoom("demo", RoomPatch{Admission: &open}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		s    *Session
		conn *fakeConn
		name string
	}{{a, ac, "Ana"}, {b, bc, "Beto"}} {
		token := p.conn.last(t, "admitted").(domain.AdmittedMessage).Token
		p.s.Handle([]byte(`{"type":"join","name":"` + p.name + `","admit":"` + token + `"}`))
		// The room is open now, so the token is no longer needed but must not hurt.
		if p.s.self == nil {
			t.Fatalf("%s not seated after the room opened", p.name)
		}
	}
}

func TestClosingTheRoomSendsWaitingGuestsAway(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	_, conn := arrive(t, f, "demo", "Ana")
	if err := f.hub.CloseRoom("demo"); err != nil {
		t.Fatal(err)
	}
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeRoomClosed || conn.closeCode != 4410 {
		t.Fatalf("closed: %+v close=%d", e, conn.closeCode)
	}
}

func TestTheWaitingRoomHasACap(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	for i := 0; i < MaxWaiting; i++ {
		arrive(t, f, "demo", "G")
	}
	_, conn := arrive(t, f, "demo", "One too many")
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeRoomFull || !e.Fatal {
		t.Fatalf("over the cap: %+v", e)
	}
	if got := len(waitingOf(t, f, "demo")); got != MaxWaiting {
		t.Fatalf("waiting = %d", got)
	}
}

func TestAnOpenRoomIgnoresAdmission(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "open")
	s, conn := arrive(t, f, "open", "Ana")
	if s.self == nil || strings.Contains(conn.types(), "waiting") {
		t.Fatalf("an open room must seat at once: %s", conn.types())
	}
}

// Resuming a dropped session never goes through the waiting room.
func TestResumeSkipsTheWaitingRoom(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	s, conn := arrive(t, f, "demo", "Ana")
	_ = f.hub.Admit("demo", waitingOf(t, f, "demo")[0].ID)
	token := conn.last(t, "admitted").(domain.AdmittedMessage).Token
	s.Handle([]byte(`{"type":"join","name":"Ana","admit":"` + token + `"}`))
	w := conn.last(t, "welcome").(domain.WelcomeMessage)
	s.Closed()

	again := &fakeConn{}
	a := NewSession(f.hub, again, quiet)
	a.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + f.token("demo") + `"}`))
	a.Handle([]byte(`{"type":"join","name":"Ana","resume":"` + string(w.Self) + `","resumeToken":"` + w.ResumeToken + `"}`))
	if got := again.last(t, "welcome").(domain.WelcomeMessage); !got.Resumed {
		t.Fatalf("resume went through the waiting room: %s", again.types())
	}
}

func TestInvitationUseIsCountedWhenTheGuestIsSeatedNotWhenTheyWait(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	inv, _ := f.hub.Invite("demo", time.Hour, 1)
	conn := &fakeConn{}
	s := NewSession(f.hub, conn, quiet)
	s.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + inv.Token + `"}`))
	s.Handle([]byte(`{"type":"join","name":"Ana"}`))
	if list, _ := f.hub.Invites("demo"); list[0].Uses != 0 {
		t.Fatalf("waiting used the invitation: %+v", list[0])
	}
	_ = f.hub.Admit("demo", waitingOf(t, f, "demo")[0].ID)
	token := conn.last(t, "admitted").(domain.AdmittedMessage).Token
	s.Handle([]byte(`{"type":"join","name":"Ana","admit":"` + token + `"}`))
	if list, _ := f.hub.Invites("demo"); list[0].Uses != 1 {
		t.Fatalf("uses = %d", list[0].Uses)
	}
}

func TestRevokingTheInvitationWhileWaitingStopsTheSeat(t *testing.T) {
	f := newFixture(t, false)
	approvalRoom(t, f, "demo")
	inv, _ := f.hub.Invite("demo", time.Hour, 0)
	conn := &fakeConn{}
	s := NewSession(f.hub, conn, quiet)
	s.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + inv.Token + `"}`))
	s.Handle([]byte(`{"type":"join","name":"Ana"}`))
	_ = f.hub.Admit("demo", waitingOf(t, f, "demo")[0].ID)
	_ = f.hub.RevokeInvite("demo", inv.ID)
	token := conn.last(t, "admitted").(domain.AdmittedMessage).Token
	s.Handle([]byte(`{"type":"join","name":"Ana","admit":"` + token + `"}`))
	if s.self != nil {
		t.Fatal("seated through a revoked invitation")
	}
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeInvalidToken {
		t.Fatalf("error = %+v", e)
	}
}

func TestAdmissionSettingIsValidated(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	bad := domain.AdmissionMode("moderated")
	if _, err := f.hub.UpdateRoom("demo", RoomPatch{Admission: &bad}); err == nil {
		t.Fatal("unknown admission mode accepted")
	}
	if _, err := f.hub.CreateRoom("x", domain.RoomSettings{Title: "T", Host: domain.HostInfo{Name: "H"}, Admission: bad}); err == nil {
		t.Fatal("unknown admission mode accepted at creation")
	}
	if view, _ := f.hub.Room("demo"); view.Admission != domain.AdmissionOpen {
		t.Fatalf("default admission = %q", view.Admission)
	}
}
