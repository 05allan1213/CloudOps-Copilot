# Gate 3 Shared Workspace, URL, Inspector, Error, and Async Report

Recorded: 2026-07-31 (Asia/Shanghai)

## Status

```text
GATE_03=PASS
SHARED_WORKSPACE_FOUNDATION=PASS
URL_INSPECTOR_CONTRACT=PASS
ERROR_AND_ASYNC_PRESENTATION=PASS
GATE_04_ENTRY_CONDITION=PASS
CURRENT_GATE=GATE_04_READY_NOT_STARTED
B1=PASS
B2=NOT RUN
B3=PASS
B4=PASS
B5=PASS
B6=NOT RUN
B7=NOT RUN
B8=NOT RUN
REAL_READONLY_INTEGRATION=NOT RUN
WRITE_PATH_E2E=NOT RUN
BACKEND_GAP=NONE
APPROVED_DEVIATION=NONE
REVIEW_CONCLUSION=COMPLIANT
```

Gate 3 is complete. Its three exit conditions pass, so the Gate 4 entry
condition is satisfied. Gate 4 routes, pages, Atlas code, and APIs were not
implemented or changed.

## Identity and Scope

| Item | Evidence |
| --- | --- |
| Repository | `/home/monody/k8s/CloudOps-Copilot` |
| Branch | `main` |
| Gate 3 rollback point | `470c6aece2de69893cafae6200ac672a7c071a73` (Gate 2) |
| Gate final commit | The local Gate 3 commit containing this report; resolve with `git log -1 --format=%H -- docs/evidence/frontend-redesign/implementation/gate-03-workspace/report.md` |
| Owner authorization | `FRONTEND_REFACTOR_PLAN_APPROVED=YES`, `LOCAL_GATE_COMMITS_AUTHORIZED=YES` |
| Browser source | Deterministic presentation fixture at `http://127.0.0.1:52732/workspace` |
| Real read-only integration | `NOT RUN`; the fixture makes no CloudOps API or Provider request |
| Real or isolated write integration | `NOT RUN`; no isolated target, credential, cleanup proof, or separate write authorization |
| Backend/API/database/Provider/Kubernetes changes | None |

The implementation is additive and is not consumed by a production Workspace
route yet. This preserves the Gate 2 rollback boundary and leaves all legacy
routes operational until their assigned migration Gate.

## Implementation Result

- Added reusable Nuxt UI business compositions for Workspace headers,
  one-line or tabbed toolbars, dense virtual tables, Inspector, typed errors,
  loading/empty/partial/stale/disconnected/target states, realtime trust, and
  consequence-specific confirmations.
- Added a URL codec that canonicalizes legacy aliases, rejects invalid values,
  retains unrelated stable context, and removes transient UI keys. Inspector
  selection uses push for first open, replace for rapid switching, Back for
  close, and push for full-page work.
- Inspector entry focuses its heading. Close restores the trigger, table scroll,
  and document scroll. Dirty state prevents silent dismissal; Escape affects
  only the topmost dismissible surface.
- The table keeps critical columns pinned, stores optional-column preferences
  only in LocalStorage, uses neutral rows with a 3px severity marker, exposes
  full-value copy, and virtualizes the 20,000-row boundary.
- `ApiError` presentation retains code, request ID, trace ID, replay metadata,
  and next steps. Existing request helpers and cancellation semantics remain the
  only client path; no second client was introduced.
- `useLatestAsync` cancels superseded requests, suppresses stale results, keeps
  loaded content during background refresh, and cleans up with its Vue scope.
  `useRealtimeCleanup` provides shared resource teardown without replacing any
  domain-specific SSE state machine.
- `WorkspaceOperationProgress` presents current stage, tabular elapsed time,
  and cancellation only when the owning domain declares cancellation support.
  The fixture also binds async state to Nuxt UI button loading and proves that
  a failed submit retains its exact input.
- Realtime presentation distinguishes connecting, live, reconnecting,
  disconnected, stale, cursor expiry, resyncing, resync failure, and stopped.
  Only the trustworthy `live` state makes a Live claim. New rows remain
  user-controlled and existing-row updates do not reposition the reader.
