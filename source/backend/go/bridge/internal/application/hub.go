// Package application implements the bridge's use cases: the signaling v2
// sessions of the webrelay guests, the host's room management and the
// broadcast lifecycle. It depends only on domain and ports.
package application

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

// Config tunes the hub.
type Config struct {
	// DevMode creates rooms on demand and accepts any invitation token except
	// the literal "invalid" (room "closed" does not exist). Mirrors the
	// webrelay's development bridge so its E2E runs unchanged. Never enable
	// it on a reachable deployment.
	DevMode     bool
	Profile     domain.QualityProfile
	IceServers  []domain.IceServer
	InviteTTL   time.Duration
	WebRelayURL string
	// ResumeGrace is how long a participant whose socket dropped keeps its
	// place (and media) waiting for `join {resume}`.
	ResumeGrace time.Duration
	// MaxRooms and MaxDestinations bound what the admin API (or, in development
	// mode, on-demand rooms) can allocate in memory; zero picks the default.
	MaxRooms        int
	MaxDestinations int
	// AllowPrivateDestinations lets destinations point at loopback/private
	// hosts (local RTMP sinks in development); otherwise they must be public.
	AllowPrivateDestinations bool
}

// Defaults for Config.MaxRooms and Config.MaxDestinations.
const (
	DefaultMaxRooms        = 50
	DefaultMaxDestinations = 8
)

// DevChannels are shown for on-demand development rooms without destinations.
var DevChannels = []string{string(domain.KindYouTube), string(domain.KindTwitch)}

// Hub owns every room and the participants' connections. A single mutex
// keeps the model consistent; adapters are never called while holding it,
// except for the non-blocking Connection.Send.
type Hub struct {
	mu       sync.Mutex
	cfg      Config
	clock    ports.Clock
	signer   *domain.InviteSigner
	sfu      ports.SFU
	log      *slog.Logger
	rooms    map[domain.RoomID]*roomEntry
	onChange func(domain.RoomID)
}

type roomEntry struct {
	room    *domain.Room
	members map[domain.ParticipantID]*member
	invites map[string]*inviteRecord // by invitation id
	waiting map[string]*waitingGuest // by the id the host sees
}

// waitingGuest is someone with a valid invitation parked in the waiting room of
// an approval-mode room: no seat, no media session, no presence for the others.
type waitingGuest struct {
	id     string
	name   string
	title  string
	conn   ports.Connection
	since  time.Time
	token  string // set once the host admitted them: the one-time key to `join`
	timer  ports.Timer
	invite string
}

// Waiting room limits.
const (
	MaxWaiting = 20
	// WaitingTimeout is how long a guest waits for the host; AdmitClaimTimeout how
	// long an admitted guest has to come back with the token.
	WaitingTimeout    = 5 * time.Minute
	AdmitClaimTimeout = 30 * time.Second
)

// inviteRecord is what the bridge remembers about one issued invitation: the
// token itself is a signed bearer credential, but whether it still works (not
// revoked, uses left) is decided here.
type inviteRecord struct {
	id        string
	createdAt time.Time
	expiresAt time.Time
	maxUses   int // 0: unlimited
	uses      int
	revoked   bool
}

// usable reports why a record cannot admit another guest, or nil.
func (r *inviteRecord) usable(now time.Time) error {
	switch {
	case r.revoked:
		return domain.Fatalf(domain.CodeInvalidToken, "this invitation was revoked")
	case !now.Before(r.expiresAt):
		return domain.Fatalf(domain.CodeInvalidToken, "invitation token is invalid or expired")
	case r.maxUses > 0 && r.uses >= r.maxUses:
		return domain.Fatalf(domain.CodeInvalidToken, "this invitation has no uses left")
	}
	return nil
}

// Limits of the invitation registry.
const (
	MaxInvitesPerRoom = 100
	MaxInviteUses     = 1000
)

func newRoomEntry(room *domain.Room) *roomEntry {
	return &roomEntry{room: room, members: map[domain.ParticipantID]*member{}, invites: map[string]*inviteRecord{},
		waiting: map[string]*waitingGuest{}}
}

type member struct {
	conn  ports.Connection // nil while disconnected (grace period)
	peer  ports.MediaPeer
	grace ports.Timer
	// resumeToken is the secret needed to take this participant over after a
	// dropped socket. Participant ids are sequential and public, so the id
	// alone must never be enough.
	resumeToken string
}

