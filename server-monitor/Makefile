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
FRONTEND_DIR := frontend
SERVER_PROBE_DIR := server-probe
SERVER_WEB_DIR := server-web
ALERT_SERVICE_DIR := alert-service
PKG_DIR := pkg
CHART_DIR := charts/server-monitor
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
LOCAL_PROMETHEUS_RELOAD_URL ?= http://localhost:9090/-/reload
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
LOCAL_RULES_FILE ?= $(PROJECT_DIR)/docker/custom-alerts.yml
LOCAL_RUNBOOK_DIR ?= $(PROJECT_DIR)/runbooks

.PHONY: help \
	env-init frontend-install clean \
	build build-go build-frontend build-server-probe build-server-web build-alert-service \
	test test-go test-frontend test-server-probe test-server-web test-alert-service test-pkg test-k8s \
	migrate-up migrate-down migrate-status migrate-version \
	vet vet-server-probe vet-server-web vet-alert-service vet-pkg \
	lint lint-go lint-frontend lint-server-probe lint-server-web lint-alert-service lint-pkg \
	fmt fmt-go fmt-goimports check-goimports check ci \
	dev-deps dev-stop dev-web dev-probe dev-alert-service dev-frontend \
	compose-config compose-build compose-up compose-down compose-logs compose-ps compose-clean \
	helm-lint helm-template deploy-helm undeploy-helm deploy-k8s undeploy-k8s k8s-script-check k8s-setup k8s-teardown \
	docker docker-up docker-down docker-logs docker-clean ci-check dev-server-web dev-server-probe dev-alert

define require_cmd
	@command -v $(1) >/dev/null 2>&1 || { echo "缺少命令: $(1)"; exit 1; }
endef

# ============================================
# 初始化
# ============================================

env-init: ## 初始化 .env（若不存在则从 .env.example 复制）
	@if [ ! -f .env ]; then cp .env.example .env; fi
	@echo ".env 已就绪"

frontend-install: ## 安装前端依赖
	cd $(FRONTEND_DIR) && $(NPM) ci --no-audit --prefer-offline

# ============================================
# 构建
# ============================================

build: build-go build-frontend ## 构建全部可执行产物

build-go: build-server-probe build-server-web build-alert-service ## 构建所有 Go 服务

build-server-probe: ## 构建 server-probe
	cd $(SERVER_PROBE_DIR) && $(GO) build -o probe .

build-server-web: ## 构建 server-web
	cd $(SERVER_WEB_DIR) && $(GO) build -o web .

build-alert-service: ## 构建 alert-service
	cd $(ALERT_SERVICE_DIR) && $(GO) build -o alert-service .

build-frontend: ## 构建前端静态资源
	cd $(FRONTEND_DIR) && $(NPM) run build

# ============================================
# 测试
# ============================================

test: test-go test-frontend ## 运行项目测试与前端类型检查

test-go: test-server-probe test-server-web test-alert-service test-pkg ## 运行所有 Go 测试

test-server-probe: ## 运行 server-probe 测试
	cd $(SERVER_PROBE_DIR) && $(GO) test ./...

test-server-web: ## 运行 server-web 测试
	cd $(SERVER_WEB_DIR) && $(GO) test ./...

test-alert-service: ## 运行 alert-service 测试
	cd $(ALERT_SERVICE_DIR) && $(GO) test ./...

test-pkg: ## 运行 pkg 测试
	cd $(PKG_DIR) && $(GO) test ./...

test-k8s: ## 运行 server-web 内的 K8s 相关测试
	cd $(SERVER_WEB_DIR) && $(GO) test ./internal/copilot/k8s/ ./internal/service/k8s/ ./internal/handler/ -run TestK8s

migrate-up: ## 应用 server-web 显式 V2 SQL migrations（使用 MYSQL_* 环境变量）
	cd $(SERVER_WEB_DIR) && $(GO) run ./cmd/migrate up

migrate-down: ## 回滚一个 server-web V2 SQL migration
	cd $(SERVER_WEB_DIR) && $(GO) run ./cmd/migrate down

migrate-status: ## 查看 server-web V2 SQL migration 状态
	cd $(SERVER_WEB_DIR) && $(GO) run ./cmd/migrate status

migrate-version: ## 查看 server-web V2 SQL migration 版本
	cd $(SERVER_WEB_DIR) && $(GO) run ./cmd/migrate version

test-frontend: ## 运行前端类型检查
	cd $(FRONTEND_DIR) && npx vue-tsc --noEmit

vet: vet-server-probe vet-server-web vet-alert-service vet-pkg ## 运行所有 Go vet 检查

vet-server-probe: ## 对 server-probe 运行 go vet
	cd $(SERVER_PROBE_DIR) && $(GO) vet ./...

vet-server-web: ## 对 server-web 运行 go vet
	cd $(SERVER_WEB_DIR) && $(GO) vet ./...

vet-alert-service: ## 对 alert-service 运行 go vet
	cd $(ALERT_SERVICE_DIR) && $(GO) vet ./...

vet-pkg: ## 对 pkg 运行 go vet
	cd $(PKG_DIR) && $(GO) vet ./...

# ============================================
# 格式与校验
# ============================================

fmt: fmt-go ## 格式化 Go 代码

fmt-go: ## 使用 go fmt 格式化 Go 模块
	cd $(SERVER_PROBE_DIR) && $(GO) fmt ./...
	cd $(SERVER_WEB_DIR) && $(GO) fmt ./...
	cd $(ALERT_SERVICE_DIR) && $(GO) fmt ./...
	cd $(PKG_DIR) && $(GO) fmt ./...

