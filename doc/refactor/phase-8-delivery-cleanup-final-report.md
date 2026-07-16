# CloudOps-Copilot V2 Phase 8 Final Testing, Delivery Hardening and Safe Cleanup Report

- Date: 2026-07-16
- Repository: `/home/monody/k8s/CloudOps-Copilot`
- Branch: `main`
- Modification-before SHA: `0700093220a55e25c6ea7ae0c383aa75e3808b57`
- Scope: final local test matrix, delivery/supply-chain hardening, configuration/security regression and evidence-constrained cleanup
- Commit, push, PR, merge, Registry push, real signature, cluster write, staging and production operations: **NOT RUN**

## 1. Modification-before baseline

Before any modification:

```text
git rev-parse HEAD: 0700093220a55e25c6ea7ae0c383aa75e3808b57
git branch --show-current: main
git status --short: no output
```

The latest commit was `0700093 feat: add the incident workbench and safe incident-scoped realtime views`; it was followed by independent Phase 6 through Phase 1 commits. Phase 7 was therefore an independent commit and the working tree was clean. No stash, reset, checkout, commit, push or history rewrite occurred.

## 2. Reading and source inspection scope

The pass read the original 709-line cloud-native/Agent audit, the complete 2,989-line V2 implementation specification, every Phase 1-7 final report and all three Phase 3 supplemental reports, progress, migration ledger, risk register, cleanup inventory, ADR 0010-0037, all six SQL migrations, current CI/Docker/Compose/Helm/raw YAML/Shell/Makefile surfaces, all four Go modules, the frontend package/router/API/store/views/tests and the current Incident-to-Postmortem implementation.

Caller inspection covered Alerts/History, Diagnosis/Feedback, Copilot/Session, PendingAction/Action, AuditLog, Kubernetes dashboard, general V2 DTOs, Workbench DTOs, global WebSocket and Incident SSE. Current routes, imports, repositories and state-machine paths were treated as authoritative.

## 3. Report/specification and current-source deviations

- The old specification numbers progressive delivery as Phase 8, frontend consolidation as Phase 9, hardening as Phase 10 and deletion as Phase 11. The current approved Phase 8 prompt supersedes that numbering and does not authorize Argo Rollouts or a Phase 9.
- The old specification assumes a root Go module, outbox/inbox cutover, official observability replacements and V2-only traffic before deletion. Current source still has four modules, no outbox relay/inbox cutover, legacy observability deployments and active V1/V2 compatibility callers.
- Phase 7 described an eight-attempt SSE reconnect bound, but source reset the counter after every successful connection and could reconnect indefinitely; clean EOF also skipped query resynchronization.
- Phase 7 recorded URL-token WebSocket compatibility and the original audit recorded a tracked kubeconfig. Both security defects still existed in current source.
- Phase 7 added frontend tests, but current CI did not run them or an explicit strict typecheck. CI also lacked Go build, uncached tests, Helm template/schema, actionlint, kubeconform, full Shell checks, vulnerability scanning, SBOM, signing and digest deployment.
- The deploy workflow pushed `latest`, deployed SHA tags, and disabled provenance/SBOM. Current Helm templates had no digest input.
- Existing Incident Agent settings were implemented in Go but absent from Compose/Helm delivery surfaces and `.env.example`; Phase 5/6 `.env` settings were also undocumented.
- Existing `doc/` remains ignored by `.gitignore`; required report/ADR/ledger files exist locally but do not appear in `git status --short`. This existing repository behavior was preserved to avoid surfacing every historical Phase 1-7 document as new Phase 8 work.
- Docker daemon/buildx is now available, unlike earlier reports, but application Build/Inspect was blocked by repeated Docker Hub BuildKit-frontend timeouts. MySQL available locally was 8.0.45 rather than the earlier 8.0.46 binary.

## 4. Actual modification scope

