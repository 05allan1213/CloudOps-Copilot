# 第一阶段：云原生监控闭环 — 详细实现方案

## 一、阶段目标

跑通 **指标采集 → 指标存储 → 大盘展示 → 告警触发 → 实时推送** 的完整闭环。

### 完成标志

- [ ] server-probe 以 DaemonSet 部署，采集每个 Node 宿主机指标并暴露 /metrics
- [ ] Prometheus 成功采集 probe 指标
- [ ] Grafana 通过 Provisioning 自动配置数据源和大盘，展示监控指标
- [ ] Prometheus Rules 触发告警 → AlertManager 接收
- [ ] AlertManager Webhook → server-web 接收
- [ ] server-web 通过 Redis Pub/Sub 广播告警到所有 Pod
- [ ] 所有 server-web Pod 通过 WebSocket 推送告警到前端
- [ ] Redis 缓存热点数据，降低 Prometheus 查询压力
- [ ] 前端不直接传 PromQL，后端维护 PromQL 白名单模板
- [ ] Vue3 + ECharts 前端可视化
- [ ] Helm Chart 部署到 K8s
- [ ] GitHub Actions CI/CD 跑通（含 promtool 校验）

---

## 二、技术栈

```
后端：   Go 1.26 + Gin + gopsutil/v3 + Prometheus client_golang + go-redis + gorilla/websocket
缓存：   Redis 7.x
监控：   Prometheus + Grafana + AlertManager
前端：   Vue3 + TypeScript + ECharts + WebSocket
部署：   Docker + K8s + Helm + Ingress + HPA
CI/CD：  GitHub Actions
```

---

## 三、架构设计

```
┌─────────────────────────────────────────────────────────────────┐
│                        Kubernetes Cluster                        │
│                                                                  │
│  ┌──────────────────┐                                           │
│  │ server-probe     │── /metrics ──▶ Prometheus ──▶ Grafana     │
│  │ (DaemonSet)      │                    │                      │
│  │ 每个 Node 一个    │              Prometheus Rules             │
│  │ hostPath: /proc  │                    │                      │
│  │ hostPath: /sys   │                    ▼                      │
│  └──────────────────┘              AlertManager                 │
│                                        │                        │
│                                   Webhook                       │
│                                        │                        │
│  ┌──────────────┐                      ▼                        │
│  │  server-web  │◀─────────────────────┘                        │
│  │  (Pod A)     │                                               │
│  │              │──▶ Redis Publish (alert:channel)              │
│  └──────────────┘                    │                           │
│                                      ▼                           │
│  ┌──────────────┐              Redis Pub/Sub                    │
│  │  server-web  │◀─────────────────┘                            │
│  │  (Pod B)     │                                               │
│  │              │──▶ WebSocket ──▶ frontend                     │
│  └──────────────┘                                               │
│                                                                  │
│  ┌──────────────┐                                               │
│  │  server-web  │──▶ Redis Cache ◀── Prometheus HTTP API        │
│  │              │                       │                        │
│  │ PromQL 白名单 │                       ▼                        │
│  │ 模板查询      │                  PromQL 查询                  │
│  └──────┬───────┘                                               │
│         │                                                        │
│    WebSocket                                                    │
│         │                                                        │
│  ┌──────▼───────┐                                               │
│  │  frontend    │                                               │
│  │ Vue3+ECharts │                                               │
│  │ 告警面板      │                                               │
│  └──────────────┘                                               │
│                                                                  │
│  ┌──────────────┐                                               │
│  │    Redis     │  缓存主机状态 / Dashboard 聚合 / 限流计数       │
│  │              │  Pub/Sub 多副本广播 / 告警事件存储              │
│  └──────────────┘                                               │
│                                                                  │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐     │
│  │ Prometheus   │     │   Grafana    │     │ AlertManager │     │
│  │ :9090        │     │   :3000      │     │ :9093        │     │
│  └──────────────┘     └──────────────┘     └──────────────┘     │
│                                                                  │
│  ┌──────────────┐                                               │
│  │   Ingress    │  monitor.local → server-web / frontend        │
│  └──────────────┘                                               │
└─────────────────────────────────────────────────────────────────┘
```

---

## 四、server-probe 改造

### 4.1 改造要点

| 改动 | 原代码 | 改造后 |
|------|--------|--------|
| 去掉 MySQL | 直写 DB | 完全移除，只暴露 /metrics |
| 部署形态 | Deployment | **DaemonSet**（生产）/ Deployment（开发降级） |
| 宿主机指标采集 | 容器内视角 | hostPath 挂载 /proc、/sys 采集宿主机真实指标 |
| 扩展指标 | CPU/内存 | CPU/内存/磁盘/网络/进程/系统负载 |
| 指标命名 | probe_cpu_usage_percent | server_monitor_cpu_usage_percent（避免与 node-exporter 混淆） |
| 指标类型 | Gauge | Gauge + Counter + Histogram |
| 采集间隔 | 硬编码 5s | 可配置（环境变量 SCRAPE_INTERVAL） |

