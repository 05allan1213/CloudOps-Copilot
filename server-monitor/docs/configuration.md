# server-web 配置说明

server-web 通过环境变量配置，支持以下参数：

## 通用配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `LISTEN_ADDR` | string | `:8080` | HTTP 监听地址 |
| `GIN_MODE` | string | `debug` | Gin 运行模式（debug/release/test） |
| `TRUSTED_PROXIES` | string[] | 空 | 受信任的反向代理 IP 列表 |
| `CORS_ALLOWED_ORIGINS` | string[] | 空 | 允许的跨域来源列表 |
| `STATIC_DIR` | string | 空 | 前端静态文件目录 |

## Prometheus 配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `PROMETHEUS_URL` | string | `http://prometheus:9090` | Prometheus 查询地址 |
| `PROMETHEUS_RELOAD_URL` | string | 自动拼接 | Prometheus 热重载地址 |
| `REQUEST_TIMEOUT_SECONDS` | duration | `5` | Prometheus 查询请求超时时间（秒） |
| `READY_TIMEOUT_SECONDS` | duration | `3` | 就绪检查超时时间（秒） |

## 告警规则同步配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `ALERT_RULES_FILE_PATH` | string | 空 | 告警规则文件路径，为空时禁用 |
| `ALERT_RULE_SYNC_ENABLED` | bool | `true` | 是否启用告警规则同步 |
| `PROMTOOL_PATH` | string | `promtool` | promtool 可执行文件路径 |
| `ALERT_RULE_SYNC_TIMEOUT_SECONDS` | duration | `10` | 规则同步超时时间（秒） |

## Redis 配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `REDIS_ADDR` | string | 空 | Redis 地址，为空时禁用 Redis |
| `REDIS_PASSWORD` | string | 空 | 🔴 Redis 密码（敏感） |
| `REDIS_DB` | int | `0` | Redis 数据库编号 |
| `REDIS_STARTUP_TIMEOUT_SECONDS` | duration | `5` | Redis 启动连接超时（秒） |
| `REDIS_DIAL_TIMEOUT_SECONDS` | duration | `5` | Redis 拨号超时（秒） |
| `REDIS_READ_TIMEOUT_SECONDS` | duration | `3` | Redis 读取超时（秒） |
| `REDIS_WRITE_TIMEOUT_SECONDS` | duration | `3` | Redis 写入超时（秒） |
| `REDIS_CONN_MAX_LIFETIME_SECONDS` | duration | `1800` | Redis 连接最大存活时间（秒） |
| `REDIS_CONN_MAX_IDLE_TIME_SECONDS` | duration | `300` | Redis 连接最大空闲时间（秒） |

## MySQL 配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `MYSQL_HOST` | string | 空 | MySQL 主机地址，为空时禁用 MySQL |
| `MYSQL_PORT` | string | `3306` | MySQL 端口 |
| `MYSQL_USER` | string | 空 | MySQL 用户名 |
| `MYSQL_PASSWORD` | string | 空 | 🔴 MySQL 密码（敏感） |
| `MYSQL_DATABASE` | string | 空 | MySQL 数据库名 |
| `MYSQL_STARTUP_TIMEOUT_SECONDS` | duration | `5` | MySQL 启动连接超时（秒） |
| `MYSQL_PING_TIMEOUT_SECONDS` | duration | `3` | MySQL 健康检查超时（秒） |

## 认证配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `AUTH_ENABLED` | bool | `true` | 是否启用鉴权，生产环境必须开启 |
| `JWT_SECRET` | string | 空 | 🔴 JWT 签名密钥，启用鉴权时不少于 32 字节（敏感） |
| `JWT_EXPIRE_HOURS` | int | `24` | JWT 令牌过期时间（小时） |
| `ADMIN_PASSWORD` | string | 空 | 🔴 初始管理员密码（敏感） |

## 限流配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `RATE_LIMIT_ENABLED` | bool | `false` | 是否启用限流 |
| `RATE_LIMIT_REQUESTS` | int | `120` | 限流窗口内最大请求数 |
| `RATE_LIMIT_WINDOW_SECONDS` | duration | `60` | 限流滑动窗口时长（秒） |
| `RATE_LIMIT_OPERATION_TIMEOUT_MILLISECONDS` | duration | `500` | 限流 Redis 操作超时（毫秒） |

## 缓存配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `HOSTS_CACHE_TTL_SECONDS` | duration | `30` | 主机列表缓存 TTL（秒） |
| `DASHBOARD_OVERVIEW_TTL_SECONDS` | duration | `10` | 仪表盘概览缓存 TTL（秒） |
| `ALERT_EVENT_DEDUPE_TTL_SECONDS` | duration | `86400` | 告警事件去重窗口 TTL（秒） |
| `CACHE_WRITE_TIMEOUT_SECONDS` | duration | `3` | 缓存写入超时（秒） |

