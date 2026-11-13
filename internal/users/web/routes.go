package web

import (
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/users"
	transport "github.com/benpsk/go-starter/internal/web"
	"github.com/go-chi/chi/v5"
)

// Mount registers the signed-in account page.
func Mount(r chi.Router, setting config.Setting, svc *users.Service, session transport.SessionMiddleware) {
	h := NewAccountHandler(svc, setting, session.LoginPath)
	r.With(session.Require).Get("/account", h.Show)
}
