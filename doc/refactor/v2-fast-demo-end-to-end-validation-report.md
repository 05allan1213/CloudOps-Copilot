# CloudOps-Copilot V2 Fast Demo End-to-End Validation Report

- Date: 2026-07-16
- Result: `V2_DEMO_END_TO_END_COMPLETE`
- Repository: `/home/monody/k8s/CloudOps-Copilot`
- Branch: `main`
- Base exact SHA: `2acec15910fd8c8605eb34990423c5c029f820d8`
- Scope: one disposable, non-production Incident lifecycle demo; stopped after the first complete scenario

## 1. Environment

| Item | Actual value / status |
| --- | --- |
| Kubernetes | local disposable kind, server `v1.36.1` |
| Context | `kind-cloudops-demo` |
| Namespace | `default` |
| Workload | `Deployment/cloudops-demo-workload`, final state `2/2 Ready` |
| Compose project | `server-monitor` |
| Frontend | embedded production build served by `server-web` |
| MySQL | real `mysql:8.0`, healthy; Goose migrations `00001`-`00006` applied |
| Redis | real `redis:7-alpine`, healthy |
| Kafka | real `confluentinc/cp-kafka:7.6.1`, healthy |
| Prometheus | real `prom/prometheus:v2.51.0`, healthy; demo scrape/rule configuration |
| Alertmanager | real `prom/alertmanager:v0.27.0`, healthy; demo webhook route |
| Tracing | real Jaeger `2.17.0` is running; Tempo Evidence was not used |
| Grafana / VictoriaMetrics | not required and not run for this scenario |
| Loki / Tempo / Argo CD | not run |

The deployment reused the repository Compose stack plus the minimal files under `server-monitor/docker/fast-demo/`. Authentication, rate limiting and automatic alert-rule synchronization were disabled only in this disposable demo configuration. Kubernetes reads and the bounded write executor used the mounted kind kubeconfig and the `default` namespace allowlist.

## 2. Images and provenance boundary

The three application containers used immutable exact-SHA-derived local tags; no application `latest` tag was used:

```text
cloudops-demo/server-probe:2acec15910fd8c8605eb34990423c5c029f820d8-demo5
cloudops-demo/alert-service:2acec15910fd8c8605eb34990423c5c029f820d8-demo5
cloudops-demo/server-web:2acec15910fd8c8605eb34990423c5c029f820d8-demo5
```

The demo workload used:

```text
cloudops-demo/workload:2acec15910fd8c8605eb34990423c5c029f820d8
```

The application image labels and tag are anchored to the base SHA. The fast-demo-only blocker fixes are an uncommitted local worktree overlay in these disposable images, so this report does not claim formal release provenance for `-demo5`. The previously completed hosted supply-chain proof was not rerun.

## 3. Scenario and real signal

One scenario was executed:

```text
healthy Deployment at 2 replicas
-> scale Deployment to 0
-> Prometheus target becomes unhealthy
-> Alertmanager sends a real firing alert
-> Incident is created
-> bounded Agent investigation reads Kubernetes Evidence
-> human approval of an Evidence-bound replica restoration plan
-> controlled direct Kubernetes execution restores 2 replicas
-> Prometheus returns to up=1 and Alertmanager persists a resolved Signal
-> deterministic Verification passes
-> Incident resolves and Postmortem is generated
```

Initial state was `2/2 Ready`, Prometheus `up=1`, and the Incident list was empty. The fault was injected by scaling the real kind Deployment to zero; no Incident, AgentRun, Evidence, approval, Delivery, Verification or resolved state was created through direct database modification.

## 4. Incident lifecycle evidence

| Artifact | ID / result |
| --- | --- |
| Incident | `c8f62a9b-66bb-4465-951e-c67848052992`, final `RESOLVED` |
| AgentRun | `d1c8acb3-0b0b-4c51-b90b-32003be67743`, `COMPLETED` |
| EvidenceItem | `3c3b0e66-615f-4a77-9b14-374c140f955e` |
| RemediationPlan | `b1cce943-f895-40b6-86c2-81f33ba9e751` |
| ChangeRequest | `419f7ea3-498c-4b33-8848-7137f2f2f29b`, `delivered` |
| VerificationRun | `6372448b-8ab7-488a-a711-2ecefb631980`, `passed` |
| Postmortem | `e949c5ef-abfd-4950-99a6-6c7293dbc213` |

The Incident was created from the persisted Alertmanager firing Signal. Signal and Timeline APIs show detection, Incident creation, Agent start, diagnosis, planning, approval, delivery, resolved Signal, three Verification checks and final resolution in order.

The `fast-demo-deterministic` model still ran the durable Agent graph. It invoked the existing read-only `k8s.get_deployments` tool and persisted one real Evidence item for `kind-cloudops-demo/default`; the observed Deployment had zero ready workload capacity. The non-degraded diagnosis reported 0.95 confidence and cited Evidence `3c3b0e66-615f-4a77-9b14-374c140f955e` in both its hypothesis and confirmed fact.

