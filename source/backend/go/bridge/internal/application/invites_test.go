package application

import (
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

func TestAnInvitationWithoutALimitAdmitsEveryone(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	inv, err := f.hub.Invite("demo", time.Hour, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Ana", "Beto"} {
		conn := &fakeConn{}
		s := NewSession(f.hub, conn, quiet)
		s.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + inv.Token + `"}`))
		s.Handle([]byte(`{"type":"join","name":"` + name + `"}`))
		conn.last(t, "welcome")
	}
	list, _ := f.hub.Invites("demo")
	if len(list) != 1 || list[0].Uses != 2 || !list[0].Active {
		t.Fatalf("list = %+v", list)
	}
}

func TestMaxUsesClosesTheInvitationAfterThatManyGuests(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	inv, _ := f.hub.Invite("demo", time.Hour, 2)
	join := func(name string) *fakeConn {
		conn := &fakeConn{}
		s := NewSession(f.hub, conn, quiet)
		s.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + inv.Token + `"}`))
		s.Handle([]byte(`{"type":"join","name":"` + name + `"}`))
		return conn
	}
	join("Ana").last(t, "welcome")
	join("Beto").last(t, "welcome")
	// Used up: the next guest is refused already at the lobby.
	conn := &fakeConn{}
	NewSession(f.hub, conn, quiet).Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + inv.Token + `"}`))
	e := conn.last(t, "error").(domain.ErrorMessage)
	if e.Code != domain.CodeInvalidToken || !e.Fatal || conn.closeCode != 4401 {
		t.Fatalf("third guest: %+v close=%d", e, conn.closeCode)
	}
	list, _ := f.hub.Invites("demo")
	if list[0].Uses != 2 || list[0].Active {
		t.Fatalf("list = %+v", list[0])
	}
}

// Two guests can both pass room.peek while one use is left; only one may take it.
func TestTheLastUseCannotBeTakenTwice(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	inv, _ := f.hub.Invite("demo", time.Hour, 1)
	peeked := func() (*Session, *fakeConn) {
		conn := &fakeConn{}
		s := NewSession(f.hub, conn, quiet)
		s.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + inv.Token + `"}`))
		return s, conn
	}
	first, firstConn := peeked()
	second, secondConn := peeked()
	first.Handle([]byte(`{"type":"join","name":"Ana"}`))
	second.Handle([]byte(`{"type":"join","name":"Beto"}`))
	firstConn.last(t, "welcome")
	if e := secondConn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeInvalidToken || !e.Fatal {
		t.Fatalf("second guest: %+v", e)
	}
	if view, _ := f.hub.Room("demo"); len(view.Participants) != 1 {
		t.Fatalf("participants = %d, want 1", len(view.Participants))
	}
}

func TestAFullRoomDoesNotBurnAnInvitationUse(t *testing.T) {
	f := newFixture(t, false)
	if _, err := f.hub.CreateRoom("tiny", domain.RoomSettings{Title: "T", Host: domain.HostInfo{Name: "H"}, Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	inv, _ := f.hub.Invite("tiny", time.Hour, 3)
	for _, name := range []string{"Ana", "Beto"} {
		conn := &fakeConn{}
		s := NewSession(f.hub, conn, quiet)
		s.Handle([]byte(`{"type":"room.peek","room":"tiny","token":"` + inv.Token + `"}`))
		s.Handle([]byte(`{"type":"join","name":"` + name + `"}`))
	}
	if list, _ := f.hub.Invites("tiny"); list[0].Uses != 1 {
		t.Fatalf("uses = %d, want 1 (the second guest found the room full)", list[0].Uses)
	}
}

func TestResumingDoesNotUseTheInvitationAgain(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	inv, _ := f.hub.Invite("demo", time.Hour, 1)
	conn := &fakeConn{}
	s := NewSession(f.hub, conn, quiet)
	s.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + inv.Token + `"}`))
	s.Handle([]byte(`{"type":"join","name":"Ana"}`))
	w := conn.last(t, "welcome").(domain.WelcomeMessage)
	s.Closed()
	again := &fakeConn{}
	// The invitation is used up, so a fresh lobby is refused...
	NewSession(f.hub, again, quiet).Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + inv.Token + `"}`))
	again.last(t, "error")
	// ...but the guest who is already in keeps their seat through the resume path
	// of a socket that peeked before the invitation ran out.
	if view, _ := f.hub.Room("demo"); len(view.Participants) != 1 || view.Participants[0].ID != w.Self {
		t.Fatalf("participants = %+v", view.Participants)
	}
}

