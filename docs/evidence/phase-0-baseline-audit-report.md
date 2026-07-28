# Phase 0 - V3 规范与基线审计报告

> 审计状态：PASS
>
> 唯一规范来源：`docs/CloudOps-Incident-Agent-V3-Refactor-Design.md`
>
> 审计原则：以审计开始时的 live worktree 为事实源；旧 V2 文档仅作为迁移输入。

## 1. 审计基线

采集时间：`2026-07-18T12:08:20+08:00`（Asia/Shanghai）。

| 项目 | 基线值 | 状态 |
|---|---|---|
| 仓库根目录 | `/home/monody/k8s/CloudOps-Copilot` | PASS |
| 当前分支 | `main` | PASS |
| HEAD | `2f7e426d69a4ed7d8d32ec3ca83c13af0c71586e` | PASS |
| upstream | `origin/main`，ahead `0` / behind `0` | PASS |
| HEAD subject | `docs:remove folder` | PASS |
| 已暂存 tracked 变更 | 无 | PASS |
| 未暂存 tracked 变更 | 无 | PASS |
| 未跟踪文件 | `docs/CloudOps-Incident-Agent-V3-Refactor-Design.md` | PASS，来源已显式记录 |
| V3 设计 Git blob ID | `c8a5ce011d54e730541289be5481f52e43cec8d4` | PASS |
| V3 设计 SHA-256 | `c04d91aa60bcff6796fff90771cd5a528559e0b62889ef85d0d521a70fe78924` | PASS |
| V3 设计大小 | `135936` bytes，`3188` lines | PASS |

说明：设计文档在审计开始时尚未被 Git 跟踪，因此上述 blob ID 是对 live worktree 内容执行 `git hash-object` 得到的内容地址，而不是 `HEAD:<path>` 的 blob。该文件是审计开始时唯一未提交文件；后续 Phase 0 产出必须与这组原始值区分。

### 1.1 基线命令

```text
git branch --show-current
git rev-parse HEAD
git status --short --untracked-files=all
git status --porcelain=v2 --branch
git diff --name-only
git diff --cached --name-only
git ls-files --others --exclude-standard
git hash-object docs/CloudOps-Incident-Agent-V3-Refactor-Design.md
sha256sum docs/CloudOps-Incident-Agent-V3-Refactor-Design.md
wc -l docs/CloudOps-Incident-Agent-V3-Refactor-Design.md
```

## 2. 状态语义

- `PASS`：该 Phase 0 审计控制已执行，且证据足够。
- `FAIL`：当前 V2 资产不满足 V3 目标，或静态/运行态检查发现真实问题。它是后续 Phase 输入；只要问题被完整识别且 Phase 0 本身不要求实现修复，就不自动导致 Phase 0 Gate 失败。
- `NOT RUN`：缺少本阶段授权、凭据、资源、可达外部系统，或该验证属于后续 Gate。不得用历史报告、mock 或旧 SHA 替代。

## 3. Live Repository Audit

本节证据路径均以仓库根为基准；为保持表格可读，`internal/**` 默认指 `server-monitor/server-web/internal/**`，`frontend/**` 默认指 `server-monitor/frontend/**`。Migration、Chart、workflow 和脚本使用完整仓库相对路径。

### 3.1 仓库、Go modules 与进程

| 检查项 | 状态 | 当前事实与证据 | 后续输入 |
|---|---|---|---|
| Git tree inventory | PASS | `HEAD` 共 341 个 tracked 文件：`server-monitor/**` 337、workflow 2、`README.md` 与 `.gitignore` 各 1；其中 Go 194、前端 `src` 49、raw manifest 18、Chart template 20 | Phase 1 以 live tree 而非旧报告为输入 |
| Root module | FAIL | 根目录无 `go.mod` / `go.sum` / `go.work`；`cmd/`、`internal/`、`migrations/` 只是未被 Git 跟踪的空目录，不能算实现 | Phase 1 机械建立一个 root module |
| Nested modules | PASS（审计）/ FAIL（V3） | `server-monitor/server-web/go.mod` 为 `module server-web`，通过 `replace server-monitor/pkg => ../pkg` 依赖 `server-monitor/pkg/go.mod` | Phase 1 吸收 `pkg` 并删除 nested module/replace |
| Executable entrypoints | PASS（审计）/ FAIL（V3） | 产品入口只有 `server-monitor/server-web/main.go`；另有 nested `server-monitor/server-web/cmd/migrate/main.go` | Phase 1 建 `cloudops-api`、`cloudops-worker`、`cloudops-migrate` |
| Process ownership | FAIL | `server-monitor/server-web/app.go:44-112,144-153` 在同一 API 进程组装并启动 Agent、Remediation、Delivery/Verification Worker | Phase 1 把所有 Worker loop 移出 API binary |
| Worker loops | PASS（已定位） | Agent：`internal/service/agentruntime/service.go:527`；Remediation：`internal/service/remediation/worker.go:36`；Delivery/Verification：`internal/service/deliveryverification/service.go:561` | Phase 2 逐 loop 迁到统一 async task runner |
| Runtime AutoMigrate | FAIL | `internal/startup/infra.go:76-95` 在运行时调用 `database.Migrate`；`internal/infra/database/migrate.go:16-53` 执行 GORM `AutoMigrate` | Phase 1 移除 runtime schema mutation；Migrate 只用 Goose |
| Current nested modules package parse | PASS | `go list -mod=readonly ./...` 在 `server-web` 与 `pkg` 两个 module 均成功 | 仅证明当前 nested module 可解析，不是 root-module Gate |

