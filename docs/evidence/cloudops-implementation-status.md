# CloudOps 实施状态

> 产品契约：V1
>
> 当前分支：`codex/v3-refactor`
>
> Task 0 开工基线：`10a1f2b659b4ee9adb1a3efcb7725f83504b9d1f`
>
> 最后更新：2026-07-26 09:20（Asia/Shanghai）

## 状态总览

| 任务 | 状态 | 硬依赖判断 | 当前结论 |
|---|---|---|---|
| 任务 0：语义基线与本地生命周期 | `DONE_WITH_NOT_RUN` | 无 | 本地 UI -> `/api/v1` -> MySQL、数据迁移及生命周期完成；外部 Provider 明确 `NOT RUN` |
| 任务 1：平台 Shell 与 Settings | `READY` | 任务 0 API、数据和生命周期契约 | 唯一 `/api/v1`、schema 1、Make/Helm/MySQL 契约已稳定 |
| 任务 2：Infrastructure 与 Atlas | `BLOCKED` | 任务 0；任务 1 Scope/Shell | 等待任务 1 Operational Scope 与 Shell contract |
| 任务 3：Monitoring | `BLOCKED` | 任务 0；任务 1 Provider/Scope/Query | 等待任务 1 Provider、Scope 与 Query contract |
| 任务 4：Logs 与 Traces | `BLOCKED` | 任务 1；任务 2/3 context contract | 等待任务 1-3 的 resource/time contract |
| 任务 5：Alerts | `BLOCKED` | 任务 0；任务 1 notification/Settings/Context Link | 等待任务 1 公共契约 |
| 任务 6：Agent | `BLOCKED` | 任务 1；任务 2-5 真实 Evidence source | 前置 Evidence Plane 尚未完成 |
| 任务 7：Incidents 与 Verify | `BLOCKED` | 任务 5、6；任务 2-4 Context Link | 前置领域尚未完成 |
| 任务 8：Operations 与 DevOps | `BLOCKED` | 任务 6；Incident 链另需任务 7 | authority contract 尚未完成 |
| 任务 9：Scenario 与最终收敛 | `BLOCKED` | 任务 0-8 本地必需能力 | 前置任务尚未完成 |

## 任务 0：语义基线与本地生命周期

### 实施结果

- 唯一公开运行契约为 `/api/v1`；内部 API、router、store、domain、config、Chart 与 Kubernetes identity 使用语义命名。
- 登录、session、Bearer、CSRF、应用 RBAC、generation guard、numbered runtime profile 和兼容 route 已删除。
- 18 个顺序 migration 收敛为 `migrations/00001_cloudops_baseline.sql`；本地数据由一次性转换进入 schema version 1。
- 顶层 `make local-*`、单一 `charts/cloudops`、`kind-cloudops-local`、release `cloudops` 是唯一公开本地路径；旧 Compose、raw manifests、平行 Chart 与 `server-monitor` runtime 已删除。
- Agent Quality 数据集收敛为 `eval/sha256-eee09f074b37277b3f64dcdff22731647926b0daadc53f86b7d1e1c2988c0f0e`，`eval/index.json` 只保存当前内容地址；CLI 在运行前核对文件 SHA-256。
- `make local-backup`、隔离 restore、restore 自动 rollback、restart、down/up persistence、status 与 doctor 均已实际验证。

### Runtime 与数据审计

| 项目 | 结果 | 当前证据 |
|---|---|---|
| Build/runtime | `PASS` | `kind-cloudops-local` / `cloudops-system` / Helm `cloudops` revision 9；API、Worker、MySQL Ready，migration Job completed |
| Schema | `PASS` | schema version `1`；31 tables；identity `sha256:56cbc891ea6a959184c01ea9a66a5bc917402ff9a26f90f54f5c431bd3e0a315` |
| Data retention | `PASS` | 当前 MySQL：8 Incident、19 Agent run、11 Evidence；down/up 后 counts 仍为 `8 / 19 / 11` |
| Transformation | `PASS` | import audit `37ec2e04-8882-11f1-986d-56daa46a94da` 状态 `completed` |
| Evidence contract | `PASS` | 11/11 retained Evidence；public ID、hash、provenance 违规均为 0 |
| Backup/restore | `PASS` | 5 个 format 2 `cloudops-semantic` checksummed backup；隔离 restore、自动 rollback 与恢复后审计均通过 |
| Doctor | `PASS` | Docker/kind/kubectl/Helm/MySQL/schema/loopback/private secret dirs/latest backup 全部通过 |
| Agent eval | `PASS`（确定性部分） | 29-case content-addressed manifest validate；13-case fixed baseline 完成；10/10 guardrail PASS；真实模型质量分支 `NOT RUN` |

保留的 backup：

- `.cloudops/backups/20260725T195748Z-10a1f2b659b4`
- `.cloudops/backups/20260726T003808Z-10a1f2b659b4`
- `.cloudops/backups/20260726T004124Z-10a1f2b659b4`
- `.cloudops/backups/20260726T004311Z-10a1f2b659b4`
- `.cloudops/backups/20260726T004354Z-10a1f2b659b4`

### MCP 联调证据

