# Server Monitor

## 项目概述

### 项目背景

在云原生环境下，服务器监控是保障业务稳定运行的基础能力。传统监控方案往往需要组合多个独立工具，配置复杂、学习成本高，且缺乏端到端的实时告警推送能力。Server Monitor 旨在提供一个轻量级、开箱即用的服务器监控平台，覆盖从指标采集、存储、告警到可视化展示的完整链路。

### 目标

- 实现「探针采集 → 指标存储 → 告警管理 → 实时推送 → 可视化展示」的完整闭环
- 支持主机维度的 CPU、内存、磁盘、网络、负载、进程等核心指标监控
- 提供告警规则管理、通知渠道配置、告警历史查询等告警生命周期管理
- 通过 WebSocket 实现告警和主机状态的实时推送，无需手动刷新
- 支持多用户认证与 RBAC 权限控制
- 提供 Docker Compose 一键部署和 Kubernetes/Helm 生产级部署两种形态

### 适用场景

- 中小规模服务器集群的日常监控与告警
- 开发/测试环境的快速监控部署
- 云原生监控体系的学习与参考
- 需要自定义告警规则和通知渠道的运维团队

### 核心链路

```
server-probe → Prometheus → AlertManager → server-web webhook → Redis Pub/Sub → WebSocket → 前端大屏
server-probe/server-web stdout → Fluent Bit → Elasticsearch → Kibana
```

## 核心功能

### 主机监控

- 自动发现并监控所有安装探针的主机
- 实时展示 CPU、内存、磁盘、网络、负载、进程等指标
- 支持主机在线/离线状态、风险等级（高 CPU / 高内存）筛选
- 主机分组管理，按业务维度组织主机
- 主机详情页支持 15m / 1h / 6h / 24h 时间范围的趋势图表

### 告警管理

- 接收 AlertManager Webhook 推送的告警事件
- 活跃告警查询，按严重级别（critical / warning / info）筛选
- 告警事件流，实时展示触发和恢复事件
- 告警历史分页查询，支持多条件筛选（状态、级别、名称、实例、时间范围）
- 告警规则 CRUD，支持 PromQL 表达式和 promtool 语法校验
- 告警规则同步到 Prometheus（渲染 YAML → 校验 → 写文件 → reload）

### 通知渠道

- 支持 Webhook 类型通知渠道
- 通知渠道 CRUD 管理
- 通知渠道连通性测试（发送测试请求并返回延迟和状态码）

### 实时推送

- WebSocket 连接 `/ws/alerts`，实时接收告警事件和主机列表更新
- 前端自动重连（指数退避策略，1s ~ 30s）
- 新告警到达时弹出 Toast 通知
- 页面标题动态更新告警数量

### 可视化大屏

- 暗色主题监控大屏，无第三方 UI 框架依赖
- 总览页：9 个统计卡片 + Top 12 主机资源柱状图
- 主机列表页：搜索、状态筛选、排序、风险筛选
- 告警页：当前告警与历史告警 Tab 切换
- 系统状态页：健康检查、就绪检查、依赖状态、监控概览

### 认证与权限

- JWT Token 认证
- admin / viewer 两种角色
- 路由守卫保护，管理员页面仅 admin 可访问
- Token 版本校验，支持强制下线

### 可观测性

- Prometheus 指标暴露（`/metrics`）
- OpenTelemetry 链路追踪（Jaeger）
- 结构化 JSON 日志（Fluent Bit → Elasticsearch → Kibana）
- 健康检查（`/healthz`）和就绪检查（`/readyz`）
- HTTP 请求指标（`http_requests_total`、`http_request_duration_seconds`）
- Redis 滑动窗口限流

## 架构

```
┌─────────────┐  :9090/metrics  ┌─────────────┐  alert  ┌──────────────┐
│ server-probe │───────────────▶│  Prometheus  │───────▶│ AlertManager │
│  (采集探针)   │                │  (指标存储)   │        │  (告警管理)    │
└─────────────┘                 └──────┬───────┘        └──────┬───────┘
                                       │ query                  │ webhook
                                       ▼                        ▼
                                ┌──────────────────────────────────┐
                                │           server-web             │
                                │  (API + WebSocket + 静态文件托管)  │
                                │                                  │
                                │  ┌─────────┐  ┌───────────────┐  │
                                │  │  Redis  │  │  Frontend     │  │
                                │  │(缓存+广播)│  │ (监控大屏)     │  │
                                │  └─────────┘  └───────────────┘  │
                                └──────────────────────────────────┘
                                       :8080
```