### 4.2 server-probe 部署形态说明

server-probe 以 **DaemonSet** 方式部署到每个 Kubernetes Node 上，每个节点运行一个 probe 实例，通过 hostPath 挂载 /proc、/sys 等宿主机目录采集节点资源指标，并暴露 /metrics 供 Prometheus 采集。

如果本地开发环境无法使用 DaemonSet，则降级为 Deployment 单实例模式，用于演示指标采集链路。

DaemonSet 关键配置：

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
    name: server-probe
spec:
    selector:
        matchLabels:
            app: server-probe
    template:
        spec:
            containers:
                - name: server-probe
                  image: 05allan1213/server-probe:latest
                  ports:
                    - containerPort: 9090
                  volumeMounts:
                    - name: proc
                      mountPath: /host/proc
                      readOnly: true
                    - name: sys
                      mountPath: /host/sys
                      readOnly: true
                  env:
                    - name: HOST_PROC
                      value: /host/proc
                    - name: HOST_SYS
                      value: /host/sys
            volumes:
                - name: proc
                  hostPath:
                    path: /proc
                - name: sys
                  hostPath:
                    path: /sys
```

注意：server-probe 以 DaemonSet 部署并挂载宿主机 /proc、/sys 时，securityContext 需单独处理，不能简单套用普通 Web 服务的 readOnlyRootFilesystem 配置。

### 4.3 指标体系设计

#### 主机指标

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `server_monitor_cpu_usage_percent` | Gauge | instance | CPU 使用率 |
| `server_monitor_memory_usage_percent` | Gauge | instance | 内存使用率 |
| `server_monitor_memory_total_bytes` | Gauge | instance | 内存总量 |
| `server_monitor_memory_available_bytes` | Gauge | instance | 可用内存 |
| `server_monitor_disk_usage_percent` | Gauge | instance, mountpoint | 磁盘使用率 |
| `server_monitor_disk_total_bytes` | Gauge | instance, mountpoint | 磁盘总量 |
| `server_monitor_disk_free_bytes` | Gauge | instance, mountpoint | 磁盘可用 |
| `server_monitor_disk_read_bytes_total` | Counter | instance, device | 磁盘读取字节数 |
| `server_monitor_disk_write_bytes_total` | Counter | instance, device | 磁盘写入字节数 |
| `server_monitor_network_recv_bytes_total` | Counter | instance, interface | 网络接收字节数 |
| `server_monitor_network_sent_bytes_total` | Counter | instance, interface | 网络发送字节数 |
| `server_monitor_load1` | Gauge | instance | 1 分钟负载 |
| `server_monitor_load5` | Gauge | instance | 5 分钟负载 |
| `server_monitor_load15` | Gauge | instance | 15 分钟负载 |
| `server_monitor_process_count` | Gauge | instance | 进程总数 |
| `server_monitor_uptime_seconds` | Gauge | instance | 系统运行时间 |

**指标命名说明**：本项目未直接使用 node-exporter，而是自研 server-probe 暴露类 node-exporter 风格指标，使用 `server_monitor_` 前缀避免与 node-exporter 的 `node_` 前缀混淆，同时展示自定义 Exporter 的实现能力。

#### 服务指标（server-web 自身暴露）

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `http_request_duration_seconds` | **Histogram** | method, path, status | 接口耗时分布 |
| `http_requests_total` | Counter | method, path, status | 接口请求总数 |
| `websocket_connections_active` | Gauge | — | WebSocket 活跃连接数 |

**Histogram buckets 设计**：

```go
[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}
```

**为什么用 Histogram 而不是 Summary**：Histogram 生成 `_bucket` 指标，支持服务端通过 `histogram_quantile()` 计算任意分位数（P50/P95/P99），且支持多实例聚合；Summary 在客户端预计算分位数，无法跨实例聚合。这是 Prometheus 指标类型的核心区别。

### 4.4 目录结构

```
server-probe/
├── main.go                 入口
├── collector/
│   ├── collector.go        采集器接口
│   ├── cpu.go              CPU 采集
│   ├── memory.go           内存采集
│   ├── disk.go             磁盘采集
│   ├── network.go          网络采集
│   ├── load.go             负载采集
│   └── process.go          进程采集
├── config/
│   └── config.go           配置（环境变量读取）
├── Dockerfile
├── go.mod
└── go.sum
```

### 4.5 核心代码设计

#### 配置 (config/config.go)

```go
type Config struct {
    ScrapeInterval time.Duration
    MetricsPath    string
    ListenAddr     string
    Hostname       string
    HostProc       string
    HostSys        string
}

