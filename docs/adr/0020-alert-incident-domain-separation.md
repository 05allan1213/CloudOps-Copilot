# ADR 0020: Separate Alert and Incident Domains

- Status: Accepted domain decision; implementation NOT RUN
- Date: 2026-07-26
- Refined by: ADR 0033

## Context

The current V3 ingress normalizes each accepted Alertmanager item into a Signal and immediately creates, reopens or attaches it to an Incident. Exposing that model as an Alert workspace would only duplicate the Incident list and would not support alert triage as a distinct operational responsibility.

## Decision

Alert and Incident are separate first-class domain objects. An Alert represents an operational condition and can remain outside an Incident. An Incident represents a coordinated response case and can group multiple related Alerts. Correlation or promotion into an Incident is an explicit policy or authorized operator decision, not an unavoidable consequence of receiving every firing Alert.

Signal remains the immutable normalized source fact. It is not the user-managed Alert lifecycle.

## Consequences

- The Alert workspace must have its own backend query and lifecycle contracts; it cannot be an alias for the Incident projection.
- Alert acknowledgement, silence, correlation and promotion require explicit validation, concurrency and audit semantics before they become interactive controls. ADR 0033 removes assignment from the single-Owner target.
- Incident views must expose linked Alerts and their relationship provenance without collapsing the two lifecycles.
- The current automatic Signal-to-Incident path requires a compatible migration strategy; existing Incident evidence and history cannot be rewritten.

## Rejected Alternative

Continue converting every accepted firing Alert directly into an Incident and present the same records under separate navigation labels. This creates duplicate pages without a distinct triage workflow.
