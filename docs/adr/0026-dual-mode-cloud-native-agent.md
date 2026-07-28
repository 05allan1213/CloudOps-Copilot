# ADR 0026: Dual-Mode Cloud-Native Agent

- Status: Accepted Agent product decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0025

## Context

The current Agent is visible only as a bounded Investigation projection inside Incident Detail, and its public command contract can start work only for an existing Incident. That makes the Agent a subordinate section rather than a core way to understand cloud-native state.

## Decision

The product provides two complementary Agent modes:

- Agent Investigation is a durable, bounded run triggered for an Alert or Incident. It persists steps, tool calls, Evidence, uncertainty and a diagnostic outcome.
- Agent Consultation is an Owner-initiated, context-scoped diagnostic conversation available from native monitoring, Alert, log, trace and Kubernetes views. It can select bounded read tools and must cite attributable Evidence, source time ranges and uncertainty in its answers.

Consultation can create or continue an Incident when the Owner chooses. Neither mode may directly execute arbitrary shell, kubectl or infrastructure writes. A proposed change crosses into an explicit, separately confirmed operation contract.

## Consequences

- Agent becomes a first-class workspace with consultation history and Investigation runs, while contextual launchers remain available throughout the product.
- The backend needs Agent Consultation, message, streaming, context-snapshot and Evidence contracts in addition to Incident-scoped Investigation APIs.
- Both modes reuse bounded provider adapters and present visible tool progress rather than an unexplained loading state.
- The existing LLM chat client is not itself a product chat API; persistence, cancellation, replay bounds and evidence attribution must be added deliberately.
- Generic unscoped chat and direct write-tool execution remain outside the product.

## Rejected Alternatives

- Keep Agent activity only inside Incident Detail.
- Add an unrestricted chatbot that is disconnected from CloudOps context and Evidence.