func Load() *Config {
    interval := getEnvInt("SCRAPE_INTERVAL", 5)
    return &Config{
        ScrapeInterval: time.Duration(interval) * time.Second,
        MetricsPath:    getEnv("METRICS_PATH", "/metrics"),
        ListenAddr:     getEnv("LISTEN_ADDR", ":9090"),
        Hostname:       getHostname(),
        HostProc:       getEnv("HOST_PROC", ""),
        HostSys:        getEnv("HOST_SYS", ""),
    }
}
```

#### 采集器接口 (collector/collector.go)

```go
type Collector interface {
    Name() string
    Register(registry *prometheus.Registry)
    Update() error
}
```

#### 主入口 (main.go)

```go
func main() {
    cfg := config.Load()

    if cfg.HostProc != "" {
        gopsutil.SetHostProc(cfg.HostProc)
    }
    if cfg.HostSys != "" {
        gopsutil.SetHostSys(cfg.HostSys)
    }

    registry := prometheus.NewRegistry()
    collectors := []Collector{
        collector.NewCPU(cfg),
        collector.NewMemory(cfg),
        collector.NewDisk(cfg),
        collector.NewNetwork(cfg),
        collector.NewLoad(cfg),
        collector.NewProcess(cfg),
    }

    for _, c := range collectors {
        c.Register(registry)
    }

    go func() {
        ticker := time.NewTicker(cfg.ScrapeInterval)
        for range ticker.C {
            for _, c := range collectors {
                if err := c.Update(); err != nil {
                    slog.Error("collect failed", "collector", c.Name(), "error", err)
                }
            }
        }
    }()

    mux := http.NewServeMux()
    mux.Handle(cfg.MetricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("ok"))
    })

    slog.Info("probe started", "addr", cfg.ListenAddr, "interval", cfg.ScrapeInterval)
    log.Fatal(http.ListenAndServe(cfg.ListenAddr, mux))
}
```

---

## 五、server-web 改造

### 5.1 改造要点

| 改动 | 原代码 | 改造后 |
|------|--------|--------|
| 去掉 MySQL + GORM | 直读 DB | 完全移除 |
| 数据来源 | MySQL 查询 | Prometheus HTTP API 查询（PromQL 白名单模板） |
| 前端渲染 | Go 拼接 HTML | REST API + Vue3 前端 |
| 实时推送 | 无 | WebSocket |
| 告警接收 | 无 | AlertManager Webhook |
| 多副本广播 | 无 | Redis Pub/Sub |
| 缓存 | 无 | Redis |
| 限流 | 无 | Redis 滑动窗口 |
| 健康检查 | 无 | /healthz + /readyz |
| 优雅关闭 | 无 | graceful shutdown + 主动断开 WebSocket |

### 5.2 目录结构

```
server-web/
├── main.go                 入口 + graceful shutdown
├── api/
│   ├── router.go           路由注册
│   ├── handlers/
│   │   ├── hosts.go        主机列表 API
│   │   ├── metrics.go      指标查询 API（PromQL 白名单模板）
│   │   ├── alerts.go       告警查询 API
│   │   └── health.go       健康检查 API (/healthz, /readyz)
│   └── middleware/
│       ├── ratelimit.go    Redis 滑动窗口限流
│       ├── cors.go         CORS 跨域
│       ├── logging.go      请求日志（结构化，预留 trace_id）
│       └── metrics.go      Prometheus 指标中间件（Histogram）
├── prometheus/
│   ├── client.go           Prometheus HTTP API 客户端
│   └── queries.go          PromQL 白名单模板
├── redis/
│   ├── client.go           Redis 连接管理
│   └── cache.go            缓存封装（Get/Set/Delete）
├── webhook/
│   └── alertmanager.go     AlertManager Webhook 接收（firing/resolved/幂等/去重）
├── websocket/
│   ├── hub.go              WebSocket 连接管理中心
│   └── client.go           WebSocket 客户端封装
├── config/
│   └── config.go           配置
├── Dockerfile
├── go.mod
└── go.sum
```

### 5.3 API 设计

**核心原则**：前端不直接传 PromQL，只传 metric 类型和 instance，PromQL 由后端统一生成。

#### 主机列表

```
GET /api/v1/hosts

Response:
{
    "status": "success",
    "data": [
        {
            "instance": "server-1",
            "cpu": 72.5,
            "memory": 65.3,
            "disk": 45.2,
            "status": "healthy",
            "lastScrape": "2024-04-25T10:30:00Z"
        }
    ]
}
```

#### 主机指标

```
GET /api/v1/hosts/:instance/metrics?range=1h

Response:
{
    "status": "success",
    "data": {
        "cpu": [...],
        "memory": [...],
        "disk": [...],
        "network_recv": [...],
        "network_sent": [...]
    }
}
```

#### Dashboard 汇总

```
GET /api/v1/dashboard/overview

