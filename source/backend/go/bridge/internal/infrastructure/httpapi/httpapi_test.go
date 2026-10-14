package httpapi

import (
	"context"
	"encoding/json"
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
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

const adminToken = "test-admin-token-123"

type nullCompositor struct{ live map[domain.RoomID]bool }

func (c *nullCompositor) Apply(id domain.RoomID, _ domain.CompositionPlan, _ []ports.Output) error {
	c.live[id] = true
	return nil
}
func (c *nullCompositor) Stop(id domain.RoomID) { delete(c.live, id) }
func (c *nullCompositor) Status(id domain.RoomID) ports.BroadcastStatus {
	return ports.BroadcastStatus{Live: c.live[id]}
}

func newServer(t *testing.T, dev bool) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	media, err := sfu.New(sfu.Config{IncludeLoopback: true}, log)
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := domain.NewInviteSigner([]byte(strings.Repeat("x", 32)))
	hub := application.NewHub(application.Config{DevMode: dev, Profile: domain.DefaultProfile, WebRelayURL: "http://relay"}, clock.System{}, signer, media, log)
	broadcaster := application.NewBroadcaster(hub, &nullCompositor{live: map[domain.RoomID]bool{}}, media, clock.System{},
		application.BroadcastConfig{Output: domain.DefaultOutput}, log)
	srv := httptest.NewServer(New(hub, broadcaster, Options{
		AdminToken: adminToken, AllowedOrigins: []string{"relay.example"}, DevMode: dev, WebRelayURL: "http://relay", Output: domain.DefaultOutput,
	}, log))
	t.Cleanup(func() { broadcaster.StopAll(); srv.Close(); media.Close() })
	return srv
}

