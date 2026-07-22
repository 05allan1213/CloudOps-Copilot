SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

GO ?= go
NPM ?= npm
DOCKER ?= docker
DOCKER_COMPOSE ?= docker compose
GOIMPORTS ?= goimports
GOLANGCI_LINT ?= golangci-lint
ACTIONLINT ?= actionlint
SHELLCHECK ?= shellcheck
HELM ?= helm
YQ ?= yq
JQ ?= jq
KUBECONFORM ?= kubeconform
PROMTOOL ?= promtool

TMPDIR ?= /tmp
BUILD_DIR ?= $(TMPDIR)/cloudops-copilot-build
FRONTEND_DIR := frontend
CHART_DIR := charts/cloudops
PLATFORM_CHART_DIR := server-monitor/charts/cloudops-kind-platform
DEMO_CHART_DIR := server-monitor/charts/cloudops-demo
MANIFEST_DIR := server-monitor/k8s
COMPOSE_FILE := server-monitor/docker-compose.yml
COMPOSE_ENV := server-monitor/.env.example
V3_KIND_SCRIPT := server-monitor/scripts/v3-kind.sh
PROFILE_CONTRACT_SCRIPT := server-monitor/scripts/check-cloudops-profile-render.sh
ARGO_GITOPS_CONTRACT_SCRIPT := server-monitor/scripts/check-argocd-gitops-contract.sh
GOLDEN_E2E_SCRIPT := server-monitor/scripts/golden-e2e.sh
GOLDEN_E2E_CONTRACT_SCRIPT := server-monitor/scripts/check-golden-e2e-contract.sh
GITOPS_BOOTSTRAP_SCRIPT := server-monitor/scripts/gitops-demo-bootstrap.sh
ARGO_PROJECT_MANIFEST := deploy/platform/argocd/appproject.yaml
ARGO_APPLICATION_MANIFEST := deploy/platform/argocd/application.yaml
GITOPS_HEALTHY_DIR := deploy/contracts/gitops-demo/healthy/apps/demo
GITOPS_REGRESSION_DIR := deploy/contracts/gitops-demo/regression/apps/demo
VERSION_LOCK_FILE := server-monitor/deploy/kind/versions.env
GO_FILES := $(shell find cmd internal migrations -type f -name '*.go' -print 2>/dev/null | sort)
SHELL_FILES := $(sort $(shell git ls-files '*.sh') $(GITOPS_BOOTSTRAP_SCRIPT))

.PHONY: help build build-go build-api build-worker build-migrate build-demo build-frontend \
	test test-go test-race test-frontend frontend-lint frontend-typecheck frontend-unit frontend-e2e \
	vet lint lint-go check-gofmt check-goimports check-deps check-structure \
	actionlint shellcheck helm-lint helm-template kubeconform kubeconform-chart \
	kubeconform-raw promtool compose-config static-checks check docker-build \
	docker-build-api docker-build-worker docker-build-migrate docker-build-demo frontend-install \
	preflight demo-up demo-down kind-render kind-check helm-contracts argocd-contracts \
	demo-bootstrap-pr scenario-open-regression-pr e2e-gitops golden-e2e-contracts

define require_cmd
	@command -v $(1) >/dev/null 2>&1 || { echo "missing command: $(1)" >&2; exit 1; }
endef

build: build-go build-frontend ## Build all local application artifacts.

build-go: build-api build-worker build-migrate build-demo ## Build all Go processes.

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

test-frontend: frontend-lint frontend-typecheck frontend-unit

frontend-lint:
	cd $(FRONTEND_DIR) && $(NPM) run lint

frontend-typecheck:
	cd $(FRONTEND_DIR) && $(NPM) exec -- vue-tsc --noEmit

frontend-unit:
	cd $(FRONTEND_DIR) && $(NPM) test

