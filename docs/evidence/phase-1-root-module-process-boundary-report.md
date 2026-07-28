# Phase 1 Root Module and Process Boundary Report

> Normative source: [`docs/CloudOps-Incident-Agent-V3-Refactor-Design.md`](../CloudOps-Incident-Agent-V3-Refactor-Design.md)
>
> Scope: Phase 1 only, Root Module and process boundary
>
> Evidence is from the live, staged, uncommitted worktree. No commit or push was made.

## 1. Verdict

All Phase 1 implementation gates listed in the V3 design, ADR 0002, and the task request passed locally. The verdict is limited to this phase and to disposable local infrastructure. It is not a claim of Phase 2 readiness in the sense of async runtime, state conversion, external GitOps, kind, hosted CI, or production migration.

```text
PHASE_1_STATUS=PASS
ROOT_MODULE_UNIFIED=YES
THREE_BINARIES_BUILD=YES
API_WORKER_BOUNDARY_ESTABLISHED=YES
RUNTIME_AUTOMIGRATE_REMOVED=YES
BUSINESS_API_BEHAVIOR_CHANGED=NO
EXTERNAL_ENVIRONMENT_MUTATED=NO
READY_FOR_PHASE_2=YES
```

`API_WORKER_BOUNDARY_ESTABLISHED=YES` means the Phase 1 contract: API does not construct or link the legacy Agent/Remediation/Delivery/Verification worker loops, while Worker mechanically owns those legacy loops. The explicitly enabled legacy Fast Demo compatibility path can still initialize Kubernetes capability in API; this residual is recorded below and is not represented as final V3 credential hardening.

## 2. Provenance

### Initial live state

| Control | Value |
|---|---|
| Repository | `/home/monody/k8s/CloudOps-Copilot` |
| Branch | `main` |
| HEAD | `1ea0c3a21ed3ed1f822399f205afac225b1d5464` |
| Upstream | `origin/main` at `2f7e426d69a4ed7d8d32ec3ca83c13af0c71586e` |
| Ahead/behind | `0` behind, `1` ahead |
| Origin URL | `https://github.com/05allan1213/CloudOps-Copilot.git` |
| Initial worktree | clean, before this phase |

The expected baseline was therefore live and matched the requested branch/SHA. Existing ignored paths were preserved: `.codex/`, `server-monitor/docker/kubeconfig`, `server-monitor/frontend/node_modules/`, and `server-monitor/frontend/dist/`.

Before editing, the complete contents of all requested inputs were read: `docs/CloudOps-Incident-Agent-V3-Refactor-Design.md`, `docs/architecture.md`, `docs/migration-ledger.md`, `docs/risk-register.md`, `docs/adr/0002-root-module-processes.md`, and `docs/evidence/phase-0-baseline-audit-report.md`.

### Final live state

| Control | Value |
|---|---|
| Branch/HEAD | unchanged: `main@1ea0c3a21ed3ed1f822399f205afac225b1d5464` |
| Staged paths | 247 (including mechanical renames/deletions and this report) |
| Unstaged tracked paths | 0 |
| Untracked non-ignored paths | 0 |
| Commit/push | NOT RUN by design |
| External deployment/write | NOT RUN by design |
| Disposable MySQL | stopped after evidence collection |

The final worktree is intentionally dirty because the requested implementation is staged but not committed. The final `git diff --check` and `git diff --cached --check` were clean.

### Tool versions

| Tool | Live version/evidence |
|---|---|
| Go launcher | `go1.26.1`; selected toolchain `go1.26.5` (`go.mod` uses `1.26.5`) |
| Node/npm | Node `v24.13.0`, npm `11.6.2` |
| MySQL client/server used for evidence | `8.0.46-0ubuntu0.24.04.3` |
| Docker | client/server `29.3.0` |
| Docker Compose/Buildx | Compose `v5.1.0`, Buildx `v0.31.1` |
| goimports/golangci-lint | goimports module `golang.org/x/tools v0.43.0`, golangci-lint `2.11.4` |
| actionlint/shellcheck | actionlint `1.7.7`, shellcheck `0.10.0` |
| Helm/kubeconform/promtool | Helm `v3.14.4`, kubeconform `development`, promtool `2.51.0` |

