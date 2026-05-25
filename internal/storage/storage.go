package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/benpsk/go-starter/internal/r2"
)

type Store interface {
	Upload(ctx context.Context, key string, body io.Reader, contentType string) (string, error)
	Delete(ctx context.Context, key string) error
	PublicURL(key string) string
}

type LocalStore struct {
	root       string
	appURL     string
	publicPath string
}

func NewLocal(root, appURL, publicPath string) (*LocalStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("local storage root is required")
	}
	if strings.TrimSpace(publicPath) == "" {
		publicPath = "/media"
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create local storage dir: %w", err)
	}
	return &LocalStore{root: root, appURL: appURL, publicPath: publicPath}, nil
}

func (s *LocalStore) Upload(ctx context.Context, key string, body io.Reader, contentType string) (string, error) {
	_ = ctx
	_ = contentType
	dstPath, err := s.pathForKey(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return "", fmt.Errorf("create local storage subdir: %w", err)
	}
	dst, err := os.Create(dstPath)
	if err != nil {
		return "", fmt.Errorf("create local storage file: %w", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, body); err != nil {
		return "", fmt.Errorf("write local storage file: %w", err)
	}
	return s.PublicURL(key), nil
}

func (s *LocalStore) Delete(ctx context.Context, key string) error {
	_ = ctx
	p, err := s.pathForKey(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete local storage file: %w", err)
	}
	return nil
}

func (s *LocalStore) PublicURL(key string) string {
	u, _ := url.Parse(s.appURL)
	u.Path = path.Join(u.Path, s.publicPath, path.Clean("/"+key))
	return u.String()
}

func (s *LocalStore) Dir() string {
	return s.root
}

func (s *LocalStore) pathForKey(key string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(key, "/")))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("invalid storage key")
	}
	return filepath.Join(s.root, clean), nil
}

type R2Store struct {
	client *r2.Client
}

func NewR2(client *r2.Client) *R2Store {
	return &R2Store{client: client}
}

func (s *R2Store) Upload(ctx context.Context, key string, body io.Reader, contentType string) (string, error) {
	return s.client.Upload(ctx, key, body, contentType)
}

func (s *R2Store) Delete(ctx context.Context, key string) error {
	return s.client.Delete(ctx, key)
}

func (s *R2Store) PublicURL(key string) string {
	return s.client.GetPublicURL(key)
}
