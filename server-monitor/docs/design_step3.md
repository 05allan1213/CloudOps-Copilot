# 第三阶段：链路追踪 + 事件驱动 — 详细实现方案

## 一、阶段目标

建立分布式链路追踪能力，引入事件驱动架构，实现 **请求级可观测** 和 **异步事件处理**。

### 阶段边界

第三阶段在第二阶段日志链路基础上，补齐链路追踪能力，并引入事件驱动架构。

**第三阶段必须完成**：

- 所有服务集成 OpenTelemetry SDK，HTTP 请求自动注入/提取 trace context
- Jaeger 存储、查询、展示 Trace 数据
- 日志与 Trace 通过 `trace_id` / `span_id` 关联
- Prometheus remote_write → VictoriaMetrics 长期存储
- Kafka 事件总线，承载告警事件
- alert-service 从 Kafka 消费告警事件，实现告警去重、聚合与状态管理
- 告警从"Prometheus 单一来源"扩展为"Kafka 事件 + Prometheus 规则"双通道

**第三阶段只预留，不实现**：

- MySQL 用户/权限/配置管理（第四阶段）
- ChatOps Agent / AI 分析（第四阶段）
- operation-events 的生产者和消费者业务逻辑（仅创建 Topic）

**第三阶段不做**：

- 用户登录 / JWT / RBAC
- 主机分组管理
- 告警规则界面化配置
- 通知渠道配置
- 告警历史归档到 MySQL

### 阶段内实施拆分

| 模块 | 内容 | 完成标准 |
|------|------|---------|
| 3.1 OTel SDK 集成 | server-probe / server-web 集成 OpenTelemetry SDK，HTTP middleware 自动创建/传播 trace context | 请求经过任一服务时自动产生 trace_id / span_id，日志中包含 trace_id |
| 3.2 Jaeger 部署 | Docker Compose 和 Helm 部署 Jaeger | Jaeger UI 可查询 Trace，展示服务调用链 |
| 3.3 日志-Trace 关联 | 日志输出 trace_id / span_id，Grafana / Kibana 可通过 trace_id 跳转 | 在 Kibana 查到日志后，可通过 trace_id 跳转到 Jaeger 查看完整链路 |
| 3.4 VictoriaMetrics 接入 | Prometheus remote_write → VictoriaMetrics | Grafana 可查询超过 Prometheus 本地保留期的历史指标数据 |
| 3.5 Kafka 事件总线 | Docker Compose 和 Helm 部署 Kafka（KRaft 模式） | Kafka 可生产和消费消息，alert-events Topic 已创建 |
| 3.6 alert-service 开发 | 新建 alert-service，Kafka 消费 → 告警去重/聚合 → 状态管理 | alert-service 能消费告警事件，去重、聚合、写入 Redis |
| 3.7 事件驱动告警 | server-web 告警事件异步写入 Kafka，alert-service 消费并处理 | 告警事件从 Kafka 流经 alert-service，实现双通道告警 |

每个模块完成后单独格式化、测试、验证和提交，不把代码改造、部署改造、Dashboard 改造混在同一个提交里。

### 完成标志

> 当前勾选表示代码实现、单元测试、构建、静态配置校验或 Docker Compose 运行态 API 验收已有证据；浏览器点击类联动仍在第十四节单独标注。

- [x] 所有服务集成 OpenTelemetry SDK，HTTP 请求自动产生 trace_id / span_id
- [x] 日志中包含 trace_id / span_id，与第二阶段预留的 ES mapping 对齐
- [x] Jaeger 可查询和展示 Trace 链路
- [x] Grafana / Kibana 已具备基于 trace_id 的关联配置
- [x] Prometheus remote_write → VictoriaMetrics 配置已接入，Grafana 已配置 VictoriaMetrics 数据源
- [x] Kafka 集群配置已接入（KRaft 模式），alert-events Topic 初始化配置已创建
- [x] server-web 可异步生产告警事件到 Kafka
- [x] alert-service 可消费 Kafka 告警事件，实现去重和聚合
- [x] Grafana 已新增基于应用指标和日志的 Trace/Kafka 观察面板
- [x] 告警双通道可用：受控 Alertmanager webhook → Kafka 事件 → alert-service 已验收，Prometheus Rules → AlertManager 配置已通过静态校验
- [x] Docker Compose 包含 Jaeger + VictoriaMetrics + Kafka + alert-service
- [x] Helm Chart 包含 Jaeger + VictoriaMetrics + Kafka + alert-service 部署

---

## 二、技术栈

```
链路追踪：  OpenTelemetry SDK (Go) + Jaeger
事件总线：  Kafka 3.7 (KRaft 模式，无 ZooKeeper)
长期存储：  VictoriaMetrics single-node
告警服务：  Go + Kafka Consumer + Redis
部署：      Docker Compose（本地开发）+ Helm Chart（K8s 生产）
```

### 新增中间件

| 组件 | 版本 | 职责 | 部署形态 |
|------|------|------|---------|
| Jaeger | 2.17 | Trace 存储、查询、展示 | 单容器内存存储（开发）/ Production 模式（生产） |
| VictoriaMetrics | 1.102 | Prometheus 长期存储 | single-node（开发）/ Cluster 模式（生产） |
| Kafka | 3.7 | 事件总线 | KRaft 单节点（开发）/ KRaft 集群（生产） |
| alert-service | 自建 | Kafka 消费 → 告警去重/聚合 → 状态管理 | Deployment |

### 新增 Go 依赖

| 依赖 | 用途 | 引入服务 |
|------|------|---------|
| go.opentelemetry.io/otel | OpenTelemetry 核心 API | server-probe, server-web, alert-service |
| go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc | OTLP gRPC 导出器 | server-probe, server-web, alert-service |
| go.opentelemetry.io/otel/sdk | OpenTelemetry SDK | server-probe, server-web, alert-service |
| go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp | HTTP 自动埋点 | server-probe |
| go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin | Gin 自动埋点 | server-web |
| github.com/IBM/sarama | Kafka Go 客户端 | server-web, alert-service |

### 不引入的组件

| 组件 | 原因 |
|------|------|
| Zipkin | 功能弱，社区活跃度低，Jaeger 是 CNCF 毕业项目 |
| Jaeger SDK | 官方已弃用，推荐使用 OpenTelemetry SDK |
| SkyWalking | Java 生态为主，Go SDK 不成熟 |
| NATS | 轻量但简历含金量低，Kafka 是行业标准 |
| RabbitMQ | 传统消息队列，不适合高吞吐事件流 |
| RedPanda | Kafka 兼容但社区规模小，面试价值低 |
| Confluent Kafka Go | GPL 许可证限制，sarama 是纯 Go 实现 |
| ZooKeeper | Kafka 3.3+ 支持 KRaft 模式，无需额外部署 ZooKeeper |
| etcd（独立部署） | K8s 底层已有 etcd，再部署一套是重复建设 |

---

## 三、架构设计

```
┌──────────────────────────────────────────────────────────────────────────┐
│                        Kubernetes Cluster                                 │
│                                                                           │
│  ┌──────────────────┐   ┌──────────────────┐   ┌──────────────────┐     │
│  │ server-probe     │   │ server-web       │   │ alert-service    │     │
│  │ (DaemonSet)      │   │ (Deployment)     │   │ (Deployment)     │     │
│  │                  │   │                  │   │                  │     │
│  │ OTel SDK         │   │ OTel SDK         │   │ OTel SDK         │     │
│  │ → OTLP Exporter  │   │ → OTLP Exporter  │   │ Kafka Consumer   │     │
│  │ → trace_id 日志  │   │ → trace_id 日志  │   │ → 告警去重/聚合   │     │
│  └────────┬─────────┘   └────────┬─────────┘   │ → Redis 状态管理  │     │
│           │                      │              └────────┬─────────┘     │
│           │    OTLP (gRPC)       │    OTLP (gRPC)         │               │
│           ▼                      ▼                        │               │
│  ┌──────────────────────────────────────────┐             │               │
│  │ Jaeger                                     │             │               │
│  │ Trace 存储、查询、展示                      │             │               │
│  └──────────────────────────────────────────┘             │               │
│                                                            │               │
│  ┌──────────────────┐   ┌──────────────────┐              │               │
│  │ Prometheus       │   │ VictoriaMetrics   │              │               │
│  │ (短期存储 15d)   │──▶│ (长期存储 90d)    │              │               │
│  │ remote_write     │   │ Grafana 查询      │              │               │
│  └──────────────────┘   └──────────────────┘              │               │
│           │                                               │               │
│     Prometheus Rules                                       │               │
│           │                                               │               │
│           ▼                                               │               │
│  ┌──────────────────┐                                     │               │
│  │ AlertManager     │                                     │               │
│  │ → Webhook        │──▶ server-web                       │               │
│  └──────────────────┘                                     │               │
│                                                           │               │
│  ┌──────────────────────────────────────────────────────┐ │               │
│  │ Kafka 事件总线 (KRaft 模式，无 ZooKeeper)             │ │               │
│  │                                                       │ │               │
│  │ Topics:                                               │ │               │
│  │  - alert-events    告警事件（firing/resolved）        │ │               │
│  │  - operation-events 操作事件（预留，第三阶段不实现）   │ │               │
│  │                                                       │ │               │
│  │ server-web ──produce──▶ alert-events ──consume──▶ alert-service       │
│  └──────────────────────────────────────────────────────┘ │               │
│                                                           ▼               │
│  ┌──────────────────┐   ┌──────────────────┐   ┌──────────────────┐     │
│  │ Redis            │   │ Elasticsearch    │   │ Grafana          │     │
│  │ 缓存/限流/PubSub │   │ 日志（含trace_id）│   │ 指标+日志+Trace  │     │
│  │ 告警状态         │   │                  │   │ 联动查询         │     │
│  └──────────────────┘   └──────────────────┘   └──────────────────┘     │
└──────────────────────────────────────────────────────────────────────────┘
```

### 关键架构决策

#### 为什么选择 OpenTelemetry SDK 而不是 Jaeger SDK

