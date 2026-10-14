package application

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

func TestHostRoomManagement(t *testing.T) {
	f := newFixture(t, false)
	view, err := f.hub.CreateRoom("", domain.RoomSettings{Title: "Auto", Host: domain.HostInfo{Name: "H"}})
	if err != nil || !strings.HasPrefix(string(view.ID), "sala-") {
		t.Fatalf("random id: %+v %v", view, err)
	}
	if _, err := f.hub.CreateRoom("x", domain.RoomSettings{}); err == nil {
		t.Fatal("invalid settings accepted")
	}
	f.createRoom(t, "demo")
	f.clock.Advance(time.Second)
	f.createRoom(t, "newer")
	if rooms := f.hub.Rooms(); len(rooms) != 3 || rooms[0].ID != "newer" {
		t.Fatalf("rooms must be newest first: %+v", rooms)
	}

	title, layout, capacity := "Nuevo", domain.LayoutGrid, 5
	updated, err := f.hub.UpdateRoom("demo", RoomPatch{Title: &title, Layout: &layout, Capacity: &capacity})
	if err != nil || updated.Title != "Nuevo" || updated.Layout != domain.LayoutGrid || updated.Capacity != 5 {
		t.Fatalf("update: %+v %v", updated, err)
	}
	bad := domain.LayoutMode("pip")
	if _, err := f.hub.UpdateRoom("demo", RoomPatch{Layout: &bad}); err == nil {
		t.Fatal("invalid layout accepted")
	}
	if _, err := f.hub.UpdateRoom("ghost", RoomPatch{}); err == nil {
		t.Fatal("unknown room updated")
	}
	if _, err := f.hub.Room("ghost"); err == nil {
		t.Fatal("unknown room found")
	}
}

func TestInvitationLinks(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	inv, err := f.hub.Invite("demo", 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(inv.URL)
	if err != nil || u.Host != "relay.example" || u.Path != "/join/demo" || u.Query().Get("t") != inv.Token {
		t.Fatalf("invite url = %s", inv.URL)
	}
	if err := f.signer.Verify("demo", inv.Token, f.clock.Now().Add(time.Hour)); err != nil {
		t.Fatalf("issued token invalid: %v", err)
	}
	if !inv.ExpiresAt.Equal(f.clock.Now().Add(2 * time.Hour)) {
		t.Fatalf("expiry = %s", inv.ExpiresAt)
	}
	if def, _ := f.hub.Invite("demo", 0); !def.ExpiresAt.Equal(f.clock.Now().Add(24 * time.Hour)) {
		t.Fatalf("default ttl expiry = %s", def.ExpiresAt)
	}
	if _, err := f.hub.Invite("demo", 31*24*time.Hour); err == nil {
		t.Fatal("ttl above 30 days accepted")
	}
	if _, err := f.hub.Invite("ghost", time.Hour); err == nil {
		t.Fatal("invite for unknown room")
	}
	f.hub.CloseRoom("demo")
	if _, err := f.hub.Invite("demo", time.Hour); err == nil {
		t.Fatal("invite for a closed room")
	}
}

func TestDestinationsNeverLeakTheKey(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	d, err := f.hub.AddDestination("demo", domain.Destination{Name: "YT", URL: "rtmps://a.rtmp.youtube.com/live2", StreamKey: "secret-key-9876", Enabled: true})
	if err != nil || d.StreamKey == "secret-key-9876" || !strings.HasSuffix(d.StreamKey, "9876") {
		t.Fatalf("destination view = %+v %v", d, err)
	}
	view, _ := f.hub.Room("demo")
	if len(view.Destinations) != 1 || strings.Contains(view.Destinations[0].StreamKey, "secret") || view.Channels[0] != "youtube" {
		t.Fatalf("room view leaks or misses data: %+v", view)
	}
	if _, err := f.hub.AddDestination("demo", domain.Destination{Name: "bad", URL: "http://x"}); err == nil {
		t.Fatal("invalid destination accepted")
	}
	if _, err := f.hub.AddDestination("ghost", domain.Destination{}); err == nil {
		t.Fatal("destination for unknown room")
	}
	if err := f.hub.SetDestinationEnabled("demo", d.ID, false); err != nil {
		t.Fatal(err)
	}
	_, outputs, _ := f.hub.Snapshot("demo", domain.DefaultOutput, func(domain.SourceRef) bool { return true })
	if len(outputs) != 0 {
		t.Fatalf("disabled destination in outputs: %v", outputs)
	}
	f.hub.SetDestinationEnabled("demo", d.ID, true)
	_, outputs, _ = f.hub.Snapshot("demo", domain.DefaultOutput, func(domain.SourceRef) bool { return true })
	if len(outputs) != 1 || outputs[0].URL != "rtmps://a.rtmp.youtube.com/live2/secret-key-9876" {
		t.Fatalf("outputs = %v", outputs)
	}
	if err := f.hub.RemoveDestination("demo", d.ID); err != nil || f.hub.RemoveDestination("demo", d.ID) == nil {
		t.Fatal("remove destination")
	}
}

func TestStageAndPublishedTracksFeedTheView(t *testing.T) {
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	f.guest(t, "demo", "Ana")
	f.hub.SourcePublished("demo", "p-1", domain.SourceCamera)
	f.hub.SourcePublished("demo", "p-1", domain.SourceMicrophone)
	f.hub.SourceUnpublished("demo", "p-1", domain.SourceMicrophone)
	f.hub.SourcePublished("ghost", "p-1", domain.SourceCamera) // ignored
	if err := f.hub.SetOnStage(ParticipantRef{"demo", "p-1"}, false); err != nil {
		t.Fatal(err)
	}
	view, _ := f.hub.Room("demo")
	p := view.Participants[0]
	if p.OnStage || len(p.Published) != 1 || p.Published[0] != domain.SourceCamera || !p.Connected {
		t.Fatalf("participant view = %+v", p)
	}
	if f.hub.SetOnStage(ParticipantRef{"demo", "p-9"}, true) == nil {
		t.Fatal("unknown participant put on stage")
	}
}
