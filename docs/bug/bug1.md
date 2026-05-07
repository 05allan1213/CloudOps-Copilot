# Phase 1 设计文档 vs 实现 — 复核后审查报告

> 审查日期：2026-05-07
> 复核日期：2026-05-07
> 依据文档：`docs/realize/realize_Phase 1.md`
> 审查范围：`server-web/copilot/` 后端代码、`frontend/src/` 前端代码、路由与配置

---

## 整体结论

6 个模块核心功能已落地：4 条路由、Redis 会话管理、7 种意图 NLU、LLM 兜底、6 个只读工具、前端对话页面、8 项配置均已实现。单元测试覆盖了 handler/session/NLU/LLM/tool 主要路径，路由级测试已覆盖 Copilot 未认证访问 401。

本次复核结论：

1. 原报告中的后端参数类问题大多真实存在。
2. 原 `Bug-4 前端菜单入口缺失` 与当前代码不符，已从有效 Bug 中移除。
3. 原 `测试-1 Handler 测试缺少 401 场景` 应降级为 handler 层测试粒度缺口；路由级 401 已覆盖。
4. 新增确认 3 个遗漏：`alert.events` 状态过滤、`alert.history` 状态过滤、`host.list` 参数能力不完整。

---

## Bug（影响功能正确性）

### Bug-1: `containsDangerousQuery` 误杀合法 PromQL 指标

- **文件**: `server-web/copilot/tool/executor.go:332-339`
- **严重程度**: P1
- **描述**: `containsDangerousQuery` 函数中的 `"process_"` 模式会拦截 Prometheus 标准指标如 `process_cpu_seconds_total`、`process_resident_memory_bytes`。用户查询 `promql: process_cpu_seconds_total` 会被错误拒绝。
- **设计文档依据**: 4.4 节要求"禁止明显危险或内部查询模式"，但 `process_` 开头的指标是合法的用户级指标。
- **建议修复**: 移除 `process_` 模式，改为更精确的黑名单（如 `process_cmdline`、`process_max_fds` 等真正敏感的指标），或改用白名单模式只允许 `server_monitor_*` 等业务指标。

### Bug-2: `alert.events` 忽略用户指定的 count

- **文件**: `server-web/copilot/tool/executor.go:188`
- **严重程度**: P2
- **描述**: 实现中硬编码了 `events, err := e.alertService.AlertEvents(toolCtx, 20, "", ...)`，设计文档要求 `count` 参数默认 20、最大 100。NLU 也没有抽取 `count` 实体。
- **设计文档依据**: 4.4 节工具清单中 `alert.events` 参数包含 `count`，"count 默认 20，最大 100"。
- **建议修复**:
  1. NLU 增加 `count` 实体抽取（识别"最近 50 条"等表达）。
  2. LLM `isAllowedEntity` 白名单增加 `count`。
  3. `runAlertEvents` 从 entities 读取 count 并校验范围。

### Bug-3: `alert.history` 不支持分页参数

- **文件**: `server-web/copilot/tool/executor.go:195-228`
- **严重程度**: P2
- **描述**: `pageSize` 硬编码为 `20`，`Page` 固定为 `1`，不接受外部传入 `page` 和 `page_size` 参数。
- **设计文档依据**: 4.4 节工具清单中 `alert.history` 参数包含 `page`、`page_size`，"page_size 最大 100"。
- **建议修复**:
  1. NLU 增加 `page`、`page_size` 实体抽取。
  2. LLM `isAllowedEntity` 白名单增加 `page`、`page_size`。
  3. `runAlertHistory` 从 entities 读取分页参数并校验范围（page_size 最大 100）。

---

## 功能遗漏（设计文档要求但未实现）

### 遗漏-1: `count` 和 `page`/`page_size` 实体抽取缺失

