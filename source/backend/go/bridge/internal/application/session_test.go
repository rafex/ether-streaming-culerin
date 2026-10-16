package application

import (
	"errors"
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

func TestPeekValidatesTheInvitation(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")

	conn := &fakeConn{}
	s := NewSession(f.hub, conn, quiet)
	s.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + f.token("demo") + `"}`))
	info := conn.last(t, "room.info").(domain.RoomInfoMessage)
	if info.Title != "Demo" || info.Host.Name != "Rafex" || len(info.Channels) != 0 {
		t.Fatalf("room.info = %+v", info)
	}

	for name, tc := range map[string]struct {
		room, token string
		code        domain.ErrorCode
		close       int
	}{
		"forged token":  {"demo", "nope", domain.CodeInvalidToken, 4401},
		"unknown room":  {"ghost", "x", domain.CodeRoomNotFound, 4404},
		"other's token": {"demo", f.token("other"), domain.CodeInvalidToken, 4401},
	} {
		conn := &fakeConn{}
		NewSession(f.hub, conn, quiet).Handle([]byte(`{"type":"room.peek","room":"` + tc.room + `","token":"` + tc.token + `"}`))
		e := conn.last(t, "error").(domain.ErrorMessage)
		if e.Code != tc.code || !e.Fatal || !conn.closed || conn.closeCode != tc.close {
			t.Errorf("%s: got %+v closed=%v code=%d", name, e, conn.closed, conn.closeCode)
		}
	}

	issuedBefore := f.token("demo") // a link handed out before the host closes the room
	if err := f.hub.CloseRoom("demo"); err != nil {
		t.Fatal(err)
	}
	conn = &fakeConn{}
	NewSession(f.hub, conn, quiet).Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + issuedBefore + `"}`))
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeRoomClosed {
		t.Fatalf("closed room: %+v", e)
	}
}

func TestDevModeMirrorsTheDevelopmentBridge(t *testing.T) {
	f := newFixture(t, true)
	conn := &fakeConn{}
	NewSession(f.hub, conn, quiet).Handle([]byte(`{"type":"room.peek","room":"e2e-lobby","token":"anything"}`))
	info := conn.last(t, "room.info").(domain.RoomInfoMessage)
	if info.Title != "Sala de pruebas · e2e-lobby" || info.Host.Name != "Anfitrión de pruebas" || len(info.Channels) != 2 {
		t.Fatalf("dev room.info = %+v", info)
	}
	for _, tc := range []struct{ room, token string }{{"x", "invalid"}, {"closed", "t"}} {
		conn := &fakeConn{}
		NewSession(f.hub, conn, quiet).Handle([]byte(`{"type":"room.peek","room":"` + tc.room + `","token":"` + tc.token + `"}`))
		if e := conn.last(t, "error").(domain.ErrorMessage); !e.Fatal {
			t.Fatalf("expected fatal error for %+v", tc)
		}
	}
	// A dev room can be adopted by the host without losing it.
	view, err := f.hub.CreateRoom("e2e-lobby", domain.RoomSettings{Title: "Real", Host: domain.HostInfo{Name: "H"}})
	if err != nil || view.Ephemeral || view.Title != "Real" {
		t.Fatalf("adopt: %+v %v", view, err)
	}
	if _, err := f.hub.CreateRoom("e2e-lobby", domain.RoomSettings{Title: "Real", Host: domain.HostInfo{Name: "H"}}); err == nil {
		t.Fatal("duplicate room accepted")
	}
}

func TestJoinWelcomeAndPresence(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	_, ana := f.guest(t, "demo", "Ana")
	if ana.types() != "room.info,welcome,tracks" {
		t.Fatalf("welcome must precede SFU messages: %s", ana.types())
	}
	w := ana.last(t, "welcome").(domain.WelcomeMessage)
	if w.Self != "p-1" || w.Resumed || w.Profile != domain.DefaultProfile || len(w.Participants) != 1 {
		t.Fatalf("welcome = %+v", w)
	}

	beto, betoConn := f.guest(t, "demo", "Beto")
	joined := ana.last(t, "participant.joined").(domain.ParticipantMessage)
	if joined.Participant.Name != "Beto" {
		t.Fatalf("joined = %+v", joined)
	}
	if w := betoConn.last(t, "welcome").(domain.WelcomeMessage); len(w.Participants) != 2 {
		t.Fatalf("Beto sees %d participants", len(w.Participants))
	}

	beto.Handle([]byte(`{"type":"self.update","muted":true,"cameraOff":true,"screenSharing":false}`))
	updated := ana.last(t, "participant.updated").(domain.ParticipantMessage)
	if !updated.Participant.Muted || !updated.Participant.CameraOff {
		t.Fatalf("updated = %+v", updated)
	}

	beto.Handle([]byte(`{"type":"leave"}`))
	if left := ana.last(t, "participant.left").(domain.ParticipantLeftMessage); left.ID != "p-2" {
		t.Fatalf("left = %+v", left)
	}
	if !f.sfu.peer("p-2").closed {
		t.Fatal("media not closed on leave")
	}
	view, _ := f.hub.Room("demo")
	if len(view.Participants) != 1 {
		t.Fatalf("participants after leave: %d", len(view.Participants))
	}
}

