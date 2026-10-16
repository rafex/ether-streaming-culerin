package httpapi

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Limits of the public surface. They are generous for people (a guest opens
// one socket per tab, ICE trickle sends a burst of messages) and tight for
// scripts: the admin token is 40 random characters, so these exist to stop
// guessing, floods and resource exhaustion rather than to meter normal use.
const (
	// authFailureBurst failed admin-token attempts are allowed per client, then
	// one more every authFailureEvery until the bucket refills.
	authFailureBurst = 10
	authFailureEvery = 6 * time.Second

	// signalConnBurst new WebSockets per client, refilled at signalConnPerSec.
	signalConnBurst  = 20
	signalConnPerSec = 1.0
	// maxSignalConnsPerClient bounds concurrent sockets from one address.
	maxSignalConnsPerClient = 30

	// A socket must present a valid invitation (room.peek) within this long.
	admitDeadline = 10 * time.Second

	// Per-socket message budget: ICE trickle bursts, then a steady rate.
	messageBurst  = 200
	messagePerSec = 100.0
)

// bucket is a token bucket; the zero value is not usable, use newBucket.
type bucket struct {
	tokens float64
	last   time.Time
}

func (b *bucket) refill(now time.Time, burst, perSec float64) {
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = min(burst, b.tokens+elapsed*perSec)
	}
	b.last = now
}

// take consumes one token if there is one.
func (b *bucket) take(now time.Time, burst, perSec float64) bool {
	b.refill(now, burst, perSec)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// messageBudget limits how fast one socket may send signaling messages.
type messageBudget struct {
	b     bucket
	burst float64
}

func newMessageBudget(now time.Time, burst int) *messageBudget {
	return &messageBudget{b: bucket{tokens: float64(burst), last: now}, burst: float64(burst)}
}

func (m *messageBudget) allow(now time.Time) bool {
	return m.b.take(now, m.burst, messagePerSec)
}

// keyedLimiter is a set of token buckets keyed by client address.
type keyedLimiter struct {
	mu      sync.Mutex
	burst   float64
	perSec  float64
	buckets map[string]*bucket
	now     func() time.Time
}

func newKeyedLimiter(burst int, perSec float64, now func() time.Time) *keyedLimiter {
	return &keyedLimiter{burst: float64(burst), perSec: perSec, buckets: map[string]*bucket{}, now: now}
}

// bucketLocked returns the (refilled) bucket of key, pruning idle ones so a
// scan from many addresses cannot grow the map without bound.
func (l *keyedLimiter) bucketLocked(key string) *bucket {
	now := l.now()
	if len(l.buckets) > 4096 {
		for k, b := range l.buckets {
			b.refill(now, l.burst, l.perSec)
			if b.tokens >= l.burst {
				delete(l.buckets, k)
			}
		}
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.refill(now, l.burst, l.perSec)
	return b
}

// allow consumes one token of key.
func (l *keyedLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucketLocked(key)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// exhausted reports whether key has no tokens left, without consuming one.
func (l *keyedLimiter) exhausted(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.bucketLocked(key).tokens < 1
}

// retryAfter is how long until key gets a token back.
func (l *keyedLimiter) retryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	need := 1 - l.bucketLocked(key).tokens
	if need <= 0 {
		return 0
	}
	return time.Duration(need / l.perSec * float64(time.Second))
}

// connCounter bounds concurrent connections per client address.
type connCounter struct {
	mu    sync.Mutex
	max   int
	count map[string]int
}

func newConnCounter(max int) *connCounter { return &connCounter{max: max, count: map[string]int{}} }

// acquire reserves a slot; the returned func releases it.
func (c *connCounter) acquire(key string) (release func(), ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.count[key] >= c.max {
		return nil, false
	}
	c.count[key]++
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.count[key]--; c.count[key] <= 0 {
				delete(c.count, key)
			}
		})
	}, true
}

// clientAddr is the address limits are keyed on. Behind `hops` trusted
// reverse proxies the client is the entry `hops` places from the right of
// X-Forwarded-For (what the outermost proxy appended); anything further left
// is client-controlled and ignored. With hops == 0 the TCP peer is used.
//
// With hops > 0 and no usable entry it returns "": the only address left is a
// proxy's, shared by every client, and charging limits to it would let one
// misbehaving client lock out everyone. Callers skip their limits for "" (and
// the warning in the log points at the misconfiguration).
func clientAddr(r *http.Request, hops int) string {
	if hops <= 0 {
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			peer = r.RemoteAddr
		}
		return peer
	}
	var chain []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				chain = append(chain, part)
			}
		}
	}
	if len(chain) < hops {
		return ""
	}
	if ip := net.ParseIP(chain[len(chain)-hops]); ip != nil {
		return ip.String()
	}
	return ""
}