- **文件**: `server-web/copilot/nlu/nlu.go`
- **严重程度**: P2（与 Bug-2、Bug-3 关联）
- **描述**: NLU 的 `extractEntities` 没有抽取 `count`（用于 alert.events）和 `page`/`page_size`（用于 alert.history）。设计文档的工具参数表中明确列出了这些参数。
- **建议修复**: 在 `extractEntities` 中增加对数字实体的抽取逻辑，识别"最近 50 条"、"第 2 页"等表达。

### 遗漏-2: `alert_name` 实体未实现

- **文件**: `server-web/copilot/nlu/nlu.go`、`server-web/copilot/llm/client.go`、`server-web/copilot/tool/executor.go`
- **严重程度**: P1
- **描述**:
  - NLU 没有抽取 `alert_name` 实体。
  - LLM 的 `isAllowedEntity` 白名单（`llm/client.go:186-192`）不包含 `alert_name`。
  - `runAlertHistory`（`executor.go:195-228`）没有做 `alert_name` 过滤。
  - 用户说"CPU 告警历史"时，无法按告警名称过滤。
- **设计文档依据**: 4.4 节工具清单中 `alert.history` 参数包含 `alert_name`。
- **建议修复**:
  1. NLU 增加 `alert_name` 实体抽取（从关键词中提取告警名称）。
  2. LLM 白名单增加 `alert_name`。
  3. `runAlertHistory` 增加 `alert_name` 条件过滤。

### 遗漏-3: `alert.events` 不支持 firing/resolved 状态过滤

- **文件**: `server-web/copilot/nlu/nlu.go`、`server-web/copilot/tool/executor.go`
- **严重程度**: P1
- **描述**:
  - NLU 将 `firing`、`resolved` 只作为告警关键词使用，没有抽取 `status` 实体。
  - `runAlertEvents` 调用 `AlertEvents(toolCtx, 20, "", severity)`，`statusFilter` 固定为空。
  - 用户查询"最新 resolved 告警事件"时，工具仍会返回 firing/resolved 混合事件。
- **设计文档依据**: 4.3 节触发示例包含"最新 resolved"；4.4 节 `alert.events` 数据来源为告警事件，事件查询应能表达状态条件。
- **建议修复**:
  1. NLU 增加事件状态抽取，识别 `firing`、`resolved`。
  2. LLM `isAllowedEntity` 白名单保留或复用 `status`。
  3. `runAlertEvents` 将 `status` 传入 `AlertEvents`，并复用现有状态白名单。

### 遗漏-4: `alert.history` 不支持 status 过滤

- **文件**: `server-web/copilot/tool/executor.go`
- **严重程度**: P2
- **描述**: `runAlertHistory` 目前只按 `severity`、`instance`、`window` 过滤，没有使用设计文档列出的 `status` 参数。用户查询"resolved 告警历史"时无法限定历史状态。
- **设计文档依据**: 4.4 节工具清单中 `alert.history` 参数包含 `status`。
- **建议修复**:
  1. NLU 抽取 `status`。
  2. LLM 白名单继续允许 `status`。
  3. `runAlertHistory` 增加 `status` 条件过滤，并限制为 `firing/resolved`。

### 遗漏-5: `host.list` 参数能力未完整实现

- **文件**: `server-web/copilot/tool/executor.go`
- **严重程度**: P2
- **描述**: 设计文档列出 `host.list` 参数为 `status, search, sort, risk, group_id`，当前实现只从 `status` 和 `instance` 构造查询，`sort` 固定为 `instance`，没有接入 `search`、`risk`、`group_id`。
- **设计文档依据**: 4.4 节工具清单中 `host.list` 参数包含 `status, search, sort, risk, group_id`。
- **建议修复**:
  1. 明确 Phase 1 是否必须支持 `risk/group_id`；如果不做，应在实现或文档中收窄参数表。
  2. 至少补齐 `search` 与 `sort` 的实体白名单和执行参数映射。
  3. `group_id` 如果需要落地，应复用现有 host group service，不在 Copilot 中重复查询逻辑。

---

## 测试缺口

### 测试-1: Handler 层未单独覆盖 401 场景