// newResumeToken returns 128 random bits, URL-safe.
func newResumeToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("no randomness for the resume token: %v", err)) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewHub wires the hub; it registers itself as the SFU's listener.
func NewHub(cfg Config, clock ports.Clock, signer *domain.InviteSigner, sfu ports.SFU, log *slog.Logger) *Hub {
	if cfg.InviteTTL == 0 {
		cfg.InviteTTL = 24 * time.Hour
	}
	if cfg.MaxRooms <= 0 {
		cfg.MaxRooms = DefaultMaxRooms
	}
	if cfg.MaxDestinations <= 0 {
		cfg.MaxDestinations = DefaultMaxDestinations
	}
	if cfg.ResumeGrace == 0 {
		cfg.ResumeGrace = 20 * time.Second
	}
	h := &Hub{cfg: cfg, clock: clock, signer: signer, sfu: sfu, log: log,
		rooms: map[domain.RoomID]*roomEntry{}, onChange: func(domain.RoomID) {}}
	sfu.SetListener(h)
	return h
}

// OnChange registers the callback invoked (outside the lock) after any
// change that may affect a room's broadcast.
func (h *Hub) OnChange(fn func(domain.RoomID)) { h.onChange = fn }

// ---------------------------------------------------------------------------
// Signaling use cases
// ---------------------------------------------------------------------------

// Peek validates an invitation and returns what the lobby shows.
//
// It also returns the id of the invitation the token belongs to ("" in
// development mode), which Join needs to count the use.
func (h *Hub) Peek(id domain.RoomID, token string) (domain.RoomInfoMessage, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var inviteID string
	entry := h.rooms[id]
	if h.cfg.DevMode {
		if token == "invalid" {
			return domain.RoomInfoMessage{}, "", domain.Fatalf(domain.CodeInvalidToken, "invitation token is invalid or expired")
		}
		if id == "closed" {
			return domain.RoomInfoMessage{}, "", domain.Fatalf(domain.CodeRoomNotFound, "room %s does not exist", id)
		}
		if entry == nil {
			if h.openRoomsLocked() >= h.cfg.MaxRooms {
				return domain.RoomInfoMessage{}, "", domain.Fatalf(domain.CodeConflict, "too many rooms")
			}
			room, err := domain.NewRoom(id, domain.RoomSettings{
				Title: "Sala de pruebas · " + string(id),
				Host:  domain.HostInfo{Name: "Anfitrión de pruebas"},
			}, h.clock.Now())
			if err != nil {
				return domain.RoomInfoMessage{}, "", err
			}
			room.Ephemeral = true
			entry = newRoomEntry(room)
			h.rooms[id] = entry
		}
	} else {
		if entry == nil {
			return domain.RoomInfoMessage{}, "", domain.Fatalf(domain.CodeRoomNotFound, "room %s does not exist", id)
		}
		claims, err := h.signer.Verify(id, token, h.clock.Now())
		if err != nil {
			return domain.RoomInfoMessage{}, "", err
		}
		rec := entry.invites[claims.ID]
		if rec == nil {
			// Signed by this bridge but not in its registry: issued before a restart
			// that dropped the room's state, or for a room that was recreated.
			return domain.RoomInfoMessage{}, "", domain.Fatalf(domain.CodeInvalidToken, "invitation token is invalid or expired")
		}
		if err := rec.usable(h.clock.Now()); err != nil {
			return domain.RoomInfoMessage{}, "", err
		}
		inviteID = claims.ID
	}
	if entry.room.Closed {
		return domain.RoomInfoMessage{}, "", domain.Fatalf(domain.CodeRoomClosed, "room %s is closed", id)
	}
	info := entry.room.Info()
	if entry.room.Ephemeral && len(info.Channels) == 0 {
		info.Channels = slices.Clone(DevChannels)
	}
	return info, inviteID, nil
}

// JoinResult is what a session keeps after joining.
type JoinResult struct {
	Self ParticipantRef
	Peer ports.MediaPeer
	// Waiting is the id of the waiting-room entry when the guest was not seated
	// but parked until the host admits them (Self and Peer are then empty).
	Waiting string
}

// ParticipantRef locates a participant.
type ParticipantRef struct {
	Room        domain.RoomID
	Participant domain.ParticipantID
}

