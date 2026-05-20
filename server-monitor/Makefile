.PHONY: build build-server-probe build-server-web build-alert-service build-frontend \
        test test-server-probe test-server-web test-alert-service test-pkg test-k8s test-frontend \
        lint lint-server-probe lint-server-web lint-alert-service lint-pkg \
        helm-lint \
        dev-server-probe dev-server-web dev-alert-service dev-frontend dev-deps dev-k8s dev-stop \
        ci-check \
        docker docker-up docker-down docker-logs docker-clean \
        k8s-setup k8s-teardown \
        clean fmt tidy help

# ============================================
# 构建相关
# ============================================

build: build-server-probe build-server-web build-alert-service build-frontend ## 构建所有服务

build-server-probe: ## 构建 server-probe
	@echo "构建 server-probe..."
	cd server-probe && go build -o probe .

build-server-web: ## 构建 server-web
	@echo "构建 server-web..."
	cd server-web && go build -o web .

build-alert-service: ## 构建 alert-service
	@echo "构建 alert-service..."
	cd alert-service && go build -o alert-service .

build-frontend: ## 构建前端
	@echo "构建前端..."
	cd frontend && npm run build

# ============================================
# 测试相关
# ============================================

test: test-server-probe test-server-web test-alert-service test-pkg ## 运行所有 Go 测试

test-server-probe: ## 运行 server-probe 测试
	@echo "运行 server-probe 测试..."
	cd server-probe && go test -v ./...

test-server-web: ## 运行 server-web 测试
	@echo "运行 server-web 测试..."
	cd server-web && go test -v ./...

test-alert-service: ## 运行 alert-service 测试
	@echo "运行 alert-service 测试..."
	cd alert-service && go test -v ./...

test-pkg: ## 运行 pkg 测试
	@echo "运行 pkg 测试..."
	cd pkg && go test -v ./...

test-k8s: ## 运行 K8s 相关测试
	@echo "运行 K8s 相关测试..."
	cd server-web && go test -v ./internal/copilot/k8s/ ./internal/service/k8s/ ./internal/handler/ -run TestK8s

test-frontend: ## 运行前端 ESLint 和类型检查
	@echo "运行前端 ESLint 和类型检查..."
	cd frontend && npx eslint . --ext .ts,.vue && npx vue-tsc --noEmit

# ============================================
# 静态检查
# ============================================

lint: lint-server-probe lint-server-web lint-alert-service lint-pkg ## 运行所有静态检查

lint-server-probe: ## golangci-lint 检查 server-probe
	@which golangci-lint > /dev/null || (echo "请先安装 golangci-lint" && exit 1)
	golangci-lint run ./server-probe/...

lint-server-web: ## golangci-lint 检查 server-web
	@which golangci-lint > /dev/null || (echo "请先安装 golangci-lint" && exit 1)
	golangci-lint run ./server-web/...

lint-alert-service: ## golangci-lint 检查 alert-service
	@which golangci-lint > /dev/null || (echo "请先安装 golangci-lint" && exit 1)
	golangci-lint run ./alert-service/...

lint-pkg: ## golangci-lint 检查 pkg
	@which golangci-lint > /dev/null || (echo "请先安装 golangci-lint" && exit 1)
	golangci-lint run ./pkg/...

# ============================================
# Helm 检查
# ============================================

helm-lint: ## Helm Chart 语法检查
	helm lint charts/server-monitor

# ============================================
# 开发模式
# ============================================

dev-deps: ## 启动依赖服务
	@echo "启动依赖服务（Redis、MySQL、Kafka、Prometheus、AlertManager、Grafana、server-probe）..."
	docker compose up -d redis mysql kafka kafka-init prometheus alertmanager grafana server-probe
	@echo "依赖服务已启动"
	@echo "  Redis:        localhost:6379"
	@echo "  MySQL:        localhost:3306"
	@echo "  Kafka:        localhost:9092"
	@echo "  Prometheus:   http://localhost:9090"
	@echo "  AlertManager: http://localhost:9093"
	@echo "  Grafana:      http://localhost:3000"
	@echo "  server-probe: http://localhost:8082"
	@echo "提示: Prometheus 首次抓取和规则加载通常需要 15-30 秒"

dev-server-probe: ## 本地运行 server-probe
	@echo "启动 server-probe..."
	cd server-probe && go run .