| 维度 | OpenTelemetry SDK | Jaeger SDK |
|------|-------------------|------------|
| 官方推荐 | **CNCF 可观测性统一标准，Jaeger 官方推荐** | 已弃用，不再维护 |
| 统一性 | **统一 Metrics/Traces/Logs 埋点** | 仅支持 Traces |
| 厂商中立 | **可导出到 Jaeger/Zipkin/Tempo 等任意后端** | 仅支持 Jaeger |
| 未来兼容 | **OTel 是行业标准，所有主流厂商支持** | 逐步退出历史舞台 |
| Go 生态 | **K8s/etcd/containerd 均迁移到 OTel** | 停止新功能开发 |

#### 为什么选择 Jaeger 而不是 Tempo

| 维度 | Jaeger | Tempo |
|------|--------|-------|
| 成熟度 | **CNCF 毕业项目，生产验证充分** | CNCF 孵化项目，较新 |
| 存储后端 | 支持 ES/Cassandra/Kafka/内存 | 仅支持对象存储 |
| 面试价值 | **链路追踪标准，面试高频** | 知名度较低 |
| 与 ES 集成 | **可直接使用现有 ES 作为存储** | 需要额外对象存储 |

#### 为什么选择 Kafka KRaft 模式而不是 ZooKeeper 模式

| 维度 | KRaft 模式 | ZooKeeper 模式 |
|------|-----------|---------------|
| 架构复杂度 | **无额外中间件，部署简单** | 需要额外部署和管理 ZooKeeper 集群 |
| 运维负担 | **少一套组件，少一套监控** | ZooKeeper 与 Kafka 版本兼容性问题 |
| 官方推荐 | **Kafka 3.3+ 生产可用，4.0 将移除 ZK** | 旧版兼容方案，逐步弃用 |
| 资源占用 | **仅 Kafka 进程** | Kafka + ZooKeeper 两个进程 |
| 面试价值 | **体现对新架构的了解** | 传统方案 |

#### 为什么选择 Kafka 而不是 NATS

| 维度 | Kafka | NATS |
|------|-------|------|
| 行业标准 | **大厂必备，简历含金量高** | 轻量但采用率低 |
| 持久化 | **磁盘持久化，消息不丢失** | 默认内存，持久化需 JetStream |
| 回溯消费 | **支持 offset 回溯** | 有限支持 |
| 生态 | **Kafka Connect/Streams 生态丰富** | 生态较小 |

#### 为什么选择 VictoriaMetrics 而不是 Thanos

| 维度 | VictoriaMetrics | Thanos |
|------|----------------|--------|
| 复杂度 | **单二进制，部署简单** | 5+ 组件（Sidecar/Store/Compactor/Query/MinIO） |
| 兼容性 | **完全兼容 Prometheus 协议** | 兼容，但配置复杂 |
| 资源占用 | **低，适合 demo** | 高，组件多 |
| 面试价值 | **云原生监控领域最火** | 传统方案 |

#### Kafka 与指标链路的关系

```
Metrics 链路（指标数据，不变）：
server-probe → Prometheus scrape → remote_write → VictoriaMetrics

Events 链路（事件数据，第三阶段新增）：
server-web → Kafka → alert-service

Kafka 用于告警事件等异步事件，不用于替代 Prometheus 的指标采集链路。
指标长期存储走 Prometheus remote_write → VictoriaMetrics，不走 Kafka。
```

---

## 四、OpenTelemetry SDK 集成

### 4.1 改造要点

| 改动 | 原代码 | 改造后 |
|------|--------|--------|
| 链路追踪 | 无 | OpenTelemetry SDK + OTLP gRPC Exporter |
| trace_id / span_id | ES mapping 预留，日志不输出 | 日志自动输出真实 trace_id / span_id |
| HTTP 埋点 | 无 | otelhttp / otelgin 中间件自动创建 Span |
| 上下文传播 | 无 | W3C Trace Context（traceparent 头）自动注入/提取 |
| 出站请求 | 无 trace 传播 | HTTP 客户端使用 otelhttp Transport 自动传播 |

### 4.2 OTel 初始化设计

#### tracer 包设计

```go
package tracer

import (
    "context"
    "fmt"
    "time"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

type Config struct {
    ServiceName   string
    OTELEndpoint  string
    SampleRate    float64
}

func Init(cfg Config) (func(context.Context) error, error) {
    exporter, err := otlptracegrpc.New(context.Background(),
        otlptracegrpc.WithEndpoint(cfg.OTELEndpoint),
        otlptracegrpc.WithInsecure(),
    )
    if err != nil {
        return nil, fmt.Errorf("create OTLP exporter: %w", err)
    }

    res, err := resource.Merge(
        resource.Default(),
        resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceNameKey.String(cfg.ServiceName),
        ),
    )
    if err != nil {
        return nil, fmt.Errorf("create resource: %w", err)
    }

    sampler := sdktrace.ParentBased(
        sdktrace.TraceIDRatioBased(cfg.SampleRate),
    )

    provider := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter,
            sdktrace.WithBatchTimeout(5*time.Second),
        ),
        sdktrace.WithResource(res),
        sdktrace.WithSampler(sampler),
    )

    otel.SetTracerProvider(provider)
    otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
        propagation.TraceContext{},
        propagation.Baggage{},
    ))

    return provider.Shutdown, nil
}
```

**设计说明**：

1. **OTLP gRPC Exporter**：标准协议，Jaeger 2.x 原生支持 OTLP 接收，无需 Jaeger Agent。
2. **W3C Trace Context**：行业标准传播格式，`traceparent` + `tracestate` HTTP 头。
3. **采样策略**：`ParentBased(TraceIDRatioBased)` — 如果父 Span 已采样，子 Span 必定采样；否则按比例采样。开发环境建议 `SampleRate=1.0`（全量），生产环境按需调整。
4. **Batch Span Processor**：批量发送 Span，减少网络开销，5 秒超时。
5. **优雅关闭**：返回 `shutdown` 函数，在 `main()` 的 graceful shutdown 中调用，确保所有 Span 刷出。

### 4.3 日志-Trace 关联设计

#### logger 包改造

在第二阶段的 logger 包基础上，增加从 `context.Context` 提取 trace_id / span_id 的能力：

```go
package logger

import (
    "context"

    "go.opentelemetry.io/otel/trace"
    "go.uber.org/zap"
)

func FromContext(ctx context.Context) *zap.Logger {
    spanCtx := trace.SpanContextFromContext(ctx)
    if !spanCtx.IsValid() {
        return zap.L()
    }
    return zap.L().With(
        zap.String("trace_id", spanCtx.TraceID().String()),
        zap.String("span_id", spanCtx.SpanID().String()),
    )
}
```

**使用方式**：

```go
// 有 context 的地方（handler、middleware）
logger.FromContext(c.Request.Context()).Info("http request", ...)

// 没有 context 的地方（后台任务、初始化）
zap.L().Info("service started", ...)
```

**关键约束**：
- 第二阶段日志中 `trace_id` / `span_id` 字段为空（ES mapping 已预留），第三阶段开始输出真实值
- 不要求每条日志都有 `trace_id`，只有从 HTTP 请求上下文中获取的日志才包含
- 后台任务（如采集器、Kafka consumer）的日志不包含 `trace_id`，这是正常的

### 4.4 server-probe OTel 集成

#### 新增文件

```
server-probe/
├── tracer/
│   └── tracer.go        OTel 初始化 + OTLP Exporter + 采样配置
```

#### 配置新增 (config/config.go)

```go
type Config struct {
    // ... 现有字段 ...
    OTELEndpoint   string
    OTELSampleRate float64
}

func Load() Config {
    return Config{
        // ... 现有配置 ...
        OTELEndpoint:   getEnv("TRACE_OTLP_ENDPOINT", "jaeger:4317"),
        OTELSampleRate: getEnvFloat("TRACE_SAMPLE_RATE", 1.0),
    }
}
```

**注意**：环境变量使用项目私有命名 `TRACE_OTLP_ENDPOINT` 和 `TRACE_SAMPLE_RATE`，而非 OTel 官方约定的 `OTEL_EXPORTER_OTLP_ENDPOINT`。原因是项目自行解析这些配置并传入 `tracer.Init()`，如果使用 OTel 官方环境变量名，可能与 OTel SDK 的自动配置机制产生语义冲突（官方约定 `OTEL_EXPORTER_OTLP_ENDPOINT` 包含 `/` 路径前缀的特定格式要求）。使用项目私有命名可以避免歧义。

#### 主入口改造 (main.go)

```go
func main() {
    cfg := config.Load()

    log, err := logger.Init("server-probe")
    if err != nil {
        fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
        os.Exit(1)
    }
    defer logger.Sync(log)

    shutdown, err := tracer.Init(tracer.Config{
        ServiceName:  "server-probe",
        OTELEndpoint: cfg.OTELEndpoint,
        SampleRate:   cfg.OTELSampleRate,
    })
    if err != nil {
        zap.L().Warn("tracer init failed, tracing disabled", zap.Error(err))
    } else {
        defer shutdown(context.Background())
    }

    // ... 其余逻辑不变 ...
}
```

**注意**：tracer 初始化失败不应阻止服务启动，只记录警告日志并降级为无 trace 模式。

#### HTTP middleware 改造 (main.go)

```go
// 替换原有的 loggingMiddleware，增加 otelhttp 包装
func otelMiddleware(next http.Handler) http.Handler {
    return otelhttp.NewHandler(next, "http-request")
}
```

`otelhttp.NewHandler` 会自动：
- 为每个 HTTP 请求创建 Span
- 从请求头提取 `traceparent`（如果存在）
- 将 trace context 注入 `r.Context()`

#### 请求日志改造

```go
func loggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        ww := &responseWriter{ResponseWriter: w}

        next.ServeHTTP(ww, r)

        latency := time.Since(start)
        logger.FromContext(r.Context()).Info("http request completed",
            zap.String("method", r.Method),
            zap.String("path", r.URL.Path),
            zap.Int("status", ww.status),
            zap.Float64("latency_ms", float64(latency.Microseconds())/1000),
        )
    })
}
```

**中间件链顺序**：otelMiddleware → loggingMiddleware → recoveryMiddleware → 业务 handler

### 4.5 server-web OTel 集成

#### 新增文件

```
server-web/
├── tracer/
│   └── tracer.go        OTel 初始化 + OTLP Exporter + 采样配置
```

#### 配置新增 (config/config.go)

