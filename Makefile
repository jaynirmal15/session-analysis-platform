# Session Analysis Platform — development targets.
#
# `make help` lists everything. Two local workflows are supported:
#
#   make up          everything in containers, including both services
#   make run-local   dependencies in containers, the ingester on the host
#
# Both export telemetry to the same collector, so Prometheus and Grafana show
# the service either way.

SHELL := /bin/bash

BINARIES    := ingester queryapi
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
GO_PACKAGES := ./...
BIN_DIR     := bin

# Dependency services, i.e. everything except our own binaries.
INFRA_SERVICES := postgres migrate livekit otel-collector prometheus grafana

# The host-side view of the collector, for `run-local`. Inside compose the
# services use otel-collector:4317 instead.
LOCAL_OTLP_ENDPOINT ?= localhost:4317

.DEFAULT_GOAL := help

## help: list available targets
.PHONY: help
help:
	@echo "Session Analysis Platform"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed -e 's/## //' | awk -F': ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# --- Go ---------------------------------------------------------------------

## build: compile all binaries into bin/
.PHONY: build
build: $(addprefix build-,$(BINARIES))

.PHONY: build-%
build-%:
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$* ./cmd/$*

## test: run the test suite with the race detector
.PHONY: test
test:
	go test -race -count=1 $(GO_PACKAGES)

## vet: run go vet
.PHONY: vet
vet:
	go vet $(GO_PACKAGES)

## fmt: format all Go source
.PHONY: fmt
fmt:
	gofmt -s -w .

## fmt-check: fail if any Go source is unformatted
.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -s -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt'd:"; echo "$$unformatted"; exit 1; \
	fi

## tidy: tidy and verify the module graph
.PHONY: tidy
tidy:
	go mod tidy
	go mod verify

## test-integration: run tests that need a real PostgreSQL (uses the compose one)
.PHONY: test-integration
test-integration:
	SAP_TEST_DATABASE_URL="postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable" \
		go test -tags integration -count=1 ./...

## tidy-check: fail if go.mod/go.sum are not what `go mod tidy` produces
.PHONY: tidy-check
tidy-check:
	@# CI enforces this, so `make check` must too — otherwise a green local
	@# check and a red CI run disagree, which is how trust in `make check` dies.
	@cp go.mod go.mod.bak && cp go.sum go.sum.bak
	@go mod tidy
	@if ! diff -q go.mod go.mod.bak >/dev/null || ! diff -q go.sum go.sum.bak >/dev/null; then \
		mv go.mod.bak go.mod; mv go.sum.bak go.sum; \
		echo "go.mod/go.sum are not tidy; run 'go mod tidy'" >&2; exit 1; \
	fi
	@rm -f go.mod.bak go.sum.bak

## check: everything CI runs — fmt-check, tidy-check, vet, build, test
.PHONY: check
check: fmt-check tidy-check vet build test

# --- Migrations -------------------------------------------------------------
#
# golang-migrate, run as a container so no Go dependency is needed to apply
# schema changes (ADR-0023). `docker compose up` applies them automatically via
# the one-shot migrate service; these targets are for manual control.

POSTGRES_USER     ?= sap
POSTGRES_PASSWORD ?= sap
POSTGRES_DB       ?= sap
POSTGRES_PORT     ?= 5432

# Deferred (=) not immediate (:=), so overriding POSTGRES_* on the command line
# still reaches the URL.
MIGRATE = docker compose run --rm --entrypoint migrate migrate \
	-path=/migrations \
	-database=postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@postgres:5432/$(POSTGRES_DB)?sslmode=disable

## migrate-up: apply all pending migrations
.PHONY: migrate-up
migrate-up:
	$(MIGRATE) up

## migrate-down: roll back exactly one migration
.PHONY: migrate-down
migrate-down:
	$(MIGRATE) down 1

## migrate-reset: roll back every migration (destroys all data)
.PHONY: migrate-reset
migrate-reset:
	$(MIGRATE) down -all

