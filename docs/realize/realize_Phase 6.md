# CloudOps Copilot Phase 6 实施方案

> 方案版本：v1.0
> 制定日期：2026-05-11
> 依据文档：`docs/design.md` v3.1
> 阶段定位：动作审批与审计，在 Phase 3 诊断报告、Phase 4 Runbook 检索、Phase 5 异步诊断 Worker 基础上，补齐 Human-in-the-loop 安全闭环：AI 只生成建议动作，写操作必须进入 PendingAction，admin 审批后才能执行，所有批准、拒绝、执行成功、执行失败和权限拒绝都写入 AuditLog。

---

## 1. 阶段目标

Phase 6 的目标是把诊断报告中的 `recommended_actions` 从“文本建议”升级为“可审批、可追踪、可审计的待处理动作”。系统仍坚持 AI 安全边界：LLM 输出不可信，不能直接触发写操作；ActionAdvisor 只基于结构化诊断报告、规则分析和白名单策略生成 PendingAction；ActionExecutor 在审批后再次校验动作类型、目标、参数、RBAC 和状态流转，再执行受控动作或返回可审计失败。

本阶段优先实现审批与审计框架本身，而不是扩展 Kubernetes 深度能力。`k8s.restart_deployment`、`k8s.scale_deployment` 的动作类型、参数结构、风险等级和执行接口在 Phase 6 定义清楚；真实 K8s 只读工具、日志、事件、独立 ServiceAccount 和更完整的集群接入属于 Phase 7。Phase 6 可通过 mock / disabled executor 完成失败审计和端到端审批流验证；若当前代码已经具备可注入的 K8s executor，则只开放设计文档允许的两个中风险动作。

### 1.1 核心交付物

| 交付物 | 内容 | 验收标准 |
|---|---|---|
| `PendingAction` 模型 | 待审批动作表，记录诊断关联、动作类型、目标、参数、风险、状态、审批人和执行结果 | 可 AutoMigrate；字段与设计文档一致；状态只能按合法状态机流转 |
| `AuditLog` 模型 | 审计日志表，记录 actor、角色、动作、资源、脱敏请求、结果、错误、trace_id | 批准、拒绝、执行成功、执行失败、权限拒绝都有审计记录 |
| Action Policy | 白名单、风险等级、参数范围、RBAC、状态校验 | 未注册动作、高风险动作、非法参数、非 admin 操作均被拒绝 |
| ActionAdvisor | 从诊断报告推荐动作中生成 PendingAction | 只处理 `requires_approval=true` 且命中白名单的中风险动作；重复建议可幂等处理 |
| Action API | `/api/v1/actions/pending`、`/api/v1/actions/:id`、`approve`、`reject`、`execute` | viewer 不能访问；admin 可查看、审批、拒绝、执行；错误响应清晰 |
| ActionExecutor | 受控执行接口和白名单实现 | 未审批动作不能执行；执行前二次校验；成功/失败均更新状态并写 AuditLog |
| `operation-events` 生产 | 审批和执行事件写入 Kafka Topic | Kafka 不可用时不影响 MySQL 状态，记录日志和审计降级信息 |
| WebSocket 状态推送 | 复用 `/ws/alerts` 推送 `action_pending` / `action_status` | 前端审批页可实时看到待审批和状态变化 |
| 前端审批与审计页面 | admin 可查看待审批动作、详情、证据、批准、拒绝、执行、审计日志 | viewer 看不到入口；admin 操作后页面状态一致刷新 |
| 验证闭环 | 单元测试、API 测试、前端构建、权限和失败路径验证 | 未审批不能执行，非 admin 不能审批，成功/失败/拒绝均可追溯 |

### 1.2 本阶段不做

1. 不让 LLM 直接执行任何写操作。
2. 不开放删除 Namespace、PVC、Secret、ConfigMap、Node drain、批量重启等高风险动作。
3. 不实现完整 K8s 只读工具集；`k8s.get_pods`、`k8s.get_logs`、`k8s.get_events` 等属于 Phase 7。
4. 不把审批状态写入 Redis 作为主存储；MySQL 是 PendingAction 和 AuditLog 的事实来源。
5. 不新增独立 action-service 微服务；审批审计嵌入 `server-web`。
6. 不改变现有诊断报告 API 的外层结构；只允许在兼容方式下增加关联动作查询或动作入口。
7. 不修改现有 JWT / RBAC 角色模型；继续使用 `admin` / `viewer`。
8. 不把敏感参数、Token、Secret、完整内部错误直接展示给前端、写入 LLM Prompt 或写入未脱敏审计字段。

---

## 2. 当前基础与前置条件

### 2.1 当前已具备能力

| 能力 | 当前落点 | Phase 6 复用方式 |
|---|---|---|
| 诊断报告模型 | `server-monitor/server-web/model/diagnosis_report.go` | 读取 `recommended_actions_json`，关联 `diagnosis_report_id` 创建 PendingAction |
| 诊断服务 | `server-monitor/server-web/copilot/diagnosis` | ActionAdvisor 以诊断报告、规则结果和推荐动作作为输入 |
| Copilot Tool Registry | `server-monitor/server-web/copilot/tool` | 复用工具风险、参数 schema、执行上下文和错误脱敏思路 |
| JWT / RBAC | `server-monitor/server-web/api/middleware/auth.go`、`rbac.go` | Action API 全部要求登录，审批、执行、审计只允许 admin |
| Router 分组 | `server-monitor/server-web/api/router.go` | 新增 action 和 audit 的 admin 路由组 |
| MySQL / GORM | `server-monitor/server-web/database`、`model.AllModels()` | 新增模型并纳入 AutoMigrate |
| Kafka Topic | `server-monitor/server-web/kafka/topics.go`、Compose/K8s/Helm 已预留 `operation-events` | 新增 operation event producer 方法或独立 ActionProducer |
| WebSocket Hub | `server-monitor/server-web/websocket/hub.go` | 推送 `action_pending`、`action_status` |
| 前端权限路由 | `server-monitor/frontend/src/router/index.ts` | 新增 admin-only 审批和审计页面 |
| 前端诊断页 | `server-monitor/frontend/src/pages/DiagnosisDetailPage.vue` | 展示推荐动作创建 PendingAction 的入口或关联动作状态 |

### 2.2 前置假设