// Join admits a guest (or resumes a dropped one), sends `welcome` and
// attaches its media peer. The welcome always precedes any SFU message.
func (h *Hub) Join(id domain.RoomID, conn ports.Connection, msg domain.JoinMessage, inviteID string) (JoinResult, error) {
	h.mu.Lock()
	entry := h.rooms[id]
	if entry == nil {
		h.mu.Unlock()
		return JoinResult{}, domain.Fatalf(domain.CodeRoomNotFound, "room %s does not exist", id)
	}
	if msg.Resume != "" {
		m, ok := entry.members[msg.Resume]
		if ok && subtle.ConstantTimeCompare([]byte(m.resumeToken), []byte(msg.ResumeToken)) != 1 {
			// Someone who only knows the (public) id: never take the session over.
			// Treat it as a brand-new guest instead of an error, so clients that
			// predate the token merely lose their old seat.
			h.log.Warn("resume refused: bad token", "room", id, "participant", msg.Resume)
			ok = false
		}
		if ok && m.peer != nil {
			if m.grace != nil {
				m.grace.Stop()
				m.grace = nil
			}
			previous := m.conn
			m.conn = conn
			conn.Send(domain.NewWelcome(msg.Resume, true, m.resumeToken, h.cfg.Profile, h.cfg.IceServers, entry.room.ParticipantInfos()))
			peer := m.peer
			h.mu.Unlock()
			if previous != nil && previous != conn {
				previous.Close(4409, "replaced by a newer connection")
			}
			h.log.Info("participant resumed", "room", id, "participant", msg.Resume)
			peer.Resume()
			return JoinResult{Self: ParticipantRef{id, msg.Resume}, Peer: peer}, nil
		}
	}
	// The invitation may have been revoked, or used up, since this socket showed it.
	var rec *inviteRecord
	if inviteID != "" {
		if rec = entry.invites[inviteID]; rec == nil {
			h.mu.Unlock()
			return JoinResult{}, domain.Fatalf(domain.CodeInvalidToken, "invitation token is invalid or expired")
		}
		if err := rec.usable(h.clock.Now()); err != nil {
			h.mu.Unlock()
			return JoinResult{}, err
		}
	}
	if entry.room.Settings.Admission == domain.AdmissionApproval && !entry.room.Closed {
		if h.claimAdmissionLocked(entry, conn, msg.Admit) == nil {
			if msg.Admit != "" {
				h.mu.Unlock()
				return JoinResult{}, domain.Fatalf(domain.CodeDenied, "the host's approval is no longer valid")
			}
			res, err := h.enqueueWaitingLocked(id, entry, conn, msg, inviteID)
			h.mu.Unlock()
			return res, err
		}
	}
	p, err := entry.room.Join(msg.Name, msg.Title, msg.Video, h.clock.Now())
	if err != nil {
		h.mu.Unlock()
		return JoinResult{}, err
	}
	if rec != nil {
		rec.uses++ // only a guest who really got a seat uses the invitation up
	}
	m := &member{conn: conn, resumeToken: newResumeToken()}
	entry.members[p.ID] = m
	conn.Send(domain.NewWelcome(p.ID, false, m.resumeToken, h.cfg.Profile, h.cfg.IceServers, entry.room.ParticipantInfos()))
	h.broadcastLocked(entry, p.ID, domain.NewParticipantJoined(p.Info()))
	h.mu.Unlock()
	h.log.Info("participant joined", "room", id, "participant", p.ID, "name", p.Name)

	ref := ParticipantRef{id, p.ID}
	peer, err := h.sfu.Attach(id, p.ID, func(out domain.ServerMessage) { h.sendTo(ref, out) })
	if err != nil {
		h.Leave(ref)
		return JoinResult{}, domain.Errorf(domain.CodeMedia, "cannot create media session: %v", err)
	}
	h.mu.Lock()
	if current, ok := h.memberLocked(ref); ok && current == m {
		m.peer = peer
		h.mu.Unlock()
	} else {
		h.mu.Unlock()
		peer.Close()
		return JoinResult{}, domain.Fatalf(domain.CodeNotJoined, "participant left while joining")
	}
	h.onChange(id)
	return JoinResult{Self: ref, Peer: peer}, nil
}

