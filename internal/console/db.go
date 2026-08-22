package console

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/benpsk/go-starter/internal/app"
	"github.com/benpsk/go-starter/internal/postgres"
)

// schemaChangeTimeout bounds a migration or seed run.
const schemaChangeTimeout = 5 * time.Minute

// dumpTimeout bounds a pg_dump run, which can be much slower than a migration.
const dumpTimeout = 10 * time.Minute

func databaseCommands() []Command {
	return []Command{
		migrateCommand(),
		seedCommand(),
		statusCommand(),
		freshCommand(),
		dumpCommand(),
	}
}

func migrateCommand() Command {
	return Command{
		Name:     "db:migrate",
		Group:    GroupDatabase,
		Summary:  "Apply pending migrations",
		NeedsApp: true,
		Setup: func(fs *flag.FlagSet) RunFunc {
			path := fs.String("path", defaultMigrationsDir, "directory holding .sql migrations; falls back to the embedded copies")
			only := fs.String("file", "", "apply only this file")
			force := fs.Bool("force", false, "re-apply --file even though it is recorded (development only)")

			return func(ctx context.Context, a *app.App, out io.Writer) error {
				// Migrations are not idempotent: re-running `alter table add
				// column` fails and a bare insert duplicates. Re-applying one is
				// almost never right — a new migration is. So force is gated to
				// development and must name its file.
				if *force {
					if env := a.Config().AppEnv; env != "development" {
						return usagef("db:migrate --force refuses to run with APP_ENV=%q; write a new migration instead", env)
					}
					if *only == "" {
						return usagef("db:migrate --force requires --file; re-applying every migration is never correct")
					}
				}

				source, err := migrationSource(*path)
				if err != nil {
					return err
				}

				ctx, cancel := context.WithTimeout(ctx, schemaChangeTimeout)
				defer cancel()

				pool, err := a.Pool(ctx)
				if err != nil {
					return err
				}
				if err := postgres.EnsureHistoryTable(ctx, pool); err != nil {
					return err
				}

				applied, err := source.apply(ctx, pool, *only, *force)
				if errors.Is(err, postgres.ErrFileNotFound) {
					// Nothing was attempted, so reporting a run would mislead.
					return usagef("%s", err)
				}
				// Report what landed even when a later file failed, so a
				// partial run is not a mystery.
				reportApplied(out, "migrate", source.describe(), applied)
				return err
			}
		},
	}
}

func seedCommand() Command {
	return Command{
		Name:     "db:seed",
		Group:    GroupDatabase,
		Summary:  "Apply pending seeders",
		NeedsApp: true,
		Setup: func(fs *flag.FlagSet) RunFunc {
			path := fs.String("path", defaultSeedersDir, "directory holding .sql seeders; falls back to the embedded copies")
			only := fs.String("file", "", "apply only this file")
			force := fs.Bool("force", false, "re-apply seeders that are already recorded")
			all := fs.Bool("all", false, "with --force, re-apply every seeder")

			return func(ctx context.Context, a *app.App, out io.Writer) error {
				// Seeders are written to be idempotent, so re-running one is
				// safe. Re-running *all* of them still has to be asked for
				// explicitly, because it reads as harmless in a shell history
				// and is not.
				if *force && *only == "" && !*all {
					return usagef("db:seed --force needs --file to name one seeder, or --all to re-apply every seeder")
				}
				if *all && !*force {
					return usagef("db:seed --all only applies with --force")
				}

				source, err := seederSource(*path)
				if err != nil {
					return err
				}

				ctx, cancel := context.WithTimeout(ctx, schemaChangeTimeout)
				defer cancel()

				pool, err := a.Pool(ctx)
				if err != nil {
					return err
				}
				if err := postgres.EnsureHistoryTable(ctx, pool); err != nil {
					return err
				}

				applied, err := source.apply(ctx, pool, *only, *force)
				if errors.Is(err, postgres.ErrFileNotFound) {
					return usagef("%s", err)
				}
				reportApplied(out, "seed", source.describe(), applied)
				return err
			}
		},
	}
}

