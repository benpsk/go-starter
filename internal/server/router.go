package server

import (
	"github.com/go-chi/chi/v5"

	usersession "github.com/benpsk/go-starter/internal/auth/user"
	"github.com/benpsk/go-starter/internal/config"
)

// NewRouter builds the active router from the internal/server route tree.
func NewRouter(cfg config.Config, userSessions *usersession.Manager, services Services) chi.Router {
	return newHTTPRouter(cfg, userSessions, services)
}
