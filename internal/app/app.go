// Package app is the composition root. It builds the pool, the object store,
// the domain services, and the session manager, and hands them to whichever
// entrypoint asked.
//
// Everything is built on demand and memoised, because the console shares this
// package with the HTTP server: `db:migrate` needs a pool and nothing else, so
// it must not pay for an object storage client, and `help` must not open a
// database connection at all.
package app

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benpsk/go-starter/internal/appsvc"
	usersession "github.com/benpsk/go-starter/internal/auth/user"
	"github.com/benpsk/go-starter/internal/config"
	healthsvc "github.com/benpsk/go-starter/internal/health"
	loginsvc "github.com/benpsk/go-starter/internal/login"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/storage"
	usersvc "github.com/benpsk/go-starter/internal/users"
)

// App holds the process-wide dependencies.
type App struct {
	cfg config.Config

	mu       sync.Mutex
	pool     *pgxpool.Pool
	services *appsvc.Services
	sessions *usersession.Manager
}

// New prepares an App without building anything.
func New(cfg config.Config) *App {
	return &App{cfg: cfg}
}

// Config returns the loaded configuration.
func (a *App) Config() config.Config {
	return a.cfg
}

// Pool opens the database connection pool on first use.
func (a *App) Pool(ctx context.Context) (*pgxpool.Pool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.poolLocked(ctx)
}

func (a *App) poolLocked(ctx context.Context) (*pgxpool.Pool, error) {
	if a.pool != nil {
		return a.pool, nil
	}
	pool, err := postgres.Connect(ctx, a.cfg.Database)
	if err != nil {
		return nil, err
	}
	a.pool = pool
	return pool, nil
}

// Services builds the domain services on first use, opening the pool and the
// object store as a side effect.
func (a *App) Services(ctx context.Context) (appsvc.Services, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.services != nil {
		return *a.services, nil
	}

	pool, err := a.poolLocked(ctx)
	if err != nil {
		return appsvc.Services{}, err
	}

	userService := usersvc.NewService(postgres.NewUserRepository(pool))
	loginService := loginsvc.NewService(
		postgres.NewLoginRepository(pool),
		userService,
		nil,
		loginsvc.Config{
			AppURL:            a.cfg.AppURL,
			AccessTokenSecret: a.cfg.Auth.API.AccessTokenSecret,
			AccessTokenTTL:    a.cfg.Auth.API.AccessTokenTTL,
			RefreshTokenTTL:   a.cfg.Auth.API.RefreshTokenTTL,
			Google: loginsvc.OAuthProviderConfig{
				ClientID:     a.cfg.Auth.Social.Google.ClientID,
				ClientSecret: a.cfg.Auth.Social.Google.ClientSecret,
			},
			GitHub: loginsvc.OAuthProviderConfig{
				ClientID:     a.cfg.Auth.Social.GitHub.ClientID,
				ClientSecret: a.cfg.Auth.Social.GitHub.ClientSecret,
			},
		},
	)
	if !loginService.APITokensConfigured() {
		log.Printf("auth: api tokens disabled: API_ACCESS_TOKEN_SECRET is not set")
	}

	services := appsvc.Services{
		Health:  healthsvc.NewService(postgres.NewHealthRepository(pool)),
		Users:   userService,
		Login:   loginService,
		Storage: a.newObjectStorage(ctx),
	}
	a.services = &services
	return services, nil
}

// Sessions builds the browser session manager on first use. Only the HTTP
// server needs it; no console command validates a cookie.
func (a *App) Sessions(ctx context.Context) (*usersession.Manager, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.sessions != nil {
		return a.sessions, nil
	}
	pool, err := a.poolLocked(ctx)
	if err != nil {
		return nil, err
	}
	a.sessions = usersession.NewManager(
		pool,
		a.cfg.Auth.SessionCookieName,
		a.cfg.Auth.SessionTTL,
		a.cfg.Auth.CookieSecure || a.cfg.IsProduction(),
	)
	return a.sessions, nil
}

// Close releases anything that was opened. It is safe to call when nothing was.
func (a *App) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.pool != nil {
		a.pool.Close()
		a.pool = nil
	}
}

// newObjectStorage builds the configured object store. A driver that cannot
// start degrades to a disabled store so the rest of the application still
// works; only the features that persist files stop.
func (a *App) newObjectStorage(ctx context.Context) storage.Service {
	switch a.cfg.Storage.Driver {
	case config.StorageDriverLocal:
		local, err := storage.NewLocalService(storage.LocalConfig{
			RootDir:       a.cfg.Storage.LocalDir,
			PublicBaseURL: strings.TrimRight(a.cfg.AppURL, "/") + a.cfg.Storage.LocalPublicPath,
		})
		if err != nil {
			log.Printf("storage: disabled: %v", err)
			return storage.NewDisabledService(err.Error())
		}
		log.Printf("storage: configured driver=local root=%s", a.cfg.Storage.LocalDir)
		return local

	case config.StorageDriverR2:
		r2, err := storage.NewR2Service(ctx, storage.R2Config{
			Endpoint:        a.cfg.R2.Endpoint,
			Region:          a.cfg.R2.Region,
			AccessKeyID:     a.cfg.R2.AccessKeyID,
			SecretAccessKey: a.cfg.R2.SecretAccessKey,
			Bucket:          a.cfg.R2.Bucket,
			PublicBaseURL:   a.cfg.R2.PublicBaseURL,
		})
		if err != nil {
			log.Printf("storage: disabled: %v", err)
			return storage.NewDisabledService(err.Error())
		}
		log.Printf("storage: configured driver=r2 bucket=%s", a.cfg.R2.Bucket)
		return r2

	default:
		log.Printf("storage: disabled: unknown driver %q", a.cfg.Storage.Driver)
		return storage.NewDisabledService("unknown storage driver " + a.cfg.Storage.Driver)
	}
}
