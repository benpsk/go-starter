package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ResetSchema drops and recreates the public schema, removing all objects.
func ResetSchema(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin reset schema: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck - safe to ignore rollback errors

	if _, err := tx.Exec(ctx, `drop schema if exists public cascade`); err != nil {
		return fmt.Errorf("drop schema public: %w", err)
	}
	if _, err := tx.Exec(ctx, `create schema public`); err != nil {
		return fmt.Errorf("create schema public: %w", err)
	}
	if _, err := tx.Exec(ctx, `grant all on schema public to public`); err != nil {
		return fmt.Errorf("grant schema public: %w", err)
	}
	if _, err := tx.Exec(ctx, `grant all on schema public to current_user`); err != nil {
		return fmt.Errorf("grant schema public current_user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit reset schema: %w", err)
	}
	return nil
}
