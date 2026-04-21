.PHONY: build test lint clean run ingest docker-build docker-up docker-down migrate install help

GO      ?= go
BINDIR  ?= bin
COMPOSE ?= docker compose

help:
	@echo "dh-support-assistant — available targets:"
	@echo ""
	@echo "  build          Compile all Go binaries to $(BINDIR)/"
	@echo "  test           Run Go test suite"
	@echo "  lint           go vet + gofmt check"
	@echo "  clean          Remove build artefacts"
	@echo "  run            Run the API server locally ($(GO) run ./cmd/server)"
	@echo "  ingest FILE=x  Run the ingest CLI against FILE"
	@echo "  migrate        Apply goose migrations to the configured DB"
	@echo "  install        go install all binaries"
	@echo "  docker-build   docker compose build"
	@echo "  docker-up      docker compose up -d"
	@echo "  docker-down    docker compose down"

build:
	@mkdir -p $(BINDIR)
	$(GO) build -o $(BINDIR)/ ./...

test:
	$(GO) test ./...

lint:
	$(GO) vet ./...
	@fmt_out=$$(gofmt -l .); if [ -n "$$fmt_out" ]; then echo "gofmt issues:"; echo "$$fmt_out"; exit 1; fi

clean:
	rm -rf $(BINDIR) dist build

run:
	$(GO) run ./cmd/server

ingest:
	@if [ -z "$(FILE)" ]; then echo "usage: make ingest FILE=path/to/export.xlsx"; exit 2; fi
	$(GO) run ./cmd/ingest -file=$(FILE)

migrate:
	@echo "goose migrations - configured in Phase 1 plan"

install:
	$(GO) install ./...

docker-build:
	$(COMPOSE) -f docker/compose.yaml build

docker-up:
	$(COMPOSE) -f docker/compose.yaml up -d

docker-down:
	$(COMPOSE) -f docker/compose.yaml down
