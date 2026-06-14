package auth

import (
	"encoding/json"
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
	DefaultRateLimitRequests = 10
	DefaultRateLimitWindow   = time.Minute
)

type RateLimitBucket struct {
	windowStart time.Time
	count       int
	lastSeenAt  time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]RateLimitBucket
	limit   int
	window  time.Duration
	now     func() time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = DefaultRateLimitRequests
	}
	if window <= 0 {
		window = DefaultRateLimitWindow
	}
	return &RateLimiter{
		buckets: make(map[string]RateLimitBucket),
		limit:   limit,
		window:  window,
		now:     time.Now,
	}
}

func (l *RateLimiter) SetNowForTest(now func() time.Time) {
	if now != nil {
		l.now = now
	}
}

func (l *RateLimiter) LimitByIP(scope string) func(http.Handler) http.Handler {
	scope = strings.TrimSpace(scope)
	if l == nil || scope == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := scope + ":" + NormalizedClientIP(r)
			if allowed, retryAfter := l.allow(key); !allowed {
				if retryAfter > 0 {
					seconds := int(math.Ceil(retryAfter.Seconds()))
					if seconds < 1 {
						seconds = 1
					}
					w.Header().Set("Retry-After", strconv.Itoa(seconds))
				}
				if r != nil && r.URL != nil && strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": "rate limit exceeded"})
					return
				}
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (l *RateLimiter) allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanupLocked(now)

	bucket := l.buckets[key]
	if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= l.window {
		bucket = RateLimitBucket{
			windowStart: now,
			count:       1,
			lastSeenAt:  now,
		}
		l.buckets[key] = bucket
		return true, 0
	}

	bucket.lastSeenAt = now
	if bucket.count >= l.limit {
		l.buckets[key] = bucket
		retryAfter := l.window - now.Sub(bucket.windowStart)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, retryAfter
	}

	bucket.count++
	l.buckets[key] = bucket
	return true, 0
}

func (l *RateLimiter) cleanupLocked(now time.Time) {
	if len(l.buckets) == 0 {
		return
	}
	staleAfter := l.window * 2
	if staleAfter <= 0 {
		staleAfter = 2 * time.Minute
	}
	for key, bucket := range l.buckets {
		if now.Sub(bucket.lastSeenAt) >= staleAfter {
			delete(l.buckets, key)
		}
	}
}

func NormalizedClientIP(r *http.Request) string {
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
