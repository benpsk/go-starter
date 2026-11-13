// Package appsvc holds the concrete service aggregate used by application
// construction and HTTP route composition. Feature adapters receive only
// their individual dependencies.
package appsvc

import (
	healthsvc "github.com/benpsk/go-starter/internal/health"
	loginsvc "github.com/benpsk/go-starter/internal/login"
	"github.com/benpsk/go-starter/internal/storage"
	usersvc "github.com/benpsk/go-starter/internal/users"
)

// Services aggregates the services built in internal/app and consumed by handlers.
// Add one field per domain; handlers take only the services they use, never the
// whole aggregate.
type Services struct {
	Health *healthsvc.Service
	Users  *usersvc.Service
	Login  *loginsvc.Service
	// Storage is the configured object store. Domain services that persist
	// files take it as a constructor dependency.
	Storage storage.Service
}