1. Phase 3 的 `DiagnosisReport` 已可持久化，并包含 `recommended_actions_json`。
2. Phase 5 的自动诊断完成后，诊断报告可能由 `manual`、`chat` 或 `auto` 触发，Phase 6 不区分触发源，只处理已落库报告。
3. `operation-events` Topic 在 Docker Compose、原生 K8s 和 Helm 中已经预留；如果 Kafka 未启用，审批主链路仍以 MySQL 为准并记录降级日志。
4. 当前 `server-web` 仍是动作审批和审计的唯一后端入口，不新增服务间 HTTP / gRPC。
5. Phase 6 实施时若没有真实 K8s executor，应提供 disabled / mock executor，使审批、拒绝、失败审计可完整验证；真实执行能力在 Phase 7 接入。
6. 前端已有 admin 路由保护能力，新增页面应复用现有导航、表格、筛选和 API client 风格。

### 2.3 阶段边界

Phase 6 只负责动作从“建议”到“审批、执行、审计”的安全闭环。它不扩大 AI 的决策权，不新增高风险动作，不绕过 Tool Registry 或 Action Policy，不改变已有告警、诊断、Runbook、自动诊断的主链路。任何写操作必须先落 PendingAction，再由 admin 审批，执行前再次校验。

---

## 3. 总体实施路径

Phase 6 拆为 9 个模块推进，每个模块都能独立验证。

```text
模块 1：数据模型与状态机
  ↓
模块 2：Audit Logger 与脱敏策略
  ↓
模块 3：Action Policy 与参数校验
  ↓
模块 4：ActionAdvisor 生成 PendingAction
  ↓
模块 5：Action API 与 RBAC
  ↓
模块 6：ActionExecutor 与 operation-events
  ↓
模块 7：WebSocket 状态推送
  ↓
模块 8：前端审批与审计页面
  ↓
模块 9：部署、联调、回归与验收
```

---

## 4. 文件规划

### 4.1 后端新增文件

| 文件 | 职责 |
|---|---|
| `server-monitor/server-web/model/pending_action.go` | 定义 `PendingAction` GORM 模型、状态常量、风险常量 |
| `server-monitor/server-web/model/audit_log.go` | 定义 `AuditLog` GORM 模型和结果常量 |
| `server-monitor/server-web/copilot/action/types.go` | 定义动作类型、目标、参数、请求/响应 DTO、状态机事件 |
| `server-monitor/server-web/copilot/action/policy.go` | 白名单、风险等级、参数范围、RBAC、状态流转校验 |
| `server-monitor/server-web/copilot/action/policy_test.go` | 覆盖未注册动作、高风险拒绝、replicas 范围、状态流转 |
| `server-monitor/server-web/copilot/action/repository.go` | PendingAction / AuditLog 的 MySQL 读写封装 |
| `server-monitor/server-web/copilot/action/repository_test.go` | 使用 sqlite 或现有测试 DB 模式覆盖创建、查询、状态更新 |
| `server-monitor/server-web/copilot/action/audit.go` | 审计写入、请求脱敏、trace_id 提取 |
| `server-monitor/server-web/copilot/action/audit_test.go` | 覆盖 Secret/Token/Password 脱敏、失败审计不阻塞主错误 |
| `server-monitor/server-web/copilot/action/advisor.go` | 从 `DiagnosisReport.recommended_actions_json` 生成待审批动作 |
| `server-monitor/server-web/copilot/action/advisor_test.go` | 覆盖 requires_approval、白名单、重复创建、非法 JSON |
| `server-monitor/server-web/copilot/action/executor.go` | 审批后执行入口，封装执行前校验、状态更新、审计、事件发布 |
| `server-monitor/server-web/copilot/action/executor_test.go` | 覆盖未审批拒绝、执行成功、执行失败、重复执行 |
| `server-monitor/server-web/copilot/action/handler.go` | Action API handler |
| `server-monitor/server-web/copilot/action/handler_test.go` | 覆盖 admin/viewer 权限、参数错误、审批拒绝执行路径 |
| `server-monitor/server-web/copilot/action/notifier.go` | WebSocket action 状态消息结构和推送接口 |
| `server-monitor/server-web/copilot/action/notifier_test.go` | 覆盖 `action_pending`、`action_status` payload |
| `server-monitor/server-web/copilot/action/k8s_executor.go` | 定义 `K8sExecutor` 接口和 disabled executor；真实 client 留给 Phase 7 |
| `server-monitor/server-web/copilot/action/events.go` | `operation-events` 事件结构、结果常量 |

### 4.2 后端修改文件

| 文件 | 修改内容 |
|---|---|
| `server-monitor/server-web/model/models.go` | 将 `&PendingAction{}`、`&AuditLog{}` 加入 `AllModels()` |
| `server-monitor/server-web/api/router.go` | 初始化 action repository/policy/advisor/executor/handler，注册 admin 路由 |
| `server-monitor/server-web/kafka/producer.go` | 增加 `SendOperationEvent` 或抽出通用发送方法 |
| `server-monitor/server-web/api/middleware/metrics.go` | 增加 pending action 创建、审批、拒绝、执行成功/失败计数和耗时指标 |
| `server-monitor/server-web/copilot/diagnosis/service.go` | 诊断完成后可选调用 ActionAdvisor，或提供“从报告创建动作”的显式入口 |
| `server-monitor/server-web/copilot/diagnosis/handler.go` | 如采用显式入口，增加从诊断报告生成 PendingAction 的 handler 调用点 |
| `server-monitor/server-web/docs/swagger.yaml` / `docs.go` / `swagger.json` | 更新动作审批和审计 API 文档 |

### 4.3 前端新增文件

| 文件 | 职责 |
|---|---|
| `server-monitor/frontend/src/api/actions.ts` | PendingAction API：列表、详情、批准、拒绝、执行 |
| `server-monitor/frontend/src/api/auditLogs.ts` | AuditLog API：列表、筛选 |
| `server-monitor/frontend/src/pages/ActionsPage.vue` | 待审批动作列表和状态筛选 |
| `server-monitor/frontend/src/pages/ActionDetailPage.vue` | 动作详情、诊断证据、参数、审批、拒绝、执行结果 |
| `server-monitor/frontend/src/pages/AuditLogsPage.vue` | 审计日志列表、结果/动作/操作者筛选 |

### 4.4 前端修改文件