## WebSocket 配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `WS_MAX_CONNECTIONS` | int | `1000` | WebSocket 最大并发连接数 |
| `HOSTS_BROADCAST_INTERVAL_SECONDS` | duration | `5` | 主机列表广播间隔（秒） |

## Webhook 配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `ALERTMANAGER_WEBHOOK_MAX_BODY_BYTES` | int | `1048576` | Alertmanager Webhook 请求体最大字节数 |

## HTTP 服务器配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `HTTP_READ_HEADER_TIMEOUT_SECONDS` | duration | `5` | 读取请求头超时（秒） |
| `HTTP_READ_TIMEOUT_SECONDS` | duration | `15` | 读取请求体超时（秒） |
| `HTTP_WRITE_TIMEOUT_SECONDS` | duration | `30` | 写入响应超时（秒） |
| `HTTP_IDLE_TIMEOUT_SECONDS` | duration | `120` | 长连接空闲超时（秒） |
| `SHUTDOWN_TIMEOUT_SECONDS` | duration | `5` | 优雅关闭超时（秒） |

## 链路追踪配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `TRACE_OTLP_ENDPOINT` | string | 空 | OTLP gRPC 端点，为空时禁用追踪 |
| `TRACE_SAMPLE_RATE` | float | `1.0` | 采样率 [0, 1] |

## Kafka 配置

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `KAFKA_BROKERS` | string[] | 空 | Kafka Broker 地址列表，为空时禁用 |

---

# alert-service 配置说明

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `LISTEN_ADDR` | string | `:8081` | HTTP 监听地址 |
| `HTTP_READ_HEADER_TIMEOUT_SECONDS` | duration | `5` | 读取请求头超时（秒） |
| `HTTP_READ_TIMEOUT_SECONDS` | duration | `15` | 读取请求体超时（秒） |
| `HTTP_WRITE_TIMEOUT_SECONDS` | duration | `30` | 写入响应超时（秒） |
| `HTTP_IDLE_TIMEOUT_SECONDS` | duration | `120` | 长连接空闲超时（秒） |
| `SHUTDOWN_TIMEOUT_SECONDS` | duration | `10` | 优雅关闭超时（秒） |
| `KAFKA_BROKERS` | string[] | `kafka:9092` | Kafka Broker 地址列表 |
| `KAFKA_GROUP_ID` | string | `alert-service` | Kafka 消费者组 ID |
| `REDIS_ADDR` | string | `redis:6379` | Redis 连接地址 |
| `REDIS_PASSWORD` | string | 空 | 🔴 Redis 密码（敏感） |
| `TRACE_OTLP_ENDPOINT` | string | `jaeger:4317` | OTLP gRPC 端点 |
| `TRACE_SAMPLE_RATE` | float | `1.0` | 采样率 [0, 1] |

---

# server-probe 配置说明

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `LISTEN_ADDR` | string | `:9090` | HTTP 监听地址 |
| `METRICS_PATH` | string | `/metrics` | 指标暴露路径，必须以 / 开头 |
| `SCRAPE_INTERVAL` | duration | `5` | 指标采集间隔（秒） |
| `PROMHTTP_MAX_REQUESTS_IN_FLIGHT` | int | `5` | Prometheus HTTP Handler 最大并发数 |
| `PROMHTTP_TIMEOUT` | duration | `5` | Prometheus HTTP Handler 超时（秒） |
| `HTTP_READ_TIMEOUT_SECONDS` | duration | `10` | 读取请求体超时（秒） |
| `HTTP_WRITE_TIMEOUT_SECONDS` | duration | `10` | 写入响应超时（秒） |
| `HTTP_IDLE_TIMEOUT_SECONDS` | duration | `60` | 长连接空闲超时（秒） |
| `SHUTDOWN_TIMEOUT_SECONDS` | duration | `5` | 优雅关闭超时（秒） |
| `HOSTNAME` | string | 自动获取 | 探针主机名标识 |
| `HOST_PROC` | string | 空 | 宿主机 /proc 挂载路径 |
| `HOST_SYS` | string | 空 | 宿主机 /sys 挂载路径 |
| `TRACE_OTLP_ENDPOINT` | string | 空 | OTLP gRPC 端点，为空时禁用追踪 |
| `TRACE_SAMPLE_RATE` | float | `1.0` | 采样率 [0, 1] |
