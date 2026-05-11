# Phase 6 Code Review: Action Approval & Audit System

> 审查日期：2026-05-11
> 审查范围：Phase 6 动作审批与审计系统
> 基准提交：abd8de5
> 审查依据：`docs/realize/realize_Phase 6.md`

---

## Strengths

1. **包结构清晰。** `copilot/action` 包按职责拆分为 `types.go`（DTO）、`policy.go`（规则）、`audit.go`（脱敏）、`service.go`（业务逻辑）、`handler.go`（HTTP）、`repository.go`（持久化）、`notifier.go`（WebSocket）、`events.go`（Kafka）、`k8s_executor.go`（执行器接口），单一职责明确。

2. **审计脱敏覆盖全面。** `audit.go:102-109` 覆盖了设计文档要求的所有敏感 key（`password`、`passwd`、`secret`、`token`、`api_key`、`authorization`、`kubeconfig`），支持大小写不敏感匹配和嵌套对象/数组递归处理。非法 JSON 截断到 2048 字符。

3. **DisabledK8sExecutor 模式合理。** `k8s_executor.go:17-25` 返回 `ErrExecutionDisabled`，service 层将其标记为 `failed` 并写入审计记录，符合 Phase 6 范围。

4. **配置校验完整。** `config.go:535-539` 按设计文档校验 `ACTION_MAX_REPLICAS` 范围 [1,100] 和 `ACTION_PENDING_TTL_HOURS` 范围 [1,168]。

5. **部署配置一致。** Docker Compose、K8s configmap/web.yaml、Helm values/configmap/server-web.yaml 六个文件均包含相同的六个 action 环境变量，默认值一致。

6. **前端页面遵循项目规范。** ActionsPage、ActionDetailPage、AuditLogsPage 使用了与现有页面一致的 CSS 变量和卡片布局。Router guard 在 `router/index.ts:140-142` 正确拦截非 admin 用户。

7. **核心路径有测试覆盖。** `service_test.go` 覆盖了从诊断创建（跳过不安全动作）、approve-reject-execute 流程和 disabled executor 失败路径。`policy_test.go` 覆盖了合法 scale、未注册动作、replicas 超限、敏感参数和状态流转。`audit_test.go` 覆盖了敏感字段脱敏和非法 JSON 处理。

8. **WebSocket 消息处理具有弹性。** `useAlertsWebSocket.ts:74-83` 在派发前校验 `action_pending` 和 `action_status` 消息。未知类型用 `console.warn` 记录，不会导致页面异常。

---

## Issues

### Critical (Must Fix)

**C1. `CanTransition` 已定义但从未调用 — 状态机未被强制执行。**

- 文件：`server-monitor/server-web/copilot/action/service.go`
- `CanTransition()` 函数在 `policy.go:141-168` 中定义，在 `policy_test.go:89-115` 中测试，但 **`service.go` 中没有任何代码调用它**。`Approve`、`Reject`、`Execute` 方法直接用 `if` 语句检查 `action.Status`，未通过 `CanTransition` 验证状态流转合法性。状态机只是装饰性的，未被强制执行。
- **影响：** 代码演进时 `ValidateApprove` 或 `ValidateExecute` 中的状态检查可能与 `CanTransition` 产生偏差。设计文档要求状态机作为权威守卫。
- **修复：** 将 ad-hoc 状态检查替换为 `CanTransition` 调用。例如在 `Approve` 中将 `if action.Status != model.ActionStatusPending` 替换为 `_, ok := CanTransition(action.Status, EventApprove); if !ok { ... }`。

**C2. `SaveAction` 无乐观锁或事务 — 并发状态更新可产生竞态。**

- 文件：`server-monitor/server-web/copilot/action/repository.go:99-105`
- `SaveAction` 调用 `r.db.WithContext(ctx).Save(&action)` 执行全量 UPDATE，无 `WHERE status = ?` 条件，无 `SELECT ... FOR UPDATE`，未包裹事务。两个并发 `Approve` 请求可同时读取 `status=pending`，同时通过校验，同时保存 `status=approved`，后者覆盖前者的 `approved_by`/`approved_at`。
- **影响：** 设计文档明确要求"事务 + 行级锁 + 状态机终态保护"。这是审批工作流中最关键的正确性问题。
- **修复：** 新增 `SaveActionCAS` 方法，使用 `UPDATE pending_actions SET ... WHERE id = ? AND status = ?` 并检查 `RowsAffected`。或使用事务 + `SELECT ... FOR UPDATE`。

