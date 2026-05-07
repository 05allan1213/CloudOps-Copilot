# Phase 2 设计 vs 实现复核分析

> 分析日期：2026-05-07
> 依据文档：`docs/realize/realize_Phase 2.md`
> 分析范围：`server-monitor/server-web/copilot/` 全部实现
> 复核结论：原报告大方向属实，但部分条目严重度偏高或表述不准；本文已按源码复核结果修正。

---

## 一、确认属实的功能遗漏

### 1. `alert.rule_list` 工具未实现

设计文档 (4.4 节步骤 6) 明确提到：

> 为 `alert.rule_list` 创建工具实现：若 Phase 1 已实现则迁移。若 Phase 1 未实现，则在 Phase 2 中作为可选只读工具，不阻塞核心验收。

实现中完全没有 `alert.rule_list`。没有常量、没有 Schema、没有 NLU Intent。REST API `/api/v1/alert-rules` 已存在但未接入 Registry。验收标准 (第 11 节) 列表里也没有它，但设计文档正文提到它是"可选"工具——当前状态可以接受，但应明确记录为"Phase 2 未实现，不阻塞验收"。

**复核结论：属实，但不阻塞核心验收。**

### 2. `HealthCheck` 只返回注册状态

设计文档 (4.2 节) 要求：

> 对需要外部依赖的工具可返回轻量可用性状态。

实际实现 (`registry.go:167`) 只是把所有已注册工具标记为 `true`，没有检查 Prometheus 连通性、DB 连接或 AlertService 可用性。

**复核结论：属实，但应降级。** 设计文档同一节也写了"Phase 2 先检查工具是否已注册"，外部依赖检查是可选增强，不应作为高优先级 bug。

### 3. 未暴露工具 Schema 的 HTTP 端点

设计文档 (1.1 节) 提到：

> Registry 可列出工具 Schema，为后续 LLM Tool Calling、诊断和审计奠定基础。

`Registry.List()` 已实现，但没有 HTTP endpoint 暴露它。前端或外部系统无法动态发现工具。这不是 Phase 2 的硬性验收项，但是一个缺失能力。

**复核结论：属实，但不是硬验收项。** 设计要求 Registry 能列出 Schema，没有明确要求必须在 Phase 2 暴露 HTTP endpoint。

### 4. 集成测试覆盖不完整

设计文档 (8.2 节) 要求覆盖：

- `host.metrics`
- `alert.history`
- 未注册工具
- 非法参数
- 工具超时
- PromQL 合法与非法范围

实际 `integration/copilot_chat_test.go` 只测了：

- `alert.list_active`
- `host.list`
- `prom.query_range` 的正常路径

`alert.events` 虽然在 8.2 接口测试表里没有列出，但它属于 Phase 1 核心只读工具，当前也缺少 Chat API 级别回归。

**复核结论：属实，且应保持中优先级。**

### 5. `COPILOT_TOOL_LOG_ARGS` 和 `COPILOT_TOOL_DEFAULT_TIMEOUT` 配置未实现

设计文档 (10.1 节) 建议三个配置项：

- `COPILOT_TOOL_REGISTRY_ENABLED` — Go 配置层已实现
- `COPILOT_TOOL_DEFAULT_TIMEOUT` — **未实现**，默认超时硬编码为 `30s` (`registry.go:23`)
- `COPILOT_TOOL_LOG_ARGS` — **未实现**

同时，`COPILOT_TOOL_REGISTRY_ENABLED` 没有在 Docker Compose、原始 K8s ConfigMap、Helm ConfigMap 中暴露，运维配置面不完整。

**复核结论：属实。** `COPILOT_TOOL_DEFAULT_TIMEOUT` 应优先于 `COPILOT_TOOL_LOG_ARGS` 修复，因为它已经影响实际工具超时语义。

### 6. 关闭 Registry 开关后没有明确 fallback 或禁用语义

设计文档 (4.6 / 10.2 / 10.3) 要求：

> 关闭 Registry 开关后有明确降级或禁用行为。

实际 `api/router.go` 中，当 `COPILOT_TOOL_REGISTRY_ENABLED=false` 时，`tools` 保持 `nil`。`copilot.Service.executeTools` 遇到 `s.tools == nil` 会直接跳过工具执行，Chat API 仍返回成功，但回复内容是"Read-only tools will return live data in the next module." 这类占位文本。

这不是 Phase 1 fallback，也不是明确禁用 Copilot。对用户来说会表现为 Copilot 可用但不查真实数据。

**复核结论：原报告遗漏，应补为中优先级。**

---

## 二、确认属实的 Bug

### Bug 1: 工具注册失败时静默降级

**位置：** `executor.go:123-129`

