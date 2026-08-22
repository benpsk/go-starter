package postgres_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func migrationOpts() postgres.ApplyOptions {
	return postgres.ApplyOptions{Kind: postgres.KindMigration}
}

func writeSQL(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// forgetHistory removes the rows a test created, so the shared test database
// stays usable by the other tests in this package.
func forgetHistory(t *testing.T, pool *pgxpool.Pool, names ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, name := range names {
			_, _ = pool.Exec(context.Background(), `delete from schema_history where name = $1`, name)
		}
	})
}

func appliedAt(t *testing.T, pool *pgxpool.Pool, kind postgres.Kind, name string) time.Time {
	t.Helper()
	var at time.Time
	err := pool.QueryRow(context.Background(),
		`select applied_at from schema_history where kind = $1 and name = $2`, string(kind), name).Scan(&at)
	if err != nil {
		t.Fatalf("read applied_at for %s %s: %v", kind, name, err)
	}
	return at
}

func TestApplyRunsEachFileOnce(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeSQL(t, dir, "000001_widgets.sql", `create table if not exists migrator_widgets (id bigserial primary key)`)
	forgetHistory(t, pool, "000001_widgets.sql")
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `drop table if exists migrator_widgets`) })

	applied, err := postgres.Apply(ctx, pool, dir, migrationOpts())
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if len(applied) != 1 || applied[0] != "000001_widgets.sql" {
		t.Fatalf("unexpected applied list: %v", applied)
	}

	// A second run must be a no-op, which is what makes migrations safe to
	// re-run on every deploy.
	again, err := postgres.Apply(ctx, pool, dir, migrationOpts())
	if err != nil {
		t.Fatalf("second Apply failed: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("expected no re-application, got %v", again)
	}
}

func TestApplyOrdersLexicographically(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeSQL(t, dir, "000002_add_column.sql", `alter table migrator_ordered add column label text`)
	writeSQL(t, dir, "000001_create.sql", `create table migrator_ordered (id bigserial primary key)`)
	forgetHistory(t, pool, "000001_create.sql", "000002_add_column.sql")
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `drop table if exists migrator_ordered`) })

	applied, err := postgres.Apply(ctx, pool, dir, migrationOpts())
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if len(applied) != 2 || applied[0] != "000001_create.sql" || applied[1] != "000002_add_column.sql" {
		t.Fatalf("expected lexicographic order, got %v", applied)
	}
}

func TestApplyRollsBackAFailedFile(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeSQL(t, dir, "000001_broken.sql", `this is not valid sql`)

	if _, err := postgres.Apply(ctx, pool, dir, migrationOpts()); err == nil {
		t.Fatalf("expected a broken migration to fail")
	}

	var recorded bool
	if err := pool.QueryRow(ctx,
		`select exists (select 1 from schema_history where name = '000001_broken.sql')`).Scan(&recorded); err != nil {
		t.Fatalf("check bookkeeping: %v", err)
	}
	if recorded {
		t.Fatalf("a failed file must not be recorded as applied")
	}
}

func TestApplyRecordsAnEmptyFile(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeSQL(t, dir, "000001_empty.sql", "   \n")
	forgetHistory(t, pool, "000001_empty.sql")

	applied, err := postgres.Apply(ctx, pool, dir, migrationOpts())
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("an empty file should still be recorded, got %v", applied)
	}
}

func TestApplyRequiresAKind(t *testing.T) {
	pool := testutil.SetupDB(t)

	if _, err := postgres.Apply(context.Background(), pool, t.TempDir(), postgres.ApplyOptions{}); err == nil {
		t.Fatalf("expected a missing kind to be rejected")
	}
}

func TestApplyOnlyRunsTheNamedFile(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeSQL(t, dir, "000001_one.sql", `select 1`)
	writeSQL(t, dir, "000002_two.sql", `select 2`)
	forgetHistory(t, pool, "000001_one.sql", "000002_two.sql")

	applied, err := postgres.Apply(ctx, pool, dir, postgres.ApplyOptions{
		Kind: postgres.KindMigration,
		Only: "000002_two.sql",
	})
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if len(applied) != 1 || applied[0] != "000002_two.sql" {
		t.Fatalf("expected only the named file, got %v", applied)
	}
}

func TestApplyOnlyRejectsAnAbsentFile(t *testing.T) {
	pool := testutil.SetupDB(t)

	_, err := postgres.Apply(context.Background(), pool, t.TempDir(), postgres.ApplyOptions{
		Kind: postgres.KindMigration,
		Only: "nope.sql",
	})
	if !errors.Is(err, postgres.ErrFileNotFound) {
		t.Fatalf("expected ErrFileNotFound, got %v", err)
	}
}

