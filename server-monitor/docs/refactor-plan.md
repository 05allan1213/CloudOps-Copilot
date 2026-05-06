# server-monitor 项目整改方案

> 生成日期：2026-05-05
> 基于全量代码审查，涵盖 3 个 Go 微服务 + 1 个 Vue 前端 + Docker/K8s/Helm 基础设施

---

## 一、问题总览

| # | 问题 | 严重程度 | 修复难度 | 涉及模块 |
|---|------|---------|---------|---------|
| 1 | 核心测试覆盖仍不足 | 🔴 致命 | 高 | 全部 Go 服务 |
| 2 | 跨服务代码大量复制粘贴 | 🔴 严重 | 中 | tracer/logger/config/middleware |
| 3 | 中间件行为不一致 | 🔴 严重 | 低 | alert-service / server-probe |
| 4 | handlers.go 上帝文件（1121 行） | 🟠 高 | 中 | server-web |
| 5 | 配置无注释、无验证、接口不统一 | 🟠 高 | 低 | 三个 config.go |
| 6 | 无依赖注入框架，组件手工传递 | 🟡 中 | 中 | 三个 main.go |
| 7 | WebSocket Hub 无最大连接数限制 | 🟡 中 | 低 | server-web/websocket |
| 8 | config 包反向依赖 logger 包 | 🟡 中 | 低 | server-probe/config |
| 9 | 项目无中文注释，英文注释也几乎为零 | 🟡 中 | 低 | 全部 Go 服务 |
| 10 | 死代码与无用全局状态 | 🟢 低 | 极低 | server-probe/main.go |
| 11 | Alertmanager Webhook 数据结构不完整 | 🟢 低 | 低 | server-web/webhook |
| 12 | 优雅关闭逻辑跨服务重复 | 🟢 低 | 低 | 三个 main.go |
| 13 | 文档缺失（API 文档 / 配置文档 / 数据流文档） | 🟡 中 | 中 | 全项目 |
| 14 | Helm 默认 Secret 不完整，默认安装可能无法启动 | 🔴 严重 | 低 | charts/server-monitor |
| 15 | K8s/Helm 告警规则同步未接通 | 🔴 严重 | 中 | server-web / Prometheus / Helm |
| 16 | Compose 默认关闭鉴权且 Web API 暴露到所有网卡 | 🔴 严重 | 低 | docker-compose / server-web |
| 17 | HTTP API 接受 query token，Token 容易进入日志和历史记录 | 🟠 高 | 低 | server-web / frontend |
| 18 | JWT 纯无状态校验，用户删除或角色变更后旧 Token 仍有效 | 🟠 高 | 中 | server-web/auth |
| 19 | Helm values 中部分资源配置不生效 | 🟡 中 | 低 | charts/server-monitor |

---

## 二、问题详细分析

### 问题 1：核心测试覆盖仍不足（🔴 致命）

**现状：** 当前仓库已经存在 `*_test.go` 文件，不能再按“零测试覆盖”处理；但测试仍不均衡，部分高风险链路缺少足够的边界测试和集成级验证。

**仍需重点补强的关键逻辑：**

| 逻辑 | 所在文件 | 风险说明 |
|------|---------|---------|
| Redis Lua 脚本（告警去重） | server-web/redis/cache.go L194-230 | 原子操作，bug 极难排查 |
| Redis Lua 脚本（滑动窗口限流） | server-web/redis/cache.go L136-175 | 限流准确性无法保证 |
| Redis Lua 脚本（告警 firing/resolved） | alert-service/redis/client.go L28-71 | 告警状态机正确性无保障 |
| Kafka 消息处理与偏移量提交 | alert-service/kafka/consumer.go | 消息丢失或重复消费风险 |
| 告警 fingerprint 去重 | server-web/api/handlers/handlers.go L490-612 | 去重失效会导致告警风暴 |
| JWT 令牌生成/验证 | server-web/auth/token.go | 安全漏洞风险 |
| RBAC 权限校验 | server-web/api/middleware/rbac.go | 越权访问风险 |
| PromQL 查询模板 | server-web/prometheus/queries.go | 查询错误导致监控盲区 |
| 配置加载与验证 | 三个 config.go | 非法配置导致运行时崩溃 |
| Docker/Helm 渲染结果 | docker-compose.yml / charts/server-monitor | 静态 lint 通过不代表默认部署能启动 |

