# CloudOps 实施状态

> 产品契约：V1
>
> 当前分支：`codex/v3-refactor`
>
> Task 0 开工基线：`10a1f2b659b4ee9adb1a3efcb7725f83504b9d1f`
>
> 最后更新：2026-07-27 17:07（Asia/Shanghai）

## 状态总览

| 任务 | 状态 | 硬依赖判断 | 当前结论 |
|---|---|---|---|
| 任务 0：语义基线与本地生命周期 | `DONE_WITH_NOT_RUN` | 无 | 本地 UI -> `/api/v1` -> MySQL、数据迁移及生命周期完成；外部 Provider 明确 `NOT RUN` |
| 任务 1：平台 Shell 与 Settings | `DONE_WITH_NOT_RUN` | 任务 0 API、数据和生命周期契约 | Shell、Settings、revision、secret、notification 与 Worker activation 已完成真实联调；外部 Provider 调用明确 `NOT RUN` |
| 任务 2：Infrastructure 与 Atlas | `DONE_WITH_NOT_RUN` | 任务 0；任务 1 Scope/Shell | 真实 Kubernetes typed reader、Infrastructure、Operations Atlas、多 Scope contract 与 Context Link 已完成；第二真实集群及 MCP 维度明确 `NOT RUN` |
| 任务 3：Monitoring | `DONE_WITH_NOT_RUN` | 任务 0；任务 1 Provider/Scope/Query | 真实 UI -> `/api/v1` -> Prometheus、bounded query、Definition/Execution/Authorization、审计、Workspace 与精确 Context Link 已完成；外部环境与真实 Agent runtime 明确 `NOT RUN` |
| 任务 4：Logs 与 Traces | `DONE_WITH_NOT_RUN` | 任务 1；任务 2/3 context contract | 真实 UI -> `/api/v1` -> Elasticsearch/Tempo、Kubernetes correlation、Evidence 与不可变 Context Snapshot 已完成；缺少的专用 MCP 与外部控制台明确 `NOT RUN` |
| 任务 5：Alerts | `DONE_WITH_NOT_RUN` | 任务 0；任务 1 notification/Settings/Context Link | 本地真实 Alertmanager firing/resolved、Signal-to-Alert、ack、provider-backed silence、显式 Incident、Owner Notification、Workspace 与幂等重投已完成；专用 Alertmanager MCP 明确 `NOT RUN` |
| 任务 6：Agent | `DONE_WITH_NOT_RUN` | 任务 1；任务 2-5 真实 Evidence source | 真实 Alert Investigation、Logs Consultation、bounded tools、Evidence/Guidance、snapshot、SSE、Knowledge、Agent UI 与三层 authority 已完成；真实 LLM 诊断明确 `NOT RUN` |
| 任务 7：Incidents 与 Verify | `READY` | 任务 5、6；任务 2-4 Context Link | 任务 5/6 与 Context Link contract 均已完成，可开始 Incident/Verify 收敛 |
| 任务 8：Operations 与 DevOps | `READY` | 任务 6；Incident 链另需任务 7 | Task 6 Operation Plan/authority contract 已完成；local reversible groundwork 可开始，Incident-bound execution/verify 仍等待任务 7 |
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

## 任务 2：Infrastructure 与 Operations Atlas

### 实施结果

- Worker 持有只读 Kubernetes typed client；API 不挂 ServiceAccount token，而是调用固定的内部 probe/topology/event gateway。资源投影覆盖 Namespace、Service、Deployment、StatefulSet、DaemonSet、Pod、Node、Ingress 与 EndpointSlice 关系事实，不返回原始 YAML。
- `GET /api/v1/topology`、`GET /api/v1/topology/events`、`GET /api/v1/resources`、资源详情及 Event routes 已接入唯一 V1；Overview 与 Infrastructure 消费同一个 bounded topology projection。
- Overview 使用按路由加载的 Three.js Operations Atlas、stable layout、Raycaster 与 OrbitControls；没有持续动画或装饰节点。结构化视图与 Canvas 使用同一 node identity，并支持显式和 WebGL 自动 fallback。
- Infrastructure Workspace 提供 Namespace/Kind/search、typed detail、owner/selector/endpoint/scheduling/condition/Event 与相关资源；`cluster/namespace/resource/from/to/kind/search` 保存在 URL，浏览器 Back/Forward 恢复相同资源和时间上下文。
- schema 4 持久化 1 至 10 个 revision-owned Scope 与独立 `active_operational_scope`。Header 使用持久活动集群 selector；切换前 probe 目标 reader，成功后刷新 bootstrap 并清除旧资源查询上下文，失败时保持原活动 Scope。
- `K8S_CONNECTIONS_JSON` 注册 1 至 10 个具名 reader：cluster identity 唯一、最多一个 in-cluster、外部 kubeconfig 必须为绝对挂载路径并指定 context、每连接有 Namespace allowlist/default/timeout；未知 cluster fail closed。
- Helm 对 in-cluster + external、external-only 和单连接兼容路径执行 fail-closed render 校验；external-only Worker 不挂 ServiceAccount token，也不创建 Kubernetes read RBAC。

### Runtime 与数据审计

| 项目 | 结果 | 当前证据 |
|---|---|---|
| Build/runtime | `PASS` | `make local-up` 从当前 worktree 重建并加载 API/Worker/Migrate；Helm `cloudops` revision `15`；API、Worker、MySQL Ready，Migrate Job succeeded，全部 Pod `0` restart |
| Runtime images | `PASS` | local image IDs：API `sha256:0b6ffba1610b...`、Worker `sha256:30908d3cb8ab...`、Migrate `sha256:fc58b7110806...`；当前 Pods 使用本次新导入的 imageID |
| Schema | `PASS` | `goose_db_version=4`；`00003_infrastructure_topology.sql` 与 `00004_operational_scope_registry.sql` 已应用 |
| MySQL integration | `PASS` | MySQL `8.0.46` 上 `TestMySQLConfigurationRevisionSecretAndWorkerBoundary` 真实执行；两 Scope revision、active Scope 切换、secret 与 Worker activation 通过；临时库和临时用户残留均为 `0` |
| Final configuration | `PASS` | active revision `8`，ID `a50e1a81-e89d-4802-8c26-711c143c318c`，hash `dcce24f597b880a66e5c4bdcfbdb99898d80ae4e1e36728088481b50a3bd57c4` |
| Worker boundary | `PASS` | task `c748118e-e218-4fbc-854e-6b4eafb02546` succeeded；Worker `cloudops-worker-5f5476d74b-mmh6f` observed exact revision hash |
| Active Scope | `PASS` | Scope `970c8025-35f5-42cc-9f6d-7bffb8b23877`，cluster `cloudops-local`，Namespaces `cloudops-system,demo`，`active=true` |
| Multi-Scope fail closed | `PASS` | UI 加入未注册 `cluster-unregistered` 后 validation `KUBERNETES_READER_UNAVAILABLE`、typed readers `1/2`、发布禁用；删除后 `1/1` available 并成功发布 revision `6` |
| Provider unavailable | `PASS` | revision `7` 停用 Kubernetes 后 Overview/Infrastructure/API 均为 `disabled`、`0 nodes`、`0 edges`、`0 Canvas`、无假拓扑；revision `8` 恢复 enabled |
| Final topology | `PASS` | `kubernetes://cloudops-local` / Kubernetes `v1.36.1`；snapshot `96c9bf2e-506a-4b8d-be96-36fe70d14e9b`，content hash `547997cdd30140f3095db005b93f4d23bf919c91b318aa2fa821028bb23ed09d`，`13 nodes / 27 edges`，非 partial、非 truncated |

