package server

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/testenv"
	"github.com/jackc/pgx/v5/pgxpool"
)

var integrationPool *pgxpool.Pool

func TestMain(m *testing.M) {
	if err := testenv.Load(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := postgres.Connect(ctx, cfg.Database)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	unlock, err := testenv.LockIntegrationDB(ctx, pool, 7202603)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := postgres.EnsureTable(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := postgres.EnsureSeedTable(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := postgres.ResetSchema(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := postgres.EnsureTable(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := postgres.EnsureSeedTable(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := postgres.Apply(ctx, pool, "../../db/migrations"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	integrationPool = pool
	code := m.Run()
	unlock()
	pool.Close()
	os.Exit(code)
}

func withTx(t *testing.T) (context.Context, func()) {
	t.Helper()
	ctx := context.Background()
	tx, err := integrationPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	return postgres.WithDBHandle(ctx, tx), func() {
		_ = tx.Rollback(ctx)
	}
}
