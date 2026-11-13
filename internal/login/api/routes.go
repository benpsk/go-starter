package api

import (
	"net/http"

	transport "github.com/benpsk/go-starter/internal/api"
	"github.com/benpsk/go-starter/internal/apperror"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/login"
	"github.com/benpsk/go-starter/internal/ratelimit"
	"github.com/benpsk/go-starter/internal/users"
	"github.com/go-chi/chi/v5"
)

// Mount registers authentication endpoints beneath the common API router.
func Mount(r chi.Router, cfg config.Config, svc *login.Service, userService *users.Service) {
	limitConfig := func(limit int) ratelimit.Config {
		return ratelimit.Config{Limit: limit, Window: cfg.RateLimit.Window, SkipLocalhost: !cfg.IsProduction()}
	}
	onLimit := func(w http.ResponseWriter, _ *http.Request) {
		transport.Error(w, apperror.TooManyRequests("rate limit exceeded"))
	}
	loginLimit := ratelimit.NewFixedWindow("api_auth_login", limitConfig(cfg.RateLimit.APILogin), onLimit)
	refreshLimit := ratelimit.NewFixedWindow("api_auth_refresh", limitConfig(cfg.RateLimit.APIRefresh), onLimit)
	h := NewLoginAPIHandler(svc, userService, LoginAPIConfig{RefreshCookieName: cfg.Auth.API.RefreshCookieName, CookieSecure: cfg.Auth.CookieSecure})
	r.Route("/auth", func(auth chi.Router) {
		auth.With(loginLimit).Post("/login/{provider}", h.Login)
		auth.With(refreshLimit).Post("/refresh", h.Refresh)
		auth.Post("/logout", h.Logout)
		auth.With(Authenticator(svc)).Get("/me", h.Me)
	})
}
