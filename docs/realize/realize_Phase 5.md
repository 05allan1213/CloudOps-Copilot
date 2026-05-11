# CloudOps Copilot Phase 5 实施方案

> 方案版本：v1.0
> 制定日期：2026-05-11
> 依据文档：`docs/design.md` v3.1
> 阶段定位：异步诊断 Worker，在 Phase 3 诊断报告和 Phase 4 Runbook 检索基础上，消费 Kafka `alert-events`，对 `firing` 告警自动触发诊断，并通过 WebSocket 推送诊断状态。

---

## 1. 阶段目标

Phase 5 的目标是把“用户手动触发诊断”扩展为“告警事件自动触发诊断”：Alertmanager Webhook 仍保持快速返回，`server-web` 继续把告警写入 Redis、MySQL、Kafka 和 WebSocket；新增 Diagnosis Worker 后台消费 Kafka `alert-events`，只处理 `firing` 告警，经过 Redis 幂等去重后调用现有 Diagnosis Pipeline，生成并持久化 `DiagnosisReport`，最后推送诊断状态给前端。

本阶段坚持嵌入式实现：Diagnosis Worker 与 `server-web` 同进程运行，复用现有 Redis、MySQL、Prometheus、Tool Registry、Runbook Retriever、LLM Client、WebSocket Hub 和日志/Trace 能力。不新增独立 worker 服务容器，不把 LLM 调用放进 Alertmanager Webhook 同步路径，不引入新的消息队列或调度框架。

### 1.1 核心交付物

| 交付物 | 内容 | 验收标准 |
|---|---|---|
| Worker 配置 | `DIAGNOSIS_ENABLED`、`DIAGNOSIS_WORKER_COUNT`、`DIAGNOSIS_KAFKA_GROUP_ID`、去重 TTL、任务超时等配置 | 默认关闭自动诊断；开启后配置校验清晰，非法值启动失败 |
| Kafka Consumer | `server-web` 内新增 `DiagnosisConsumer`，消费 `alert-events` | 可消费 `server-web/kafka.AlertEvent`，非法 JSON 提交 offset，临时错误不提交 offset |
| 自动诊断触发器 | `DiagnosisWorker` 过滤 `firing`、解析告警上下文、调用 `diagnosis.Service` | `resolved` 不触发诊断；`firing` 可生成 `trigger_type=auto` 的诊断报告 |
| Redis 幂等去重 | 使用 `diagnosis:task:<fingerprint>` 或等价 key 防止重复诊断 | 同一 fingerprint 在 TTL 内只触发一次自动诊断；进行中和已完成均可识别 |
| 状态推送 | 复用 `/ws/alerts` 的 WebSocket Hub 推送 `diagnosis_update` | 前端能收到 pending/running/completed/failed 状态消息 |
| 前端状态展示 | 告警列表/诊断入口展示自动诊断状态，诊断列表支持自动诊断结果 | 告警触发后无需手动点击，也可看到诊断进度和报告入口 |
| 观测与降级 | Worker 日志、指标、健康状态和错误分类 | Kafka/LLM/MySQL/Redis 异常时不阻塞 Webhook，诊断失败可追踪 |
| 验证闭环 | 单元测试、集成测试、前端构建、Compose 联调 | Alertmanager 告警后异步生成诊断，Webhook 响应不被 LLM 阻塞 |

### 1.2 本阶段不做

1. 不新增 `diagnosis-events` Kafka Topic；状态推送先复用当前 WebSocket Hub。
2. 不新增独立 Diagnosis Worker 容器；Worker 嵌入 `server-web` 生命周期。
3. 不创建 PendingAction，不接入动作审批、审计和执行；这些属于 Phase 6。
4. 不新增 Kubernetes 只读或写操作工具；这些属于 Phase 7。
5. 不改变 Alertmanager Webhook 的外部响应格式。
6. 不改变 `diagnosis_reports` 表的核心字段结构；仅允许补充 `trigger_type=auto` 常量和查询过滤能力。
7. 不对 `resolved` 告警自动诊断；`resolved` 只用于后续状态补充或人工查询。
8. 不依赖真实外部 LLM 才能完成验收；必须支持 Mock LLM 和 rule-only 降级验证。

---

## 2. 当前基础与前置条件

### 2.1 当前已具备能力

