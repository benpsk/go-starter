// Command app is the HTTP server entrypoint and the composition root of this
// application: it loads configuration, opens the database, builds every
// repository and service, and hands the assembled aggregate to the router.
//
// Wiring lives here and nowhere else. internal/server composes middleware and
// mounts transports; it never constructs a service.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	_ "github.com/joho/godotenv/autoload"

	usersession "github.com/benpsk/go-starter/internal/auth/user"
	"github.com/benpsk/go-starter/internal/config"
	healthsvc "github.com/benpsk/go-starter/internal/health"
	loginsvc "github.com/benpsk/go-starter/internal/login"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/server"
	"github.com/benpsk/go-starter/internal/storage"
	usersvc "github.com/benpsk/go-starter/internal/users"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Connect(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	// Repositories: one per domain, all over the same pool.
	healthRepository := postgres.NewHealthRepository(db)
	userRepository := postgres.NewUserRepository(db)
	loginRepository := postgres.NewLoginRepository(db)

	cookieSecure := cfg.Auth.CookieSecure || cfg.IsProduction()
	userSessions := usersession.NewManager(
		db,
		cfg.Auth.SessionCookieName,
		cfg.Auth.SessionTTL,
		cookieSecure,
	)

	objectStorage := newObjectStorage(ctx, cfg)

	// Services: domain behaviour over the repositories above.
	userService := usersvc.NewService(userRepository)
	loginService := loginsvc.NewService(loginRepository, userService, nil, loginsvc.Config{
		AppURL:            cfg.AppURL,
		AccessTokenSecret: cfg.Auth.API.AccessTokenSecret,
		AccessTokenTTL:    cfg.Auth.API.AccessTokenTTL,
		RefreshTokenTTL:   cfg.Auth.API.RefreshTokenTTL,
		Google: loginsvc.OAuthProviderConfig{
			ClientID:     cfg.Auth.Social.Google.ClientID,
			ClientSecret: cfg.Auth.Social.Google.ClientSecret,
		},
		GitHub: loginsvc.OAuthProviderConfig{
			ClientID:     cfg.Auth.Social.GitHub.ClientID,
			ClientSecret: cfg.Auth.Social.GitHub.ClientSecret,
		},
	})
	if !loginService.APITokensConfigured() {
		log.Printf("auth: api tokens disabled: API_ACCESS_TOKEN_SECRET is not set")
	}

	services := server.Services{
		Health:  healthsvc.NewService(healthRepository),
		Users:   userService,
		Login:   loginService,
		Storage: objectStorage,
	}

	r := server.NewRouter(cfg, userSessions, services)
	srv := server.New(cfg, r)

	log.Printf("Listening on %s", listenURL(cfg.HTTPAddr))
	if err := srv.Start(ctx); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// newObjectStorage builds the configured object store. A driver that cannot
// start degrades to a disabled store so the rest of the application still
// boots; only the features that upload files stop working.
func newObjectStorage(ctx context.Context, cfg config.Config) storage.Service {
	switch cfg.Storage.Driver {
	case config.StorageDriverLocal:
		local, err := storage.NewLocalService(storage.LocalConfig{
			RootDir:       cfg.Storage.LocalDir,
			PublicBaseURL: strings.TrimRight(cfg.AppURL, "/") + cfg.Storage.LocalPublicPath,
		})
		if err != nil {
			log.Printf("storage: disabled: %v", err)
			return storage.NewDisabledService(err.Error())
		}
		log.Printf("storage: configured driver=local root=%s", cfg.Storage.LocalDir)
		return local

	case config.StorageDriverR2:
		r2, err := storage.NewR2Service(ctx, storage.R2Config{
			Endpoint:        cfg.R2.Endpoint,
			Region:          cfg.R2.Region,
			AccessKeyID:     cfg.R2.AccessKeyID,
			SecretAccessKey: cfg.R2.SecretAccessKey,
			Bucket:          cfg.R2.Bucket,
			PublicBaseURL:   cfg.R2.PublicBaseURL,
		})
		if err != nil {
			log.Printf("storage: disabled: %v", err)
			return storage.NewDisabledService(err.Error())
		}
		log.Printf("storage: configured driver=r2 bucket=%s", cfg.R2.Bucket)
		return r2

	default:
		log.Printf("storage: disabled: unknown driver %q", cfg.Storage.Driver)
		return storage.NewDisabledService("unknown storage driver " + cfg.Storage.Driver)
	}
}

func listenURL(addr string) string {
	listen := addr
	if listen == "" {
		return "http://127.0.0.1:8080"
	}
	if strings.HasPrefix(listen, ":") {
		listen = "127.0.0.1" + listen
	} else if strings.HasPrefix(listen, "0.0.0.0:") {
		listen = "127.0.0.1" + listen[len("0.0.0.0"):]
	}
	if !strings.Contains(listen, "://") {
		listen = "http://" + listen
	}
	return listen
}
