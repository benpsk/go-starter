package web

import (
	"net/http"

	"github.com/benpsk/go-starter/internal/auth"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	auth        *auth.Service
	appName     string
	appURL      string
	googleTagID string
}

func NewHandler(cfg config.Config, authService *auth.Service) Handler {
	return Handler{
		auth:        authService,
		appName:     cfg.AppName,
		appURL:      cfg.AppURL,
		googleTagID: cfg.GoogleTagID,
	}
}

func Routes(h Handler, limiter *auth.RateLimiter) chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.homePage)
	r.Get("/about", h.aboutPage)
	r.With(h.auth.RequireGuest).Get("/auth/login", h.loginPage)
	r.With(limiter.LimitByIP("web_oauth_start"), h.auth.RequireGuest).Post("/auth/login/{provider}", h.startSocialLogin)
	r.With(h.auth.RequireGuest).Get("/auth/callback/{provider}", h.oauthCallback)
	r.With(h.auth.RequireAuth).Get("/account", h.accountPage)
	r.With(h.auth.RequireAuth).Post("/auth/logout", h.logout)
	return r
}

func (h Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	h.notFoundPage(w, r)
}

func (h Handler) MethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	h.methodNotAllowedPage(w, r)
}