### 浏览器联调证据

验收 URL：`http://127.0.0.1:18083`。当前工具面未提供可调用的 Chrome DevTools/Playwright MCP；以下浏览器证据来自工作区 `@playwright/test` + Chromium，不冒充 MCP。

| 维度 | 结果 | 证据 |
|---|---|---|
| Real resource flow | `PASS` | `cloudops-api` Deployment -> Pod `cloudops-api-5848cbdf6d-4r54s` -> 4 条真实 Scheduled/Pulled/Created/Started Event -> Namespace `cloudops-system`；Namespace Event 为 `200` 空投影；连续 Back 返回同一 Pod、Workload 和 Overview selection |
| Canvas | `PASS` | 1440x900 drawing buffer `888x748`，`63,956` 个非背景像素、非背景比 `0.096287`、WebGL error `0`；Canvas node count `13` 与 structured `13/13` 一致 |
| Interaction | `PASS` | 1440x900 拖拽像素签名 `6922a7e1 -> 5d1221dc`；390x844 为 `a67fae3d -> c5f4ff22`；两端重绘后非背景像素仍大于 `0`，console 均无错误 |
| Fallback | `PASS` | `?view=structured` 显式路径为 `13/13`、无 Canvas；强制 WebGL init failure 自动切换 structured `13/13`、无 Canvas并显示明确原因 |
| Responsive | `PASS` | 1440x900、390x844、320x568 实测；390 Canvas `390x360`；390/320 的 `scrollWidth` 分别等于 `390/320`，header selector、工具栏、内容与 bottom navigation 无遮挡 |
| Network | `PASS` | topology/resources/detail/events 均为 `/api/v1` 且 `200`；Pod Event request/trace `dk8ddnpi8ruc-7e`，Namespace Event request/trace `dk8ddp6gviyf-7j` |
| Console | `PASS` | 正常 desktop/mobile、Settings、Context Link 与 Provider-disabled 流程无 console/page error；自动 fallback 仅出现预期的 WebGL context 创建失败日志 |
| Task result | `DONE_WITH_NOT_RUN` | Task 2 本地单活动集群纵向能力、multi-cluster configuration contract 与 fail-closed 边界完成；缺少的外部验收维度如下列为 `NOT RUN` |

`web-design-guidelines` 最新规则用于最终审查并影响实现：Scope 删除增加确认；native select 补 name/autocomplete 与暗色颜色；Scope 错误提示增加 live region；时间统一用 `Intl.DateTimeFormat`；Canvas 文案改为真实 freshness/snapshot；spinner 遵循 reduced-motion。

### 检查结果

`PASS`：

- `go test -count=1 ./...`
- `go vet ./...`
- `go test -count=1 ./internal/settings -run TestMySQLConfigurationRevisionSecretAndWorkerBoundary -v`（MySQL 8.0.46）
- `npm exec -- vue-tsc --noEmit`
- `npm run lint -- --quiet`
- `npm test`：13 files / 51 tests
- `npm run build`
- `helm lint --strict charts/cloudops -f charts/cloudops/values-local.yaml`
- `bash scripts/check-runtime-render.sh`
- `git diff --check`
- `make local-up`、`make local-status`、`/readyz`、`/api/v1/bootstrap`、`/api/v1/scopes`、Infrastructure/Atlas V1 routes

### 实际文件清单

- Domain/data：`CONTEXT.md`、`migrations/00003_infrastructure_topology.sql`、`migrations/00004_operational_scope_registry.sql`、`internal/infrastructure/**`、schema/migration contract tests。
- Kubernetes/provider：`internal/infra/{kubernetestopology,infrastructuregateway}/**`、`internal/bootstrap/worker_infrastructure.go`、`internal/config/kubernetes*`、`internal/infra/k8sread/**`。
- API/runtime：`internal/api/{infrastructure_handler,infrastructure_contract_test,platform_handler,handler}.go`、`internal/{router,di,startup,settings,bootstrap}/**` 相关文件、`docs/api-v1-openapi.yaml`。
- Frontend：`frontend/src/api/{platform,infrastructure}.ts`、`frontend/src/components/infrastructure/**`、`frontend/src/views/{overview,infrastructure,settings,workspaces}/**`、layout/router/operationalScope utilities、Three.js dependencies。
- Helm：`charts/cloudops/templates/{_helpers,rbac,runtime-validation,worker}.yaml`、`charts/cloudops/{values,values.schema}.yaml`、`scripts/check-runtime-render.sh`。
- Task 2 implementation commit 共 `61` 个精确文件；状态文件为本节证据文件，不计入 implementation 数。

### Delivery record

| 项目 | 结果 | 证据 |
|---|---|---|
| Branch/base | `PASS` | `codex/v3-refactor`；implementation parent 为 `9edc02b` |
| Implementation commit | `PASS` | `49a6d86e8efa1ef34ae2a77ed73e978c693a89e4`；61 个精确 implementation 文件 |
| Worktree preservation | `PASS` | 保留任务开始时及后续全部未提交成果，未 reset、revert 或清理 |
| Push | `NOT RUN` | 本地提交尚未 push；未创建 PR/tag、未触碰默认分支 |

### NOT RUN

- 第二个真实 Kubernetes 集群：当前只存在一个可用 reader/真实集群 `cloudops-local`。多连接实现、render contract、真实 MySQL 两 Scope persistence 与未注册 cluster fail-closed 已验证，但未用当前 kind 的别名伪造第二集群成功证据。
- Kubernetes MCP：当前环境没有 Kubernetes MCP server；`kubectl` 与产品 typed client 的真实证据不冒充 MCP。
- Chrome DevTools MCP 与 Playwright MCP：当前工具面没有可调用的浏览器 MCP；工作区 Playwright 浏览器验收不冒充 MCP。
- Gateway API CRD kind：当前真实集群未安装 Gateway API CRD；Ingress typed projection 已实现并验证，Gateway API 成功读取 `NOT RUN`。
- hosted/staging/production、外部 Provider、PR、push、tag、默认分支：均不属于本轮本地 Task 2 收尾，`NOT RUN`。

## 任务 3：Monitoring

### 实施结果

- Worker 持有 bounded Prometheus adapter；API 只调用固定内部 gateway，浏览器不接触 Provider endpoint、credential 或原始 response。guided 与 expert 查询共享活动 Operational Scope、最大 lookback、timeout、response bytes、series、samples、step 与 concurrency 合同。
- Query Definition、Query Execution 与 Query Authorization 是三个独立持久化概念。Owner 可直接执行或绑定 immutable Definition；Agent 只能使用一次性 exact authorization 或已授权的 Definition version，scope/query/bounds expansion、revoked authorization 和浏览器伪造 `actor=agent` 均 fail closed。
- `migrations/00005_observability_queries.sql` 将 schema 提升到 version 5，保存 Definition revision/content hash、Execution provenance/result bounds、Authorization consumption/revocation和顺序审计事件；查询结果不复制为新的遥测事实库。
- Monitoring Workspace 提供真实 Workload selector、guided CPU/error query、expert PromQL、时间/step 控件、chart、table、history、audit、保存 Definition、授权/撤销及 Provider state。URL 保存 cluster/namespace/resource/mode/from/to/execution，可精确恢复同一查询上下文。
- Context Link 同时生成 CloudOps 内部精确链接和 Grafana Explore provider link；两者绑定 normalized PromQL、真实 resource identity 与绝对 UTC 时间范围。

### Runtime 与数据审计