## 技术栈

### 后端

| 技术 | 版本 | 用途 |
|------|------|------|
| Go | 1.26 | 后端开发语言 |
| Gin | v1.12.0 | HTTP 框架 |
| GORM | v1.25.12 | ORM |
| gopsutil | v3.24.5 | 系统指标采集 |
| gorilla/websocket | v1.5.3 | WebSocket |
| go-redis | v9.17.0 | Redis 客户端 |
| Sarama | v1.48.0 | Kafka 客户端 |
| Prometheus client_golang | v1.23.2 | 指标暴露 |
| OpenTelemetry | v1.43.0 | 链路追踪 |
| Zap | v1.27.0 | 结构化日志 |
| golang.org/x/crypto | v0.50.0 | 密码加密（bcrypt） |
| swaggo/swag | v1.16.6 | Swagger 文档生成 |

### 前端

| 技术 | 版本 | 用途 |
|------|------|------|
| Vue | 3.5.22 | 前端框架 |
| TypeScript | 5.9.3 | 类型安全 |
| Vite | 7.1.7 | 构建工具 |
| Vue Router | 4.6.4 | 路由管理 |
| Pinia | 3.0.4 | 状态管理 |
| Axios | 1.15.2 | HTTP 客户端 |
| ECharts | 6.0.0 | 图表可视化 |

### 数据库与中间件

| 技术 | 版本 | 用途 |
|------|------|------|
| Redis | 7 | 缓存 + Pub/Sub 广播 |
| MySQL | 8.0 | 业务数据存储 |
| Prometheus | v2.51.0 | 指标存储与查询 |
| VictoriaMetrics | v1.102.1 | 长期指标存储 |
| AlertManager | v0.27.0 | 告警路由与管理 |
| Kafka | 7.6.1 (KRaft) | 事件总线 |
| Elasticsearch | 8.13.0 | 日志存储 |
| Kibana | 8.13.0 | 日志查询 |
| Fluent Bit | 3.1.4 | 日志采集 |
| Grafana | 10.4.0 | 可视化大盘 |
| Jaeger | 2.17.0 | 链路追踪 |

### 开发与部署工具

| 工具 | 用途 |
|------|------|
| Docker / Docker Compose | 容器化部署 |
| Kubernetes | 生产级容器编排 |
| Helm | Kubernetes 包管理 |
| GitHub Actions | CI/CD |
| promtool | Prometheus 规则校验 |
| golangci-lint | Go 静态检查 |

## 安装与配置

### 环境要求

| 依赖 | 最低版本 |
|------|---------|
| Go | 1.26 |
| Node.js | 20 |
| Docker | 20.x |
| Docker Compose | v2 |

### 方式一：Docker Compose 一键部署

```bash
make docker-up
```

访问 http://localhost:8080 查看监控大屏。

默认账号：

| 服务 | 地址 | 用户名 | 密码 |
|------|------|--------|------|
| 监控大屏 | http://localhost:8080 | admin | server-monitor-local-admin |
| Grafana | http://localhost:3000 | admin | server-monitor-local-grafana |
| Kibana | http://localhost:5601 | — | — |

注意事项：

- 首次启动后 Prometheus 抓取和告警规则加载需要 15-30 秒
- Elasticsearch / Kibana 首次启动较慢，日志链路可查询前需等待服务健康
- Docker Compose 默认绑定 `127.0.0.1`，生产环境必须修改绑定地址和默认密码
- 生产环境必须通过环境变量覆盖 `JWT_SECRET`、`ADMIN_PASSWORD`、`REDIS_PASSWORD` 等敏感配置

### 方式二：开发模式

无需构建 Docker 镜像，改代码后秒级生效：

```bash
# 终端 1：启动依赖服务
make dev-deps

# 终端 2：本地运行 server-web
make dev-web

# 终端 3：本地运行前端开发服务器
make dev-frontend
```

前端开发服务器访问 http://localhost:5173，Vite proxy 自动转发 API 和 WebSocket 到 server-web。

停止开发依赖服务：

```bash
make dev-stop
```

### 方式三：Kubernetes / Helm 部署

