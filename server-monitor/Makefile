SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

GO ?= go
NPM ?= npm
DOCKER_COMPOSE ?= docker compose
HELM ?= helm
KUBECTL ?= kubectl
KIND ?= kind
GOLANGCI_LINT ?= golangci-lint
GOIMPORTS ?= goimports

PROJECT_DIR := $(CURDIR)
ROOT_DIR := $(abspath $(PROJECT_DIR)/..)
FRONTEND_DIR := frontend
CHART_DIR := charts/server-monitor
PLATFORM_CHART_DIR := charts/cloudops-kind-platform
DEMO_CHART_DIR := charts/cloudops-demo
MANIFEST_DIR := k8s

COMPOSE_SERVICES ?=
LOGS_SERVICE ?=
HELM_RELEASE ?= server-monitor
HELM_VALUES ?= $(CHART_DIR)/values.yaml
HELM_SET_ARGS ?=
KUBE_NAMESPACE ?= server-monitor
K8S_APPLY_ARGS ?=
KIND_CLUSTER_NAME ?= cloudops-test

LOCAL_PROMETHEUS_URL ?= http://localhost:9090
LOCAL_REDIS_ADDR ?= localhost:6379
LOCAL_REDIS_PASSWORD ?= server-monitor-local-redis
LOCAL_MYSQL_HOST ?= localhost
LOCAL_MYSQL_PORT ?= 3306
LOCAL_MYSQL_DATABASE ?= server_monitor
LOCAL_MYSQL_USER ?= server_monitor
LOCAL_MYSQL_PASSWORD ?= server-monitor-local-mysql
LOCAL_KAFKA_BROKERS ?= localhost:9092
LOCAL_JWT_SECRET ?= server-monitor-local-dev-jwt-secret-change-me
LOCAL_ADMIN_PASSWORD ?= server-monitor-local-admin
LOCAL_TRACE_OTLP_ENDPOINT ?= localhost:4317
LOCAL_RUNBOOK_DIR ?= $(PROJECT_DIR)/runbooks

.PHONY: help demo-v2 demo-v2-check demo-v2-clean \
	env-init frontend-install clean \
	build build-go build-frontend build-api build-worker build-migrate build-demo build-server-web \
	test test-go test-frontend test-server-web test-pkg test-k8s \
	migrate-up \
	vet vet-server-web vet-pkg \
	lint lint-go lint-frontend lint-server-web lint-pkg \
	fmt fmt-go fmt-goimports check-goimports check ci \
	dev-deps dev-stop dev-web dev-frontend \
	compose-config compose-build compose-migrate compose-up compose-down compose-logs compose-ps compose-clean \
	helm-lint helm-template deploy-helm undeploy-helm deploy-k8s undeploy-k8s k8s-script-check k8s-setup k8s-teardown \
	preflight demo-up demo-down kind-render kind-check demo-bootstrap-pr scenario-open-regression-pr e2e-gitops golden-e2e-contracts \
	docker docker-up docker-down docker-logs docker-clean ci-check dev-server-web

define require_cmd
	@command -v $(1) >/dev/null 2>&1 || { echo "缺少命令: $(1)"; exit 1; }
endef

# ============================================
# 初始化
# ============================================

env-init: ## 初始化 .env（若不存在则从 .env.example 复制）
	@if [ ! -f .env ]; then cp .env.example .env; fi
	@echo ".env 已就绪"

demo-v2: ## 运行受 local-demo/disposable 保护的 V2 完整演示链路
	bash scripts/run-v2-demo.sh

demo-v2-check: ## 只检查 Demo 依赖、Compose、Helm、端口和必需文件
	bash scripts/run-v2-demo.sh --check

demo-v2-clean: ## 只清理 cloudops-v2-demo Compose 项目和 cloudops-demo kind 集群
	bash scripts/run-v2-demo.sh --clean

frontend-install: ## 安装前端依赖
	cd $(FRONTEND_DIR) && $(NPM) ci --no-audit --prefer-offline