## migrate-version: print the current schema version
.PHONY: migrate-version
migrate-version:
	$(MIGRATE) version

## migrate-force: clear a dirty state by pinning VERSION (see ADR-0023)
.PHONY: migrate-force
migrate-force:
	@test -n "$(VERSION)" || (echo "usage: make migrate-force VERSION=<n>" >&2; exit 1)
	$(MIGRATE) force $(VERSION)

## psql: open a psql shell on the development database
.PHONY: psql
psql:
	docker compose exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

# --- Benchmarks -------------------------------------------------------------
#
# Reproduces the measurements cited in ADR-0004 and ADR-0024.
# DESTRUCTIVE: truncates and reseeds participant_join.

## bench: run the reference-query benchmark (destroys participant_join data)
.PHONY: bench
bench:
	./benchmarks/run.sh

## bench-verify: sanity-check the currently seeded dataset
.PHONY: bench-verify
bench-verify:
	docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) \
		-f - < benchmarks/verify_seed.sql

# --- Local development ------------------------------------------------------

## up: build and start the whole stack in containers
.PHONY: up
up:
	docker compose up -d --build

## down: stop the stack, keeping volumes
.PHONY: down
down:
	docker compose down

## infra-up: start dependencies only (no ingester, no queryapi)
.PHONY: infra-up
infra-up:
	docker compose up -d $(INFRA_SERVICES)

## infra-down: stop dependencies only
.PHONY: infra-down
infra-down:
	docker compose stop $(INFRA_SERVICES)

## run-local: dependencies in compose, ingester on the host (foreground)
.PHONY: run-local
run-local: infra-up
	@# Free the port in case `make up` left the containerised one running.
	@docker compose stop ingester >/dev/null 2>&1 || true
	@echo "==> ingester on the host, exporting to $(LOCAL_OTLP_ENDPOINT)"
	SAP_OTLP_ENDPOINT=$(LOCAL_OTLP_ENDPOINT) \
	SAP_OTLP_INSECURE=true \
	SAP_ENVIRONMENT=local-host \
	go run -ldflags "$(LDFLAGS)" ./cmd/ingester

## run-local-api: dependencies in compose, queryapi on the host (foreground)
.PHONY: run-local-api
run-local-api: infra-up
	@docker compose stop queryapi >/dev/null 2>&1 || true
	@echo "==> queryapi on the host, exporting to $(LOCAL_OTLP_ENDPOINT)"
	SAP_OTLP_ENDPOINT=$(LOCAL_OTLP_ENDPOINT) \
	SAP_OTLP_INSECURE=true \
	SAP_ENVIRONMENT=local-host \
	go run -ldflags "$(LDFLAGS)" ./cmd/queryapi

## partitions: show the current partition window and runway
.PHONY: partitions
partitions:
	@docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -c \
	  "SELECT count(*) AS partitions, \
	          round(EXTRACT(EPOCH FROM (max(range_end)-now()))/86400.0, 1) AS runway_days, \
	          round(EXTRACT(EPOCH FROM (now()-min(range_start)))/86400.0, 1) AS oldest_days \
	     FROM event_raw_partition;"

## maintain: run partition maintenance by hand (idempotent)
.PHONY: maintain
maintain:
	@docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -c \
	  "SELECT * FROM maintain_event_raw_partitions();"

## alert-check: break the runway on purpose and prove the alert fires
.PHONY: alert-check
alert-check:
	./scripts/partition-alert-check.sh

## smoke: drive real LiveKit and assert the rows its webhooks produce
.PHONY: smoke
smoke:
	./scripts/webhook-smoke.sh

## ps: show stack status
.PHONY: ps
ps:
	docker compose ps

## logs: follow logs for the whole stack
.PHONY: logs
logs:
	docker compose logs -f --tail=100

## clean: remove build output, containers and volumes (destroys local data)
.PHONY: clean
clean:
	docker compose down -v --remove-orphans
	rm -rf $(BIN_DIR)