Response:
{
    "status": "success",
    "data": {
        "total_hosts": 3,
        "healthy_hosts": 2,
        "active_alerts": 1,
        "avg_cpu": 45.2,
        "avg_memory": 62.1
    }
}
```

#### 活跃告警

```
GET /api/v1/alerts/active

Response:
{
    "status": "success",
    "data": [
        {
            "fingerprint": "abc123",
            "status": "firing",
            "labels": {"alertname": "HighCPU", "instance": "server-1"},
            "annotations": {"summary": "CPU usage above 80%"},
            "startsAt": "2024-04-25T10:30:00Z"
        }
    ]
}
```

#### AlertManager Webhook

```
POST /api/v1/webhook/alertmanager

Request (AlertManager 发送):
{
    "receiver": "webhook",
    "status": "firing",
    "alerts": [
        {
            "status": "firing",
            "fingerprint": "abc123",
            "labels": {"alertname": "HighCPU", "instance": "server-1"},
            "annotations": {"summary": "CPU usage above 80%"},
            "startsAt": "2024-04-25T10:30:00Z",
            "endsAt": "0001-01-01T00:00:00Z"
        }
    ]
}

Response:
{
    "status": "accepted"
}
```

#### 健康检查

```
GET /healthz     存活检查，返回 200
GET /readyz      就绪检查，检查 Redis 和 Prometheus 连通性
```

#### WebSocket

```
WS /ws/alerts

服务端推送消息格式：
{
    "type": "alert",
    "data": {
        "fingerprint": "abc123",
        "status": "firing",
        "labels": {"alertname": "HighCPU", "instance": "server-1"},
        "annotations": {"summary": "CPU usage above 80%"},
        "startsAt": "2024-04-25T10:30:00Z"
    }
}

恢复通知：
{
    "type": "alert",
    "data": {
        "fingerprint": "abc123",
        "status": "resolved",
        "labels": {"alertname": "HighCPU", "instance": "server-1"},
        "annotations": {"summary": "CPU usage above 80%"},
        "startsAt": "2024-04-25T10:30:00Z",
        "endsAt": "2024-04-25T10:35:00Z"
    }
}
```

### 5.4 PromQL 白名单模板

前端不直接传 PromQL，只传 metric 类型和 instance，PromQL 由后端统一生成：

```go
var QueryTemplates = map[string]string{
    "cpu_usage":     `server_monitor_cpu_usage_percent{instance="%s"}`,
    "memory_usage":  `server_monitor_memory_usage_percent{instance="%s"}`,
    "memory_total":  `server_monitor_memory_total_bytes{instance="%s"}`,
    "disk_usage":    `server_monitor_disk_usage_percent{instance="%s",mountpoint="%s"}`,
    "network_recv":  `rate(server_monitor_network_recv_bytes_total{instance="%s"}[5m])`,
    "network_sent":  `rate(server_monitor_network_sent_bytes_total{instance="%s"}[5m])`,
    "host_list":     `server_monitor_cpu_usage_percent`,
    "active_alerts": `ALERTS{alertstate="firing"}`,
}

func BuildQuery(metric string, instance string, params map[string]string) (string, error) {
    tmpl, ok := QueryTemplates[metric]
    if !ok {
        return "", fmt.Errorf("unknown metric: %s", metric)
    }
    return fmt.Sprintf(tmpl, instance), nil
}
```

简历表述：封装 Prometheus HTTP API，基于 PromQL 模板实现指标查询白名单，避免前端直接透传任意 PromQL。

### 5.5 Redis 使用设计

#### 缓存 Key 设计

| Key | 类型 | TTL | 说明 |
|-----|------|-----|------|
| `host:{instance}:status` | Hash | 30s | 主机最新状态（CPU/内存/磁盘/状态） |
| `dashboard:overview` | Hash | 10s | Dashboard 汇总数据 |
| `ratelimit:{ip}:{path}` | ZSet | 60s | 滑动窗口限流计数 |
| `alert:active` | **Hash** | — | 当前活跃告警（key=fingerprint） |
| `alert:events` | **Stream** | — | 最近 N 条告警事件（含 firing + resolved） |
| `alert:channel` | **Pub/Sub** | — | server-web 多副本广播 channel |

#### 缓存策略

```
前端请求 → server-web
    ↓
查 Redis 缓存
    ├── 命中 → 直接返回
    └── 未命中 → 查 Prometheus HTTP API
                    ↓
                写入 Redis（TTL 10-30s）
                    ↓
                返回结果
```

#### 限流策略

```
滑动窗口限流：每 IP 每分钟最多 60 次请求

请求到达 → Redis ZADD (score=timestamp)
         → ZREMRANGEBYSCORE (清除窗口外数据)
         → ZCARD (计数)
         → 超过阈值 → 429 Too Many Requests