# ============================================
# 构建
# ============================================

build: build-go build-frontend ## 构建全部可执行产物

build-go: build-api build-worker build-migrate build-demo ## 构建 root Go 进程

build-api: ## 构建 cloudops-api
	$(MAKE) -C $(ROOT_DIR) build-api

build-worker: ## 构建 cloudops-worker
	$(MAKE) -C $(ROOT_DIR) build-worker

build-migrate: ## 构建 cloudops-migrate
	$(MAKE) -C $(ROOT_DIR) build-migrate

build-demo: ## 构建 cloudops-demo
	$(MAKE) -C $(ROOT_DIR) build-demo

build-server-web: build-api ## 兼容旧目标名；只构建 cloudops-api

build-frontend: ## 构建前端静态资源
	cd $(FRONTEND_DIR) && $(NPM) run build

# ============================================
# 测试
# ============================================

test: test-go test-frontend ## 运行项目测试与前端类型检查

test-go: ## 从 root module 运行所有 Go 测试
	cd $(ROOT_DIR) && $(GO) test -count=1 ./...

test-server-web: test-go ## 兼容旧目标名；运行 root module 测试

test-pkg: test-go ## 兼容旧目标名；pkg 已吸收到 root module

test-k8s: ## 运行 root module 内的 K8s 相关测试
	cd $(ROOT_DIR) && $(GO) test ./internal/infra/k8sread ./internal/infra/k8schange ./internal/handler/ -run TestK8s

migrate-up: ## 使用 root cloudops-migrate 执行 forward up
	cd $(ROOT_DIR) && $(GO) run ./cmd/cloudops-migrate up

test-frontend: ## 运行前端类型检查
	cd $(FRONTEND_DIR) && npx vue-tsc --noEmit

vet: ## 从 root module 运行 Go vet
	cd $(ROOT_DIR) && $(GO) vet ./...

vet-server-web: vet ## 兼容旧目标名

vet-pkg: vet ## 兼容旧目标名

# ============================================
# 格式与校验
# ============================================

fmt: fmt-go ## 格式化 Go 代码

fmt-go: ## 使用 go fmt 格式化 Go 模块
	cd $(ROOT_DIR) && $(GO) fmt ./cmd/... ./internal/... ./migrations/...

fmt-goimports: ## 使用 goimports 格式化 Go 代码
	$(call require_cmd,$(GOIMPORTS))
	cd $(ROOT_DIR) && $(GOIMPORTS) -w cmd internal migrations

check-goimports: ## 检查 Go imports 是否规范
	$(call require_cmd,$(GOIMPORTS))
	$(MAKE) -C $(ROOT_DIR) check-goimports

lint: lint-go lint-frontend ## 运行 Go 与前端静态检查

lint-go: ## 从 root module 运行 Go lint
	$(MAKE) -C $(ROOT_DIR) lint-go

lint-server-web: lint-go ## 兼容旧目标名

lint-pkg: lint-go ## 兼容旧目标名

lint-frontend: ## 运行前端 ESLint
	cd $(FRONTEND_DIR) && $(NPM) run lint

compose-config: ## 展开并检查 Compose 配置
	$(DOCKER_COMPOSE) config >/tmp/server-monitor-compose.yaml
	@echo "Compose 配置已输出到 /tmp/server-monitor-compose.yaml"

helm-lint: ## 校验 Helm Chart
	$(call require_cmd,$(HELM))
	$(HELM) lint $(CHART_DIR)
	$(HELM) lint $(PLATFORM_CHART_DIR)
	$(HELM) lint $(DEMO_CHART_DIR)

kind-render: ## 渲染并检查 V3 Phase 3 kind/Helm profile
	$(MAKE) -C $(ROOT_DIR) kind-render

preflight: ## 检查 V3 disposable kind 前置条件
	$(MAKE) -C $(ROOT_DIR) preflight

demo-up: ## 启动 V3 Phase 3 kind + Helm 可观测性演示
	$(MAKE) -C $(ROOT_DIR) demo-up

