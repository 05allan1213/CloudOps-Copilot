# CloudOps 实施状态

> 产品契约：V1
>
> 当前分支：`codex/v3-refactor`
>
> Task 0 开工基线：`10a1f2b659b4ee9adb1a3efcb7725f83504b9d1f`
>
> 最后更新：2026-07-26 11:47（Asia/Shanghai）

## 状态总览

| 任务 | 状态 | 硬依赖判断 | 当前结论 |
|---|---|---|---|
| 任务 0：语义基线与本地生命周期 | `DONE_WITH_NOT_RUN` | 无 | 本地 UI -> `/api/v1` -> MySQL、数据迁移及生命周期完成；外部 Provider 明确 `NOT RUN` |
| 任务 1：平台 Shell 与 Settings | `DONE_WITH_NOT_RUN` | 任务 0 API、数据和生命周期契约 | Shell、Settings、revision、secret、notification 与 Worker activation 已完成真实联调；外部 Provider 调用明确 `NOT RUN` |
| 任务 2：Infrastructure 与 Atlas | `READY` | 任务 0；任务 1 Scope/Shell | Operational Scope、Shell、Context Link 与 Provider health contract 已落地 |
| 任务 3：Monitoring | `READY` | 任务 0；任务 1 Provider/Scope/Query | Provider、Scope、Query policy 与公共时间范围已落地 |
| 任务 4：Logs 与 Traces | `BLOCKED` | 任务 1；任务 2/3 context contract | 等待任务 1-3 的 resource/time contract |
| 任务 5：Alerts | `READY` | 任务 0；任务 1 notification/Settings/Context Link | notification、Settings 与 Context Link 公共契约已落地 |
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

完整逐文件清单可由 `git diff-tree --name-status -r 602a67748b932922e5859379def040013bf489b1` 重放。

### Delivery record

| 项目 | 结果 | 证据 |
|---|---|---|
| Remote | `PASS` | `origin` = `https://github.com/05allan1213/CloudOps-Copilot.git` |
| Branch | `PASS` | `codex/v3-refactor`；默认分支为 `origin/main` |
| Implementation commit | `PASS` | local = remote = `602a67748b932922e5859379def040013bf489b1` |
| Push policy | `PASS` | normal fast-forward `10a1f2b..602a677`；未 force push、未推 tag、未触碰默认分支 |
| Commit scope | `PASS` | Task 0 精确差异 545 files；提交前无 unstaged/untracked 文件，cached diff check PASS |

### NOT RUN

- Kubernetes、Prometheus、Elasticsearch、Tempo Provider Gateway：本地 Task 0 默认 `false`，未做 live query；属于后续 READY 任务的纵向范围。
- GitHub、Argo CD 外部读取/写入及 hosted publish/sign/attest：无当前任务运行授权或凭据，`NOT RUN`。
- 真实 LLM Agent Quality：未配置真实模型凭据，`NOT RUN`；确定性 fixture/guardrail 不替代模型调用。
- PR、tag、默认分支、force push、production/staging：`NOT RUN`。

## 任务 1：平台 Shell 与 Settings

### 实施结果

- 十个 Workspace 收敛到同一 responsive Shell；桌面侧栏、移动端 bottom navigation、More 导航、document scrolling、浏览器历史和 Context Link 使用同一语义路由。
- Overview、Settings、Notification Inbox 与 SSE 已接入唯一 `/api/v1`；无并行版本 API、generation-labelled route 或 numbered runtime profile。
- Settings 实现 draft validate、不可变 Configuration Revision、activate、history、restore、Provider health/test、write-only secret、Operational Scope 与 Query policy。
- validate 结果绑定 canonical draft hash；修改已验证 draft 后 apply 返回 `409 STALE_VALIDATION`，不会创建或激活 revision。
- Worker 通过数据库任务边界观察新 revision；活动 revision、activation task、async task 均保存 revision identity/hash，避免 API 内联伪造激活成功。
- `migrations/00002_platform_foundation.sql` 将 schema 提升到 version 2；configuration、scope、provider health、secret metadata、notification 与 data lifecycle 均持久化到真实 MySQL/PVC。
- `make local-up` 构建并加载固定 local image 后会滚动 API/Worker，确保 Pod 使用当前 bundle；Helm release `cloudops` 当前 revision 12。

### Runtime 与数据审计

| 项目 | 结果 | 当前证据 |
|---|---|---|
| Build/runtime | `PASS` | `make local-up` 完成；API、Worker、MySQL 均 Ready；API/Worker 当前 Pod 均为最新 rollout 且 0 restart |
| Schema | `PASS` | `goose_db_version=2`；`migrations/00002_platform_foundation.sql` 已应用 |
| Configuration | `PASS` | active revision `2`，public ID `ce892417-88b8-4053-8376-de78fb818b42`，hash `ce569e83cc3aaf92a6f802e567936f18a8e4927b8e3918419a803809057c998c` |
| Worker boundary | `PASS` | activation `succeeded`，attempt `1`，observed hash 与 revision hash 相等 |
| Stale protection | `PASS` | validation `b7752cda-67c7-4852-998d-e52ec4ca85ec` 的 `applied_revision_id=NULL`；active revision 仍为 `2` |
| Secret storage | `PASS` | secret metadata count `1`；API response 不含 value；private directory `0700`、secret file `0600` |
| Data lifecycle | `PASS` | PVC `cloudops-data` Bound、1Gi；MySQL PVC `data-mysql-0` Bound、2Gi |