```

### 5.6 AlertManager Webhook 处理设计

#### 告警事件处理规则

1. **firing**：写入活跃告警集合（Redis alert:active），写入事件流（Redis alert:events），并推送前端
2. **resolved**：从活跃告警集合移除，写入事件流，并推送恢复通知
3. **唯一标识**：使用 AlertManager 提供的 `fingerprint` 字段做告警唯一标识
4. **幂等处理**：webhook 处理必须幂等，重复收到同一告警不会重复插入
5. **多副本广播**：接收 webhook 的 Pod 将告警发布到 Redis Pub/Sub channel `alert:channel`，所有 server-web 实例订阅该 channel，并向本实例维护的 WebSocket 客户端广播告警消息

#### 多副本 WebSocket 广播方案

```
AlertManager Webhook
        ↓
某个 server-web Pod 接收
        ↓
解析告警，写入 Redis alert:active + alert:events
        ↓
Redis Publish 到 alert:channel
        ↓
所有 server-web Pod 订阅 alert:channel
        ↓
每个 Pod 向自己本地 WebSocket 客户端广播
        ↓
所有前端客户端收到告警
```

这个方案解决了 server-web 多副本 + HPA 场景下，AlertManager webhook 只命中某一个 Pod，但 WebSocket 客户端可能连接在其他 Pod 上的问题。

### 5.7 Prometheus 客户端设计

```go
type PrometheusClient struct {
    baseURL    string
    httpClient *http.Client
}

func (c *PrometheusClient) Query(ctx context.Context, query string, ts time.Time) (model.Value, error)
func (c *PrometheusClient) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (model.Value, error)
func (c *PrometheusClient) GetActiveAlerts(ctx context.Context) ([]Alert, error)
```

### 5.8 WebSocket Hub 设计

```go
type Hub struct {
    clients    map[*Client]bool
    broadcast  chan []byte
    register   chan *Client
    unregister chan *Client
}

func (h *Hub) Run()
func (h *Hub) BroadcastAlert(alert Alert)
```

### 5.9 健康检查与优雅关闭

```
server-web:
- GET /healthz    存活检查，返回 200
- GET /readyz     就绪检查，检查 Redis 和 Prometheus 连通性
- 支持 graceful shutdown：
  1. 收到 SIGTERM 信号
  2. 停止接收新请求
  3. 主动断开所有 WebSocket 连接
  4. 等待进行中的请求完成（超时 10s）
  5. 退出

server-probe:
- GET /healthz    存活检查
- GET /metrics    指标暴露 + 就绪检查
```

---

## 六、前端设计

### 6.1 技术栈

```
Vue3 + TypeScript + Vite
ECharts 5.x（图表）
WebSocket（实时推送）
Axios（HTTP 请求）
Pinia（状态管理）
Vue Router（路由）
```

### 6.2 页面设计

| 页面 | 路由 | 功能 |
|------|------|------|
| 监控大盘 | / | 所有主机概览，CPU/内存/磁盘/网络趋势图 |
| 主机详情 | /host/:instance | 单台主机详细指标，历史趋势 |
| 告警面板 | /alerts | 实时告警列表，告警历史 |
| 系统状态 | /status | 服务健康状态，WebSocket 连接数 |

### 6.3 目录结构

```
frontend/
├── src/
│   ├── App.vue
│   ├── main.ts
│   ├── router/
│   │   └── index.ts
│   ├── stores/
│   │   ├── hosts.ts
│   │   ├── alerts.ts
│   │   └── websocket.ts
│   ├── views/
│   │   ├── Dashboard.vue       监控大盘
│   │   ├── HostDetail.vue      主机详情
│   │   ├── Alerts.vue          告警面板
│   │   └── Status.vue          系统状态
│   ├── components/
│   │   ├── CpuChart.vue        CPU 图表
│   │   ├── MemoryChart.vue     内存图表
│   │   ├── DiskChart.vue       磁盘图表
│   │   ├── NetworkChart.vue    网络图表
│   │   ├── HostCard.vue        主机卡片
│   │   ├── AlertItem.vue       告警条目
│   │   └── AlertToast.vue      告警弹窗通知
│   ├── composables/
│   │   ├── useWebSocket.ts     WebSocket 封装（自动重连）
│   │   └── useApi.ts           后端 API 封装
│   ├── api/
│   │   ├── hosts.ts            主机 API
│   │   └── alerts.ts           告警 API
│   └── types/
│       └── index.ts            类型定义
├── package.json
├── vite.config.ts
├── tsconfig.json
└── Dockerfile
```

### 6.4 WebSocket 集成

```typescript
// composables/useWebSocket.ts
export function useWebSocket(url: string) {
    const alerts = ref<Alert[]>([])
    const connected = ref(false)
    let ws: WebSocket | null = null

    function connect() {
        ws = new WebSocket(url)
        ws.onopen = () => { connected.value = true }
        ws.onclose = () => {
            connected.value = false
            setTimeout(connect, 3000)
        }
        ws.onmessage = (event) => {
            const msg = JSON.parse(event.data)
            if (msg.type === 'alert') {
                if (msg.data.status === 'firing') {
                    alerts.value.unshift(msg.data)
                    // 弹出告警通知
                } else if (msg.data.status === 'resolved') {
                    // 从活跃告警中移除，弹出恢复通知
                }
            }
        }
    }

    return { alerts, connected, connect }
}
```

---

## 七、Prometheus 配置

### 7.1 采集配置 (prometheus.yml)

```yaml
global:
    scrape_interval: 5s
    evaluation_interval: 5s

