package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// SignalingSubprotocol is the WebSocket subprotocol of the webrelay contract
// (ether-webrelay docs/signaling.md). Upgrades without it are rejected.
const SignalingSubprotocol = "ether-webrelay.signaling.v2"

const (
	MaxNameLength  = 60
	MaxTitleLength = 80
)

// MediaSource identifies what a published track carries.
type MediaSource string

const (
	SourceCamera      MediaSource = "camera"
	SourceMicrophone  MediaSource = "microphone"
	SourceScreen      MediaSource = "screen"
	SourceScreenAudio MediaSource = "screen-audio"
)

// Valid reports whether s is one of the contract's sources.
func (s MediaSource) Valid() bool {
	switch s {
	case SourceCamera, SourceMicrophone, SourceScreen, SourceScreenAudio:
		return true
	}
	return false
}

// IsVideo reports whether the source carries video.
func (s MediaSource) IsVideo() bool { return s == SourceCamera || s == SourceScreen }

// PeerRole names one of the two peer connections of a participant.
type PeerRole string

const (
	RolePublish   PeerRole = "publish"
	RoleSubscribe PeerRole = "subscribe"
)

// TransportMode is the media transport the room asks the webrelay to use.
type TransportMode string

const (
	TransportWebRTC    TransportMode = "webrtc"
	TransportWebSocket TransportMode = "websocket"
)

// IceCandidate mirrors RTCIceCandidateInit.
type IceCandidate struct {
	Candidate     string  `json:"candidate"`
	SDPMid        *string `json:"sdpMid,omitempty"`
	SDPMLineIndex *uint16 `json:"sdpMLineIndex,omitempty"`
}

// IceServer mirrors RTCIceServer.
type IceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// SelfState is the visible state a participant reports with `self.update`.
type SelfState struct {
	Muted         bool `json:"muted"`
	CameraOff     bool `json:"cameraOff"`
	ScreenSharing bool `json:"screenSharing"`
}

// ---------------------------------------------------------------------------
// Client → bridge
// ---------------------------------------------------------------------------

// ClientMessage is any message the webrelay sends. Unknown types parse to
// UnknownMessage so that newer clients keep working (forward compatibility).
type ClientMessage interface{ clientMessage() }

type (
	PeekMessage struct {
		Room  string
		Token string
	}
	JoinMessage struct {
		Name   string
		Title  string
		Audio  bool
		Video  bool
		Resume ParticipantID
		// ResumeToken is the secret the welcome handed out with the participant
		// id; without it a resume is ignored (ids are sequential and public).
		ResumeToken string
	}
	PublishOfferMessage struct {
		SDP     string
		Sources map[string]MediaSource
	}
	SubscribeAnswerMessage struct{ SDP string }
	ClientIceMessage       struct {
		PC        PeerRole
		Candidate IceCandidate
	}
	SelfUpdateMessage struct{ State SelfState }
	PingMessage       struct{ T float64 }
	LeaveMessage      struct{}
	UnknownMessage    struct{ Type string }
)

func (PeekMessage) clientMessage()            {}
func (JoinMessage) clientMessage()            {}
func (PublishOfferMessage) clientMessage()    {}
func (SubscribeAnswerMessage) clientMessage() {}
func (ClientIceMessage) clientMessage()       {}
func (SelfUpdateMessage) clientMessage()      {}
func (PingMessage) clientMessage()            {}
func (LeaveMessage) clientMessage()           {}
func (UnknownMessage) clientMessage()         {}

type rawClientMessage struct {
	Type      string                       `json:"type"`
	Room      string                       `json:"room"`
	Token     string                       `json:"token"`
	Name      string                       `json:"name"`
	Title     string                       `json:"title"`
	Media     *struct{ Audio, Video bool } `json:"media"`
	Resume    string                       `json:"resume"`
	ResumeTok string                       `json:"resumeToken"`
	SDP       string                       `json:"sdp"`
	Sources   map[string]string            `json:"sources"`
	PC        string                       `json:"pc"`
	Candidate json.RawMessage              `json:"candidate"`
	Muted     bool                         `json:"muted"`
	CameraOff bool                         `json:"cameraOff"`
	Sharing   bool                         `json:"screenSharing"`
	T         *float64                     `json:"t"`
}