- **文件**: `server-web/copilot/handler/handler_test.go`
- **严重程度**: P3
- **描述**: `newTestRouter` 直接注入了用户上下文（`c.Set(middleware.ContextUserID, uint64(1))`），handler 包内没有单独测试未认证请求返回 401 的场景。但 `server-web/api/router_test.go` 已有 `TestCopilotRoutesRequireAuth`，路由级未认证 401 已覆盖。
- **设计文档依据**: 4.1 节验收标准"未登录访问返回 401"。
- **建议修复**: 可选补充 handler 层无用户上下文测试；优先级低于工具参数和过滤功能缺口。

### 测试-2: 缺少端到端集成测试

- **文件**: 无
- **严重程度**: P3
- **描述**: 现有测试都是单元级别 mock 测试。设计文档 8.2 节集成测试方案要求验证"Chat 查询活跃告警"、"Chat 查询主机列表"、"Chat 查询指标"等场景，但未实现。
- **设计文档依据**: 8.2 节集成测试表。
- **建议修复**: 补充集成测试，使用 mock Prometheus/Redis 验证完整 chat 流程。

---

## 潜在问题（不影响功能但需注意）

### 注意-1: Copilot 独立 alert service 实例

- **文件**: `server-web/api/router.go:117-121`
- **描述**: Router 中为 Copilot 创建了独立的 `copilotAlertService`，与主 alert service 共享 `cacheClient` 和 `dedupeTTL`，但 Kafka producer 和 DB 来自 router 参数。如果主 service 配置有变化，两处可能不一致。
- **风险**: 低。当前配置一致，但后续维护需注意。

### 注意-2: 前端错误展示逻辑

- **文件**: `frontend/src/pages/CopilotPage.vue:120-134`
- **描述**: `submitMessage` 的 catch 块先设置 `error.value`，然后又 push 了一条 assistant 消息显示错误内容。如果后续操作也出错，`error` 会被覆盖，且消息列表中会有多条错误消息。
- **风险**: 低。不影响功能，但用户体验可优化。

### 已否定-1: 前端 Copilot 导航入口缺失

- **原编号**: Bug-4
- **文件**: `frontend/src/App.vue`
- **复核结论**: 不成立。当前导航菜单已经存在 `<RouterLink to="/copilot">Copilot</RouterLink>`，登录用户可以从顶部导航进入 Copilot 页面。
- **处理建议**: 不需要修复；从 P0 阻塞项中移除。

---

## 修复优先级汇总

| 优先级 | 编号 | 问题 | 影响 |
|---|---|---|---|
| P1 | Bug-1 | `process_` 误杀合法 PromQL | 合法查询被拒 |
| P1 | 遗漏-2 | `alert_name` 缺失 | 告警历史无法按名称过滤 |
| P1 | 遗漏-3 | `alert.events` 状态过滤缺失 | resolved/firing 事件查询结果不准 |
| P2 | Bug-2 | count 硬编码 | alert.events 始终返回 20 条 |
| P2 | Bug-3 | 分页缺失 | alert.history 无法翻页 |
| P2 | 遗漏-1 | 实体抽取遗漏 | 与 Bug-2、Bug-3 关联 |
| P2 | 遗漏-4 | alert.history status 过滤缺失 | 历史查询无法按状态收敛 |
| P2 | 遗漏-5 | host.list 参数能力不完整 | 主机查询能力与设计参数表不一致 |
| P3 | 测试-1 | handler 层 401 测试缺口 | 路由级已覆盖，handler 粒度不足 |
| P3 | 测试-2 | 集成测试缺失 | 测试覆盖不足 |

---

## 本次复核验证记录

- **已执行并通过**: 在 `server-monitor/server-web` 下执行 `go test ./...`，通过。
- **已执行但失败**: 曾在 `server-monitor` 下执行 `go test ./server-web/...`，失败原因为 `server-web` 是独立 Go module，命令目录不正确。
- **未执行**: 前端测试、端到端集成测试；本次只做文档复核，没有启动完整运行环境。
