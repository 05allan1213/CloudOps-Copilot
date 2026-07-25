# ADR 0023: Single-Owner Product Identity

- Status: Accepted single-Owner decision; GitHub OAuth mapping superseded by ADR 0024; implementation NOT RUN
- Date: 2026-07-26
- Partially supersedes: ADR 0009 role model

## Context

CloudOps-Copilot is a personal project with one intended user. The viewer/operator split and a future platform-administrator role would add permission management and interaction friction without representing a real collaboration requirement.

## Decision

The product has one Owner. ADR 0024 defines how the local runtime establishes the Owner context. The Owner can access every native view, operate Alerts and Incidents, run the Agent, approve bounded remediation, and manage platform configuration.

The product does not provide user management, role assignment, role switching or a multi-role RBAC interface. The OAuth access token and CloudOps session remain isolated as defined by ADR 0009 and ADR 0022.

Owner authority does not bypass safety controls. Mutations still require the relevant confirmation, CSRF and Origin checks, idempotency, expected version or content hash, validation and durable audit. Secret values are never returned to the browser after submission.

## Consequences

- Existing `viewer` and `operator` API, OpenAPI, middleware, chart and test contracts must migrate to `owner` during implementation.
- Frontend controls are shaped by resource state and safety preconditions, not by a role-management UI.
- Provider machine identities, GitHub App writes and CloudOps user identity remain separate even though one person owns the product.
- Anonymous or public product access is not part of this decision.
- Multi-user collaboration requires a later ADR and an explicit identity, permission and migration design.

## Rejected Alternatives

- Preserve enterprise-style viewer, operator and platform-administrator roles for one person.
- Remove authentication and mutation safeguards because the deployment has only one user.
