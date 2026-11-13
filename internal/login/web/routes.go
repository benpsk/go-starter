package web

import (
	"net/http"

	usersession "github.com/benpsk/go-starter/internal/auth/user"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/login"
	"github.com/benpsk/go-starter/internal/ratelimit"
	transport "github.com/benpsk/go-starter/internal/web"
	"github.com/go-chi/chi/v5"
)

// Mount registers browser sign-in and sign-out routes.
func Mount(r chi.Router, cfg config.Config, sessions *usersession.Manager, svc *login.Service, session transport.SessionMiddleware) {
	h := NewUserLoginHandler(svc, sessions, cfg.Setting())
	limit := ratelimit.NewFixedWindow("web_oauth_start", ratelimit.Config{Limit: cfg.RateLimit.WebOAuth, Window: cfg.RateLimit.Window, SkipLocalhost: !cfg.IsProduction()}, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
	})
	r.Group(func(guest chi.Router) {
		guest.Use(session.RequireGuest)
		guest.Get(loginPath, h.Show)
		guest.With(limit).Post("/auth/login/{provider}", h.Start)
		guest.Get("/auth/callback/{provider}", h.Callback)
	})
	r.With(session.Require).Post("/auth/logout", h.Logout)
}