```bash
# 使用 Helm Chart 部署
helm install server-monitor charts/server-monitor \
  --namespace server-monitor --create-namespace \
  --set secret.jwtSecret=<your-jwt-secret> \
  --set secret.adminPassword=<your-admin-password>

# 或使用原始清单
kubectl apply -f k8s/
```

验证部署：

```bash
kubectl get pods -n server-monitor
kubectl port-forward svc/server-web 8080:8080 -n server-monitor
```

### 配置文件说明

| 文件 | 用途 |
|------|------|
| `docker/prometheus.yml` | Prometheus 采集配置 |
| `docker/alerts.yml` | 内置告警规则 |
| `docker/custom-alerts.yml` | 自定义告警规则（可由 server-web 写入） |
| `docker/alertmanager.yml` | AlertManager 路由与接收器配置 |
| `docker/jaeger/jaeger.yaml` | Jaeger 链路追踪配置 |
| `docker/fluent-bit/fluent-bit.conf` | Fluent Bit 日志采集配置 |
| `docker/grafana/provisioning/` | Grafana 数据源和大盘自动加载 |
| `docker/elasticsearch/` | Elasticsearch ILM 策略和索引模板 |
| `k8s/configmap.yaml` | Kubernetes ConfigMap（非敏感配置） |
| `k8s/secret.yaml` | Kubernetes Secret（敏感配置） |
| `charts/server-monitor/values.yaml` | Helm Chart 默认值 |

### 环境变量

#### server-probe

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `LISTEN_ADDR` | `:9090` | 监听地址 |
| `METRICS_PATH` | `/metrics` | 指标暴露路径 |
| `SCRAPE_INTERVAL` | `5` | 采集间隔（秒） |
| `HOSTNAME` | 自动获取 | 探针标识主机名 |
| `HOST_PROC` | 空 | 宿主机 /proc 挂载路径 |
| `HOST_SYS` | 空 | 宿主机 /sys 挂载路径 |
| `TRACE_OTLP_ENDPOINT` | 空 | OTLP gRPC 端点（空则禁用） |
| `TRACE_SAMPLE_RATE` | `1.0` | 链路追踪采样率 [0, 1] |

#### server-web

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `LISTEN_ADDR` | `:8080` | 监听地址 |
| `PROMETHEUS_URL` | `http://prometheus:9090` | Prometheus 地址 |
| `PROMETHEUS_RELOAD_URL` | 自动拼接 | Prometheus reload 地址 |
| `ALERT_RULES_FILE_PATH` | 空 | 可写告警规则文件路径 |
| `ALERT_RULE_SYNC_ENABLED` | `true` | 是否启用规则同步 |
| `PROMTOOL_PATH` | `promtool` | promtool 路径 |
| `ALERT_RULE_SYNC_TIMEOUT_SECONDS` | `10` | 规则同步超时（秒） |
| `REDIS_ADDR` | 空 | Redis 地址 |
| `REDIS_PASSWORD` | 空 | Redis 密码 |
| `REDIS_DB` | `0` | Redis 数据库编号 |
| `MYSQL_HOST` | 空 | MySQL 主机地址 |
| `MYSQL_PORT` | `3306` | MySQL 端口 |
| `MYSQL_USER` | 空 | MySQL 用户名 |
| `MYSQL_PASSWORD` | 空 | MySQL 密码 |
| `MYSQL_DATABASE` | 空 | MySQL 数据库名 |
| `JWT_SECRET` | 空 | JWT 签名密钥（≥32 字节） |
| `JWT_EXPIRE_HOURS` | `24` | JWT 有效期（小时） |
| `AUTH_ENABLED` | `true` | 是否启用鉴权 |
| `ADMIN_PASSWORD` | 空 | 初始管理员密码 |
| `CORS_ALLOWED_ORIGINS` | 空 | 允许跨域来源（逗号分隔） |
| `RATE_LIMIT_ENABLED` | `false` | 是否启用限流 |
| `RATE_LIMIT_REQUESTS` | `120` | 限流窗口内最大请求数 |
| `RATE_LIMIT_WINDOW_SECONDS` | `60` | 限流窗口长度（秒） |
| `REQUEST_TIMEOUT_SECONDS` | `5` | 请求超时（秒） |
| `HOSTS_BROADCAST_INTERVAL_SECONDS` | `5` | WebSocket 主机广播周期（秒） |
| `HOSTS_CACHE_TTL_SECONDS` | `30` | 主机缓存 TTL（秒） |
| `DASHBOARD_OVERVIEW_TTL_SECONDS` | `10` | 总览缓存 TTL（秒） |
| `ALERTMANAGER_WEBHOOK_MAX_BODY_BYTES` | `1048576` | Webhook 请求体上限（字节） |
| `STATIC_DIR` | 空 | 前端静态文件目录 |
| `GIN_MODE` | `debug` | Gin 运行模式 |
| `TRACE_OTLP_ENDPOINT` | 空 | OTLP gRPC 端点 |
| `TRACE_SAMPLE_RATE` | `1.0` | 链路追踪采样率 |
| `KAFKA_BROKERS` | 空 | Kafka Broker 列表（逗号分隔） |
| `WS_MAX_CONNECTIONS` | `1000` | WebSocket 最大连接数 |