### 3.2 当前领域与数据库

| 检查项 | 状态 | 当前事实与证据 | 后续输入 |
|---|---|---|---|
| Goose history | PASS | `server-monitor/server-web/migrations/00001-00006` 共 738 行，六个文件均 tracked；内容地址已写入 `docs/migration-ledger.md` | 永久不可修改，只能新增 forward migration |
| Goose business tables | PASS（已盘点） | 15 张：`incidents`、`agent_runs`、`agent_steps`、`incident_signals`、`incident_events`、`evidence_items`、`outbox_events`、`incident_correlation_locks`、`changes`、`remediation_plans`、`remediation_approvals`、`change_requests`、`verification_runs`、`verification_checks`、`postmortems` | 按 ledger expand/backfill/cutover/contract |
| AutoMigrate legacy tables | FAIL | `internal/model/models.go:5-20` 仍管理 `users`、`host_groups`、`host_group_members`、`alert_rules`、`notification_channels`、`alert_histories`、`diagnosis_reports`、`diagnosis_feedback`、`pending_actions`、`audit_logs` | 先用 forward migration 固化/归档，再删除 AutoMigrate 路径 |
| Incident state machine | FAIL | `internal/incident/types.go:11-31` 有 11 个顶层状态及 `FAILED`；`internal/service/incident/service.go:280-339` 的 resolved Signal 在部分状态仍可直接 `RESOLVED` | Phase 2 压缩为 7 状态；只有 passing Verification 可 resolve |
| Lease ownership | FAIL | lease 分散在 `agent_runs`（00002）、`change_requests`（00004）和 `verification_runs`（00005），无统一 `lease_generation`，claim 不是统一 MySQL `NOW(6)` 合同 | Phase 2 收敛到 `async_tasks` 唯一 lease |
| Outbox semantics | PASS（已纠偏） | `outbox_events` 只有 `published_at/attempts/last_error`；接口/实现仅 Add 与 PendingCount（`incident/repository.go:51-55`、`incidentmysql/repository.go:400-417`），没有 relay、claim、mark-published | 它是领域事件审计输入，不得直接转换成工作 Task |
| Change/approval compatibility | FAIL | `00003` 的 `changes` 原行可变；`00004` Approval 不绑定 V3 所需 base/post-image/tree/policy/verification/evidence 全 hash | 旧 Approval 禁止产生新外部写；仅已存在完整 Draft PR/merged state 可只读 reconcile |
| Verification/Postmortem | FAIL | `00005` 无 cycle/no-change/inconclusive/sample；`00006` 是 V2 `postmortems`，不是 V3 `resolution_reports` | 不兼容 Run 取消并回调查；Postmortem 只读 archive |
| Real MySQL migration/data audit | NOT RUN | 本轮未连接任一现存 MySQL、未执行 up/down、backfill、row count、hash 或 EXPLAIN | Phase 2 真实 MySQL Gate；不得复用 V2 历史报告 |

### 3.3 API、Agent、外部副作用与前端

