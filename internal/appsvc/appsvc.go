// Package appsvc holds the aggregate of domain services the HTTP transports
// consume.
//
// It lives in its own package so internal/web and internal/api can receive the
// aggregate without importing internal/server, which would create an import
// cycle. internal/server re-exports it as server.Services.
package appsvc

import (
	healthsvc "github.com/benpsk/go-starter/internal/health"
	loginsvc "github.com/benpsk/go-starter/internal/login"
	"github.com/benpsk/go-starter/internal/storage"
	usersvc "github.com/benpsk/go-starter/internal/users"
)

// Services aggregates the services built in cmd/app and consumed by handlers.
// Add one field per domain; handlers take only the services they use, never the
// whole aggregate.
type Services struct {
	Health healthsvc.Service
	Users  usersvc.Service
	Login  loginsvc.Service
	// Storage is the configured object store. Domain services that persist
	// files take it as a constructor dependency.
	Storage storage.Service
}
