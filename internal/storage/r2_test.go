package storage

import (
	"context"
	"testing"
)

func TestR2ConfigComplete(t *testing.T) {
	full := R2Config{
		Endpoint:        "https://account.r2.cloudflarestorage.com",
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
		Bucket:          "bucket",
	}

	cases := []struct {
		name   string
		mutate func(*R2Config)
		want   bool
	}{
		{"complete", func(*R2Config) {}, true},
		{"no endpoint", func(c *R2Config) { c.Endpoint = "" }, false},
		{"no access key", func(c *R2Config) { c.AccessKeyID = "" }, false},
		{"no secret", func(c *R2Config) { c.SecretAccessKey = "" }, false},
		{"no bucket", func(c *R2Config) { c.Bucket = "" }, false},
		{"blank endpoint", func(c *R2Config) { c.Endpoint = "   " }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := full
			tc.mutate(&cfg)
			if got := cfg.Complete(); got != tc.want {
				t.Fatalf("Complete() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewR2ServiceRejectsIncompleteConfig(t *testing.T) {
	if _, err := NewR2Service(context.Background(), R2Config{Bucket: "bucket"}); err == nil {
		t.Fatalf("expected an incomplete config to be rejected")
	}
}

func TestR2PublicURL(t *testing.T) {
	svc, err := NewR2Service(context.Background(), R2Config{
		Endpoint:        "https://account.r2.cloudflarestorage.com",
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
		Bucket:          "bucket",
		PublicBaseURL:   "https://cdn.example.com/",
	})
	if err != nil {
		t.Fatalf("NewR2Service failed: %v", err)
	}

	if got := svc.PublicURL("/a/b.png"); got != "https://cdn.example.com/a/b.png" {
		t.Fatalf("unexpected public url: %q", got)
	}
	if got := svc.PublicURL("../secret"); got != "" {
		t.Fatalf("an invalid key must not produce a url, got %q", got)
	}
}
