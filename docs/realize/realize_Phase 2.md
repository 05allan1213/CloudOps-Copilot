# CloudOps Copilot Phase 2 实施方案

> 方案版本：v1.0
> 制定日期：2026-05-07
> 依据文档：`docs/design.md` v3.1
> 阶段定位：Tool Registry 工具注册中心，替换 Phase 1 轻量工具适配和硬编码分发，实现统一注册、校验、超时、权限、Trace、脱敏、日志和健康检查。

---

## 1. 阶段目标

Phase 2 的目标是在 Phase 1 已经具备 Copilot Chat API 与只读工具闭环的基础上，将工具执行能力从临时适配层升级为正式 `ToolRegistry`。升级后，LLM、NLU 和 Decision Engine 不再直接依赖工具 `switch-case` 或具体 service，而是通过注册中心发现工具、校验参数、执行工具并记录调用结果。

本阶段完成后，系统应具备以下能力：

1. 所有 Phase 1 只读工具统一实现 `Tool` 接口。
2. Copilot 对话链路通过 `ToolRegistry.Execute` 执行工具。
3. 未注册工具不能被执行。
4. 工具参数错误能返回清晰、可前端展示的错误。
5. 每个工具拥有独立 timeout、风险等级、只读标识和参数 Schema。
6. 工具调用自动接入权限检查、Trace、脱敏和调用日志。
7. Registry 可列出工具 Schema，为后续 LLM Tool Calling、诊断和审计奠定基础。

### 1.1 核心交付物

| 交付物 | 内容 | 验收标准 |
|---|---|---|
| `Tool` 接口 | 定义统一工具元数据、Schema 和执行协议 | Phase 1 工具均实现接口 |
| `ToolRegistry` | 支持注册、查询、列表、校验、执行、健康检查 | 未注册工具执行被拒绝 |
| 参数 Schema 校验 | 支持必填、类型、枚举、默认值、范围校验 | 参数错误返回明确字段和原因 |
| 工具迁移 | 将 Phase 1 只读工具迁移到 Registry | Chat API 行为保持兼容 |
| 权限与风险控制 | viewer 只能执行只读工具；写工具预留但不开放 | 非只读工具在 Phase 2 默认拒绝 |
| 观测与日志 | 工具调用记录 duration、success、error_type、args_hash | 日志不包含明文敏感参数 |
| 测试套件 | 单元测试、接口回归、超时与错误场景 | `go test` 覆盖 Registry 核心路径 |

### 1.2 本阶段不做

1. 不新增 Kubernetes 工具。
2. 不新增 Runbook / RAG 检索工具。
3. 不实现告警诊断报告。
4. 不实现异步 Diagnosis Worker。
5. 不实现 PendingAction、审批、审计落库和写操作执行。
6. 不开放任何真实写操作工具。
7. 不引入 LangChain、复杂 Agent 框架或向量数据库。
8. 不改变 Phase 1 Chat API 的请求和响应结构，除非只是兼容性增加字段。

---

## 2. 范围边界

### 2.1 输入前提

Phase 2 依赖 Phase 1 已完成以下能力：

| 前提 | 说明 |
|---|---|
| Copilot Chat API | `POST /api/v1/copilot/chat` 可用 |
| NLU 基础意图识别 | 能将用户输入映射到只读工具调用 |
| 只读工具适配层 | 已具备 `host.list`、`host.metrics`、`alert.list_active` 等工具逻辑 |
| Redis 会话 | 会话读写和 TTL 已可用 |
| JWT / RBAC | Copilot API 已接入登录态 |
| Trace / Logger | server-web 已有请求日志和链路追踪基础 |

如果 Phase 1 尚未完全实现，Phase 2 可以先以 mock 或现有 service 适配方式开发 Registry，但最终验收必须跑通真实 Phase 1 工具迁移。

### 2.2 Phase 2 只处理的工具

| 工具名 | 迁移目标 | 风险等级 | 只读 |
|---|---|---|---|
| `host.list` | 主机列表查询 | low | 是 |
| `host.metrics` | 主机趋势指标查询 | low | 是 |
| `alert.list_active` | 活跃告警查询 | low | 是 |
| `alert.events` | 告警事件流查询 | low | 是 |
| `alert.history` | 告警历史查询 | low | 是 |
| `alert.rule_list` | 告警规则查询，若 Phase 1 已实现则迁移 | low | 是 |
| `prom.query_range` | 受控范围 PromQL 查询 | medium | 是 |

