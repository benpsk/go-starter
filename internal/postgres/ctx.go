package postgres

import "context"

type dbtxKey struct{}

func WithDBTX(ctx context.Context, db DBTX) context.Context {
	if db == nil {
		return ctx
	}
	return context.WithValue(ctx, dbtxKey{}, db)
}

func DBFromContext(ctx context.Context, fallback DBTX) DBTX {
	if ctx == nil {
		return fallback
	}
	if value := ctx.Value(dbtxKey{}); value != nil {
		if db, ok := value.(DBTX); ok {
			return db
		}
	}
	return fallback
}