frontend-e2e: ## Run deterministic frontend Playwright browser gates.
	cd $(FRONTEND_DIR) && $(NPM) run test:e2e

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
	! rg -n '^replace[[:space:]]' go.mod
	! rg -n '(^[[:space:]]*"|^import[[:space:]]+")(server-web|server-monitor/pkg)(/|")' --glob '*.go' cmd internal migrations
	! rg -n 'internal/copilot/(nlu|runbook)/eval' .github/workflows
	! rg -n 'server-web/migrations|cd[[:space:]]+server-web|\./cmd/migrate|^migrate-(down|status|version):' server-monitor/Makefile server-monitor/scripts
	! rg -n 'CUTOVER_V3|state conversion|lease conversion|outbox conversion' --glob '*.go' --glob '*.sql' --glob '!**/*_test.go' cmd internal migrations
	test -z "$$($(GO) list -deps ./cmd/cloudops-worker | rg 'github.com/05allan1213/CloudOps-Copilot/(internal/bootstrap/migrate|internal/migration|migrations)$$')"
	test -z "$$($(GO) list -deps ./cmd/cloudops-worker | rg 'github.com/05allan1213/CloudOps-Copilot/internal/(startup/legacyworker|service/agentruntime|service/remediation|service/deliveryverification|infra/incidentmysql)$$')"
	! rg -n '\b(ClaimNext|ClaimDelivery|ClaimRun)\b' cmd/cloudops-worker internal/bootstrap/worker.go internal/taskhandler --glob '!**/*_test.go'
	@worker_bin="$$(mktemp "$(TMPDIR)/cloudops-worker-symbols.XXXXXX")"; \
		trap 'rm -f "$$worker_bin"' EXIT; \
		$(GO) build -trimpath -o "$$worker_bin" ./cmd/cloudops-worker; \
		test -z "$$($(GO) tool nm "$$worker_bin" | rg 'incidentmysql.*\.(ClaimNext|ClaimDelivery|ClaimRun)$$')"
	test -z "$$($(GO) list -deps ./cmd/cloudops-migrate | rg 'github.com/05allan1213/CloudOps-Copilot/internal/(bootstrap$$|startup|service/agentruntime|service/remediation|service/deliveryverification|agent/llm|infra/githubwrite|infra/observabilityread)')"

actionlint:
	$(call require_cmd,$(ACTIONLINT))
	$(ACTIONLINT) -no-color

shellcheck:
	$(call require_cmd,$(SHELLCHECK))
	@test -n "$(SHELL_FILES)"
	$(SHELLCHECK) $(SHELL_FILES)

helm-lint:
	$(call require_cmd,$(HELM))
	@for profile in phase3 phase4 phase5 phase6; do \
		$(HELM) lint --strict $(CHART_DIR) --values $(CHART_DIR)/values-$$profile.yaml; \
	done
	$(HELM) lint $(PLATFORM_CHART_DIR)
	$(HELM) lint $(DEMO_CHART_DIR)

helm-template:
	$(call require_cmd,$(HELM))
	@for profile in phase3 phase4 phase5 phase6; do \
		$(HELM) template cloudops-$$profile $(CHART_DIR) --namespace cloudops-system --values $(CHART_DIR)/values-$$profile.yaml >/dev/null; \
	done
	$(HELM) template cloudops-platform $(PLATFORM_CHART_DIR)
	$(HELM) template cloudops-demo $(DEMO_CHART_DIR) --namespace demo

helm-contracts:
	$(call require_cmd,$(HELM))
	$(call require_cmd,$(YQ))
	$(call require_cmd,jq)
	@tmp_dir="$$(mktemp -d "$(TMPDIR)/cloudops-chart-contracts.XXXXXX")"; \
		trap 'rm -rf "$$tmp_dir"' EXIT; \
		for profile in phase3 phase4 phase5 phase6; do \
			$(HELM) template cloudops-$$profile $(CHART_DIR) --namespace cloudops-system \
				--values $(CHART_DIR)/values-$$profile.yaml >"$$tmp_dir/$$profile.yaml"; \
			$(YQ) -o=json 'select(.kind != null)' "$$tmp_dir/$$profile.yaml" >"$$tmp_dir/$$profile.json"; \
		done; \
		bash $(PROFILE_CONTRACT_SCRIPT) \
			"$$tmp_dir/phase3.json" "$$tmp_dir/phase4.json" \
			"$$tmp_dir/phase5.json" "$$tmp_dir/phase6.json" $(CHART_DIR)

