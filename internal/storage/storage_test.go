package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStoreUploadDeleteAndPublicURL(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocal(root, "http://localhost:8080", "/media")
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}

	publicURL, err := store.Upload(context.Background(), "uploads/1/test.txt", strings.NewReader("hello"), "text/plain")
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if publicURL != "http://localhost:8080/media/uploads/1/test.txt" {
		t.Fatalf("unexpected public url: %q", publicURL)
	}
	path := filepath.Join(root, "uploads", "1", "test.txt")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("unexpected local file content: %q", string(got))
	}

	if err := store.Delete(context.Background(), "uploads/1/test.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected local file deleted, stat err=%v", err)
	}
}

func TestLocalStoreRejectsTraversalKeys(t *testing.T) {
	store, err := NewLocal(t.TempDir(), "http://localhost:8080", "/media")
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	if _, err := store.Upload(context.Background(), "../escape.txt", strings.NewReader("x"), "text/plain"); err == nil {
		t.Fatal("expected traversal key upload to fail")
	}
}