#### alert-service

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `LISTEN_ADDR` | `:8081` | 监听地址 |
| `KAFKA_BROKERS` | `kafka:9092` | Kafka Broker 列表 |
| `KAFKA_GROUP_ID` | `alert-service` | Kafka 消费者组 ID |
| `REDIS_ADDR` | `redis:6379` | Redis 地址 |
| `REDIS_PASSWORD` | 空 | Redis 密码 |
| `TRACE_OTLP_ENDPOINT` | `jaeger:4317` | OTLP gRPC 端点 |
| `TRACE_SAMPLE_RATE` | `1.0` | 链路追踪采样率 |

## 使用方法

### 启动服务

```bash
# Docker Compose 一键启动
make docker-up

# 开发模式（三个终端）
make dev-deps
make dev-web
make dev-frontend
```

### 基本操作流程

1. **登录**：访问 http://localhost:8080，使用 `admin` / `server-monitor-local-admin` 登录
2. **查看总览**：首页展示主机总数、健康/离线主机数、活跃告警数、资源 Top 排名
3. **浏览主机**：进入主机列表页，按状态/风险/分组筛选，点击主机卡片查看详情
4. **查看告警**：进入告警页，查看当前活跃告警和历史告警事件
5. **管理规则**：管理员进入设置 → 告警规则，创建/编辑/删除规则，同步到 Prometheus
6. **配置通知**：管理员进入设置 → 通知渠道，创建 Webhook 渠道并测试连通性

### 常见功能演示

#### 创建告警规则

1. 进入 **设置 → 告警规则**
2. 填写规则名称、PromQL 表达式、持续时间、严重级别
3. 点击创建，规则保存到 MySQL
4. 点击 **同步规则**，规则渲染为 YAML 并写入 Prometheus，自动触发 reload

#### 测试通知渠道

1. 进入 **设置 → 通知渠道**
2. 填写名称、类型（webhook）、URL
3. 点击 **测试**，系统发送 HTTP 请求并返回状态码和延迟

#### 查询告警历史

1. 进入 **告警历史** 页面
2. 按状态、级别、告警名、实例、时间范围筛选
3. 分页浏览历史告警记录

#### Kibana 日志查询

1. 访问 http://localhost:5601
2. 创建 Data View：`sm-logs-*`，时间字段选择 `@timestamp`
3. 按 `service: server-web` 或 `path: /healthz` 查询请求日志

## API 文档

### 统一响应格式

```json
{
  "status": "success | error",
  "data": {},
  "error": ""
}
```

### 认证

#### 登录

```
POST /api/v1/auth/login
```

请求体：

```json
{
  "username": "admin",
  "password": "server-monitor-local-admin"
}
```

响应：

```json
{
  "status": "success",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIs...",
    "expires_at": "2026-05-06T12:00:00Z",
    "user": { "id": 1, "username": "admin", "role": "admin" }
  }
}
```

#### 获取当前用户

```
GET /api/v1/auth/me
Authorization: Bearer <token>
```

响应：

```json
{
  "status": "success",
  "data": { "id": 1, "username": "admin", "role": "admin" }
}
```

### 主机监控

#### 获取主机列表

```
GET /api/v1/hosts
Authorization: Bearer <token>
```

查询参数：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `status` | string | 否 | `up` / `down` |
| `q` | string | 否 | 搜索关键词 |
| `sort` | string | 否 | `instance` / `cpu_desc` / `memory_desc` |
| `risk` | string | 否 | `high_cpu` / `high_memory` |
| `group` | string | 否 | 主机组 ID |

#### 获取主机指标