**C3. `Execute` 未在执行前重新加锁读取 action。**

- 文件：`server-monitor/server-web/copilot/action/service.go:216-268`
- 设计文档（8.6 节）要求："开启事务 → 重新读取 action 并加行级锁 → 校验状态 approved → 更新为 executing → 事务提交"。当前实现只读取一次 action（`repo.GetAction`），校验后直接保存 `status=executing`。读取和保存之间，另一个请求可能已开始执行。`GetAction` 未使用 `FOR UPDATE`。
- **影响：** 两个并发 `Execute` 调用可同时通过 `ValidateExecute`，导致重复执行和双重审计记录。
- **修复：** 实现设计文档描述的事务模式：`BEGIN → SELECT ... FOR UPDATE → validate → UPDATE status=executing → COMMIT → execute → UPDATE result`。

---

### Important (Should Fix)

**I1. `actorFromGin` 在 role 为空时默认为 "admin"。**

- 文件：`server-monitor/server-web/copilot/action/handler.go:190-193`
- `if actor.Role == "" { actor.Role = "admin" }` 意味着中间件配置错误时请求将被视为 admin。应默认为限制性角色。
- **修复：** 移除默认值，或默认为 `"viewer"`。

**I2. `ActionPendingTTL` 已加载但从未使用。**

- 文件：`server-monitor/server-web/config/config.go:209` 定义了 `ActionPendingTTL`，`config.go:538-539` 校验了范围，但从未传递给 `ServiceConfig` 或在 action service 中使用。没有过期/取消机制。
- **影响：** 设计文档要求"pending 动作建议过期时间，超期可取消"，状态机包含 `cancel` 事件。pending action 将无限期堆积。
- **修复：** 实现后台定时任务取消过期 pending action，或将此功能明确标记为后续阶段。

**I3. 缺少 handler、repository、notifier、events、k8s_executor 的测试文件。**

- 设计文档指定了 `handler_test.go`、`repository_test.go`、`notifier_test.go`、`executor_test.go`、`advisor_test.go`，但实际只存在 `policy_test.go`、`audit_test.go`、`service_test.go`。缺少：
  - HTTP handler 行为测试（状态码、JSON 结构、错误响应）
  - Repository 真实 GORM 测试（SQL 正确性）
  - Notifier 消息结构测试
  - Kafka 事件发布测试
  - `CanTransition` 与实际 approve/reject/execute 流程的集成测试
- **修复：** 至少添加使用 `httptest` 的 handler 测试，验证 HTTP 状态码、RBAC 强制（viewer 返回 403）和响应 JSON 结构。

**I4. `Reject` 使用内联状态检查而非 policy 模式。**

- 文件：`server-monitor/server-web/copilot/action/service.go:193`
- `Reject` 直接检查 `actor.Role != "admin" || action.Status != model.ActionStatusPending`，而非调用 `policy.ValidateReject(action, actor)`。Policy 上没有 `ValidateReject` 方法。与 `Approve`（调用 `policy.ValidateApprove`）和 `Execute`（调用 `policy.ValidateExecute`）不一致。
- **修复：** 在 Policy 上新增 `ValidateReject` 方法，让 `Reject` 调用它。

**I5. Swagger 文档未更新。**

- 设计文档列出了 `server-web/docs/swagger.yaml` / `docs.go` / `swagger.json` 作为修改文件，但未做任何 swagger 修改。
- **修复：** 为新的 action 和 audit API 端点添加 Swagger 注解或手动更新 swagger spec。

**I6. `ACTION_EXECUTION_ENABLED=true` 时无 executor 校验。**

