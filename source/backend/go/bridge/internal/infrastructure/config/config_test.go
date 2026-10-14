package config

import (
	"log/slog"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestDevModeDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"BRIDGE_DEV_MODE": "true"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8090" || cfg.AdminToken != "dev" || !cfg.GeneratedAdminToken || len(cfg.InviteSecret) < 32 ||
		cfg.WebRelayURL != "http://localhost:5173" || cfg.LogLevel != slog.LevelInfo || len(cfg.AllowedOrigins) != 2 {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestProductionRequiresSecrets(t *testing.T) {
	_, err := Load(env(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "BRIDGE_INVITE_SECRET") || !strings.Contains(err.Error(), "BRIDGE_ADMIN_TOKEN") {
		t.Fatalf("err = %v", err)
	}
	cfg, err := Load(env(map[string]string{
		"BRIDGE_INVITE_SECRET": strings.Repeat("s", 32), "BRIDGE_ADMIN_TOKEN": strings.Repeat("a", 24),
		"BRIDGE_WEBRELAY_URL": "https://relay.example/", "BRIDGE_ICE_PUBLIC_IPS": "203.0.113.7, 198.51.100.2",
		"BRIDGE_ICE_SERVERS": `[{"urls":["stun:stun.example:3478"]}]`, "BRIDGE_LOG_LEVEL": "debug",
		"BRIDGE_PROFILE": `{"width":640,"height":360}`, "BRIDGE_OUTPUT": `{"videoBitrate":6000000}`,
		"BRIDGE_ICE_UDP_PORT": "8443", "BRIDGE_ALLOWED_ORIGINS": "relay.example",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebRelayURL != "https://relay.example" || len(cfg.ICEPublicIPs) != 2 || cfg.IceServers[0].URLs[0] != "stun:stun.example:3478" ||
		cfg.Profile.Width != 640 || cfg.Profile.Framerate != 30 || cfg.Output.VideoBitrate != 6_000_000 || cfg.ICEUDPPort != 8443 ||
		cfg.LogLevel != slog.LevelDebug || cfg.AllowedOrigins[0] != "relay.example" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestInvalidValues(t *testing.T) {
	for key, value := range map[string]string{
		"BRIDGE_DEV_MODE":      "maybe",
		"BRIDGE_INVITE_SECRET": "short",
		"BRIDGE_ICE_UDP_PORT":  "70000",
		"BRIDGE_INVITE_TTL":    "-1h",
		"BRIDGE_LOG_LEVEL":     "loud",
		"BRIDGE_ICE_SERVERS":   "{",
		"BRIDGE_PROFILE":       `{"framerate":0}`,
		"BRIDGE_OUTPUT":        `{"width":1281}`,
		"BRIDGE_RTP_PORT_MAX":  "40001",
	} {
		vars := map[string]string{"BRIDGE_DEV_MODE": "true", key: value}
		if _, err := Load(env(vars)); err == nil || !strings.Contains(err.Error(), strings.SplitN(key, "_", 2)[0]) {
			t.Errorf("%s=%s accepted (err %v)", key, value, err)
		}
	}
	if _, err := Load(env(map[string]string{"BRIDGE_INVITE_SECRET": strings.Repeat("s", 32), "BRIDGE_ADMIN_TOKEN": "short"})); err == nil {
		t.Fatal("short admin token accepted outside dev mode")
	}
}