// UpdateSelf stores a participant's state and tells the others.
func (h *Hub) UpdateSelf(ref ParticipantRef, state domain.SelfState) error {
	h.mu.Lock()
	entry := h.rooms[ref.Room]
	if entry == nil {
		h.mu.Unlock()
		return domain.Errorf(domain.CodeNotJoined, "room %s does not exist", ref.Room)
	}
	p, err := entry.room.Update(ref.Participant, state)
	if err != nil {
		h.mu.Unlock()
		return err
	}
	h.broadcastLocked(entry, ref.Participant, domain.NewParticipantUpdated(p.Info()))
	h.mu.Unlock()
	h.onChange(ref.Room)
	return nil
}

// Leave removes a participant and closes its media.
func (h *Hub) Leave(ref ParticipantRef) {
	h.mu.Lock()
	m := h.removeLocked(ref)
	h.mu.Unlock()
	if m == nil {
		return
	}
	if m.peer != nil {
		m.peer.Close()
	}
	h.log.Info("participant left", "room", ref.Room, "participant", ref.Participant)
	h.onChange(ref.Room)
}

// Disconnected starts the resume grace period for a dropped socket.
func (h *Hub) Disconnected(ref ParticipantRef, conn ports.Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m, ok := h.memberLocked(ref)
	if !ok || m.conn != conn {
		return
	}
	m.conn = nil
	m.grace = h.clock.AfterFunc(h.cfg.ResumeGrace, func() {
		h.mu.Lock()
		current, ok := h.memberLocked(ref)
		expired := ok && current == m && m.conn == nil
		h.mu.Unlock()
		if expired {
			h.Leave(ref)
		}
	})
}

// SourcePublished implements ports.MediaListener.
func (h *Hub) SourcePublished(room domain.RoomID, p domain.ParticipantID, s domain.MediaSource) {
	h.setPublished(room, p, s, true)
}

// SourceUnpublished implements ports.MediaListener.
func (h *Hub) SourceUnpublished(room domain.RoomID, p domain.ParticipantID, s domain.MediaSource) {
	h.setPublished(room, p, s, false)
}

func (h *Hub) setPublished(room domain.RoomID, p domain.ParticipantID, s domain.MediaSource, published bool) {
	h.mu.Lock()
	entry := h.rooms[room]
	changed := entry != nil && entry.room.SetPublished(p, s, published)
	h.mu.Unlock()
	if changed {
		h.onChange(room)
	}
}

// ---------------------------------------------------------------------------
// Host use cases
// ---------------------------------------------------------------------------

// CreateRoom opens a room; an empty id gets a random slug.
func (h *Hub) CreateRoom(id domain.RoomID, settings domain.RoomSettings) (RoomView, error) {
	if id == "" {
		id = domain.RoomID("sala-" + randomHex(4))
	}
	room, err := domain.NewRoom(id, settings, h.clock.Now())
	if err != nil {
		return RoomView{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneClosedLocked()
	if _, exists := h.rooms[id]; !exists && h.openRoomsLocked() >= h.cfg.MaxRooms {
		return RoomView{}, domain.Errorf(domain.CodeConflict, "too many open rooms (limit %d); close one first", h.cfg.MaxRooms)
	}
	if existing, ok := h.rooms[id]; ok && !existing.room.Ephemeral {
		return RoomView{}, domain.Errorf(domain.CodeConflict, "room %s already exists", id)
	} else if ok {
		// A development room becomes a managed one, keeping its guests.
		existing.room.Settings = room.Settings
		existing.room.Ephemeral = false
		return h.viewLocked(existing), nil
	}
	entry := newRoomEntry(room)
	h.rooms[id] = entry
	h.log.Info("room created", "room", id)
	return h.viewLocked(entry), nil
}

// Rooms lists every room, newest first.
func (h *Hub) Rooms() []RoomView {
	h.mu.Lock()
	defer h.mu.Unlock()
	views := make([]RoomView, 0, len(h.rooms))
	for _, entry := range h.rooms {
		views = append(views, h.viewLocked(entry))
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].CreatedAt.Equal(views[j].CreatedAt) {
			return views[i].ID < views[j].ID
		}
		return views[i].CreatedAt.After(views[j].CreatedAt)
	})
	return views
}

// Room returns one room.
func (h *Hub) Room(id domain.RoomID) (RoomView, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, err := h.entryLocked(id)
	if err != nil {
		return RoomView{}, err
	}
	return h.viewLocked(entry), nil
}

// RoomPatch holds the optional fields of a room update.
type RoomPatch struct {
	Title      *string
	HostName   *string
	HostAvatar *string
	Capacity   *int
	Layout     *domain.LayoutMode
	Admission  *domain.AdmissionMode
}

