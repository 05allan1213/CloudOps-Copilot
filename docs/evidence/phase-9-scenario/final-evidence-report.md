# Phase 9 Final Evidence Report

## 1. Final result

```text
RESULT=DONE_WITH_NOT_RUN
LOCAL_CORE_SCENARIO=PASS
DEFINITION_OF_DONE_FAIL=0
EXTERNAL_DELIVERY=HOSTED_PASS_WITH_ENV_NOT_RUN
LOCAL_PHASE9_BRANCH=codex/v3-refactor
LOCAL_PHASE9_HEAD=7af4144f7cdc4c03b8d3b88fdb751ae06b8686c1
LOCAL_PHASE9_WORKTREE=DIRTY_AT_CAPTURE
DELIVERY_PRS=https://github.com/05allan1213/CloudOps-Copilot/pull/1,https://github.com/05allan1213/CloudOps-Copilot/pull/2,https://github.com/05allan1213/CloudOps-Copilot/pull/3
VALIDATED_MAIN_SHA=44a2ede586f0a624da48a82de384c783fc348cbd
REQUIRED_CI_RUN=30353151266
HOSTED_SUPPLY_CHAIN_RUN=30353402682
FINAL_HELM_REVISION=53
FINAL_SCHEMA_VERSION=11
FINAL_SCENARIO_STATE=inactive
```

本报告的本地主体只声明 Phase 9 worktree 实际运行的能力；末尾外部交付表持续记录后续真实执行。远端 PR/human merge、merged-SHA Required CI、GHCR validation publish、SBOM、attestation、keyless signing、transparency verification 与 cleanup 已真实运行并 `PASS`。产品 GitHub App、Argo、staging、production、第二真实集群与 production DR 仍存在 `NOT RUN`，因此整项目结论仍不是无条件 `PASS`。

## 2. Authority and preflight audit

已读取并按当前权威执行：

