package testutil

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMain(m *testing.M) { os.Exit(Run(m)) }

func TestParallelSchemasDoNotShareTablesOrData(t *testing.T) {
	for i := 0; i < 4; i++ {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()
			pool := CommittedDB(t)
			ctx := context.Background()
			if _, err := pool.Exec(ctx, `insert into users (email, display_name) values ('same@example.com', 'parallel')`); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := pool.QueryRow(ctx, `select count(*) from users`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("schema contains %d users, want only this test's row", count)
			}
		})
	}
}

func TestTransactionsRollBackAutomatically(t *testing.T) {
	pool := SetupDB(t)
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			_, tx, _ := BeginTx(t, pool)
			if _, err := tx.Exec(context.Background(), `insert into users (email, display_name) values ('rollback@example.com', 'rollback')`); err != nil {
				t.Fatal(err)
			}
			// Deliberately rely on t.Cleanup, including when this test returns early.
		})
	}
	var count int
	if err := pool.QueryRow(context.Background(), `select count(*) from users where email = 'rollback@example.com'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rollback left %d committed rows", count)
	}
}

func TestAllConnectionsAndReopenedPoolsUseTheIsolatedSchema(t *testing.T) {
	pool := CommittedDB(t)
	ctx := context.Background()
	var schema string
	if err := pool.QueryRow(ctx, `select current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(schema, "go_test_") {
		t.Fatalf("unexpected schema %q", schema)
	}
	var connections []*pgxpool.Conn
	defer func() {
		for _, conn := range connections {
			conn.Release()
		}
	}()
	for i := int32(0); i < pool.Config().MaxConns; i++ {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, conn)
		var actual string
		if err := conn.QueryRow(ctx, `select current_schema()`).Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if actual != schema {
			t.Fatalf("connection uses %q, want %q", actual, schema)
		}
	}
	reopened, err := pgxpool.New(ctx, pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var actual string
	if err := reopened.QueryRow(ctx, `select current_schema()`).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != schema {
		t.Fatalf("reopened pool uses %q, want %q", actual, schema)
	}
}

func TestCommittedSchemaIsRemovedAfterTheTest(t *testing.T) {
	var schema string
	t.Run("commit", func(t *testing.T) {
		pool := CommittedDB(t)
		if err := pool.QueryRow(context.Background(), `select current_schema()`).Scan(&schema); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(context.Background(), `insert into users (display_name) values ('committed')`); err != nil {
			t.Fatal(err)
		}
	})
	if schema == "" {
		t.Skip("database unavailable")
	}
	cfg, err := databaseConfig()
	if err != nil {
		t.Fatal(err)
	}
	admin, err := connect(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var exists bool
	if err := admin.QueryRow(context.Background(), `select exists (select 1 from pg_namespace where nspname = $1)`, schema).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("test schema %q survived cleanup", schema)
	}
}

func TestMigrationSchemaDoesNotApplyApplicationMigrations(t *testing.T) {
	pool := MigrationDB(t)
	var usersMissing, historyExists bool
	err := pool.QueryRow(context.Background(), `select to_regclass('users') is null, to_regclass('schema_history') is not null`).Scan(&usersMissing, &historyExists)
	if err != nil {
		t.Fatal(err)
	}
	if !usersMissing || !historyExists {
		t.Fatalf("migration schema usersMissing=%v historyExists=%v", usersMissing, historyExists)
	}
}

func TestDatabaseRequirement(t *testing.T) {
	for _, tc := range []struct {
		ci, required string
		want         bool
	}{
		{"", "", false}, {"false", "0", false}, {"true", "", true}, {"", "1", true},
	} {
		t.Run(tc.ci+"/"+tc.required, func(t *testing.T) {
			t.Setenv("CI", tc.ci)
			t.Setenv("REQUIRE_TEST_DB", tc.required)
			if got := databaseRequired(); got != tc.want {
				t.Fatalf("databaseRequired()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestUnavailableDatabasePolicy(t *testing.T) {
	if os.Getenv("GO_STARTER_DB_POLICY_CHILD") == "1" {
		SetupDB(t)
		return
	}
	for _, required := range []string{"0", "1"} {
		t.Run(required, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestUnavailableDatabasePolicy$", "-test.v")
			// Replace rather than append duplicate environment keys; the child must
			// never inherit an actual database URL or a CI requirement from its parent.
			env := []string{}
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				switch key {
				case "CI", "REQUIRE_TEST_DB", "TEST_DATABASE_URL", "GO_STARTER_DB_POLICY_CHILD":
					continue
				}
				env = append(env, entry)
			}
			cmd.Env = append(env, "CI=", "REQUIRE_TEST_DB="+required, "TEST_DATABASE_URL=postgres://nobody@127.0.0.1:1/unavailable?connect_timeout=1", "GO_STARTER_DB_POLICY_CHILD=1")
			out, err := cmd.CombinedOutput()
			if required == "1" {
				if err == nil || !strings.Contains(string(out), "FAIL") {
					t.Fatalf("required DB should fail: err=%v output=%s", err, out)
				}
			} else if err != nil || !strings.Contains(string(out), "SKIP") {
				t.Fatalf("optional DB should skip: err=%v output=%s", err, out)
			}
		})
	}
}

func TestSharedSuiteCleanup(t *testing.T) {
	skipShort(t)
	if os.Getenv("GO_STARTER_DB_LIFECYCLE_CHILD") == "1" {
		pool := SetupDB(t)
		var schema string
		if err := pool.QueryRow(context.Background(), `select current_schema()`).Scan(&schema); err != nil {
			t.Fatal(err)
		}
		// The parent verifies TestMain removes this schema after the child exits.
		println("TEST_SCHEMA=" + schema)
		return
	}
	pool := SetupDB(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestSharedSuiteCleanup$", "-test.v")
	cmd.Env = append(os.Environ(), "GO_STARTER_DB_LIFECYCLE_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child suite: %v\n%s", err, out)
	}
	var schema string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "TEST_SCHEMA=") {
			schema = strings.TrimPrefix(line, "TEST_SCHEMA=")
		}
	}
	if !strings.HasPrefix(schema, "go_test_") {
		t.Fatalf("child did not report an isolated schema: %s", out)
	}
	var parentSchema string
	if err := pool.QueryRow(context.Background(), `select current_schema()`).Scan(&parentSchema); err != nil {
		t.Fatal(err)
	}
	if schema == parentSchema {
		t.Fatal("simultaneous suite processes reused a schema")
	}
	var exists bool
	if err := pool.QueryRow(context.Background(), `select exists (select 1 from pg_namespace where nspname = $1)`, schema).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("child suite left its shared schema %q behind", schema)
	}
}