活动 Revision 2 的实际值：summary `Task 1 live UI validation`，LLM model `deepseek-reasoner`；activation worker identity `cloudops-worker-d5dffc6d7-5p9ll`。该 identity 是完成 activation 时的真实 Worker，后续 `make local-up` rollout 不改写历史任务归属。

### MCP 联调证据

验收 URL：`http://127.0.0.1:18082/settings`；Chrome DevTools MCP isolated context `cloudops-task1-final`。

| 维度 | 结果 | 证据 |
|---|---|---|
| Browser | `PASS` | 1440px、390px、320px 实测；十个 Workspace 均可达；320px `scrollWidth=320`，五个 bottom-nav 控件均在 viewport 内，More 可达其余六个 Workspace；Agent <-> Overview 浏览器 Back/Forward 正常；Settings 的 scrolling element 为 `document.documentElement` |
| Network | `PASS` | UI validate `200`、revision create `201`、stale apply `409`、secret create `201`、SSE `/api/v1/notification-events` `200`；当前业务请求全部为 `/api/v1` |
| Data | `PASS` | UI 显示 Configuration Revision #2、model `deepseek-reasoner` 和 activation succeeded；与上面的 MySQL active revision/hash/task projection 一致 |
| Provider | `PASS` | MySQL 与 Worker 为真实 release Provider；`POST /api/v1/providers/llm/tests` 返回 `200 disabled`，request/trace `dk86sd7130ir-95`，detail `Provider 在当前配置中未启用` |
| Failure/unavailable | `PASS` | 修改已验证 draft 后得到 `409 STALE_VALIDATION` 且无 revision 写入；浏览器通知拒绝显示 `BROWSER_NOTIFICATION_PERMISSION_DENIED`；disabled LLM 未被显示为 available |
| Console | `PASS` | 干净 isolated context 的最终页面无 console message、Vue warning 或未处理异常 |
| Task result | `DONE_WITH_NOT_RUN` | Task 1 本地核心纵向能力完成；未配置的外部 Provider 与 hosted 环境分支如下列为 `NOT RUN` |

### 检查结果

`PASS`：

- `npm exec -- vue-tsc --noEmit`
- `npm run lint -- --quiet`
- `npm test`：12 files / 50 tests
- `npm run build`：initial JS gzip `137.59 KiB`
- `go test ./internal/api ./internal/notification ./internal/settings`
- `bash scripts/check-runtime-render.sh`
- `bash -n scripts/local-lifecycle.sh`
- `git diff --check`
- `/livez`、`/readyz`、`/api/v1/bootstrap`、`/api/v1/settings`

### 实际文件清单

- Shell 与 navigation：`frontend/index.html`、`frontend/package*.json`、`frontend/src/components/layout/**`、`frontend/src/navigation*`、`frontend/src/router/**`、`frontend/src/pages/NotFoundPage.vue`、`frontend/src/style.css`、`frontend/src/types/index.ts`。
- Platform UI：`frontend/src/api/{client,notifications,platform}.ts`、`frontend/src/utils/contextLink*`、`frontend/src/views/{overview,settings,workspaces}/**`。
- 现有 Incident UI contract 对齐：`frontend/src/views/incidents/**` 与 `frontend/src/components/incidents/**`。
- V1/backend：`docs/api-v1-openapi.yaml`、`internal/api/{handler,types,platform_handler,openapi_contract_test}.go`、`internal/router/{api,dependencies}.go`、`internal/notification/**`、`internal/settings/**`。
- Revision propagation/Worker：`internal/{asyncjob,bootstrap,config,di,startup}/**` 相关文件、`internal/taskhandler/investigation_start.go`、`internal/command/integration_test.go`、`internal/infra/incidentstore/no_change.go`。
- Data contract：`migrations/00002_platform_foundation.sql`、`migrations/baseline_test.go`、`internal/schemaversion/version.go`、`internal/migration/evidence_supersession_test.go`。
- Helm/Make lifecycle：`charts/cloudops/templates/{_helpers,api,worker,data}.yaml`、`charts/cloudops/{values,values.schema}.yaml`、`scripts/{check-runtime-render,local-lifecycle}.sh`。
- 当前 Task 1 范围共 86 个工作树文件：85 个 implementation 文件加本状态文件；implementation 精确逐文件清单可由 `git diff-tree --name-status -r 47145aef04e64f9cef1d70d8d168c7e88ce2bc42` 重放。

### Delivery record

| 项目 | 结果 | 证据 |
|---|---|---|
| Remote | `PASS` | `origin` = `https://github.com/05allan1213/CloudOps-Copilot.git` |
| Branch | `PASS` | `codex/v3-refactor`；非默认实施分支 |
| Implementation commit | `PASS` | 85 个精确 implementation 文件；local = remote = `47145aef04e64f9cef1d70d8d168c7e88ce2bc42` |
| Push | `PASS` | normal fast-forward `f5a459f..47145ae`；未 force push、未推 tag、未触碰默认分支 |

### NOT RUN

- 真实外部 LLM 调用：当前 LLM Provider 为 disabled，未配置或使用模型 secret；disabled path 已真实联调，但不替代 LLM Provider 成功调用。
- Kubernetes、Prometheus、Alertmanager、Elasticsearch、Tempo、GitHub、Argo CD Provider 的成功查询：不属于 Task 1；按后续 READY 任务分别实施和验收。
- 浏览器系统通知成功投递：当前环境权限为 denied；产品错误码路径已验证，系统级成功通知 `NOT RUN`。
- hosted/staging/production、PR、tag、默认分支、force push：`NOT RUN`。
