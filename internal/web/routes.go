package web

import (
	usersession "github.com/benpsk/go-starter/internal/auth/user"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/go-chi/chi/v5"
)

// Mount wires the server-rendered browser routes onto the provided router.
//
// Common CSRF and cookie-session middleware wraps the feature routes,
// without running for the JSON API or static files.
func Mount(r chi.Router, cfg config.Config, userSessions *usersession.Manager, mount func(chi.Router, SessionMiddleware)) {
	setting := cfg.Setting()
	csrf := NewCSRFMiddleware(CSRFConfig{Secure: cfg.Auth.CookieSecure})
	session := SessionMiddleware{Sessions: userSessions, LoginPath: "/auth/login", HomePath: "/account"}
	page := NewPageHandler(setting)
	r.NotFound(page.NotFound)
	r.MethodNotAllowed(page.MethodNotAllowed)
	r.Group(func(browser chi.Router) {
		browser.Use(csrf)
		browser.Use(session.WithUser)
		browser.Get("/", page.Home)
		browser.Get("/about", page.About)
		mount(browser, session)
	})
}
