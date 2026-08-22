package console

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/benpsk/go-starter/internal/app"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/testutil"
)

// These are feature tests for the console transport: the real registry, the real
// commands, and a real database. Only APP_ENV is adjusted, because db:fresh
// refuses to run outside development.

// newLiveRegistry wires the registry against the prepared test database.
func newLiveRegistry(t *testing.T, appEnv string) (*Registry, AppFunc, *bytes.Buffer) {
	t.Helper()

	testutil.SetupDB(t) // loads .env.test and migrates, or skips
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.AppEnv = appEnv
	// Keep the object store out of the working tree.
	cfg.Storage.Driver = config.StorageDriverLocal
	cfg.Storage.LocalDir = filepath.Join(t.TempDir(), "media")

	instance := app.New(cfg)
	t.Cleanup(instance.Close)

	var out bytes.Buffer
	registry := New("app", &out)
	return registry, func(context.Context) (*app.App, error) { return instance, nil }, &out
}

// runFromRepoRoot runs a command with the working directory at the repository
// root, so the default db/migrations path resolves the way it does in practice.
func runFromRepoRoot(t *testing.T, r *Registry, appFunc AppFunc, args ...string) error {
	t.Helper()
	root, err := testutil.RepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	t.Chdir(root)
	return r.Run(context.Background(), appFunc, args)
}

func TestMigrateIsIdempotent(t *testing.T) {
	r, appFunc, out := newLiveRegistry(t, "development")

	if err := runFromRepoRoot(t, r, appFunc, "db:migrate"); err != nil {
		t.Fatalf("db:migrate failed: %v", err)
	}
	// SetupDB already migrated, so a second run must be a no-op rather than an
	// error.
	if !strings.Contains(out.String(), "nothing to apply") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestMigrateFromTheEmbeddedBundle(t *testing.T) {
	r, appFunc, out := newLiveRegistry(t, "development")

	// An empty --path asks for the copies compiled into the binary, which is
	// what a deployed binary uses.
	if err := runFromRepoRoot(t, r, appFunc, "db:migrate", "--path="); err != nil {
		t.Fatalf("db:migrate failed: %v", err)
	}
	if !strings.Contains(out.String(), "embedded") {
		t.Fatalf("expected the embedded source to be named: %q", out.String())
	}
}

func TestSeedIsIdempotent(t *testing.T) {
	r, appFunc, out := newLiveRegistry(t, "development")

	if err := runFromRepoRoot(t, r, appFunc, "db:seed"); err != nil {
		t.Fatalf("db:seed failed: %v", err)
	}
	if err := runFromRepoRoot(t, r, appFunc, "db:seed"); err != nil {
		t.Fatalf("second db:seed failed: %v", err)
	}
	if !strings.Contains(out.String(), "nothing to apply") {
		t.Fatalf("expected the second run to be a no-op: %q", out.String())
	}
}

func TestStatusReportsTheAppliedMigration(t *testing.T) {
	r, appFunc, out := newLiveRegistry(t, "development")

	if err := runFromRepoRoot(t, r, appFunc, "db:status"); err != nil {
		t.Fatalf("db:status failed: %v", err)
	}
	body := out.String()
	for _, want := range []string{"STATE", "applied", "000001_initial.sql", "pending"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status output missing %q: %q", want, body)
		}
	}
}

func TestFreshRefusesOutsideDevelopment(t *testing.T) {
	for _, env := range []string{"production", "test", "staging", ""} {
		t.Run(env, func(t *testing.T) {
			r, appFunc, _ := newLiveRegistry(t, env)

			err := runFromRepoRoot(t, r, appFunc, "db:fresh")
			if !IsUsageError(err) {
				t.Fatalf("db:fresh must refuse APP_ENV=%q, got %v", env, err)
			}
			if !strings.Contains(err.Error(), "development only") {
				t.Fatalf("unexpected message: %v", err)
			}
		})
	}
}

func TestFreshResetsAndReMigrates(t *testing.T) {
	r, appFunc, out := newLiveRegistry(t, "development")

	if err := runFromRepoRoot(t, r, appFunc, "db:fresh", "--seed"); err != nil {
		t.Fatalf("db:fresh failed: %v", err)
	}
	body := out.String()
	for _, want := range []string{"schema reset", "migrate: applied", "seed: applied"} {
		if !strings.Contains(body, want) {
			t.Fatalf("fresh output missing %q: %q", want, body)
		}
	}
}

