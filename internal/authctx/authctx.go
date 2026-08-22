// Package authctx carries the authenticated principal on a request context. It
// is deliberately narrow: handlers that need the full record fetch it from the
// users service rather than widening this struct.
package authctx

import "context"

type userKey struct{}

// User is the authenticated principal for the current request.
type User struct {
	ID          int64
	Email       string
	DisplayName string
	AvatarURL   string
}

// WithUser stores the principal on the provided context.
func WithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, userKey{}, user)
}

// UserFromContext extracts the principal from the context.
func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userKey{}).(User)
	return user, ok
}
