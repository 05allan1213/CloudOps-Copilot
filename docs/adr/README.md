# CloudOps Architecture Decision Records

These ADRs freeze target decisions for Phase 0 and later product decisions. Status Accepted means the target decision is approved; it does not mean the implementation, live integration or later Gate has passed. A newer ADR that explicitly supersedes an older decision controls that decision while preserving the older ADR as history.

The normative implementation specification is [../CloudOps-Implementation-Spec.md](../CloudOps-Implementation-Spec.md). It incorporates ADR 0018 through ADR 0045 and supersedes the old generation-labelled refactor design and two-page frontend plan as implementation authority. Historical technical contracts remain useful only when the current specification or an Accepted ADR retains them explicitly.

Executable task prompts and non-linear dependency guidance are maintained in [../CloudOps-Implementation-Taskbook.md](../CloudOps-Implementation-Taskbook.md); the taskbook does not override the specification or ADRs.

| ADR | Decision | Owning Gate |
|---|---|---|
| [0001](0001-v3-product-boundary.md) | Historical Incident-only product boundary; superseded by ADR 0018 and ADR 0045 | Phase 0 / final DoD |
| [0002](0002-root-module-processes.md) | Root module and API/Worker/Migrate split | Phase 1 |
| [0003](0003-mysql-async-tasks.md) | MySQL async tasks; no Kafka/Redis | Phase 2 |
| [0004](0004-observability-stack.md) | Prometheus Operator, ECK/Filebeat, OTel/Tempo | Phase 3 |
| [0005](0005-statedelta-evidence-sufficiency.md) | StateDelta, Evidence and deterministic sufficiency | Phase 4 |
| [0006](0006-deployment-identity-baseline.md) | Deployment identity and last-known-good baseline | Phase 5 |
| [0007](0007-restore-env-hash-approval.md) | restore_required_env and hash-bound approval | Phase 5 |
| [0008](0008-github-argo-exact-sha.md) | GitHub App, Argo read-only and exact SHA | Phase 5-7 |
| [0009](0009-oauth-proxy-rbac.md) | GitHub OAuth, oauth2-proxy, CSRF and RBAC | Phase 5 |
| [0010](0010-kind-helm-resource-boundary.md) | kind + Helm only and resource boundary | Phase 3 / 7 |
| [0011](0011-eval-gates-claim-safety.md) | Eval, hard Gates and claim safety | Phase 2-7 |
| [0012](0012-legacy-cutover-contract.md) | Legacy cutover and deletion strategy | Phase 7A / 7B |
| [0013](0013-agent-quality-thresholds.md) | Baseline-derived Agent Quality thresholds | Phase 4 / AGENT_QUALITY |
| [0014](0014-agent-eval-v3-policy-and-thresholds.md) | Remove invalid identity-regression sufficiency policy | Phase 4 / AGENT_QUALITY |
| [0015](0015-exhaustive-action-candidate-stop-contract.md) | Stop after exhaustive frozen action candidates | Phase 4 / AGENT_QUALITY |
| [0016](0016-runtime-bound-agent-eval-revision.md) | Bind Eval evidence to current production Agent runtime | Phase 4 / AGENT_QUALITY |
| [0017](0017-migrated-legacy-evidence-eval-v6.md) | Exclude migrated legacy facts and freeze Eval v6 | Phase 4 / Phase 7A / AGENT_QUALITY |
| [0018](0018-unified-cloudops-product-boundary.md) | Unified CloudOps product boundary; Incident is no longer the only product surface | Product redesign |
| [0019](0019-operator-first-presentation-quality.md) | Operator-first experience with presentation quality as a first-class concern | Product redesign |
| [0020](0020-alert-incident-domain-separation.md) | Separate first-class Alert and Incident lifecycles | Product redesign / alerting |
| [0021](0021-provider-backed-unified-experience.md) | Native CloudOps experience backed by specialized cloud-native provider engines | Product redesign / integrations |
| [0022](0022-native-cloudops-session-boundary.md) | One session for native CloudOps workspaces, excluding provider-console sessions | Product redesign / identity |
| [0023](0023-single-owner-product-identity.md) | One Owner with full product access and retained mutation safeguards | Product redesign / identity |
| [0024](0024-local-owner-runtime-boundary.md) | Loopback-only Local Owner Mode without login or RBAC | Product redesign / local runtime |
| [0025](0025-cloud-native-agent-product-core.md) | Cloud-native operations and Agent as the core; GitOps as supporting capability | Product redesign / positioning |
| [0026](0026-dual-mode-cloud-native-agent.md) | Incident-bound Investigations plus context-scoped interactive Consultations | Product redesign / Agent |
| [0027](0027-workspace-information-architecture.md) | Ten native Workspaces with typed internal and provider Context Links | Product redesign / navigation |
| [0028](0028-chinese-first-product-language.md) | Chinese-first UI with canonical professional terms preserved | Product redesign / language |
| [0029](0029-live-operations-atlas-visual-system.md) | Three.js Live Operations Atlas with stable operator-first workspaces | Product redesign / visual system |
| [0030](0030-versioned-local-configuration.md) | Versioned runtime settings with write-only local secrets and no implicit restart | Product redesign / configuration |
| [0031](0031-single-cluster-operations-atlas.md) | One active cluster per real-resource Operations Atlas with progressive topology detail | Product redesign / topology |
| [0032](0032-responsive-shell-scroll-ownership.md) | Full-capability responsive navigation with one primary scroll owner per route | Product redesign / responsive shell |
| [0033](0033-explicit-alert-escalation.md) | Alert triage and optional policy-bound Incident escalation without assignment | Product redesign / alerting |
| [0034](0034-guided-and-expert-observability-queries.md) | Native guided and expert observability queries with bounded Agent execution | Product redesign / observability |
| [0035](0035-context-bound-agent-consultations.md) | Persistent global Agent Consultations with explicit immutable Context Snapshots | Product redesign / Agent |
| [0036](0036-tiered-agent-operation-authority.md) | Three authority levels for bounded Agent reads, reversible actions and exact external Plans | Product redesign / Agent actions |
| [0037](0037-visual-performance-budgets.md) | Numeric performance, accessibility and adaptive-motion targets for the visual system | Product redesign / quality |
| [0038](0038-vertical-feature-integration-validation.md) | Full-stack capability delivery with MCP-driven real integration acceptance | Product redesign / delivery |
| [0039](0039-cloud-native-agent-operational-loop.md) | One Observe-to-Verify loop across the cloud-native Evidence Plane and Agent | Product redesign / product throughline |
| [0040](0040-make-managed-local-lifecycle.md) | One encapsulated top-level Make interface for the complete local kind lifecycle | Product redesign / local operation |
| [0041](0041-live-and-real-scenario-data.md) | Truthful Live Mode plus explicit real cloud-native Demonstration Scenarios | Product redesign / data truth |
| [0042](0042-local-retention-backup-and-reset.md) | Durable domain history, bounded telemetry and backup-first local reset | Product redesign / data lifecycle |
| [0043](0043-explicit-cross-session-agent-knowledge.md) | Explicit scoped cross-session knowledge without hidden Agent memory | Product redesign / Agent knowledge |
| [0044](0044-native-owner-notifications.md) | Durable native notification inbox with selective browser attention cues | Product redesign / notifications |
| [0045](0045-single-v1-contract-and-semantic-code.md) | One V1 product contract with semantic, generation-free implementation names | Product redesign / naming and migration |
