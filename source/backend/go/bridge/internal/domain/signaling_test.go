package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseClientMessages(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want ClientMessage
	}{
		{"peek", `{"type":"room.peek","room":"demo","token":"abc"}`, PeekMessage{Room: "demo", Token: "abc"}},
		{"join", `{"type":"join","name":"  Ana ","title":"Host","media":{"audio":true,"video":false},"resume":"p-1"}`,
			JoinMessage{Name: "Ana", Title: "Host", Audio: true, Resume: "p-1"}},
		{"join without media", `{"type":"join","name":"Ana"}`, JoinMessage{Name: "Ana"}},
		{"subscribe answer", `{"type":"subscribe.answer","sdp":"v=0"}`, SubscribeAnswerMessage{SDP: "v=0"}},
		{"self update", `{"type":"self.update","muted":true,"cameraOff":false,"screenSharing":true}`,
			SelfUpdateMessage{State: SelfState{Muted: true, ScreenSharing: true}}},
		{"ping", `{"type":"ping","t":12.5}`, PingMessage{T: 12.5}},
		{"leave", `{"type":"leave"}`, LeaveMessage{}},
		{"unknown is ignored", `{"type":"chat","text":"hola"}`, UnknownMessage{Type: "chat"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseClientMessage([]byte(tc.in))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotJSON, wantJSON := mustJSON(t, got), mustJSON(t, tc.want); gotJSON != wantJSON {
				t.Fatalf("got %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestParsePublishOfferAndIce(t *testing.T) {
	msg, err := ParseClientMessage([]byte(`{"type":"publish.offer","sdp":"v=0","sources":{"0":"camera","1":"microphone"}}`))
	if err != nil {
		t.Fatal(err)
	}
	offer := msg.(PublishOfferMessage)
	if offer.Sources["0"] != SourceCamera || offer.Sources["1"] != SourceMicrophone {
		t.Fatalf("sources = %v", offer.Sources)
	}

	msg, err = ParseClientMessage([]byte(`{"type":"ice","pc":"subscribe","candidate":{"candidate":"candidate:1 1 udp 1 1.2.3.4 5 typ host","sdpMid":"0","sdpMLineIndex":0}}`))
	if err != nil {
		t.Fatal(err)
	}
	ice := msg.(ClientIceMessage)
	if ice.PC != RoleSubscribe || *ice.Candidate.SDPMid != "0" || *ice.Candidate.SDPMLineIndex != 0 {
		t.Fatalf("ice = %+v", ice)
	}
}

func TestParseRejectsInvalidMessages(t *testing.T) {
	for _, in := range []string{
		`not json`,
		`{}`,
		`{"type":"room.peek","room":"Bad Room","token":"x"}`,
		`{"type":"room.peek","room":"demo"}`,
		`{"type":"join","name":"   "}`,
		`{"type":"join","name":"` + strings.Repeat("a", 61) + `"}`,
		`{"type":"join","name":"Ana","title":"` + strings.Repeat("t", 81) + `"}`,
		`{"type":"publish.offer","sdp":""}`,
		`{"type":"publish.offer","sdp":"v=0","sources":{"0":"webcam"}}`,
		`{"type":"subscribe.answer"}`,
		`{"type":"ice","pc":"both","candidate":{}}`,
		`{"type":"ice","pc":"publish"}`,
		`{"type":"ping"}`,
	} {
		if _, err := ParseClientMessage([]byte(in)); err == nil {
			t.Errorf("expected error for %s", in)
		}
	}
}

func TestServerMessagesCarryTheirType(t *testing.T) {
	msgs := []ServerMessage{
		NewRoomInfo("demo", "T", HostInfo{Name: "H"}, nil),
		NewWelcome("p-1", false, "rt", DefaultProfile, nil, nil),
		NewPublishAnswer("v=0"), NewSubscribeOffer("v=0"),
		NewIce(RolePublish, IceCandidate{Candidate: "c"}),
		NewTracks(map[string]TrackInfo{"0": {Participant: "p-1", Source: SourceCamera}}),
		NewParticipantJoined(ParticipantInfo{ID: "p-1", Name: "Ana"}),
		NewParticipantUpdated(ParticipantInfo{ID: "p-1", Name: "Ana"}),
		NewParticipantLeft("p-1"), NewProfile(DefaultProfile), NewPong(1),
		NewError(Fatalf(CodeKicked, "bye")),
	}
	want := []string{"room.info", "welcome", "publish.answer", "subscribe.offer", "ice", "tracks",
		"participant.joined", "participant.updated", "participant.left", "profile", "pong", "error"}
	for i, m := range msgs {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(mustJSON(t, m)), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["type"] != want[i] || m.MessageType() != want[i] {
			t.Errorf("message %d has type %v, want %s", i, decoded["type"], want[i])
		}
	}
	// Empty collections must serialise as arrays: the webrelay rejects null.
	welcome := mustJSON(t, NewWelcome("p-1", false, "rt", DefaultProfile, nil, nil))
	if !strings.Contains(welcome, `"iceServers":[]`) || !strings.Contains(welcome, `"participants":[]`) {
		t.Fatalf("welcome = %s", welcome)
	}
	if info := mustJSON(t, NewRoomInfo("demo", "T", HostInfo{Name: "H"}, nil)); !strings.Contains(info, `"channels":[]`) {
		t.Fatalf("room.info = %s", info)
	}
}

func TestMediaSource(t *testing.T) {
	if !SourceScreen.IsVideo() || SourceScreenAudio.IsVideo() || MediaSource("x").Valid() {
		t.Fatal("unexpected source classification")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestJoinCarriesTheAdmissionToken(t *testing.T) {
	msg, err := ParseClientMessage([]byte(`{"type":"join","name":"Ana","admit":"tok123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if j := msg.(JoinMessage); j.Admit != "tok123" {
		t.Fatalf("join = %+v", j)
	}
	long := `{"type":"join","name":"Ana","admit":"` + strings.Repeat("a", 257) + `"}`
	if _, err := ParseClientMessage([]byte(long)); err == nil {
		t.Fatal("an oversized admission token was accepted")
	}
}

func TestWaitingAndAdmittedMessagesSerialize(t *testing.T) {
	if got := mustJSON(t, NewWaiting()); got != `{"type":"waiting"}` {
		t.Fatalf("waiting = %s", got)
	}
	if got := mustJSON(t, NewAdmitted("abc")); got != `{"type":"admitted","token":"abc"}` {
		t.Fatalf("admitted = %s", got)
	}
}

func TestAdmissionDefaultsToOpenAndRejectsUnknownModes(t *testing.T) {
	s := RoomSettings{Title: "T", Host: HostInfo{Name: "H"}}
	if err := s.Validate(); err != nil || s.Admission != AdmissionOpen {
		t.Fatalf("default = %q err=%v", s.Admission, err)
	}
	s = RoomSettings{Title: "T", Host: HostInfo{Name: "H"}, Admission: "moderated"}
	if err := s.Validate(); err == nil {
		t.Fatal("unknown admission accepted")
	}
}