// UpdateRoom edits the room settings; guests see the new title on their next
// lobby visit (signaling has no room.info push).
func (h *Hub) UpdateRoom(id domain.RoomID, patch RoomPatch) (RoomView, error) {
	h.mu.Lock()
	entry, err := h.entryLocked(id)
	if err != nil {
		h.mu.Unlock()
		return RoomView{}, err
	}
	s := entry.room.Settings
	if patch.Title != nil {
		s.Title = *patch.Title
	}
	if patch.HostName != nil {
		s.Host.Name = *patch.HostName
	}
	if patch.HostAvatar != nil {
		s.Host.Avatar = *patch.HostAvatar
	}
	if patch.Capacity != nil {
		s.Capacity = *patch.Capacity
	}
	if patch.Layout != nil {
		s.Layout = *patch.Layout
	}
	if patch.Admission != nil {
		s.Admission = *patch.Admission
	}
	if err := s.Validate(); err != nil {
		h.mu.Unlock()
		return RoomView{}, err
	}
	entry.room.Settings = s
	if s.Admission == domain.AdmissionOpen {
		// Nobody should be left waiting for an approval the room no longer asks for.
		for _, w := range entry.waiting {
			h.approveLocked(w)
		}
	}
	view := h.viewLocked(entry)
	h.mu.Unlock()
	h.onChange(id)
	return view, nil
}

// CloseRoom tells every guest the room is over and removes them. Existing
// invitations stop working.
func (h *Hub) CloseRoom(id domain.RoomID) error {
	h.mu.Lock()
	entry, err := h.entryLocked(id)
	if err != nil {
		h.mu.Unlock()
		return err
	}
	entry.room.Close()
	for wid := range entry.waiting {
		h.dropWaitingLocked(entry, wid, domain.Fatalf(domain.CodeRoomClosed, "the host closed the room"), 4410)
	}
	var refs []ParticipantRef
	for pid, m := range entry.members {
		if m.conn != nil {
			m.conn.Send(domain.NewError(domain.Fatalf(domain.CodeRoomClosed, "the host closed the room")))
			m.conn.Close(4410, "room-closed")
		}
		refs = append(refs, ParticipantRef{id, pid})
	}
	h.mu.Unlock()
	for _, ref := range refs {
		h.Leave(ref)
	}
	h.log.Info("room closed", "room", id)
	h.onChange(id)
	return nil
}

// ---------------------------------------------------------------------------
// Waiting room
// ---------------------------------------------------------------------------

// enqueueWaitingLocked parks a guest until the host admits them.
func (h *Hub) enqueueWaitingLocked(room domain.RoomID, entry *roomEntry, conn ports.Connection, msg domain.JoinMessage, inviteID string) (JoinResult, error) {
	if len(entry.waiting) >= MaxWaiting {
		return JoinResult{}, domain.Fatalf(domain.CodeRoomFull, "too many people are waiting to enter %s", room)
	}
	w := &waitingGuest{id: randomHex(4), name: msg.Name, title: msg.Title, conn: conn, since: h.clock.Now(), invite: inviteID}
	entry.waiting[w.id] = w
	w.timer = h.clock.AfterFunc(WaitingTimeout, func() { h.expireWaiting(room, w) })
	conn.Send(domain.NewWaiting())
	h.log.Info("guest waiting for approval", "room", room, "waiting", w.id, "name", w.name)
	return JoinResult{Waiting: w.id}, nil
}

// claimAdmissionLocked consumes the one-time admission a guest was given.
func (h *Hub) claimAdmissionLocked(entry *roomEntry, conn ports.Connection, token string) *waitingGuest {
	if token == "" {
		return nil
	}
	for wid, w := range entry.waiting {
		if w.token != "" && w.conn == conn && subtle.ConstantTimeCompare([]byte(w.token), []byte(token)) == 1 {
			if w.timer != nil {
				w.timer.Stop()
			}
			delete(entry.waiting, wid)
			return w
		}
	}
	return nil
}

// approveLocked tells a waiting guest they may come in (idempotent).
func (h *Hub) approveLocked(w *waitingGuest) {
	if w.token != "" {
		return
	}
	w.token = randomHex(16)
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = h.clock.AfterFunc(AdmitClaimTimeout, func() { h.expireClaim(w) })
	w.conn.Send(domain.NewAdmitted(w.token))
}

