# Repository Guidelines

A Go web + JSON API starter: chi, pgx, templ, htmx, Tailwind + DaisyUI.
Follow a mix of layered architecture and domain-driven design, which keeps
package boundaries clear.

## Project Structure & Module Organization

- `cmd/app`: HTTP server entrypoint. **This is the composition root** — the only
  place repositories and services are constructed.
- `cmd/cli`: migration, seed, and maintenance entrypoint.
- `internal/server`: the HTTP composition layer — global middleware, static
  files, health endpoints, and route mounting. Nothing else.
- `internal/appsvc`: the `Services` aggregate passed to the transports.
- `internal/<domain>`: domain services (`users`, `login`, `health`, ...).
- `internal/postgres`: PostgreSQL data access and migration helpers.
- `internal/web`: browser routes, handlers, templ pages/components, web assets.
- `internal/api`: JSON API routes and handlers.
- `internal/apperror`, `internal/ratelimit`, `internal/authctx`,
  `internal/storage`: cross-cutting packages shared by more than one caller.
- `internal/auth/user`: browser session cookie manager.
- `internal/testutil`: test scaffolding (`.env.test`, schema setup, `WithTx`).
- `db/migrations`, `db/seeders`: raw SQL scripts, embedded into the binaries.
- `static/`: built CSS/JS and vendored browser files served by the app.
- `pkg/`: shared utilities usable outside this module (e.g. pagination).

Keep API and server-rendered web flows distinct. Do not mix API response
contracts with web page data unless they are intentionally shared.

## Package Boundaries

- Keep `internal/server` small. It composes global middleware, static files,
  health checks, and the mounted routers.
- Keep domain packages transport-free **and database-free**. `internal/products`
  holds product models, a `Service` interface, business rules, and a
  `Repository` interface — never HTTP handlers or SQL.
- Declare the `Repository` interface in the **domain** package that needs it.
  `internal/postgres` implements it and returns domain types.
- Repositories take `postgres.Querier`, not `*pgxpool.Pool`, so the same code
  runs against the pool in production and inside a test transaction.
- Put JSON API routes/handlers in `internal/api`, e.g. `products_routes.go` and
  `products_api_handler.go`.
- Put browser routes/handlers in `internal/web`, e.g. `products_routes.go` and
  `products_handler.go`.
- Put middleware near the transport that uses it: API-only middleware in
  `internal/api`, web-only middleware in `internal/web`, and truly global
  middleware in `internal/server`.
- Promote code to a shared package only when more than one package needs it.
- Never construct a service outside `cmd/*`. If a handler needs something new,
  add a field to `appsvc.Services` and wire it in `cmd/app/main.go`.

### Adding a domain, end to end

1. `internal/products/service.go` — models, `Service`, `Repository`, `service`,
   `NewService`.
2. `internal/postgres/products.go` — `ProductRepository` over `Querier`,
   returning `products.Product`.
3. `internal/appsvc/appsvc.go` — add `Products productsvc.Service`.
4. `cmd/app/main.go` — build the repository, build the service, assign it.
5. `internal/api/products_api_handler.go` + routes in `internal/api/routes.go`,
   and/or `internal/web/products_handler.go` + routes in `internal/web/routes.go`.
6. Tests: a service test with a repository stub, a repository test against the
   real database, and a feature test through the router.

## Build, Test, and Development Commands

- `make live`: start the watcher stack (Go server, templ, frontend assets).
- `make assets`: generate templ output and bundled static assets.
- `make build`: generate assets and compile both binaries.
- `make test`: run Go tests.
- `make testv`: run Go tests with verbose output.
- `make test-race`: run tests with the race detector.
- `make test-cover`: report core coverage.
- `make test-cover-html`: write an HTML coverage report.
- `make test-cover-gate`: fail below `COVERAGE_MIN` (80%).
- `make tidy` / `make fmt`: `go mod tidy` / `go fmt ./...`.
- `make migrate` / `make seed` / `make fresh`: database scripts via `cmd/cli`.
- `make migrate-test` / `make fresh-test` / `make fresh-seed-test`: same against
  `.env.test`.
