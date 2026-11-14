package main

import (
	"bytes"
	"context"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/benpsk/go-starter/internal/app"
	"github.com/benpsk/go-starter/internal/config"
)

func TestServeAbortsWhenStartupMigrationsFail(t *testing.T) {
	t.Chdir(t.TempDir())
	// An invalid migration directory fails before opening any database or listener.
	if err := os.WriteFile("db", []byte("invalid directory"), 0644); err != nil {
		t.Fatal(err)
	}
	a := app.New(config.Config{})
	defer a.Close()
	var out bytes.Buffer
	run := serveCommand().Setup(flag.NewFlagSet("serve", flag.ContinueOnError))
	err := run(context.Background(), a, &out)
	if err == nil || !strings.Contains(err.Error(), "startup migrations:") {
		t.Fatalf("expected startup migration error, got %v", err)
	}
	if strings.Contains(out.String(), "listening") {
		t.Fatalf("server started: %s", &out)
	}
}
