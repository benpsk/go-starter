package postgres

import "context"

type querierKey struct{}

// WithQuerier pins a Querier onto the context. Tests use it to run a whole
// request inside one transaction they later roll back.
func WithQuerier(ctx context.Context, db Querier) context.Context {
	if db == nil {
		return ctx
	}
	return context.WithValue(ctx, querierKey{}, db)
}

// QuerierFromContext returns the context's Querier, or the fallback when none
// was pinned.
func QuerierFromContext(ctx context.Context, fallback Querier) Querier {
	if ctx == nil {
		return fallback
	}
	if value, ok := ctx.Value(querierKey{}).(Querier); ok {
		return value
	}
	return fallback
}