```go
type Config struct {
    // ... 现有字段 ...
    OTELEndpoint   string
    OTELSampleRate float64
}

func Load() Config {
    return Config{
        // ... 现有配置 ...
        OTELEndpoint:   getEnv("TRACE_OTLP_ENDPOINT", "jaeger:4317"),
        OTELSampleRate: getEnvFloat("TRACE_SAMPLE_RATE", 1.0),
    }
}
```

#### 路由改造 (api/router.go)

```go
func SetupRouter(cfg *config.Config, cacheClient *redis.Client, promClient *prometheus.Client) *gin.Engine {
    router := gin.New()

    router.Use(
        middleware.CORS(cfg.CORSOrigins),
        otelgin.Middleware("server-web"),   // 新增：OTel Gin 中间件
        middleware.Logging(),
        middleware.Recovery(),
        metrics.Handler(),
        middleware.RateLimit(cacheClient, cfg.RateLimit),
    )

    // ... 路由注册不变 ...
}
```

`otelgin.Middleware` 会自动：
- 为每个 Gin 请求创建 Span
- 从请求头提取 `traceparent`
- 将 trace context 注入 `c.Request.Context()`

#### 请求日志改造 (api/middleware/logging.go)

```go
func Logging() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        requestID := c.GetHeader(requestIDHeader)
        if requestID == "" {
            requestID = newRequestID(start)
        }
        c.Header(requestIDHeader, requestID)
        c.Set(requestIDKey, requestID)

        c.Next()

        path := c.FullPath()
        if path == "" {
            path = c.Request.URL.Path
        }

        logger.FromContext(c.Request.Context()).Info("http request",
            zap.String("request_id", requestID),
            zap.String("method", c.Request.Method),
            zap.String("path", path),
            zap.Int("status", c.Writer.Status()),
            zap.Float64("latency_ms", float64(time.Since(start).Microseconds())/1000),
            zap.String("client_ip", c.ClientIP()),
        )
    }
}
```

#### 出站请求 trace 传播

server-web 调用 Prometheus HTTP API 时，需要传播 trace context：

```go
// prometheus/client.go
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

func NewPrometheusClient(baseURL string) *PrometheusClient {
    transport := otelhttp.NewTransport(http.DefaultTransport)
    return &PrometheusClient{
        baseURL: baseURL,
        httpClient: &http.Client{
            Transport: transport,
            Timeout:   10 * time.Second,
        },
    }
}
```

这样 Prometheus HTTP API 调用会自动创建子 Span 并传播 trace context。

### 4.6 OTel 环境变量

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `TRACE_OTLP_ENDPOINT` | `jaeger:4317` | OTLP gRPC 接收端地址（项目私有配置） |
| `TRACE_SAMPLE_RATE` | `1.0` | 采样率（0.0-1.0），开发环境全量，生产按需调整 |

**为什么不使用 OTel 官方环境变量**：项目自行解析配置并传入 `tracer.Init()`，使用 OTel 官方环境变量名（如 `OTEL_EXPORTER_OTLP_ENDPOINT`）可能与 OTel SDK 的自动配置机制产生语义冲突。项目私有命名 `TRACE_*` 语义明确，不与任何自动配置机制冲突。

---

## 五、Jaeger 部署

### 5.1 Jaeger 版本说明

Jaeger v2（2.x）是当前主线版本，基于 OpenTelemetry Collector 构建，原生支持 OTLP 协议。v1 已进入维护模式。

| 维度 | Jaeger v2 | Jaeger v1 |
|------|-----------|-----------|
| OTLP 支持 | **原生支持，无需额外组件，需在 v2 配置中启用 OTLP receiver** | 需要设置 `COLLECTOR_OTLP_ENABLED=true` |
| 配置方式 | **YAML 配置文件** | 环境变量 |
| 存储后端 | **ES / Cassandra / Kafka / 内存** | ES / Cassandra / Kafka / 内存 |
| 维护状态 | **活跃开发** | 维护模式 |
| 镜像 | `cr.jaegertracing.io/jaegertracing/jaeger:2.17.0` | `jaegertracing/all-in-one:1.76.0`（归档版本） |

第三阶段默认使用 Jaeger v2。

### 5.2 Docker Compose 部署

```yaml
services:
  # ... 现有服务不变 ...

  # ------------------------------------------
  # Jaeger 链路追踪
  # ------------------------------------------
  jaeger:
    image: cr.jaegertracing.io/jaegertracing/jaeger:2.17.0
    restart: unless-stopped
    command: ["--config=file:/etc/jaeger/jaeger.yaml"]
    volumes:
      - ./docker/jaeger/jaeger.yaml:/etc/jaeger/jaeger.yaml:ro
    ports:
      - "127.0.0.1:16686:16686"   # Jaeger UI
      - "127.0.0.1:4317:4317"     # OTLP gRPC
      - "127.0.0.1:4318:4318"     # OTLP HTTP（预留）
    deploy:
      resources:
        limits:
          cpus: "0.50"
          memory: 512M
```

**说明**：
- Docker Compose 开发环境使用显式 Jaeger v2 最小 Collector 风格配置，不再使用 Jaeger v1/伪 all-in-one 字段
- `docker/jaeger/jaeger.yaml` 已通过 `jaeger validate --config=file:/tmp/jaeger.yaml` 校验
- 开发环境使用内存存储，重启后 Trace 数据丢失（可接受）
- 4317 端口用于服务发送 Trace 数据（OTLP gRPC）
- 4318 端口用于服务发送 Trace 数据（OTLP HTTP，预留）
- 16686 端口用于 Jaeger UI 访问
- 当前配置包含 `receivers`、`processors`、`exporters`、`extensions`、`service`，其中 `jaeger_storage`、`jaeger_query`、`jaeger_storage_exporter` 共同组成单容器内存存储闭环

### 5.3 Helm Chart 部署

```yaml
# charts/server-monitor/templates/jaeger.yaml

apiVersion: v1
kind: ConfigMap
metadata:
  name: jaeger-config
data:
  jaeger.yaml: |
    service:
      extensions: [jaeger_storage, jaeger_query]
      pipelines:
        traces:
          receivers: [otlp]
          processors: [batch]
          exporters: [jaeger_storage_exporter]
    extensions:
      jaeger_storage:
        backends:
          memory_store:
            memory:
              max_traces: 100000
      jaeger_query:
        storage:
          traces: memory_store
        http:
          endpoint: 0.0.0.0:16686
        grpc:
          endpoint: 0.0.0.0:16685
    receivers:
      otlp:
        protocols:
          grpc:
            endpoint: 0.0.0.0:4317
          http:
            endpoint: 0.0.0.0:4318
    processors:
      batch:
    exporters:
      jaeger_storage_exporter:
        trace_storage: memory_store
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: jaeger
spec:
  replicas: 1
  selector:
    matchLabels:
      app: jaeger
  template:
    metadata:
      labels:
        app: jaeger
    spec:
      containers:
        - name: jaeger
          image: cr.jaegertracing.io/jaegertracing/jaeger:2.17.0
          args:
            - --config=file:/etc/jaeger/jaeger.yaml
          ports:
            - containerPort: 16686
            - containerPort: 4317
            - containerPort: 4318
          readinessProbe:
            httpGet:
              path: /
              port: 16686
            initialDelaySeconds: 10
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /
              port: 16686
            initialDelaySeconds: 30
            periodSeconds: 20
          volumeMounts:
            - name: jaeger-config
              mountPath: /etc/jaeger/jaeger.yaml
              subPath: jaeger.yaml
              readOnly: true
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
            limits:
              cpu: 500m
              memory: 512Mi
      volumes:
        - name: jaeger-config
          configMap:
            name: jaeger-config
---
apiVersion: v1
kind: Service
metadata:
  name: jaeger
spec:
  selector:
    app: jaeger
  ports:
    - name: otlp-grpc
      port: 4317
      targetPort: 4317
    - name: otlp-http
      port: 4318
      targetPort: 4318
    - name: ui
      port: 16686
      targetPort: 16686
```

Helm 与 Docker Compose 使用同一类 Jaeger v2 最小配置：ConfigMap 挂载到 `/etc/jaeger/jaeger.yaml`，容器参数使用 `--config=file:/etc/jaeger/jaeger.yaml`。现有 Service 端口和值结构保持不变。

### 5.4 Jaeger 存储说明

开发环境使用 Jaeger v2 显式最小 Collector 风格配置和内存存储，生产环境应在同一配置结构上切换为 Elasticsearch 等持久化存储。生产配置必须按 Jaeger v2 当前官方配置结构校验后再落地，避免使用 Jaeger v1 字段或未经验证的 YAML 字段。

```yaml
# 生产环境 Jaeger 部署形态示意，具体配置字段以 Jaeger v2 官方配置为准
containers:
  - name: jaeger
    image: cr.jaegertracing.io/jaegertracing/jaeger:2.17.0
    args:
      - --config=file:/jaeger/config.yaml
    volumeMounts:
      - name: config
        mountPath: /jaeger
data:
  config.yaml: |
    # 按 Jaeger v2 官方配置文件结构填写 Elasticsearch storage 配置。
    # 生产部署前必须用真实镜像启动验证配置字段和索引策略。
```

使用 ES 作为 Jaeger 存储的优势：
- 复用第二阶段已部署的 Elasticsearch
- Trace 数据持久化，重启不丢失
- 可通过 Kibana 直接查询 Trace 相关索引
- Jaeger ES 存储会创建 `jaeger-*` 索引，需在 ES ILM 策略中补充对应保留策略

第三阶段开发环境使用内存存储，生产配置预留 ES 存储选项。

---

## 六、VictoriaMetrics 接入

### 6.1 架构说明

```
server-probe → /metrics → Prometheus scrape
                              │
                              ├── 本地 TSDB（短期 15d）
                              │
                              └── remote_write → VictoriaMetrics（长期 90d）
                                                        │
                                                        └── Grafana 查询
```

VictoriaMetrics 作为 Prometheus 的远程存储后端，Prometheus 通过 `remote_write` 将指标数据发送到 VictoriaMetrics。