func TestFreshValidatesPathsBeforeTouchingTheSchema(t *testing.T) {
	r, appFunc, out := newLiveRegistry(t, "development")

	// A bad --seed-path must be caught before the schema is dropped, or a typo
	// destroys the database and then fails.
	err := runFromRepoRoot(t, r, appFunc, "db:fresh", "--seed", "--seed-path=/definitely/not/here")
	if !IsUsageError(err) {
		t.Fatalf("expected a usage error, got %v", err)
	}
	if strings.Contains(out.String(), "schema reset") {
		t.Fatalf("the schema must not be reset when a path is invalid")
	}
}

func TestPruneSessions(t *testing.T) {
	r, appFunc, out := newLiveRegistry(t, "development")

	if err := runFromRepoRoot(t, r, appFunc, "cron:prune-sessions", "--older-than=1h"); err != nil {
		t.Fatalf("cron:prune-sessions failed: %v", err)
	}
	if !strings.Contains(out.String(), "prune-sessions: removed") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestPruneRefreshTokens(t *testing.T) {
	r, appFunc, out := newLiveRegistry(t, "development")

	if err := runFromRepoRoot(t, r, appFunc, "cron:prune-refresh-tokens", "--older-than=1h"); err != nil {
		t.Fatalf("cron:prune-refresh-tokens failed: %v", err)
	}
	if !strings.Contains(out.String(), "prune-refresh-tokens: removed") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestPruneRejectsANegativeGrace(t *testing.T) {
	for _, name := range []string{"cron:prune-sessions", "cron:prune-refresh-tokens"} {
		t.Run(name, func(t *testing.T) {
			r, appFunc, _ := newLiveRegistry(t, "development")

			err := runFromRepoRoot(t, r, appFunc, name, "--older-than=-1h")
			if !IsUsageError(err) {
				t.Fatalf("expected a usage error, got %v", err)
			}
		})
	}
}

func TestDumpRejectsConflictingModes(t *testing.T) {
	r, appFunc, _ := newLiveRegistry(t, "development")

	err := runFromRepoRoot(t, r, appFunc, "db:dump", "--schema-only", "--data-only")
	if !IsUsageError(err) {
		t.Fatalf("expected a usage error, got %v", err)
	}
}

func TestDumpReportsAMissingBinary(t *testing.T) {
	r, appFunc, _ := newLiveRegistry(t, "development")

	// Resolved before anything else runs, so the failure names the cause.
	err := runFromRepoRoot(t, r, appFunc, "db:dump", "--pg-dump-bin=pg_dump_does_not_exist")
	if !IsUsageError(err) {
		t.Fatalf("expected a usage error, got %v", err)
	}
	if !strings.Contains(err.Error(), "not found on PATH") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestDumpWritesAFile(t *testing.T) {
	requirePgDump(t)

	r, appFunc, out := newLiveRegistry(t, "development")
	target := filepath.Join(t.TempDir(), "nested", "dump.sql")

	if err := runFromRepoRoot(t, r, appFunc, "db:dump", "--schema-only", "--out="+target); err != nil {
		t.Fatalf("db:dump failed: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("expected the dump file to exist: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("expected a non-empty dump")
	}
	if !strings.Contains(out.String(), "dump written:") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestMigrateForceIsDevelopmentOnly(t *testing.T) {
	for _, env := range []string{"production", "test", "staging"} {
		t.Run(env, func(t *testing.T) {
			r, appFunc, _ := newLiveRegistry(t, env)

			err := runFromRepoRoot(t, r, appFunc, "db:migrate", "--force", "--file=000001_initial.sql")
			if !IsUsageError(err) {
				t.Fatalf("expected a usage error, got %v", err)
			}
			if !strings.Contains(err.Error(), "write a new migration instead") {
				t.Fatalf("unexpected message: %v", err)
			}
		})
	}
}

func TestMigrateForceRequiresAFile(t *testing.T) {
	r, appFunc, _ := newLiveRegistry(t, "development")

	// Re-applying every migration is never correct, so blanket force is
	// refused even in development.
	err := runFromRepoRoot(t, r, appFunc, "db:migrate", "--force")
	if !IsUsageError(err) {
		t.Fatalf("expected a usage error, got %v", err)
	}
	if !strings.Contains(err.Error(), "requires --file") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestMigrateForceReAppliesOneFile(t *testing.T) {
	r, appFunc, out := newLiveRegistry(t, "development")

	// The initial migration is written with `if not exists` guards, so
	// re-applying it is safe.
	if err := runFromRepoRoot(t, r, appFunc, "db:migrate", "--force", "--file=000001_initial.sql"); err != nil {
		t.Fatalf("db:migrate --force failed: %v", err)
	}
	if !strings.Contains(out.String(), "migrate: applied 000001_initial.sql") {
		t.Fatalf("expected the file to be re-applied: %q", out.String())
	}
}

func TestSeedForceNeedsFileOrAll(t *testing.T) {
	r, appFunc, _ := newLiveRegistry(t, "development")

	err := runFromRepoRoot(t, r, appFunc, "db:seed", "--force")
	if !IsUsageError(err) {
		t.Fatalf("expected a usage error, got %v", err)
	}
	if !strings.Contains(err.Error(), "--all") {
		t.Fatalf("expected the message to name the alternatives: %v", err)
	}
}

func TestSeedAllRequiresForce(t *testing.T) {
	r, appFunc, _ := newLiveRegistry(t, "development")

	err := runFromRepoRoot(t, r, appFunc, "db:seed", "--all")
	if !IsUsageError(err) {
		t.Fatalf("expected a usage error, got %v", err)
	}
}

func TestSeedForceReApplies(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"one file", []string{"db:seed", "--force", "--file=000001_seed.sql"}},
		{"every file", []string{"db:seed", "--force", "--all"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, appFunc, out := newLiveRegistry(t, "development")

			// Seed once so the record exists, then prove force runs it again.
			if err := runFromRepoRoot(t, r, appFunc, "db:seed"); err != nil {
				t.Fatalf("db:seed failed: %v", err)
			}
			if err := runFromRepoRoot(t, r, appFunc, tc.args...); err != nil {
				t.Fatalf("%v failed: %v", tc.args, err)
			}
			if !strings.Contains(out.String(), "seed: applied 000001_seed.sql") {
				t.Fatalf("expected the seeder to be re-applied: %q", out.String())
			}
		})
	}
}

func TestFileFlagRejectsAnUnknownName(t *testing.T) {
	cases := [][]string{
		{"db:migrate", "--file=nope.sql"},
		{"db:seed", "--file=nope.sql"},
	}
	for _, args := range cases {
		t.Run(args[0], func(t *testing.T) {
			r, appFunc, _ := newLiveRegistry(t, "development")

			// A typo should exit 2, not look like a failed migration.
			err := runFromRepoRoot(t, r, appFunc, args...)
			if !IsUsageError(err) {
				t.Fatalf("expected a usage error, got %v", err)
			}
			if !strings.Contains(err.Error(), "nope.sql") {
				t.Fatalf("expected the name in the message: %v", err)
			}
		})
	}
}

func TestStatusReportsBothKinds(t *testing.T) {
	r, appFunc, out := newLiveRegistry(t, "development")

	if err := runFromRepoRoot(t, r, appFunc, "db:status"); err != nil {
		t.Fatalf("db:status failed: %v", err)
	}
	body := out.String()
	// One table means both kinds report in one listing.
	for _, want := range []string{"migration (source:", "seeder (source:", "000001_initial.sql", "pending"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status output missing %q: %q", want, body)
		}
	}
}

func TestDumpRejectsInvalidFlagCombinations(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"schema and data", []string{"db:dump", "--schema-only", "--data-only"}, "at most one"},
		{"unknown format", []string{"db:dump", "--format=tar"}, "--format must be"},
		// pg_dump rejects this pair itself; catching it here explains why.
		{"clean with data-only", []string{"db:dump", "--clean", "--data-only"}, "no schema to drop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, appFunc, _ := newLiveRegistry(t, "development")

			err := runFromRepoRoot(t, r, appFunc, tc.args...)
			if !IsUsageError(err) {
				t.Fatalf("expected a usage error, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q in %v", tc.want, err)
			}
		})
	}
}

