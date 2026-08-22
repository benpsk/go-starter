package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/benpsk/go-starter/internal/app"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		AppName: "Go Starter",
		AppEnv:  "test",
		AppURL:  "http://127.0.0.1:8080",
		Auth: config.AuthConfig{
			SessionCookieName: "go_starter_session",
			SessionTTL:        time.Hour,
			API: config.APIAuthConfig{
				AccessTokenSecret: "test-secret",
				AccessTokenTTL:    10 * time.Minute,
				RefreshTokenTTL:   time.Hour,
				RefreshCookieName: "go_starter_api_refresh",
			},
		},
		Storage: config.StorageConfig{
			Driver:          config.StorageDriverLocal,
			LocalDir:        filepath.Join(t.TempDir(), "media"),
			LocalPublicPath: "/media",
		},
	}
}

func TestConfigIsAvailableWithoutBuildingAnything(t *testing.T) {
	// db:dump needs the DSN and nothing else, so reading configuration must not
	// open a pool.
	cfg := testConfig(t)
	cfg.Database.URL = "postgres://nobody@127.0.0.1:59999/nope"

	a := app.New(cfg)
	defer a.Close()

	if got := a.Config().AppName; got != "Go Starter" {
		t.Fatalf("unexpected config: %q", got)
	}
}

func TestCloseWithoutOpeningIsSafe(t *testing.T) {
	a := app.New(testConfig(t))
	a.Close()
	a.Close() // idempotent
}

func TestPoolIsMemoised(t *testing.T) {
	pool := testutil.SetupDB(t)
	cfg := testConfig(t)
	cfg.Database = poolConfig(t, pool)

	a := app.New(cfg)
	defer a.Close()

	first, err := a.Pool(context.Background())
	if err != nil {
		t.Fatalf("Pool failed: %v", err)
	}
	second, err := a.Pool(context.Background())
	if err != nil {
		t.Fatalf("second Pool failed: %v", err)
	}
	if first != second {
		t.Fatalf("expected the same pool to be reused")
	}
}

func TestServicesAreMemoisedAndComplete(t *testing.T) {
	pool := testutil.SetupDB(t)
	cfg := testConfig(t)
	cfg.Database = poolConfig(t, pool)

	a := app.New(cfg)
	defer a.Close()

	services, err := a.Services(context.Background())
	if err != nil {
		t.Fatalf("Services failed: %v", err)
	}
	if services.Users == nil || services.Login == nil || services.Health == nil || services.Storage == nil {
		t.Fatalf("expected every service to be wired: %+v", services)
	}

	again, err := a.Services(context.Background())
	if err != nil {
		t.Fatalf("second Services failed: %v", err)
	}
	if again.Login != services.Login {
		t.Fatalf("expected services to be built once")
	}
}

func TestServicesDoNotCreateTheStorageRootUntilAWriteHappens(t *testing.T) {
	pool := testutil.SetupDB(t)
	cfg := testConfig(t)
	cfg.Database = poolConfig(t, pool)

	a := app.New(cfg)
	defer a.Close()

	if _, err := a.Services(context.Background()); err != nil {
		t.Fatalf("Services failed: %v", err)
	}
	if _, err := os.Stat(cfg.Storage.LocalDir); !os.IsNotExist(err) {
		t.Fatalf("building the services must not create the storage root, got %v", err)
	}
}

func TestSessionsAreMemoised(t *testing.T) {
	pool := testutil.SetupDB(t)
	cfg := testConfig(t)
	cfg.Database = poolConfig(t, pool)

	a := app.New(cfg)
	defer a.Close()

	first, err := a.Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions failed: %v", err)
	}
	if first.CookieName() != "go_starter_session" {
		t.Fatalf("unexpected cookie name: %q", first.CookieName())
	}
	second, err := a.Sessions(context.Background())
	if err != nil {
		t.Fatalf("second Sessions failed: %v", err)
	}
	if first != second {
		t.Fatalf("expected the session manager to be built once")
	}
}

func TestUnreachableDatabaseIsReported(t *testing.T) {
	cfg := testConfig(t)
	cfg.Database.URL = "postgres://nobody@127.0.0.1:59999/nope"

	a := app.New(cfg)
	defer a.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := a.Pool(ctx); err == nil {
		t.Fatalf("expected connecting to an unreachable database to fail")
	}
	if _, err := a.Services(ctx); err == nil {
		t.Fatalf("expected Services to fail when the pool cannot open")
	}
}

// poolConfig returns a database config pointing at the same DSN the shared test
// pool uses, so the App opens its own pool against the prepared schema.
func poolConfig(t *testing.T, pool interface{ Config() *pgxpool.Config }) config.DatabaseConfig {
	t.Helper()
	return config.DatabaseConfig{
		URL:      pool.Config().ConnString(),
		MaxConns: 2,
	}
}
