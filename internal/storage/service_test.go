package storage

import (
	"context"
	"strings"
	"testing"
)

func TestNormaliseKey(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain", "file.txt", "file.txt", false},
		{"nested", "folder/file.txt", "folder/file.txt", false},
		{"leading slash", "/folder/file.txt", "folder/file.txt", false},
		{"backslashes", `folder\file.txt`, "folder/file.txt", false},
		{"redundant segments", "folder/./file.txt", "folder/file.txt", false},
		{"contained traversal", "folder/../folder/file.txt", "", true},
		{"empty", "", "", true},
		{"blank", "   ", "", true},
		{"escaping traversal", "../secret.txt", "", true},
		{"escaping traversal nested", "folder/../../secret.txt", "", true},
		{"root", "/", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normaliseKey(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("normaliseKey(%q) error = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("normaliseKey(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDisabledServiceRefusesEverythingWithItsReason(t *testing.T) {
	svc := NewDisabledService("r2 is not configured")
	ctx := context.Background()

	if _, err := svc.Put(ctx, PutInput{Key: "a.txt"}); err == nil || !strings.Contains(err.Error(), "r2 is not configured") {
		t.Fatalf("expected the configured reason from Put, got %v", err)
	}
	if _, err := svc.Open(ctx, "a.txt"); err == nil {
		t.Fatalf("expected Open to fail")
	}
	if err := svc.Delete(ctx, "a.txt"); err == nil {
		t.Fatalf("expected Delete to fail")
	}
	if got := svc.PublicURL("a.txt"); got != "" {
		t.Fatalf("expected no public url, got %q", got)
	}
}

func TestDisabledServiceHasADefaultReason(t *testing.T) {
	if _, err := NewDisabledService("  ").Put(context.Background(), PutInput{Key: "a.txt"}); err == nil || err.Error() == "" {
		t.Fatalf("expected a fallback reason, got %v", err)
	}
}

func TestJoinPublicURL(t *testing.T) {
	cases := []struct {
		base, key, want string
	}{
		{"https://cdn.example.com", "a/b.png", "https://cdn.example.com/a/b.png"},
		{"https://cdn.example.com/", "a/b.png", "https://cdn.example.com/a/b.png"},
		{"", "a/b.png", ""},
	}
	for _, tc := range cases {
		if got := joinPublicURL(tc.base, tc.key); got != tc.want {
			t.Fatalf("joinPublicURL(%q, %q) = %q, want %q", tc.base, tc.key, got, tc.want)
		}
	}
}
