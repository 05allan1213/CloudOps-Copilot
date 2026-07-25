# ADR 0043: Explicit Cross-Session Agent Knowledge

- Status: Accepted product decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0026, ADR 0035, ADR 0036 and ADR 0042

## Context

Persistent Agent Consultations make prior reasoning available, but automatically injecting arbitrary old conversations into new work would create hidden context, stale conclusions and findings with no inspectable source. CloudOps also already has Git-managed Runbook Guidance, whose retrieval role must remain distinct from both retained conversation history and current Evidence.

## Decision

Agent Consultation history is durable within its original Consultation. Starting a new Consultation does not silently import previous conversations, conclusions or preferences. The Agent automatically receives only the new Consultation's explicit Context Snapshots, retained Evidence references and the configuration and bounded knowledge sources authorized for that context.

Reusable cross-session memory is represented as a versioned Knowledge Item. The Owner may explicitly save a conclusion, preference, known environment fact or operating experience; the Agent may propose a candidate, but one exact Owner confirmation is required before it becomes available to future Consultations. Passive extraction from chat history is prohibited.

Every Knowledge Item records its source, creation time, applicable cluster, Namespace and resource scope, authoring path and optional review or expiry time. It can be disabled, revised as a new version or explicitly deleted. Updating an item never rewrites the version used by an existing Consultation or Investigation.

When an Agent uses a Knowledge Item, it exposes the exact item version, source and age in its answer. Expired, review-due or scope-mismatched items are excluded or visibly marked as stale; a prior conclusion is never presented as a current runtime fact without fresh Evidence.

Git-managed Runbook Guidance remains a separate versioned guidance source and can be browsed from the Agent Workspace or Settings. Runbook Guidance and Knowledge Items can influence investigation strategy, but neither can establish root cause, satisfy Evidence requirements or authorize an Operation Plan.

## Consequences

- The Agent Workspace needs native Consultation-history and Knowledge Item management surfaces, including provenance, scope, status, version history and deletion controls.
- Agent inputs and outputs must record which Knowledge Item and Runbook versions were retrieved so a result can be reproduced and audited.
- Knowledge retrieval is bounded by the active Context Snapshot and Operational Scope rather than global semantic similarity alone.
- Deleting a Knowledge Item prevents future retrieval while retained Agent records preserve the historical version reference required to explain past output.

## Rejected Alternatives

- Automatically inject every previous chat into each new Consultation.
- Let the Agent silently create durable preferences or environment facts from conversation.
- Treat old diagnoses, Knowledge Items or Runbook Guidance as current Evidence.
- Merge Owner-managed Knowledge Items into the Git-managed Runbook corpus without preserving their distinct provenance and lifecycle.