**整改建议：**

1. **优先级 P0**：为所有 Redis Lua 脚本编写单元测试，使用 `miniredis` 模拟 Redis
2. **优先级 P0**：为告警去重逻辑编写表驱动测试
3. **优先级 P1**：为 JWT 生成/验证、RBAC 校验编写测试
4. **优先级 P1**：为 Kafka 消费者编写测试（使用 mock 接口）
5. **优先级 P2**：为 PromQL 模板、配置加载编写测试

**目标：** 核心逻辑（去重、限流、认证、消费）测试覆盖率达到 80%+，其余逻辑逐步补充。

---

### 问题 2：跨服务代码大量复制粘贴（🔴 严重）

**现状：** 三个 Go 微服务各自拥有独立的 go.mod，但存在大量完全相同的代码复制。

#### 2.1 tracer.go — 100% 完全相同

| 文件 | 行数 | 差异 |
|------|------|------|
| server-web/tracer/tracer.go | 61 行 | — |
| alert-service/tracer/tracer.go | 61 行 | 与 server-web 逐字一致 |
| server-probe/tracer/tracer.go | 61 行 | 与 server-web 逐字一致 |

**重复行数：** 61 × 3 = 183 行纯浪费

#### 2.2 logger.go — 核心代码 100% 相同，接口不统一

| 文件 | 行数 | 差异 |
|------|------|------|
| server-web/logger/logger.go | 246 行 | 含 slog 桥接 |
| server-probe/logger/logger.go | 246 行 | 与 server-web 逐字一致 |
| alert-service/logger/logger.go | 121 行 | 无 slog 桥接，Init 签名不同 |

**关键差异：**
- server-web/server-probe：`Init(service string)` — 日志级别从 `os.Getenv("LOG_LEVEL")` 内部读取
- alert-service：`Init(service, rawLevel string)` — 日志级别通过参数传入

**重复行数：** 246 × 2 + 121 = 613 行，其中约 400 行是纯重复

#### 2.3 config.go 辅助函数 — ~80% 相同，行为有微妙差异

以下函数在三个文件中重复实现：

| 函数 | server-web | alert-service | server-probe | 一致性 |
|------|-----------|--------------|-------------|--------|
| `getEnv` | ✅ | ✅ | ✅ | 完全相同 |
| `getEnvNonEmpty` | ✅ | ✅ | ✅ | 完全相同 |
| `getEnvFloatRange` | ✅ | ✅ | ✅ | 完全相同 |
| `getEnvPositiveInt` | ✅ | ✅ | ❌ | server-web 与 alert-service 相同 |
| `getEnvInt` | ✅ | ❌ | ✅ | ⚠️ server-probe 的 `getEnvInt` 行为等同于 `getEnvPositiveInt` |
| `getEnvDurationSeconds` | ✅ | ✅ | ✅ | ⚠️ server-probe 底层用 `getEnvInt`，行为不同 |
| `getEnvList` | ✅ | ✅ | ❌ | ⚠️ 签名不同：server-web 无 fallback，alert-service 有 fallback |
| `getEnvBool` | ✅ | ❌ | ❌ | 仅 server-web 有 |
| `getEnvPath` | ❌ | ❌ | ✅ | 仅 server-probe 有 |

**重复行数：** 约 60 行 × 3 = 180 行

#### 2.4 main.go 中间件 — ~70% 相同，行为不一致

alert-service/main.go 和 server-probe/main.go 中的中间件代码高度重复：

| 组件 | 重复度 | 关键差异 |
|------|--------|---------|
| statusRecorder / statusResponseWriter | ~80% | 类型名不同；WriteHeader 防重复逻辑不同 |
| loggingMiddleware | ~60% | alert-service 手动创建 OTel span，server-probe 有 request ID |
| recoveryMiddleware | ~85% | 日志字段不同；错误响应格式不同（JSON vs 纯文本） |

**重复行数：** 约 80 行 × 2 = 160 行

**整改建议：**

