package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var secret = []byte(strings.Repeat("s", MinInviteSecretLength))

func TestInviteRoundTrip(t *testing.T) {
	signer, err := NewInviteSigner(secret)
	if err != nil {
		t.Fatal(err)
	}
	inv := signer.Issue("demo", now.Add(time.Hour))
	if inv.Room != "demo" || !inv.ExpiresAt.Equal(now.Add(time.Hour)) || strings.ContainsAny(inv.Token, "+/=?&") {
		t.Fatalf("invite = %+v (token must be URL-safe)", inv)
	}
	if err := signer.Verify("demo", inv.Token, now); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
}

func TestInviteRejections(t *testing.T) {
	signer, _ := NewInviteSigner(secret)
	other, _ := NewInviteSigner([]byte(strings.Repeat("o", MinInviteSecretLength)))
	valid := signer.Issue("demo", now.Add(time.Hour)).Token
	cases := map[string]struct {
		room  RoomID
		token string
		at    time.Time
	}{
		"other room":       {"other", valid, now},
		"expired":          {"demo", valid, now.Add(time.Hour)},
		"other secret":     {"demo", other.Issue("demo", now.Add(time.Hour)).Token, now},
		"no dot":           {"demo", "abc", now},
		"bad base64":       {"demo", "!!!.???", now},
		"tampered payload": {"demo", enc("v1.demo.99999999999") + valid[strings.Index(valid, "."):], now},
		"wrong version":    {"demo", forge("v2.demo.99999999999"), now},
		"bad expiry":       {"demo", forge("v1.demo.soon"), now},
	}
	for name, tc := range cases {
		err := signer.Verify(tc.room, tc.token, tc.at)
		var de *Error
		if !errors.As(err, &de) || de.Code != CodeInvalidToken || !de.Fatal {
			t.Errorf("%s: expected fatal invalid-token, got %v", name, err)
		}
	}
	if _, err := NewInviteSigner([]byte("short")); err == nil {
		t.Fatal("short secret accepted")
	}
}

// forge signs an arbitrary payload with the test secret.
func forge(payload string) string {
	s, _ := NewInviteSigner(secret)
	return enc(payload) + "." + enc(string(s.mac(payload)))
}
