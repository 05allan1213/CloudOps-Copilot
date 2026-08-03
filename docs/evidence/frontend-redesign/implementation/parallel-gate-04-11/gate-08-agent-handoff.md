# Gate 8 Agent Lane Handoff

Recorded: 2026-07-31 (Asia/Shanghai)

## Status

```text
IMPLEMENTATION=COMPLETE
FOCUSED_SMOKE=PASS
FULL_VALIDATION=DEFERRED
REAL_READONLY_INTEGRATION=NOT RUN
AGENT_WRITE_E2E=NOT RUN
BACKEND_GAP=NONE
APPROVED_DEVIATION=NONE
```

Gate 8 is implemented on its isolated lane. The production `/agent` tree now
uses the Nuxt UI Agent workspace, preserves the real typed API/SSE contracts,
and does not introduce backend, API, database, Provider, dependency, Router, or
shared-token changes.

## Identity

| Item | Value |
| --- | --- |
| Repository | `/home/monody/k8s/CloudOps-Copilot-agent` |
| Branch | `frontend/g8-agent` |
| Base SHA | `136356a41504df5096b75353ccb18dd0aebcef76` |
| Final implementation SHA | `adccd4a7d66c2a7101373b8cdd4a0c7e7fad9f1e` |
| Implementation commit | `adccd4a7d66c2a7101373b8cdd4a0c7e7fad9f1e feat(frontend): migrate agent workspace gate 8` |
| Handoff commit | The local commit containing this file; resolve with `git log -1 --format=%H -- docs/evidence/frontend-redesign/implementation/parallel-gate-04-11/gate-08-agent-handoff.md` |
| Route | `/agent`; canonical selection Query is `consultation` or `investigation`, with legacy `run` accepted and replaced |
| Browser source | Fixture-backed read-only UI at `http://127.0.0.1:4183/agent`, proxying to `http://127.0.0.1:18083` |

No push, merge, PR, publication, real Agent mutation, or external write was
performed.

## Implementation

- Rebuilt History, Conversation, and Inspector as one continuous three-column
  Nuxt UI workspace. History and Inspector independently collapse to a 28px
  recoverable rail; the preference remains page-local and does not enter the
  URL.
- Preserved context-triggered, structured-new, and free-query entry modes in
  descending visual priority. Creation fails closed unless the supplied Scope,
  namespace, resource, UTC range, and real Query/Evidence reference satisfy the
  current backend contract.
- Restored Consultation, Investigation, and legacy `run` selection from the URL
  and canonicalized new selection state with `router.replace`.
- Added typed Consultation creation and `AbortSignal` support for Agent reads
  and mutations. Typed `ApiError` status, code, request ID, trace ID,
  idempotent-replay identity, and next steps remain visible.
- Reused the same `Idempotency-Key` for retries of identical message content;
  changing content produces a new key.
- Added explicit stream ownership generations, connecting/connected/
  reconnecting/disconnected/stopped states, a bounded 256-event cursor dedupe,
  delayed authoritative refresh, stale-request suppression, and complete
  teardown on selection change or unmount.
- Preserved long messages, Tool progress, Evidence, Guidance, Knowledge,
  Runbook, Action Card, and Operation Plan capabilities. Long message and large
  Evidence boundaries use virtualization while copy actions retain the full
  source value and accessible order.
- Exact authorization review uses Nuxt UI Modal and shows subject identity,
  exact hash, target, preconditions, Owner reason, and precise expiry UTC. The
  UI states explicitly that authorization is not execution, Delivery, or
  Verification.
- Fixed the final browser-smoke defect where the History header's implicit Grid
  column expanded beyond its 220px lane and made the collapse control
  unclickable. The constrained `minmax(0, 1fr)` column now keeps all three panes
  disjoint.

Covered decisions include D-20 through D-22, FR-SUP-002, FR-SUP-003,
FR-SUP-005, and FR-CX-002 through FR-CX-007 for the Gate 8 production tree.

## Files

| File | Result |
| --- | --- |
| `frontend/src/views/agent/AgentWorkspaceView.vue` | Three-column orchestration, entry modes, context strip, URL state, responsive collapse, teardown |
| `frontend/src/components/agent/AgentHistory.vue` | Typed history tabs, selection, compact/collapsed rails, bounded layout |
| `frontend/src/components/agent/AgentConversation.vue` | Message/Tool timeline, full copy, virtualization, cancel/send/Knowledge controls |
| `frontend/src/components/agent/AgentInspector.vue` | Snapshot/Evidence/Guidance/Knowledge/Runbook/authority presentation and exact authorization Modal |
| `frontend/src/stores/agentWorkspace.ts` | API cancellation, route selection, typed failures, SSE ownership/dedupe/reconnect, mutation lifecycle, idempotent retry |
| `frontend/src/api/agent.ts` | Typed Consultation create response, request cancellation, stream open/error callbacks |
| `frontend/src/utils/agentContext.ts` | Canonical/legacy route decoding, Evidence boundary, free-query context |
| `frontend/src/stores/agentWorkspace.test.ts` | SSE ownership/dedupe/teardown, idempotent retry, fail-closed creation |
| `frontend/src/utils/agentContext.test.ts` | Route selection and free-query context contracts |
| `frontend/src/components/agent/agentAccessibility.test.ts` | Nuxt UI controls, accessible names, Modal and collapse contract |

