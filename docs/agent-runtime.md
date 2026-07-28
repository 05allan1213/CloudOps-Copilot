# Agent Runtime

本文记录当前语义 Agent runtime 的代码边界。产品目标以 [实施规范](CloudOps-Implementation-Spec.md) 为准；离线 dataset、fixture 和历史报告不能替代当前 Provider 联调。

## 1. Ownership

Agent 只由 `cloudops-worker` 运行。`cloudops-api` 只负责 MySQL-backed Query/Command，不加载模型、调查工具或 Provider write credential。

Worker 装配路径：

- `internal/bootstrap/worker.go`：MySQL、standby/active runner、management readiness 与 shutdown；
- `internal/bootstrap/worker_provider.go`：Provider Gateway config validation 和 production operation factory；
- `internal/bootstrap/worker_operations.go`：subject-bound operations 与只读 Provider adapters；
- `internal/taskhandler/registry.go`：缺少 owning operation 时 fail closed；
- `internal/asyncjob`：MySQL task claim、lease、heartbeat、retry、checkpoint 与 replay。

本地 Chart 默认 `PROVIDER_GATEWAY_ENABLED=false`。此时 Worker 使用 standby runner，只验证 durable store，不领取任何 Provider-backed task。

## 2. Task chain

| Queue | Operation | 单步职责 |
|---|---|---|
| investigate | investigation start/step | 创建或推进一个 current-cycle AgentRun |
| investigate | remediation prepare | 只从 confirmed Evidence 生成受限 Plan |
| deliver | change ensure PR | 每个 task 最多推进一个可重放 GitHub write step |
| observe | delivery observe | 每次只观察一个 GitHub/Argo/Kubernetes source |
| verify | verification advance | 处理一个到期 check 并持久化 sample/result |

Task 的 Incident/cycle/subject version、dedupe key、logical operation key、lease generation 与 checkpoint hash 是执行边界。Handler 不能绕过 durable task 直接启动另一套 loop。

## 3. Evidence and authority

一次调查 step 只能进行一个 typed model decision、一个批准的只读 tool call，或一次基于当前 Evidence 的 synthesis。Reducer 拒绝 foreign subject/cycle、stale basis、未知 action、越界参数、unsupported claim 和无引用结论。

Evidence 只保留 bounded typed facts、source/collected time、producer identity、schema/content/result hash 与 provenance。Provider raw response、credential、chain-of-thought、任意 shell 或不受限 query 不进入 Evidence。

模型没有 Provider identity、approval authority 或外部写能力。任何 Operation Plan/Decision、Provider credential 与执行 adapter 都是独立后端边界。

## 4. Budgets and failure

- model/tool/step/token/runtime/Evidence/checkpoint 都有固定上限；
- malformed structured output 只能按显式 policy 做 bounded repair；
- transient error 才能 retry，policy/permission/invalid input fail closed；
- lease takeover 从 durable checkpoint 继续，旧 owner 的 stale write 被 fencing 拒绝；
- Provider unavailable 不能转成 diagnosis success 或 synthetic Evidence。

## 5. Current status

当前 Task 0 runtime 已保留 Agent/Task/Evidence 数据并提供读取投影，但默认 Provider Gateway 关闭。真实 LLM、Kubernetes、Metrics、Logs、Traces、GitHub、Argo 和 Registry 调查链只有在对应实施任务完成并取得当前 Browser/Network/Data/Provider/Console 证据后才能标记 `PASS`。

状态与证据见 [实施状态](evidence/cloudops-implementation-status.md)。Focused checks：

```bash
go test ./internal/agent/... ./internal/taskhandler/... ./internal/asyncjob/...
go test ./internal/bootstrap -run 'Test.*(Worker|Provider)'
```