The Evidence-bound, low-risk plan proposed `set_replicas: 2` for `default/Deployment/cloudops-demo-workload`. The existing approval API recorded explicit approval of the exact plan and patch at `2026-07-16T14:50:50.313151Z`; the safe Workbench DTO intentionally exposes the actor as `Unknown`. The controlled executor then changed the real Kubernetes Deployment. Delivery recorded generation `9`, observed generation `9`, desired/updated/available replicas `2/2/2`, unavailable replicas `0`, and exact target revision `2acec15910fd8c8605eb34990423c5c029f820d8`.

## 5. Recovery and Verification

Before aggregation completed, the Incident remained `VERIFYING` with `resolved_at=null`; the resolved Alertmanager Signal did not prematurely close it. Verification then passed all three required real checks:

| Check | Result |
| --- | --- |
| `deployment_rollout` | PASS |
| `workload_ready` | PASS |
| `alert_resolved` | PASS |

Verification timing was valid:

```text
started_at:   2026-07-16T14:51:43.476925Z
completed_at: 2026-07-16T14:51:45.330422Z
deadline_at:  2026-07-16T14:55:52.349990Z
```

After aggregation, the Incident transitioned to `RESOLVED`, the Postmortem was generated, Kubernetes remained `2/2 Ready`, the workload returned HTTP 200, and Prometheus again reported `up{job="cloudops-demo-workload"}=1`.

## 6. UI and API confirmation

- Incident list: `http://localhost:8080/incidents`
- Incident detail: `http://localhost:8080/incidents/c8f62a9b-66bb-4465-951e-c67848052992`
- Workload port-forward: `http://localhost:18082/`

The list and detail routes returned HTTP 200. The detail, Signals, Timeline, Evidence, Agent Investigation, Remediation/Approval, Delivery and Verification Workbench APIs, plus the Postmortem API, each returned HTTP 200 for this Incident. The browser Workbench therefore exposes the complete persisted process rather than a static screenshot.

## 7. Demo blockers fixed

Only main-chain blockers were changed:

- Added a default-off credential-free deterministic Agent model for the demo while retaining durable AgentRun, graph steps, tool execution and Evidence persistence.
- Added default-off fast-demo plan/execute/verify endpoints and wired controlled direct execution through the existing bounded Kubernetes scale executor.
- Added a direct-mode Verification profile that requires real rollout, readiness and resolved-Signal evidence without making false GitHub/Argo assertions.
- Allowed Agent start to transition `CORRELATING` to `DIAGNOSING`.
- Prevented a resolved Signal from closing Incidents prematurely during `APPLYING_CHANGE` or `VERIFYING`.
- Added Verification deadline protection and a bounded five-minute demo timeout.

All paths are gated by `FAST_DEMO_ENABLED=false` by default. No production release behavior was enabled.

## 8. Minimal validation

| Scope | Result |
| --- | --- |
| Affected Go packages | PASS |
| Frontend ESLint | PASS |
| Frontend typecheck | PASS |
| Frontend production build | PASS; existing large-chunk advisory only |
| Goose migrations `00001`-`00006` | PASS |
| Merged Compose configuration | PASS |
| Prometheus configuration and demo alert rule | PASS |
| Runtime Compose health | PASS |
| Final kind workload readiness | PASS, `2/2` |
| One complete demo E2E | PASS |
| `git diff --check` | PASS |

## 9. Fallbacks and intentionally not-run work

Argo CD was not available on the shortest usable path. The demo therefore used the existing bounded Kubernetes mutation capability behind explicit approval:

```text
DEMO_USED_CONTROLLED_DIRECT_EXECUTION
GITOPS_FULL_STAGING_NOT_RUN
```

Loki Evidence, Tempo Evidence, external model credentials, non-core notifications and Grafana visualization were not required. No additional fault scenario was run.

The following production-level work was intentionally not run: production release, formal GHCR lifecycle closure or referrer cleanup, new hosted build/scan/SBOM/provenance/signature pass, formal `v*` tag/release, full race suite, all-module lint, long soak/stability window, HA/PDB/TLS/domain/production Secret validation, broad security/performance/DR audit, and production deployment gates. The known GHCR OCI referrer residual remains non-blocking technical debt.

## 10. Final acceptance

```text
UI_ACCESSIBLE
INCIDENT_CREATED_FROM_REAL_SIGNAL
AGENT_RUN_COMPLETED
EVIDENCE_VISIBLE
REMEDIATION_APPROVED
CHANGE_DELIVERED
WORKLOAD_RECOVERED
VERIFICATION_PASSED
INCIDENT_RESOLVED
DEMO_USED_CONTROLLED_DIRECT_EXECUTION
GITOPS_FULL_STAGING_NOT_RUN
V2_DEMO_END_TO_END_COMPLETE
```

The requested single end-to-end demonstration is complete. The disposable environment is intentionally left running for direct inspection; no production hardening, GHCR lifecycle work, release, publication or extra scenario follows this report.