`prom.query_range` 虽为只读工具，但可能拖慢 Prometheus，因此风险等级设为 `medium`，需要更严格的范围、点数和超时限制。

### 2.3 对外兼容要求

1. `POST /api/v1/copilot/chat` 请求结构不变。
2. 原有 `tool_calls` 响应字段保持兼容。
3. 前端可继续展示工具调用状态。
4. 工具错误不能变成 HTTP 500，除非是系统级不可恢复错误。
5. Registry 内部错误要转化为用户可理解的业务错误。

---

## 3. 总体实施路径

Phase 2 拆为 6 个小模块，每个模块可独立测试、独立提交。

```text
模块 1：定义 Tool 契约和错误模型
  ↓
模块 2：实现 ToolRegistry 核心能力
  ↓
模块 3：实现参数 Schema 校验与默认值注入
  ↓
模块 4：迁移 Phase 1 只读工具
  ↓
模块 5：接入权限、Trace、脱敏和调用日志
  ↓
模块 6：联调、回归、灰度与验收
```

---

## 4. 详细实施步骤

### 4.1 模块 1：定义 Tool 契约和错误模型

**目标：** 建立稳定的工具接口，避免后续工具继续散落在 handler 或 service 中。

**实施步骤：**

1. 在 `server-web/copilot/tool` 下定义核心接口：

```go
type Tool interface {
    Name() string
    Description() string
    Schema() ToolSchema
    Run(ctx context.Context, args json.RawMessage) (ToolResult, error)
}
```

2. 定义 `ToolSchema`：
   - `name`
   - `description`
   - `parameters`
   - `risk_level`
   - `read_only`
   - `timeout`
3. 定义 `ParamSchema`：
   - `name`
   - `type`
   - `required`
   - `description`
   - `enum`
   - `default`
   - `min`
   - `max`
   - `pattern`
4. 定义 `ToolResult`：
   - `success`
   - `data`
   - `error`
   - `duration`
   - `metadata`
5. 定义错误类型：
   - `ErrToolNotFound`
   - `ErrInvalidArgs`
   - `ErrPermissionDenied`
   - `ErrToolTimeout`
   - `ErrToolExecution`
6. 定义统一错误响应映射，避免工具内部错误直接穿透到 HTTP 响应。

**技术要求：**

1. 接口和结构体字段使用 JSON tag。
2. 错误类型可用 `errors.Is` / `errors.As` 判断。
3. 不在 `Tool` 接口里引入 HTTP、Gin 或前端概念。
4. 不把具体 service 依赖写入接口定义。

**验收标准：**

1. Tool 契约可被 mock 工具实现。
2. 错误类型有单元测试覆盖。
3. Copilot service 能依赖接口而不是具体工具实现。

### 4.2 模块 2：实现 ToolRegistry 核心能力

**目标：** 提供统一注册、查询、列表、校验、执行和健康检查入口。

**实施步骤：**

1. 定义 Registry 接口：

```go
type Registry interface {
    Register(tool Tool) error
    Get(name string) (Tool, error)
    List() []ToolSchema
    Validate(name string, args json.RawMessage) error
    Execute(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
    HealthCheck(ctx context.Context) map[string]bool
}
```

2. 实现内存注册表：
   - 使用 `map[string]Tool` 保存工具。
   - 注册时校验工具名非空。
   - 工具名重复注册直接返回错误。
   - `List` 按工具名稳定排序，保证测试和前端展示稳定。
3. 在 Copilot 初始化流程中注册 Phase 1 工具。
4. 将原有工具调用入口改为 `registry.Execute`。
5. 实现 `HealthCheck`：
   - Phase 2 先检查工具是否已注册。
   - 对需要外部依赖的工具可返回轻量可用性状态。
   - 不在健康检查中执行昂贵 PromQL 或数据库扫描。

**技术要求：**

1. Registry 初始化失败应阻止 Copilot 启动或明确禁用 Copilot。
2. `Register` 不允许覆盖已有工具。
3. Registry 本身并发读安全。
4. 工具执行不得持有 Registry 写锁。

**验收标准：**