| 能力 | 当前落点 | Phase 5 复用方式 |
|---|---|---|
| 告警 Webhook | `server-monitor/server-web/webhook/alertmanager.go`、`alert/service.go`、`api/handlers` | Webhook 继续快速处理并发送 Kafka `alert-events` |
| Kafka Producer | `server-monitor/server-web/kafka/producer.go` | 复用 `AlertEvent` 结构、`TopicAlertEvents` 常量和 fingerprint 作为消息 key |
| Kafka Consumer 模式 | `server-monitor/alert-service/kafka/consumer.go` | 参考 ConsumerGroup、offset 提交、永久错误分类、panic recovery 模式 |
| Diagnosis Pipeline | `server-monitor/server-web/copilot/diagnosis` | Worker 调用现有 `Service.Trigger`，新增 `TriggerAuto` 请求类型 |
| Runbook 检索 | `server-monitor/server-web/copilot/runbook`、`copilot/tool/runbook_tool.go` | 自动诊断同样采集 Runbook 证据 |
| WebSocket Hub | `server-monitor/server-web/websocket/hub.go`、`main.go` | 推送 `diagnosis_update` 类型消息 |
| 配置体系 | `server-monitor/server-web/config/config.go` | 新增 Diagnosis Worker 配置并纳入 `Validate()` |
| 部署体系 | `docker-compose.yml`、`k8s/`、`charts/server-monitor/` | 增加同名环境变量，保持 Compose、原生 K8s、Helm 一致 |

### 2.2 前置假设

1. Phase 3 的 `DiagnosisReport`、`POST /api/v1/diagnosis`、`diagnosis.Service` 已可用。
2. Phase 4 的 `runbook.search` 已接入 EvidenceCollector，自动诊断可复用同一证据链。
3. `KAFKA_BROKERS` 为空时 Kafka 能力禁用；此时自动诊断必须禁用或启动时给出清晰日志。
4. `server-web` 可访问 Redis、MySQL、Prometheus 和 Kafka；真实 LLM 不可用时仍能 rule-only 完成诊断报告。
5. 前端已经有诊断列表和详情页，Phase 5 只补充自动诊断状态感知，不重做诊断 UI。

### 2.3 阶段边界

Phase 5 只改变告警事件后的异步诊断链路。Alertmanager Webhook 的职责仍是接收、校验、落 Redis/MySQL、发 Kafka 和推送告警事件；Webhook handler 不等待 Kafka 消费、不等待 Diagnosis Worker、不调用 LLM、不等待 MySQL 写入诊断报告。

---

## 3. 总体实施路径

Phase 5 拆为 8 个模块推进，每个模块都能独立验证。

```text
模块 1：配置与启动边界
  ↓
模块 2：Kafka Diagnosis Consumer
  ↓
模块 3：自动诊断请求归一化
  ↓
模块 4：Redis 幂等去重与任务状态
  ↓
模块 5：Diagnosis Worker 主流程
  ↓
模块 6：WebSocket 诊断状态推送
  ↓
模块 7：前端自动诊断状态展示
  ↓
模块 8：部署、联调、回归与验收
```

---

## 4. 文件规划

### 4.1 后端新增文件

| 文件 | 职责 |
|---|---|
| `server-monitor/server-web/copilot/diagnosis/worker.go` | Diagnosis Worker 主流程：过滤事件、去重、触发诊断、推送状态 |
| `server-monitor/server-web/copilot/diagnosis/worker_test.go` | 覆盖 `firing/resolved` 过滤、重复事件、成功/失败状态 |
| `server-monitor/server-web/copilot/diagnosis/dedupe.go` | Redis 任务幂等接口与 key 生成 |
| `server-monitor/server-web/copilot/diagnosis/dedupe_test.go` | 覆盖 key、TTL、已存在、失败释放策略 |
| `server-monitor/server-web/copilot/diagnosis/notifier.go` | WebSocket 状态消息结构和推送接口 |
| `server-monitor/server-web/copilot/diagnosis/notifier_test.go` | 覆盖消息类型、payload 字段、hub 失败降级 |
| `server-monitor/server-web/kafka/consumer.go` | `server-web` 侧 Kafka ConsumerGroup 实现 |
| `server-monitor/server-web/kafka/consumer_test.go` | 参考 `alert-service` 覆盖 JSON 错误、永久错误、offset 提交 |

### 4.2 后端修改文件

| 文件 | 修改内容 |
|---|---|
| `server-monitor/server-web/config/config.go` | 新增自动诊断配置、默认值和校验 |
| `server-monitor/server-web/copilot/diagnosis/types.go` | 新增 `TriggerAuto` 常量、Worker 事件/状态类型 |
| `server-monitor/server-web/copilot/diagnosis/request.go` | 允许 `trigger_type=auto`，保持 manual/chat 兼容 |
| `server-monitor/server-web/copilot/diagnosis/service.go` | 为 Worker 提供可复用触发入口；必要时返回已存在报告信息 |
| `server-monitor/server-web/copilot/diagnosis/repository.go` | 增加按 fingerprint/status/trigger_type 查询最近诊断的方法 |
| `server-monitor/server-web/api/router.go` | 返回可供 main 启动 Worker 使用的依赖，或抽出 Copilot 初始化工厂 |
| `server-monitor/server-web/main.go` | 初始化 Diagnosis Worker、随应用启动/关闭，并纳入 graceful shutdown |
| `server-monitor/server-web/api/middleware/metrics.go` | 增加 worker 消费、跳过、成功、失败、耗时等 Prometheus 指标 |
| `server-monitor/server-web/docs/swagger.yaml` / `docs.go` / `swagger.json` | 如果新增前端可见查询参数或状态字段，更新 API 文档 |

