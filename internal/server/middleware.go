package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

const csrfCookieName = "csrf_token"

const (
	defaultAuthRateLimitRequests = 10
	defaultAuthRateLimitWindow   = time.Minute
)

type authRateLimitBucket struct {
	windowStart time.Time
	count       int
	lastSeenAt  time.Time
}

type authRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]authRateLimitBucket
	limit   int
	window  time.Duration
	now     func() time.Time
}

func newAuthRateLimiter(limit int, window time.Duration) *authRateLimiter {
	if limit <= 0 {
		limit = defaultAuthRateLimitRequests
	}
	if window <= 0 {
		window = defaultAuthRateLimitWindow
	}
	return &authRateLimiter{
		buckets: make(map[string]authRateLimitBucket),
		limit:   limit,
		window:  window,
		now:     time.Now,
	}
}

func (l *authRateLimiter) limitByIP(scope string) func(http.Handler) http.Handler {
	scope = strings.TrimSpace(scope)
	if l == nil || scope == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := scope + ":" + normalizedClientIP(r)
			if allowed, retryAfter := l.allow(key); !allowed {
				if retryAfter > 0 {
					seconds := int(math.Ceil(retryAfter.Seconds()))
					if seconds < 1 {
						seconds = 1
					}
					w.Header().Set("Retry-After", strconv.Itoa(seconds))
				}
				if r != nil && r.URL != nil && strings.HasPrefix(r.URL.Path, "/api/") {
					writeErrorJSON(w, http.StatusTooManyRequests, "rate limit exceeded")
					return
				}
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (l *authRateLimiter) allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanupLocked(now)

	bucket := l.buckets[key]
	if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= l.window {
		bucket = authRateLimitBucket{
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

func (l *authRateLimiter) cleanupLocked(now time.Time) {
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

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func csrfProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		ensureCSRFCookie(w, r)

		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		cookieToken := currentCSRFCookie(r)
		headerToken := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
		formToken := strings.TrimSpace(r.FormValue("csrf_token"))

		candidate := headerToken
		if candidate == "" {
			candidate = formToken
		}
		if !csrfTokensEqual(cookieToken, candidate) {
			http.Error(w, "invalid csrf token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request) string {
	if token := currentCSRFCookie(r); token != "" {
		return token
	}

	token, err := newCSRFToken()
	if err != nil {
		return ""
	}

	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // JS reads this to set htmx header and hidden form fields.
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	return token
}

func currentCSRFCookie(r *http.Request) string {
	c, err := r.Cookie(csrfCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}

func newCSRFToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func csrfTokensEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func normalizedClientIP(r *http.Request) string {
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
