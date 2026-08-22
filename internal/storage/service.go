// Package storage provides object storage behind a single interface. Callers
// choose a driver through configuration; they never import a driver directly.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// Visibility selects whether a stored object gets a public URL.
type Visibility string

const (
	// VisibilityPrivate keeps an object reachable only through the app.
	VisibilityPrivate Visibility = "private"
	// VisibilityPublic exposes an object at a stable public URL.
	VisibilityPublic Visibility = "public"
)

// ErrNotFound is returned when a key does not exist in the backing store.
var ErrNotFound = errors.New("storage: object not found")

// PutInput describes an object to write.
type PutInput struct {
	Key         string
	Body        []byte
	ContentType string
	Visibility  Visibility
}

// StoredObject is the result of a successful write.
type StoredObject struct {
	Key       string
	PublicURL string
}

// OpenedFile is a readable handle on a stored object. Callers close Body.
type OpenedFile struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64
}

// Service is the object storage contract every driver implements.
type Service interface {
	Put(ctx context.Context, input PutInput) (StoredObject, error)
	Open(ctx context.Context, key string) (OpenedFile, error)
	Delete(ctx context.Context, key string) error
	PublicURL(key string) string
}

type disabledService struct {
	reason string
}

// NewDisabledService returns a store that refuses every operation with the
// supplied reason. Startup uses it so a missing storage configuration degrades
// one feature instead of preventing the application from booting.
func NewDisabledService(reason string) Service {
	if strings.TrimSpace(reason) == "" {
		reason = "object storage is not configured"
	}
	return disabledService{reason: reason}
}

func (s disabledService) Put(context.Context, PutInput) (StoredObject, error) {
	return StoredObject{}, errors.New(s.reason)
}

func (s disabledService) Open(context.Context, string) (OpenedFile, error) {
	return OpenedFile{}, errors.New(s.reason)
}

func (s disabledService) Delete(context.Context, string) error {
	return errors.New(s.reason)
}

func (s disabledService) PublicURL(string) string {
	return ""
}

// normaliseKey returns a clean slash-separated key relative to the store root.
//
// A key containing a ".." segment is rejected rather than cleaned away: path
// cleaning would rewrite "../secret.txt" to "secret.txt", so the caller would
// silently read or write a different object than it asked for.
func normaliseKey(key string) (string, error) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "", fmt.Errorf("storage key is required")
	}

	slashed := strings.ReplaceAll(trimmed, "\\", "/")
	for _, segment := range strings.Split(slashed, "/") {
		if segment == ".." {
			return "", fmt.Errorf("invalid storage key %q", key)
		}
	}

	clean := strings.TrimPrefix(path.Clean("/"+slashed), "/")
	if clean == "" || clean == "." {
		return "", fmt.Errorf("invalid storage key %q", key)
	}
	return clean, nil
}

func joinPublicURL(baseURL, key string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	return base + "/" + key
}
