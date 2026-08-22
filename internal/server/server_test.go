package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/benpsk/go-starter/internal/config"
)

func TestStartShutsDownWhenTheContextIsCancelled(t *testing.T) {
	cfg := config.Config{HTTPAddr: "127.0.0.1:0", ShutdownTimeout: time.Second}
	srv := New(cfg, http.NotFoundHandler())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a cancelled context should shut down cleanly, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Start did not return after the context was cancelled")
	}
}

func TestStartReportsAListenFailure(t *testing.T) {
	cfg := config.Config{HTTPAddr: "127.0.0.1:1", ShutdownTimeout: time.Second}
	srv := New(cfg, http.NotFoundHandler())

	if err := srv.Start(context.Background()); err == nil {
		t.Fatalf("expected binding to a privileged port to fail")
	}
}