## 3. Implementation

### Root module and mechanical move

- Created the only Go module at the repository root with module path `github.com/05allan1213/CloudOps-Copilot`.
- Mechanically moved `server-monitor/server-web/internal/**` to root `internal/**` and `server-monitor/server-web/migrations/**` to root `migrations/**`.
- Absorbed `server-monitor/pkg`; removed `server-monitor/pkg/go.mod`, `server-monitor/pkg/go.sum`, all nested module and replace assumptions, and the duplicate Redis implementation.
- Preserved the frontend at `server-monitor/frontend/`.
- Added an immutable SHA-256 test for `00001` through `00006` in addition to the ledger and old-HEAD `cmp` proof.

### Three processes

- `cmd/cloudops-api`: API-only bootstrap in `internal/bootstrap/api`; repository-only Agent, Remediation and Verification application facades retain existing `/api/v2` Query/Command behavior without worker loops.
- `cmd/cloudops-worker`: management-only listener and the three existing legacy loop assemblies under `internal/startup/legacyworker` (Agent, Remediation, Delivery/Verification). No `async_tasks`, four-pool runner, lease unification, state conversion, outbox conversion, or Phase 2 state machine was added.
- `cmd/cloudops-migrate`: isolated `internal/bootstrap/migrate` package. It accepts no operation or `up` only, uses Goose and the MySQL session advisory lock, and exposes no Down/Redo/Reset command.

### Runtime contracts

- Typed `APIConfig`, `WorkerConfig`, and migrate `Config` are separate process entrypoint types.
- API and Worker have independent `/livez`, `/readyz`, and `/metrics` management surfaces; API readiness requires an initialized MySQL client and supported Goose schema version, and Worker readiness requires initialized legacy loops plus MySQL/schema readiness.
- API and Worker shutdown paths close HTTP/infra resources; Worker loop `Stop()` is bounded by the shutdown timeout so a stuck legacy loop cannot hold the process indefinitely.
- Runtime production Go source has zero `AutoMigrate` calls. Remaining `AutoMigrate` calls are test-only fixtures that create an existing V2 schema for migration proof.

### Build and CI surface

- Added root `Makefile`, root `.dockerignore`, and root multi-target `Dockerfile` for API, Worker, Migrate, with a compatibility `runtime` API alias.
- Updated Compose build context and the hosted/PR Docker matrices to root context; retained existing `server-web` compatibility image/deployment naming rather than changing Chart ownership.
- Repaired `server-monitor/Makefile` and the V2 demo script's deleted-module paths; removed old down/status/version migration targets.
- Removed CI calls to nonexistent `internal/copilot/*/eval` paths while keeping the existing frontend and deployment assets in place.

## 4. Migration Evidence

### Immutable history and 00007

`00001` through `00006` were compared byte-for-byte with their initial HEAD paths. All six Git blobs and SHA-256 values remain exactly those recorded in the ledger. The new file is:

| File | Git blob | SHA-256 | Content boundary |
|---|---|---|---|
| `migrations/00007_expand_legacy_schema.sql` | `ba66c7c8f69465cdaa6232f9d68b0bc41003550e` | `e254655698086f7ff3679fe615d0d7b6c2bd58158eb44501086ca37f44c54f45` | 190 lines; ten legacy compatibility tables; `CREATE TABLE IF NOT EXISTS` only; no DML, ALTER, conversion, backfill, state change, lease/outbox conversion, or Down section |

### Disposable MySQL instance

Evidence used one isolated MySQL 8.0.46 instance under `/tmp/cloudops-phase1-mysql.QVJ05x`, Unix socket `mysql.sock`, TCP `127.0.0.1:33441`. It was stopped after the tests. No existing database, container, kind cluster, registry, GitHub, Argo, or Kubernetes environment was used or modified.