- 文件：`server-monitor/server-web/config/config.go` — 启动时未校验 `ACTION_EXECUTION_ENABLED=true` 是否有真实（非 disabled）executor 可用。设计文档（6.3 节）要求："ACTION_EXECUTION_ENABLED=true 时必须有可用 executor；没有 executor 则启动失败或强制降级为 false"。
- 当前 `router.go:222` 始终使用 `DisabledK8sExecutor{}`，即使设置 `ACTION_EXECUTION_ENABLED=true` 也会静默失败。
- **修复：** 启动时如果 `ACTION_EXECUTION_ENABLED=true` 但只有 disabled executor，应打印警告或强制降级为 false。

**I7. `Approve` 未通过状态机验证 approve 事件。**

- 文件：`server-monitor/server-web/copilot/action/service.go:159`
- `s.policy.ValidateApprove(action, actor)` 直接检查 `action.Status != model.ActionStatusPending`，而非使用 `CanTransition(action.Status, EventApprove)`。与 C1 相关。

---

### Minor (Nice to Have)

**M1. `FindPendingByDedupeKey` 的去重查询未排除终态。**

- 文件：`server-monitor/server-web/copilot/action/repository.go:51-61`
- 查询为 `WHERE dedupe_key = ?`，未过滤状态。如果之前的 action 已 rejected/cancelled，新创建会被跳过。应排除终态。
- **修复：** 添加 `AND status NOT IN ('rejected', 'failed', 'cancelled', 'executed')`。

**M2. `truncateUTF8` 可能在多字节字符中间截断。**

- 文件：`server-monitor/server-web/copilot/action/audit.go:120-131`
- 函数按字节截断到 `limit`，`utf8.ValidString` 循环只保证 UTF-8 有效性，不保证在 limit 内。`value[:limit]` 仍可能切断多字节 rune。
- **修复：** 使用 `[]rune` 转换或按 rune 迭代到 limit。

**M3. 前端 `ActionsPage` 无分页控件。**

- 文件：`server-monitor/frontend/src/pages/ActionsPage.vue:41`
- `page_size: 50` 硬编码，无分页控件。超过 50 条 action 时用户无法查看。
- **修复：** 添加分页控件，或至少记录此限制。

**M4. `ActionDetailPage` 使用 `window.prompt` 输入拒绝原因。**

- 文件：`server-monitor/frontend/src/pages/ActionDetailPage.vue:49`
- `window.prompt` 不支持多行输入，无字符限制显示。后端限制 1-500 字符，但用户提交前无反馈。
- **修复：** 使用 modal 或 textarea 组件替代。

**M5. `Service.Execute` 在错误时同时返回响应和错误。**

- 文件：`server-monitor/server-web/copilot/action/service.go:268`
- `return toActionResponse(final), execErr` — handler 在 `handler.go:123` 返回 500 并包含完整 action 响应，可能泄露 `error_message` 和 `result_json` 中的信息。
- **修复：** 确保错误响应中的 `error_message` 也经过 `SanitizeJSON` 脱敏。

**M6. `isNotFound` 辅助函数已定义但未使用。**

- 文件：`server-monitor/server-web/copilot/action/service.go:505-507`
- 此函数从未被调用，可移除。

---

## Recommendations

1. **新增 `repository.SaveActionCAS` 方法**，使用 `UPDATE pending_actions SET ... WHERE id = ? AND status = ?` 并检查 `RowsAffected`。这是解决并发问题（C2）的最小修复方案。

2. **将 `CanTransition` 接入 service 方法。** 即使当前直接状态检查恰好正确，使用 `CanTransition` 作为单一真相源可防止偏差并使代码可审计。

3. **将 `actorFromGin` 的 role 默认值改为 "viewer"**，在中间件配置错误时安全降级。

4. **添加 handler 级集成测试**，使用 `httptest` + mock service。这是最有价值的测试缺口 — 可验证 RBAC、请求解析、错误响应格式和 HTTP 状态码。

5. **如果 `ACTION_PENDING_TTL` 故意延迟实现，应明确文档化**。有校验但未使用的配置字段会造成混淆。

---

## Assessment

**Ready to merge?** No, with fixes.

**Reasoning:** 实现正确覆盖了设计文档的模块结构、API 端点、配置字段、部署文件和前端页面。但状态机（`CanTransition`）已定义却未被强制执行，repository 缺乏并发状态更新的事务安全保障（C1、C2、C3）。这些是安全关键审批工作流中的正确性问题，设计文档明确要求并发安全和终态保护。`actorFromGin` 的 admin 默认值（I1）存在安全隐患。修复 C1-C3 和 I1 后可合并；其余项可在后续提交中处理。

