package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// RoomID is the slug in the invitation link (/join/<room>).
type RoomID string

// ParticipantID is assigned by the bridge on join (p-1, p-2…).
type ParticipantID string

var roomIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidateRoomID accepts lowercase slugs of 1..63 characters.
func ValidateRoomID(id RoomID) error {
	if !roomIDPattern.MatchString(string(id)) {
		return fmt.Errorf("room id %q must match %s", id, roomIDPattern)
	}
	return nil
}

// LayoutMode is how the broadcast arranges the participants on stage.
type LayoutMode string

const (
	// LayoutAuto shows a grid, or the screen share in focus with the cameras
	// in a side column while someone shares.
	LayoutAuto LayoutMode = "auto"
	// LayoutGrid always uses a grid (screens are just another tile).
	LayoutGrid LayoutMode = "grid"
	// LayoutSpeaker gives the first participant on stage the focus.
	LayoutSpeaker LayoutMode = "speaker"
)

// Valid reports whether m is a known layout.
func (m LayoutMode) Valid() bool {
	return m == LayoutAuto || m == LayoutGrid || m == LayoutSpeaker
}

const (
	DefaultCapacity = 8
	MaxCapacity     = 16
)

// AdmissionMode says how a guest with a valid invitation gets in.
type AdmissionMode string

const (
	// AdmissionOpen seats guests as soon as they join (the default).
	AdmissionOpen AdmissionMode = "open"
	// AdmissionApproval parks guests in a waiting room until the host admits them.
	AdmissionApproval AdmissionMode = "approval"
)

// Valid reports whether m is a known mode.
func (m AdmissionMode) Valid() bool { return m == AdmissionOpen || m == AdmissionApproval }

// RoomSettings is what the host edits.
type RoomSettings struct {
	Title     string
	Host      HostInfo
	Capacity  int
	Layout    LayoutMode
	Admission AdmissionMode
}

// Validate normalises and checks the settings.
func (s *RoomSettings) Validate() error {
	s.Title = strings.TrimSpace(s.Title)
	s.Host.Name = strings.TrimSpace(s.Host.Name)
	switch {
	case s.Title == "" || utf8.RuneCountInString(s.Title) > 120:
		return Errorf(CodeBadRequest, "title must have 1..120 characters")
	case s.Host.Name == "" || utf8.RuneCountInString(s.Host.Name) > MaxNameLength:
		return Errorf(CodeBadRequest, "host name must have 1..%d characters", MaxNameLength)
	case checkLabel("title", s.Title) != nil:
		return checkLabel("title", s.Title)
	case checkLabel("host name", s.Host.Name) != nil:
		return checkLabel("host name", s.Host.Name)
	case s.Host.Avatar != "" && !strings.HasPrefix(s.Host.Avatar, "https://") && !strings.HasPrefix(s.Host.Avatar, "http://"):
		return Errorf(CodeBadRequest, "host avatar must be an http(s) URL")
	}
	if s.Capacity == 0 {
		s.Capacity = DefaultCapacity
	}
	if s.Capacity < 1 || s.Capacity > MaxCapacity {
		return Errorf(CodeBadRequest, "capacity must be in [1, %d]", MaxCapacity)
	}
	if s.Admission == "" {
		s.Admission = AdmissionOpen
	}
	if !s.Admission.Valid() {
		return Errorf(CodeBadRequest, "admission %q is not open|approval", s.Admission)
	}
	if s.Layout == "" {
		s.Layout = LayoutAuto
	}
	if !s.Layout.Valid() {
		return Errorf(CodeBadRequest, "layout %q is not auto|grid|speaker", s.Layout)
	}
	return nil
}

// Participant is a guest in the room.
type Participant struct {
	ID        ParticipantID
	Name      string
	Title     string
	State     SelfState
	OnStage   bool
	JoinedAt  time.Time
	Published map[MediaSource]bool
}

// Info is the participant as other guests see it.
func (p *Participant) Info() ParticipantInfo {
	return ParticipantInfo{
		ID: p.ID, Name: p.Name, Title: p.Title,
		Muted: p.State.Muted, CameraOff: p.State.CameraOff, ScreenSharing: p.State.ScreenSharing,
	}
}

// Room is the aggregate: settings, destinations and participants in join order.
type Room struct {
	ID           RoomID
	Settings     RoomSettings
	CreatedAt    time.Time
	Closed       bool
	Ephemeral    bool // created on demand in development mode
	Destinations []Destination
	participants []*Participant
	nextID       int
	nextDest     int
}

