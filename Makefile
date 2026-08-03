SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

GO ?= go
NPM ?= npm
DOCKER ?= docker
GOIMPORTS ?= goimports
GOLANGCI_LINT ?= golangci-lint
ACTIONLINT ?= actionlint
SHELLCHECK ?= shellcheck
HELM ?= helm
KUBECONFORM ?= kubeconform

TMPDIR ?= /tmp
BUILD_DIR ?= $(TMPDIR)/cloudops-copilot-build
FRONTEND_DIR := frontend
CHART_DIR := charts/cloudops
LOCAL_LIFECYCLE := scripts/local-lifecycle.sh
RUNTIME_RENDER_CHECK := scripts/check-runtime-render.sh
NAMING_CHECK := scripts/check-first-party-naming.sh
GO_FILES := $(shell find cmd internal migrations -type f -name '*.go' -print 2>/dev/null | sort)
SHELL_FILES := $(shell find scripts -type f -name '*.sh' -print 2>/dev/null | sort)

.PHONY: help \
	local-up local-open local-status local-logs local-restart local-doctor local-down \
	local-backup local-restore local-reset scenario-up scenario-status scenario-down \
	build build-go build-api build-worker build-migrate build-demo build-frontend frontend-install \
	test test-go test-race test-frontend frontend-lint frontend-lint-budget frontend-typecheck frontend-e2e-typecheck frontend-unit frontend-e2e frontend-e2e-stable \
	vet lint lint-go check-gofmt check-goimports check-deps check-structure check-naming \
	actionlint shellcheck helm-lint helm-template helm-contracts kubeconform static-checks check \
	docker-build docker-build-api docker-build-worker docker-build-migrate docker-build-demo

define require_cmd
	@command -v $(1) >/dev/null 2>&1 || { echo "missing command: $(1)" >&2; exit 1; }
endef

local-up: ## Create or reconcile cloudops-local and print its loopback URL.
	bash $(LOCAL_LIFECYCLE) up

local-open: ## Open CloudOps in the default browser, or print its loopback URL.
	bash $(LOCAL_LIFECYCLE) open

local-status: ## Show runtime, Provider, schema, storage, and backup status without secrets.
	bash $(LOCAL_LIFECYCLE) status

local-logs: ## Read bounded logs; set COMPONENT=api|worker|migrate|mysql and LINES as needed.
	bash $(LOCAL_LIFECYCLE) logs "$(COMPONENT)"

local-restart: ## Restart API and Worker without replacing persistent data.
	bash $(LOCAL_LIFECYCLE) restart

local-doctor: ## Diagnose prerequisites, runtime, schema, Provider, port, and backup state.
	bash $(LOCAL_LIFECYCLE) doctor

local-down: ## Stop CloudOps workloads while preserving persistent data.
	bash $(LOCAL_LIFECYCLE) down

local-backup: ## Create a private checksummed database and configuration backup.
	bash $(LOCAL_LIFECYCLE) backup

local-restore: ## Restore BACKUP into an explicitly confirmed target database.
	bash $(LOCAL_LIFECYCLE) restore "$(BACKUP)"

local-reset: ## Backup first, then remove CloudOps persistent state with explicit confirmation.
	bash $(LOCAL_LIFECYCLE) reset

scenario-up: ## Start the bounded real Kubernetes fault Scenario.
	bash $(LOCAL_LIFECYCLE) scenario-up

scenario-status: ## Show Scenario resources, fault, Evidence Plane, Agent, and write-gate state.
	bash $(LOCAL_LIFECYCLE) scenario-status

scenario-down: ## Remove only Scenario runtime resources and retain CloudOps history.
	bash $(LOCAL_LIFECYCLE) scenario-down

build: build-go build-frontend ## Build all local application artifacts.

build-go: build-api build-worker build-migrate build-demo

build-api:
	@mkdir -p $(BUILD_DIR)
	$(GO) build -trimpath -o $(BUILD_DIR)/cloudops-api ./cmd/cloudops-api

build-worker:
	@mkdir -p $(BUILD_DIR)
	$(GO) build -trimpath -o $(BUILD_DIR)/cloudops-worker ./cmd/cloudops-worker

build-migrate:
	@mkdir -p $(BUILD_DIR)
	$(GO) build -trimpath -o $(BUILD_DIR)/cloudops-migrate ./cmd/cloudops-migrate

build-demo:
	@mkdir -p $(BUILD_DIR)
	$(GO) build -trimpath -o $(BUILD_DIR)/cloudops-demo ./cmd/cloudops-demo

build-frontend:
	cd $(FRONTEND_DIR) && $(NPM) run build

frontend-install:
	cd $(FRONTEND_DIR) && $(NPM) ci --no-audit --prefer-offline

test: test-go test-frontend ## Run Go and frontend tests.

test-go:
	$(GO) test -count=1 ./...