```
GET /api/v1/hosts/:instance/metrics
Authorization: Bearer <token>
```

查询参数：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `range` | string | 否 | `15m` / `1h` / `6h` / `24h`，默认 `1h` |
| `mountpoint` | string | 否 | 磁盘挂载点过滤 |

响应：

```json
{
  "status": "success",
  "data": {
    "instance": "10.0.0.1:9100",
    "range": "1h",
    "stepSeconds": 60,
    "metrics": {
      "cpu": [[timestamp, value], ...],
      "memory": [...],
      "disk": [...],
      "network_recv": [...],
      "network_sent": [...],
      "load1": [...],
      "process_count": [...],
      "uptime": [...]
    }
  }
}
```

#### 仪表盘概览

```
GET /api/v1/dashboard/overview
Authorization: Bearer <token>
```

响应：

```json
{
  "status": "success",
  "data": {
    "total_hosts": 10,
    "healthy_hosts": 9,
    "down_hosts": 1,
    "active_alerts": 3,
    "avg_cpu": 45.2,
    "avg_memory": 62.1,
    "generated_at": "2026-05-05T12:00:00Z"
  }
}
```

### 告警

#### 活跃告警

```
GET /api/v1/alerts/active
Authorization: Bearer <token>
```

查询参数：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `severity` | string | 否 | `critical` / `warning` / `info` |

#### 告警事件

```
GET /api/v1/alerts/events
Authorization: Bearer <token>
```

查询参数：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `limit` | int | 否 | 返回条数，默认 8，最大 100 |
| `status` | string | 否 | `firing` / `resolved` |
| `severity` | string | 否 | `critical` / `warning` / `info` |

#### 告警历史

```
GET /api/v1/alert-histories
Authorization: Bearer <token>
```

查询参数：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `status` | string | 否 | `firing` / `resolved` |
| `severity` | string | 否 | `critical` / `warning` / `info` |
| `alert_name` | string | 否 | 告警名称 |
| `instance` | string | 否 | 实例过滤 |
| `group` | uint | 否 | 主机组 ID |
| `start` | string | 否 | 开始时间（RFC3339） |
| `end` | string | 否 | 结束时间（RFC3339） |
| `page` | int | 否 | 页码，默认 1 |
| `page_size` | int | 否 | 每页数量，默认 20，最大 100 |

响应：

```json
{
  "status": "success",
  "data": {
    "items": [...],
    "total": 100,
    "page": 1,
    "page_size": 20
  }
}
```

#### AlertManager Webhook

```
POST /api/v1/webhook/alertmanager
```

请求体（AlertManager 标准格式）：

```json
{
  "receiver": "webhook",
  "status": "firing",
  "alerts": [{
    "status": "firing",
    "fingerprint": "abc123",
    "labels": { "alertname": "HighCPU", "instance": "10.0.0.1:9100" },
    "annotations": { "summary": "CPU usage above 80%" },
    "startsAt": "2026-05-05T12:00:00Z"
  }]
}
```

响应：`202 Accepted`

### 主机组

| 方法 | 路径 | 权限 | 说明 |
|------|------|------|------|
| GET | `/api/v1/host-groups` | 登录用户 | 列出主机组 |
| GET | `/api/v1/host-groups/:id` | 登录用户 | 获取主机组详情（含成员） |
| POST | `/api/v1/host-groups` | admin | 创建主机组 |
| PUT | `/api/v1/host-groups/:id` | admin | 更新主机组 |
| DELETE | `/api/v1/host-groups/:id` | admin | 删除主机组 |
| POST | `/api/v1/host-groups/:id/members` | admin | 添加组成员 |
| DELETE | `/api/v1/host-groups/:id/members` | admin | 删除组成员 |

创建/更新请求体：

```json
{
  "name": "生产环境",
  "description": "生产服务器组",
  "instances": ["10.0.0.1:9100", "10.0.0.2:9100"]
}
```

### 告警规则

| 方法 | 路径 | 权限 | 说明 |
|------|------|------|------|
| GET | `/api/v1/alert-rules` | 登录用户 | 列出告警规则 |
| GET | `/api/v1/alert-rules/:id` | 登录用户 | 获取规则详情 |
| POST | `/api/v1/alert-rules` | admin | 创建告警规则 |
| PUT | `/api/v1/alert-rules/:id` | admin | 更新告警规则 |
| DELETE | `/api/v1/alert-rules/:id` | admin | 删除告警规则 |
| POST | `/api/v1/alert-rules/sync` | admin | 同步规则到 Prometheus |

