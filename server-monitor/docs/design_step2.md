# 第二阶段：补日志链路 — 详细实现方案

## 一、阶段目标

建立统一的 **日志采集 → 日志存储 → 日志查询** 体系，实现结构化 JSON 日志输出、Fluent Bit 自动采集、Elasticsearch 索引存储、Kibana 可视化查询，并与 Grafana 联动实现指标 + 日志关联查询。

### 阶段边界

第二阶段只处理日志链路，不提前实现第三阶段链路追踪或事件驱动能力。

**第二阶段必须完成**：

- Go 服务统一输出结构化 JSON 日志
- 日志级别通过 `LOG_LEVEL` 配置
- Fluent Bit 采集 Docker Compose 容器日志和 Kubernetes Pod 日志
- Elasticsearch 存储并按时间索引日志
- Kibana 能按服务、级别、时间范围查询日志
- Grafana 接入 Elasticsearch 数据源，支持指标面板旁查看日志

**第二阶段只预留，不实现**：

- `trace_id` / `span_id` 字段映射
- 日志与 Trace 的跳转关系
- OpenTelemetry SDK、Jaeger、跨服务 trace 上下文传播

**第二阶段不做**：

- 日志告警规则
- 日志长期归档到对象存储
- 多租户日志隔离
- 用户权限、登录、审计日志
- Kafka / VictoriaMetrics / Jaeger 接入

### 阶段内实施拆分

第二阶段整体方案可以完整设计，但实现时必须按小模块推进：

| 模块 | 内容 | 完成标准 |
|------|------|---------|
| 2.1a 日志基础设施 | server-probe / server-web 分别新增 logger 包，统一 JSON Encoder、LOG_LEVEL、service、instance 字段 | 两个服务都能初始化 zap Logger，启动早期日志也不会退回到文本格式 |
| 2.1b server-probe 日志标准化 | server-probe 从 slog 迁移到 zap，HTTP 请求耗时统一输出 `latency_ms` | server-probe stdout 日志是 JSON，包含 service、instance、level、msg |
| 2.1c server-web 日志标准化 | server-web 从 slog / gin 默认日志迁移到 zap，包含 request_id、HTTP 请求日志、recovery 日志 | server-web stdout 日志是 JSON，包含 service、instance、level、msg、request_id |
| 2.2 本地日志链路 | Docker Compose 增加 Elasticsearch、Kibana、Fluent Bit | 本地访问接口后，日志能进入 ES，并可在 Kibana 查询 |
| 2.3 K8s 日志链路 | Helm 增加 Fluent Bit DaemonSet、ES StatefulSet、Kibana Deployment | Pod 日志进入 ES，并带 Kubernetes 元数据 |
| 2.4 查询与联动 | Grafana 增加 ES 数据源和日志面板，Kibana Data View 给出可复现创建步骤 | Grafana / Kibana 都能按时间窗口查询日志 |
| 2.5 生命周期与初始化 | ES index template、ILM policy、初始化脚本或 Job | `sm-logs-*` 字段类型稳定，30 天保留策略生效 |

每个模块完成后单独格式化、测试、验证和提交，不把代码改造、部署改造、Dashboard 改造混在同一个提交里。

### 完成标志

- [ ] 所有服务输出结构化 JSON 日志（zap Logger）
- [ ] 日志级别可通过环境变量配置（LOG_LEVEL）
- [ ] 日志中包含 service、instance 等默认字段
- [ ] ES 索引模板预留 trace_id、span_id 字段（为第三阶段链路追踪做准备）
- [ ] Fluent Bit 以 DaemonSet 部署，自动采集所有 Pod 日志
- [ ] Fluent Bit 正确解析 JSON 日志，添加 Kubernetes 元数据
- [ ] Elasticsearch 成功接收并索引日志
- [ ] Kibana 可查询和可视化日志
- [ ] Grafana 关联 ES 数据源，实现指标 + 日志联动查询
- [ ] Docker Compose 本地开发环境包含 EFK 栈
- [ ] Helm Chart 包含 Fluent Bit + Elasticsearch + Kibana 部署

---

## 二、技术栈

```
日志输出：  Go zap（uber-go/zap，云原生 Go 生态事实标准）
日志采集：  Fluent Bit 3.x（CNCF 项目，C 编写极轻量，K8s 原生 DaemonSet）
日志存储：  Elasticsearch 8.x（倒排索引，全文检索）
日志查询：  Kibana 8.x（日志可视化查询）
监控联动：  Grafana + Elasticsearch 数据源（指标 + 日志关联）
部署：      Docker Compose（本地开发）+ Helm Chart（K8s 生产）
```

### 新增中间件

| 组件 | 版本 | 职责 | 部署形态 |
|------|------|------|---------|
| Fluent Bit | 3.x | DaemonSet 采集 Pod 日志，解析 JSON，添加 K8s 元数据，写入 ES | DaemonSet（K8s）/ 独立容器（Docker Compose） |
| Elasticsearch | 8.x | 日志索引与全文检索 | 单节点 StatefulSet（开发）/ 集群（生产） |
| Kibana | 8.x | 日志查询与可视化 | Deployment |

### 新增 Go 依赖

| 依赖 | 用途 |
|------|------|
| go.uber.org/zap | 结构化 JSON 日志，零分配高性能 |

### 不引入的组件

| 组件 | 原因 |
|------|------|
| Logstash | Fluent Bit 更轻量，资源占用更少，K8s 场景下 Fluent Bit 是主流选择 |
| Loki | 虽然轻量，但市场占有率和面试深度不如 ES，本项目选择 ES |
| Filebeat | 较重且非 CNCF，Fluent Bit 更云原生 |
| slog | 标准库方案可用，但本阶段选择 zap 来演示更完整的云原生结构化日志实践 |
| logrus | 已停止维护，性能差，不推荐 |
| zerolog | 社区规模不如 zap，云原生项目采用率低 |

---

