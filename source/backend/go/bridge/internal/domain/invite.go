package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MinInviteSecretLength guards against trivially guessable HMAC keys.
const MinInviteSecretLength = 32

// InviteSigner issues and verifies invitation tokens (the `t` of
// /join/<room>?t=<token>). A token binds a room and an expiry:
//
//	base64url("v1.<room>.<unix-expiry>") + "." + base64url(HMAC-SHA256(secret, payload))
//
// Tokens are bearer credentials scoped to one room; they are stateless, so
// closing the room is how a host revokes them.
type InviteSigner struct {
	secret []byte
}

// NewInviteSigner requires a secret of at least MinInviteSecretLength bytes.
func NewInviteSigner(secret []byte) (*InviteSigner, error) {
	if len(secret) < MinInviteSecretLength {
		return nil, fmt.Errorf("invite secret must have at least %d bytes", MinInviteSecretLength)
	}
	return &InviteSigner{secret: append([]byte(nil), secret...)}, nil
}

// Invite is an issued invitation.
type Invite struct {
	Room      RoomID
	Token     string
	ExpiresAt time.Time
}

// Issue signs a token for room valid until expiresAt.
func (s *InviteSigner) Issue(room RoomID, expiresAt time.Time) Invite {
	payload := fmt.Sprintf("v1.%s.%d", room, expiresAt.Unix())
	token := enc(payload) + "." + enc(string(s.mac(payload)))
	return Invite{Room: room, Token: token, ExpiresAt: time.Unix(expiresAt.Unix(), 0).UTC()}
}

// Verify checks signature, room and expiry.
func (s *InviteSigner) Verify(room RoomID, token string, now time.Time) error {
	invalid := Fatalf(CodeInvalidToken, "invitation token is invalid or expired")
	encodedPayload, encodedMAC, ok := strings.Cut(token, ".")
	if !ok {
		return invalid
	}
	payload, err1 := dec(encodedPayload)
	mac, err2 := dec(encodedMAC)
	if err1 != nil || err2 != nil || !hmac.Equal([]byte(mac), s.mac(payload)) {
		return invalid
	}
	parts := strings.Split(payload, ".")
	if len(parts) != 3 || parts[0] != "v1" || RoomID(parts[1]) != room {
		return invalid
	}
	expiry, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || !now.Before(time.Unix(expiry, 0)) {
		return invalid
	}
	return nil
}

func (s *InviteSigner) mac(payload string) []byte {
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte(payload))
	return h.Sum(nil)
}

func enc(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func dec(s string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	return string(b), err
}
