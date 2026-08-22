package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/testutil"
)

func TestApplyRunsEachFileOnce(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeSQL(t, dir, "000001_widgets.sql", `create table if not exists migrator_widgets (id bigserial primary key)`)

	applied, err := postgres.Apply(ctx, pool, dir)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if len(applied) != 1 || applied[0] != "000001_widgets.sql" {
		t.Fatalf("unexpected applied list: %v", applied)
	}

	// A second run must be a no-op, which is what makes migrations safe to
	// re-run on every deploy.
	again, err := postgres.Apply(ctx, pool, dir)
	if err != nil {
		t.Fatalf("second Apply failed: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("expected no re-application, got %v", again)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `drop table if exists migrator_widgets`)
		_, _ = pool.Exec(context.Background(), `delete from schema_migrations where name = '000001_widgets.sql'`)
	})
}

func TestApplyOrdersLexicographically(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeSQL(t, dir, "000002_add_column.sql", `alter table migrator_ordered add column label text`)
	writeSQL(t, dir, "000001_create.sql", `create table migrator_ordered (id bigserial primary key)`)

	applied, err := postgres.Apply(ctx, pool, dir)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if len(applied) != 2 || applied[0] != "000001_create.sql" || applied[1] != "000002_add_column.sql" {
		t.Fatalf("expected lexicographic order, got %v", applied)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `drop table if exists migrator_ordered`)
		_, _ = pool.Exec(context.Background(), `delete from schema_migrations where name like '00000%_%.sql' and name in ('000001_create.sql','000002_add_column.sql')`)
	})
}

func TestApplyRollsBackAFailedFile(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeSQL(t, dir, "000001_broken.sql", `this is not valid sql`)

	if _, err := postgres.Apply(ctx, pool, dir); err == nil {
		t.Fatalf("expected a broken migration to fail")
	}

	var recorded bool
	if err := pool.QueryRow(ctx, `select exists (select 1 from schema_migrations where name = '000001_broken.sql')`).Scan(&recorded); err != nil {
		t.Fatalf("check bookkeeping: %v", err)
	}
	if recorded {
		t.Fatalf("a failed migration must not be recorded as applied")
	}
}

func TestApplyMissingDirectory(t *testing.T) {
	pool := testutil.SetupDB(t)

	if _, err := postgres.Apply(context.Background(), pool, filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatalf("expected a missing directory to be reported")
	}
}

func TestApplyFSAndSeedFS(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	migrations := fstest.MapFS{
		"000001_embedded.sql": {Data: []byte(`create table migrator_embedded (id bigserial primary key, name text)`)},
		"readme.md":           {Data: []byte(`ignored`)},
	}
	applied, err := postgres.ApplyFS(ctx, pool, migrations)
	if err != nil {
		t.Fatalf("ApplyFS failed: %v", err)
	}
	if len(applied) != 1 || applied[0] != "000001_embedded.sql" {
		t.Fatalf("expected only .sql files to run, got %v", applied)
	}

	seeders := fstest.MapFS{
		"000001_seed.sql": {Data: []byte(`insert into migrator_embedded (name) values ('seeded')`)},
	}
	seeded, err := postgres.SeedFS(ctx, pool, seeders)
	if err != nil {
		t.Fatalf("SeedFS failed: %v", err)
	}
	if len(seeded) != 1 {
		t.Fatalf("expected one seeder to run, got %v", seeded)
	}

	var name string
	if err := pool.QueryRow(ctx, `select name from migrator_embedded`).Scan(&name); err != nil {
		t.Fatalf("read seeded row: %v", err)
	}
	if name != "seeded" {
		t.Fatalf("unexpected seeded row: %q", name)
	}

	// Seeders are tracked too, so a second run does not duplicate data.
	again, err := postgres.SeedFS(ctx, pool, seeders)
	if err != nil {
		t.Fatalf("second SeedFS failed: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("expected no re-seeding, got %v", again)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `drop table if exists migrator_embedded`)
		_, _ = pool.Exec(context.Background(), `delete from schema_migrations where name = '000001_embedded.sql'`)
		_, _ = pool.Exec(context.Background(), `delete from schema_seeders where name = '000001_seed.sql'`)
	})
}

func TestSeedFromDirectory(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `create table if not exists migrator_seeded (id bigserial primary key, name text)`); err != nil {
		t.Fatalf("create fixture table: %v", err)
	}

	dir := t.TempDir()
	writeSQL(t, dir, "000001_rows.sql", `insert into migrator_seeded (name) values ('from-dir')`)
	writeSQL(t, dir, "000002_empty.sql", ``)

	seeded, err := postgres.Seed(ctx, pool, dir)
	if err != nil {
		t.Fatalf("Seed failed: %v", err)
	}
	if len(seeded) != 2 {
		t.Fatalf("expected both files to be recorded, got %v", seeded)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `drop table if exists migrator_seeded`)
		_, _ = pool.Exec(context.Background(), `delete from schema_seeders where name in ('000001_rows.sql','000002_empty.sql')`)
	})
}

func TestSeedMissingDirectory(t *testing.T) {
	pool := testutil.SetupDB(t)

	if _, err := postgres.Seed(context.Background(), pool, filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatalf("expected a missing directory to be reported")
	}
}

func writeSQL(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
