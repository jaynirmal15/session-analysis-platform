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
INFRA_SERVICES := postgres livekit otel-collector prometheus grafana

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

## check: everything CI runs — fmt-check, vet, build, test
.PHONY: check
check: fmt-check vet build test

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
