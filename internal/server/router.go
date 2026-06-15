package server

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/api"
	"github.com/benpsk/go-starter/internal/auth"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/storage"
	"github.com/benpsk/go-starter/internal/web"
	webstatic "github.com/benpsk/go-starter/static"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewRouter(cfg config.Config, db *pgxpool.Pool, store storage.Store) *chi.Mux {
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

	authService := auth.NewService(db, cfg)
	authRateLimiter := auth.NewRateLimiter(auth.DefaultRateLimitRequests, auth.DefaultRateLimitWindow)
	webHandler := web.NewHandler(cfg, authService)
	apiHandler := api.NewHandler(db, authService)

	staticFS := webstatic.FileSystem()
	if _, err := os.Stat("static"); err == nil {
		staticFS = http.Dir("static")
	}

	_ = store
	r.Use(authService.LoadSession)

	r.NotFound(webHandler.NotFound)
	r.MethodNotAllowed(webHandler.MethodNotAllowed)

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(staticFS)))
	if cfg.Storage.Driver == "local" {
		mediaPrefix := strings.TrimRight(cfg.Storage.LocalPublicPath, "/") + "/"
		r.Handle(mediaPrefix+"*", http.StripPrefix(mediaPrefix, http.FileServer(http.Dir(cfg.Storage.LocalDir))))
	}
	r.Get("/healthz", apiHandler.Health)
	r.Mount("/api", api.Routes(apiHandler, authRateLimiter))
	r.Mount("/", web.Routes(webHandler, authRateLimiter))

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