**查询策略**：
- **短期数据（≤15d）**：Grafana 查询 Prometheus 数据源
- **长期数据（>15d）**：Grafana 查询 VictoriaMetrics 数据源
- Grafana Dashboard 面板需根据时间范围选择对应数据源
- VictoriaMetrics 兼容 Prometheus 查询 API，Grafana 中 type 设为 `prometheus`

**保留时间对齐**：
- Prometheus 本地保留：15d（`--storage.tsdb.retention.time=15d`）
- VictoriaMetrics 保留：90d（`--retentionPeriod=90d`）

### 6.2 Docker Compose 部署

```yaml
services:
  # ... 现有服务不变 ...

  # ------------------------------------------
  # VictoriaMetrics 长期指标存储
  # ------------------------------------------
  victoriametrics:
    image: victoriametrics/victoria-metrics:v1.102.1
    restart: unless-stopped
    command:
      - "--storageDataPath=/victoria-metrics-data"
      - "--retentionPeriod=90d"
      - "--httpListenAddr=:8428"
    ports:
      - "127.0.0.1:8428:8428"
    volumes:
      - victoriametrics-data:/victoria-metrics-data
    deploy:
      resources:
        limits:
          cpus: "0.50"
          memory: 512M
```

#### Prometheus remote_write 配置

在现有 `prometheus.yml` 中追加：

```yaml
remote_write:
  - url: http://victoriametrics:8428/api/v1/write
    queue_config:
      max_samples_per_send: 10000
      capacity: 20000
      max_shards: 5
```

### 6.3 Helm Chart 部署

```yaml
# charts/server-monitor/templates/victoriametrics.yaml

apiVersion: apps/v1
kind: Deployment
metadata:
  name: victoriametrics
spec:
  replicas: 1
  selector:
    matchLabels:
      app: victoriametrics
  template:
    metadata:
      labels:
        app: victoriametrics
    spec:
      containers:
        - name: victoriametrics
          image: victoriametrics/victoria-metrics:v1.102.1
          args:
            - --storageDataPath=/victoria-metrics-data
            - --retentionPeriod={{ .Values.victoriametrics.retention }}
            - --httpListenAddr=:8428
          ports:
            - containerPort: 8428
          volumeMounts:
            - name: data
              mountPath: /victoria-metrics-data
          readinessProbe:
            httpGet:
              path: /health
              port: 8428
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /health
              port: 8428
            initialDelaySeconds: 10
            periodSeconds: 20
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
            limits:
              cpu: 500m
              memory: 512Mi
      volumes:
        - name: data
          {{- if .Values.victoriametrics.persistence.enabled }}
          persistentVolumeClaim:
            claimName: victoriametrics
          {{- else }}
          emptyDir: {}
          {{- end }}
---
apiVersion: v1
kind: Service
metadata:
  name: victoriametrics
spec:
  selector:
    app: victoriametrics
  ports:
    - port: 8428
      targetPort: 8428
```

### 6.4 Grafana 数据源配置

在现有 Grafana datasources ConfigMap 中新增 VictoriaMetrics 数据源：

```yaml
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    jsonData:
      timeInterval: "15s"
  - name: VictoriaMetrics
    type: prometheus
    access: proxy
    url: http://victoriametrics:8428
    editable: true
    jsonData:
      timeInterval: "15s"
  - name: Elasticsearch
    type: elasticsearch
    access: proxy
    url: http://elasticsearch:9200
    database: "sm-logs-*"
    jsonData:
      timeField: "@timestamp"
      esVersion: "8.13.0"
      logMessageField: msg
      logLevelField: level
    editable: true
```

### 6.5 values.yaml 新增配置

```yaml
victoriametrics:
  enabled: true
  image: victoriametrics/victoria-metrics:v1.102.1
  retention: 90d
  persistence:
    enabled: true
    accessModes:
      - ReadWriteOnce
    size: 20Gi
    storageClassName: ""
  service:
    type: ClusterIP
    port: 8428
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      cpu: 500m
      memory: 512Mi
```

---

## 七、Kafka 事件总线

### 7.1 架构说明

```
┌─────────────────────────────────────────────────────────────┐
│ Kafka 事件总线 (KRaft 模式，无 ZooKeeper)                     │
│                                                               │
│  Topics:                                                      │
│                                                               │
│  alert-events                                                 │
│  ├── 生产者: server-web (AlertManager Webhook → Kafka)       │
│  ├── 消费者: alert-service                                    │
│  ├── Key: fingerprint                                         │
│  ├── Value: JSON {type, fingerprint, status, labels, ...}    │
│  └── Partitions: 3, Replication: 1 (开发) / 3 (生产)         │
│                                                               │
│  operation-events                                             │
│  ├── 生产者: 预留（第三阶段不实现业务逻辑）                    │
│  ├── 消费者: 预留（第三阶段不实现业务逻辑）                    │
│  ├── Key: instance                                            │
│  ├── Value: JSON {type, instance, action, ...}               │
│  └── Partitions: 3, Replication: 1 (开发) / 3 (生产)         │
└─────────────────────────────────────────────────────────────┘
```

**operation-events 阶段边界**：第三阶段仅创建 `operation-events` Topic，不实现生产者和消费者业务逻辑。主机变更、配置变更等操作事件的生产和消费属于第四阶段职责。

### 7.2 事件格式

#### alert-events

```json
{
  "type": "alert",
  "fingerprint": "abc123",
  "status": "firing",
  "labels": {
    "alertname": "HighCPU",
    "instance": "server-1",
    "severity": "warning"
  },
  "annotations": {
    "summary": "CPU usage above 80%"
  },
  "starts_at": "2024-04-25T10:30:00Z",
  "ends_at": "0001-01-01T00:00:00Z",
  "source": "prometheus",
  "timestamp": "2024-04-25T10:30:00Z"
}
```

#### operation-events（预留格式，第三阶段不实现）

```json
{
  "type": "operation",
  "action": "host_status_change",
  "instance": "server-1",
  "old_status": "healthy",
  "new_status": "unhealthy",
  "timestamp": "2024-04-25T10:30:00Z"
}
```

### 7.3 Kafka 生产者设计 (server-web)

#### 新增文件

```
server-web/
├── kafka/
│   ├── producer.go      Kafka 异步生产者封装
│   └── topics.go        Topic 常量定义
```

#### producer.go（异步生产者）

```go
package kafka

import (
    "encoding/json"
    "sync"

    "github.com/IBM/sarama"
    "go.uber.org/zap"
)

const (
    TopicAlertEvents     = "alert-events"
    TopicOperationEvents = "operation-events"
)

type Producer struct {
    producer sarama.AsyncProducer
    wg       sync.WaitGroup
}

func NewProducer(brokers []string) (*Producer, error) {
    config := sarama.NewConfig()
    config.Producer.RequiredAcks = sarama.WaitForLocal
    config.Producer.Retry.Max = 3
    config.Producer.Return.Successes = true
    config.Producer.Return.Errors = true
    config.Producer.Partitioner = sarama.NewHashPartitioner
    config.Producer.Flush.Messages = 100
    config.Producer.Flush.Frequency = 500 * time.Millisecond

    producer, err := sarama.NewAsyncProducer(brokers, config)
    if err != nil {
        return nil, err
    }

    p := &Producer{producer: producer}

    p.wg.Add(2)
    go p.handleSuccesses()
    go p.handleErrors()

    return p, nil
}

func (p *Producer) handleSuccesses() {
    defer p.wg.Done()
    for msg := range p.producer.Successes() {
        zap.L().Debug("kafka message sent",
            zap.String("topic", msg.Topic),
            zap.Int32("partition", msg.Partition),
            zap.Int64("offset", msg.Offset),
        )
    }
}

func (p *Producer) handleErrors() {
    defer p.wg.Done()
    for err := range p.producer.Errors() {
        zap.L().Warn("kafka produce failed",
            zap.String("topic", err.Msg.Topic),
            zap.Error(err.Err),
        )
    }
}

func (p *Producer) SendAlertEvent(event AlertEvent) error {
    value, err := json.Marshal(event)
    if err != nil {
        return err
    }

    msg := &sarama.ProducerMessage{
        Topic: TopicAlertEvents,
        Key:   sarama.StringEncoder(event.Fingerprint),
        Value: sarama.ByteEncoder(value),
    }

    select {
    case p.producer.Input() <- msg:
        return nil
    default:
        return fmt.Errorf("kafka producer channel full, dropping event")
    }
}

func (p *Producer) Close() error {
    err := p.producer.Close()
    p.wg.Wait()
    return err
}
```

**设计说明**：

1. **使用 AsyncProducer**：消息通过 channel 异步发送，不阻塞 Webhook 请求处理。与 SyncProducer 相比，Webhook 响应时间不受 Kafka 写入延迟影响。
2. **降级策略**：当 producer channel 满时（`default` 分支），直接丢弃事件并返回错误。这保证了 Webhook 不会被 Kafka 背压阻塞。
3. **错误处理**：异步错误通过 `p.producer.Errors()` channel 消费，记录警告日志但不影响业务。
4. **批量刷新**：`Flush.Messages=100` + `Flush.Frequency=500ms`，平衡延迟和吞吐。
5. **WaitForLocal**：等待 Leader 确认即可，不等待 ISR 全部同步，降低延迟。

#### Webhook handler 改造

server-web 的 AlertManager Webhook handler 在现有逻辑（写入 Redis + Pub/Sub 广播）基础上，增加 Kafka 异步生产：

```go
func (h *Handlers) AlertmanagerWebhook(c *gin.Context) {
    // ... 现有解析逻辑 ...

    // 现有逻辑：写入 Redis alert:active + alert:events
    // 现有逻辑：Redis Pub/Sub 广播

    // 新增：异步发送到 Kafka
    if h.kafkaProducer != nil {
        for _, alert := range alerts {
            event := kafka.AlertEvent{
                Type:        "alert",
                Fingerprint: alert.Fingerprint,
                Status:      alert.Status,
                Labels:      alert.Labels,
                Annotations: alert.Annotations,
                StartsAt:    alert.StartsAt,
                EndsAt:      alert.EndsAt,
                Source:      "prometheus",
                Timestamp:   time.Now().UTC(),
            }
            if err := h.kafkaProducer.SendAlertEvent(event); err != nil {
                logger.FromContext(c.Request.Context()).Warn("kafka produce alert event failed",
                    zap.String("fingerprint", alert.Fingerprint),
                    zap.Error(err),
                )
            }
        }
    }
}
```

