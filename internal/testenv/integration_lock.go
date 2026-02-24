package testenv

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func LockIntegrationDB(ctx context.Context, pool *pgxpool.Pool, lockID int64) (func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire advisory lock conn: %w", err)
	}
	if _, err := conn.Exec(ctx, `select pg_advisory_lock($1)`, lockID); err != nil {
		conn.Release()
		return nil, fmt.Errorf("acquire advisory lock: %w", err)
	}
	return func() {
		_, _ = conn.Exec(context.Background(), `select pg_advisory_unlock($1)`, lockID)
		conn.Release()
	}, nil
}
