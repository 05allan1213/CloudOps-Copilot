# Phase 7A Release A Cutover Implementation Report

- Date: `2026-07-22`
- Branch: `codex/v3-refactor`
- Baseline SHA: `9e76c8fb806fd521cfe61818b7ee0e9d746dd3b8`
- Release A runtime exact SHA: `f801e71495c9b51273af62a1928f85c1b9ef8502`
- Runtime generation: `v3`
- Overall implementation: `IMPLEMENTATION_PASS`
- Local validation: `LOCAL_VALIDATION_PASS`
- Golden: `GOLDEN_NOT_RUN`
- Phase 7A final DoD: `NOT PASS` because a real Golden run was not executed
- Phase 7B: `NOT RUN`

The evidence-report commit follows the source-bound runtime commit. The OCI
images below are built from the Release A runtime exact SHA, not from an older
historical image or tag.

## Scope and boundaries

This change completes the non-credentialed Phase 7A Release A implementation.
It adds only forward migration `00017`; migrations `00001` through `00016` were
not modified. No Goose Down, legacy schema deletion, legacy worker removal,
Kubernetes workload mutation, push, pull request, tag, release, or real
`CUTOVER-V3` marker write was performed. `111.txt` was not read, printed, or
committed.

## Local commits

| Commit | Subject |
|---|---|
| `c0be650` | `feat(migration): add restartable phase7a backfill schema` |
| `9be0a8e` | `feat(cutover): implement versioned legacy converters` |
| `2810a25` | `fix(cutover): enforce deterministic state and task conversion` |
| `b373f8c` | `test(cutover): add mysql and negative conversion fixtures` |
| `f801e71` | `feat(cutover): freeze v3 runtime generation` |

## Implementation inventory

Schema and versioning:

- `migrations/00017_phase7a_backfill_contract.sql`
- `internal/schemaversion/version.go`
- `internal/migration/evidence_supersession_test.go`
- `migrations/immutable_test.go`
- `migrations/phase7a_cutover_test.go`

Cutover engine:

- `internal/cutover/backfill.go`
- `internal/cutover/canonical.go`
- `internal/cutover/ledger.go`
- `internal/cutover/phase7a_ledger.go`
- `internal/cutover/phase7a.go`
- `internal/cutover/audit.go`
- `internal/cutover/marker.go`
- `internal/cutover/writer.go`
- `internal/cutover/outbox_registry.go`
- `internal/cutover/outbox_store.go`
- `internal/cutover/archive_misc.go`
- `internal/cutover/plan_archive.go`
- `internal/cutover/converter_agent.go`
- `internal/cutover/converter_verification.go`
- `internal/cutover/converter_change.go`
- `internal/cutover/agent_store.go`
- `internal/cutover/verification_store.go`
- `internal/cutover/change_store.go`
- `internal/cutover/incident_store.go`
- `internal/cutover/state_mapping.go`
- `internal/cutover/conversion_store.go`

Runtime, projections, and read-only reconciliation:

- `internal/bootstrap/migrate/config.go`
- `internal/bootstrap/migrate/migrate.go`
- `internal/bootstrap/migrate/github_reconcile.go`
- `internal/infra/githubread/client.go`
- `internal/infra/deliveryread/v3.go`
- `internal/asyncjob/{types.go,repository.go,repository_sql.go}`
- `internal/apiv3/{types.go,mysql_query.go,workbench_types.go,workbench_mysql.go}`
- `internal/infra/incidentv3mysql/{store.go,no_change.go}`
- `internal/infra/remediationmysql/repository.go`
- `internal/change/sources.go`
- `internal/remediation/types.go`
- `internal/command/port.go`
- `internal/taskhandler/{investigation_start.go,investigation_step.go,remediation_prepare.go,remediation_prepare_mysql.go,change_ensure_pr.go,change_ensure_pr_preflight.go,delivery_observe.go,verification_advance.go,verification_advance_mysql.go}`
- `docs/api-v3-openapi.yaml`
- `server-monitor/scripts/golden-e2e.sh`

Primary test evidence:

- `internal/cutover/converter_contract_test.go`
- `internal/cutover/ledger_contract_test.go`
- `internal/cutover/phase7a_mysql_integration_test.go`
- `internal/cutover/{marker_test.go,writer_test.go}`
- `internal/bootstrap/migrate/github_reconcile_test.go`
- `internal/taskhandler/delivery_observe_test.go`
- `internal/apiv3/resolution_report_test.go`

