# Go Starter

A minimal Go web + JSON API starter with a layered, domain-driven structure.

## Stack

- Go + chi
- pgx (PostgreSQL) with raw SQL migrations and seeders
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
4. Run migrations: `make migrate`
5. Start the server: `make live` (or `go run ./cmd/app serve`)

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

Wiring happens in exactly one place. `internal/app` is the composition root: it
opens the pool, builds every repository, builds every service, packs them into
`appsvc.Services`, and hands that to whichever entrypoint asked. `cmd/app` is a
dispatcher — it resolves a command and maps the outcome to an exit code.

```
cmd/app/main.go          argv -> command; registers `serve`
  ├─ internal/console    the command transport: db:*, cron:*, help, version
  └─ internal/server     global middleware, static files, /healthz, /readyz
       ├─ internal/web   browser routes, handlers, templ pages   (web.Mount)
       └─ internal/api   JSON routes and handlers                (api.Mount)

internal/app             config -> pool -> repositories -> services -> Services
internal/<domain>        Service + Repository interfaces, models, business rules
internal/postgres        Repository implementations over a shared Querier
```

Each layer only knows the one below it:

- A **domain package** (`internal/users`, `internal/login`, `internal/health`)
  declares a `Service` interface, its models, a `Repository` interface, and a
  private implementation. It imports no transport and no database driver.
- **`internal/postgres`** implements those `Repository` interfaces, one file per
  domain, and returns domain types. Repositories take `postgres.Querier`, which
  both `*pgxpool.Pool` and `pgx.Tx` satisfy.
- **Transports** (`internal/web`, `internal/api`) hold only the services they
  use. They never see a database handle.
- **`internal/server`** composes middleware and mounts the two HTTP transports.
  It constructs nothing else.
- **`internal/console`** is a third transport, serving argv the way the others
  serve HTTP. It imports neither `server`, `web`, nor `api`: `serve` is
  registered from `cmd/app`, so the import graph makes it impossible for a
  console command to reach a handler.

### Layout

- `cmd/app` — the single entrypoint: argv dispatch, and the `serve` command.
- `internal/app` — the composition root, built lazily in tiers.
- `internal/console` — the command transport (`db:*`, `cron:*`, `help`, `version`).
- `internal/server` — HTTP composition layer plus the health handler.
- `internal/appsvc` — the `Services` aggregate (aliased as `server.Services`).
- `internal/api` — JSON routes, handlers, response envelope, bearer auth.
- `internal/web` — browser routes, handlers, templ pages/components, assets,
  CSRF and session middleware.
- `internal/postgres` — repositories, connection pool, migrator.
- `internal/users`, `internal/login`, `internal/health` — domain packages.
- `internal/auth/user` — browser session cookie manager.
- `internal/apperror` — the shared error type every transport renders.
- `internal/ratelimit` — transport-agnostic fixed-window limiter.
- `internal/authctx` — the request-scoped authenticated principal.
- `internal/storage` — object storage interface with local and R2 drivers.
- `internal/config` — environment configuration and validation.
- `internal/testutil` — test scaffolding.
- `db` — embedded migrations and seeders.
- `pkg/pagination` — page-size rules for list endpoints.

## Errors and responses

Services return `*apperror.AppError`, which carries the HTTP status, a
user-facing message, an optional machine-readable code, field-level details, and
the internal cause. Transports render it:

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
  db:seed                    Apply pending seeders        --path --file --force --all
  db:status                  Applied and pending, both kinds
  db:fresh                   Drop, recreate, migrate      --seed --path --seed-path
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
keyed on the pair so a seeder can share a filename with a migration without
being mistaken for it. Re-applying updates `applied_at`, so a row reads as *last*
applied.

Seeders are written to be idempotent, so re-running one is supported:

```
app db:seed --force --file=000002_products.sql   # one seeder
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

Tests run against a real PostgreSQL database; nothing at the persistence layer is
mocked. Copy `.env.example` to `.env.test`, point `DATABASE_URL` at a scratch
database, and run `make migrate-test`.

`testutil.SetupDB(t)` prepares the schema once per test binary and **skips** the
calling test when no database is reachable, so `make test` still passes on a
fresh clone. `testutil.WithTx(t, pool)` returns a context whose querier is a
transaction that is always rolled back; pass it to the request under test with
`req.WithContext(ctx)` and the whole handler chain stays isolated.

Three layers of tests, following the three layers of code:

- **Service tests** use an in-memory stub of the domain `Repository` interface.
- **Repository tests** live in the external `postgres_test` package and exercise
  real SQL, constraints, and transactions.
- **Feature tests** drive the mounted router with real services and a real
  database, substituting only the OAuth provider.

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