| 项目 | 结果 | 当前证据 |
|---|---|---|
| Build/runtime | `PASS` | `make local-up` 从当前 worktree 重建；Helm `cloudops` revision `18`；API、Worker、MySQL、Prometheus、Grafana 均 Ready，当前 Deployment 均 `1/1` |
| Schema | `PASS` | `goose_db_version=5`；`migrations/00005_observability_queries.sql` 已应用 |
| Prometheus authority | `PASS` | Kubernetes MCP 复核 image `quay.io/prometheus/prometheus:v3.13.1-distroless`；6/6 成功 Execution 保存 provider identity `http://prometheus:9090` 与 server version `3.13.1` |
| Persistence/audit | `PASS` | 真实 MySQL：`query_executions=6`、`query_execution_events=18`、`query_definitions=1`、`query_authorizations=1`；每次成功执行均有 created/started/succeeded 三个顺序事件 |
| Definition | `PASS` | UI 保存 immutable Definition `48eee2ff-dded-4fa0-a6cb-b786ccbbc415`；Owner 绑定执行 `fdcaf50a-e695-46e3-b61a-45cd8492759e` 为 `202`，实际 bounds 收紧为 `900s / 200 series / 1000 samples` |
| Authorization boundary | `PASS` | UI 创建一次性 Authorization `eabad0f7-52ef-4544-a7cd-f8822777aa66` 得到 `201`，撤销得到 `204`，UI 与 MySQL 均为 `revoked`；API test 拒绝浏览器声明 Agent actor，domain test 证明 Agent 复用 Owner bounded contract 且 revoked authorization 被拒绝 |
| Provider unavailable | `PASS` | Kubernetes MCP 将 Prometheus scale 到 `0` 后 catalog 仍为 `200` 且 `provider_state=unavailable`，UI 显示“Prometheus 不可用”、endpoint 与禁用执行按钮；恢复 `1` 后 Pod Ready、catalog available，恢复执行 `6fb46419-798f-4e28-9979-a57454169b70` succeeded |

### MCP 联调证据

验收 URL：`http://127.0.0.1:18082/monitoring`；Playwright MCP 与 Kubernetes MCP 直接操作当前 `kind-cloudops-local` / `cloudops-system` runtime。工具面没有独立 Prometheus MCP，因此 Provider 调用证据来自产品真实链路和 Prometheus provenance，不冒充专用 MCP。

| 维度 | 结果 | 证据 |
|---|---|---|
| Guided query | `PASS` | 选择真实 `cloudops-api` Deployment；CPU execution `d2cbf7ec-ba40-46c5-9aa5-46fd7a861abb` 返回 `1 series / 22 samples`，error execution `70fa6d27-85bf-43a8-bd40-1d3eb171d574` 返回 `1 / 10` |
| Expert PromQL | `PASS` | execution `ebc862dc-2f9e-4bce-87bc-cb4d534b02ca` 返回 `4 series / 92 samples / 2.4 KiB`；UI chart、四行 table、history 与审计显示 `status=200/202/304/403` |
| UI/API/Provider chain | `PASS` | Playwright network 记录 UI `POST /api/v1/monitoring/queries` -> `202`，随后 execution `GET` -> `200`；页面结果与 MySQL 中的 Prometheus `3.13.1` provenance、series/sample counts 一致，无 fixture 或浏览器直连 Provider |
| Definition/authorization | `PASS` | UI 保存 Definition，并对同一 normalized query 创建和撤销 Authorization；页面显示“已撤销”，数据库 projection 一致 |
| Exact Context Link | `PASS` | CloudOps 与 Grafana 均打开 normalized PromQL `sum by (status) (rate(http_requests_total{...}[5m]))`，绝对范围 `2026-07-26T13:26:00Z` 至 `13:41:00Z`；Grafana 本地显示 `21:26:00` 至 `21:41:00`，图例同为 `200/202/304/403` |
| Invalid query | `PASS` | UI 提交 `sum(` 得到可见 `422 INVALID_MONITORING_QUERY`；request/trace `dk8jqcsd0d8f-6x` |
| Oversized query | `PASS` | UI 提交 `6h` range + `15s` step，`1441` points 超过 `1000` samples bound，得到可见 `422 INVALID_MONITORING_QUERY`；request/trace `dk8jqo3pcb0v-76` |
| Console | `PASS` | 最终页面无 Vue/page exception；浏览器记录的两个 resource error 是上述预期 `422` 负向请求，未被误报为成功 |
| Task result | `DONE_WITH_NOT_RUN` | Task 3 本地真实 UI-to-Prometheus 纵向能力与 unavailable 分支完成；未运行项如下 |

`domain-modeling` Skill 用于收敛领域语言并影响实现：Definition 描述可复用且不可变版本化的查询意图，Execution 描述一次有界 Provider 调用及 provenance，Authorization 只描述 Agent 执行权限；三者没有合并为一个可变“saved query”记录。

### 检查结果

`PASS`：

- `make check`：module/structure、`go vet`、`golangci-lint`、Go tests、race tests、build、frontend lint/typecheck/51 tests/build、actionlint、ShellCheck、Helm strict lint、runtime render、kubeconform `24/24` 与 naming gate 全部通过。
- `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`：`0 issues`。
- `go test -count=1 ./internal/observability ./internal/api`。
- `git diff --check`。
- `make local-up` 与 Kubernetes/Playwright MCP runtime 复核。

### 实际文件清单

- Domain/data：`internal/observability/**`、`migrations/00005_observability_queries.sql`、schema/migration contract tests、`CONTEXT.md`。
- Provider boundary：`internal/infra/{monitoringprometheus,monitoringgateway,prometheus}/**`、`internal/bootstrap/worker_monitoring.go` 与 Worker/API runtime wiring。
- API/contract：`internal/api/monitoring_handler*`、router/dependency wiring、`docs/api-v1-openapi.yaml`。
- Frontend：`frontend/src/api/monitoring.ts`、`frontend/src/views/monitoring/**`、Monitoring route。
- Configuration/runtime：Prometheus/Context Link settings、Helm monitoring stack、runtime validation、local lifecycle 与 Docker dependency closure。

### Delivery record

| 项目 | 结果 | 证据 |
|---|---|---|
| Branch/base | `PASS` | `codex/v3-refactor`；当前 HEAD `13079082c98e18f837c212212859f5347d670e41` |
| Worktree preservation | `PASS` | 保留任务开始时和本轮全部未提交成果；未 reset、revert 或清理 |
| Local commit | `PASS` | Task 3 implementation 与本状态节一并纳入本地 commit；精确 SHA 以提交完成后的 Git HEAD 为准 |
| Push | `NOT RUN` | 未创建 PR 或 tag，未 push，未触碰默认分支 |

### NOT RUN

- 独立 Prometheus MCP：当前工具面未提供；真实 UI/API/Prometheus 与 Kubernetes MCP 证据不冒充专用 Prometheus MCP。
- 真实 Agent runtime/LLM 发起的 PromQL：Agent 属于任务 6，当前未启用真实模型调用；共享 bounded contract、浏览器 actor 隔离、一次性授权及撤销已由实现、自动化测试和真实 Owner UI 验证，但不冒充 Agent runtime PASS。
- 长时间运行查询的 live cancellation：本地查询在取消操作前已完成；cancellation API、持久状态与 Worker context 已实现并通过完整编译/静态门禁，但真实 in-flight cancellation 为 `NOT RUN`。
- hosted/staging/production、外部 Prometheus、PR、push、tag、默认分支：`NOT RUN`。

