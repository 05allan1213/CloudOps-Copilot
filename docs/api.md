# API

CloudOps 只有一个公开产品合同：V1。权威描述是 [OpenAPI 3.1](api-v1-openapi.yaml)，实现位于 [`internal/api`](../internal/api/)。浏览器和第一方 client 只允许访问 `/api/v1`。

## 1. Transport contract

- Base path：`/api/v1`
- Success：`application/json`
- Error：`application/problem+json`
- Event stream：`text/event-stream`
- Public ID：canonical UUID
- Local Owner：固定 `local-owner` 审计身份，无 login、session、RBAC、CSRF 或 bearer token

每个错误包含稳定 semantic `code`、HTTP status、request ID 与 trace ID。客户端只根据 code/status 处理行为，不解析英文 detail。

## 2. Query endpoints

| Method | Path | 内容 |
|---|---|---|
| GET | `/incidents` | status/severity/service filter 与 cursor page |
| GET | `/incidents/{id}` | Incident current projection |
| GET | `/incidents/{id}/signals` | bounded Signal page |
| GET | `/incidents/{id}/timeline` | monotonic Timeline page |
| GET | `/incidents/{id}/evidence` | sanitized Evidence facts/provenance projection |
| GET | `/incidents/{id}/investigations` | AgentRun/step summary |
| GET | `/incidents/{id}/remediation-plans` | Plan、diff、hash 与 decision |
| GET | `/incidents/{id}/delivery` | Change/PR/CI/rollout projection |
| GET | `/incidents/{id}/verifications` | run/check/sample projection |
| GET | `/incidents/{id}/resolution-report` | current-cycle deterministic report |
| GET | `/incidents/{id}/events` | Incident-scoped SSE refresh hints |

Timeline 使用 `after_id`；其他 collection 使用 opaque cursor。`limit` 有服务端上限。SSE 接受 `Last-Event-ID`，只发送 `incident.refresh` hint；客户端收到后重新读取权威 projection。

## 3. Command endpoints

| Method | Path | 命令 |
|---|---|---|
| POST | `/incidents/{id}/investigations` | 启动 bounded investigation |
| POST | `/incidents/{id}/close` | closed-no-action transition |
| POST | `/remediation-plans/{id}/decisions` | approve/reject exact Plan/hash |

所有 mutation 必须满足：

- `Content-Type: application/json`；
- same-origin 或 allowlisted `Origin`；
- bounded `Idempotency-Key`；
- body 中的 positive `expected_version`；
- decision body 中的 lowercase SHA-256 `expected_hash`。

相同 identity/resource/command/key 与相同 canonical payload 重放原结果；相同 key 配不同 payload 返回 `IDEMPOTENCY_KEY_REUSED`。服务端以固定 Owner identity 写审计 actor，浏览器不能覆盖。

## 4. Examples

读取当前 Incident：

```bash
BASE_URL=http://127.0.0.1:18080
curl --fail --silent --show-error \
  "${BASE_URL}/api/v1/incidents?limit=20"
```

Mutation 示例保留 Origin、version 和 idempotency 约束：

```bash
BASE_URL=http://127.0.0.1:18080
INCIDENT_ID=<canonical-uuid>

curl --fail --silent --show-error \
  -X POST \
  -H "Origin: ${BASE_URL}" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: owner-investigation-001' \
  --data '{"expected_version":1,"reason":"owner requested investigation"}' \
  "${BASE_URL}/api/v1/incidents/${INCIDENT_ID}/investigations"
```

命令是否成功还取决于当前领域状态和 Provider availability；`202` 只表示命令被 durable 接受，不代表 Provider 工作已完成。

## 5. Internal listener

`/webhooks/alertmanager` 不属于浏览器 V1 API。它只存在于 `cloudops-api` 的内部 listener/Service，要求 secret file 提供的 bearer token，并将规范化 Signal 写入当前领域模型。用户 Service 不暴露该 route，内部 Service 不暴露 `/api/v1`。

## 6. Verification

```bash
go test ./internal/api ./internal/router
rg -n '^  /api/v1/' docs/api-v1-openapi.yaml
make check-naming
```

OpenAPI、runtime route、typed frontend client 和 contract tests 必须在同一变更中保持一致。当前 live 联调证据见 [实施状态](evidence/cloudops-implementation-status.md)；静态 route 存在不能替代该证据。
