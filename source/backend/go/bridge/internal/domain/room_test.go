package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func newTestRoom(t *testing.T, capacity int) *Room {
	t.Helper()
	r, err := NewRoom("demo", RoomSettings{Title: "Demo", Host: HostInfo{Name: "Rafex"}, Capacity: capacity}, now)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRoomSettingsValidation(t *testing.T) {
	ok := RoomSettings{Title: " Demo ", Host: HostInfo{Name: "Rafex"}}
	if err := ok.Validate(); err != nil || ok.Title != "Demo" || ok.Capacity != DefaultCapacity || ok.Layout != LayoutAuto {
		t.Fatalf("defaults not applied: %+v %v", ok, err)
	}
	for _, bad := range []RoomSettings{
		{Title: "", Host: HostInfo{Name: "H"}},
		{Title: "T", Host: HostInfo{Name: ""}},
		{Title: "T", Host: HostInfo{Name: "H", Avatar: "javascript:alert(1)"}},
		{Title: "T", Host: HostInfo{Name: "H"}, Capacity: MaxCapacity + 1},
		{Title: "T", Host: HostInfo{Name: "H"}, Layout: "pip"},
		{Title: strings.Repeat("x", 121), Host: HostInfo{Name: "H"}},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("expected error for %+v", bad)
		}
	}
	if _, err := NewRoom("Bad Id", ok, now); err == nil {
		t.Fatal("invalid room id accepted")
	}
}

func TestJoinLeaveAndCapacity(t *testing.T) {
	r := newTestRoom(t, 2)
	a, err := r.Join("Ana", "", true, now)
	if err != nil || a.ID != "p-1" || a.State.CameraOff || !a.OnStage {
		t.Fatalf("join: %+v %v", a, err)
	}
	b, _ := r.Join("Beto", "Invitado", false, now)
	if !b.State.CameraOff || b.Info().Title != "Invitado" {
		t.Fatalf("second participant: %+v", b)
	}
	_, err = r.Join("Caro", "", true, now)
	var de *Error
	if !errors.As(err, &de) || de.Code != CodeRoomFull || !de.Fatal {
		t.Fatalf("expected fatal room-full, got %v", err)
	}
	if !r.Leave("p-1") || r.Leave("p-1") {
		t.Fatal("leave should succeed once")
	}
	c, _ := r.Join("Caro", "", true, now)
	if c.ID != "p-3" {
		t.Fatalf("ids must not be reused, got %s", c.ID)
	}
	if infos := r.ParticipantInfos(); len(infos) != 2 || infos[0].ID != "p-2" {
		t.Fatalf("participants = %+v", infos)
	}
	r.Close()
	if _, err := r.Join("Dani", "", true, now); !errors.As(err, &de) || de.Code != CodeRoomClosed {
		t.Fatalf("expected room-closed, got %v", err)
	}
}

func TestUpdateStageAndPublished(t *testing.T) {
	r := newTestRoom(t, 0)
	p, _ := r.Join("Ana", "", true, now)
	if _, err := r.Update(p.ID, SelfState{Muted: true}); err != nil || !p.State.Muted {
		t.Fatal("update failed")
	}
	if _, err := r.Update("p-9", SelfState{}); err == nil {
		t.Fatal("update of unknown participant accepted")
	}
	if err := r.SetOnStage(p.ID, false); err != nil || p.OnStage {
		t.Fatal("stage toggle failed")
	}
	if err := r.SetOnStage("p-9", true); err == nil {
		t.Fatal("stage toggle of unknown participant accepted")
	}
	if !r.SetPublished(p.ID, SourceCamera, true) || !p.Published[SourceCamera] {
		t.Fatal("publish not recorded")
	}
	r.SetPublished(p.ID, SourceCamera, false)
	if p.Published[SourceCamera] || r.SetPublished("p-9", SourceCamera, true) {
		t.Fatal("unpublish failed")
	}
}

