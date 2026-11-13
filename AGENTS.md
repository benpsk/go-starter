# Repository Guidelines

A Go web + JSON API starter: chi, pgx, templ, htmx, Tailwind + DaisyUI.
Follow a mix of layered architecture and domain-driven design, which keeps
package boundaries clear.

## Project Structure & Module Organization

Organize by business feature under `internal/<feature>`. Each feature is a
normal collection of Go packages within the single root `go.mod`.

- `cmd/app`: single entrypoint; dispatches commands and registers `serve`.
- `internal/app`: composition root; lazily constructs repositories and services.
- `internal/appsvc`: concrete service aggregate passed to the composition layer.
- `internal/console`: command transport (`db:*`, `cron:*`, `help`, `version`).
- `internal/server`: HTTP lifecycle, global middleware, static files, and feature
  route mounting. Feature handlers live with their features.
- `internal/<feature>`: models, concrete `Service`, business rules, domain
  errors, and the `Repository` interface consumed by that service.
- `internal/<feature>/postgres`: SQL repository implementation and database tests.
- `internal/<feature>/api`: JSON handlers, request/response types, routes, and
  feature-specific middleware and tests.
- `internal/<feature>/web`: browser handlers, routes, page props, templ files,
  and browser feature tests. Create only the adapters a feature actually needs.
- `internal/postgres`: shared connection, querier, transaction, and migration
  infrastructure. No feature repositories.
- `internal/api`: shared API envelope, request parsing, and common API routing.
- `internal/web`: shared browser middleware, rendering, layouts, components,
  assets, and the static home/about/error pages.
- `internal/auth/user`: browser session cookie manager.
- `internal/apperror`, `internal/httperror`, `internal/ratelimit`,
  `internal/authctx`, `internal/storage`: shared infrastructure.
- `internal/testutil`: test scaffolding (`.env.test`, schema setup, transactions).
- `db/migrations`, `db/seeders`: raw SQL scripts embedded into the binary.
- `static`: built CSS/JS and vendored browser files.
- `pkg`: utilities usable outside this application, such as pagination.

## Package Boundaries

- Feature root packages import neither their adapters nor database drivers.
  Adapters import the feature root; `internal/app` constructs their dependencies.
- Return concrete service types. Define narrow interfaces in consuming packages
  when a dependency needs an abstraction, including cross-feature dependencies.
- Declare repository interfaces in the feature that consumes them. Implement
  them in that feature's `postgres` subpackage and return domain types.
- Repositories take the shared `postgres.Querier`, allowing a pool or transaction.
- HTTP handlers parse requests and map responses; services enforce business rules;
  repositories implement persistence. API contracts and browser page props stay
  distinct, even when both call the same service.
- Feature route functions receive only their required services. Shared transport
  packages never import feature adapters or the application service aggregate.
- Middleware lives with its consumer: feature-specific middleware in its adapter,
  common transport middleware in `internal/api` or `internal/web`, and global
  HTTP middleware in `internal/server`.
- Construct production services only in `internal/app`. Tests construct the
  smallest real dependency graph needed for the behavior under test.
- Promote code to a shared package only when more than one package needs it.
- Split by business responsibility and dependency boundaries, not file count.
  Do not add empty adapter packages, generic repositories, or a separate go.mod
  for every feature.

### Adding a feature, end to end

1. `internal/products/service.go`: models, concrete service, repository contract.
2. `internal/products/postgres/repository.go`: SQL implementation over the shared
   `postgres.Querier`.
3. `internal/appsvc/appsvc.go`: add `Products *products.Service`.
4. `internal/app/app.go`: construct the repository and service.
5. `internal/products/api` and/or `internal/products/web`: handlers and a `Mount`
   function. Register it in `internal/server/http_router.go` within the common
   API or browser router, so it inherits the appropriate middleware.
6. Add service tests with repository stubs, real repository tests, and selected
   module feature tests. Reserve full-application tests for routing and important
   cross-feature behavior.

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
- `make migrate` / `make seed` / `make status` / `make fresh`: database commands.
- `make cmd ARGS="db:status"`: run any console command.
- `make serve`: run the HTTP server without the asset watchers.
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
- Use `snake_case` for every JSON field name, including nested response fields,
  pagination metadata, and validation error keys. Give every exported response
  struct field an explicit `json:"snake_case_name"` tag instead of relying on
  Go's default field-name encoding.
