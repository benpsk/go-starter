# Go Starter

Minimal Go web starter extracted from the Tokio Focus server stack.

## Stack

- Go + chi
- pgx (PostgreSQL)
- templ
- TailwindCSS + DaisyUI
- htmx
- Chart.js
- Social login web auth (Google/GitHub) with db-backed cookie sessions
- API auth with short-lived JWT access tokens + db-backed rotating refresh tokens
- Optional Google tag (gtag.js) with HTMX pageview tracking
- Raw SQL migrations/seeders

## Quick start

1. Copy `.env.example` to `.env`, update `DATABASE_URL`, configure OAuth client env vars (`GOOGLE_*`, `GITHUB_*`) for social login, and set `API_ACCESS_TOKEN_SECRET` for API auth.
2. Install frontend deps: `npm install`
3. Build assets and generated templates: `make assets`
4. Run migrations: `make migrate`
5. Start server: `make live` or `go run ./cmd/app`

## Commands

- `make live` : dev mode (templ + Go + assets watchers)
- `make build` : build app binary
- `make test` : run tests
- `make migrate` / `make seed` / `make fresh`
- `make migrate-test` / `make fresh-test` / `make fresh-seed-test` : test database migration flow (loads `.env.test`)
- `make dump` : dump database using `pg_dump`

## Project structure

- `cmd/app` wires config, database, storage, router, and server startup.
- `cmd/cli` contains database maintenance commands.
- `internal/server` is the HTTP composition layer: global middleware, static files, and route mounting.
- `internal/api` owns JSON API routes and handlers.
- `internal/web` owns browser routes, handlers, templ pages/components, and web assets.
- `internal/auth` contains shared auth/session/OAuth/JWT helpers used by API and web handlers.
- `internal/postgres` contains database access and migration helpers.
- `db` contains embedded migrations and seeders.

## Package boundaries

- Keep `internal/server` small. It should compose global middleware, static files, and mounted routers.
- Keep domain packages transport-free. For example, `internal/products` should contain product models, services, stores, and business rules, not HTTP handlers.
- Put JSON API routes/handlers in `internal/api`, e.g. `products_routes.go` and `products_api_handler.go`.
- Put browser routes/handlers in `internal/web`, e.g. `products_routes.go` and `products_handler.go`.
- Put middleware near the transport that uses it: API-only middleware in `internal/api`, web-only middleware in `internal/web`, and truly global middleware in `internal/server`.
- Promote code to shared packages only when multiple packages need it, like `internal/auth`.

## Notes

- `cmd/cli` provides `migrate`, `seed`, `fresh`, and `dump`.
- `fresh` is blocked unless `APP_ENV=development`.
- `make dump` requires `pg_dump` installed locally.
- Integration tests in `internal/postgres` use a real Postgres DB and load `.env.test` (copy `.env.example` to `.env.test` and adjust `DATABASE_URL`).
- Web auth uses social login only (Google/GitHub) and `user_sessions` (db-backed cookie sessions). Password login/register is intentionally not included.
- Social login auto-creates users on first sign-in. Account linking between providers is intentionally not included in the starter v1.
- OAuth login flow state/PKCE verifier storage is in-memory for starter simplicity (single instance). Move to shared storage if you deploy multiple instances.
- Session cookie auth checks the session in DB on authenticated web requests.
- API auth uses short-lived JWT access tokens (no DB lookup on normal requests) plus rotating opaque refresh tokens stored hashed in DB (`api_refresh_tokens`).
- API endpoints: `POST /api/auth/login/{provider}`, `POST /api/auth/refresh`, `POST /api/auth/logout`, `GET /api/auth/me`.
- Refresh token is accepted from JSON body (`refresh_token`) and also mirrored in an `HttpOnly` cookie (`/api/auth` path). Cookie-based API auth flows are CSRF-sensitive; this starter skips CSRF checks for `/api/*` to keep API clients simple.
- OAuth providers are disabled unless both client id and secret are configured for each provider.
- `GOOGLE_TAG_ID` is optional. When set (for example `G-XXXXXXXXXX`), the layout injects gtag and `app.js` sends page views for initial load plus `hx-boost` navigations/history restores.
- Nav menu active state is handled client-side (`app.js`) for hard reloads, `hx-boost` navigations, and browser history restores.
