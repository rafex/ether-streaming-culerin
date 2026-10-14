package application

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

// BroadcastConfig tunes the broadcast loop.
type BroadcastConfig struct {
	Output domain.OutputSettings
	// StallAfter turns a track that delivered nothing for this long into a
	// name card, so a frozen input never freezes the whole picture.
	StallAfter time.Duration
	// Debounce groups bursts of changes (join + publish + self.update).
	Debounce time.Duration
	// Tick re-evaluates track liveness periodically.
	Tick time.Duration
}

// Broadcaster drives the compositor of every live room: it recomputes the
// composition plan on room changes and on a timer, and applies it only when
// it changed.
type Broadcaster struct {
	hub   *Hub
	comp  ports.Compositor
	tap   ports.MediaTap
	clock ports.Clock
	cfg   BroadcastConfig
	log   *slog.Logger

	mu   sync.Mutex
	live map[domain.RoomID]*liveRoom
}

type liveRoom struct {
	dirty chan struct{}
	stop  chan struct{}
	done  chan struct{}
	err   string
}

// NewBroadcaster hooks itself to the hub's change notifications.
func NewBroadcaster(hub *Hub, comp ports.Compositor, tap ports.MediaTap, clock ports.Clock, cfg BroadcastConfig, log *slog.Logger) *Broadcaster {
	if cfg.StallAfter == 0 {
		cfg.StallAfter = 2500 * time.Millisecond
	}
	if cfg.Debounce == 0 {
		cfg.Debounce = 400 * time.Millisecond
	}
	if cfg.Tick == 0 {
		cfg.Tick = time.Second
	}
	b := &Broadcaster{hub: hub, comp: comp, tap: tap, clock: clock, cfg: cfg, log: log, live: map[domain.RoomID]*liveRoom{}}
	hub.OnChange(b.notify)
	return b
}

// Start puts the room on air. It needs at least one enabled destination.
func (b *Broadcaster) Start(id domain.RoomID) error {
	plan, outputs, err := b.snapshot(id)
	if err != nil {
		return err
	}
	if len(outputs) == 0 {
		return domain.Errorf(domain.CodeNoDestinations, "room %s has no enabled destination", id)
	}
	b.mu.Lock()
	if _, ok := b.live[id]; ok {
		b.mu.Unlock()
		return nil
	}
	lr := &liveRoom{dirty: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	b.live[id] = lr
	b.mu.Unlock()

	if err := b.comp.Apply(id, plan, outputs); err != nil {
		b.mu.Lock()
		delete(b.live, id)
		b.mu.Unlock()
		b.comp.Stop(id)
		return domain.Errorf(domain.CodeMedia, "cannot start the broadcast: %v", err)
	}
	b.log.Info("broadcast started", "room", id, "outputs", len(outputs))
	go b.loop(id, lr, signature(plan, outputs))
	return nil
}

// Stop takes the room off air.
func (b *Broadcaster) Stop(id domain.RoomID) {
	b.mu.Lock()
	lr, ok := b.live[id]
	if ok {
		delete(b.live, id)
	}
	b.mu.Unlock()
	if ok {
		close(lr.stop)
		<-lr.done
	}
	b.comp.Stop(id)
	if ok {
		b.log.Info("broadcast stopped", "room", id)
	}
}

// StopAll ends every broadcast (shutdown).
func (b *Broadcaster) StopAll() {
	b.mu.Lock()
	ids := make([]domain.RoomID, 0, len(b.live))
	for id := range b.live {
		ids = append(ids, id)
	}
	b.mu.Unlock()
	for _, id := range ids {
		b.Stop(id)
	}
}

// Status reports the room's broadcast.
func (b *Broadcaster) Status(id domain.RoomID) ports.BroadcastStatus {
	status := b.comp.Status(id)
	b.mu.Lock()
	_, live := b.live[id]
	b.mu.Unlock()
	status.Live = live && status.Live
	return status
}

// Plan returns what the broadcast would show right now (host preview of the
// layout, also when off air).
func (b *Broadcaster) Plan(id domain.RoomID) (domain.CompositionPlan, error) {
	plan, _, err := b.snapshot(id)
	return plan, err
}

func (b *Broadcaster) notify(id domain.RoomID) {
	b.mu.Lock()
	lr, ok := b.live[id]
	b.mu.Unlock()
	if !ok {
		return
	}
	select {
	case lr.dirty <- struct{}{}:
	default:
	}
}

func (b *Broadcaster) loop(id domain.RoomID, lr *liveRoom, applied string) {
	defer close(lr.done)
	ticker := time.NewTicker(b.cfg.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-lr.stop:
			return
		case <-ticker.C:
		case <-lr.dirty:
			select { // debounce
			case <-lr.stop:
				return
			case <-time.After(b.cfg.Debounce):
			}
		}
		plan, outputs, err := b.snapshot(id)
		if err != nil || len(outputs) == 0 {
			reason := "no enabled destination"
			if err != nil {
				reason = err.Error()
			}
			b.log.Info("broadcast ending", "room", id, "reason", reason)
			go b.Stop(id)
			<-lr.stop
			return
		}
		sig := signature(plan, outputs)
		if sig == applied {
			continue
		}
		if err := b.comp.Apply(id, plan, outputs); err != nil {
			b.log.Warn("compositor update failed", "room", id, "err", err)
			continue
		}
		applied = sig
	}
}

func (b *Broadcaster) snapshot(id domain.RoomID) (domain.CompositionPlan, []ports.Output, error) {
	now := b.clock.Now()
	live := func(ref domain.SourceRef) bool {
		last := b.tap.LastPacket(id, ref)
		return !last.IsZero() && now.Sub(last) <= b.cfg.StallAfter
	}
	return b.hub.Snapshot(id, b.cfg.Output, live)
}

func signature(plan domain.CompositionPlan, outputs []ports.Output) string {
	var b strings.Builder
	b.WriteString(plan.Signature())
	for _, o := range outputs {
		b.WriteString("|" + o.ID + "=" + o.URL)
	}
	return b.String()
}