func TestDumpCleanEmitsDropStatements(t *testing.T) {
	requirePgDump(t)
	r, appFunc, out := newLiveRegistry(t, "development")
	target := filepath.Join(t.TempDir(), "clean.sql")

	if err := runFromRepoRoot(t, r, appFunc, "db:dump", "--clean", "--out="+target); err != nil {
		t.Fatalf("db:dump --clean failed: %v", err)
	}
	body := readDump(t, target)
	if !strings.Contains(body, "DROP TABLE IF EXISTS") {
		t.Fatalf("expected guarded drop statements so the dump restores over an existing database")
	}
	if !strings.Contains(out.String(), "restore with psql") {
		t.Fatalf("expected the restore tool to be named: %q", out.String())
	}
}

func TestDumpNeverReferencesARole(t *testing.T) {
	requirePgDump(t)
	r, appFunc, _ := newLiveRegistry(t, "development")
	target := filepath.Join(t.TempDir(), "roles.sql")

	if err := runFromRepoRoot(t, r, appFunc, "db:dump", "--out="+target); err != nil {
		t.Fatalf("db:dump failed: %v", err)
	}

	// Any of these would make the dump fail to import on a server that lacks
	// the role, which is what --no-owner and --no-privileges prevent.
	body := readDump(t, target)
	for _, forbidden := range []string{"OWNER TO", "GRANT ", "REVOKE ", "SESSION AUTHORIZATION"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("dump contains %q, so it cannot restore as an arbitrary role", forbidden)
		}
	}
}

