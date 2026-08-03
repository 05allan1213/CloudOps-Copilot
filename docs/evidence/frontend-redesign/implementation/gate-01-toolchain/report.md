# Gate 1 Production Toolchain, Token Pipeline, and Budget Report

Recorded: 2026-07-31 (Asia/Shanghai)

## Status

```text
GATE_01=PASS
NUXT_UI_PRODUCTION_TOOLCHAIN=PASS
CANONICAL_TOKEN_PIPELINE=PASS
BUNDLE_BUDGET_CI=PASS
NO_NEW_WARNING_GATE=PASS
REVIEW_CONCLUSION=COMPLIANT
```

## Identity and Scope

| Item | Evidence |
| --- | --- |
| Repository | `/home/monody/k8s/CloudOps-Copilot` |
| Branch | `main` |
| Gate base | `149c9614f0514a92f655d56644fff7527f5b50d6` |
| Gate final identity | The local Gate 1 commit containing this report; resolve with `git log -1 --format=%H -- docs/evidence/frontend-redesign/implementation/gate-01-toolchain/report.md` |
| Owner authorization | `FRONTEND_REFACTOR_PLAN_APPROVED=YES`, `LOCAL_GATE_COMMITS_AUTHORIZED=YES` |
| Data source for browser checks | Deterministic fixture at `127.0.0.1:18082` through the Vite proxy; not real integration |
| External writes | None |
| Backend/API/database/Provider/Kubernetes changes | None |

The Gate started from a clean committed Gate 0 rollback point. No route ownership changed in Gate 1: existing routes remain `LEGACY_ELEMENT_PLUS` behind a root Nuxt UI `UApp` provider, and no formal smoke route was added. The bounded dual-library state is migration-only and cannot support a release-ready conclusion.

## Files

| Area | Files and purpose |
| --- | --- |
| Dependencies | `frontend/package.json`, `frontend/package-lock.json` |
| Production integration | `frontend/vite.config.ts`, `frontend/src/main.ts`, `frontend/src/App.vue`, generated `frontend/auto-imports.d.ts`, generated `frontend/components.d.ts` |
| Canonical theme | `frontend/index.html`, `frontend/src/styles/tokens.css`, `frontend/src/styles/app.css` |
| Legacy compatibility | `frontend/src/styles/variables.scss`, `frontend/src/styles/light.scss`, `frontend/src/styles/dark.scss` |
| Toolchain smoke | `frontend/src/nuxt-ui-production-smoke.fixture.vue`, `frontend/src/nuxt-ui-production-smoke.test.ts` |
| Budget guard | `frontend/scripts/check-bundle-budget.mjs`, its focused test, `.github/workflows/ci.yaml` |
| Language | `docs/CloudOps-Frontend-Terminology.md` |
| Gate evidence | This directory and the Gate 1 current-state update in the approved plan |

## Dependency and Toolchain Result

The production dependency tree contains one version of each locked package:

```text
@nuxt/ui@4.10.0
@tailwindcss/vite@4.3.3
tailwindcss@4.3.3
@iconify-json/lucide@1.2.120
@tanstack/vue-virtual@3.13.35
uplot@1.6.32
three@0.185.1
```

Nuxt UI is integrated through its official Vite and Vue plugin entry points. The representative SSR smoke compiles and renders `UButton`, `UForm`, `UFormField`, `UInput`, `UTable`, `UModal`, and `USlideover` without exposing a production route or a `#build` type failure. Visible Nuxt UI icon configuration has zero non-`i-lucide-*` matches.

The first browser pass found a real integration defect: the standalone Nuxt UI color-mode plugin re-added `dark` beside the persisted `light` class. Final production configuration sets `colorMode: false`, leaving the existing CloudOps pre-paint and persisted-theme pipeline as the single owner. The final browser result has exactly one root class in both themes.

## Canonical Token Pipeline

`frontend/src/styles/tokens.css` is the only new raw-value source and implements:

```text
Primitive color/type/spacing/shape/motion/depth
  -> Light/Dark Semantic variables
  -> structural/component density variables
  -> Nuxt UI --ui-* mapping
  -> Tailwind @theme aliases
  -> computed Semantic variables available to later uPlot/Three.js/virtualization adapters
```

The Light/Dark semantic sections contain zero direct hex/rgb/hsl values; they reference Primitive variables. `app.css` and the retained legacy compatibility files also contain zero raw color values. Existing Element Plus variables now map to the same canonical Semantic layer and remain only for unmigrated routes.

This Gate establishes the renderer-facing computed variables and exact dependencies. It does not claim that uPlot or TanStack Virtual already has a production consumer; those route migrations remain Gate 5 and Gate 6. Existing Atlas ownership remains unchanged until Gate 4.

Decision foundation covered here:

- D-06: 220px Sidebar, 64px rail, content and Inspector bounds.
- D-07: dense control/table/type metrics.
- D-08: restrained radius, border and overlay-only shadow tokens.
- D-09: separate severity foreground/background/border semantics.
- D-10: stable Skeleton/loading token and motion foundation.
- D-11: sans/mono and tabular-ready typography foundation plus the Chinese-first terminology contract.
- FR-SUP-002: 1280/1024 desktop degradation foundation.
- FR-CX-007: Light/Dark parity, visible Focus, Skip Link and reduced-motion foundation.

Page-level business behavior for these decisions remains assigned to later Gates; this report does not count a token as a migrated user capability.

## Focused Validation

