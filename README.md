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
5. Start the server: `make live` (or `go run ./cmd/app`)

`.env` is loaded automatically by both binaries, so `go run ./cmd/app` works
without sourcing anything first.

## Commands

| Command | Purpose |
|---|---|
| `make live` | dev mode: templ, Go server, and asset watchers |
| `make build` | build assets and both binaries into `tmp/bin/` |
| `make assets` | generate templ output and bundle static assets |
| `make test` | run tests |
| `make test-race` | run tests with the race detector |
| `make test-cover` | report core coverage |
| `make test-cover-gate` | fail below `COVERAGE_MIN` (80%) |
| `make migrate` / `seed` / `fresh` | database scripts |
| `make migrate-test` / `fresh-test` / `fresh-seed-test` | same against `.env.test` |
| `make dump` | dump the database with `pg_dump` |
| `make build-prod` | build production binaries |

## Architecture

Wiring happens in exactly one place. `cmd/app/main.go` is the composition root:
it loads configuration, opens the pool, builds every repository, builds every
service, packs them into `appsvc.Services`, and hands that to the router.

```
cmd/app/main.go          config -> pool -> repositories -> services -> Services
  └─ internal/server     global middleware, static files, /healthz, /readyz
       ├─ internal/web   browser routes, handlers, templ pages   (web.Mount)
       └─ internal/api   JSON routes and handlers                (api.Mount)

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
- **`internal/server`** composes middleware and mounts the two transports. It
  constructs nothing else.

### Layout

- `cmd/app` — HTTP server and composition root.
- `cmd/cli` — `migrate`, `seed`, `fresh`, `dump`.
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
