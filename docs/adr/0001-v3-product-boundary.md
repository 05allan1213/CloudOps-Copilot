# ADR 0001: V3 Product Boundary and One Incident Flow

- Status: Superseded by ADR 0018; retained as the historical Incident-only decision
- Date: 2026-07-18
- Owner: Phase 0, enforced by every later Gate
- Supersedes: V2 product-scope and execution-spec decisions

## Context

The live V2 repository still contains monitoring-platform, local-auth, direct-remediation and multi-deployment concepts. Treating those assets as product requirements would preserve a broad portal and weaken the Cloud Native and Agent story.

## Decision

V3 solves one problem: a Kubernetes workload failure enters one Incident, a bounded read-only Agent gathers Evidence, deterministic code compiles one restricted Plan, a human approves an exact change, GitHub/CI/Argo deliver it and deterministic Verification proves recovery.

Only viewer and operator exist. Closed-no-action, no-change Verification and failed-Verification reinvestigation are branches inside the same Incident model, not separate products.

Generic Chat, host monitoring, AlertRule/notification CRUD, Kubernetes console, generic DevOps execution, automatic merge/sync/rollback, multi-cluster, multi-tenant, HA/DR and production claims are non-goals.

## Consequences

- Incident List and Incident Detail are the only product pages.
- Supporting Metric/Log/Trace/Kubernetes/GitOps capabilities must enter the one flow.
- Existing V2 capabilities outside the boundary receive an explicit DELETE or archive decision.
- kind evidence must be described as local disposable Demo evidence.

## Rejected Alternatives

- Preserve V2 as a general monitoring portal.
- Add a parallel Agent Chat or task center.
- Build multiple real fault-remediation scenarios before the one Golden flow works.

## Evidence Required

Phase 0 proves only decision consistency. Phase 7 and final DoD require the exact-SHA Golden flow and absence of bypass/product surfaces.