## 三、架构设计

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Kubernetes Cluster                            │
│                                                                      │
│  ┌──────────────────┐   ┌──────────────────┐                        │
│  │ server-probe     │   │ server-web       │                        │
│  │ (DaemonSet)      │   │ (Deployment)     │                        │
│  │                  │   │                  │                        │
│  │ stdout → JSON    │   │ stdout → JSON    │                        │
│  │ {service,level,  │   │ {service,level,  │                        │
│  │  instance,       │   │  instance,       │                        │
│  │  request_id,...} │   │  request_id,...} │                        │
│  └────────┬─────────┘   └────────┬─────────┘                        │
│           │                      │                                   │
│           ▼                      ▼                                   │
│  ┌──────────────────────────────────────────┐                        │
│  │ 容器运行时（containerd / Docker）         │                        │
│  │ 将 stdout/stderr 写入 /var/log/containers/│                        │
│  └────────────────────┬─────────────────────┘                        │
│                       │                                              │
│                       ▼                                              │
│  ┌──────────────────────────────────────────┐                        │
│  │ Fluent Bit (DaemonSet)                    │                        │
│  │ 每个 Node 一个 Pod                         │                        │
│  │                                            │                        │
│  │ Input:  tail /var/log/containers/*.log     │                        │
│  │ Parser: CRI/Docker + JSON 应用日志            │                        │
│  │ Filter: Kubernetes (添加 Pod 元数据)        │                        │
│  │ Output: Elasticsearch                      │                        │
│  └────────────────────┬─────────────────────┘                        │
│                       │                                              │
│                       ▼                                              │
│  ┌──────────────────────────────────────────┐                        │
│  │ Elasticsearch                              │                        │
│  │ 索引: sm-logs-2024.04.25                   │                        │
│  │ ILM: delete(30d)，先不做 rollover/warm      │                        │
│  │                                            │                        │
│  │ 日志字段:                                   │                        │
│  │  - ts, level, msg, service, instance       │                        │
│  │  - request_id, method, path, trace_id      │                        │
│  │  - kubernetes.pod, kubernetes.namespace     │                        │
│  │  - kubernetes.container, kubernetes.node    │                        │
│  └──────────┬───────────────────┬────────────┘                        │
│             │                   │                                     │
│             ▼                   ▼                                     │
│  ┌──────────────────┐  ┌──────────────────┐                          │
│  │ Kibana            │  │ Grafana           │                          │
│  │ 日志查询与可视化   │  │ ES 数据源关联      │                          │
│  │ Dashboard 预配置  │  │ 指标 + 日志联动    │                          │
│  └──────────────────┘  └──────────────────┘                          │
│                                                                      │
│  ┌──────────────────┐                                               │
│  │ Prometheus       │  指标链路不变                                   │
│  │ Grafana          │  新增 ES 数据源                                 │
│  │ AlertManager     │  告警链路不变                                   │
│  │ Redis            │  缓存链路不变                                   │
│  └──────────────────┘                                               │
└─────────────────────────────────────────────────────────────────────┘
```

### 关键架构决策

#### 为什么选择 zap 而不是 slog

| 维度 | zap | slog |
|------|-----|------|
| 云原生生态 | **K8s / etcd / containerd / OTel Go SDK 均使用 zap** | 云原生核心项目采用率低 |
| OpenTelemetry 集成 | **社区中有成熟 zap 集成方案，便于第三阶段扩展 trace 字段** | slog 也可集成，但本项目当前不选双日志路线 |
| 性能 | **高性能、低分配，适合高频结构化日志** | 标准库方案足够通用，但本阶段目标是演示云原生日志栈 |
| 结构化字段 | `zap.String/Int/Error` 强类型，编译期检查 | `slog.String` 也支持，但 key 是字符串无编译检查 |
| 全局 Logger | `zap.ReplaceGlobals()` 替换全局 logger | `slog.SetDefault()` 替换全局 logger |
| 面试含金量 | **云原生 Go 岗位高频考点** | 较少被问，属于标准库基础能力 |
| 社区验证 | **生产级验证，Uber 开源，10 年历史** | Go 1.21 才引入，生产验证时间短 |

**结论**：从云原生学习项目角度，zap 的工程实践价值更突出，结构化字段、调用位置、采样、Core 扩展都更适合后续接入 trace 字段。第三阶段引入 OTel 时，可以在保持 JSON 日志格式稳定的前提下补充 `trace_id` / `span_id`。

#### 为什么选择 Fluent Bit 而不是 Filebeat

| 维度 | Fluent Bit | Filebeat |
|------|-----------|----------|
| 语言 | C 编写，极轻量 | Go 编写，较重 |
| 内存占用 | ~5MB | ~40MB+ |
| CNCF | CNCF 项目 | 非 CNCF（Elastic 公司） |
| K8s 原生 | DaemonSet 部署标准方案 | DaemonSet 部署也可 |
| 许可证 | Apache 2.0 | Elastic License 2.0（部分功能） |
| 面试价值 | 云原生标准方案 | 传统方案 |

#### Docker Compose 日志采集方案

Docker Compose 环境下无法使用 DaemonSet，采用以下方案：

```
方案：Fluent Bit 挂载 Docker 容器日志目录

所有服务 stdout/stderr
        ↓
Docker json-file log driver（默认）
写入 /var/lib/docker/containers/<container_id>/<container_id>-json.log
        ↓
Fluent Bit 容器挂载 /var/lib/docker/containers:/var/log/docker:ro
        ↓
Fluent Bit tail Input 读取日志文件
        ↓
Parser 解析 Docker JSON 包装 + 内层应用 JSON 日志
        ↓
Output → Elasticsearch
```

这个方案与 K8s 生产环境的采集逻辑一致，只是日志文件路径和元数据来源不同。

---

## 四、结构化 JSON 日志改造

### 4.1 改造要点

| 改动 | 原代码 | 改造后 |
|------|--------|--------|
| 日志库 | log/slog（默认 TextHandler） | **zap（JSON Encoder）** |
| 日志格式 | 文本格式 | JSON 格式 |
| 日志级别 | 无配置，全部输出 | 可通过 LOG_LEVEL 环境变量配置 |
| 默认字段 | 无 | service、instance |
| trace_id / span_id | 无 | ES 映射预留字段，第三阶段接入 OTel 后再输出真实值 |
| 全局替换 | slog.Info/Error/Warn | zap.L().Info/Error/Warn |

**重要约束**：日志初始化必须早于业务初始化。`config.Load()` 这类早期配置加载函数不应直接打业务日志；如果必须记录配置加载异常，应在 `logger.Init()` 前使用一个最小 bootstrap logger，或改为返回错误由 `main()` 在 logger 初始化后记录，避免启动早期日志仍是文本格式。

### 4.2 日志格式规范

#### 标准输出格式

```json
{
  "ts": "2024-04-25T10:30:00.123Z",
  "level": "info",
  "msg": "http request",
  "service": "server-web",
  "instance": "server-web-7d8f9c6b4-x2k1p",
  "request_id": "lz9j1q-1",
  "method": "GET",
  "path": "/api/v1/hosts",
  "status": 200,
  "latency_ms": 12.5,
  "client_ip": "10.244.0.1"
}
```

#### 字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| ts | string | 是 | ISO8601 时间戳，zap 自动生成 |
| level | string | 是 | 日志级别：debug / info / warn / error |
| msg | string | 是 | 日志消息 |
| service | string | 是 | 服务名：server-probe / server-web |
| instance | string | 是 | 实例标识（Pod 名或主机名） |
| trace_id | string | 否 | 链路追踪 ID，第二阶段只在 ES mapping 预留，第三阶段接入 OTel 后输出 |
| span_id | string | 否 | Span ID，第二阶段只在 ES mapping 预留，第三阶段接入 OTel 后输出 |
| request_id | string | 否 | 请求 ID（仅 HTTP 请求日志） |
| method | string | 否 | HTTP 方法（仅 HTTP 请求日志） |
| path | string | 否 | 请求路径（仅 HTTP 请求日志） |
| status | int | 否 | HTTP 状态码（仅 HTTP 请求日志） |
| latency_ms | float | 否 | 请求耗时毫秒数，便于 ES / Grafana 聚合 |
| error | string | 否 | 错误信息（仅错误日志） |
| collector | string | 否 | 采集器名称（仅 server-probe 采集日志） |

#### 敏感字段约束

日志中禁止记录以下内容：

- Redis / Elasticsearch / Grafana 密码
- `Authorization`、`Cookie`、Token、Secret、API Key
- 用户密码、验证码、私钥
- 完整请求体和完整响应体
- 未脱敏的连接串

如果确实需要定位请求内容，只记录必要的非敏感摘要，例如请求方法、路由模板、状态码、耗时、错误类型，不记录原始凭据和原始 payload。

#### zap 字段名与 slog 字段名对比

| 含义 | zap 默认 | 本项目自定义 | 说明 |
|------|---------|-------------|------|
| 时间 | ts | ts | 自定义 UTC 毫秒格式，便于 Fluent Bit 解析 |
| 级别 | level | level | 保持 zap 默认 |
| 消息 | msg | msg | 保持 zap 默认 |
| 错误 | error | error | zap.Error() 自动使用 "error" key |

### 4.3 zap 初始化设计

#### logger 包设计

```go
package logger

import (
    "os"
    "strings"
    "time"

    "go.uber.org/zap"
    "go.uber.org/zap/zapcore"
)

type Config struct {
    Service  string
    Instance string
    Level    string
}

func Init(cfg Config) {
    level := parseLevel(cfg.Level)

    encoderConfig := zapcore.EncoderConfig{
        TimeKey:        "ts",
        LevelKey:       "level",
        NameKey:        "logger",
        CallerKey:      "caller",
        FunctionKey:    zapcore.OmitKey,
        MessageKey:     "msg",
        StacktraceKey:  "stacktrace",
        LineEnding:     zapcore.DefaultLineEnding,
        EncodeLevel:    zapcore.LowercaseLevelEncoder,
        EncodeTime:     utcMillisTimeEncoder,
        EncodeDuration: zapcore.MillisDurationEncoder,
        EncodeCaller:   zapcore.ShortCallerEncoder,
    }

    core := zapcore.NewCore(
        zapcore.NewJSONEncoder(encoderConfig),
        zapcore.AddSync(os.Stdout),
        level,
    )

    logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zap.ErrorLevel)).With(
        zap.String("service", cfg.Service),
        zap.String("instance", cfg.Instance),
    )
    zap.ReplaceGlobals(logger)
}

func parseLevel(s string) zapcore.Level {
    switch strings.ToUpper(strings.TrimSpace(s)) {
    case "DEBUG":
        return zap.DebugLevel
    case "INFO":
        return zap.InfoLevel
    case "WARN":
        return zap.WarnLevel
    case "ERROR":
        return zap.ErrorLevel
    default:
        return zap.InfoLevel
    }
}

func utcMillisTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
    enc.AppendString(t.UTC().Format("2006-01-02T15:04:05.000Z"))
}
```

**设计说明**：

1. **logger.With 默认字段**：通过 `logger.With()` 在全局 logger 上附加 `service` 和 `instance`，实现简单，避免为了默认字段自定义 `zapcore.Core`。

2. **zap.ReplaceGlobals()**：替换全局 logger，之后所有 `zap.L().Info/Error/Warn` 调用自动使用自定义 logger。

3. **UTC 毫秒时间格式**：统一输出 `2024-04-25T10:30:00.123Z`，与 Fluent Bit `app_json` parser 的 `%Y-%m-%dT%H:%M:%S.%LZ` 保持一致。

4. **zap.AddCaller()**：自动记录调用位置（文件名:行号），便于排查问题。

5. **zap.AddStacktrace(zap.ErrorLevel)**：仅在 ERROR 级别记录堆栈，避免 INFO/WARN 日志膨胀。

6. **trace_id 处理**：第二阶段不自动附加 `trace_id`，因为还没有 OTel context。第三阶段需要通过 HTTP middleware 建立 trace 上下文，并在需要关联 trace 的日志调用处使用带 context 的 logger 或封装辅助函数输出 `trace_id` / `span_id`。

### 4.4 slog → zap 迁移映射

| slog 调用 | zap 调用 | 说明 |
|-----------|---------|------|
| `slog.Info("msg", "key", value)` | `zap.L().Info("msg", zap.String("key", value))` | zap 强类型字段 |
| `slog.Error("msg", "error", err)` | `zap.L().Error("msg", zap.Error(err))` | zap.Error 自动记录 error |
| `slog.Warn("msg", "key", value)` | `zap.L().Warn("msg", zap.String("key", value))` | |
| `slog.Debug("msg", "key", value)` | `zap.L().Debug("msg", zap.String("key", value))` | |
| `slog.Info("msg", "key", intVal)` | `zap.L().Info("msg", zap.Int("key", intVal))` | zap 强类型 |
| `slog.Info("msg", "key", dur)` | `zap.L().Info("msg", zap.Duration("key", dur))` | |
| `slog.Info("msg", "key", any)` | `zap.L().Info("msg", zap.Any("key", any))` | 弱类型兜底 |

### 4.5 server-probe 日志改造

#### 新增依赖

```bash
cd server-probe && go get go.uber.org/zap
```

#### 配置新增 (config/config.go)

```go
type Config struct {
    // ... 现有字段 ...
    LogLevel string
}

func Load() Config {
    return Config{
        // ... 现有配置 ...
        LogLevel: getEnv("LOG_LEVEL", "info"),
    }
}
```

配置加载层不再直接调用 `slog.Warn` 或 `zap.L().Warn`。例如 `getHostname()` 如果失败，优先返回 `"unknown"`，由 `main()` 在 logger 初始化后按需记录，避免 logger 初始化前产生非 JSON 日志。

#### 新增文件

```
server-probe/
├── logger/
│   └── logger.go        zap 初始化 + 默认字段 + parseLevel
```

#### 主入口改造 (main.go)

```go
func main() {
    cfg := config.Load()

    logger.Init(logger.Config{
        Service:  "server-probe",
        Instance: cfg.Hostname,
        Level:    cfg.LogLevel,
    })

    // ... 其余逻辑不变，slog 调用全部替换为 zap.L() ...
}
```

#### slog → zap 替换清单（server-probe）

| 文件 | 原调用 | 替换为 |
|------|--------|--------|
| main.go | `slog.Error("apply host paths failed", "error", err)` | `zap.L().Error("apply host paths failed", zap.Error(err))` |
| main.go | `slog.Info("server-probe listening", "addr", ..., "metrics_path", ...)` | `zap.L().Info("server-probe listening", zap.String("addr", ...), zap.String("metrics_path", ...))` |
| main.go | `slog.Error("healthz response write failed", "error", err)` | `zap.L().Error("healthz response write failed", zap.Error(err))` |
| main.go | `slog.Error("readyz response write failed", "error", err)` | `zap.L().Error("readyz response write failed", zap.Error(err))` |
| main.go | `slog.Error("collector update failed", "collector", c.Name(), "error", err)` | `zap.L().Error("collector update failed", zap.String("collector", c.Name()), zap.Error(err))` |
| main.go | `slog.Info("server-probe shutting down...", "signal", sig.String())` | `zap.L().Info("server-probe shutting down...", zap.String("signal", sig.String()))` |
| main.go | `slog.Error("server-probe exited", "error", err)` | `zap.L().Error("server-probe exited", zap.Error(err))` |
| main.go | `slog.Error("server-probe shutdown error", "error", err)` | `zap.L().Error("server-probe shutdown error", zap.Error(err))` |
| main.go | `slog.Info("server-probe stopped")` | `zap.L().Info("server-probe stopped")` |
| main.go (loggingMiddleware) | `slog.Info("http request completed", ...)` | `zap.L().Info("http request completed", ..., zap.Float64("latency_ms", ...))` |
| main.go (recoveryMiddleware) | `slog.Error("http request panic recovered", ...)` | `zap.L().Error("http request panic recovered", ...)` |
| config/config.go | `slog.Warn("hostname lookup failed", "error", err)` | 移除配置加载阶段直接日志，返回 `"unknown"`，由 `main()` 初始化 logger 后按需记录 |
| collector/cpu.go | `slog.Warn("collector cpu: cpu.Percent returned empty slice")` | `zap.L().Warn("collector cpu: cpu.Percent returned empty slice")` |
| collector/disk.go | `slog.Warn("disk usage collect failed", ...)` | `zap.L().Warn("disk usage collect failed", zap.String("path", ...), zap.Error(err))` |
| collector/disk.go | `slog.Warn("disk counter reset detected", ...)` | `zap.L().Warn("disk counter reset detected", zap.String("path", ...))` |
| collector/network.go | `slog.Warn("network counter reset detected", ...)` | `zap.L().Warn("network counter reset detected", zap.String("interface", ...))` |

#### 移除 slog 导入

所有文件不再 `import "log/slog"`，替换为 `import "go.uber.org/zap"`。

### 4.6 server-web 日志改造

#### 新增依赖

```bash
cd server-web && go get go.uber.org/zap
```

#### 配置新增 (config/config.go)

```go
type Config struct {
    // ... 现有字段 ...
    LogLevel string
}

func Load() Config {
    return Config{
        // ... 现有配置 ...
        LogLevel: getEnv("LOG_LEVEL", "info"),
    }
}
```

#### 新增文件

```
server-web/
├── logger/
│   └── logger.go        zap 初始化 + 默认字段 + parseLevel
```

#### 主入口改造 (main.go)

```go
func main() {
    cfg := config.Load()

    logger.Init(logger.Config{
        Service:  "server-web",
        Instance: getHostname(),
        Level:    cfg.LogLevel,
    })

    // ... 其余逻辑不变，slog 调用全部替换为 zap.L() ...
}
```

#### 请求日志中间件改造 (api/middleware/logging.go)

```go
package middleware

import (
    "strconv"
    "sync/atomic"
    "time"

    "go.uber.org/zap"

    "github.com/gin-gonic/gin"
)

const requestIDHeader = "X-Request-ID"

var requestIDCounter uint64

func Logging() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        requestID := c.GetHeader(requestIDHeader)
        if requestID == "" {
            requestID = newRequestID(start)
        }
        c.Header(requestIDHeader, requestID)

        c.Next()

        path := c.FullPath()
        if path == "" {
            path = c.Request.URL.Path
        }

        zap.L().Info("http request",
            zap.String("request_id", requestID),
            zap.String("method", c.Request.Method),
            zap.String("path", path),
            zap.Int("status", c.Writer.Status()),
            zap.Float64("latency_ms", float64(time.Since(start).Microseconds())/1000),
            zap.String("client_ip", c.ClientIP()),
        )
    }
}

func newRequestID(now time.Time) string {
    seq := atomic.AddUint64(&requestIDCounter, 1)
    return strconv.FormatInt(now.UnixNano(), 36) + "-" + strconv.FormatUint(seq, 36)
}
```

Gin 默认的 `gin.Recovery()` 会输出非统一格式日志，server-web 迁移时需要替换为项目自己的 zap recovery middleware，确保 panic 日志也包含 `service`、`instance`、`method`、`path`、`error` 等字段。必要时同时设置 `gin.DefaultWriter` / `gin.DefaultErrorWriter`，避免 Gin 内部日志绕过 zap。

#### slog → zap 替换清单（server-web）

| 文件 | 替换要点 |
|------|---------|
| main.go | 所有 `slog.Info/Error/Warn` 替换为 `zap.L().Info/Error/Warn`，字符串值用 `zap.String`，错误用 `zap.Error` |
| api/middleware/logging.go | `slog.Info` 替换为 `zap.L().Info`，字段强类型化 |
| api/middleware/ratelimit.go | `slog.Warn` 替换为 `zap.L().Warn`，保留 key/error 上下文 |
| api/router.go | `gin.Recovery()` 替换为 zap recovery middleware |
| api/handlers/handlers.go | 所有 `slog.Info/Error/Warn` 替换 |
| pubsub/subscriber.go | 所有 `slog.Info/Error/Warn` 替换 |
| redis/client.go | 所有 `slog.Info/Error/Warn` 替换 |
| websocket/hub.go | 所有 `slog.Info/Error/Warn` 替换 |

### 4.7 logger 包目录结构

由于两个服务（server-probe 和 server-web）都需要 logger 初始化逻辑，但它们是独立的 Go Module，有两种方案：

| 方案 | 说明 | 优缺点 |
|------|------|--------|
| A. 各自复制 | 每个服务内部各放一份 logger 包 | 简单，但代码重复 |
| B. 共享包 | 创建 internal/logger 或独立 module | 无重复，但增加 module 管理复杂度 |

**选择方案 A**：两个服务的 logger 包代码量很小（约 90 行），复制一份更简单，避免引入共享 module 的管理复杂度。如果未来服务增多再考虑抽取共享包。

#### server-probe 新增文件

```
server-probe/
├── logger/
│   └── logger.go        zap 初始化 + 默认字段 + parseLevel
```

#### server-web 新增文件

```
server-web/
├── logger/
│   └── logger.go        zap 初始化 + 默认字段 + parseLevel
```

### 4.8 zap 与 slog 的关键区别

#### 强类型字段

slog 使用 `key-value` 对，key 是字符串，value 是任意类型：

```go
slog.Info("http request", "method", "GET", "status", 200)
```

zap 使用强类型字段，编译期类型检查：

```go
zap.L().Info("http request", zap.String("method", "GET"), zap.Int("status", 200))
```

**好处**：
- 编译期类型检查，不会传错类型
- 零分配，性能更高
- ES 索引时字段类型明确，不会出现同一个字段有时是 string 有时是 int 的问题

#### 全局 Logger

slog 通过 `slog.SetDefault()` 设置全局 logger：

```go
slog.SetDefault(slog.New(handler))
slog.Info("hello") // 使用全局 logger
```

zap 通过 `zap.ReplaceGlobals()` 设置全局 logger：

```go
zap.ReplaceGlobals(logger)
zap.L().Info("hello") // 使用全局 logger
```

**注意**：zap 必须通过 `zap.L()` 获取全局 logger，不能直接用包级函数（`zap.Info` 不存在）。

---

## 五、Fluent Bit 配置

### 5.1 Fluent Bit 采集流程

```
┌─────────────────────────────────────────────────────────────────┐
│ Fluent Bit Pipeline                                              │
│                                                                   │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐  │
│  │  Input   │───▶│  Parser  │───▶│  Filter  │───▶│  Output  │  │
│  │  (tail)  │    │  (JSON)  │    │  (K8s)   │    │  (ES)    │  │
│  └──────────┘    └──────────┘    └──────────┘    └──────────┘  │
│                                                                   │
│  读取容器日志     解析 JSON 日志    添加 Pod 元数据   写入 ES 索引  │
│  /var/log/        提取 level,      pod_name,        sm-logs-     │
│  containers/      msg, service     namespace,       YYYY.MM.DD  │
│                   等字段           container,                     │
│                                   node_name                     │
└─────────────────────────────────────────────────────────────────┘
```

### 5.2 K8s 环境 Fluent Bit 配置

#### Fluent Bit ConfigMap (fluent-bit.conf)

```ini
[SERVICE]
    Flush         5
    Daemon        Off
    Log_Level     info
    Parsers_File  parsers.conf
    HTTP_Server   On
    HTTP_Listen   0.0.0.0
    HTTP_Port     2020

[INPUT]
    Name              tail
    Path              /var/log/containers/*.log
    Parser            cri
    Tag               kube.*
    Mem_Buf_Limit     5MB
    Skip_Long_Lines   On
    Refresh_Interval  10

[FILTER]
    Name                kubernetes
    Match               kube.*
    Kube_URL            https://kubernetes.default.svc:443
    Kube_CA_File        /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
    Kube_Token_File     /var/run/secrets/kubernetes.io/serviceaccount/token
    Kube_Tag_Prefix     kube.var.log.containers.
    Merge_Log           On
    Merge_Parser        app_json
    Keep_Log            Off
    K8S-Logging.Parser  On
    K8S-Logging.Exclude Off

[OUTPUT]
    Name            es
    Match           *
    Host            elasticsearch
    Port            9200
    Index           sm-logs
    Logstash_Format On
    Logstash_Prefix sm-logs
    Logstash_DateFormat %Y.%m.%d
    Replace_Dots    On
    Retry_Limit     False
    tls             Off
```

#### Parsers ConfigMap (parsers.conf)

```ini
[PARSER]
    Name        cri
    Format      regex
    Regex       ^(?<time>[^ ]+) (?<stream>stdout|stderr) (?<logtag>[^ ]*) (?<log>.*)$
    Time_Key    time
    Time_Format %Y-%m-%dT%H:%M:%S.%L%z

[PARSER]
    Name   app_json
    Format json
    Time_Key ts
    Time_Format %Y-%m-%dT%H:%M:%S.%LZ
```

**说明**：Kubernetes 环境优先按 CRI 日志格式解析 `/var/log/containers/*.log`。当前多数集群使用 containerd，不能假设日志外层一定是 Docker JSON。`kubernetes` filter 开启 `Merge_Log On` 并指定 `Merge_Parser app_json` 后，会把 `log` 字段里的应用 JSON 展开到顶层，`Keep_Log Off` 避免重复保留原始 JSON 字符串。

注意 `app_json` parser 的 `Time_Key` 是 `ts`，与 zap 输出的时间字段名一致；zap 的时间编码必须保持 UTC 毫秒格式，例如 `2024-04-25T10:30:00.123Z`。第二阶段要求最终进入 ES 的应用字段是顶层字段，例如 `service`、`level`、`msg`、`request_id`，不要只落在 `log_parsed.service` 这类嵌套字段中。

### 5.3 Docker Compose 环境 Fluent Bit 配置

#### Fluent Bit ConfigMap (fluent-bit.conf)

```ini
[SERVICE]
    Flush         5
    Daemon        Off
    Log_Level     info
    Parsers_File  parsers.conf

[INPUT]
    Name              tail
    Path              /var/log/docker/*/*-json.log
    Parser            docker_json
    Tag               docker.*
    Mem_Buf_Limit     5MB
    Skip_Long_Lines   On
    Refresh_Interval  10

