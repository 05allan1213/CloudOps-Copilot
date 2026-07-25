# ADR 0029: Live Operations Atlas Visual System

- Status: Accepted visual-system decision; implementation NOT RUN
- Date: 2026-07-26
- Extends: ADR 0019

## Context

The current frontend is a conventional card-and-table administration layout. It is orderly but does not make cloud-native relationships, live telemetry or Agent activity visually identifiable. A purely cinematic redesign would create impact at the cost of the operator-first priority.

## Decision

The visual system uses a Live Operations Atlas as its central metaphor. Overview presents a full-bleed, interactive 2.5D topology of real Kubernetes resources, service relationships, telemetry, Alerts and Agent activity. The atlas is an operating surface: selection, focus, filtering and drill-down lead directly to native Workspaces and exact context.

Three.js is the renderer for the 2.5D scene. Depth distinguishes system layers; the camera remains stable and controllable rather than becoming a decorative rotating scene. Empty or unavailable data produces an explicit native state, never a fabricated topology.

The visual baseline uses neutral black, white and graphite surfaces with multiple semantic signal colors. It rejects a one-note blue-purple gradient aesthetic. Experimental typography and large-scale motion are reserved for Overview and meaningful state transitions; dense monitoring, Alert, log, configuration and action surfaces remain compact and predictable.

All product icons use Lucide. Emoji and hand-drawn substitute icons are prohibited.

## Consequences

- Motion must represent a real state transition or relationship, remain interruptible, animate compositor-friendly properties, and provide a zero-continuous-motion `prefers-reduced-motion` path.
- The topology must remain keyboard-accessible through an equivalent structured resource view; canvas interaction cannot be the only navigation path.
- Critical controls, labels and system truth retain stable geometry while the scene updates.
- Repeated cards, nested cards, decorative gradient blobs, glow-heavy sci-fi styling and marketing-page composition are outside the visual system.
- Visual acceptance includes desktop and mobile screenshots, canvas nonblank pixel checks, interaction tests, Lighthouse accessibility, performance traces and layout-shift checks.
- Award-level presentation is an aspiration evaluated alongside task completion, readability, accessibility and performance; it cannot waive those gates.

## Rejected Alternatives

- Retain a generic administrative dashboard with cosmetic color changes.
- Use an atmospheric 3D scene that is unrelated to real product data.
- Apply experimental layout and motion uniformly to dense operational workspaces.
