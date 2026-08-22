package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

// LocalConfig captures filesystem-backed storage settings.
type LocalConfig struct {
	RootDir       string
	PublicBaseURL string
}

type localService struct {
	rootDir       string
	publicBaseURL string
}

// NewLocalService constructs a filesystem-backed storage service.
func NewLocalService(cfg LocalConfig) (Service, error) {
	rootDir := strings.TrimSpace(cfg.RootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("local storage root dir is required")
	}
	// The root is created on first write rather than here. Construction stays
	// free of side effects, so a console command that builds the services but
	// never stores a file does not leave a directory behind.
	return &localService{
		rootDir:       rootDir,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
	}, nil
}

// Put writes the object under the storage root.
func (s *localService) Put(ctx context.Context, input PutInput) (StoredObject, error) {
	key, fullPath, err := s.resolve(input.Key)
	if err != nil {
		return StoredObject{}, err
	}
	if err := ctx.Err(); err != nil {
		return StoredObject{}, err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return StoredObject{}, fmt.Errorf("create storage directory: %w", err)
	}
	if err := os.WriteFile(fullPath, input.Body, 0o644); err != nil {
		return StoredObject{}, fmt.Errorf("write storage file: %w", err)
	}

	stored := StoredObject{Key: key}
	if input.Visibility == VisibilityPublic {
		if s.publicBaseURL == "" {
			return StoredObject{}, fmt.Errorf("public base url is required for public uploads")
		}
		stored.PublicURL = joinPublicURL(s.publicBaseURL, key)
	}
	return stored, nil
}

// Open returns a readable handle plus the content type inferred from the name.
func (s *localService) Open(ctx context.Context, key string) (OpenedFile, error) {
	_, fullPath, err := s.resolve(key)
	if err != nil {
		return OpenedFile{}, err
	}
	if err := ctx.Err(); err != nil {
		return OpenedFile{}, err
	}
	file, err := os.Open(fullPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return OpenedFile{}, ErrNotFound
		}
		return OpenedFile{}, fmt.Errorf("open storage file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return OpenedFile{}, fmt.Errorf("stat storage file: %w", err)
	}
	contentType := mime.TypeByExtension(filepath.Ext(fullPath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return OpenedFile{Body: file, ContentType: contentType, Size: info.Size()}, nil
}

// Delete removes the object, treating a missing key as success.
func (s *localService) Delete(_ context.Context, key string) error {
	_, fullPath, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(fullPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete storage file: %w", err)
	}
	return nil
}

// PublicURL builds the browser-reachable URL for a key.
func (s *localService) PublicURL(key string) string {
	clean, err := normaliseKey(key)
	if err != nil {
		return ""
	}
	return joinPublicURL(s.publicBaseURL, clean)
}

func (s *localService) resolve(key string) (string, string, error) {
	clean, err := normaliseKey(key)
	if err != nil {
		return "", "", err
	}
	return clean, filepath.Join(s.rootDir, filepath.FromSlash(clean)), nil
}
