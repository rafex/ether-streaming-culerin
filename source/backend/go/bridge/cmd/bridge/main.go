// Command bridge is the Ether streaming bridge: it hosts the webrelay's
// video call (signaling v2 + Pion SFU), composes the room with FFmpeg and
// publishes it to RTMP(S) destinations managed from the host API/console.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/application"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/infrastructure/clock"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/infrastructure/config"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/infrastructure/ffmpeg"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/infrastructure/httpapi"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/infrastructure/sfu"
)

// version is set at build time (-ldflags "-X main.version=…").
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		fmt.Println(version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	if cfg.DevMode {
		log.Warn("DEVELOPMENT MODE: rooms are created on demand and any invitation token is accepted; never expose this bridge")
	}
	if cfg.GeneratedAdminToken {
		log.Warn("BRIDGE_ADMIN_TOKEN not set; using the development token", "token", cfg.AdminToken)
	}

	signer, err := domain.NewInviteSigner(cfg.InviteSecret)
	if err != nil {
		return err
	}
	media, err := sfu.New(sfu.Config{UDPPort: cfg.ICEUDPPort, PublicIPs: cfg.ICEPublicIPs, IncludeLoopback: cfg.ICELoopback}, log)
	if err != nil {
		return err
	}
	defer media.Close()
	compositor, err := ffmpeg.New(ffmpeg.Config{
		Binary: cfg.FFmpegPath, FontFile: cfg.FontFile, WorkDir: cfg.WorkDir,
		RTPPortMin: cfg.RTPPortMin, RTPPortMax: cfg.RTPPortMax, RecordDir: cfg.RecordDir,
	}, media, log)
	if err != nil {
		return err
	}
	defer compositor.Close()

	sys := clock.System{}
	hub := application.NewHub(application.Config{
		DevMode: cfg.DevMode, Profile: cfg.Profile, IceServers: cfg.IceServers,
		InviteTTL: cfg.InviteTTL, WebRelayURL: cfg.WebRelayURL,
		MaxRooms: cfg.MaxRooms, MaxDestinations: cfg.MaxDestinations, AllowPrivateDestinations: cfg.AllowPrivateDestinations,
	}, sys, signer, media, log)
	broadcaster := application.NewBroadcaster(hub, compositor, media, sys, application.BroadcastConfig{Output: cfg.Output}, log)
	defer broadcaster.StopAll()

	server := &http.Server{
		Addr: cfg.Addr,
		Handler: httpapi.New(hub, broadcaster, httpapi.Options{
			AdminToken: cfg.AdminToken, AllowedOrigins: cfg.AllowedOrigins, DevMode: cfg.DevMode,
			WebRelayURL: cfg.WebRelayURL, Output: cfg.Output, TrustedProxyHops: cfg.TrustedProxyHops,
		}, log),
		ReadHeaderTimeout: 10 * time.Second,
		// No Read/WriteTimeout: they would cut the signaling WebSockets. Idle
		// keep-alive connections and oversized headers are bounded instead.
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	log.Info("bridge listening", "version", version, "addr", cfg.Addr,
		"signaling", "ws://"+displayAddr(cfg.Addr)+"/signal", "console", "http://"+displayAddr(cfg.Addr)+"/console/")

	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}

func displayAddr(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return "localhost" + addr
	}
	return addr
}
