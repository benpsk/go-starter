package postgres

import (
	"context"
	"fmt"
)

// HealthRepository provides the datastore probe used by health reporting.
type HealthRepository struct {
	db Querier
}

// NewHealthRepository constructs a health repository instance.
func NewHealthRepository(db Querier) *HealthRepository {
	return &HealthRepository{db: db}
}

// Ping issues the cheapest possible round trip to the database.
func (r *HealthRepository) Ping(ctx context.Context) error {
	var one int
	if err := QuerierFromContext(ctx, r.db).QueryRow(ctx, `select 1`).Scan(&one); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}