| 检查项 | 状态 | 当前事实与证据 | 后续输入 |
|---|---|---|---|
| API version | FAIL | `internal/router/router.go:53-111` 暴露 local login、`/api/v2`、admin remediation 与 `/api/v2/demo/**` | Phase 2/6 收敛 `/api/v3` Command/Query/SSE |
| Webhook boundary | FAIL | Alertmanager webhook 与用户 API 共用同一 Gin listener，且无 V3 独立 Bearer listener | Phase 1/2 拆 internal listener 与认证 |
| Agent foundation | PASS（可迁移） | `internal/agent/graph/**` 有 Eino typed graph，AgentRun/Step/Evidence/checkpoint 有持久化基础 | Phase 4 改 StateDelta、deterministic sufficiency、task-per-step |
| Agent read bounds | FAIL | `internal/agent/tool/readonly_tools.go:117` 仍允许模型提供 PromQL；存在 `k8s.get_logs` 与 V2 工具合同 | Phase 4 只允许八个固定模板工具 |
| Direct mutation/demo | FAIL | `internal/infra/k8schange/**`、`internal/service/fastdemo/**` 和 `/api/v2/demo/**` 仍可走受控直接 Kubernetes 修复 | Phase 7 前删除，不能作为 V3 Golden 证据 |
| GitHub write recovery | FAIL | Remediation Worker 一次推进 branch/commit/PR，缺 V3 分 phase marker/reconcile/fencing 合同 | Phase 5 每个 Task 最多一次外部写 |
| Frontend page boundary | PASS（可迁移） | `frontend/src/router/routes.ts:3-8` 只有 `/incidents` 与 `/incidents/:incidentId` 两个业务页面；`navigation.ts:8-10` 只有一个主入口 | 保留路由与展示壳 |
| Frontend auth | FAIL | `authStorage.ts:1-39` 使用 localStorage JWT；`api/client.ts:28-44` 注入 Bearer；`LoginPage.vue` 是本地账号登录 | Phase 5 改 oauth2-proxy cookie 身份 |
| Frontend Query contract | FAIL | `api/incidents.ts:18-85` 全为 `/api/v2`；页码/envelope 不是 V3 cursor/problem+json | Phase 6 适配 `/api/v3` |
| Viewer access and commands | FAIL | viewer 不加载 remediation；`IncidentRemediation.vue` 只读且不展示完整 diff/hash/Decision，无 Approve/Reject | viewer 可看，operator 才可 Command |
| GET projection purity | FAIL | `internal/handler/incident_workbench.go:322-360` 的 Workbench resources GET 直接查询 Kubernetes | Phase 6 只读 MySQL projection/Evidence |
| SSE resume | FAIL | client/server 均未处理 `Last-Event-ID`（`useIncidentRealtime.ts:44-47`、`incident_workbench.go:547-571`） | Phase 6 适配 cookie SSE 与 refresh hint |
| Frontend detail model | FAIL | 仍是 10 个平级 section、11 个 V2 大写状态、Postmortem，且缺 `inconclusive` | 收敛四区、7 状态、ResolutionReport |

### 3.4 部署、观测栈与 CI

| 检查项 | 状态 | 当前事实与证据 | 后续输入 |
|---|---|---|---|
| Deployment source count | FAIL | Compose、18 个 raw Kubernetes YAML、Helm Chart 三套并存；Makefile 仍提供 `compose-*`、`deploy-k8s`、`deploy-helm` | 最终只保留 kind + Helm；Argo 只管 Demo |
| Compose parse | PASS | `docker compose --env-file .env.example config --services` 成功，展开 14 个服务 | 只证明旧 Compose 可解析；该路径目标为 DELETE |
| Helm lint/render | PASS | `helm lint charts/server-monitor` 通过；默认 render 54 个资源 | 只证明旧 Chart 语法；不证明 V3 ownership |
| Chart ownership | FAIL | Chart 只有 `server-web`，无 API/Worker/Migrate/oauth2-proxy；默认管理 MySQL 和整套数据组件 | Phase 1/3 按 V3 ownership 重建 `charts/cloudops` |
| Observability selection | FAIL | 默认使用手写 Prometheus、Alertmanager、Grafana、ES/Kibana、Fluent Bit、Jaeger，并带 VictoriaMetrics/Kafka/Redis | Phase 3 改 Prometheus Operator + ECK/Filebeat + OTel/Tempo |
| Argo assets | FAIL | 无 AppProject/Application 与单 source GitOps Demo；tag deploy 仍由 CI 直接 Helm 到集群 | Phase 3/5 建 read-only Argo 边界；删除直接 deploy job |
| RBAC/container hardening | FAIL | SA 可读 `pods/log`，可选 Deployment write/Node ClusterRole；缺系统性 automount=false、readOnlyRootFilesystem、seccomp | Phase 1/3 最小权限与负例 |
| Secret model | FAIL | Chart/raw values 包含本地占位密码并创建 Secret；存在 ignored `server-monitor/docker/kubeconfig`（未读取） | V3 只引用预创建 Secret；后续 cleanup 删除 kubeconfig |
| Image provenance pattern | PASS（可迁移） | Dockerfile base digest、OCI revision/source/version 与 non-root runtime 可保留 | 为 V3 API/Worker exact-SHA image 适配 |
| Workflow pinning | PASS | 42/42 external `uses:` 固定 40 字符 SHA | 保留静态供应链控制 |
| CI live path correctness | FAIL | `.github/workflows/ci.yaml:100-116` 仍执行已不存在的 `internal/copilot/nlu/eval` 与 `internal/copilot/runbook/eval` | Phase 1 更新 root/current package Gate；本轮不改 CI 行为 |
| CI trigger/Gate coverage | FAIL | trigger 仅覆盖 `server-monitor/**`；缺 V3 migration static、secret/RBAC/Auth 负例、真实 MySQL async、kind smoke、MANUAL_GOLDEN | 各 owning Phase 逐步补齐 |
| Static delivery validators | PASS | actionlint、shell `bash -n`/shellcheck、Helm lint、Kubeconform、Promtool 均通过 | 作为后续 CI 可迁移基础 |
| V3 resource peak | NOT RUN | 旧静态 manifests 约 `3376Mi` requests / `7872Mi` limits；这不是 V3 Operator 栈实测 | Phase 3 干净 kind 实测并冻结 |