[FILTER]
    Name          parser
    Match         docker.*
    Key_Name      log
    Parser        app_json
    Reserve_Data  On
    Preserve_Key  Off

[OUTPUT]
    Name            es
    Match           *
    Host            elasticsearch
    Port            9200
    Index           sm-logs
    Logstash_Format On
    Logstash_Prefix sm-logs
    Logstash_DateFormat %Y.%m.%d
    Replace_Dots    On
    Retry_Limit     False
    tls             Off
```

**与 K8s 的差异**：
- Docker Compose 没有 Kubernetes Filter（无 K8s API 可调用）
- 日志路径不同：`/var/log/docker/*/*-json.log`（挂载 Docker 容器日志目录）
- 不添加 Kubernetes 元数据
- Docker Compose 先解析 Docker JSON 外层，再通过 parser filter 解析 `log` 字段中的应用 JSON

### 5.4 Fluent Bit DaemonSet 部署 (K8s)

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: fluent-bit
  namespace: monitoring
  labels:
    app: fluent-bit
spec:
  selector:
    matchLabels:
      app: fluent-bit
  template:
    metadata:
      labels:
        app: fluent-bit
    spec:
      serviceAccountName: fluent-bit
      containers:
        - name: fluent-bit
          image: fluent/fluent-bit:3.1.4
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
            limits:
              cpu: 200m
              memory: 128Mi
          volumeMounts:
            - name: var-log
              mountPath: /var/log
              readOnly: true
            - name: docker-containers
              mountPath: /var/lib/docker/containers
              readOnly: true
            - name: fluent-bit-config
              mountPath: /fluent-bit/etc/
      volumes:
        - name: var-log
          hostPath:
            path: /var/log
        - name: docker-containers
          hostPath:
            path: /var/lib/docker/containers
        - name: fluent-bit-config
          configMap:
            name: fluent-bit-config
```

#### Fluent Bit RBAC

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: fluent-bit
  namespace: monitoring
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: fluent-bit-read
rules:
  - apiGroups: [""]
    resources: ["pods", "namespaces"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: fluent-bit-read
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: fluent-bit-read
subjects:
  - kind: ServiceAccount
    name: fluent-bit
    namespace: monitoring
```

---

## 六、Elasticsearch 配置

### 6.1 索引设计

#### 索引命名

```
sm-logs-YYYY.MM.DD
```

- 前缀 `sm-logs` 代表 server-monitor-logs
- 按天分索引，便于 ILM 管理和按时间范围查询
- 使用 Logstash_Format 由 Fluent Bit 自动生成
- 第二阶段采用“日索引 + 保留期删除”的简单策略，不使用 rollover alias
- 后续日志量明显增长后，再评估 rollover、冷热分层和对象存储归档

#### 索引模板

```json
{
  "index_patterns": ["sm-logs-*"],
  "template": {
    "settings": {
      "number_of_shards": 1,
      "number_of_replicas": 0,
      "index.lifecycle.name": "sm-logs-policy"
    },
    "mappings": {
      "properties": {
        "ts": { "type": "date", "format": "strict_date_optional_time||epoch_millis" },
        "level": { "type": "keyword" },
        "msg": { "type": "text", "fields": { "keyword": { "type": "keyword", "ignore_above": 256 } } },
        "service": { "type": "keyword" },
        "instance": { "type": "keyword" },
        "trace_id": { "type": "keyword" },
        "span_id": { "type": "keyword" },
        "request_id": { "type": "keyword" },
        "method": { "type": "keyword" },
        "path": { "type": "keyword" },
        "status": { "type": "integer" },
        "latency_ms": { "type": "float" },
        "client_ip": { "type": "keyword" },
        "error": { "type": "text", "fields": { "keyword": { "type": "keyword", "ignore_above": 256 } } },
        "collector": { "type": "keyword" },
        "caller": { "type": "keyword" },
        "stacktrace": { "type": "text" },
        "kubernetes": {
          "properties": {
            "pod_name": { "type": "keyword" },
            "namespace_name": { "type": "keyword" },
            "container_name": { "type": "keyword" },
            "host": { "type": "keyword" },
            "labels": { "type": "object", "enabled": true }
          }
        },
        "stream": { "type": "keyword" }
      }
    }
  }
}
```

### 6.2 ILM 策略

```json
{
  "sm-logs-policy": {
    "phases": {
      "hot": {
        "min_age": "0ms",
        "actions": {
          "set_priority": {
            "priority": 100
          }
        }
      },
      "delete": {
        "min_age": "30d",
        "actions": {
          "delete": {}
        }
      }
    }
  }
}
```

**说明**：第二阶段日志量可控，ILM 只负责保留期删除，避免日索引和 rollover alias 混用。若后续需要按索引大小 rollover，应改为 `sm-logs-write` 写入别名，并关闭 Fluent Bit 的日索引命名。

### 6.3 Elasticsearch 部署

#### K8s StatefulSet

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: elasticsearch
  namespace: monitoring
spec:
  serviceName: elasticsearch
  replicas: 1
  selector:
    matchLabels:
      app: elasticsearch
  template:
    metadata:
      labels:
        app: elasticsearch
    spec:
      containers:
        - name: elasticsearch
          image: docker.elastic.co/elasticsearch/elasticsearch:8.13.0
          ports:
            - containerPort: 9200
          env:
            - name: discovery.type
              value: single-node
            - name: xpack.security.enabled
              value: "false"
            - name: ES_JAVA_OPTS
              value: "-Xms512m -Xmx512m"
            - name: cluster.name
              value: sm-logs
          resources:
            requests:
              cpu: 500m
              memory: 1Gi
            limits:
              cpu: "1"
              memory: 2Gi
          volumeMounts:
            - name: data
              mountPath: /usr/share/elasticsearch/data
          readinessProbe:
            httpGet:
              path: /_cluster/health?local=true
              port: 9200
            initialDelaySeconds: 30
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /_cluster/health?local=true
              port: 9200
            initialDelaySeconds: 60
            periodSeconds: 20
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 10Gi
```

**注意**：开发环境使用单节点 + 关闭安全认证（`xpack.security.enabled=false`），生产环境应开启安全认证并配置集群。

---

## 七、Kibana 配置

### 7.1 Kibana 部署

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kibana
  namespace: monitoring
spec:
  replicas: 1
  selector:
    matchLabels:
      app: kibana
  template:
    metadata:
      labels:
        app: kibana
    spec:
      containers:
        - name: kibana
          image: docker.elastic.co/kibana/kibana:8.13.0
          ports:
            - containerPort: 5601
          env:
            - name: ELASTICSEARCH_HOSTS
              value: "http://elasticsearch:9200"
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
            limits:
              cpu: 500m
              memory: 512Mi
          readinessProbe:
            httpGet:
              path: /api/status
              port: 5601
            initialDelaySeconds: 30
            periodSeconds: 10
```

### 7.2 Kibana Data View

第二阶段先要求给出可复现的 Data View 创建步骤，不把 Kibana Dashboard 自动导入作为硬性完成项。原因是 Saved Objects API 需要 Kibana 完全启动且版本敏感，容易让日志链路主线被 UI 导入问题阻塞。

Kibana 启动后需要手动或通过 API 创建以下配置：

#### 索引模式（Index Pattern）

```
名称: sm-logs-*
时间字段: ts
```

#### 可选 Dashboard

| Dashboard | 内容 |
|-----------|------|
| 服务日志概览 | 各服务日志量趋势、错误率趋势、日志级别分布 |
| server-web 日志 | HTTP 请求日志、错误日志、慢请求 |
| server-probe 日志 | 采集器错误日志、采集失败统计 |

**说明**：Kibana 的 Dashboard 预配置可以通过 Kibana Saved Objects API 导入，但为简化第二阶段实现，先手动在 Kibana 界面创建；本阶段验收以 Data View 可查询日志为准，Dashboard 自动化导入留到后续独立小模块。

---

## 八、Grafana 联动配置

### 8.1 新增 Elasticsearch 数据源

#### Grafana Provisioning Datasources ConfigMap

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

### 8.2 指标 + 日志联动查询

Grafana Dashboard 面板支持从 Prometheus 数据源切换到 Elasticsearch 数据源，实现：

1. **指标异常 → 查看对应时间窗口日志**：在 Grafana 面板中，点击指标图表的时间点，可以跳转到 Elasticsearch 日志查询
2. **日志面板嵌入 Dashboard**：在 Grafana Dashboard 中添加 Logs 面板，直接展示与指标同时间窗口的日志
3. **trace_id 关联**：第三阶段链路追踪完成后，可通过 trace_id 在日志和 Trace 之间跳转

### 8.3 Grafana Dashboard 新增面板

在现有 Dashboard 基础上新增：

| Dashboard | 新增面板 | 数据源 |
|-----------|---------|--------|
| 服务监控 | 日志量趋势 | Elasticsearch |
| 服务监控 | 错误日志列表 | Elasticsearch |
| 主机概览 | 主机相关日志 | Elasticsearch |

---

## 九、Docker Compose 改造

### 9.1 新增服务

```yaml
services:
  # ... 现有服务不变 ...

  # ------------------------------------------
  # Elasticsearch 日志存储
  # ------------------------------------------
  elasticsearch:
    image: docker.elastic.co/elasticsearch/elasticsearch:8.13.0
    restart: unless-stopped
    environment:
      discovery.type: single-node
      xpack.security.enabled: "false"
      ES_JAVA_OPTS: "-Xms512m -Xmx512m"
      cluster.name: sm-logs
    ports:
      - "127.0.0.1:9200:9200"
    volumes:
      - elasticsearch-data:/usr/share/elasticsearch/data
    healthcheck:
      test: ["CMD-SHELL", "curl -sf http://127.0.0.1:9200/_cluster/health || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 20
      start_period: 30s
    deploy:
      resources:
        limits:
          cpus: "1.00"
          memory: 2G

  # ------------------------------------------
  # Kibana 日志查询
  # ------------------------------------------
  kibana:
    image: docker.elastic.co/kibana/kibana:8.13.0
    restart: unless-stopped
    environment:
      ELASTICSEARCH_HOSTS: "http://elasticsearch:9200"
    ports:
      - "127.0.0.1:5601:5601"
    depends_on:
      elasticsearch:
        condition: service_healthy
    healthcheck:
      test: ["CMD-SHELL", "curl -sf http://127.0.0.1:5601/api/status || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 20
      start_period: 30s
    deploy:
      resources:
        limits:
          cpus: "0.50"
          memory: 512M

  # ------------------------------------------
  # Fluent Bit 日志采集
  # ------------------------------------------
  fluent-bit:
    image: fluent/fluent-bit:3.1.4
    restart: unless-stopped
    volumes:
      - ./docker/fluent-bit/fluent-bit.conf:/fluent-bit/etc/fluent-bit.conf:ro
      - ./docker/fluent-bit/parsers.conf:/fluent-bit/etc/parsers.conf:ro
      - /var/lib/docker/containers:/var/log/docker:ro
    depends_on:
      elasticsearch:
        condition: service_healthy
    deploy:
      resources:
        limits:
          cpus: "0.20"
          memory: 128M

volumes:
  # ... 现有 volumes ...
  elasticsearch-data:
```

### 9.2 现有服务环境变量新增

```yaml
services:
  server-probe:
    environment:
      LOG_LEVEL: info

  server-web:
    environment:
      LOG_LEVEL: info
```

### 9.3 Fluent Bit 配置文件

#### docker/fluent-bit/fluent-bit.conf

```ini
[SERVICE]
    Flush         5
    Daemon        Off
    Log_Level     info
    Parsers_File  parsers.conf

[INPUT]
    Name              tail
    Path              /var/log/docker/*/*-json.log
    Parser            docker_json
    Tag               docker.*
    Mem_Buf_Limit     5MB
    Skip_Long_Lines   On
    Refresh_Interval  10

