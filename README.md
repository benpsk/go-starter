# Go Starter

A minimal Go web + JSON API starter organized by business feature, with explicit package boundaries.

## Stack

- Go + chi
- pgx (PostgreSQL) with raw SQL migrations and Go seeders
- templ + htmx + Tailwind CSS + DaisyUI + Chart.js
- Social login for the browser (Google/GitHub) with database-backed cookie sessions
- API auth with short-lived JWT access tokens and rotating refresh tokens
- Object storage behind one interface (local filesystem or Cloudflare R2)
- Optional Google tag (gtag.js) with htmx pageview tracking

## Quick start

1. Copy `.env.example` to `.env` and set `DATABASE_URL`. For social login set
   `GOOGLE_*` / `GITHUB_*`; for API auth set `API_ACCESS_TOKEN_SECRET`.
2. Install frontend deps: `npm install`
3. Build assets and generated templates: `make assets`
4. Start the server: `make live` (or `go run ./cmd/app serve`). Pending migrations
   run automatically before the server accepts requests; a migration failure stops startup.
   You can still run migrations separately with `make migrate`.

`.env` is loaded automatically by both binaries, so `go run ./cmd/app` works
without sourcing anything first.

## Commands

| Command | Purpose |
|---|---|
| `make live` | dev mode: templ, Go server, and asset watchers |
| `make build` | build assets and the binary into `tmp/bin/` |
| `make assets` | generate templ output and bundle static assets |
| `make test` | run tests |
| `make test-race` | run tests with the race detector |
| `make test-cover` | report core coverage |
| `make test-cover-gate` | fail below `COVERAGE_MIN` (80%) |
| `make migrate` / `seed` / `status` / `fresh` | database commands |
| `make migrate-test` / `fresh-test` / `fresh-seed-test` | same against `.env.test` |
| `make dump` | dump the database with `pg_dump` |
| `make cmd ARGS="db:status"` | run any console command |
| `make build-prod` | build the production binary |

## Architecture

This is a modular monolith: one binary and one `go.mod`, with business features
organized under `internal`. `internal/app` is the composition root: it lazily
constructs repositories and services. `cmd/app` dispatches commands and maps
outcomes to exit codes. `internal/server` mounts feature routers under the shared
browser and API middleware.

```text
internal/
  users/
    service.go             models, business rules, Repository interface
    service_test.go        business tests without HTTP or PostgreSQL
    postgres/              user SQL and database tests
    web/                   account routes, handler, props, and template
  login/
    service.go             OAuth and token operations
    postgres/              refresh-token SQL and database tests
    api/                   auth JSON routes, bearer middleware, feature tests
    web/                   browser sign-in routes, props, template, feature tests
  health/
    service.go             dependency readiness rules
    postgres/              database probe
    api/                   /healthz and /readyz
  app/                     dependency construction and lifecycle
  appsvc/                  concrete service aggregate for composition
  server/                  HTTP lifecycle, global middleware, router mounting
  console/                 command transport
  postgres/                shared pool, querier, migrations, and transaction helpers
  api/                     common JSON envelope, parsing, and API middleware
  web/                     common browser middleware, layouts, static pages, assets
```

A feature root package never imports its own adapters. Both `login/api` and
`login/postgres` import `login`; `app` constructs the service with its repository.
Feature routers receive individual services, not the entire application.
Cross-feature dependencies use narrow interfaces declared by the consumer, such
as login's `UserResolver`. Shared transport packages do not import feature
adapters, so features can be mounted and tested independently.

Create adapter subpackages only when needed. A feature does not need HTTP, SQL,
and browser packages merely to follow a directory template. Keep multiple
focused files in one package until a distinct dependency boundary is useful.

To add products:

1. Define models, a concrete service, and its repository contract in
   `internal/products`.
2. Implement SQL in `internal/products/postgres` over the shared
   `postgres.Querier`, which accepts a pool or transaction.
3. Add the service to `appsvc.Services` and construct it in `internal/app`.
4. Add `internal/products/api` and/or `internal/products/web`, then mount its
   routes from `internal/server/http_router.go` inside the common transport
   router. Put feature page props and templates together in its `web` package.

Shared infrastructure also includes configuration, application errors and HTTP
error mapping, rate limiting, authenticated context, browser sessions, storage,
and test helpers. SQL migrations remain embedded in `db`; Go seeders live in `internal/seeder`;
built frontend assets are served from `static`.

## Errors and responses

Services return `*apperror.AppError`, which carries a semantic failure kind, a
user-facing message, an optional machine-readable code, field-level details, and
the internal cause. HTTP transports map kinds to status codes through
`internal/httperror`, preserving the API response contract:

```go
// service
return apperror.WithCode(apperror.Conflict("email already used"), "email_conflict")

// handler
if err != nil { api.Error(w, err); return }
api.Success(w, http.StatusOK, payload)
```

