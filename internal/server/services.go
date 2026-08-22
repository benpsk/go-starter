package server

import "github.com/benpsk/go-starter/internal/appsvc"

// Services aggregates domain services consumed by HTTP handlers.
//
// The concrete definition lives in internal/appsvc so that the transport
// packages (internal/web, internal/api) can receive it without importing
// internal/server, which would create an import cycle. The alias keeps
// server.Services usable from cmd/app.
type Services = appsvc.Services