## 任务 4：Logs 与 Traces

### 实施结果

- 复用了任务 1 的 Configuration Revision、Operational Scope、Provider health、bounded query、Worker gateway、Shell 与通知合同；任务 2 的 Kubernetes resource identity 和任务 3 的绝对 UTC time range/query execution 可无损进入 Logs、Traces 与 Workload。
- Worker 持有 Elasticsearch 与 Tempo 只读 adapter；API 只调用固定内部 gateway。浏览器不接触 Provider credential，不直连 Elasticsearch，也不把完整日志或 Trace 数据复制到 MySQL。
- Logs Workspace 实现 guided/expert query、固定 bounds、histogram、field projection、bounded tail、查询历史、虚拟化长列表、`trace_id` 关联与精确 Kibana link 生成合同。
- Traces Workspace 实现 bounded TraceQL search、Trace detail、waterfall/span tree、critical path、字段检查、查询历史、Kubernetes resource correlation 与精确 Tempo link。
- 只有 Owner 显式选择的有界 log/span 片段保存为 Evidence；Consultation 在单事务内保存引用 query execution、Evidence、resource、time range 与 Configuration Revision 的不可变 Context Snapshot。
- `migrations/00006_telemetry_evidence_context.sql` 将 schema 提升到 version 6；扩展共享 Query Definition/Execution/Authorization provider enum，并增加 query-scoped Evidence、Consultation 与 Context Snapshot。

### Runtime 与数据审计

| 项目 | 结果 | 当前证据 |
|---|---|---|
| 前置合同 | `PASS` | 任务 1 shared contract、任务 2 resource context、任务 3 query/resource/time context 均由当前 API、URL 和持久 execution identity 实际复用 |
| Build/runtime | `PASS` | `kind-cloudops-local` / `cloudops-system` / Helm `cloudops` revision `21`；API、Worker、MySQL、Elasticsearch、Filebeat、OTel Collector、Tempo、Prometheus 均 Ready |
| Final images | `PASS` | API manifest `sha256:6b13748af8b4f96a8c6da4e6b9e0fc5cd42e286ad83265d458d9e6d6cf154554`，Worker manifest `sha256:079f2643097efe9dc722049163ba8049668d8e295b4f78ff8516f24150c12091`；Pod 内 binary hash 与最终本地镜像一致 |
| Schema | `PASS` | `goose_db_version=6`；`00006_telemetry_evidence_context.sql` 已应用 |
| Elasticsearch chain | `PASS` | 真实 Elasticsearch `9.4.3` 与 Filebeat `9.4.3`；当前 MySQL 有 `7` 次 succeeded、`3` 次 fail-closed execution，Filebeat 最终采集批次持续 ack |
| Tempo chain | `PASS` | 真实 Tempo `3.0.2` 与 OTel Collector `0.156.0`；当前 MySQL 有 `9` 次 succeeded、`4` 次 fail-closed execution，目标 trace detail 多次由 Tempo 返回 `200` |
| Bounded persistence | `PASS` | query-scoped Evidence 为 `3` 条 Elasticsearch log selection、`4` 条 Tempo trace selection；`3` 个 Consultation 与 `3` 个 Context Snapshot；`query_executions` 不含 raw result/provider response 列 |
| Empty/truncated | `PASS` | empty execution `c5346ee9-5e24-4062-9f59-b2dced513233` 为 `0 rows / 161 B / truncated=false`；execution `b2b10f3b-3035-4157-8cad-36d2e327560e` 为 `200 rows / 95297 B / truncated=true` |
| Provider unavailable | `PASS` | Elasticsearch/Tempo 分别 scale `0` 后 catalog 显示 unavailable、UI 禁用查询、直接请求返回 `503 TELEMETRY_PROVIDER_UNAVAILABLE`；failed executions `f020b5a6-7723-4ac6-978b-8c759ba6d7b1` 与 `7e1af181-aa7d-4a18-829a-affb71b94be4` 保存 `PROVIDER_UNAVAILABLE`；两者均已恢复 `1/1` |

### MCP 联调证据

验收 URL：`http://127.0.0.1:18080`。Chrome DevTools MCP 与 Kubernetes MCP 操作当前 `kind-cloudops-local` / `cloudops-system` runtime；工具面没有独立 Logs、Traces、Elasticsearch 或 Tempo MCP，未把产品链或 `kubectl` 冒充专用 MCP。

| 维度 | 结果 | 证据 |
|---|---|---|
| Monitoring -> Logs | `PASS` | Prometheus execution `5c40829e-98a2-4ea7-b291-2aa70c04780d` 为 `1 series / 31 samples`；从选定时间点进入同一 `cloudops-api` Deployment 的 Logs，bounded execution `b2b10f3b-3035-4157-8cad-36d2e327560e` 返回真实 Elasticsearch 日志 |
| Logs -> trace_id | `PASS` | 日志行携带 `trace_id=59de591edc477ffb218ecea260672177`；UI 保持 resource/time context 并打开 Trace；最终镜像复跑使用 trace `1a463bde076b49353ff543ba8f3ed70a` 与 Logs execution `e9025a57-9ce8-47f4-b53b-807aa3ba0235` |
| Trace waterfall | `PASS` | Tempo execution `3f20f1bb-cf1d-4cb7-9a60-a06c378954cd` 返回 root `cloudops-api · GET /metrics`、span `940043a3733d8025`、`0.917 ms`；最终镜像 trace detail 为 `cloudops-api · GET /api/v1/bootstrap`、span `84ff237f8a5dd11e`、`2.46 ms` |
| Workload correlation | `PASS` | Kubernetes MCP 与 UI 均确认 Deployment `cloudops-api` -> Pod `cloudops-api-6f9b7f9bc5-m5wpl`；Service `cloudops-api` 通过 EndpointSlice/selector 路由到该 Pod，并显示 Deployment rollout Events |
| Evidence | `PASS` | 初次 Log/Trace Evidence 为 `cfca4ca2-7cf7-4b25-b3d6-0fda845dce2d` / `07ee7f56-d03d-4c96-95aa-6532884d429a`；最终镜像复跑保存 `cfbb7210-8091-4f1d-93c5-310b02cc243c` / `ab1c5235-7896-4a13-8d45-f43fe3e235cc`，均只包含 1 个显式选择项 |
| Frozen context | `PASS` | Consultation `711bea8c-3bfe-4c7e-8572-670cdfe38550` 创建 Snapshot `1ad71a5b-b4f3-4f1d-8265-1623a8d3b59e`，hash `1580b8f4acce94f7432f33cfb7f698caf88f5fd17d1032226001adf182c596c8`，引用 exact execution `17faf25f-b4a8-4441-a68f-988ce947f8b4` 与 Evidence `ab1c5235-7896-4a13-8d45-f43fe3e235cc` |
| Browser health | `PASS` | 最终硬刷新后 21 个 document/asset/API 请求全部 `200`，目标 Tempo detail 为 `200`；console 无 message、Vue warning 或未处理异常 |
| Provider logs | `PASS` | Kubernetes MCP 读取当前 API、Filebeat、Tempo Pod：Evidence/Consultation routes 为 `201`，Filebeat published/acked 持续前进，Tempo 对目标 trace 的 detail query 为 `200` |
| Task result | `DONE_WITH_NOT_RUN` | 本地真实 UI -> API -> Elasticsearch/Tempo -> Kubernetes -> Evidence/Snapshot 核心链与 unavailable 分支完成；未运行项如下 |

