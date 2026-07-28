# ADR 0018: Unified CloudOps Product Boundary

- Status: Accepted product-boundary decision; implementation NOT RUN
- Date: 2026-07-26
- Supersedes: ADR 0001 product boundary
- Product priority refined by: ADR 0025

## Context

ADR 0001 constrained the product to an Incident List and Incident Detail. That boundary forced monitoring, alerts, logs, Agent activity, delivery and configuration into one Incident flow, leaving the wider CloudOps capabilities undiscoverable and difficult to operate.

## Decision

CloudOps-Copilot is a unified CloudOps Operations Platform, not an Incident-only product. Monitoring, alerting and incident response, logs and traces, Agent operations, DevOps delivery, and controlled platform configuration are first-class product concerns. The Incident workflow remains a core cross-domain flow, but it is no longer the only product surface.

The final information architecture, user and permission model, write boundaries, and interaction language remain unresolved. They must be decided before implementation begins.

## Consequences

- The two-page-only constraint in the V3 design and the existing frontend redesign plan must be replaced by a new agreed information architecture.
- Existing Incident workflow contracts remain valid unless a later decision explicitly supersedes them.
- New configuration and operational actions require explicit backend ownership, authorization, validation and audit contracts; the browser must not become a direct credential or infrastructure control plane.
- Frontend-only navigation placeholders do not satisfy this decision. Each first-class concern needs an honest data source and capability boundary.

## Rejected Alternative

Retain the Incident-only product and simulate broader capabilities with more sections, anchors or external links inside Incident Detail. This preserves the discoverability, navigation and operability failures that triggered the decision.