[FILTER]
    Name          parser
    Match         docker.*
    Key_Name      log
    Parser        app_json
    Reserve_Data  On
    Preserve_Key  Off

[OUTPUT]
    Name            es
    Match           *
    Host            elasticsearch
    Port            9200
    Logstash_Format On
    Logstash_Prefix sm-logs
    Logstash_DateFormat %Y.%m.%d
    Replace_Dots    On
    Retry_Limit     False
    tls             Off
```

#### docker/fluent-bit/parsers.conf

```ini
[PARSER]
    Name   docker_json
    Format json
    Time_Key time
    Time_Format %Y-%m-%dT%H:%M:%S.%LZ

[PARSER]
    Name   app_json
    Format json
    Time_Key ts
    Time_Format %Y-%m-%dT%H:%M:%S.%LZ
```

---

## 十、Helm Chart 改造

### 10.1 新增模板文件

```
charts/server-monitor/
├── templates/
│   ├── ... 现有文件不变 ...
│   ├── elasticsearch/
│   │   ├── statefulset.yaml       Elasticsearch StatefulSet
│   │   ├── service.yaml           Elasticsearch Service
│   │   └── configmap.yaml         索引模板 + ILM 策略初始化
│   ├── kibana/
│   │   ├── deployment.yaml        Kibana Deployment
│   │   └── service.yaml           Kibana Service
│   └── fluent-bit/
│       ├── daemonset.yaml         Fluent Bit DaemonSet
│       ├── serviceaccount.yaml    ServiceAccount
│       ├── clusterrole.yaml       ClusterRole
│       ├── clusterrolebinding.yaml ClusterRoleBinding
│       ├── configmap.yaml         Fluent Bit 配置
│       └── parsers-configmap.yaml Parsers 配置
```

### 10.2 values.yaml 新增配置

```yaml
config:
  # ... 现有配置 ...
  logLevel: info

