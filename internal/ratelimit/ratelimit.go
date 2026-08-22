// Package ratelimit provides a transport-agnostic fixed-window limiter. Each
// transport supplies its own onLimit callback so the API can answer with its
// JSON envelope while browser routes answer with plain text.
package ratelimit

import (
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultLimit is the request allowance used when none is configured.
	DefaultLimit = 10
	// DefaultWindow is the window length used when none is configured.
	DefaultWindow = time.Minute
)

// Config captures the tunable limiter settings.
type Config struct {
	Limit         int
	Window        time.Duration
	SkipLocalhost bool
}

type bucket struct {
	windowStart time.Time
	count       int
	lastSeenAt  time.Time
}

// FixedWindow counts requests per client IP inside a fixed time window.
type FixedWindow struct {
	mu            sync.Mutex
	buckets       map[string]bucket
	scope         string
	limit         int
	window        time.Duration
	skipLocalhost bool
	now           func() time.Time
}

// NewFixedWindow builds limiter middleware for one named scope. Scopes keep
// unrelated endpoints from sharing an allowance.
func NewFixedWindow(scope string, cfg Config, onLimit func(http.ResponseWriter, *http.Request)) func(http.Handler) http.Handler {
	return New(scope, cfg).Middleware(onLimit)
}

// New constructs a limiter without wrapping it in middleware, so tests can
// drive the clock.
func New(scope string, cfg Config) *FixedWindow {
	if cfg.Limit <= 0 {
		cfg.Limit = DefaultLimit
	}
	if cfg.Window <= 0 {
		cfg.Window = DefaultWindow
	}
	return &FixedWindow{
		buckets:       make(map[string]bucket),
		scope:         strings.TrimSpace(scope),
		limit:         cfg.Limit,
		window:        cfg.Window,
		skipLocalhost: cfg.SkipLocalhost,
		now:           time.Now,
	}
}

// SetNowForTest overrides the clock used to age windows.
func (l *FixedWindow) SetNowForTest(now func() time.Time) {
	if now != nil {
		l.now = now
	}
}

// Middleware rejects requests over the allowance, calling onLimit to render the
// response in whatever shape the surrounding transport uses.
func (l *FixedWindow) Middleware(onLimit func(http.ResponseWriter, *http.Request)) func(http.Handler) http.Handler {
	if l == nil || l.scope == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if l.skipLocalhost && isLocalhost(r) {
				next.ServeHTTP(w, r)
				return
			}
			allowed, retryAfter := l.Allow(l.scope + ":" + ClientIP(r))
			if allowed {
				next.ServeHTTP(w, r)
				return
			}
			if retryAfter > 0 {
				seconds := int(math.Ceil(retryAfter.Seconds()))
				if seconds < 1 {
					seconds = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(seconds))
			}
			if onLimit != nil {
				onLimit(w, r)
				return
			}
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		})
	}
}

// Allow reports whether the key is under its allowance, plus how long the
// caller should wait when it is not.
func (l *FixedWindow) Allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanupLocked(now)

	current := l.buckets[key]
	if current.windowStart.IsZero() || now.Sub(current.windowStart) >= l.window {
		l.buckets[key] = bucket{windowStart: now, count: 1, lastSeenAt: now}
		return true, 0
	}

	current.lastSeenAt = now
	if current.count >= l.limit {
		l.buckets[key] = current
		retryAfter := l.window - now.Sub(current.windowStart)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, retryAfter
	}

	current.count++
	l.buckets[key] = current
	return true, 0
}

func (l *FixedWindow) cleanupLocked(now time.Time) {
	if len(l.buckets) == 0 {
		return
	}
	staleAfter := l.window * 2
	if staleAfter <= 0 {
		staleAfter = 2 * time.Minute
	}
	for key, entry := range l.buckets {
		if now.Sub(entry.lastSeenAt) >= staleAfter {
			delete(l.buckets, key)
		}
	}
}

// ClientIP normalises RemoteAddr into a bare IP suitable for use as a key.
func ClientIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	value := strings.TrimSpace(r.RemoteAddr)
	if value == "" {
		return "unknown"
	}
	if addr, err := netip.ParseAddrPort(value); err == nil {
		return addr.Addr().String()
	}
	if host, _, err := net.SplitHostPort(value); err == nil && strings.TrimSpace(host) != "" {
		return strings.TrimSpace(strings.Trim(host, "[]"))
	}
	value = strings.Trim(value, "[]")
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr.String()
	}
	return value
}

func isLocalhost(r *http.Request) bool {
	ip := ClientIP(r)
	if ip == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	return addr.IsLoopback()
}
