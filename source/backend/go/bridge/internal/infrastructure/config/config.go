// Package config reads the bridge configuration from BRIDGE_* environment
// variables (12-factor; secrets are injected by the platform, never stored
// in the repository).
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

// MinAdminTokenLength is the shortest admin token accepted outside development
// mode; it guards every room, invitation and stream key.
const MinAdminTokenLength = 32

// Config is the whole bridge configuration.
type Config struct {
	Addr       string
	DevMode    bool
	AdminToken string
	// AdminTokenNext is a second token accepted during a rotation (optional).
	AdminTokenNext string
	InviteSecret   []byte
	InviteTTL      time.Duration
	WebRelayURL    string
	AllowedOrigins []string
	Profile        domain.QualityProfile
	Output         domain.OutputSettings
	IceServers     []domain.IceServer
	ICEUDPPort     int
	ICEPublicIPs   []string
	ICELoopback    bool
	FFmpegPath     string
	FontFile       string
	WorkDir        string
	RecordDir      string
	RTPPortMin     int
	RTPPortMax     int
	LogLevel       slog.Level
	// TrustedProxyHops is how many reverse proxies append to X-Forwarded-For.
	TrustedProxyHops int
	// MaxRooms and MaxDestinations cap what the admin API can allocate.
	MaxRooms        int
	MaxDestinations int
	// AllowPrivateDestinations permits RTMP destinations on loopback/private
	// hosts; it defaults to DevMode (local sinks), never on in production.
	AllowPrivateDestinations bool
	// Generated is true for values invented because they were missing in
	// development mode (they are logged so the developer can use them).
	GeneratedAdminToken bool
}