func TestRevokedInvitationStopsAdmittingButKeepsWhoIsIn(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	inv, _ := f.hub.Invite("demo", time.Hour, 0)
	other, _ := f.hub.Invite("demo", time.Hour, 0)

	inConn := &fakeConn{}
	in := NewSession(f.hub, inConn, quiet)
	in.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + inv.Token + `"}`))
	in.Handle([]byte(`{"type":"join","name":"Ana"}`))
	inConn.last(t, "welcome")

	// A guest at the lobby when the host revokes: refused when pressing Join.
	lateConn := &fakeConn{}
	late := NewSession(f.hub, lateConn, quiet)
	late.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + inv.Token + `"}`))
	if err := f.hub.RevokeInvite("demo", inv.ID); err != nil {
		t.Fatal(err)
	}
	late.Handle([]byte(`{"type":"join","name":"Beto"}`))
	if e := lateConn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeInvalidToken || !e.Fatal {
		t.Fatalf("late guest: %+v", e)
	}

	// New lobbies with the revoked token are refused too; the other link still works.
	conn := &fakeConn{}
	NewSession(f.hub, conn, quiet).Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + inv.Token + `"}`))
	conn.last(t, "error")
	ok := &fakeConn{}
	NewSession(f.hub, ok, quiet).Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + other.Token + `"}`))
	ok.last(t, "room.info")

	if view, _ := f.hub.Room("demo"); len(view.Participants) != 1 {
		t.Fatalf("the guest already in was removed: %+v", view.Participants)
	}
	if err := f.hub.RevokeInvite("demo", "nope"); err == nil {
		t.Fatal("revoking an unknown invitation succeeded")
	}
	if err := f.hub.RevokeInvite("ghost", inv.ID); err == nil {
		t.Fatal("revoking in an unknown room succeeded")
	}
}

func TestInvitationsExpireAndAreListedWithoutTokens(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	short, _ := f.hub.Invite("demo", time.Minute, 0)
	_, _ = f.hub.Invite("demo", time.Hour, 5)
	f.clock.Advance(2 * time.Minute)

	list, err := f.hub.Invites("demo")
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v %v", list, err)
	}
	byID := map[string]InviteSummary{}
	for _, s := range list {
		byID[s.ID] = s
	}
	if s := byID[short.ID]; !s.Expired || s.Active {
		t.Fatalf("expired invitation = %+v", s)
	}
	conn := &fakeConn{}
	NewSession(f.hub, conn, quiet).Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + short.Token + `"}`))
	conn.last(t, "error")
	if _, err := f.hub.Invites("ghost"); err == nil {
		t.Fatal("listing an unknown room succeeded")
	}
}

func TestInvitationLimits(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	for _, uses := range []int{-1, MaxInviteUses + 1} {
		if _, err := f.hub.Invite("demo", time.Hour, uses); err == nil {
			t.Errorf("maxUses %d accepted", uses)
		}
	}
	for i := 0; i < MaxInvitesPerRoom; i++ {
		if _, err := f.hub.Invite("demo", time.Hour, 0); err != nil {
			t.Fatalf("invitation %d: %v", i, err)
		}
	}
	if _, err := f.hub.Invite("demo", time.Hour, 0); err == nil {
		t.Fatal("invitation over the cap accepted")
	}
	// Expired ones are forgotten, which makes room again.
	f.clock.Advance(2 * time.Hour)
	if _, err := f.hub.Invite("demo", time.Hour, 0); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
}

func TestATokenSignedForAnotherBridgeStateIsRefused(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	// Valid signature, but the registry has never heard of this invitation id
	// (a restart dropped the room's state and it was recreated).
	token := f.signer.Issue("demo", "0123456789abcdef", f.clock.Now().Add(time.Hour)).Token
	conn := &fakeConn{}
	NewSession(f.hub, conn, quiet).Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + token + `"}`))
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeInvalidToken {
		t.Fatalf("error = %+v", e)
	}
}
