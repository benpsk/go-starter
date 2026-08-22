package console

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/benpsk/go-starter/internal/app"
	"github.com/benpsk/go-starter/internal/config"
)

// stubApp builds an App from a config without opening anything, so dispatch can
// be tested without a database.
func stubAppFunc(calls *int) AppFunc {
	return func(context.Context) (*app.App, error) {
		*calls++
		return app.New(config.Config{AppEnv: "test"}), nil
	}
}

func failingAppFunc(err error) AppFunc {
	return func(context.Context) (*app.App, error) { return nil, err }
}

// recordCommand captures whether it ran and what its flag received.
func recordCommand(ran *bool, seen *string) Command {
	return Command{
		Name:     "spy",
		Group:    GroupOther,
		Summary:  "test command",
		NeedsApp: true,
		Setup: func(fs *flag.FlagSet) RunFunc {
			value := fs.String("value", "default", "a value")
			return func(_ context.Context, a *app.App, out io.Writer) error {
				*ran = true
				*seen = *value
				if a == nil {
					return errors.New("expected an app instance")
				}
				io.WriteString(out, "spy ran\n")
				return nil
			}
		},
	}
}

func newTestRegistry() (*Registry, *bytes.Buffer) {
	var out bytes.Buffer
	return New("app", &out), &out
}

