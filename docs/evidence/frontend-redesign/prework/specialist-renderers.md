# Specialist Renderers and Large-Data Decision

Recorded: 2026-07-30 (Asia/Shanghai)

## Result

```text
SPECIALIST_RENDERERS=PASS
SELECTED_MONITORING_RENDERER=uPlot 1.6.32
TRACE_RENDERER_DECISION=RETAIN_CURRENT_RENDERER_AND_ADD_VIRTUALIZATION
ATLAS_RENDERER_DECISION=RETAIN_THREE_JS 0.185.1
SELECTED_VIRTUALIZATION=TanStack Vue Virtual 3.13.35
PRODUCTION_MIGRATION=NOT RUN
```

All candidates were evaluated in the isolated prototype under `prototypes/cloudops-prework/`. Nuxt UI remains the only general UI system. uPlot, Three.js and TanStack Virtual own only chart, topology and large-list rendering; they reuse the CloudOps token, tooltip, focus and overlay language.

## Current Documentation and Package Evidence

The following current sources were checked on 2026-07-30 through Context7 or the packages' official documentation and registry metadata:

| Library | Current source checked | Relevant contract |
| --- | --- | --- |
| uPlot 1.6.32 | <https://github.com/leeoniya/uPlot> and Context7 `/leeoniya/uplot/1.6.32` | Aligned multi-series data, null gaps, cursor, scale/range changes and TypeScript declarations |
| Three.js 0.185.1 | <https://threejs.org/docs/#api/en/renderers/WebGLRenderer> and Context7 `/mrdoob/three.js` | Pixel ratio, resize, animation lifecycle, context loss, `forceContextLoss`, render-list/resource disposal |
| TanStack Virtual 3.13.35 | <https://tanstack.com/virtual/latest/docs/framework/vue/vue-virtual> and Context7 `/websites/tanstack_virtual` | Vue virtualizer, scroll-element ownership, count, estimated size, overscan, virtual items and total size |
| Vite | <https://vite.dev/guide/features.html#dynamic-import> | Route-level dynamic imports and specialist chunk isolation |

Registry metadata showed uPlot's package unpacked size at 545,468 bytes, compared with ECharts 6.1.0 at 60,297,703 bytes and Chart.js 4.5.1 at 6,178,899 bytes. These package sizes were screening evidence, not substitutes for the measured Vite gzip output below. Registry modification timestamps were 2025-03-14 for uPlot, 2026-07-01 for Three.js and 2026-07-28 for TanStack Virtual.

## Monitoring Selection

uPlot is selected because it met the time-series contract with the smallest measured specialist route and without importing another component or theme system.

| Requirement | Evidence | Result |
| --- | --- | --- |
| Realistic scale | Deterministic 7,200-point, three-series fixture at five-second intervals | PASS |
| Missing data | Fifty aligned null points remain gaps rather than invented values | PASS |
| Multi-series and synchronized values | CPU, latency and error series share the cursor and accessible data table | PASS |
| Keyboard path | Focused chart surface responds to Arrow keys and updates the synchronized table | PASS |
| Range/zoom | 15-minute range control reduces the visible point window | PASS |
| Partial and Empty truth | Partial Provider stays visible when chart data is empty and after recovery | PASS |
| Light/Dark | 1920x1080 and 1440x900 captures in both themes | PASS |
| Nonblank canvas | 55,721 of 55,728 sampled pixels differ from the origin background; 111 quantized colors | PASS |
| Console and Network | No application Console/page errors or unexpected failed responses | PASS |
| Bundle | Monitoring route 25,451 bytes gzip, below 81,920 bytes | PASS |

ECharts and Chart.js remained package-metadata comparison candidates. They were not promoted to a second full prototype because uPlot passed every blocking behavior, accessibility-equivalent table and bundle gate; running another general chart stack after a passing unique selection would not change the decision.

## Trace Decision

