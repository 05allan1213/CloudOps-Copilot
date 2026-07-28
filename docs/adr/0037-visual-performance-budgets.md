# ADR 0037: Visual Performance and Motion Targets

- Status: Accepted quality-target decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0019 and ADR 0029

## Context

The Live Operations Atlas and award-level presentation goal introduce GPU, bundle-size and motion costs that can easily undermine the operator-first decision. Numeric targets provide direction and diagnostic evidence without turning every implementation step into a separate test gate.

## Decision

Visual implementation targets measurable performance, accessibility and motion budgets on a documented local production-build reference profile.

- Lighthouse Performance is at least 90 on desktop and 85 on mobile; Lighthouse Accessibility is at least 95.
- Core Web Vitals targets are LCP at or below 2.5 seconds, INP at or below 200 milliseconds and CLS at or below 0.1.
- The Atlas targets 60 frames per second on desktop and does not fall below 30 frames per second on mobile with the standard 200-visible-node acceptance fixture.
- The initial shell JavaScript budget is 300 KiB gzip. Three.js and Atlas code are route-lazy chunks and do not block another Workspace's first render.

Atlas quality adapts before interaction degrades. It may lower device pixel ratio, label density, shadow quality and post-processing based on viewport, node count and measured capability, but cannot hide Alerts, change topology truth or remove required controls. Rendering and high-frequency refresh pause when the page is hidden.

Motion communicates resource relationships, live state or an explicit interaction. Continuous decorative motion outside the Atlas is prohibited. Animations remain interruptible and use compositor-friendly properties wherever DOM rendering is involved. `prefers-reduced-motion` removes continuous movement while preserving state and interaction.

If WebGL is unavailable, initialization fails or a reduced rendering tier cannot meet the minimum, Overview falls back to the complete structured resource view using the same topology projection. A blank canvas is never an accepted state.

## Verification

Visual diagnostics are run at the completion of a major visual capability rather than after each implementation step. Available checks may include production builds, documented desktop and mobile viewports, Playwright or Chrome MCP workflows and screenshots, Canvas nonblank pixel checks, keyboard equivalence, Lighthouse audits, Performance traces, layout-shift inspection, browser-console checks and long-text or dense-data overflow checks.

ADR 0038 controls feature acceptance: successful real frontend/backend integration through MCP is the primary requirement. The numeric targets in this ADR do not require every diagnostic category to pass before a major capability can be considered functionally normal. Each measured result remains reported honestly as PASS, FAIL or NOT RUN.

## Consequences

- Heavy visual libraries, fonts and provider editors load only in the routes that use them.
- The implementation plan must define the exact reference machine, browser version, throttling profile and fixture before claiming a numeric PASS.
- A visually preferred effect that causes observable interaction failure is reduced or removed rather than protected for presentation alone.
- Performance and accessibility diagnostics are concentrated at major capability boundaries instead of repeated after every edit.

## Rejected Alternatives

- Approve visual quality from static screenshots alone.
- Run the Atlas at maximum quality on every device.
- Keep an animated empty scene when real data or WebGL is unavailable.
- Ignore observable accessibility, responsive or performance failures merely because numeric targets are non-blocking.