`web-design-guidelines` Skill 用于核对 Logs/Traces 的可访问名称、键盘可选行、稳定长列表/waterfall 尺寸、无横向溢出和跨 Workspace 导航。当前技能面没有独立可观测性 Skill，因此没有虚构该维度的执行证据。

### 检查结果

`PASS`：

- `go test -count=1 ./...`。
- gofmt、goimports、dependency、structure、`go vet` 与 `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`（`0 issues`）。
- frontend lint（exit `0`、`0 errors / 1493 warnings`）、Vue typecheck、Vitest `14 files / 54 tests`、production build。
- 专项 Playwright `frontend/tests/e2e/telemetry-navigation.spec.ts`：`1/1`。
- actionlint、ShellCheck、Helm strict lint/template/runtime contracts、kubeconform `35/35`。
- Elasticsearch/Tempo unavailable 与恢复、8 轮并发 topology/resource 回归、最终 Chrome network/console 检查。

`FAIL`：

- `make local-doctor`：本轮用于浏览器验收的 tmux port-forward 未登记为 lifecycle managed PID；最新保留 backup 仍为 schema `1`，而当前 runtime schema 为 `6`。命令真实返回非零；未新建 backup 掩盖该结果。

### 实际文件清单

- Domain/data：`internal/telemetry/**`、`migrations/00006_telemetry_evidence_context.sql`、schema/migration/Evidence contract tests。
- Provider boundary：`internal/infra/{telemetryread,telemetrygateway}/**`、`internal/bootstrap/worker_telemetry.go` 与 Worker/API wiring。
- API/contract：`internal/api/telemetry_handler*`、router/dependency wiring、`docs/api-v1-openapi.yaml`。
- Frontend：`frontend/src/api/telemetry.ts`、`frontend/src/models/telemetry*`、`frontend/src/views/{logs,traces}/**`、telemetry workspace styles、Monitoring Context Link 与 router/layout tests。
- Runtime：`charts/cloudops/templates/telemetry-stack.yaml`、Chart values/schema/runtime validation、`scripts/{check-runtime-render,local-lifecycle}.sh`。
- Task 4 implementation 为 `48` 个精确文件；本状态文件不计入 implementation 数。

### Delivery record

| 项目 | 结果 | 证据 |
|---|---|---|
| Branch/base | `PASS` | `codex/v3-refactor`；当前 HEAD `84f62be65eba0494dd6d6a24e342eaf9960a4abd` |
| Worktree preservation | `PASS` | 保留任务开始时及本轮全部未提交成果；未 reset、revert 或清理 |
| Local commit | `PASS` | Task 4 implementation 与本状态节纳入同一本地提交；精确 SHA 以提交完成后的 Git HEAD 为准 |
| Push | `NOT RUN` | 未创建 PR/tag，未 push，未触碰默认分支 |

### NOT RUN

- 独立 Logs、Traces、Elasticsearch、Tempo MCP：当前工具面未提供；Chrome DevTools/Kubernetes MCP 与真实产品 Provider 链不冒充这些专用 MCP。
- Kibana 实例与外部 Elasticsearch/Tempo：当前本地 release 没有 Kibana；精确 Kibana link 生成合同已实现，但真实 Kibana 打开 `NOT RUN`。本轮 PASS 仅属于实际运行的本地 Elasticsearch、Tempo、Prometheus 与 Kubernetes 链。
- hosted/staging/production、外部 Provider、真实 Agent/LLM 消费 Evidence、PR、push、tag、默认分支：`NOT RUN`。

## 任务 5：Alerts 与显式 Incident escalation

### 实施结果

- `Signal` 保持不可变来源事实；Alertmanager envelope 先完整校验和规范化，再按稳定 `source + fingerprint` 更新独立 `Alert` aggregate。每个 Signal 通过不可变 link 保留 provenance，firing/resolved 使用不同 canonical `source_event_id`，重复投递由数据库唯一约束和 ingress lock 幂等收敛。
- Alert lifecycle 独立保存状态、recurrence、version 与 event timeline；acknowledgement、silence、Investigation relation 和 Incident relation 是不同对象。resolved Alert 不关闭 Incident，silence 也不等于 acknowledgement 或 resolution。
- Owner 可执行幂等 acknowledge、300 至 86400 秒的 bounded silence、提前 expire silence、启动 Investigation，以及显式创建或关联 Incident。Alertmanager adapter 只发送 bounded exact matcher silence，并保存 provider silence identity、Configuration Revision 与审计事件。
- Settings 支持 revision-owned Escalation Policy、severity/namespace/label matcher bounds、最短 firing 时间与 recurrence threshold；默认 `automatic_escalation_enabled=false`，没有 policy 时 firing 只创建 Alert 与通知，不创建 Incident。
- Alerts Workspace 提供 firing/resolved、severity、namespace、search、ack/silence/Investigation/Incident facets，详情展示原始 Signals、完整 timeline、Context Links 和 provenance；桌面与移动端复用任务 1 Shell。
- P1/P2 firing 生成去重的 durable Owner Notification，并携带 exact Alert Context Link。当前 P1 notification 指向同一 Alert、cluster 与 namespace。
- `migrations/00007_alert_lifecycle.sql` 将既有 automatic Signal-to-Incident 历史迁移为 Alert、Signal link 和 Incident link，不改写历史 Signal/Incident；provenance 固定为 `legacy_automatic_ingress`。
- `domain-modeling` Skill 用于核对领域边界；现有 `CONTEXT.md` 已准确区分 Signal、Alert、Acknowledgement、Silence、Investigation、Escalation Policy 与 Incident，因此无需制造第二份领域模型。

### Runtime 与数据审计

| 项目 | 结果 | 当前证据 |
|---|---|---|
| 前置合同 | `PASS` | 任务 0 schema/lifecycle 与任务 1 notification、Settings、Context Link contract 均由当前实现直接复用 |
| Build/runtime | `PASS` | `kind-cloudops-local` / `cloudops-system` / Helm `cloudops` revision `25` deployed；Migrate Job `cloudops-migrate-25` succeeded；API、Worker、MySQL、Prometheus、Alertmanager 均 Ready，当前 API/Worker/Alertmanager 为 `0` restart |
| Final images | `PASS` | API `sha256:b283dd31078f6d0dbafd2da8609d2ac8eece921dd008ee5429b4ef1f9c9030b6`；Worker `sha256:6979ed8e34c89e7bc413d1b86603da94d299670c7e92c21ff63b947ae904600f`；Migrate `sha256:8c9e35610d63bf7252d484d94d16defacda22d46f060a35c232097e6aff09fee` |
| Schema | `PASS` | `goose_db_version=7`；`migrations/00007_alert_lifecycle.sql` 已应用 |
| Active configuration | `PASS` | revision `11`，ID `19e127ca-5a96-4f99-9e61-1dea0a2f81e2`，hash `2fdfebc8f45167b6974705b415b02da85fae36bf47ae79f5af5a2ecf99045595`；Worker activation `succeeded` 且 observed hash 相同 |
| Alertmanager configuration | `PASS` | provider enabled，endpoint `http://alertmanager.cloudops-system.svc:9093`；`automatic_escalation_enabled=0`，当前 revision policy count `0` |
| MySQL totals | `PASS` | `27` Signals、`10` Alerts、`27` Alert-Signal links、`15` Alert-Incident links、`16` Alert events、`9` Incidents、`1` acknowledgement、`1` silence、`1` Owner Notification |
| Acceptance Alert | `PASS` | Alert `d82cf76f-c923-4e90-a131-4f9faf528c15` / fingerprint `bd01a46a78652be5` 最终 `resolved`、version `6`、`2` Signals、recurrence `1`；firing/resolved source events 各精确 `1` 条 |
| Explicit Incident | `PASS` | Owner 创建 Incident `98c6344c-db04-4f40-b755-b631c0377af3`，link `368de682-0d83-41f2-a473-13430526e505` provenance=`owner_created`；Alert resolved 后 Incident 仍为 `detected` |
| Legacy provenance | `PASS` | `9` 个 migrated Alert；`25` 个 legacy Alert-Signal links 与 `14` 个 legacy Alert-Incident links 均保留 `legacy_automatic_ingress`，未重写原始记录 |

