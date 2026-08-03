# CloudOps Frontend Terminology

> Status: `ACTIVE`
>
> Established: 2026-07-30 (Asia/Shanghai)
>
> Scope: user-visible frontend copy; source identifiers and Provider payloads remain verbatim

## Writing Rules

- Chinese is the default for navigation, commands, labels, validation, empty states, errors, recovery actions and operational guidance.
- Keep established professional terms, protocol names, Kubernetes Kinds, query languages, source fields, commands, identifiers and raw Provider text in their exact form.
- Use `CloudOps` in the compact Shell and `CloudOps-Copilot` where the full product name is needed. Do not introduce another product name or Incident-only brand.
- State what is known, unknown and actionable. Do not turn `accepted`, `dispatched`, `observed` or partial Provider facts into verified success.
- Display exact UTC directly in audit-critical Evidence, Approval, Authorization, Delivery, Verification, Revision History and dangerous-operation results.
- UI icons are Lucide only. Do not use emoji as icons or state markers.

## Product Terms

| Canonical UI term | Chinese context | Do not substitute |
| --- | --- | --- |
| Agent | `Agent 调查`、`Agent Workspace` | AI 助手、机器人客服 |
| Incident | `Incident`、`事故处置` | 工单、普通告警 |
| Alert | `Alert`、`告警` | Incident |
| Evidence | `Evidence`、`证据` | 推测、日志截图 |
| Approval | `Approval`、`精确审批` | 确认一下、已授权 when only accepted |
| Delivery | `Delivery`、`交付` | 恢复成功 |
| Verification | `Verification`、`恢复验证` | Delivery success |
| Trace | `Trace`、`链路` | 日志 |
| Span | `Span` | 节点 when referring to trace spans |
| Provider | `Provider`、`数据来源` | 平台自有数据 when Provider-backed |
| Scope | `Scope`、`运维范围` | 权限范围 unless authority is meant |
| Operations Atlas | `Operations Atlas`、`运维拓扑` | 拓扑装饰图、关系大屏 |
| Command Center | `Operations Agent Command Center`、`运维态势` | Dashboard 首页、营销首页 |
| Context Link | `Context Link`、`上下文链接` | 裸 URL |
| Revision | `Revision`、`配置修订` | Draft when no backend Draft exists |

## Lifecycle Terms

| Backend/domain fact | Canonical Chinese presentation | Required boundary |
| --- | --- | --- |
| connecting | 正在连接 | Do not show live |
| live | 实时 | Only while continuity is trustworthy |
| reconnecting | 正在重新连接 | Preserve current reading position |
| disconnected | 已断开 | Stop live claim |
| stale | 数据已陈旧 | Show source time and refresh action |
| cursor expired | 游标已过期 | Explain whether complete resync is possible |
| resync failed | 补齐失败 | Do not imply completeness |
| accepted | 请求已接受 | Not dispatched, observed or verified |
| dispatched | 已下发 | Not observed or verified |
| observed | 已观察到变化 | Not verified recovery |
| verified | 已验证 | Requires current Verification and Evidence |
| partial | 部分完成 | Identify successful and failed sources/items |
| permission denied | 权限不足 | Preserve request/trace identity and context |
| authority expired | Authority 已过期 | Block action and show renewal path if provided |
| exact hash changed | 精确 Hash 已变化 | Require a new review; never reuse approval |

## Command Copy

Use concise verbs tied to real effects:

- Read-only: `查看`、`查询`、`刷新`、`打开 Context Link`.
- Low-impact acknowledgement: `确认已知悉`.
- Reversible configuration: `校验更改`、`应用此区段`.
- Exact approval: `审批此 Hash`.
- Rollback: `回滚到 <revision>`.
- Forced termination: `强制终止 <operation>`.

Confirmations must identify target, effect, authority, exact hash/version when applicable, irreversible consequences and recovery limits. A single generic “确定继续吗” message is not acceptable.

## Error Copy

When supplied by the API, display `code`, request ID, trace ID, idempotent replay truth and next steps. Prefer this order:

1. What failed in Chinese.
2. What data or operation is affected.
3. Exact identity and source facts.
4. The next safe action.

Do not expose secrets, credentials, raw sensitive configuration or hidden chain-of-thought.
