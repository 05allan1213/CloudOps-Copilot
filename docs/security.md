# Security

本文记录当前 Local Owner runtime 的安全边界，不构成生产威胁模型、合规认证或公开部署建议。CloudOps 只支持本机单 Owner；LAN、远程、公开或多用户模式需要新的认证与架构决策。

## 1. Local Owner boundary

应用只通过 `kubectl port-forward --address=127.0.0.1` 暴露到 loopback。浏览器无需登录，服务端为 mutation 固定记录：

```text
subject=local-owner provider=local login=owner role=owner
```

当前合同没有 OAuth、账号、session、role map、CSRF token、Authorization header 或 frontend token storage。Mutation 仍受 Origin/CORS、body bounds、idempotency、expected version 与 exact hash 保护。

## 2. Process and database identities

| 身份 | 允许 | 禁止 |
|---|---|---|
| `cloudops-api` | MySQL Query/Command、静态 UI、用户 API、internal bearer webhook | Kubernetes token、async claim、schema migration |
| `cloudops-worker` | MySQL task runtime、bounded Provider read、精确授权的 allowlisted operation | 任意 shell/YAML/kubectl、未授权 write、schema migration |
| `cloudops-migrate` | forward-only DDL 与 schema verification | 用户 API、Provider credential、后台任务 |
| `cloudops-demo` | Scenario HTTP/metrics/log/trace workload | CloudOps database、Provider credential、外部 write |
| MySQL | durable domain/config/evidence truth | 浏览器直接访问 |

API、Worker、Migrate 使用不同 MySQL user/Secret。MySQL root 仅用于本地 bootstrap、受控 backup/restore 与数据库管理；Chart contract 拒绝 root credential 下放。

## 3. Three-layer authority and Kubernetes write gate

Provider read、model suggestion 与 external effect 是三层不同 authority：

1. Investigation/Consultation 可以读取 bounded Evidence；
2. Agent 可以提出 immutable Action Card/Operation Plan；
3. 只有 Owner 对 exact material hash 的未过期 Authorization 才可能允许 effect。

Kubernetes operation 还必须同时满足 allowlisted Deployment scale、active Configuration Revision、effect-time precondition、Worker `K8S_WRITE_ENABLED=true` 和对应 Role。Scenario inactive 时 write gate 为 false，Scenario scale RBAC 的 `can-i` 结果为 `no`。模型输出、URL 参数、Context Link 或浏览器字段都不能授予 authority。

## 4. Browser command safeguards

- exact `/api/v1` route；
- same-origin/allowlisted `Origin`；
- JSON body size limit 与 unknown-field rejection；
- bounded `Idempotency-Key`；
- expected row version，必要时绑定 expected content hash；
- stable Problem Details、request ID 与 trace ID；
- material/config drift 使既有 Authorization 失效。

同一 idempotency key 不能授权不同 payload。Owner identity、resource UUID、command kind 与 canonical payload 一起构成 scope。

## 5. Secrets and backups

本地 secret、bootstrap credential、runtime PID/log 与 backup 位于 Git 忽略的 `.cloudops/`，目录权限为 `0700`，secret/manifest 文件按 `0600` 创建。Provider credential 只能通过 backend env 或 mounted file 进入相应进程，不能进入 Git、Helm values、前端 bundle、localStorage、API response、Evidence、日志、Trace 或文档。

Settings 的 secret API 只接受 write，读取仅返回 metadata/fingerprint。Agent 与集成测试只记录 provider/model、token usage 与 hash，不输出 secret value。

## 6. Provider and external effects

- Kubernetes：read 为 bounded；write 仅为 exact-authorized Scenario Deployment scale。
- Alertmanager：internal webhook 必须携带只读 secret file 中的 bearer；target 必须命中 allowlist。
- Logs/Trace/annotation/GitHub text/runbook/tool error/model output 均是不可信输入，必须 sanitize 并携带 provenance。
- LLM 只能生成 Evidence-bound 内容，没有 credential、approval 或 execution authority。
- GitHub/Argo/Registry 是 optional external branches；没有凭据、Plan/Authorization 与明确运行权限时保持 `NOT RUN`。
- 普通源码 push 不触发 Registry publish；Golden image publish 只由显式 workflow dispatch 执行。

## 7. Evidence integrity

Durable Evidence 保存 producer identity、source time、query/context/config revision、schema/content/result hash、provenance 与 owning subject/cycle。Unsupported claim、foreign Evidence、secret-like field 与越权 action 不能获得 authority。历史截图、fixture 或旧 SHA 不替代当前 Provider evidence。

## 8. Verification status

```bash
go test ./internal/api ./internal/bootstrap/... ./internal/taskhandler/...
make helm-contracts
make check-naming
python3 scripts/check-immutable-workflow-dependencies.py
```

安全相关代码或配置变更应同时运行对应 Go test、Helm contract、naming check 和 workflow dependency check。公开或多用户部署不在当前支持边界内。
