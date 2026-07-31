# Gate 12A Frontend Exception Ledger

## Status And Scope

```text
GATE_12A_EXCEPTION_LEDGER=PASS
RAW_PAGE_COLOR_EXCEPTIONS=0
SECOND_GENERAL_UI_EXCEPTIONS=0
NON_LUCIDE_ICON_EXCEPTIONS=0
UNEXPLAINED_LIBRARY_DOM_SELECTORS=0
```

This ledger applies to the final Gate 12A integration tree based on
`65c55a673599ed68e326712445d3c01c1709ea75` plus the Gate 12A cleanup delta.
It records the bounded non-zero results required by plan section 0.5.6 and the
historical Gate 12 scan. An entry permits only the named responsibility; it is
not permission to add another raw-value source or page-specific UI framework.

## Approved Exceptions

| Scan | Current result | Approved boundary | Why it remains | Evidence boundary |
| --- | ---: | --- | --- | --- |
| Raw color | `91` matches in four files | `frontend/src/styles/tokens.css` is the only canonical Primitive raw-color source. `frontend/src/theme/atlasTheme.ts` contains 11 defensive fallbacks whose values mirror canonical semantics. The two test files use sentinel values only. | Three.js needs concrete colors if computed CSS tokens are temporarily unavailable during renderer construction or a non-DOM test. Runtime page styles must consume Semantic or Component variables. | `atlasTheme.test.ts` verifies token precedence and readable fallback. Final build and Atlas route smoke cover runtime consumption. |
| `:global()` | `13` matches in four SFCs | Only named Nuxt UI `UModal` and `USlideover` portal slot classes supplied through each component's `ui` configuration. | Portal content renders outside the SFC scoped-style root. The selectors target project-owned stable slot class names, not Nuxt UI's generated DOM structure. | Final Chromium route smoke covers Notification, Global Agent, Agent entry, and Agent authority overlays where reachable without writes. |
| `:deep()` | `32` matches in twelve SFCs | Only scoped layout, icon sizing, width, or spacing applied to Lucide, Nuxt UI primitives, or an owned business child component. | Scoped CSS cannot otherwise reach the rendered child root. These rules do not recreate control behavior, color state, focus management, or library internals. | Lane focused smoke plus final all-route Chromium smoke cover the affected Agent, Incident, and Settings surfaces. |
| `!important` | `14` matches, all in `frontend/src/style.css` | Nine declarations implement the reusable visually-hidden contract; four force the reduced-motion override; one preserves the compact-width 44px touch-target floor. | These accessibility rules must win over component and transition declarations regardless of source order. No page or visual-theme override uses `!important`. | Final build and Chromium smoke; full accessibility and multi-viewport validation remain Gate 12B `NOT RUN`. |
| Fixed dimensions | Non-zero by design | Stable icon boxes, dense rows, bounded Inspector/Modal/Slideover regions, virtualization viewports, chart/Atlas canvases, and approved responsive breakpoints. | These are component geometry and interaction constraints, not a parallel spacing/color/radius/elevation token system. Reusable visual values continue to come from `--co-*` tokens. | Final 1440x900 smoke checks clipping, overlap, and layout stability. Multi-viewport, zoom, large-data, and performance matrices remain Gate 12B `NOT RUN`. |

## Raw Color Detail

- `frontend/src/styles/tokens.css`: canonical Primitive values and their
  Semantic/Component/UI-library mappings. Raw values are expected here by the
  three-layer token contract.
- `frontend/src/theme/atlasTheme.ts`: defensive renderer-only fallbacks. The
  normal path always reads `--co-*` Semantic tokens from
  `document.documentElement`.
- `frontend/src/theme/atlasTheme.test.ts` and
  `frontend/src/components/monitoring/monitoringPresentation.test.ts`:
  test-only sentinel strings proving token trimming, mapping, and fallback;
  neither enters the production bundle.
- Production source outside `tokens.css` and `atlasTheme.ts`: zero raw-color
  matches.

## Selector Detail

The remaining `:global()` selectors are limited to these project-owned portal
classes:

- `notification-slideover-*`
- `global-agent-slideover-*`
- `agent-entry-modal-*`
- `agent-authority-modal-*`

The remaining `:deep()` selectors are limited to child roots such as `button`,
`a`, `textarea`, Lucide `svg`, Nuxt UI `.u-form-field`/`.u-input`/`.u-select`,
and the owned `.hash-value`. There are no generated class names, structural
`nth-child` selectors, Element Plus selectors, or selectors that replace Nuxt
UI keyboard/ARIA behavior.

## Rejection Rules

Gate 12A does not approve any of the following:

- raw colors in a page, business component, or new specialist adapter;
- a new `:global()`, `:deep()`, or `!important` occurrence without an updated
  ledger and focused browser evidence;
- selectors coupled to undocumented Nuxt UI DOM structure;
- non-Lucide visible icons, hand-drawn SVG controls, or a second general UI
  library;
- claims that this bounded ledger replaces Gate 12B accessibility,
  multi-browser, multi-viewport, performance, or large-data validation.
