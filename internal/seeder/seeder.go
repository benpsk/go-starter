// Package seeder holds the application's compiled Go seeders.
package seeder

import (
	"context"
	"fmt"

	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type seed struct {
	name string
	run  func(context.Context, postgres.Querier) error
}

// Register seeders here in execution order. Each must be independently idempotent.
var seeds = []seed{
	{name: "initial", run: initial},
}

// initial is intentionally empty: the starter ships no application seed data.
// Add Go data preparation here and use q for transactional repository writes.
func initial(ctx context.Context, q postgres.Querier) error {
	return nil
}

// Apply runs pending Go seeders, optionally selecting or forcing a named seeder.
func Apply(ctx context.Context, pool *pgxpool.Pool, only string, force bool) ([]string, error) {
	selected := seeds
	if only != "" {
		selected = nil
		for _, s := range seeds {
			if s.name == only {
				selected = append(selected, s)
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("%q: %w", only, postgres.ErrFileNotFound)
		}
	}
	var applied []string
	for _, s := range selected {
		ran, err := postgres.Seed(ctx, pool, s.name, force, s.run)
		if err != nil {
			return applied, err
		}
		if ran {
			applied = append(applied, s.name)
		}
	}
	return applied, nil
}

// Status reports pending, applied, and removed Go seeders.
func Status(ctx context.Context, pool *pgxpool.Pool) ([]postgres.HistoryEntry, error) {
	names := make([]string, 0, len(seeds))
	for _, s := range seeds {
		names = append(names, s.name)
	}
	return postgres.SeederStatus(ctx, pool, names)
}