- Follow `plans/api.md` for the response envelope and pagination contract.
- Return errors as `*apperror.AppError` from services, describing failures with
  semantic kinds. Render them with `api.Error` / `web.Error`; HTTP status mapping
  lives in `internal/httperror`.
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
- Feature page props live beside their templates in
  `internal/<feature>/web/*_data.go` as `XxxProps` embedding the shared
  `components.PageProps`. Shared props live in `internal/web/components`.
  Handlers map domain types onto narrow view types; templ files take
  one props argument, never a growing list of scalars.
- Frontend builds use the Tailwind CLI and esbuild via `package.json`; generated
  assets go under `static/`.

## Testing Guidelines

- Test business variations in feature service tests using repository stubs.
- Test SQL, constraints, and transaction behavior against real PostgreSQL.
- Feature tests construct only their required modules, services, and middleware;
  do not boot the entire application to test one feature. Reserve full router
  tests for shared middleware, route composition, and cross-feature behavior.
- Run `go test ./internal/users` for business tests and
  `go test ./internal/users/...` for the full users feature. The same pattern
  applies to login and health. Keep race checks separate from the normal suite.
- Prepare the schema once per test binary and use rollback for ordinary cases;
  never rebuild the schema for each test.
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
- Store raw SQL migrations in `db/migrations` and seeders in `db/seeders`; files
  run lexicographically through `app db:migrate` and `app db:seed`. A deployed
  binary has no checkout beside it, so the embedded copies are the fallback.
- A file may contain multiple statements; group related changes together. Each
  file runs in one transaction, so a failure leaves neither the change nor its
  record behind.
- One table tracks both: `schema_history (kind, name, applied_at)`, keyed on
  `(kind, name)`. Avoid external migration tooling.
  - The pair is the key, not `name` alone. A seeder sharing a filename with a
    migration would otherwise be recorded as applied and silently skipped.
  - Re-applying a file updates `applied_at`, so the row reads as *last* applied.
- `db:fresh` is refused unless `APP_ENV=development`.
- `db:dump` always passes `--no-owner --no-privileges`. Keep them: without them
  the dump carries `OWNER TO` and `GRANT` statements and fails to import on a
  server that lacks those roles. Prefer `--exclude-table-data` over
  `--exclude-table` when trimming a dump, so the foreign keys still resolve.

### Migrations

- Migrations are **not** expected to be idempotent, so re-running one is
  deliberately hard: `db:migrate --force` requires `--file` and refuses to run
  outside development.
- If an applied migration is wrong, write a new migration. Reach for
  `--force` only for a migration you wrote minutes ago on your own machine, and
  prefer `db:fresh`.

### Seeders

Seeders are re-runnable, so **every seeder must be idempotent**. Running one
twice must leave the database in the same state as running it once.

- Use `insert ... on conflict do nothing`, or `on conflict (col) do update`.
  Never a bare `insert`.
- That needs something to conflict on: a table you seed must have a natural
  unique constraint. Add it in the migration, not the seeder.
- Reference rows by natural key, never by a hardcoded `id`. Identity columns
  shift between environments.
- No `truncate`, and no unqualified `delete`. A re-runnable file that clears
  tables will eventually clear real data.
- No DDL in a seeder. Schema belongs in a migration; this matters more precisely
  because seeders re-run.
- Keep a seeder independent of the others. `db:seed --force --file=X` runs X
  alone, so it cannot assume an earlier file ran in the same pass.

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

## Console Commands

- The console is a transport, not an application. `internal/console` imports
  neither `internal/server`, `internal/web`, nor `internal/api`; `serve` is
  registered from `cmd/app` so the import graph enforces that rather than
  trusting discipline.
- A command declares `NeedsApp`. `help` and `version` set it false so they work
  before there is a valid `.env`.
- Reach for the narrowest accessor a command needs: `a.Config()` opens nothing,
  `a.Pool(ctx)` opens the pool, `a.Sessions(ctx)` adds the session manager, and
  `a.Services(ctx)` builds the domain services and the object store.
- Commands return errors and never call `os.Exit`. Return a `UsageError` for
  caller mistakes (exit 2) and an ordinary error for failures (exit 1).
- Commands take flags, not positional arguments, and write to the `io.Writer`
  they are handed so tests can capture output.
- Name commands `group:verb`, and put them in the group that matches.
- New scheduled work is a `cron:` command triggered externally. Do not add an
  in-process scheduler.

## Bug Fix Notes

- Before modifying backend code, check whether the change affects an API
  contract. If it does, ask for confirmation first.
- Always return the minimal data needed in API responses.
