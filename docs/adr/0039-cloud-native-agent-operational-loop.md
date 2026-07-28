# ADR 0039: Cloud-Native and Agent Operational Loop

- Status: Accepted product-throughline decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0025, ADR 0027, ADR 0031 and ADR 0034

## Context

A sidebar containing Monitoring, Alerts, Logs, Traces, Agent, Incidents and DevOps would still feel like unrelated tools unless the same operating context and evidence can move through them. The project has two core directions, cloud-native operations and Agent, while DevOps is supporting. Future observability integrations must extend those directions rather than create additional disconnected product identities.

## Decision

The whole product follows one Operational Loop:

`Observe -> Detect -> Investigate -> Decide -> Act -> Verify`

- Observe presents real Kubernetes topology and state, Metrics, Logs and Traces in one Operational Scope.
- Detect turns immutable Signals into independently managed Alerts.
- Investigate lets the Agent and Owner correlate Kubernetes facts, Metrics, Alerts, Logs and Traces into attributable Evidence.
- Decide presents diagnosis, uncertainty, Query Authorization and exact action or Operation Plan choices to the Owner.
- Act may require no mutation, follow a Runbook, perform an authorized Kubernetes operation or enter the optional DevOps and GitOps delivery branch.
- Verify returns to the same resource and time context to confirm recovery with current Metrics, Logs, Traces, Alerts and Kubernetes state. Failed verification begins another investigation cycle.

Incident is an optional durable coordination lifecycle around this loop, not a prerequisite for observation, Alert triage or Agent Consultation. DevOps is an optional action and change-evidence branch, not the end goal. Successful delivery without observable recovery does not complete the loop.

The Cloud-Native Evidence Plane is the shared source layer for both native Workspaces and Agent reasoning. The initial product requires Kubernetes state and topology, Metrics, Alerts, Logs and Traces as first-class sources. Correlation preserves available cluster, environment, Namespace, workload, Service, Pod, trace identity and time-window provenance rather than relying on display text or model inference.

Every Workspace provides typed Context Links that continue the Operational Loop with the same Operational Scope, selected resource, time range and Evidence references. Logs and Traces are therefore initial core capabilities: a metric or Alert can open related logs, a log `trace_id` can open its Trace, a Trace can identify Kubernetes resources, and the Agent can cite the resulting Evidence.

Future sources such as Kubernetes Events, audit logs, continuous profiling, Cilium or Hubble network flows, SLO burn rates and cost signals extend the same Evidence Plane. A new source requires a bounded provider adapter, typed provenance and health state, native view or overlay, Context Links and an authorized Agent read contract where useful. It does not create a parallel operational lifecycle or duplicate the provider's telemetry store.

## Consequences

- Major feature slices and demonstrations are evaluated by how far they complete the Operational Loop, not by route count alone.
- Cross-Workspace identity and time propagation are shared contracts rather than page-specific URL conventions.
- Agent answers and verification cannot claim correlation without attributable Evidence Plane identities and source times.
- Overview visualizes the loop's current cloud-native state; it is not a marketing dashboard detached from investigation.
- DevOps remains valuable for change context and controlled delivery but cannot dominate navigation, implementation order or completion claims.

## Rejected Alternatives

- Treat each Workspace as an independent provider viewer.
- Make Incident creation mandatory before Agent or observability work can begin.
- Define GitOps delivery as the only successful product outcome.
- Add later observability sources as separate dashboards with no shared Evidence contract.