- Acknowledgement, reversible configuration, Approval exact hash, rollback,
  and forced termination use distinct labels, required facts, consequences,
  authority, recovery, and dismissal strength.
- Context links now reject normalized path escape, unsafe query keys, control
  characters, and oversized query payloads while preserving existing typed
  route ownership.

## Files

| Area | Files and purpose |
| --- | --- |
| Workspace compositions | `frontend/src/components/workspace/` |
| URL and Inspector | `frontend/src/composables/useWorkspaceQuery.ts`, `useWorkspaceInspector.ts`, and tests |
| Async and SSE cleanup | `frontend/src/composables/useLatestAsync.ts`, `useRealtimeCleanup.ts`, and tests |
| Error identity | `frontend/src/api/client.ts` and focused test |
| Router context | `frontend/src/router/scrollBehavior.ts` and focused test |
| Safe handoff | `frontend/src/utils/contextLink.ts` and focused test |
| Canonical component tokens | `frontend/src/styles/tokens.css` |
| Browser fixture | `frontend/tests/fixtures/workspace-foundation/` |
| Generated declarations | `frontend/components.d.ts` |
| Evidence | `docs/evidence/frontend-redesign/implementation/gate-03-workspace/` |

No Gate 4 file from the plan's Gate 4 file scope is part of this change.

## Contract Coverage

| Contract | Gate 3 result |
| --- | --- |
| D-05, D-18 | `PASS`: tabbed and un-tabbed toolbar structure is fixed; primary CTA is right-aligned and dangerous actions remain outside the main toolbar |
| D-06 to D-11 | `PASS`: 460/520px Inspector targets, compact hierarchy, 48px table target, continuous canvas, restrained overlay elevation, severity marker, state presentation, mono identifiers, and UTC display are implemented and browser checked |
| D-19, FR-SUP-003 | `PASS`: filters, sort, page, tab, and selection are URL-owned; first selection pushes, rapid selection replaces, and Back/Forward preserve context |
| D-33, FR-SUP-005 | `PASS`: new rows are user-controlled, existing rows update in place, and continuity failures stop the Live claim |
| FR-SUP-004 | `PASS`: quick Inspector and pushed full-workspace navigation are separate, repeatable paths |
| FR-SUP-007 | `PASS`: five consequence classes have different facts and commands |
| FR-SUP-009 | `PASS`: critical columns stay pinned; optional columns are page-local and do not enter the URL |
| FR-CX-001 | `PASS`: focus entry/restore, topmost Escape, dirty guard, scroll context, and invalid/deleted/denied/expired targets were exercised |
| FR-CX-002 | `PASS`: URL and local transient state are separated and legacy selected/sort aliases canonicalize safely |
| FR-CX-003 | `PASS`: exact UTC and complete identifier access are present in the fixture contract |
| FR-CX-004 | `PASS`: first-load Skeleton, background refresh retention, cancellation, and failure-context retention are represented by shared primitives and focused tests |
| FR-CX-005 | `PASS`: shared presentation categories do not replace domain states; permission, expiry, invalid, deletion, partial, stale, and disconnect remain distinct |
| FR-CX-006 | `PASS` for the Gate 3 foundation: virtualization, latest-request cancellation, long-value display, and complete copy are available; route-specific Logs/Trace/Timeline semantics remain assigned to Gate 6/9 |
| FR-CX-007 | `PASS`: keyboard row activation, accessible controls, focus, non-color status, reduced motion, safe links, long text, and Light/Dark were checked |
| FR-CX-008 | `PASS`: the fixture imports production compositions while production imports no fixture; no route is falsely marked migrated |

## Focused Validation

| Check | Result |
| --- | --- |
| `npm run typecheck` | `PASS` |
| Nine focused Vitest files | `PASS`, 9/9 files and 28/28 tests |
| Targeted ESLint with `--max-warnings 0` | `PASS`, zero findings |
| Shared Workspace Element Plus scan | `PASS`, zero matches |
| Production fixture-import scan | `PASS`, zero matches |
| Non-Lucide quoted icon scan | `PASS`, zero matches |
| Emoji scan | `PASS`, zero matches |
| `:deep()` / `:global()` / `!important` scan | `PASS`, zero matches |
| `git diff --check` | `PASS` |