### 4.3 前端修改文件

| 文件 | 修改内容 |
|---|---|
| `server-monitor/frontend/src/api/diagnosis.ts` | 增加 `trigger_type=auto`、状态字段类型兼容 |
| `server-monitor/frontend/src/pages/DiagnosisListPage.vue` | 增加自动诊断来源标识和状态筛选 |
| `server-monitor/frontend/src/pages/DiagnosisDetailPage.vue` | 展示自动触发来源、告警 fingerprint 和状态更新时间 |
| `server-monitor/frontend/src/stores/monitor.ts` 或当前 WebSocket 入口 | 识别 `diagnosis_update` 消息，更新告警/诊断状态 |
| `server-monitor/frontend/src/api/alerts.ts` | 如告警列表需要展示诊断状态，补充类型字段或本地状态映射 |

### 4.4 部署与配置文件

| 文件 | 修改内容 |
|---|---|
| `server-monitor/docker-compose.yml` | 为 `server-web` 增加 Diagnosis Worker 环境变量 |
| `server-monitor/k8s/configmap.yaml` | 增加同名配置 |
| `server-monitor/k8s/web.yaml` | 注入新增配置 |
| `server-monitor/charts/server-monitor/values.yaml` | 增加 Helm values 默认值 |
| `server-monitor/charts/server-monitor/templates/configmap.yaml` | 输出新增配置 |
| `server-monitor/charts/server-monitor/templates/server-web.yaml` | 注入新增配置 |

---

## 5. 配置设计

### 5.1 新增环境变量

| 环境变量 | 默认值 | 类型 | 说明 | 敏感 |
|---|---:|---|---|---|
| `DIAGNOSIS_ENABLED` | `false` | bool | 是否启用自动诊断 Worker | 否 |
| `DIAGNOSIS_WORKER_COUNT` | `1` | int | 同进程 worker 并发数量 | 否 |
| `DIAGNOSIS_KAFKA_GROUP_ID` | `diagnosis-worker` | string | Kafka Consumer Group ID | 否 |
| `DIAGNOSIS_TASK_TTL_SECONDS` | `1800` | duration | Redis 去重任务 TTL，默认 30 分钟 | 否 |
| `DIAGNOSIS_TASK_TIMEOUT_SECONDS` | `120` | duration | 单次自动诊断总超时 | 否 |
| `DIAGNOSIS_RETRYABLE_ERRORS` | `true` | bool | 临时错误是否不提交 Kafka offset | 否 |
| `DIAGNOSIS_STATUS_PUSH_ENABLED` | `true` | bool | 是否推送 `diagnosis_update` WebSocket 消息 | 否 |

### 5.2 配置校验规则

1. `DIAGNOSIS_ENABLED=true` 时，`COPILOT_ENABLED` 必须为 `true`，否则启动失败。
2. `DIAGNOSIS_ENABLED=true` 时，`KAFKA_BROKERS` 不能为空，否则启动失败。
3. `DIAGNOSIS_WORKER_COUNT` 必须在 `[1, 8]`；默认单 worker，避免 LLM 和 Prometheus 被并发打满。
4. `DIAGNOSIS_TASK_TTL_SECONDS` 必须大于 `DIAGNOSIS_TASK_TIMEOUT_SECONDS`。
5. `DIAGNOSIS_TASK_TIMEOUT_SECONDS` 必须大于 `DIAGNOSIS_LLM_TIMEOUT_SECONDS`。
6. `DIAGNOSIS_KAFKA_GROUP_ID` 不能为空，且不同环境可通过配置隔离。

### 5.3 默认关闭策略

自动诊断默认关闭，原因是它会引入 Kafka 消费、LLM 调用和后台任务生命周期。Phase 5 实施完成后，本地 Compose 可以通过 `.env` 或环境变量显式开启：

```bash
DIAGNOSIS_ENABLED=true
DIAGNOSIS_WORKER_COUNT=1
DIAGNOSIS_KAFKA_GROUP_ID=diagnosis-worker
```

---

## 6. 详细实施步骤

### 6.1 模块 1：配置与启动边界

**目标：** 先让自动诊断能力具备明确开关和安全默认值，避免上线后意外消费 Kafka 或调用 LLM。

**实施步骤：**

1. 在 `config.Config` 增加 Diagnosis Worker 字段：
   - `DiagnosisEnabled bool`
   - `DiagnosisWorkerCount int`
   - `DiagnosisKafkaGroupID string`
   - `DiagnosisTaskTTL time.Duration`
   - `DiagnosisTaskTimeout time.Duration`
   - `DiagnosisRetryableErrors bool`
   - `DiagnosisStatusPushEnabled bool`
2. 在 `Load()` 中读取第 5 章列出的环境变量。
3. 在 `Validate()` 中实现配置校验规则。
4. 在 `main.go` 中只在 `DiagnosisEnabled=true` 时初始化 Worker；关闭时记录 info 日志：
   - `diagnosis worker disabled`