### 3.5 本机外部运行态

| 检查项 | 状态 | 结果 |
|---|---|---|
| Docker daemon | PASS | Server `29.3.0` 可达 |
| 历史 Compose | FAIL（非 V3） | 两组旧 V2 容器仍在运行；镜像 revision 为 `8212b96...` 或 `2acec159...`，不是当前 HEAD；`cloudops-v2-demo-server-web-1` 处于 restart/exit 1 |
| kind inventory | PASS | `kind get clusters` 返回 `cloudops-demo` |
| kind API reachability | FAIL | 使用现有 ignored kubeconfig 只读查询时，API `https://cloudops-demo-control-plane:6443` 持续返回 EOF；未执行修复或清理 |
| Runtime provenance | FAIL | 现存 Pod/容器不能证明 `2f7e426...`，更不能证明未提交的 V3 文档或后续 V3 实现 |
| GitHub/Registry/Argo/LLM live validation | NOT RUN | 本阶段未使用外部凭据，未产生网络写或 hosted run |
| Golden E2E | NOT RUN | V3 实现、凭据、branch rules、Argo/Operator 栈和真实模型均未就绪；历史 V2 direct-scale Demo 不可替代 |

未停止、重启、删除或修复任何现有容器/集群，避免改变用户外部状态和部署结果。

### 3.6 历史 V2 文档

