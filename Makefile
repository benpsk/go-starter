SHELL := /bin/bash

GOCACHE ?= $(PWD)/.gocache
GOENV := GOCACHE=$(GOCACHE)
BIN_DIR ?= tmp/bin
COVER_OUT ?= tmp/coverage-core.out
COVER_FILTERED_OUT ?= tmp/coverage-core-filtered.out
COVER_HTML ?= tmp/coverage-core.html
COVERAGE_MIN ?= 80.00
# Generated and wiring-only code is excluded from the coverage scope.
COVER_EXCLUDE_PKG_RE := ^github.com/benpsk/go-starter/cmd/|^github.com/benpsk/go-starter/static($$|/)|^github.com/benpsk/go-starter/internal/testutil($$|/)
COVER_EXCLUDE_FILE_RE := /internal/web/.+_templ\.go:|/static/
# With -coverpkg every test binary emits a block for every package, so the raw
# profile holds duplicates. Merge them by keeping the highest count per block,
# otherwise `go tool cover` counts each copy separately and understates totals.
COVER_MERGE := awk '$$0 !~ /^mode:/ { k = $$1 " " $$2; c = $$3 + 0; if (!(k in seen) || c > seen[k]) seen[k] = c } END { for (k in seen) print k, seen[k] }'

.PHONY: tidy fmt build assets templ css js vendor \
	test testv test-race test-cover test-cover-html test-cover-gate \
	migrate seed fresh dump migrate-test fresh-test fresh-seed-test \
	live live/templ live/server live/assets live/sync_assets \
	clean build-app build-cli build-prod

tidy: ## Download modules and prune unused dependencies
	@$(GOENV) go mod tidy

fmt: ## Format Go source files
	@$(GOENV) go fmt ./...

build: assets ## Generate assets and compile the app binary
	@mkdir -p $(BIN_DIR)
	@$(GOENV) go build -o $(BIN_DIR)/app ./cmd/app
	@$(GOENV) go build -o $(BIN_DIR)/cli ./cmd/cli

templ: ## Generate templ output
	templ generate

css: ## Build the stylesheet
	npm run build:css

js: ## Bundle the browser scripts
	npm run build:js

vendor: ## Copy vendored browser libraries into static/
	npm run vendor

assets: templ css js vendor ## Generate templ output and bundled static assets

test: ## Run tests
	@set -o pipefail; $(GOENV) go test ./... | grep -v "\[no test files\]"

testv: ## Run tests with verbose output
	@set -o pipefail; $(GOENV) go test ./... -v | grep -v "\[no test files\]"

test-race: ## Run tests with the race detector
	@set -o pipefail; $(GOENV) go test -race ./... | grep -v "\[no test files\]"

test-cover: ## Report core coverage (excludes cmd, static, testutil, and generated templ files)
	@mkdir -p tmp
	@PKGS="$$( $(GOENV) go list ./... | grep -Ev '$(COVER_EXCLUDE_PKG_RE)' )"; \
	if [ -z "$$PKGS" ]; then echo "no packages matched coverage scope"; exit 1; fi; \
	COVERPKG="$$( echo $$PKGS | tr ' ' ',' )"; \
	$(GOENV) go test $$PKGS -count=1 -covermode=set -coverpkg="$$COVERPKG" -coverprofile=$(COVER_OUT) >/dev/null; \
	{ echo "mode: set"; grep -vE '$(COVER_EXCLUDE_FILE_RE)' $(COVER_OUT) | $(COVER_MERGE) | sort; } > $(COVER_FILTERED_OUT); \
	$(GOENV) go tool cover -func=$(COVER_FILTERED_OUT) | tail -n 1

test-cover-html: test-cover ## Write an HTML coverage report
	@$(GOENV) go tool cover -html=$(COVER_FILTERED_OUT) -o $(COVER_HTML)
	@echo "wrote $(COVER_HTML)"