5. 增加配置单元测试：
   - 默认值测试。
   - `DIAGNOSIS_ENABLED=true` 但无 `KAFKA_BROKERS` 的失败测试。
   - `DIAGNOSIS_TASK_TTL_SECONDS <= DIAGNOSIS_TASK_TIMEOUT_SECONDS` 的失败测试。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./config
```

**通过标准：**

1. 默认配置不启动 Worker，不影响现有 `server-web`。
2. 显式开启自动诊断时，缺失 Kafka 或 Copilot 配置会给出清晰错误。
3. 不修改既有配置键名，不改变现有默认行为。

### 6.2 模块 2：Kafka Diagnosis Consumer

**目标：** 在 `server-web` 内实现可测试的 Kafka ConsumerGroup，消费 `alert-events`。

**实施步骤：**

1. 在 `server-web/kafka/consumer.go` 定义接口：

```go
type AlertProcessor interface {
    Process(ctx context.Context, event AlertEvent) error
}
```

2. 实现 `NewConsumer(brokers []string, groupID string, processor AlertProcessor) (*Consumer, error)`。
3. Consumer 固定订阅 `TopicAlertEvents`，复用 Sarama，策略与 `alert-service` 保持一致：
   - `BalanceStrategyRange`
   - `Offsets.Initial = sarama.OffsetOldest`
   - 成功处理后 `MarkMessage`
4. 对错误分类：
   - JSON 解析失败：记录 warn，提交 offset。
   - 永久错误：记录 warn，提交 offset。
   - 临时错误：记录 error，不提交 offset，等待 Kafka 重投。
5. 增加 `Permanent(err error)`、`IsPermanent(err error)`，保持与 `alert-service` 模式一致。
6. 增加 panic recovery；panic 时不提交 offset，并记录 fingerprint/status。
7. 增加 ConsumerObserver：
   - `processed`
   - `skipped`
   - `invalid_json`
   - `permanent_error`
   - `process_error`

**验证命令：**

```bash
cd server-monitor/server-web
go test ./kafka
```

**通过标准：**

1. 非法 JSON 不会导致 Consumer 退出。
2. 临时错误不会提交 offset。
3. 成功处理会提交 offset。
4. 代码不引入新依赖，沿用现有 Sarama。

### 6.3 模块 3：自动诊断请求归一化

**目标：** 让 Diagnosis Service 能明确区分手动、对话和自动触发。

**实施步骤：**

1. 在 `diagnosis/types.go` 增加：

```go
const TriggerAuto = "auto"
```

2. 修改 `NormalizeRequest()`，允许 `manual/chat/auto` 三种触发方式。
3. 增加单元测试：
   - `TriggerAuto` 合法。
   - 空 trigger 仍默认 `manual`。
   - 其他值仍返回 `ErrInvalidRequest`。
4. Worker 生成请求时优先使用 Kafka event 的字段：
   - `Fingerprint`
   - `Labels["alertname"]`
   - `Labels["instance"]`
   - `TriggerType: TriggerAuto`
5. 如果 event 缺少 fingerprint，使用 `alertname + instance + startsAt` 生成稳定降级 key，但仍在 `CollectionErrors` 或日志中记录字段缺失。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/diagnosis -run 'TestNormalizeRequest'
```

**通过标准：**

1. 自动触发的报告落库后 `trigger_type=auto`。
2. 原有手动和 Chat 触发测试不回退。

### 6.4 模块 4：Redis 幂等去重与任务状态

**目标：** 防止同一条 firing 告警在短时间内反复触发 LLM 诊断。

**Redis Key 设计：**

```text
diagnosis:task:<fingerprint>
```

**Value 结构：**

```json
{
  "fingerprint": "abc123",
  "status": "running",
  "report_id": 123,
  "trigger_type": "auto",
  "started_at": "2026-05-11T10:00:00Z",
  "updated_at": "2026-05-11T10:00:10Z",
  "error": ""
}
```

**实施步骤：**

1. 定义 `TaskStore` 接口，接口放在 `diagnosis` 包内：

```go
type TaskStore interface {
    TryStart(ctx context.Context, fingerprint string, ttl time.Duration) (bool, error)
    MarkRunning(ctx context.Context, fingerprint string, reportID uint64, ttl time.Duration) error
    MarkCompleted(ctx context.Context, fingerprint string, reportID uint64, ttl time.Duration) error
    MarkFailed(ctx context.Context, fingerprint string, errText string, ttl time.Duration) error
}
```

2. Redis 实现使用 `SET key value NX EX ttl` 完成原子抢占。
3. `TryStart` 返回 `false` 时 Worker 直接跳过，不调用 LLM。
4. 诊断成功后保留 completed 状态直到 TTL 到期。
5. 诊断失败后保留 failed 状态，避免短时间内无限重试打爆 LLM；是否重试由 Kafka offset 策略控制。
6. Redis 不可用时：
   - `DIAGNOSIS_ENABLED=true` 且 Redis 客户端未启用：启动失败。
   - 运行中 Redis 临时错误：返回临时错误，不提交 offset，等待恢复。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/diagnosis -run 'Test.*Dedupe|Test.*TaskStore'
