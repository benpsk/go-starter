package server

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/config"
	webstatic "github.com/benpsk/go-starter/static"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewRouter(cfg config.Config, db *pgxpool.Pool) *chi.Mux {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   appOrigins(cfg.AppURL),
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(securityHeaders)
	r.Use(csrfProtection)
	r.Use(middleware.Recoverer)

	staticFS := webstatic.FileSystem()
	if _, err := os.Stat("static"); err == nil {
		staticFS = http.Dir("static")
	}

	h := newHandler(db, cfg)
	authRateLimiter := newAuthRateLimiter(defaultAuthRateLimitRequests, defaultAuthRateLimitWindow)
	r.Use(h.loadSession)

	r.NotFound(h.notFoundPage)
	r.MethodNotAllowed(h.methodNotAllowedPage)

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(staticFS)))
	r.Get("/", h.homePage)
	r.Get("/about", h.aboutPage)
	r.With(h.requireGuest).Get("/auth/login", h.loginPage)
	r.With(authRateLimiter.limitByIP("web_oauth_start"), h.requireGuest).Post("/auth/login/{provider}", h.startSocialLogin)
	r.With(h.requireGuest).Get("/auth/callback/{provider}", h.oauthCallback)
	r.With(h.requireAuth).Get("/account", h.accountPage)
	r.With(h.requireAuth).Post("/auth/logout", h.logout)
	r.Route("/api/auth", func(r chi.Router) {
		r.With(authRateLimiter.limitByIP("api_auth_login")).Post("/login/{provider}", h.apiLogin)
		r.With(authRateLimiter.limitByIP("api_auth_refresh")).Post("/refresh", h.apiRefresh)
		r.Post("/logout", h.apiLogout)
		r.With(h.requireAPIAuth).Get("/me", h.apiMe)
	})
	r.Get("/healthz", h.healthz)
	r.Get("/api/health", h.healthz)

	return r
}

func appOrigins(appURL string) []string {
	appURL = strings.TrimSpace(appURL)
	if appURL == "" {
		return nil
	}
	parsed, err := url.Parse(appURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil
	}
	return []string{parsed.Scheme + "://" + parsed.Host}
}
