// Package ports declares what the application needs from the outside world
// (SFU, compositor, connections, clock). Adapters in infrastructure/
// implement them; the application never imports an adapter.
package ports

import (
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

// Clock abstracts time for tests.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is a cancellable scheduled call.
type Timer interface{ Stop() bool }

// Connection is one webrelay signaling socket. Send must not block: adapters
// queue messages and drop the connection if the client cannot keep up.
type Connection interface {
	Send(msg domain.ServerMessage)
	// Close ends the socket with an application close code (4xxx).
	Close(code int, reason string)
}

// SignalSink receives the messages the SFU originates for a participant
// (tracks, subscribe.offer, ice). The application routes them to the
// participant's current connection, which may change on resume.
type SignalSink func(msg domain.ServerMessage)

// SFU forwards media between the participants of a room.
type SFU interface {
	// Attach creates the participant's media peer: a subscribe connection
	// that receives the room's current and future tracks.
	Attach(room domain.RoomID, participant domain.ParticipantID, sink SignalSink) (MediaPeer, error)
	// SetListener registers who is told when tracks appear and disappear.
	SetListener(listener MediaListener)
}

// MediaPeer is one participant's pair of peer connections.
type MediaPeer interface {
	// Publish applies a publish offer and returns the answer SDP.
	Publish(sdp string, sources map[string]domain.MediaSource) (string, error)
	// AnswerSubscribe applies the participant's subscribe answer.
	AnswerSubscribe(sdp string) error
	// AddCandidate applies a trickled ICE candidate (queued if early).
	AddCandidate(role domain.PeerRole, candidate domain.IceCandidate) error
	// Resume is called when the participant reconnects its signaling; the
	// peer re-offers the subscribe connection (ICE restart if needed).
	Resume()
	// Close tears down both connections and unpublishes the tracks.
	Close()
}

// MediaListener is notified (asynchronously) of published tracks.
type MediaListener interface {
	SourcePublished(room domain.RoomID, participant domain.ParticipantID, source domain.MediaSource)
	SourceUnpublished(room domain.RoomID, participant domain.ParticipantID, source domain.MediaSource)
}

// Codec describes a published track for the compositor's RTP input.
type Codec struct {
	MimeType    string // video/VP8, video/H264, audio/opus
	ClockRate   uint32
	Channels    uint16
	PayloadType uint8
	Fmtp        string
}

// MediaTap lets the compositor read the room's tracks.
type MediaTap interface {
	// Tap copies every RTP packet of the source to fn (which must not block)
	// until stop is called.
	Tap(room domain.RoomID, ref domain.SourceRef, fn func(packet []byte)) (codec Codec, stop func(), err error)
	// RequestKeyframe asks the publisher for a keyframe (PLI).
	RequestKeyframe(room domain.RoomID, ref domain.SourceRef)
	// LastPacket is when the source last delivered media (zero if never).
	LastPacket(room domain.RoomID, ref domain.SourceRef) time.Time
}

// Output is one destination of the broadcast, already resolved to its
// publish URL (which contains the secret stream key).
type Output struct {
	ID  string
	URL string
}

// BroadcastStatus is what the compositor reports for a room.
type BroadcastStatus struct {
	Live      bool      `json:"live"`
	Since     time.Time `json:"since,omitzero"`
	Restarts  int       `json:"restarts"`
	LastError string    `json:"lastError,omitempty"`
}

// Compositor renders a CompositionPlan and publishes it to the outputs.
type Compositor interface {
	// Apply starts the broadcast or brings it in line with plan/outputs.
	Apply(room domain.RoomID, plan domain.CompositionPlan, outputs []Output) error
	// Stop ends the room's broadcast.
	Stop(room domain.RoomID)
	// Status reports the room's broadcast state.
	Status(room domain.RoomID) BroadcastStatus
}