```

**通过标准：**

1. 并发两个相同 fingerprint 只有一个成功 `TryStart`。
2. key TTL 正确设置。
3. Redis 错误不被吞掉。

### 6.5 模块 5：Diagnosis Worker 主流程

**目标：** 把 Kafka event 转换为自动诊断任务，并复用已有 Diagnosis Pipeline。

**处理流程：**

```text
收到 Kafka AlertEvent
  │
  ├─ 校验 topic/message 基本字段
  ├─ 过滤 status != firing
  ├─ 提取 fingerprint/alertname/instance/severity
  ├─ Redis TryStart 去重
  ├─ 推送 diagnosis_update: pending
  ├─ context.WithTimeout(DIAGNOSIS_TASK_TIMEOUT_SECONDS)
  ├─ 调用 diagnosis.Service.Trigger(user=system, trigger_type=auto)
  ├─ 成功: MarkCompleted + 推送 completed
  └─ 失败: MarkFailed + 推送 failed + 按错误类型决定是否提交 offset
```

**系统用户约定：**

自动诊断不来自真实登录用户，使用内部 system user：

```text
ID: 0
Username: system
Role: admin
```

说明：`Role=admin` 只用于读取诊断依赖资源，不授予写操作执行能力。Phase 5 不包含写操作，后续 Phase 6 审批审计必须单独处理 system actor。

**错误分类：**

| 场景 | 类型 | Kafka offset |
|---|---|---|
| `status=resolved` | 跳过 | 提交 |
| 缺少 alertname/instance 且无法解析 | 永久错误 | 提交 |
| Redis 去重显示重复 | 跳过 | 提交 |
| MySQL 查询目标不存在 | 永久错误 | 提交 |
| LLM 超时但 rule-only 可完成 | 成功 | 提交 |
| Prometheus 临时不可用但诊断降级完成 | 成功 | 提交 |
| Redis/MySQL 完全不可用 | 临时错误 | 不提交 |
| panic | 临时错误 | 不提交 |

**实施步骤：**

1. 新增 `Worker` 类型，依赖通过构造函数注入：
   - `Service`
   - `TaskStore`
   - `Notifier`
   - `Timeout`
   - `Now`
   - `Logger`
2. Worker 实现 `Process(ctx context.Context, event kafka.AlertEvent) error`。
3. 在 `main.go` 初始化 Consumer 和 Worker：
   - 构造 `diagnosis.Service` 时不要只藏在 `api.NewRouter()` 内部。
   - 推荐抽出 `api.NewCopilotRuntime(...)` 或让 `api.NewRouter` 接收已构造的 runtime，避免重复创建 LLM/Tool/Runbook。
4. `startBackgroundTasks()` 中启动 Consumer goroutine：
   - 使用 `app.ctx` 控制退出。
   - panic recovery 只包住长期后台任务。
   - shutdown 时先停止 HTTP，再 cancel app context，最后关闭 Kafka consumer。
5. Worker 不创建新的数据库连接池，不创建新的 Redis 客户端，不重复加载 Runbook。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/diagnosis -run 'TestWorker'
go test ./...
```

**通过标准：**

1. `firing` 事件触发 `Service.Trigger`。
2. `resolved` 事件不触发诊断。
3. 重复 fingerprint 不重复触发。
4. 超时会取消诊断上下文。
5. app shutdown 后 Consumer goroutine 能退出。

### 6.6 模块 6：WebSocket 诊断状态推送

**目标：** 让前端实时感知自动诊断状态，不需要轮询才能看到结果。

**消息格式：**

```json
{
  "type": "diagnosis_update",
  "data": {
    "fingerprint": "abc123",
    "alert_name": "HighCPU",
    "instance": "node-1:9090",
    "status": "completed",
    "trigger_type": "auto",
    "report_id": 123,
    "summary": "CPU usage stayed high for 15m",
    "error": "",
    "updated_at": "2026-05-11T10:01:30Z"
  }
}
```

**状态枚举：**

| 状态 | 含义 |
|---|---|
| `pending` | Worker 已接收事件，准备开始 |
| `running` | 已创建诊断报告，正在采集证据或调用 LLM |
| `completed` | 诊断报告已完成 |
| `failed` | 诊断失败，前端可提示人工重试 |
| `skipped` | 重复、resolved 或字段不足导致跳过 |

**实施步骤：**

1. 新增 `Notifier` 接口：

```go
type Notifier interface {
    NotifyDiagnosis(ctx context.Context, update DiagnosisUpdate) error
}
```