1. **创建共享 Go Module** `pkg/`，将以下代码提取为共享包：
   - `pkg/tracer/` — 统一 tracer 初始化
   - `pkg/logger/` — 统一 logger 初始化（统一 Init 签名，保留 slog 桥接）
   - `pkg/configutil/` — 统一环境变量读取辅助函数（消除行为差异）
   - `pkg/httpmiddleware/` — 统一 HTTP 中间件（logging、recovery、request ID）
   - `pkg/shutdown/` — 统一优雅关闭逻辑

2. **三个服务通过 `replace` 指令引用共享模块**，或发布为内部 Go Module

3. **统一接口签名**：logger.Init 统一为 `Init(service string)`，日志级别从环境变量读取

---

### 问题 3：中间件行为不一致（🔴 严重）

**现状：** 同一项目的不同服务，对同一类 HTTP 请求的处理方式不同。

| 行为 | alert-service | server-probe | 影响 |
|------|--------------|-------------|------|
| recoveryMiddleware 错误响应 | JSON 格式 `{"status":"error","error":"..."}` | 纯文本 `Internal Server Error` | API 网关/前端无法统一解析错误 |
| statusRecorder WriteHeader | 允许重复调用 | 防止重复调用（`status != 0` 时 return） | 可能导致不同的 HTTP 响应行为 |
| loggingMiddleware 链路追踪 | 手动创建 OTel span | 使用 `otelhttp.NewHandler` | 追踪数据格式不一致 |
| Request ID | 无 | 有 | 无法跨服务追踪请求 |

**整改建议：**

1. 提取统一中间件到 `pkg/httpmiddleware/`
2. 错误响应统一为 JSON 格式
3. statusRecorder 统一为防重复 WriteHeader 版本
4. 链路追踪统一使用 `otelhttp.NewHandler`
5. 所有服务统一支持 Request ID 传播

---

### 问题 4：handlers.go 上帝文件（🟠 高）

**现状：** [server-web/api/handlers/handlers.go](server-web/api/handlers/handlers.go) 共 1121 行，混合了多种职责。

**当前职责分布：**

| 职责 | 行数 | 应归属 |
|------|------|--------|
| HTTP 请求处理 | ~300 行 | handler 层 |
| 业务逻辑（过滤/排序/转换） | ~200 行 | service 层 |
| 缓存读写 | ~150 行 | cache service 层 |
| 告警 Webhook 处理 | ~122 行 | webhook service 层 |
| 辅助函数 | ~200 行 | 各自归属的层 |
| 数据结构定义 | ~150 行 | model 层 |

**关键问题方法：**

- `AlertmanagerWebhook`（L490-612，122 行）：包含验证、存储、去重、归档、发布、Kafka 发送 6 个职责
- `Hosts`（L313-365）：包含缓存读取、Prometheus 查询、过滤、排序、分组
- `ReadyzFull`（L257-311）：包含多组件健康检查逻辑

**整改建议：**

分步拆分，不要一次性重构：

1. **第一步**：提取缓存操作为独立的 `CacheService`（接口 + 实现）
2. **第二步**：提取告警处理逻辑为 `AlertService`（Webhook 处理、去重、归档）
3. **第三步**：提取主机查询逻辑为 `HostService`（查询、过滤、排序）
4. **第四步**：Handler 层只保留 HTTP 请求解析和响应组装

---

### 问题 5：配置无注释、无验证、接口不统一（🟠 高）

**现状：**

#### 5.1 零注释

三个 config.go 合计 **72 个环境变量**，没有一个字段有注释。示例：

```go
// 当前代码（无注释）
PrometheusAddr     string
RequestTimeout     time.Duration
HostsBroadcastInterval time.Duration

// 应改为
// PrometheusAddr Prometheus 查询地址，格式为 http://host:port
// 默认值：http://prometheus:9090
PrometheusAddr string

// RequestTimeout Prometheus 查询超时时间
// 默认值：10s，最小值：1s
RequestTimeout time.Duration
```

#### 5.2 验证严重不足

| 服务 | 环境变量数 | 验证项数 | 缺失验证 |
|------|-----------|---------|---------|
| server-web | 48 | 1（仅 JWTSecret） | 端口范围、URL 格式、必填项、正整数 |
| alert-service | 12 | 0 | 全部 |
| server-probe | 12 | 0 | 全部 |

#### 5.3 辅助函数行为不一致