The first targeted ESLint diagnostic exposed 140 warnings in new files. No
budget was increased and no rule was disabled: the files were formatted, six
optional prop defaults were made explicit, and the identical final check passed
with zero findings. A pre-final typecheck rejected `AggregateError` under the
current lib target; the cleanup now attempts every disposer and rethrows the
first failure without changing TypeScript configuration. Two exploratory UI scan invocations had harness quoting or
over-broad-pattern failures; the corrected exact scans above passed and are the
Gate result.

Full lint, full warning-budget execution, full unit, E2E typecheck, build,
dependency audit, stable E2E, full E2E, Lighthouse, and the full browser matrix
remain `NOT RUN` under the approved Gate cadence. See
`commands/validation.txt` for exact commands and deferrals.

## Browser Evidence

| Code | Status | Evidence |
| --- | --- | --- |
| B1 | `PASS` | Chromium 1440x900 Light/Dark; shared states, async lifecycle, realtime truth, risk classes, Console, and Network |
| B2 | `NOT RUN` | 1920 is not required for this focused Gate 3 exit; primary-wide coverage remains at Gate 12 |
| B3 | `PASS` | Chromium 1280x800 table and 1024x768 Inspector; no page-level horizontal overflow |
| B4 | `PASS` | 125%/150% browser zoom, 200% text, long resource/hash/error content, independently scrolling Modal and Inspector |
| B5 | `PASS` | Keyboard table activation, Inspector focus entry/restore, dirty guard, topmost Escape, Back/Forward, Skip Link, and reduced motion |
| B6 | `NOT RUN` | Firefox and WebKit remain deferred to route Gates and Gate 12 |
| B7 | `NOT RUN` | The fixture makes no real UI -> API -> Provider request |
| B8 | `NOT RUN` | No isolated write target, credential, cleanup proof, or separate authorization |

The 20,000-row fixture rendered 24-37 body rows depending on viewport, always
below the 100-row boundary. Measured row height was approximately 49.9px. At
1024x768 and 200% text, the risk Modal body scrolled independently, its footer
remained visible, its `z-index: 101` remained above the Shell Header at 40, and
page/Modal horizontal overflow was zero. The long-title Inspector kept its
heading inside the panel, its body scrollable, its footer visible, all four
fact label/value rectangles disjoint, and page/panel horizontal overflow zero.

Final Console inspection reported 0 errors and 0 warnings. Network inspection
showed only successful Vite/static resources and successful Iconify Lucide
requests; there was no CloudOps API, SSE, Provider, or write traffic. Detailed
actions, measurements, and screenshot mapping are in `browser/results.md`.

## Limitations and Deviations

- `REAL_READONLY_INTEGRATION=NOT RUN`: presentation fixtures are not Provider
  evidence. Gate 4 owns its first required real read-only topology chain.
- `WRITE_PATH_E2E=NOT RUN`: no real confirmation action was submitted and no
  fixture interaction is counted as persistence or Provider proof.
- `BACKEND_GAP=NONE`: no backend contract was required or found missing in this
  additive foundation Gate.
- `APPROVED_DEVIATION=NONE`: no plan, API, type, warning, or dependency
  deviation was used.
- Runtime Iconify requests remain a Gate 12 CSP/offline-delivery observation.

## Rollback and Exit

Rollback point: `470c6aece2de69893cafae6200ac672a7c071a73` (Gate 2). The
authorized local Gate 3 commit is an independent rollback checkpoint. No
rollback operation was performed.

```text
SHARED_WORKSPACE_FOUNDATION=PASS
URL_INSPECTOR_CONTRACT=PASS
ERROR_AND_ASYNC_PRESENTATION=PASS
GATE_03=PASS
GATE_04_ENTRY_CONDITION=PASS
CURRENT_GATE=GATE_04_READY_NOT_STARTED
```