// dropWaitingLocked removes a guest from the waiting room and tells them why.
func (h *Hub) dropWaitingLocked(entry *roomEntry, wid string, reason *domain.Error, closeCode int) {
	w, ok := entry.waiting[wid]
	if !ok {
		return
	}
	delete(entry.waiting, wid)
	if w.timer != nil {
		w.timer.Stop()
	}
	w.conn.Send(domain.NewError(reason))
	w.conn.Close(closeCode, string(reason.Code))
}

// expireWaiting removes a guest the host never answered.
func (h *Hub) expireWaiting(room domain.RoomID, w *waitingGuest) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if entry := h.rooms[room]; entry != nil && entry.waiting[w.id] == w && w.token == "" {
		h.dropWaitingLocked(entry, w.id, domain.Fatalf(domain.CodeDenied, "nobody admitted you in time"), 4405)
	}
}

// expireClaim removes an admitted guest who never came back with the token.
func (h *Hub) expireClaim(w *waitingGuest) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, entry := range h.rooms {
		if entry.waiting[w.id] == w {
			delete(entry.waiting, w.id)
			return
		}
	}
}

// WaitingGone forgets a waiting guest whose socket closed or who left.
func (h *Hub) WaitingGone(room domain.RoomID, wid string, conn ports.Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry := h.rooms[room]
	if entry == nil {
		return
	}
	if w, ok := entry.waiting[wid]; ok && w.conn == conn {
		delete(entry.waiting, wid)
		if w.timer != nil {
			w.timer.Stop()
		}
	}
}

// Admit lets a waiting guest in: they are told and come back with a token.
func (h *Hub) Admit(room domain.RoomID, wid string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, err := h.entryLocked(room)
	if err != nil {
		return err
	}
	w, ok := entry.waiting[wid]
	if !ok {
		return domain.Errorf(domain.CodeNotFound, "nobody is waiting as %s in room %s", wid, room)
	}
	h.approveLocked(w)
	h.log.Info("guest admitted", "room", room, "waiting", wid, "name", w.name)
	return nil
}

// Deny turns a waiting guest away.
func (h *Hub) Deny(room domain.RoomID, wid string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, err := h.entryLocked(room)
	if err != nil {
		return err
	}
	if _, ok := entry.waiting[wid]; !ok {
		return domain.Errorf(domain.CodeNotFound, "nobody is waiting as %s in room %s", wid, room)
	}
	h.dropWaitingLocked(entry, wid, domain.Fatalf(domain.CodeDenied, "the host did not let you in"), 4405)
	h.log.Info("guest denied", "room", room, "waiting", wid)
	return nil
}

// MaxClosedRooms is how many closed rooms the bridge keeps as history (the console
// lists them) before forgetting the oldest. They hold no guests, but they do hold the
// room's destinations, so they must not pile up for the life of the process.
const MaxClosedRooms = 100

// openRoomsLocked counts the rooms that can still hold a call. Closed rooms are
// history: they must not use up the limit.
func (h *Hub) openRoomsLocked() int {
	n := 0
	for _, entry := range h.rooms {
		if !entry.room.Closed {
			n++
		}
	}
	return n
}

// pruneClosedLocked forgets the oldest closed rooms beyond MaxClosedRooms.
func (h *Hub) pruneClosedLocked() {
	var closed []*roomEntry
	for _, entry := range h.rooms {
		if entry.room.Closed {
			closed = append(closed, entry)
		}
	}
	if len(closed) <= MaxClosedRooms {
		return
	}
	sort.Slice(closed, func(i, j int) bool { return closed[i].room.CreatedAt.Before(closed[j].room.CreatedAt) })
	for _, entry := range closed[:len(closed)-MaxClosedRooms] {
		delete(h.rooms, entry.room.ID)
	}
}

// InviteView is an issued invitation link. The token and URL exist only in the
// response that creates it: the bridge keeps no copy it could show again.
type InviteView struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
	MaxUses   int       `json:"maxUses"`
}

// InviteSummary describes an invitation without its token.
type InviteSummary struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	MaxUses   int       `json:"maxUses"`
	Uses      int       `json:"uses"`
	Revoked   bool      `json:"revoked"`
	Expired   bool      `json:"expired"`
	Active    bool      `json:"active"`
}