- `getEnvInt`（server-probe）实际行为等同于 `getEnvPositiveInt`，命名误导
- `getEnvList` 签名不同：server-web 无 fallback 参数，alert-service 有 fallback 参数

#### 5.4 config 包反向依赖 logger 包

server-probe/config/config.go 的 `getHostname()` 函数直接调用 `zap.L().Warn()`，违反了依赖方向。

**整改建议：**

1. 为所有配置字段添加中文注释，说明用途、默认值、是否必填、是否敏感
2. 为每个服务补充 `Validate()` 方法，校验端口范围、URL 格式、必填项
3. 统一辅助函数到 `pkg/configutil/`，消除行为差异
4. server-probe/config 移除对 logger 的依赖，`getHostname()` 改为返回 error

---

### 问题 6：无依赖注入框架（🟡 中）

**现状：** 所有组件在 main() 中手动创建，通过函数参数传递。

**典型问题：**

```go
// server-web/main.go L190
// 7 个参数，随着功能增长会越来越难维护
router, err := api.NewRouter(cfg, prometheusClient, redisClient, mysqlClient, authService, websocketHub, kafkaProducer)
```

**接口定义不够彻底：**

- `*promclient.Client`、`*ws.Hub` 是具体类型注入，没有接口抽象
- `cacheClient` 接口定义在 handlers 包里，而不是由 redis 包导出

**整改建议：**

1. 引入 `google/wire` 或 `uber-go/fx` 进行编译期/运行期依赖注入
2. 为 `*promclient.Client`、`*ws.Hub` 等提取接口
3. 接口由消费方定义（当前部分已正确，如 handlers.go 中的 cacheClient）
4. 如果不引入 DI 框架，至少将 main() 拆分为 `initApp()` / `runApp()` / `shutdownApp()` 三个阶段函数

---

### 问题 7：WebSocket Hub 无最大连接数限制（🟡 中）

**现状：** [server-web/websocket/hub.go](server-web/websocket/hub.go) 的 Hub 没有最大连接数限制。

**风险：** 如果客户端大量连接（无论正常还是恶意），会造成内存压力，甚至 OOM。

**整改建议：**

1. 在 Config 中增加 `WSMaxConnections` 配置项（默认 1000）
2. Hub.Register 中检查当前连接数，超出时拒绝新连接并返回错误
3. 增加连接数监控指标（Prometheus Gauge）

---

### 问题 8：config 包反向依赖 logger 包（🟡 中）

**现状：** server-probe/config/config.go 的 `getHostname()` 函数直接调用 `zap.L().Warn()`。

**问题：** config 是最底层的包，应在 logger 之前初始化。config 依赖 logger 违反了依赖方向，可能导致初始化顺序问题。

**整改建议：**

`getHostname()` 改为返回 error，由调用方决定如何处理：

```go
// 改前
func getHostname() string {
    hostname, err := os.Hostname()
    if err != nil {
        zap.L().Warn("failed to get hostname, using 'unknown'", zap.Error(err))
        return "unknown"
    }
    return hostname
}

// 改后
func getHostname() (string, error) {
    hostname, err := os.Hostname()
    if err != nil {
        return "unknown", err
    }
    return hostname, nil
}
```

---

### 问题 9：项目无中文注释（🟡 中）

**现状：** 整个项目的 Go 代码几乎没有注释（无论中英文）。结构体字段无注释、函数无文档、复杂逻辑无说明。

**整改建议：**

1. 所有导出类型和函数添加中文注释（Go doc 规范）
2. 复杂逻辑（Redis Lua 脚本、告警去重、滑动窗口限流）添加详细中文注释
3. 配置字段添加中文注释（见问题 5）
4. 每个包添加 `doc.go` 说明包的职责

---

### 问题 10：死代码与无用全局状态（🟢 低）

**现状：**

| 位置 | 代码 | 问题 |
|------|------|------|
| server-probe/main.go L271-279 | `requestIDFromRequest()` | 未被任何代码调用 |
| server-probe/main.go L32 | `requestIDCounter` | 包级变量，增加全局状态 |
| server-probe/main.go L31 | `requestIDContextKey{}` | 仅在 loggingMiddleware 中使用，可内联 |

**整改建议：** 删除 `requestIDFromRequest` 函数，将 `requestIDCounter` 和 `requestIDContextKey` 移到使用它们的中间件函数内部。