| 输入 | 状态 | 规范地位 |
|---|---|---|
| `/home/monody/k8s/docs/CloudOps-Copilot-V2-Refactor-Implementation-Spec.md` | PASS（已审计） | 2989 行，SHA-256 `5ccc461aa907bd5883bdf31da4dd1dcb0ab34a198d39f64e523292547cdf5c55`；其自称 authoritative 的第 3-7 行已被 V3 设计取代，只作迁移输入 |
| Historical `doc/` inventory | PASS | exact source `8212b96ad07d5375a37280b8a2619e2e7f56fda0`（`chore: finalize V2 interview delivery and observability validation`）共 6 份，已按 Git object 完整审计 6/6；不用会随未来提交漂移的 `HEAD^` |
| `8212b96...:doc/adr/0038-phase-8-immutable-release-and-safe-compatibility.md` | PASS（历史输入） | 24 行 / 2408 bytes；blob `4717950a52a8ce89d5fdd10e8e5576bbecf71e4a`；SHA-256 `707275602569fe8e6e2acc30cb2e316afbab54200c00ceec40cc90f5f3e4e86a`。immutable digest 思想可迁移；旧 release/Cosign Gate 不覆盖 V3 |
| `8212b96...:doc/refactor/10-migration-ledger.md` | PASS（历史输入） | 85 行 / 10419 bytes；blob `263683b03d7a19024ba80327ff17ba931bbb6f81`；SHA-256 `5104071108467082e240293fe4ab14ae166a1c9041639d6298b7d7eb3b860540`。只提供旧资产线索，旧 ledger 状态不继承 |
| `8212b96...:doc/refactor/11-risk-register.md` | PASS（历史输入） | 142 行 / 21040 bytes；blob `69dacad5df2e5ee0b34e74d0f03ddec39511b1b5`；SHA-256 `0ecb84b837fb4abed97dc05103bd6eb96387c87e42d72e186c5bfe8f0f21a8c1`。只提供风险线索，旧 owner/status 不继承 |
| `8212b96...:doc/refactor/phase-8-cleanup-candidates.md` | PASS（历史输入） | 20 行 / 4796 bytes；blob `a5b84546448f5d54c48b51995c9dcd6d1006a098`；SHA-256 `3ac0127a9ed5efcdf3ed65857d7538edd8cd2ff99992e175b45729b9effb8224`。旧 caller/parity/rollback 清单可用于迁移盘点；V2 defer/block 决定不能否决 V3 DELETE |
| `8212b96...:doc/refactor/phase-8-delivery-cleanup-final-report.md` | PASS（历史输入） | 370 行 / 24468 bytes；blob `72278879957b84dbb8d2e0e6a385ecd353e469e4`；SHA-256 `0b7c8cd0e8ee5baa6c6c31acf5c0ff8f1be90214005faf5ca815355becd428cb`。旧 module/caller/validation 仅作线索；其 image、kind、staging、production 证据不是当前 SHA 或 V3 Gate 证据 |
| `8212b96...:doc/refactor/v2-fast-demo-end-to-end-validation-report.md` | PASS（历史输入） | 174 行 / 9271 bytes；blob `4497af634b0597ba7bc6ba92e5453e7e30ca5a0a`；SHA-256 `03668ce211077e26864bb3eeda979bc4909a953f91450854d46d8701d66b0e8e`。scale + direct Kubernetes + fixture model，不是 V3 Golden E2E 证据 |
| `/home/monody/k8s/docs/CloudOps-Copilot 云原生与 Agent 升级审计报告.md` | PASS（历史输入） | 709 行 / 42330 bytes；SHA-256 `13b5916a65b9129c38f076ddb33f33f1be6440824ccfd5bca4e13b47371c6d88`。早期审计的 `server-web` 单体、固定流水线和 direct-write 问题可迁移；Kafka/Strimzi/NATS、Loki、controller-runtime、host/dashboard、break-glass 等选型均被 V3 取代 |
| HEAD 中的 `doc/` | PASS | 已不存在；禁止恢复 `doc/` / `docs/` 路径分裂 |
| 审计起点的 `README.md` | FAIL | 以当前语气描述 V2/Compose/Redis/Kafka/direct demo，且有 7 个失效 `doc/**` 链接 | 该发现已在 Phase 0 通过 authority banner、`docs/**` 链接和真实项目树修正；其余 V2 实施描述继续明确为迁移输入 |
| Phase 0 后的 `README.md` 文档权威性 | PASS | 首屏指向 V3 唯一规范；相对 Markdown 链接均存在；没有 `doc/**` 链接；未声称 V3 业务已实施 | 实施事实只可由后续 owning Phase 按真实 Gate 更新 |

结论：全面 live audit 与历史输入审计的覆盖范围、证据和差距清单为 `PASS`。所有旧 V2 PASS/FAIL/NOT RUN、组件裁决、Phase 编号、README 声明、运行容器和镜像都不继承规范地位；当前实现的 V3 conformance `FAIL` 已进入 architecture、migration ledger、risk register 或 Phase 1+ 输入，本结论不追认任何后续 Gate。

## 4. 当前基线验证

| 检查 | 状态 | 结果 |
|---|---|---|
| `go test -mod=readonly -count=1 ./...`（server-web） | PASS | 当前所有 package 通过；真实 MySQL integration 因无测试 DSN 未执行 |
| `go test -mod=readonly -count=1 ./...`（pkg） | PASS | 当前所有 package 通过 |
| Frontend ESLint | PASS | `npm run lint` 通过 |
| Frontend TypeScript | PASS | `npx vue-tsc --noEmit` 通过 |
| Frontend unit | PASS | 5 files / 18 tests 通过 |
| Frontend production build | NOT RUN | 避免覆盖用户现有 ignored `dist/`；Phase 0 仅改文档 |
| Script syntax | PASS | `bash -n scripts/run-v2-demo.sh docker/setup-k8s.sh` |
| Go vet/race/build | NOT RUN | Phase 0 不改 Go；当前 CI 配置仍有独立 path FAIL |
| Real MySQL / provider / Golden | NOT RUN | 属于后续 Gate，且不得复用旧 SHA 证据 |

## 5. V3 内部冲突审查与直接修正

所有修正都发生在唯一规范与 Phase 0 文档中，没有修改业务实现。修正原则是消除歧义、补齐可实现合同和收紧既有 Gate，不新增产品、组件、操作类型或部署路径。