func TestApplyForceReRunsAndUpdatesTheTimestamp(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	dir := t.TempDir()
	// Idempotent on purpose: this is how a seeder is expected to be written.
	writeSQL(t, dir, "000001_rows.sql", `
        create table if not exists migrator_forced (id int primary key);
        insert into migrator_forced (id) values (1) on conflict (id) do nothing;
    `)
	forgetHistory(t, pool, "000001_rows.sql")
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `drop table if exists migrator_forced`) })

	opts := postgres.ApplyOptions{Kind: postgres.KindSeeder}
	if _, err := postgres.Apply(ctx, pool, dir, opts); err != nil {
		t.Fatalf("first Apply failed: %v", err)
	}
	first := appliedAt(t, pool, postgres.KindSeeder, "000001_rows.sql")

	// Without force the recorded file is skipped.
	skipped, err := postgres.Apply(ctx, pool, dir, opts)
	if err != nil {
		t.Fatalf("second Apply failed: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("expected the recorded file to be skipped, got %v", skipped)
	}

	forced := opts
	forced.Force = true
	reapplied, err := postgres.Apply(ctx, pool, dir, forced)
	if err != nil {
		t.Fatalf("forced Apply failed: %v", err)
	}
	if len(reapplied) != 1 {
		t.Fatalf("expected force to re-apply, got %v", reapplied)
	}
	if second := appliedAt(t, pool, postgres.KindSeeder, "000001_rows.sql"); !second.After(first) {
		t.Fatalf("expected applied_at to move forward: %v then %v", first, second)
	}

	// The idempotent insert must not have duplicated.
	var rows int
	if err := pool.QueryRow(ctx, `select count(*) from migrator_forced`).Scan(&rows); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected an idempotent seeder to leave one row, got %d", rows)
	}
}

func TestKindsAreTrackedIndependently(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	// The reason schema_history is keyed on (kind, name): a seeder sharing a
	// filename with a migration must still run.
	const name = "000001_same_name.sql"

	migrations := t.TempDir()
	writeSQL(t, migrations, name, `create table if not exists migrator_shared (id int primary key)`)
	seeders := t.TempDir()
	writeSQL(t, seeders, name, `insert into migrator_shared (id) values (1) on conflict (id) do nothing`)
	forgetHistory(t, pool, name)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `drop table if exists migrator_shared`) })

	if applied, err := postgres.Apply(ctx, pool, migrations, postgres.ApplyOptions{Kind: postgres.KindMigration}); err != nil || len(applied) != 1 {
		t.Fatalf("migration Apply = %v, %v", applied, err)
	}
	applied, err := postgres.Apply(ctx, pool, seeders, postgres.ApplyOptions{Kind: postgres.KindSeeder})
	if err != nil {
		t.Fatalf("seeder Apply failed: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("a seeder sharing a migration's name must still run, got %v", applied)
	}

	var kinds int
	if err := pool.QueryRow(ctx, `select count(*) from schema_history where name = $1`, name).Scan(&kinds); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if kinds != 2 {
		t.Fatalf("expected one row per kind, got %d", kinds)
	}
}

func TestApplyFSAndStatusFS(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	source := fstest.MapFS{
		"000001_embedded.sql": {Data: []byte(`create table if not exists migrator_embedded (id bigserial primary key)`)},
		"readme.md":           {Data: []byte(`ignored`)},
	}
	forgetHistory(t, pool, "000001_embedded.sql")
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `drop table if exists migrator_embedded`) })

	applied, err := postgres.ApplyFS(ctx, pool, source, migrationOpts())
	if err != nil {
		t.Fatalf("ApplyFS failed: %v", err)
	}
	if len(applied) != 1 || applied[0] != "000001_embedded.sql" {
		t.Fatalf("expected only .sql files to run, got %v", applied)
	}

	entries, err := postgres.StatusFS(ctx, pool, source, postgres.KindMigration)
	if err != nil {
		t.Fatalf("StatusFS failed: %v", err)
	}
	var found bool
	for _, entry := range entries {
		if entry.Name == "000001_embedded.sql" {
			found = true
			if !entry.Applied || entry.Kind != postgres.KindMigration {
				t.Fatalf("unexpected entry: %+v", entry)
			}
		}
	}
	if !found {
		t.Fatalf("expected the embedded file in status output")
	}
}

