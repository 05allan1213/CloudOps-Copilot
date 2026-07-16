# V2 Migration Ledger

Decision vocabulary: `REUSE_AS_IS`, `ADAPT`, `REWRITE`, `REPLACE_WITH_COMPONENT`, `DELETE`, and `DEFER`.

| Legacy module | Decision | Current treatment | Retained compatibility surface | Later gate / deletion condition |
| --- | --- | --- | --- | --- |
| AlertHistory | REPLACE_WITH_COMPONENT | No new change; Incident remains additive | V1 history model, API and queries | Backfill, reconciliation, traffic cutover and rollback window |
| Copilot Chat | REUSE_AS_IS | New Agent API is separate; no chat/session behavior changed | `/api/v1/copilot/**`, classifier, summarizer | Product-approved convergence only |
| Redis Session | REUSE_AS_IS | Agent checkpoints are MySQL-backed and do not use chat session state | `internal/copilot/session`, Redis TTL/session APIs | No deletion planned in Phase 2 |
| Diagnosis Worker | ADAPT | Added independent DB-poll Agent Worker; Kafka worker remains | `InitDiagnosisConsumer`, Redis dedupe, Kafka group | V2 production soak and explicit cutover plan |
| DiagnosisReport | REPLACE_WITH_COMPONENT | Added structured, Evidence-bound `AgentRun.final_diagnosis`; no backfill | Diagnosis repository, API, feedback linkage | Parity, backfill, consumer migration, rollback closure |
| Tool Registry | ADAPT | Reused registry execution/validation behind a stricter Phase 2 read-only adapter | Existing Copilot schemas and execution | Maintain shared registry; no deletion |
| Runbook RAG | REUSE_AS_IS | `runbook.search` is available only when registered and healthy enough to initialize | BM25/vector/RRF/reranker implementation | Re-evaluate retrieval quality separately |
| Kafka Consumer | DEFER | Agent Worker does not consume Kafka; no relay/inbox or topic change | Existing diagnosis consumer and retry policy | Dedicated relay/inbox design and lag/DLQ Gate |
| PendingAction | DEFER | No Agent graph edge or adapter can reach actions | PendingAction model and approval API | Phase 4 controlled remediation design |
| Action Executor | DEFER | Explicitly excluded from Agent dependencies and allowlist | Existing action policy, approval and executor | Policy/audit/rollback proof in a later phase |
| AuditLog | REUSE_AS_IS | Agent adds bounded logs and Incident timeline; security audit remains unchanged | AuditLog model and action audit semantics | Compliance mapping before any replacement |
| GitHub change reads | ADAPT | Fixed-host read adapter and GitHub App/token-file auth feed deterministic Change facts | No repository mutation path | Credentialed staging E2E before production enablement |
| Argo CD | ADAPT | REST GET Application/history/resource tree behind Application/Project allowlists | Existing deployment remains externally owned | Credentialed staging E2E before production enablement |
| Image revision metadata | ADAPT | Docker/Actions stamp OCI labels; fixed-host Registry reader verifies manifest/config by runtime digest and feeds exact OCI/Argo/GitHub resolution | Existing image names, build contexts and bounded Change metadata | Credentialed Registry/deployment staging parity check |
| Registry/OCI metadata reads | ADAPT | New GET-only immutable manifest/config adapter with exact allowlists, file auth, bounded cache/retry/telemetry and no model-facing Registry input | No image layer, tag-list or write surface; no schema change | Credentialed staging E2E and least-privilege review |
| Change correlation | REWRITE | New pure deterministic scoring/exclusion domain; no LLM authority | Incident and Agent contracts unchanged | Recalibrate only with reviewed fixture evidence |
| GitOps writer / PR creation | REPLACE_WITH_COMPONENT | Phase 3 deferral is closed by a separate default-off Phase 4 writer; it is unreachable from Agent tools and still unvalidated against real GitHub | External delivery workflow unchanged | Real GitHub App/GitOps staging validation before enablement |
| Remediation planning / approval | REWRITE | Typed Evidence-bound plan, deterministic policy/hash and independent human approval | V1 PendingAction remains unchanged and is not reused as GitOps authority | Staging administrator workflow and model planner E2E |
| GitOps writer / Draft PR creation | REWRITE | Separate default-off GitHub writer, durable ChangeRequest lease and one-Draft-PR idempotency | Phase 3 GitHub read client and credentials remain isolated | Credentialed GitHub App staging E2E before production enablement |
| Delivery observation | ADAPT | Extends Phase 4 ChangeRequest with exact GitHub merge, Argo revision/sync/health and Deployment rollout facts; all provider access is read-only | Existing external delivery controllers remain authoritative | Credentialed staging exact-revision observation and timeout rehearsal |
| Recovery verification | REWRITE | New trusted compiler and deterministic Run/Check engine own recovery verdicts and Incident resolve/reinvestigate transitions | Agent has no verdict authority; V1 diagnosis/action paths remain | Staging stability-window, provider outage and multi-replica validation |
| Observability recovery checks | ADAPT | Fixed-template Prometheus, Loki and Tempo GET-only adapters feed the existing deterministic Run/Check engine | Legacy raw Prometheus query surface remains outside verdict authority; no backend deployment change | Credentialed provider contract and staging stability validation |
| Structured Postmortem | REWRITE | One deterministic, fact-classified, Evidence-referenced record is generated from persisted facts after a passed final attempt | Incident Timeline, Evidence and legacy reports remain | Product/UI presentation may begin only in Phase 7 |

