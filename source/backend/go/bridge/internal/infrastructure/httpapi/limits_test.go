package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/application"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/infrastructure/clock"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/infrastructure/sfu"
)

// tunedServer starts the real handler with limits the test can lower.
func tunedServer(t *testing.T, tweak func(*Server)) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	media, err := sfu.New(sfu.Config{IncludeLoopback: true}, log)
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := domain.NewInviteSigner([]byte(strings.Repeat("x", 32)))
	hub := application.NewHub(application.Config{DevMode: true, Profile: domain.DefaultProfile, WebRelayURL: "http://relay"}, clock.System{}, signer, media, log)
	broadcaster := application.NewBroadcaster(hub, &nullCompositor{live: map[domain.RoomID]bool{}}, media, clock.System{},
		application.BroadcastConfig{Output: domain.DefaultOutput}, log)
	s := buildServer(hub, broadcaster, Options{AdminToken: adminToken, AllowedOrigins: []string{"relay.example"}, DevMode: true,
		WebRelayURL: "http://relay", Output: domain.DefaultOutput}, log)
	if tweak != nil {
		tweak(s)
	}
	srv := httptest.NewServer(s.routes())
	t.Cleanup(func() { broadcaster.StopAll(); srv.Close(); media.Close() })
	return srv
}

func TestWrongAdminTokensLockTheClientOut(t *testing.T) {
	srv := tunedServer(t, nil)
	for i := 0; i < authFailureBurst; i++ {
		if code, _ := call(t, srv, "GET", "/api/v1/rooms", "", "wrong"); code != 401 {
			t.Fatalf("attempt %d: %d", i, code)
		}
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/rooms", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("after the burst: %d retry-after=%q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	// Even the right token is refused while locked out: guessing must not pay.
	if code, body := call(t, srv, "GET", "/api/v1/rooms", "", adminToken); code != 429 || body["code"] != "rate-limited" {
		t.Fatalf("right token while locked: %d %v", code, body)
	}
}

func TestLockoutEndsAndIsPerClient(t *testing.T) {
	now := time.Now()
	srv := tunedServer(t, func(s *Server) { s.now = func() time.Time { return now } })
	for i := 0; i < authFailureBurst; i++ {
		call(t, srv, "GET", "/api/v1/rooms", "", "wrong")
	}
	if code, _ := call(t, srv, "GET", "/api/v1/rooms", "", adminToken); code != 429 {
		t.Fatalf("locked out expected, got %d", code)
	}
	now = now.Add(authFailureEvery + time.Second) // one token is back
	if code, _ := call(t, srv, "GET", "/api/v1/rooms", "", adminToken); code != 200 {
		t.Fatalf("after waiting: %d", code)
	}
}

func TestSuccessfulRequestsAreNeverThrottled(t *testing.T) {
	srv := tunedServer(t, nil)
	for i := 0; i < 5*authFailureBurst; i++ {
		if code, _ := call(t, srv, "GET", "/api/v1/config", "", adminToken); code != 200 {
			t.Fatalf("request %d: %d", i, code)
		}
	}
}

func TestSignalRejectsConnectionFloods(t *testing.T) {
	srv := tunedServer(t, nil)
	var refused int
	for i := 0; i < signalConnBurst+10; i++ {
		c, resp, err := dial(t, srv, "https://relay.example", "ether-webrelay.signaling.v2")
		if err != nil {
			if resp != nil && resp.StatusCode == 429 {
				refused++
			}
			continue
		}
		defer c.CloseNow()
	}
	if refused < 5 {
		t.Fatalf("only %d connections refused out of %d", refused, signalConnBurst+10)
	}
}

func TestSignalCapsOpenConnectionsPerClient(t *testing.T) {
	srv := tunedServer(t, func(s *Server) {
		s.signalConns = newConnCounter(3)
		s.signalRate = newKeyedLimiter(1000, 1000, s.clock)
	})
	var open []*websocket.Conn
	for i := 0; i < 3; i++ {
		c, _, err := dial(t, srv, "https://relay.example", "ether-webrelay.signaling.v2")
		if err != nil {
			t.Fatal(err)
		}
		open = append(open, c)
	}
	if _, resp, err := dial(t, srv, "https://relay.example", "ether-webrelay.signaling.v2"); err == nil || resp == nil || resp.StatusCode != 429 {
		t.Fatalf("4th socket: err=%v resp=%v", err, resp)
	}
	open[0].CloseNow()
	time.Sleep(200 * time.Millisecond) // the server notices the close
	c, _, err := dial(t, srv, "https://relay.example", "ether-webrelay.signaling.v2")
	if err != nil {
		t.Fatalf("a slot should be free again: %v", err)
	}
	c.CloseNow()
}

func TestAnonymousSocketsAreClosed(t *testing.T) {
	srv := tunedServer(t, func(s *Server) { s.admitDeadline = 300 * time.Millisecond })
	c, _, err := dial(t, srv, "https://relay.example", "ether-webrelay.signaling.v2")
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != 4408 {
		t.Fatalf("anonymous socket not closed with 4408: %v", err)
	}
}

func TestSignalMessageFloodIsCutOff(t *testing.T) {
	srv := tunedServer(t, func(s *Server) { s.messageBurst = 5 })
	c, _, err := dial(t, srv, "https://relay.example", "ether-webrelay.signaling.v2")
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		for i := 0; i < 50; i++ {
			if c.Write(ctx, websocket.MessageText, []byte(`{"type":"ping","t":1}`)) != nil {
				return
			}
		}
	}()
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Fatalf("flood ended with %v, want policy violation", err)
			}
			return
		}
	}
}

