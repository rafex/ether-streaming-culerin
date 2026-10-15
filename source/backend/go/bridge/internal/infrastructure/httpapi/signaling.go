package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/application"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

const (
	maxMessageBytes = 256 << 10
	// idleTimeout closes sockets that send nothing (the webrelay pings
	// every 5 s and gives up itself after 3 missed pongs).
	idleTimeout  = 30 * time.Second
	writeTimeout = 10 * time.Second
	sendQueue    = 256
)

// signal upgrades to the signaling v2 WebSocket and runs a Session.
func (s *Server) signal(w http.ResponseWriter, r *http.Request) {
	client := clientAddr(r, s.opts.TrustedProxyHops)
	if !s.signalRate.allow(client) {
		w.Header().Set("Retry-After", strconv.Itoa(int(s.signalRate.retryAfter(client).Seconds())+1))
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	release, ok := s.signalConns.acquire(client)
	if !ok {
		http.Error(w, "too many open connections", http.StatusTooManyRequests)
		return
	}
	defer release()
	if !offersSubprotocol(r, domain.SignalingSubprotocol) {
		http.Error(w, "subprotocol "+domain.SignalingSubprotocol+" required", http.StatusBadRequest)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:   []string{domain.SignalingSubprotocol},
		OriginPatterns: s.opts.AllowedOrigins,
	})
	if err != nil {
		s.log.Debug("websocket upgrade rejected", "err", err, "origin", r.Header.Get("Origin"))
		return
	}
	c.SetReadLimit(maxMessageBytes)

	ctx, cancel := context.WithCancel(context.Background())
	conn := &wsConn{ws: c, out: make(chan outgoing, sendQueue), cancel: cancel}
	log := s.log.With("remote", r.RemoteAddr, "client", client)
	session := application.NewSession(s.hub, conn, log)
	go conn.writeLoop(ctx)

	budget := newMessageBudget(s.clock(), s.messageBurst)
	// Until a valid invitation is shown the socket is anonymous: close it with
	// a reason instead of letting it idle for the whole idleTimeout. (This is a
	// timer, not a read deadline: an expired read context tears the connection
	// down without a close frame.)
	var admitted atomic.Bool
	anonymous := time.AfterFunc(s.admitDeadline, func() {
		if !admitted.Load() {
			conn.Close(4408, "invitation required")
		}
	})
	defer anonymous.Stop()
	for {
		readCtx, done := context.WithTimeout(ctx, idleTimeout)
		kind, data, err := c.Read(readCtx)
		done()
		if err != nil {
			break
		}
		if kind != websocket.MessageText {
			continue
		}
		if !budget.allow(s.clock()) {
			log.Warn("signaling rate limit exceeded; closing")
			conn.Close(int(websocket.StatusPolicyViolation), "rate limit")
			break
		}
		session.Handle(data)
		if session.Admitted() {
			admitted.Store(true)
		}
	}
	session.Closed()
	conn.Close(int(websocket.StatusNormalClosure), "")
}

func offersSubprotocol(r *http.Request, want string) bool {
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(header, ",") {
			if strings.TrimSpace(p) == want {
				return true
			}
		}
	}
	return false
}

type outgoing struct {
	data   []byte
	close  bool
	code   websocket.StatusCode
	reason string
}

// wsConn implements ports.Connection with a bounded queue drained by one
// writer goroutine, so the hub never blocks on a slow client.
type wsConn struct {
	ws     *websocket.Conn
	out    chan outgoing
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
}

var _ ports.Connection = (*wsConn)(nil)

func (c *wsConn) Send(msg domain.ServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	c.enqueue(outgoing{data: data})
}

func (c *wsConn) Close(code int, reason string) {
	c.enqueue(outgoing{close: true, code: websocket.StatusCode(code), reason: reason})
}

func (c *wsConn) enqueue(item outgoing) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if item.close {
		c.closed = true
	}
	select {
	case c.out <- item:
	default:
		// Queue full: the client is not reading. Drop it; it will resume.
		c.closed = true
		c.cancel()
		go c.ws.Close(websocket.StatusPolicyViolation, "slow consumer")
	}
}

func (c *wsConn) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-c.out:
			if item.close {
				_ = c.ws.Close(item.code, item.reason)
				c.cancel()
				return
			}
			wctx, done := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Write(wctx, websocket.MessageText, item.data)
			done()
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}