| 文件 | 修改内容 |
|---|---|
| `server-monitor/frontend/src/router/index.ts` | 新增 `/actions`、`/actions/:id`、`/audit-logs` admin-only 路由 |
| `server-monitor/frontend/src/App.vue` | 在 admin 导航区增加动作审批、审计日志入口 |
| `server-monitor/frontend/src/types/index.ts` | 增加 `PendingAction`、`AuditLog`、`ActionStatus`、`RiskLevel` 类型 |
| `server-monitor/frontend/src/pages/DiagnosisDetailPage.vue` | 展示推荐动作的审批状态；允许 admin 从报告生成待审批动作 |
| `server-monitor/frontend/src/composables/useAlertsWebSocket.ts` | 识别 `action_pending`、`action_status` 消息 |
| `server-monitor/frontend/src/stores/monitor.ts` | 如当前全局状态承载 WS 消息，增加 action 状态缓存 |

### 4.5 部署与配置文件

| 文件 | 修改内容 |
|---|---|
| `server-monitor/docker-compose.yml` | 确认 `operation-events` Topic 已创建；如需新增 action 开关则注入环境变量 |
| `server-monitor/k8s/configmap.yaml` | 增加 action 配置默认值 |
| `server-monitor/k8s/web.yaml` | 注入 action 配置 |
| `server-monitor/charts/server-monitor/values.yaml` | 增加 Helm values：`action.enabled`、`action.maxReplicas`、`action.executionEnabled` |
| `server-monitor/charts/server-monitor/templates/configmap.yaml` | 输出 action 配置 |
| `server-monitor/charts/server-monitor/templates/server-web.yaml` | 注入 action 配置 |

---

## 5. 数据模型设计

### 5.1 PendingAction

`PendingAction` 对应设计文档中的 `pending_actions` 表，是待审批动作的事实来源。

```text
id                    uint64     primaryKey, autoIncrement
diagnosis_report_id   uint64     index, 关联 diagnosis_reports.id，可为 0
action_type           varchar(64) 动作类型，例如 k8s.restart_deployment / k8s.scale_deployment
target_kind           varchar(32) 目标类型，例如 k8s_deployment
target_name           varchar(256) 目标名称
namespace             varchar(128) K8s 命名空间
params_json           longtext    动作参数 JSON，写入前脱敏和规范化
risk_level            varchar(16) 风险等级: low/medium/high
status                varchar(32) 状态: pending/approved/rejected/executing/executed/failed/cancelled
requested_by          varchar(32) 请求来源: ai-copilot/user
approved_by           uint64      审批人 user_id
executed_by           uint64      执行人 user_id
result_json           longtext    执行结果 JSON，脱敏后写入
error_message         text        错误摘要，不写内部堆栈和敏感信息
created_at            time.Time
approved_at           *time.Time
executed_at           *time.Time
updated_at            time.Time
```

### 5.2 AuditLog

`AuditLog` 记录所有安全关键事件。审计写入失败不能伪装成功，handler 必须返回清晰错误；但 Kafka 发布失败不回滚已提交的 MySQL 审批状态。

```text
id                    uint64     primaryKey, autoIncrement
actor                 varchar(128) 操作者 username / ai-copilot
actor_role            varchar(32)  角色 admin/viewer/system
action                varchar(64)  动作，例如 action.approve/action.reject/action.execute
resource_type         varchar(64)  资源类型，例如 pending_action
resource_id           varchar(128) 资源 ID
request_json          longtext     请求内容，必须脱敏
result                varchar(32)  success/failure/denied/timeout
error_message         text         错误摘要
trace_id              varchar(64)  OpenTelemetry trace id
created_at            time.Time
```

### 5.3 状态机

合法状态流转如下：

| 当前状态 | 事件 | 下一个状态 | 允许角色 | 说明 |
|---|---|---|---|---|
| `pending` | approve | `approved` | admin | 记录 `approved_by`、`approved_at` |
| `pending` | reject | `rejected` | admin | 拒绝必须记录 reason |
| `pending` | cancel | `cancelled` | admin/system | 可用于过期动作清理 |
| `approved` | execute | `executing` | admin/system | 执行前再次校验白名单和参数 |
| `executing` | execute_success | `executed` | system | 写入 result_json、executed_at |
| `executing` | execute_failure | `failed` | system | 写入 error_message、executed_at |
| `approved` | execute_failure | `failed` | system | executor 初始化失败时允许直接失败 |

禁止状态流转：

1. `pending` 不能直接 `executed`。
2. `rejected`、`executed`、`failed`、`cancelled` 为终态，不能再次 approve / reject / execute。
3. viewer 不能触发任何状态变更。
4. high risk 动作不能进入 pending；只能作为诊断建议展示。

---

## 6. Action Policy 技术要求

### 6.1 白名单动作

Phase 6 只定义设计文档允许的中风险写操作：

| 动作类型 | 目标类型 | 风险 | 参数 | Phase 6 执行策略 |
|---|---|---|---|---|
| `k8s.restart_deployment` | `k8s_deployment` | medium | `namespace`, `name` | 需要 admin 审批；无真实 executor 时执行失败并审计 |
| `k8s.scale_deployment` | `k8s_deployment` | medium | `namespace`, `name`, `replicas` | replicas 必须在 `[1, ACTION_MAX_REPLICAS]`；无真实 executor 时执行失败并审计 |

低风险只读建议不创建 PendingAction，例如继续观察、查看指标、查看 Runbook、通知负责人。高风险动作禁止创建 PendingAction，例如删除 Namespace/PVC/Secret、修改 Secret、批量变更配置。

### 6.2 参数校验

`ActionPolicy.ValidateCreate()` 校验：

1. `action_type` 必须在白名单中。
2. `risk_level` 必须与白名单风险一致，不能由请求方覆盖成更低风险。
3. `namespace` 必须非空，长度不超过 128，仅允许 DNS label / namespace 合法字符。
4. `target_name` 必须非空，长度不超过 256，仅允许 Kubernetes resource name 合法字符。
5. `params_json` 必须是 JSON object。
6. `scale_deployment.replicas` 必须为整数，范围 `[1, ACTION_MAX_REPLICAS]`，默认 `ACTION_MAX_REPLICAS=10`。
7. 请求中出现 `token`、`password`、`secret`、`kubeconfig` 等字段时拒绝创建，并写 denied 审计。

`ActionPolicy.ValidateExecute()` 校验：

1. 状态必须是 `approved`。
2. 当前执行者必须是 admin 或系统后台 executor。
3. 动作仍在白名单中，参数仍合法。
4. 目标和参数与审批时记录一致，不允许 execute 请求覆盖参数。
5. 如果 `ACTION_EXECUTION_ENABLED=false`，真实执行直接失败为可审计错误，不修改集群。