// NewRoom validates settings and builds an open room.
func NewRoom(id RoomID, settings RoomSettings, now time.Time) (*Room, error) {
	if err := ValidateRoomID(id); err != nil {
		return nil, Errorf(CodeBadRequest, "%s", err.Error())
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return &Room{ID: id, Settings: settings, CreatedAt: now}, nil
}

// Info is what the lobby shows.
func (r *Room) Info() RoomInfoMessage {
	return NewRoomInfo(r.ID, r.Settings.Title, r.Settings.Host, r.Channels())
}

// Channels lists the kinds of the enabled destinations, deduplicated, for
// the lobby's informative "streamed to" chips.
func (r *Room) Channels() []string {
	channels := []string{}
	for _, d := range r.Destinations {
		if d.Enabled && !slices.Contains(channels, string(d.Kind)) {
			channels = append(channels, string(d.Kind))
		}
	}
	return channels
}

// Participants returns the participants in join order.
func (r *Room) Participants() []*Participant { return slices.Clone(r.participants) }

// ParticipantInfos is the public list (welcome.participants).
func (r *Room) ParticipantInfos() []ParticipantInfo {
	infos := make([]ParticipantInfo, 0, len(r.participants))
	for _, p := range r.participants {
		infos = append(infos, p.Info())
	}
	return infos
}

// Participant finds a participant by id.
func (r *Room) Participant(id ParticipantID) (*Participant, bool) {
	for _, p := range r.participants {
		if p.ID == id {
			return p, true
		}
	}
	return nil, false
}

// Join admits a guest, enforcing that the room is open and not full.
func (r *Room) Join(name, title string, cameraOn bool, now time.Time) (*Participant, error) {
	if r.Closed {
		return nil, Fatalf(CodeRoomClosed, "room %s is closed", r.ID)
	}
	if len(r.participants) >= r.Settings.Capacity {
		return nil, Fatalf(CodeRoomFull, "room %s is full (%d)", r.ID, r.Settings.Capacity)
	}
	r.nextID++
	p := &Participant{
		ID:        ParticipantID(fmt.Sprintf("p-%d", r.nextID)),
		Name:      name,
		Title:     title,
		State:     SelfState{CameraOff: !cameraOn},
		OnStage:   true,
		JoinedAt:  now,
		Published: map[MediaSource]bool{},
	}
	r.participants = append(r.participants, p)
	return p, nil
}

// Leave removes a participant; it reports whether it was present.
func (r *Room) Leave(id ParticipantID) bool {
	for i, p := range r.participants {
		if p.ID == id {
			r.participants = slices.Delete(r.participants, i, i+1)
			return true
		}
	}
	return false
}

// Update stores a participant's reported state.
func (r *Room) Update(id ParticipantID, state SelfState) (*Participant, error) {
	p, ok := r.Participant(id)
	if !ok {
		return nil, Errorf(CodeNotJoined, "participant %s not in room %s", id, r.ID)
	}
	p.State = state
	return p, nil
}

// SetOnStage puts a participant on or off the broadcast.
func (r *Room) SetOnStage(id ParticipantID, onStage bool) error {
	p, ok := r.Participant(id)
	if !ok {
		return Errorf(CodeNotFound, "participant %s not in room %s", id, r.ID)
	}
	p.OnStage = onStage
	return nil
}

// SetPublished records which sources a participant is publishing to the SFU.
func (r *Room) SetPublished(id ParticipantID, source MediaSource, published bool) bool {
	p, ok := r.Participant(id)
	if !ok {
		return false
	}
	if published {
		p.Published[source] = true
	} else {
		delete(p.Published, source)
	}
	return true
}

// Close marks the room closed; participants must be told and removed by the
// caller.
func (r *Room) Close() { r.Closed = true }

// AddDestination validates and appends an RTMP(S) destination.
func (r *Room) AddDestination(d Destination) (Destination, error) {
	if err := d.Validate(); err != nil {
		return Destination{}, err
	}
	r.nextDest++
	d.ID = fmt.Sprintf("d-%d", r.nextDest)
	r.Destinations = append(r.Destinations, d)
	return d, nil
}

// RemoveDestination deletes a destination by id.
func (r *Room) RemoveDestination(id string) error {
	for i, d := range r.Destinations {
		if d.ID == id {
			r.Destinations = slices.Delete(r.Destinations, i, i+1)
			return nil
		}
	}
	return Errorf(CodeNotFound, "destination %s not found", id)
}

// SetDestinationEnabled toggles a destination.
func (r *Room) SetDestinationEnabled(id string, enabled bool) error {
	for i := range r.Destinations {
		if r.Destinations[i].ID == id {
			r.Destinations[i].Enabled = enabled
			return nil
		}
	}
	return Errorf(CodeNotFound, "destination %s not found", id)
}

// EnabledDestinations are the outputs of the broadcast.
func (r *Room) EnabledDestinations() []Destination {
	var out []Destination
	for _, d := range r.Destinations {
		if d.Enabled {
			out = append(out, d)
		}
	}
	return out
}