rule_files:
    - /etc/prometheus/rules/*.yml

alerting:
    alertmanagers:
        - static_configs:
            - targets: ['alertmanager:9093']

scrape_configs:
    - job_name: 'server-probe'
        kubernetes_sd_configs:
            - role: pod
        relabel_configs:
            - source_labels: [__meta_kubernetes_pod_label_app]
                action: keep
                regex: server-probe
            - source_labels: [__meta_kubernetes_pod_ip]
                target_label: __address__
                replacement: '${1}:9090'

    - job_name: 'server-web'
        kubernetes_sd_configs:
            - role: pod
        relabel_configs:
            - source_labels: [__meta_kubernetes_pod_label_app]
                action: keep
                regex: server-web
            - source_labels: [__meta_kubernetes_pod_ip]
                target_label: __address__
                replacement: '${1}:8080'
        metrics_path: /metrics
```

### 7.2 告警规则 (rules/alerts.yml)

```yaml
groups:
    - name: host_alerts
        rules:
            - alert: HighCPU
                expr: server_monitor_cpu_usage_percent > 80
                for: 2m
                labels:
                    severity: warning
                annotations:
                    summary: "High CPU usage on {{ $labels.instance }}"
                    description: "CPU usage is {{ $value }}% (threshold: 80%)"

            - alert: CriticalCPU
                expr: server_monitor_cpu_usage_percent > 95
                for: 1m
                labels:
                    severity: critical
                annotations:
                    summary: "Critical CPU usage on {{ $labels.instance }}"
                    description: "CPU usage is {{ $value }}% (threshold: 95%)"

            - alert: HighMemory
                expr: server_monitor_memory_usage_percent > 85
                for: 2m
                labels:
                    severity: warning
                annotations:
                    summary: "High memory usage on {{ $labels.instance }}"
                    description: "Memory usage is {{ $value }}% (threshold: 85%)"

            - alert: HighDisk
                expr: server_monitor_disk_usage_percent > 90
                for: 5m
                labels:
                    severity: warning
                annotations:
                    summary: "High disk usage on {{ $labels.instance }}"
                    description: "Disk usage is {{ $value }}% (threshold: 90%)"

            - alert: HostDown
                expr: up{job="server-probe"} == 0
                for: 1m
                labels:
                    severity: critical
                annotations:
                    summary: "Host {{ $labels.instance }} is down"
                    description: "Prometheus cannot reach the probe on {{ $labels.instance }}"

    - name: service_alerts
        rules:
            - alert: HighErrorRate
                expr: rate(http_requests_total{status=~"5.."}[5m]) / rate(http_requests_total[5m]) > 0.05
                for: 2m
                labels:
                    severity: warning
                annotations:
                    summary: "High error rate on {{ $labels.instance }}"
                    description: "Error rate is {{ $value | humanizePercentage }} (threshold: 5%)"

            - alert: HighLatency
                expr: histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le, instance)) > 1
                for: 5m
                labels:
                    severity: warning
                annotations:
                    summary: "High latency on {{ $labels.instance }}"
                    description: "P95 latency is {{ $value }}s (threshold: 1s)"
```

---

## 八、AlertManager 配置

### 8.1 alertmanager.yml

```yaml
global:
    resolve_timeout: 5m

route:
    group_by: ['alertname', 'instance']
    group_wait: 10s
    group_interval: 30s
    repeat_interval: 4h
    receiver: 'webhook'
    routes:
        - match:
                severity: critical
            receiver: 'webhook'
            repeat_interval: 1h
        - match:
                severity: warning
            receiver: 'webhook'
            repeat_interval: 4h

receivers:
    - name: 'webhook'
        webhook_configs:
            - url: 'http://server-web:8080/api/v1/webhook/alertmanager'
                send_resolved: true

inhibit_rules:
    - source_match:
            severity: 'critical'
        target_match:
            severity: 'warning'
        equal: ['alertname', 'instance']
```

---

## 九、Grafana 大盘设计

### 9.1 Grafana Provisioning

Grafana 通过 provisioning 自动配置 Prometheus DataSource 和 Dashboard JSON，避免手动进入页面配置。Helm 部署后 Grafana 自动拥有数据源和大盘。

Provisioning 配置：

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
    name: grafana-datasources
data:
    datasources.yaml: |
        apiVersion: 1
        datasources:
            - name: Prometheus
              type: prometheus
              access: proxy
              url: http://prometheus:9090
              isDefault: true
```

Dashboard JSON 通过 ConfigMap 挂载到 Grafana provisioning 目录。

### 9.2 Dashboard 规划

| Dashboard | 面板 | 数据源 |
|-----------|------|--------|
| 主机概览 | CPU/内存/磁盘/网络概览 | Prometheus |
| 主机详情 | 单机所有指标趋势 | Prometheus |
| 服务监控 | QPS/延迟/错误率/连接数 | Prometheus |
| 告警概览 | 活跃告警/告警趋势 | Prometheus |

### 9.3 关键 PromQL

```
# CPU 使用率趋势
server_monitor_cpu_usage_percent{instance="$instance"}

# 内存使用率趋势
server_monitor_memory_usage_percent{instance="$instance"}

# 磁盘使用率
server_monitor_disk_usage_percent{instance="$instance", mountpoint="$mountpoint"}

# 网络流量（5分钟速率）
rate(server_monitor_network_recv_bytes_total{instance="$instance"}[5m])
rate(server_monitor_network_sent_bytes_total{instance="$instance"}[5m])

# 系统负载
server_monitor_load1{instance="$instance"}
server_monitor_load5{instance="$instance"}
server_monitor_load15{instance="$instance"}

# 接口 QPS
rate(http_requests_total{method="$method"}[5m])

# P95 延迟（Histogram 分位数）
histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le, instance))

# 错误率
rate(http_requests_total{status=~"5.."}[5m]) / rate(http_requests_total[5m])

# 活跃告警数
count(ALERTS{alertstate="firing"})
```

---

## 十、Kubernetes 部署

### 10.1 Helm Chart 结构

```
helm/server-monitor/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── _helpers.tpl
│   ├── server-probe/
│   │   ├── daemonset.yaml           DaemonSet 部署（生产模式）
│   │   ├── deployment.yaml          Deployment 部署（开发模式）
│   │   ├── service.yaml
│   │   └── configmap.yaml
│   ├── server-web/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   ├── configmap.yaml
│   │   ├── secret.yaml
│   │   └── hpa.yaml
│   ├── frontend/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── configmap.yaml
│   ├── redis/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── configmap.yaml
│   ├── prometheus/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   ├── configmap.yaml
│   │   └── rules-configmap.yaml
│   ├── grafana/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   ├── configmap.yaml
│   │   └── dashboards-configmap.yaml   Grafana Provisioning Dashboard JSON
│   ├── alertmanager/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── configmap.yaml
│   └── ingress.yaml
└── values/
    ├── dev.yaml
    └── prod.yaml
```

### 10.2 values.yaml 核心配置

```yaml
serverProbe:
    deployMode: daemonset
    image:
        repository: 05allan1213/server-probe
        tag: latest
    resources:
        requests:
            cpu: 50m
            memory: 64Mi
        limits:
            cpu: 200m
            memory: 128Mi
    config:
        scrapeInterval: 5
    hostPaths:
        proc: /proc
        sys: /sys

serverWeb:
    replicaCount: 2
    image:
        repository: 05allan1213/server-web
        tag: latest
    resources:
        requests:
            cpu: 100m
            memory: 128Mi
        limits:
            cpu: 500m
            memory: 256Mi
    hpa:
        minReplicas: 2
        maxReplicas: 5
        targetCPUUtilization: 50

frontend:
    replicaCount: 1
    image:
        repository: 05allan1213/frontend
        tag: latest

redis:
    image: redis:7-alpine
    resources:
        requests:
            cpu: 50m
            memory: 64Mi

prometheus:
    image: prom/prometheus:latest
    retention: 15d
    storageSize: 10Gi

grafana:
    image: grafana/grafana:latest
    provisioning:
        datasources: true
        dashboards: true

alertmanager:
    image: prom/alertmanager:latest

ingress:
    enabled: true
    className: nginx
    host: monitor.local
```

### 10.3 Ingress 配置

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
    name: server-monitor-ingress
    annotations:
        nginx.ingress.kubernetes.io/rewrite-target: /
spec:
    ingressClassName: nginx
    rules:
        - host: monitor.local
            http:
                paths:
                    - path: /api
                        pathType: Prefix
                        backend:
                            service:
                                name: server-web
                                port:
                                    number: 8080
                    - path: /ws
                        pathType: Prefix
                        backend:
                            service:
                                name: server-web
                                port:
                                    number: 8080
                    - path: /
                        pathType: Prefix
                        backend:
                            service:
                                name: frontend
                                port:
                                    number: 80
```

---

## 十一、Docker 改造

### 11.1 server-probe Dockerfile

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o probe .

FROM alpine:latest
RUN apk add --no-cache tzdata
WORKDIR /app
COPY --from=builder /app/probe .
EXPOSE 9090
CMD ["/app/probe"]
```

### 11.2 server-web Dockerfile

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o web .

FROM alpine:latest
RUN apk add --no-cache tzdata
WORKDIR /app
COPY --from=builder /app/web .
EXPOSE 8080
CMD ["/app/web"]
```

### 11.3 frontend Dockerfile

```dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
```

---

## 十二、GitHub Actions CI/CD

```yaml
name: Build and Push Images

on:
    push:
        branches: [main]

jobs:
    check-prometheus-config:
        runs-on: ubuntu-latest
        steps:
            - uses: actions/checkout@v4

            - name: Install promtool
                run: |
                    wget -q https://github.com/prometheus/prometheus/releases/download/v2.50.0/prometheus-2.50.0.linux-amd64.tar.gz
                    tar xzf prometheus-*.tar.gz
                    sudo mv prometheus-*/promtool /usr/local/bin/

            - name: Check Prometheus config
                run: promtool check config helm/server-monitor/templates/prometheus/configmap.yaml

            - name: Check alert rules
                run: promtool check rules helm/server-monitor/templates/prometheus/rules-configmap.yaml

    build:
        runs-on: ubuntu-latest
        needs: check-prometheus-config
        strategy:
            matrix:
                service: [server-probe, server-web, frontend]
        steps:
            - uses: actions/checkout@v4

            - name: Login to DockerHub
                uses: docker/login-action@v3
                with:
                    username: ${{ secrets.DOCKERHUB_USERNAME }}
                    password: ${{ secrets.DOCKERHUB_TOKEN }}

            - name: Build and push ${{ matrix.service }}
                uses: docker/build-push-action@v5
                with:
                    context: ./${{ matrix.service }}
                    push: true
                    tags: 05allan1213/${{ matrix.service }}:${{ github.sha }}