---

### 问题 11：Alertmanager Webhook 数据结构不完整（🟢 低）

**现状：** [server-web/webhook/alertmanager.go](server-web/webhook/alertmanager.go) 的 `AlertmanagerWebhookRequest` 缺少以下字段：

- `GroupLabels map[string]string` — 告警分组标签
- `CommonLabels map[string]string` — 公共标签
- `CommonAnnotations map[string]string` — 公共注解
- `ExternalURL string` — Alertmanager 外部 URL

这些信息在告警分组展示时有用，当前被静默丢弃。

**整改建议：** 补充缺失字段，并在前端告警展示中使用。

---

### 问题 12：优雅关闭逻辑跨服务重复（🟢 低）

**现状：** 三个服务的信号监听 + 分阶段关闭逻辑高度相似，但各自实现了一遍。

**整改建议：** 提取到 `pkg/shutdown/` 包，提供通用的分阶段关闭框架：

```go
// 预期接口
type Phase struct {
    Name    string
    Timeout time.Duration
    Fn      func(ctx context.Context) error
}

func Graceful(shutdownTimeout time.Duration, phases []Phase)
```

---

### 问题 13：文档缺失（🟡 中）

**现状：**

| 文档类型 | 状态 | 影响 |
|---------|------|------|
| API 接口文档（OpenAPI/Swagger） | 无 | 前后端协作困难 |
| 配置文档（环境变量说明） | 无 | 部署和运维困难 |
| 数据流文档 | 无 | 新人理解困难 |
| 架构设计文档 | 无 | 无法全局理解系统 |

**整改建议：**

1. 使用 `swaggo/swag` 自动生成 API 文档
2. 从 config.go 注释自动生成配置文档
3. 绘制核心数据流图（Alertmanager → Webhook → Kafka → alert-service → Redis → Pub/Sub → WebSocket）
4. 补充 README 中的架构说明

---

### 问题 14：Helm 默认 Secret 不完整，默认安装可能无法启动（🔴 严重）

**现状：** `charts/server-monitor/values.yaml` 默认只配置了 `redisPassword`、`grafanaAdminUser`、`grafanaAdminPassword`。但 `templates/server-web.yaml` 固定引用：

- `MYSQL_PASSWORD`
- `JWT_SECRET`
- `ADMIN_PASSWORD`

`templates/mysql.yaml` 也固定引用：

- `MYSQL_ROOT_PASSWORD`
- `MYSQL_PASSWORD`

而 `templates/secret.yaml` 只有在 `.Values.secret.mysqlPassword`、`.Values.secret.mysqlRootPassword`、`.Values.secret.jwtSecret`、`.Values.secret.adminPassword` 存在时才渲染这些 key。

**风险：**

1. `helm lint` 可以通过，但默认安装后 Pod 可能因 Secret key 缺失进入 `CreateContainerConfigError`。
2. `auth.enabled` 默认是 true，`JWT_SECRET` 缺失会直接影响 server-web 启动。
3. chart 的“默认可安装”预期被破坏，用户必须额外知道要补哪些 secret。

**整改建议：**

1. 在 `values.yaml` 中补齐本地/学习环境默认值，并明确标注必须生产替换。
2. 或者使用 `required` 在模板渲染阶段直接失败，避免部署后才暴露问题。
3. 在 README / Helm 文档中列出必须配置的敏感项。

---

### 问题 15：K8s/Helm 告警规则同步未接通（🔴 严重）

**现状：** Docker Compose 中 server-web 设置了：

- `PROMETHEUS_RELOAD_URL`
- `ALERT_RULES_FILE_PATH`
- `PROMTOOL_PATH`

但 Helm 的 `monitor-config` 和 `server-web` Deployment 没有配置 `ALERT_RULES_FILE_PATH`，server-web 代码在该字段为空时会返回 `alert rule sync is not configured`。

同时，Helm 中 Prometheus 规则来自 ConfigMap 挂载，server-web 即使补上路径，也不能直接写 ConfigMap 挂载目录完成规则更新。

**风险：**