- [CloudOps Implementation Taskbook - Task 9](../../CloudOps-Implementation-Taskbook.md#任务-9真实-scenario视觉质量与最终收敛)
- [CloudOps Implementation Spec - Phase 9](../../CloudOps-Implementation-Spec.md#phase-9真实-scenario视觉质量与最终收敛)
- [Final Definition of Done](../../CloudOps-Implementation-Spec.md#18-最终-definition-of-done)
- `CONTEXT.md`
- ADR 0001–0045；ADR 0045 定义旧 generation-labelled ADR 的历史边界，ADR 0041/0042 定义真实 Scenario 与 retention。

任务 0–8 的实际状态先审计为 `DONE_WITH_NOT_RUN`。下表只判断 Phase 9 本地硬依赖是否就绪，不改写历史外部 `NOT RUN`：

| Task | 本地依赖 | Phase 9 判断 | 继续保留的边界 |
|---|---|---|---|
| 0 | semantic V1、single runtime、MySQL、backup/restore | `PASS` | hosted/staging/production `NOT RUN` |
| 1 | Shell、Settings、Scope、secret、notification | `PASS` | 外部 identity/provider branch `NOT RUN` |
| 2 | Kubernetes reader、Infrastructure、Atlas、Context Link | `PASS` | 第二真实集群 `NOT RUN` |
| 3 | Prometheus catalog/query/Evidence | `PASS` | 外部 Prometheus environment `NOT RUN` |
| 4 | Elasticsearch/Tempo query/detail/Evidence | `PASS` | 外部 console/专用 MCP `NOT RUN` |
| 5 | Alertmanager ingress/lifecycle/notification | `PASS` | 外部 Alertmanager MCP `NOT RUN` |
| 6 | durable Agent、tools、Evidence、Knowledge、authority | `PASS` | hosted/frozen-dataset Agent Quality branch `NOT RUN` |
| 7 | Incident、current cycle、Verify、ResolutionReport | `PASS` | 外部 environment recovery `NOT RUN` |
| 8 | immutable Plan、exact Authorization、execution/verify、DevOps | `PASS` | GitHub/Argo/hosted delivery `NOT RUN` |

只有上述本地依赖完成后才启动跨 Phase Scenario。

## 3. Scenario identity and lifecycle

| Field | Evidence |
|---|---|
| Scenario ID | `scenario-20260728045922-4c81122b` |
| Cluster / context | `cloudops-local` / `kind-cloudops-local` |
| CloudOps Namespace / release | `cloudops-system` / `cloudops` |
| Scenario Namespace | `demo` |
| Active runtime | healthy Deployment `1/1`、traffic Deployment `1/1`、fault Deployment degraded then recovered `0/0`；共 6 owned resources |
| Identity | Kubernetes labels、shell badge、Scope、Context Snapshot、Alert、Agent 与 Plan 使用同一 Scenario ID |
| Active status | Metrics `PASS`、Alert `PASS_RESOLVED`、Logs `PASS`、Traces `PASS`、Agent `PASS`、history `4` |
| Active worktree deployment | Helm revision 52；API/Worker Ready、restart `0` |
| Final cleanup | Helm revision 53；runtime `0`、write gate `false`、stale firing `0`、Worker scale RBAC `no` |
| Retention | `scenario-down` 前后 history `4 -> 4`；MySQL 只读复核为 `4` |

`make local-up` 在 active Scenario 中保持原 identity 和 recovered state，没有静默创建第二条 Scenario。Codex PTY 回收过受管 background port-forward；`make local-status` 正确报告 `loopback=stopped`，随后对同一 canonical Service 建立前台 `127.0.0.1:18080` 转发完成浏览器检查。Helm/Pod/Provider readiness 未失败。

## 4. Observe -> Detect -> Investigate -> Decide -> Act -> Verify

| Stage | Result | Durable identity / evidence |
|---|---|---|
| Observe | `PASS` | Atlas 显示 Scenario workload/fault、真实 Kubernetes topology 与 degraded state |
| Detect | `PASS` | Alert `077751b1-d5f9-4576-9687-95c8d732fd99` 从 firing 到 resolved |
| Correlate | `PASS` | Logs query `ea25c0aa-089f-43e7-9ed8-f4526fe0acf2`；Trace `e43344bcb9ad1d5f6ad7a91af3edc16b`，带 fault resource、`namespace=demo` 和原始绝对时间窗 |
| Investigate | `PASS` | Investigation `af47fc82-dcd1-4b6a-a01f-3ffd9af65bd7`，4 个真实 Provider tools、4 条 Evidence、outcome `insufficient` |
| Decide | `PASS` | Plan `ba8eadf7-ffef-4bdf-b944-b2e106757ffa`，hash `1e40bef86a26da12122d70f72d14bd22f692fdb977cdf24b0b732dceb3968c2d`；Owner exact review/authorization |
| Act | `PASS` | Execution `fbebe225-fca5-49a4-8451-56fa9c26938d`，allowlisted Deployment scale；零授权路径无 effect |
| Verify | `PASS` | VerificationRun `10a6e87f-a678-4ae5-a97f-b36cced39119`，current Metrics/Alert/Kubernetes/Trace checks |
| Resolve | `PASS` | Incident `fee1939f-e291-4dac-b1c2-c52848b616a9`；ResolutionReport `3a4b63da-d444-4675-86d2-3592466b58e8` |

Trace detail execution 为 `7adfbfc3-559b-498b-97d6-a9513172a5ba`。一次缺少 Context 的旧 Trace URL 正确返回 `404 TELEMETRY_RESOURCE_NOT_FOUND`，它是 fail-closed 诊断，不纳入 clean flow。

## 5. Real LLM provenance

MySQL 只读查询没有输出 secret value：

| Field | Value |
|---|---|
| Configuration Revision | `12` |
| provider / actual model | `llm` / `deepseek-v4-flash` |
| status / outcome / uncertainty | `completed` / `insufficient` / `high` |
| model calls | `1 / 1` |
| token budget / input / output | `12000 / 710 / 233` |
| Prompt | `agent-workspace/v1` / `5a64393edee79dfb6b92f45d7875e0650c45ce3a652f0149a0eb3bf42e233ef8` |
| Tool Schema | `cloudops-bounded-tools/v1` / `2c5fae6a4728aadf18b59050f72110c316d746100110565791b52467df4ba9fa` |
| tool steps | Kubernetes、Metrics、Logs、Traces，4/4 completed |
| final answer | 166 characters，持久化在 `final_diagnosis.answer` |

Secret API 只返回 configured fingerprint；报告、console、network 和数据库输出均未包含原值。

## 6. Browser, visual and interaction evidence

### 6.1 Full flow and clean network

Revision 51 完整 flow 覆盖 Overview、Alert、Logs、Trace、Agent、Incident 与 DevOps：212 个 `/api/v1` request 全部 `200`，console errors `0`、page errors `0`、bad responses `0`。

- [clean console](browser/playwright-console-rev51-clean-errors.txt)
- [clean network](browser/playwright-network-rev51-clean.txt)
- [Alert resolved](screenshots/alert-resolved-rev51-1440x900.png)
- [Trace detail](screenshots/trace-detail-rev51-1440x900.png)
- [Agent Investigation](screenshots/agent-investigation-rev51-1440x900.png)
- [Incident closed](screenshots/incident-closed-rev51-1440x900.png)
- [DevOps succeeded](screenshots/devops-succeeded-rev51-1440x900.png)

最终表单 accessibility 修复部署到 Revision 52 后，独立短回归访问 8 个 route；在 `390x844` 下每个 route 的 document overflow 均为 `0`，225 个 API response 无 4xx/5xx，console/page error 均为 `0`。

- [Revision 52 console](browser/playwright-console-rev52-final-errors.txt)
- [Revision 52 network](browser/playwright-network-rev52-final.txt)

### 6.2 Viewport and scroll matrix

| Check | Result | Evidence |
|---|---|---|
| Desktop | `PASS` | `1440x900`；普通 route 无横向 overflow，Canvas/sidebar/detail panel 可见 |
| Tablet | `PASS` | `1024x768`、`768x1024`；内容与切换控件不被遮挡 |
| Mobile | `PASS` | `390x844`、`320x568`、landscape；bottom navigation 可达全部 Workspace |
| Zoom / themes | `PASS` | 200% zoom equivalent、light/dark、reduced-motion |
| Long content | `PASS` | long Chinese/English/resource/SHA/log/query 均在 bounded region 处理 |
| Logs | `PASS` | 200 rows、virtual scroll `790x518`、`scrollHeight=10800`、仅 16 rendered rows |
| Trace/Agent/DevOps | `PASS` | tree/evidence/identity/diff 使用局部 bounded scroll，不创建第二个主页面 scroller |

代表截图：

- [Revision 52 desktop Canvas](screenshots/overview-canvas-rev52-1440x900.png)
- [Revision 52 mobile Canvas](screenshots/overview-canvas-rev52-390x844.png)
- [320x568 Canvas](screenshots/overview-canvas-320x568.png)
- [landscape Canvas](screenshots/overview-canvas-844x390-landscape.png)
- [dark mode](screenshots/overview-dark-1440x900.png)
- [reduced motion](screenshots/overview-reduced-motion-1440x900.png)
- [200% zoom equivalent](screenshots/overview-zoom200-equivalent-720x450.png)
- [long Logs](screenshots/logs-long-query-rev51-1440x900.png)
- [long Agent](screenshots/agent-investigation-long-bounded-scroll-1440x900.png)
- [long DevOps identity](screenshots/devops-long-identity-bounded-scroll-1440x900.png)

### 6.3 Canvas pixel and WebGL

Revision 51 canvas pixel audit：CSS/drawing buffer `888x748`，采样色 `233`，非主背景像素比 `0.10912`，WebGL error `0`。Revision 52 再验：CSS/drawing buffer `888x748`、PNG data URL length `106258`、WebGL error `0`、document overflow `0`。Structured view 与 WebGL failure fallback 仍使用同一 topology source。

### 6.4 Accessibility and performance

| Audit | Desktop | Mobile |
|---|---:|---:|
| Accessibility | 100 | 100 |
| Best Practices | 100 | 100 |
| SEO | 91 | 91 |
| Agentic Browsing | 67 | 67 |

SEO/Agentic 的非满分项仅为 `robots.txt` / `llms.txt` 建议，不是 accessibility failure。Revision 52 的 390px 可见 button/link 中小于 `44x44` 的 touch target 数量为 `0`。

- [desktop Lighthouse JSON](browser/lighthouse-desktop-revision-51.json)
- [mobile Lighthouse JSON](browser/lighthouse-mobile-revision-51.json)
- [performance trace](browser/overview-performance-trace-revision-51.json.gz)

Performance trace：LCP `338ms`，CLS `0.00`。

## 7. Failure-state evidence

Chrome DevTools MCP 置为 Offline 后刷新 Infrastructure：

- 保留真实 40-node stale projection；
- 显示 `基础设施 API 不可用`、`Network Error`、`REQUEST_FAILED` 与“重试”；
- 网络恢复并重试后错误清除，40-node projection 恢复；
- 页面横向 overflow 为 `0`。

[mobile offline screenshot](screenshots/infrastructure-offline-390x844.png)

## 8. Cleanup and post-down proof

执行 `make scenario-down`：

```text
PASS Scenario fault ended desired=0 running_pods=0
PASS Alert resolution delivered and persisted
PASS runtime removed
retained_history_before=4
retained_history_after=4
scenario_state=inactive
scenario_write_gate=false
scenario_runtime_resources=0
scenario_stale_firing_alerts=0
```

Kubernetes MCP 对 label `cloudops.io/scenario-id=scenario-20260728045922-4c81122b` 返回空。CLI `auth can-i update deployments/scale` 以 Worker ServiceAccount 返回 `no`。API/Worker Revision 53 Pod 均 Ready、restart `0`。

Post-down browser：`Scenario Active=false`、`cloudops-scenario-*` runtime name=false、Live Mode=true、34 nodes、overflow `0`；21 个 API response 无 bad response，console/page error `0`。

- [post-down console](browser/playwright-console-after-scenario-down-errors.txt)
- [post-down network](browser/playwright-network-after-scenario-down.txt)
- [post-down Live Mode screenshot](screenshots/overview-live-mode-after-scenario-down-1440x900.png)

## 9. Removed temporary and inactive paths

`PASS`：

- `internal/agent/adapter/demo_model*`
- `internal/gitopscontract/**`
- `cmd/gitops-demo-contract/**`
- `deploy/contracts/gitops-demo/**`
- `deploy/platform/argocd/**`
- inactive `frontend/src/views/workspaces/WorkspaceStatusView.vue`
- old `server-monitor` runtime/path
- fixture/Golden claims that could be mistaken for current Provider evidence
- dead links and parallel deployment instructions

旧 ignored kubeconfig 移入 `.cloudops/retired/phase-9/server-monitor-docker-kubeconfig`，mode `0600`。该 private retired artifact 未进入 Git evidence。

## 10. Final local gates

| Command | Result | Detail |
|---|---|---|
| `make test-go` | `PASS` | all Go packages |
| `make test-race` | `PASS` | all Go packages under race detector |
| `make vet` | `PASS` | no finding |
| `make lint-go` | `PASS` | `0 issues` |
| `make build-go` | `PASS` | API/Worker/Migrate/Demo |
| `make test-frontend` | `PASS` | ESLint 0 error; typecheck; Vitest 19 files / 67 tests |
| `make build-frontend` | `PASS` | production Vite build |
| gofmt/goimports/deps/structure/naming | `PASS` | generation-free semantic naming |
| actionlint / ShellCheck | `PASS` | workflow and lifecycle scripts |
| Helm lint/template/contracts | `PASS` | strict lint; semantic runtime render |
| kubeconform | `PASS` | 38 valid / 0 invalid / 0 error |
| `git diff --check` | `PASS` | final worktree whitespace check |
| Markdown local links | `PASS` | current README/docs Markdown targets resolve |

Frontend ESLint reports existing formatting warnings but `0 errors`; Vitest logs unresolved `RouterLink` warnings in an isolated SSR accessibility test. Current real browser console is clean, so these are recorded as non-blocking test/lint warnings rather than hidden.

## 11. Definition of Done

### Product

| Item | Result | Evidence |
|---|---|---|
| Ten Workspaces navigable, deep-linkable, Back/Forward, mobile-reachable | `PASS` | desktop/mobile route and navigation matrix |
| Chinese-first, Lucide-only, no emoji or Incident-only branding | `PASS` | current shell/routes and naming review |
| Operational Context and Context Links preserve resource/time | `PASS` | Alert -> Logs/Trace/Agent/Incident exact links |
| Notification Inbox and global Agent panel work | `PASS` | full browser flow and clean network |

### Cloud Native

| Item | Result | Evidence |
|---|---|---|
| Active cluster topology, Metrics, Alerts, Logs, Traces have native API/UI | `PASS` | kind + Prometheus + Alertmanager + Elasticsearch + Tempo |
| Partial/stale/unavailable explicit, no fake data | `PASS` | Offline failure state and provider contracts |
| Atlas/structured view same source; WebGL fallback nonblank | `PASS` | canvas pixel/WebGL and structured projection |
| Scenario crosses Kubernetes and all first-wave Evidence sources | `PASS` | active Scenario status and full flow |

### Agent

| Item | Result | Evidence |
|---|---|---|
| Investigation and Consultation durable, cancellable, traceable | `PASS` | Task 6 current MySQL/API/UI plus Phase 9 Investigation |
| Conclusions cite Evidence/source/query/config/knowledge revision | `PASS` | 4 provider Evidence, immutable context/provenance |
| Cross-session Knowledge activates only after Owner confirmation | `PASS` | Task 6 Knowledge authority contract |
| Three-layer authority and immutable Plan; zero auth zero write | `PASS` | exact Plan/Authorization plus negative gates |

### Data and Configuration

| Item | Result | Evidence |
|---|---|---|
| Settings changes Operational Configuration; secret never echoed | `PASS` | Revision 12 and fingerprint-only secret response |
| local-down retention; backup/restore/reset contract | `PASS` | Task 0 current retained data/lifecycle evidence |
| Raw telemetry retention and capacity bounds effective | `PASS` | Settings bounds, Provider limits, virtual list |
| Old local data enters semantic V1 losslessly; FK/hash/provenance audit | `PASS` | Task 0 transformation and current schema evidence |

### Code and Runtime

| Item | Result | Evidence |
|---|---|---|
| First-party code/data has no product V2/V3 or numbered phase identity | `PASS` | `make check-naming` |
| Only `/api/v1`; no generation compatibility route/runtime/schema | `PASS` | OpenAPI/router/structure checks |
| API/Worker/Migrate ownership clear; MySQL sole durable runtime truth | `PASS` | architecture, Chart and integration tests |
| Compose/raw manifests/parallel Chart/old service removed | `PASS` | final source cleanup |
| Top-level `make local-*` is sole public startup | `PASS` | Makefile/lifecycle/docs convergence |

### Frontend Quality

| Item | Result | Evidence |
|---|---|---|
| Ordinary routes use document main scroll; severe scroll bug absent | `PASS` | route/long-content matrix |
| 320–1440 no critical overlap or page horizontal overflow | `PASS` | specified viewport screenshots and measurements |
| Keyboard/focus/form/aria-live/dialog restore/44px touch target | `PASS` | web-design-guidelines review, Lighthouse and browser checks |
| Atlas/bundle/Core Web Vitals/accessibility diagnostics recorded honestly | `PASS` | Canvas, Vite, Lighthouse and performance artifacts |

### Validation

| Item | Result | Evidence |
|---|---|---|
| Each completed major function has real UI -> API -> Provider MCP evidence | `PASS` | Playwright, Chrome DevTools and Kubernetes MCP runs |
| Phase 9 core Scenario completes Observe-to-Verify | `PASS` | durable IDs and provider verification above |
| External unrun items remain `NOT RUN`; no fixture/old evidence masquerades | `PASS` | explicit list below |

DoD summary：`PASS=28`、`FAIL=0`、`NOT RUN=0` for the 28 local-product DoD statements. This does not promote external optional branches to PASS; their run status follows.

## 12. External delivery status

| External item | Result | Reason |
|---|---|---|
| Product GitHub App runtime read/write | `NOT RUN` | no real App runtime credential; human `gh` identity is not product-adapter evidence |
| Remote branch/commit/PR | `PASS` | implementation PR [#1](https://github.com/05allan1213/CloudOps-Copilot/pull/1) and cleanup hardening PRs [#2](https://github.com/05allan1213/CloudOps-Copilot/pull/2), [#3](https://github.com/05allan1213/CloudOps-Copilot/pull/3) were created from remote branches and passed their PR checks |
| Human merge | `PASS` | repository owner merged PRs #1-#3; final validated implementation merge is `44a2ede586f0a624da48a82de384c783fc348cbd` |
| Required CI exact merged head | `PASS` | run [30353151266](https://github.com/05allan1213/CloudOps-Copilot/actions/runs/30353151266) covered exact SHA `44a2ede586f0a624da48a82de384c783fc348cbd`; all eight jobs including aggregate `Required CI` passed |
| Argo Application/exact merged revision/sync/rollout | `NOT RUN` | provider not configured/run |
| GHCR validation publish, SBOM, signing, attestation, cleanup | `PASS` | exact merged-SHA run [30353402682](https://github.com/05allan1213/CloudOps-Copilot/actions/runs/30353402682) passed for api/worker/migrate/demo; all temporary packages and referrers were removed |
| GitHub-hosted PR CI validation | `PASS` | PR #1-#3 checks passed before each human merge; no local result substitutes for hosted CI |
| Hosted supply-chain validation | `PASS` | workflow_dispatch on `refs/heads/main` used exact SHA `44a2ede586f0a624da48a82de384c783fc348cbd`; full evidence matrix and artifact names are recorded in [Hosted Supply-Chain Validation Report](hosted-supply-chain-validation-report.md) |
| Release tag / retained release publication | `NOT RUN` | validation packages were intentionally ephemeral; no tag, GitHub Release, retained production image, or deployment was created |
| Staging | `NOT RUN` | no staging scope/credential/authorization |
| Production | `NOT RUN` | outside supported local product and not authorized |
| Second real cluster / multi-cluster / multi-tenant | `NOT RUN` | not configured; outside current local scope |
| Production backup/DR | `NOT RUN` | local backup/restore is not production DR evidence |

因此最终结果保持 `DONE_WITH_NOT_RUN`。