---

## Codex Recheck & Fix Notes

> 复核日期：2026-05-11
> 复核结论：核心阻断项属实，已做最小修复；仍有非阻断设计补齐项留待后续阶段。

### 已确认并修复

1. **C1 / C7：状态机未被 service 强制执行。**
   - 复核结果：属实。`Approve`、`Reject`、`Execute` 原先直接读 action 后散写状态，`CanTransition` 只存在于 policy 测试中。
   - 修复：`Policy.ValidateApprove`、`Policy.ValidateExecute` 改为调用 `CanTransition`；新增 `Policy.ValidateReject`；service 的 approve/reject/execute 均通过统一状态流转入口更新。

2. **C2 / C3：审批与执行状态更新缺少事务和行级锁，存在并发覆盖与重复执行风险。**
   - 复核结果：属实。原 `SaveAction` 是全量 `Save`，没有 `FOR UPDATE` 或状态条件保护。
   - 修复：移除 service 对 `SaveAction` 的直接依赖，新增 `Repository.TransitionAction`，在 GORM transaction 内用 `SELECT ... FOR UPDATE` 重新读取 action、调用 `CanTransition` 校验，再保存状态变更。`Execute` 进入 `executing` 前也走该路径，避免 stale approved action 继续触发 executor。
   - 回归测试：新增 stale pending approve 与 stale approved execute 两个测试，验证状态已变化时不会覆盖终态、不会调用 executor。

3. **I1：`actorFromGin` role 为空时默认 admin。**
   - 复核结果：属实。
   - 修复：默认角色改为 `viewer`，中间件上下文缺失时安全降级。

4. **I4：Reject 未走 policy 模式。**
   - 复核结果：属实。
   - 修复：新增 `ValidateReject` 并在 `Service.Reject` 中使用，审批/拒绝/执行的校验入口保持一致。

5. **I6：`ACTION_EXECUTION_ENABLED=true` 时没有真实 executor 校验。**
   - 复核结果：属实。router 始终注入 `DisabledK8sExecutor{}`。
   - 修复：router 在检测到 `ACTION_EXECUTION_ENABLED=true` 且当前仍无真实 executor 时打印明确 warn，并强制降级为 disabled 执行，避免配置语义误导。

6. **M1：`FindPendingByDedupeKey` 未排除终态。**
   - 复核结果：属实。
   - 修复：查询排除 `rejected`、`failed`、`cancelled`、`executed`，终态 action 不再阻止后续重新创建 pending action。

7. **M2：`truncateUTF8` 可切断多字节字符。**
   - 复核结果：属实。
   - 修复：改为按 rune 边界截断到不超过 limit 的最大字节前缀，并新增 UTF-8 回归测试。

8. **M6：`isNotFound` 未使用。**
   - 复核结果：属实。
   - 修复：已移除未使用辅助函数。

### 仍属实但未在本次修复

1. **I2：`ActionPendingTTL` 仍未接入过期取消机制。**
   - 原因：需要新增后台任务/调度生命周期、审计事件和取消策略，属于独立功能补齐，不适合与并发状态修复混在同一最小补丁中。

2. **I3：handler/repository/notifier/events/executor 测试仍不完整。**
   - 本次只补了 action service 与 audit 的关键回归测试；handler、notifier、events、executor 可作为后续测试补齐批次。

3. **I5：Swagger 文档仍未补齐 action/audit API。**
   - 原因：属于 API 文档生成/维护任务，本次未改 API 路由或响应结构。

4. **M3 / M4：前端分页与 reject 输入体验仍未改。**
   - 原因：属于前端交互增强，不影响本次后端审批状态正确性闭环。

5. **M5：执行失败响应仍返回 action data。**
   - 当前 handler 已返回通用错误文案，但仍携带 action 响应体；是否移除 `data` 会影响前端错误展示，建议单独评估。

### 本次验证

```bash
go test ./copilot/action
go test ./...
go vet ./...
git diff --check
```

以上命令均在 `server-monitor/server-web` 或仓库根目录按需执行通过。
