package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Kind distinguishes the sorts of SQL file tracked in schema_history.
type Kind string

const (
	// KindMigration is a schema change. Migrations are not expected to be
	// idempotent, so re-running one is deliberately hard.
	KindMigration Kind = "migration"
	// KindSeeder is data. Seeders are expected to be idempotent, so re-running
	// one is safe and supported.
	KindSeeder Kind = "seeder"
)

// ErrFileNotFound reports that a named file is absent from the source.
var ErrFileNotFound = errors.New("file not found in source")

// HistoryEntry reports the state of one tracked file.
type HistoryEntry struct {
	Kind      Kind
	Name      string
	Applied   bool
	AppliedAt time.Time
	// Missing marks a file recorded as applied that is no longer present in
	// the source, which usually means it was renamed or deleted after running
	// somewhere.
	Missing bool
}

// ApplyOptions selects what to run and whether to re-run it.
type ApplyOptions struct {
	// Kind is the sort of file being applied. Required.
	Kind Kind
	// Only restricts the run to a single file name. Empty means every file.
	Only string
	// Force re-applies files already recorded in schema_history.
	Force bool
}

// EnsureHistoryTable creates the single bookkeeping table, and folds in the two
// tables that preceded it.
//
// The table is keyed on (kind, name) rather than name alone: a seeder sharing a
// filename with a migration would otherwise be recorded as already applied and
// silently skipped.
func EnsureHistoryTable(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin ensure schema_history: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck - safe to ignore rollback errors

	if _, err := tx.Exec(ctx, `
        create table if not exists schema_history (
            kind text not null,
            name text not null,
            applied_at timestamptz not null default now(),
            primary key (kind, name)
        )
    `); err != nil {
		return fmt.Errorf("create schema_history: %w", err)
	}

	// Adopt the rows from the previous two-table layout, once. The bookkeeping
	// table is not itself managed by a migration, so this is where an existing
	// database catches up.
	legacy := []struct {
		table string
		kind  Kind
	}{
		{"schema_migrations", KindMigration},
		{"schema_seeders", KindSeeder},
	}
	for _, source := range legacy {
		var exists bool
		if err := tx.QueryRow(ctx, `select to_regclass($1) is not null`, source.table).Scan(&exists); err != nil {
			return fmt.Errorf("check %s: %w", source.table, err)
		}
		if !exists {
			continue
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
            insert into schema_history (kind, name, applied_at)
            select $1, name, applied_at from %s
            on conflict (kind, name) do nothing
        `, source.table), string(source.kind)); err != nil {
			return fmt.Errorf("adopt %s: %w", source.table, err)
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`drop table %s`, source.table)); err != nil {
			return fmt.Errorf("drop %s: %w", source.table, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ensure schema_history: %w", err)
	}
	return nil
}

// Apply executes the .sql files in dir, ordered lexicographically, recording
// each one in schema_history. Call EnsureHistoryTable first.
func Apply(ctx context.Context, pool *pgxpool.Pool, dir string, opts ApplyOptions) ([]string, error) {
	files, err := readDirFiles(dir)
	if err != nil {
		return nil, err
	}
	return apply(ctx, pool, os.DirFS(dir), files, opts)
}

// ApplyFS executes the .sql files in the provided filesystem.
func ApplyFS(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, opts ApplyOptions) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read sql filesystem: %w", err)
	}
	return apply(ctx, pool, fsys, listSQLFiles(entries), opts)
}

func apply(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, files []string, opts ApplyOptions) ([]string, error) {
	if opts.Kind == "" {
		return nil, fmt.Errorf("apply: kind is required")
	}

	files, err := selectFiles(files, opts.Only)
	if err != nil {
		return nil, err
	}

	var applied []string
	for _, name := range files {
		if !opts.Force {
			recorded, err := isRecorded(ctx, pool, opts.Kind, name)
			if err != nil {
				return applied, err
			}
			if recorded {
				continue
			}
		}

		contents, err := fs.ReadFile(fsys, name)
		if err != nil {
			return applied, fmt.Errorf("read %s: %w", name, err)
		}

		if err := run(ctx, pool, opts.Kind, name, strings.TrimSpace(string(contents))); err != nil {
			return applied, err
		}
		applied = append(applied, name)
	}
	return applied, nil
}

// run executes one file and records it in the same transaction, so a failure
// leaves neither the change nor the record behind.
func run(ctx context.Context, pool *pgxpool.Pool, kind Kind, name, statement string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin %s %s: %w", kind, name, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck - safe to ignore rollback errors

	// An empty file is still recorded, so adding content to it later does not
	// silently re-run it.
	if statement != "" {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("exec %s %s: %w", kind, name, err)
		}
	}

	// Re-applying updates the timestamp rather than failing, so the record
	// reads as "last applied at".
	if _, err := tx.Exec(ctx, `
        insert into schema_history (kind, name)
        values ($1, $2)
        on conflict (kind, name) do update set applied_at = now()
    `, string(kind), name); err != nil {
		return fmt.Errorf("record %s %s: %w", kind, name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s %s: %w", kind, name, err)
	}
	return nil
}

// Status reports which files in dir have been applied.
func Status(ctx context.Context, pool *pgxpool.Pool, dir string, kind Kind) ([]HistoryEntry, error) {
	files, err := readDirFiles(dir)
	if err != nil {
		return nil, err
	}
	return status(ctx, pool, files, kind)
}

// StatusFS reports which files in the provided filesystem have been applied.
func StatusFS(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, kind Kind) ([]HistoryEntry, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read sql filesystem: %w", err)
	}
	return status(ctx, pool, listSQLFiles(entries), kind)
}

func status(ctx context.Context, pool *pgxpool.Pool, files []string, kind Kind) ([]HistoryEntry, error) {
	recorded, err := recordedFiles(ctx, pool, kind)
	if err != nil {
		return nil, err
	}

	entries := make([]HistoryEntry, 0, len(files)+len(recorded))
	onDisk := make(map[string]struct{}, len(files))
	for _, name := range files {
		onDisk[name] = struct{}{}
		at, ok := recorded[name]
		entries = append(entries, HistoryEntry{Kind: kind, Name: name, Applied: ok, AppliedAt: at})
	}

	// Anything recorded but absent from the source is reported too, rather
	// than silently omitted.
	orphans := make([]string, 0)
	for name := range recorded {
		if _, ok := onDisk[name]; !ok {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	for _, name := range orphans {
		entries = append(entries, HistoryEntry{
			Kind:      kind,
			Name:      name,
			Applied:   true,
			AppliedAt: recorded[name],
			Missing:   true,
		})
	}
	return entries, nil
}

func isRecorded(ctx context.Context, pool *pgxpool.Pool, kind Kind, name string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
        select exists (select 1 from schema_history where kind = $1 and name = $2)
    `, string(kind), name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check %s %s: %w", kind, name, err)
	}
	return exists, nil
}

func recordedFiles(ctx context.Context, pool *pgxpool.Pool, kind Kind) (map[string]time.Time, error) {
	rows, err := pool.Query(ctx, `
        select name, applied_at from schema_history where kind = $1
    `, string(kind))
	if err != nil {
		return nil, fmt.Errorf("list applied %ss: %w", kind, err)
	}
	defer rows.Close()

	recorded := make(map[string]time.Time)
	for rows.Next() {
		var (
			name string
			at   time.Time
		)
		if err := rows.Scan(&name, &at); err != nil {
			return nil, fmt.Errorf("scan applied %s: %w", kind, err)
		}
		recorded[name] = at
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied %ss: %w", kind, err)
	}
	return recorded, nil
}

// selectFiles narrows the run to one named file when Only is set.
func selectFiles(files []string, only string) ([]string, error) {
	only = strings.TrimSpace(only)
	if only == "" {
		return files, nil
	}
	for _, name := range files {
		if name == only {
			return []string{name}, nil
		}
	}
	return nil, fmt.Errorf("%q: %w", only, ErrFileNotFound)
}

func readDirFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("sql directory %q not found", dir)
		}
		return nil, fmt.Errorf("read sql dir: %w", err)
	}
	return listSQLFiles(entries), nil
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