func TestMediaNegotiationIsDelegated(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	s, conn := f.guest(t, "demo", "Ana")
	s.Handle([]byte(`{"type":"publish.offer","sdp":"OFFER","sources":{"0":"camera"}}`))
	if a := conn.last(t, "publish.answer").(domain.SDPMessage); a.SDP != "answer:OFFER" {
		t.Fatalf("answer = %+v", a)
	}
	s.Handle([]byte(`{"type":"subscribe.answer","sdp":"ANSWER"}`))
	s.Handle([]byte(`{"type":"ice","pc":"publish","candidate":{"candidate":"c"}}`))
	s.Handle([]byte(`{"type":"ice","pc":"subscribe","candidate":{"candidate":""}}`))
	peer := f.sfu.peer("p-1")
	if len(peer.publishes) != 1 || peer.publishes[0]["0"] != domain.SourceCamera || len(peer.answers) != 1 || len(peer.cands) != 1 {
		t.Fatalf("peer = %+v", peer)
	}

	peer.publishErr = errors.New("bad sdp")
	conn.reset()
	s.Handle([]byte(`{"type":"publish.offer","sdp":"X","sources":{}}`))
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeMedia || e.Fatal {
		t.Fatalf("publish failure must be a non-fatal error: %+v", e)
	}

	s.Handle([]byte(`{"type":"ping","t":42}`))
	if p := conn.last(t, "pong").(domain.PongMessage); p.T != 42 {
		t.Fatalf("pong = %+v", p)
	}
	s.Handle([]byte(`{"type":"wave"}`))
	s.Handle([]byte(`garbage`))
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeBadRequest || e.Fatal {
		t.Fatalf("garbage: %+v", e)
	}
}

func TestOrderingRules(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	conn := &fakeConn{}
	s := NewSession(f.hub, conn, quiet)
	s.Handle([]byte(`{"type":"publish.offer","sdp":"x","sources":{}}`))
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeNotJoined {
		t.Fatalf("publish before join: %+v", e)
	}
	s.Handle([]byte(`{"type":"subscribe.answer","sdp":"x"}`))
	s.Handle([]byte(`{"type":"self.update"}`))
	s.Handle([]byte(`{"type":"ice","pc":"publish","candidate":{"candidate":"c"}}`))
	s.Handle([]byte(`{"type":"join","name":"Ana"}`))
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeRoomNotFound || !e.Fatal {
		t.Fatalf("join before peek: %+v", e)
	}
}

func TestRoomFullAndAttachFailure(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo") // capacity 3
	for _, n := range []string{"A", "B", "C"} {
		f.guest(t, "demo", n)
	}
	conn := &fakeConn{}
	s := NewSession(f.hub, conn, quiet)
	s.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + f.token("demo") + `"}`))
	s.Handle([]byte(`{"type":"join","name":"D"}`))
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeRoomFull || conn.closeCode != 4429 {
		t.Fatalf("room full: %+v", e)
	}

	g := newFixture(t, false)
	g.createRoom(t, "demo")
	g.sfu.fail = true
	conn = &fakeConn{}
	s = NewSession(g.hub, conn, quiet)
	s.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + g.token("demo") + `"}`))
	s.Handle([]byte(`{"type":"join","name":"A"}`))
	if e := conn.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeMedia {
		t.Fatalf("attach failure: %+v", e)
	}
	if view, _ := g.hub.Room("demo"); len(view.Participants) != 0 {
		t.Fatal("participant kept after attach failure")
	}
}