scenario-open-regression-pr: ## 使用本机 human gh 身份创建固定 regression PR（不合并）
	$(MAKE) -C $(ROOT_DIR) scenario-open-regression-pr

e2e-gitops: ## 运行真实 GitHub/Argo/kind/LLM/OAuth/App Golden 验证并导出证据
	$(MAKE) -C $(ROOT_DIR) e2e-gitops

golden-e2e-contracts: ## 仅验证 Golden harness 合同，不访问或修改外部系统
	$(MAKE) -C $(ROOT_DIR) golden-e2e-contracts

demo-bootstrap-pr: ## 生成固定五文件 bootstrap 分支并创建 draft PR；绝不 merge
	$(MAKE) -C $(ROOT_DIR) demo-bootstrap-pr

kind-check: ## 校验运行中的 V3 Phase 3 metrics target/rule
	$(MAKE) -C $(ROOT_DIR) kind-check

demo-down: ## 仅删除 V3 Phase 3 disposable kind 集群
	$(MAKE) -C $(ROOT_DIR) demo-down

k8s-script-check: ## 检查 docker/setup-k8s.sh 语法
	bash -n docker/setup-k8s.sh

check: check-goimports vet lint test compose-config helm-lint k8s-script-check ## 运行本地完整校验

ci: check build-frontend ## 执行与 CI 对齐的本地校验入口

# ============================================
# 本地开发
# ============================================

dev-deps: ## 启动本地开发依赖（Redis/MySQL/Kafka/Prometheus/AlertManager/Grafana/Jaeger）
	$(DOCKER_COMPOSE) up -d redis mysql kafka kafka-init prometheus alertmanager grafana jaeger
	@echo "依赖服务已启动：Prometheus=http://localhost:9090 Grafana=http://localhost:3000"

dev-web: ## 本地运行 server-web（需先启动 make dev-deps）
	cd $(ROOT_DIR) && \
	PROMETHEUS_URL=$(LOCAL_PROMETHEUS_URL) \
	REDIS_ADDR=$(LOCAL_REDIS_ADDR) \
	REDIS_PASSWORD=$(LOCAL_REDIS_PASSWORD) \
	MYSQL_HOST=$(LOCAL_MYSQL_HOST) \
	MYSQL_PORT=$(LOCAL_MYSQL_PORT) \
	MYSQL_DATABASE=$(LOCAL_MYSQL_DATABASE) \
	MYSQL_USER=$(LOCAL_MYSQL_USER) \
	MYSQL_PASSWORD=$(LOCAL_MYSQL_PASSWORD) \
	JWT_SECRET=$(LOCAL_JWT_SECRET) \
	ADMIN_PASSWORD=$(LOCAL_ADMIN_PASSWORD) \
	KAFKA_BROKERS=$(LOCAL_KAFKA_BROKERS) \
	TRACE_OTLP_ENDPOINT=$(LOCAL_TRACE_OTLP_ENDPOINT) \
	RUNBOOK_DIR=$(LOCAL_RUNBOOK_DIR) \
	GIN_MODE=debug \
	$(GO) run ./cmd/cloudops-api

dev-frontend: ## 启动前端开发服务器
	cd $(FRONTEND_DIR) && $(NPM) run dev

dev-stop: ## 停止本地开发依赖
	$(DOCKER_COMPOSE) down

# ============================================
# Compose
# ============================================

compose-build: ## 构建 Compose 中的应用镜像
	$(DOCKER_COMPOSE) build $(COMPOSE_SERVICES)

