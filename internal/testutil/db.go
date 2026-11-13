package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	dbsql "github.com/benpsk/go-starter/db"
	"github.com/benpsk/go-starter/internal/postgres"
)

const setupTimeout = 30 * time.Second

var (
	setupOnce     sync.Once
	sharedDB      *pgxpool.Pool
	setupErr      error
	sharedCleanup func() error
)

var errDatabaseUnavailable = errors.New("test database is unavailable")

// Run runs a package's tests and releases its shared database resources.
// Call os.Exit(testutil.Run(m)) from TestMain in packages using SetupDB.
func Run(m *testing.M) int {
	code := m.Run()
	if sharedCleanup != nil {
		if err := sharedCleanup(); err != nil {
			fmt.Fprintln(os.Stderr, "clean up test database:", err)
			return 1
		}
	}
	return code
}

// SetupDB shares one migrated, uniquely named schema per test binary. Package
// binaries and concurrent suite runs never share schema objects or data.
// Register Run in TestMain so the schema is removed when the suite finishes.
func SetupDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	skipShort(t)
	setupOnce.Do(func() { sharedDB, sharedCleanup, setupErr = newSchema(true) })
	requireDB(t, setupErr)
	return sharedDB
}

// CommittedDB gives a test its own migrated schema and pool. Use it for real
// commits, independent connections, or DDL; cleanup drops the whole schema.
func CommittedDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return isolatedSchema(t, true)
}

// MigrationDB gives a test an empty schema with only schema_history prepared.
// Application migrations are deliberately not applied. Cleanup drops the schema.
func MigrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return isolatedSchema(t, false)
}

func isolatedSchema(t *testing.T, migrate bool) *pgxpool.Pool {
	t.Helper()
	skipShort(t)
	pool, cleanup, err := newSchema(migrate)
	requireDB(t, err)
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})
	return pool
}

// DisposableDatabase gives a test a migrated database with its own public
// schema. Reserve it for database-wide operations such as db:fresh and pg_dump.
// The configured role must have CREATEDB. Cleanup closes the pool and drops it.
func DisposableDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	skipShort(t)
	cfg, err := databaseConfig()
	requireDB(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	admin, err := connect(ctx, cfg)
	requireDB(t, err)
	name := uniqueName()
	ident := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "create database "+ident); err != nil {
		admin.Close()
		t.Fatalf("create disposable test database (role needs CREATEDB): %v", err)
	}
	// Preserve the new database in ConnString too: callers may construct app pools.
	cfg, err = pgxpool.ParseConfig(withDatabase(cfg.ConnString(), name))
	if err != nil {
		t.Fatalf("configure disposable database: %v", err)
	}
	cfg.MaxConns = 4
	cfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	cleanup := func() error {
		if pool != nil {
			pool.Close()
		}
		defer admin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
		defer cancel()
		_, err := admin.Exec(ctx, "drop database "+ident+" with (force)")
		return err
	}
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Errorf("drop disposable database: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("connect disposable database: %v", err)
	}
	if err := migrateSchema(ctx, pool, true); err != nil {
		t.Fatalf("prepare disposable database: %v", err)
	}
	return pool
}

func newSchema(migrate bool) (*pgxpool.Pool, func() error, error) {
	cfg, err := databaseConfig()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	admin, err := connect(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	name := uniqueName()
	ident := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "create schema "+ident); err != nil {
		admin.Close()
		return nil, nil, fmt.Errorf("create test schema: %w", err)
	}
	// Put search_path into the DSN, not only the parsed RuntimeParams: app tests
	// may open another pool from Config().ConnString(). Every connection must use it.
	scoped, err := pgxpool.ParseConfig(withSearchPath(cfg.ConnString(), name))
	var pool *pgxpool.Pool
	cleanup := func() error {
		if pool != nil {
			pool.Close()
		}
		defer admin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
		defer cancel()
		_, err := admin.Exec(ctx, "drop schema "+ident+" cascade")
		return err
	}
	if err == nil {
		scoped.MaxConns = 4
		scoped.MinConns = 0
		pool, err = pgxpool.NewWithConfig(ctx, scoped)
	}
	if err == nil {
		err = migrateSchema(ctx, pool, migrate)
	}
	if err != nil {
		cleanupErr := cleanup()
		return nil, nil, errors.Join(err, cleanupErr)
	}
	return pool, cleanup, nil
}

func migrateSchema(ctx context.Context, pool *pgxpool.Pool, migrate bool) error {
	if err := postgres.EnsureHistoryTable(ctx, pool); err != nil {
		return err
	}
	if migrate {
		migrations, err := fs.Sub(dbsql.Migrations, "migrations")
		if err != nil {
			return err
		}
		_, err = postgres.ApplyFS(ctx, pool, migrations, postgres.ApplyOptions{Kind: postgres.KindMigration})
		return err
	}
	return nil
}

func databaseConfig() (*pgxpool.Config, error) {
	dsn := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dsn == "" {
		path, err := FindUp(".env.test")
		if err != nil {
			return nil, fmt.Errorf("%w: set TEST_DATABASE_URL or create .env.test", errDatabaseUnavailable)
		}
		values, err := godotenv.Read(path)
		if err != nil {
			return nil, fmt.Errorf("read .env.test: %w", err)
		}
		dsn = strings.TrimSpace(values["DATABASE_URL"])
	}
	if dsn == "" {
		return nil, fmt.Errorf("%w: test DSN is empty", errDatabaseUnavailable)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse test database configuration: %w", err)
	}
	cfg.MaxConns = 2
	cfg.MinConns = 0
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	return cfg, nil
}

func connect(ctx context.Context, cfg *pgxpool.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("%w: %v", errDatabaseUnavailable, err)
	}
	return pool, nil
}

func uniqueName() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("generate test schema name: " + err.Error())
	}
	return "go_test_" + hex.EncodeToString(value[:])
}

func withSearchPath(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, _ := url.Parse(dsn)
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String()
	}
	return dsn + " search_path=" + schema
}

func withDatabase(dsn, name string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, _ := url.Parse(dsn)
		u.Path = "/" + name
		q := u.Query()
		q.Del("search_path")
		u.RawQuery = q.Encode()
		return u.String()
	}
	return dsn + " dbname=" + name + " search_path=public"
}

func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("database tests disabled by -short")
	}
}

func requireDB(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if errors.Is(err, errDatabaseUnavailable) && !databaseRequired() {
		t.Skipf("skipping database test: %v", err)
	}
	t.Fatalf("prepare test database: %v", err)
}

func databaseRequired() bool {
	enabled := func(value string) bool { return value != "" && value != "0" && !strings.EqualFold(value, "false") }
	return enabled(os.Getenv("CI")) || enabled(os.Getenv("REQUIRE_TEST_DB"))
}

// WithTx starts an automatically cleaned-up test transaction. The returned
// cleanup is safe to defer too; t.Cleanup also covers failures and early returns.
func WithTx(t *testing.T, pool *pgxpool.Pool) (context.Context, func()) {
	t.Helper()
	ctx, _, done := BeginTx(t, pool)
	return ctx, done
}

// BeginTx also returns the transaction for dependencies that need explicit
// injection, such as the session manager. Never commit this transaction.
func BeginTx(t *testing.T, pool *pgxpool.Pool) (context.Context, pgx.Tx, func()) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	var once sync.Once
	done := func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Errorf("rollback test transaction: %v", err)
			}
		})
	}
	t.Cleanup(done)
	return postgres.WithQuerier(ctx, tx), tx, done
}