创建/更新请求体：

```json
{
  "name": "HighCPU",
  "expr": "100 - (avg by(instance)(rate(node_cpu_seconds_total{mode=\"idle\"}[2m])) * 100) > 80",
  "duration": "2m",
  "severity": "warning",
  "summary": "CPU usage above 80%",
  "description": "Instance {{ $labels.instance }} CPU usage is {{ $value }}%",
  "enabled": true
}
```

校验规则：
- name：1-128 字符
- expr：最长 2048 字符，禁止引用 `admin_api`/`scrape_interval`/`scrape_duration`，禁止子查询
- duration：必须为合法 Prometheus duration（如 `2m`、`5m`、`1h`）
- severity：`critical` / `warning` / `info`

### 通知渠道

| 方法 | 路径 | 权限 | 说明 |
|------|------|------|------|
| GET | `/api/v1/channels` | 登录用户 | 列出通知渠道 |
| GET | `/api/v1/channels/:id` | 登录用户 | 获取渠道详情 |
| POST | `/api/v1/channels` | admin | 创建通知渠道 |
| PUT | `/api/v1/channels/:id` | admin | 更新通知渠道 |
| DELETE | `/api/v1/channels/:id` | admin | 删除通知渠道 |
| POST | `/api/v1/channels/:id/test` | admin | 测试通知渠道 |

创建/更新请求体：

```json
{
  "name": "Ops Webhook",
  "type": "webhook",
  "url": "https://hooks.example.com/alert",
  "enabled": true
}
```

校验规则：
- name：1-128 字符
- type：仅支持 `webhook`
- url：1-512 字符，禁止内网/回环地址

测试响应：

```json
{
  "status": "success",
  "data": { "success": true, "latency_ms": 150, "status_code": 200 }
}
```

### 用户管理

| 方法 | 路径 | 权限 | 说明 |
|------|------|------|------|
| POST | `/api/v1/auth/register` | admin | 注册用户 |
| GET | `/api/v1/users` | admin | 列出用户 |
| DELETE | `/api/v1/users/:id` | admin | 删除用户（不可删除自己） |

注册请求体：

```json
{
  "username": "viewer1",
  "password": "secure-password",
  "role": "viewer"
}
```

### WebSocket

```
WS /ws/alerts?token=<jwt>
```

消息类型：

| type | 说明 | 方向 |
|------|------|------|
| `hosts` | 主机列表数据 | 服务端 → 客户端 |
| `alert` | 告警事件（firing/resolved） | 服务端 → 客户端 |

连接参数：
- 心跳间隔：30s（ping/pong）
- 写超时：10s
- 读超时：60s
- 最大消息大小：1024 字节
- 最大连接数：1000（可配置）

### 健康检查

| 路径 | 说明 |
|------|------|
| `GET /healthz` | 健康检查 |
| `GET /readyz` | 就绪检查（Prometheus + Redis） |
| `GET /readyz/full` | 完整就绪检查（Prometheus + Redis + MySQL） |
| `GET /metrics` | Prometheus 指标 |

## 项目结构