compose-migrate: ## 启动 Compose MySQL 并执行显式 V2 Goose migrations
	$(DOCKER_COMPOSE) up -d mysql
	@set -e; \
	for attempt in $$(seq 1 60); do \
		if $(DOCKER_COMPOSE) exec -T mysql sh -ec 'mysqladmin ping -h 127.0.0.1 -u"$$MYSQL_USER" -p"$$MYSQL_PASSWORD" --silent' >/dev/null 2>&1; then break; fi; \
		if [ "$$attempt" -eq 60 ]; then echo "Compose MySQL did not become ready" >&2; exit 1; fi; \
		sleep 2; \
	done; \
	mysql_port="$$($(DOCKER_COMPOSE) port mysql 3306 | awk -F: 'END {print $$NF}')"; \
	mysql_database="$$($(DOCKER_COMPOSE) exec -T mysql printenv MYSQL_DATABASE | tr -d '\r')"; \
	mysql_user="$$($(DOCKER_COMPOSE) exec -T mysql printenv MYSQL_USER | tr -d '\r')"; \
	mysql_password="$$($(DOCKER_COMPOSE) exec -T mysql printenv MYSQL_PASSWORD | tr -d '\r')"; \
	cd $(ROOT_DIR) && MYSQL_HOST=127.0.0.1 MYSQL_PORT="$$mysql_port" MYSQL_DATABASE="$$mysql_database" MYSQL_USER="$$mysql_user" MYSQL_PASSWORD="$$mysql_password" $(GO) run ./cmd/cloudops-migrate up

compose-up: compose-migrate ## 执行 V2 migrations 后启动 Compose 完整服务
	$(DOCKER_COMPOSE) up -d $(COMPOSE_SERVICES)

compose-down: ## 停止 Compose 服务
	$(DOCKER_COMPOSE) down

compose-logs: ## 查看 Compose 日志（可选：LOGS_SERVICE=server-web）
	$(DOCKER_COMPOSE) logs -f $(LOGS_SERVICE)

compose-ps: ## 查看 Compose 服务状态
	$(DOCKER_COMPOSE) ps

compose-clean: ## 停止 Compose 并清理卷
	$(DOCKER_COMPOSE) down -v --remove-orphans

# ============================================
# 部署
# ============================================

helm-template: ## 渲染 Helm 模板
	$(call require_cmd,$(HELM))
	$(HELM) template $(HELM_RELEASE) $(CHART_DIR) --namespace $(KUBE_NAMESPACE) --values $(HELM_VALUES) $(HELM_SET_ARGS)

deploy-helm: ## 使用 Helm 部署或升级
	$(call require_cmd,$(HELM))
	$(HELM) upgrade --install $(HELM_RELEASE) $(CHART_DIR) --namespace $(KUBE_NAMESPACE) --create-namespace --values $(HELM_VALUES) $(HELM_SET_ARGS)

undeploy-helm: ## 卸载 Helm Release
	$(call require_cmd,$(HELM))
	$(HELM) uninstall $(HELM_RELEASE) --namespace $(KUBE_NAMESPACE)

deploy-k8s: ## 使用原始清单部署到 Kubernetes
	$(call require_cmd,$(KUBECTL))
	$(KUBECTL) apply -f $(MANIFEST_DIR) $(K8S_APPLY_ARGS)

undeploy-k8s: ## 删除原始 Kubernetes 清单
	$(call require_cmd,$(KUBECTL))
	$(KUBECTL) delete -f $(MANIFEST_DIR) $(K8S_APPLY_ARGS) --ignore-not-found

k8s-setup: ## 创建 kind 集群并生成本地 K8s 集成 kubeconfig
	bash docker/setup-k8s.sh

k8s-teardown: ## 删除 kind 集群
	$(KIND) delete cluster --name $(KIND_CLUSTER_NAME) 2>/dev/null || true

# ============================================
# 清理与帮助
# ============================================

clean: ## 清理本地产物
	rm -rf /tmp/cloudops-copilot-build
	rm -rf $(FRONTEND_DIR)/dist

help: ## 显示帮助信息
	@echo "Server Monitor 常用命令"
	@echo ""
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# 兼容旧命名

docker: compose-build

docker-up: compose-up

docker-down: compose-down

docker-logs: compose-logs

docker-clean: compose-clean

ci-check: ci

dev-server-web: dev-web
