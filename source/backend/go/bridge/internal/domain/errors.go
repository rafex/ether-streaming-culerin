// Package domain holds the bridge's pure model: rooms, participants,
// invitations, quality profiles, the signaling v2 messages and the broadcast
// composition. It has no I/O and imports nothing outside the standard library.
package domain

import "fmt"

// ErrorCode is a machine-readable error shared with the webrelay (signaling
// v2 `error.code`) and the host API.
type ErrorCode string

const (
	CodeInvalidToken   ErrorCode = "invalid-token"
	CodeRoomNotFound   ErrorCode = "room-not-found"
	CodeRoomClosed     ErrorCode = "room-closed"
	CodeRoomFull       ErrorCode = "room-full"
	CodeKicked         ErrorCode = "kicked"
	CodeDenied         ErrorCode = "denied"
	CodeBadRequest     ErrorCode = "bad-request"
	CodeNotJoined      ErrorCode = "not-joined"
	CodeConflict       ErrorCode = "conflict"
	CodeNotFound       ErrorCode = "not-found"
	CodeNoDestinations ErrorCode = "no-destinations"
	CodeMedia          ErrorCode = "media-error"
)

// Error is a domain error with a code, a human message and whether a client
// must stop retrying (signaling v2 `fatal`).
type Error struct {
	Code    ErrorCode
	Message string
	Fatal   bool
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// Errorf builds a non-fatal domain error.
func Errorf(code ErrorCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Fatalf builds a fatal domain error (the client must not reconnect).
func Fatalf(code ErrorCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Fatal: true}
}