1. 重复注册返回错误。
2. 未注册工具执行返回 `ErrToolNotFound`。
3. `List` 能返回完整工具 Schema。
4. Chat API 不再直接调用旧 switch-case 分发。

### 4.3 模块 3：参数 Schema 校验与默认值注入

**目标：** 保证所有工具执行前完成统一参数校验，阻断 LLM 或用户构造的非法调用。

**实施步骤：**

1. 实现基础类型校验：
   - `string`
   - `number`
   - `integer`
   - `boolean`
   - `array`
   - `object`
2. 实现必填校验：
   - `required=true` 且字段缺失时返回明确错误。
3. 实现枚举校验：
   - 如 `severity` 只能为 `critical/warning/info`。
   - 如 `window` 只能为 `15m/1h/6h/24h`。
4. 实现默认值注入：
   - `alert.events.count` 默认 20。
   - `alert.history.page` 默认 1。
   - `alert.history.page_size` 默认 20。
   - `prom.query_range.max_points` 默认 1000。
5. 实现范围校验：
   - `count <= 100`。
   - `page_size <= 100`。
   - `max_points <= 1000`。
6. 实现字符串模式校验：
   - `instance` 基础长度限制。
   - `query` 禁止明显危险 PromQL 模式。
7. 返回标准化校验错误：

```json
{
  "error": "invalid_args",
  "field": "window",
  "reason": "must be one of 15m, 1h, 6h, 24h"
}
```

**PromQL 特殊校验：**

1. `end - start <= 7d`。
2. `step >= 15s`。
3. `(end - start) / step <= max_points`。
4. `max_points <= 1000`。
5. 禁止 `offset` 超过 7 天。
6. 禁止子查询。
7. 禁止 `__internal_` 前缀指标。

**技术要求：**

1. 优先使用标准库和现有依赖，不为简单校验新增大型 JSON Schema 依赖。
2. 校验逻辑应可单元测试，不依赖 Gin。
3. 默认值注入后，工具收到的是规范化参数。

**验收标准：**

1. 参数缺失、类型错误、枚举错误均能被拦截。
2. 默认值注入可被测试验证。
3. PromQL 越界查询不会进入 Prometheus Client。
4. 错误消息对前端和用户可理解。

### 4.4 模块 4：迁移 Phase 1 只读工具

**目标：** 将 Phase 1 工具从轻量适配或 switch-case 迁移到正式 Registry。

**实施步骤：**

1. 为 `host.list` 创建工具实现：
   - 参数：`status, search, sort, risk, group_id`。
   - 调用来源：现有 `host.Service`。
2. 为 `host.metrics` 创建工具实现：
   - 参数：`instance, window`。
   - 调用来源：`host.Service` + `prometheus.Client`。
3. 为 `alert.list_active` 创建工具实现：
   - 参数：`severity`。
   - 调用来源：Redis `alert:active` 或 alert service。
4. 为 `alert.events` 创建工具实现：
   - 参数：`count`。
   - 调用来源：Redis Stream `alert:events`。
5. 为 `alert.history` 创建工具实现：
   - 参数：`status, severity, alert_name, instance, page, page_size`。
   - 调用来源：MySQL `AlertHistory`。
6. 为 `alert.rule_list` 创建工具实现：
   - 若 Phase 1 已实现则迁移。
   - 若 Phase 1 未实现，则在 Phase 2 中作为可选只读工具，不阻塞核心验收。
7. 为 `prom.query_range` 创建工具实现：
   - 参数：`query, start, end, step, max_points`。
   - 调用来源：现有 Prometheus Client。
8. 修改 Decision Engine / Copilot Service：
   - 意图到工具名的映射保持不变。
   - 执行路径改为 `registry.Execute`。
   - `tool_calls` 响应从 Registry 执行结果生成。

**迁移策略：**

1. 先保留旧工具实现，新增 Registry 包装器。
2. 单个工具迁移、单个工具测试。
3. 全部迁移后删除旧 switch-case 分发。
4. 不删除仍被其他模块使用的 service 代码。

**验收标准：**

1. Phase 1 所有核心只读工具可通过 Registry 执行。
2. Chat API 典型问题返回内容与 Phase 1 兼容。
3. 旧 switch-case 分发被移除或仅作为受控 fallback，且有明确 TODO 计划不得长期保留。

### 4.5 模块 5：接入权限、Trace、脱敏和调用日志

