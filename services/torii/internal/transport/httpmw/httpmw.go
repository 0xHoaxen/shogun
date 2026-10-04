// Package httpmw holds the HTTP middleware of torii's public listener: a
// per-client rate limit and a request body cap.
package httpmw

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// maxTrackedClients bounds the limiter map; past it, idle entries are dropped.
	maxTrackedClients = 4096
	idleAfter         = 10 * time.Minute
	retryAfterSeconds = 1
)

type client struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter allows each client address a steady rate with a burst.
type RateLimiter struct {
	perSecond rate.Limit
	burst     int
	now       func() time.Time

	mu      sync.Mutex
	clients map[string]*client
}

// NewRateLimiter returns a limiter of perSecond requests per second per
// client, with bursts up to burst.
func NewRateLimiter(perSecond, burst int) *RateLimiter {
	return &RateLimiter{
		perSecond: rate.Limit(perSecond),
		burst:     burst,
		now:       time.Now,
		clients:   map[string]*client{},
	}
}

// Middleware rejects requests over the limit with 429 and a Retry-After.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientKey(r)) {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *RateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	c, ok := l.clients[key]
	if !ok {
		l.dropIdle(now)
		c = &client{limiter: rate.NewLimiter(l.perSecond, l.burst)}
		l.clients[key] = c
	}
	c.lastSeen = now
	return c.limiter.AllowN(now, 1)
}

// dropIdle forgets clients not seen for a while once the map is large.
func (l *RateLimiter) dropIdle(now time.Time) {
	if len(l.clients) < maxTrackedClients {
		return
	}
	for key, c := range l.clients {
		if now.Sub(c.lastSeen) > idleAfter {
			delete(l.clients, key)
		}
	}
}

// clientKey is the remote host. Behind the web app's proxy that is the proxy,
// so the limit is shared by all browsers, which is what a single-owner app wants.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// LimitBody caps request bodies at limit bytes; reading past it fails.
func LimitBody(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