| 审查项 | 状态 | 发现的问题 | 修正与证据 |
|---|---|---|---|
| Incident close / resolved Signal | PASS | 原文未完整区分未消费 Approval、已创建 ChangeRequest 和 external write unknown，可能隐藏在途副作用 | [V3 设计](../CloudOps-Incident-Agent-V3-Refactor-Design.md) §8.2/8.4 冻结可取消范围、write intent 边界和只读 reconcile |
| AgentRun 创建所有权 | PASS | reopen、人工重试、Verification failure 和总览曾可读成 Handler/Ingress 直接建 Run | §8.2、§10.2、§19.4、Phase 6/Golden 统一为一个 Incident-scoped `investigation.start` mode Task；只有该 Task 创建 AgentRun |
| Task 类型与幂等 | PASS | `ensure_branch` 曾像第六种 Task type；duplicate Webhook 曾把 Task 与 Run 创建混写 | `ensure_branch` 固定为 `change.ensure_pr(write_phase=ensure_branch)`；Webhook 只幂等建 Incident/start Task，start claim 再受 active Run key 保护 |
| Plan / Approval / CI identity | PASS | `approved plan hash` 无字段/算法，且“批准 PR head”要求绑定审批时尚不存在的 SHA | §17.3 定义版本化 `canonical_plan_hash` 覆盖完整不可变 Plan 和精确 diff；Approval 绑定 base/post-image/tree 等 hash，当前 PR head 只在 tree/hash 一致后用于 CI 证据；[ADR 0007](../adr/0007-restore-env-hash-approval.md) 同步 |
| Session / OAuth / CSRF | PASS | oauth2-proxy 与 API ownership、OAuth state 是否每个 mutation 携带不明确 | §20.1 冻结 proxy session/OAuth state，API identity-bound 短期 CSRF token 只存前端内存；mutation 不重复携带 OAuth state |
| API Query / SSE / role | PASS | Signal、ResolutionReport 与 `/events` 的 Query/stream 边界及 viewer 可见性不完整 | §21.1 分离 Signal/ResolutionReport Query 与 SSE `/events`，补 `/session/csrf` 和 viewer/operator 矩阵 |
| Filebeat / OTel RBAC | PASS | k8sattributes 与日志 metadata 所需权限未冻结 | §20.3 将 Filebeat、OTel Collector 限于 CloudOps/Demo namespace 的最小 get/list/watch，无 Secret/Node/write |
| Phase 2 / 4 / 6 Gate | PASS | unique claim code Gate 曾可能被读成 live cutover；Phase 4 fixture/live Change 边界、前端迁移 owner 和 AGENT_QUALITY 顺序不清 | Phase 2 仅代码/测试唯一 claim；live conversion 在 7A；Phase 4 冻结八个工具且真实模型不通过即阻止 Phase 5+；真实 GitHub/Argo Change 属于 Phase 5；前端只在 Phase 6 一次迁到 root |
| Outbox / cutover / contract | PASS | 原设计把 outbox 当 ready/running queue，且首次 cutover 与删除、Release A ledger PASS 范围混写 | §28.4 与 [migration ledger](../migration-ledger.md) 将 outbox 归档、Task 仅由兼容 child converter + anti-join 产生；Phase 7A cutover/Golden 保持 `CONTRACT-V3=NOT RUN`，Phase 7B 独立删除和 post-check |
| Phase 0 文档边界 | PASS | Phase 0 ADR 数量和后续文档 owner 不明确，容易用空文档冒充实施 | §26/§29 冻结 12 个 target-decision ADR；agent-runtime/security/API/demo/exact-SHA manifest 由 owning Phase 产生，ADR 实施证据保持 NOT RUN |
| 第三方精确版本兼容矩阵 | NOT RUN | Phase 0 没有干净安装 ECK、Tempo、Argo、Prometheus Operator | 组件类别与权威边界无冲突；精确版本/Chart/image digest 只能由 Phase 3 真实安装和数据路径 Gate 冻结，不编造兼容结论 |

复核结论：产品范围、外部权威边界、组件类别、唯一 Incident/GitOps 路径、non-goal 和 Phase Gate 在修正后无已知内部冲突，状态为 `PASS`。第三方版本矩阵的 `NOT RUN` 是已分配的后续实证，不是文档矛盾。

## 6. 交付物检查