**目标：** 将 Tool Registry 从“能执行”升级为“可控、可观测、可审计前置”。

**实施步骤：**

1. 权限检查：
   - 从 `context.Context` 获取当前用户和角色。
   - `viewer` 只能执行 `read_only=true` 的工具。
   - `admin` 在 Phase 2 也只允许执行已注册只读工具。
   - `read_only=false` 工具即使注册也默认拒绝执行。
2. 风险等级：
   - `low`：主机、告警列表等轻量查询。
   - `medium`：`prom.query_range` 等可能消耗资源的只读查询。
   - `high`：Phase 2 不开放。
3. Timeout 控制：
   - 每个工具从 Schema 读取 timeout。
   - 未设置时默认 30 秒。
   - `prom.query_range` 默认 30 秒。
   - LLM timeout 不由 Registry 控制，保持 Copilot service 独立配置。
4. Trace 注入：
   - `Execute` 生成 tool span。
   - span 记录 tool_name、risk_level、read_only、success、duration。
   - 不记录明文参数。
5. 参数脱敏：
   - 对 `secret`、`token`、`password`、`authorization`、`api_key` 字段脱敏。
   - 日志和 ToolResult metadata 不保留敏感明文。
6. 调用日志：
   - 记录 `tool_name`。
   - 记录 `args_hash`。
   - 记录 `duration_ms`。
   - 记录 `success`。
   - 记录 `error_type`。
   - 记录 `trace_id`。
7. 可选指标：
   - `copilot_tool_calls_total`
   - `copilot_tool_call_duration_seconds`
   - `copilot_tool_call_errors_total`

**技术要求：**

1. 日志不得包含完整 PromQL 以外的敏感参数；PromQL 可记录 hash 和长度。
2. 权限失败返回 `permission_denied`，不伪装为工具不存在。
3. timeout 失败返回 `tool_timeout`，可由前端展示。
4. Trace 失败不能影响工具执行。

**验收标准：**

1. viewer 执行只读工具成功。
2. 非只读工具即使注册也无法执行。
3. 工具 timeout 有测试覆盖。
4. 日志中无敏感字段明文。
5. Trace span 可在本地日志或 Jaeger 中关联请求。

### 4.6 模块 6：联调、回归、灰度与验收

**目标：** 确认 Registry 替换不会破坏 Phase 1 的 ChatOps 对话体验和现有监控告警链路。

**实施步骤：**

1. 添加 Registry 开关：
   - `COPILOT_TOOL_REGISTRY_ENABLED=true`。
   - 默认启用。
   - 如需灰度，可保留旧路径 fallback，但不建议长期并存。
2. 执行后端单元测试：
   - Registry 注册与查询。
   - Schema 校验。
   - 权限控制。
   - timeout。
   - 脱敏。
3. 执行 Chat API 接口测试：
   - 查询活跃告警。
   - 查询主机列表。
   - 查询主机指标。
   - 查询告警历史。
   - 非法工具名。
   - 非法参数。
4. 执行现有功能回归：
   - 登录。
   - 主机列表。
   - Dashboard。
   - 活跃告警。
   - WebSocket。
   - Alertmanager Webhook。
5. 记录验收结果：
   - 已执行并通过。
   - 已执行但失败。
   - 未执行及原因。

**验收标准：**

1. Chat API 所有工具调用均通过 Registry。
2. 未注册工具无法执行。
3. 非法参数不会进入真实数据源。
4. 现有 server-monitor 功能不回退。
5. 关闭 Registry 开关后有明确降级或禁用行为。

---

## 5. 资源分配

### 5.1 人员角色

| 角色 | 人数 | 职责 |
|---|---:|---|
| 后端开发 | 1 | Tool 接口、Registry、Schema 校验、工具迁移、权限和日志 |
| 测试/验证 | 1 | Registry 单元测试、Chat API 回归、异常场景测试 |
| DevOps 支持 | 0.5 | 配置开关、日志/Trace 验证、本地 Compose 回归 |
| 前端开发 | 0.5 | 适配工具错误展示和工具 Schema 展示需求 |
| 项目负责人 | 0.5 | 控制 Phase 2 范围，确认验收和风险关闭 |