1. K8s/Helm 环境中，前端创建的告警规则只能落库，不能同步到 Prometheus。
2. UI 可能表现为“同步失败”，但部署文档没有提前说明。
3. 若直接让 server-web 写 Prometheus 规则目录，会遇到 ConfigMap 只读挂载和多副本一致性问题。

**整改建议：**

1. 明确告警规则同步在 Compose 与 K8s 的不同实现方式。
2. K8s 环境不要让 server-web 直接写 ConfigMap；优先设计独立的规则同步方案，例如：
   - 使用 PVC 挂载可写规则目录；
   - 或通过受控 controller / Job 更新 ConfigMap 并触发 Prometheus reload；
   - 或对接 Prometheus Operator 的 `PrometheusRule`。
3. 在 Helm values 中增加显式开关，未接通时隐藏或禁用同步入口。

---

### 问题 16：Compose 默认关闭鉴权且 Web API 暴露到所有网卡（🔴 严重）

**现状：** `docker-compose.yml` 中 server-web：

- 端口映射为 `"8080:8080"`，会绑定宿主机所有网卡；
- `AUTH_ENABLED` 默认值为 `false`；
- 写接口仅在 `AUTH_ENABLED=true` 时才启用鉴权和 admin 角色校验。

**风险：**

1. 开发机在局域网或云主机上运行 Compose 时，未鉴权 API 可能被外部访问。
2. 告警规则、主机分组、通知渠道、用户注册等写接口在默认 Compose 下没有保护。
3. 默认 `ADMIN_PASSWORD=admin` 和默认 JWT Secret 容易被误用到非本地环境。

**整改建议：**

1. Compose 默认改为 `127.0.0.1:8080:8080`。
2. 将 `AUTH_ENABLED` 默认改为 true，或至少在文档中明确“仅限本机学习环境”。
3. 对写接口增加更保守的默认保护策略，避免关闭鉴权时暴露高风险操作。

---

### 问题 17：HTTP API 接受 query token，Token 容易进入日志和历史记录（🟠 高）

**现状：** 前端 WebSocket 连接通过 `/ws/alerts?token=xxx` 传递 token；后端鉴权中间件在所有受保护路由缺少 `Authorization` 时都会尝试读取 `?token=`。

**风险：**

1. query token 可能进入浏览器历史、代理访问日志、网关日志和 Referer。
2. 普通 HTTP API 也接受 query token，暴露面超出 WebSocket 的实际需要。
3. 当前访问日志记录 path，虽然不记录 raw query，但后续网关或代理未必会过滤 query。

**整改建议：**

1. **短期**：普通 HTTP API 仍接受 query token，但在日志中间件中过滤 token 参数，避免进入日志
2. **中期**：前端改为只通过 `Authorization: Bearer <token>` 传 token，仅 `/ws/alerts` 保留 query token
3. **长期**：WebSocket 改用短期一次性 ticket（登录后通过 API 获取，一次性使用后失效）
4. 明确日志策略：任何网关、反代、应用日志都不得记录 token query

---

### 问题 18：JWT 纯无状态校验，用户删除或角色变更后旧 Token 仍有效（🟠 高）

**现状：** 登录时把 `id`、`username`、`role` 写入 JWT。鉴权只校验签名和过期时间，不回查数据库中的用户状态。

**风险：**

1. 删除用户后，该用户已签发的 token 在过期前仍可继续访问。
2. 用户角色从 admin 降级为 viewer 后，旧 token 仍保留 admin 权限。
3. 发生 token 泄露时只能等待过期，缺少主动吊销能力。

**整改建议：**

1. 短期方案：缩短 token TTL，并在文档中明确旧 token 生效窗口。
2. 中期方案：在用户表增加 `token_version` 或 `password_changed_at`，JWT 中携带版本，鉴权时回查。
3. 高安全场景：增加 Redis blacklist / session 表，实现主动吊销。

---

### 问题 19：Helm values 中部分资源配置不生效（🟡 中）

**现状：** `values.yaml` 定义了 `serverWeb.resources` 和 `serverProbe.resources`，但模板中 server-web、server-probe 的 `resources` 是硬编码值；只有 alert-service 使用了 `.Values.alertService.resources`。

**风险：**

1. 用户通过 values 覆盖 server-web / server-probe 资源限制时不会生效。
2. 线上排查资源问题时容易误判，以为调整已发布。
3. Helm values 与模板行为不一致，降低 chart 可维护性。

