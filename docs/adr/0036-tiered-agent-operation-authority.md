# ADR 0036: Tiered Agent Operation Authority

- Status: Accepted Agent action decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0024, ADR 0026 and ADR 0034

## Context

Agent must be able to assist with real work rather than only describe it, but treating one conversational instruction as permanent authority would allow later context, parameters or model output to change the effect without a new Owner decision. Requiring the same heavyweight approval for a saved view and a Kubernetes mutation would instead make a personal tool unnecessarily cumbersome.

## Decision

Agent operations use three authority levels.

1. Read operations use built-in bounded tools directly and expert observability queries under the Query Authorization contract in ADR 0034.
2. CloudOps-local or readily reversible operations use an exact action card with a one-click Owner confirmation. This level includes actions such as acknowledging an Alert, creating or relating an Incident, saving a Query Definition and creating or removing a bounded Alertmanager Silence. The card identifies the target and resulting state before execution.
3. External or high-impact writes require an immutable Operation Plan. The Plan binds exact target identity, parameters, diff or intended state, preconditions, risk, expiry, Configuration Revision and verification intent. Creating a GitHub pull request or changing a Kubernetes Workload is in this level. The Owner authorizes one exact Plan before Agent execution and post-operation verification.

Action Authorization binds the content identity of the action card or Operation Plan, not the Agent, conversation or model. Any material target, parameter, diff, precondition or version change invalidates the decision and requires a new authorization. There is no reusable or conversational blanket write authority.

Agent cannot execute arbitrary shell, `kubectl`, unrestricted provider mutation or silent background writes. Every proposed, authorized, running and completed operation is visible in the Agent timeline. Operations are cancellable before their irreversible boundary and expose compensation or recovery guidance when the provider supports it.

Explicitly configured automation such as an Escalation Policy is governed by its own versioned policy contract; it does not grant the Agent general write authority.

## Consequences

- A shared typed command envelope, idempotency identity, optimistic concurrency and audit projection must cover Agent dock, Workspace actions and Incident workflows.
- Level-two actions need concise confirmation without disguising their exact provider effect.
- Level-three Plans preserve the existing hash-bound approval properties while expanding beyond the current Incident-only presentation.
- Execution checks authorization, expiry and preconditions immediately before the effect, then records provider identity and verification outcome.
- A failed or partial operation cannot be presented as completed and remains recoverable from its persisted timeline.

## Rejected Alternatives

- Make Agent permanently read-only.
- Grant reusable write permission to a conversation or model.
- Require a heavyweight immutable Plan for every local reversible action.
- Permit arbitrary shell or `kubectl` after a generic confirmation.
