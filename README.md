# Go Starter

Minimal Go web starter extracted from the Tokio Focus server stack.

## Stack

- Go + chi
- pgx (PostgreSQL)
- templ
- TailwindCSS + DaisyUI
- htmx
- Chart.js
- Raw SQL migrations/seeders

## Quick start

1. Copy `.env.example` to `.env` and update `DATABASE_URL`.
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

## Notes

- `cmd/cli` provides `migrate`, `seed`, `fresh`, and `dump`.
- `fresh` is blocked unless `APP_ENV=development`.
- `make dump` requires `pg_dump` installed locally.
- Integration tests in `internal/postgres` use a real Postgres DB and load `.env.test` (copy from `.env.test.example`).
