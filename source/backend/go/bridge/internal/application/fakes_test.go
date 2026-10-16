package application

import (
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeClock runs timers only when the test advances time.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Time
	fn      func()
	stopped bool
}

func (t *fakeTimer) Stop() bool { was := !t.stopped; t.stopped = true; return was }

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *fakeClock) AfterFunc(d time.Duration, f func()) ports.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now.Add(d), fn: f}
	c.timers = append(c.timers, t)
	return t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.at.After(c.now) {
			t.stopped = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.fn()
	}
}

// fakeConn records what the session sends.
type fakeConn struct {
	mu        sync.Mutex
	sent      []domain.ServerMessage
	closed    bool
	closeCode int
}

func (c *fakeConn) Send(m domain.ServerMessage) {
	c.mu.Lock()
	c.sent = append(c.sent, m)
	c.mu.Unlock()
}

func (c *fakeConn) Close(code int, _ string) {
	c.mu.Lock()
	c.closed, c.closeCode = true, code
	c.mu.Unlock()
}

func (c *fakeConn) types() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, m := range c.sent {
		out = append(out, m.MessageType())
	}
	return strings.Join(out, ",")
}

func (c *fakeConn) last(t *testing.T, typ string) domain.ServerMessage {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.sent) - 1; i >= 0; i-- {
		if c.sent[i].MessageType() == typ {
			return c.sent[i]
		}
	}
	t.Fatalf("no %s message in %v", typ, c.sent)
	return nil
}

func (c *fakeConn) reset() { c.mu.Lock(); c.sent = nil; c.mu.Unlock() }

// fakeSFU hands out fakePeers and lets tests emit SFU messages.
type fakeSFU struct {
	mu       sync.Mutex
	peers    map[domain.ParticipantID]*fakePeer
	listener ports.MediaListener
	fail     bool
}

type fakePeer struct {
	sink       ports.SignalSink
	publishes  []map[string]domain.MediaSource
	answers    []string
	cands      []domain.PeerRole
	resumed    int
	closed     bool
	publishErr error
}

func newFakeSFU() *fakeSFU { return &fakeSFU{peers: map[domain.ParticipantID]*fakePeer{}} }

func (s *fakeSFU) Attach(_ domain.RoomID, id domain.ParticipantID, sink ports.SignalSink) (ports.MediaPeer, error) {
	if s.fail {
		return nil, io.ErrClosedPipe
	}
	p := &fakePeer{sink: sink}
	s.mu.Lock()
	s.peers[id] = p
	s.mu.Unlock()
	p.sink(domain.NewTracks(map[string]domain.TrackInfo{}))
	return p, nil
}

func (s *fakeSFU) SetListener(l ports.MediaListener) { s.listener = l }

func (s *fakeSFU) peer(id domain.ParticipantID) *fakePeer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peers[id]
}

func (p *fakePeer) Publish(sdp string, sources map[string]domain.MediaSource) (string, error) {
	if p.publishErr != nil {
		return "", p.publishErr
	}
	p.publishes = append(p.publishes, sources)
	return "answer:" + sdp, nil
}

func (p *fakePeer) AnswerSubscribe(sdp string) error { p.answers = append(p.answers, sdp); return nil }

func (p *fakePeer) AddCandidate(role domain.PeerRole, _ domain.IceCandidate) error {
	p.cands = append(p.cands, role)
	return nil
}

func (p *fakePeer) Resume() { p.resumed++ }
func (p *fakePeer) Close()  { p.closed = true }

// fakeCompositor records applied plans.
type fakeCompositor struct {
	mu      sync.Mutex
	applied []domain.CompositionPlan
	outputs [][]ports.Output
	stopped int
	failing bool
}

func (c *fakeCompositor) Apply(_ domain.RoomID, plan domain.CompositionPlan, outputs []ports.Output) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failing {
		return io.ErrUnexpectedEOF
	}
	c.applied = append(c.applied, plan)
	c.outputs = append(c.outputs, outputs)
	return nil
}

func (c *fakeCompositor) Stop(domain.RoomID) { c.mu.Lock(); c.stopped++; c.mu.Unlock() }

func (c *fakeCompositor) Status(domain.RoomID) ports.BroadcastStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ports.BroadcastStatus{Live: len(c.applied) > 0 && c.stopped == 0}
}

func (c *fakeCompositor) count() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.applied) }

func (c *fakeCompositor) lastPlan() domain.CompositionPlan {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.applied[len(c.applied)-1]
}

// fakeTap reports every source as live unless told otherwise.
type fakeTap struct {
	mu    sync.Mutex
	clock *fakeClock
	dead  map[domain.SourceRef]bool
}

func (t *fakeTap) Tap(domain.RoomID, domain.SourceRef, func([]byte)) (ports.Codec, func(), error) {
	return ports.Codec{}, func() {}, nil
}
func (t *fakeTap) RequestKeyframe(domain.RoomID, domain.SourceRef) {}
func (t *fakeTap) LastPacket(_ domain.RoomID, ref domain.SourceRef) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dead[ref] {
		return time.Time{}
	}
	return t.clock.Now()
}

type fixture struct {
	clock  *fakeClock
	sfu    *fakeSFU
	hub    *Hub
	signer *domain.InviteSigner
}

func newFixture(t *testing.T, dev bool) *fixture {
	t.Helper()
	// Most tests publish to a local sink; the public-host rule has its own tests.
	return newFixtureWith(t, Config{DevMode: dev, AllowPrivateDestinations: true})
}

// newFixtureWith builds the fixture around a custom hub configuration.
func newFixtureWith(t *testing.T, cfg Config) *fixture {
	t.Helper()
	signer, err := domain.NewInviteSigner([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	clock, sfu := newFakeClock(), newFakeSFU()
	cfg.Profile, cfg.WebRelayURL = domain.DefaultProfile, "https://relay.example"
	hub := NewHub(cfg, clock, signer, sfu, quiet)
	return &fixture{clock: clock, sfu: sfu, hub: hub, signer: signer}
}

func (f *fixture) createRoom(t *testing.T, id domain.RoomID) {
	t.Helper()
	if _, err := f.hub.CreateRoom(id, domain.RoomSettings{Title: "Demo", Host: domain.HostInfo{Name: "Rafex"}, Capacity: 3}); err != nil {
		t.Fatal(err)
	}
}

// token returns a working invitation for an existing room; for a room that does
// not exist it forges one with a valid signature (the hub must still refuse it).
func (f *fixture) token(id domain.RoomID) string {
	if inv, err := f.hub.Invite(id, time.Hour, 0); err == nil {
		return inv.Token
	}
	return f.signer.Issue(id, "ffffffffffffffff", f.clock.Now().Add(time.Hour)).Token
}

// guest opens a session and joins.
func (f *fixture) guest(t *testing.T, room domain.RoomID, name string) (*Session, *fakeConn) {
	t.Helper()
	conn := &fakeConn{}
	s := NewSession(f.hub, conn, quiet)
	s.Handle([]byte(`{"type":"room.peek","room":"` + string(room) + `","token":"` + f.token(room) + `"}`))
	s.Handle([]byte(`{"type":"join","name":"` + name + `","media":{"audio":true,"video":true}}`))
	if s.self == nil {
		t.Fatalf("%s did not join: %s", name, conn.types())
	}
	return s, conn
}