// ParseClientMessage validates one JSON text frame from the webrelay.
func ParseClientMessage(data []byte) (ClientMessage, error) {
	var raw rawClientMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	switch raw.Type {
	case "room.peek":
		if err := ValidateRoomID(RoomID(raw.Room)); err != nil {
			return nil, err
		}
		if raw.Token == "" || len(raw.Token) > 2048 {
			return nil, fmt.Errorf("room.peek.token missing or too long")
		}
		return PeekMessage{Room: raw.Room, Token: raw.Token}, nil
	case "join":
		name := strings.TrimSpace(raw.Name)
		if name == "" || utf8.RuneCountInString(name) > MaxNameLength {
			return nil, fmt.Errorf("join.name must have 1..%d characters", MaxNameLength)
		}
		title := strings.TrimSpace(raw.Title)
		if utf8.RuneCountInString(title) > MaxTitleLength {
			return nil, fmt.Errorf("join.title must have at most %d characters", MaxTitleLength)
		}
		if err := checkLabel("join.name", name); err != nil {
			return nil, err
		}
		if err := checkLabel("join.title", title); err != nil {
			return nil, err
		}
		if len(raw.ResumeTok) > 256 {
			return nil, fmt.Errorf("join.resumeToken too long")
		}
		msg := JoinMessage{Name: name, Title: title, Resume: ParticipantID(raw.Resume), ResumeToken: raw.ResumeTok}
		if raw.Media != nil {
			msg.Audio, msg.Video = raw.Media.Audio, raw.Media.Video
		}
		return msg, nil
	case "publish.offer":
		if raw.SDP == "" {
			return nil, fmt.Errorf("publish.offer.sdp missing")
		}
		sources := make(map[string]MediaSource, len(raw.Sources))
		for mid, s := range raw.Sources {
			source := MediaSource(s)
			if !source.Valid() {
				return nil, fmt.Errorf("publish.offer.sources[%s]=%q is not a known source", mid, s)
			}
			sources[mid] = source
		}
		return PublishOfferMessage{SDP: raw.SDP, Sources: sources}, nil
	case "subscribe.answer":
		if raw.SDP == "" {
			return nil, fmt.Errorf("subscribe.answer.sdp missing")
		}
		return SubscribeAnswerMessage{SDP: raw.SDP}, nil
	case "ice":
		role := PeerRole(raw.PC)
		if role != RolePublish && role != RoleSubscribe {
			return nil, fmt.Errorf("ice.pc=%q is not publish|subscribe", raw.PC)
		}
		var candidate IceCandidate
		if len(raw.Candidate) == 0 || json.Unmarshal(raw.Candidate, &candidate) != nil {
			return nil, fmt.Errorf("ice.candidate missing")
		}
		return ClientIceMessage{PC: role, Candidate: candidate}, nil
	case "self.update":
		return SelfUpdateMessage{State: SelfState{
			Muted: raw.Muted, CameraOff: raw.CameraOff, ScreenSharing: raw.Sharing,
		}}, nil
	case "ping":
		if raw.T == nil {
			return nil, fmt.Errorf("ping.t missing")
		}
		return PingMessage{T: *raw.T}, nil
	case "leave":
		return LeaveMessage{}, nil
	case "":
		return nil, fmt.Errorf("message type missing")
	default:
		return UnknownMessage{Type: raw.Type}, nil
	}
}

// ---------------------------------------------------------------------------
// Bridge → client
// ---------------------------------------------------------------------------

// ServerMessage is any message the bridge sends; each one marshals with its
// `type` field.
type ServerMessage interface{ MessageType() string }

// HostInfo describes who invites (the lobby's "X te invitó").
type HostInfo struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
}

// ParticipantInfo is the public view of a participant.
type ParticipantInfo struct {
	ID            ParticipantID `json:"id"`
	Name          string        `json:"name"`
	Title         string        `json:"title,omitempty"`
	Muted         bool          `json:"muted"`
	CameraOff     bool          `json:"cameraOff"`
	ScreenSharing bool          `json:"screenSharing"`
}

// TrackInfo maps a subscribe-side mid to its publisher and source.
type TrackInfo struct {
	Participant ParticipantID `json:"participant"`
	Source      MediaSource   `json:"source"`
}

type RoomInfoMessage struct {
	Type      string        `json:"type"`
	Room      RoomID        `json:"room"`
	Title     string        `json:"title"`
	Host      HostInfo      `json:"host"`
	Channels  []string      `json:"channels"`
	Transport TransportMode `json:"transport,omitempty"`
}

