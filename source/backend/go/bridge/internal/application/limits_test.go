package application

import (
	"fmt"
	"testing"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

func TestDestinationsMustBePublic(t *testing.T) {
	f := newFixtureWith(t, Config{})
	f.createRoom(t, "demo")
	for _, url := range []string{
		"rtmp://127.0.0.1/live", "rtmp://localhost/live", "rtmps://10.43.0.1:1935/live",
		"rtmp://192.168.1.10/live", "rtmp://169.254.169.254/latest", "rtmp://[::1]/live",
		"rtmp://ether-bridge.ether-streaming.svc.cluster.local/live", "rtmp://redis/live", "rtmp://100.64.0.5/live",
	} {
		if _, err := f.hub.AddDestination("demo", domain.Destination{Name: "x", URL: url, Enabled: true}); err == nil {
			t.Errorf("%s accepted", url)
		}
	}
	for _, url := range []string{"rtmps://a.rtmps.youtube.com:443/live2", "rtmp://live.twitch.tv/app", "rtmp://203.0.113.9/live"} {
		if _, err := f.hub.AddDestination("demo", domain.Destination{Name: "ok", URL: url, Enabled: true}); err != nil {
			t.Errorf("%s rejected: %v", url, err)
		}
	}
}

func TestPrivateDestinationsAreAllowedWhenConfigured(t *testing.T) {
	f := newFixtureWith(t, Config{AllowPrivateDestinations: true})
	f.createRoom(t, "demo")
	if _, err := f.hub.AddDestination("demo", domain.Destination{Name: "sink", URL: "rtmp://127.0.0.1/live", Enabled: true}); err != nil {
		t.Fatal(err)
	}
}

func TestRoomsAndDestinationsAreCapped(t *testing.T) {
	f := newFixtureWith(t, Config{AllowPrivateDestinations: true, MaxRooms: 2, MaxDestinations: 2})
	settings := domain.RoomSettings{Title: "T", Host: domain.HostInfo{Name: "H"}, Capacity: 2}
	for _, id := range []domain.RoomID{"a", "b"} {
		if _, err := f.hub.CreateRoom(id, settings); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.hub.CreateRoom("c", settings); err == nil {
		t.Fatal("room over the cap accepted")
	}
	if err := f.hub.CloseRoom("a"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := f.hub.AddDestination("b", domain.Destination{Name: fmt.Sprint(i), URL: "rtmp://127.0.0.1/live"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.hub.AddDestination("b", domain.Destination{Name: "3", URL: "rtmp://127.0.0.1/live"}); err == nil {
		t.Fatal("destination over the cap accepted")
	}
}

func TestDevelopmentRoomsOnDemandAreCapped(t *testing.T) {
	f := newFixtureWith(t, Config{DevMode: true, MaxRooms: 2})
	for _, id := range []domain.RoomID{"a", "b"} {
		if _, _, err := f.hub.Peek(id, "t"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := f.hub.Peek("c", "t"); err == nil {
		t.Fatal("a third on-demand room was created past the cap")
	}
}