// Load reads and validates the environment. Outside development mode the
// admin token and the invitation secret are mandatory.
func Load(getenv func(string) string) (Config, error) {
	env := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	var errs []string
	fail := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	cfg := Config{
		Addr:           env("BRIDGE_ADDR", ":8090"),
		WebRelayURL:    strings.TrimRight(env("BRIDGE_WEBRELAY_URL", "http://localhost:5173"), "/"),
		AdminToken:     env("BRIDGE_ADMIN_TOKEN", ""),
		AdminTokenNext: env("BRIDGE_ADMIN_TOKEN_NEXT", ""),
		FFmpegPath:     env("BRIDGE_FFMPEG", "ffmpeg"),
		FontFile:       env("BRIDGE_FONT_FILE", ""),
		WorkDir:        env("BRIDGE_WORK_DIR", os.TempDir()),
		RecordDir:      env("BRIDGE_RECORD_DIR", ""),
		Profile:        domain.DefaultProfile,
		Output:         domain.DefaultOutput,
	}
	cfg.DevMode = parseBool(env("BRIDGE_DEV_MODE", "false"), fail, "BRIDGE_DEV_MODE")
	cfg.ICELoopback = parseBool(env("BRIDGE_ICE_LOOPBACK", "false"), fail, "BRIDGE_ICE_LOOPBACK")
	cfg.InviteTTL = parseDuration(env("BRIDGE_INVITE_TTL", "24h"), fail, "BRIDGE_INVITE_TTL")
	cfg.ICEUDPPort = parseInt(env("BRIDGE_ICE_UDP_PORT", "0"), fail, "BRIDGE_ICE_UDP_PORT", 0, 65535)
	cfg.RTPPortMin = parseInt(env("BRIDGE_RTP_PORT_MIN", "40000"), fail, "BRIDGE_RTP_PORT_MIN", 1024, 65000)
	cfg.RTPPortMax = parseInt(env("BRIDGE_RTP_PORT_MAX", "40999"), fail, "BRIDGE_RTP_PORT_MAX", 1025, 65535)
	if cfg.RTPPortMax-cfg.RTPPortMin < 16 {
		fail("BRIDGE_RTP_PORT_MAX must be at least BRIDGE_RTP_PORT_MIN+16")
	}
	cfg.TrustedProxyHops = parseInt(env("BRIDGE_TRUSTED_PROXY_HOPS", "0"), fail, "BRIDGE_TRUSTED_PROXY_HOPS", 0, 5)
	cfg.MaxRooms = parseInt(env("BRIDGE_MAX_ROOMS", "50"), fail, "BRIDGE_MAX_ROOMS", 1, 1000)
	cfg.MaxDestinations = parseInt(env("BRIDGE_MAX_DESTINATIONS", "8"), fail, "BRIDGE_MAX_DESTINATIONS", 1, 50)
	cfg.AllowPrivateDestinations = parseBool(env("BRIDGE_ALLOW_PRIVATE_DESTINATIONS", strconv.FormatBool(cfg.DevMode)), fail, "BRIDGE_ALLOW_PRIVATE_DESTINATIONS")
	cfg.ICEPublicIPs = splitList(env("BRIDGE_ICE_PUBLIC_IPS", ""))
	cfg.AllowedOrigins = splitList(env("BRIDGE_ALLOWED_ORIGINS", "localhost:*,127.0.0.1:*"))

	switch strings.ToLower(env("BRIDGE_LOG_LEVEL", "info")) {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "info":
		cfg.LogLevel = slog.LevelInfo
	case "warn":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		fail("BRIDGE_LOG_LEVEL must be debug|info|warn|error")
	}

	if raw := env("BRIDGE_ICE_SERVERS", ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.IceServers); err != nil {
			fail("BRIDGE_ICE_SERVERS must be a JSON array of {urls, username?, credential?}: %v", err)
		}
	}
	if raw := env("BRIDGE_PROFILE", ""); raw != "" {
		profile := domain.DefaultProfile
		if err := json.Unmarshal([]byte(raw), &profile); err != nil {
			fail("BRIDGE_PROFILE must be JSON: %v", err)
		} else if err := profile.Validate(); err != nil {
			fail("BRIDGE_PROFILE: %v", err)
		}
		cfg.Profile = profile
	}
	if raw := env("BRIDGE_OUTPUT", ""); raw != "" {
		output := domain.DefaultOutput
		if err := json.Unmarshal([]byte(raw), &output); err != nil {
			fail("BRIDGE_OUTPUT must be JSON: %v", err)
		} else if err := output.Validate(); err != nil {
			fail("BRIDGE_OUTPUT: %v", err)
		}
		cfg.Output = output
	}

	secret := env("BRIDGE_INVITE_SECRET", "")
	switch {
	case secret != "" && len(secret) < domain.MinInviteSecretLength:
		fail("BRIDGE_INVITE_SECRET must have at least %d characters", domain.MinInviteSecretLength)
	case secret != "":
		cfg.InviteSecret = []byte(secret)
	case cfg.DevMode:
		cfg.InviteSecret = []byte(random(32))
	default:
		fail("BRIDGE_INVITE_SECRET is required (>= %d characters) unless BRIDGE_DEV_MODE=true", domain.MinInviteSecretLength)
	}
	switch {
	case cfg.AdminToken != "" && len(cfg.AdminToken) < MinAdminTokenLength && !cfg.DevMode:
		fail("BRIDGE_ADMIN_TOKEN must have at least %d characters (try: openssl rand -base64 32)", MinAdminTokenLength)
	case cfg.AdminToken == "" && cfg.DevMode:
		cfg.AdminToken, cfg.GeneratedAdminToken = "dev", true
	case cfg.AdminToken == "":
		fail("BRIDGE_ADMIN_TOKEN is required unless BRIDGE_DEV_MODE=true")
	}
	if cfg.AdminTokenNext != "" {
		switch {
		case len(cfg.AdminTokenNext) < MinAdminTokenLength && !cfg.DevMode:
			fail("BRIDGE_ADMIN_TOKEN_NEXT must have at least %d characters (try: openssl rand -base64 32)", MinAdminTokenLength)
		case cfg.AdminTokenNext == cfg.AdminToken:
			fail("BRIDGE_ADMIN_TOKEN_NEXT must differ from BRIDGE_ADMIN_TOKEN (a rotation that changes nothing)")
		}
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return cfg, nil
}

func parseBool(v string, fail func(string, ...any), key string) bool {
	b, err := strconv.ParseBool(v)
	if err != nil {
		fail("%s must be true|false", key)
	}
	return b
}

func parseInt(v string, fail func(string, ...any), key string, minimum, maximum int) int {
	n, err := strconv.Atoi(v)
	if err != nil || n < minimum || n > maximum {
		fail("%s must be an integer in [%d, %d]", key, minimum, maximum)
	}
	return n
}

func parseDuration(v string, fail func(string, ...any), key string) time.Duration {
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		fail("%s must be a positive duration (e.g. 24h)", key)
	}
	return d
}

func splitList(v string) []string {
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