2. 实现 WebSocket notifier，内部调用 `websocketHub.BroadcastBlocking(ctx, payload)`。
3. 推送失败只记录 warn，不影响 Kafka offset 提交，避免 WebSocket 短暂异常导致重复诊断。
4. 复用 `/ws/alerts`，不新增 WebSocket 路径。
5. 单元测试使用 fake notifier 验证状态顺序。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/diagnosis -run 'TestNotifier|TestWorker'
```

**通过标准：**

1. 成功链路至少推送 pending/running/completed。
2. 失败链路推送 failed。
3. 跳过链路可推送 skipped，但不得污染诊断报告列表。

### 6.7 模块 7：前端自动诊断状态展示

**目标：** 前端能自然展示自动诊断结果，避免用户误以为告警没有被分析。

**实施步骤：**

1. 在 WebSocket 消息处理处识别 `diagnosis_update`。
2. 在告警列表或告警详情中维护 `fingerprint -> diagnosisStatus` 的本地映射。
3. `completed` 时显示“查看诊断”入口，链接到 `DiagnosisDetailPage`。
4. `failed` 时显示失败状态和“手动重试”入口，仍调用现有 `POST /api/v1/diagnosis`。
5. 诊断列表增加来源展示：
   - `manual`：手动
   - `chat`：对话
   - `auto`：自动
6. 不新增大面积页面，不改变现有导航结构。

**验证命令：**

```bash
cd server-monitor/frontend
npm run build
```

**通过标准：**

1. TypeScript 类型检查通过。
2. 现有诊断列表和详情页不回退。
3. 未收到 WebSocket 时仍可通过诊断列表看到自动报告。

### 6.8 模块 8：部署、联调、回归与验收

**目标：** 把自动诊断从单元测试推进到本地 Compose 可验证。

**实施步骤：**

1. Docker Compose：
   - `server-web` 增加 `DIAGNOSIS_ENABLED` 等变量。
   - 默认值保持 `false`，避免本地无 LLM key 时自动调用。
2. K8s 原生 YAML：
   - `configmap.yaml` 增加变量。
   - `web.yaml` 注入变量。
3. Helm：
   - `values.yaml` 增加默认值。
   - `templates/configmap.yaml` 和 `templates/server-web.yaml` 同步。
4. 本地联调：
   - 启动 Redis、MySQL、Kafka、Prometheus、Alertmanager、server-web。
   - 开启 `DIAGNOSIS_ENABLED=true`。
   - 触发一条 `HighCPU` 或构造 Alertmanager webhook。
   - 确认 webhook 快速返回。
   - 确认 Kafka `alert-events` 被消费。
   - 确认生成 `trigger_type=auto` 的 `diagnosis_reports`。
   - 确认前端收到或可查询诊断结果。
5. 回归：
   - `resolved` 事件不生成新诊断。
   - 同一 fingerprint 连续发送不重复生成。
   - LLM API key 缺失时 rule-only 降级仍可完成或给出清晰 failed。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./...
go vet ./...

cd ../frontend
npm run build

cd ..
docker compose config
```

如实际修改 Helm：

```bash
cd server-monitor
helm lint charts/server-monitor
```

**通过标准：**

1. Go 测试和 vet 通过。
2. 前端 build 通过。
3. Compose 配置渲染通过。
4. 真实告警或 webhook 构造事件能异步生成自动诊断。

---

## 7. 资源分配

### 7.1 人员角色

| 角色 | 人数 | 职责 |
|---|---:|---|
| 后端开发 | 1 | Kafka Consumer、Worker、Redis 去重、诊断服务接入、配置与测试 |
| 前端开发 | 1 | WebSocket 状态处理、诊断来源展示、失败重试入口 |
| 测试/联调 | 1 | Kafka/Redis/MySQL/Prometheus/Alertmanager 联调、回归用例、验收记录 |
| 运维/部署 | 1 | Compose、K8s、Helm 配置同步、环境变量和日志指标检查 |

小团队执行时可由同一人兼任，但必须按模块分批提交和验证。

### 7.2 工时估算

| 模块 | 估算 |
|---|---:|
| 配置与启动边界 | 0.5 天 |
| Kafka Diagnosis Consumer | 1 天 |
| 自动诊断请求归一化 | 0.5 天 |
| Redis 幂等去重与任务状态 | 1 天 |
| Diagnosis Worker 主流程 | 1.5 天 |
| WebSocket 状态推送 | 0.5 天 |
| 前端自动诊断状态展示 | 1 天 |
| 部署、联调、回归与验收 | 1.5 天 |
| 合计 | 7 天 |

---

## 8. 时间节点

以 2026-05-11 开始实施为基准，建议 7 个工作日完成。

| 日期 | 里程碑 | 输出 |
|---|---|---|
| 2026-05-11 | M1 配置和 Kafka Consumer 骨架 | 配置字段、校验测试、Consumer 单测 |
| 2026-05-12 | M2 Consumer 完整错误处理 | offset 提交策略、panic recovery、指标事件 |
| 2026-05-13 | M3 去重和自动请求 | `TriggerAuto`、Redis TaskStore、重复告警测试 |
| 2026-05-14 | M4 Worker 主流程 | firing 自动触发、resolved 跳过、service 接入 |
| 2026-05-15 | M5 状态推送和前端展示 | `diagnosis_update`、诊断来源展示、前端 build |
| 2026-05-18 | M6 部署配置同步 | Compose/K8s/Helm 配置一致，`docker compose config` 通过 |
| 2026-05-19 | M7 联调验收 | Alertmanager 告警后自动生成诊断，验收记录完整 |