test-cover-gate: ## Fail if core coverage is below COVERAGE_MIN
	@mkdir -p tmp
	@PKGS="$$( $(GOENV) go list ./... | grep -Ev '$(COVER_EXCLUDE_PKG_RE)' )"; \
	if [ -z "$$PKGS" ]; then echo "no packages matched coverage scope"; exit 1; fi; \
	COVERPKG="$$( echo $$PKGS | tr ' ' ',' )"; \
	$(GOENV) go test $$PKGS -count=1 -covermode=set -coverpkg="$$COVERPKG" -coverprofile=$(COVER_OUT) >/dev/null; \
	{ echo "mode: set"; grep -vE '$(COVER_EXCLUDE_FILE_RE)' $(COVER_OUT) | $(COVER_MERGE) | sort; } > $(COVER_FILTERED_OUT); \
	PCT="$$( $(GOENV) go tool cover -func=$(COVER_FILTERED_OUT) | awk '/^total:/ { gsub("%","",$$3); print $$3 }' )"; \
	echo "core coverage: $$PCT% (min: $(COVERAGE_MIN)%)"; \
	awk -v got="$$PCT" -v min="$(COVERAGE_MIN)" 'BEGIN { if ((got + 0) < (min + 0)) exit 1 }'

migrate: ## Apply SQL migrations
	@if [ -f .env ]; then set -a; . .env; set +a; fi; \
	$(GOENV) go run ./cmd/cli migrate

seed: ## Apply SQL seeders
	@if [ -f .env ]; then set -a; . .env; set +a; fi; \
	$(GOENV) go run ./cmd/cli seed

fresh: ## Drop and recreate the schema, then migrate
	@if [ -f .env ]; then set -a; . .env; set +a; fi; \
	$(GOENV) go run ./cmd/cli fresh

dump: ## Create a database dump (requires pg_dump)
	@if [ -f .env ]; then set -a; . .env; set +a; fi; \
	$(GOENV) go run ./cmd/cli dump

migrate-test: ## Apply migrations to the test database
	@if [ -f .env.test ]; then set -a; . .env.test; set +a; fi; \
	$(GOENV) go run ./cmd/cli migrate

fresh-test: ## Reset the test database schema
	@if [ -f .env.test ]; then set -a; . .env.test; set +a; fi; \
	$(GOENV) go run ./cmd/cli fresh

fresh-seed-test: ## Reset and seed the test database
	@if [ -f .env.test ]; then set -a; . .env.test; set +a; fi; \
	$(GOENV) go run ./cmd/cli fresh -seed

# Rebuild and restart the server on any Go file change.
live/server:
	@mkdir -p tmp/server $(BIN_DIR)
	go run github.com/cosmtrek/air@v1.51.0 \
	--tmp_dir "tmp/server" \
	--build.cmd "go build -o $(BIN_DIR)/app ./cmd/app" --build.bin "$(BIN_DIR)/app" --build.delay "100" \
	--build.exclude_dir "node_modules,tmp,static" \
	--build.include_ext "go" \
	--build.stop_on_error "false" \
	--misc.clean_on_exit true

live/templ:
	templ generate --watch --proxy="http://localhost:8080" --open-browser=false -v

live/assets:
	npm run watch

# Reload the browser through the templ proxy when a built asset changes.
live/sync_assets:
	@mkdir -p tmp/sync-assets
	go run github.com/cosmtrek/air@v1.51.0 \
	--tmp_dir "tmp/sync-assets" \
	--build.cmd "templ generate --notify-proxy || true" \
	--build.bin "true" \
	--build.delay "100" \
	--build.include_dir "static" \
	--build.include_ext "js,css"

live: ## Start the local watcher stack (Go server, templ, assets)
	@if [ -f .env ]; then set -a; . .env; set +a; fi; \
	$(MAKE) -j4 live/templ live/server live/assets live/sync_assets

clean: ## Remove build and test caches
	@$(GOENV) go clean -cache -testcache
	@rm -rf $(GOCACHE) tmp

build-app: assets ## Build the app binary for Linux
	@echo "Building app binary for Linux..."
	@mkdir -p $(BIN_DIR)
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -o $(BIN_DIR)/app ./cmd/app
	@echo "Build complete: $(BIN_DIR)/app"

build-cli: ## Build the cli binary for Linux
	@echo "Building cli binary for Linux..."
	@mkdir -p $(BIN_DIR)
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -o $(BIN_DIR)/cli ./cmd/cli
	@echo "Build complete: $(BIN_DIR)/cli"

build-prod: build-app build-cli ## Build all production binaries
	@echo "All production binaries are in $(BIN_DIR)/"
