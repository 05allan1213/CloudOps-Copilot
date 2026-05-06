.PHONY: all build build-probe build-web build-alert-service run run-probe run-web docker docker-up docker-down docker-logs clean test help dev-deps dev-web dev-frontend dev-stop

all: build

# ============================================
# 构建相关
# ============================================

build: build-probe build-web build-alert-service

build-probe:
	@echo "构建 server-probe..."
	cd server-probe && go build -o probe .

build-web:
	@echo "构建 server-web..."
	cd server-web && go build -o web .

build-alert-service:
	@echo "构建 alert-service..."
	cd alert-service && go build -o alert-service .

# ============================================
# 本地运行
# ============================================

run-probe:
	@echo "启动 server-probe..."
	cd server-probe && go run main.go

run-web:
	@echo "启动 server-web..."
	cd server-web && go run main.go

# ============================================
# 开发模式（无需构建 Docker 镜像）
# ============================================

dev-deps:
	@echo "启动依赖服务（Redis、Prometheus、AlertManager、Grafana、server-probe）..."
	docker compose up -d redis prometheus alertmanager grafana server-probe
	@echo "依赖服务已启动"
	@echo "  Redis:        localhost:6379"
	@echo "  Prometheus:   http://localhost:9091"
	@echo "  AlertManager: http://localhost:9093"
	@echo "  Grafana:      http://localhost:3000"
	@echo "  server-probe: http://localhost:9090"
	@echo "提示: Prometheus 首次抓取和规则加载通常需要 15-30 秒"

dev-web:
	@echo "本地启动 server-web..."
	@echo "环境变量: PROMETHEUS_URL=http://localhost:9091 REDIS_ADDR=localhost:6379 REDIS_PASSWORD=server-monitor-local-redis"
	@echo "访问地址: http://localhost:8080/healthz 和 http://localhost:8080/readyz"
	cd server-web && PROMETHEUS_URL=http://localhost:9091 REDIS_ADDR=localhost:6379 REDIS_PASSWORD=server-monitor-local-redis GIN_MODE=debug go run main.go

dev-frontend:
	@echo "本地启动前端开发服务器..."
	@echo "Vite 代理已配置: /api -> localhost:8080, /ws -> ws://localhost:8080"
	@echo "访问地址: http://localhost:5173"
	cd frontend && npm run dev

dev-stop:
	@echo "停止开发依赖服务..."
	docker compose down

# ============================================
# Docker 相关
# ============================================

docker:
	@echo "构建 Docker 镜像..."
	docker compose build

docker-up:
	@echo "启动 Docker Compose..."
	docker compose up -d
	@echo "服务已启动"
	@echo "  监控大屏:     http://localhost:8080"
	@echo "  Prometheus:   http://localhost:9091"
	@echo "  AlertManager: http://localhost:9093"
	@echo "  Grafana:      http://localhost:3000"
	@echo "  Elasticsearch:http://localhost:9200"
	@echo "  Kibana:       http://localhost:5601"
	@echo "提示: 首次启动后等待 15-30 秒，再访问 /readyz 或前端页面"

docker-down:
	@echo "停止 Docker Compose..."
	docker compose down

docker-logs:
	docker compose logs -f

docker-clean:
	@echo "清理 Docker 资源..."
	docker compose down -v
	@echo "清理完成"

# ============================================
# 测试与检查
# ============================================

test:
	@echo "运行测试..."
	cd server-probe && go test -v ./...
	cd server-web && go test -v ./...
	cd alert-service && go test -v ./...

fmt:
	@echo "格式化代码..."
	cd server-probe && go fmt ./...
	cd server-web && go fmt ./...
	cd alert-service && go fmt ./...

lint:
	@echo "静态检查..."
	@which golangci-lint > /dev/null || (echo "请先安装 golangci-lint" && exit 1)
	cd server-probe && golangci-lint run
	cd server-web && golangci-lint run
	cd alert-service && golangci-lint run

# ============================================
# 清理
# ============================================

clean:
	@echo "清理构建产物..."
	rm -f server-probe/probe
	rm -f server-web/web
	rm -f alert-service/alert-service
	rm -f main
	@echo "清理完成"

# ============================================
# 依赖管理
# ============================================

tidy:
	@echo "整理依赖..."
	cd server-probe && go mod tidy
	cd server-web && go mod tidy
	cd alert-service && go mod tidy

# ============================================
# 帮助
# ============================================

help:
	@echo "Server Monitor - Makefile 命令说明"
	@echo ""
	@echo "构建命令:"
	@echo "  make build          构建所有服务"
	@echo "  make build-probe    构建 server-probe"
	@echo "  make build-web      构建 server-web"
	@echo "  make build-alert-service 构建 alert-service"
	@echo ""
	@echo "开发模式（推荐开发阶段使用，无需构建镜像）:"
	@echo "  make dev-deps       启动依赖服务（Redis/Prometheus/AlertManager/Grafana/Probe）"
	@echo "  make dev-web        本地运行 server-web（需先启动 dev-deps）"
	@echo "  make dev-frontend   本地运行前端开发服务器（需先启动 dev-web）"
	@echo "  make dev-stop       停止开发依赖服务"
	@echo ""
	@echo "Docker 命令（生产/完整部署）:"
	@echo "  make docker         构建 Docker 镜像"
	@echo "  make docker-up      启动所有服务"
	@echo "  make docker-down    停止所有服务"
	@echo "  make docker-logs    查看服务日志"
	@echo "  make docker-clean   停止并清理所有数据"
	@echo ""
	@echo "本地运行:"
	@echo "  make run-probe      运行 server-probe"
	@echo "  make run-web        运行 server-web"
	@echo ""
	@echo "测试与检查:"
	@echo "  make test           运行测试"
	@echo "  make fmt            格式化代码"
	@echo "  make lint           静态检查（需安装 golangci-lint）"
	@echo ""
	@echo "其他:"
	@echo "  make tidy           整理依赖"
	@echo "  make clean          清理构建产物"
	@echo "  make help           显示此帮助信息"