func call(t *testing.T, srv *httptest.Server, method, path, body string, token string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestAPIRequiresTheAdminToken(t *testing.T) {
	srv := newServer(t, false)
	for _, token := range []string{"", "wrong"} {
		if code, body := call(t, srv, "GET", "/api/v1/rooms", "", token); code != 401 || body["code"] != "unauthorized" {
			t.Fatalf("token %q: %d %v", token, code, body)
		}
	}
	if code, body := call(t, srv, "GET", "/api/v1/config", "", adminToken); code != 200 || body["devMode"] != false {
		t.Fatalf("config: %d %v", code, body)
	}
	if code, _ := call(t, srv, "GET", "/healthz", "", ""); code != 200 {
		t.Fatal("healthz must be public")
	}
}

func TestAPIRoomLifecycle(t *testing.T) {
	srv := newServer(t, false)
	code, room := call(t, srv, "POST", "/api/v1/rooms", `{"id":"demo","title":"Demo","host":{"name":"Rafex"},"capacity":4,"layout":"grid"}`, adminToken)
	if code != 201 || room["id"] != "demo" || room["layout"] != "grid" {
		t.Fatalf("create: %d %v", code, room)
	}
	if code, body := call(t, srv, "POST", "/api/v1/rooms", `{"id":"demo","title":"Demo","host":{"name":"Rafex"}}`, adminToken); code != 409 {
		t.Fatalf("duplicate: %d %v", code, body)
	}
	if code, _ := call(t, srv, "POST", "/api/v1/rooms", `{"title":"Demo","bogus":1}`, adminToken); code != 400 {
		t.Fatal("unknown fields must be rejected")
	}
	if code, _ := call(t, srv, "POST", "/api/v1/rooms", `{"title":""}`, adminToken); code != 400 {
		t.Fatal("invalid room accepted")
	}
	if code, body := call(t, srv, "PATCH", "/api/v1/rooms/demo", `{"title":"Nuevo","host":{"name":"Otro"}}`, adminToken); code != 200 || body["title"] != "Nuevo" {
		t.Fatalf("patch: %d %v", code, body)
	}
	if code, _ := call(t, srv, "PATCH", "/api/v1/rooms/demo", `{"layout":"pip"}`, adminToken); code != 400 {
		t.Fatal("bad layout accepted")
	}

	code, inv := call(t, srv, "POST", "/api/v1/rooms/demo/invites", `{"ttlSeconds":3600}`, adminToken)
	if code != 201 || !strings.HasPrefix(inv["url"].(string), "http://relay/join/demo?t=") {
		t.Fatalf("invite: %d %v", code, inv)
	}
	if code, _ := call(t, srv, "POST", "/api/v1/rooms/demo/invites", "", adminToken); code != 201 {
		t.Fatal("invite with default ttl")
	}

	if code, _ := call(t, srv, "POST", "/api/v1/rooms/demo/broadcast/start", "", adminToken); code != 409 {
		t.Fatal("going live without destinations must conflict")
	}
	code, dest := call(t, srv, "POST", "/api/v1/rooms/demo/destinations", `{"name":"YT","url":"rtmps://a.rtmp.youtube.com/live2","streamKey":"super-secret-1234"}`, adminToken)
	if code != 201 || dest["kind"] != "youtube" || strings.Contains(dest["streamKey"].(string), "secret") {
		t.Fatalf("destination: %d %v", code, dest)
	}
	id := dest["id"].(string)
	if code, _ := call(t, srv, "POST", "/api/v1/rooms/demo/destinations", `{"name":"x","url":"ftp://x"}`, adminToken); code != 400 {
		t.Fatal("invalid destination accepted")
	}
	if code, body := call(t, srv, "POST", "/api/v1/rooms/demo/broadcast/start", "", adminToken); code != 200 || body["broadcast"].(map[string]any)["live"] != true {
		t.Fatalf("start: %d %v", code, body)
	}
	if code, body := call(t, srv, "GET", "/api/v1/rooms/demo/layout", "", adminToken); code != 200 || body["title"] != "Nuevo" {
		t.Fatalf("layout: %d %v", code, body)
	}
	if code, body := call(t, srv, "POST", "/api/v1/rooms/demo/broadcast/stop", "", adminToken); code != 200 || body["broadcast"].(map[string]any)["live"] != false {
		t.Fatalf("stop: %d %v", code, body)
	}
	if code, _ := call(t, srv, "PATCH", "/api/v1/rooms/demo/destinations/"+id, `{"enabled":false}`, adminToken); code != 200 {
		t.Fatal("disable destination")
	}
	if code, _ := call(t, srv, "PATCH", "/api/v1/rooms/demo/destinations/"+id, `{}`, adminToken); code != 400 {
		t.Fatal("enabled is required")
	}
	if code, _ := call(t, srv, "DELETE", "/api/v1/rooms/demo/destinations/"+id, "", adminToken); code != 204 {
		t.Fatal("delete destination")
	}
	if code, _ := call(t, srv, "PATCH", "/api/v1/rooms/demo/participants/p-1", `{"onStage":false}`, adminToken); code != 404 {
		t.Fatal("unknown participant")
	}
	if code, _ := call(t, srv, "PATCH", "/api/v1/rooms/demo/participants/p-1", `{}`, adminToken); code != 400 {
		t.Fatal("onStage is required")
	}
	if code, _ := call(t, srv, "DELETE", "/api/v1/rooms/demo/participants/p-1", "", adminToken); code != 404 {
		t.Fatal("kick unknown participant")
	}

	code, list := call(t, srv, "GET", "/api/v1/rooms", "", adminToken)
	if code != 200 || len(list["rooms"].([]any)) != 1 {
		t.Fatalf("list: %d %v", code, list)
	}
	if code, _ := call(t, srv, "DELETE", "/api/v1/rooms/demo", "", adminToken); code != 204 {
		t.Fatal("close room")
	}
	if code, body := call(t, srv, "GET", "/api/v1/rooms/demo", "", adminToken); code != 200 || body["closed"] != true {
		t.Fatalf("closed room: %d %v", code, body)
	}
	if code, _ := call(t, srv, "GET", "/api/v1/rooms/ghost", "", adminToken); code != 404 {
		t.Fatal("unknown room")
	}
	if code, _ := call(t, srv, "POST", "/api/v1/rooms/ghost/broadcast/stop", "", adminToken); code != 404 {
		t.Fatal("stop on unknown room")
	}
}

func TestConsoleIsServedWithACSP(t *testing.T) {
	srv := newServer(t, false)
	resp, err := http.Get(srv.URL + "/console/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Consola de anfitrión") ||
		!strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatalf("console: %d %s", resp.StatusCode, resp.Header)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, _ = client.Get(srv.URL + "/")
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/console/" {
		t.Fatalf("root redirect: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func dial(t *testing.T, srv *httptest.Server, origin string, protocols ...string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/signal",
		&websocket.DialOptions{Subprotocols: protocols, HTTPHeader: header})
}

func roundTrip(t *testing.T, c *websocket.Conn, msg string, wantType string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if msg != "" {
		if err := c.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
			t.Fatal(err)
		}
	}
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %s: %v", wantType, err)
		}
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if m["type"] == wantType {
			return m
		}
	}
}