Success responses are enveloped as `{"data": ...}` (or `{"message": ...}` for a
bare string). Errors are `{"errors": {...}}` plus an optional `"code"`. The
internal cause is logged, never sent to the client.

## Commands

One binary, one composition root. `./app` with no arguments prints the command
list and exits non-zero, so a stray invocation in a script cannot start a
server.

```
Serving
  serve                      Start the HTTP server        --addr

Database
  db:migrate                 Apply pending migrations     --path --file --force
  db:seed                    Apply pending Go seeders     --name --force --all
  db:status                  Applied and pending, both kinds
  db:fresh                   Drop, recreate, migrate      --seed --path
  db:dump                    Dump via pg_dump             --out --format --clean
                                                          --schema-only --data-only
                                                          --exclude-table --exclude-table-data

Maintenance
  cron:prune-sessions        Delete expired/revoked sessions        --older-than
  cron:prune-refresh-tokens  Delete expired/revoked refresh tokens  --older-than

Other
  help [command]             Command list, or one command's flags
  version                    Version and build information
```

Name feature commands `<feature>:<verb>` and pass input through flags. Future
commands could look like this:

```sh
# Naming examples only; these commands are not implemented by the starter.
./app products:import --file=products.csv
./app users:disable --id=123
```

Put their adapters in `internal/<feature>/console` and register them through the
command registry. Keep business logic in the feature service.

Dependencies are built lazily, so a command pays only for what it uses:

| | config | pool | services | http |
|---|---|---|---|---|
| `help`, `version` | | | | |
| `db:dump` | ✓ | | | |
| `db:*` | ✓ | ✓ | | |
| `cron:prune-sessions` | ✓ | ✓ | | |
| `cron:prune-refresh-tokens` | ✓ | ✓ | ✓ | |
| `serve` | ✓ | ✓ | ✓ | ✓ |

`help` and `version` work before there is a valid `.env`, because dispatch never
loads configuration for them.

Exit codes: `0` success, `1` the command ran and failed, `2` misuse — an unknown
command, a bad flag, or a guard refusing (`db:fresh` outside development).

### Dumping

`db:dump` always passes `--no-owner --no-privileges`, so the output contains no
`OWNER TO` or `GRANT` statements and restores into any server as any role. That
is what avoids `role "..." does not exist` on import.

```
app db:dump                                  # plain SQL, restore with psql
app db:dump --clean                          # adds guarded DROPs, so it restores
                                             #   over an existing database
app db:dump --format=custom                  # archive, restore with pg_restore
app db:dump --exclude-table=user_sessions    # omit the table entirely
app db:dump --exclude-table-data=users       # keep the schema, omit the rows
```

Both exclude flags are repeatable and accept patterns. Prefer
`--exclude-table-data` when you want a shareable dump: dropping a table outright
breaks the foreign keys that reference it, while dropping only its rows leaves a
dump that still restores cleanly.

Two format notes. A plain dump from PostgreSQL 18 contains `\restrict`
meta-commands, which only `psql` understands — use `--format=custom` if you
import with anything else. And `--clean` cannot be combined with `--data-only`,
because there is no schema to drop.

### Re-running a file

Both kinds are tracked in one table, `schema_history (kind, name, applied_at)`,
keyed on the pair so a seeder can share a name with a migration without
being mistaken for it. Re-applying updates `applied_at`, so a row reads as *last*
applied.

`./app db:seed` runs the Go functions registered in `internal/seeder/seeder.go`,
in registration order. Add seed functions there and register each with a stable
name. Seeders are compiled into the binary; rebuild after changing them.
Each function receives a context and transactional `postgres.Querier`, which can
be passed to feature repositories. Its writes and history record commit together.
The starter's `initial` seeder is empty and adds no application data.

Use `./app db:seed --name=products` to run one pending seeder.
Seeders must be idempotent; already applied names are skipped unless forced:

```
app db:seed --force --name=products   # one seeder
app db:seed --force --all                        # every seeder, asked for explicitly
```

Migrations are not idempotent, so re-running one is deliberately hard. It needs
`--file` and refuses outside development:

```
app db:migrate --force --file=000003_add_index.sql   # APP_ENV=development only
```

If an applied migration is wrong, the answer is a new migration. See AGENTS.md
for the seeder rules that make `--force` safe.

The `cron:` jobs are meant to be triggered by an external scheduler — a system
crontab, a Kubernetes CronJob — rather than an in-process loop, so there is no
scheduler to leader-elect across replicas. Nothing else deletes expired sessions
or refresh tokens, so without them those tables grow without bound.

## Endpoints

| Route | Purpose |
|---|---|
| `GET /healthz` | liveness; never touches dependencies |
| `GET /readyz` | readiness; reports database state, 503 when degraded |
| `GET /`, `/about`, `/auth/login`, `/account` | server-rendered pages |
| `POST /auth/login/{provider}` | start the browser OAuth flow |
| `GET /auth/callback/{provider}` | complete it and open a session |
| `POST /auth/logout` | clear the session |
| `POST /api/auth/login/{provider}` | client-driven PKCE exchange → token pair |
| `POST /api/auth/refresh` | rotate the refresh token |
| `POST /api/auth/logout` | revoke the refresh token |
| `GET /api/auth/me` | the authenticated user (bearer token) |