func TestResumeKeepsTheParticipant(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	_, ana := f.guest(t, "demo", "Ana")
	beto, betoConn := f.guest(t, "demo", "Beto")
	token := betoConn.last(t, "welcome").(domain.WelcomeMessage).ResumeToken
	if token == "" {
		t.Fatal("welcome must hand out a resume token")
	}

	beto.Closed() // socket dropped
	view, _ := f.hub.Room("demo")
	if len(view.Participants) != 2 || view.Participants[1].Connected {
		t.Fatalf("dropped participant must stay, disconnected: %+v", view.Participants)
	}
	f.clock.Advance(10 * time.Second)

	conn := &fakeConn{}
	again := NewSession(f.hub, conn, quiet)
	again.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + f.token("demo") + `"}`))
	again.Handle([]byte(`{"type":"join","name":"Beto","resume":"p-2","resumeToken":"` + token + `"}`))
	w := conn.last(t, "welcome").(domain.WelcomeMessage)
	if w.Self != "p-2" || !w.Resumed || w.ResumeToken != token || f.sfu.peer("p-2").resumed != 1 {
		t.Fatalf("resume failed: %+v", w)
	}
	f.clock.Advance(time.Minute) // the old grace timer must have been cancelled
	if view, _ := f.hub.Room("demo"); len(view.Participants) != 2 {
		t.Fatal("resumed participant removed by a stale timer")
	}

	// SFU messages now reach the new socket.
	f.sfu.peer("p-2").sink(domain.NewSubscribeOffer("v=0"))
	conn.last(t, "subscribe.offer")

	// Without resume within the grace period the participant leaves.
	again.Closed()
	f.clock.Advance(21 * time.Second)
	if left := ana.last(t, "participant.left").(domain.ParticipantLeftMessage); left.ID != "p-2" {
		t.Fatalf("left = %+v", left)
	}
}

// Participant ids are sequential and listed to everyone in the room, so
// knowing one must not be enough to take that participant's session over.
func TestResumeNeedsTheSecretToken(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	_, _ = f.guest(t, "demo", "Ana")
	_, betoConn := f.guest(t, "demo", "Beto")
	betoToken := betoConn.last(t, "welcome").(domain.WelcomeMessage).ResumeToken

	for name, body := range map[string]string{
		"no token":    `{"type":"join","name":"Mallory","resume":"p-2"}`,
		"wrong token": `{"type":"join","name":"Mallory","resume":"p-2","resumeToken":"AAAAAAAAAAAAAAAAAAAAAA"}`,
		"other's own": `{"type":"join","name":"Mallory","resume":"p-2","resumeToken":"` + betoToken[:len(betoToken)-1] + `x"}`,
	} {
		conn := &fakeConn{}
		attacker := NewSession(f.hub, conn, quiet)
		attacker.Handle([]byte(`{"type":"room.peek","room":"demo","token":"` + f.token("demo") + `"}`))
		attacker.Handle([]byte(body))
		w := conn.last(t, "welcome").(domain.WelcomeMessage)
		if w.Resumed || w.Self == "p-2" {
			t.Fatalf("%s: session hijacked: %+v", name, w)
		}
		if w.ResumeToken == "" || w.ResumeToken == betoToken {
			t.Fatalf("%s: the new guest needs its own token, got %q", name, w.ResumeToken)
		}
		if betoConn.closeCode != 0 {
			t.Fatalf("%s: the victim's socket was closed (%d)", name, betoConn.closeCode)
		}
		if f.sfu.peer("p-2").resumed != 0 {
			t.Fatalf("%s: the victim's media peer was resumed", name)
		}
		attacker.Handle([]byte(`{"type":"leave"}`)) // free the seat for the next case
	}
}

func TestKickAndCloseRoom(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	_, ana := f.guest(t, "demo", "Ana")
	_, beto := f.guest(t, "demo", "Beto")

	if err := f.hub.Kick(ParticipantRef{"demo", "p-2"}); err != nil {
		t.Fatal(err)
	}
	if e := beto.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeKicked || !e.Fatal || beto.closeCode != 4403 {
		t.Fatalf("kick: %+v", e)
	}
	ana.last(t, "participant.left")
	if f.hub.Kick(ParticipantRef{"demo", "p-2"}) == nil {
		t.Fatal("kicking twice must fail")
	}

	if err := f.hub.CloseRoom("demo"); err != nil {
		t.Fatal(err)
	}
	if e := ana.last(t, "error").(domain.ErrorMessage); e.Code != domain.CodeRoomClosed || ana.closeCode != 4410 {
		t.Fatalf("close: %+v", e)
	}
	view, _ := f.hub.Room("demo")
	if !view.Closed || len(view.Participants) != 0 {
		t.Fatalf("closed room = %+v", view)
	}
	if f.hub.CloseRoom("ghost") == nil {
		t.Fatal("closing an unknown room must fail")
	}
}