**关键约束**：
- Kafka 生产失败不应影响现有告警链路（Redis 写入 + Pub/Sub 广播）
- Kafka 不可用时，降级为仅走 Redis 链路
- Kafka 生产是异步增强，不是同步依赖
- `SendAlertEvent` 使用 `select + default` 非阻塞发送，channel 满时丢弃事件

### 7.4 Docker Compose 部署（KRaft 模式）

```yaml
services:
  # ... 现有服务不变 ...

  # ------------------------------------------
  # Kafka 事件总线 (KRaft 模式，无 ZooKeeper)
  # ------------------------------------------
  kafka:
    image: confluentinc/cp-kafka:7.6.1
    restart: unless-stopped
    ports:
      - "127.0.0.1:19092:19092"
    environment:
      KAFKA_NODE_ID: 1
      KAFKA_PROCESS_ROLES: broker,controller
      KAFKA_LISTENERS: INTERNAL://:9092,EXTERNAL://:19092,CONTROLLER://:9093
      KAFKA_ADVERTISED_LISTENERS: INTERNAL://kafka:9092,EXTERNAL://localhost:19092
      KAFKA_INTER_BROKER_LISTENER_NAME: INTERNAL
      KAFKA_CONTROLLER_LISTENER_NAMES: CONTROLLER
      KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: CONTROLLER:PLAINTEXT,INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT
      KAFKA_CONTROLLER_QUORUM_VOTERS: 1@kafka:9093
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
      KAFKA_AUTO_CREATE_TOPICS_ENABLE: "false"
      KAFKA_LOG_RETENTION_HOURS: 72
      CLUSTER_ID: "MkU3OEVBNTcwNTJENDM2Qk"
    volumes:
      - kafka-data:/var/lib/kafka/data
    deploy:
      resources:
        limits:
          cpus: "0.50"
          memory: 512M

  # ------------------------------------------
  # Kafka Topic 初始化
  # ------------------------------------------
  kafka-init:
    image: confluentinc/cp-kafka:7.6.1
    restart: "no"
    depends_on:
      - kafka
    entrypoint: ["/bin/sh", "-c"]
    command:
      - |
        echo "Waiting for Kafka to be ready..."
        until kafka-topics --bootstrap-server kafka:9092 --list > /dev/null 2>&1; do
          sleep 2
        done
        echo "Creating topics..."
        kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic alert-events --partitions 3 --replication-factor 1
        kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic operation-events --partitions 3 --replication-factor 1
        echo "Topics created."

volumes:
  kafka-data:
  victoriametrics-data:
```

**KRaft 模式说明**：
- `KAFKA_PROCESS_ROLES: broker,controller` — 单节点同时承担 broker 和 controller 角色
- `KAFKA_CONTROLLER_QUORUM_VOTERS: 1@kafka:9093` — controller 投票者列表
- `CLUSTER_ID` — KRaft 模式必须提供集群 ID（Base64 编码的 UUID）
- 无需 ZooKeeper，减少一个中间件组件

**双 Listener 说明**：
- `INTERNAL://kafka:9092` — 容器网络内访问（server-web、alert-service 等使用）
- `EXTERNAL://localhost:19092` — 宿主机调试访问（kafka-topics 命令行工具等）
- `KAFKA_INTER_BROKER_LISTENER_NAME: INTERNAL` — 明确 broker 间通信使用内部 listener
- 容器内服务连接 `kafka:9092`，宿主机工具连接 `localhost:19092`

### 7.5 Helm Chart 部署（KRaft 模式）

```yaml
# charts/server-monitor/templates/kafka.yaml

# Kafka StatefulSet (KRaft 模式)
# 注意：此配置仅适用于 dev/demo 环境，broker 和 controller 合并进程。
# 生产环境应使用独立 controller/broker、Strimzi Operator、Bitnami Kafka Chart 或托管 Kafka 服务。
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: kafka
spec:
  serviceName: kafka
  replicas: 1
  selector:
    matchLabels:
      app: kafka
  template:
    metadata:
      labels:
        app: kafka
    spec:
      containers:
        - name: kafka
          image: confluentinc/cp-kafka:7.6.1
          env:
            - name: KAFKA_NODE_ID
              value: "1"
            - name: KAFKA_PROCESS_ROLES
              value: "broker,controller"
            - name: KAFKA_LISTENERS
              value: "INTERNAL://:9092,EXTERNAL://:19092,CONTROLLER://:9093"
            - name: KAFKA_ADVERTISED_LISTENERS
              value: "INTERNAL://kafka:9092,EXTERNAL://localhost:19092"
            - name: KAFKA_INTER_BROKER_LISTENER_NAME
              value: "INTERNAL"
            - name: KAFKA_CONTROLLER_LISTENER_NAMES
              value: "CONTROLLER"
            - name: KAFKA_LISTENER_SECURITY_PROTOCOL_MAP
              value: "CONTROLLER:PLAINTEXT,INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT"
            - name: KAFKA_CONTROLLER_QUORUM_VOTERS
              value: "1@kafka:9093"
            - name: KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR
              value: "1"
            - name: KAFKA_AUTO_CREATE_TOPICS_ENABLE
              value: "false"
            - name: KAFKA_LOG_RETENTION_HOURS
              value: {{ .Values.kafka.logRetentionHours | quote }}
            - name: CLUSTER_ID
              value: {{ .Values.kafka.clusterId | quote }}
          ports:
            - containerPort: 9092
            - containerPort: 9093
          readinessProbe:
            tcpSocket:
              port: 9092
            initialDelaySeconds: 15
            periodSeconds: 10
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
            limits:
              cpu: 500m
              memory: 512Mi
---
apiVersion: v1
kind: Service
metadata:
  name: kafka
spec:
  selector:
    app: kafka
  ports:
    - name: client
      port: 9092
      targetPort: 9092
    - name: controller
      port: 9093
      targetPort: 9093
---
# Kafka Topic 初始化 Job
apiVersion: batch/v1
kind: Job
metadata:
  name: kafka-init
  annotations:
    "helm.sh/hook": post-install,post-upgrade
    "helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
spec:
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: kafka-init
          image: confluentinc/cp-kafka:7.6.1
          command:
            - /bin/sh
            - -c
            - |
              echo "Waiting for Kafka to be ready..."
              until kafka-topics --bootstrap-server kafka:9092 --list > /dev/null 2>&1; do
                sleep 2
              done
              echo "Creating topics..."
              kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic alert-events --partitions 3 --replication-factor 1
              kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic operation-events --partitions 3 --replication-factor 1
              echo "Topics created."
```

### 7.6 values.yaml 新增配置

```yaml
kafka:
  enabled: true
  image: confluentinc/cp-kafka:7.6.1
  clusterId: "MkU3OEVBNTcwNTJENDM2Qk"
  topics:
    alertEvents:
      name: alert-events
      partitions: 3
      replicationFactor: 1
    operationEvents:
      name: operation-events
      partitions: 3
      replicationFactor: 1
  logRetentionHours: 72
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      cpu: 500m
      memory: 512Mi
```

---

## 八、alert-service 开发

### 8.1 服务定位

alert-service 是第三阶段新增的微服务，职责：

1. **Kafka 消费**：订阅 `alert-events` Topic，消费告警事件
2. **告警去重**：基于 fingerprint 去重，避免重复处理
3. **告警聚合**：按 alertname + instance 聚合，合并相同告警
4. **状态管理**：维护告警状态（firing / resolved / acknowledged），写入 Redis
5. **事件增强**：为告警事件添加额外上下文（如关联指标、历史趋势）

**alert-service 不做**：

- 不替代 AlertManager 的告警路由和分组
- 不替代 server-web 的 WebSocket 推送
- 不直接接收 AlertManager Webhook（告警仍由 server-web 接收后写入 Kafka）
- 不管理通知渠道

**与总设计的职责对齐**：alert-service 统一定位为 `Kafka 消费 → 去重 / 聚合 / 状态管理 / 上下文增强`。AlertManager 继续负责 Prometheus 规则告警路由，server-web 继续负责 Webhook 接入和实时推送。

### 8.2 目录结构

```
alert-service/
├── main.go                 入口 + graceful shutdown + HTTP 健康检查
├── config/
│   └── config.go           配置
├── logger/
│   └── logger.go           zap 日志初始化（与 server-web 相同）
├── tracer/
│   └── tracer.go           OTel 初始化（与 server-web 相同）
├── kafka/
│   ├── consumer.go         Kafka 消费者封装
│   └── topics.go           Topic 常量
├── redis/
│   └── client.go           Redis 客户端
├── alert/
│   ├── dedup.go            告警去重
│   ├── aggregator.go       告警聚合
│   └── store.go            告警状态存储
├── health/
│   └── handler.go          /healthz + /readyz
├── Dockerfile
├── go.mod
└── go.sum
```

### 8.3 核心代码设计

#### 配置 (config/config.go)

```go
type Config struct {
    KafkaBrokers   []string
    KafkaGroupID   string
    RedisAddr      string
    RedisPassword  string
    OTELEndpoint   string
    OTELSampleRate float64
    LogLevel       string
    ListenAddr     string
}

func Load() *Config {
    return &Config{
        KafkaBrokers:   getEnvSlice("KAFKA_BROKERS", []string{"kafka:9092"}),
        KafkaGroupID:   getEnv("KAFKA_GROUP_ID", "alert-service"),
        RedisAddr:      getEnv("REDIS_ADDR", "redis:6379"),
        RedisPassword:  getEnv("REDIS_PASSWORD", ""),
        OTELEndpoint:   getEnv("TRACE_OTLP_ENDPOINT", "jaeger:4317"),
        OTELSampleRate: getEnvFloat("TRACE_SAMPLE_RATE", 1.0),
        LogLevel:       getEnv("LOG_LEVEL", "info"),
        ListenAddr:     getEnv("LISTEN_ADDR", ":8080"),
    }
}
```

#### Kafka 消费者 (kafka/consumer.go) — at-least-once 语义