The durable integration anchor was `TestPhase1MigrationFreshExistingParityAndLock` in `internal/migration`:

| Check | Result | Evidence |
|---|---|---|
| Fresh empty database, `00001`-`00007` | PASS | Goose reached version 7 |
| Existing V2 database (`00001`-`00006` plus actual legacy GORM fixture) | PASS | Goose upgraded to 7 without conversion |
| Fresh/existing schema parity | PASS | Canonical `information_schema` SHA-256 `ac1a0e1cd0882d55eea7d4d13356581f0c309d7d9001e0d994d23efd7d1d3e3c`; columns/defaults/indexes/constraints/engine/collation included |
| Legacy data preservation | PASS | Ten-table sentinel data SHA-256 `01320e2b8b0ea4375fe5abc55b48bdc8cfbb17c3f878c9d47a021de4db10afcf` unchanged |
| Repeat `Up` | PASS | Zero new migrations; schema and data hashes unchanged |
| Concurrent independent runners | PASS | Two providers completed and version 7 has exactly one applied row |
| Held advisory lock | PASS | Runner timed out at version 6; release then allowed version 7 |
| Failure lock release | PASS | Invalid test-only Goose SQL failed, then another connection acquired the same lock immediately |
| CLI concurrent `cloudops-migrate up` | PASS | Two final binaries completed; `MAX(version_id)=7`, version-7 applied count `1` |
| CLI `down` | PASS | Final binary rejected `down` with non-zero exit and usage error |

The existing V2 repository/concurrency anchor `TestMySQLMigrationRepositoryAndConcurrentIngestion` was also rerun against a fresh disposable database and passed.

### Runtime no-DDL proof

- A DDL identity ran the final `cloudops-migrate up` to version 7 and repeated it successfully.
- A DML-only identity (`phase1_dml@127.0.0.1`) had only `USAGE` plus `SELECT, INSERT, UPDATE, DELETE` on the migrated disposable database. API and Worker both returned ready, shut down cleanly, and left the table count unchanged.
- The same DML-only identity was tested against an empty, unmigrated disposable database. API and Worker both returned readiness `503`, and the table count remained unchanged. This is executable evidence that runtime startup does not create the schema.

## 5. Gate Results

| Gate | Status | Command/evidence |
|---|---|---|
| Root ordinary tests | PASS | `go test -count=1 ./...` |
| Root race tests | PASS | `go test -race -count=1 ./...` |
| Root vet | PASS | `go vet ./...` |
| Three binary builds | PASS | `go build` for `./cmd/cloudops-api`, `./cmd/cloudops-worker`, `./cmd/cloudops-migrate` |
| gofmt/goimports | PASS | root `make check-gofmt`, `make check-goimports` |
| Go lint | PASS | `golangci-lint run ./...`, 0 issues |
| Module dependency check | PASS | `go mod tidy -diff`, `go mod verify`, `go list -mod=readonly ./...` |
| Nested module/replace/parallel implementation scan | PASS | `make check-structure`; no nested `go.mod`, replace, old imports, stale eval paths, Phase 2 markers, or stale Makefile/script migration paths |
| API worker-goroutine structure | PASS | `internal/bootstrap/api/boundary_test.go`, API `go list -deps` forbidden set empty, final `go tool nm` forbidden symbol set empty |
| Worker/Migrate capability separation | PASS | `internal/bootstrap/process_boundary_test.go`; Worker has no migration closure, Migrate has no runtime/legacy-worker closure |
| API `/api/v2` regression surface | PASS | Existing router/handler/service tests, repository integration, and API repository-only facades pass; no `/api/v3` business routes added |
| Runtime AutoMigrate absence | PASS | Non-test Go scan is zero; unmigrated DML-only runtime startup returns 503 without table creation |
| Process health/readiness | PASS | API/Worker positive and negative readiness tests, schema mismatch tests, independent `/metrics` health handler |
| Graceful shutdown | PASS | API shutdown test; Worker loop lifecycle and bounded blocking-`Stop()` test |
| Goose/advisory lock | PASS | Fresh/existing/concurrent/held-lock/failure-release MySQL tests and two-process CLI run |
| Fresh/existing migration parity | PASS | Disposable MySQL schema/data hashes above |
| Frontend original lint/typecheck/unit | PASS | `npm run lint`, `vue-tsc --noEmit`, Vitest: 5 files/18 tests; `npm run build` also passed |
| actionlint | PASS | `actionlint -no-color` |
| shellcheck | PASS | all tracked shell scripts, including updated V2 demo script |
| Helm lint/template | PASS | `helm lint`; `helm template` rendered successfully |
| kubeconform | PASS | Chart 54/54 and raw manifests 54/54 |
| promtool | PASS | Prometheus config and both rule files |
| Compose static render | PASS | `docker compose ... config --quiet` |
| Docker build targets | PASS | Final local `make docker-build`; API/Worker/Migrate targets, non-root `app:app` images |
| `git diff --check` / final scope | PASS | staged and unstaged whitespace checks clean; 247 staged paths, zero unstaged/untracked |

