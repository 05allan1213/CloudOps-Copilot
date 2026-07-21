# V3 Architecture Decision Records

These ADRs freeze target decisions for Phase 0. Status Accepted means the target decision is approved; it does not mean the implementation, live integration or later Gate has passed.

The sole normative design remains ../CloudOps-Incident-Agent-V3-Refactor-Design.md. If an ADR and that design differ, the design wins until both are explicitly reconciled.

| ADR | Decision | Owning Gate |
|---|---|---|
| [0001](0001-v3-product-boundary.md) | V3 product boundary and one Incident flow | Phase 0 / final DoD |
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