// Invite issues a signed invitation link for the room. maxUses 0 means unlimited.
func (h *Hub) Invite(id domain.RoomID, ttl time.Duration, maxUses int) (InviteView, error) {
	if ttl <= 0 {
		ttl = h.cfg.InviteTTL
	}
	if ttl > 30*24*time.Hour {
		return InviteView{}, domain.Errorf(domain.CodeBadRequest, "invitation ttl must be at most 30 days")
	}
	if maxUses < 0 || maxUses > MaxInviteUses {
		return InviteView{}, domain.Errorf(domain.CodeBadRequest, "maxUses must be in [0, %d]", MaxInviteUses)
	}
	now := h.clock.Now()
	h.mu.Lock()
	entry, err := h.entryLocked(id)
	if err != nil {
		h.mu.Unlock()
		return InviteView{}, err
	}
	if entry.room.Closed {
		h.mu.Unlock()
		return InviteView{}, domain.Errorf(domain.CodeRoomClosed, "room %s is closed", id)
	}
	// Forget invitations that can no longer admit anyone before applying the cap.
	for key, rec := range entry.invites {
		if rec.usable(now) != nil {
			delete(entry.invites, key)
		}
	}
	if len(entry.invites) >= MaxInvitesPerRoom {
		h.mu.Unlock()
		return InviteView{}, domain.Errorf(domain.CodeConflict, "a room can have at most %d active invitations; revoke some first", MaxInvitesPerRoom)
	}
	inviteID := randomHex(8)
	inv := h.signer.Issue(id, inviteID, now.Add(ttl))
	entry.invites[inviteID] = &inviteRecord{id: inviteID, createdAt: now, expiresAt: inv.ExpiresAt, maxUses: maxUses}
	h.mu.Unlock()
	link := h.cfg.WebRelayURL + "/join/" + url.PathEscape(string(id)) + "?t=" + url.QueryEscape(inv.Token)
	h.log.Info("invitation issued", "room", id, "invite", inviteID, "expires", inv.ExpiresAt, "maxUses", maxUses)
	return InviteView{ID: inviteID, Token: inv.Token, URL: link, ExpiresAt: inv.ExpiresAt, MaxUses: maxUses}, nil
}