```go
registry := NewRegistry()
if err := registerReadOnlyTools(registry, executor); err != nil {
    executor.registry = NewRegistry()  // 空 Registry
    return executor
}
```

如果任何一个工具注册失败（比如名称冲突），整个 Registry 变成空的，但 `NewExecutor` 不返回 error。调用方无法知道注册失败了。设计文档 (4.2 节) 要求：

> Registry 初始化失败应阻止 Copilot 启动或明确禁用 Copilot。

当前行为是静默降级——所有工具调用都会返回 `ErrToolNotFound`，但 Copilot 仍然启动。

**严重度：高。** 生产环境中注册失败会被完全掩盖，难以排查。

**建议修复：**

1. 首选把 `NewExecutor` 改为返回 `(*Executor, error)`，由 router 初始化阶段决定禁用 Copilot 或启动失败。
2. 如果暂时不改签名，至少记录结构化错误日志，并让 executor 进入明确 `ErrToolUnavailable` 状态，而不是空 Registry。

### Bug 2: 默认工具超时与设计不一致

实际涉及：

- `executor.go:35`: `defaultToolTimeout = 5 * time.Second`
- `registry.go:23`: `defaultRegistryTimeout = 30 * time.Second`
- `api/router.go`: `Options.Timeout` 使用 `cfg.RequestTimeout`
- `config.go`: `REQUEST_TIMEOUT_SECONDS` 默认 `5`

工具 Schema 中的 `Timeout` 字段被设置为 `executor.timeout`，Registry 的 `toolTimeout` 在 Schema.Timeout > 0 时使用 Schema 值。因此生产默认工具超时实际是 `REQUEST_TIMEOUT_SECONDS=5s`，不是设计文档要求的 `COPILOT_TOOL_DEFAULT_TIMEOUT=30s`。

**严重度：中。** 合法的 Prometheus、MySQL 查询可能因 5s 超时提前失败；同时设计中"未声明 timeout 的工具默认 30s"没有配置化落地。

**建议修复：**

1. 增加 `COPILOT_TOOL_DEFAULT_TIMEOUT` 配置，默认 `30s`。
2. 不再复用通用 `REQUEST_TIMEOUT_SECONDS` 作为所有 Copilot 工具默认超时。
3. 保留单工具 Schema Timeout 覆盖能力。

### Bug 3: `promQueryRangeTool.Run` 对整数 JSON 的二次解析存在边界问题

**位置：** `readonly_tools.go:108-118`

```go
var queryArgs struct {
    MaxPoints int `json:"max_points"`
}
if err := json.Unmarshal(args, &queryArgs); err != nil {
    return ToolResult{}, NewInvalidArgsError("", "must be valid JSON")
}
```

Schema 层使用 `json.Decoder.UseNumber()`，`integerValue` 会接受 JSON 数字 `1000.0`，因为它是数学意义上的整数。但 `NormalizeArgs` 重新 marshal 后，`1000.0` 仍可能以 JSON 数字形式进入 `promQueryRangeTool.Run`，随后 `json.Unmarshal` 到 Go `int` 字段会失败。

因此，原报告中"1000.0 实际没问题"的判断不准确。这个边界会导致 Schema 认为合法、工具解析阶段又失败，并且最终错误原因会被简化成 `must be valid JSON`，对用户不清晰。

**严重度：低到中。** 只影响特定输入，但会破坏"Schema 校验与执行解析一致"的设计目标。

**建议修复：**

1. 在 Normalize 阶段把 integer 规范化为整数值，而不是保留 `1000.0` 形式。
2. 或者 `promQueryRangeTool.Run` 复用 `map[string]interface{}` / `json.Number` 解析，并显式转换整数。

### Bug 4: 工具错误可能泄露内部错误细节

**位置：**

- `registry.go:errorResult`
- `executor.go:buildCall`

当前非 `ToolError` 错误会把 `err.Error()` 写入 `ToolError.Reason` 或 `ToolCall.Error`。数据库错误、Prometheus 错误、依赖连接错误可能直接出现在 Chat API 的 `tool_calls.error` 中。

设计文档要求：

> Registry 内部错误要转化为用户可理解的业务错误。

> 对外响应不能泄露敏感信息。

**严重度：中。** 当前日志参数做了 hash 和结果脱敏，但错误文本本身没有统一脱敏/映射。

**建议修复：**

1. 工具内部对外返回稳定错误码和简短原因。
2. 原始错误只进服务端日志，不进入 Chat API 响应。
3. 对 `ToolError` 保留 field/reason，对普通 error 映射为 `tool_execution: tool execution failed`。

---

## 三、低优先级问题或需要改写的原报告条目

### 1. Registry Execute 重复校验和重复 Normalize

**位置：** `registry.go:97-109`

