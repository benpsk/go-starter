package console

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/benpsk/go-starter/internal/app"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/testutil"
)

func TestStartupMigrations(t *testing.T) {
	for _, source := range []string{"embedded", "directory", "invalid"} {
		t.Run(source, func(t *testing.T) {
			pool := testutil.MigrationDB(t)
			a := app.New(config.Config{Database: config.DatabaseConfig{URL: pool.Config().ConnString(), MaxConns: 2}})
			t.Cleanup(a.Close)
			t.Chdir(t.TempDir())
			if source != "embedded" {
				if err := os.MkdirAll("db/migrations", 0755); err != nil {
					t.Fatal(err)
				}
				sql := `create table startup_marker (id int primary key)`
				if source == "invalid" {
					sql += `; select missing_column from startup_marker`
				}
				if err := os.WriteFile(filepath.Join("db/migrations", "startup.sql"), []byte(sql), 0644); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			err := Migrate(context.Background(), a, &out)
			if source == "invalid" {
				if err == nil {
					t.Fatal("expected migration failure")
				}
				var exists bool
				if err := pool.QueryRow(context.Background(), `select to_regclass('startup_marker') is not null`).Scan(&exists); err != nil {
					t.Fatal(err)
				}
				if exists {
					t.Fatal("failed migration was not rolled back")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "migrate: applied") {
				t.Fatalf("output: %s", &out)
			}
			out.Reset()
			if err := Migrate(context.Background(), a, &out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "nothing to apply") {
				t.Fatalf("restart output: %s", &out)
			}
		})
	}
}