type WelcomeMessage struct {
	Type    string        `json:"type"`
	Self    ParticipantID `json:"self"`
	Resumed bool          `json:"resumed"`
	// ResumeToken proves ownership of Self on a later `join {resume}`; it is
	// only ever sent to the connection that owns the participant.
	ResumeToken  string            `json:"resumeToken"`
	Profile      QualityProfile    `json:"profile"`
	IceServers   []IceServer       `json:"iceServers"`
	Participants []ParticipantInfo `json:"participants"`
}

type SDPMessage struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

type ServerIceMessage struct {
	Type      string       `json:"type"`
	PC        PeerRole     `json:"pc"`
	Candidate IceCandidate `json:"candidate"`
}

type TracksMessage struct {
	Type   string               `json:"type"`
	Tracks map[string]TrackInfo `json:"tracks"`
}

type ParticipantMessage struct {
	Type        string          `json:"type"`
	Participant ParticipantInfo `json:"participant"`
}

type ParticipantLeftMessage struct {
	Type string        `json:"type"`
	ID   ParticipantID `json:"id"`
}

type ProfileMessage struct {
	Type    string         `json:"type"`
	Profile QualityProfile `json:"profile"`
}

type PongMessage struct {
	Type string  `json:"type"`
	T    float64 `json:"t"`
}

type ErrorMessage struct {
	Type    string    `json:"type"`
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Fatal   bool      `json:"fatal"`
}

func (m RoomInfoMessage) MessageType() string        { return m.Type }
func (m WelcomeMessage) MessageType() string         { return m.Type }
func (m SDPMessage) MessageType() string             { return m.Type }
func (m ServerIceMessage) MessageType() string       { return m.Type }
func (m TracksMessage) MessageType() string          { return m.Type }
func (m ParticipantMessage) MessageType() string     { return m.Type }
func (m ParticipantLeftMessage) MessageType() string { return m.Type }
func (m ProfileMessage) MessageType() string         { return m.Type }
func (m PongMessage) MessageType() string            { return m.Type }
func (m ErrorMessage) MessageType() string           { return m.Type }

// Constructors fill the `type` field so callers cannot get it wrong.

func NewRoomInfo(id RoomID, title string, host HostInfo, channels []string) RoomInfoMessage {
	if channels == nil {
		channels = []string{}
	}
	return RoomInfoMessage{Type: "room.info", Room: id, Title: title, Host: host, Channels: channels}
}

func NewWelcome(self ParticipantID, resumed bool, resumeToken string, profile QualityProfile, ice []IceServer, participants []ParticipantInfo) WelcomeMessage {
	if ice == nil {
		ice = []IceServer{}
	}
	if participants == nil {
		participants = []ParticipantInfo{}
	}
	return WelcomeMessage{Type: "welcome", Self: self, Resumed: resumed, ResumeToken: resumeToken, Profile: profile, IceServers: ice, Participants: participants}
}

func NewPublishAnswer(sdp string) SDPMessage  { return SDPMessage{Type: "publish.answer", SDP: sdp} }
func NewSubscribeOffer(sdp string) SDPMessage { return SDPMessage{Type: "subscribe.offer", SDP: sdp} }

func NewIce(role PeerRole, c IceCandidate) ServerIceMessage {
	return ServerIceMessage{Type: "ice", PC: role, Candidate: c}
}

func NewTracks(tracks map[string]TrackInfo) TracksMessage {
	return TracksMessage{Type: "tracks", Tracks: tracks}
}

func NewParticipantJoined(p ParticipantInfo) ParticipantMessage {
	return ParticipantMessage{Type: "participant.joined", Participant: p}
}

func NewParticipantUpdated(p ParticipantInfo) ParticipantMessage {
	return ParticipantMessage{Type: "participant.updated", Participant: p}
}

func NewParticipantLeft(id ParticipantID) ParticipantLeftMessage {
	return ParticipantLeftMessage{Type: "participant.left", ID: id}
}

func NewProfile(p QualityProfile) ProfileMessage { return ProfileMessage{Type: "profile", Profile: p} }
func NewPong(t float64) PongMessage              { return PongMessage{Type: "pong", T: t} }

func NewError(err *Error) ErrorMessage {
	return ErrorMessage{Type: "error", Code: err.Code, Message: err.Message, Fatal: err.Fatal}
}
