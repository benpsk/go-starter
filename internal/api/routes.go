package api

import (
	"context"
	"net/http"
	"time"

	"github.com/benpsk/go-starter/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db   *pgxpool.Pool
	auth *auth.Service
}

func NewHandler(db *pgxpool.Pool, authService *auth.Service) Handler {
	return Handler{db: db, auth: authService}
}

func Routes(h Handler, limiter *auth.RateLimiter) chi.Router {
	r := chi.NewRouter()
	r.Route("/auth", func(r chi.Router) {
		r.With(limiter.LimitByIP("api_auth_login")).Post("/login/{provider}", h.login)
		r.With(limiter.LimitByIP("api_auth_refresh")).Post("/refresh", h.refresh)
		r.Post("/logout", h.logout)
		r.With(h.requireAPIAuth).Get("/me", h.me)
	})
	r.Get("/health", h.Health)
	return r
}

func (h Handler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	payload := map[string]any{"status": "ok", "database": "up"}
	status := http.StatusOK

	if h.db != nil {
		if err := h.db.Ping(ctx); err != nil {
			payload["status"] = "degraded"
			payload["database"] = err.Error()
			status = http.StatusServiceUnavailable
		}
	}

	writeJSON(w, status, payload)
}