### MCP 与纵向联调证据

验收 URL：`http://127.0.0.1:18080`。Playwright MCP、Chrome DevTools MCP 与 Kubernetes MCP 直接操作当前 `kind-cloudops-local` / `cloudops-system` runtime。当前工具面没有独立 Alertmanager MCP；下表中的 Provider 证据来自真实 Alertmanager 原生 API 与产品链，不冒充专用 MCP。

| 维度 | 结果 | 证据 |
|---|---|---|
| Real firing -> Alert | `PASS` | Prometheus rule `CloudOpsAlertLifecycleValidation` 真实 firing，经 Alertmanager webhook 生成 Signal `cbc5463f-1865-4379-9c1e-a169447a3ddf`、source event `51065e07add91c8eae2ed92169b21fe65726ed0e26efe28ec49fe2e32e100b31` 与独立 Alert；firing 后无 automatic Incident link，随后才由 Owner 显式创建 Incident |
| Acknowledge | `PASS` | acknowledgement `6f5d29b7-e910-4966-92f4-be4d3e5407fb`，recurrence `1`、Alert version `1`、actor `local/owner/owner`；UI 显示 reason 与 Ack facet |
| Bounded silence | `PASS` | CloudOps silence `4e431a2c-bde8-4219-82b5-b40920e08e8c` 请求 `300` 秒；Alertmanager 返回 provider ID `f2b7b653-7ff5-410c-b9b3-379c354b428c`，原生 `/api/v2/silences` 显示 6 个 exact equality matcher，最终状态 `expired` |
| Explicit Incident branch | `PASS` | 本次 MCP 验收选择规范允许的显式 Incident 分支；详情同时展示 Investigation 入口、`0` Investigation、`1` 个 linked detected Incident 及 exact Incident link |
| Resolved Signal | `PASS` | resolved Signal `a66fa499-d2ab-4554-95d0-5bc3eafba882` / source event `52b3704b8dafb906e4b953157eb64562ab8a43a3f176dcb68fcde0c4dce413dd` 更新同一 Alert 到 `resolved v6`，没有关闭 linked Incident |
| Duplicate webhook | `PASS` | 重投 response 为 `{"alerts":1,"duplicates":1,"ingested":1,"rejected":0,"status":"accepted"}`；重投前后 totals 不变，resolved source event count 仍为 `1`，未新增 Alert、Signal、Incident 或 link |
| Owner Notification | `PASS` | notification `c4d4c430-e842-451d-9a89-6018d7e1cf25` 为 `P1 / alert / firing:1`；drawer exact link 为 `/alerts/d82cf76f-c923-4e90-a131-4f9faf528c15?cluster_id=cloudops-local&namespace=demo` |
| Alerts Workspace | `PASS` | desktop 与 `390x844` mobile 实测；filters URL 精确保留 resolved/critical/namespace/search，结果仅目标 Alert；详情显示 resolved v6、2 Signals、expired silence、linked Incident，mobile `clientWidth=390 / scrollWidth=390` 无横向溢出 |
| UI/API/console | `PASS` | 最终硬刷新 document/assets/bootstrap/scopes/notifications/SSE/Alert list/detail 均为 `200` 且业务请求只使用 `/api/v1`；Chrome DevTools 与 Playwright console 均为 `0 errors / 0 warnings` |
| Kubernetes MCP | `PASS` | 最终 API、Worker、Prometheus、Alertmanager Deployment 均 `1/1`；对应 Pod Ready，API/Worker/Alertmanager restart 均为 `0` |
| Alertmanager final state | `PASS` | 验收后 Prometheus rule 恢复为 `vector(0) > 0`，状态 `inactive`、health `ok`；没有留下 firing test Alert |
| Task result | `DONE_WITH_NOT_RUN` | 本地真实 Signal -> Alert -> acknowledge -> silence -> explicit Incident -> resolved Signal 主链、UI/API/MySQL/Alertmanager 与重复 webhook 验收完成；未运行项如下 |

### 检查结果

`PASS`：

- `make check`：Go unit/integration 与 race suite、`go vet`、`golangci-lint`（`0 issues`）、gofmt/goimports/dependency/structure。
- frontend ESLint（exit `0`、`0 errors`，保留仓库既有 warning baseline）、Vue typecheck、Vitest `15 files / 55 tests`、production build。
- actionlint、immutable workflow dependency audit、ShellCheck。
- Helm strict lint/template/runtime contracts；kubeconform `38/38`；first-party naming guard。
- Alertmanager adapter/gateway、Alert ingress/service/policy、Settings Escalation Policy、API/router、migration 与 frontend routes/API 的专项测试均包含在上述门禁。
- `git diff --check`（状态文件更新后复跑）。

### 实际文件清单

- Domain/data：`internal/alert/**`、`migrations/00007_alert_lifecycle.sql`、schema/migration contract tests。
- Provider boundary：`internal/infra/{alertmanagerapi,alertmanagergateway}/**`、`internal/bootstrap/worker_alertmanager.go`、Alertmanager ingress normalization 与 Worker/API wiring。
- API/contract：`internal/api/alert_handler.go`、router/dependency wiring、`docs/api-v1-openapi.yaml`。
- Frontend：`frontend/src/api/alerts.ts`、`frontend/src/components/alerts/**`、`frontend/src/views/alerts/**`、router tests，以及 Settings Escalation Policy UI/API tests。
- Runtime：`charts/cloudops/templates/alerting-stack.yaml`、Prometheus/Alertmanager Helm values/schema、runtime validation 与 local lifecycle scripts。
- Task 5 implementation 为 `54` 个精确文件；本状态文件不计入 implementation 数。

### Delivery record

| 项目 | 结果 | 证据 |
|---|---|---|
| Branch/base | `PASS` | `codex/v3-refactor`；当前 HEAD `0f7160b340804c3945a8ce2ede64d248480bbf54` |
| Worktree preservation | `PASS` | 保留任务开始时及本轮全部未提交成果；未 reset、revert 或清理 |
| Local commit | `PASS` | Task 5 的 `54` 个 implementation 文件与本状态节纳入同一本地提交；精确 SHA 以提交完成后的 Git HEAD 为准 |
| Push | `NOT RUN` | 未创建 PR/tag，未 push，未触碰默认分支 |

### NOT RUN

- 独立 Alertmanager MCP：当前工具面未提供；真实 Alertmanager `v0.33.1`、原生 API、CloudOps adapter、浏览器与 Kubernetes MCP 证据不冒充专用 MCP。
- Live Investigation 分支：规范允许 Investigation 或显式 Incident 二选一；本轮选择并通过显式 Incident 分支。Investigation relation/API/UI 已实现并通过自动化测试，但真实 Agent Investigation 属于任务 6，当前 `NOT RUN`。
- 启用 automatic escalation 后由 policy 自动创建 Incident：当前验收有意保持产品默认 `off` 且 policy count `0`；revision-owned policy validation/匹配实现与自动化测试已通过，但 enabled live branch `NOT RUN`。
- hosted/staging/production、外部 Alertmanager、系统级浏览器通知送达、真实 Agent/LLM、PR、push、tag、默认分支：`NOT RUN`。

