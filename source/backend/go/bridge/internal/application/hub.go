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
}

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
func (h *Hub) Peek(id domain.RoomID, token string) (domain.RoomInfoMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry := h.rooms[id]
	if h.cfg.DevMode {
		if token == "invalid" {
			return domain.RoomInfoMessage{}, domain.Fatalf(domain.CodeInvalidToken, "invitation token is invalid or expired")
		}
		if id == "closed" {
			return domain.RoomInfoMessage{}, domain.Fatalf(domain.CodeRoomNotFound, "room %s does not exist", id)
		}
		if entry == nil {
			room, err := domain.NewRoom(id, domain.RoomSettings{
				Title: "Sala de pruebas · " + string(id),
				Host:  domain.HostInfo{Name: "Anfitrión de pruebas"},
			}, h.clock.Now())
			if err != nil {
				return domain.RoomInfoMessage{}, err
			}
			room.Ephemeral = true
			entry = &roomEntry{room: room, members: map[domain.ParticipantID]*member{}}
			h.rooms[id] = entry
		}
	} else {
		if entry == nil {
			return domain.RoomInfoMessage{}, domain.Fatalf(domain.CodeRoomNotFound, "room %s does not exist", id)
		}
		if err := h.signer.Verify(id, token, h.clock.Now()); err != nil {
			return domain.RoomInfoMessage{}, err
		}
	}
	if entry.room.Closed {
		return domain.RoomInfoMessage{}, domain.Fatalf(domain.CodeRoomClosed, "room %s is closed", id)
	}
	info := entry.room.Info()
	if entry.room.Ephemeral && len(info.Channels) == 0 {
		info.Channels = slices.Clone(DevChannels)
	}
	return info, nil
}

// JoinResult is what a session keeps after joining.
type JoinResult struct {
	Self ParticipantRef
	Peer ports.MediaPeer
}

// ParticipantRef locates a participant.
type ParticipantRef struct {
	Room        domain.RoomID
	Participant domain.ParticipantID
}

// Join admits a guest (or resumes a dropped one), sends `welcome` and
// attaches its media peer. The welcome always precedes any SFU message.
func (h *Hub) Join(id domain.RoomID, conn ports.Connection, msg domain.JoinMessage) (JoinResult, error) {
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
	p, err := entry.room.Join(msg.Name, msg.Title, msg.Video, h.clock.Now())
	if err != nil {
		h.mu.Unlock()
		return JoinResult{}, err
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
	if existing, ok := h.rooms[id]; ok && !existing.room.Ephemeral {
		return RoomView{}, domain.Errorf(domain.CodeConflict, "room %s already exists", id)
	} else if ok {
		// A development room becomes a managed one, keeping its guests.
		existing.room.Settings = room.Settings
		existing.room.Ephemeral = false
		return h.viewLocked(existing), nil
	}
	entry := &roomEntry{room: room, members: map[domain.ParticipantID]*member{}}
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
	if err := s.Validate(); err != nil {
		h.mu.Unlock()
		return RoomView{}, err
	}
	entry.room.Settings = s
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

// InviteView is an issued invitation link.
type InviteView struct {
	Token     string    `json:"token"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Invite issues a signed invitation link for the room.
func (h *Hub) Invite(id domain.RoomID, ttl time.Duration) (InviteView, error) {
	if ttl <= 0 {
		ttl = h.cfg.InviteTTL
	}
	if ttl > 30*24*time.Hour {
		return InviteView{}, domain.Errorf(domain.CodeBadRequest, "invitation ttl must be at most 30 days")
	}
	h.mu.Lock()
	entry, err := h.entryLocked(id)
	closed := err == nil && entry.room.Closed
	h.mu.Unlock()
	if err != nil {
		return InviteView{}, err
	}
	if closed {
		return InviteView{}, domain.Errorf(domain.CodeRoomClosed, "room %s is closed", id)
	}
	inv := h.signer.Issue(id, h.clock.Now().Add(ttl))
	link := h.cfg.WebRelayURL + "/join/" + url.PathEscape(string(id)) + "?t=" + url.QueryEscape(inv.Token)
	return InviteView{Token: inv.Token, URL: link, ExpiresAt: inv.ExpiresAt}, nil
}

// AddDestination stores an RTMP(S) output; the key is returned masked.
func (h *Hub) AddDestination(id domain.RoomID, d domain.Destination) (DestinationView, error) {
	h.mu.Lock()
	entry, err := h.entryLocked(id)
	if err != nil {
		h.mu.Unlock()
		return DestinationView{}, err
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
