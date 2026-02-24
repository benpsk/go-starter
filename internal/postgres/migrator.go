package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnsureTable creates the bookkeeping table required to track applied migrations.
func EnsureTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
        create table if not exists schema_migrations (
            name text primary key,
            applied_at timestamptz not null default now()
        )
    `)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	return nil
}

// EnsureSeedTable creates the bookkeeping table required to track applied seeders.
func EnsureSeedTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
        create table if not exists schema_seeders (
            name text primary key,
            applied_at timestamptz not null default now()
        )
    `)
	if err != nil {
		return fmt.Errorf("create schema_seeders : %w", err)
	}
	return nil
}

// Apply executes unapplied .sql files found in dir, ordered lexicographically.
// Each file is executed inside a transaction; files should contain a single SQL
// statement compatible with PostgreSQL's extended protocol.
func Apply(ctx context.Context, pool *pgxpool.Pool, dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("migrations directory %q not found", dir)
		}
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	return apply(ctx, pool, os.DirFS(dir), entries)
}

// ApplyFS executes migrations discovered in the provided filesystem.
func ApplyFS(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("migrations filesystem empty: %w", err)
		}
		return nil, fmt.Errorf("read migrations fs: %w", err)
	}

	return apply(ctx, pool, fsys, entries)
}

func apply(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, entries []fs.DirEntry) ([]string, error) {
	files := listSQLFiles(entries)

	var applied []string

	for _, name := range files {
		alreadyApplied, err := migrationApplied(ctx, pool, name)
		if err != nil {
			return applied, err
		}
		if alreadyApplied {
			continue
		}

		contents, err := fs.ReadFile(fsys, name)
		if err != nil {
			return applied, fmt.Errorf("read %s: %w", name, err)
		}
		statement := strings.TrimSpace(string(contents))
		if statement == "" {
			if err := recordMigration(ctx, pool, name); err != nil {
				return applied, err
			}
			applied = append(applied, name)
			continue
		}

		if err := runMigration(ctx, pool, name, statement); err != nil {
			return applied, err
		}

		applied = append(applied, name)
	}

	return applied, nil
}

// Seed executes .sql seed files found in dir, ordered lexicographically. Each
// file is executed inside a transaction. Seeders are not tracked in the
// schema_migrations table.
func Seed(ctx context.Context, pool *pgxpool.Pool, dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("seeders directory %q not found", dir)
		}
		return nil, fmt.Errorf("read seeders dir: %w", err)
	}

	return seed(ctx, pool, os.DirFS(dir), entries)
}

// SeedFS executes seeders discovered in the provided filesystem.
func SeedFS(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("seeders filesystem empty: %w", err)
		}
		return nil, fmt.Errorf("read seeders fs: %w", err)
	}

	return seed(ctx, pool, fsys, entries)
}

func seed(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, entries []fs.DirEntry) ([]string, error) {
	files := listSQLFiles(entries)

	var applied []string

	for _, name := range files {
		alreadyApplied, err := seedApplied(ctx, pool, name)
		if err != nil {
			return applied, err
		}
		if alreadyApplied {
			continue
		}

		contents, err := fs.ReadFile(fsys, name)
		if err != nil {
			return applied, fmt.Errorf("read %s: %w", name, err)
		}

		statement := strings.TrimSpace(string(contents))
		if statement == "" {
			if err := recordSeed(ctx, pool, name); err != nil {
				return applied, err
			}
			applied = append(applied, name)
			continue
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return applied, fmt.Errorf("begin seed %s: %w", name, err)
		}

		if _, err := tx.Exec(ctx, statement); err != nil {
			tx.Rollback(ctx) //nolint:errcheck - safe to ignore rollback errors
			return applied, fmt.Errorf("exec seed %s: %w", name, err)
		}

		if err := recordSeedTx(ctx, tx, name); err != nil {
			tx.Rollback(ctx) //nolint:errcheck - safe to ignore rollback errors
			return applied, err
		}

		if err := tx.Commit(ctx); err != nil {
			return applied, fmt.Errorf("commit seed %s: %w", name, err)
		}

		applied = append(applied, name)
	}

	return applied, nil
}

func migrationApplied(ctx context.Context, pool *pgxpool.Pool, name string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `select exists (select 1 from schema_migrations where name = $1)`, name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check migration %s: %w", name, err)
	}
	return exists, nil
}

func seedApplied(ctx context.Context, pool *pgxpool.Pool, name string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `select exists (select 1 from schema_seeders where name = $1)`, name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check seed %s: %w", name, err)
	}
	return exists, nil
}

func runMigration(ctx context.Context, pool *pgxpool.Pool, name, statement string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck - safe to ignore rollback errors

	if _, err := tx.Exec(ctx, statement); err != nil {
		return fmt.Errorf("exec migration %s: %w", name, err)
	}

	if err := recordMigrationTx(ctx, tx, name); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}

	return nil
}

func recordMigration(ctx context.Context, pool *pgxpool.Pool, name string) error {
	if _, err := pool.Exec(ctx, `insert into schema_migrations (name) values ($1)`, name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	return nil
}

func recordMigrationTx(ctx context.Context, tx pgx.Tx, name string) error {
	if _, err := tx.Exec(ctx, `insert into schema_migrations (name) values ($1)`, name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	return nil
}

func recordSeed(ctx context.Context, pool *pgxpool.Pool, name string) error {
	if _, err := pool.Exec(ctx, `insert into schema_seeders (name) values ($1)`, name); err != nil {
		return fmt.Errorf("record seed %s: %w", name, err)
	}
	return nil
}

func recordSeedTx(ctx context.Context, tx pgx.Tx, name string) error {
	if _, err := tx.Exec(ctx, `insert into schema_seeders (name) values ($1)`, name); err != nil {
		return fmt.Errorf("record seed %s: %w", name, err)
	}
	return nil
}

func listSQLFiles(entries []fs.DirEntry) []string {
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}

	sort.Strings(files)

	return files
}