- Hardened WebSocket authentication, Incident SSE recovery, frontend route contracts and associated Go/frontend tests.
- Removed the tracked generated kubeconfig and ignored future generated copies.
- Upgraded/pinned Go 1.26.5, quic-go 0.59.1 and remediated the npm lockfile; added repeatable vulnerability gates.
- Expanded CI across Go, frontend, Helm, Compose, workflow, Shell, manifests, SBOM, vulnerability scan and release controls.
- Pinned all application Dockerfile builder/runtime/frontend bases and BuildKit frontend by digest.
- Added Helm digest-first image rendering and `values.schema.json`; release deploy consumes actual pushed digests.
- Added existing Incident Agent configuration to Compose/Helm and Phase 2/5/6 settings to `.env.example`.
- Removed one duplicate indirect Go requirement proven redundant by `go mod tidy -diff`.
- Updated README, ADR 0038, Workbench architecture, progress, ledger, risk register and cleanup matrix.

No migration, state-machine transition, Provider query language, Kubernetes write interface, business route, API, service, repository, table or historical record was removed.

## 5. Cleanup decision matrix

The complete caller/evidence/rollback matrix is `doc/refactor/phase-8-cleanup-candidates.md`.

| Candidate group | Decision |
| --- | --- |
| Alerts / Alert History | `BLOCKED_BY_STAGING_EVIDENCE` |
| Diagnosis / DiagnosisReport / feedback | `RETAIN_COMPATIBILITY` |
| Copilot chat / sessions | `RETAIN_COMPATIBILITY` |
| PendingAction / Actions | `DEPRECATE_ONLY` |
| Audit Logs | `RETAIN_COMPATIBILITY` |
| Standalone Kubernetes dashboard/client | `RETAIN_COMPATIBILITY` |
| General V2 DTOs | `RETAIN_COMPATIBILITY` |
| Global WebSocket stream | `BLOCKED_BY_STAGING_EVIDENCE` |
| Legacy services/repositories/adapters | `RETAIN_COMPATIBILITY` |
| Compose/raw YAML/Helm/observability duplicates | `BLOCKED_BY_STAGING_EVIDENCE` |
| Duplicate indirect Go requirement | `DELETE` |
| Tracked generated kind kubeconfig | `DELETE` |

## 6. Deleted items and evidence

1. `server-monitor/docker/kubeconfig`: contained certificate authority data, client certificate data and client key data; setup code regenerates it and no source caller needs a committed copy. `.gitignore` now excludes it. Rollback is `make k8s-setup` for a local cluster; the credential must never be restored to Git.
2. Duplicate indirect `go.yaml.in/yaml/v3 v3.0.4` entry: the same module/version was already a direct requirement. `go mod tidy -diff` removed only the duplicate declaration and all four-module gates passed. Rollback is another reviewed `go mod tidy`, not manual dependency duplication.

No historical table/data was dropped.

## 7. Retained and deferred items

All business legacy candidates remain. Static callers prove that Diagnosis links to AlertHistory, Feedback and PendingAction; Copilot has distinct sessions; AuditLog has separate compliance semantics; the Kubernetes client is shared with Workbench; general V2 DTOs serve non-Workbench contracts; global WebSocket messages still carry alert/host/diagnosis/action/K8s updates; and Compose/raw YAML/Helm remain documented local/deployment inputs.

Traffic, subscriber, compliance, backfill, cutover and credentialed staging evidence is absent. Destructive cleanup is therefore deferred rather than guessed.

## 8. Legacy compatibility result

All legacy page paths, deep links, V1 APIs, V2 general APIs, status codes, admin route guards, tables and message payloads remain. Frontend route tests assert Workbench and legacy paths plus admin-only action/audit pages.

The `/ws/alerts` route and message contract remain, but browser authentication now sends the JWT through the `cloudops-bearer` WebSocket subprotocol. Authorization-header clients remain supported. Query-string JWT authentication is intentionally rejected because URL credential exposure is a demonstrated vulnerability. This is a security migration, not a route deletion.

## 9. Tests added or strengthened

- Go middleware contract: Authorization header and bearer subprotocol succeed; query token fails without reaching token authentication.
- Frontend WebSocket contract: token is absent from URL and present only in subprotocols.
- Incident SSE: foreign/duplicate/stale suppression, parser rejection, exact retry delays and eight-attempt limit.
- Frontend route contract: Incident and all cleanup-candidate deep links remain; action/audit routes remain admin-only.
- Existing handler tests continue to cover Workbench auth/RBAC/public UUID/bounds/sensitive-field exclusion.
- CI now executes the tests uncached and adds vulnerability/supply-chain gates.

