package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func request(remoteAddr string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	r.RemoteAddr = remoteAddr
	return r
}

func TestAllowWithinWindowThenBlocks(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	limiter := New("login", Config{Limit: 2, Window: time.Minute})
	limiter.SetNowForTest(func() time.Time { return now })

	for attempt := 1; attempt <= 2; attempt++ {
		if allowed, _ := limiter.Allow("k"); !allowed {
			t.Fatalf("attempt %d should be allowed", attempt)
		}
	}

	allowed, retryAfter := limiter.Allow("k")
	if allowed {
		t.Fatalf("the third attempt should be blocked")
	}
	if retryAfter <= 0 || retryAfter > time.Minute {
		t.Fatalf("unexpected retry after: %v", retryAfter)
	}

	now = now.Add(time.Minute)
	if allowed, _ := limiter.Allow("k"); !allowed {
		t.Fatalf("a new window should reset the allowance")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	limiter := New("login", Config{Limit: 1, Window: time.Minute})

	if allowed, _ := limiter.Allow("a"); !allowed {
		t.Fatalf("first key should be allowed")
	}
	if allowed, _ := limiter.Allow("b"); !allowed {
		t.Fatalf("a different key has its own allowance")
	}
	if allowed, _ := limiter.Allow("a"); allowed {
		t.Fatalf("the first key should now be exhausted")
	}
}

func TestMiddlewareCallsOnLimitAndSetsRetryAfter(t *testing.T) {
	var limited int
	middleware := NewFixedWindow("login", Config{Limit: 1, Window: time.Minute}, func(w http.ResponseWriter, _ *http.Request) {
		limited++
		w.WriteHeader(http.StatusTooManyRequests)
	})
	handler := middleware(okHandler())

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, request("203.0.113.5:1111"))
	if first.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", first.Code)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, request("203.0.113.5:2222"))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", second.Code)
	}
	if limited != 1 {
		t.Fatalf("expected onLimit to run once, ran %d times", limited)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatalf("expected a Retry-After header")
	}
}

func TestMiddlewareSkipsLocalhostWhenConfigured(t *testing.T) {
	middleware := NewFixedWindow("login", Config{Limit: 1, Window: time.Minute, SkipLocalhost: true}, nil)
	handler := middleware(okHandler())

	for attempt := 1; attempt <= 5; attempt++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, request("127.0.0.1:5555"))
		if rec.Code != http.StatusOK {
			t.Fatalf("localhost attempt %d should not be limited, got %d", attempt, rec.Code)
		}
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		remoteAddr string
		want       string
	}{
		{"203.0.113.5:1234", "203.0.113.5"},
		{"[2001:db8::1]:1234", "2001:db8::1"},
		{"203.0.113.5", "203.0.113.5"},
		{"", "unknown"},
	}
	for _, tc := range cases {
		if got := ClientIP(request(tc.remoteAddr)); got != tc.want {
			t.Fatalf("ClientIP(%q) = %q, want %q", tc.remoteAddr, got, tc.want)
		}
	}
	if got := ClientIP(nil); got != "unknown" {
		t.Fatalf("ClientIP(nil) = %q, want unknown", got)
	}
}
