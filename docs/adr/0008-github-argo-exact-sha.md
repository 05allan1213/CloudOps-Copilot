# ADR 0008: GitHub App, Argo Read-Only and Exact SHA

- Status: Accepted target decision; implementation NOT RUN
- Date: 2026-07-18
- Owner: Phase 5-7

## Context

CloudOps needs one external write surface while Git and Argo remain desired-state authorities. Provider timeouts, mutable PR heads and skipped Argo revisions can otherwise duplicate changes or falsely prove delivery.

## Decision

Use one GitHub App installed only on the Demo GitOps repository. Its adapter is fixed to one repo/base/path/branch prefix, can create branch/commit/Draft PR and read checks, and has no merge API. Human review and merge remain separate actions.

Each branch/commit/PR write is one task phase. A durable write-intent marker and stable logical operation key precede the call; timeout triggers branch/tree/PR-marker reconciliation, never blind retry.

CI is accepted only for the current unique PR head whose tree/post-image equals Approval-bound hashes, with exact head SHA, completed/success, expected check name, producer App and workflow identity.

CloudOps Argo identity is get-only. Argo is the only reconciler. Delivery requires merged commit/tree equality, Argo sync revision equality and successful syncResult revision equality. A skipped target revision fails delivery.

## Consequences

- CloudOps never merges, reruns CI, syncs, overrides, rolls back or updates a stale branch.
- GitHub path isolation is an adapter/policy/ruleset boundary, not complete protection from a fully compromised Worker.
- A validated PR head SHA is evidence, not part of the earlier human Approval.

## Rejected Alternatives

- Personal token or multiple machine writers.
- Direct Git push to main.
- CI or CloudOps calling Argo sync/rollback.
- Accepting PR overall status or a successor Argo revision.

## Evidence Required

Installation/ruleset/path negatives, ambiguous-write crash fixtures, check producer/workflow binding, exact-tree comparison, Argo read-only RBAC and exact-revision Golden evidence.
