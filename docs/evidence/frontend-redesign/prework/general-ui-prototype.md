# General UI Candidate Prototype

## Selection

```text
GENERAL_UI_PROTOTYPE=PASS
SELECTED_GENERAL_UI=Nuxt UI 4.10.0
SELECTED_CSS_SYSTEM=Tailwind CSS 4.3.3
PRODUCTION_MIGRATION=NOT RUN
```

Nuxt UI was evaluated as the only general UI candidate. No fallback candidate or second general UI library was needed.

## Official Documentation Check

The following current official documentation was consulted on 2026-07-30 before the prototype was accepted:

| Capability | Official source | Confirmed use |
| --- | --- | --- |
| Standalone Vue 3 + Vite install | <https://ui.nuxt.com/docs/getting-started/installation/vue> | `@nuxt/ui/vite`, `@nuxt/ui/vue-plugin`, `UApp`, CSS imports |
| Dashboard shell/sidebar | <https://ui.nuxt.com/docs/components/dashboard-sidebar> | Collapsible/resizable grouped desktop shell |
| Table | <https://ui.nuxt.com/docs/components/table> | Dense typed table, row selection, sticky header |
| Form controls/validation | <https://ui.nuxt.com/docs/components/form> | Input/select/switch, sync validation and error text |
| Modal and Slideover | <https://ui.nuxt.com/docs/components/modal> and <https://ui.nuxt.com/docs/components/slideover> | Controlled close, focus entry/restore, dirty confirmation |
| Theme and icons | <https://ui.nuxt.com/docs/getting-started/theme/css-variables> | Runtime CSS variable mapping and global default icon overrides |
| Tailwind Vite integration | <https://tailwindcss.com/docs/installation/using-vite> | Tailwind 4 Vite integration without a legacy config file |
| Tailwind theme variables | <https://tailwindcss.com/docs/theme> | `@theme inline` maps canonical runtime tokens |
| Vue lifecycle/focus | <https://vuejs.org/guide/essentials/lifecycle.html> | Route focus and renderer cleanup |
| Vite code splitting | <https://vite.dev/guide/features.html#dynamic-import> | Route-level lazy chunks for specialists and Workspaces |

## Isolation

The prototype lives at `docs/evidence/frontend-redesign/prework/prototypes/cloudops-prework/` with its own `package.json`, lockfile, Vite config, source and tests. Regenerated `dist/`, `node_modules/` and local browser output are excluded from the versioned baseline. Production `frontend/package.json`, production lockfile and production Vite config contain no Nuxt UI, Tailwind, uPlot or TanStack Virtual additions.

Exact prototype dependencies:

| Package | Version | Role |
| --- | --- | --- |
| `@nuxt/ui` | 4.10.0 | Only general UI system |
| `@tailwindcss/vite` / `tailwindcss` | 4.3.3 | Layout utilities and theme variable mapping |
| `@iconify-json/lucide` | 1.2.120 | Only icon collection |
| Vue / Vue Router | 3.5.40 / 4.6.4 | App and URL contracts |
| Zod | 4.4.3 | Representative form validation |
| uPlot | 1.6.32 | Monitoring rendering only |
| Three.js | 0.185.1 | Atlas rendering only |
| TanStack Vue Virtual | 3.13.35 | Large-list virtualization only |

## Representative Product Surfaces

- Grouped Dashboard Sidebar, Header and Context Toolbar.
- Dense Incident table with search/filter/pagination/row selection.
- Inspector Slideover with exact hash, lifecycle states and dirty-close guard.
- Settings form with validation, revision conflict, explicit apply and partial result.
- Controlled modal/confirmation behavior with Escape restrictions where data would be discarded.
- Light/Dark persistence, long Chinese text, long hash/error strings and 200% text enlargement.
- All visible icons and Nuxt UI default icon slots resolve to `i-lucide-*`.
- Static scan finds no raw form/dialog controls, emoji or second general UI library after the final Agent step-control repair.

## Accessibility and Interaction Evidence

| Contract | Result |
| --- | --- |
| Skip link and route H1 focus | PASS |
| Modal/Slideover role, focus entry and restore | PASS |
| Dirty Inspector confirmation cannot be dismissed accidentally with Escape | PASS |
| Table selection and labeled controls | PASS |
| Settings sync validation and leave guard | PASS |
| Theme persistence through reload | PASS |
| 1920, 1440, 1280 and 1024 desktop degradation | PASS |
| 125% and 150% browser zoom | PASS |
| Practical 200% root text enlargement | PASS, no page-level horizontal overflow |
| Reduced motion final-state behavior | PASS, `0.01ms` effective durations |

## Build and Browser Results

```text
TYPECHECK=PASS
BUILD=PASS
NPM_AUDIT=PASS (0 vulnerabilities, official registry)
CHROMIUM=PASS (9/9)
FIREFOX=PASS (1/1 critical read-only flow)
WEBKIT=NOT RUN (host libraries missing)
```

Final bundle report:

| Budget | Actual gzip | Limit | Result |
| --- | ---: | ---: | --- |
| Main entry | 60,914 bytes | 307,200 bytes | PASS |
| Three.js lazy chunk | 183,351 bytes | 204,800 bytes | PASS |
| Monitoring route | 25,451 bytes | 81,920 bytes | PASS |
| Virtualization chunk | 6,605 bytes | 81,920 bytes | PASS |

The final main-entry value includes the local Lucide icon payload used to remove external Iconify API requests. The captured Chromium traces contain no request to the external Iconify endpoints.

The host network repeatedly removed/re-added `eth3`, producing Chromium `ERR_NETWORK_CHANGED` before application assertions. The final Chromium evidence ran inside an ephemeral Bubblewrap namespace with loopback and a non-routable dummy interface. Assertions, Console/page monitoring and fixture behavior were unchanged. The final JSON shows 9 expected, 0 unexpected and 0 flaky.


## Evidence

- `output/playwright/prototype/chromium-results.json`
- `output/playwright/prototype/firefox-results.json`
- `output/playwright/prototype/webkit-results.json`
- `output/playwright/prototype/review/index.json`
- `output/playwright/prototype/metrics/bundle-report.json`

Passing-run trace, video, HTML report and CLI scratch artifacts were inspected during prework, then deleted instead of being committed. The versioned evidence keeps the exact prototype source, lockfile, browser result JSON, review screenshots/metadata and focused metrics.

## Decision

Nuxt UI 4.10.0 plus Tailwind CSS 4.3.3 satisfies the standalone Vue/Vite, mature-control, density, theme, keyboard, focus, icon, build, lazy-loading and visual-adaptation requirements without a second general UI library. Selection is a prework conclusion only; production migration remains forbidden until a later approved implementation plan.