如中途发现 Phase 3/4 未完全落地，应先补齐诊断报告或 Runbook 检索的阻塞项，再继续 Worker 联调。

---

## 9. 技术要求

### 9.1 Go 实现要求

1. 不新增第三方依赖，继续使用现有 `github.com/IBM/sarama`。
2. 后台 goroutine 必须受 `app.ctx` 控制，应用退出时可取消。
3. 不在 struct 中存储 `context.Context`；context 只作为方法第一个参数传递。
4. Kafka 消费、Redis、MySQL、Prometheus、LLM 调用都必须有 timeout 或可取消 context。
5. 不吞掉后台错误；必须分类记录日志并暴露指标。
6. Worker panic recovery 只包住长期后台消费逻辑，不扩大到普通业务方法。
7. 不在 Webhook handler 中等待自动诊断结果。
8. 测试不得依赖真实 Kafka、Redis、MySQL、Prometheus 或固定端口；使用 fake consumer、fake task store、fake diagnosis service、fake notifier。

### 9.2 诊断一致性要求

1. 自动诊断使用与手动诊断相同的 EvidenceCollector、RuleAnalyzer、LLMSummarizer。
2. 自动诊断报告必须能通过现有 `GET /api/v1/diagnosis/:id` 查询。
3. `trigger_type=auto` 不应破坏原有列表接口；若增加筛选，必须兼容不传参数的旧行为。
4. 对同一 fingerprint 的重复事件必须幂等。
5. 如果活跃告警和历史告警 payload 不一致，优先使用 Kafka event 的 fingerprint/status 与 MySQL `alert_histories` 的完整上下文补齐。

### 9.3 性能要求

| 指标 | 目标 |
|---|---:|
| Alertmanager Webhook 响应 | 不因自动诊断增加同步等待 |
| 单条自动诊断总超时 | 默认 120s |
| Worker 并发 | 默认 1，最大 8 |
| 去重 TTL | 默认 30 分钟 |
| Kafka 单消息处理 | 成功/跳过路径必须可提交 offset |
| WebSocket 推送失败 | 不影响诊断报告落库 |

### 9.4 安全要求

1. 自动诊断只读，不执行任何写操作。
2. system user 不得绕过 Phase 6 的审批审计设计。
3. 日志不输出 LLM API key、JWT、Redis/MySQL 密码或完整敏感 payload。
4. WebSocket payload 只包含诊断状态、报告 ID、告警标识和摘要，不包含内部错误堆栈。
5. LLM 失败时外部展示使用公共错误描述，详细错误仅进入服务端日志。

---

## 10. 测试方案

### 10.1 单元测试

| 模块 | 测试重点 | Mock 方式 |
|---|---|---|
| Config | 默认值、非法组合、边界值 | 环境变量隔离 |
| Kafka Consumer | JSON 错误、永久错误、临时错误、panic | fake marker、fake processor |
| Request Normalize | `TriggerAuto`、非法 trigger、字段缺失 | 纯函数 |
| TaskStore | `SET NX` 语义、TTL、状态迁移 | fake Redis 或接口 fake |
| Worker | firing/resolved、重复事件、成功/失败、超时 | fake service/task store/notifier |
| Notifier | payload 格式、推送失败降级 | fake WebSocket hub |
| Repository | 按 fingerprint 查询最近报告 | sqlite 或 mock gorm，按现有测试风格选择 |

### 10.2 集成测试

| 场景 | 验证点 |
|---|---|
| Kafka event → Worker → Diagnosis Service | `firing` 生成自动诊断报告 |
| 重复 event | 只生成一条报告，第二条 skipped |
| LLM 失败 | rule-only 降级或 failed 状态符合预期 |
| MySQL 临时失败 | 不提交 offset，日志和指标可见 |
| WebSocket 不可用 | 报告仍落库，推送错误不影响主链路 |

### 10.3 端到端验收

1. 启动本地 Compose，并开启自动诊断。
2. 构造 Alertmanager webhook：
   - `status=firing`
   - `labels.alertname=HighCPU`
   - `labels.instance=<已有 server-probe instance>`
   - `fingerprint=<稳定值>`
3. 验证 Webhook HTTP 响应快速返回。
4. 验证 Kafka `alert-events` 有消息。
5. 验证 Worker 日志出现 `diagnosis worker processed alert event`。
6. 验证 MySQL `diagnosis_reports` 出现 `trigger_type=auto`。
7. 验证前端诊断列表出现自动诊断报告。
8. 再次发送相同 fingerprint，验证不会重复生成。
9. 发送 `status=resolved`，验证不会生成自动诊断。

---

## 11. 风险评估与应对措施