func TestRunRequiresACommand(t *testing.T) {
	r, out := newTestRegistry()

	err := r.Run(context.Background(), failingAppFunc(errors.New("must not be called")), nil)
	if !IsUsageError(err) {
		t.Fatalf("expected a usage error, got %v", err)
	}
	// Help is printed so a bare invocation is still useful.
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("expected help output, got %q", out.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	r, _ := newTestRegistry()

	err := r.Run(context.Background(), failingAppFunc(errors.New("must not be called")), []string{"nope"})
	if !IsUsageError(err) {
		t.Fatalf("expected a usage error, got %v", err)
	}
	if !strings.Contains(err.Error(), `unknown command "nope"`) {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestRunDispatchesAndParsesFlags(t *testing.T) {
	r, out := newTestRegistry()
	var ran bool
	var seen string
	r.Add(recordCommand(&ran, &seen))

	calls := 0
	if err := r.Run(context.Background(), stubAppFunc(&calls), []string{"spy", "--value=given"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran || seen != "given" {
		t.Fatalf("command did not receive its flag: ran=%v seen=%q", ran, seen)
	}
	if calls != 1 {
		t.Fatalf("expected the app to be built once, got %d", calls)
	}
	if !strings.Contains(out.String(), "spy ran") {
		t.Fatalf("expected the command to write to the registry writer")
	}
}

func TestRunAppliesFlagDefaults(t *testing.T) {
	r, _ := newTestRegistry()
	var ran bool
	var seen string
	r.Add(recordCommand(&ran, &seen))

	calls := 0
	if err := r.Run(context.Background(), stubAppFunc(&calls), []string{"spy"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen != "default" {
		t.Fatalf("expected the default value, got %q", seen)
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	r, _ := newTestRegistry()
	var ran bool
	var seen string
	r.Add(recordCommand(&ran, &seen))

	err := r.Run(context.Background(), failingAppFunc(errors.New("must not be called")), []string{"spy", "--nope"})
	if !IsUsageError(err) {
		t.Fatalf("expected a usage error, got %v", err)
	}
	if ran {
		t.Fatalf("the command must not run with a bad flag")
	}
}

func TestRunRejectsPositionalArguments(t *testing.T) {
	r, _ := newTestRegistry()
	var ran bool
	var seen string
	r.Add(recordCommand(&ran, &seen))

	err := r.Run(context.Background(), failingAppFunc(errors.New("must not be called")), []string{"spy", "extra"})
	if !IsUsageError(err) {
		t.Fatalf("a stray argument must be reported, got %v", err)
	}
	if ran {
		t.Fatalf("the command must not run")
	}
}

func TestRunDoesNotBuildTheAppForConfiglessCommands(t *testing.T) {
	// help and version have to work before there is a valid .env, which means
	// dispatch must not touch configuration for them.
	for _, args := range [][]string{{"help"}, {"version"}, {"--help"}, {"-h"}, {"--version"}, {"-v"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r, out := newTestRegistry()
			err := r.Run(context.Background(), failingAppFunc(errors.New("config is broken")), args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Len() == 0 {
				t.Fatalf("expected output")
			}
		})
	}
}

func TestRunSurfacesAppBuildFailure(t *testing.T) {
	r, _ := newTestRegistry()
	var ran bool
	var seen string
	r.Add(recordCommand(&ran, &seen))

	want := errors.New("config: DATABASE_URL is required")
	err := r.Run(context.Background(), failingAppFunc(want), []string{"spy"})
	if !errors.Is(err, want) {
		t.Fatalf("expected the build failure to surface, got %v", err)
	}
	if IsUsageError(err) {
		t.Fatalf("a broken config is a failure, not misuse")
	}
	if ran {
		t.Fatalf("the command must not run without its dependencies")
	}
}

func TestCommandHelpShowsFlags(t *testing.T) {
	r, out := newTestRegistry()
	var ran bool
	var seen string
	r.Add(recordCommand(&ran, &seen))

	if err := r.Run(context.Background(), failingAppFunc(errors.New("must not be called")), []string{"spy", "--help"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, "-value") || !strings.Contains(body, "a value") {
		t.Fatalf("expected the flag to be documented, got %q", body)
	}
	if ran {
		t.Fatalf("--help must not run the command")
	}
}

func TestHelpForOneCommand(t *testing.T) {
	r, out := newTestRegistry()

	if err := r.Run(context.Background(), failingAppFunc(errors.New("x")), []string{"help", "db:migrate"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "db:migrate") || !strings.Contains(out.String(), "-path") {
		t.Fatalf("unexpected help output: %q", out.String())
	}
}

func TestHelpForUnknownCommand(t *testing.T) {
	r, _ := newTestRegistry()

	err := r.Run(context.Background(), failingAppFunc(errors.New("x")), []string{"help", "nope"})
	if !IsUsageError(err) {
		t.Fatalf("expected a usage error, got %v", err)
	}
}

func TestBuiltInCommandsAreRegistered(t *testing.T) {
	r, _ := newTestRegistry()

	want := []string{
		"help", "version",
		"db:migrate", "db:seed", "db:status", "db:fresh", "db:dump",
		"cron:prune-sessions", "cron:prune-refresh-tokens",
	}
	for _, name := range want {
		if _, ok := r.Lookup(name); !ok {
			t.Fatalf("expected %q to be registered; have %v", name, r.Names())
		}
	}
	// serve is registered by cmd/app, because it is the only command that
	// needs the HTTP layer.
	if _, ok := r.Lookup("serve"); ok {
		t.Fatalf("serve must not be registered by internal/console")
	}
}

func TestHelpGroupsEveryCommand(t *testing.T) {
	r, out := newTestRegistry()
	r.PrintHelp(out)
	body := out.String()

	for _, name := range r.Names() {
		if !strings.Contains(body, name) {
			t.Fatalf("help omitted %q", name)
		}
	}
	for _, group := range []string{GroupDatabase, GroupMaintenance, GroupOther} {
		if !strings.Contains(body, group) {
			t.Fatalf("help omitted the %q group", group)
		}
	}
}

func TestUsageErrorIsDistinct(t *testing.T) {
	if IsUsageError(nil) {
		t.Fatalf("nil is not a usage error")
	}
	if IsUsageError(errors.New("plain")) {
		t.Fatalf("a plain error is not a usage error")
	}
	if !IsUsageError(usagef("bad %s", "input")) {
		t.Fatalf("expected a usage error")
	}
	if got := usagef("bad %s", "input").Error(); got != "bad input" {
		t.Fatalf("unexpected message: %q", got)
	}
}

func TestDescribeBuildAlwaysNamesTheApp(t *testing.T) {
	if got := describeBuild(); !strings.HasPrefix(got, "Go Starter ") {
		t.Fatalf("unexpected build description: %q", got)
	}

	version = "1.2.3"
	t.Cleanup(func() { version = "" })
	if got := describeBuild(); got != "Go Starter 1.2.3" {
		t.Fatalf("expected the linker-set version to win, got %q", got)
	}
}
