package httpapi

import (
	"net/http"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/application"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

func roomID(r *http.Request) domain.RoomID { return domain.RoomID(r.PathValue("room")) }

func (s *Server) withBroadcast(view application.RoomView) application.RoomView {
	status := s.broadcaster.Status(view.ID)
	view.Broadcast = &status
	return view
}

func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"devMode": s.opts.DevMode, "webrelayUrl": s.opts.WebRelayURL, "output": s.opts.Output,
	})
}

func (s *Server) listRooms(w http.ResponseWriter, _ *http.Request) {
	rooms := s.hub.Rooms()
	for i := range rooms {
		rooms[i] = s.withBroadcast(rooms[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

type hostBody struct {
	Name   *string `json:"name"`
	Avatar *string `json:"avatar"`
}

type roomBody struct {
	ID       string             `json:"id"`
	Title    *string            `json:"title"`
	Host     *hostBody          `json:"host"`
	Capacity *int               `json:"capacity"`
	Layout   *domain.LayoutMode `json:"layout"`
	// Admission is "open" (guests are seated at once) or "approval" (they wait for the host).
	Admission *domain.AdmissionMode `json:"admission"`
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	var body roomBody
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	settings := domain.RoomSettings{}
	if body.Title != nil {
		settings.Title = *body.Title
	}
	if body.Host != nil && body.Host.Name != nil {
		settings.Host.Name = *body.Host.Name
	}
	if body.Host != nil && body.Host.Avatar != nil {
		settings.Host.Avatar = *body.Host.Avatar
	}
	if body.Capacity != nil {
		settings.Capacity = *body.Capacity
	}
	if body.Layout != nil {
		settings.Layout = *body.Layout
	}
	if body.Admission != nil {
		settings.Admission = *body.Admission
	}
	view, err := s.hub.CreateRoom(domain.RoomID(body.ID), settings)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.withBroadcast(view))
}

func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) {
	view, err := s.hub.Room(roomID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.withBroadcast(view))
}

func (s *Server) updateRoom(w http.ResponseWriter, r *http.Request) {
	var body roomBody
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	patch := application.RoomPatch{Title: body.Title, Capacity: body.Capacity, Layout: body.Layout, Admission: body.Admission}
	if body.Host != nil {
		patch.HostName, patch.HostAvatar = body.Host.Name, body.Host.Avatar
	}
	view, err := s.hub.UpdateRoom(roomID(r), patch)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.withBroadcast(view))
}

func (s *Server) closeRoom(w http.ResponseWriter, r *http.Request) {
	s.broadcaster.Stop(roomID(r))
	if err := s.hub.CloseRoom(roomID(r)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) invite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TTLSeconds int `json:"ttlSeconds"`
		MaxUses    int `json:"maxUses"`
	}
	if r.ContentLength != 0 {
		if err := readJSON(r, &body); err != nil {
			writeError(w, err)
			return
		}
	}
	inv, err := s.hub.Invite(roomID(r), time.Duration(body.TTLSeconds)*time.Second, body.MaxUses)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
	list, err := s.hub.Invites(roomID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) revokeInvite(w http.ResponseWriter, r *http.Request) {
	if err := s.hub.RevokeInvite(roomID(r), r.PathValue("invite")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) admitGuest(w http.ResponseWriter, r *http.Request) {
	if err := s.hub.Admit(roomID(r), r.PathValue("guest")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) denyGuest(w http.ResponseWriter, r *http.Request) {
	if err := s.hub.Deny(roomID(r), r.PathValue("guest")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addDestination(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string                 `json:"name"`
		Kind      domain.DestinationKind `json:"kind"`
		URL       string                 `json:"url"`
		StreamKey string                 `json:"streamKey"`
		Enabled   *bool                  `json:"enabled"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	enabled := body.Enabled == nil || *body.Enabled
	view, err := s.hub.AddDestination(roomID(r), domain.Destination{
		Name: body.Name, Kind: body.Kind, URL: body.URL, StreamKey: body.StreamKey, Enabled: enabled,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) updateDestination(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Enabled == nil {
		writeError(w, domain.Errorf(domain.CodeBadRequest, "enabled is required"))
		return
	}
	if err := s.hub.SetDestinationEnabled(roomID(r), r.PathValue("dest"), *body.Enabled); err != nil {
		writeError(w, err)
		return
	}
	s.getRoom(w, r)
}

func (s *Server) removeDestination(w http.ResponseWriter, r *http.Request) {
	if err := s.hub.RemoveDestination(roomID(r), r.PathValue("dest")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) startBroadcast(w http.ResponseWriter, r *http.Request) {
	if err := s.broadcaster.Start(roomID(r)); err != nil {
		writeError(w, err)
		return
	}
	s.getRoom(w, r)
}

func (s *Server) stopBroadcast(w http.ResponseWriter, r *http.Request) {
	if _, err := s.hub.Room(roomID(r)); err != nil {
		writeError(w, err)
		return
	}
	s.broadcaster.Stop(roomID(r))
	s.getRoom(w, r)
}

func (s *Server) layout(w http.ResponseWriter, r *http.Request) {
	plan, err := s.broadcaster.Plan(roomID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) updateParticipant(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OnStage *bool `json:"onStage"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.OnStage == nil {
		writeError(w, domain.Errorf(domain.CodeBadRequest, "onStage is required"))
		return
	}
	ref := application.ParticipantRef{Room: roomID(r), Participant: domain.ParticipantID(r.PathValue("participant"))}
	if err := s.hub.SetOnStage(ref, *body.OnStage); err != nil {
		writeError(w, err)
		return
	}
	s.getRoom(w, r)
}

func (s *Server) kick(w http.ResponseWriter, r *http.Request) {
	ref := application.ParticipantRef{Room: roomID(r), Participant: domain.ParticipantID(r.PathValue("participant"))}
	if err := s.hub.Kick(ref); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