## 10. Complete command/result matrix

### Four Go modules

Executed from each of `server-probe`, `server-web`, `alert-service`, and `pkg` under auto-selected `go1.26.5`:

| Command | Result |
| --- | --- |
| `go test -count=1 ./...` | PASS in all four modules |
| `go test -race -count=1 ./...` | PASS in all four modules |
| `go vet ./...` | PASS in all four modules; no output |
| `go build ./...` | PASS in all four modules; no output |
| `golangci-lint run ./...` | PASS in all four modules; no findings |
| `goimports -l $(rg --files -g '*.go')` | PASS; no output |
| `go mod tidy -diff` | PASS; no output after redundant requirement removal |
| `go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...` | PASS in all four modules; zero reachable vulnerabilities |

An earlier govulncheck correctly failed on local Go 1.26.1 standard-library vulnerabilities, then on quic-go 0.59.0. Go was pinned to 1.26.5 and quic-go upgraded to 0.59.1; the entire matrix and all four scans were rerun and passed.

### Frontend

| Command | Result |
| --- | --- |
| `npm ci` | PASS; 292 packages from lockfile; deprecation warnings recorded |
| `npm audit --audit-level=high --registry=https://registry.npmjs.org` | PASS; 0 vulnerabilities |
| `npm run lint` | PASS |
| `npx vue-tsc --noEmit` | PASS; no output |
| `npm test` | PASS; 5 files, 18 tests |
| `npm run build` | PASS; Vite 7.3.6, 2,536 modules |

The first audit through the configured npm mirror failed because that mirror does not implement the advisories endpoint. The official endpoint then found six advisories, including three high; `npm audit fix` updated the lockfile, and a fresh `npm ci` plus final audit/test/build passed. Existing ESLint/dependency deprecation and large-chunk/PURE-comment build warnings remain non-fatal risks.

### Delivery/static checks

| Command / tool | Result |
| --- | --- |
| `helm lint server-monitor/charts/server-monitor` | PASS; icon recommendation only |
| `helm template cloudops ...` default and digest values | PASS; three application images rendered as `repository@sha256` |
| Helm `values.schema.json` validation | PASS through Helm lint/template |
| `docker compose --env-file server-monitor/.env.example ... config --quiet` | PASS |
| `bash -n` for every repository `.sh` | PASS |
| `shellcheck` for every repository `.sh` | PASS |
| PyYAML parse of workflow, Compose and 20 raw manifests | PASS; 22 files |
| pinned `actionlint v1.7.7` through `go run` | PASS; no output |
| pinned `kubeconform v0.6.7` through `go run` | PASS; Helm 58/58 and raw 60/60 valid |
| pinned Prometheus 2.51 `promtool check config/rules` | PASS; config and all three rule files |
| secret/URL-token source scan | PASS after credential/auth remediation |
| `git diff --check` | PASS; no output |

Host `actionlint` and `kubeconform` binaries were absent and are recorded as host-binary `NOT RUN`; the same pinned tools were executed through Go as explicit alternatives. Host `syft`, `trivy` and `cosign` were absent; their controlled CI positions were implemented but no local signature/scan claim is made.

## 11. MySQL migration validation

`MYSQL_MIGRATION_NOT_APPLICABLE`: Phase 8 introduces no schema change and latest remains migration 00006.

An actual disposable MySQL `8.0.45` container on a random localhost port still validated:

- `0 -> 6`: PASS;
- `6 -> 5`: PASS;
- `5 -> 6`: PASS;
- repeated up at 6: PASS, no migrations to run;
- race repository integration: PASS in 5.512s;
- migration/repository/foreign-key/transaction/idempotency/concurrent ingestion/approval hash/lease takeover/stale writer/Verification/Postmortem/recovery/down-to-zero: PASS.

The container used an empty test-only schema, was never connected to staging/production, and was stopped/deleted after the test.

## 12. CI/CD and supply-chain result