func TestDumpExcludesTables(t *testing.T) {
	requirePgDump(t)

	cases := []struct {
		name       string
		flag       string
		wantSchema bool
	}{
		// The table disappears completely.
		{"exclude-table", "--exclude-table=user_sessions", false},
		// The schema stays, so the dump still restores against the app's
		// expectations; only the rows are gone.
		{"exclude-table-data", "--exclude-table-data=user_sessions", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, appFunc, _ := newLiveRegistry(t, "development")
			target := filepath.Join(t.TempDir(), "excluded.sql")

			if err := runFromRepoRoot(t, r, appFunc, "db:dump", tc.flag, "--out="+target); err != nil {
				t.Fatalf("db:dump failed: %v", err)
			}
			body := readDump(t, target)
			hasSchema := strings.Contains(body, "CREATE TABLE public.user_sessions")
			if hasSchema != tc.wantSchema {
				t.Fatalf("CREATE TABLE present = %v, want %v", hasSchema, tc.wantSchema)
			}
			// The untouched table is unaffected either way.
			if !strings.Contains(body, "CREATE TABLE public.users") {
				t.Fatalf("excluding one table must not drop the others")
			}
		})
	}
}

func TestDumpExcludeFlagsAreRepeatable(t *testing.T) {
	requirePgDump(t)
	r, appFunc, _ := newLiveRegistry(t, "development")
	target := filepath.Join(t.TempDir(), "multi.sql")

	if err := runFromRepoRoot(t, r, appFunc, "db:dump",
		"--exclude-table=user_sessions",
		"--exclude-table=api_refresh_tokens",
		"--out="+target,
	); err != nil {
		t.Fatalf("db:dump failed: %v", err)
	}
	body := readDump(t, target)
	for _, table := range []string{"user_sessions", "api_refresh_tokens"} {
		if strings.Contains(body, "CREATE TABLE public."+table) {
			t.Fatalf("expected %s to be excluded", table)
		}
	}
}

func TestDumpCustomFormatIsBinaryAndPsqlFree(t *testing.T) {
	requirePgDump(t)
	r, appFunc, out := newLiveRegistry(t, "development")
	target := filepath.Join(t.TempDir(), "archive.dump")

	if err := runFromRepoRoot(t, r, appFunc, "db:dump", "--format=custom", "--out="+target); err != nil {
		t.Fatalf("db:dump --format=custom failed: %v", err)
	}

	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("PGDMP")) {
		t.Fatalf("expected a pg_dump archive header, got %q", raw[:min(8, len(raw))])
	}
	// The point of this format here: no psql meta-commands, so importers that
	// are not psql can read it.
	if bytes.Contains(raw, []byte("\\restrict")) {
		t.Fatalf("custom format should carry no psql meta-commands")
	}
	if !strings.Contains(out.String(), "restore with pg_restore") {
		t.Fatalf("expected the restore tool to be named: %q", out.String())
	}
}

func TestDumpDefaultFilenameMatchesTheFormat(t *testing.T) {
	cases := []struct {
		format string
		suffix string
	}{
		{"plain", ".sql"},
		{"custom", ".dump"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			if got := defaultDumpPath(tc.format); !strings.HasSuffix(got, tc.suffix) {
				t.Fatalf("defaultDumpPath(%q) = %q, want suffix %q", tc.format, got, tc.suffix)
			}
		})
	}
}

func requirePgDump(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump is not installed")
	}
}

func readDump(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	if len(raw) == 0 {
		t.Fatalf("dump is empty")
	}
	return string(raw)
}