| 交付物 | 状态 | 证据与结论 |
|---|---|---|
| V3 唯一规范 | PASS | [CloudOps-Incident-Agent-V3-Refactor-Design.md](../CloudOps-Incident-Agent-V3-Refactor-Design.md) 已修正并冻结 target design |
| Architecture + KEEP/ADAPT/DELETE | PASS | [architecture.md](../architecture.md) 精确到源码目录、进程、服务、表、状态机、API、前端、部署和 CI；复合标签首词是主决策，后半段是迁移/生命周期限定 |
| Migration ledger | PASS | [migration-ledger.md](../migration-ledger.md) 记录 00001-00006 内容地址、15 张 Goose 表、10 张 AutoMigrate 表、三套旧 lease、outbox/archive、expand→backfill→quiesce→cutover→contract |
| Risk register | PASS | [risk-register.md](../risk-register.md) 覆盖架构、数据、外部副作用、GitHub/Argo、可观测性、资源、凭据、CI/UI 和 Golden E2E；每个高风险有 treatment/owner 与 exit 或 accepted boundary |
| ADR index | PASS | [docs/adr/README.md](../adr/README.md) 明确设计优先级和 target-decision 状态语义 |
| Numbered ADRs | PASS | `docs/adr/0001` 至 `0012` 连续、无缺号/重复，共 12 篇，覆盖 V3 设计 §29 最低清单 |
| ADR implementation / live evidence | NOT RUN | 每篇 ADR 都明确 implementation/integration/data/live evidence 尚未执行；不得把 `Accepted target decision` 解释为后续 Gate PASS |
| README authority | PASS | [README.md](../../README.md) 首屏声明 V3 唯一规范，旧 V2 内容只作迁移输入；失效 `doc/**` 链接和项目树已修正 |
| Phase 0 report | PASS | 本报告包含基线、全量审计、历史输入、PASS/FAIL/NOT RUN、问题、Phase 1 输入、最终 staging 和 Gate verdict |

## 7. 后续 Phase 1 输入

| 当前问题 | 当前状态 | Phase 1 必须完成 | 证据路径 |
|---|---|---|---|
| 无 root Go module | FAIL | 在仓库根建立唯一 `go.mod/go.sum`，保持包行为不变 | [architecture.md](../architecture.md) §2.1/§5；当前两个 nested `go.mod` |
| nested `server-monitor/pkg` + replace | FAIL | 把共享包吸收到 root module，删除 nested module/replace，所有 import/build/test 从 root 可解析 | `server-monitor/server-web/go.mod`、`server-monitor/pkg/go.mod` |
| `server-web` 混合 API 与 Worker | FAIL | 建 `cmd/cloudops-api`、`cmd/cloudops-worker`、`cmd/cloudops-migrate`；API assembly 不启动任何后台 loop、不持有 K8s token | `server-monitor/server-web/app.go:44-112,144-153` |
| Runtime AutoMigrate | FAIL | 从 `00007+` forward migration 显式接管仍需兼容的 legacy schema；fresh/existing MySQL schema parity 通过后移除 runtime AutoMigrate | `server-monitor/server-web/internal/startup/infra.go:76-95`、[migration-ledger.md](../migration-ledger.md) §3.2/§4 |
| Process bootstrap contract | FAIL | typed config、process-specific livez/readyz、graceful shutdown 和 migrate advisory lock；不得启用 V3 业务路径 | V3 设计 §7.2/§23.1；[ADR 0002](../adr/0002-root-module-processes.md) |
| CI path/trigger 已过期 | FAIL | CI 覆盖 root module/current package；移除不存在的 `internal/copilot/*/eval` 调用，但不提前加入后续 Phase 的假 Gate | `.github/workflows/ci.yaml:3-7,100-116` |
| 前端最终位置 | PASS（边界已冻结） | Phase 1 保持 `server-monitor/frontend/`；Phase 6 在 Workbench 适配时一次迁到 root，禁止平行 UI | [architecture.md](../architecture.md) §5；V3 设计 Phase 6 |
| 外部环境与 legacy data | NOT RUN | Phase 1 不清容器/集群，不做 state/outbox/lease conversion，不删 legacy data/deploy assets | [migration-ledger.md](../migration-ledger.md) §4/§8/§9 |

Phase 1 明确禁止：压缩 Incident 状态、启用 `async_tasks` live cutover、转换 outbox/lease、改变 `/api/v2` 行为或启用 `/api/v3` 业务、移动/重做前端、安装 Operator、启用 GitHub write、修改现有容器/集群、删除 legacy schema/data/deployment。Phase 1 完成后必须独立验证、报告并停止。

## 8. 最终暂存与不可变证据

