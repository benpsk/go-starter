package api

import (
	"net/http"

	"github.com/benpsk/go-starter/internal/apperror"
	"github.com/benpsk/go-starter/internal/authctx"
	loginsvc "github.com/benpsk/go-starter/internal/login"
)

// Authenticator rejects requests without a valid bearer access token and stores
// the principal on the request context.
//
// Only the token is verified here: access tokens are short-lived and
// self-contained, so an authenticated request costs no database round trip.
// Handlers that need the full user record load it from the users service.
func Authenticator(svc loginsvc.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, err := svc.ParseAccessToken(loginsvc.BearerTokenFromRequest(r))
			if err != nil {
				Error(w, apperror.Unauthorized("unauthorized"))
				return
			}
			ctx := authctx.WithUser(r.Context(), authctx.User{ID: claims.UserID})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
