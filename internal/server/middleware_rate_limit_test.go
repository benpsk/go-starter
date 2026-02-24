package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthRateLimiterMiddlewareBlocksAfterLimitAndResets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 24, 12, 0, 0, 0, time.UTC)
	limiter := newAuthRateLimiter(2, time.Minute)
	limiter.now = func() time.Time { return now }

	hitCount := 0
	handler := limiter.limitByIP("api_auth_refresh")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		w.WriteHeader(http.StatusNoContent)
	}))

	req := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
		r.RemoteAddr = "203.0.113.5:54321"
		handler.ServeHTTP(rec, r)
		return rec
	}

	if got := req().Code; got != http.StatusNoContent {
		t.Fatalf("first request status = %d, want %d", got, http.StatusNoContent)
	}
	if got := req().Code; got != http.StatusNoContent {
		t.Fatalf("second request status = %d, want %d", got, http.StatusNoContent)
	}

	third := req()
	if third.Code != http.StatusTooManyRequests {
		t.Fatalf("third request status = %d, want %d", third.Code, http.StatusTooManyRequests)
	}
	if third.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header on throttled response")
	}
	if got := third.Body.String(); got == "" {
		t.Fatalf("expected api throttled response body")
	}
	if hitCount != 2 {
		t.Fatalf("handler hit count = %d, want 2", hitCount)
	}

	now = now.Add(61 * time.Second)
	if got := req().Code; got != http.StatusNoContent {
		t.Fatalf("request after window reset status = %d, want %d", got, http.StatusNoContent)
	}
}

func TestAuthRateLimiterMiddlewareKeysByClientIP(t *testing.T) {
	t.Parallel()

	limiter := newAuthRateLimiter(1, time.Minute)
	handler := limiter.limitByIP("web_oauth_start")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	makeReq := func(remoteAddr string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/auth/login/google", nil)
		r.RemoteAddr = remoteAddr
		handler.ServeHTTP(rec, r)
		return rec
	}

	if got := makeReq("198.51.100.10:1000").Code; got != http.StatusNoContent {
		t.Fatalf("first ip status = %d, want %d", got, http.StatusNoContent)
	}
	if got := makeReq("198.51.100.10:1001").Code; got != http.StatusTooManyRequests {
		t.Fatalf("same ip second request status = %d, want %d", got, http.StatusTooManyRequests)
	}
	if got := makeReq("198.51.100.11:1002").Code; got != http.StatusNoContent {
		t.Fatalf("different ip status = %d, want %d", got, http.StatusNoContent)
	}
}

func TestNormalizedClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{name: "ipv4 hostport", remoteAddr: "203.0.113.9:8080", want: "203.0.113.9"},
		{name: "ipv6 hostport", remoteAddr: "[2001:db8::1]:8080", want: "2001:db8::1"},
		{name: "ipv4 no port", remoteAddr: "203.0.113.10", want: "203.0.113.10"},
		{name: "empty", remoteAddr: "", want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if got := normalizedClientIP(r); got != tt.want {
				t.Fatalf("normalizedClientIP(%q) = %q, want %q", tt.remoteAddr, got, tt.want)
			}
		})
	}
}