**整改建议：**

1. server-web / server-probe 模板统一改为 `toYaml .Values.xxx.resources`。
2. 对所有有 values 配置的资源项做一次模板一致性检查。
3. 增加 `helm template` 结果断言或文档化验证命令。

---

## 三、整改优先级与实施计划

### 阶段一A：安全止血（P0 — 必须立即修复）✅ 已完成

| 序号 | 任务 | 涉及文件 | 状态 |
|------|------|---------|------|
| 1.1 | 修复 Helm 默认 Secret 缺失问题 | charts/server-monitor/values.yaml, templates/secret.yaml | ✅ 已完成 |
| 1.2 | 收紧 Compose 默认暴露面与鉴权默认值 | docker-compose.yml, README.md | ✅ 已完成 |
| 1.3 | 明确并修复 K8s/Helm 告警规则同步策略 | charts/server-monitor/, server-web/ | ✅ 已完成 |

### 阶段一B：代码止血（P0 — 必须立即修复）✅ 已完成

| 序号 | 任务 | 涉及文件 | 状态 |
|------|------|---------|------|
| 1.4 | 为 Redis Lua 脚本编写单元测试 | server-web/redis/client_test.go, alert-service/redis/client_test.go | ✅ 已完成 |
| 1.5 | 为告警去重逻辑编写测试 | server-web/api/handlers/handlers_test.go, alert-service/alert/ | ✅ 已完成 |
| 1.6 | 统一中间件行为，消除不一致 | → 合并至 2.1，随 pkg/httpmiddleware/ 一起提取 | ⏩ 合并至阶段二 |

### 阶段二：去重（P1 — 核心代码质量提升）✅ 已完成

| 序号 | 任务 | 涉及文件 | 状态 |
|------|------|---------|------|
| 2.1 | 创建 pkg/ 共享模块（含中间件统一，承接 1.6） | pkg/tracer/, pkg/logger/, pkg/configutil/, pkg/httpmiddleware/ | ✅ 已完成 |
| 2.2 | 三个服务引用共享模块 | 三个 go.mod, 三个 main.go | ✅ 已完成 |
| 2.3 | 统一 logger.Init 签名 | alert-service/logger/, alert-service/main.go | ✅ 已完成 |
| 2.4 | 统一 config 辅助函数 | 三个 config.go | ✅ 已完成 |

### 阶段三：分层（P1 — 架构改善）✅ 已完成

| 序号 | 任务 | 涉及文件 | 状态 |
|------|------|---------|------|
| 3.1 | handlers.go 拆分：提取 CacheService | server-web/cache/service.go（134行）+ 测试 | ✅ 已完成 |
| 3.2 | handlers.go 拆分：提取 AlertService | server-web/alert/service.go（446行）+ 测试 | ✅ 已完成 |
| 3.3 | handlers.go 拆分：提取 HostService | server-web/host/service.go（396行）+ 测试 | ✅ 已完成 |
| 3.4 | main.go 拆分为 initApp/runApp/shutdownApp | 三个 main.go | ✅ 已完成 |

> **成果**：handlers.go 从 1121 行降至约 550 行，Handler 层仅保留 HTTP 请求解析和响应组装。三个 main.go 均采用 initApp/runApp/shutdownApp 三阶段结构。
>
> **残留复核**：`parseAlertEventFilter` 已不存在；`validAlertEventStatuses` / `validAlertEventSeverities` 仍被当前 handler 层过滤参数校验使用，不属于可直接删除的死代码。

### 阶段四：加固（P2 — 安全与健壮性）

| 序号 | 任务 | 涉及文件 | 预计改动 |
|------|------|---------|---------|
| 4.1 | 配置添加中文注释 | 三个 config.go | 修改 |
| 4.2 | 配置添加 Validate | 三个 config.go | 修改 |
| 4.3 | WebSocket Hub 增加最大连接数 | server-web/websocket/hub.go, server-web/config/config.go | 修改 |
| 4.4 | config 包移除对 logger 的依赖 | server-probe/config/config.go | 修改 |
| 4.5 | 补充 JWT/RBAC/Kafka 消费者测试 | server-web/auth/, server-web/api/middleware/, alert-service/kafka/ | 新增 |
| 4.6 | 限制 query token 只用于 WebSocket 或改为一次性 ticket | server-web/api/middleware/, server-web/api/router.go, frontend/src/ | 修改 |
| 4.7 | 增加 JWT 主动失效机制 | server-web/auth/, server-web/model/ | 修改 |
| 4.8 | 修复 Helm resources values 不生效问题 | charts/server-monitor/templates/ | 修改 |