No shared-owner file is included in the implementation commit.

## Focused Validation

| Check | Result |
| --- | --- |
| `./node_modules/.bin/eslint src/api/agent.ts src/components/agent/AgentConversation.vue src/components/agent/AgentHistory.vue src/components/agent/AgentInspector.vue src/components/agent/agentAccessibility.test.ts src/stores/agentWorkspace.ts src/stores/agentWorkspace.test.ts src/utils/agentContext.ts src/utils/agentContext.test.ts src/views/agent/AgentWorkspaceView.vue --max-warnings 0` | `PASS`; zero warnings |
| `./node_modules/.bin/vitest run src/api/agent.test.ts src/utils/agentContext.test.ts src/stores/agentWorkspace.test.ts src/components/agent/agentAccessibility.test.ts --maxWorkers=1 --no-file-parallelism` | `PASS`; 4 files, 12 tests |
| Initial `npm run typecheck` | `FAIL`; three inline Modal callback parameters had implicit `any` types |
| Corrective and final `npm run typecheck` | `PASS`; typed handlers replaced the inline callbacks, final run exited zero |
| `git diff --check` and staged `git diff --cached --check` | `PASS` |
| Element Plus/native form/`window.confirm`/new raw-color scans over Gate 8 files | `PASS`; zero matches |

`npm run typecheck` regenerated `frontend/auto-imports.d.ts` and
`frontend/components.d.ts` while checking the lane. Both were restored to the
base because generated declarations are not owned by Gate 8; the typecheck
itself completed successfully before restoration.

## Browser Smoke

Fixture and application commands:

```bash
CLOUDOPS_E2E_FIXTURE_PORT=18083 CLOUDOPS_E2E_APP_ORIGIN=http://127.0.0.1:4183 node tests/e2e/fixture-server.mjs
VITE_API_PROXY_TARGET=http://127.0.0.1:18083 npm run dev -- --host 127.0.0.1 --port 4183
```

Playwright CLI opened `/agent`, resized Chromium to `1440x900`, confirmed the
document `light` theme, inspected the accessibility snapshot, collapsed and
restored History, measured the final pane geometry, and queried Console errors.

| Item | Result |
| --- | --- |
| Route entry and canonical selection | `PASS`; `/agent?consultation=00000032-0000-4000-8000-000000000001` |
| Main content | `PASS`; heading, context, History, Conversation, Tool progress, Inspector, Evidence, and Authority rendered |
| Core interaction | Initial `FAIL` because Conversation intercepted the overflowing History collapse button; corrected final smoke `PASS` for collapse and restore |
| Final geometry | `PASS`; History 220px, Conversation 660px, Inspector 340px; `scrollWidth=clientWidth=1440` |
| Theme and viewport | `PASS`; Chromium 1440x900 Light |
| Console | `PASS`; 0 errors and 0 warnings |
| Network/data classification | Fixture-backed read-only; GET/SSE only, no Agent write action triggered |

The fixture is presentation and contract smoke evidence only. It is not real
UI -> API -> MySQL/Provider integration evidence.

## Gate 12A Requests

- Keep `/agent` route meta under the existing Router owner and mark the route
  migrated when Gate 12A integrates this lane; Gate 8 did not modify
  `frontend/src/router/routes.ts`.
- Canonicalize Overview, Alert, and Incident Agent links to the compatible
  structured context and `consultation`/`investigation` selection contract
  after those lanes merge. Legacy `run` must remain accepted.
- Regenerate `frontend/components.d.ts` and `frontend/auto-imports.d.ts` once
  all lanes are merged.
- Recheck Global Agent versus full `/agent` stream ownership after the Gate 2
  shell and all page lanes are integrated; the Gate 8 store now provides the
  required teardown and generation boundaries.

## Deferred Validation

The following are deliberately `NOT RUN` under plan section 0.5.3:

- Dark theme; 1920x1080, 1280x800, 1024x768, zoom, and 200% text.
- Firefox, WebKit, the full keyboard/accessibility matrix, and Owner visual
  review.
- Performance, bundle, long-lived SSE soak, and real large-data measurements.
- Full lint, warning-budget suite, full unit, E2E typecheck, build, audit,
  stable E2E, and full E2E.
- Real frontend/backend/MySQL/Provider read-only integration.
- Agent message, Knowledge, plan, Action Card, authorization, execution, or any
  other real write-path E2E. No isolated target, restricted credential,
  initial identity/hash, cleanup proof, or separate Owner write authorization
  was supplied.

```text
IMPLEMENTATION=COMPLETE
FOCUSED_SMOKE=PASS
FULL_VALIDATION=DEFERRED
```
