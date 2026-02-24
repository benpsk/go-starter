SHELL := /bin/bash

GOCACHE ?= $(PWD)/.gocache
GOENV := GOCACHE=$(GOCACHE)

fmt:
	@$(GOENV) go fmt ./...

build: templ css js
	@$(GOENV) go build -o app ./cmd/app

test:
	@set -o pipefail; $(GOENV) go test ./... | grep -v "\\[no test files\\]" || true

migrate:
	@if [ -f .env ]; then set -a; . .env; set +a; fi; \
	$(GOENV) go run ./cmd/cli migrate

seed:
	@if [ -f .env ]; then set -a; . .env; set +a; fi; \
	$(GOENV) go run ./cmd/cli seed

fresh:
	@if [ -f .env ]; then set -a; . .env; set +a; fi; \
	$(GOENV) go run ./cmd/cli fresh

dump:
	@if [ -f .env ]; then set -a; . .env; set +a; fi; \
	$(GOENV) go run ./cmd/cli dump

migrate-test:
	@if [ -f .env.test ]; then set -a; source .env.test; set +a; fi; \
	$(GOENV) go run ./cmd/cli migrate

fresh-test:
	@if [ -f .env.test ]; then set -a; source .env.test; set +a; fi; \
	$(GOENV) go run ./cmd/cli fresh

fresh-seed-test:
	@if [ -f .env.test ]; then set -a; source .env.test; set +a; fi; \
	$(GOENV) go run ./cmd/cli fresh -seed

css:
	npm run build:css

js:
	npm run build:js

vendor:
	npm run vendor

templ:
	templ generate

assets: templ css js vendor

live/templ:
	templ generate --watch --proxy="http://localhost:8080" --open-browser=false -v

live/server:
	go run github.com/cosmtrek/air@v1.51.0 \
	--build.cmd "go build -o tmp/bin/main ./cmd/app" --build.bin "tmp/bin/main" --build.delay "100" \
	--build.exclude_dir "node_modules" \
	--build.include_ext "go" \
	--build.stop_on_error "false" \
	--misc.clean_on_exit true

live/assets:
	npm run watch

live/sync_assets:
	go run github.com/cosmtrek/air@v1.51.0 \
	--build.cmd "templ generate --notify-proxy" \
	--build.bin "true" \
	--build.delay "100" \
	--build.exclude_dir "" \
	--build.include_dir "./static" \
	--build.include_ext "js,css"

live:
	@if [ -f .env ]; then set -a; . .env; set +a; fi; \
	$(MAKE) -j4 live/templ live/server live/assets live/sync_assets

clean:
	@$(GOENV) go clean -cache -testcache
	@rm -rf $(GOCACHE) tmp

.PHONY: build-app build-cli build-prod

build-app:
	@echo "Building app binary for Linux..."
	@mkdir -p tmp/bin
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -o app ./cmd/app
	@echo "Build complete: app"

build-cli:
	@echo "Building cli binary for Linux..."
	@mkdir -p tmp/bin
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -o cli ./cmd/cli
	@echo "Build complete: cli"

build-prod:
	@$(MAKE) templ css js vendor
	@$(MAKE) build-app
	@$(MAKE) build-cli
	@echo "Production build complete: app, cli"

.PHONY: fmt build test migrate seed fresh dump migrate-test fresh-test fresh-seed-test css js vendor templ assets live clean build-app build-cli build-prod
