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
	inv := signer.Issue("demo", "abc123", now.Add(time.Hour))
	if inv.Room != "demo" || inv.ID != "abc123" || !inv.ExpiresAt.Equal(now.Add(time.Hour)) || strings.ContainsAny(inv.Token, "+/=?&") {
		t.Fatalf("invite = %+v (token must be URL-safe)", inv)
	}
	claims, err := signer.Verify("demo", inv.Token, now)
	if err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if claims.Room != "demo" || claims.ID != "abc123" || !claims.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestInviteRejections(t *testing.T) {
	signer, _ := NewInviteSigner(secret)
	other, _ := NewInviteSigner([]byte(strings.Repeat("o", MinInviteSecretLength)))
	valid := signer.Issue("demo", "abc123", now.Add(time.Hour)).Token
	cases := map[string]struct {
		room  RoomID
		token string
		at    time.Time
	}{
		"other room":       {"other", valid, now},
		"expired":          {"demo", valid, now.Add(time.Hour)},
		"other secret":     {"demo", other.Issue("demo", "abc123", now.Add(time.Hour)).Token, now},
		"no dot":           {"demo", "abc", now},
		"bad base64":       {"demo", "!!!.???", now},
		"tampered payload": {"demo", enc("v2.demo.99999999999.abc123") + valid[strings.Index(valid, "."):], now},
		"old v1 format":    {"demo", forge("v1.demo.99999999999"), now},
		"unknown version":  {"demo", forge("v3.demo.99999999999.abc123"), now},
		"missing id":       {"demo", forge("v2.demo.99999999999"), now},
		"empty id":         {"demo", forge("v2.demo.99999999999."), now},
		"id with a dot":    {"demo", forge("v2.demo.99999999999.a.b"), now},
		"id upper case":    {"demo", forge("v2.demo.99999999999.ABC"), now},
		"bad expiry":       {"demo", forge("v2.demo.soon.abc123"), now},
	}
	for name, tc := range cases {
		_, err := signer.Verify(tc.room, tc.token, tc.at)
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