func TestStatusReportsAppliedPendingAndMissing(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeSQL(t, dir, "000001_status_applied.sql", `select 1`)
	forgetHistory(t, pool, "000001_status_applied.sql", "000002_status_pending.sql", "000000_status_ghost.sql")

	if _, err := postgres.Apply(ctx, pool, dir, migrationOpts()); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	writeSQL(t, dir, "000002_status_pending.sql", `select 1`)
	if _, err := pool.Exec(ctx,
		`insert into schema_history (kind, name) values ('migration', '000000_status_ghost.sql')`); err != nil {
		t.Fatalf("insert ghost row: %v", err)
	}

	entries, err := postgres.Status(ctx, pool, dir, postgres.KindMigration)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	byName := make(map[string]postgres.HistoryEntry, len(entries))
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	if e := byName["000001_status_applied.sql"]; !e.Applied || e.Missing || e.AppliedAt.IsZero() {
		t.Fatalf("unexpected applied entry: %+v", e)
	}
	if e := byName["000002_status_pending.sql"]; e.Applied || e.Missing {
		t.Fatalf("unexpected pending entry: %+v", e)
	}
	if e := byName["000000_status_ghost.sql"]; !e.Missing || !e.Applied {
		t.Fatalf("expected a recorded-but-absent file to be reported: %+v", e)
	}
	if !entries[len(entries)-1].Missing {
		t.Fatalf("expected orphans to sort last: %+v", entries)
	}
}

func TestStatusIsScopedToItsKind(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	forgetHistory(t, pool, "000001_seeder_only.sql")
	if _, err := pool.Exec(ctx,
		`insert into schema_history (kind, name) values ('seeder', '000001_seeder_only.sql')`); err != nil {
		t.Fatalf("insert seeder row: %v", err)
	}

	entries, err := postgres.Status(ctx, pool, t.TempDir(), postgres.KindMigration)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	for _, entry := range entries {
		if entry.Name == "000001_seeder_only.sql" {
			t.Fatalf("a seeder row must not appear in migration status: %+v", entry)
		}
	}
}

func TestMissingDirectoryIsReported(t *testing.T) {
	pool := testutil.SetupDB(t)
	absent := filepath.Join(t.TempDir(), "nope")

	if _, err := postgres.Apply(context.Background(), pool, absent, migrationOpts()); err == nil {
		t.Fatalf("expected Apply to report a missing directory")
	}
	if _, err := postgres.Status(context.Background(), pool, absent, postgres.KindMigration); err == nil {
		t.Fatalf("expected Status to report a missing directory")
	}
}

func TestEnsureHistoryTableIsIdempotent(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := postgres.EnsureHistoryTable(ctx, pool); err != nil {
			t.Fatalf("EnsureHistoryTable failed on call %d: %v", i+1, err)
		}
	}
}

func TestEnsureHistoryTableAdoptsTheLegacyTables(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	// Recreate the two-table layout this replaced, as an existing database
	// would still have it.
	for _, ddl := range []string{
		`create table schema_migrations (name text primary key, applied_at timestamptz not null default now())`,
		`create table schema_seeders (name text primary key, applied_at timestamptz not null default now())`,
		`insert into schema_migrations (name) values ('000001_legacy_migration.sql')`,
		`insert into schema_seeders (name) values ('000001_legacy_seeder.sql')`,
	} {
		if _, err := pool.Exec(ctx, ddl); err != nil {
			t.Fatalf("prepare legacy layout: %v", err)
		}
	}
	forgetHistory(t, pool, "000001_legacy_migration.sql", "000001_legacy_seeder.sql")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `drop table if exists schema_migrations`)
		_, _ = pool.Exec(context.Background(), `drop table if exists schema_seeders`)
	})

	if err := postgres.EnsureHistoryTable(ctx, pool); err != nil {
		t.Fatalf("EnsureHistoryTable failed: %v", err)
	}

	cases := []struct {
		kind postgres.Kind
		name string
	}{
		{postgres.KindMigration, "000001_legacy_migration.sql"},
		{postgres.KindSeeder, "000001_legacy_seeder.sql"},
	}
	for _, tc := range cases {
		var exists bool
		if err := pool.QueryRow(ctx,
			`select exists (select 1 from schema_history where kind = $1 and name = $2)`,
			string(tc.kind), tc.name).Scan(&exists); err != nil {
			t.Fatalf("check adopted row: %v", err)
		}
		if !exists {
			t.Fatalf("expected %s %s to be adopted", tc.kind, tc.name)
		}
	}

	// The old tables are removed, so the adoption cannot run twice.
	for _, table := range []string{"schema_migrations", "schema_seeders"} {
		var present bool
		if err := pool.QueryRow(ctx, `select to_regclass($1) is not null`, table).Scan(&present); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if present {
			t.Fatalf("expected %s to be dropped after adoption", table)
		}
	}
}
