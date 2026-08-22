package console

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	dbembed "github.com/benpsk/go-starter/db"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultMigrationsDir = "db/migrations"
	defaultSeedersDir    = "db/seeders"
)

// sqlSource is where a command reads .sql files from: a directory on disk, or
// the copies embedded in the binary. Deployed binaries have no repository
// checkout beside them, so the embedded set is the fallback.
type sqlSource struct {
	kind postgres.Kind
	dir  string
	fsys fs.FS
}

func (s sqlSource) embedded() bool { return s.fsys != nil }

// describe names the source for log output.
func (s sqlSource) describe() string {
	if s.embedded() {
		return "embedded"
	}
	return s.dir
}

// apply runs the source's files, honouring the --file and --force selection.
func (s sqlSource) apply(ctx context.Context, pool *pgxpool.Pool, only string, force bool) ([]string, error) {
	opts := postgres.ApplyOptions{Kind: s.kind, Only: only, Force: force}
	if s.embedded() {
		return postgres.ApplyFS(ctx, pool, s.fsys, opts)
	}
	return postgres.Apply(ctx, pool, s.dir, opts)
}

func (s sqlSource) status(ctx context.Context, pool *pgxpool.Pool) ([]postgres.HistoryEntry, error) {
	if s.embedded() {
		return postgres.StatusFS(ctx, pool, s.fsys, s.kind)
	}
	return postgres.Status(ctx, pool, s.dir, s.kind)
}

// migrationSource resolves the migrations location for a --path value.
func migrationSource(path string) (sqlSource, error) {
	return resolveSource(postgres.KindMigration, path, defaultMigrationsDir, dbembed.Migrations, "migrations")
}

// seederSource resolves the seeders location for a --path value.
func seederSource(path string) (sqlSource, error) {
	return resolveSource(postgres.KindSeeder, path, defaultSeedersDir, dbembed.Seeders, "seeders")
}

// resolveSource prefers the directory when it exists. A missing default path
// falls back to the embedded copies; a missing explicit path is an error,
// because the caller asked for something specific and did not get it.
func resolveSource(kind postgres.Kind, path, defaultPath string, embedded fs.FS, sub string) (sqlSource, error) {
	useEmbedded := path == ""
	if !useEmbedded {
		info, err := os.Stat(path)
		switch {
		case err == nil:
			if !info.IsDir() {
				return sqlSource{}, usagef("path %q is not a directory", path)
			}
			return sqlSource{kind: kind, dir: path}, nil
		case errors.Is(err, os.ErrNotExist):
			if path != defaultPath {
				return sqlSource{}, usagef("path %q not found", path)
			}
			useEmbedded = true
		default:
			return sqlSource{}, fmt.Errorf("stat path %q: %w", path, err)
		}
	}

	fsys, err := fs.Sub(embedded, sub)
	if err != nil {
		return sqlSource{}, fmt.Errorf("open embedded %s: %w", sub, err)
	}
	return sqlSource{kind: kind, fsys: fsys}, nil
}

// sqlFileNames lists the .sql files a source carries. It exists for tests that
// assert the embedded bundle is not empty.
func sqlFileNames(s sqlSource) ([]string, error) {
	if !s.embedded() {
		entries, err := os.ReadDir(s.dir)
		if err != nil {
			return nil, err
		}
		return sqlNames(entries), nil
	}
	entries, err := fs.ReadDir(s.fsys, ".")
	if err != nil {
		return nil, err
	}
	return sqlNames(entries), nil
}

func sqlNames(entries []fs.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	return names
}