### 6.3 配置项

| 环境变量 | 默认值 | 类型 | 说明 | 敏感 |
|---|---:|---|---|---|
| `ACTION_APPROVAL_ENABLED` | `true` | bool | 是否启用动作审批 API | 否 |
| `ACTION_EXECUTION_ENABLED` | `false` | bool | 是否允许真实执行白名单动作；Phase 6 默认 false | 否 |
| `ACTION_MAX_REPLICAS` | `10` | int | `scale_deployment` 最大副本数 | 否 |
| `ACTION_PENDING_TTL_HOURS` | `24` | int | pending 动作建议过期时间，超期可取消 | 否 |
| `ACTION_OPERATION_EVENTS_ENABLED` | `true` | bool | 是否发布 `operation-events` | 否 |
| `ACTION_STATUS_PUSH_ENABLED` | `true` | bool | 是否推送 WebSocket action 状态 | 否 |

配置校验：

1. `ACTION_MAX_REPLICAS` 必须在 `[1, 100]`。
2. `ACTION_PENDING_TTL_HOURS` 必须在 `[1, 168]`。
3. `ACTION_EXECUTION_ENABLED=true` 时必须有可用 executor；没有 executor 则启动失败或强制降级为 false，并打印明确日志。
4. 不新增 Secret 配置；Phase 7 接入 K8s ServiceAccount 时再处理集群权限。

---

## 7. API 设计

### 7.1 Action API

所有 Action API 均要求登录；除可选的“从诊断报告生成动作”外，管理、审批、执行均要求 admin。

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `POST` | `/api/v1/diagnosis/:id/actions` | admin | 从诊断报告推荐动作生成 PendingAction |
| `GET` | `/api/v1/actions/pending` | admin | 待审批动作列表 |
| `GET` | `/api/v1/actions` | admin | 动作列表，支持 status/risk/action_type/page/page_size |
| `GET` | `/api/v1/actions/:id` | admin | 动作详情 |
| `POST` | `/api/v1/actions/:id/approve` | admin | 审批通过 |
| `POST` | `/api/v1/actions/:id/reject` | admin | 拒绝 |
| `POST` | `/api/v1/actions/:id/execute` | admin | 执行已审批动作 |

### 7.2 创建待审批动作

请求：

```json
{
  "source": "diagnosis",
  "selected_action_types": ["k8s.scale_deployment"]
}
```

响应：

```json
{
  "status": "success",
  "data": {
    "created": [
      {
        "id": 7,
        "diagnosis_report_id": 42,
        "action_type": "k8s.scale_deployment",
        "target_kind": "k8s_deployment",
        "namespace": "production",
        "target_name": "order-service",
        "risk_level": "medium",
        "status": "pending",
        "created_at": "2026-05-11T12:00:00Z"
      }
    ],
    "skipped": [
      {
        "action_type": "inspect_process",
        "reason": "read-only action does not require approval"
      }
    ]
  }
}
```

### 7.3 审批通过

请求：

```json
{
  "comment": "确认扩容风险可接受，先审批，执行前再核对副本数。"
}
```

响应：

```json
{
  "status": "success",
  "data": {
    "id": 7,
    "status": "approved",
    "approved_by": 1,
    "approved_at": "2026-05-11T12:05:00Z"
  }
}
```

### 7.4 拒绝动作

请求：

```json
{
  "reason": "当前缺少业务发布窗口，不允许扩容操作。"
}
```

响应：

```json
{
  "status": "success",
  "data": {
    "id": 7,
    "status": "rejected"
  }
}
```

### 7.5 执行动作

请求：

```json
{
  "confirm": true
}
```

响应成功：

```json
{
  "status": "success",
  "data": {
    "id": 7,
    "status": "executed",
    "result": {
      "action_type": "k8s.scale_deployment",
      "target": "production/order-service",
      "replicas": 3
    }
  }
}
```

响应失败：

```json
{
  "status": "error",
  "error": "action execution failed; see audit log for details"
}
```

失败时 `pending_actions.status=failed`，`audit_logs.result=failure`，`error_message` 写脱敏摘要。

### 7.6 Audit API

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `GET` | `/api/v1/audit-logs` | admin | 审计日志列表，支持 action/result/actor/page/page_size |
| `GET` | `/api/v1/audit-logs/:id` | admin | 审计日志详情 |

---

## 8. 详细实施步骤

### 8.1 模块 1：数据模型与状态机

**目标：** 先建立可靠的数据结构和状态机，避免后续 handler 直接散写状态。

**实施步骤：**

1. 新增 `model.PendingAction` 和状态常量：
   - `ActionStatusPending`
   - `ActionStatusApproved`
   - `ActionStatusRejected`
   - `ActionStatusExecuting`
   - `ActionStatusExecuted`
   - `ActionStatusFailed`
   - `ActionStatusCancelled`
2. 新增 `model.AuditLog` 和审计结果常量：
   - `AuditResultSuccess`
   - `AuditResultFailure`
   - `AuditResultDenied`
   - `AuditResultTimeout`
3. 修改 `model.AllModels()`，纳入两个新模型。
4. 在 `copilot/action/types.go` 定义 DTO，避免 handler 直接暴露 GORM 模型。
5. 在 `copilot/action/policy.go` 实现 `CanTransition(from, event)`。
6. 增加状态机单元测试。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./model ./copilot/action
```

**通过标准：**

1. 新模型能编译并纳入 AutoMigrate。
2. 非法状态流转测试全部拒绝。
3. 终态不能再次变更。

### 8.2 模块 2：Audit Logger 与脱敏策略

**目标：** 所有安全关键路径先具备审计能力，再接 API。

**实施步骤：**

1. 在 `copilot/action/audit.go` 定义：
   - `Logger.Record(ctx, Entry) error`
   - `SanitizeJSON(raw json.RawMessage) json.RawMessage`
   - `TraceIDFromContext(ctx) string`
2. 脱敏字段规则：
   - key 包含 `password`、`passwd`、`secret`、`token`、`api_key`、`authorization`、`kubeconfig` 时替换为 `"[REDACTED]"`。
   - 嵌套 object 和 array 递归脱敏。
   - 非法 JSON 作为字符串截断到 2048 字符后写入。
3. 审计写入字段统一使用 actor、actor_role、action、resource_type、resource_id、request_json、result、error_message、trace_id。
4. handler 层遇到权限拒绝、参数拒绝、状态拒绝时也写 denied 审计。
5. 审计写入失败时返回 500，不允许让关键操作“无审计成功”。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/action -run 'TestAudit|TestSanitize'
```

