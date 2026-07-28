# ADR 0007: restore_required_env and Hash-Bound Approval

- Status: Accepted target decision; implementation NOT RUN
- Date: 2026-07-18
- Owner: Phase 5

## Context

The current V2 remediation operations are rollback_image and set_replicas, with incomplete approval hashes and a direct-demo path. V3 needs one bounded correction that cannot be rewritten after human review.

## Decision

The only V3 operation is restore_required_env for one allowlisted non-Secret environment key in one Deployment file/container.

The Agent emits only an operation hint, target field reference and Evidence IDs. Deterministic Go code reads exact base and verified baseline commits, validates ancestry, parses YAML AST, copies the complete env node, computes the post-image/tree, produces one bounded diff, evaluates Policy and freezes the VerificationPlan.

The immutable Plan has a versioned canonical plan hash over its complete semantic fields, including the exact bounded diff, diagnosis, base, pre/post-image, tree, change manifest, patch, Policy, Verification, ordered Evidence set, risk and expiry. The immutable Decision binds that hash and schema version together with the actor, role, request authentication time, expiry and reason.

Approval never binds a future PR head or merged SHA. Before each write phase, current base/evidence/policy/plan hashes are revalidated. Any change invalidates/supersedes the Plan; there is no silent rebase.

## Consequences

- Secret, image, replicas, RBAC, CRD, PVC, security context and arbitrary YAML changes are rejected.
- No Approval means zero external write.
- Consumed means a unique ChangeRequest exists; historical approved content never changes.
- V2 Approval is archive-only and cannot authorize V3 work.

## Rejected Alternatives

- Let the model generate an env value, patch, YAML or Git command.
- Support several remediation actions for the first release.
- Approve a summary and compute the actual patch later.

## Evidence Required

YAML AST/property tests, policy negatives, hash/preflight/stale-base tests, no-Approval zero-write proof and complete diff/Decision UI evidence.
