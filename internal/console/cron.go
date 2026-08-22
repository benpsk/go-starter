package console

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/benpsk/go-starter/internal/app"
)

// defaultPruneGrace keeps recently expired or revoked rows around long enough
// to stay visible while an incident is investigated.
const defaultPruneGrace = 720 * time.Hour // 30 days

// pruneTimeout bounds a single prune run.
const pruneTimeout = 5 * time.Minute

// maintenanceCommands are the jobs meant to be triggered by an external
// scheduler — a system crontab, a Kubernetes CronJob — rather than an
// in-process loop. Each one runs, reports, and exits, so there is no scheduler
// to make leader-elect across replicas.
func maintenanceCommands() []Command {
	return []Command{
		pruneSessionsCommand(),
		pruneRefreshTokensCommand(),
	}
}

func pruneSessionsCommand() Command {
	return Command{
		Name:     "cron:prune-sessions",
		Group:    GroupMaintenance,
		Summary:  "Delete expired and revoked browser sessions",
		NeedsApp: true,
		Setup: func(fs *flag.FlagSet) RunFunc {
			olderThan := fs.Duration("older-than", defaultPruneGrace, "keep rows that expired or were revoked more recently than this")

			return func(ctx context.Context, a *app.App, out io.Writer) error {
				if *olderThan < 0 {
					return usagef("cron:prune-sessions: --older-than must not be negative")
				}

				ctx, cancel := context.WithTimeout(ctx, pruneTimeout)
				defer cancel()

				// Sessions are owned by the session manager, which needs only
				// the pool, so this never builds the domain services.
				sessions, err := a.Sessions(ctx)
				if err != nil {
					return err
				}
				removed, err := sessions.PruneExpired(ctx, *olderThan, time.Now())
				if err != nil {
					return err
				}

				fmt.Fprintf(out, "prune-sessions: removed %d session(s) older than %s\n", removed, *olderThan)
				return nil
			}
		},
	}
}

func pruneRefreshTokensCommand() Command {
	return Command{
		Name:     "cron:prune-refresh-tokens",
		Group:    GroupMaintenance,
		Summary:  "Delete expired and revoked API refresh tokens",
		NeedsApp: true,
		Setup: func(fs *flag.FlagSet) RunFunc {
			olderThan := fs.Duration("older-than", defaultPruneGrace, "keep rows that expired or were revoked more recently than this")

			return func(ctx context.Context, a *app.App, out io.Writer) error {
				if *olderThan < 0 {
					return usagef("cron:prune-refresh-tokens: --older-than must not be negative")
				}

				ctx, cancel := context.WithTimeout(ctx, pruneTimeout)
				defer cancel()

				services, err := a.Services(ctx)
				if err != nil {
					return err
				}
				removed, err := services.Login.PruneRefreshTokens(ctx, *olderThan, time.Now())
				if err != nil {
					return err
				}

				fmt.Fprintf(out, "prune-refresh-tokens: removed %d token(s) older than %s\n", removed, *olderThan)
				return nil
			}
		},
	}
}
