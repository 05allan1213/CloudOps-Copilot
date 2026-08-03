# Gate 2 App Shell, Navigation, Theme, and Global Overlay Report

Recorded: 2026-07-31 (Asia/Shanghai)

## Status

```text
GATE_02=PASS
APP_SHELL_MIGRATION=PASS
MOBILE_NAV_RETIREMENT=PASS
GLOBAL_OVERLAY_AND_SSE_CONTRACTS=PASS
OWNER_VISUAL_GATE_SHELL=PASS
GATE_03_ENTRY=READY
B7=NOT RUN
B8=NOT RUN
REAL_READONLY_INTEGRATION=NOT RUN
WRITE_PATH_E2E=NOT RUN
REVIEW_CONCLUSION=COMPLIANT
```

The implementation, focused technical checks, and Owner visual review are complete. The Owner returned the exact verdict `OWNER_VISUAL_GATE_SHELL=PASS` on 2026-07-31, so Gate 2 may exit and Gate 3 may begin after the local rollback commit is created.

## Identity and Scope

| Item | Evidence |
| --- | --- |
| Repository | `/home/monody/k8s/CloudOps-Copilot` |
| Branch | `main` |
| Gate base and current `HEAD` | `b27af59ebc38f08cd0037ffa926685e78fbaa19e` |
| Gate final commit | The local Gate 2 commit containing this report; resolve with `git log -1 --format=%H -- docs/evidence/frontend-redesign/implementation/gate-02-shell/report.md` |
| Worktree identity | The Gate 2 files listed below plus this evidence directory; no pre-existing change was reset, restored, cleaned, or overwritten |
| Owner authorization | `FRONTEND_REFACTOR_PLAN_APPROVED=YES`, `LOCAL_GATE_COMMITS_AUTHORIZED=YES` |
| Browser data source | Deterministic fixture at `127.0.0.1:18082` through the Vite proxy |
| Real read-only integration | `NOT RUN`; fixture traffic is not Provider proof |
| Real or isolated write integration | `NOT RUN`; no isolated target, credential, cleanup proof, or separate write authorization |
| Backend/API/database/Provider/Kubernetes changes | None |

Gate 1 is the rollback point. No dependency or lockfile changed in Gate 2, and no full lint, full unit, build, full E2E, Firefox/WebKit, real integration, or write-path suite was run early.

Owner visual verdict, recorded verbatim:

```text
OWNER_VISUAL_GATE_SHELL=PASS
```

## Files

| Area | Files and purpose |
| --- | --- |
| Shell | `frontend/src/components/layout/AppLayout.vue`, `AppHeader.vue`, `AppSidebar.vue`, `SidebarMenu.vue` |
| Global overlays | `frontend/src/components/layout/NotificationInbox.vue`, `frontend/src/components/agent/GlobalAgentPanel.vue` |
| Mobile retirement | deleted `frontend/src/components/layout/MobileBottomNav.vue` |
| Navigation and route ownership | `frontend/src/navigation.ts`, `frontend/src/router/routes.ts`, and focused tests |
| Theme and startup | `frontend/src/composables/useTheme.ts`, `frontend/src/main.ts`, generated `frontend/components.d.ts` |
| Router behavior | `frontend/src/router/scrollBehavior.ts` and its focused test |
| Lifecycle contracts | `frontend/src/utils/agentContext.ts`, `frontend/src/api/notifications.test.ts`, and focused tests |
| Fixture browser support | `frontend/tests/e2e/fixture-server.mjs`, `frontend/tests/e2e/support.ts` |
| Evidence | `docs/evidence/frontend-redesign/implementation/gate-02-shell/` |

The fixture additions provide read-only Settings/storage projections and deterministic Notification/Agent browser states. They do not change production API semantics and are not real integration evidence.

## Route Ownership

The Shell outside `RouterView` is `MIGRATED_NUXT_UI`. The bounded transition remains explicit at the route boundary:

| Route tree | Gate 2 ownership |
| --- | --- |
| `/overview`, `/infrastructure`, `/monitoring` | `LEGACY_ELEMENT_PLUS` |
| `/alerts`, `/alerts/:alertId`, `/logs`, `/traces` | `LEGACY_ELEMENT_PLUS` |
| `/agent`, `/incidents`, `/incidents/:incidentId` | `LEGACY_ELEMENT_PLUS` |
| `/devops`, `/settings`, catch-all 404 | `LEGACY_ELEMENT_PLUS` |
| additive `/atlas` | Not registered yet; implementation remains Gate 4 |