fmt-goimports: ## 使用 goimports 格式化 Go 代码
	$(call require_cmd,$(GOIMPORTS))
	cd $(SERVER_PROBE_DIR) && $(GOIMPORTS) -w .
	cd $(SERVER_WEB_DIR) && $(GOIMPORTS) -w .
	cd $(ALERT_SERVICE_DIR) && $(GOIMPORTS) -w .
	cd $(PKG_DIR) && $(GOIMPORTS) -w .

check-goimports: ## 检查 Go imports 是否规范
	$(call require_cmd,$(GOIMPORTS))
	cd $(SERVER_PROBE_DIR) && test -z "$$($(GOIMPORTS) -l .)"
	cd $(SERVER_WEB_DIR) && test -z "$$($(GOIMPORTS) -l .)"
	cd $(ALERT_SERVICE_DIR) && test -z "$$($(GOIMPORTS) -l .)"
	cd $(PKG_DIR) && test -z "$$($(GOIMPORTS) -l .)"

lint: lint-go lint-frontend ## 运行 Go 与前端静态检查

lint-go: lint-server-probe lint-server-web lint-alert-service lint-pkg ## 运行所有 Go 模块 lint

lint-server-probe: ## 对 server-probe 运行 golangci-lint
	$(call require_cmd,$(GOLANGCI_LINT))
	cd $(SERVER_PROBE_DIR) && $(GOLANGCI_LINT) run ./...

lint-server-web: ## 对 server-web 运行 golangci-lint
	$(call require_cmd,$(GOLANGCI_LINT))
	cd $(SERVER_WEB_DIR) && $(GOLANGCI_LINT) run ./...

lint-alert-service: ## 对 alert-service 运行 golangci-lint
	$(call require_cmd,$(GOLANGCI_LINT))
	cd $(ALERT_SERVICE_DIR) && $(GOLANGCI_LINT) run ./...

lint-pkg: ## 对 pkg 运行 golangci-lint
	$(call require_cmd,$(GOLANGCI_LINT))
	cd $(PKG_DIR) && $(GOLANGCI_LINT) run ./...

lint-frontend: ## 运行前端 ESLint
	cd $(FRONTEND_DIR) && $(NPM) run lint

compose-config: ## 展开并检查 Compose 配置
	$(DOCKER_COMPOSE) config >/tmp/server-monitor-compose.yaml
	@echo "Compose 配置已输出到 /tmp/server-monitor-compose.yaml"

helm-lint: ## 校验 Helm Chart
	$(call require_cmd,$(HELM))
	$(HELM) lint $(CHART_DIR)

k8s-script-check: ## 检查 docker/setup-k8s.sh 语法
	bash -n docker/setup-k8s.sh

check: check-goimports vet lint test compose-config helm-lint k8s-script-check ## 运行本地完整校验

ci: check build-frontend ## 执行与 CI 对齐的本地校验入口

# ============================================
# 本地开发
# ============================================

dev-deps: ## 启动本地开发依赖（Redis/MySQL/Kafka/Prometheus/AlertManager/Grafana/Jaeger/Probe）
	$(DOCKER_COMPOSE) up -d redis mysql kafka kafka-init prometheus alertmanager grafana jaeger server-probe
	@echo "依赖服务已启动：Prometheus=http://localhost:9090 Grafana=http://localhost:3000 server-probe=http://localhost:8082"

dev-web: ## 本地运行 server-web（需先启动 make dev-deps）
	cd $(SERVER_WEB_DIR) && \
	PROMETHEUS_URL=$(LOCAL_PROMETHEUS_URL) \
	PROMETHEUS_RELOAD_URL=$(LOCAL_PROMETHEUS_RELOAD_URL) \
	ALERT_RULE_SYNC_ENABLED=false \
	ALERT_RULES_FILE_PATH=$(LOCAL_RULES_FILE) \
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
	$(GO) run .

dev-probe: ## 本地运行 server-probe
	cd $(SERVER_PROBE_DIR) && \
	TRACE_OTLP_ENDPOINT=$(LOCAL_TRACE_OTLP_ENDPOINT) \
	$(GO) run .

dev-alert-service: ## 本地运行 alert-service（需先启动 make dev-deps）
	cd $(ALERT_SERVICE_DIR) && \
	KAFKA_BROKERS=$(LOCAL_KAFKA_BROKERS) \
	KAFKA_GROUP_ID=alert-service \
	REDIS_ADDR=$(LOCAL_REDIS_ADDR) \
	REDIS_PASSWORD=$(LOCAL_REDIS_PASSWORD) \
	TRACE_OTLP_ENDPOINT=$(LOCAL_TRACE_OTLP_ENDPOINT) \
	$(GO) run .

dev-frontend: ## 启动前端开发服务器
	cd $(FRONTEND_DIR) && $(NPM) run dev

dev-stop: ## 停止本地开发依赖
	$(DOCKER_COMPOSE) down

# ============================================
# Compose
# ============================================

compose-build: ## 构建 Compose 中的应用镜像
	$(DOCKER_COMPOSE) build $(COMPOSE_SERVICES)

compose-up: ## 启动 Compose 服务
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
	rm -f $(SERVER_PROBE_DIR)/probe
	rm -f $(SERVER_WEB_DIR)/web
	rm -f $(ALERT_SERVICE_DIR)/alert-service
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

dev-server-probe: dev-probe

dev-alert: dev-alert-service
