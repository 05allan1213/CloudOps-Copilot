# Agent Runtime

CloudOps Agent 由 `cloudops-worker` 执行。`cloudops-api` 只提供 MySQL-backed Query/Command，不加载模型、调查工具或 Provider write credential。

## Ownership

- `internal/bootstrap/worker.go`：组合 MySQL、task runner、management readiness 与 shutdown。
- `internal/bootstrap/worker_provider.go`：校验 Provider Gateway 配置并构造 operation factory。
- `internal/bootstrap/worker_operations.go`：装配 subject-bound operations 与只读 Provider adapters。
- `internal/taskhandler/registry.go`：拒绝没有 owning operation 的 task。
- `internal/asyncjob`：负责 MySQL claim、lease、heartbeat、retry、checkpoint 与 replay。

Chart 通过显式配置启用 Provider Gateway。未启用时 Worker 使用 standby runner，只验证 durable store，不领取 Provider-backed task。

## Durable Task Model

| Queue | Operation | Responsibility |
| --- | --- | --- |
| investigate | investigation start/step | 创建或推进 current-cycle Agent Investigation |
| investigate | remediation prepare | 从 confirmed Evidence 生成有界且不可变的 Plan |
| deliver | change ensure PR | 推进一个可重放的 GitHub write step |
| observe | delivery observe | 观察一个 GitHub、Argo 或 Kubernetes source |
| verify | verification advance | 处理一个到期 check 并持久化 sample/result |

Task 绑定 Incident/cycle、subject version、dedupe key、logical operation key、lease generation 与 checkpoint hash。Handler 不能绕过 durable task 启动第二套执行循环。

## Evidence

一次 Agent step 只能执行一个 typed model decision、一个允许的只读 tool call，或一次基于当前 Evidence 的 synthesis。Reducer 拒绝 foreign subject/cycle、stale basis、未知 action、越界参数、unsupported claim 和无引用结论。

Evidence 只保留 bounded typed facts、source/collection time、producer identity、schema/content/result hash 与 provenance。Provider raw response、credential、chain-of-thought、任意 shell 和不受限 query 不进入 Evidence。

## Authority

模型没有 Provider identity、approval authority 或外部写能力。Agent 可以提出 immutable Action Card 或 Operation Plan，但 Authorization、credential 与 execution adapter 均由独立后端边界持有。

执行外部效果前，Worker 必须重验：

- exact material hash 和未过期 Owner Authorization；
- active Configuration Revision；
- effect-time Provider precondition；
- operation allowlist、target scope 与 write gate。

## Bounds And Failure

- model、tool、step、token、runtime、Evidence 和 checkpoint 均有固定上限；
- malformed structured output 只能按显式 policy 做 bounded repair；
- 只有 transient error 可以 retry，policy、permission 和 invalid input fail closed；
- lease takeover 从 durable checkpoint 继续，旧 owner 的 stale write 被 fencing 拒绝；
- Provider unavailable 不能变成 diagnosis success 或 synthetic Evidence；
- shutdown 停止新 claim，进行 bounded drain，再取消剩余 handler 与 heartbeat。

## Verification

```bash
go test ./internal/agent/... ./internal/taskhandler/... ./internal/asyncjob/...
go test ./internal/bootstrap -run 'Test.*(Worker|Provider)'
```

离线 `eval/` 数据集用于可重复的 Agent quality checks；它不替代真实 Provider 集成验证。
