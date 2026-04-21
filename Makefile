.PHONY: build test test-integration lint clean run ingest docker-build docker-build-app \
        docker-up docker-up-app docker-rebuild-app docker-down docker-pull \
        migrate migrate-down install help

GO           ?= go
BINDIR       ?= bin
COMPOSE      ?= docker compose
COMPOSE_FILE ?= docker/compose.yaml

help:
	@echo "dh-support-assistant — available targets:"
	@echo ""
	@echo "  build              Compile all Go binaries to $(BINDIR)/"
	@echo "  test               Run Go test suite (unit only)"
	@echo "  test-integration   Run integration tests (requires Docker on host)"
	@echo "  lint               go vet + gofmt check"
	@echo "  clean              Remove build artefacts"
	@echo "  run                Run the API server locally ($(GO) run ./cmd/server)"
	@echo "  ingest FILE=x      Run the ingest CLI against FILE"
	@echo "  migrate            Apply goose migrations to DH_DB_URL (ad-hoc)"
	@echo "  migrate-down       Roll back the most recent goose migration"
	@echo "  install            go install all binaries"
	@echo ""
	@echo "  docker-up          Bring up the FULL stack (app + postgres + ollama)"
	@echo "  docker-up-app      Fast cycle: app + postgres only (ollama left as-is)"
	@echo "  docker-build       Rebuild ALL service images that have build:"
	@echo "  docker-build-app   Rebuild ONLY the app image (no pulls, no ollama)"
	@echo "  docker-rebuild-app Rebuild + restart app container only"
	@echo "  docker-pull        Explicitly pull base images (postgres, ollama)"
	@echo "  docker-down        docker compose down"

build:
	@mkdir -p $(BINDIR)
	$(GO) build -o $(BINDIR)/ ./...

test:
	$(GO) test ./...

# Integration tests gated by the `integration` build tag. Require Docker
# reachable from the host — testcontainers-go provisions a fresh Postgres
# per run. Kept out of `make test` so the default suite stays hermetic
# (no daemon dependency); run this target before every commit that
# touches internal/ingest or internal/db.
test-integration:
	$(GO) test -tags integration -count=1 -timeout=5m ./internal/ingest/...

lint:
	$(GO) vet ./...
	@dirs=$$($(GO) list -f '{{.Dir}}' ./...); fmt_out=$$(gofmt -l $$dirs); if [ -n "$$fmt_out" ]; then echo "gofmt issues:"; echo "$$fmt_out"; exit 1; fi

clean:
	rm -rf $(BINDIR) dist build

run:
	$(GO) run ./cmd/server

ingest:
	@if [ -z "$(FILE)" ]; then echo "usage: make ingest FILE=path/to/export.xlsx (must live under export-data/)"; exit 2; fi
	@# Runs the ingest binary INSIDE the already-running app container so
	@# the internal: true support-net is honoured — host processes cannot
	@# reach postgres directly. The container sees export-data/ via the
	@# read-only bind mount declared in docker/compose.yaml.
	$(COMPOSE) -f $(COMPOSE_FILE) exec -T app /usr/local/bin/ingest -file=/app/export-data/$$(basename $(FILE))

# Host-side ad-hoc migrations. Requires DH_DB_URL in the environment
# pointing at a reachable Postgres (the containerised stack applies
# migrations automatically at app startup — see cmd/server/main.go —
# so this target is only for out-of-stack dev against a Postgres that
# has been published to the host, e.g. via a compose override).
migrate:
	@if [ -z "$$DH_DB_URL" ]; then echo "DH_DB_URL not set. Host-side migrate needs a reachable Postgres URL."; exit 2; fi
	$(GO) run ./cmd/server -migrate=up

migrate-down:
	@if [ -z "$$DH_DB_URL" ]; then echo "DH_DB_URL not set. Host-side migrate needs a reachable Postgres URL."; exit 2; fi
	$(GO) run ./cmd/server -migrate=down

install:
	$(GO) install ./...

# Full-stack lifecycle. `docker compose up -d` re-uses the local image cache;
# pulls only happen for missing images. Once ollama/ollama:latest is pulled
# (one-off ~3.6GB) it stays cached until you `docker compose pull` or
# `docker image rm`. The ollama-models named volume persists downloaded LLMs.
# vendor/ is regenerated from the host's already-populated module cache so
# the Docker build can stay offline. The corporate WSL network intercepts
# TLS, breaking `go mod download` inside the build container.
.PHONY: vendor
vendor:
	$(GO) mod vendor

# Full-stack lifecycle. `docker compose up -d` re-uses the local image cache;
# pulls only happen for missing images. Once ollama/ollama:latest is pulled
# (one-off ~3.6GB) it stays cached until you `docker compose pull` or
# `docker image rm`. The ollama-models named volume persists downloaded LLMs.
docker-build: vendor
	$(COMPOSE) -f $(COMPOSE_FILE) build

docker-up:
	$(COMPOSE) -f $(COMPOSE_FILE) up -d

docker-down:
	$(COMPOSE) -f $(COMPOSE_FILE) down

# Daily-dev cycle: rebuild + restart only the Go app. Skips ollama entirely
# (its container is left in whatever state it was — running, stopped, absent).
# postgres is named explicitly so depends_on is satisfied without needing
# ollama to come up.
docker-build-app: vendor
	$(COMPOSE) -f $(COMPOSE_FILE) build app

docker-up-app:
	$(COMPOSE) -f $(COMPOSE_FILE) up -d app postgres

docker-rebuild-app: docker-build-app
	$(COMPOSE) -f $(COMPOSE_FILE) up -d app

# Explicit pull. Use this when intentionally bumping a base image — never
# implicitly. Prevents a stray run from triggering a 3.6GB ollama re-fetch.
docker-pull:
	$(COMPOSE) -f $(COMPOSE_FILE) pull