如果单人执行，建议顺序为：接口契约 → Registry → Schema 校验 → 工具迁移 → 观测能力 → 回归验证。不要先做可视化展示，以免 Registry 契约反复变动。

### 5.2 基础设施资源

| 资源 | 用途 | 要求 |
|---|---|---|
| Redis | 活跃告警、事件、会话、限流 | 复用现有实例 |
| MySQL | 告警历史、告警规则 | Phase 2 不新增表 |
| Prometheus | `prom.query_range` 工具 | 必须限制查询范围 |
| Jaeger / OTel | 工具调用 Trace | 复用现有链路追踪 |
| Docker Compose | 本地回归 | 不新增服务 |
| LLM Provider | 不属于 Registry 核心 | Phase 2 不调整 LLM 协议 |

---

## 6. 时间节点

以 2026-05-22 作为 T0 启动日估算，Phase 2 建议用 8 个工作日完成。若 Phase 1 完成时间调整，可按 T+N 平移。

| 时间 | 工作日 | 里程碑 | 交付结果 |
|---|---:|---|---|
| 2026-05-22 | T+1 | 契约设计 | `Tool`、`ToolSchema`、`ToolResult`、错误模型完成 |
| 2026-05-25 | T+2 | Registry 核心 | 注册、查询、列表、执行骨架完成 |
| 2026-05-26 | T+3 | Schema 校验 | 必填、类型、枚举、默认值、范围校验完成 |
| 2026-05-27 | T+4 | 工具迁移第一批 | `host.list`、`alert.list_active`、`alert.events` 迁移 |
| 2026-05-28 | T+5 | 工具迁移第二批 | `host.metrics`、`alert.history`、`prom.query_range` 迁移 |
| 2026-05-29 | T+6 | 权限与观测 | RBAC、timeout、Trace、脱敏、调用日志完成 |
| 2026-06-01 | T+7 | 联调与回归 | Chat API、前端工具状态、现有功能回归完成 |
| 2026-06-02 | T+8 | 验收收口 | 测试记录、风险关闭、提交拆分建议完成 |

### 6.1 关键评审点

| 评审点 | 时间 | 通过条件 |
|---|---|---|
| 契约评审 | T+1 结束 | Tool 接口不依赖 HTTP/Gin，不绑定具体 service |
| 校验评审 | T+3 结束 | 非法工具参数能被统一拦截 |
| 迁移评审 | T+5 结束 | Phase 1 核心工具全部走 Registry |
| 发布前评审 | T+8 结束 | 回归通过，旧 switch-case 已删除或明确禁用 |

---

## 7. 技术要求

### 7.1 Registry 设计要求

1. Registry 必须是进程内组件，不新增微服务。
2. Registry 不直接依赖 Gin。
3. 工具名必须全局唯一。
4. 工具注册失败必须显式返回错误。
5. 工具列表输出顺序稳定。
6. 工具执行必须统一经过校验、权限、timeout、日志流程。
7. 工具内部错误要包装上下文，但不能泄露敏感实现细节。
8. Registry 应支持单元测试中的 mock tool。
9. Registry 不负责 LLM Prompt 拼装，只提供工具 Schema 和执行结果。
10. Registry 不负责审批和审计落库，审批审计属于 Phase 6。

### 7.2 参数校验要求

1. 所有工具参数必须声明 Schema。
2. 必填参数缺失必须拒绝执行。
3. 类型错误必须拒绝执行。
4. 枚举值错误必须拒绝执行。
5. 默认值只能用于非必填参数。
6. 范围限制必须在调用真实数据源前完成。
7. 校验错误必须包含字段名和原因。
8. PromQL 安全校验必须独立测试。

### 7.3 权限与安全要求

1. Phase 2 只允许只读工具真实执行。
2. `read_only=false` 的工具默认拒绝，即使调用者是 admin。
3. viewer 可执行只读工具。
4. admin 与 viewer 在 Phase 2 的真实可执行工具范围一致。
5. 工具结果返回前必须脱敏。
6. 日志记录参数 hash，不记录完整敏感参数。
7. Tool Schema 不得包含真实密钥、token 或内部地址。

### 7.4 可观测性要求

1. 每次工具调用必须记录 duration。
2. 每次工具调用必须记录 success/failure。
3. 每次工具调用失败必须分类。
4. 每次工具调用应关联 trace_id。
5. OTel span 名称建议为 `copilot.tool.<tool_name>`。
6. Prometheus 指标可选，但建议预留：
   - `copilot_tool_calls_total`
   - `copilot_tool_call_duration_seconds`
   - `copilot_tool_call_errors_total`