## Design-to-code mapping

| Design contract | Implementation | Deterministic evidence |
|---|---|---|
| Section 26 bounded, restartable `BACKFILL-V3` | `backfill.go`, `ledger.go`, migration cursor/archive tables in `00017` | MySQL fault injection, retry chain, fresh and 16-to-17 migration in `phase7a_mysql_integration_test.go` |
| Agent checkpoint conversion and incompatible-run fallback | `converter_agent.go`, `agent_store.go`, `incident_store.go` | compatible mapping plus missing field, hash, stale/cross-cycle Evidence, duplicate signature, budget, and next-node negatives in `converter_contract_test.go` |
| Verification conversion, revision/profile/check/sample/common-window validation | `converter_verification.go`, `verification_store.go` | Loki, ownership, revision, sample unit, min sample, common-window negatives and compatible positive conversion |
| Plan, Approval, ChangeRequest archive and read-only GitHub reconciliation | `plan_archive.go`, `converter_change.go`, `change_store.go`, `github_reconcile.go` | partial write, no-write Approval, Draft PR, merged PR, ambiguous external state, bounded GitHub read tests |
| Read-only legacy `delivery.observe` | `taskhandler/delivery_observe.go` | full repository/PR/URL/base/head/merge identity test; no post-delivery Verification or ResolutionReport on legacy path |
| Exact Incident state mapping and FAILED priority | `state_mapping.go`, `incident_store.go` | all named legacy statuses, four-level FAILED priority, and RESOLVED verified/unverified fixtures |
| Versioned outbox registry and full-row archive hash | `outbox_registry.go`, `outbox_store.go`, `phase7a_ledger.go` | unknown type/schema rejection, publication parity, fixture hashes, full-row hash sensitivity, no outbox-derived Task category |
| Task anti-join and dedupe identity | `conversion_store.go` | dedupe component test, existing native Task preservation, terminal child exclusion, rerun and fallback uniqueness in MySQL |
| Ledger truth and audit export | `ledger.go`, `phase7a_ledger.go`, `audit.go`, `writer.go` | every invariant fails closed, passed-batch drift rejection, audit redaction/count families, marker prerequisite revalidation |
| `migrated_legacy` end-to-end provenance | `00017`, async Task types/repository, task handlers, API projections, no-change flow, Golden filters | MySQL provenance assertions, API test, sufficiency contract, Golden native-only filters |
| Section 28.4 forward-only cutover boundary | `00017`, migration immutable tests, marker reader/writer split | no Down/destructive statements; test-only marker; actual marker and V3 process startup remain NOT RUN |
| Runtime generation freeze | `internal/cutover/marker.go` | `CurrentRuntimeGeneration = RuntimeV3`; marker/runtime guard tests PASS |

## Validation results

| Gate | Result | Evidence |
|---|---|---|
| Formatting | `PASS` | all applicable Go files formatted |
| Focused Go tests | `PASS` | cutover, bootstrap/migrate, migration, taskhandler, agent, migrations |
| Go vet | `PASS` | cutover, bootstrap/migrate, taskhandler, agent |
| Patch whitespace | `PASS` | `git diff --check` |
| Fresh MySQL schema `00001 -> 00017` | `PASS` | disposable MySQL 8.0.46 |
| Existing schema `00016 -> 00017` | `PASS` | legacy fixture inserted before forward migration |
| Backfill crash/restart and new-attempt ledger | `PASS` | injected after-target-write failure, attempt 1 failed, attempt 2 passed with previous ledger reference |
| Repeated and concurrent prepare | `PASS` | stable ledger IDs and unchanged Task/conversion/ledger counts |
| Archive count/hash parity | `PASS` | outbox and legacy archives checked in real MySQL |
| Task anti-join and terminal child exclusion | `PASS` | native Task preserved, one fallback, terminal Agent/Verification produced no Task |
| Ledger PASS/FAIL truth and audit redaction | `PASS` | post-pass drift generated failed attempt; payload/checkpoint/narrative absent from audit export |
| Marker prerequisites and runtime guards | `PASS` in disposable test DB | one test-only marker; compatibility refusal and V3 acceptance verified |
| Actual production/cutover marker write | `NOT RUN` | no authorized real cutover database or operation approval |
| V3-only API/Worker startup | `NOT RUN` | actual marker was intentionally absent |
| Exact-SHA local images | `PASS` | API, Worker, Migrate labels and digests below |
| Golden preflight | `FAIL` | `argocd` CLI is absent; preflight stopped before credential reads |
| Golden run | `NOT RUN` | live GitHub Apps/OAuth, CI, Argo, Kubernetes, Registry, LLM, and human merge/approval evidence was not established |
| Phase 7B cleanup | `NOT RUN` | legacy schema, lease/claim paths, manifests, and services retained |

