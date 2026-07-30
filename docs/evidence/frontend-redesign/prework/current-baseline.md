# CloudOps Frontend Current Baseline

Recorded: 2026-07-30 (Asia/Shanghai)

## Result

```text
CURRENT_BASELINE=PASS
BASELINE_SHA=c8e709fd10ea47976b262dea22440e5496385c1e
BRANCH=main...origin/main
WRITE_PATH_E2E=NOT RUN
```

This report describes the current production frontend and the local runtime used by the prework. It does not claim that the isolated prototype has been migrated into production.

## Provenance

| Item | Current fact |
| --- | --- |
| Repository | `/home/monody/k8s/CloudOps-Copilot` |
| Initial worktree | Clean at the start of rollout `019fb1de-abdf-7b10-a071-9f785186d1f9` |
| Current worktree | Dirty by design: authorized baseline fixes, test harness changes, isolated prototype, screenshots and reports; no commit or push |
| Production frontend | `frontend/`, Vue 3.5, Vue Router 4.6, Pinia 3.0, Element Plus 2.14, Lucide, Three.js 0.185 |
| Local toolchain | Node `v24.13.0`, npm `11.6.2`, Playwright `1.61.1`; CI remains pinned to Node 20 |
| Runtime | kind context `kind-cloudops-local`, namespace `cloudops-system`, Helm release `cloudops` revision 57, status `deployed` |
| Real application | `http://127.0.0.1:18080` through a read-only `kubectl port-forward` |
| Real data source | CloudOps API -> Kubernetes typed Provider -> `kubernetes://cloudops-local` |
| Mutations during real-page evidence | 0 |

The local stack was rebuilt from the current application worktree before the real-page capture and reached Helm revision 57. No Go backend, API, database, Provider or Kubernetes semantic source change was made by this frontend prework.

## Frontend Inventory

| Boundary | Current owner |
| --- | --- |
| App bootstrap and global styles | `frontend/src/main.ts`, `App.vue`, `style.css`, `styles/*.scss` |
| Public routing | `frontend/src/router/routes.ts`, `router/index.ts`, `scrollBehavior.ts` |
| Grouped navigation | `frontend/src/navigation.ts`, `components/layout/*` |
| Global operational scope | `AppLayout.vue`, `api/platform.ts`, `utils/operationalScope.ts` |
| Notification list and SSE | `AppLayout.vue`, `NotificationInbox.vue`, `api/notifications.ts` |
| Global Agent drawer and Agent workspace | `components/agent/*`, `stores/agentWorkspace.ts`, `api/agent.ts` |
| Incident list/detail and realtime | `views/incidents/*`, `composables/incidents/*`, `api/incidents.ts` |
| Other Workspaces | `views/{overview,infrastructure,monitoring,alerts,logs,traces,devops,settings}` |
| Typed API and error identity | `api/client.ts`; `ApiError` carries status, code, request ID, trace ID, replay and next steps |
| Unit and browser contracts | `src/**/*.test.ts`, `tests/e2e/*`, `playwright.config.ts`, `tsconfig.e2e.json` |
| CI gates | `frontend/package.json`, root `Makefile`, `.github/workflows/ci.yaml` |

## Public Routes

The live router exposes `/overview`, `/infrastructure`, `/monitoring`, `/alerts`, `/alerts/:alertId`, `/logs`, `/traces`, `/agent`, `/incidents`, `/incidents/:incidentId`, `/devops`, `/settings`, `/` -> `/overview`, and the catch-all 404 route. The grouped desktop navigation exposes ten Workspaces. The current code also mounts `MobileBottomNav`; that is a current capability, but the approved target is desktop-only and does not retain phone-product ownership.

## Pre-Fix Baseline

| Check or defect | Initial result | Classification |
| --- | --- | --- |
| `npm run lint` | PASS, 0 errors / 2608 warnings | CURRENT historical debt |
| App typecheck | PASS | CURRENT |
| E2E typecheck | FAIL, script absent | FIXED |
| Unit tests | PASS, 19 files / 67 tests; two RouterLink harness warnings | CURRENT |
| Build | PASS; entry 152.14 KiB gzip, Three.js 189.46 KiB gzip | CURRENT |
| Official-registry audit | PASS, 0 vulnerabilities | CURRENT |
| Existing E2E | FAIL, 1 passed / 1 failed / 17 not run; stale `#incident-content` selector | FIXED |
| Monitoring save dialog | Raw unregistered `<el-dialog>` rendered inline | FIXED |
| Closed Global Agent | Eager index reads and Consultation SSE while closed | FIXED |
| Settings at 768/769 | Summary/action clipping | FIXED |
| Monitoring tab ARIA | Invalid/repeated semantics | FIXED |
| Provider external URLs | Inconsistent protocol validation | FIXED |
| Atlas framebuffer | `preserveDrawingBuffer: true` | FIXED |
| Phone Bottom Navigation | Mounted in current Shell | DRIFTED from desktop-only target |
| Real write paths | Isolation, identity and cleanup conditions absent | NOT RUN |

## Current Validation Snapshot

| Gate | Result |
| --- | --- |
| Lint | PASS, 0 errors / 2564 warnings |
| No-new-warning budget | PASS, 2564 <= 2608 |
| App typecheck | PASS |
| E2E typecheck | PASS |
| Unit | PASS, 19 files / 67 tests |
| Build | PASS; entry 153.84 KiB gzip, Three.js lazy chunk 189.46 KiB gzip |
| `npm audit --registry=https://registry.npmjs.org` | PASS, 0 vulnerabilities |
| Stable Chromium read-only E2E | PASS, 2/2 |
| Full Chromium current-capability E2E | PASS, 11/11 during the same prework pass |
| Firefox critical Incident read path | PASS, 1/1 |
| WebKit | NOT RUN, required GTK/GStreamer/WebKit host libraries are missing |

Two Vue `RouterLink` resolution warnings remain confined to the shallow `agentAccessibility.test.ts` harness. The browser gates have zero application Console errors, page errors and unexpected failed responses.

## Real Read-Only Runtime

The captured evidence at `output/playwright/production-real/overview-real-readonly.json` records HTTP 200 for `GET /api/v1/overview`, request ID `dkbu2sf25lj5-2t`, trace ID `64e625e7f721eff165904a0143f006d0`, Provider `kubernetes://cloudops-local`, Kubernetes `v1.36.1`, 35 nodes, 85 edges and zero mutations. The associated Canvas and structured-selection screenshots are immutable point-in-time evidence.

A final live liveness check at `2026-07-30T11:20:39Z` returned the same product/scope/Provider contract with 34 nodes and 83 edges. The count change is expected live-cluster drift and is not rewritten into the earlier screenshot metadata.

```text
REAL_READONLY_INTEGRATION=PASS
REAL_WRITE_PATHS=NOT RUN
```

## Evidence

- `screenshots/baseline-before/`: reproduced defects before repair.
- `screenshots/baseline-after/`: Monitoring dialog, Settings breakpoints and real Atlas after repair.
- `output/playwright/production-real/`: current real UI -> API -> Kubernetes Provider proof.
- `frontend/tests/e2e/baseline-readonly.spec.ts`: stable mutation-free regression gate.