```go
type Consumer struct {
    consumer sarama.ConsumerGroup
    handler  *AlertHandler
    topics   []string
    ready    chan struct{}
}

type AlertHandler struct {
    store *alert.Store
}

func (h *AlertHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *AlertHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *AlertHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
    for msg := range claim.Messages() {
        var event AlertEvent
        if err := json.Unmarshal(msg.Value, &event); err != nil {
            zap.L().Warn("unmarshal alert event failed", zap.Error(err))
            session.MarkMessage(msg, "")
            continue
        }

        if err := h.store.Process(event); err != nil {
            zap.L().Error("process alert event failed, skipping offset commit",
                zap.String("fingerprint", event.Fingerprint),
                zap.Error(err),
            )
            // at-least-once: 处理失败时不提交 offset，下次重新消费
            // 不执行 session.MarkMessage，让消息在下次 rebalance 时重新投递
            continue
        }

        session.MarkMessage(msg, "")
    }
    return nil
}
```

**消费确认语义**：采用 **at-least-once** 语义：
- 处理成功后才提交 offset（`session.MarkMessage`）
- 处理失败时不提交 offset，消息会在下次 rebalance 时重新投递
- JSON 解析失败的消息直接提交 offset（属于不可恢复错误）
- 去重逻辑（基于 Redis `SetNX`）保证即使消息重复投递也不会重复处理
- 未来可扩展死信 Topic：连续失败 N 次的消息写入 `alert-events-dlq`，避免阻塞消费

#### 告警状态存储 (alert/store.go)

```go
type Store struct {
    redis *redis.Client
}

func (s *Store) Process(event AlertEvent) error {
    ctx := context.Background()

    dedupKey := fmt.Sprintf("alert:dedup:%s:%s", event.Fingerprint, event.Status)
    ok, err := s.redis.SetNX(ctx, dedupKey, "1", 5*time.Minute).Result()
    if err != nil {
        return fmt.Errorf("dedup check failed: %w", err)
    }
    if !ok {
        return nil
    }

    switch event.Status {
    case "firing":
        return s.handleFiring(ctx, event)
    case "resolved":
        return s.handleResolved(ctx, event)
    }

    return nil
}

func (s *Store) handleFiring(ctx context.Context, event AlertEvent) error {
    data, err := json.Marshal(event)
    if err != nil {
        return fmt.Errorf("marshal alert event: %w", err)
    }
    if err := s.redis.HSet(ctx, "alert:active:enriched", event.Fingerprint, data).Err(); err != nil {
        return fmt.Errorf("write enriched active alert: %w", err)
    }
    if err := s.redis.HIncrBy(ctx, "alert:stats", event.Labels["alertname"], 1).Err(); err != nil {
        return fmt.Errorf("update alert stats: %w", err)
    }
    return nil
}

func (s *Store) handleResolved(ctx context.Context, event AlertEvent) error {
    if err := s.redis.HDel(ctx, "alert:active:enriched", event.Fingerprint).Err(); err != nil {
        return fmt.Errorf("remove enriched active alert: %w", err)
    }
    return nil
}
```

#### 健康检查 (health/handler.go)

```go
package health

import (
    "net/http"
    "sync/atomic"

    "github.com/redis/go-redis/v9"
)

type Handler struct {
    redis      *redis.Client
    kafkaReady atomic.Bool
}

func NewHandler(redisClient *redis.Client) *Handler {
    h := &Handler{
        redis: redisClient,
    }
    h.kafkaReady.Store(false)
    return h
}

func (h *Handler) SetKafkaReady(ready bool) {
    h.kafkaReady.Store(ready)
}

func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("ok"))
}

func (h *Handler) Readyz(w http.ResponseWriter, r *http.Request) {
    if !h.kafkaReady.Load() {
        http.Error(w, "kafka not ready", http.StatusServiceUnavailable)
        return
    }
    if err := h.redis.Ping(r.Context()).Err(); err != nil {
        http.Error(w, "redis not ready", http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("ok"))
}
```

**说明**：
- `/healthz`：存活检查，进程在即返回 200
- `/readyz`：就绪检查，Kafka consumer 已加入 group 且 Redis 连通时返回 200
- `kafkaReady` 使用标准库 `sync/atomic.Bool`，在初始化时 `Store(false)`
- `kafkaReady` 在 Kafka consumer 首次成功加入 ConsumerGroup 后设为 `true`
- `kafkaReady` 在 rebalance cleanup、consumer 退出或 `Consume` 返回错误时应设为 `false`（见 main.go 示例）

### 8.4 主入口 (main.go)

```go
func main() {
    cfg := config.Load()

    log, err := logger.Init("alert-service")
    if err != nil {
        fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
        os.Exit(1)
    }
    defer logger.Sync(log)

    shutdown, err := tracer.Init(tracer.Config{
        ServiceName:  "alert-service",
        OTELEndpoint: cfg.OTELEndpoint,
        SampleRate:   cfg.OTELSampleRate,
    })
    if err != nil {
        zap.L().Warn("tracer init failed, tracing disabled", zap.Error(err))
    } else {
        defer shutdown(context.Background())
    }

    redisClient := redis.NewClient(cfg.RedisAddr, cfg.RedisPassword)
    store := alert.NewStore(redisClient)

    healthHandler := health.NewHandler(redisClient)

    consumer, err := kafka.NewConsumer(cfg.KafkaBrokers, cfg.KafkaGroupID, store)
    if err != nil {
        zap.L().Fatal("kafka consumer init failed", zap.Error(err))
    }
    defer consumer.Close()

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go func() {
        if err := consumer.Consume(ctx, func() {
            healthHandler.SetKafkaReady(true)
        }); err != nil {
            zap.L().Error("kafka consumer error", zap.Error(err))
            cancel()
        }
    }()

    mux := http.NewServeMux()
    mux.HandleFunc("/healthz", healthHandler.Healthz)
    mux.HandleFunc("/readyz", healthHandler.Readyz)
    httpServer := &http.Server{
        Addr:    cfg.ListenAddr,
        Handler: mux,
    }

    go func() {
        zap.L().Info("alert-service http server listening", zap.String("addr", cfg.ListenAddr))
        if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            zap.L().Fatal("http server error", zap.Error(err))
        }
    }()

    zap.L().Info("alert-service started")

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
    <-sigCh

    zap.L().Info("alert-service shutting down")
    shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer shutdownCancel()
    httpServer.Shutdown(shutdownCtx)
    cancel()
}
```

### 8.5 Dockerfile

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o alert-service .

FROM alpine:latest
RUN apk add --no-cache tzdata
WORKDIR /app
COPY --from=builder /app/alert-service .
EXPOSE 8080
CMD ["/app/alert-service"]
```

### 8.6 Helm Chart 部署

```yaml
# charts/server-monitor/templates/alert-service.yaml

apiVersion: apps/v1
kind: Deployment
metadata:
  name: alert-service
spec:
  replicas: 1
  selector:
    matchLabels:
      app: alert-service
  template:
    metadata:
      labels:
        app: alert-service
    spec:
      containers:
        - name: alert-service
          image: {{ .Values.alertService.image | quote }}
          env:
            - name: KAFKA_BROKERS
              value: "kafka:9092"
            - name: KAFKA_GROUP_ID
              value: "alert-service"
            - name: REDIS_ADDR
              value: "redis:6379"
            - name: REDIS_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: monitor-secret
                  key: REDIS_PASSWORD
            - name: TRACE_OTLP_ENDPOINT
              value: "jaeger:4317"
            - name: TRACE_SAMPLE_RATE
              value: "1.0"
            - name: LOG_LEVEL
              valueFrom:
                configMapKeyRef:
                  name: monitor-config
                  key: LOG_LEVEL
            - name: LISTEN_ADDR
              value: ":8080"
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 20
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
---
apiVersion: v1
kind: Service
metadata:
  name: alert-service
spec:
  selector:
    app: alert-service
  ports:
    - port: 8080
      targetPort: 8080
```

### 8.7 values.yaml 新增配置

```yaml
alertService:
  enabled: true
  image: 05allan1213/alert-service:latest
  replicaCount: 1
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      cpu: 200m
      memory: 128Mi
```

---

## 九、Grafana 联动增强

### 9.1 新增 Jaeger 数据源

在 Grafana datasources ConfigMap 中新增 Jaeger 和 VictoriaMetrics 数据源：

```yaml
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    jsonData:
      timeInterval: "15s"
  - name: VictoriaMetrics
    type: prometheus
    access: proxy
    url: http://victoriametrics:8428
    editable: true
    jsonData:
      timeInterval: "15s"
  - name: Elasticsearch
    type: elasticsearch
    access: proxy
    url: http://elasticsearch:9200
    database: "sm-logs-*"
    jsonData:
      timeField: "@timestamp"
      esVersion: "8.13.0"
      logMessageField: msg
      logLevelField: level
    editable: true
  - name: Jaeger
    type: jaeger
    access: proxy
    url: http://jaeger:16686
    editable: true
```

### 9.2 Trace-Log 联动配置

Grafana 当前 Jaeger Trace to Logs 自动入口的目标日志数据源仅支持 Loki 或 Splunk，不支持 Elasticsearch 作为 `tracesToLogsV2.datasourceUid`。第三阶段不引入 Loki，因此本阶段采用：

- Jaeger Trace 页面展示 Trace 数据。
- Elasticsearch Explore 手动按 `trace_id` 查询关联日志。
- Elasticsearch 日志详情通过 `dataLinks` 点击 `trace_id` 跳转到 Jaeger Trace。

#### Elasticsearch 数据源：Log to Trace

```yaml
- name: Elasticsearch
  type: elasticsearch
  access: proxy
  url: http://elasticsearch:9200
  database: "sm-logs-*"
  jsonData:
    timeField: "@timestamp"
    esVersion: "8.13.0"
    logMessageField: msg
    logLevelField: level
    dataLinks:
      - datasourceUid: jaeger
        field: trace_id
        url: "$${__value.raw}"
  editable: true
