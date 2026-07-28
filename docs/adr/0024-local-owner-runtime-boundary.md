# ADR 0024: Local Owner Runtime Boundary

- Status: Accepted runtime and trust-boundary decision; implementation NOT RUN
- Date: 2026-07-26
- Supersedes: ADR 0009 and ADR 0022 for the target product
- Refines: ADR 0023 authentication mapping

## Context

CloudOps-Copilot is a personal project intended to run only on the Owner's machine through loopback, local containers or a local kind cluster. A GitHub OAuth login, role mapping and deployable multi-user authentication boundary add friction without protecting an actual shared or public deployment.

## Decision

The target product runs in Local Owner Mode. Opening the locally exposed application establishes the sole Owner context without login, account management or RBAC. The supported access boundary is loopback-only; public, LAN-shared and multi-user exposure are out of scope.

Provider credentials remain backend-only. Locally stored secrets must stay outside Git, logs and frontend read responses. Mutations with external effects, including GitHub writes or Kubernetes changes, retain an explicit confirmation and bounded idempotent operation contract. Same-origin and Origin validation remain invisible safeguards for local browser-triggered mutations; the authenticated CSRF-token flow is removed.

## Consequences

- Remove GitHub OAuth, oauth2-proxy, login routes, role maps and session-expiry UI from the target local profile.
- Bind or expose the application only through a verified loopback path; startup must refuse an unsafe listen configuration in Local Owner Mode.
- Settings can optimize for direct local persistence, validation and reload instead of deployment-time identity administration.
- External provider identities and machine credentials remain distinct from the implicit Owner context.
- Any future public, LAN-shared, remote or multi-user mode requires a new ADR before implementation.

## Rejected Alternatives

- Keep production-style OAuth and RBAC for a single local user.
- Remove all safeguards around secrets and irreversible external effects merely because the user is local.
