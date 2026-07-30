# Design System Prototype

## Result

```text
DESIGN_SYSTEM_PROTOTYPE=PASS
TOKEN_CANONICAL_SOURCE=CloudOps runtime CSS custom properties
LIGHT_DARK_PARITY=PASS
OWNER_VISUAL_REVIEW=PASS
```

## Current Production Inventory

Production styles currently combine `variables.scss`, Light/Dark files, Element Plus mappings and scoped component overrides. A static scan found:

| Debt type | Count |
| --- | ---: |
| Raw hex/rgb/hsl literals | 118 |
| `:deep()` | 11 |
| `:global()` | 10 |
| `!important` | 22 |

These are migration inputs, not a request to mass-rewrite the trusted baseline.

## Canonical Pipeline

```text
canonical CloudOps CSS variables
  -> Primitive tokens
  -> Semantic tokens
  -> Component tokens
  -> Tailwind @theme aliases
  -> Nuxt UI theme variables
  -> uPlot / Three.js / virtualization adapters
```

There is one runtime token source. Tailwind, Nuxt UI and specialist renderers consume it; they do not define parallel status meanings.

## Three Token Layers

| Layer | Representative tokens | Rule |
| --- | --- | --- |
| Primitive | ink 50-950, white, blue, green, amber, red, violet | Raw palette and scale values have no business meaning |
| Semantic | canvas, surface, muted surface, overlay, selected, text, border, action, focus, critical/warning/success/info/stale, code surface | Business state and Light/Dark meaning live here |
| Component | control/row height, control/panel/overlay radius, overlay shadow, spacing, z-index, motion | Components consume semantic values and density metrics, not raw colors |

Tailwind `@theme inline` exposes font, canvas, surface, border and text aliases. Nuxt UI receives `--ui-bg`, `--ui-text`, `--ui-border`, `--ui-primary` and radius mappings. Renderer views read the same computed CSS variables for canvas backgrounds, axes, series, nodes, edges, tooltip and selection.

## Semantic Coverage

| Need | Token/behavior |
| --- | --- |
| Canvas/surface/overlay/selected | Separate semantic backgrounds in both themes |
| Text/border/action/focus | Primary/secondary/muted text, default/strong border, action hover, 2px focus-visible outline |
| Status | Critical, warning, success, info and stale use foreground + background + text/icon |
| Partial/disconnected/permission/expired | Explicit content and Lucide icon; no color-only signal |
| Skeleton/loading | Stable reserved geometry; loading icon/status text does not resize containers |
| Code/hash | Dedicated dark code surface and anywhere wrapping |
| Density | 34px controls, 42px rows, 4px base spacing |
| Shape/elevation | 5/7/8px radii; shadows limited to overlays |
| Motion | 120/180ms causal transitions; reduced motion resolves at 0.01ms |
| Z-index | Sticky 20, overlay 100, skip link above overlays |

## Component State Matrix

| State | Visual and interaction contract |
| --- | --- |
| Default / hover / pressed | Stable dimensions; surface or selected token change |
| Focus-visible | Two-pixel semantic focus outline with offset |
| Selected / expanded | Background plus `aria-selected`, `aria-current` or expanded state |
| Disabled | Native disabled behavior plus reduced emphasis; no hidden reason |
| Loading | Reserved label/control size, progress icon and textual state |
| Invalid | Field message, invalid semantics and critical token |
| Read-only | Explicit badge/text; controls absent or disabled, not merely gray |
| Empty | Dedicated empty statement; existing scope/filter context retained |
| Partial | Warning statement identifies which Provider/result is incomplete |
| Stale | Violet/neutral stale treatment plus timestamp/continuity language |
| Disconnected | Plug state, reconnect information and no live claim |
| Permission denied | Shield icon, reason and retained surrounding context |
| Expired authority / hash changed | Exact authority/hash text and blocked action |
| Accepted/dispatched/observed/verified | Four distinct stages; current stage never implies later stages |

## Representative Business Compositions

| Composition | Evidence |
| --- | --- |
| Grouped Sidebar + Header + Context Toolbar | All prototype routes and review captures |
| Dense Incident table + Inspector | 1920/1440 Light/Dark screenshots and URL/focus tests |
| Settings form + Revision/Partial result | 1440 Light/Dark and 1024 200% text screenshot |
| Incident state/evidence/authority region | Inspector and complete-work-page fixtures |
| Monitoring adapter | uPlot Light/Dark with partial/empty/synchronized table |
| Atlas adapter | Three.js Light/Dark, Inspector and structured fallback |
| Agent desktop degradation | 1920/1440/1280/1024 visibility metrics and screenshots |
| Exceptional states | Ten domain states plus SSE lifecycle Light/Dark |

## Responsive and Motion Results

- 1920x1080 and 1440x900 Light/Dark: PASS.
- 1280x800 and 1024x768: PASS with progressive secondary-rail collapse.
- 125% and 150% browser zoom: PASS with no page-level horizontal overflow.
- 200% root text at 1024x768: PASS with no page-level horizontal overflow; compact navigation uses truncation/tooltips rather than changing the phone-product boundary.
- Reduced motion: PASS, effective animation and transition duration `0.00001s`.

## Exception Ledger

The isolated prototype contains 58 literal color values, almost entirely in the single canonical token declaration and specialist data-series/code-surface adapters. It contains nine `:deep()` selectors: dense Nuxt UI table cell sizing, uPlot canvas mounting, Three.js canvas mounting and table density. It contains no `:global()` and one `!important`, limited to the reduced-motion media query. These exceptions do not create a second token source or depend on unversioned library DOM for business semantics.

## Owner Material

`output/playwright/prototype/review/index.json` indexes 21 labeled screenshots. Four WebM recordings and matching traces were inspected during the Owner review, then excluded from the versioned baseline as passing-run diagnostics; the retained screenshots and adjacent metadata preserve the reviewed states.

Automated contrast/layout/focus/state checks do not substitute for product judgment. The Owner supplied the required exact response in the active thread on 2026-07-30:

```text
OWNER_VISUAL_ACCEPTED=YES
OWNER_VISUAL_REVIEW=PASS
```

This acceptance approves the reviewed visual/product direction only; production migration remains separately gated.
