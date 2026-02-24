package server

import (
	"net/http"
	"net/url"
	"os"
	"strings"

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
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	staticFS := webstatic.FileSystem()
	if _, err := os.Stat("static"); err == nil {
		staticFS = http.Dir("static")
	}

	h := newHandler(db, strings.TrimSpace(cfg.AppName), strings.TrimSpace(cfg.AppURL))

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(staticFS)))
	r.Get("/", h.homePage)
	r.Get("/about", h.aboutPage)
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