func TestSignalingOverWebSocket(t *testing.T) {
	srv := newServer(t, true)
	if _, resp, err := dial(t, srv, "http://relay.example"); err == nil || resp == nil || resp.StatusCode != 400 {
		t.Fatal("the v2 subprotocol must be required")
	}
	if _, _, err := dial(t, srv, "http://evil.example", domain.SignalingSubprotocol); err == nil {
		t.Fatal("foreign origins must be rejected")
	}

	c, _, err := dial(t, srv, "http://relay.example", domain.SignalingSubprotocol)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if c.Subprotocol() != domain.SignalingSubprotocol {
		t.Fatalf("subprotocol = %q", c.Subprotocol())
	}
	info := roundTrip(t, c, `{"type":"room.peek","room":"e2e","token":"dev"}`, "room.info")
	if info["title"] != "Sala de pruebas · e2e" {
		t.Fatalf("room.info = %v", info)
	}
	welcome := roundTrip(t, c, `{"type":"join","name":"Ana","media":{"audio":true,"video":true}}`, "welcome")
	if welcome["self"] != "p-1" || welcome["profile"].(map[string]any)["width"] != float64(1280) {
		t.Fatalf("welcome = %v", welcome)
	}
	if pong := roundTrip(t, c, `{"type":"ping","t":7}`, "pong"); pong["t"] != float64(7) {
		t.Fatalf("pong = %v", pong)
	}

	// A second guest sees Ana; Ana is told about the newcomer.
	b, _, err := dial(t, srv, "http://relay.example", domain.SignalingSubprotocol)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, b, `{"type":"room.peek","room":"e2e","token":"dev"}`, "room.info")
	if w := roundTrip(t, b, `{"type":"join","name":"Beto"}`, "welcome"); len(w["participants"].([]any)) != 2 {
		t.Fatalf("Beto's welcome = %v", w)
	}
	if j := roundTrip(t, c, "", "participant.joined"); j["participant"].(map[string]any)["name"] != "Beto" {
		t.Fatalf("joined = %v", j)
	}
	b.Close(websocket.StatusNormalClosure, "")

	// A rejected invitation is a fatal error followed by a 4401 close.
	bad, _, err := dial(t, srv, "http://relay.example", domain.SignalingSubprotocol)
	if err != nil {
		t.Fatal(err)
	}
	e := roundTrip(t, bad, `{"type":"room.peek","room":"e2e","token":"invalid"}`, "error")
	if e["code"] != "invalid-token" || e["fatal"] != true {
		t.Fatalf("error = %v", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := bad.Read(ctx); websocket.CloseStatus(err) != 4401 {
		t.Fatalf("close status = %v", err)
	}
}