elasticsearch:
  enabled: true
  image: docker.elastic.co/elasticsearch/elasticsearch:8.13.0
  javaOpts: "-Xms512m -Xmx512m"
  clusterName: sm-logs
  securityEnabled: false
  persistence:
    enabled: true
    accessModes:
      - ReadWriteOnce
    size: 10Gi
    storageClassName: ""
  service:
    type: ClusterIP
    port: 9200
  resources:
    requests:
      cpu: 500m
      memory: 1Gi
    limits:
      cpu: "1"
      memory: 2Gi
  ilm:
    deleteAge: 30d

kibana:
  enabled: true
  image: docker.elastic.co/kibana/kibana:8.13.0
  service:
    type: NodePort
    port: 5601
    nodePort: 30061
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      cpu: 500m
      memory: 512Mi

fluentBit:
  enabled: true
  image: fluent/fluent-bit:3.1.4
  resources:
    requests:
      cpu: 50m
      memory: 32Mi
    limits:
      cpu: 200m
      memory: 128Mi
```

### 10.3 ConfigMap 新增环境变量

```yaml
data:
  # ... 现有配置 ...
  LOG_LEVEL: {{ .Values.config.logLevel | default "info" | quote }}
```

### 10.4 Grafana 数据源 Provisioning 更新

更新 Grafana datasources ConfigMap，新增 Elasticsearch 数据源：

```yaml
grafana:
  provisioning:
    datasources:
      - name: Prometheus
        type: prometheus
        access: proxy
        url: http://prometheus:9090
        isDefault: true
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
```

---

## 十一、CI/CD 改造

### 11.1 GitHub Actions 新增检查

建议在现有 CI 基础上补充轻量检查，而不是引入复杂新工具：

现有 CI 步骤已覆盖：
- Go 代码检查（goimports + go test + go vet）
- Docker 构建检查
- Helm lint
- Prometheus 配置检查

第二阶段建议新增或确认：

- `docker compose config`：校验 Compose YAML 语法和服务引用
- `helm lint charts/server-monitor`：覆盖新增 EFK Helm 模板
- `jq -e` 或等价 JSON 校验：校验 Elasticsearch index template / ILM policy JSON
- 如本地或 CI 镜像包含 Fluent Bit，可执行 `fluent-bit --dry-run -c <conf>` 校验配置；没有该二进制时需说明未执行原因

### 11.2 Docker 构建影响

第二阶段不新增自研服务镜像类型，但 `server-probe` 和 `server-web` 因为新增 zap 依赖和日志代码，仍需要构建现有镜像验证。Elasticsearch、Kibana、Fluent Bit 使用官方镜像，无需在 CI 中重新构建。

### 11.3 go.mod 变更

两个 Go 服务的 go.mod 新增依赖：

```
go.uber.org/zap v1.27.0
```

CI 中的 `go mod download` 会自动下载，无需额外配置。

---

## 十二、实施步骤

| 步骤 | 内容 | 验证标准 |
|------|------|---------|
| 1 | server-probe 新增 logger 包、LOG_LEVEL 配置，并消除 config.Load 阶段的非统一日志 | `LOG_LEVEL=debug` 可输出 DEBUG 日志，启动日志为 JSON |
| 2 | server-probe 所有 slog 调用替换为 zap.L()，HTTP 请求耗时改为 `latency_ms` 数字 | 启动后日志输出 JSON 格式，包含 service/instance 字段 |
| 3 | server-probe 移除 slog 导入，go.mod 新增 zap 依赖 | `goimports`、`go test ./...`、`go vet ./...` 通过 |
| 4 | server-web 新增 logger 包、LOG_LEVEL 配置，并在 main 入口最早初始化 logger | `LOG_LEVEL=debug` 可输出 DEBUG 日志，启动日志为 JSON |
| 5 | server-web 所有 slog 调用替换为 zap.L()，包含 handlers、redis、pubsub、websocket、ratelimit | 启动后日志输出 JSON 格式，包含 service/instance/request_id 字段 |
| 6 | server-web 替换 Gin 默认 recovery / writer，确保 panic 和 Gin 内部错误日志也走 zap | panic 场景输出 JSON 错误日志，现有 API 响应行为不变 |
| 7 | server-web 移除 slog 导入，go.mod 新增 zap 依赖 | `goimports`、`go test ./...`、`go vet ./...` 通过 |
| 8 | Docker Compose 新增 Elasticsearch + Kibana + Fluent Bit | `docker compose config` 通过，ES 健康检查通过 |
| 9 | Docker Compose 现有服务新增 LOG_LEVEL 环境变量 | 服务日志输出 JSON 格式 |
| 10 | Fluent Bit 配置文件（Docker Compose 版本） | Fluent Bit 成功采集容器日志并写入 ES |
| 11 | Kibana 创建 Data View `sm-logs-*` | Kibana Discover 页面可查看日志 |
| 12 | Helm Chart 新增 Elasticsearch StatefulSet + Service | `helm lint` 通过，部署后 ES 正常运行 |
| 13 | Helm Chart 新增 Kibana Deployment + Service | Kibana 可访问 |
| 14 | Helm Chart 新增 Fluent Bit DaemonSet + RBAC + ConfigMap | Fluent Bit 成功采集 Pod 日志，应用字段展开到顶层 |
| 15 | Helm Chart values.yaml 新增 ES/Kibana/FluentBit/LogLevel 配置 | `helm lint` 通过 |
| 16 | Grafana Provisioning 新增 ES 数据源 | Grafana 可查询 ES 日志 |
| 17 | Grafana Dashboard 新增日志面板 | Dashboard 展示日志量趋势和错误日志 |
| 18 | Elasticsearch 索引模板 + ILM delete 策略初始化 | 日志索引按天创建，30 天后自动删除 |
| 19 | 端到端验证 | 产生日志 → Fluent Bit 采集 → ES 存储 → Kibana/Grafana 查询 |

---

## 十三、验收标准

### 功能验收

- [ ] server-probe 日志输出 JSON 格式，包含 service=server-probe、instance、level 字段
- [ ] server-web 日志输出 JSON 格式，包含 service=server-web、instance、level、request_id 字段
- [ ] ES 索引模板预留 trace_id、span_id 字段，但第二阶段不要求输出真实 trace
- [ ] LOG_LEVEL 环境变量可控制日志级别（debug/info/warn/error）
- [ ] ERROR 级别日志自动记录堆栈信息
- [ ] 所有日志自动记录调用位置（caller 字段）
- [ ] Fluent Bit 以 DaemonSet 部署，每个 Node 一个 Pod
- [ ] Fluent Bit 成功采集所有 Pod 的 JSON 日志
- [ ] Fluent Bit 为日志添加 Kubernetes 元数据（pod_name、namespace、container、node）
- [ ] Elasticsearch 成功接收并索引日志
- [ ] 日志索引按天创建（sm-logs-YYYY.MM.DD）
- [ ] ILM 策略生效，30 天后自动删除旧索引
- [ ] Kibana 可查询和过滤日志
- [ ] Kibana 可按 service、level、instance 字段过滤
- [ ] Grafana 关联 ES 数据源，Dashboard 可展示日志
- [ ] Docker Compose 环境包含完整 EFK 栈

### 端到端验收用例

- [ ] 访问 `server-web /healthz` 后，Kibana 能查到 `service=server-web`、`path=/healthz` 的 HTTP 请求日志
- [ ] 访问一个不存在的 API 路径后，Kibana 能查到对应 `status=404` 的请求日志
- [ ] 人为触发一个错误场景后，Kibana 能按 `level=error` 或 `level=warn` 过滤到日志
- [ ] `LOG_LEVEL=warn` 时，普通 `info` 请求日志不输出；`LOG_LEVEL=debug` 时，debug 日志可输出
- [ ] Docker Compose 环境中，应用日志从 stdout → Fluent Bit → Elasticsearch → Kibana 查询全链路可用
- [ ] K8s 环境中，应用日志包含 Kubernetes 元数据，例如 namespace、pod、container、node
- [ ] Grafana 日志面板能按当前时间窗口展示 `sm-logs-*` 日志
- [ ] 停止 Fluent Bit 或 Elasticsearch 后，server-web / server-probe 业务接口仍正常响应

### 非功能验收

- [ ] Fluent Bit 内存占用 < 128MB
- [ ] Elasticsearch 单节点内存占用 < 2GB
- [ ] Kibana 内存占用 < 512MB
- [ ] 日志采集延迟 < 10 秒（从服务输出到 ES 可查询）
- [ ] zap 零分配特性不影响服务性能
- [ ] 现有功能（指标采集、告警推送、WebSocket）不受影响

### 兼容性验收

- [ ] 第一阶段所有功能正常（指标采集、Grafana 大盘、告警闭环、WebSocket 推送）
- [ ] Docker Compose `docker compose up` 全栈启动正常
- [ ] Helm Chart `helm upgrade` 升级不影响现有服务
- [ ] 现有 API 接口行为不变
- [ ] 现有 Prometheus 告警规则不受影响

---

## 十四、风险与注意事项

### 14.1 Elasticsearch 资源占用

Elasticsearch 是资源消耗较大的组件，单节点开发环境建议至少分配 2GB 内存。如果本地开发机器资源不足，可以：
- 降低 ES_JAVA_OPTS 到 `-Xms256m -Xmx256m`
- 使用 `docker compose up elasticsearch kibana` 单独启动日志组件，不启动全栈

### 14.2 Docker Compose 日志采集限制

Docker Compose 环境下 Fluent Bit 通过挂载 `/var/lib/docker/containers` 采集日志，需要：
- Docker 守护进程使用默认的 json-file log driver
- 挂载路径有读取权限
- 在某些 Docker Desktop 环境（macOS/Windows）中，该路径可能不可用

**替代方案**：如果挂载路径不可用，可以在 Docker Compose 中使用 Docker `fluentd` log driver，将日志直接发送到 Fluent Bit 的 forward 端口：

```yaml
services:
  server-web:
    logging:
      driver: fluentd
      options:
        fluentd-address: localhost:24224
        tag: docker.server-web