The current production Trace surface already supplies search, waterfall position, span selection, full attributes/events, Evidence retention and frozen-context handoff. Its identified scale gap is row rendering, not missing Trace semantics. The prototype therefore keeps the current renderer and adds TanStack virtualization at the list/waterfall boundary instead of introducing a second Trace renderer.

The 2,500-span fixture rendered fewer than 100 DOM rows at once, supported end-to-start scrolling, selection, complete-value copying, Evidence handoff composition and stale-request cancellation. A new Trace rendering library is rejected until production-scale evidence demonstrates a capability that the retained renderer plus virtualization cannot provide.

## Atlas Decision

Three.js remains selected for Atlas and stays route-lazy. The prototype uses 200 nodes and a complete structured equivalent path.

| Requirement | Evidence | Result |
| --- | --- | --- |
| Nonblank 3D scene | Canvas sampled 10,823 non-background pixels and 62 quantized colors | PASS |
| Inspector resize | Closing Inspector expands canvas by more than 180 CSS pixels; reopening restores the bounded layout | PASS |
| Light/Dark | 1920x1080 and 1440x900 captures in both themes | PASS |
| WebGL failure | `?webgl=fail` presents all 200 resources through the structured path | PASS |
| Context loss | Fault injection exposes an explicit context-lost state and structured path | PASS |
| Visibility pause | Injected hidden state reports `hidden/paused`; visible state resumes FPS | PASS |
| DPR | Renderer caps device pixel ratio and resizes through `ResizeObserver` | PASS |
| Disposal | RAF canceled, observers/listeners removed, geometries/materials disposed, context force-lost and canvas removed | PASS |
| Repeated navigation | Five load/dispose cycles retained constant Three.js application-object counts | PASS |
| Production framebuffer | Current production WebGL context reports `preserveDrawingBuffer=false` | PASS |
| Bundle | Three.js lazy chunk 183,351 bytes gzip, below 204,800 bytes | PASS |

Known limitation: Chromium retained lost native WebGL context wrappers until tab teardown across five forced-loss cycles. Three.js application object counts remained constant and each canvas was removed, so this does not invalidate selection, but it is not evidence of a production long-soak memory pass. Production Atlas still requires long-session monitoring after migration.

## Large-Data Strategy

TanStack Vue Virtual is selected for bounded row virtualization:

| Surface | Fixture size | Browser result |
| --- | ---: | --- |
| Logs | 10,000 rows | Fewer than 100 DOM rows; end scroll and full-value copy PASS |
| Trace spans | 2,500 rows | Fewer than 100 DOM rows; selection/Evidence composition PASS |
| Incident timeline | 5,000 rows | Fewer than 100 DOM rows; stable reading position contract PASS |
| Large table | 20,000 rows | Fewer than 100 DOM rows; no page-level overflow PASS |

The shared virtualization chunk is 6,605 bytes gzip, below the 81,920-byte specialist budget. Virtualization does not alter API pagination, truncation, request identity, full-value access or stale-request cancellation semantics.

## Bundle Budget

| Chunk | Actual gzip | Limit | Result |
| --- | ---: | ---: | --- |
| Main entry including local Lucide payload | 60,914 bytes | 307,200 bytes | PASS |
| Three.js lazy chunk | 183,351 bytes | 204,800 bytes | PASS |
| Monitoring/uPlot route | 25,451 bytes | 81,920 bytes | PASS |
| TanStack virtualization chunk | 6,605 bytes | 81,920 bytes | PASS |

## Evidence

- `output/playwright/prototype/metrics/bundle-report.json`
- `output/playwright/prototype/metrics/monitoring-canvas-pixels.json`
- `output/playwright/prototype/metrics/atlas-canvas-pixels.json`
- `output/playwright/prototype/metrics/atlas-disposal.json`
- `output/playwright/prototype/metrics/atlas-memory.json`
- `output/playwright/prototype/chromium-results.json`

Passing-run trace/video directories were reviewed locally and intentionally excluded from the versioned baseline; the retained source, result JSON and focused metrics reproduce the specialist checks without committing large transient binaries.