- `make dump`: create a database dump (needs `pg_dump`).
- `make build-prod`: build production binaries (`app` and `cli`).

## Coding Style & Naming Conventions

- Follow standard Go formatting via `gofmt` (`make fmt`).
- Use idiomatic package naming (short, lower-case) and keep files focused on a
  single concern.
- Prefer dependency injection for handlers and services, to simplify testing.
- Document exported functions and types with GoDoc comments.
- Always use all lower-case for any SQL syntax.
- Prioritise consistency: follow the existing pattern, not random things.
- Don't over-engineer. Keep it as simple as possible.
- Comments explain *why*, not *what*. Delete a comment rather than let it rot.

## API Design

- Keep handlers thin; delegate business logic to services under `internal/`.
- Return errors as `*apperror.AppError` from services, and render them with
  `api.Error` / `web.Error`. Handlers do not choose status codes ad hoc.
- Wrap success payloads with `api.Success`, which enforces the `{"data": ...}`
  envelope.
- Validate incoming data before calling a service; centralise request parsing in
  `api_request_helpers.go`.
- Prefer dedicated narrow response types over reusing broader domain structs.
- For server-rendered pages, fetch the fuller shape the template needs on the
  server side rather than widening an API payload for convenience.

## View Layer

- Use templ for templating (<https://templ.guide>).
- Use htmx for SPA-like interactions.
- Use DaisyUI for every UI component, icon, and theme. Reach for raw Tailwind
  only when DaisyUI has no built-in.
- Chart.js for charts and bars.
- Page props live in `internal/web/components/*_data.go` as `XxxProps` embedding
  `PageProps`. Handlers map domain types onto narrow view types; templ files take
  one props argument, never a growing list of scalars.
- Frontend builds use the Tailwind CLI and esbuild via `package.json`; generated
  assets go under `static/`.

## Testing Guidelines

- Mainly write feature tests, including the database and everything else.
- Don't mock services or the database. A stub implementing a domain
  `Repository` interface is fine — that is a test double for a contract, not a
  fake database.
- No external test libraries: pure `go test`.
- Add table-driven tests for multiple cases.
- Repository tests live in the external `postgres_test` package so they can use
  `internal/testutil`; internal unit tests use `package postgres`.
- `testutil.SetupDB(t)` prepares the schema once per binary and **skips** when no
  database is reachable, so a fresh clone still runs the rest of the suite.
- `testutil.WithTx(t, pool)` gives a context whose querier is a transaction that
  is always rolled back. Pass it to the request under test
  (`req.WithContext(ctx)`) so the whole handler chain stays isolated.
- When an interface changes, update every stub in the tests immediately. A stale
  test double is a compile-time failure in this repo.
- Keep core coverage at or above 80% (`make test-cover-gate`).

## Database & Migrations

- Connect to PostgreSQL via the pgx pool configured in `internal/postgres`.
- Store raw SQL migrations in `db/migrations`; files run lexicographically
  through `cmd/cli`.
- A migration file may contain multiple statements; group related schema changes
  together.
- `schema_migrations` and `schema_seeders` track applied files; avoid external
  migration tooling.
- `fresh` is refused unless `APP_ENV=development`.

## Deployment & Security Notes

- Load runtime configuration (ports, DSNs, secrets) from environment variables;
  never hardcode sensitive data.
- When updating `.env.example`, always append new vars to the bottom of the file
  instead of inserting them into the middle.
- Global middleware provides request logging, panic recovery, timeouts, and
  security headers. CSRF protection is browser-only; the API authenticates with
  a bearer token instead.
- Confirm CORS settings match the client before release. Production restricts
  origins to `APP_URL`; development allows any origin.
- Rate limits are skipped for localhost outside production.

## Bug Fix Notes

- Before modifying backend code, check whether the change affects an API
  contract. If it does, ask for confirmation first.
- Always return the minimal data needed in API responses.