| 风险 | 影响 | 概率 | 应对措施 |
|---|---|---:|---|
| Worker 重复消费导致重复 LLM 调用 | 成本升高、报告重复 | 中 | Redis `SET NX` 去重；Kafka key 使用 fingerprint；列表按 fingerprint 可追踪 |
| LLM 延迟或超时 | Worker 堆积、诊断延迟 | 高 | 单任务 timeout、默认单 worker、rule-only 降级、指标观察耗时 |
| Kafka 临时不可用 | 自动诊断停止 | 中 | 自动诊断默认关闭；Consumer ready 指标；临时错误不提交 offset |
| Redis 不可用 | 无法幂等，可能重复诊断 | 中 | 开启自动诊断时 Redis 必须可用；运行中 Redis 错误按临时错误处理 |
| MySQL 写入失败 | 诊断报告无法持久化 | 中 | 不提交 offset 或标记 failed；日志记录；后续可扩展 Redis 暂存重试 |
| WebSocket 推送失败 | 前端无法实时看到状态 | 中 | 推送失败不影响报告落库；前端保留列表查询兜底 |
| Alert payload 字段不完整 | 无法定位诊断对象 | 中 | 字段校验；缺关键字段按永久错误提交 offset；日志记录原始 key |
| 自动诊断压垮 Prometheus | 查询延迟升高 | 低到中 | 并发上限、工具 timeout、PromQL 范围限制沿用 Tool Registry |
| 与 alert-service 消费同 topic 互相影响 | 消费组隔离不当会抢消息 | 中 | 使用独立 `DIAGNOSIS_KAFKA_GROUP_ID=diagnosis-worker`，不得复用 `alert-service` |
| Phase 6 审批边界被提前突破 | 安全风险 | 低 | Phase 5 禁止写操作；ActionAdvisor 只生成建议，不创建 PendingAction |
| 部署配置不一致 | Compose 可用但 K8s/Helm 不可用 | 中 | 同步修改 Compose、原生 K8s、Helm；用 `docker compose config` 和 `helm lint` 验证 |

---

## 12. 验收标准

### 12.1 功能验收

1. `DIAGNOSIS_ENABLED=false` 时，系统行为与 Phase 4 一致。
2. `DIAGNOSIS_ENABLED=true` 且 Kafka 可用时，Worker 能消费 `alert-events`。
3. `firing` 告警能异步生成 `trigger_type=auto` 的诊断报告。
4. `resolved` 告警不会自动生成诊断报告。
5. 同一 fingerprint 在 TTL 内不会重复生成自动诊断。
6. 自动诊断状态能通过 WebSocket 推送，前端能展示 completed/failed 状态。
7. Webhook 响应不等待 LLM，不因诊断耗时而阻塞。

### 12.2 回归验收

1. `POST /api/v1/diagnosis` 手动触发仍可用。
2. Copilot Chat 中的诊断请求仍可用。
3. Runbook 命中仍展示在诊断详情页。
4. 告警 Webhook、活跃告警、告警历史、WebSocket 告警推送不回退。
5. `alert-service` 消费 `alert-events` 不受 Diagnosis Worker 影响。

### 12.3 验证命令清单

```bash
cd server-monitor/server-web
go test ./...
go vet ./...

cd ../frontend
npm run build

cd ..
docker compose config

helm lint charts/server-monitor
```

如果当前环境没有 Helm 或 Node 依赖，应在验收报告中明确记录未运行原因，不能写成通过。

---

## 13. 建议提交拆分

### 提交 1：配置与 Consumer

```bash
git add server-monitor/server-web/config/config.go \
        server-monitor/server-web/kafka/consumer.go \
        server-monitor/server-web/kafka/consumer_test.go
git commit -m "feat: add diagnosis worker kafka consumer"
```

### 提交 2：Worker 与幂等

```bash
git add server-monitor/server-web/copilot/diagnosis
git commit -m "feat: trigger diagnosis from alert events"
```

### 提交 3：状态推送与前端展示

```bash
git add server-monitor/frontend/src
git commit -m "feat: show automatic diagnosis status"
```

### 提交 4：部署配置与验收

```bash
git add server-monitor/docker-compose.yml \
        server-monitor/k8s \
        server-monitor/charts/server-monitor
git commit -m "chore: configure automatic diagnosis worker"
```

实际提交前应重新检查 `git status`，避免把无关变更混入。

---

## 14. Phase 6 交接说明

Phase 5 完成后，系统具备“告警自动诊断”的闭环，但仍然只提供建议，不执行动作。Phase 6 应在此基础上实现：

1. `PendingAction` 模型和审批 API。
2. `AuditLog` 模型和查询页面。
3. ActionAdvisor 将建议动作转为待审批动作。
4. admin 审批、拒绝、超时和失败均写审计。
5. 写操作仍不直接由 LLM 执行，必须经过 Human-in-the-loop。

Phase 5 的 Worker 可以在诊断完成后识别 recommended actions，但只能展示建议，不创建执行任务，不调用 K8s，不修改业务资源。