All ten Workspace paths remain registered and lazy. Agent is pinned outside the ordinary navigation groups while `/agent` remains available. Gate 2 did not claim any Workspace migration.

## Decision Coverage

| Decision | Gate 2 result and evidence |
| --- | --- |
| D-01 | `PASS`: continuous Sidebar plus main Shell; route content remains behind the explicit legacy boundary |
| D-02 | `PASS`: Scope appears only in the Sidebar; Header has no duplicate selector |
| D-03 | `PASS`: Agent is a fixed Sidebar pin, not an ordinary navigation item; `/agent` is retained |
| D-06 | `PASS`: expanded Sidebar is 220px and desktop rail is 64px in the tested viewports |
| D-20 | `PASS`: 1280 remains expanded while 1024 degrades to the rail without a phone workflow or page overflow |
| D-34 | `PASS`: Provider health uses Lucide semantics and `X/Y`, supports hover/focus, and opens `/settings#providers` |
| FR-SUP-002 | `PASS` for the Gate 2 Shell: 1920/1440 primary and 1280/1024 desktop degradation were checked; phone UI is retired |
| FR-CX-007 | `PASS` for the Shell: semantic landmarks, accessible icon controls, keyboard Focus, topmost Escape, Skip Link, Light/Dark, and reduced motion |
| FR-CX-008 | `PASS`: Shell ownership and every existing route's retained legacy ownership are explicit; no capability is silently counted as migrated |

The broader URL/Inspector, Workspace state, and page-level D/FR decisions remain assigned to their later Gates.

## Implementation Result

- The Shell now uses Nuxt UI controls and Lucide icons for grouped navigation, compact Header, Scope, Provider health, Notification, Agent, theme, Owner identity, and Sidebar collapse.
- Sidebar order is brand, Scope, operating groups, response group, system group, Agent pin, Local Owner, and collapse command. Rail links keep visible Tooltips and explicit accessible names.
- Header responsibilities are unique: breadcrumb, Provider health, Live/Scenario truth, Notification, theme, and Owner identity. Scope and Agent are not duplicated.
- `MobileBottomNav.vue` and both mobile navigation exports were removed. At 1024px the product uses the 64px desktop rail.
- Theme selection is applied by the canonical pre-paint owner, persisted across reload, and remains equivalent in Light/Dark.
- Notification keeps its independent list, unread/read/read-all, refresh, context link, and SSE lifecycle. Fixture-backed POST actions were used only to validate presentation and are not B8.
- A closed Global Agent performs zero Agent list/detail/event reads. Opening loads the read-only projection and consultation stream; closing tears the active stream down. Notification SSE ownership remains independent.
- Overlay Escape handling is topmost-only. Closing the last surface restores its trigger Focus.
- Cross-route hashes wait for asynchronously rendered anchors, so `/settings#providers` resolves without an early Router warning. Saved browser positions still take precedence, and route entry restores H1 Focus.
- All current component routes carry an explicit `uiOwner: "legacy-element-plus"` marker. The Nuxt UI Shell does not import Element Plus.

## Focused Validation

| Check | Result |
| --- | --- |
| `npm run typecheck` | `PASS` |
| Six focused Vitest files for navigation, routes, scroll, theme, Agent lifecycle, and Notification SSE | `PASS`, 6/6 files and 19/19 tests |
| Targeted ESLint on Gate 2 TypeScript/Vue files | `PASS`, 0 findings |
| `node --check tests/e2e/fixture-server.mjs` | `PASS` |
| Mobile Bottom Navigation/export scan | `PASS`, zero matches |
| Shell Element Plus import/render scan | `PASS`, zero matches |
| Shell emoji scan | `PASS`, zero matches |
| `git diff --check` | `PASS` |

See `commands/validation.txt` for exact focused commands and deferred checks. Full repository checks remain `NOT RUN` under the approved Gate cadence.

## Browser Evidence