func TestSecurityHeadersEverywhere(t *testing.T) {
	srv := tunedServer(t, nil)
	for _, path := range []string{"/healthz", "/api/v1/rooms", "/console/"} {
		// No X-Forwarded-Proto: behind the ingress the bridge never learns the scheme.
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		for header, want := range map[string]string{
			"X-Content-Type-Options":       "nosniff",
			"X-Frame-Options":              "DENY",
			"Referrer-Policy":              "no-referrer",
			"Cross-Origin-Resource-Policy": "same-origin",
			"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
		} {
			if got := resp.Header.Get(header); got != want {
				t.Errorf("%s %s = %q, want %q", path, header, got, want)
			}
		}
	}
	console, _ := http.Get(srv.URL + "/console/")
	console.Body.Close()
	if !strings.Contains(console.Header.Get("Permissions-Policy"), "camera=()") {
		t.Errorf("console Permissions-Policy = %q", console.Header.Get("Permissions-Policy"))
	}
}

func TestClientAddr(t *testing.T) {
	req := func(remote, xff string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}
	for name, tc := range map[string]struct {
		r    *http.Request
		hops int
		want string
	}{
		"no proxies: the TCP peer":                {req("203.0.113.5:4000", "198.51.100.1"), 0, "203.0.113.5"},
		"one proxy: what it appended":             {req("10.0.0.2:80", "198.51.100.1"), 1, "198.51.100.1"},
		"a client-forged prefix is ignored":       {req("10.0.0.2:80", "6.6.6.6, 198.51.100.1"), 1, "198.51.100.1"},
		"two proxies":                             {req("10.0.0.3:80", "198.51.100.1, 10.0.0.2"), 2, "198.51.100.1"},
		"header shorter than the hops: no client": {req("10.0.0.2:80", "198.51.100.1"), 2, ""},
		"no header behind a proxy: no client":     {req("10.0.0.2:80", ""), 1, ""},
		"garbage in the header: no client":        {req("10.0.0.2:80", "not-an-ip"), 1, ""},
		"IPv6 peer":                               {req("[2001:db8::1]:443", ""), 0, "2001:db8::1"},
	} {
		if got := clientAddr(tc.r, tc.hops); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

// Behind a proxy whose X-Forwarded-For is missing, the only address is the
// proxy's, shared by everyone: limiting on it would let one client lock out all
// the others. The limits must step aside instead.
func TestLimitsStepAsideWhenTheClientIsUnknown(t *testing.T) {
	srv := tunedServer(t, func(s *Server) { s.opts.TrustedProxyHops = 1 })
	for i := 0; i < 3*authFailureBurst; i++ {
		if code, _ := call(t, srv, "GET", "/api/v1/rooms", "", "wrong"); code != 401 {
			t.Fatalf("attempt %d got %d, want 401 (not 429)", i, code)
		}
	}
	if code, _ := call(t, srv, "GET", "/api/v1/rooms", "", adminToken); code != 200 {
		t.Fatalf("the admin was locked out by someone else's failures: %d", code)
	}
	for i := 0; i < 3*signalConnBurst; i++ {
		c, resp, err := dial(t, srv, "https://relay.example", "ether-webrelay.signaling.v2")
		if err != nil {
			t.Fatalf("socket %d refused (%v)", i, resp)
		}
		defer c.CloseNow()
	}
}

func TestLimitsKeyOnTheForwardedClient(t *testing.T) {
	srv := tunedServer(t, func(s *Server) { s.opts.TrustedProxyHops = 1 })
	send := func(client, token string) int {
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/rooms", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Forwarded-For", "6.6.6.6, "+client) // the left part is client-forged
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for i := 0; i < authFailureBurst; i++ {
		send("203.0.113.9", "wrong")
	}
	if code := send("203.0.113.9", adminToken); code != 429 {
		t.Fatalf("the abusive client was not cut off: %d", code)
	}
	if code := send("198.51.100.4", adminToken); code != 200 {
		t.Fatalf("another client was affected: %d", code)
	}
}
