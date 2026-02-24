package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/server"
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

	r := server.NewRouter(cfg, db)
	srv := server.New(cfg, r)

	log.Printf("Listening on %s", listenURL(cfg.HTTPAddr))
	if err := srv.Start(ctx); err != nil {
		log.Fatalf("server: %v", err)
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
