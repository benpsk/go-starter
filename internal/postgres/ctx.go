package postgres

import "context"

type dbHandleKey struct{}

func WithDBHandle(ctx context.Context, db DBHandle) context.Context {
	if db == nil {
		return ctx
	}
	return context.WithValue(ctx, dbHandleKey{}, db)
}

func DBFromContext(ctx context.Context, fallback DBHandle) DBHandle {
	if ctx == nil {
		return fallback
	}
	if value := ctx.Value(dbHandleKey{}); value != nil {
		if db, ok := value.(DBHandle); ok {
			return db
		}
	}
	return fallback
}
