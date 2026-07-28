# ADR 0035: Context-Bound Agent Consultations

- Status: Accepted Agent Consultation decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0026 and ADR 0034

## Context

Making Agent a core product capability requires more than moving the existing Incident Agent panel into the sidebar. A generic chat box would lose the operational state that motivated each question, while silently following page navigation would make earlier answers and tool calls impossible to reproduce.

## Decision

Agent Consultation is a persistent first-class product object. A global Agent dock can be opened from every native Workspace without leaving the current task. The `/agent` Workspace provides the full history of Consultations and durable Alert or Incident Investigations, their current state and related Evidence.

Starting a Consultation attaches an immutable Context Snapshot containing the current Operational Scope, selected resource identities, time range, active filters or Query Definition versions, relevant Evidence references and Configuration Revision. Navigating or changing selection elsewhere does not silently mutate that snapshot. `Attach current context` creates and records a new Context Snapshot for subsequent messages.

Consultation messages, Context Snapshots, tool calls, Query Authorization, Evidence references, model identity and execution status persist locally. The Owner can resume, search, rename and explicitly delete Consultations subject to retaining any Evidence already owned by an Alert or Incident record.

Responses stream incrementally and can be stopped or retried. The interface exposes meaningful tool progress, source identity, observation time, uncertainty and citations rather than presenting an unexplained waiting state or unsupported final answer.

The Owner may promote a Consultation into an Alert Investigation, create a new Incident from it or attach it to an existing Incident. Promotion records the originating Consultation and selected Context Snapshot; it does not reinterpret the entire conversation as verified Evidence. Agent query execution continues to obey ADR 0034 Query Authorization.

## Consequences

- The backend needs durable Consultation, message, Context Snapshot, tool-execution and stream-resume contracts separate from existing Incident-scoped Agent runs.
- Context attachment and promotion are idempotent commands with visible provenance.
- A Consultation can remain useful without becoming an Alert or Incident, and deletion cannot erase Evidence already retained by another lifecycle.
- The global dock and full Agent Workspace share one state model instead of implementing separate chat products.
- Browser refresh, route navigation and temporary provider failure must not lose completed messages or completed tool attribution.

## Rejected Alternatives

- Keep Agent only as an Incident Detail panel.
- Add an ephemeral unscoped chatbot.
- Let a conversation silently inherit whatever page happens to be open later.
- Treat every chat statement as Incident Evidence during promotion.