```

**字段说明**：
- `datasourceUid`：Jaeger 数据源的 UID
- `field`：ES 日志中的 `trace_id` 字段名
- `url`：传给 Jaeger 数据源的 Trace ID，使用字段原始值

### 9.3 Dashboard 新增面板

| Dashboard | 新增面板 | 数据源 |
|-----------|---------|--------|
| 服务监控 | Trace 速率 | Jaeger |
| 服务监控 | Trace 延迟分布 | Jaeger |
| 服务监控 | Kafka 消息速率 | Prometheus (kafka_consumer_* metrics) |
| 长期指标 | CPU 趋势（90 天） | VictoriaMetrics |
| 长期指标 | 内存趋势（90 天） | VictoriaMetrics |

---

## 十、Docker Compose 改造汇总

### 10.1 新增服务

| 服务 | 镜像 | 端口 | 用途 |
|------|------|------|------|
| jaeger | cr.jaegertracing.io/jaegertracing/jaeger:2.17.0 | 16686, 4317, 4318 | 链路追踪 |
| victoriametrics | victoriametrics/victoria-metrics:v1.102.1 | 8428 | 长期指标存储 |
| kafka | confluentinc/cp-kafka:7.6.1 | 9092（内部）/ 19092（宿主机） | 事件总线（KRaft） |
| kafka-init | confluentinc/cp-kafka:7.6.1 | 无 | Topic 初始化 |
| alert-service | 自建 | 8080 | 告警事件处理 |

### 10.2 现有服务环境变量新增

```yaml
services:
  server-probe:
    environment:
      TRACE_OTLP_ENDPOINT: "jaeger:4317"
      TRACE_SAMPLE_RATE: "1.0"

  server-web:
    environment:
      TRACE_OTLP_ENDPOINT: "jaeger:4317"
      TRACE_SAMPLE_RATE: "1.0"
      KAFKA_BROKERS: "kafka:9092"

  prometheus:
    # 追加 remote_write 配置（通过挂载更新后的 prometheus.yml）
```

---

## 十一、Helm Chart 改造汇总

### 11.1 新增模板文件

```
charts/server-monitor/templates/
├── ... 现有文件不变 ...
├── jaeger.yaml            Jaeger 部署
├── victoriametrics.yaml   VictoriaMetrics 部署
├── kafka.yaml             Kafka (KRaft) + Topic 初始化
└── alert-service.yaml     alert-service 部署
```

### 11.2 现有模板修改

| 文件 | 修改内容 |
|------|---------|
| configmap.yaml | 新增 TRACE_OTLP_ENDPOINT、TRACE_SAMPLE_RATE、KAFKA_BROKERS |
| grafana.yaml | datasources 新增 VictoriaMetrics + Jaeger（含 Trace-Log 联动配置），Dashboard 新增 Trace/Kafka 面板 |
| prometheus.yaml | 追加 remote_write 配置 |
| server-probe.yaml | 新增 TRACE_OTLP_ENDPOINT + TRACE_SAMPLE_RATE 环境变量 |
| server-web.yaml | 新增 TRACE_OTLP_ENDPOINT + TRACE_SAMPLE_RATE + KAFKA_BROKERS 环境变量 |

---

## 十二、CI/CD 改造

### 12.1 GitHub Actions 新增

| 步骤 | 内容 | 说明 |
|------|------|------|
| 构建 alert-service 镜像 | 新增 matrix 服务 | 与 server-probe / server-web 并行构建 |
| Kafka 配置校验 | `kafka-topics --bootstrap-server` 检查 | 仅在集成测试环境执行 |
| OTel SDK 编译检查 | `go build` 包含 OTel 依赖 | 已有 go build 步骤覆盖 |

### 12.2 Docker 构建影响

新增 `alert-service` 镜像构建，CI matrix 扩展为：

```yaml
strategy:
  matrix:
    service: [server-probe, server-web, frontend, alert-service]
```

---

## 十三、实施步骤

| 步骤 | 状态 | 内容 | 验证标准 |
|------|------|------|---------|
| 1 | 已完成 | server-probe 新增 tracer 包、TRACE 环境变量配置 | tracer.Init 成功初始化，降级场景不阻塞启动 |
| 2 | 已完成 | server-probe HTTP middleware 增加 otelhttp 包装 | 请求自动产生 trace_id，Jaeger 可查到 Span |
| 3 | 已完成 | server-probe 请求日志改用 logger.FromContext | 日志中包含 trace_id / span_id |
| 4 | 已完成 | server-web 新增 tracer 包、TRACE 环境变量配置 | tracer.Init 成功初始化 |
| 5 | 已完成 | server-web 路由增加 otelgin 中间件 | 请求自动产生 trace_id |
| 6 | 已完成 | server-web 请求日志改用 logger.FromContext | 日志中包含 trace_id / span_id |
| 7 | 已完成 | server-web Prometheus 客户端增加 otelhttp Transport | 出站请求传播 trace context |
| 8 | 已完成 | Docker Compose 新增 Jaeger | Compose 配置校验通过 |
| 9 | 已完成 | Helm Chart 新增 Jaeger | Helm lint/template 通过 |
| 10 | 已完成 | Docker Compose 新增 VictoriaMetrics + Prometheus remote_write | Compose 配置校验通过 |
| 11 | 已完成 | Helm Chart 新增 VictoriaMetrics | Helm lint/template 通过 |
| 12 | 已完成 | Grafana 新增 VictoriaMetrics + Jaeger 数据源（含 Trace-Log 联动） | 数据源配置静态校验通过 |
| 13 | 已完成 | server-web 新增 kafka 包（AsyncProducer）、KAFKA_BROKERS 配置 | go test / go vet 通过 |
| 14 | 已完成 | server-web Webhook handler 增加 Kafka 异步生产 | go test / go vet 通过 |
| 15 | 已完成 | Docker Compose 新增 Kafka (KRaft) + kafka-init | docker compose config 通过 |
| 16 | 已完成 | Helm Chart 新增 Kafka (KRaft) | Helm lint/template 通过 |
| 17 | 已完成 | alert-service 项目初始化（目录、go.mod、Dockerfile） | go test / go vet / go build 通过 |
| 18 | 已完成 | alert-service Kafka 消费者实现（at-least-once） | 消费失败不提交 offset 的单元测试通过 |
| 19 | 已完成 | alert-service 告警去重和聚合实现 | 去重、聚合、resolved 处理单元测试通过 |
| 20 | 已完成 | alert-service Redis 状态管理实现 | Redis Store fake client 单元测试通过 |
| 21 | 已完成 | alert-service 健康检查实现（/healthz + /readyz） | health handler 单元测试通过 |
| 22 | 已完成 | Docker Compose 新增 alert-service | docker compose config 通过 |
| 23 | 已完成 | Helm Chart 新增 alert-service | Helm lint/template 通过 |
| 24 | 已完成 | Grafana Dashboard 新增 Trace/Kafka 面板 | Docker JSON 校验、Helm template 通过 |
| 25 | 已完成 | 端到端验证 | 告警 → Kafka → alert-service → Redis 全链路，受控 firing/resolved webhook 验收通过 |

---

## 十四、验收标准

### 新增应用指标

本阶段新增的 Prometheus 指标如下：

- `server_web_kafka_alert_events_total{result}`：server-web Kafka 告警事件生产计数，`result` 固定为 `queued`、`dropped`、`send_success`、`send_error`。
- `alert_service_kafka_messages_total{result}`：alert-service Kafka 消息处理计数，`result` 固定为 `processed`、`invalid_json`、`process_error`。
- `alert_service_alert_events_total{status,result}`：alert-service 告警事件处理结果计数，`status` 为 `firing`、`resolved` 或 `unknown`，`result` 为 `stored`、`deduped`、`failed`。
- `alert_service_kafka_ready`：alert-service Kafka consumer ready 状态，`1` 表示 ready，`0` 表示 not ready。

Dashboard 来源：

- Docker Compose：`docker/grafana/dashboards/trace-kafka-overview.json`。
- Helm Chart：`charts/server-monitor/templates/grafana.yaml` 内嵌 `trace-kafka-overview.json`。

说明：当前 Dashboard 使用应用级 producer/consumer 指标和 Elasticsearch 日志，不依赖 Kafka broker exporter 指标。

### 当前验证边界

已完成的工程验证包括：

- `GOCACHE=/tmp/server-monitor-*-go-cache go test ./...`、`go vet ./...` 覆盖 server-probe、server-web、alert-service 对应模块。
- `docker run --rm ... cr.jaegertracing.io/jaegertracing/jaeger:2.17.0 validate --config=file:/tmp/jaeger.yaml` 覆盖 Jaeger v2 配置。
- `docker compose config` 覆盖 Jaeger、VictoriaMetrics、Kafka、alert-service、Grafana Dashboard provisioning 的 Compose 静态配置。
- `helm lint charts/server-monitor` 和 `helm template server-monitor charts/server-monitor` 覆盖 Helm 静态渲染。
- `promtool check config`、`promtool check rules` 通过 Prometheus 容器内 `promtool` 校验，覆盖 Prometheus 配置和告警规则。
- `docker compose up -d --force-recreate jaeger server-probe server-web alert-service` 完成运行态复验，Jaeger 不再重启，三个核心服务 healthy。

已完成的运行态验收包括：

- Jaeger UI `http://127.0.0.1:16686/` 返回 200，`/api/services` 包含 `server-probe`、`server-web`、`alert-service`，三类服务均可查询到 trace 数据。
- Elasticsearch `sm-logs-*` 中已查到非空 `trace_id` / `span_id` 日志，Kibana `/api/status` 为 available。
- VictoriaMetrics `/api/v1/query?query=up` 返回 `server-probe`、`server-web`、`alert-service` 的 `up=1`。
- Kafka topic 列表包含 `alert-events`、`operation-events`。
- 受控 Alertmanager webhook 验收通过：firing/resolved 均返回 202，server-web Kafka `queued/send_success` 和 alert-service `processed/stored` 指标递增，resolved 后 active alerts 不再包含测试 fingerprint。
- Jaeger、VictoriaMetrics、Kafka、alert-service 当前运行内存分别约为 11.4MiB、30.22MiB、399.6MiB、8.598MiB，均低于 Compose limits。

仍需人工或长窗口数据确认：

- Elasticsearch 日志详情中的 `trace_id` data link 需要浏览器人工点击确认。
- Jaeger → Elasticsearch 自动跳转在当前 Jaeger + Elasticsearch 数据源组合下不作为验收项；若强制要求该方向自动跳转，需要引入 Loki 或 Splunk 作为日志数据源。
- VictoriaMetrics 超过 Prometheus 本地保留期（15d）的历史查询需要实际保留窗口内的数据，无法在新启动的本地环境自动证明。
- Kafka 不可用、Jaeger 不可用、producer channel 满等故障注入场景本轮未主动破坏运行栈验证。

