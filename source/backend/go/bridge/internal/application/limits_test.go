package application

import (
	"fmt"
	"testing"
	"time"

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

func TestClosedRoomsDoNotUseUpTheLimit(t *testing.T) {
	f := newFixtureWith(t, Config{MaxRooms: 2})
	settings := domain.RoomSettings{Title: "T", Host: domain.HostInfo{Name: "H"}, Capacity: 2}
	for _, id := range []domain.RoomID{"a", "b"} {
		if _, err := f.hub.CreateRoom(id, settings); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.hub.CreateRoom("c", settings); err == nil {
		t.Fatal("a third open room was accepted")
	}
	if err := f.hub.CloseRoom("a"); err != nil {
		t.Fatal(err)
	}
	// However many rooms have been closed before, only the open ones count.
	for i := 0; i < 5; i++ {
		id := domain.RoomID(fmt.Sprintf("n%d", i))
		if _, err := f.hub.CreateRoom(id, settings); err != nil {
			t.Fatalf("round %d: closed rooms are using up the limit: %v", i, err)
		}
		if err := f.hub.CloseRoom(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.hub.CreateRoom("a", settings); err == nil {
		t.Fatal("the id of a closed room was reused")
	}
	open := 0
	for _, r := range f.hub.Rooms() {
		if !r.Closed {
			open++
		}
	}
	if open != 1 {
		t.Fatalf("open rooms = %d, want 1 (only b)", open)
	}
}

func TestOldClosedRoomsAreForgotten(t *testing.T) {
	f := newFixtureWith(t, Config{MaxRooms: 1})
	settings := domain.RoomSettings{Title: "T", Host: domain.HostInfo{Name: "H"}, Capacity: 2}
	total := MaxClosedRooms + 20
	for i := 0; i < total; i++ {
		id := domain.RoomID(fmt.Sprintf("r%03d", i))
		if _, err := f.hub.CreateRoom(id, settings); err != nil {
			t.Fatalf("room %d: %v", i, err)
		}
		f.clock.Advance(time.Second) // distinct creation times: the oldest goes first
		if err := f.hub.CloseRoom(id); err != nil {
			t.Fatal(err)
		}
	}
	// One more create triggers the pruning.
	if _, err := f.hub.CreateRoom("last", settings); err != nil {
		t.Fatal(err)
	}
	rooms := f.hub.Rooms()
	if len(rooms) > MaxClosedRooms+1 {
		t.Fatalf("%d rooms kept, want at most %d closed + 1 open", len(rooms), MaxClosedRooms)
	}
	have := map[domain.RoomID]bool{}
	for _, r := range rooms {
		have[r.ID] = true
	}
	if have["r000"] || !have[domain.RoomID(fmt.Sprintf("r%03d", total-1))] || !have["last"] {
		t.Fatalf("the oldest closed room must go, the newest and the open one stay: %v", have)
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
