package application

import (
	"errors"
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

func newBroadcastFixture(t *testing.T) (*fixture, *Broadcaster, *fakeCompositor, *fakeTap) {
	t.Helper()
	f := newFixture(t, false)
	f.createRoom(t, "demo")
	comp := &fakeCompositor{}
	tap := &fakeTap{clock: f.clock, dead: map[domain.SourceRef]bool{}}
	b := NewBroadcaster(f.hub, comp, tap, f.clock, BroadcastConfig{
		Output: domain.DefaultOutput, Debounce: time.Millisecond, Tick: 5 * time.Millisecond,
	}, quiet)
	t.Cleanup(b.StopAll)
	return f, b, comp, tap
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestBroadcastNeedsADestination(t *testing.T) {
	f, b, comp, _ := newBroadcastFixture(t)
	var de *domain.Error
	if err := b.Start("demo"); !errors.As(err, &de) || de.Code != domain.CodeNoDestinations {
		t.Fatalf("expected no-destinations, got %v", err)
	}
	if err := b.Start("ghost"); err == nil {
		t.Fatal("unknown room went live")
	}
	f.hub.AddDestination("demo", domain.Destination{Name: "M", URL: "rtmp://localhost/live", StreamKey: "k", Enabled: true})
	comp.failing = true
	if err := b.Start("demo"); err == nil || b.Status("demo").Live {
		t.Fatal("compositor failure must abort the start")
	}
}

func TestBroadcastFollowsTheRoom(t *testing.T) {
	f, b, comp, tap := newBroadcastFixture(t)
	f.hub.AddDestination("demo", domain.Destination{Name: "M", URL: "rtmp://localhost/live", StreamKey: "k", Enabled: true})
	if err := b.Start("demo"); err != nil {
		t.Fatal(err)
	}
	if err := b.Start("demo"); err != nil {
		t.Fatal("starting twice must be a no-op")
	}
	if comp.count() != 1 || len(comp.lastPlan().Tiles) != 0 || comp.outputs[0][0].URL != "rtmp://localhost/live/k" {
		t.Fatalf("first plan = %+v", comp.applied)
	}
	if !b.Status("demo").Live {
		t.Fatal("status must be live")
	}

	f.guest(t, "demo", "Ana")
	f.hub.SourcePublished("demo", "p-1", domain.SourceCamera)
	eventually(t, "a plan with Ana's camera", func() bool {
		p := comp.lastPlan()
		return len(p.Tiles) == 1 && p.Tiles[0].Kind == domain.TileVideo
	})
	applied := comp.count()
	time.Sleep(30 * time.Millisecond)
	if comp.count() != applied {
		t.Fatal("an unchanged plan must not be re-applied")
	}

	// A stalled track becomes a card without any room event.
	tap.mu.Lock()
	tap.dead[domain.SourceRef{Participant: "p-1", Source: domain.SourceCamera}] = true
	tap.mu.Unlock()
	eventually(t, "the stalled camera as a card", func() bool { return comp.lastPlan().Tiles[0].Kind == domain.TileCard })

	if plan, err := b.Plan("demo"); err != nil || len(plan.Tiles) != 1 {
		t.Fatalf("preview plan: %+v %v", plan, err)
	}

	b.Stop("demo")
	if b.Status("demo").Live || comp.stopped == 0 {
		t.Fatal("stop failed")
	}
}

func TestBroadcastEndsWhenTheRoomCloses(t *testing.T) {
	f, b, comp, _ := newBroadcastFixture(t)
	f.hub.AddDestination("demo", domain.Destination{Name: "M", URL: "rtmp://localhost/live", Enabled: true})
	if err := b.Start("demo"); err != nil {
		t.Fatal(err)
	}
	f.hub.CloseRoom("demo")
	eventually(t, "the broadcast to stop", func() bool {
		comp.mu.Lock()
		defer comp.mu.Unlock()
		return comp.stopped > 0
	})
	if _, err := b.Plan("demo"); err == nil {
		t.Fatal("closed rooms have no plan")
	}
}

func TestBroadcastEndsWithoutDestinations(t *testing.T) {
	f, b, comp, _ := newBroadcastFixture(t)
	d, _ := f.hub.AddDestination("demo", domain.Destination{Name: "M", URL: "rtmp://localhost/live", Enabled: true})
	if err := b.Start("demo"); err != nil {
		t.Fatal(err)
	}
	f.hub.SetDestinationEnabled("demo", d.ID, false)
	eventually(t, "the broadcast to stop", func() bool {
		comp.mu.Lock()
		defer comp.mu.Unlock()
		return comp.stopped > 0
	})
}