| Code | Status | Evidence |
| --- | --- | --- |
| B1 | `PASS` | Chromium 1440x900 Light/Dark, persisted theme/sidebar state, Shell controls, Console and request inspection |
| B2 | `PASS` | Chromium 1920x1080 Light/Dark, expanded 220px Sidebar and information density |
| B3 | `PASS` | Chromium 1280x800 expanded layout and 1024x768 Light/Dark 64px rail; no page-level horizontal overflow |
| B4 | `NOT RUN` | Zoom and 200% text are not required for this focused Shell exit and remain for affected page Gates/Gate 12 |
| B5 | `PASS` | keyboard Provider link, rail Tooltip/name, route H1, Skip Link, overlay stack, Escape, trigger Focus restore, reduced motion, Back/Forward and scroll restoration |
| B6 | `NOT RUN` | Firefox and WebKit are deferred to their route Gates and Gate 12 |
| B7 | `NOT RUN` | Fixture-backed browser traffic is not a real UI -> API -> Provider chain |
| B8 | `NOT RUN` | No isolated target, credential, cleanup proof, or separate write authorization |

The final Playwright CLI session was `gate2-shell-final`, using Chromium at 1440x900, locale `zh-CN`, timezone `Asia/Shanghai`, and reduced motion. Final Console inspection reported 0 errors and 0 warnings. Relevant local API and SSE requests returned 200. One external `https://api.iconify.design/lucide.json` request also returned 200; this is retained as a Gate 12 CSP/offline-delivery observation and is not described as Provider traffic.

Retained versionable screenshots are in `browser/`. The interaction trace and 23 MB keyboard/Focus recording remain diagnostic output rather than commit payload:

- `output/playwright/gate2-shell-clean/.playwright-cli/traces/trace-1785433785598.trace`
- `output/playwright/gate2-shell-clean/gate2-agent-focus.webm`

Detailed actions, measurements, request identities, and screenshot mapping are in `browser/results.md`.

## State Applicability

Gate 2 validates the Shell around the legacy `/incidents` route, not the Incident page migration.

| State | Applicable | Run status | Reason |
| --- | --- | --- | --- |
| Ready | YES | `PASS` | Required for layout, navigation, theme, Focus, Notification, and Agent checks |
| Loading | YES | `NOT RUN` | Workspace loading presentation is unchanged and remains assigned to later route Gates |
| Empty | YES | `NOT RUN` | Workspace empty presentation is unchanged |
| Error | YES | `NOT RUN` | Workspace error presentation is unchanged |
| Partial | YES | `NOT RUN` | The Header fixture rendered one available Provider; partial Provider health was not exercised |
| Stale | YES | `NOT RUN` | Page/domain stale presentation is assigned to later Gates |
| Disconnected | YES | `NOT RUN` | Unit lifecycle/teardown passed; a browser network-fault presentation was not run in this Gate |
| Permission Denied | YES | `NOT RUN` | API/error presentation is assigned to Gate 3 and route Gates |

## Limitations and Deviations

- `REAL_READONLY_INTEGRATION=NOT RUN`; there is no request/trace/Provider identity proof in this Gate.
- `WRITE_PATH_E2E=NOT RUN`; fixture Notification state changes do not prove persistence or Provider effects.
- B4, B6, full lint, lint warning-budget execution, `typecheck:e2e`, full unit, build, dependency audit, full E2E, Lighthouse, and full performance validation are `NOT RUN` and deferred by the approved plan.
- The one runtime Iconify request is a recorded delivery/CSP observation. It did not fail this local fixture-backed visual Gate, but must not be omitted from Gate 12 release evidence.
- `BACKEND_GAP`: none discovered in the bounded Gate 2 Shell scope.
- `APPROVED_DEVIATION`: none.

## Rollback and Exit

Rollback point: `b27af59ebc38f08cd0037ffa926685e78fbaa19e` (Gate 1). The Gate 2 diff is independent of Workspace route implementations and is captured by the authorized local commit containing this report. No rollback action was performed.

Current exit state:

```text
APP_SHELL_MIGRATION=PASS
MOBILE_NAV_RETIREMENT=PASS
GLOBAL_OVERLAY_AND_SSE_CONTRACTS=PASS
OWNER_VISUAL_GATE_SHELL=PASS
GATE_03_ENTRY=READY
```
