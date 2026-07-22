# Security

本文记录 V3 当前安全边界和明确未验证项。它不构成生产威胁模型、合规认证或凭据审计证明。

## 1. 身份与进程隔离

| 身份 | 允许 | 禁止 |
|---|---|---|
| `cloudops-api` | MySQL Query/Command、Alertmanager bearer ingress、用户 API | Kubernetes token、LLM、GitHub App write、Argo polling |
| `cloudops-worker` | namespace-scoped K8s read、bounded observability read、GitHub/Registry/Argo read、审批后 GitHub Draft PR write、LLM | Kubernetes write、Argo sync/rollback、任意 shell/query/URL |
| `cloudops-baseline-verifier` | Demo/K8s/Argo/observability/Registry read；baseline tables write | GitHub write、LLM、Incident mutation |
| `oauth2-proxy` | GitHub OAuth、session cookie、可信 user header | GitHub repository write、向 API 转发 access/session secret |

Chart ownership可在 [API Deployment](../charts/cloudops/templates/api.yaml)、[Worker Deployment](../charts/cloudops/templates/worker.yaml)、[baseline verifier](../charts/cloudops/templates/baseline-verifier.yaml) 和 [RBAC](../charts/cloudops/templates/rbac.yaml) 中审计。API ServiceAccount token 默认不挂载；Worker 只允许 `demo` namespace 的只读资源。

## 2. OAuth、角色与浏览器命令

[oauth2-proxy authenticator](../internal/apiv3/oauth_proxy_auth.go) 只接受 sidecar 覆盖的单一 `X-Auth-Request-User`，并拒绝 `Authorization`、proxy authorization、OAuth access token header 和 session cookie 泄漏到 API。

产品角色只有：

- `viewer`：读取 Incident、Evidence、完整 Plan/diff、delivery、Verification、ResolutionReport 和 SSE。
- `operator`：包含 viewer，并可执行 start investigation、close、approve/reject。

Mutation 还必须同时满足：

- operator role。
- allowlisted `Origin`。
- identity-bound、短期 `X-CSRF-Token`。
- bounded `Idempotency-Key`。
- expected version/hash。

相关实现位于 [API middleware](../internal/apiv3/middleware.go)、[transport middleware](../internal/apiv3/transport_middleware.go) 和 [command idempotency](../internal/apiv3/idempotency.go)。

## 3. 凭据处理

Phase 5/6 Chart 只引用预创建 Secret；敏感值通过只读文件路径进入进程。主要合同在 [values](../charts/cloudops/values.yaml)：

- `V3_LLM_API_KEY_FILE`
- GitHub read/write App private key files
- `ARGOCD_TOKEN_FILE`
- Elasticsearch username/password/CA files
- 独立 OAuth client/cookie Secret 与 API CSRF signing Secret

禁止把 private key、token、cookie、provider raw response、Prompt 或 chain-of-thought 写入 Git、日志、Evidence Manifest、MySQL projection 或浏览器存储。`111.txt` 不是任何运行命令或文档的默认输入。

## 4. 外部副作用边界

- CloudOps 只能通过 GitHub Draft PR 修改 GitOps desired state。
- Argo adapter 和 Worker identity只能读取 Application、revision、sync 和 health；不能 sync、rollback 或 override。
- Kubernetes adapter只读；`K8S_WRITE_ENABLED` 必须为 false。
- GitHub branch/commit/PR write 被 approval hashes、base/head/tree/post-image、path/repo allowlist 和 logical operation key 绑定。
- `make scenario-open-regression-pr` 使用操作者的 human `gh` 身份制造固定 regression PR；runtime GitHub App不拥有故障注入能力，命令不 merge。

Argo repo/path/kind 边界见 [AppProject](../deploy/platform/argocd/appproject.yaml)、[Application](../deploy/platform/argocd/application.yaml) 和 [GitOps contract check](../server-monitor/scripts/check-argocd-gitops-contract.sh)。AppProject 不能限制对象名称，因此名称/manifest shape 由单一 path、adapter allowlist 和 rendered policy共同约束。

## 5. Agent 与 Evidence 安全

Agent 只消费标记 authority 的 typed facts。日志、Trace attribute、Kubernetes annotation、GitHub text、runbook 和 tool error 都视为 untrusted data，不能覆盖 system policy、scope 或工具合同。

[StateDelta reducer](../internal/agent/state_delta.go) 和 [investigation operation](../internal/taskhandler/investigation_step.go) 拒绝：

- write tool、scope escape、foreign Evidence、invalid signature。
- unsupported confirmed claim 和未引用 Evidence。
- secret/canary adoption、prompt injection continuation。
- budget overrun、stale checkpoint、foreign subject。

当前真实模型 hostile-input 证据见 [Agent Quality v5 report](evidence/phase-4-agent-quality-v5-report.md)。

## 6. 当前状态

| 控制 | 状态 | 说明 |
|---|---|---|
| API OAuth header/credential rejection source contract | `PASS` | `internal/apiv3/oauth_proxy_auth.go` 及其 tests |
| Role/CSRF/Origin/idempotency source contract | `PASS` | `internal/apiv3/` |
| API/Worker/Verifier Chart credential and SA separation | `PASS` | `charts/cloudops/` static profile contracts |
| Agent prompt-injection/secret-canary v5 | `PASS` | 真实模型各 3/3，见 v5 report |
| GitHub/Argo/Kubernetes no-bypass source contract | `PASS` | adapters、RBAC、Golden static contract |
| 当前 HEAD 完整 security test/shellcheck/Chart render | `NOT RUN` | 本文档切片不运行代码/Chart测试 |
| 真实 OAuth login/session/browser flow | `NOT RUN` | 需要 live GitHub OAuth App |
| 真实 GitHub App installation/ruleset/negative permissions | `NOT RUN` | 需要 hosted identity 和 repository scope |
| 真实 Argo token `can-i` 与 live Application | `NOT RUN` | 需要 live kind/Argo |
| Secret rotation、production TLS、backup/DR、external audit | `NOT RUN` | Demo scope不宣称完成 |

## 7. 静态与 focused 命令

```bash
go test ./internal/apiv3 ./internal/agent/... ./internal/infra/githubwrite ./internal/infra/argocdread
make helm-contracts
make argocd-contracts
make golden-e2e-contracts
```

真实凭据检查只允许通过 [Golden harness](evidence/golden-e2e-harness.md) 的显式文件参数执行；缺凭据时必须为 `NOT RUN`。
