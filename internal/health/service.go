// Package health owns the liveness and readiness domain. It reports dependency
// state without knowing how those dependencies are reached.
package health

import (
	"context"
	"time"
)

// Status codes reported by the health service.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
	StatusSkipped  = "skipped"
	StatusDown     = "down"
)

// Status is the readiness payload for the whole application.
type Status struct {
	Status    string          `json:"status"`
	CheckedAt time.Time       `json:"checked_at"`
	Database  DatabaseDetails `json:"database"`
}

// DatabaseDetails reports the state of the primary datastore.
type DatabaseDetails struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Service exposes health reporting.
type Service interface {
	Status(ctx context.Context) (Status, error)
}

// Repository abstracts the dependency checks health reporting needs.
type Repository interface {
	Ping(ctx context.Context) error
}

type service struct {
	repo Repository
}

// NewService constructs a health service using the supplied repository.
func NewService(repo Repository) Service {
	return &service{repo: repo}
}

// Status probes the datastore and reports aggregate readiness. A failed probe
// is a degraded status, not an error: the caller still needs a payload to send.
func (s *service) Status(ctx context.Context) (Status, error) {
	out := Status{
		Status:    StatusOK,
		CheckedAt: time.Now().UTC(),
		Database:  DatabaseDetails{Status: StatusOK},
	}
	if s.repo == nil {
		out.Status = StatusDegraded
		out.Database = DatabaseDetails{Status: StatusSkipped, Message: "database is not configured"}
		return out, nil
	}
	if err := s.repo.Ping(ctx); err != nil {
		out.Status = StatusDegraded
		out.Database = DatabaseDetails{Status: StatusDown, Message: err.Error()}
	}
	return out, nil
}
