package testutil

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/postgres"
)

// schemaLockID serialises every package that migrates the test database, so
// packages running in parallel cannot reset the schema under each other.
const schemaLockID int64 = 7202602

var (
	setupOnce sync.Once
	sharedDB  *pgxpool.Pool
	setupErr  error
)

// errDatabaseUnavailable reports that no test database could be reached.
var errDatabaseUnavailable = errors.New("test database is unavailable")

// SetupDB returns a pool for the migrated test database, preparing it once per
// test binary. It skips the calling test when no database is reachable, so a
// fresh clone can still run the rest of the suite.
//
// Tests run against real PostgreSQL. Nothing is mocked at this layer, because
// the SQL and its constraints are exactly what these tests exist to check.
func SetupDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	setupOnce.Do(func() {
		sharedDB, setupErr = prepareDB()
	})
	if setupErr != nil {
		if errors.Is(setupErr, errDatabaseUnavailable) {
			t.Skipf("skipping database test: %v", setupErr)
		}
		t.Fatalf("prepare test database: %v", setupErr)
	}
	return sharedDB
}

func prepareDB() (*pgxpool.Pool, error) {
	if err := LoadEnv(); err != nil {
		return nil, fmt.Errorf("%w: %v", errDatabaseUnavailable, err)
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	pool, err := postgres.Connect(ctx, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errDatabaseUnavailable, err)
	}

	// The lock is held for the lifetime of the test binary and released when
	// the process exits.
	if _, err := lockDB(ctx, pool, schemaLockID); err != nil {
		pool.Close()
		return nil, err
	}

	if err := postgres.ResetSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	if err := postgres.EnsureHistoryTable(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	root, err := RepoRoot()
	if err != nil {
		pool.Close()
		return nil, err
	}
	if _, err := postgres.Apply(ctx, pool, filepath.Join(root, "db", "migrations"), postgres.ApplyOptions{
		Kind: postgres.KindMigration,
	}); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}

// WithTx runs a test inside a transaction that is always rolled back, so cases
// stay isolated without truncating tables between them.
func WithTx(t *testing.T, pool *pgxpool.Pool) (context.Context, func()) {
	t.Helper()
	ctx, _, done := BeginTx(t, pool)
	return ctx, done
}

// BeginTx is WithTx that also hands back the transaction, for tests that must
// give it to a component reading the database outside the request context (the
// session manager, for one).
func BeginTx(t *testing.T, pool *pgxpool.Pool) (context.Context, pgx.Tx, func()) {
	t.Helper()

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	return postgres.WithQuerier(ctx, tx), tx, func() {
		_ = tx.Rollback(ctx)
	}
}

// lockDB takes a session-scoped advisory lock so parallel packages serialise.
func lockDB(ctx context.Context, pool *pgxpool.Pool, lockID int64) (func(), error) {
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
