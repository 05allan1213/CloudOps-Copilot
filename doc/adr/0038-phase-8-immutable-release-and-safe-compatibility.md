# ADR 0038: Phase 8 immutable release and safe compatibility boundary

- Status: Accepted
- Date: 2026-07-16

## Context

The prior workflow pushed both `latest` and commit-SHA tags, deployed SHA tags rather than Registry digests, disabled SBOM/provenance, and had no controlled signing gate. The repository also still has active legacy frontend, API, database and realtime callers, while credentialed traffic, subscriber and compliance evidence remains unavailable.

## Decision

Release image publication is restricted to `v*` tag events. Each application image is named only by the checked-out commit SHA, carries exact revision/source/version labels, emits SBOM and provenance attestations, passes vulnerability scanning, and is signed with keyless Cosign. The deploy job uses a protected `production` environment, downloads the actual pushed digests, validates their syntax and renders Helm image references as `repository@sha256:digest`. Mutable tags are not release or deployment inputs.

All application Dockerfile base and BuildKit frontend images are digest pinned. Helm retains tag rendering only as a local compatibility fallback; release automation must supply digests.

No business legacy candidate is deleted without static caller parity, automated regression, data readability, rollback and staging traffic/subscriber/compliance evidence. The global WebSocket route and messages remain, but browser bearer tokens move from URL query strings to the `cloudops-bearer` WebSocket subprotocol. Query-string bearer authentication is removed because URL credential exposure is a demonstrated security defect, not a usage-based cleanup decision.

## Consequences

Publishing, signing and deployment do not occur on ordinary `main` pushes. A release fails closed if scanning, attestations, signing, digest artifacts or environment approval is unavailable. Existing legacy product routes remain callable; external WebSocket clients must send an Authorization header or the documented bearer subprotocol.

## Rollback

Revert the workflow and Helm digest changes only as a reviewed source change; do not reintroduce mutable production deployment. If release automation must be disabled, stop at image build/scan and preserve the last known immutable deployment. Do not restore the tracked kubeconfig or URL-token authentication. Legacy business surfaces remain the rollback path for Workbench adoption.
