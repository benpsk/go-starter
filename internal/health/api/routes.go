package api

import (
	"github.com/benpsk/go-starter/internal/health"
	"github.com/go-chi/chi/v5"
)

// Mount registers process liveness and dependency readiness endpoints.
func Mount(r chi.Router, svc *health.Service) {
	h := NewHealthHandler(svc)
	r.Get("/healthz", h.Live)
	r.Get("/readyz", h.Ready)
}
