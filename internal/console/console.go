// Package console is the command transport. Where internal/web serves browsers
// and internal/api serves JSON clients, this serves argv: command names instead
// of routes, flags instead of a request body, an exit code instead of a status.
//
// It deliberately imports neither internal/server, internal/web, nor
// internal/api. The one command that needs the HTTP layer, serve, is registered
// by cmd/app, so the import graph makes it impossible for a console command to
// reach a handler by accident.
package console

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/benpsk/go-starter/internal/app"
)

// Command groups, in the order they are listed in help.
const (
	GroupServing     = "Serving"
	GroupDatabase    = "Database"
	GroupMaintenance = "Maintenance"
	GroupOther       = "Other"
)

var groupOrder = []string{GroupServing, GroupDatabase, GroupMaintenance, GroupOther}

// RunFunc executes a command. It must not call os.Exit: returning an error lets
// the caller decide the exit code.
type RunFunc func(ctx context.Context, a *app.App, out io.Writer) error

// Command is one console command.
type Command struct {
	// Name is what the user types, e.g. "db:migrate".
	Name string
	// Group places the command in the help listing.
	Group string
	// Summary is the one-line description shown in help.
	Summary string
	// NeedsApp reports whether the command needs configuration and the
	// dependency graph. help and version do not, so they keep working before
	// there is a valid .env.
	NeedsApp bool
	// Setup registers the command's flags and returns the closure that runs
	// it. It is called for help rendering as well as execution, so flags are
	// declared exactly once.
	Setup func(fs *flag.FlagSet) RunFunc
}

// AppFunc builds the dependency graph on demand.
type AppFunc func(ctx context.Context) (*app.App, error)

// Registry holds the available commands.
type Registry struct {
	program  string
	out      io.Writer
	commands map[string]Command
}

// New builds a registry with the built-in commands registered.
func New(program string, out io.Writer) *Registry {
	r := &Registry{
		program:  program,
		out:      out,
		commands: make(map[string]Command),
	}
	r.Add(helpCommand(r))
	r.Add(versionCommand())
	for _, c := range databaseCommands() {
		r.Add(c)
	}
	for _, c := range maintenanceCommands() {
		r.Add(c)
	}
	return r
}

// Add registers a command, replacing any command of the same name.
func (r *Registry) Add(c Command) {
	if c.Group == "" {
		c.Group = GroupOther
	}
	r.commands[c.Name] = c
}

// Lookup returns a command by name.
func (r *Registry) Lookup(name string) (Command, bool) {
	c, ok := r.commands[name]
	return c, ok
}

// Names returns every registered command name, sorted.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.commands))
	for name := range r.commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Run dispatches args to a command. args excludes the program name.
//
// An empty argument list is a usage error rather than an implicit "serve", so a
// stray invocation in a script cannot start a server.
func (r *Registry) Run(ctx context.Context, appFunc AppFunc, args []string) error {
	if len(args) == 0 {
		r.PrintHelp(r.out)
		return usagef("a command is required")
	}

	name := args[0]
	switch name {
	case "-h", "--help":
		name = "help"
	case "-v", "--version":
		name = "version"
	}

	cmd, ok := r.commands[name]
	if !ok {
		return usagef("unknown command %q", name)
	}

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var flagErr bytes.Buffer
	fs.SetOutput(&flagErr)
	fs.Usage = func() {}

	run := cmd.Setup(fs)

	if err := fs.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			r.PrintCommandHelp(r.out, name)
			return nil
		}
		return usagef("%s: %s", name, strings.TrimSpace(flagErr.String()))
	}

	// Commands take flags, not positional arguments. Catching leftovers here
	// turns a typo like `db:migrate --path=x extra` into a clear error instead
	// of silently ignoring it.
	if fs.NArg() > 0 && !cmd.acceptsArgs() {
		return usagef("%s: unexpected argument %q", name, fs.Arg(0))
	}

	var a *app.App
	if cmd.NeedsApp {
		built, err := appFunc(ctx)
		if err != nil {
			return err
		}
		a = built
	}

	return run(ctx, a, r.out)
}

// acceptsArgs reports whether the command consumes positional arguments. Only
// help does, so it can name another command.
func (c Command) acceptsArgs() bool {
	return c.Name == "help"
}

// PrintHelp writes the command listing.
func (r *Registry) PrintHelp(w io.Writer) {
	fmt.Fprintf(w, "%s\n\n", describeBuild())
	fmt.Fprintf(w, "Usage:\n  %s <command> [flags]\n", r.program)

	byGroup := make(map[string][]Command)
	for _, c := range r.commands {
		byGroup[c.Group] = append(byGroup[c.Group], c)
	}

	for _, group := range groupOrder {
		commands := byGroup[group]
		if len(commands) == 0 {
			continue
		}
		sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })

		fmt.Fprintf(w, "\n%s\n", group)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, c := range commands {
			fmt.Fprintf(tw, "  %s\t%s\n", c.Name, c.Summary)
		}
		tw.Flush()
	}

	fmt.Fprintf(w, "\nRun `%s help <command>` for a command's flags.\n", r.program)
}

// PrintCommandHelp writes one command's summary and flags.
func (r *Registry) PrintCommandHelp(w io.Writer, name string) error {
	cmd, ok := r.commands[name]
	if !ok {
		return usagef("unknown command %q", name)
	}

	fmt.Fprintf(w, "%s\n\n%s\n", cmd.Name, cmd.Summary)

	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(w)
	cmd.Setup(fs)

	hasFlags := false
	fs.VisitAll(func(*flag.Flag) { hasFlags = true })
	if !hasFlags {
		fmt.Fprintf(w, "\nUsage:\n  %s %s\n", r.program, cmd.Name)
		return nil
	}

	fmt.Fprintf(w, "\nUsage:\n  %s %s [flags]\n\nFlags:\n", r.program, cmd.Name)
	fs.PrintDefaults()
	return nil
}
