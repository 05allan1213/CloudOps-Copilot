# ADR 0006: Deployment Identity and Last-Known-Good Baseline

- Status: Accepted target decision; implementation NOT RUN
- Date: 2026-07-18
- Owner: Phase 5

## Context

Source commit, running image and GitOps desired state are different identities. Mutable tags, telemetry labels or application-reported versions cannot safely identify the deployed change or a restore source.

## Decision

Always store and validate three separate values:

| Identity | Authority |
|---|---|
| source_revision | Registry OCI revision plus GitHub exact commit |
| image_digest | Kubernetes Pod imageID plus Registry manifest |
| gitops_revision | Argo Application/history exact commit |

DeploymentBaseline is a separately verified target record with immutable observations. A Phase 5 baseline-verifier Job uses a dedicated least-privilege identity and no GitHub write/LLM credential. Only one active verified baseline exists per target.

restore_required_env may copy the exact allowlisted YAML node only from a verified ancestor baseline. No baseline, identity mismatch or invalid ancestry stops remediation.

## Consequences

- The Golden configuration regression changes only gitops_revision.
- Telemetry attributes are correlation hints, not authority.
- Baseline facts copied into an Incident become new bounded Evidence with source observation hashes.
- A passing final Verification can supersede the active baseline.

## Rejected Alternatives

- Treat service.version, source revision, image tag or Argo targetRevision as one generic revision.
- Infer last-known-good from time proximity or a model suggestion.
- Write baselines during Phase 3 readiness probing.

## Evidence Required

Registry/Kubernetes/GitHub/Argo exact-chain tests, mutable-tag negatives, baseline uniqueness/ancestry tests and least-privilege Job evidence.