**通过标准：**

1. 敏感字段无论大小写和嵌套层级均被脱敏。
2. 审批、拒绝、执行的审计 entry 字段完整。
3. 审计失败路径可被测试捕获。

### 8.3 模块 3：Action Policy 与参数校验

**目标：** 把安全规则集中在 policy 层，handler、advisor、executor 都复用同一套校验。

**实施步骤：**

1. 定义白名单：
   - `k8s.restart_deployment`
   - `k8s.scale_deployment`
2. 定义风险策略：
   - 只读建议：不进入 PendingAction。
   - medium：必须审批。
   - high：拒绝创建，仅作为诊断文本建议展示。
3. 实现 `ValidateCreate(input CreateActionInput) (NormalizedAction, error)`。
4. 实现 `ValidateApprove(action PendingAction, actor User) error`。
5. 实现 `ValidateExecute(action PendingAction, actor User) error`。
6. 实现 namespace、name、replicas 校验。
7. 实现参数规范化：同一动作生成稳定的 `dedupe_key`，用于避免同一诊断报告重复创建同一动作。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/action -run TestPolicy
```

**通过标准：**

1. `replicas=0`、`replicas>ACTION_MAX_REPLICAS` 被拒绝。
2. 非白名单动作被拒绝。
3. 高风险动作被拒绝。
4. 合法 scale/restart 动作被规范化为稳定参数。

### 8.4 模块 4：ActionAdvisor 生成 PendingAction

**目标：** 从诊断报告推荐动作生成待审批动作，但不执行。

**实施步骤：**

1. 解析 `DiagnosisReport.RecommendedActionsJSON`。
2. 只处理满足以下条件的推荐动作：
   - `requires_approval=true`
   - `risk=medium`
   - `type` 能映射到白名单动作
   - `target` 中包含 namespace/name 或可从诊断报告 `namespace/target_name` 补全
3. 对低风险建议写入 skipped 结果，不创建 PendingAction。
4. 对高风险建议写入 skipped 结果，reason 为 `high risk action is not allowed`。
5. 对每条可创建动作调用 Policy 规范化。
6. 使用 `diagnosis_report_id + action_type + namespace + target_name + normalized_params` 做幂等查重。
7. 创建成功后写 AuditLog：
   - actor=`ai-copilot` 或当前 admin username
   - action=`action.create_pending`
   - result=`success`
8. 创建成功后推送 `action_pending` WebSocket 消息。

**触发方式建议：**

Phase 6 首选显式触发：admin 在诊断详情页点击“创建待审批动作”，调用 `POST /api/v1/diagnosis/:id/actions`。这样可避免自动诊断一完成就堆积 PendingAction。后续如需要自动创建，可加配置 `ACTION_AUTO_CREATE_FROM_DIAGNOSIS=false`，默认仍关闭。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/action -run TestAdvisor
```

**通过标准：**

1. 同一诊断报告重复触发不会重复创建相同 PendingAction。
2. 推荐动作 JSON 格式错误时返回清晰错误，不 panic。
3. 不符合白名单的动作不会落库。

### 8.5 模块 5：Action API 与 RBAC

**目标：** 对外提供审批、拒绝、执行入口，并严格复用 admin 权限。

**实施步骤：**

1. 在 `api/router.go` 中新增 admin 路由组：
   - `/api/v1/actions`
   - `/api/v1/audit-logs`
   - `/api/v1/diagnosis/:id/actions`
2. `GET /api/v1/actions/pending` 默认只查 `status=pending`。
3. `GET /api/v1/actions` 支持：
   - `status`
   - `risk_level`
   - `action_type`
   - `page`
   - `page_size`
4. `POST /api/v1/actions/:id/approve`：
   - 加载 action。
   - 校验状态 pending。
   - 校验 actor admin。
   - 更新 approved 字段。
   - 写 AuditLog success。
   - 推送 `action_status`。
5. `POST /api/v1/actions/:id/reject`：
   - reason 必填，长度 1 到 500。
   - 更新 rejected。
   - 写 AuditLog success。
   - 推送 `action_status`。
6. `POST /api/v1/actions/:id/execute`：
   - 只接受 `confirm=true`。
   - 调用 Executor，不在 handler 内直接执行。
7. 所有错误响应沿用当前项目 JSON 风格，避免泄露内部错误。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./api ./copilot/action
```

**通过标准：**

1. viewer 请求审批接口返回 403。
2. 未登录请求返回现有认证错误。
3. admin 可批准/拒绝 pending action。
4. pending 之外状态不能再次批准/拒绝。

### 8.6 模块 6：ActionExecutor 与 operation-events

**目标：** 审批后的执行路径受控、可观测、可审计。

**实施步骤：**

1. 定义执行接口：

```go
type K8sExecutor interface {
    RestartDeployment(ctx context.Context, namespace, name string) (ActionResult, error)
    ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) (ActionResult, error)
}
```

2. 提供 `DisabledK8sExecutor`：
   - 当 `ACTION_EXECUTION_ENABLED=false` 时使用。
   - 返回明确错误：`action execution disabled`。
   - Executor 将 action 标记为 failed，并写 failure 审计。
3. Executor 主流程：
   - 开启事务。
   - 重新读取 action 并加行级锁。
   - 校验状态 `approved`。
   - 更新为 `executing`。
   - 事务提交。
   - 调用底层 executor。
   - 根据结果更新 `executed` 或 `failed`。
   - 写 AuditLog。
   - 发布 `operation-events`。
   - 推送 WebSocket `action_status`。
4. Kafka `operation-events` payload：

```json
{
  "type": "action",
  "action_id": 7,
  "action_type": "k8s.scale_deployment",
  "target": "production/order-service",
  "status": "executed",
  "actor": "admin",
  "trace_id": "abc123",
  "occurred_at": "2026-05-11T12:10:00Z"
}
```

5. Kafka 发布失败处理：
   - 不回滚已完成的 MySQL 状态。
   - 记录 warn 日志。
   - 在 AuditLog 的 `error_message` 或 result_json 附带 `operation_event_publish=failed`。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/action ./kafka
```

**通过标准：**

1. 未审批 action 不能执行。
2. disabled executor 会产生 failed 状态和 failure 审计。
3. mock executor 成功时 action 进入 executed。
4. Kafka publish 失败不破坏 MySQL 状态。

