# ADR 0033: Explicit Alert Escalation

- Status: Accepted Alert-lifecycle decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0020 and ADR 0023

## Context

The current ingress path turns every accepted firing Alertmanager Signal into an Incident immediately. That creates noisy Incident history, prevents a distinct triage workflow and makes the new Alert Workspace an alias for Incidents. Conversely, requiring manual promotion for every severe or sustained failure would discard useful automation in a personal operations tool.

## Decision

An accepted Signal creates or updates an Alert lifecycle; it does not unconditionally create or reopen an Incident. Alert firing or resolved state follows attributable source facts, while acknowledgement, silence, Agent Investigation and Incident relationships remain separate facets.

The default product configuration has automatic escalation disabled. From the Alert Workspace, the Owner can acknowledge an Alert, create a bounded Alert Silence through Alertmanager, start an Agent Investigation, promote the Alert into a new Incident or attach it to an existing Incident. These are native idempotent commands with visible results and audit history.

Optional Escalation Policies are managed as versioned Operational Configuration. A policy can match explicit severity, labels, Namespace or other bounded scope and require a minimum firing duration or recurrence count. A matching policy creates at most one active Incident for its correlation identity and records the policy identity and Configuration Revision that authorized the promotion. Manual promotion records Owner provenance instead.

Resolving an Alert does not automatically resolve or close a related Incident. An Incident has its own investigation, remediation and recovery-verification lifecycle and may still contain other firing Alerts. A Silence suppresses provider notifications for a bounded interval but does not acknowledge or resolve the Alert.

Assignment is not part of the Local Owner Mode Alert model because there is only one Owner. A future multi-user product must define ownership and handoff semantics before adding it.

## Consequences

- Alert persistence and APIs need independent lifecycle, acknowledgement, silence, Agent-run and Incident-link projections.
- Alertmanager ingestion must migrate from direct Incident creation to Signal and Alert updates without rewriting existing Incident history.
- Existing Incidents retain their Signals and gain explicit legacy relationship provenance during compatible migration.
- Policy evaluation must be deterministic, idempotent and bound to a Configuration Revision.
- Alert and Incident pages must show relationship direction and provenance instead of implying that their status changes together.

## Rejected Alternatives

- Continue creating an Incident for every firing Alert.
- Disable all automatic escalation permanently.
- Resolve or close Incidents automatically when one linked Alert resolves.
- Add assignment controls that can only assign work back to the same Owner.
