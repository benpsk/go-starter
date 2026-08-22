package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLocalServiceRequiresRoot(t *testing.T) {
	if _, err := NewLocalService(LocalConfig{}); err == nil {
		t.Fatalf("expected a validation error for a missing root dir")
	}
}

func TestLocalServiceRoundTrip(t *testing.T) {
	root := t.TempDir()
	svc, err := NewLocalService(LocalConfig{RootDir: root, PublicBaseURL: "https://app.example.com/media"})
	if err != nil {
		t.Fatalf("NewLocalService failed: %v", err)
	}
	ctx := context.Background()

	stored, err := svc.Put(ctx, PutInput{
		Key:        "/folder/file.txt",
		Body:       []byte("hello-local"),
		Visibility: VisibilityPublic,
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if stored.Key != "folder/file.txt" {
		t.Fatalf("unexpected stored key: %q", stored.Key)
	}
	if stored.PublicURL != "https://app.example.com/media/folder/file.txt" {
		t.Fatalf("unexpected public url: %q", stored.PublicURL)
	}

	written, err := os.ReadFile(filepath.Join(root, "folder", "file.txt"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(written) != "hello-local" {
		t.Fatalf("unexpected written body: %q", string(written))
	}

	opened, err := svc.Open(ctx, "folder/file.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer opened.Body.Close()
	body, err := io.ReadAll(opened.Body)
	if err != nil {
		t.Fatalf("read opened body: %v", err)
	}
	if string(body) != "hello-local" {
		t.Fatalf("unexpected opened body: %q", string(body))
	}
	if !strings.HasPrefix(opened.ContentType, "text/plain") {
		t.Fatalf("unexpected content type: %q", opened.ContentType)
	}
	if opened.Size != int64(len("hello-local")) {
		t.Fatalf("unexpected size: %d", opened.Size)
	}

	if err := svc.Delete(ctx, "folder/file.txt"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "folder", "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected the file to be gone, got %v", err)
	}
}

func TestLocalServicePrivateUploadHasNoPublicURL(t *testing.T) {
	svc, err := NewLocalService(LocalConfig{RootDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewLocalService failed: %v", err)
	}

	stored, err := svc.Put(context.Background(), PutInput{Key: "a.txt", Body: []byte("x")})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if stored.PublicURL != "" {
		t.Fatalf("a private object must not get a public url, got %q", stored.PublicURL)
	}
}

func TestLocalServicePublicUploadNeedsBaseURL(t *testing.T) {
	svc, err := NewLocalService(LocalConfig{RootDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewLocalService failed: %v", err)
	}

	if _, err := svc.Put(context.Background(), PutInput{
		Key:        "a.txt",
		Body:       []byte("x"),
		Visibility: VisibilityPublic,
	}); err == nil {
		t.Fatalf("expected a public upload to require a base url")
	}
}

func TestLocalServiceRejectsKeysEscapingTheRoot(t *testing.T) {
	root := t.TempDir()
	svc, err := NewLocalService(LocalConfig{RootDir: root})
	if err != nil {
		t.Fatalf("NewLocalService failed: %v", err)
	}
	ctx := context.Background()

	for _, key := range []string{"../escaped.txt", "a/../../escaped.txt", ""} {
		if _, err := svc.Put(ctx, PutInput{Key: key, Body: []byte("x")}); err == nil {
			t.Fatalf("expected Put(%q) to be rejected", key)
		}
		if err := svc.Delete(ctx, key); err == nil {
			t.Fatalf("expected Delete(%q) to be rejected", key)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("a file escaped the storage root")
	}
}

func TestLocalServiceOpenMissingKey(t *testing.T) {
	svc, err := NewLocalService(LocalConfig{RootDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewLocalService failed: %v", err)
	}

	if _, err := svc.Open(context.Background(), "nope.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLocalServiceDeleteMissingKeyIsNotAnError(t *testing.T) {
	svc, err := NewLocalService(LocalConfig{RootDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewLocalService failed: %v", err)
	}

	if err := svc.Delete(context.Background(), "nope.txt"); err != nil {
		t.Fatalf("deleting a missing object should succeed, got %v", err)
	}
}

func TestLocalServicePublicURL(t *testing.T) {
	svc, err := NewLocalService(LocalConfig{RootDir: t.TempDir(), PublicBaseURL: "https://app.example.com/media"})
	if err != nil {
		t.Fatalf("NewLocalService failed: %v", err)
	}

	if got := svc.PublicURL("/a/b.png"); got != "https://app.example.com/media/a/b.png" {
		t.Fatalf("unexpected public url: %q", got)
	}
	if got := svc.PublicURL("../secret"); got != "" {
		t.Fatalf("an invalid key must not produce a url, got %q", got)
	}
}