| 控制 | 状态 | 最终证据 |
|---|---|---|
| Branch / HEAD unchanged | PASS | `main@2f7e426d69a4ed7d8d32ec3ca83c13af0c71586e`，upstream ahead 0 / behind 0；无 commit/push |
| Initial design identity preserved | PASS | 审计起点 blob `c8a5ce011d54e730541289be5481f52e43cec8d4`、SHA-256 `c04d91aa...78924`、135936 bytes / 3188 lines 保留在 §1 |
| Frozen staged design blob | PASS | index 与 worktree 均为 `82aff89ab7b0bf2b13db35465229a78c821c46ac` |
| Frozen design SHA-256 / size | PASS | `0c930f8065761e4730cb183dbfe6fa89d7f9a435155c7c10c4bca1d61df6cdf0`；143123 bytes / 3224 lines |
| Staged path set | PASS | 19 个路径；仅 `README.md` 与 `docs/**`，包含设计、4 个 Phase 0 主文档、ADR index + 12 ADR |
| Unstaged tracked changes | PASS | `git diff --name-only` 为空 |
| Untracked files | PASS | `git ls-files --others --exclude-standard` 为空 |
| Out-of-scope source/deploy/runtime diff | PASS | `git diff HEAD --name-only` 不含 Go、SQL、frontend、workflow、Chart、manifest、script 或配置文件 |
| Markdown relative links | PASS | README 与 `docs/**/*.md` 的全部相对文件/anchor 目标存在 |
| ADR sequence/status | PASS | 0001-0012 连续；每篇都显式说明实施或 live evidence `NOT RUN` |
| README `doc/**` links | PASS | 0 个；历史 Git object 仅在本报告代码文本中引用，不恢复路径 |
| Whitespace / index consistency | PASS | `git diff --check`、`git diff --cached --check` 均无输出；design index/worktree blob 相同 |
| Business behavior | PASS | 变更只涉及 Markdown；业务逻辑、数据库行为、API 行为、运行时架构、CI 行为和部署结果均未改变 |

最终复核命令：

```text
git status --short --untracked-files=all
git diff --name-only
git diff --cached --name-only
git diff HEAD --name-only
git rev-parse :docs/CloudOps-Incident-Agent-V3-Refactor-Design.md
git hash-object docs/CloudOps-Incident-Agent-V3-Refactor-Design.md
sha256sum docs/CloudOps-Incident-Agent-V3-Refactor-Design.md
git diff --check
git diff --cached --check
```

## 9. Phase 0 Gate

| Gate control | 状态 | 判定 |
|---|---|---|
| 当前 branch / HEAD / initial worktree / design blob 可追溯 | PASS | §1 已冻结，final staged identity 另列于 §8 |
| 源码、modules、进程、migrations、部署、CI、前端、运行态和历史 V2 文档全面审计 | PASS | §3，历史 Git `doc/` 6/6 + 两份 workspace V2 输入均完成 |
| 可执行 KEEP / ADAPT / DELETE | PASS | architecture 精确到模块、目录、服务、表、状态机和部署资产 |
| Migration ledger | PASS | 00001-00006 不变、legacy state/lease/outbox、forward path 和 contract boundary 均冻结 |
| Risk register | PASS | 请求的八类风险及 Golden provenance 均覆盖，后续 owner/Gate 明确 |
| 选型、边界、non-goal、Phase Gate 冲突检查 | PASS | 已发现的问题均在 §5 直接修正，无范围扩大 |
| 旧 V2 规范地位撤销 | PASS | V3 是唯一规范；V2 仅 migration input，旧证据不继承 |
| Phase 0 ADR / report / staging | PASS | 交付物齐全，19 个路径全部 staged，无 unstaged/untracked |
| 业务、数据库、API、运行时、CI、部署行为不变 | PASS | Markdown-only diff；未操作外部运行态 |
| Real MySQL row/schema/backfill/cutover | NOT RUN | Phase 0 不授权；属于 Phase 1/2/7A owning Gate，不影响本阶段文档 Gate |
| GitHub/Argo/OAuth/LLM/provider/Golden E2E | NOT RUN | 无凭据且实现未就绪；属于 Phase 3-7/AGENT_QUALITY Gate，不得复用旧证据 |
| ECK/Tempo/Argo/Prometheus Operator exact compatibility/resource peak | NOT RUN | Phase 3 clean kind install/data-path/resource Gate |
| Phase 1 implementation | NOT RUN | 本轮只冻结输入，未开始 Phase 1 |
| **Phase 0 Gate** | **PASS** | 所有 Phase 0 必需控制为 PASS；后续实证 NOT RUN 均已分配且未被伪装成成功 |

```text
PHASE_0_STATUS=PASS
V3_BASELINE_FROZEN=YES
BUSINESS_BEHAVIOR_CHANGED=NO
READY_FOR_PHASE_1=YES
```