- Go matrix now includes pinned Go 1.26.5, uncached tests/race, vet, build, imports, lint and pinned govulncheck.
- Frontend runs clean install, official audit, lint, strict typecheck, unit tests and production build.
- CI validates Compose, all Shell, workflow YAML/actionlint, Helm schema/lint/template, rendered/raw Kubernetes schema and Prometheus rules.
- Application image jobs verify OCI revision/source/version, scan HIGH/CRITICAL vulnerabilities and upload SPDX JSON SBOM artifacts.
- Release image publication runs only for `v*` tag events, pushes only the commit-SHA image tag, emits max provenance and SBOM attestations, and performs keyless Cosign signing.
- Deploy requires the protected `production` environment and valid actual digests downloaded from release jobs; mutable tags are not passed to Helm.

The workflow was parsed/linted locally but was not dispatched.

## 13. Image provenance status

All Dockerfile BuildKit frontend, Go 1.26.5 builder, Node 20 builder, Alpine 3.21 runtime and Prometheus 2.51 source images are digest pinned. Runtime stages remain non-root and contain application binary/static/runbook assets plus explicitly required CA/tz/wget and server-web promtool, not compilers or source trees.

Docker daemon/buildx was available (`29.3.0`, BuildKit `v0.28.0`, linux/amd64). Three local application image builds were attempted twice with non-misleading `uncommitted-phase8` labels. Both attempts failed before Dockerfile execution because Docker Hub timed out fetching the BuildKit frontend over IPv6. Therefore application image ID, RepoDigest, OCI label inspection, runtime user/entrypoint/architecture inspection and current-worktree provenance are **NOT RUN / environment blocked**.

The current Phase 8 worktree is uncommitted by explicit prohibition, so no application image could honestly claim exact current-source commit provenance even if the pull succeeded.

```text
PHASE_8_IMAGE_PROVENANCE_VERIFIED: NOT SET
```

## 14. Helm, Compose and Kubernetes validation

Helm lint/schema/template, digest rendering, raw/Helm kubeconform, YAML parsing, Compose config and Prometheus configuration all pass. Existing probes and non-root security contexts remain. Incident Agent worker settings now exist consistently in Go, Compose and Helm; Phase 5/6 `.env` examples now match current Go defaults.

`kind` is not installed and `kubectl config current-context` reports no current context. No cluster request or write occurred.

```text
PHASE_8_LOCAL_KIND_E2E_COMPLETE: NOT SET
Local kind E2E: NOT RUN
```

## 15. Local E2E status

No deterministic fully local adapter currently models the entire GitHub merge -> Argo CD sync -> Kubernetes rollout chain without faking external success, and kind is unavailable. No approval, provenance or external delivery step was bypassed. Unit/provider/MySQL/Helm/manifest gates are not represented as full business E2E.

## 16. Security regression

- Removed a tracked kubeconfig containing client-key material and blocked future generated copies from Git.
- Removed browser JWTs from WebSocket URLs and rejected query-token authentication.
- Preserved Workbench Authorization-header SSE, protected routes and admin RBAC.
- Confirmed no Workbench mutation route, verdict recomputation, raw Provider response/query, prompt/checkpoint/private reasoning, numeric control ID or credential exposure was added.
- Confirmed Kubernetes dashboard remains authenticated GET-only at the HTTP route boundary; legacy direct restart/scale remains separately admin/feature gated and is retained as compatibility, not made available to Workbench/Agent.
- npm audit reports zero vulnerabilities after lockfile remediation.
- Pinned govulncheck reports zero reachable vulnerabilities after Go/quic-go remediation.
- CI now fail-closes on Go/npm/image vulnerability findings and supply-chain gates.

This is a source/configuration/regression audit, not a professional penetration test. Existing sample local passwords are development defaults, not presented as production secrets.

## 17. Known risks

- All credentialed provider, multi-replica, disconnect soak, traffic/subscriber and compliance evidence remains absent.
- Current source has no immutable commit/image provenance.
- Local application Build/Inspect is blocked by Registry networking.
- No local kind or Kubernetes context exists.
- Vite still reports a large main chunk; npm reports deprecated tooling packages despite zero advisories.
- Four Go modules, legacy AutoMigrate, outbox without relay/inbox, duplicate deployment/observability paths and active legacy products remain by evidence-constrained decision.
- WebSocket query-token clients must migrate to Authorization header or bearer subprotocol.