## 6. NOT RUN and Residuals

- No existing container was started/stopped/reconfigured. The only Docker mutations were local image builds explicitly allowed by the task; no container, kind, Kubernetes, Registry, GitHub, Argo, or hosted CI write occurred.
- No deployed/existing production database inventory, row hash, backfill, cutover marker, state conversion, lease conversion, outbox conversion, or legacy schema/data deletion was attempted. The existing-V2 database in the test is a disposable fixture, not production evidence.
- No `async_tasks`, unified lease, four-pool task runner, Incident state compression, `/api/v3` business behavior, or Phase 2 state machine was added or enabled.
- API and Worker still wrap the broad legacy typed application configuration for compatibility. API's explicitly enabled Fast Demo path can initialize Kubernetes capability; no claim is made that the final V3 no-token credential boundary is complete.
- Existing `server-web` compatibility image/service names and Chart ownership remain. The V2 Demo script was not executed because deployment/demo behavior is outside the allowed Phase 1 local validation boundary.
- Frontend remains at `server-monitor/frontend/`; its eventual root migration is not started.
- Hosted GitHub Actions, Registry provenance/signing, kind installation, and real GitHub/Argo/Kubernetes behavior remain `NOT RUN` and are not inherited from historical reports.
- Frontend build emitted existing Rollup large-chunk and annotation warnings; lint, typecheck, unit and build exit codes were successful.

## 7. Phase 2 Inputs

Phase 2 may use the root module and process package boundaries established here, plus the explicit compatibility ownership in `00007`. It must remain a new, independently gated change:

1. Add async task tables/attempts and unified claim semantics only under the Phase 2 migration and concurrency gates.
2. Keep the three legacy loop implementations mechanically isolated until the Phase 2 replacement has passed takeover, backpressure, pool isolation and shutdown tests.
3. Treat all legacy rows, leases, outbox events and 11-state values as read-compatible facts; do not infer a live conversion from this report.
4. Preserve API `/api/v2` behavior and do not enable `/api/v3` business behavior in this Phase 1 worktree.
5. Revisit the shared legacy config and Fast Demo/Kubernetes capability as an explicit credential/process-boundary decision; this report does not close that risk.

No Phase 2 implementation was started in this run.

## 8. Final Scope Check

The staged diff contains the root module move, absorbed packages, three command entrypoints, API/Worker/Migrate bootstrap and tests, explicit migration 00007, build/CI path updates, migration ledger/risk evidence, and this report. It does not contain commits, pushes, deployment actions, external writes, frontend relocation, Chart ownership changes, state conversion, async task activation, or legacy data deletion.

```text
INITIAL_SHA=1ea0c3a21ed3ed1f822399f205afac225b1d5464
FINAL_SHA=1ea0c3a21ed3ed1f822399f205afac225b1d5464
FINAL_WORKTREE=STAGED_ONLY_NO_UNSTAGED_OR_UNTRACKED
```
