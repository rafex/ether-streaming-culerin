package application

import (
	"slices"
	"strings"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

// RoomView is the host API's representation of a room. Stream keys are
// always masked.
type RoomView struct {
	ID           domain.RoomID          `json:"id"`
	Title        string                 `json:"title"`
	Host         domain.HostInfo        `json:"host"`
	Capacity     int                    `json:"capacity"`
	Layout       domain.LayoutMode      `json:"layout"`
	Admission    domain.AdmissionMode   `json:"admission"`
	Waiting      []WaitingView          `json:"waiting"`
	Closed       bool                   `json:"closed"`
	Ephemeral    bool                   `json:"ephemeral"`
	CreatedAt    time.Time              `json:"createdAt"`
	Channels     []string               `json:"channels"`
	Participants []ParticipantView      `json:"participants"`
	Destinations []DestinationView      `json:"destinations"`
	Broadcast    *ports.BroadcastStatus `json:"broadcast,omitempty"`
}

// WaitingView is a guest in the waiting room, as the host sees them.
type WaitingView struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Title    string    `json:"title,omitempty"`
	Since    time.Time `json:"since"`
	Admitted bool      `json:"admitted"` // told to come in, not back yet
}

// ParticipantView adds host-only fields to the public participant info.
type ParticipantView struct {
	domain.ParticipantInfo
	OnStage   bool                 `json:"onStage"`
	Connected bool                 `json:"connected"`
	Published []domain.MediaSource `json:"published"`
	JoinedAt  time.Time            `json:"joinedAt"`
}

// DestinationView is a destination with its stream key masked.
type DestinationView struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Kind      domain.DestinationKind `json:"kind"`
	URL       string                 `json:"url"`
	StreamKey string                 `json:"streamKey"`
	Enabled   bool                   `json:"enabled"`
}

func destinationView(d domain.Destination) DestinationView {
	return DestinationView{ID: d.ID, Name: d.Name, Kind: d.Kind, URL: d.URL, StreamKey: d.MaskedKey(), Enabled: d.Enabled}
}

func (h *Hub) viewLocked(entry *roomEntry) RoomView {
	r := entry.room
	view := RoomView{
		ID: r.ID, Title: r.Settings.Title, Host: r.Settings.Host, Capacity: r.Settings.Capacity,
		Layout: r.Settings.Layout, Admission: r.Settings.Admission, Waiting: []WaitingView{}, Closed: r.Closed, Ephemeral: r.Ephemeral, CreatedAt: r.CreatedAt,
		Channels: r.Channels(), Participants: []ParticipantView{}, Destinations: []DestinationView{},
	}
	for _, p := range r.Participants() {
		published := make([]domain.MediaSource, 0, len(p.Published))
		for s := range p.Published {
			published = append(published, s)
		}
		slices.Sort(published)
		m := entry.members[p.ID]
		view.Participants = append(view.Participants, ParticipantView{
			ParticipantInfo: p.Info(), OnStage: p.OnStage, Connected: m != nil && m.conn != nil,
			Published: published, JoinedAt: p.JoinedAt,
		})
	}
	for _, w := range entry.waiting {
		view.Waiting = append(view.Waiting, WaitingView{ID: w.id, Name: w.name, Title: w.title, Since: w.since, Admitted: w.token != ""})
	}
	slices.SortFunc(view.Waiting, func(a, b WaitingView) int {
		if c := a.Since.Compare(b.Since); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	for _, d := range r.Destinations {
		view.Destinations = append(view.Destinations, destinationView(d))
	}
	return view
}
