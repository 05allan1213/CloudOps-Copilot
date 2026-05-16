# Server Monitor 项目架构重构与代码整改方案

> 版本：v2.5
> 日期：2026-05-16
> 范围：server-monitor 全项目（server-web、alert-service、server-probe、pkg）
> v2.0 变更：合并目录结构重构方案（参考 honey_server internal 分层架构），新增 §15-§19
> v2.1 变更：修复 §15-§19 中 P0 编译冲突——目录聚合 ≠ package 合并，service/infra 改为子包模式，Kafka/Redis 与 §3 对齐进 pkg，copilot 保留原子包、eval 不合并
> v2.2 变更：修正真实 package 名（rediscache/promclient）、补充 diagnosisAccessAdapter 导出构造函数、修正文件数量、补齐 webhook 现状
> v2.3 变更：修正 diagnosisAccessAdapter 示例代码（返回具体类型避免 import cycle）、copilot 子包数 7→11
> v2.4 变更：新增 §20 构建部署与 CI 镜像影响分析
> v2.5 变更：补齐 Copilot AI 升级后的 `context/summary/suggestion` 子包、结构化建议与 SSE 响应影响

---

## 目录

1. [现状分析](#1-现状分析)
2. [目标架构](#2-目标架构)
3. [公共代码提取清单](#3-公共代码提取清单)
4. [依赖注入实现方式](#4-依赖注入实现方式)
5. [main 文件优化策略](#5-main-文件优化策略)
6. [优雅关闭实现方案](#6-优雅关闭实现方案)
7. [依赖启动流程与顺序](#7-依赖启动流程与顺序)
8. [文件结构变更说明](#8-文件结构变更说明)
9. [代码注释整改规范](#9-代码注释整改规范)
10. [配置文件注释整改](#10-配置文件注释整改)
11. [代码风格统一规范](#11-代码风格统一规范)
12. [冗余代码清理清单](#12-冗余代码清理清单)
13. [实施步骤与风险控制](#13-实施步骤与风险控制)
14. [预期收益](#14-预期收益)
15. [目录结构重构 — 参考项目分析](#15-目录结构重构--参考项目分析)
16. [目录结构重构 — 目标结构](#16-目录结构重构--目标结构)
17. [目录结构重构 — 文件移动映射](#17-目录结构重构--文件移动映射)
18. [目录结构重构 — 与参考项目差异](#18-目录结构重构--与参考项目差异)
19. [目录结构重构 — 实施步骤与风险](#19-目录结构重构--实施步骤与风险)
20. [构建部署与 CI 镜像影响](#20-构建部署与-ci-镜像影响)

---

## 1. 现状分析

### 1.1 当前项目结构

```
server-monitor/
├── pkg/                          # 共享公共包（独立 Go module: server-monitor/pkg）
│   ├── go.mod                    # module server-monitor/pkg
│   ├── logger/logger.go          # 日志初始化（zap + slog 桥接）
│   ├── shutdown/shutdown.go      # 分阶段优雅关闭
│   ├── tracer/tracer.go          # OpenTelemetry 链路追踪
│   ├── httpmiddleware/           # 标准 net/http 中间件
│   └── configutil/env.go        # 环境变量类型转换工具
│
├── server-web/                   # Web 服务（主服务，最大最复杂）
│   ├── go.mod                    # module server-web; replace server-monitor/pkg => ../pkg
│   ├── main.go                   # 454 行，初始化+运行+关闭
│   ├── config/config.go          # 配置加载与校验（798 行，含详细注释）
│   ├── api/
│   │   ├── router.go             # 507 行巨型路由文件（路由注册+服务组装）
│   │   ├── handlers/             # 业务 Handler（7 个文件）
│   │   └── middleware/           # Gin 中间件（7 个文件）
│   ├── alert/                    # 告警服务（Webhook + Redis + Kafka）
│   ├── auth/                     # 认证服务（JWT + bcrypt）
│   ├── cache/                    # 缓存服务（主机列表 + 仪表盘概览）
│   ├── copilot/                  # AI Copilot 模块（14 个子包，约 76 个非测试 Go 文件）
│   ├── database/                 # MySQL 客户端 + GORM 迁移
│   ├── host/                     # 主机服务（Prometheus 查询 + 缓存）
│   ├── kafka/                    # Kafka 生产者与消费者
│   ├── model/                    # GORM 数据模型（10 个文件）
│   ├── prometheus/               # Prometheus 客户端 + PromQL 查询模板
│   ├── pubsub/                   # 发布订阅（AlertHub + Redis Subscriber）
│   ├── redis/                    # Redis 客户端 + 缓存常量
│   ├── webhook/                  # Alertmanager Webhook 类型
│   └── websocket/                # WebSocket Hub
│
├── alert-service/                # 告警处理服务
│   ├── go.mod                    # module alert-service; replace server-monitor/pkg => ../pkg
│   ├── main.go                   # 291 行
│   ├── config/config.go          # 配置（已有字段注释）
│   ├── alert/                    # 告警去重与状态管理
│   ├── health/                   # 健康检查
│   ├── kafka/                    # Kafka 消费者
│   ├── metrics/                  # Prometheus 指标
│   └── redis/                    # Redis 客户端（含 Lua 脚本）
│
├── server-probe/                 # 主机探针
│   ├── go.mod                    # module server-probe; replace server-monitor/pkg => ../pkg
│   ├── main.go                   # 270 行
│   ├── config/config.go          # 配置（已有字段注释）
│   └── collector/                # 指标采集器（6 种）
│
├── charts/                       # Helm Charts
├── docker/                       # Docker 配置
├── frontend/                     # Vue 前端
└── Makefile
```

### 1.2 Go Module 依赖关系

当前项目使用 **4 个独立 Go module**，通过 `replace` 指令引用共享包：

```
server-monitor/pkg          (module server-monitor/pkg)
    ↑ replace ../pkg
server-web                  (module server-web)
    ↑ replace ../pkg
alert-service               (module alert-service)
    ↑ replace ../pkg
server-probe                (module server-probe)
```

**关键约束：** 新增 `pkg/kafka`、`pkg/redis` 子包时，只需在 `server-monitor/pkg/go.mod` 中添加 sarama/go-redis 依赖，各服务通过 `replace server-monitor/pkg => ../pkg` 自动获得。不需要为各服务单独添加新的 `require` 行（除非服务直接依赖新库而非通过 pkg 间接引用）。

### 1.3 核心问题诊断

| 编号 | 问题类别 | 具体描述 | 影响范围 | 严重度 |
|------|---------|---------|---------|--------|
| P1 | **代码重复** | Kafka Consumer 在 server-web 和 alert-service 中几乎完全重复（含 retry backoff、skipped error 处理等） | 2 个服务 | 高 |
| P2 | **代码重复** | AlertEvent 类型在两个服务中重复定义（server-web 在 `producer.go` 中，alert-service 在 `event.go` 中） | 2 个服务 | 高 |
| P3 | **代码重复** | Kafka Topics 常量在两个服务中重复 | 2 个服务 | 高 |
| P4 | **代码重复** | Redis Client 基础结构（NewClient、Enabled、Close、Ping、Options）在两个服务中重复 | 2 个服务 | 中 |
| P5 | **代码重复** | `validateHostPort` 函数在三个服务的 config 中重复 | 3 个服务 | 中 |
| P6 | **巨型文件** | `api/router.go` 约 507 行，同时承担路由注册和服务组装两个职责 | server-web | 高 |
| P7 | **巨型 main** | `server-web/main.go` 454 行，initApp 是单体初始化函数 | server-web | 中 |
| P8 | **依赖注入缺失** | 大量具体类型直接传递，无接口抽象，难以测试和替换 | 全局 | 高 |
| P9 | **关闭阻塞风险** | `subscriberDone`、`diagnosisDone`、`alertHubConsumers` 三个 channel 等待无超时保护，可能无限阻塞 | server-web | 高 |

### 1.4 重复代码统计

| 重复项 | server-web 位置 | alert-service 位置 | 重复行数（约） |
|--------|----------------|-------------------|--------------|
| Kafka Consumer | `kafka/consumer.go` | `kafka/consumer.go` | ~200 行 |
| Kafka Topics | `kafka/topics.go` | `kafka/topics.go` | ~10 行 |
| Kafka AlertEvent | `kafka/producer.go`（第 14-24 行） | `kafka/event.go` | ~15 行 |
| Redis Client 基础 | `redis/client.go` | `redis/client.go` | ~80 行 |
| validateHostPort | `config/config.go` | `config/config.go` | ~15 行 |
| **合计** | | | **~320 行** |

**注意：** `server-web/kafka/` 目录下**不存在** `event.go` 文件。`AlertEvent` 类型定义在 `server-web/kafka/producer.go` 的第 14-24 行。而 `alert-service/kafka/` 下有独立的 `event.go` 文件。

### 1.5 当前依赖关系图

```
                    ┌─────────────────────┐
                    │    server-web       │
                    │     main.go         │
                    └─────────┬───────────┘
                              │ 直接创建所有依赖
          ┌───────────────────┼──────────────────┐
          │                   │                  │
   ┌──────▼──────┐    ┌──────▼──────┐    ┌──────▼──────┐
   │  api/       │    │  kafka/     │    │  redis/     │
   │  router.go  │    │  consumer   │    │  client     │
   │  (巨型文件) │    │  producer   │    │  cache      │
   └──────┬──────┘    └──────┬──────┘    └──────┬──────┘
          │                  │                  │
   ┌──────▼──────┐          │           ┌──────▼──────┐
   │ copilot/*   │          │           │  pubsub/    │
   │ (11 个子包) │          │           │  subscriber │
   └─────────────┘          │           └─────────────┘
                            │
   ┌────────────────────────┼───────────────────────┐
   │                        │                       │
┌──▼───────────┐   ┌───────▼────────┐   ┌──────────▼──────┐
│alert-service │   │  server-probe  │   │     pkg/         │
│  kafka/      │   │  (独立模块)    │   │  (共享包)        │
│  redis/      │   │                │   │  logger          │
│  (重复代码)  │   │                │   │  shutdown        │
└──────────────┘   └────────────────┘   │  tracer          │
                                       │  httpmiddleware   │
                                       │  configutil       │
                                       └──────────────────┘
```

---

## 2. 目标架构

### 2.1 架构设计原则

1. **模块化**：每个功能模块有清晰的边界和接口
2. **依赖注入**：通过构造函数注入，接口定义在使用方
3. **公共代码提取**：跨服务共享代码统一到 `pkg/`
4. **精简 main**：main.go 仅负责启动流程编排
5. **显式依赖**：依赖启动顺序通过 App 结构体显式声明
6. **优雅关闭**：统一关闭框架，支持分阶段、有序、超时保护地释放资源
7. **少量必要注释**：包注释 + 导出符号注释 + 关键逻辑注释，Go 风格命名自解释

### 2.2 目标项目结构

> **注意：** 以下结构是 §3-§8 公共代码提取 + main/router 拆分后的中间态。目录结构重构（`internal/` 迁移）在 §16 中描述，是后续独立步骤。

```
server-monitor/
├── pkg/                              # 共享公共包（module server-monitor/pkg）
│   ├── go.mod                        # 需新增 sarama、go-redis 依赖
│   ├── logger/                       # 日志（不变）
│   ├── shutdown/                     # 优雅关闭（不变，已支持 Phase.Timeout）
│   ├── tracer/                       # 链路追踪（不变）
│   ├── httpmiddleware/               # HTTP 中间件（不变）
│   ├── configutil/                   # 配置工具（增强）
│   │   ├── env.go                    # 环境变量工具
│   │   └── validate.go               # 新增：通用校验函数
│   ├── kafka/                        # 新增：Kafka 公共包
│   │   ├── consumer.go               # 统一消费者
│   │   ├── producer.go               # 统一生产者（含 AlertEvent 定义）
│   │   └── topics.go                 # 统一 Topic 常量
│   └── redis/                        # 新增：Redis 公共包
│       ├── client.go                 # 基础客户端
│       └── options.go                # 配置选项
│
├── server-web/                       # Web 服务
│   ├── main.go                       # 精简后 < 80 行
│   ├── app.go                        # 新增：应用组装（依赖注入）
│   ├── config/config.go              # 配置（校验函数改用 pkg/configutil）
│   ├── api/
│   │   ├── router.go                 # 精简后：仅路由注册
│   │   ├── handlers/
│   │   └── middleware/
│   ├── alert/
│   ├── auth/
│   ├── cache/
│   ├── copilot/
│   ├── database/
│   ├── host/
│   ├── model/
│   ├── prometheus/
│   ├── pubsub/
│   ├── redis/                    # Redis 业务封装（基于 pkg/redis.Client）+ 缓存常量
│   └── websocket/
│
├── alert-service/                    # 告警处理服务
│   ├── main.go                       # 精简后 < 60 行
│   ├── app.go                        # 新增：应用组装
│   ├── config/config.go
│   ├── alert/
│   ├── health/
│   └── metrics/
│
├── server-probe/                     # 主机探针
│   ├── main.go                       # 精简后 < 60 行
│   ├── app.go                        # 新增：应用组装
│   ├── config/config.go
│   └── collector/
│
├── charts/
├── docker/
├── frontend/
└── Makefile
```

---

## 3. 公共代码提取清单

### 3.1 新增 `pkg/kafka/` — 统一 Kafka 消息层

**提取来源：**
- `server-web/kafka/consumer.go` → `pkg/kafka/consumer.go`
- `server-web/kafka/producer.go`（含 AlertEvent 定义）→ `pkg/kafka/producer.go`
- `server-web/kafka/topics.go` → `pkg/kafka/topics.go`
- `alert-service/kafka/event.go`（AlertEvent）→ 合并到 `pkg/kafka/producer.go`
- `alert-service/kafka/consumer.go` → 合并到 `pkg/kafka/consumer.go`
- `alert-service/kafka/topics.go` → 合并到 `pkg/kafka/topics.go`

**注意：** `server-web/kafka/` 下不存在 `event.go` 文件。`AlertEvent` 定义在 `producer.go` 中。统一后 `AlertEvent` 仍放在 `pkg/kafka/producer.go` 中（与 Producer 紧密关联），或提取到独立的 `pkg/kafka/types.go`。

**合并策略：**

以 `server-web/kafka/consumer.go` 为基础（功能更完整，含 retry backoff、skipped error、retryable errors），合并 `alert-service/kafka/consumer.go` 的差异点。

**行为差异需测试锁定：**

| 特性 | server-web Consumer | alert-service Consumer | 统一后处理 |
|------|--------------------|-----------------------|-----------|
| 消费错误策略 | 重试（带 backoff 循环） | fail-fast（出错即返回） | 显式配置，见下方 |
| Skipped error 日志 | ✅ 有 | ❌ 无 | 保留 |
| SetRetryableErrors | ✅ 有 | ❌ 无 | 保留，默认 false |
| SetObserver | ✅ 有 | ✅ 有 | 保留 |
| 就绪回调 (onReady/onNotReady) | ✅ 有 | ✅ 有 | 保留 |

**统一后的消费错误策略设计：**

当前两个 Consumer 的核心行为差异在于消费循环的错误处理：
- `server-web`：`Consume` 出错后带 backoff 重试，直到 context 取消
- `alert-service`：`Consume` 出错后直接返回错误（fail-fast）

统一 Consumer 通过显式配置锁住两条路径：

```go
// ConsumerConfig 消费者配置。
type ConsumerConfig struct {
    Brokers      []string
    GroupID      string
    Topics       []string
    RetryBackoff func(int) time.Duration  // 非 nil 时启用重试循环；nil 时 fail-fast
    StopOnError  bool                     // true 时 Consume 出错即返回；与 RetryBackoff 互斥
}
```

| 使用方 | 配置 | 行为 |
|--------|------|------|
| server-web（Diagnosis Worker） | `RetryBackoff: defaultConsumeRetryBackoff` | 重试循环，与当前行为一致 |
| alert-service | `StopOnError: true` | fail-fast，与当前行为一致 |

> **实现约束：** `RetryBackoff` 和 `StopOnError` 互斥。`NewConsumer` 中应校验二者不得同时为 true，否则返回错误。

**依赖影响：** 将 sarama 引入 `server-monitor/pkg/go.mod`，扩大共享模块依赖图。这是有意取舍——sarama 已被 server-web 和 alert-service 同时依赖，提取到 pkg 只是集中管理，不增加总体依赖数量。

**统一后的核心类型：**

```go
// Package kafka 提供统一的 Kafka 消息生产与消费功能。
package kafka

// AlertEvent 告警事件，由 Alertmanager Webhook 生成，
// 通过 Kafka 在 server-web 和 alert-service 之间传递。
type AlertEvent struct {
    Type         string            `json:"type"`
    Fingerprint  string            `json:"fingerprint"`
    Status       string            `json:"status"`
    Labels       map[string]string `json:"labels"`
    Annotations  map[string]string `json:"annotations"`
    StartsAt     time.Time         `json:"startsAt"`
    EndsAt       time.Time         `json:"endsAt"`
    GeneratorURL string            `json:"generatorURL,omitempty"`
    ReceivedAt   time.Time         `json:"receivedAt"`
}

const (
    TopicAlertEvents     = "alert-events"
    TopicOperationEvents = "operation-events"
)
```

**迁移后各服务变更：**

| 服务 | 变更 |
|------|------|
| `server-web` | 删除 `kafka/` 目录（含 `consumer.go`、`producer.go`、`topics.go`、`consumer_test.go`），import 改为 `server-monitor/pkg/kafka` |
| `alert-service` | 删除 `kafka/` 目录（含 `consumer.go`、`event.go`、`topics.go`），import 改为 `server-monitor/pkg/kafka` |

### 3.2 新增 `pkg/redis/` — 统一 Redis 客户端

**提取来源：**
- `server-web/redis/client.go` → `pkg/redis/client.go`（基础客户端 + 通用方法）
- `alert-service/redis/client.go` → 合并基础方法到 `pkg/redis/client.go`

**合并策略：**

两个 Redis Client 的基础结构（NewClient、Enabled、Close、Ping、Options）完全相同。`server-web/redis/client.go` 包含更多通用方法（Get、Set、HSet、Publish、Subscribe、限流等），`alert-service/redis/client.go` 包含业务特定的 Lua 脚本方法。

统一方案：
- `pkg/redis/client.go`：基础客户端 + 通用方法
- `alert-service/redis/client.go`：保留 `package redisstore`（当前实际包名），内部组合 `pkgredis.Client`，保留业务 Lua 脚本方法

**依赖影响：** 将 go-redis 引入 `server-monitor/pkg/go.mod`。同 sarama，这是集中管理而非新增。

**alert-service 适配方案：**

```go
// alert-service/redis/client.go
// 基于 pkg/redis.Client 的业务封装，保留 Lua 脚本方法。
// 保留 package redisstore（当前实际包名），避免无收益改包名。
package redisstore

import pkgredis "server-monitor/pkg/redis"

type Client struct {
    base *pkgredis.Client
}

func NewClient(options pkgredis.Options) *Client {
    return &Client{base: pkgredis.NewClient(options)}
}

func (c *Client) Enabled() bool  { return c.base.Enabled() }
func (c *Client) Close() error   { return c.base.Close() }
func (c *Client) Ping(ctx context.Context) error { return c.base.Ping(ctx) }

// 业务特定的 Lua 脚本方法保留在此
func (c *Client) ApplyFiringEvent(...) (bool, error) { ... }
func (c *Client) ApplyResolvedEvent(...) (bool, error) { ... }
```

### 3.3 新增 `pkg/configutil/validate.go` — 统一校验函数

**提取来源：**
- `server-web/config/config.go` 中的 `validateHostPort`、`validatePort`、`validateHTTPURL`、`validateListenAddr`
- `alert-service/config/config.go` 中的 `validateHostPort`
- `server-probe/config/config.go` 中的 `validateHostPort`

**统一后的校验函数：**

```go
// Package configutil 提供环境变量读取、类型转换和配置校验工具函数。
package configutil

// ValidateHostPort 校验 host:port 格式的地址。
func ValidateHostPort(name, raw string) error { ... }

// ValidatePort 校验端口号，确保为 1-65535 范围内的整数。
func ValidatePort(name, raw string) error { ... }

// ValidateHTTPURL 校验 HTTP/HTTPS URL 格式。
func ValidateHTTPURL(name, raw string) error { ... }

// ValidateListenAddr 校验监听地址，格式同 host:port。
func ValidateListenAddr(name, raw string) error { ... }
```

**各服务 config 变更：** 删除本地 `validateHostPort` 等函数，改用 `configutil.ValidateHostPort`。

---

## 4. 依赖注入实现方式

### 4.1 设计原则

1. **构造函数注入**：所有依赖通过构造函数参数传入
2. **接口定义在使用方**：消费者包定义接口，提供者包实现
3. **无 DI 框架**：不引入 wire、dig 等框架，手动组装保持透明
4. **App 组装器模式**：每个服务一个 `app.go` 文件负责依赖组装

### 4.2 server-web 依赖注入重构

**现状问题：** `api/router.go` 的 `NewRouterWithRuntime` 函数约 340 行，同时承担路由注册和服务组装两个职责。

**重构方案：** 将服务组装从 router.go 中拆分到 `app.go`，router.go 仅负责路由注册。

#### app.go — 依赖组装

```go
// app.go 负责 server-web 的依赖组装，实现手动依赖注入。
// 依赖分为三层：
//   - 基础设施层：Tracer、Prometheus、Redis、MySQL、Kafka、WebSocket、AlertHub
//   - 业务服务层：Auth、Alert、Copilot（LLM、NLU、Tool、Diagnosis、Action、Feedback、K8s）
//   - 消费者层：Diagnosis Kafka Consumer

// infrastructure 基础设施层依赖，无业务逻辑依赖。
type infrastructure struct {
    shutdownTracer   func(context.Context) error
    prometheusClient *promclient.Client
    redisClient      *rediscache.Client
    mysqlClient      *database.MySQL
    kafkaProducer    *eventbus.Producer
    websocketHub     *ws.Hub
    alertHub         *pubsub.Hub
}

// initInfrastructure 初始化基础设施层。
func initInfrastructure(ctx context.Context, cfg config.Config) (*infrastructure, error) { ... }

// services 业务服务层依赖，依赖基础设施层。
type services struct {
    authService  *authpkg.Service
    alertService *alert.Service
    copilotDeps  *api.CopilotDeps
}

// initServices 初始化业务服务层。
func initServices(cfg config.Config, infra *infrastructure) (*services, error) { ... }
```

#### api/router.go — 仅路由注册

```go
// Dependencies 路由层所需的全部依赖，由 app.go 组装后传入。
type Dependencies struct {
    PrometheusClient *promclient.Client
    RedisClient      *rediscache.Client
    MySQLClient      *database.MySQL
    AuthService      AuthService
    WebSocketHub     *ws.Hub
    KafkaProducer    *kafka.Producer
    AlertService     *alert.Service
    CopilotDeps      *CopilotDeps
}

// NewRouter 创建 Gin 路由引擎，注册所有 API 路由和中间件。
func NewRouter(cfg config.Config, deps Dependencies) (*gin.Engine, error) { ... }
```

### 4.3 alert-service / server-probe 依赖注入

参照 server-web 模式，各新增 `app.go` 封装 `initApp` 函数，main.go 仅调用 `initApp` → `runApp` → `shutdownApp`。

---

## 5. main 文件优化策略

### 5.1 优化目标

| 服务 | 当前行数 | 目标行数 | 策略 |
|------|---------|---------|------|
| server-web/main.go | 454 | < 80 | 拆分到 app.go + 各 init 函数 |
| alert-service/main.go | 291 | < 60 | 拆分到 app.go |
| server-probe/main.go | 270 | < 60 | 拆分到 app.go |

### 5.2 精简后的 main.go 统一模式

三个服务的 main.go 统一为以下模式：

```go
func main() {
    log, err := logger.Init("<service-name>")
    if err != nil { ... }
    defer logger.Sync(log)

    app, err := initApp(context.Background())
    if err != nil { ... }

    exitCode := runApp(app)
    shutdownApp(app)
    if exitCode != 0 { os.Exit(exitCode) }
}
```

`initApp`、`runApp`、`shutdownApp` 定义在各自的 `app.go` 中。

---

## 6. 优雅关闭实现方案

### 6.1 设计目标

1. **分阶段关闭**：按依赖关系逆序关闭
2. **超时控制**：每个阶段有独立超时（`pkg/shutdown` 已支持 `Phase.Timeout`）
3. **Channel 等待超时保护**：所有 `<-chan struct{}` 等待必须有超时兜底，防止无限阻塞
4. **资源释放**：确保连接关闭、缓冲区刷新

### 6.2 当前问题：Channel 等待无超时

`server-web/main.go` 中的关闭流程存在三处无超时阻塞等待：

```go
// ❌ 当前代码：无超时保护
if app.subscriberDone != nil {
    <-app.subscriberDone          // 可能无限阻塞
}
if app.diagnosisDone != nil {
    <-app.diagnosisDone           // 可能无限阻塞
}
// ...
if app.alertHubConsumers != nil {
    <-app.alertHubConsumers       // 可能无限阻塞
}
```

### 6.3 修复方案：为所有 Channel 等待添加超时

```go
// ✅ 修复后：带超时保护的等待
func waitWithTimeout(ch <-chan struct{}, timeout time.Duration, name string) {
    if ch == nil {
        return
    }
    select {
    case <-ch:
        zap.L().Info("shutdown wait completed", zap.String("name", name))
    case <-time.After(timeout):
        zap.L().Warn("shutdown wait timed out, proceeding", zap.String("name", name), zap.Duration("timeout", timeout))
    }
}
```

> **注意：** `subscriberDone`、`diagnosisDone` 在对应组件未启用时为 nil。nil channel 的 select case 永远不会就绪，所以必须先检查 nil，否则会白等一个 `ShutdownTimeout`。

### 6.4 server-web 关闭流程（修复后）

```
阶段 1: 停止流量入口（使用 shutdown.Graceful）
  ├── 关闭 HTTP Server
  └── 关闭 Tracer

阶段 2: 停止消费者
  ├── 取消 app context
  ├── 关闭 Diagnosis Kafka Consumer
  ├── 等待 subscriberDone（超时保护）
  └── 等待 diagnosisDone（超时保护）

阶段 3: 释放资源（使用 shutdown.Graceful）
  ├── 关闭 Redis
  ├── 关闭 MySQL
  └── 关闭 Kafka Producer

阶段 4: 清理
  ├── 关闭 AlertHub
  └── 等待 alertHubConsumers（超时保护）
```

```go
func shutdownApp(app *app) {
    zap.L().Info("server-web shutting down")

    // 阶段 1
    shutdown.Graceful(app.cfg.ShutdownTimeout, []shutdown.Phase{
        {Name: "http-server", Fn: func(ctx context.Context) error { return app.server.Shutdown(ctx) }},
        {Name: "tracer", Fn: app.shutdownTracer},
    })

    // 阶段 2
    app.cancel()
    if app.diagnosisConsumer != nil {
        if err := app.diagnosisConsumer.Close(); err != nil {
            zap.L().Warn("diagnosis kafka consumer close failed", zap.Error(err))
        }
    }
    waitWithTimeout(app.subscriberDone, app.cfg.ShutdownTimeout, "subscriber")
    waitWithTimeout(app.diagnosisDone, app.cfg.ShutdownTimeout, "diagnosis-consumer")

    // 阶段 3
    shutdown.Graceful(app.cfg.ShutdownTimeout, []shutdown.Phase{
        {Name: "redis", Fn: func(ctx context.Context) error { return app.redisClient.Close() }},
        {Name: "mysql", Fn: func(ctx context.Context) error {
            if app.mysqlClient != nil { return app.mysqlClient.Close() }
            return nil
        }},
        {Name: "kafka-producer", Fn: func(ctx context.Context) error {
            if app.kafkaProducer != nil { return app.kafkaProducer.Close() }
            return nil
        }},
    })

    // 阶段 4
    app.alertHub.Close()
    waitWithTimeout(app.alertHubConsumers, app.cfg.ShutdownTimeout, "alert-hub-consumers")

    zap.L().Info("server-web stopped")
}
```

### 6.5 alert-service / server-probe 关闭流程

alert-service 和 server-probe 的关闭流程相对简单，同样需要检查 channel 等待是否有超时保护。

---

## 7. 依赖启动流程与顺序

### 7.1 server-web 启动流程

```
1. 初始化 Logger
2. 加载与校验配置
3. 初始化基础设施（无业务依赖）
   ├── initTracer()
   ├── prometheus.NewClient()
   ├── redis.NewClient()
   ├── initMySQL()
   ├── initKafkaProducer()
   ├── websocket.NewHub()
   └── pubsub.NewHub()
4. 初始化业务服务（依赖基础设施）
   ├── initAuthService()       → 依赖 MySQL
   ├── alert.NewService()      → 依赖 Redis + Kafka Producer
   └── initCopilotDeps()       → 依赖上述全部
5. 创建 HTTP Router（依赖业务服务）
6. 初始化 Diagnosis Consumer（依赖业务服务 + Redis）
7. 启动后台任务
8. 启动 HTTP Server
```

### 7.2 alert-service 启动流程

```
1. 初始化 Logger
2. 加载与校验配置
3. 初始化基础设施（Tracer → Redis）
4. 初始化业务服务（Metrics → AlertStore → Kafka Consumer）
5. 启动 Kafka Consumer
6. 启动 HTTP Server
```

### 7.3 server-probe 启动流程

```
1. 初始化 Logger
2. 加载与校验配置
3. 初始化基础设施（Tracer → 主机路径）
4. 创建 Collectors + 注册 Prometheus 指标
5. 启动采集循环
6. 启动 HTTP Server
```

---

## 8. 文件结构变更说明

### 8.1 新增文件

| 文件路径 | 说明 |
|---------|------|
| `pkg/configutil/validate.go` | 统一校验函数 |
| `pkg/kafka/consumer.go` | 统一 Kafka Consumer |
| `pkg/kafka/producer.go` | 统一 Kafka Producer（含 AlertEvent） |
| `pkg/kafka/topics.go` | 统一 Topic 常量 |
| `pkg/redis/client.go` | 统一 Redis 基础客户端 |
| `pkg/redis/options.go` | Redis 配置选项 |
| `server-web/app.go` | server-web 依赖组装 |
| `alert-service/app.go` | alert-service 依赖组装 |
| `server-probe/app.go` | server-probe 依赖组装 |

### 8.2 删除文件

| 文件路径 | 原因 |
|---------|------|
| `server-web/kafka/consumer.go` | 迁移至 `pkg/kafka/consumer.go` |
| `server-web/kafka/producer.go` | 迁移至 `pkg/kafka/producer.go` |
| `server-web/kafka/topics.go` | 迁移至 `pkg/kafka/topics.go` |
| `server-web/kafka/consumer_test.go` | 迁移至 `pkg/kafka/consumer_test.go` |
| `alert-service/kafka/consumer.go` | 迁移至 `pkg/kafka/`（合并） |
| `alert-service/kafka/event.go` | 合并到 `pkg/kafka/producer.go` |
| `alert-service/kafka/topics.go` | 合并到 `pkg/kafka/topics.go` |

**注意：** `server-web/kafka/` 下不存在 `event.go`。`AlertEvent` 定义在 `producer.go` 中。

### 8.3 修改文件

| 文件路径 | 变更内容 |
|---------|---------|
| `pkg/go.mod` | 新增 sarama、go-redis 依赖（Kafka/Redis 公共包所需） |
| `server-web/main.go` | 精简至 < 80 行，初始化逻辑移至 app.go |
| `server-web/config/config.go` | 删除本地 validate 函数，改用 pkg/configutil |
| `server-web/api/router.go` | 拆分服务组装逻辑，仅保留路由注册 |
| `server-web/redis/client.go` | 改为基于 `pkg/redis.Client` 的业务封装 |
| `alert-service/main.go` | 精简至 < 60 行，初始化逻辑移至 app.go |
| `alert-service/config/config.go` | 删除本地 validateHostPort，改用 pkg/configutil |
| `alert-service/redis/client.go` | 改为基于 `pkg/redis.Client` 的业务封装 |
| `server-probe/main.go` | 精简至 < 60 行，初始化逻辑移至 app.go |
| `server-probe/config/config.go` | 删除本地 validateHostPort，改用 pkg/configutil |
| `Makefile` | 新增 `test-pkg` 目标，验证 pkg 模块 |

### 8.4 保留不变文件

| 文件路径 | 原因 |
|---------|------|
| `pkg/shutdown/shutdown.go` | 已支持 `Phase.Timeout`，无需增强 |
| `pkg/logger/logger.go` | 已是公共包，无需变更 |
| `pkg/tracer/tracer.go` | 已是公共包，无需变更 |
| `pkg/httpmiddleware/httpmiddleware.go` | 已是公共包，无需变更 |
| `pkg/configutil/env.go` | 已是公共包，无需变更 |
| `server-web/redis/cache.go` | 业务常量，保留 |
| `server-web/websocket/hub.go` | 业务逻辑，保留 |
| `server-web/pubsub/*` | 业务逻辑，保留 |
| `server-web/prometheus/*` | 业务逻辑，保留 |
| `server-web/copilot/*` | 业务逻辑，保留 |
| `server-web/auth/*` | 业务逻辑，保留 |
| `server-web/model/*` | 数据模型，保留 |
| `server-web/database/*` | 数据库逻辑，保留 |
| `server-web/host/*` | 业务逻辑，保留 |
| `server-web/cache/*` | 业务逻辑，保留 |
| `server-web/alert/*` | 业务逻辑，保留 |
| `alert-service/alert/*` | 业务逻辑，保留 |
| `alert-service/health/*` | 业务逻辑，保留 |
| `alert-service/metrics/*` | 业务逻辑，保留 |
| `server-probe/collector/*` | 业务逻辑，保留 |

---

## 9. 代码注释整改规范

### 9.1 注释原则

**核心原则：少量必要注释 + Go 风格命名自解释。**

- 不搞大规模注释运动，不追求"所有文件都有包注释"
- 注释只加在**真正需要**的地方：包的公共 API、非显而易见的逻辑、容易误解的设计决策
- 命名本身能说明问题的，不加注释
- 注释使用中文，技术术语保留英文

### 9.2 需要注释的场景

| 场景 | 示例 | 注释内容 |
|------|------|---------|
| 包的公共 API 用法不直观 | `pkg/kafka` 的 Consumer 需要说明生命周期 | 包注释 |
| 函数行为不符合直觉 | `AllowSlidingWindow` 的返回值含义 | 函数注释 |
| 关键设计决策 | 为什么 alert-service 的 Redis 用 Lua 脚本 | 行内注释 |
| 复杂算法 | NLU 的意图分类规则 | 逻辑块注释 |
| 魔数或特殊常量 | `0.6` 置信度阈值 | 行内注释 |

### 9.3 不需要注释的场景

| 场景 | 示例 | 原因 |
|------|------|------|
| 命名已自解释 | `func (c *Client) Close() error` | Close 含义明确 |
| 标准库封装 | `func HashPassword(password string) (string, error)` | bcrypt 哈希，命名清晰 |
| 简单 CRUD | `func (r *Repository) List(ctx context.Context) ([]Model, error)` | 行为明确 |
| 结构体字段名已说明 | `ListenAddr string` | 字段名即含义 |

### 9.4 本次整改需补充的注释

仅补充以下**真正缺失且有价值**的注释：

| 文件 | 补充内容 | 原因 |
|------|---------|------|
| `pkg/kafka/consumer.go`（新建） | 包注释 + Consumer 生命周期说明 | 公共 API，用法需说明 |
| `pkg/kafka/producer.go`（新建） | 包注释 + AlertEvent 字段注释 | 跨服务共享类型 |
| `pkg/redis/client.go`（新建） | 包注释 + Inner() 方法说明 | 暴露底层客户端是特殊设计 |
| `pkg/configutil/validate.go`（新建） | 包注释 | 公共工具 |
| `server-web/app.go`（新建） | 依赖分层说明 | 启动顺序是关键知识 |
| `alert-service/app.go`（新建） | 依赖分层说明 | 同上 |
| `server-probe/app.go`（新建） | 依赖分层说明 | 同上 |
| `server-web/main.go` | 补充关闭流程注释 | 关闭顺序是关键知识 |

**不补充注释的文件：** 已有注释的 config.go、命名清晰的 model/*.go、简单的 handler 函数等。

---

## 10. 配置文件注释整改

### 10.1 当前状态

| 服务 | 配置注释状态 | 需要整改 |
|------|------------|---------|
| server-web/config/config.go | ✅ 已有详细中文注释 | 无需 |
| alert-service/config/config.go | ✅ 已有字段注释 | 无需 |
| server-probe/config/config.go | ✅ 已有字段注释 | 无需 |

### 10.2 环境变量参考

以下列出三个服务的主要环境变量。完整配置项请参阅各服务的 `config/config.go` 源码。

#### alert-service 环境变量

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `LISTEN_ADDR` | string | `:8081` | HTTP 监听地址 |
| `KAFKA_BROKERS` | list | `kafka:9092` | Kafka Broker 列表 |
| `KAFKA_GROUP_ID` | string | `alert-service` | Consumer Group ID |
| `REDIS_ADDR` | string | `redis:6379` | Redis 地址 |
| `REDIS_PASSWORD` | string | 空 | Redis 密码（敏感） |
| `TRACE_OTLP_ENDPOINT` | string | `jaeger:4317` | OTLP 端点 |
| `TRACE_SAMPLE_RATE` | float | `1.0` | 追踪采样率 |
| `SHUTDOWN_TIMEOUT_SECONDS` | int | `10` | 关闭超时 |

#### server-probe 环境变量

| 环境变量 | 类型 | 默认值 | 说明 |
|---------|------|--------|------|
| `LISTEN_ADDR` | string | `:9090` | HTTP 监听地址 |
| `METRICS_PATH` | string | `/metrics` | 指标端点路径 |
| `SCRAPE_INTERVAL` | int | `5` | 采集间隔（秒） |
| `PROMHTTP_MAX_REQUESTS_IN_FLIGHT` | int | `5` | Prometheus Handler 最大并发 |
| `PROMHTTP_TIMEOUT` | int | `5` | Prometheus Handler 超时（秒） |
| `HOSTNAME` | string | 自动获取 | 主机标识名 |
| `HOST_PROC` | string | 空 | 宿主机 /proc 路径 |
| `HOST_SYS` | string | 空 | 宿主机 /sys 路径 |
| `TRACE_OTLP_ENDPOINT` | string | 空 | OTLP 端点 |
| `SHUTDOWN_TIMEOUT` | int | `5` | 关闭超时（秒） |

---

## 11. 代码风格统一规范

### 11.1 命名规范

| 类别 | 规范 | 示例 |
|------|------|------|
| 包名 | 小写单词，不使用下划线 | `kafka`, `configutil` |
| 导出类型 | 大驼峰 | `AlertEvent`, `ConsumerConfig` |
| 导出函数 | 大驼峰，动词开头 | `NewConsumer`, `ValidateHostPort` |
| 接口 | 大驼峰，单方法以 er 结尾 | `AlertProcessor`, `ConsumerObserver` |
| 环境变量 | 全大写下划线 | `KAFKA_BROKERS`, `REDIS_ADDR` |

### 11.2 错误处理风格

```go
// ✅ 推荐：wrap 错误添加上下文
if err != nil {
    return fmt.Errorf("load config: %w", err)
}

// ❌ 避免：静默丢弃错误
_ = err

// ❌ 避免：同时日志和返回同一错误
log.Error("failed", zap.Error(err))
return err
```

---

## 12. 冗余代码清理清单

| 重复项 | 清理方式 | 优先级 |
|--------|---------|--------|
| Kafka Consumer/Producer/Topics | 提取到 `pkg/kafka/`，删除各服务本地副本 | P1 |
| Redis Client 基础方法 | 提取到 `pkg/redis/`，alert-service 保留业务脚本 | P2 |
| validateHostPort | 提取到 `pkg/configutil/validate.go`，删除本地副本 | P2 |

---

## 13. 实施步骤与风险控制

### 实施顺序（由小到大，逐步验证）

#### 步骤 1：configutil 小迁移（低风险）

1. 创建 `pkg/configutil/validate.go`，提取 `ValidateHostPort`
2. 更新三个服务的 config.go，使用 `configutil.ValidateHostPort`
3. 验证：`cd server-monitor && make build && make test`

#### 步骤 2：Kafka 公共包（中风险）

1. 创建 `pkg/kafka/` 包，以 server-web 版本为基准合并
2. 更新 `pkg/go.mod`，添加 sarama 依赖
3. 更新 server-web 和 alert-service 的 import
4. 删除各服务本地 `kafka/` 目录
5. **编写测试锁定两条路径**（server-web 的 retry backoff 路径 + alert-service 的基础路径）
6. 验证：`cd server-monitor && make build && make test && cd pkg && go test ./...`

#### 步骤 3：Redis 公共包（中风险）

1. 创建 `pkg/redis/` 包，提取基础客户端
2. 更新 `pkg/go.mod`，添加 go-redis 依赖
3. alert-service/redis/client.go 改为基于 `pkg/redis.Client` 的封装
4. server-web/redis/client.go 改为基于 `pkg/redis.Client` 的封装
5. 验证：`cd server-monitor && make build && make test && cd pkg && go test ./...`

#### 步骤 4：拆分 main/router（中风险）

1. 创建各服务的 `app.go`，迁移初始化逻辑
2. 精简各 main.go
3. 拆分 `api/router.go` 的服务组装逻辑
4. 修复 channel 等待超时问题
5. 验证：`cd server-monitor && make build && make test && cd pkg && go test ./...`

### 验证命令

```bash
cd server-monitor && make build && make test
cd server-monitor/pkg && go test ./...
```

### Makefile 补充

```makefile
test-pkg:
	@echo "运行 pkg 测试..."
	cd pkg && go test -v ./...
```

### 风险总览

| 风险 | 等级 | 缓解措施 |
|------|------|---------|
| Kafka Consumer 合并后行为差异 | 中 | 以 server-web 版本为基准，编写测试锁定两条路径 |
| Redis Client 合并后 alert-service Lua 脚本兼容 | 低 | 保留 alert-service/redis/client.go 业务封装，通过 `Inner()` 访问底层客户端 |
| router.go 拆分引入新 bug | 中 | 分步拆分，每步编译+测试验证 |
| pkg 依赖扩大（sarama、go-redis） | 低 | 这些库已被两个服务同时依赖，提取到 pkg 只是集中管理 |
| Channel 等待超时配置不合理 | 低 | 使用 ShutdownTimeout 作为超时值，与现有配置一致 |

---

## 14. 预期收益

### 14.1 量化指标

| 指标 | 现状 | 目标 | 改善幅度 |
|------|------|------|---------|
| 重复代码行数 | ~320 行 | ~0 行 | -100% |
| server-web/main.go 行数 | 454 行 | < 80 行 | -82% |
| api/router.go 行数 | 507 行 | < 150 行 | -70% |
| Channel 无超时阻塞等待 | 3 处 | 0 处 | -100% |
| pkg 模块测试覆盖 | 无 | 有 | 从无到有 |

### 14.2 质量收益

| 维度 | 改善内容 |
|------|---------|
| **可维护性** | 公共代码统一维护，修改一处生效全局 |
| **可测试性** | 依赖注入使单元测试可用 mock 替换真实依赖 |
| **可扩展性** | 新服务可直接复用 pkg/kafka、pkg/redis |
| **稳定性** | Channel 等待超时保护，避免关闭时无限阻塞 |
| **一致性** | 统一的代码风格和错误处理模式 |

---

## 15. 目录结构重构 — 参考项目分析

### 15.1 参考项目（honey_server）目录结构

```
honey_server/
├── cert/               # 证书文件
├── internal/            # 内部代码包（不可被外部导入）
│   ├── api/             # HTTP 处理层（请求解析、响应构建）
│   ├── config/          # 配置加载与校验
│   ├── core/            # 核心业务逻辑
│   ├── flags/           # 命令行参数
│   ├── global/          # 全局状态（本项目不需要）
│   ├── middleware/       # HTTP 中间件
│   ├── models/          # 数据模型定义
│   ├── routers/         # 路由注册
│   ├── rpc/             # RPC 客户端/服务端
│   ├── service/         # 业务服务层
│   └── utils/           # 工具函数
├── uploads/             # 上传文件存储
├── main.go              # 入口
├── settings.yaml        # 配置文件
├── Dockerfile
└── go.mod / go.sum
```

### 15.2 参考项目的组织逻辑

| 层级 | 目录 | 职责 | 关键原则 |
|------|------|------|---------|
| 入口 | `main.go` | 启动流程编排 | 仅调用 app.Run() |
| 路由 | `internal/routers/` | URL → Handler 映射 | 纯路由注册，不含业务 |
| 处理器 | `internal/api/` | HTTP 请求处理 | 解析参数、调用 service、返回响应 |
| 中间件 | `internal/middleware/` | 横切关注点 | 认证、日志、限流等 |
| 服务 | `internal/service/` | 业务逻辑编排 | 无 HTTP 依赖，可独立测试 |
| 数据 | `internal/models/` | 类型与模型定义 | 纯数据结构 |
| 配置 | `internal/config/` | 配置加载与校验 | 环境变量 → 结构体 |
| 基础设施 | `internal/infra/` | 外部客户端封装 | DB、Redis、Kafka 等 |

### 15.3 当前 server-web 目录结构问题

| 问题 | 描述 | 影响 |
|------|------|------|
| **无 internal 封装** | 所有包均可被外部导入，无访问控制边界 | 包稳定性无法保证 |
| **Handler 和 Service 混合** | `alert/`、`auth/`、`cache/`、`host/` 作为顶级包平铺，缺少统一的服务层入口 | Service 层职责不清 |
| **基础设施散落各处** | `kafka/`、`redis/`、`prometheus/`、`database/`、`pubsub/`、`websocket/` 作为顶级包平铺 | 缺乏"基础设施"的聚合概念 |
| **API 层过重** | `api/router.go` 同时承担路由注册和服务组装；`api/diagnosis_access_adapter.go` 是适配器却放在 api 下 | 单一职责违反 |
| **Copilot 子模块过大** | 65 个文件分散在 11 个子包中，部分子包内部结构不清晰（如 diagnosis 有 14 个文件） | 可维护性下降 |

---

## 16. 目录结构重构 — 目标结构

### 16.1 设计原则

1. **internal 封装**：所有业务代码放入 `internal/`，防止外部非法导入
2. **目录聚合 ≠ package 合并**：`internal/service/` 和 `internal/infra/` 是目录分类，每个子目录是独立 package，不把多个原独立包合并成单个 package（避免标识符冲突）
3. **pkg 优先**：跨服务复用的基础设施（Kafka、Redis）进 `pkg/`，与 §3 公共代码提取方案一致；服务特有的基础设施进 `internal/infra/<component>/`
4. **不删除功能代码**：仅移动位置，不删除文件内容；仅当 §3 明确要求合并（如 Kafka Consumer）时才合并
5. **适配 DI 架构**：不需要 global 包，配置通过构造函数注入
6. **Copilot 保持内聚**：作为大型功能模块保持内部子包结构，service/handler/session 保留为子包而非扁平化
7. **最小改动原则**：import 路径变更控制在合理范围，优先调整顶层结构

### 16.2 目标目录结构

> **关键设计决策：** `internal/service/` 和 `internal/infra/` 是**目录分类**，不是单个 Go package。
> 每个子目录保持独立 package 名，避免标识符冲突（如 alert.Service vs auth.Service）。
> Kafka/Redis 按 §3 进入 `pkg/kafka`、`pkg/redis`，不进 `internal/infra`。

```
server-web/
├── main.go                          # 入口（< 80 行）：initApp → runApp → shutdownApp
├── app.go                           # 应用组装（依赖注入）
│
├── internal/                        # 内部代码包（不可被外部导入）
│   │
│   ├── config/                      # 配置管理（package config）
│   │   └── config.go                # 配置加载与校验
│   │
│   ├── handler/                     # HTTP 处理层（package handler）
│   │   ├── handler.go               # Handler 集合 + 通用方法
│   │   ├── alert_rules.go           # 告警规则 CRUD
│   │   ├── alert_histories.go       # 告警历史查询
│   │   ├── channels.go              # 通知渠道管理
│   │   ├── host_groups.go           # 主机组管理
│   │   ├── users.go                 # 用户管理
│   │   └── auth.go                  # 认证登录
│   │
│   ├── router/                      # 路由注册（package router）
│   │   └── router.go                # 纯路由注册 + 中间件挂载
│   │
│   ├── middleware/                   # HTTP 中间件（package middleware）
│   │   ├── auth.go                  # JWT 认证
│   │   ├── cors.go                  # CORS
│   │   ├── logging.go               # 请求日志
│   │   ├── metrics.go               # Prometheus 指标收集
│   │   ├── ratelimit.go             # 滑动窗口限流
│   │   ├── rbac.go                  # 角色权限控制
│   │   └── recovery.go              # Panic 恢复
│   │
│   ├── service/                     # 业务服务层（目录分类，每个子目录是独立 package）
│   │   ├── alert/                   # package alert（原 alert/）
│   │   │   └── service.go
│   │   ├── auth/                    # package auth（原 auth/）
│   │   │   ├── service.go
│   │   │   ├── token.go
│   │   │   └── password.go
│   │   ├── cache/                   # package cache（原 cache/）
│   │   │   └── service.go
│   │   └── host/                    # package host（原 host/）
│   │       └── service.go
│   │
│   ├── model/                       # 数据模型（package model）
│   │   ├── models.go                # AllModels 注册
│   │   ├── alert_rule.go
│   │   ├── alert_history.go
│   │   ├── audit_log.go
│   │   ├── diagnosis_feedback.go
│   │   ├── diagnosis_report.go
│   │   ├── host_group.go
│   │   ├── notification_channel.go
│   │   ├── pending_action.go
│   │   └── user.go
│   │
│   ├── infra/                       # 基础设施客户端（目录分类，每个子目录是独立 package）
│   │   ├── database/                # package database（原 database/）
│   │   │   ├── mysql.go
│   │   │   └── migrate.go
│   │   ├── redis/                   # package rediscache（原 redis/，业务封装基于 pkg/redis.Client）
│   │   │   ├── client.go            # Redis 业务封装（限流、缓存等）
│   │   │   └── cache.go             # 缓存常量
│   │   ├── prometheus/              # package promclient（原 prometheus/）
│   │   │   ├── client.go
│   │   │   └── queries.go
│   │   ├── pubsub/                  # package pubsub（原 pubsub/）
│   │   │   ├── hub.go
│   │   │   └── subscriber.go
│   │   ├── websocket/               # package websocket（原 websocket/）
│   │   │   └── hub.go
│   │   └── webhook/                 # package webhook（原 webhook/）
│   │       └── alertmanager.go
│   │
│   └── copilot/                     # Copilot AI 模块（保持内部子包结构）
│       ├── service/                 # package service（原 copilot/service/）
│       │   └── service.go
│       ├── handler/                 # package handler（原 copilot/handler/）
│       │   └── handler.go
│       ├── session/                 # package session（原 copilot/session/）
│       │   └── store.go
│       │
│       ├── context/                 # 多轮上下文管理
│       │   └── manager.go
│       ├── summary/                 # LLM 工具结果摘要
│       │   ├── prompt.go
│       │   └── summarizer.go
│       ├── suggestion/              # 结构化建议构造与归一化
│       │   └── suggestion.go
│       │
│       ├── nlu/                     # 自然语言理解
│       │   ├── nlu.go
│       │   └── eval/                # package eval（nlu 评估，保持独立）
│       │       ├── comparator.go
│       │       ├── dataset.go
│       │       ├── evaluator.go
│       │       └── multi_evaluator.go
│       │
│       ├── llm/                     # LLM 客户端
│       │   └── client.go
│       │
│       ├── tool/                    # 工具系统
│       │   ├── contract.go
│       │   ├── executor.go
│       │   ├── registry.go
│       │   ├── errors.go
│       │   ├── schema_converter.go
│       │   ├── validator.go
│       │   ├── readonly_tools.go
│       │   ├── k8s_tool.go
│       │   └── runbook_tool.go
│       │
│       ├── runbook/                 # Runbook 检索
│       │   ├── retriever.go
│       │   ├── bm25.go
│       │   ├── embedder.go
│       │   ├── hybrid.go
│       │   ├── reranker.go
│       │   ├── loader.go
│       │   ├── parser.go
│       │   ├── chunker.go
│       │   ├── tokenizer.go
│       │   ├── vector_store.go
│       │   ├── types.go
│       │   └── eval/                # package eval（runbook 评估，保持独立）
│       │       ├── comparator.go
│       │       ├── dataset.go
│       │       └── evaluator.go
│       │
│       ├── k8s/                     # K8s 客户端
│       │   ├── client.go
│       │   ├── service.go
│       │   ├── types.go
│       │   └── sanitize.go
│       │
│       ├── diagnosis/               # 自动诊断
│       │   ├── service.go
│       │   ├── worker.go
│       │   ├── resolver.go
│       │   ├── evidence.go
│       │   ├── summarizer.go
│       │   ├── rule.go
│       │   ├── dedupe.go
│       │   ├── notifier.go
│       │   ├── handler.go
│       │   ├── request.go
│       │   ├── context.go
│       │   ├── json.go
│       │   ├── repository.go
│       │   └── types.go
│       │
│       ├── action/                  # 动作审批
│       │   ├── service.go
│       │   ├── handler.go
│       │   ├── repository.go
│       │   ├── policy.go
│       │   ├── audit.go
│       │   ├── metrics.go
│       │   ├── events.go
│       │   ├── notifier.go
│       │   ├── types.go
│       │   ├── k8s_executor.go
│       │   └── k8s_client_executor.go
│       │
│       └── feedback/                # 反馈
│           ├── service.go
│           ├── handler.go
│           ├── repository.go
│           └── types.go
│
├── docs/                            # Swagger 文档（不变）
│   └── docs.go
│
├── go.mod
├── go.sum
└── Dockerfile
```

**与 v2.0 目标结构的关键差异：**

| 变更点 | v2.0（有编译冲突） | v2.1（修订后） | 原因 |
|--------|-------------------|---------------|------|
| service 层 | `internal/service/` 单 package | `internal/service/<domain>/` 子 package | alert/auth/cache/host 都有 `Service`、`NewService`，单 package 编译冲突 |
| infra 层 | `internal/infra/` 单 package | `internal/infra/<component>/` 子 package | redis/prometheus/websocket 都有 `Client`，pubsub/websocket 都有 `Hub`，单 package 编译冲突 |
| Kafka | 进 `internal/infra/kafka_*` | 进 `pkg/kafka/`（按 §3） | 与公共代码提取方案一致，跨服务复用 |
| Redis 基础 | 进 `internal/infra/redis.go` | 进 `pkg/redis/`（按 §3） | 与公共代码提取方案一致，跨服务复用 |
| Redis 业务封装 | 无 | `internal/infra/redis/` | server-web 特有的限流、缓存等业务方法 |
| copilot service/handler/session | 扁平化到 `internal/copilot/` | 保留子包 `internal/copilot/service/` 等 | service.go 依赖 session 子包并定义别名，扁平化需重写引用 |
| copilot eval | 合并到 `internal/copilot/eval/` | 保留 `nlu/eval/` 和 `runbook/eval/` 各自独立 | 两边都有 dataset.go/evaluator.go/comparator.go，合并会撞文件名 |

### 16.3 各目录职责对照

| 目标目录 | 来源 | 职责 | 改动类型 |
|---------|------|------|---------|
| `internal/config/` | `config/` | 配置加载、校验、默认值 | 移入 internal |
| `internal/handler/` | `api/handlers/` | HTTP 请求处理 | 移入 internal，重命名包 |
| `internal/router/` | `api/router.go` | 路由注册 | 拆分为独立目录 |
| `internal/middleware/` | `api/middleware/` | HTTP 中间件 | 从 api 下移出为同级 |
| `internal/service/alert/` | `alert/` | 告警服务 | 移入 internal/service 子目录，package 名不变 |
| `internal/service/auth/` | `auth/` | 认证服务 | 移入 internal/service 子目录，package 名不变 |
| `internal/service/cache/` | `cache/` | 缓存服务 | 移入 internal/service 子目录，package 名不变 |
| `internal/service/host/` | `host/` | 主机服务 | 移入 internal/service 子目录，package 名不变 |
| `internal/model/` | `model/` | GORM 数据模型 | 移入 internal |
| `internal/infra/database/` | `database/` | MySQL 客户端 + 迁移 | 移入 internal/infra 子目录，package 名不变 |
| `internal/infra/redis/` | `redis/` | Redis 业务封装（基于 pkg/redis.Client） | 移入 internal/infra 子目录，package 名不变（`rediscache`） |
| `internal/infra/prometheus/` | `prometheus/` | Prometheus 客户端 | 移入 internal/infra 子目录，package 名不变（`promclient`） |
| `internal/infra/pubsub/` | `pubsub/` | 发布订阅 | 移入 internal/infra 子目录，package 名不变 |
| `internal/infra/websocket/` | `websocket/` | WebSocket Hub | 移入 internal/infra 子目录，package 名不变 |
| `internal/infra/webhook/` | `webhook/` | Webhook 类型 | 移入 internal/infra 子目录，package 名不变 |
| `pkg/kafka/` | `server-web/kafka/` + `alert-service/kafka/` | Kafka 公共包（跨服务复用） | 按 §3 提取到 pkg |
| `pkg/redis/` | `server-web/redis/` + `alert-service/redis/` | Redis 基础客户端（跨服务复用） | 按 §3 提取到 pkg |
| `internal/copilot/` | `copilot/` | AI Copilot 功能模块 | 移入 internal，内部子包结构不变 |

---

## 17. 目录结构重构 — 文件移动映射

### 17.1 核心层映射

| 原路径 | 目标路径 | 说明 |
|--------|---------|------|
| `config/config.go` | `internal/config/config.go` | 配置 |
| `api/handlers/*.go` | `internal/handler/*.go` | Handler |
| `api/router.go` | `internal/router/router.go` | 路由 |
| `api/diagnosis_access_adapter.go` | `internal/copilot/diagnosis/access_adapter.go` | 适配器归入 copilot，需改为导出构造函数（见下方说明） |
| `api/middleware/*.go` | `internal/middleware/*.go` | 中间件 |
| `alert/service.go` | `internal/service/alert/service.go` | 告警服务（package 名不变） |
| `auth/service.go` | `internal/service/auth/service.go` | 认证服务（package 名不变） |
| `auth/token.go` | `internal/service/auth/token.go` | Token 管理 |
| `auth/password.go` | `internal/service/auth/password.go` | 密码处理 |
| `cache/service.go` | `internal/service/cache/service.go` | 缓存服务（package 名不变） |
| `host/service.go` | `internal/service/host/service.go` | 主机服务（package 名不变） |
| `model/*.go` | `internal/model/*.go` | 数据模型 |
| `database/mysql.go` | `internal/infra/database/mysql.go` | MySQL（package 名不变） |
| `database/migrate.go` | `internal/infra/database/migrate.go` | 迁移 |
| `redis/client.go` | `internal/infra/redis/client.go` | Redis 业务封装（package `rediscache`，底层改用 pkg/redis.Client） |
| `redis/cache.go` | `internal/infra/redis/cache.go` | 缓存常量 |
| `kafka/consumer.go` | `pkg/kafka/consumer.go` | Kafka Consumer（按 §3 进 pkg） |
| `kafka/producer.go` | `pkg/kafka/producer.go` | Kafka Producer（按 §3 进 pkg） |
| `kafka/topics.go` | `pkg/kafka/topics.go` | Topic 常量（按 §3 进 pkg） |
| `kafka/consumer_test.go` | `pkg/kafka/consumer_test.go` | Consumer 测试 |
| `prometheus/client.go` | `internal/infra/prometheus/client.go` | Prometheus（package `promclient`） |
| `prometheus/queries.go` | `internal/infra/prometheus/queries.go` | PromQL |
| `pubsub/hub.go` | `internal/infra/pubsub/hub.go` | PubSub Hub（package 名不变） |
| `pubsub/subscriber.go` | `internal/infra/pubsub/subscriber.go` | Subscriber |
| `websocket/hub.go` | `internal/infra/websocket/hub.go` | WebSocket Hub（package 名不变） |
| `webhook/alertmanager.go` | `internal/infra/webhook/alertmanager.go` | Webhook（package 名不变） |

> **diagnosisAccessAdapter 迁移说明：** 当前 `diagnosisAccessAdapter` 是 `package api` 下的未导出类型，由同包 `router.go:282` 直接构造（`&diagnosisAccessAdapter{repo: ...}`）。移到 `internal/copilot/diagnosis/` 包后，`internal/router` 无法再直接构造未导出类型。需要改为导出构造函数：
>
> ```go
> // internal/copilot/diagnosis/access_adapter.go
> package diagnosis
>
> import (
>     "context"
>     "server-web/internal/model"
> )
>
> // ReportAccessAdapter 报告访问权限适配器，
> // 实现 feedback.ReportAccessChecker 接口（由调用方按接口接收）。
> type ReportAccessAdapter struct {
>     repo *Repository
> }
>
> // NewReportAccessChecker 创建报告访问权限检查适配器。
> func NewReportAccessChecker(repo *Repository) *ReportAccessAdapter {
>     return &ReportAccessAdapter{repo: repo}
> }
>
> // GetAccessibleReport 校验用户权限并返回诊断报告。
> func (a *ReportAccessAdapter) GetAccessibleReport(ctx context.Context, id uint64, userID uint64, role string) (model.DiagnosisReport, error) {
>     return a.repo.GetByID(ctx, id, User{ID: userID, Role: role})
> }
> ```
>
> `internal/router/router.go` 改为调用 `diagnosis.NewReportAccessChecker(repo)`。
> `feedback.NewHandler` 按接口接收即可，diagnosis 包不需要反向 import feedback（避免 import cycle）。

### 17.2 Copilot 内部结构调整

| 原路径 | 目标路径 | 说明 |
|--------|---------|------|
| `copilot/service/service.go` | `internal/copilot/service/service.go` | 保持子包（service.go 依赖 session 子包并定义别名） |
| `copilot/handler/handler.go` | `internal/copilot/handler/handler.go` | 保持子包 |
| `copilot/session/store.go` | `internal/copilot/session/store.go` | 保持子包 |
| （AI 升级新增） | `internal/copilot/context/*` | 多轮上下文管理，仍在 copilot 内聚边界内 |
| （AI 升级新增） | `internal/copilot/summary/*` | LLM 工具结果摘要生成 |
| （AI 升级新增） | `internal/copilot/suggestion/*` | 结构化建议构造与归一化 |
| `copilot/nlu/nlu.go` | `internal/copilot/nlu/nlu.go` | 保持子包 |
| `copilot/nlu/eval/*` | `internal/copilot/nlu/eval/*` | 保持 nlu 下的 eval 子包，不合并 |
| `copilot/llm/client.go` | `internal/copilot/llm/client.go` | 保持子包 |
| `copilot/tool/*` | `internal/copilot/tool/*` | 保持子包 |
| `copilot/runbook/*` | `internal/copilot/runbook/*` | 保持子包（不含 eval/） |
| `copilot/runbook/eval/*` | `internal/copilot/runbook/eval/*` | 保持 runbook 下的 eval 子包，不合并 |
| `copilot/k8s/*` | `internal/copilot/k8s/*` | 保持子包 |
| `copilot/diagnosis/*` | `internal/copilot/diagnosis/*` | 保持子包 |
| `copilot/action/*` | `internal/copilot/action/*` | 保持子包 |
| `copilot/feedback/*` | `internal/copilot/feedback/*` | 保持子包 |

> **为什么不合并 eval：** `copilot/nlu/eval/` 和 `copilot/runbook/eval/` 各自包含 `dataset.go`、`evaluator.go`、`comparator.go`，文件名相同但内容不同。合并到同一目录会撞文件名，且两个 package eval 的导出符号也可能冲突。保持各自独立是最安全的做法。

> **为什么不扁平化 service/handler/session：** `copilot/service/service.go` 内部 import `copilot/session` 并定义了 `SessionStore` 等别名。扁平化到 `internal/copilot/` 意味着这三个子包的文件全部变成 `package copilot`，需要重写所有内部引用，改动量大且收益不明显。

### 17.3 Import 路径变更

```go
// 重构前
"server-web/alert"
"server-web/auth"
"server-web/cache"
"server-web/host"
"server-web/api/handlers"
"server-web/api/middleware"
"server-web/database"
"server-web/kafka"
"server-web/redis"
"server-web/prometheus"
"server-web/pubsub"
"server-web/websocket"
"server-web/webhook"
"server-web/model"
"server-web/copilot/service"
"server-web/copilot/handler"
"server-web/copilot/session"

// 重构后
"server-web/internal/service/alert"     // package alert（名不变）
"server-web/internal/service/auth"      // package auth（名不变）
"server-web/internal/service/cache"     // package cache（名不变）
"server-web/internal/service/host"      // package host（名不变）
"server-web/internal/handler"           // package handler
"server-web/internal/middleware"        // package middleware
"server-web/internal/infra/database"    // package database（名不变）
"server-web/internal/infra/redis"       // package rediscache（需 alias：rediscache "server-web/internal/infra/redis"）
"server-web/internal/infra/prometheus"  // package promclient（需 alias：promclient "server-web/internal/infra/prometheus"）
"server-web/internal/infra/pubsub"      // package pubsub（名不变）
"server-web/internal/infra/websocket"   // package websocket（名不变）
"server-web/internal/infra/webhook"     // package webhook（名不变）
"server-web/internal/model"             // package model
"server-web/internal/router"            // package router
"server-web/internal/copilot/service"   // package service（名不变）
"server-web/internal/copilot/handler"   // package handler（名不变）
"server-web/internal/copilot/session"   // package session（名不变）
"server-monitor/pkg/kafka"              // package kafka（按 §3）
"server-monitor/pkg/redis"              // package redis（按 §3，与 internal/infra/redis 的 rediscache 区分）
```

> **关键优势：** 由于 service/infra 采用子包模式且 package 名不变，大部分业务代码的**包内引用**（如 `alert.NewService`、`database.NewMySQL`）无需修改。只需更新 import 路径的前缀部分（`server-web/alert` → `server-web/internal/service/alert`）。
>
> **注意：** `rediscache` 和 `promclient` 的目录名与 package 名不同（`internal/infra/redis/` → `package rediscache`，`internal/infra/prometheus/` → `package promclient`），import 时需保留 alias，与当前代码风格一致。

---

## 18. 目录结构重构 — 与参考项目差异

| 维度 | honey_server（参考） | server-web（目标） | 差异原因 |
|------|---------------------|-------------------|---------|
| `global/` | ✅ 有 | ❌ 不需要 | 本项目采用 DI 架构，配置通过构造函数注入 |
| `flags/` | ✅ 有 | ❌ 不需要 | 本项目通过环境变量配置，无需命令行参数 |
| `rpc/` | ✅ 有 | ❌ 不需要 | 本项目是纯 HTTP REST API，无 RPC |
| `core/` | ✅ 有 | ❌ 用 service 替代 | 命名偏好不同，语义一致 |
| `routers/` | ✅ 独立目录 | ✅ 独立目录 | 一致 |
| `utils/` | ✅ 有 | ❌ 不需要 | 工具函数已收敛到 pkg/configutil |
| `cert/` | ✅ 有 | ❌ 不需要 | 本项目无证书管理需求 |
| `uploads/` | ✅ 有 | ❌ 不需要 | 本项目无文件上传需求 |
| `settings.yaml` | ✅ 有 | ❌ 不需要 | 本项目使用环境变量配置 |
| `infra/` 单 package | ✅ 无此目录 | ❌ 目录分类 + 子 package | 本项目基础设施客户端有标识符冲突（Client/Hub），必须拆子包 |
| `service/` 单 package | ✅ 单 package | ❌ 目录分类 + 子 package | 本项目多个服务都有 Service/NewService，单 package 编译冲突 |
| `copilot/` | ❌ 无 | ✅ 有 | 本项目特有的大型 AI 功能模块 |
| `handler/` 在 `internal/api/` 下 | ✅ | ❌ 独立于 router 同级 | 更清晰的分层：router → handler → service |
| `pkg/` 跨服务公共包 | ❌ 无 | ✅ 有 | 本项目多服务共享 Kafka/Redis，需独立 module |

---

## 19. 目录结构重构 — 实施步骤与风险

### 19.1 实施步骤

#### 步骤 1：创建目标目录结构

```bash
cd server-web
mkdir -p internal/{config,handler,router,middleware,model}
mkdir -p internal/service/{alert,auth,cache,host}
mkdir -p internal/infra/{database,redis,prometheus,pubsub,websocket,webhook}
mkdir -p internal/copilot/{service,handler,session,nlu,llm,tool,runbook,k8s,diagnosis,action,feedback}
mkdir -p internal/copilot/nlu/eval
mkdir -p internal/copilot/runbook/eval
```

#### 步骤 2：按映射清单移动文件

使用 `git mv` 逐个移动文件，保持 Git 历史连续性。

**移动顺序建议（由底层到上层）：**
1. `model/` → `internal/model/`（无业务依赖）
2. `database/` → `internal/infra/database/`（仅依赖 model）
3. `prometheus/` → `internal/infra/prometheus/`（无业务依赖）
4. `redis/` → `internal/infra/redis/`（底层基础设施）
5. `pubsub/` → `internal/infra/pubsub/`（依赖 redis）
6. `websocket/` → `internal/infra/websocket/`（无业务依赖）
7. `webhook/` → `internal/infra/webhook/`（无业务依赖）
8. `alert/` → `internal/service/alert/`（依赖 redis、kafka）
9. `auth/` → `internal/service/auth/`（依赖 database）
10. `cache/` → `internal/service/cache/`（依赖 redis）
11. `host/` → `internal/service/host/`（依赖 prometheus、cache）
12. `copilot/` → `internal/copilot/`（依赖上述全部）
13. `api/middleware/` → `internal/middleware/`
14. `api/handlers/` → `internal/handler/`
15. `api/router.go` → `internal/router/router.go`
16. `config/` → `internal/config/`

> **Kafka 文件不在此步骤移动：** `kafka/` 目录按 §3 提取到 `pkg/kafka/`，属于 §13 步骤 2 的范围，不在此处处理。

#### 步骤 3：更新所有 import 路径

全局搜索替换（见 §17.3）。由于 package 名不变，只需替换 import 路径前缀：

```bash
# 示例替换命令（需逐个确认）
sed -i 's|"server-web/alert"|"server-web/internal/service/alert"|g' $(find . -name '*.go')
sed -i 's|"server-web/auth"|"server-web/internal/service/auth"|g' $(find . -name '*.go')
sed -i 's|"server-web/database"|"server-web/internal/infra/database"|g' $(find . -name '*.go')
# ... 其余见 §17.3
```

#### 步骤 4：验证

```bash
cd server-monitor && make build && make test
cd server-monitor/pkg && go test ./...
```

> **无需更新 package 声明：** 由于 service/infra 采用子包模式且 package 名保持不变（如 `package alert`、`package database`），移动后 package 声明无需修改。这是子包模式相比单 package 合并的核心优势。

### 19.2 风险评估

| 风险 | 等级 | 缓解措施 |
|------|------|---------|
| import 路径遗漏导致编译失败 | 高 | 全局搜索替换 + `go build` 逐包验证 |
| import 路径层级加深，可读性略降 | 低 | IDE 自动补全，实际影响小；换来的是封装性和无冲突 |
| copilot 内部 import 路径变更多 | 中 | copilot 子包结构不变，只需更新前缀 `server-web/copilot/` → `server-web/internal/copilot/` |
| git mv 后历史追踪断裂 | 低 | 使用 git mv 而非 mv + git add |
| 第三方工具（如 swag）依赖旧路径 | 低 | swag 基于注释生成，不受路径影响 |
| `internal/infra/redis/` 与 `pkg/redis` 混淆 | 低 | 命名区分：`pkg/redis` 是基础客户端（跨服务复用），`internal/infra/redis` 是 server-web 业务封装（限流、缓存等） |

### 19.3 文件移动统计

| 操作类型 | 数量 |
|---------|------|
| 移入 `internal/config/` | 1 个文件 |
| 移入 `internal/handler/` | 7 个文件 |
| 移入 `internal/router/` | 1 个文件 |
| 移入 `internal/middleware/` | 7 个文件 |
| 移入 `internal/service/alert/` | 1 个文件 |
| 移入 `internal/service/auth/` | 3 个文件 |
| 移入 `internal/service/cache/` | 1 个文件 |
| 移入 `internal/service/host/` | 1 个文件 |
| 移入 `internal/model/` | 10 个文件 |
| 移入 `internal/infra/database/` | 2 个文件 |
| 移入 `internal/infra/redis/` | 2 个文件 |
| 移入 `internal/infra/prometheus/` | 2 个文件 |
| 移入 `internal/infra/pubsub/` | 2 个文件 |
| 移入 `internal/infra/websocket/` | 1 个文件 |
| 移入 `internal/infra/webhook/` | 1 个文件 |
| 移入 `internal/copilot/` | ~76 个非测试 Go 文件 |
| **合计** | **~118 个文件移动/新增** |

> **注：** Kafka 的 4 个文件（consumer.go、producer.go、topics.go、consumer_test.go）按 §3 提取到 `pkg/kafka/`，不在此处统计。

---

## 20. 构建部署与 CI 镜像影响

目录结构重构后，构建部署和 CI 配置需要同步调整。以下逐项分析影响和所需变更。

### 20.1 Dockerfile

三个服务的 Dockerfile 构建模式一致：先 `COPY pkg/`，再 `COPY <service>/go.mod go.sum`，`go mod download`，最后 `COPY <service>/ ./`。

**影响分析：**

| Dockerfile | 当前 COPY 模式 | 是否需要修改 | 原因 |
|-----------|---------------|------------|------|
| `server-web/Dockerfile` | `COPY server-web/ ./` | ❌ 不需要 | COPY 是把整个 `server-web/` 目录复制到容器的 `WORKDIR`，`internal/` 是子目录，自动包含在内 |
| `alert-service/Dockerfile` | `COPY alert-service/ ./` | ❌ 不需要 | alert-service 不涉及目录重构 |
| `server-probe/Dockerfile` | `COPY server-probe/ ./` | ❌ 不需要 | server-probe 不涉及目录重构 |

**但有一个前提：** Docker build context 是项目根目录（`docker-compose.yml` 中 `context: .`），所以 `COPY server-web/ ./` 会把重构后的 `server-web/internal/` 一并复制进去，`go build` 能正确找到。无需修改 Dockerfile。

### 20.2 CI (`.github/workflows/ci.yaml`)

**大部分 CI 步骤不受影响：** `goimports`、`go test ./...`、`go vet ./...` 都在 `working-directory: server-web` 下递归执行，`internal/` 子目录会被自动包含。

Copilot AI 升级新增的 `internal/copilot/context/`、`internal/copilot/summary/`、`internal/copilot/suggestion/` 仍位于 `server-web` module 内；结构化建议和 SSE 响应只影响 `server-web` 编译、测试与前端构建，不需要新增独立 CI job。

**需要修改的步骤：**

| CI 步骤 | 当前路径 | 修改后路径 | 原因 |
|---------|---------|-----------|------|
| NLU Evaluation | `./copilot/nlu/eval/` | `./internal/copilot/nlu/eval/` | copilot 移入 internal |
| RAG Evaluation | `./copilot/runbook/eval/` | `./internal/copilot/runbook/eval/` | copilot 移入 internal |
| NLU Multi-Intent Evaluation | `./copilot/nlu/eval/` | `./internal/copilot/nlu/eval/` | copilot 移入 internal |

**需要新增的 job：**

当前 CI 只有 `server-probe`、`server-web`、`alert-service` 三个 Go module 的检查 job，没有 `pkg` job。但 `pkg/kafka`、`pkg/redis` 新增后，`pkg` 成为包含核心公共代码和测试（如 `consumer_test.go`）的独立 module，必须有独立的 CI 检查。

```yaml
pkg:
  name: pkg checks
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4

    - uses: actions/setup-go@v5
      with:
        go-version: "1.26"
        cache-dependency-path: pkg/go.sum

    - name: Install goimports
      run: go install golang.org/x/tools/cmd/goimports@latest

    - name: Goimports
      working-directory: pkg
      run: test -z "$(goimports -l .)"

    - name: Test
      working-directory: pkg
      run: go test ./...

    - name: Vet
      working-directory: pkg
      run: go vet ./...
```

同时需要把 `docker-build` job 的 `needs` 列表加上 `pkg`，确保 pkg 测试通过后才构建镜像。

**具体变更：**

```yaml
# 修改前
- name: NLU Evaluation
  working-directory: server-web
  run: |
    set -o pipefail
    go test ./copilot/nlu/eval/ -run TestEvaluate -v -count=1 2>&1 | tee nlu-eval-report.txt

- name: RAG Evaluation
  working-directory: server-web
  run: |
    set -o pipefail
    go test ./copilot/runbook/eval/ -run TestRAGEval -v -count=1 2>&1 | tee rag-eval-report.txt

- name: NLU Multi-Intent Evaluation
  working-directory: server-web
  run: |
    set -o pipefail
    go test ./copilot/nlu/eval/ -run TestEvaluateMulti -v -count=1 2>&1 | tee nlu-multi-eval-report.txt

# 修改后
- name: NLU Evaluation
  working-directory: server-web
  run: |
    set -o pipefail
    go test ./internal/copilot/nlu/eval/ -run TestEvaluate -v -count=1 2>&1 | tee nlu-eval-report.txt

- name: RAG Evaluation
  working-directory: server-web
  run: |
    set -o pipefail
    go test ./internal/copilot/runbook/eval/ -run TestRAGEval -v -count=1 2>&1 | tee rag-eval-report.txt

- name: NLU Multi-Intent Evaluation
  working-directory: server-web
  run: |
    set -o pipefail
    go test ./internal/copilot/nlu/eval/ -run TestEvaluateMulti -v -count=1 2>&1 | tee nlu-multi-eval-report.txt
```

### 20.3 Makefile

**不受影响。** `go test ./...`、`go fmt ./...` 会递归包含 `internal/` 子目录；`go build .` 编译入口包并解析 `internal/` 下的 import，因此也无需改。`test-pkg` 目标已在 §13 中新增。

### 20.4 docker-compose.yml

**不受影响。** docker-compose 引用的是 Dockerfile 路径和环境变量，不涉及源码目录结构。服务镜像构建由 Dockerfile 完成，Dockerfile 本身无需修改。

### 20.5 Helm Charts

**不受影响。** Charts 部署的是 Docker 镜像，不关心源码目录结构。

### 20.6 变更汇总

| 配置文件 | 是否需要修改 | 修改内容 |
|---------|------------|---------|
| `server-web/Dockerfile` | ❌ | COPY 模式自动包含 internal/ |
| `alert-service/Dockerfile` | ❌ | 不涉及目录重构 |
| `server-probe/Dockerfile` | ❌ | 不涉及目录重构 |
| `.github/workflows/ci.yaml` | ✅ | 1. 新增 `pkg` job（goimports + test + vet）；2. 3 个 eval 测试步骤路径加 `internal/`；3. `docker-build.needs` 加 `pkg` |
| `Makefile` | ❌ | `go test ./...`/`go fmt ./...` 递归包含 internal；`go build .` 解析 internal imports |
| `docker-compose.yml` | ❌ | 不涉及源码结构 |
| `charts/` | ❌ | 部署镜像，不涉及源码 |

> **关键结论：** 目录结构重构对构建部署的影响较小。Dockerfile、docker-compose、Helm Charts 均无需修改。Makefile 无需修改。CI 需要两类变更：新增 `pkg` job 并加入 `docker-build.needs`，以及修改 3 个 eval 测试路径从 `./copilot/` 改为 `./internal/copilot/`。