argocd-contracts: ## Validate canonical Argo objects and contract-only GitOps fixtures.
	$(call require_cmd,$(firstword $(YQ)))
	$(call require_cmd,$(firstword $(JQ)))
	$(call require_cmd,$(firstword $(KUBECONFORM)))
	@tmp_dir="$$(mktemp -d "$(TMPDIR)/cloudops-argocd-contracts.XXXXXX")"; \
		trap 'rm -rf "$$tmp_dir"' EXIT; \
		$(YQ) -o=json -I=0 '.' $(ARGO_PROJECT_MANIFEST) >"$$tmp_dir/appproject.json"; \
		$(YQ) -o=json -I=0 '.' $(ARGO_APPLICATION_MANIFEST) >"$$tmp_dir/application.json"; \
		$(YQ) -o=json -I=0 '.' $(CHART_DIR)/values.yaml >"$$tmp_dir/values.json"; \
		bash $(ARGO_GITOPS_CONTRACT_SCRIPT) \
			"$$tmp_dir/appproject.json" "$$tmp_dir/application.json" \
			"$$tmp_dir/values.json" $(GITOPS_HEALTHY_DIR) \
			$(GITOPS_REGRESSION_DIR) $(VERSION_LOCK_FILE); \
		$(KUBECONFORM) -strict -summary -ignore-missing-schemas \
			$(ARGO_PROJECT_MANIFEST) $(ARGO_APPLICATION_MANIFEST) \
			$(GITOPS_HEALTHY_DIR)/*.yaml $(GITOPS_REGRESSION_DIR)/*.yaml

golden-e2e-contracts: ## Validate the fail-closed Golden E2E command and evidence contracts without external writes.
	bash $(GOLDEN_E2E_CONTRACT_SCRIPT)

demo-bootstrap-pr: ## Push the fixed five-file demo bootstrap branch and create a draft PR; never merge.
	bash $(GITOPS_BOOTSTRAP_SCRIPT)

kind-render: ## Render and enforce the V3 phase3 kind/Helm profile.
	bash $(V3_KIND_SCRIPT) render

preflight: ## Check disposable kind prerequisites without changing the cluster.
	bash $(V3_KIND_SCRIPT) preflight

demo-up: ## Start the V3 phase3 disposable kind + Helm observability demo.
	bash $(V3_KIND_SCRIPT) up

scenario-open-regression-pr: ## Create (but never merge) the fixed GitOps regression PR with the human gh identity.
	bash $(GOLDEN_E2E_SCRIPT) scenario-open-regression-pr

e2e-gitops: ## Run the real-system Golden GitOps verifier and write exact-SHA evidence.
	bash $(GOLDEN_E2E_SCRIPT) run

kind-check: ## Verify the running V3 phase3 kind metrics target and rule.
	bash $(V3_KIND_SCRIPT) check

demo-down: ## Delete only the V3 phase3 disposable kind cluster.
	bash $(V3_KIND_SCRIPT) down

kubeconform: kubeconform-chart kubeconform-raw

kubeconform-chart:
	$(call require_cmd,$(HELM))
	$(call require_cmd,$(KUBECONFORM))
	@for profile in phase3 phase4 phase5 phase6; do \
		$(HELM) template cloudops-$$profile $(CHART_DIR) --namespace cloudops-system \
			--values $(CHART_DIR)/values-$$profile.yaml | \
			$(KUBECONFORM) -strict -summary -ignore-missing-schemas; \
	done
	$(HELM) template cloudops-platform $(PLATFORM_CHART_DIR) | $(KUBECONFORM) -strict -summary -ignore-missing-schemas
	$(HELM) template cloudops-demo $(DEMO_CHART_DIR) --namespace demo | $(KUBECONFORM) -strict -summary -ignore-missing-schemas

kubeconform-raw:
	$(call require_cmd,$(KUBECONFORM))
	$(KUBECONFORM) -strict -summary -ignore-missing-schemas $(MANIFEST_DIR)/*.yaml

promtool:
	$(call require_cmd,$(PROMTOOL))
	$(PROMTOOL) check config server-monitor/docker/prometheus.yml
	$(PROMTOOL) check rules server-monitor/docker/alerts.yml
	$(PROMTOOL) check rules server-monitor/docker/custom-alerts.yml

compose-config:
	$(DOCKER_COMPOSE) --env-file $(COMPOSE_ENV) -f $(COMPOSE_FILE) config --quiet

static-checks: actionlint shellcheck helm-lint helm-contracts argocd-contracts golden-e2e-contracts kubeconform promtool compose-config

check: check-gofmt check-goimports check-deps check-structure vet lint-go test-go test-race build-go test-frontend build-frontend static-checks kind-render

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