func TestDestinationsAndChannels(t *testing.T) {
	r := newTestRoom(t, 0)
	yt, err := r.AddDestination(Destination{Name: "YT", URL: "rtmps://a.rtmp.youtube.com/live2/", StreamKey: "abcd-1234-efgh", Enabled: true})
	if err != nil || yt.ID != "d-1" || yt.Kind != KindYouTube || yt.URL != "rtmps://a.rtmp.youtube.com/live2" {
		t.Fatalf("youtube destination: %+v %v", yt, err)
	}
	if yt.PublishURL() != "rtmps://a.rtmp.youtube.com/live2/abcd-1234-efgh" || yt.MaskedKey() != "••••••••efgh" {
		t.Fatalf("publish url %s / masked %s", yt.PublishURL(), yt.MaskedKey())
	}
	tw, _ := r.AddDestination(Destination{Name: "TW", URL: "rtmp://live.twitch.tv/app", StreamKey: "k", Enabled: false})
	r.AddDestination(Destination{Name: "YT2", URL: "rtmp://b.rtmp.youtube.com/live2", Enabled: true})
	if ch := r.Channels(); len(ch) != 1 || ch[0] != "youtube" {
		t.Fatalf("channels = %v (disabled and duplicates excluded)", ch)
	}
	if err := r.SetDestinationEnabled(tw.ID, true); err != nil || len(r.Channels()) != 2 || len(r.EnabledDestinations()) != 3 {
		t.Fatal("enable failed")
	}
	if err := r.RemoveDestination(tw.ID); err != nil || r.RemoveDestination(tw.ID) == nil || r.SetDestinationEnabled("d-9", true) == nil {
		t.Fatal("remove failed")
	}
	if info := r.Info(); info.Title != "Demo" || info.Host.Name != "Rafex" || len(info.Channels) != 1 {
		t.Fatalf("info = %+v", info)
	}
}

func TestDestinationValidation(t *testing.T) {
	for _, d := range []Destination{
		{Name: "", URL: "rtmp://x/app"},
		{Name: "x", URL: "https://x/app"},
		{Name: "x", URL: "rtmp:///app"},
		{Name: "x", URL: "rtmp://user:pass@x/app"},
		{Name: "x", URL: "rtmp://x/app?key=1"},
		{Name: "x", URL: "rtmp://x/app", StreamKey: "a|b"},
		{Name: "x", URL: "rtmp://x/app", Kind: "tiktok"},
	} {
		if err := d.Validate(); err == nil {
			t.Errorf("expected error for %+v", d)
		}
	}
	d := Destination{Name: "fb", URL: "rtmps://live-api-s.facebook.com:443/rtmp"}
	if err := d.Validate(); err != nil || d.Kind != KindFacebook || d.PublishURL() != d.URL || d.MaskedKey() != "" {
		t.Fatalf("facebook: %+v %v", d, err)
	}
	if (Destination{StreamKey: "ab"}).MaskedKey() != "••" {
		t.Fatal("short keys must be fully masked")
	}
}

func TestProfileAndOutputValidation(t *testing.T) {
	if DefaultProfile.Validate() != nil || DefaultOutput.Validate() != nil {
		t.Fatal("defaults must be valid")
	}
	bad := DefaultProfile
	bad.Framerate = 0
	if bad.Validate() == nil {
		t.Fatal("framerate 0 accepted")
	}
	for _, o := range []OutputSettings{
		{Width: 1281, Height: 720, Framerate: 30, VideoBitrate: 1e6, AudioBitrate: 128000},
		{Width: 1280, Height: 100, Framerate: 30, VideoBitrate: 1e6, AudioBitrate: 128000},
		{Width: 1280, Height: 720, Framerate: 5, VideoBitrate: 1e6, AudioBitrate: 128000},
		{Width: 1280, Height: 720, Framerate: 30, VideoBitrate: 1, AudioBitrate: 128000},
		{Width: 1280, Height: 720, Framerate: 30, VideoBitrate: 1e6, AudioBitrate: 1},
	} {
		if o.Validate() == nil {
			t.Errorf("expected error for %+v", o)
		}
	}
	if !LayoutSpeaker.Valid() || LayoutMode("x").Valid() {
		t.Fatal("layout validation")
	}
	if Errorf(CodeConflict, "x %d", 1).Error() != "conflict: x 1" {
		t.Fatal("error format")
	}
}