## Testing

Tests use real PostgreSQL for persistence. Set `TEST_DATABASE_URL` explicitly or
copy `.env.example` to `.env.test` and set its `DATABASE_URL` to a dedicated test
database. Test helpers read that file without changing process environment; the
application's `DATABASE_URL` is never used implicitly. No manual migration step
is needed. The role needs permission to create schemas, and `CREATEDB` for the
console tests exercising database-wide commands.

| Mode | Helper | Isolation and lifecycle |
|---|---|---|
| Ordinary repository and HTTP feature tests | `SetupDB` + `BeginTx` / `WithTx` | One migrated schema per test binary; rollback after each test |
| Real commits, multiple connections, or DDL | `CommittedDB` | One migrated schema per test; dropped during cleanup |
| Migrations | `MigrationDB` | One empty schema per test, with only history bookkeeping prepared |
| Full application / database-wide commands | Construct the required app; use `DisposableDatabase` when operations require `public` | Dedicated migrated database per test; dropped during cleanup |

Schema names include random identifiers, so packages and simultaneous suite runs
can execute in parallel on the same server without a global lock. Every pooled
connection uses the isolated schema; no `public` fallback is included. Full
application startup is reserved for composition and command tests, not ordinary
HTTP feature tests.

Packages using `SetupDB` register the suite lifecycle:

```go
func TestMain(m *testing.M) {
    os.Exit(testutil.Run(m))
}
```

`Run` closes the shared pool and drops its schema after all tests, including
parallel tests. Each disposable helper registers its own cleanup. Transaction
cleanup is automatic even on early return; the returned cleanup function can
also be deferred. Pass the returned context through requests with
`req.WithContext(ctx)`, or inject the returned transaction directly into the
repository/session manager. Transaction tests must not commit or escape through
another pool. Choose `CommittedDB` when real commits are part of the behavior.

Parallelize packages first. Use `t.Parallel()` inside a package only with isolated
transactions or disposable schemas, and no process-wide changes such as
`t.Setenv` or `t.Chdir`. Do not truncate shared tables. Keep seeds and fixtures
minimal; application seeders are not automatically run.

Service tests use repository stubs and no infrastructure. Repository tests check
SQL and constraints. HTTP feature tests mount only the required module routers,
real services, and middleware; substitute external OAuth providers. Selected full
router tests verify shared middleware and route composition.

```sh
make test-unit                    # -short: no real database setup
make test                        # regular suite; unavailable DB skips locally
make test-db                     # uncached suite; missing DB fails
make test-ci                     # uncached suite + race detector; DB required
make test-cover-gate              # core coverage must be at least 80%
go test ./internal/users          # one feature's business rules
go test ./internal/login/...      # complete feature
```

`CI=true` or `REQUIRE_TEST_DB=1` makes unavailable databases fail instead of skip.
Use `make test-ci` in CI. `-short` explicitly opts out of DB-backed tests even in
CI. For a required coverage run, use `REQUIRE_TEST_DB=1 make test-cover-gate`.
Killed test processes can leave `go_test_*` schemas or databases behind; ordinary
completion cleans them automatically. Tests that operate on hardcoded `public`
objects must use `DisposableDatabase`, never the shared package schema.

## Notes

- Web auth is social login only (Google/GitHub) plus `user_sessions`. Password
  login and cross-provider account linking are intentionally not included.
- Social login auto-creates the account on first sign-in. A *verified* email that
  already belongs to another provider is refused rather than silently merged.
- OAuth flow state and PKCE verifiers are held in memory for simplicity, which
  is correct for a single instance. Move them to shared storage before running
  multiple replicas.
- API access tokens are self-contained, so an authenticated request costs no
  database round trip. Refresh tokens are opaque, stored hashed, and rotated on
  every use; replaying a rotated token revokes the whole family.
- The refresh token is accepted from the JSON body and mirrored in an `HttpOnly`
  cookie scoped to `/api/auth`.
- CSRF protection is browser-only, applied inside `internal/web`. The API is not
  behind it because it authenticates with a bearer token rather than an ambient
  cookie.
- OAuth providers stay disabled unless both the client id and secret are set.
- Storage degrades rather than failing startup: a misconfigured driver logs the
  reason and installs a store that refuses every operation.
- In development, static assets are read from disk first and served `no-store`,
  so an asset rebuild lands on the next request. Production serves the embedded
  copies.
- `GOOGLE_TAG_ID` is optional. When set (e.g. `G-XXXXXXXXXX`), the layout injects
  gtag and `app.js` reports page views for the initial load plus `hx-boost`
  navigations and history restores.
