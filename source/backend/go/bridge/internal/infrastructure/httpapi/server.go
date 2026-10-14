// Package httpapi exposes the bridge over HTTP: the webrelay's signaling
// WebSocket (/signal), the host REST API (/api/v1) and the host console
// (/console/).
package httpapi

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/application"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

//go:embed console
var consoleFiles embed.FS

// Options configures the HTTP surface.
type Options struct {
	AdminToken     string
	AllowedOrigins []string
	DevMode        bool
	WebRelayURL    string
	Output         domain.OutputSettings
}

// Server wires the handlers.
type Server struct {
	hub         *application.Hub
	broadcaster *application.Broadcaster
	opts        Options
	log         *slog.Logger
}

// New builds the HTTP handler.
func New(hub *application.Hub, broadcaster *application.Broadcaster, opts Options, log *slog.Logger) http.Handler {
	s := &Server{hub: hub, broadcaster: broadcaster, opts: opts, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /signal", s.signal)

	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/config", s.getConfig)
	api.HandleFunc("GET /api/v1/rooms", s.listRooms)
	api.HandleFunc("POST /api/v1/rooms", s.createRoom)
	api.HandleFunc("GET /api/v1/rooms/{room}", s.getRoom)
	api.HandleFunc("PATCH /api/v1/rooms/{room}", s.updateRoom)
	api.HandleFunc("DELETE /api/v1/rooms/{room}", s.closeRoom)
	api.HandleFunc("POST /api/v1/rooms/{room}/invites", s.invite)
	api.HandleFunc("POST /api/v1/rooms/{room}/destinations", s.addDestination)
	api.HandleFunc("PATCH /api/v1/rooms/{room}/destinations/{dest}", s.updateDestination)
	api.HandleFunc("DELETE /api/v1/rooms/{room}/destinations/{dest}", s.removeDestination)
	api.HandleFunc("POST /api/v1/rooms/{room}/broadcast/start", s.startBroadcast)
	api.HandleFunc("POST /api/v1/rooms/{room}/broadcast/stop", s.stopBroadcast)
	api.HandleFunc("GET /api/v1/rooms/{room}/layout", s.layout)
	api.HandleFunc("PATCH /api/v1/rooms/{room}/participants/{participant}", s.updateParticipant)
	api.HandleFunc("DELETE /api/v1/rooms/{room}/participants/{participant}", s.kick)
	mux.Handle("/api/", s.authorize(api))

	console, _ := fs.Sub(consoleFiles, "console")
	mux.Handle("GET /console/", consoleHeaders(http.StripPrefix("/console/", http.FileServerFS(console))))
	mux.Handle("GET /{$}", http.RedirectHandler("/console/", http.StatusFound))
	return logRequests(mux, log)
}

// authorize checks the host's bearer token in constant time.
func (s *Server) authorize(next http.Handler) http.Handler {
	expected := []byte("Bearer " + s.opts.AdminToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, expected) != 1 {
			writeJSON(w, http.StatusUnauthorized, apiError{Code: "unauthorized", Message: "missing or invalid admin token"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		next.ServeHTTP(w, r)
	})
}

func consoleHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https: data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController (and the WebSocket upgrade) reach the
// underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func logRequests(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/console/") {
			return
		}
		log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start))
	})
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, err error) {
	var de *domain.Error
	if !errors.As(err, &de) {
		writeJSON(w, http.StatusInternalServerError, apiError{Code: "internal", Message: err.Error()})
		return
	}
	status := http.StatusInternalServerError
	switch de.Code {
	case domain.CodeBadRequest:
		status = http.StatusBadRequest
	case domain.CodeNotFound, domain.CodeRoomNotFound, domain.CodeNotJoined:
		status = http.StatusNotFound
	case domain.CodeConflict, domain.CodeNoDestinations, domain.CodeRoomClosed:
		status = http.StatusConflict
	case domain.CodeMedia:
		status = http.StatusBadGateway
	}
	writeJSON(w, status, apiError{Code: string(de.Code), Message: de.Message})
}

func readJSON(r *http.Request, into any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return domain.Errorf(domain.CodeBadRequest, "invalid JSON body: %v", err)
	}
	return nil
}
