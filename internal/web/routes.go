package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/benpsk/go-starter/internal/appsvc"
	usersession "github.com/benpsk/go-starter/internal/auth/user"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/ratelimit"
)

// Mount wires the server-rendered browser routes onto the provided router.
//
// Handlers are constructed here from the services in svc. Middleware that only
// browsers need (CSRF, cookie sessions, the OAuth start rate limit) is applied
// here rather than globally, so it never runs for the JSON API or static files.
func Mount(r chi.Router, cfg config.Config, userSessions *usersession.Manager, svc appsvc.Services) {
	setting := cfg.Setting()

	csrf := NewCSRFMiddleware(CSRFConfig{Secure: cfg.Auth.CookieSecure})
	session := SessionMiddleware{
		Sessions:  userSessions,
		LoginPath: loginPath,
		HomePath:  defaultPostLoginPath,
	}
	oauthRateLimit := ratelimit.NewFixedWindow("web_oauth_start", ratelimit.Config{
		Limit:         cfg.RateLimit.WebOAuth,
		Window:        cfg.RateLimit.Window,
		SkipLocalhost: !cfg.IsProduction(),
	}, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
	})

	page := NewPageHandler(setting)
	login := NewUserLoginHandler(svc.Login, userSessions, setting)
	account := NewAccountHandler(svc.Users, setting, loginPath)

	// Fallbacks live on the root router so a miss anywhere renders the
	// branded error page rather than chi's plain text.
	r.NotFound(page.NotFound)
	r.MethodNotAllowed(page.MethodNotAllowed)

	r.Group(func(browser chi.Router) {
		browser.Use(csrf)
		browser.Use(session.WithUser)

		browser.Get("/", page.Home)
		browser.Get("/about", page.About)

		browser.Group(func(guest chi.Router) {
			guest.Use(session.RequireGuest)
			guest.Get(loginPath, login.Show)
			guest.With(oauthRateLimit).Post("/auth/login/{provider}", login.Start)
			guest.Get("/auth/callback/{provider}", login.Callback)
		})

		browser.Group(func(protected chi.Router) {
			protected.Use(session.Require)
			protected.Get("/account", account.Show)
			protected.Post("/auth/logout", login.Logout)
		})
	})
}