## Disposable MySQL evidence

- Server: `MySQL 8.0.46-0ubuntu0.24.04.3`
- Isolation: new `/tmp` data directory, Unix socket only, TCP disabled, no user environment touched
- Test: `TestMySQLPhase7AMigrationBackfillConversionAuditAndMarkerContracts`
- Result: `PASS` in `11.39s`
- Observed final fixture counts: `tasks=5`, `conversions=15`, `ledgers=23`, test marker count `1`
- Cleanup: both generated databases were dropped and the disposable server was stopped

Real MySQL validation found and closed four issues that static tests did not
prove: mixed-collation event identity comparison, writes while a MySQL row
cursor was still open, two fixture placeholder-count errors, and an archive
column too narrow for `non_authoritative`.

## Exact-SHA image evidence

All images are local `linux/amd64` images with user `65532:65532` and these
labels:

- `org.opencontainers.image.revision=f801e71495c9b51273af62a1928f85c1b9ef8502`
- `org.opencontainers.image.version=f801e71495c9b51273af62a1928f85c1b9ef8502`
- `org.opencontainers.image.source=https://github.com/05allan1213/CloudOps-Copilot`

| Image | Exact digest |
|---|---|
| `cloudops-phase7a-api:f801e71495c9b51273af62a1928f85c1b9ef8502` | `sha256:4c0c42fc4b300905ba2277cd1042e90b266054a039beee3f22d9bd7f56febefb` |
| `cloudops-phase7a-worker:f801e71495c9b51273af62a1928f85c1b9ef8502` | `sha256:0fd79ddf9e8b8f3b4a2b55e1ef3dc1787965c6673bbe6420181eb6560b7fb7cd` |
| `cloudops-phase7a-migrate:f801e71495c9b51273af62a1928f85c1b9ef8502` | `sha256:2c0962fddf5dc04b651d31361e5d36921796aa9e1521ab8febbffcfa9ef921a9` |

The images were not pushed or deployed.

## Commands executed

```bash
git status --short --branch
git rev-parse HEAD
git log --oneline -15

gofmt -w $(rg --files internal/cutover internal/bootstrap/migrate internal/migration internal/taskhandler internal/agent migrations -g '*.go')
go test ./internal/cutover ./internal/bootstrap/migrate ./internal/migration ./internal/taskhandler ./internal/agent ./migrations
go vet ./internal/cutover ./internal/bootstrap/migrate ./internal/taskhandler ./internal/agent
git diff --check

/usr/sbin/mysqld --no-defaults --initialize-insecure ...
/usr/sbin/mysqld --no-defaults --daemonize --skip-networking --mysqlx=OFF ...
CLOUDOPS_TEST_MYSQL_ADMIN_DSN='root@unix(<disposable-socket>)/?parseTime=true&multiStatements=true' \
  go test ./internal/cutover \
  -run '^TestMySQLPhase7AMigrationBackfillConversionAuditAndMarkerContracts$' -count=1 -v
mysqladmin --protocol=SOCKET --socket=<disposable-socket> -uroot shutdown

docker buildx build --network host --load --provenance=false --sbom=false \
  --target cloudops-api --build-arg VCS_REF=<runtime-sha> \
  --build-arg VCS_SOURCE=https://github.com/05allan1213/CloudOps-Copilot \
  --build-arg VERSION=<runtime-sha> --tag cloudops-phase7a-api:<runtime-sha> .
docker buildx build --network host --load --provenance=false --sbom=false \
  --target cloudops-worker ...
docker buildx build --network host --load --provenance=false --sbom=false \
  --target cloudops-migrate ...
docker image inspect cloudops-phase7a-{api,worker,migrate}:<runtime-sha> ...

bash server-monitor/scripts/golden-e2e.sh preflight
```

## Final disposition

`IMPLEMENTATION_PASS / LOCAL_VALIDATION_PASS / GOLDEN_NOT_RUN`

Phase 7A must not be reported as final PASS until a credentialed, human-gated,
live Golden run succeeds against exact-SHA deployed images. Phase 7B remains
`NOT RUN`.
