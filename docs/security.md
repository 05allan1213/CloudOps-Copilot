# Security

本文记录当前 Local Owner runtime 的安全边界，不构成生产威胁模型、合规认证或公开部署建议。CloudOps 当前只支持本机单 Owner；任何 LAN、远程、公开或多用户模式都需要新的架构决策和认证设计。

## 1. Local Owner boundary

`make local-up` 只把应用通过 `kubectl port-forward --address=127.0.0.1` 暴露到 loopback。浏览器无需登录，服务端为 mutation 固定记录：

```text
subject=local-owner provider=local login=owner role=owner
```

当前合同没有 OAuth、oauth2-proxy、账号、session、role map、CSRF token、Authorization header 或 frontend token storage。Local Owner 不等于取消浏览器安全：跨源 request 被 CORS/Origin policy 拒绝，所有 mutation 仍需要 idempotency 和 optimistic concurrency。

## 2. Process and database identities

| 身份 | 允许 | 禁止 |
|---|---|---|
| `cloudops-api` | MySQL Query/Command、静态 UI、用户 API、内部 bearer webhook | Kubernetes token、Provider claim loop、schema migration |
| `cloudops-worker` | MySQL task runtime；显式启用后执行 bounded Provider operation | 隐式 Provider fallback、未配置时领取任务、Kubernetes write |
| `cloudops-migrate` | forward-only DDL 与 schema version verification | 用户 API、后台任务、Provider credential |
| MySQL | 本地 durable domain/config/evidence truth | 浏览器直接访问 |

API、Worker、Migrate 使用不同 MySQL user/Secret；MySQL root 只用于本地 bootstrap、受控 backup/restore 与数据库管理。Chart validation 拒绝共享 workload credential 或 root credential 下放。

API ServiceAccount 和默认 standby Worker 不挂载 Kubernetes token。显式开启 Kubernetes read 后，Worker Role 只允许 `get/list` 目标 namespace 的 Pod、Service、Event、Deployment、ReplicaSet 和 EndpointSlice。

## 3. Browser command safeguards

Mutation 的最小合同是：

- exact `/api/v1` route；
- same-origin/allowlisted `Origin`；
- JSON body size limit 和 unknown-field rejection；
- bounded `Idempotency-Key`；
- expected row version，必要时绑定 expected content hash；
- stable Problem Details、request ID 和 trace ID。

同一个 idempotency key 不能授权不同 payload。Owner identity、resource UUID、command kind 与 canonical payload 一起构成 scope。模型输出、URL 参数或前端字段不能授予 Provider authority。

## 4. Secrets and backups

本地 secret、bootstrap credential、runtime PID/log 和 backup 位于 Git 忽略的 `.cloudops/`，目录权限为 `0700`，secret/manifest 文件按私有权限创建。Provider credential 只能通过 backend env 或 mounted file 进入相应进程，不能进入：

- Git、Helm values、前端 bundle 或 localStorage；
- API response、Evidence projection、日志或 Trace attribute；
- commit/push 输出或实施状态文档。

`local-backup` 生成 checksummed 私有归档。`local-restore` 在隔离 database 中验证格式、checksum、schema identity 和 row counts，失败时不覆盖活动数据。`local-reset` 需要 backup-first 与显式确认。

## 5. Provider and external effects

默认 `PROVIDER_GATEWAY_ENABLED=false`、`K8S_ENABLED=false`、`K8S_WRITE_ENABLED=false`。缺少 Provider endpoint、identity 或 secret 时，Worker 保持 standby 或 fail closed，不能用 fixture 代替实际调用。

Provider Gateway 开启后仍遵循：

- Kubernetes adapter 只读；
- GitHub read/write identity 分离，write 必须绑定已批准 Plan、repo/path/branch allowlist、content hash 与 logical operation key；
- Argo adapter 只读，不执行 sync/rollback；
- LLM 只能生成受 schema 和 Evidence 约束的建议，没有 credential 或 approval authority；
- Provider raw response 经过 bounds、sanitization、source time 和 identity 后才能成为 Evidence。

源码分支 push 不触发 Registry 写。CI 只有 read permission 并只构建不发布；Golden publish 与 hosted supply-chain validation 都只能由明确的 `workflow_dispatch` 触发。未实际运行的 hosted signing、attestation、Registry cleanup 保持 `NOT RUN`。

## 6. Internal ingress

Alertmanager webhook 位于独立 internal listener/Service，必须携带从只读 secret file 加载的 bearer。用户 Service 不路由 webhook，internal Service 不路由用户 API。Signal target 必须匹配固定 cluster/environment/namespace/workload allowlist 后才能进入领域层。

## 7. Evidence and Agent safety

日志、annotation、GitHub text、runbook、tool error 和模型输出均是不可信输入。Durable Evidence 需要 producer identity、schema hash、content/result hash、provenance 和 owning Incident/cycle；unsupported claim、foreign Evidence、secret-like field 和越权 action 不能获得 authority。

历史 Agent eval 报告只证明其记录的 exact revision/run，不能替代当前任务的真实 UI -> API -> Provider 验收。

## 8. Verification status

当前源码、Chart、运行时、数据和外部 Provider 的逐项 `PASS` / `FAIL` / `NOT RUN` 见 [实施状态](evidence/cloudops-implementation-status.md)。常用 focused checks：

```bash
go test ./internal/api ./internal/bootstrap/... ./internal/taskhandler/...
make helm-contracts
make check-naming
python3 scripts/check-immutable-workflow-dependencies.py
```