### 7.5 兼容性要求

1. Chat API 响应结构保持兼容。
2. 前端无需因为 Registry 替换而大改。
3. Phase 1 工具名保持不变。
4. 旧工具参数含义保持不变。
5. 错误响应可以更清晰，但不能变成不可解析的纯文本。

---

## 8. 测试方案

### 8.1 单元测试

| 模块 | 测试内容 | Mock 方式 |
|---|---|---|
| Tool 契约 | mock tool 实现、Schema 输出、Run 返回 | 自定义 fake tool |
| Registry 注册 | 空名、重复名、正常注册、稳定 List | 内存 Registry |
| Registry 执行 | 未注册工具、成功工具、失败工具 | fake tool |
| Schema 校验 | 必填、类型、枚举、默认值、范围、pattern | 表驱动测试 |
| Timeout | 工具阻塞超过 timeout | fake slow tool |
| 权限 | viewer/admin/read_only=false | fake user context |
| 脱敏 | secret/token/password/api_key 字段 | 构造参数和结果 |
| PromQL Guard | 范围、step、点数、offset、subquery | 表驱动测试 |

### 8.2 接口测试

| 场景 | 验证点 |
|---|---|
| 查询活跃告警 | NLU → Registry → `alert.list_active` |
| 查询主机列表 | NLU → Registry → `host.list` |
| 查询主机指标 | NLU → Registry → `host.metrics` |
| 查询告警历史 | NLU → Registry → `alert.history` |
| 查询 PromQL | Registry 拦截非法范围，合法查询正常执行 |
| 未注册工具 | 返回 `tool_not_found` |
| 非法参数 | 返回 `invalid_args`，不访问真实数据源 |
| 工具超时 | 返回 `tool_timeout`，请求可恢复 |

### 8.3 回归测试

| 现有能力 | 验证方式 |
|---|---|
| 登录认证 | `/api/v1/auth/login`、`/api/v1/auth/me` |
| 主机列表 | `/api/v1/hosts` |
| Dashboard | `/api/v1/dashboard/overview` |
| 活跃告警 | `/api/v1/alerts/active` |
| 告警事件 | `/api/v1/alerts/events` |
| 告警历史 | `/api/v1/alert-histories` |
| Alertmanager Webhook | `POST /api/v1/webhook/alertmanager` |
| WebSocket | `/ws/alerts` 握手和消息推送 |
| Readyz | `/readyz`、`/readyz/full` |

### 8.4 必须执行的验证命令

后端改动完成后：

```bash
goimports -w <本次修改的 Go 文件>
go test ./...
go vet ./...
```

如果改动涉及 Compose 或 Helm：

```bash
docker compose config
helm lint <chart-path>
kubectl apply --dry-run=client -f <manifest>
```

如果本地缺少 `helm`、`kubectl` 或运行环境不可用，必须在验收记录中明确写明未执行原因。

---

## 9. 风险评估与应对措施

| 风险 | 概率 | 影响 | 应对措施 | 责任角色 |
|---|---|---|---|---|
| Registry 替换导致 Chat API 回归 | 中 | 高 | 保持工具名和响应结构兼容；逐工具迁移；接口回归 | 后端 |
| Schema 校验过严导致合法请求失败 | 中 | 中 | 用 Phase 1 真实请求样例建立回归集；默认值策略明确 | 后端/测试 |
| Schema 校验过松导致危险 PromQL 进入数据源 | 中 | 高 | PromQL Guard 独立模块和表驱动测试 | 后端 |
| 工具 timeout 设置不合理 | 中 | 中 | 默认 30s；PromQL 单独限制；压测后调整 | 后端 |
| 权限上下文缺失导致误拒绝 | 中 | 中 | user context helper 统一获取；缺失时返回明确错误 | 后端 |
| 敏感参数进入日志 | 中 | 高 | 参数 hash、脱敏黑名单、日志测试 | 后端/测试 |
| 并发注册或执行出现竞态 | 低 | 高 | 初始化阶段注册；运行期只读；必要时 `go test -race` | 后端 |
| 旧 switch-case 和新 Registry 双路径不一致 | 中 | 中 | 灰度期短暂保留；验收前删除或禁用旧路径 | 项目负责人 |
| Phase 2 范围膨胀到诊断/审批 | 中 | 高 | 坚持不做清单；只做工具框架升级 | 项目负责人 |
| 可观测性埋点影响业务执行 | 低 | 中 | Trace/指标失败不影响工具执行 | 后端 |

