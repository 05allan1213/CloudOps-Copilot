# ADR 0041: Live and Real Scenario Data

- Status: Accepted data-truth decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0029, ADR 0038, ADR 0039 and ADR 0040

## Context

An operational frontend needs enough activity to demonstrate topology, Alerts, Logs, Traces and Agent reasoning, but silently seeded or mocked data would make the interface visually convincing while proving nothing about the actual system. Requiring an external GitHub and Argo CD workflow for every demonstration would instead make the supplemental DevOps branch block the cloud-native and Agent core.

## Decision

CloudOps has an explicit data-truth boundary between Live Mode and Demonstration Scenarios.

Live Mode is the default. It renders only current or persisted facts from configured real Providers and CloudOps domain records. Missing, empty, stale or unavailable data is shown honestly. The frontend never fills an empty topology, chart, Alert list, Evidence view or Agent result with hidden fixtures.

`make scenario-up` activates a project-owned Demonstration Scenario in a dedicated bounded Namespace. It deploys a real workload and traffic source and introduces a controlled fault that produces actual Kubernetes state, Prometheus Metrics, Alertmanager Signals, Elasticsearch Logs and OpenTelemetry or Tempo Traces. CloudOps consumes those sources through the same native APIs, persistence and Agent contracts as Live Mode.

The first Scenario exercises the complete core Operational Loop: observable degradation, Alert creation, related error logs and Trace, Agent Investigation with attributable Evidence, Owner-authorized recovery and post-action verification. It does not require GitHub or Argo CD. Optional later Scenarios may demonstrate the DevOps or GitOps action branch.

Scenario state is visibly identified throughout the shell, Operational Scope, Agent context and retained domain history so it cannot be mistaken for Live Mode. `make scenario-down` removes only explicitly owned Scenario runtime resources. Any deletion of retained CloudOps history remains a separate explicit operation under the retention policy.

Frontend fixtures and deterministic provider stubs remain permitted for isolated development or diagnosis, but they cannot satisfy ADR 0038 real frontend/backend integration acceptance and cannot be presented as a working Demonstration Scenario.

## Consequences

- Scenario workloads, traffic, fault injection and cleanup need stable ownership labels and bounded identities.
- The same Context Links and Evidence provenance must work in Live Mode and a Scenario; no demo-only frontend route or response shape is allowed.
- Scenario preflight reports unavailable Provider or LLM dependencies instead of substituting synthetic results.
- Screenshots and demonstrations always disclose Scenario state when it is active.
- A visually empty but truthful Live Mode is valid and receives a useful native empty state.

## Rejected Alternatives

- Seed attractive fake data whenever Providers are empty.
- Treat the Playwright fixture server as full-stack demonstration evidence.
- Require an external GitHub or Argo workflow before the core loop can be shown.
- Let Scenario cleanup delete unrelated Namespace, cluster or retained history implicitly.