| Check | Result |
| --- | --- |
| `npm run typecheck` | `PASS` |
| Focused Vitest for budget and production Nuxt UI smoke | `PASS`, 4/4 |
| Targeted ESLint on changed TypeScript/Vue files | `PASS`, 0 findings |
| Exact dependency tree and duplicate-version scan | `PASS`, one version per locked package |
| Official-registry `npm audit --audit-level=high` | `PASS`; one low Windows-only esbuild dev-server advisory under `fontless` remains |
| Production `npm run build` | `PASS` |
| `npm run bundle:check` | `PASS` |
| Token raw-value and non-Lucide scans | `PASS`, zero forbidden matches |
| `git diff --check` | `PASS` |
| Gate 0 rollback build and route start | `PASS` in an isolated `git archive` snapshot |

The ESLint ceiling is reduced from 2,608 to the versioned Gate-entry baseline of 2,564. Changed TypeScript/Vue files add zero findings. Full repository lint remains `NOT RUN` because the approved plan defers complete lint to Gate 12; the cap was not increased.

Production build warnings are retained rather than suppressed:

- two upstream `@vueuse/core` misplaced PURE-annotation warnings introduced through the Nuxt UI dependency tree;
- the existing raw Three.js chunk warning above Vite's default 500 kB display threshold.

`chunkSizeWarningLimit` was not changed. The fixed gzip budgets are stricter than the display warning and pass:

| Budget | Actual | Limit | Status |
| --- | ---: | ---: | --- |
| Main static JavaScript | 212,128 bytes | 307,200 bytes | `PASS` |
| Three.js plus OrbitControls | 193,940 bytes | 204,800 bytes | `PASS` |

uPlot and virtualization do not emit chunks yet because their production routes have not migrated. The manifest-based guard classifies and enforces their 80 KiB budgets as soon as they become emitted consumers.

## Browser Evidence

| Code | Status | Evidence |
| --- | --- | --- |
| B1 | `PASS` | Chromium 1440x900, persisted and first-use system Light/Dark, Console/Network clean |
| B2 | `NOT RUN` | 1920 density is not required for this dependency/Token Gate |
| B3 | `PASS` | Chromium 1280x800 and 1024x768; 1024 collapses to 64px rail; document/main widths match |
| B4 | `NOT RUN` | Zoom and 200% text are deferred to affected page migrations and Gate 12 |
| B5 | `PASS` | route H1 Focus, visible Skip Link, main entry, overlay trap/Escape/restore, reduced motion |
| B6 | `NOT RUN` | Gate 1 focused matrix is Chromium B1/B3/B5 only |
| B7 | `NOT RUN` | Fixture is not a real UI -> API -> Provider chain |
| B8 | `NOT RUN` | No isolated write target, credential, cleanup proof, or separate write authorization |

Final Chromium observations:

- Light: root class `light`, canvas `rgb(244, 246, 248)`, primary text `#17212b`.
- Dark: root class `dark`, canvas `rgb(11, 15, 20)`, primary text `#f1f5f7`.
- First-use storage is absent and system Light/Dark is applied correctly.
- 1440 document/main widths: `1440/1440` and `1220/1220`.
- 1280 document/main widths: `1280/1280` and `1060/1060`.
- 1024 document/main widths: `1024/1024` and `960/960`; Sidebar is 64px.
- route entry Focus is `H1[tabindex=-1]`; Skip Link is visible on Focus and Enter moves Focus into `main`.
- Notification overlay traps Tab inside an `aria-modal=true` dialog; Escape closes it and restores Focus to `打开通知收件箱`.
- reduced motion is active; measured animation and transition duration is `1e-05s`.
- Console: 0 errors, 0 warnings. Network: only fixture-backed GET requests, all HTTP 200; no mutation request.

See `browser/results.md` and the five retained screenshots. These artifacts prove a fixture-backed route regression only, not B7 or B8.

## State Applicability

The browser route was `/incidents`, used only as an existing-route theme/layout/Focus regression surface.

| State | Applicable | Run status | Reason |
| --- | --- | --- | --- |
| Ready | YES | `PASS` | Required to inspect theme, layout and Focus |
| Loading | YES | `NOT RUN` | State behavior unchanged by Gate 1 |
| Empty | YES | `NOT RUN` | State behavior unchanged by Gate 1 |
| Error | YES | `NOT RUN` | State behavior unchanged by Gate 1 |
| Partial | YES | `NOT RUN` | Provider/state composition unchanged by Gate 1 |
| Stale | YES | `NOT RUN` | SSE/state composition unchanged by Gate 1 |
| Disconnected | YES | `NOT RUN` | SSE lifecycle unchanged by Gate 1 |
| Permission Denied | YES | `NOT RUN` | API/error behavior unchanged by Gate 1 |

Full lint, typecheck:e2e, full unit, full E2E, Firefox/WebKit, Lighthouse, real read-only integration and all write paths remain `NOT RUN` until their approved Gate.

## Rollback

Rollback point: `149c9614f0514a92f655d56644fff7527f5b50d6`.

An isolated `git archive` of that exact commit completed `npm ci`, `npm run build`, Vite startup, and a real Chromium load of `/incidents`; H1, document width and Console checks passed. Reverting only the local Gate 1 commit therefore restores the old dependency, Vite, entry and legacy theme setup without resetting or discarding any other work.

## Exit

```text
NUXT_UI_PRODUCTION_TOOLCHAIN=PASS
CANONICAL_TOKEN_PIPELINE=PASS
BUNDLE_BUDGET_CI=PASS
NO_NEW_WARNING_GATE=PASS
GATE_02_ENTRY=READY
```