### 8.7 模块 7：WebSocket 状态推送

**目标：** 让前端能实时感知待审批动作和执行状态变化。

**实施步骤：**

1. 定义 `action_pending` 消息：

```json
{
  "type": "action_pending",
  "data": {
    "action_id": 7,
    "diagnosis_report_id": 42,
    "action_type": "k8s.scale_deployment",
    "target": "production/order-service",
    "risk_level": "medium",
    "requested_by": "ai-copilot"
  }
}
```

2. 定义 `action_status` 消息：

```json
{
  "type": "action_status",
  "data": {
    "action_id": 7,
    "status": "executed",
    "result": "success",
    "updated_at": "2026-05-11T12:10:00Z"
  }
}
```

3. 推送失败只记录 warn，不影响数据库状态。
4. 前端收到消息后：
   - 如果在 ActionsPage，刷新当前页或局部更新状态。
   - 如果在 DiagnosisDetailPage，更新关联动作状态。
   - viewer 即使收到消息，也不展示 admin 操作入口。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/action -run TestNotifier
```

**通过标准：**

1. 消息 type 和字段稳定。
2. 推送失败不会导致审批/执行失败。
3. 前端对未知 type 仍保持兼容。

### 8.8 模块 8：前端审批与审计页面

**目标：** 提供一个安静、可扫描、适合运维场景的审批和审计工作台。

**实施步骤：**

1. 新增 `actions.ts`：
   - `listActions(params)`
   - `getAction(id)`
   - `approveAction(id, payload)`
   - `rejectAction(id, payload)`
   - `executeAction(id, payload)`
   - `createActionsFromDiagnosis(diagnosisID, payload)`
2. 新增 `auditLogs.ts`：
   - `listAuditLogs(params)`
   - `getAuditLog(id)`
3. 新增 `ActionsPage.vue`：
   - 顶部筛选：status、risk、action_type。
   - 表格列：ID、动作、目标、风险、状态、来源、创建时间、操作。
   - pending 行突出显示审批入口。
4. 新增 `ActionDetailPage.vue`：
   - 左侧基本信息：动作、目标、参数、风险、状态。
   - 右侧关联诊断：summary、证据入口、Runbook 入口。
   - 底部操作：批准、拒绝、执行。
   - 执行按钮仅在 `approved` 状态显示。
5. 新增 `AuditLogsPage.vue`：
   - 筛选：action、result、actor。
   - 表格列：时间、操作者、动作、资源、结果、trace_id、错误摘要。
6. 修改 `DiagnosisDetailPage.vue`：
   - recommended_actions 中 `requires_approval=true` 的项显示“创建审批动作”。
   - 已有关联 PendingAction 时展示状态，不重复创建。
7. 修改 `router/index.ts` 和 `App.vue`：
   - admin-only 路由。
   - 导航入口放在设置或运维管理区，避免 viewer 误触。
8. 所有按钮必须处理 loading、disabled、错误提示和二次确认。

**验证命令：**

```bash
cd server-monitor/frontend
npm run build
```

**通过标准：**

1. viewer 无法进入审批和审计页面。
2. admin 可以完成列表、详情、批准、拒绝、执行操作。
3. 长 action_type、长 target、长错误信息不会撑破布局。
4. 前端构建通过。

### 8.9 模块 9：部署、联调、回归与验收

**目标：** 将配置、部署和真实链路验证补齐，确认 Phase 6 没有破坏已有监控和诊断能力。

**实施步骤：**

1. Compose：
   - 确认 `operation-events` Topic 创建命令存在。
   - 为 `server-web` 增加 Action 配置环境变量。
2. 原生 K8s：
   - `configmap.yaml` 增加 Action 配置。
   - `web.yaml` 注入环境变量。
3. Helm：
   - `values.yaml` 增加 action 配置块。
   - `templates/configmap.yaml` 和 `server-web.yaml` 输出对应环境变量。
4. Swagger：
   - 更新 action 和 audit API。
5. 后端全量测试：

```bash
cd server-monitor/server-web
go test ./...
go vet ./...
```

6. 前端构建：

```bash
cd server-monitor/frontend
npm run build
```

7. 部署文件校验：

```bash
cd server-monitor
docker compose config
helm lint charts/server-monitor
kubectl apply --dry-run=client -f k8s/
```

8. 联调验收：
   - 创建一条带 `requires_approval=true` 的诊断推荐动作。
   - admin 创建 PendingAction。
   - viewer 调审批接口应 403。
   - admin approve。
   - pending 前 execute 应失败；approved 后 execute。
   - disabled executor 下状态为 failed，AuditLog 有 failure。
   - mock/safe executor 下状态为 executed，AuditLog 有 success。
   - reject 路径 AuditLog 有 success，终态不能再 approve/execute。

**通过标准：**

1. 未审批动作不能执行。
2. 非 admin 不能审批、拒绝、执行、查看审计。
3. 审批通过、拒绝、执行成功、执行失败都有审计。
4. WebSocket 状态推送不影响主事务。
5. Kafka 不可用不导致审批状态丢失。
6. 现有 Copilot、Diagnosis、Runbook、Diagnosis Worker 测试不回退。

---

## 9. 资源分配

### 9.1 人员角色

| 角色 | 人数 | 职责 |
|---|---:|---|
| 后端负责人 | 1 | 数据模型、Policy、Advisor、Executor、API、Kafka、审计、测试 |
| 前端负责人 | 1 | Actions/Audit 页面、诊断详情动作入口、WebSocket 状态更新、构建验证 |
| DevOps / 部署负责人 | 1 | Compose、K8s、Helm 配置一致性、Topic 校验、部署 dry-run |
| 测试 / 质量负责人 | 1 | 权限矩阵、状态机、失败审计、端到端验收脚本 |

如果只有 1 名开发者执行，建议按模块顺序串行推进，不并行修改后端和前端，避免 API DTO 尚未稳定时返工。

### 9.2 工作量估算

| 模块 | 预计工时 | 主要产出 |
|---|---:|---|
| 模块 1：数据模型与状态机 | 0.5 天 | `PendingAction`、`AuditLog`、状态机测试 |
| 模块 2：Audit Logger | 0.5 天 | 脱敏审计、trace 记录、审计测试 |
| 模块 3：Action Policy | 0.5 天 | 白名单、参数校验、风险策略 |
| 模块 4：ActionAdvisor | 0.5 天 | 从诊断报告生成 PendingAction |
| 模块 5：Action API | 1 天 | admin API、handler 测试、Swagger |
| 模块 6：ActionExecutor + Kafka | 1 天 | disabled/mock executor、operation-events、执行测试 |
| 模块 7：WebSocket 推送 | 0.5 天 | action 状态消息 |
| 模块 8：前端页面 | 1.5 天 | 审批页、详情页、审计页、诊断动作入口 |
| 模块 9：部署与联调 | 1 天 | Compose/K8s/Helm、全量验证、验收报告 |

总计：约 7 天。若同时安排前后端并行，压缩到 5 个工作日；若要求接入真实 K8s executor，则至少增加 2 到 3 天，并应移入 Phase 7。

---

## 10. 时间节点

### 第 1 天：模型、状态机、审计基础

1. 完成 `PendingAction`、`AuditLog` 模型。
2. 完成状态机和审计脱敏。
3. 验证：

```bash
cd server-monitor/server-web
go test ./model ./copilot/action
```

### 第 2 天：Policy 与 Advisor

1. 完成白名单动作和参数校验。
2. 完成从诊断报告生成 PendingAction。
3. 覆盖重复创建、高风险拒绝、非法参数。
4. 验证：

```bash
cd server-monitor/server-web
go test ./copilot/action
```

### 第 3 天：Action API

1. 完成 action / audit handler。
2. 接入 router admin 路由。
3. 覆盖 viewer 403、admin approve/reject、非法状态流转。
4. 验证：

```bash
cd server-monitor/server-web
go test ./api ./copilot/action
```

### 第 4 天：Executor、Kafka、WebSocket

1. 完成 disabled/mock executor。
2. 完成 operation event 发布。
3. 完成 action WebSocket 状态推送。
4. 验证：

```bash
cd server-monitor/server-web
go test ./copilot/action ./kafka ./websocket
```

### 第 5 天：前端审批与审计页面

1. 完成 `actions.ts`、`auditLogs.ts`。
2. 完成 `ActionsPage.vue`、`ActionDetailPage.vue`、`AuditLogsPage.vue`。
3. 修改诊断详情页和 admin 导航。
4. 验证：

```bash
cd server-monitor/frontend
npm run build
```

### 第 6 天：部署配置与接口文档

1. 同步 Compose、K8s、Helm action 配置。
2. 更新 Swagger。
3. 验证：

```bash
cd server-monitor
docker compose config
helm lint charts/server-monitor
kubectl apply --dry-run=client -f k8s/
```

### 第 7 天：端到端联调与回归

1. 跑后端全量测试和 vet。
2. 跑前端 build。
3. 联调“诊断建议 → 创建 PendingAction → approve/reject/execute → AuditLog → WebSocket”。
4. 输出验收记录和剩余风险。

---

## 11. 技术要求

### 11.1 Go 后端要求

1. 所有外部调用必须使用请求或操作上下文，不能在业务逻辑中直接使用 `context.Background()`。
2. Executor 必须有明确超时；默认不超过 30 秒。
3. 审批状态更新必须使用事务；执行前重新读取并校验状态。
4. 不新增第三方依赖；JSON 校验优先用标准库和现有项目工具。
5. 错误向外返回时必须脱敏，内部错误只写日志或审计摘要。
6. 不在 handler 堆业务逻辑，handler 只做参数绑定、用户上下文、调用 service、返回响应。
7. 不允许 panic；executor 和 Kafka/WebSocket 失败必须可恢复。
8. 不改变现有公开 API 响应结构；新增 API 遵循当前 `status/data/error` 风格。
9. 不修改 Prometheus 现有 metric 名称；新增指标命名保持 `server_web_*` 或当前 middleware 风格。
10. 不修改现有配置键语义；新增 action 配置必须有默认值和校验。

### 11.2 前端要求

1. 页面风格保持当前监控控制台风格，避免营销式 hero、装饰性卡片堆叠。
2. 审批按钮必须有二次确认、loading、disabled 和错误提示。
3. 长文本必须可换行或截断展示，不能撑破表格。
4. viewer 不能看到 admin-only 入口；即便直接访问路由也应被 router guard 拦截。
5. WebSocket 状态更新必须容忍乱序和未知 action id，不能造成页面异常。
6. 审计详情中的 request_json/result_json 应格式化展示并保持脱敏。

### 11.3 安全要求

1. LLM 推荐动作只作为输入建议，必须经过 ActionAdvisor 和 Policy 校验。
2. high risk 动作不创建 PendingAction。
3. admin 审批后执行前仍需二次校验。
4. 所有关键事件必须审计：create_pending、approve、reject、execute、denied、failed。
5. 审计日志不能包含 Secret、Token、Password、Authorization、kubeconfig。
6. 执行失败必须可追踪，不能只返回“失败”而不记录原因摘要。
7. 默认 `ACTION_EXECUTION_ENABLED=false`，防止开发环境误执行真实集群写操作。

### 11.4 部署要求

1. Compose、原生 K8s、Helm 配置必须一致。
2. `operation-events` Topic 必须在 Compose/K8s/Helm 的 Kafka 初始化中保持存在。
3. Phase 6 不新增数据库迁移工具；继续使用当前 GORM AutoMigrate 策略，除非项目后续引入 migrations。
4. 生产启用真实 executor 前必须确认 Phase 7 的 ServiceAccount、RBAC、namespace 限制和回滚策略。

---

## 12. 风险评估与应对措施

| 风险 | 影响 | 概率 | 应对措施 | 验收检查 |
|---|---|---:|---|---|
| LLM 生成危险动作 | 误创建高风险 PendingAction | 中 | Policy 白名单只允许两类 medium 动作；high risk 拒绝创建 | 高风险推荐动作测试必须 skipped |
| 非 admin 绕过审批 | 安全边界破坏 | 低 | Router admin group + handler 二次校验 + denied 审计 | viewer 调用 approve/reject/execute 返回 403 |
| 状态并发更新 | 同一 action 被重复执行 | 中 | 事务 + 行级锁 + 状态机终态保护 | 并发 execute 测试只有一次成功 |
| 审计写入失败 | 操作不可追溯 | 低 | 审计失败则关键操作返回失败；执行结果写入失败需告警 | mock audit failure 测试 |
| Kafka 发布失败 | operation-events 缺失 | 中 | MySQL 为事实来源；Kafka 失败记录日志和审计摘要 | producer error 不回滚状态 |
| WebSocket 推送失败 | 前端状态延迟 | 中 | 前端列表轮询/刷新仍能看到 MySQL 状态；推送失败只降级 | 关闭 hub 后审批仍成功 |
| disabled executor 被误判为成功 | 误以为真实执行完成 | 中 | `ACTION_EXECUTION_ENABLED=false` 时状态必须 failed，错误明确 | disabled executor 测试必须 failure |
| params_json 泄露敏感信息 | 凭据泄露 | 中 | 创建前拒绝敏感 key；审计脱敏递归处理 | 脱敏单测覆盖嵌套字段 |
| 前端误向 viewer 暴露入口 | 权限体验混乱 | 低 | router meta admin + App 导航按 `auth.isAdmin` 展示 | viewer build/manual 检查 |
| AutoMigrate 影响现有表 | 数据风险 | 低 | 仅新增表，不修改旧表字段；上线前备份 MySQL | `git diff` 确认未改旧模型结构 |
| Phase 6/7 边界混淆 | 提前引入复杂 K8s 依赖 | 中 | Phase 6 默认 disabled/mock executor，真实 K8s 深接入留给 Phase 7 | 依赖检查无新增 client-go 改动，除非已存在复用 |

---

## 13. 测试方案

### 13.1 单元测试

| 模块 | 测试重点 |
|---|---|
| Model / State Machine | 合法状态流转、终态保护、非法事件拒绝 |
| Audit Logger | 敏感字段脱敏、trace_id、审计失败 |
| Action Policy | 白名单、风险等级、replicas 范围、namespace/name 校验 |
| ActionAdvisor | 推荐动作解析、重复创建、高风险 skipped、非法 JSON |
| Repository | 创建、分页、按状态查询、事务更新 |
| Executor | 未审批拒绝、disabled failure、mock success、重复执行 |
| Handler | RBAC、请求校验、approve/reject/execute 响应 |
| Kafka Events | operation event marshal、producer failure 降级 |
| Notifier | action_pending/action_status payload |

### 13.2 集成测试

| 场景 | 测试方式 |
|---|---|
| 诊断报告生成 PendingAction | httptest + 测试 DB + mock action service |
| viewer 权限拒绝 | httptest 带 viewer JWT，上报 403 和 denied 审计 |
| admin 审批拒绝 | httptest 带 admin JWT，检查 DB 状态和 AuditLog |
| 执行失败审计 | disabled executor，检查 failed 状态和 failure 审计 |
| 执行成功审计 | mock executor，检查 executed 状态、result_json 和 operation event |

### 13.3 端到端验收

1. 登录 admin。
2. 打开某条诊断报告详情页。
3. 根据 recommended action 创建 PendingAction。
4. 在 Actions 页面看到 pending action。
5. 使用 viewer Token 调 approve，确认 403。
6. 使用 admin approve，状态变为 approved。
7. 执行 disabled executor，状态变为 failed，审计日志有 failure。
8. 创建另一条动作并 reject，状态变为 rejected，审计日志有 success。
9. 查看 AuditLogs 页面，能按 action/result/actor 筛选。
10. WebSocket 消息到达时列表状态自动刷新；刷新页面后 MySQL 状态仍一致。

### 13.4 必跑命令

后端：

```bash
cd server-monitor/server-web
go test ./...
go vet ./...
```

前端：

```bash
cd server-monitor/frontend
npm run build
```

部署：

```bash
cd server-monitor
docker compose config
helm lint charts/server-monitor
kubectl apply --dry-run=client -f k8s/
```

文档和格式：

```bash
cd /home/ayp/study/cloudops
git diff --check
```

---

## 14. 验收标准

Phase 6 完成必须同时满足以下条件：

1. `pending_actions` 和 `audit_logs` 可创建、查询、更新。
2. 诊断报告推荐动作可以生成 PendingAction，但不会自动执行。
3. 非 admin 不能创建、审批、拒绝、执行动作，也不能查看审计日志。
4. 未审批动作不能执行。
5. 拒绝动作进入 `rejected` 终态，不能再审批或执行。
6. 已审批动作执行成功进入 `executed`，执行失败进入 `failed`。
7. create/approve/reject/execute/denied/failure 均写入 AuditLog。
8. 审计内容已脱敏。
9. `operation-events` 发布失败不破坏 MySQL 状态。
10. WebSocket 推送失败不破坏审批状态。
11. 前端 admin 页面可完成审批与审计查看。
12. viewer 前端不可见审批入口，后端也返回 403。
13. `go test ./...`、`go vet ./...`、`npm run build` 通过。
14. Compose/K8s/Helm 配置保持一致。
15. Phase 6 不引入未批准的新依赖，不提前扩大到 Phase 7 的 K8s 深度能力。

---

## 15. 建议提交拆分

### 提交 1：后端模型、审计与策略

```bash
git add server-monitor/server-web/model server-monitor/server-web/copilot/action
git commit -m "feat: add action approval models and policy"
```

### 提交 2：Action API、Executor 与事件

```bash
git add server-monitor/server-web/api server-monitor/server-web/kafka server-monitor/server-web/copilot/action
git commit -m "feat: add action approval api and audit flow"
```

### 提交 3：前端审批与审计页面

```bash
git add server-monitor/frontend/src
git commit -m "feat: add action approval and audit pages"
```

### 提交 4：部署配置与文档

```bash
git add server-monitor/docker-compose.yml server-monitor/k8s server-monitor/charts/server-monitor docs/realize/realize_Phase\ 6.md
git commit -m "docs: add phase 6 action approval plan"
```

> 是否实际提交由用户确认后再执行；实施期间不要自动 `git commit`、`git push`、`git reset`、`git clean`。

---

## 16. 实施完成后的交接说明

Phase 6 结束后，项目具备完整 Human-in-the-loop 安全框架：AI 诊断可以提出中风险建议，但所有写操作都必须进入 PendingAction，由 admin 审批，执行结果和拒绝原因都可追溯。此时可以进入 Phase 7，将 Kubernetes 只读工具和真实 `restart_deployment` / `scale_deployment` executor 接入同一审批审计框架。

进入 Phase 7 前必须确认：

1. Phase 6 的 disabled/mock executor 路径已验证。
2. 审计日志不会泄露敏感信息。
3. Action Policy 的白名单和参数校验已稳定。
4. Compose/K8s/Helm 中的 action 配置一致。
5. 真实 K8s ServiceAccount、RBAC、namespace 范围和回滚策略已单独设计。
