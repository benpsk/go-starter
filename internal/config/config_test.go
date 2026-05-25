package config

import (
	"strings"
	"testing"
)

func TestLoadStorageDefaults(t *testing.T) {
	setBaseEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Storage.Driver != "local" {
		t.Errorf("Driver: got %q, want local", cfg.Storage.Driver)
	}
	if cfg.Storage.LocalDir != "media" {
		t.Errorf("LocalDir: got %q, want media", cfg.Storage.LocalDir)
	}
	if cfg.Storage.LocalPublicPath != "/media" {
		t.Errorf("LocalPublicPath: got %q, want /media", cfg.Storage.LocalPublicPath)
	}
	if cfg.R2.Region != "auto" {
		t.Errorf("R2.Region default: got %q, want auto", cfg.R2.Region)
	}
}

func TestLoadStorageDriverInvalid(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("STORAGE_DRIVER", "s3-glacier")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid STORAGE_DRIVER")
	}
	if !strings.Contains(err.Error(), "STORAGE_DRIVER") {
		t.Errorf("error should mention STORAGE_DRIVER: %v", err)
	}
}

func TestLoadStorageR2MissingCreds(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("STORAGE_DRIVER", "r2")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when STORAGE_DRIVER=r2 and creds are missing")
	}
	for _, want := range []string{"R2_ENDPOINT", "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY", "R2_BUCKET", "R2_PUBLIC_BASE_URL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}

func TestLoadStorageR2WithAllCreds(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("STORAGE_DRIVER", "r2")
	t.Setenv("R2_ENDPOINT", "https://accountid.r2.cloudflarestorage.com")
	t.Setenv("R2_ACCESS_KEY_ID", "ak")
	t.Setenv("R2_SECRET_ACCESS_KEY", "sk")
	t.Setenv("R2_BUCKET", "bucket")
	t.Setenv("R2_PUBLIC_BASE_URL", "https://cdn.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Storage.Driver != "r2" {
		t.Errorf("Driver: got %q, want r2", cfg.Storage.Driver)
	}
	if cfg.R2.Bucket != "bucket" {
		t.Errorf("R2.Bucket: got %q, want bucket", cfg.R2.Bucket)
	}
}

func TestLoadStorageLocalPublicPathNormalized(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("LOCAL_STORAGE_PUBLIC_PATH", "uploads")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Storage.LocalPublicPath != "/uploads" {
		t.Errorf("LocalPublicPath: got %q, want /uploads", cfg.Storage.LocalPublicPath)
	}
}

// setBaseEnv installs the minimum env vars required for Load() to succeed,
// and neutralises storage/r2 env vars that may leak in from the host.
func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test")
	t.Setenv("STORAGE_DRIVER", "")
	t.Setenv("LOCAL_STORAGE_DIR", "")
	t.Setenv("LOCAL_STORAGE_PUBLIC_PATH", "")
	t.Setenv("R2_ENDPOINT", "")
	t.Setenv("R2_REGION", "")
	t.Setenv("R2_ACCESS_KEY_ID", "")
	t.Setenv("R2_SECRET_ACCESS_KEY", "")
	t.Setenv("R2_BUCKET", "")
	t.Setenv("R2_PUBLIC_BASE_URL", "")
}
