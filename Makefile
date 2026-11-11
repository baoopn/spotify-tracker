# Developer tasks. Run `make help` for the list.
SHELL := /bin/bash
.DEFAULT_GOAL := help

GO        ?= go
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X github.com/baoopn/spotify-tracker/internal/platform/app.Version=$(VERSION)
BIN       := bin
COMPOSE   := docker compose
PROD      := docker compose -f docker-compose.prod.yml

# Load .env for `make run-*` targets.
ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## ── Build & quality ──────────────────────────────────────────────────────────

.PHONY: web
web: ## Build the admin SPA and the public page (embedded into the api binary)
	pnpm --dir web/admin install --frozen-lockfile
	pnpm --dir web/admin build
	pnpm --dir web/public install --frozen-lockfile
	pnpm --dir web/public build

.PHONY: web-dev
web-dev: ## Run the admin SPA dev server (proxies to the API on :8080)
	pnpm --dir web/admin dev

.PHONY: web-public-dev
web-public-dev: ## Run the public page dev server (proxies to the API on :8080)
	pnpm --dir web/public dev

.PHONY: build
build: web ## Build the SPA and all binaries into ./bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/ ./cmd/...

.PHONY: test
test: ## Run unit tests with the race detector
	$(GO) test -race -count=1 ./...

.PHONY: cover
cover: ## Run tests with coverage report
	$(GO) test -count=1 -coverprofile=coverage.out ./... && $(GO) tool cover -func=coverage.out | tail -1

.PHONY: lint
lint: check-public ## Run golangci-lint, buf lint and the SPA linters
	golangci-lint run ./...
	buf lint
	pnpm --dir web/admin check
	pnpm --dir web/admin lint
	pnpm --dir web/public check
	pnpm --dir web/public lint

.PHONY: check-public
check-public: ## Fail if tracked files contain machine paths, keys or deployment details
	sh scripts/check-public.sh

.PHONY: fmt
fmt: ## Format Go code
	gofmt -w $$(git ls-files '*.go' | grep -v '^gen/')

.PHONY: proto
proto: ## Regenerate gRPC code from api/proto
	buf generate

.PHONY: tools
tools: ## Install code generation and lint tools
	$(GO) install github.com/bufbuild/buf/cmd/buf@v1.75.0
	$(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
	$(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0

## ── Local development ────────────────────────────────────────────────────────

.PHONY: up
up: ## Start the full dev stack in Docker
	$(COMPOSE) up -d --build

.PHONY: down
down: ## Stop the dev stack
	$(COMPOSE) down

.PHONY: deps
deps: ## Start only Postgres and Redis (for `make run-*`)
	$(COMPOSE) up -d postgres redis

.PHONY: logs
logs: ## Tail dev stack logs
	$(COMPOSE) logs -f --tail=100

.PHONY: run-api
run-api: ## Run the API from source against `make deps`
	$(GO) run ./cmd/api

.PHONY: run-poller
run-poller: ## Run the poller from source
	$(GO) run ./cmd/poller

.PHONY: run-enricher
run-enricher: ## Run the enricher from source
	$(GO) run ./cmd/enricher

.PHONY: migrate
migrate: ## Apply database migrations
	$(GO) run ./cmd/migrate

.PHONY: import-directus
import-directus: ## Import legacy Directus data into the dev database
	$(COMPOSE) --profile tools run --rm importer

## ── Production (any Docker host) ─────────────────────────────────────────────

.PHONY: prod-require-tag
prod-require-tag:
	@test -n "$$IMAGE_TAG" || { echo "IMAGE_TAG is not set: run e.g. IMAGE_TAG=sha-xxxxxxx make $(MAKECMDGOALS)" >&2; exit 1; }

.PHONY: prod-up
prod-up: prod-require-tag ## Pull images and start the production stack
	$(PROD) pull && $(PROD) up -d

.PHONY: prod-import
prod-import: prod-require-tag ## Run the Directus importer against production
	$(PROD) --profile tools run --rm importer

.PHONY: e2e
e2e: ## Run Playwright end-to-end tests against the dev stack (docker compose up first)
	docker compose --profile e2e run --rm e2e
