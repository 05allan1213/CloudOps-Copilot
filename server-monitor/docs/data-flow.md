# 核心数据流

## 告警事件数据流

```
Alertmanager
    │
    │ HTTP POST (webhook)
    ▼
server-web /api/v1/webhook/alertmanager
    │
    │ 解析 AlertmanagerWebhookRequest → AlertEvent
    │ 去重检查（Redis SET NX + TTL）
    │
    ├──→ Kafka (topic: alert-events)     ← 异步发送
    │        │
    │        │ Consumer Group
    │        ▼
    │    alert-service
    │        │
    │        │ Process(event)
    │        ▼
    │    Redis (存储活跃告警状态)
    │
    ├──→ Redis Pub/Sub (channel: alert-events)  ← 实时广播
    │        │
    │        ▼
    │    server-web Subscriber
    │        │
    │        ▼
    │    WebSocket Hub.Broadcast()
    │        │
    │        ▼
    │    前端 WebSocket 客户端
    │
    └──→ MySQL (alert_histories 表)      ← 持久化存储
```

## 主机监控数据流

```
server-probe (DaemonSet, 每个 Node 一个)
    │
    │ 采集 /proc, /sys 指标
    │ 暴露 /metrics (Prometheus 格式)
    ▼
Prometheus
    │
    │ 定期 scrape server-probe /metrics
    │ 存储 TSDB
    ▼
server-web
    │
    │ PromQL 查询
    │ /api/v1/hosts — 主机列表
    │ /api/v1/hosts/:instance/metrics — 单机指标
    │ /api/v1/dashboard/overview — 仪表盘概览
    │
    │ 定期广播 (HOSTS_BROADCAST_INTERVAL)
    ▼
WebSocket Hub → 前端 WebSocket 客户端
```

## 认证数据流

```
前端
    │
    │ POST /api/v1/auth/login {username, password}
    ▼
server-web auth.Service
    │
    │ 查询 MySQL users 表
    │ bcrypt 验证密码
    │ 生成 JWT (HS256, 携带 user_id + token_version)
    ▼
前端 (存储 JWT)
    │
    │ 请求携带 Authorization: Bearer <token>
    ▼
server-web middleware.Auth
    │
    │ 解析 JWT → Identity
    ▼
server-web middleware.VerifyTokenVersion
    │
    │ 回查 MySQL users.token_version
    │ 不匹配则返回 401 (token has been revoked)
    ▼
业务 Handler
```

## 告警规则同步数据流

```
用户
    │
    │ POST /api/v1/alert-rules (CRUD)
    ▼
server-web handlers
    │
    │ 写入 MySQL alert_rules 表
    │
    │ POST /api/v1/alert-rules/sync
    ▼
server-web AlertRuleSync
    │
    │ 从 MySQL 读取所有 enabled 规则
    │ 生成 Prometheus rule YAML
    │ promtool check rules 校验
    │ 写入 ALERT_RULES_FILE_PATH
    │ HTTP POST PROMETHEUS_RELOAD_URL 触发重载
    ▼
Prometheus (重新加载规则文件)
```

## 日志数据流

```
容器 stdout/stderr
    │
    ▼
Fluent Bit (DaemonSet)
    │
    │ 解析 JSON 日志
    │ 附加 Kubernetes 元数据
    ▼
Elasticsearch
    │
    ▼
Kibana (可视化查询)
```