// Invites lists the room's invitations, newest first (never their tokens).
func (h *Hub) Invites(id domain.RoomID) ([]InviteSummary, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, err := h.entryLocked(id)
	if err != nil {
		return nil, err
	}
	now := h.clock.Now()
	list := make([]InviteSummary, 0, len(entry.invites))
	for _, rec := range entry.invites {
		list = append(list, InviteSummary{
			ID: rec.id, CreatedAt: rec.createdAt, ExpiresAt: rec.expiresAt, MaxUses: rec.maxUses, Uses: rec.uses,
			Revoked: rec.revoked, Expired: !now.Before(rec.expiresAt), Active: rec.usable(now) == nil,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].ID < list[j].ID
		}
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	return list, nil
}

// RevokeInvite stops one invitation from admitting anyone else. Guests already
// in the room stay; the others are the host's to remove (kick) or the room's to
// close.
func (h *Hub) RevokeInvite(id domain.RoomID, inviteID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, err := h.entryLocked(id)
	if err != nil {
		return err
	}
	rec, ok := entry.invites[inviteID]
	if !ok {
		return domain.Errorf(domain.CodeNotFound, "invitation %s not found in room %s", inviteID, id)
	}
	rec.revoked = true
	h.log.Info("invitation revoked", "room", id, "invite", inviteID)
	return nil
}

// AddDestination stores an RTMP(S) output; the key is returned masked.
func (h *Hub) AddDestination(id domain.RoomID, d domain.Destination) (DestinationView, error) {
	if !h.cfg.AllowPrivateDestinations {
		if err := d.Validate(); err != nil {
			return DestinationView{}, err
		}
		if err := domain.CheckPublicHost(d.Hostname()); err != nil {
			return DestinationView{}, err
		}
	}
	h.mu.Lock()
	entry, err := h.entryLocked(id)
	if err != nil {
		h.mu.Unlock()
		return DestinationView{}, err
	}
	if len(entry.room.Destinations) >= h.cfg.MaxDestinations {
		h.mu.Unlock()
		return DestinationView{}, domain.Errorf(domain.CodeConflict, "a room can have at most %d destinations", h.cfg.MaxDestinations)
	}
	added, err := entry.room.AddDestination(d)
	h.mu.Unlock()
	if err != nil {
		return DestinationView{}, err
	}
	h.onChange(id)
	return destinationView(added), nil
}

// RemoveDestination deletes an output.
func (h *Hub) RemoveDestination(id domain.RoomID, destination string) error {
	return h.mutate(id, func(r *domain.Room) error { return r.RemoveDestination(destination) })
}

// SetDestinationEnabled toggles an output.
func (h *Hub) SetDestinationEnabled(id domain.RoomID, destination string, enabled bool) error {
	return h.mutate(id, func(r *domain.Room) error { return r.SetDestinationEnabled(destination, enabled) })
}

// SetOnStage puts a participant on or off the broadcast picture.
func (h *Hub) SetOnStage(ref ParticipantRef, onStage bool) error {
	return h.mutate(ref.Room, func(r *domain.Room) error { return r.SetOnStage(ref.Participant, onStage) })
}

// Kick removes a participant; its client shows "te ha retirado" and does
// not reconnect.
func (h *Hub) Kick(ref ParticipantRef) error {
	h.mu.Lock()
	m, ok := h.memberLocked(ref)
	if !ok {
		h.mu.Unlock()
		return domain.Errorf(domain.CodeNotFound, "participant %s not in room %s", ref.Participant, ref.Room)
	}
	if m.conn != nil {
		m.conn.Send(domain.NewError(domain.Fatalf(domain.CodeKicked, "the host removed you from the room")))
		m.conn.Close(4403, "kicked")
	}
	h.mu.Unlock()
	h.Leave(ref)
	return nil
}

// Snapshot computes the broadcast plan and outputs of a room.
func (h *Hub) Snapshot(id domain.RoomID, output domain.OutputSettings, live domain.LiveFunc) (domain.CompositionPlan, []ports.Output, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, err := h.entryLocked(id)
	if err != nil {
		return domain.CompositionPlan{}, nil, err
	}
	if entry.room.Closed {
		return domain.CompositionPlan{}, nil, domain.Errorf(domain.CodeRoomClosed, "room %s is closed", id)
	}
	var outputs []ports.Output
	for _, d := range entry.room.EnabledDestinations() {
		outputs = append(outputs, ports.Output{ID: d.ID, URL: d.PublishURL()})
	}
	return domain.PlanComposition(entry.room, output, live), outputs, nil
}

// ---------------------------------------------------------------------------
// helpers (callers hold h.mu where the name says Locked)
// ---------------------------------------------------------------------------

func (h *Hub) mutate(id domain.RoomID, fn func(*domain.Room) error) error {
	h.mu.Lock()
	entry, err := h.entryLocked(id)
	if err == nil {
		err = fn(entry.room)
	}
	h.mu.Unlock()
	if err != nil {
		return err
	}
	h.onChange(id)
	return nil
}

func (h *Hub) entryLocked(id domain.RoomID) (*roomEntry, error) {
	entry := h.rooms[id]
	if entry == nil {
		return nil, domain.Errorf(domain.CodeNotFound, "room %s not found", id)
	}
	return entry, nil
}

func (h *Hub) memberLocked(ref ParticipantRef) (*member, bool) {
	entry := h.rooms[ref.Room]
	if entry == nil {
		return nil, false
	}
	m, ok := entry.members[ref.Participant]
	return m, ok
}

func (h *Hub) removeLocked(ref ParticipantRef) *member {
	entry := h.rooms[ref.Room]
	if entry == nil {
		return nil
	}
	m, ok := entry.members[ref.Participant]
	if !ok {
		return nil
	}
	if m.grace != nil {
		m.grace.Stop()
	}
	delete(entry.members, ref.Participant)
	entry.room.Leave(ref.Participant)
	h.broadcastLocked(entry, ref.Participant, domain.NewParticipantLeft(ref.Participant))
	return m
}

func (h *Hub) broadcastLocked(entry *roomEntry, except domain.ParticipantID, msg domain.ServerMessage) {
	for pid, m := range entry.members {
		if pid != except && m.conn != nil {
			m.conn.Send(msg)
		}
	}
}

func (h *Hub) sendTo(ref ParticipantRef, msg domain.ServerMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m, ok := h.memberLocked(ref); ok && m.conn != nil {
		m.conn.Send(msg)
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(errors.Join(errors.New("crypto/rand failed"), err))
	}
	return hex.EncodeToString(b)
}