验收 URL：`http://127.0.0.1:18081/incidents`；Chrome DevTools MCP isolated context `cloudops-task0`；无 cookie、localStorage 或 sessionStorage。

| 维度 | 结果 | 证据 |
|---|---|---|
| Browser | `PASS` | 无登录直达 8 条 Incident；打开 `21c123ac-7199-4dff-a64b-7384f3550ea3`、run `c42b28a7-a150-4e71-9a08-a3f4b69f18b6` 和 Evidence `70184dbb-5cfc-5dfd-96b0-3d203a862cf4`；页面显示 3 runs、2 Evidence |
| Network | `PASS` | 33 个业务 fetch 全为 `/api/v1` 且 200；list trace `dk83sgyefc2j-6b`；Evidence trace `dk83sx71mmvf-6n` |
| Data | `PASS` | UI Evidence public ID/hash 与 MySQL projection 一致；当前全库 counts `8 / 19 / 11` |
| Provider | `PASS`（MySQL） | API 使用 release 内真实 MySQL；retained Incident/Evidence 来自当前 schema 1，不使用 fixture |
| Provider unavailable | `PASS`（呈现） | Worker Ready 且 `PROVIDER_GATEWAY_ENABLED=false`；UI 明确显示 `No live cluster read` / `Not projected`，未伪造 Kubernetes/observability facts |
| Console | `PASS` | 完整 list/detail/Evidence 流程无 console error、warning 或 Vue warning |
| Task result | `DONE_WITH_NOT_RUN` | Task 0 本地核心能力完成；下面列出的外部 Provider 分支未运行 |

Chrome 首次连接 `18081` 时，命令执行器已经回收 `local-up` 的后台子进程，得到 `ERR_CONNECTION_REFUSED`。改用同一 context/namespace/service 的前台 `kubectl port-forward` 后完成上述验收；受管 PID 对齐后 `make local-doctor` PASS。API/Helm readiness 未发生失败。

### 检查结果

`PASS`：

- `go test -count=1 ./...`
- `go test -race -count=1 ./...`
- `go vet ./...`
- `golangci-lint run --max-same-issues=0 ./...`
- gofmt、goimports、module、structure 与 `git diff --check`
- frontend lint、typecheck、47 unit tests 与 production build
- `actionlint`、immutable workflow dependency audit、ShellCheck
- Helm strict lint/template/runtime contracts；kubeconform 12/12
- Docker targets `cloudops-api`、`cloudops-worker`、`cloudops-migrate`、`cloudops-demo`
- `make check-naming`
- Agent eval validate、fixed baseline、guardrail

### 实际文件清单

- Lifecycle/runtime：`.gitignore`、`Makefile`、`Dockerfile`、`scripts/local-lifecycle.sh`、`scripts/check-runtime-render.sh`、`charts/cloudops/**`。
- V1 contract：`docs/api-v1-openapi.yaml`、`internal/api/**`、`internal/router/api.go`、`internal/router/router.go`、`frontend/src/api/**`、`frontend/src/router/**`。
- Semantic domain/data：`migrations/00001_cloudops_baseline.sql`、`migrations/baseline_test.go`、`internal/incident/**`、`internal/infra/incidentstore/**`、相关 bootstrap/taskhandler/verification repositories 与 integration tests。
- Local Owner frontend：删除 `frontend/src/api/auth.ts`、`frontend/src/stores/auth.ts`；更新 layout、Incident views/components/composables/models/types 和 E2E fixture contract。
- Agent Quality：`eval/index.json`、当前 `eval/sha256-eee09f074b37277b3f64dcdff22731647926b0daadc53f86b7d1e1c2988c0f0e/**`、`cmd/cloudops-agent-eval/**`、`internal/agent/eval/**`。
- Naming/CI：`scripts/check-first-party-naming.sh`、`.github/workflows/ci.yaml`、`.github/workflows/golden-image-publish.yaml`、`.github/workflows/hosted-supply-chain-validation.yaml`、`scripts/check-immutable-workflow-dependencies.py`。
- Active documentation：`README.md`、`docs/{architecture,api,security,agent-runtime,reliability,migration-ledger,demo,risk-register}.md`、`docs/evidence/golden-e2e-harness.md`。
- Removed parallel implementation：`server-monitor/**`、旧 `migrations/00001` 至 `00018`、`internal/apiv3/**`、generation/phase compatibility packages、旧 Compose/raw manifests/parallel Charts。

完整逐文件清单以 Task 0 提交的 `git diff-tree --name-status` 为准；提交与 push SHA 在本任务提交完成后补记。

### NOT RUN

- Kubernetes、Prometheus、Elasticsearch、Tempo Provider Gateway：本地 Task 0 默认 `false`，未做 live query；属于后续 READY 任务的纵向范围。
- GitHub、Argo CD 外部读取/写入及 hosted publish/sign/attest：无当前任务运行授权或凭据，`NOT RUN`。
- 真实 LLM Agent Quality：未配置真实模型凭据，`NOT RUN`；确定性 fixture/guardrail 不替代模型调用。
- PR、tag、默认分支、force push、production/staging：`NOT RUN`。
