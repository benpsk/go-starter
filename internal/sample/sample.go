package sample

import (
	"context"
	"errors"
	"strings"
	"time"
)

type Item struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

type Store interface {
	List(ctx context.Context) ([]Item, error)
	Create(ctx context.Context, name string) (Item, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) List(ctx context.Context) ([]Item, error) {
	return s.store.List(ctx)
}

func (s *Service) Create(ctx context.Context, name string) (Item, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Item{}, errors.New("name is required")
	}
	return s.store.Create(ctx, name)
}