func statusCommand() Command {
	return Command{
		Name:     "db:status",
		Group:    GroupDatabase,
		Summary:  "Show applied and pending migrations and seeders",
		NeedsApp: true,
		Setup: func(fs *flag.FlagSet) RunFunc {
			path := fs.String("path", defaultMigrationsDir, "directory holding .sql migrations; falls back to the embedded copies")
			seedPath := fs.String("seed-path", defaultSeedersDir, "directory holding .sql seeders; falls back to the embedded copies")

			return func(ctx context.Context, a *app.App, out io.Writer) error {
				migrations, err := migrationSource(*path)
				if err != nil {
					return err
				}
				seeders, err := seederSource(*seedPath)
				if err != nil {
					return err
				}

				pool, err := a.Pool(ctx)
				if err != nil {
					return err
				}
				if err := postgres.EnsureHistoryTable(ctx, pool); err != nil {
					return err
				}

				// One table means both kinds can be reported together.
				total := 0
				for _, source := range []sqlSource{migrations, seeders} {
					entries, err := source.status(ctx, pool)
					if err != nil {
						return err
					}
					total += printStatus(out, source, entries)
				}
				fmt.Fprintf(out, "\n%d pending\n", total)
				return nil
			}
		},
	}
}

// printStatus writes one section and returns how many files are pending.
func printStatus(out io.Writer, source sqlSource, entries []postgres.HistoryEntry) int {
	fmt.Fprintf(out, "\n%s (source: %s)\n", source.kind, source.describe())
	if len(entries) == 0 {
		fmt.Fprintln(out, "  none found")
		return 0
	}

	pending := 0
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  STATE\tAPPLIED AT\tFILE\n")
	for _, entry := range entries {
		switch {
		case entry.Missing:
			fmt.Fprintf(tw, "  missing\t%s\t%s\n", entry.AppliedAt.Format(time.RFC3339), entry.Name)
		case entry.Applied:
			fmt.Fprintf(tw, "  applied\t%s\t%s\n", entry.AppliedAt.Format(time.RFC3339), entry.Name)
		default:
			pending++
			fmt.Fprintf(tw, "  pending\t-\t%s\n", entry.Name)
		}
	}
	tw.Flush()
	return pending
}

func freshCommand() Command {
	return Command{
		Name:     "db:fresh",
		Group:    GroupDatabase,
		Summary:  "Drop the schema, recreate it, and migrate (development only)",
		NeedsApp: true,
		Setup: func(fs *flag.FlagSet) RunFunc {
			path := fs.String("path", defaultMigrationsDir, "directory holding .sql migrations; falls back to the embedded copies")
			withSeed := fs.Bool("seed", false, "run seeders after migrating")
			seedPath := fs.String("seed-path", defaultSeedersDir, "directory holding .sql seeders; falls back to the embedded copies")

			return func(ctx context.Context, a *app.App, out io.Writer) error {
				// This drops every table. Refusing outside development is the
				// only thing standing between a typo and data loss, and it
				// matters more now that the serving binary carries this
				// command.
				if env := a.Config().AppEnv; env != "development" {
					return usagef("db:fresh refuses to run with APP_ENV=%q; it is development only", env)
				}

				// Both sources resolve before anything is dropped, so a typo in
				// --seed-path does not cost the schema.
				migrations, err := migrationSource(*path)
				if err != nil {
					return err
				}
				var seeders sqlSource
				if *withSeed {
					if seeders, err = seederSource(*seedPath); err != nil {
						return err
					}
				}

				ctx, cancel := context.WithTimeout(ctx, schemaChangeTimeout)
				defer cancel()

				pool, err := a.Pool(ctx)
				if err != nil {
					return err
				}
				if err := postgres.ResetSchema(ctx, pool); err != nil {
					return err
				}
				fmt.Fprintln(out, "fresh: schema reset")

				if err := postgres.EnsureHistoryTable(ctx, pool); err != nil {
					return err
				}
				applied, err := migrations.apply(ctx, pool, "", false)
				reportApplied(out, "migrate", migrations.describe(), applied)
				if err != nil {
					return err
				}

				if !*withSeed {
					return nil
				}
				seeded, err := seeders.apply(ctx, pool, "", false)
				reportApplied(out, "seed", seeders.describe(), seeded)
				return err
			}
		},
	}
}

// dumpFormat values accepted by --format.
const (
	dumpFormatPlain  = "plain"
	dumpFormatCustom = "custom"
)

// stringList collects a flag that may be repeated.
type stringList []string

func (l *stringList) String() string {
	if l == nil {
		return ""
	}
	return strings.Join(*l, ",")
}

func (l *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("value must not be empty")
	}
	*l = append(*l, value)
	return nil
}