## Phase 2 additions

- `REWRITE`: durable Agent runtime is a new graph/application domain, not a wrapper around the one-shot Copilot function-call path.
- `ADAPT`: existing MySQL, Gin auth, Tool Registry, LLM client, Prometheus and OpenTelemetry are used behind Phase 2-specific ports and policies.
- `REUSE_AS_IS`: V1/Copilot/Diagnosis/Action behavior and routes.
- `DELETE`: no item is approved for deletion.

## Phase 3 additions

- `ADAPT`: GitHub, Argo CD, Kubernetes runtime reads, OCI build metadata, Tool Registry, MySQL and Agent Evidence.
- `REWRITE`: deterministic Change correlation is a new bounded domain, not LLM reasoning.
- `REUSE_AS_IS`: V1, Phase 1 Incident, Phase 2 graph/checkpoint/lease, Copilot, Diagnosis and Action paths.
- `DEFER`: GitOps writes, PR creation, Argo CD Sync/rollback, Kubernetes remediation, verification execution and Phase 4.
- `DELETE`: no item is approved for deletion.

## Phase 4 additions

- `REWRITE`: remediation plan/policy/approval/delivery are new bounded application and domain contracts.
- `ADAPT`: Incident timeline/state, Phase 3 Evidence, MySQL lease pattern, Gin RBAC, Prometheus, Helm and Compose.
- `REUSE_AS_IS`: all V1 paths, Phase 1 Incident ingestion, Phase 2 Agent investigation, Phase 3 reads and the legacy PendingAction/Action executor.
- `DEFER`: merge, Argo CD sync/rollback, Kubernetes mutation, automatic recovery verification and Phase 5 delivery tracking.
- `DELETE`: no item is approved for deletion.

## Phase 5 additions

- `ADAPT`: Phase 4 ChangeRequest, Phase 1 Incident state/timeline/outbox, Phase 2 lease/optimistic concurrency pattern, and Phase 3 GitHub/Argo/Kubernetes reads.
- `REWRITE`: delivery state machine, trusted VerificationPlan compiler, deterministic VerificationRun/VerificationCheck execution and aggregate verdict.
- `REUSE_AS_IS`: V1, Agent investigation, Change Intelligence, remediation approval and external GitOps controllers.
- `DEFER`: metric/log/trace execution until typed bounded adapters exist; retry/cancel write APIs, auto-merge, active Argo/Kubernetes operations, rollback and Phase 6.
- `DELETE`: no item is approved for deletion.

## Phase 6 additions

- `ADAPT`: Phase 5 VerificationRun/VerificationCheck, lease/takeover/stability/aggregate, Incident transitions, Timeline/Outbox, provider HTTP conventions, MySQL and authenticated V2 routing.
- `REWRITE`: strict VerificationProfile compilation, typed observability evaluation and deterministic structured Postmortem generation.
- `REUSE_AS_IS`: all V1 paths, Agent investigation, Change Intelligence, remediation approval/delivery and external GitOps controllers.
- `DEFER`: credentialed staging provider checks, deployed revision provenance, production enablement, UI workbench and all Phase 7 work.
- `DELETE`: no item is approved for deletion.

## Phase 7 additions

- `ADAPT`: existing public Incident/Agent/Remediation/Delivery/Verification/Postmortem facts into dedicated browser-safe, bounded GET DTOs; existing typed Kubernetes list clients into related-resource summaries.
- `REWRITE`: Vue Incident list/detail composition, URL-restored filtering, request identity protection and Incident-scoped refresh-hint SSE client.
- `REUSE_AS_IS`: all legacy Alerts, Diagnosis, Copilot, Actions, Audit Logs and Kubernetes routes, stores, APIs and pages; existing auth/RBAC and domain state machines.
- `DEFER`: every deletion candidate, database/schema change, legacy redirect/cutover, provider capability expansion and all Phase 8 work.
- `DELETE`: no item is approved for deletion.

## Phase 8 additions

- `ADAPT`: CI now runs the complete four-module, frontend, Helm, Compose, workflow, shell, schema, vulnerability and supply-chain gates; release/deploy consumes immutable image digests.
- `ADAPT`: Helm supports digest-first application images with tag fallback for local compatibility; Compose/Helm now expose the existing default-off Incident Agent settings.
- `ADAPT`: the global WebSocket route and message contracts remain, while browser authentication moves from URL query tokens to a bearer subprotocol; Incident SSE disconnect recovery is finite and query-authoritative.
- `REUSE_AS_IS`: Alerts/History, Diagnosis/Feedback, Copilot/Session, PendingAction/Action, AuditLog, Kubernetes dashboard, general V2 DTOs, V1/V2 historical tables and duplicate deployment paths remain because parity or staging/compliance evidence is missing.
- `DELETE`: one duplicate indirect Go requirement and the tracked generated kind kubeconfig only. No business route, API, service, table, history or deployment definition is deleted.
- `DEFER`: all destructive legacy cleanup, deployment-source cutover, outbox/inbox/Kafka cutover, official observability replacement, credentialed staging, local kind E2E and production release.
