package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/benpsk/go-starter/internal/api"
	usersession "github.com/benpsk/go-starter/internal/auth/user"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/web"
	webstatic "github.com/benpsk/go-starter/static"
)

// requestTimeout bounds how long any single request may run.
const requestTimeout = 30 * time.Second

// newHTTPRouter builds the composition root: global middleware, static assets,
// health checks, and the mounted transport routers (browser + JSON API).
//
// Nothing else belongs here. Handlers and services are constructed in cmd/app
// and reach this function through Services.
func newHTTPRouter(cfg config.Config, userSessions *usersession.Manager, services Services) chi.Router {
	r := chi.NewRouter()

	development := !cfg.IsProduction()

	allowedOrigins := []string{"*"}
	if !development {
		allowedOrigins = []string{cfg.AppURL}
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Timeout(requestTimeout))
	r.Use(securityHeaders)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RedirectSlashes)

	// Assets are embedded in the binary, which would otherwise shadow a
	// rebuilt bundle on disk until the binary is recompiled. Development reads
	// disk first, and tells the browser not to cache, so an asset rebuild is
	// picked up on the next request rather than after a hard reload.
	staticFS := webstatic.FileSystemWithFallback(http.Dir("static"))
	if development {
		staticFS = webstatic.FileSystemPreferring(http.Dir("static"))
	}
	var staticHandler http.Handler = http.StripPrefix("/static/", http.FileServer(staticFS))
	if development {
		staticHandler = noStoreAssets(staticHandler)
	}
	r.Handle("/static/*", staticHandler)

	if cfg.Storage.Driver == config.StorageDriverLocal {
		mediaPrefix := strings.TrimRight(cfg.Storage.LocalPublicPath, "/") + "/"
		r.Handle(mediaPrefix+"*", http.StripPrefix(mediaPrefix, http.FileServer(http.Dir(cfg.Storage.LocalDir))))
	}

	health := NewHealthHandler(services.Health)
	r.Get("/healthz", health.Live)
	r.Get("/readyz", health.Ready)

	web.Mount(r, cfg, userSessions, services)
	api.Mount(r, cfg, services)

	return r
}

// noStoreAssets stops the browser caching development assets, so a rebuilt
// bundle or stylesheet takes effect on the next request.
func noStoreAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
