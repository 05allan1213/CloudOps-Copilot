# ADR 0040: Make-Managed Local Project Lifecycle

- Status: Accepted local-use decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0010, ADR 0024 and ADR 0030
- Refined by: ADR 0042

## Context

CloudOps-Copilot remains a source project rather than a packaged desktop or installed application. The existing repository exposes overlapping Compose, demo, kind, Helm and manual port-forward workflows. Even with kind and Helm as the canonical architecture, that fragmentation makes routine personal use slower and requires remembering infrastructure details that should be project-owned.

## Decision

Top-level Make targets are the only public local lifecycle interface. There is no standalone launcher, desktop packaging or second orchestration implementation.

- `make local-up` checks prerequisites, obtains project-pinned helper tools when needed, creates or reuses the dedicated kind cluster, builds and loads local images, applies migrations and Helm releases, waits for readiness, establishes the stable loopback access path and prints the CloudOps URL.
- `make local-open`, `make local-status`, `make local-logs`, `make local-restart`, `make local-down`, `make local-doctor` and `make local-reset` cover normal use, diagnosis and cleanup.
- `local-up` and recovery-oriented commands are idempotent. Re-running them repairs or resumes project-owned state rather than replacing healthy resources blindly.
- `local-reset` is the only lifecycle target that deletes persistent CloudOps state and requires explicit confirmation. `local-down` cannot silently erase data.

Make remains a thin command surface. Complex behavior lives in bounded repository scripts with clear inputs and failure messages. kind, Helm, kubectl and related helper versions are pinned by the project and may be downloaded into a Git-ignored project tool cache instead of requiring system-wide installation. The host prerequisite set is kept minimal and checked before mutation.

The managed access path is loopback-only and does not require the Owner to run manual port-forward commands. The lifecycle operates only on explicitly named project resources and never deletes or reconfigures unrelated Docker containers, Kubernetes clusters or user data.

The Settings Workspace manages an already running CloudOps instance, Operational Configuration and Provider connections. It does not bootstrap kind, Helm or the local process lifecycle from inside the web application.

The old full-stack Compose and phase-specific demo entrypoints remain migration-only until feature equivalence is proven, then are removed or reduced to internal compatibility helpers. They are not parallel supported user paths.

## Consequences

- Documentation and error messages lead with the `make local-*` workflow rather than infrastructure-specific manual steps.
- Lifecycle state, managed process identities and tool cache locations need deterministic, Git-ignored paths.
- Interrupted startup must be resumable, and `local-doctor` must distinguish missing prerequisites, stale managed state and Provider unavailability.
- Changes to Helm or bootstrap scripts are incomplete until the same behavior is reachable through the public Make targets.
- ADR 0042 defines data persistence across `local-down`, automatic backup and the exact `local-reset` boundary.

## Rejected Alternatives

- Package CloudOps-Copilot as a desktop application or standalone local launcher.
- Require the Owner to run kind, Helm, migration and port-forward commands manually.
- Keep Compose and kind as equally supported complete architectures.
- Put long, stateful orchestration logic directly inside the Makefile.