## 任务 6：Agent Investigation、Consultation 与 Knowledge

### 实施结果

- 复用并前向扩展唯一 `agent_runs` / `agent_steps` / `evidence_items` 真相源，没有创建第二套 Investigation 聚合。新的 Alert Investigation 直接绑定 Alert，不隐式创建或要求 Incident；旧 Incident Agent 历史继续可读。
- `migrations/00008_agent_workspace.sql` 与 `00009_agent_workspace_tasks.sql` 将 schema 提升到 `9`，新增 durable Consultation message、SSE event、Evidence citation、Knowledge revision、Guidance citation、Action Card、Operation Plan、Authorization 及独立 Workspace task/attempt lease 合同。
- Worker 增加 durable Agent Workspace runner，从 MySQL claim pending run，按精确 Configuration Revision 调用现有 typed Kubernetes、Prometheus、Elasticsearch、Tempo gateway；模型未启用时仍保存真实 bounded Provider Evidence，并以 `MODEL_PROVIDER_DISABLED` 和 `insufficient` fail closed。
- Consultation 支持 list/detail、message、cancel、持久 SSE replay 与显式 Context Snapshot。页面导航不会静默改写旧 snapshot；只有 Owner 执行“附加当前上下文”才创建新的 immutable snapshot/hash。
- Current Evidence 只来自当前 run 的 bounded Provider observations；旧聊天、Owner-confirmed Knowledge 与 Runbook 只进入独立 Guidance Citations，并保存 exact revision/hash 与 age，不冒充当前事实。
- `/agent` 已替换占位页，提供 Investigation/Consultation history、conversation/tool progress、Context/Evidence/Guidance/Authority inspector；Header 的全局 Agent 面板复用同一 store，并对完整 Workspace/compact 实例使用独立 DOM identity。
- 三层 authority 已落地：read-only bounded tools 可直接运行；reversible action 必须 exact Action Card hash；high-impact/external action 只能形成 immutable Operation Plan。错误 hash 返回冲突，零授权时没有 mutation execution adapter 被调用。
- API/OpenAPI、Logs/Traces “咨询 Agent”入口、Alert Investigation starter、router/DI/startup、schema contracts 与本地 lifecycle 均已纵向接入。`scripts/local-lifecycle.sh` 修正 migration upgrade 时旧 API/Worker 阻塞 Helm wait 的顺序问题。
- `domain-modeling` Skill 用于核对领域边界；现有 `CONTEXT.md` 已准确区分 Evidence、Guidance、Context Snapshot、Knowledge、Runbook、Operation Plan 与 Action Authorization，无需创建重复 glossary 或 ADR。

### Runtime 与数据审计

| 项目 | 结果 | 当前证据 |
|---|---|---|
| 前置合同 | `PASS` | 任务 1 的 Configuration Revision、Operational Scope 与 Context Link，以及任务 2-5 的真实 Kubernetes、Prometheus、Elasticsearch、Tempo、Alertmanager Evidence source 均由当前实现直接复用 |
| Build/runtime | `PASS` | `kind-cloudops-local` / `cloudops-system` / Helm `cloudops` revision `29` deployed；schema `9`；API、Worker、MySQL、Prometheus、Elasticsearch、Tempo、Alertmanager、Grafana、OTel、Filebeat 均 Ready，最终 API/Worker restart 为 `0` |
| Final images | `PASS` | local API `sha256:b3d88eb36fb9887ed67c5cea8e28defaf56f058035fc54a1d0f9875b08831e9f`；Worker `sha256:3f7dc567d4e05c9e8b3cd5dad4ebdb3a6df2e85f17ec1529e8e1dbf7495ef322`；Migrate `sha256:535a2f5fcdacaa998fd9f7465f5e0adf85284c827f3dbda8a9d5d30f31618107`；运行 Pod imageID 分别为 API `sha256:432f52804c2c8d1499a9f2eed33a989524501a7aaed4cf633343d1d37e9622da`、Worker `sha256:2dfab0804e41e2cae5e939dc5f748e412987a2e08df666986689fa8ac57070af` |
| Active configuration | `PASS` | revision ID `19e127ca-5a96-4f99-9e61-1dea0a2f81e2`，hash `2fdfebc8f45167b6974705b415b02da85fae36bf47ae79f5af5a2ecf99045595`；LLM disabled，真实模型诊断保持 `NOT RUN` |
| MySQL totals | `PASS` | `4` Consultations、`3` workspace runs（`1` Investigation + `2` Consultation turns）、`12` Evidence citations、`1` Knowledge Item、`1` Action Card、`1` Operation Plan、`0` Authorizations |
| Alert Investigation | `PASS` | Alert `d82cf76f-c923-4e90-a131-4f9faf528c15`；run `80f82dd6-144a-4acc-be91-0b362fea39be`；snapshot `8e07ac8e-cdf3-48ce-a50f-d4d8a5eab0ac`；最终 `completed / insufficient / high`，failure `MODEL_PROVIDER_DISABLED` |
| Logs Consultation | `PASS` | Consultation `d21c11a9-46ea-4e9e-aca8-fef5eae3eb84`；初始 snapshot `4bb31c2e-5271-4ee2-aaf6-7ba9a5756721` / hash `8e2952c75f8458c4b77df6ac9524f56d5f7543316c600f1e2dd0d45106227d5d`；run `8f2f31db-7d96-4edf-8e75-155b47e7be7b` |
| Explicit snapshot | `PASS` | 新 Logs query `2dbe4794-a235-4ee6-b77b-2bd73e7c605b` 后由 Owner 显式创建 snapshot `fdab9544-d2e7-4428-ac25-c3e9c7455eed` / hash `d94155df9c673edd8af1fe19fbe66a1f00f065493e617132b76401c50a35b642`；旧 snapshot/hash 保持不变 |
| Knowledge | `PASS` | item `13dd7e91-c367-4749-9576-d561f8406354` active；revision ID `7f150d2b-73bd-43fd-8af8-0dc4cd22394b`，revision `1`；后续 run `ed6cad43-5daf-4851-b245-d8ec358742fd` 只在 Guidance 中引用 exact revision |
| Durable SSE | `PASS` | 两个 Consultation run 各有 `12` 个持久 stream events；包含 `run.created`、tool progress、`answer.completed` 与 `run.completed` |
| Authority | `PASS` | Action Card `155c80aa-8c2e-4805-8ec0-24937c4abe6c` / hash `26b027e57b2e336a02108965a1f4cfe1d354382931bcd5736051a6780fb6268c`；Operation Plan `3dd07b74-d576-4574-aa32-f3036d6282af` / hash `b73cab3be2bdce188334169c0023b06f95ae0b3125d8f1d25eda6d41947d2b0d`；数据库状态均 `proposed`，authorization count `0` |

### MCP 与纵向联调证据

验收 URL：`http://127.0.0.1:18080/agent`。Chrome DevTools MCP、Playwright MCP 与 Kubernetes MCP 直接操作 revision `29` 的当前 runtime。当前工具面没有独立 Prometheus、Elasticsearch、Tempo、Alertmanager 或 LLM Provider MCP；下表中的 Provider 结果来自 CloudOps Worker 的真实 typed gateway 与实际本地 Provider，不冒充专用 MCP。