```
server-monitor/
├── server-probe/              # 监控探针：采集 CPU/内存/磁盘/网络/负载/进程指标
│   ├── collector/             # 采集器（CPU、Memory、Disk、Network、Load、Process）
│   ├── config/                # 配置加载
│   ├── Dockerfile
│   └── go.mod
├── server-web/                # Web 后端：API + WebSocket + 静态文件托管
│   ├── api/
│   │   ├── handlers/          # HTTP Handler（认证、主机、告警、规则、渠道、用户）
│   │   ├── middleware/        # 中间件（认证、RBAC、CORS、日志、指标、限流、恢复）
│   │   └── router.go          # 路由注册
│   ├── auth/                  # 认证服务（JWT、密码处理）
│   ├── model/                 # 数据模型（User、AlertRule、AlertHistory、Channel、HostGroup）
│   ├── database/              # MySQL 连接与迁移
│   ├── prometheus/            # Prometheus 查询客户端与 PromQL 模板
│   ├── redis/                 # Redis 客户端与缓存封装
│   ├── cache/                 # 缓存服务
│   ├── host/                  # 主机服务
│   ├── alert/                 # 告警服务
│   ├── webhook/               # AlertManager Webhook 接收
│   ├── websocket/             # WebSocket Hub
│   ├── pubsub/                # Redis Pub/Sub 订阅
│   ├── kafka/                 # Kafka 生产者
│   ├── config/                # 配置加载
│   ├── Dockerfile
│   └── go.mod
├── alert-service/             # 告警事件消费服务：Kafka 消费 → 处理 → Redis 存储
│   ├── kafka/                 # Kafka 消费者与事件定义
│   ├── alert/                 # 告警处理器与存储
│   ├── redis/                 # Redis 客户端
│   ├── metrics/               # Prometheus 指标
│   ├── health/                # 健康检查
│   ├── config/                # 配置加载
│   ├── Dockerfile
│   └── go.mod
├── frontend/                  # Vue 3 前端：暗色监控大屏
│   ├── src/
│   │   ├── api/               # API 请求封装
│   │   ├── composables/       # 组合式函数（WebSocket）
│   │   ├── stores/            # Pinia 状态管理（auth、monitor）
│   │   ├── pages/             # 页面组件
│   │   ├── components/        # 通用组件
│   │   ├── router/            # Vue Router 路由配置
│   │   ├── types/             # TypeScript 类型定义
│   │   ├── App.vue            # 根组件
│   │   └── style.css          # 全局暗色主题样式
│   ├── vite.config.ts         # Vite 配置（含 dev proxy）
│   └── package.json
├── pkg/                       # 共享库
│   ├── shutdown/              # 优雅关闭
│   ├── httpmiddleware/         # HTTP 中间件
│   ├── configutil/            # 配置工具
│   ├── logger/                # 结构化日志
│   └── tracer/                # OpenTelemetry 链路追踪
├── docker/                    # Docker Compose 专用配置
│   ├── prometheus.yml         # Prometheus 采集配置
│   ├── alerts.yml             # 内置告警规则
│   ├── custom-alerts.yml      # 自定义告警规则
│   ├── alertmanager.yml       # AlertManager 配置
│   ├── jaeger/                # Jaeger 配置
│   ├── fluent-bit/            # Fluent Bit 日志采集配置
│   ├── grafana/               # Grafana 数据源与大盘 Provisioning
│   └── elasticsearch/         # ES ILM 策略与索引模板
├── k8s/                       # Kubernetes 原始清单
├── charts/server-monitor/     # Helm Chart
├── .github/workflows/ci.yaml  # CI 流水线
├── docker-compose.yml         # Docker Compose 编排
├── Makefile                   # 常用命令
└── README.md
```

## Makefile 命令

| 命令 | 说明 |
|------|------|
| `make dev-deps` | 启动依赖服务（Redis/Prometheus/AlertManager/Grafana/Probe） |
| `make dev-web` | 本地运行 server-web |
| `make dev-frontend` | 本地运行前端开发服务器 |
| `make dev-stop` | 停止开发依赖服务 |
| `make docker` | 构建 Docker 镜像 |
| `make docker-up` | 启动所有服务 |
| `make docker-down` | 停止所有服务 |
| `make docker-logs` | 查看服务日志 |
| `make docker-clean` | 停止并清理所有数据 |
| `make build` | 构建所有服务 |
| `make build-probe` | 构建 server-probe |
| `make build-web` | 构建 server-web |
| `make build-alert-service` | 构建 alert-service |
| `make test` | 运行所有 Go 测试 |
| `make fmt` | 格式化 Go 代码 |
| `make lint` | golangci-lint 静态检查 |
| `make tidy` | 整理 Go 依赖 |
| `make clean` | 清理构建产物 |

## 服务端口

| 服务 | 端口 | 说明 |
|------|------|------|
| server-web | 8080 | API + WebSocket + 前端 |
| server-probe | 9090 | Prometheus 指标 |
| alert-service | 8081 | 告警消费服务 |
| Prometheus | 9091 | 指标存储 |
| AlertManager | 9093 | 告警管理 |
| Grafana | 3000 | 可视化大盘 |
| Redis | 6379 | 缓存 + Pub/Sub |
| MySQL | 3306 | 业务数据 |
| Kafka | 19092 | 事件总线 |
| Elasticsearch | 9200 | 日志存储 |
| Kibana | 5601 | 日志查询 |
| Jaeger | 16686 | 链路追踪 UI |
| VictoriaMetrics | 8428 | 长期指标存储 |
