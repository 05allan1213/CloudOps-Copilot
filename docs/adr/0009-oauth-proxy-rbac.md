# ADR 0009: GitHub OAuth, oauth2-proxy, CSRF and RBAC

- Status: Superseded by ADR 0024 for the target local-only product; retained as the historical deployable OAuth decision
- Date: 2026-07-18
- Owner: Phase 5

## Context

V2 uses local users/passwords/JWT and stores a Bearer token in browser localStorage. V3 needs bounded Demo identity without building an IAM product or forwarding OAuth credentials into application code.

## Decision

Run oauth2-proxy beside cloudops-api. The public Service exposes only the proxy; the user API listens on loopback. The proxy owns GitHub OAuth, OAuth state and its HttpOnly/SameSite session cookie, overwrites trusted user headers and does not forward OAuth access token, Authorization or its session cookie.

The API records provider=github, login and request_authenticated_at. GitHub login is mutable and is not claimed as an immutable numeric subject.

The API issues a short-lived signed CSRF token from GET /api/v3/session/csrf, bound to the trusted identity, nonce and expiry. The frontend keeps it in memory and sends X-CSRF-Token. Mutations additionally require Origin, Idempotency-Key and expected version/hash.

Roles are viewer and operator. viewer can see complete Incident facts/diff/hashes/Decision. operator adds only the three documented Command families.

## Consequences

- Delete local registration, initial admin, user management, local JWT and browser token storage.
- GitHub OAuth identity is separate from the GitHub App machine writer.
- HTTP port-forward may use Secure=false cookie only as an explicit local Demo exception.

## Rejected Alternatives

- Keycloak, a user-management center or a third admin product role.
- Forward OAuth access tokens to the API or reuse them for GitHub writes.
- Rely on SameSite alone without CSRF/Origin/idempotency controls.

## Evidence Required

Forged-header, cross-user/expired CSRF, token/cookie forwarding, loopback reachability, role matrix and local-auth absence tests.