| 维度 | 结果 | 证据 |
|---|---|---|
| Alert Investigation | `PASS` | Alert-scoped run 无 Incident 前置，依次完成 `kubernetes.resources`、`metrics.query`、`logs.query`、`traces.search`；保存 Evidence `45ac4fe0-289d-49fd-8d23-fb139e7a0041`、`9b9f80ac-4c1d-47bc-8e15-341d64e74ee0`、`4089ce86-a56d-411a-b6af-6e3e392b6056`、`a5f41645-e506-419d-bc9b-1ab4a02fd1cd`；未检索 Knowledge/Runbook |
| Investigation history | `PASS` | revision `29` 的 API/UI 历史项显示 `alert · 4 Evidence`，不再错误显示 `0 Evidence`；列表 count 与详情 citations 由真实 MySQL integration assertion 锁定 |
| Logs Consultation | `PASS` | 两轮 message 均完成 4 个真实 bounded tools；后续 run 的 Current Evidence 为 `52835e8b-7926-4e13-8ef8-2addf383ba86`、`5d129755-5188-4ce4-9d05-f05791deae43`、`41a5a49f-260f-4761-88e3-37b2ce26ae98`、`435f8f65-5ad6-4146-be82-887495d52786` |
| Evidence vs Guidance | `PASS` | 后续 run 仅在 Guidance Citations 中引用 Knowledge revision `7f150d2b-73bd-43fd-8af8-0dc4cd22394b` 与 Runbook revisions `e8b9e0f69e597de0e74deaed6fe9e3208ff18193d47c0d442738a362f6a6a7a4`、`4c845617d58156d309fd649aac9525cc3279f94010f679d018341e5de2ceacc6`；Current Evidence 保持独立 Provider observation |
| Context Snapshot | `PASS` | 从 Logs 导航到 `/overview` 后初始 snapshot ID/hash 未变化；只有显式 attach 创建新 snapshot，旧 snapshot 仍可读取 |
| SSE | `PASS` | EventSource 精确绑定 `/api/v1/agent/consultations/d21c11a9-46ea-4e9e-aca8-fef5eae3eb84/events`，HTTP `200`、`content-type: text/event-stream`、`X-Accel-Buffering: no`；Chrome 与 Playwright 均确认同一 URL |
| Authority negative path | `PASS` | Action Card 与 Operation Plan 使用错误 hash 授权均返回 `409 AGENT_STATE_CONFLICT`；两者无 authorization、无 execution，未调用 Kubernetes/GitHub/Argo 或其他 mutation adapter |
| Workspace/global panel | `PASS` | 完整 Workspace 与全局 drawer 同时打开时 `duplicateIds=[]`；两个 composer 分别为 `agent-conversation-message` 与 `global-agent-conversation-message`，label/`aria-labelledby` 均可解析 |
| Responsive UI | `PASS` | desktop 与 `390x844`、`320x720` 实测；390px/320px 均 `clientWidth == scrollWidth`，全局 drawer 精确覆盖 viewport，无可见文字溢出 |
| Accessibility/console | `PASS` | Lighthouse snapshot Accessibility `100`、Best Practices `100`；Chrome 与 Playwright console 均为 `0 errors / 0 warnings`；最终 desktop/mobile screenshots 已保存为 MCP artifact |
| Kubernetes MCP | `PASS` | API/Worker Deployment 均 `1/1` available；MySQL、Prometheus、Elasticsearch、Tempo、Alertmanager 及采集组件 Pod 全部 Ready；最终 API/Worker restart 为 `0` |
| Task result | `DONE_WITH_NOT_RUN` | 本地真实 Alert/Logs -> Agent -> bounded tools -> Current Evidence -> outcome、snapshot、SSE、Knowledge exact revision、Guidance 与零授权 authority 主链完成；未运行项如下 |

### 检查结果

`PASS`：

- `make check`：Go unit/integration、race suite、`go vet`、golangci-lint（`0 issues`）、gofmt/goimports/dependency/structure/build。
- frontend ESLint（exit `0`、`0 errors`，保留仓库既有 warning baseline）、Vue typecheck、Vitest `17 files / 58 tests`、production build；SSR accessibility regression 同时渲染完整/compact Agent surface 并要求零重复 ID 与可解析 label。
- actionlint、ShellCheck、Helm strict lint/runtime contracts、kubeconform `38/38`、first-party naming guard。
- Agent Workspace repository/runner/authority、API/SSE、migration、Worker lifecycle、frontend API/router/a11y 的专项测试均包含在完整门禁。
- disposable MySQL 8 integration `TestMySQLWorkspaceRunnerCollectsEvidenceBeforeDisabledModelOutcome`：`PASS`；真实当前 MySQL schema/data 查询再次确认上述 run、event、Knowledge 与 authority 状态。
- 初次最终门禁发现 Worker readiness 测试对正式小写 `workspace` 错误合同使用大小写敏感的 `Workspace` 断言；修正测试后 focused `go test ./internal/bootstrap` 与完整 `make check` 均 `PASS`。
- `git diff --check`（状态文件更新后复跑）。

### 实际文件清单

- Domain/data：`internal/agent/workspace_*`、`migrations/00008_agent_workspace.sql`、`00009_agent_workspace_tasks.sql`、schema/migration/MySQL contract tests。
- Worker/Provider：`internal/bootstrap/worker*.go`、Worker lifecycle tests、typed Kubernetes/Prometheus/Telemetry gateway wiring、dynamic LLM ProviderAccess。
- API/contract：`internal/api/agent_handler*`、Alert/Telemetry handlers、router/dependency/startup wiring、`docs/api-v1-openapi.yaml`。
- Frontend：`frontend/src/api/agent*`、`stores/agentWorkspace.ts`、`components/agent/**`、`views/agent/AgentWorkspaceView.vue`、global panel、Logs/Traces context integration、router/a11y tests。
- Runtime：`scripts/local-lifecycle.sh` migration upgrade ordering。
- Task 6 implementation 为 `61` 个精确文件；本状态文件不计入 implementation 数。

### Delivery record

| 项目 | 结果 | 证据 |
|---|---|---|
| Branch/base | `PASS` | `codex/v3-refactor`；任务开始 HEAD `c53e518f4bcb834988897fe458546a63c8a0947d` |
| Worktree preservation | `PASS` | 保留并收敛两个续作会话中的 Task 6 未提交成果；未 reset、revert 或清理 |
| Local commit | `PASS` | Task 6 的 `61` 个 implementation 文件与本状态节纳入精确语义提交；最终 SHA 以提交完成后的 Git HEAD 为准 |
| Push | `PASS` | 按任务书预授权仅 fast-forward push 当前非默认实施分支 `origin/codex/v3-refactor`；未创建 PR/tag，未触碰默认分支；远端 SHA 与最终 Git HEAD 相同 |

### NOT RUN

- 真实 LLM/model diagnosis：当前 Configuration Revision 未启用 LLM endpoint/model/credential；真实 bounded tools 与 Evidence 收集为 `PASS`，模型诊断、模型 token streaming 和模型生成结论严格 `NOT RUN`，不得用 fixture 或 fallback 文案冒充。
- 独立 Prometheus、Elasticsearch、Tempo、Alertmanager、LLM Provider MCP：当前工具面未提供；真实 UI/API/Worker gateway/Provider 与 Kubernetes MCP 证据不冒充这些专用 MCP。
- Action Card/Operation Plan 正向授权与 mutation execution：本轮有意保持 authorization count `0`；未授权 Kubernetes、GitHub、Argo 或其他外部/高影响写，未执行产品 mutation。
- hosted/staging/production、外部 Provider、PR、tag、默认分支：`NOT RUN`。