```

### 14.3 Elasticsearch 安全配置

开发环境关闭了 `xpack.security.enabled`，生产环境必须：
- 开启安全认证
- 配置用户名密码
- 使用 Secret 存储 ES 凭据
- Fluent Bit 和 Kibana 配置认证信息

### 14.4 日志量控制

监控系统的日志量可能较大，需注意：
- Fluent Bit 配置 `Mem_Buf_Limit` 防止内存溢出
- ILM 策略控制日志保留时间
- 避免在高频路径输出 DEBUG 日志
- 生产环境 LOG_LEVEL 设为 info

### 14.5 日志链路故障降级

日志链路不能影响业务服务可用性：

- 应用只写 stdout/stderr，不直接依赖 Fluent Bit 或 Elasticsearch
- Fluent Bit 异常时，应用继续运行，只是日志无法进入 ES
- Elasticsearch 异常时，Fluent Bit 按重试策略发送；缓冲耗尽后允许丢弃日志，不能反压业务服务
- Docker Compose / Helm 文档中需要说明日志链路异常的排查入口：Fluent Bit 日志、ES `_cluster/health`、Kibana 状态页
- `Retry_Limit False` 表示无限重试，生产环境需结合内存/文件缓冲配置评估，避免 ES 长时间不可用时 Fluent Bit 资源膨胀

### 14.6 敏感信息与字段基数

日志字段需要控制安全风险和索引成本：

- 不记录密码、Token、Cookie、Secret、完整请求体
- 不把 request_id、trace_id、用户 ID 等高基数字段用于 Prometheus label
- ES 中 `request_id`、`trace_id` 可以作为 keyword 精确查询，但不要用于大规模 terms 聚合
- Kubernetes labels 可以保留为 object，但不要把不可控高基数 label 展开成大量固定字段

### 14.7 trace_id 预留

第二阶段不要求每条日志输出空 `trace_id`。更推荐：

- ES 索引模板预留 `trace_id`、`span_id` 字段
- HTTP 请求日志先输出 `request_id`
- 第三阶段引入 OpenTelemetry 后，再从 request context 中输出真实 `trace_id` / `span_id`
- 需要修改 HTTP middleware 和日志封装，让业务代码在有 context 的地方输出 trace 信息

### 14.8 slog → zap 迁移注意事项

- zap 字段是强类型的，必须使用 `zap.String()` / `zap.Int()` / `zap.Error()` 等方法
- zap 没有 `slog.Warn` 对应的包级函数，必须通过 `zap.L().Warn()` 调用
- zap 的 `zap.Error()` 会自动将 error 转为 `{"error": "message"}` 格式
- zap 的 `zap.Duration()` 默认输出受 encoder 配置影响；HTTP 请求耗时统一输出 `latency_ms` 数字，方便 ES / Grafana 聚合
- 确保所有 `log/slog` 导入被移除，避免混用两个日志库

---

## 十五、第三阶段 zap → OTel 集成预览

第二阶段使用 zap 输出 JSON 日志，第三阶段引入 OpenTelemetry 时，不应只理解为“替换 logger Core”。真实 trace 关联至少需要：

- HTTP middleware 创建 / 提取 trace context
- 出站请求继续传播 trace context
- 日志封装能从 `context.Context` 中读取 `trace_id`、`span_id`
- 需要关联 trace 的业务日志使用带 context 的日志入口

```go
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

**关键**：第二阶段只保证日志字段和 ES mapping 对第三阶段友好。第三阶段可以评估 `otelzap` 或自定义 `FromContext(ctx)` 封装，但必须以真实 context 传播为前提，不能只输出空字符串冒充 trace。

---

## 十六、简历表述建议

### 第二阶段新增亮点

```
5. 基于 Fluent Bit DaemonSet 采集 Kubernetes Pod 日志，
   所有服务使用 zap 输出结构化 JSON 日志（零分配高性能），
   通过 Elasticsearch 索引存储，Kibana 可视化查询，
   并在 Grafana 中关联 ES 数据源实现指标 + 日志联动查询，
   在日志索引中预留 trace_id/span_id 字段为链路追踪做准备。
```

### 完整简历亮点（第一 + 第二阶段）

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
```
