# API

CloudOps 只有一个公开产品合同：V1。权威描述为 [OpenAPI 3.1](api-v1-openapi.yaml)，实现位于 [`internal/api`](../internal/api/)。浏览器和第一方 client 只访问 `/api/v1`。

## 1. Transport contract

- Base path：`/api/v1`
- Success：`application/json`
- Error：`application/problem+json`
- Event stream：`text/event-stream`
- Public ID：canonical UUID
- Local Owner：固定 `local-owner` 审计身份，无 login、session 或 browser bearer

错误包含稳定 semantic `code`、HTTP status、request ID 与 trace ID。客户端只根据 code/status 决定行为，不解析英文 detail。`202` 只表示命令已 durable 接受，不等于 Provider 工作完成。

## 2. Endpoint families

| Family | 代表路径 | 所有权 |
|---|---|---|
| Bootstrap / Scope | `/bootstrap`、`/scopes`、`/overview` | active scope、Scenario identity、Provider health 与 shell bootstrap |
| Infrastructure | `/topology`、`/resources` | Kubernetes topology、structured resource 与 events |
| Settings | `/settings`、`/configuration-revisions`、`/secrets`、`/providers/{provider}/tests` | validate/apply/restore、write-only secret 与 provider test |
| Notifications | `/notifications`、`/notification-events` | Inbox、read state 与 SSE refresh |
| Alerts | `/alerts`、`/alerts/{id}`、ack/silence/investigation/incident links | Alertmanager-backed lifecycle 与 domain relation |
| Monitoring | `/monitoring/catalog`、`/monitoring/queries`、definitions/authorizations | bounded Metrics query 与 exact query authority |
| Logs | `/logs/catalog`、`/logs/queries`、`/logs/queries/{id}/evidence` | Elasticsearch query/history/Evidence |
| Traces | `/traces/catalog`、`/traces/searches`、`/traces/{trace_id}` | Tempo search/detail/Evidence |
| Agent | `/agent/investigations`、`/agent/consultations`、`/knowledge-items`、`/agent/action-cards` | durable conversation/run/Evidence/Knowledge/authority |
| Incidents | `/incidents`、detail relations、decision、verification、resolution report、events | current-cycle coordination and history |
| Operations / DevOps | `/operation-plans`、`/operations`、`/devops` | immutable Plan、Authorization、Execution、Verify 与 optional delivery projection |

完整 method、request/response schema、cursor 和 bounds 以 OpenAPI 为准；本文不复制易漂移的字段清单。

## 3. Query and Context Link contract

查询必须绑定当前 Scope、provider/resource identity 与绝对 `from/to`。Context Link 在 Workspace 间传递同一 cluster、Namespace、resource、Incident/Alert/Investigation/Evidence ID 和时间窗；目标页面不能用“最近 N 分钟”悄悄替换原时间范围。

Collection 使用服务端上限与 opaque cursor；Timeline 使用 `after_id`。Logs/Trace query 还受 lookback、result count、response bytes 与 timeout 约束。SSE 只发送 refresh hint，客户端收到后重新读取权威 projection。

## 4. Mutation safeguards

所有 mutation 至少要求：

- `Content-Type: application/json`；
- same-origin 或 allowlisted `Origin`；
- bounded `Idempotency-Key`；
- positive `expected_version`；
- decision/authorization/execution 使用 lowercase SHA-256 `expected_hash`；
- unknown-field rejection 与 body size limit。

相同 identity/resource/command/key 与相同 canonical payload 重放原结果；相同 key 配不同 payload 返回 `IDEMPOTENCY_KEY_REUSED`。Operation 执行还会在 effect 前重验 Plan hash、Authorization、expiry、Configuration Revision 与 precondition。

## 5. Internal listener

`/webhooks/alertmanager` 不属于浏览器 V1 API。它只存在于 API internal listener/Service，要求只读 secret file 中的 bearer，并将规范化 Signal 写入领域模型。用户 Service 不暴露该 route，internal Service 不暴露 `/api/v1`。

## 6. Verification

```bash
go test ./internal/api ./internal/router
rg -n '^  /api/v1/' docs/api-v1-openapi.yaml
make check-naming
```

OpenAPI、runtime route、typed frontend client 与 contract test 必须在同一变更中保持一致。真实 UI -> API -> Provider 证据见 [实施状态](evidence/cloudops-implementation-status.md)；静态 route 或 fixture 不能替代该证据。
