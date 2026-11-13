package postgres_test

import (
	"context"
	"testing"

	healthsvc "github.com/benpsk/go-starter/internal/health"
	healthpg "github.com/benpsk/go-starter/internal/health/postgres"
	"github.com/benpsk/go-starter/internal/testutil"
)

func TestHealthRepositoryPing(t *testing.T) {
	pool := testutil.SetupDB(t)

	if err := healthpg.NewHealthRepository(pool).Ping(context.Background()); err != nil {
		t.Fatalf("Ping against a live database should succeed: %v", err)
	}
}

func TestHealthRepositoryPingUsesTheContextQuerier(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	if err := healthpg.NewHealthRepository(pool).Ping(ctx); err != nil {
		t.Fatalf("Ping inside a transaction should succeed: %v", err)
	}
}

// compile-time assertion that the repository satisfies the domain contract
var _ healthsvc.Repository = (*healthpg.HealthRepository)(nil)
