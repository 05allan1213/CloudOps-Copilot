# API

V3 产品 API 的权威合同是手写 [OpenAPI 3.1](api-v3-openapi.yaml)，实现位于 [internal/apiv3](../internal/apiv3/)。浏览器产品面只消费这些 API；Handler 不运行 Agent、Delivery、Verification 或 LLM。

## 1. Base path 与媒体类型

- Base path：`/api/v3`
- Success：`application/json`
- Error：`application/problem+json`
- Event stream：`text/event-stream`
- Public ID：canonical UUID；内部 numeric ID、lease、checkpoint 和 credential 不出现在 transport DTO。

每个 error 都返回稳定 `code`、HTTP status、request ID 和 trace ID。客户端不得根据英文 detail 推导领域状态。

## 2. Query API

| Method | Path | 角色 | 内容 |
|---|---|---|---|
| GET | `/session/csrf` | viewer/operator | 当前 GitHub identity-bound 短期 CSRF token |
| GET | `/incidents` | viewer/operator | status/severity/service filter 与 cursor page |
| GET | `/incidents/{id}` | viewer/operator | Incident current projection |
| GET | `/incidents/{id}/signals` | viewer/operator | bounded Signal page |
| GET | `/incidents/{id}/timeline` | viewer/operator | monotonic timeline page |
| GET | `/incidents/{id}/evidence` | viewer/operator | sanitized Evidence metadata/facts |
| GET | `/incidents/{id}/investigations` | viewer/operator | AgentRun/step summary |
| GET | `/incidents/{id}/remediation-plans` | viewer/operator | 完整 Plan、diff、hash、decision |
| GET | `/incidents/{id}/delivery` | viewer/operator | PR/CI/Argo/rollout projection |
| GET | `/incidents/{id}/verifications` | viewer/operator | Check/sample/common-window projection |
| GET | `/incidents/{id}/resolution-report` | viewer/operator | immutable current-cycle report |
| GET | `/incidents/{id}/events` | viewer/operator | SSE refresh hints |

Timeline 使用 `after_id`；其他 collection 使用 opaque cursor。`limit` 上限由服务端固定。SSE 接受 `Last-Event-ID`，只发送 Incident-scoped refresh hint；客户端收到 hint 后重新查询权威 projection。

## 3. Command API

| Method | Path | 命令 |
|---|---|---|
| POST | `/incidents/{id}/investigations` | 显式启动新的 bounded investigation |
| POST | `/incidents/{id}/close` | closed-no-action command |
| POST | `/remediation-plans/{id}/decisions` | approve/reject exact Plan/hash |

所有 POST 必须包含：

```text
Idempotency-Key: bounded opaque value
X-CSRF-Token: current identity-bound token
Origin: allowlisted UI origin
```

Body 还必须携带 expected version；decision 必须携带 expected canonical Plan hash。相同 idempotency key + 相同 payload 返回同一结果；相同 key + 不同 payload 返回 conflict。

## 4. Authentication

Phase 5/6 使用同 Pod 的 oauth2-proxy。公网/port-forward Service只暴露 proxy；用户 API listener绑定 loopback。API 只信任 proxy 覆盖的 GitHub login header，并拒绝 access token、Authorization 或 session cookie穿透。

本地 HTTP port-forward 可在 Demo profile使用 `Secure=false` cookie；这只是本地例外，不是生产 TLS 设计。身份与命令安全详见 [Security](security.md)。

## 5. 示例

以下示例只进行 read，不打印 cookie 或 CSRF token：

```bash
BASE_URL=https://cloudops.example
COOKIE_JAR=/secure/path/oauth-cookie.jar

curl --fail --silent --show-error \
  --cookie "$COOKIE_JAR" \
  "$BASE_URL/api/v3/incidents?limit=20&status=investigating"

curl --fail --silent --show-error \
  --cookie "$COOKIE_JAR" \
  "$BASE_URL/api/v3/incidents/$INCIDENT_ID/verifications?limit=20"
```

命令示例必须从 `/api/v3/session/csrf` 在内存中取得 token，并由操作者显式确认 version/hash；文档不提供绕过 OAuth/CSRF 的命令。

## 6. 当前状态

| 控制 | 状态 | 说明 |
|---|---|---|
| Route/OpenAPI path parity source contract | `PASS` | `docs/api-v3-openapi.yaml`、`internal/apiv3/handler.go` |
| Public UUID、bounded projection、problem+json | `PASS` | `internal/apiv3/types.go`、validation/projection代码 |
| Viewer/operator、CSRF、Origin、idempotency | `PASS` | `internal/apiv3/` source contracts |
| Cursor/Last-Event-ID/SSE refresh-only contract | `PASS` | handler/query port 与 OpenAPI |
| 当前 HEAD API contract/integration tests | `NOT RUN` | 本文档切片只做静态检查 |
| 真实 oauth2-proxy + GitHub OAuth | `NOT RUN` | live credentials/session 未验证 |
| Phase 6 live two-page Workbench | `NOT RUN` | 无当前 clean-kind/browser证据 |
| Golden Incident full API audit export | `NOT RUN` | 依赖真实 Golden E2E |

## 7. 验证命令

```bash
go test ./internal/apiv3
rg -n '^  /api/v3/' docs/api-v3-openapi.yaml
rg -n 'Method: http.Method(Get|Post)' internal/apiv3/handler.go
```

OpenAPI 和 runtime route 的任何变更必须在同一提交中更新 contract tests与本文。
