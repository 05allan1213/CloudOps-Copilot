# ADR 0038: Vertical Feature Integration Validation

- Status: Accepted implementation and validation decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0019 and ADR 0037

## Context

Testing every intermediate edit across all backend, frontend, performance, accessibility and infrastructure dimensions would slow a personal project without proving that its real user workflow works. Separating all backend work from all frontend work would also recreate the current problem: substantial internal capability with no coherent native product surface.

## Decision

Implementation proceeds as full-stack vertical capabilities. Each major capability includes its domain and persistence changes, bounded provider adapters, native API contracts, Chinese-first frontend Workspace behavior and the Context Links needed to continue the same operation. Backend and frontend are not delivered as separate project-wide phases.

Validation occurs after a major capability is complete, not after every implementation step. The primary acceptance evidence is MCP-driven frontend/backend integration against the real local CloudOps API and the available configured Provider engines. The browser workflow must exercise the capability through its native UI, inspect relevant network responses and visible state, and confirm that the intended operation completes normally without a blocking console or interaction failure.

Focused builds, type checks or automated tests may still be used when required to make a capability runnable or diagnose a defect, but a broad all-aspects suite is not a mandatory acceptance gate. Fixture-only browser results do not prove real frontend/backend integration. Unavailable external services or unexecuted checks are reported as `NOT RUN`, and observed failures remain `FAIL`; neither is converted into an implied full PASS.

Existing mature libraries, design patterns and cloud-native services may be reused when they fit the accepted product boundaries. Reuse does not permit iframe shells, direct browser credentials or outsourcing a primary native workflow to an external console.

## Consequences

- Work is grouped into large observable capabilities rather than tiny test-driven increments.
- Each capability ends with one representative happy-path integration workflow plus the most relevant visible failure or unavailable state.
- MCP browser evidence takes precedence over claims based only on source inspection, unit fixtures or static screenshots.
- Performance, accessibility and visual diagnostics remain useful targets but are not required to all pass at every capability boundary.
- Integration reports state exactly which API, Provider and UI path ran and avoid claiming project-wide readiness from one workflow.

## Rejected Alternatives

- Run a comprehensive test matrix after every small edit.
- Build the entire backend first and attach a frontend afterward.
- Treat fixture-only pages or external console links as proof that a native capability works.
- Require every diagnostic dimension to pass before confirming one completed feature is functional.
