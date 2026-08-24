package api

import (
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/benpsk/go-starter/internal/apperror"
	"github.com/benpsk/go-starter/internal/appsvc"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/ratelimit"
)

// Mount wires the JSON API routes onto the provided router.
//
// Handlers are constructed here from the services in svc. Middleware that only
// the API needs (bearer auth, JSON content type, API rate limits) is applied
// here rather than globally.
func Mount(r chi.Router, cfg config.Config, svc appsvc.Services) {
	limitConfig := func(limit int) ratelimit.Config {
		return ratelimit.Config{
			Limit:         limit,
			Window:        cfg.RateLimit.Window,
			SkipLocalhost: !cfg.IsProduction(),
		}
	}
	onLimit := func(w http.ResponseWriter, _ *http.Request) {
		Error(w, apperror.TooManyRequests("rate limit exceeded"))
	}
	loginRateLimit := ratelimit.NewFixedWindow("api_auth_login", limitConfig(cfg.RateLimit.APILogin), onLimit)
	refreshRateLimit := ratelimit.NewFixedWindow("api_auth_refresh", limitConfig(cfg.RateLimit.APIRefresh), onLimit)

	login := NewLoginAPIHandler(svc.Login, svc.Users, LoginAPIConfig{
		RefreshCookieName: cfg.Auth.API.RefreshCookieName,
		CookieSecure:      cfg.Auth.CookieSecure,
	})

	r.Route("/api", func(apiRouter chi.Router) {
		apiRouter.Use(requireJSONContentType)
		apiRouter.NotFound(notFound)
		apiRouter.MethodNotAllowed(methodNotAllowed)

		apiRouter.Route("/auth", func(auth chi.Router) {
			auth.With(loginRateLimit).Post("/login/{provider}", login.Login)
			auth.With(refreshRateLimit).Post("/refresh", login.Refresh)
			auth.Post("/logout", login.Logout)

			auth.Group(func(protected chi.Router) {
				protected.Use(Authenticator(svc.Login))
				protected.Get("/me", login.Me)
			})
		})
	})
}

func requireJSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.ContentLength != 0 {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				Error(w, apperror.New(http.StatusUnsupportedMediaType, "content type must be application/json", nil))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	Error(w, apperror.NotFound("endpoint not found"))
}

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	Error(w, apperror.New(http.StatusMethodNotAllowed, "method not allowed", nil))
}