## 18. Production blockers

1. Review and commit the Phase 8 source under separate authorization.
2. Run hosted/local application image build, scan, SBOM/provenance/signature and inspect against that exact commit.
3. Validate the Pod digest -> Registry OCI -> Argo revision -> GitHub commit chain.
4. Run credentialed least-privilege GitHub, Registry, Argo CD, Kubernetes, Prometheus/Loki/Tempo and external-model staging E2E.
5. Run multi-replica lease/takeover and Workbench realtime soak.
6. Rehearse protected release, digest deploy, migration/lease drain and rollback.
7. Collect staging traffic/subscriber/compliance evidence before destructive cleanup.

Production release remains blocked.

## 19. Rollback procedure

1. Revert Phase 8 source as a reviewed change; do not use reset/checkout against an unreviewed worktree.
2. Disable release/deploy jobs if supply-chain infrastructure is unavailable; keep the last known immutable digest deployed.
3. Helm tag fallback may support local development only; do not restore mutable production deployment.
4. Keep every legacy business route/API/table active as the Workbench rollback surface.
5. Revert SSE/client refactors if needed, but retain a finite resync policy.
6. Do not restore the committed kubeconfig or URL-query JWT behavior. Regenerate a local kubeconfig with `make k8s-setup` and migrate WebSocket clients to the supported auth transport.
7. No database rollback is required because Phase 8 adds no migration. If rolling Phase 6 schema down separately, follow ADR 0036 export/drain requirements.

## 20. `git diff --stat`

Tracked-file output before this ignored report was written:

```text
 .github/workflows/ci.yaml                          | 191 +++++++++++--
 .gitignore                                         |   3 +
 README.md                                          |  20 +-
 server-monitor/.env.example                        |  53 ++++
 server-monitor/alert-service/Dockerfile            |   6 +-
 server-monitor/alert-service/go.mod                |   2 +-
 .../charts/server-monitor/templates/_helpers.tpl   |   9 +
 .../server-monitor/templates/alert-service.yaml    |   2 +-
 .../charts/server-monitor/templates/configmap.yaml |  15 +
 .../server-monitor/templates/server-probe.yaml     |   2 +-
 .../server-monitor/templates/server-web.yaml       |   2 +-
 server-monitor/charts/server-monitor/values.yaml   |  20 ++
 server-monitor/docker-compose.yml                  |  15 +
 server-monitor/docker/kubeconfig                   |  19 --
 server-monitor/frontend/package-lock.json          | 304 ++++++++++++---------
 .../incidents/useIncidentRealtime.test.ts          |  20 +-
 .../composables/incidents/useIncidentRealtime.ts   |  16 +-
 .../frontend/src/composables/useAlertsWebSocket.ts |   8 +-
 .../src/composables/useK8sEventsWebSocket.ts       |   8 +-
 server-monitor/frontend/src/router/index.ts        | 200 +-------------
 server-monitor/pkg/go.mod                          |   2 +-
 server-monitor/server-probe/Dockerfile             |   6 +-
 server-monitor/server-probe/go.mod                 |   2 +-
 server-monitor/server-web/Dockerfile               |  10 +-
 server-monitor/server-web/go.mod                   |   5 +-
 server-monitor/server-web/go.sum                   |   4 +-
 .../server-web/internal/infra/websocket/hub.go     |   1 +
 .../server-web/internal/middleware/auth.go         |  17 +-
 28 files changed, 541 insertions(+), 421 deletions(-)
```

Untracked additions are excluded from `git diff --stat`: Helm schema, WebSocket auth implementation/test, route records/test and Go middleware auth test. `doc/` changes and this report are ignored by the existing repository rule.

## 21. `git status --short`

