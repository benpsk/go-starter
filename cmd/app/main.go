// Command app is the single entrypoint for this application. It serves HTTP and
// runs every console command; which one depends on the first argument.
//
// It is deliberately thin: it resolves the command, builds the dependency graph
// only if that command needs one, and maps the outcome to an exit code. The
// wiring lives in internal/app, and the commands live in internal/console.
//
// serve is registered here rather than in internal/console, because it is the
// only command that needs the HTTP layer. Keeping it here means internal/console
// never imports internal/server, internal/web, or internal/api, so a console
// command cannot reach a handler even by accident.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	_ "github.com/joho/godotenv/autoload"

	"github.com/benpsk/go-starter/internal/app"
	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/console"
	"github.com/benpsk/go-starter/internal/server"
)

// Exit codes. Usage mistakes are separated from execution failures so a
// scheduler or CI step can tell "I called it wrong" from "it ran and broke".
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	registry := console.New(programName(), os.Stdout)
	registry.Add(serveCommand())

	// Built at most once, and only for commands that declared they need it, so
	// `help` and `version` keep working without a valid configuration.
	var instance *app.App
	defer func() {
		if instance != nil {
			instance.Close()
		}
	}()

	appFunc := func(context.Context) (*app.App, error) {
		if instance != nil {
			return instance, nil
		}
		cfg, err := config.Load()
		if err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
		instance = app.New(cfg)
		return instance, nil
	}

	err := registry.Run(ctx, appFunc, os.Args[1:])
	switch {
	case err == nil:
		return exitOK
	case console.IsUsageError(err):
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return exitUsage
	default:
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return exitFailure
	}
}

// serveCommand starts the HTTP server and blocks until the context is
// cancelled, which the signal handler does on SIGINT or SIGTERM.
func serveCommand() console.Command {
	return console.Command{
		Name:     "serve",
		Group:    console.GroupServing,
		Summary:  "Start the HTTP server",
		NeedsApp: true,
		Setup: func(fs *flag.FlagSet) console.RunFunc {
			addr := fs.String("addr", "", "listen address, overriding HTTP_ADDR")

			return func(ctx context.Context, a *app.App, out io.Writer) error {
				cfg := a.Config()
				if strings.TrimSpace(*addr) != "" {
					cfg.HTTPAddr = strings.TrimSpace(*addr)
				}

				services, err := a.Services(ctx)
				if err != nil {
					return err
				}
				sessions, err := a.Sessions(ctx)
				if err != nil {
					return err
				}

				srv := server.New(cfg, server.NewRouter(cfg, sessions, services))
				fmt.Fprintf(out, "listening on %s\n", listenURL(cfg.HTTPAddr))
				return srv.Start(ctx)
			}
		},
	}
}

func programName() string {
	if len(os.Args) == 0 || strings.TrimSpace(os.Args[0]) == "" {
		return "app"
	}
	return filepath.Base(os.Args[0])
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
