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
// /join/<room>?t=<token>). A token binds a room, an expiry and the id of an
// invitation record the bridge keeps (usage, revocation):
//
//	base64url("v2.<room>.<unix-expiry>.<id>") + "." + base64url(HMAC-SHA256(secret, payload))
//
// The signature proves the bridge issued the token; whether it still works
// (not revoked, uses left) is decided by the record, which lives in memory
// like the rooms it belongs to.
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
	ID        string
	Token     string
	ExpiresAt time.Time
}

// InviteClaims is what a verified token says.
type InviteClaims struct {
	Room      RoomID
	ID        string
	ExpiresAt time.Time
}

// ValidInviteID reports whether id can be embedded in a token.
func ValidInviteID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// Issue signs a token for invitation id of room, valid until expiresAt.
func (s *InviteSigner) Issue(room RoomID, id string, expiresAt time.Time) Invite {
	payload := fmt.Sprintf("v2.%s.%d.%s", room, expiresAt.Unix(), id)
	token := enc(payload) + "." + enc(string(s.mac(payload)))
	return Invite{Room: room, ID: id, Token: token, ExpiresAt: time.Unix(expiresAt.Unix(), 0).UTC()}
}

// Verify checks signature, room and expiry and returns the claims.
func (s *InviteSigner) Verify(room RoomID, token string, now time.Time) (InviteClaims, error) {
	invalid := Fatalf(CodeInvalidToken, "invitation token is invalid or expired")
	encodedPayload, encodedMAC, ok := strings.Cut(token, ".")
	if !ok {
		return InviteClaims{}, invalid
	}
	payload, err1 := dec(encodedPayload)
	mac, err2 := dec(encodedMAC)
	if err1 != nil || err2 != nil || !hmac.Equal([]byte(mac), s.mac(payload)) {
		return InviteClaims{}, invalid
	}
	parts := strings.Split(payload, ".")
	if len(parts) != 4 || parts[0] != "v2" || RoomID(parts[1]) != room || !ValidInviteID(parts[3]) {
		return InviteClaims{}, invalid
	}
	expiry, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || !now.Before(time.Unix(expiry, 0)) {
		return InviteClaims{}, invalid
	}
	return InviteClaims{Room: room, ID: parts[3], ExpiresAt: time.Unix(expiry, 0).UTC()}, nil
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