dev-server-web: ## 本地运行 server-web
	@echo "本地启动 server-web..."
	@echo "环境变量: PROMETHEUS_URL=http://localhost:9090 REDIS_ADDR=localhost:6379 REDIS_PASSWORD=server-monitor-local-redis JWT_SECRET=server-monitor-local-dev-jwt-secret-change-me ADMIN_PASSWORD=server-monitor-local-admin MYSQL_HOST=localhost KAFKA_BROKERS=localhost:9092 K8S_ENABLED=true"
	@echo "访问地址: http://localhost:8080/healthz 和 http://localhost:8080/readyz"
	cd server-web && PROMETHEUS_URL=http://localhost:9090 REDIS_ADDR=localhost:6379 REDIS_PASSWORD=server-monitor-local-redis JWT_SECRET=server-monitor-local-dev-jwt-secret-change-me ADMIN_PASSWORD=server-monitor-local-admin MYSQL_HOST=localhost MYSQL_PORT=3306 MYSQL_DATABASE=server_monitor MYSQL_USER=server_monitor MYSQL_PASSWORD=server-monitor-local-mysql KAFKA_BROKERS=localhost:9092 K8S_ENABLED=true GIN_MODE=debug go run .

dev-alert-service: ## 本地运行 alert-service
	@echo "启动 alert-service..."
	cd alert-service && go run .

dev-frontend: ## 本地运行前端开发服务器
	@echo "本地启动前端开发服务器..."
	@echo "Vite 代理已配置: /api -> localhost:8080, /ws -> ws://localhost:8080"
	@echo "访问地址: http://localhost:5173"
	cd frontend && npm run dev

dev-k8s: ## 启动 server-web（启用 K8s 集成）
	K8S_ENABLED=true K8S_NODES_ENABLED=true K8S_API_ENABLED=true K8S_KUBECONFIG=$(HOME)/.kube/config $(MAKE) dev-server-web

dev-stop: ## 停止开发依赖服务
	@echo "停止开发依赖服务..."
	docker compose down

# ============================================
# CI 检查
# ============================================

ci-check: lint test helm-lint ## 一键 CI 检查

# ============================================
# Docker 相关
# ============================================

docker: ## 构建 Docker 镜像
	@echo "构建 Docker 镜像..."
	docker compose build

docker-up: ## 启动 Docker Compose
	@echo "启动 Docker Compose..."
	docker compose up -d
	@echo "服务已启动"
	@echo "  监控大屏:     http://localhost:8080"
	@echo "  Prometheus:   http://localhost:9090"
	@echo "  AlertManager: http://localhost:9093"
	@echo "  Grafana:      http://localhost:3000"
	@echo "  Elasticsearch:http://localhost:9200"
	@echo "  Kibana:       http://localhost:5601"
	@echo "提示: 首次启动后等待 15-30 秒，再访问 /readyz 或前端页面"

docker-down: ## 停止 Docker Compose
	@echo "停止 Docker Compose..."
	docker compose down

docker-logs: ## 查看 Docker Compose 日志
	docker compose logs -f

docker-clean: ## 停止并清理所有数据
	@echo "清理 Docker 资源..."
	docker compose down -v
	@echo "清理完成"

# ============================================
# K8s 集成环境
# ============================================

k8s-setup: ## 创建 kind 集群并配置 K8s 集成环境
	@echo "配置 K8s 集成环境..."
	bash docker/setup-k8s.sh
	@echo ""
	@echo "K8s 环境已就绪，启动服务："
	@echo "  make docker-up        # 首次启动"
	@echo "  docker compose up -d server-web  # 仅重启 server-web"

k8s-teardown: ## 删除 kind 集群
	@echo "删除 kind 集群..."
	kind delete cluster --name cloudops-test 2>/dev/null || true
	@echo "kind 集群已删除"

# ============================================
# 其他
# ============================================

clean: ## 清理构建产物
	@echo "清理构建产物..."
	rm -f server-probe/probe
	rm -f server-web/web
	rm -f alert-service/alert-service
	rm -f main
	@echo "清理完成"

fmt: ## 格式化代码
	@echo "格式化代码..."
	cd server-probe && go fmt ./...
	cd server-web && go fmt ./...
	cd alert-service && go fmt ./...

tidy: ## 整理依赖
	@echo "整理依赖..."
	cd server-probe && go mod tidy
	cd server-web && go mod tidy
	cd alert-service && go mod tidy

help: ## 显示帮助信息
	@echo "Server Monitor - Makefile 命令说明"
	@echo ""
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2}'