### 功能验收

- [x] server-probe HTTP 请求自动产生 trace_id / span_id
- [x] server-web HTTP 请求自动产生 trace_id / span_id
- [x] 日志中包含真实的 trace_id / span_id（非空字符串）
- [x] Jaeger UI 可查询和展示 Trace 链路
- [x] Kibana 可通过 trace_id 过滤日志
- [ ] Grafana Elasticsearch 日志详情可通过 trace_id 跳转到 Jaeger Trace
- [x] Prometheus remote_write 成功写入 VictoriaMetrics
- [ ] Grafana 可查询超过 Prometheus 本地保留期（15d）的历史指标数据
- [x] Kafka 集群可用（KRaft 模式），alert-events Topic 已创建
- [x] server-web 告警事件异步写入 Kafka
- [x] alert-service 成功消费 Kafka 告警事件
- [ ] alert-service 告警去重生效（相同 fingerprint 不重复处理）
- [x] alert-service /healthz 和 /readyz 探针正常工作
- [x] 告警双通道可用：受控 Alertmanager webhook → Kafka → alert-service 已验收，Prometheus Rules → AlertManager 配置已通过静态校验

### 端到端验收用例

- [x] 访问 server-web API 后，Jaeger 能查到对应的 Trace，包含 HTTP Span
- [x] 访问 server-web API 后，Kibana 能查到带 trace_id 的日志
- [ ] 在 Grafana Elasticsearch 日志详情中点击 trace_id，能跳转到 Jaeger 查看完整链路
- [x] 在 Grafana Elasticsearch Explore 中手动按 trace_id 能查到关联日志
- [x] 触发告警后，Kafka alert-events Topic 中有对应消息
- [x] alert-service 消费告警事件后，Redis 中有对应的告警状态
- [ ] VictoriaMetrics 中能查询超过 15 天的指标数据
- [ ] Kafka 不可用时，server-web 告警链路（Redis + Pub/Sub + WebSocket）仍正常
- [ ] Jaeger 不可用时，服务正常启动，OTel exporter 记录错误但不阻塞请求
- [ ] Kafka producer channel 满时，Webhook 请求正常返回，事件被丢弃

### 非功能验收

- [ ] OTel SDK 初始化失败不阻塞服务启动
- [x] Kafka 异步生产不阻塞 Webhook 接收路径，受控 webhook 返回 202
- [ ] Kafka 生产失败不影响现有告警链路
- [x] Jaeger 内存占用 < 512MB
- [x] VictoriaMetrics 内存占用 < 512MB
- [x] Kafka 内存占用 < 512MB
- [x] alert-service 内存占用 < 128MB
- [x] 采样率可配置，生产环境可降低采样比例
- [ ] 现有功能（指标采集、告警推送、WebSocket、日志链路）不受影响；本轮已覆盖指标采集、告警推送、日志链路，WebSocket 浏览器链路未验收

### 兼容性验收

- [x] 第一、二阶段核心功能正常
- [x] Docker Compose 核心服务启动正常
- [x] Helm Chart `helm lint/template` 静态校验通过
- [x] 现有 API 接口行为不变
- [x] 现有日志格式不变（仅新增 trace_id / span_id 字段）
- [x] 现有 Prometheus 告警规则不受影响

---

## 十五、风险与注意事项

### 15.1 OpenTelemetry SDK 性能影响

OTel SDK 会为每个 HTTP 请求创建 Span，可能对高 QPS 服务产生性能影响：
- 开发环境使用 `SampleRate=1.0`（全量采样）
- 生产环境建议 `SampleRate=0.1`（10% 采样）或更低
- 使用 `ParentBased` 采样策略，确保同一链路的 Span 不会被拆分
- OTel SDK 使用 BatchSpanProcessor，批量发送 Span，减少网络开销

### 15.2 Kafka 运维复杂度

Kafka 是重量级中间件，引入后需关注：
- KRaft 模式下 controller 和 broker 的角色分配
- Topic 分区数和副本数的合理配置
- Consumer Group 的 offset 管理和 rebalance
- 磁盘空间监控（Kafka 数据目录增长）

### 15.3 alert-service 消费者 Rebalance

Kafka Consumer Group 在以下场景会触发 rebalance：
- 新消费者加入或旧消费者离开
- Topic 分区数变化
- 消费者心跳超时

rebalance 期间消费者暂停消费，需注意：
- 合理设置 `session.timeout.ms` 和 `heartbeat.interval.ms`
- 避免单次消费处理时间过长
- 使用 `sarama.Config.Consumer.Group.Rebalance.Strategy` 选择合适的 rebalance 策略

### 15.4 VictoriaMetrics 数据一致性

Prometheus remote_write 存在以下注意点：
- remote_write 是异步的，VictoriaMetrics 数据可能有几秒延迟
- Prometheus 本地 TSDB 仍是权威数据源（短期 15d），VictoriaMetrics 是长期存储副本（90d）
- Grafana 查询短期数据走 Prometheus，长期数据走 VictoriaMetrics
- 如果 VictoriaMetrics 不可用，Prometheus 会按 `queue_config` 缓冲和重试

### 15.5 Jaeger 存储选择

开发环境使用内存存储，重启后 Trace 丢失：
- 适合开发调试，不适合生产
- 生产环境应切换为 Elasticsearch 存储，复用第二阶段的 ES
- Jaeger ES 存储会创建 `jaeger-*` 索引，需调整 ILM 策略

### 15.6 trace_id 与 request_id 的关系

- `request_id`：server-web 生成的请求唯一标识，用于日志关联
- `trace_id`：OTel 生成的分布式链路标识，用于跨服务追踪
- 两者不是同一个东西，但可以同时存在于日志中
- `request_id` 在单服务内使用，`trace_id` 在跨服务场景使用
- 第三阶段不要求将 `request_id` 传播到下游服务

### 15.7 Kafka 与 Redis 的职责边界

| 维度 | Redis | Kafka |
|------|-------|-------|
| 实时推送 | Pub/Sub 广播 | 不适合 |
| 缓存 | 热点数据缓存 | 不适合 |
| 限流 | 滑动窗口计数 | 不适合 |
| 事件持久化 | Stream（有限） | **持久化、回溯消费** |
| 多消费者 | Pub/Sub 无持久化 | **Consumer Group 独立消费** |
| 事件回溯 | 不支持 | **支持 offset 重放** |
| 告警状态 | **活跃告警、去重** | 事件流 |

**结论**：Redis 负责实时推送和状态管理，Kafka 负责事件持久化和异步处理。两者互补，不替代。

### 15.8 Kafka 异步生产降级策略

AsyncProducer 的降级策略：
- **正常情况**：消息通过 channel 异步发送，不阻塞 Webhook
- **channel 满时**：`select + default` 直接丢弃事件，返回错误
- **Kafka 不可用时**：producer 初始化失败时 `kafkaProducer` 为 nil，跳过 Kafka 生产
- **发送失败时**：异步错误通过 `Errors()` channel 消费，记录警告日志
- **不影响主链路**：Redis 写入 + Pub/Sub 广播始终优先执行

---

## 十六、简历表述建议

### 第三阶段新增亮点

```
6. 基于 OpenTelemetry SDK 实现分布式链路追踪，所有服务自动注入/提取
   W3C Trace Context，日志通过 trace_id 与 Trace 关联，
   在 Grafana 中实现指标 + 日志 + Trace 三位一体联动查询。

7. 引入 Kafka 事件总线（KRaft 模式）承载告警事件，
   开发 alert-service 从 Kafka 消费告警事件实现去重与聚合，
   将告警从"Prometheus 单一来源"扩展为"Kafka 事件 + Prometheus 规则"双通道。

8. 基于 Prometheus remote_write 接入 VictoriaMetrics 实现指标长期存储，
   解决 Prometheus 本地 TSDB 保留期有限的问题，
   Grafana 可查询 90 天历史指标数据。
```

### 完整简历亮点（第一 + 第二 + 第三阶段）

```
1. 自研 server-probe Exporter 以 DaemonSet 部署采集每个 K8s Node 宿主机指标，
   基于 Prometheus TSDB 存储监控指标，避免将高频时序指标写入 MySQL，
   降低关系型数据库存储压力。

2. 基于 Prometheus Rules + AlertManager 实现指标告警闭环，
   通过 Webhook 接入 server-web，使用 Redis Pub/Sub 支持多副本 WebSocket 广播，
   并使用 WebSocket 向前端实时推送告警。

3. 封装 Prometheus HTTP API，基于 PromQL 模板实现指标查询白名单，
   避免前端直接透传任意 PromQL；
   使用 Redis 缓存主机最新状态和 Dashboard 聚合数据，
   减少前端高频刷新对后端和 Prometheus 查询接口的压力。

4. 使用 Helm 将后端、前端、Prometheus、Grafana、AlertManager
   部署到 Kubernetes，结合 Ingress 和 HPA 完成云原生化改造。

5. 基于 Fluent Bit DaemonSet 采集 Kubernetes Pod 日志，
   所有服务使用 zap 输出结构化 JSON 日志（零分配高性能），
   通过 Elasticsearch 索引存储，Kibana 可视化查询，
   并在 Grafana 中关联 ES 数据源实现指标 + 日志联动查询，
   在日志索引中预留 trace_id/span_id 字段为链路追踪做准备。

6. 基于 OpenTelemetry SDK 实现分布式链路追踪，所有服务自动注入/提取
   W3C Trace Context，日志通过 trace_id 与 Trace 关联，
   在 Grafana 中实现指标 + 日志 + Trace 三位一体联动查询。

7. 引入 Kafka 事件总线（KRaft 模式）承载告警事件，
   开发 alert-service 从 Kafka 消费告警事件实现去重与聚合，
   将告警从"Prometheus 单一来源"扩展为"Kafka 事件 + Prometheus 规则"双通道。

8. 基于 Prometheus remote_write 接入 VictoriaMetrics 实现指标长期存储，
   解决 Prometheus 本地 TSDB 保留期有限的问题，
   Grafana 可查询 90 天历史指标数据。
```
