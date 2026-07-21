# Agent Runtime

本文描述当前 V3 Incident Agent 的代码合同。目标设计仍以
[V3 Refactor Design](CloudOps-Incident-Agent-V3-Refactor-Design.md#11-agent-runtime)
为准；本文只记录当前源码已经具备的边界，不把离线 Eval、fixture 或静态检查替代为 Golden E2E。

## 1. 进程与所有权

Agent 只在 `cloudops-worker` 中运行。入口和装配路径为：

- [worker bootstrap](../internal/bootstrap/worker.go)：MySQL、runtime-generation guard、四个队列和 readiness。
- [production operation factory](../internal/bootstrap/worker_provider.go)：构造真实 provider 和五类 subject-bound operation。
- [operation registry](../internal/taskhandler/registry.go)：缺少 owning operation 时 fail closed。
- [task types](../internal/asyncjob/types.go)：冻结 queue、task type、subject 和 transition 映射。

`cloudops-api` 不调用模型、不执行 Agent loop，也不持有 Worker 的 Kubernetes、GitHub App、Argo、LLM 或观测凭据。

## 2. 唯一任务链

| Queue | Task type | Transition | 单步职责 |
|---|---|---|---|
| `investigate` | `investigation.advance` | `investigation.start` | 创建当前 cycle 的唯一 AgentRun，不调用模型或工具 |
| `investigate` | `investigation.advance` | `investigation.step` | 执行一次模型决定、一个批准的只读工具，或一次 synthesis |
| `investigate` | `remediation.prepare` | `remediation.prepare` | 只为已确认的 `restore_required_env` 生成确定性 Plan |
| `deliver` | `change.ensure_pr` | `change.ensure_pr` | 每个 task 最多推进 branch、commit、Draft PR 中的一次 GitHub write |
| `observe` | `delivery.observe` | `delivery.observe` | 每次只观察 GitHub、Argo 或 Kubernetes 中一个权威源 |
| `verify` | `verification.advance` | `verification.advance` | 每次只处理一个到期 check，并持久化 sample/状态 |

任务实现位于 [task handlers](../internal/taskhandler/)。Task lease、subject version、cycle、dedupe key 和 logical operation key 是执行边界；Handler 不能绕过 Task 直接启动第二套 Worker 链。

## 3. 调查状态机

[investigation start](../internal/taskhandler/investigation_start.go) 绑定 Incident、cycle、provider、model、prompt/tool identity 和冻结预算。[investigation step](../internal/taskhandler/investigation_step.go) 通过 typed checkpoint 恢复状态，并拒绝 stale basis、foreign subject、错误签名或越界 payload。

一次 step 只能是：

1. `decide`：模型返回 typed StateDelta；reducer 校验 action、claim、citation 和预算。
2. `tool`：只执行 reducer 已批准、签名匹配的一个只读工具合同。
3. `synthesize`：基于当前持久 Evidence 输出 diagnosis 或 insufficient evidence。

StateDelta 和 reducer 位于 [agent domain](../internal/agent/)。模型不能创建 Task、修改 Incident、生成任意查询语言、调用写工具或决定 Verification 通过。

## 4. Tool 与 Evidence

生产工具由 [worker operation assembly](../internal/bootstrap/worker_operations.go) 和 provider adapters 装配。允许的调查面是 Kubernetes、metrics、logs、traces、deployment context、change detail 和 runbook 的固定 typed contract；scope、repo、namespace、SHA、模板和 limit 均由服务端决定。

Evidence 持久化保存 bounded typed facts、provenance、content hash 和 authority，不保存 provider raw response、chain-of-thought、完整日志/trace、任意 PromQL/DSL 或凭据。Diagnosis 的 confirmed claim 必须引用当前 cycle 的有效 Evidence。

## 5. 预算、失败与恢复

- 模型调用、工具调用、step、Evidence、token、runtime 和 checkpoint 都有冻结上限。
- malformed typed output 最多进行一次结构化 repair；provider failure 不会被多数票或 retry 隐藏。
- lease takeover 复用已持久 checkpoint，不重复已完成的外部调用。
- policy/security/invalid input fail closed；transient dependency 才能进入 bounded retry。
- dead replay 创建新 generation，并重新校验当前 subject version。

队列和 shutdown 细节见 [Reliability](reliability.md)。

## 6. 当前证据状态

| 项目 | 状态 | 当前证据 |
|---|---|---|
| Runtime-bound Agent Eval v5 | `PASS` | [v5 quality report](evidence/phase-4-agent-quality-v5-report.md)：quality、prompt injection、secret canary 均通过 |
| Frozen manifest / reducer / runtime source identity | `PASS` | `eval/v5/manifest.json` 与 v5 report |
| 五类 production operation 源码装配 | `PASS` | `internal/bootstrap/worker_provider.go`、`internal/taskhandler/registry.go` |
| 当前 HEAD 的全量 test/race | `NOT RUN` | 本文档切片不运行代码测试 |
| Phase 3 clean-kind | `NOT RUN` | 当前没有新的 clean-cluster runtime 证据 |
| 真实 GitHub App/Argo/OAuth/LLM 集成链 | `NOT RUN` | 需要外部凭据与 live systems |
| Golden E2E / Phase 7A cutover | `NOT RUN` | Agent Quality PASS 不替代 Golden 或 live cutover |

## 7. 验证命令

以下命令是当前仓库的真实入口；运行 real-model 命令时只能把 key 注入模型子进程环境，不能把凭据写入报告。

```bash
go test ./internal/agent/... ./internal/taskhandler/... ./internal/asyncjob/...
go run ./cmd/cloudops-agent-eval -revision v5 -mode validate -split all
go run ./cmd/cloudops-agent-eval -revision v5 -mode guardrail -split guardrail
```

真实模型 quality、hostile surveys 和 gate 的 exact-SHA 命令及输出位置记录在 [v5 quality report](evidence/phase-4-agent-quality-v5-report.md)。
