package application

import (
	"errors"
	"log/slog"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

// Close codes sent with fatal errors (application range 4000-4999).
var closeCodes = map[domain.ErrorCode]int{
	domain.CodeInvalidToken: 4401,
	domain.CodeKicked:       4403,
	domain.CodeDenied:       4405,
	domain.CodeRoomNotFound: 4404,
	domain.CodeRoomClosed:   4410,
	domain.CodeRoomFull:     4429,
}

// Session is the signaling v2 state machine of one webrelay socket:
// room.peek → join → media negotiation → leave. Handle is called
// sequentially, in arrival order, by the transport adapter.
type Session struct {
	hub     *Hub
	conn    ports.Connection
	log     *slog.Logger
	room    domain.RoomID
	peeked  bool
	invite  string // id of the invitation shown at room.peek ("" in development mode)
	waiting string // waiting-room entry, while the host has not admitted this guest
	self    *ParticipantRef
	peer    ports.MediaPeer
}

// NewSession starts a session for a freshly accepted socket.
func NewSession(hub *Hub, conn ports.Connection, log *slog.Logger) *Session {
	return &Session{hub: hub, conn: conn, log: log}
}

// Admitted reports whether this socket has shown a valid invitation
// (room.peek). Like Handle it must only be called from the socket's reader.
func (s *Session) Admitted() bool { return s.peeked }

// Handle processes one text frame.
func (s *Session) Handle(data []byte) {
	msg, err := domain.ParseClientMessage(data)
	if err != nil {
		s.log.Debug("invalid signaling message", "err", err)
		s.fail(domain.Errorf(domain.CodeBadRequest, "%s", err.Error()))
		return
	}
	switch m := msg.(type) {
	case domain.PingMessage:
		s.conn.Send(domain.NewPong(m.T))
	case domain.PeekMessage:
		s.peek(m)
	case domain.JoinMessage:
		s.join(m)
	case domain.PublishOfferMessage:
		if s.peer == nil {
			s.fail(domain.Errorf(domain.CodeNotJoined, "publish.offer before join"))
			return
		}
		answer, err := s.peer.Publish(m.SDP, m.Sources)
		if err != nil {
			s.fail(domain.Errorf(domain.CodeMedia, "publish negotiation failed: %v", err))
			return
		}
		s.conn.Send(domain.NewPublishAnswer(answer))
	case domain.SubscribeAnswerMessage:
		if s.peer == nil {
			return
		}
		if err := s.peer.AnswerSubscribe(m.SDP); err != nil {
			s.fail(domain.Errorf(domain.CodeMedia, "subscribe negotiation failed: %v", err))
		}
	case domain.ClientIceMessage:
		if s.peer == nil || m.Candidate.Candidate == "" {
			return
		}
		if err := s.peer.AddCandidate(m.PC, m.Candidate); err != nil {
			s.log.Debug("ice candidate rejected", "err", err)
		}
	case domain.SelfUpdateMessage:
		if s.self == nil {
			return
		}
		if err := s.hub.UpdateSelf(*s.self, m.State); err != nil {
			s.fail(asDomainError(err))
		}
	case domain.LeaveMessage:
		if s.waiting != "" {
			s.hub.WaitingGone(s.room, s.waiting, s.conn)
			s.waiting = ""
		}
		if s.self != nil {
			s.hub.Leave(*s.self)
		}
		s.self, s.peer = nil, nil
	case domain.UnknownMessage:
		s.log.Debug("ignoring unknown signaling message", "type", m.Type)
	}
}

// Closed is called once when the socket ends; a joined participant keeps its
// place for the resume grace period.
func (s *Session) Closed() {
	if s.waiting != "" {
		s.hub.WaitingGone(s.room, s.waiting, s.conn)
	}
	if s.self != nil {
		s.hub.Disconnected(*s.self, s.conn)
	}
}

func (s *Session) peek(m domain.PeekMessage) {
	if s.self != nil {
		return // already in the call; a lobby re-peek is meaningless
	}
	info, invite, err := s.hub.Peek(domain.RoomID(m.Room), m.Token)
	if err != nil {
		s.fail(asDomainError(err))
		return
	}
	s.room, s.peeked, s.invite = domain.RoomID(m.Room), true, invite
	s.conn.Send(info)
}

func (s *Session) join(m domain.JoinMessage) {
	if !s.peeked {
		s.fail(domain.Fatalf(domain.CodeRoomNotFound, "send room.peek before join"))
		return
	}
	if s.self != nil {
		return
	}
	if s.waiting != "" && m.Admit == "" {
		return // already in the waiting room; the host's answer will come
	}
	result, err := s.hub.Join(s.room, s.conn, m, s.invite)
	if err != nil {
		s.fail(asDomainError(err))
		return
	}
	if result.Waiting != "" {
		s.waiting = result.Waiting
		return
	}
	s.waiting = ""
	s.self, s.peer = &result.Self, result.Peer
}

// fail sends an error; fatal ones also close the socket.
func (s *Session) fail(err *domain.Error) {
	s.conn.Send(domain.NewError(err))
	if err.Fatal {
		code, ok := closeCodes[err.Code]
		if !ok {
			code = 4400
		}
		s.conn.Close(code, string(err.Code))
	}
}

func asDomainError(err error) *domain.Error {
	var de *domain.Error
	if errors.As(err, &de) {
		return de
	}
	return domain.Errorf(domain.CodeMedia, "%s", err.Error())
}