func dumpCommand() Command {
	return Command{
		Name:     "db:dump",
		Group:    GroupDatabase,
		Summary:  "Dump the database with pg_dump",
		NeedsApp: true,
		Setup: func(fs *flag.FlagSet) RunFunc {
			out := fs.String("out", "", "output file (default tmp/dump-<timestamp>.<ext>)")
			format := fs.String("format", dumpFormatPlain, "output format: plain (restore with psql) or custom (restore with pg_restore)")
			schemaOnly := fs.Bool("schema-only", false, "dump the schema without data")
			dataOnly := fs.Bool("data-only", false, "dump the data without the schema")
			clean := fs.Bool("clean", false, "emit drop statements first, so the dump can be restored over an existing database")
			binary := fs.String("pg-dump-bin", "pg_dump", "pg_dump executable to run")

			var excludeTable stringList
			fs.Var(&excludeTable, "exclude-table", "omit this table entirely, schema and data (repeatable; accepts patterns)")
			var excludeTableData stringList
			fs.Var(&excludeTableData, "exclude-table-data", "keep this table's schema but omit its rows (repeatable; accepts patterns)")

			return func(ctx context.Context, a *app.App, w io.Writer) error {
				if *schemaOnly && *dataOnly {
					return usagef("db:dump: choose at most one of --schema-only or --data-only")
				}
				if *format != dumpFormatPlain && *format != dumpFormatCustom {
					return usagef("db:dump: --format must be %q or %q, got %q", dumpFormatPlain, dumpFormatCustom, *format)
				}
				// pg_dump rejects this pair, so catch it here with a message
				// that says why rather than surfacing an exec failure.
				if *clean && *dataOnly {
					return usagef("db:dump: --clean cannot be combined with --data-only; there is no schema to drop")
				}

				// Resolved up front so a missing pg_dump is a clear usage error
				// rather than an opaque exec failure.
				resolved, err := exec.LookPath(*binary)
				if err != nil {
					return usagef("db:dump: %q not found on PATH", *binary)
				}

				target := *out
				if target == "" {
					target = defaultDumpPath(*format)
				}
				if dir := filepath.Dir(target); dir != "" {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						return fmt.Errorf("create dump directory: %w", err)
					}
				}

				ctx, cancel := context.WithTimeout(ctx, dumpTimeout)
				defer cancel()

				// This command needs the DSN only, so it never opens a pool.
				//
				// --no-owner and --no-privileges keep every OWNER TO and GRANT
				// out of the dump, which is what lets it restore into any
				// server as any role without "role does not exist".
				args := []string{
					"--dbname", a.Config().Database.URL,
					"--format=" + *format,
					"--no-owner",
					"--no-privileges",
					"--file", target,
				}
				if *schemaOnly {
					args = append(args, "--schema-only")
				}
				if *dataOnly {
					args = append(args, "--data-only")
				}
				if *clean {
					// --if-exists is required by pg_dump alongside --clean, and
					// without it the drops fail on a database that lacks them.
					args = append(args, "--clean", "--if-exists")
				}
				for _, table := range excludeTable {
					args = append(args, "--exclude-table="+table)
				}
				for _, table := range excludeTableData {
					args = append(args, "--exclude-table-data="+table)
				}

				cmd := exec.CommandContext(ctx, resolved, args...)
				cmd.Stdout = w
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("pg_dump: %w", err)
				}

				fmt.Fprintf(w, "dump written: %s (%s)\n", target, describeRestore(*format))
				return nil
			}
		},
	}
}

// describeRestore names the tool that reads the format back, because a custom
// dump silently fails if you point psql at it.
func describeRestore(format string) string {
	if format == dumpFormatCustom {
		return "restore with pg_restore"
	}
	return "restore with psql"
}

func defaultDumpPath(format string) string {
	ext := ".sql"
	if format == dumpFormatCustom {
		ext = ".dump"
	}
	return filepath.Join("tmp", "dump-"+time.Now().Format("20060102-150405")+ext)
}

func reportApplied(out io.Writer, label, source string, applied []string) {
	if len(applied) == 0 {
		fmt.Fprintf(out, "%s: nothing to apply (source: %s)\n", label, source)
		return
	}
	for _, name := range applied {
		fmt.Fprintf(out, "%s: applied %s\n", label, name)
	}
	fmt.Fprintf(out, "%s: %d applied (source: %s)\n", label, len(applied), source)
}