---

## 10. 发布与灰度策略

### 10.1 灰度开关

建议新增或复用配置：

| 配置名 | 默认值 | 用途 |
|---|---|---|
| `COPILOT_TOOL_REGISTRY_ENABLED` | `true` | 是否启用 Registry 执行路径 |
| `COPILOT_TOOL_DEFAULT_TIMEOUT` | `30s` | 未声明 timeout 的工具默认超时 |
| `COPILOT_TOOL_LOG_ARGS` | `false` | 是否记录明文参数，默认禁止 |

`COPILOT_TOOL_LOG_ARGS` 即使开启，也不得记录 `secret/token/password/authorization/api_key` 等字段。

### 10.2 灰度步骤

1. 在本地启用 Registry。
2. 用 mock 工具完成 Registry 单测。
3. 迁移一批低风险工具：
   - `host.list`
   - `alert.list_active`
   - `alert.events`
4. 对比 Phase 1 与 Phase 2 响应结构。
5. 迁移剩余工具。
6. 开启默认 Registry 路径。
7. 删除或禁用旧 switch-case。

### 10.3 回滚方案

1. 配置级回滚：
   - 将 `COPILOT_TOOL_REGISTRY_ENABLED=false`。
   - 临时回到 Phase 1 工具路径。
2. 功能级回滚：
   - 保留 Registry 代码，禁用部分问题工具注册。
3. 版本级回滚：
   - 回退到 Phase 1 镜像。
   - Registry 不涉及数据迁移，无数据库回滚负担。

---

## 11. 验收标准

Phase 2 完成必须同时满足以下条件：

1. `Tool` 接口、`ToolSchema`、`ParamSchema`、`ToolResult` 已实现。
2. `ToolRegistry` 支持注册、查询、列表、校验、执行和健康检查。
3. Phase 1 核心只读工具均已迁移到 Registry：
   - `host.list`
   - `host.metrics`
   - `alert.list_active`
   - `alert.events`
   - `alert.history`
   - `prom.query_range`
4. 未注册工具返回清晰错误，不能执行。
5. 参数错误返回清晰错误，不能进入真实数据源。
6. 工具 timeout 生效。
7. `read_only=false` 工具在 Phase 2 默认拒绝执行。
8. 工具调用日志包含 `tool_name`、`args_hash`、`duration_ms`、`success`、`error_type`。
9. 日志和 ToolResult 不泄露敏感字段。
10. Chat API 响应结构与 Phase 1 兼容。
11. 现有监控、告警、WebSocket、认证链路不回退。
12. 测试结果明确记录：
    - 已执行并通过。
    - 已执行但失败。
    - 未执行及原因。

---

## 12. 建议提交拆分

Phase 2 建议按以下粒度拆分提交：

```bash
git add server-monitor/server-web/copilot/tool
git commit -m "feat: add copilot tool registry contract"

git add server-monitor/server-web/copilot/tool
git commit -m "feat: implement copilot tool registry validation"

git add server-monitor/server-web/copilot/tool server-monitor/server-web/copilot/service
git commit -m "feat: migrate copilot readonly tools to registry"

git add server-monitor/server-web/copilot
git commit -m "feat: add copilot tool execution observability"

git add server-monitor/server-web/copilot
git commit -m "test: add copilot tool registry coverage"
```

实际提交文件以最终改动为准。提交前必须执行 `git status`，只加入 Phase 2 相关文件，避免误提交无关工作区变更。

---

## 13. Phase 2 完成后的下一步

Phase 2 验收通过后，进入 Phase 3：告警诊断报告。

Phase 3 将在 Tool Registry 之上实现 EvidenceCollector、RuleAnalyzer 和 LLMSummarizer，并新增 `DiagnosisReport` 模型和 `POST /api/v1/diagnosis`。因此 Phase 2 的关键价值不是新增更多工具，而是把工具执行链路变成安全、可测、可观测、可扩展的底座。