### 阶段五：完善（P3 — 锦上添花）

| 序号 | 任务 | 涉及文件 | 预计改动 |
|------|------|---------|---------|
| 5.1 | 删除死代码 | server-probe/main.go | 修改 |
| 5.2 | 补充 Alertmanager Webhook 字段 | server-web/webhook/alertmanager.go | 修改 |
| 5.3 | 提取优雅关闭为共享包 | pkg/shutdown/ | 新增 |
| 5.4 | 生成 API 文档 | server-web/ | 新增 |
| 5.5 | 补充项目文档 | README.md, docs/ | 新增 |
| 5.6 | 复核 handlers.go 遗留代码 | server-web/api/handlers/handlers.go | 已复核，无可直接删除死代码 |

> **5.6 详情**：当前代码中 `parseAlertEventFilter` 已不存在；`validAlertEventStatuses` 和 `validAlertEventSeverities` 仍被 `alert_histories.go` / `alert_rules.go` 使用，用于 handler 层过滤参数校验，暂不删除。

---

## 四、关键数据

### 代码规模

| 模块 | Go 文件数 | 代码行数（估算） |
|------|---------|----------------|
| server-web | ~25 | ~3500 |
| alert-service | ~10 | ~1200 |
| server-probe | ~10 | ~800 |
| **共享可提取** | — | **~800 行重复代码** |

### 重复代码统计

| 重复模块 | 重复行数 | 占比 |
|---------|---------|------|
| tracer.go × 3 | 183 行 | 100% |
| logger.go × 2（完全相同）+ 1（部分相同） | ~400 行 | ~80% |
| config 辅助函数 × 3 | ~180 行 | ~80% |
| main.go 中间件 × 2 | ~160 行 | ~70% |
| **合计** | **~920 行** | — |

### 测试覆盖

| 模块 | 当前状态 | 目标 |
|------|----------|------|
| Redis Lua 脚本 | 已有测试入口，但需继续核对边界覆盖 | 80%+ |
| 告警去重逻辑 | 需补充更多表驱动场景 | 80%+ |
| JWT/RBAC | 已有基础测试，需补角色变更、删除用户、过期策略场景 | 80%+ |
| Kafka 消费者 | 已有基础测试，需补 rebalance、临时错误重试、永久错误提交策略 | 60%+ |
| 配置加载 | 已有基础测试，需补 URL、端口、必填敏感项验证 | 60%+ |
| Docker/Helm 渲染 | 需补默认安装可用性验证 | helm template 结果符合 values 预期 |

---

## 五、风险提示

1. **阶段一A（安全止血）优先级最高**：Helm Secret 缺失、Compose 暴露面、鉴权默认值是"默认部署即有安全风险"的问题，应最先修复。阶段一B（代码止血）紧随其后。

2. **阶段二（去重）风险最高**：创建共享模块后，三个服务的 go.mod 需要调整，可能影响本地开发和 CI 构建。建议先在一个服务中验证，再推广到其他服务。

3. **阶段三（分层）改动范围最大**：handlers.go 拆分涉及多个文件的联动修改，必须分步进行，每步确保编译通过和功能正常。

4. **测试补充应贯穿始终**：不要等所有重构完成后再补测试。每个阶段完成后，先为当前代码补测试，再进入下一阶段。

5. **不要一次性大改**：严格按照 AGENTS.md 的"小步实现"原则，每个任务独立完成、独立验证、独立提交。

6. **静态检查不能代替默认部署验证**：`helm lint`、`docker compose config` 只能证明语法和渲染基本成立，不能证明 Secret key、ConfigMap 写入路径、鉴权默认值和端口暴露策略符合预期。

7. **安全默认值优先保守**：学习项目可以保留便捷模式，但必须默认绑定本机、默认开启鉴权或在文档中明确风险，避免被误用到可被外部访问的环境。
