package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerTokenFromRequest(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"bearer", "Bearer abc123", "abc123"},
		{"lowercase scheme", "bearer abc123", "abc123"},
		{"basic is not bearer", "Basic abc123", ""},
		{"empty", "", ""},
		{"scheme only", "Bearer", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			if got := bearerTokenFromRequest(r); got != tc.want {
				t.Fatalf("bearerTokenFromRequest() = %q, want %q", got, tc.want)
			}
		})
	}
	if got := bearerTokenFromRequest(nil); got != "" {
		t.Fatalf("expected empty for a nil request, got %q", got)
	}
}
