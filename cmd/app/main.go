package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/r2"
	"github.com/benpsk/go-starter/internal/server"
	"github.com/benpsk/go-starter/internal/storage"
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

	store, err := newStorage(ctx, cfg)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	r := server.NewRouter(cfg, db, store)
	srv := server.New(cfg, r)

	log.Printf("Listening on %s", listenURL(cfg.HTTPAddr))
	if err := srv.Start(ctx); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func newStorage(ctx context.Context, cfg config.Config) (storage.Store, error) {
	switch cfg.Storage.Driver {
	case "local":
		return storage.NewLocal(cfg.Storage.LocalDir, cfg.AppURL, cfg.Storage.LocalPublicPath)
	case "r2":
		client, err := r2.New(ctx, cfg.R2.Endpoint, cfg.R2.Region, cfg.R2.AccessKeyID, cfg.R2.SecretAccessKey, cfg.R2.Bucket, cfg.R2.PublicBaseURL)
		if err != nil {
			return nil, err
		}
		return storage.NewR2(client), nil
	default:
		return nil, fmt.Errorf("unsupported storage driver %q", cfg.Storage.Driver)
	}
}

func listenURL(addr string) string {
	listen := addr
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