```text
 M .github/workflows/ci.yaml
 M .gitignore
 M README.md
 M server-monitor/.env.example
 M server-monitor/alert-service/Dockerfile
 M server-monitor/alert-service/go.mod
 M server-monitor/charts/server-monitor/templates/_helpers.tpl
 M server-monitor/charts/server-monitor/templates/alert-service.yaml
 M server-monitor/charts/server-monitor/templates/configmap.yaml
 M server-monitor/charts/server-monitor/templates/server-probe.yaml
 M server-monitor/charts/server-monitor/templates/server-web.yaml
 M server-monitor/charts/server-monitor/values.yaml
 M server-monitor/docker-compose.yml
 D server-monitor/docker/kubeconfig
 M server-monitor/frontend/package-lock.json
 M server-monitor/frontend/src/composables/incidents/useIncidentRealtime.test.ts
 M server-monitor/frontend/src/composables/incidents/useIncidentRealtime.ts
 M server-monitor/frontend/src/composables/useAlertsWebSocket.ts
 M server-monitor/frontend/src/composables/useK8sEventsWebSocket.ts
 M server-monitor/frontend/src/router/index.ts
 M server-monitor/pkg/go.mod
 M server-monitor/server-probe/Dockerfile
 M server-monitor/server-probe/go.mod
 M server-monitor/server-web/Dockerfile
 M server-monitor/server-web/go.mod
 M server-monitor/server-web/go.sum
 M server-monitor/server-web/internal/infra/websocket/hub.go
 M server-monitor/server-web/internal/middleware/auth.go
?? server-monitor/charts/server-monitor/values.schema.json
?? server-monitor/frontend/src/api/websocketAuth.test.ts
?? server-monitor/frontend/src/api/websocketAuth.ts
?? server-monitor/frontend/src/router/routes.test.ts
?? server-monitor/frontend/src/router/routes.ts
?? server-monitor/server-web/internal/middleware/auth_test.go
```

## 22. Final gate checklist

- [x] Clean Phase 7 commit baseline recorded
- [x] Current source and every required report/spec/ADR/deployment/module/frontend/migration surface inspected
- [x] Report/source deviations recorded with source taking priority
- [x] Every cleanup candidate has callers, parity, test, runtime evidence, rollback and decision
- [x] No destructive business/schema/history cleanup without evidence
- [x] WebSocket URL credential and tracked kubeconfig vulnerabilities remediated
- [x] SSE sequence/reconnect/disconnect query rebuild tested
- [x] Legacy route/API/RBAC/message compatibility retained except unsafe query-token transport
- [x] Four Go module final matrix passed under patched Go 1.26.5
- [x] Frontend clean install/audit/lint/type/test/build passed
- [x] Disposable MySQL full chain and integration passed; no schema change
- [x] Helm/Compose/YAML/actionlint/kubeconform/Shell/promtool/diff gates passed
- [x] CI release uses immutable commit/digest and controlled SBOM/scan/signing positions
- [ ] Current-worktree application image Build/Inspect/provenance — environment blocked and source uncommitted
- [ ] Local kind E2E — kind/context unavailable
- [ ] Credentialed staging/providers/model/multi-replica/soak — not authorized/not run
- [ ] Production release — blocked
- [x] No commit, push, PR, merge, Registry push, signature, deployment or cluster write performed
- [x] No Phase 9 started

## 23. Final status

```text
PHASE_8_LOCAL_IMPLEMENTATION_COMPLETE
PHASE_8_TEST_MATRIX_COMPLETE
PHASE_8_DELIVERY_HARDENING_COMPLETE
PHASE_8_SAFE_CLEANUP_COMPLETE
V2_LOCAL_DEVELOPMENT_COMPLETE
MYSQL_MIGRATION_NOT_APPLICABLE
PHASE_8_CREDENTIALED_STAGING_NOT_RUN
PHASE_8_DESTRUCTIVE_CLEANUP_DEFERRED
PRODUCTION_RELEASE_BLOCKED
```

The following statuses are intentionally not emitted:

```text
PHASE_8_LOCAL_KIND_E2E_COMPLETE
PHASE_8_IMAGE_PROVENANCE_VERIFIED
PHASE_8_CREDENTIALED_STAGING_COMPLETE
PRODUCTION_RELEASE_READY
```