test-race:
	$(GO) test -race -count=1 ./...

test-frontend: frontend-lint-budget frontend-typecheck frontend-e2e-typecheck frontend-unit

frontend-lint:
	cd $(FRONTEND_DIR) && $(NPM) run lint

frontend-lint-budget:
	cd $(FRONTEND_DIR) && $(NPM) run lint:no-new-warnings

frontend-typecheck:
	cd $(FRONTEND_DIR) && $(NPM) exec -- vue-tsc --noEmit

frontend-e2e-typecheck:
	cd $(FRONTEND_DIR) && $(NPM) run typecheck:e2e

frontend-unit:
	cd $(FRONTEND_DIR) && $(NPM) test

frontend-e2e: ## Run deterministic frontend Playwright presentation gates.
	cd $(FRONTEND_DIR) && $(NPM) run test:e2e

frontend-e2e-stable: ## Run the stable read-only Chromium regression gate.
	cd $(FRONTEND_DIR) && $(NPM) run test:e2e:stable

vet:
	$(GO) vet ./...

lint: lint-go frontend-lint

lint-go:
	$(call require_cmd,$(GOLANGCI_LINT))
	$(GOLANGCI_LINT) run ./...

check-gofmt:
	@test -n "$(GO_FILES)"
	@unformatted="$$(gofmt -l $(GO_FILES))"; \
		test -z "$$unformatted" || { printf 'gofmt required:\n%s\n' "$$unformatted" >&2; exit 1; }

check-goimports:
	$(call require_cmd,$(GOIMPORTS))
	@test -n "$(GO_FILES)"
	@unformatted="$$($(GOIMPORTS) -l $(GO_FILES))"; \
		test -z "$$unformatted" || { printf 'goimports required:\n%s\n' "$$unformatted" >&2; exit 1; }

check-deps:
	test "$$($(GO) env GOMOD)" = "$(CURDIR)/go.mod"
	$(GO) mod tidy -diff
	$(GO) mod verify
	$(GO) list -mod=readonly ./... >/dev/null

check-structure:
	test "$$(find . -mindepth 2 -name go.mod -not -path './.git/*' -print | wc -l)" -eq 0
	! grep -En '^replace[[:space:]]' go.mod
	! grep -REn --include='*.go' '(^[[:space:]]*"|^import[[:space:]]+")(server-web|server-monitor/pkg)(/|")' cmd internal migrations
	! $(GO) list -deps ./cmd/cloudops-worker | grep -E 'github.com/05allan1213/CloudOps-Copilot/(internal/bootstrap/migrate|internal/migration|migrations)$$'
	! $(GO) list -deps ./cmd/cloudops-migrate | grep -E 'github.com/05allan1213/CloudOps-Copilot/internal/(bootstrap$$|startup|service/agentruntime|service/remediation|service/deliveryverification|agent/llm|infra/githubwrite|infra/observabilityread)'

check-naming:
	bash $(NAMING_CHECK)

actionlint:
	$(call require_cmd,$(ACTIONLINT))
	$(ACTIONLINT) -no-color

shellcheck:
	$(call require_cmd,$(SHELLCHECK))
	@test -n "$(SHELL_FILES)"
	$(SHELLCHECK) $(SHELL_FILES)

helm-lint:
	$(call require_cmd,$(HELM))
	$(HELM) lint --strict $(CHART_DIR) --values $(CHART_DIR)/values-local.yaml

helm-template:
	$(call require_cmd,$(HELM))
	$(HELM) template cloudops $(CHART_DIR) --namespace cloudops-system \
		--values $(CHART_DIR)/values-local.yaml >/dev/null

helm-contracts:
	bash $(RUNTIME_RENDER_CHECK)

kubeconform:
	$(call require_cmd,$(HELM))
	$(call require_cmd,$(KUBECONFORM))
	$(HELM) template cloudops $(CHART_DIR) --namespace cloudops-system \
		--values $(CHART_DIR)/values-local.yaml | \
		$(KUBECONFORM) -strict -summary -ignore-missing-schemas

static-checks: actionlint shellcheck helm-lint helm-contracts kubeconform check-naming

check: check-gofmt check-goimports check-deps check-structure vet lint-go test-go test-race build-go test-frontend build-frontend static-checks

docker-build: docker-build-api docker-build-worker docker-build-migrate docker-build-demo

docker-build-api:
	$(DOCKER) build --target cloudops-api -t cloudops-api:local .

docker-build-worker:
	$(DOCKER) build --target cloudops-worker -t cloudops-worker:local .

docker-build-migrate:
	$(DOCKER) build --target cloudops-migrate -t cloudops-migrate:local .

docker-build-demo:
	$(DOCKER) build --target cloudops-demo -t cloudops-demo:local .

help:
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-24s %s\n", $$1, $$2}'