```go
func (r *MemoryRegistry) Execute(...) {
    tool, err := r.Get(name)
    if err := r.Validate(name, args); err != nil { ... }
    normalizedArgs, err := NormalizeArgs(tool.Schema(), args)
```

`Validate` 内部会调用 `NormalizeArgs`，随后 `Execute` 又调用一次 `NormalizeArgs`，确实存在重复校验和重复解析。

但原报告中"NormalizeArgs 失败没有经过 errorResult 包装"不准确，当前代码已经在 `registry.go:106-109` 返回 `ToolResult{Success:false, Error:errorResult(err)}`。

**严重度：低。** 主要是性能和结构问题，不影响正确性。

### 2. `executorTool.Run` 错误语义不一致

**位置：** `readonly_tools.go:53-67`

底层 run 函数有两种错误表达方式：

1. 返回 `error`
2. 返回 `ToolCall{Status:"error"}` 且 `err=nil`

Registry 两条路径都能处理，功能上可用，但语义不统一，后续做错误指标、错误码统计时会增加维护成本。

**严重度：低。** 建议作为整理项，不应排在注册失败和超时配置前面。

### 3. `authorizeTool` 缺失用户上下文时降级为 viewer

**位置：** `registry.go:204-208`

原报告认为 `!ok` 时访问 `user.Role` 是问题。Go 里访问零值 struct 字段是安全的，这不是 bug。

真正需要讨论的是策略：当 context 中没有用户时，当前逻辑会 fallback 为 `viewer`。如果项目要求"缺失认证上下文必须拒绝"，这里应返回 `permission_denied`；如果允许内部调用默认 viewer，则当前行为可接受。

**严重度：低 / 需要产品策略确认。**

---

## 四、修复优先级建议

### P1：建议优先修复

1. 工具注册失败静默降级。
2. `COPILOT_TOOL_DEFAULT_TIMEOUT` 未实现，且工具默认超时实际为 5s。
3. 关闭 `COPILOT_TOOL_REGISTRY_ENABLED` 后没有明确 fallback / 禁用语义。
4. Chat API 集成测试缺少 `host.metrics`、`alert.events`、`alert.history`、非法参数、未注册工具、timeout。
5. 工具错误响应可能泄露内部错误细节。

### P2：后续补齐

1. `COPILOT_TOOL_REGISTRY_ENABLED` 在 Compose / K8s / Helm 配置面暴露。
2. `prom.query_range max_points=1000.0` 这类整数规范化边界。
3. `COPILOT_TOOL_LOG_ARGS` 配置。
4. `HealthCheck` 增加轻量依赖检查。
5. `Registry.List()` 的 HTTP 诊断端点。
6. `executorTool.Run` 错误语义统一。

### P3：可记录但不阻塞

1. `alert.rule_list` 未实现：设计允许 Phase 2 不实现，但应在验收记录中说明。
2. `authorizeTool` 缺失用户上下文时是否默认 viewer：需要确认策略后再改。

---

## 五、总结

| 类别 | 项目 | 严重度 | 复核结论 |
|---|---|---|---|
| Bug | 注册失败静默降级 | 高 | 属实，优先修 |
| Bug | 默认工具超时实际 5s，不符合 30s 设计 | 中 | 属实，优先修 |
| Bug | 关闭 Registry 开关后无明确 fallback / 禁用 | 中 | 原报告遗漏 |
| Bug | 工具错误可能泄露内部错误细节 | 中 | 原报告遗漏 |
| 测试遗漏 | Chat API 集成测试覆盖不足 | 中 | 属实 |
| 配置遗漏 | `COPILOT_TOOL_DEFAULT_TIMEOUT` 未实现 | 中 | 属实 |
| 配置遗漏 | Registry 开关未暴露到 Compose / K8s / Helm | 中 | 原报告遗漏 |
| Bug | `max_points=1000.0` 可能通过 Schema 但工具解析失败 | 低-中 | 原报告判断不准 |
| 遗漏 | `COPILOT_TOOL_LOG_ARGS` 未实现 | 低 | 属实 |
| 遗漏 | `HealthCheck` 只返回注册状态 | 低 | 属实但可降级 |
| 遗漏 | 无工具 Schema HTTP 端点 | 低 | 属实但非硬验收 |
| 遗漏 | `alert.rule_list` 未实现 | 低 | 属实但设计允许可选 |
| 结构问题 | Execute 重复校验 / normalize | 低 | 属实但不影响正确性 |
| 结构问题 | executorTool 错误语义不一致 | 低 | 属实但不阻塞 |
| 策略问题 | 缺失用户上下文默认 viewer | 待确认 | 原报告表述不准 |

**建议优先修复**：注册失败静默降级、默认工具超时配置、关闭 Registry 开关后的行为、Chat API 集成测试缺口。其余项可以作为后续增强或策略确认项。
