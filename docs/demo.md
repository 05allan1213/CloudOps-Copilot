# Demo

V3 只有一个演示场景：GitOps 删除 Demo Deployment 的非 Secret `REQUIRED_ENV`，进程保持 live 但 readiness 失败，并产生真实 error metric、结构化日志和 Trace；人类审批后 CloudOps 只通过 GitHub Draft PR 恢复配置，Argo负责 reconciliation，最后由 deterministic Verification 判定恢复。

历史 V2 direct-scale、fixture model、Compose fast-demo 或 Kubernetes patch 不能作为 V3 Demo/Golden 证据。

## 1. 命令面

根目录 [Makefile](../Makefile) 提供：

```bash
make preflight
make demo-up
make scenario-open-regression-pr
make e2e-gitops
make demo-down
```

- `preflight`：Phase 3 disposable kind 工具/资源/文件检查，不修改集群。
- `demo-up`：创建 disposable kind 并安装 Phase 3 platform/API/Demo/observability profile。
- `scenario-open-regression-pr`：用本机 human `gh` 身份在外部 GitOps repo创建固定 PR；不 merge。
- `e2e-gitops`：真实系统 fail-closed verifier；会运行 Helm-owned load generator并写 exact-SHA manifest。
- `demo-down`：只删除显式命名的 disposable kind cluster；不会自动执行。

实现入口为 [kind bootstrap](../server-monitor/scripts/v3-kind.sh) 和 [Golden harness](../server-monitor/scripts/golden-e2e.sh)。环境输入与 secret-safe 规则见 [Golden harness guide](evidence/golden-e2e-harness.md)。

## 2. 阶段 profile

| Profile | API | OAuth | Worker | Baseline verifier | 用途 |
|---|---:|---:|---:|---:|---|
| `phase3` | on | off | off | off | observability、Alertmanager ingress、Argo/Demo baseline |
| `phase4` | on | off | off | off | 继续使用 Phase 3 runtime；真实模型只做离线 Eval |
| `phase5` | on | on | off | on | OAuth/App/baseline 与 targeted operation gates |
| `phase6` | on | on | on | on | 全部 operation 注册后的完整 Worker/Workbench目标 |

冻结值见 `charts/cloudops/values-phase3.yaml` 至 `values-phase6.yaml`。缺 Secret 导致 CrashLoop 不能替代 profile 禁用；render contract 必须明确证明开关。

## 3. Platform 与 workload

[platform chart](../server-monitor/charts/cloudops-kind-platform/) 管理 MySQL、ECK resources、OTel/Tempo 等 kind platform assets；第三方 Operator/Chart 版本与 digest 在 [version lock](../server-monitor/deploy/kind/versions.env) 冻结。CloudOps 应用由 [charts/cloudops](../charts/cloudops/) 管理。

[Demo chart](../server-monitor/charts/cloudops-demo/) 包含：

- 两副本 Demo Deployment；`/livez` 始终 live，缺 `REQUIRED_ENV` 时 `/readyz` 和业务请求失败。
- normal Service 与 `publishNotReadyAddresses=true` 的 `demo-diagnostics` ClusterIP Service。
- PodMonitor、PrometheusRule、OTLP trace configuration。
- Helm test hook `cloudops-demo-load-generator`：5 requests/s、concurrency 1、2s timeout、最长30分钟；无 ServiceAccount token。

Load generator不属于 Argo desired state、不常驻、不用 raw kubectl创建；hook delete policy和 Job TTL拥有清理。

## 4. GitOps 边界

[AppProject](../deploy/platform/argocd/appproject.yaml) 只允许固定 repo、path、destination namespace 和 resource kinds。[Application](../deploy/platform/argocd/application.yaml) 开启 automated reconciliation，但 CloudOps和CI都不能调用 sync/rollback。

仓库中的 [healthy/regression fixtures](../deploy/contracts/gitops-demo/) 只用于 contract validation，不会由 bootstrap apply，也不能替代外部 GitOps repo、human merge、GitHub Actions 或 Argo deployed revision。

`scenario-open-regression-pr` 会验证外部 checkout位于 exact `origin/main`、worktree clean、actor不是 bot，并证明 diff 只移除一个 `REQUIRED_ENV`。它只 push branch/create PR；merge必须由人完成。

## 5. Golden verifier

`make e2e-gitops` 在执行任何 Golden判断前检查：

- clean source exact SHA 与成功的 exact-SHA GitHub Actions。
- Agent Quality PASS 且相关 runtime material 未漂移。
- 独立 GitHub read/write App installation tokens 与权限。
- live kind、API/Worker readiness、Kubernetes write denial。
- live Argo Application与 observer sync denial。
- real LLM completion、real oauth2-proxy GitHub session。
- API/Worker digest-pinned image和 OCI source revision。

随后验证 regression PR/checks、bad Argo revision、Incident、AgentRun、Plan/Approval、remediation PR/checks、fix revision、60s common-window Verification和 ResolutionReport。任何缺项都 fail closed，未到达项写 `NOT RUN`。

证据输出：

```text
docs/evidence/<cloudops-exact-sha>/manifest.md
```

Manifest记录 source/image/GitOps SHAs、Actions URL/conclusion、Argo revision、公开 IDs、approval hashes、model identity、tool/token usage、分段耗时、版本、命令、资源和已知限制；不记录credential或 provider raw response。

## 6. 当前状态

| Gate | 状态 | 说明 |
|---|---|---|
| Make/脚本/Chart/GitOps fixture命令与路径 | `PASS` | 当前源码存在，本文档切片完成静态路径检查 |
| Agent Quality v5 | `PASS` | [v5 report](evidence/phase-4-agent-quality-v5-report.md) |
| Golden shell safety contract | `PASS` | `server-monitor/scripts/check-golden-e2e-contract.sh` 已纳入 `make golden-e2e-contracts` |
| 当前 HEAD Chart/render/shell tests | `NOT RUN` | 本文档切片不运行实现测试 |
| Phase 3 clean-kind bootstrap/check/down | `NOT RUN` | 当前没有新的 clean-cluster evidence |
| 真实 OAuth/App/Actions/Argo/LLM integration | `NOT RUN` | 需要外部系统和凭据 |
| Regression human merge与完整故障→恢复 | `NOT RUN` | 未执行 `make e2e-gitops` |
| Exact-SHA Golden manifest | `NOT RUN` | 不存在本次 live run artifact |
| Phase 7A live cutover / `CUTOVER-V3` | `NOT RUN` | marker writer源码不代表已执行 cutover |
| Phase 7B cleanup / `CONTRACT-V3` | `NOT RUN` | 必须晚于独立接受的 Release A |

## 7. 静态检查与运行注意

无外部写的静态入口：

```bash
make kind-render
make helm-contracts
make argocd-contracts
make golden-e2e-contracts
```

`make demo-up`、`make scenario-open-regression-pr`、`make e2e-gitops` 和 `make demo-down` 会改变本地集群或外部 Git repository 状态，只能由操作者在确认 context、凭据、worktree 和 cleanup plan 后显式执行。
