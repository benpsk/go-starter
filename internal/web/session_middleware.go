package web

import (
	"net/http"

	usersession "github.com/benpsk/go-starter/internal/auth/user"
	"github.com/benpsk/go-starter/internal/authctx"
)

// SessionMiddleware resolves the browser session cookie into a principal.
//
// This is web-only middleware. Because it is mounted on the browser routes
// rather than globally, it never runs for static assets or the JSON API and so
// needs no path skip list.
type SessionMiddleware struct {
	Sessions  *usersession.Manager
	LoginPath string
	HomePath  string
}

// WithUser attaches the principal when a valid session exists, and otherwise
// continues as an anonymous request.
func (m SessionMiddleware) WithUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := m.Sessions.Validate(r)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		ctx := authctx.WithUser(r.Context(), authctx.User{
			ID:          claims.ID,
			Email:       claims.Email,
			DisplayName: claims.DisplayName,
			AvatarURL:   claims.AvatarURL,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Require rejects anonymous requests, honouring htmx redirects.
func (m SessionMiddleware) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authctx.UserFromContext(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}
		if IsHtmx(r) {
			w.Header().Set("HX-Redirect", m.LoginPath)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, m.LoginPath, http.StatusSeeOther)
	})
}

// RequireGuest sends already-authenticated visitors away from sign-in pages.
func (m SessionMiddleware) RequireGuest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authctx.UserFromContext(r.Context()); ok {
			http.Redirect(w, r, m.HomePath, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