```

---

## 十三、实施步骤

| 步骤 | 内容 | 验证标准 |
|------|------|---------|
| 1 | 跑通原项目 | docker-compose up 能启动，8080 能看到 HTML |
| 2 | 梳理现有代码结构 | 理解 probe/web 的逻辑和依赖 |
| 3 | server-probe 改造：去掉 MySQL，扩展指标，重构目录，支持 HOST_PROC/HOST_SYS | /metrics 返回所有指标 |
| 4 | server-web 改造：去掉 MySQL，接 Prometheus API（PromQL 白名单），加 Redis | API 返回 Prometheus 查询结果 |
| 5 | 部署 Prometheus + Grafana + AlertManager | Grafana Provisioning 自动配置，能看到指标图表 |
| 6 | 告警闭环：Rules → AlertManager → Webhook → Redis Pub/Sub → WebSocket | 触发告警后所有前端实时收到 |
| 7 | Vue3 + ECharts 前端开发 | 大盘展示 + 告警面板 |
| 8 | Docker 化 + docker-compose | docker-compose up 全部启动 |
| 9 | Helm Chart + K8s 部署（DaemonSet + Deployment + HPA） | helm install 一键部署 |
| 10 | GitHub Actions CI/CD（含 promtool 校验） | push 自动构建推送 |
| 11 | 测试 + 验收 | 所有功能可演示 |

---

## 十四、验收标准

### 功能验收

- [ ] server-probe 以 DaemonSet 部署，暴露 16+ 个指标，Prometheus 成功采集
- [ ] Grafana Provisioning 自动配置数据源和大盘，展示 CPU/内存/磁盘/网络/负载/进程
- [ ] CPU > 80% 触发告警，AlertManager 接收
- [ ] 告警通过 Webhook 推送到 server-web
- [ ] server-web 通过 Redis Pub/Sub 广播告警到所有 Pod
- [ ] 所有 server-web Pod 通过 WebSocket 推送告警到前端
- [ ] 前端实时展示告警通知（firing 弹窗 + resolved 恢复）
- [ ] Redis 缓存命中，减少 Prometheus 查询
- [ ] 接口限流生效，超频返回 429
- [ ] 前端不直接传 PromQL，后端 PromQL 白名单生效
- [ ] Helm 一键部署到 K8s
- [ ] HPA 自动扩缩容生效
- [ ] GitHub Actions 自动构建推送镜像（含 promtool 校验）

### 非功能验收

- [ ] 所有服务输出结构化日志（slog，预留 trace_id 字段）
- [ ] Dockerfile 多阶段构建，镜像 < 50MB
- [ ] K8s 探针（liveness/readiness）配置正确
- [ ] ConfigMap 管理非敏感配置，Secret 管理敏感配置
- [ ] server-web 支持 graceful shutdown，关闭时主动断开 WebSocket
- [ ] /healthz 和 /readyz 健康检查端点可用
- [ ] 容器安全配置（securityContext）合理
