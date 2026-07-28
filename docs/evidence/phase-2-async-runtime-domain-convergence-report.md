# Phase 2 Async Runtime and Domain Convergence Report

> Normative source: [`docs/CloudOps-Incident-Agent-V3-Refactor-Design.md`](../CloudOps-Incident-Agent-V3-Refactor-Design.md)
>
> Scope: Phase 2 only, Async Runtime and domain convergence
>
> Historical V2 specifications, reports and implementation were used only as migration inputs. They are not Gate evidence.

## 1. Verdict

`PHASE_2_STATUS=PASS`.

The schema, MySQL queue mechanics, fencing, four-pool runner, seven-state compatibility model, V3 API skeleton, V2 regression and local repository gates passed. The normative design was amended under explicit implementation authority to remove its circular phase ordering: Phase 2 owns the unified runtime, typed registry and no-external-call `investigation.start`; subject-bound Agent, Remediation/PR, Delivery and Verification operations remain owned by Phases 4-6 and keep their original hard Gates.

The final `cloudops-worker` has exactly one claim path and links no legacy claim package or symbol. It fails closed before claim when any required operation is not supplied. This is the Phase 2 registry safety contract, not evidence that later business handlers exist. Phase 3 may proceed with the observability/Demo scope while leaving the incomplete Worker deployment disabled; full production Worker positive startup/readiness remains a Phase 6 Gate after all five subject-bound operations are registered.

| Required outcome | Result |
|---|---|
| MySQL-backed at-least-once task runtime mechanics | PASS |
| Four independent pools and bounded shutdown | PASS |
| V3 worker has only the `async_tasks` claim path | PASS, mechanical code/symbol Gate only |
| Seven-state V3 Incident compatibility model and cycle keys | PASS |
| `/api/v3` Query/Command skeleton | PASS |
| `/api/v2` behavior regression | PASS |
| Phase 2 registry/start operation and fail-closed ownership boundary | PASS |
| Subject-bound business operations implemented | NOT RUN; owning Phases 4-6 |
| Legacy data converted | NO, by design |
| Live cutover performed | NO, by design |
| External GitHub/Argo/Kubernetes environment mutated | NO |
| Ready for Phase 3 | YES, observability/Demo scope only |

## 2. Provenance

### Initial live state

The requested expected state had already been committed locally before this Phase 2 run. No reset or history rewrite was used.

| Control | Expected in request | Initial live value |
|---|---|---|
| Repository | `/home/monody/k8s/CloudOps-Copilot` | matched |
| Branch | `main` | `main` |
| HEAD | `1ea0c3a21ed3ed1f822399f205afac225b1d5464` | `748fa3f946321ead2cdbf6ef0e710e05566a620c` |
| Upstream | `origin/main@2f7e426d69a4ed7d8d32ec3ca83c13af0c71586e` | matched |
| Ahead/behind | not stated | 2 ahead, 0 behind |
| Staged paths | 247 | 0 |
| Unstaged tracked paths | 0 | 0 |
| Untracked non-ignored paths | 0 | 0 |
| Initial staged binary diff SHA-256 | `f5905084...` | empty-index SHA-256 `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

The provenance difference is explainable and identity-preserving:

- `748fa3f^` is exactly `1ea0c3a21ed3ed1f822399f205afac225b1d5464`.
- `1ea0c3a..748fa3f` contains exactly 247 paths.
- `git diff --binary 1ea0c3a..748fa3f | sha256sum` is exactly `f5905084a07f9c84a311f417ff1425a8e478bc0cdca12f9be2c581f301e70cb9`.
- Therefore the expected Phase 1 staged index had become the live HEAD commit without byte drift. Phase 2 preserved it and staged only new Phase 2 work.

The nine requested V3 design, architecture, ledger, risk, ADR and Phase 1 evidence inputs were read completely before implementation. The live worktree, not historical reports, was used as provenance.

### Tool versions

| Tool | Version/evidence |
|---|---|
| Git | `2.43.0` |
| Go | `go1.26.5 linux/amd64`; root `go.mod` selected |
| Node/npm | Node `v24.13.0`, npm `11.6.2` |
| MySQL client/server | client `8.0.46-0ubuntu0.24.04.3`; final disposable server used the pinned local `mysql:8.0` image |
| golangci-lint | `2.11.4`, built with Go `1.26.5` |
| actionlint/shellcheck | `1.7.7` / `0.10.0` |
| Helm/kubeconform/promtool | `v3.14.4` / `development` / `2.51.0` |
| Docker/Compose | `29.3.0` / `v5.1.0` |

`gofmt` and `goimports` do not expose a version flag in the installed binaries; their executable checks passed and the selected Go toolchain is recorded above.

### Final live state

| Control | Final value |
|---|---|
| Branch/HEAD/upstream | validation tree based on `codex/v3-refactor@748fa3f946321ead2cdbf6ef0e710e05566a620c`, upstream base `2f7e426d69a4ed7d8d32ec3ca83c13af0c71586e` |
| Phase 1 index identity | preserved exactly: 247 paths, binary diff SHA-256 `f5905084a07f9c84a311f417ff1425a8e478bc0cdca12f9be2c581f301e70cb9` |
| Phase 2 state | 79 staged paths after final staging; no unstaged tracked or untracked non-ignored path |
| Commit/push | Phase commit occurs after this self-report is staged; push is prohibited by the active request |
| Deployment/external write | NOT RUN by design |
| Disposable MySQL | final instance stopped; TCP port verified unavailable |

The final staged diff contains Phase 2 implementation, tests, OpenAPI, ledger/risk updates and this report. It contains no frontend source change, deployment change, backfill/conversion, marker, commit or push.

## 3. Implemented Scope

### Forward schema

- Added only `migrations/00008_expand_v3_async_runtime.sql`.
- Added the five Phase 2 tables: `async_tasks`, `async_task_attempts`, `signal_rejections`, `command_idempotency_records` and `migration_ledger`.
- Added nullable V3 compatibility fields, cycle fields, public UUIDs, generated active keys, verification trigger identity and supporting indexes.
- Kept all legacy status/data/lease/checkpoint/outbox columns readable and untouched.
- Added no backfill, DML conversion, outbox-derived task, Down section, deletion or `CUTOVER_V3` marker.

### Durable runtime

- Implemented queue-scoped ready claim and explicit expired takeover with `FOR UPDATE SKIP LOCKED` and MySQL `NOW(6)`.
- Implemented retry/backoff, max-attempt dead, replay generation, heartbeat, checkpoint and terminal resolution.
- Every running write validates task id, owner, generation, unexpired lease and task expected version. It additionally locks and validates the actual subject version, V3 schema identity, Incident id and cycle.
- Queue transactions use the canonical Incident -> child subject -> Task lock order. MySQL 1205/1213 retries restart the complete short transaction at most three times; forced-deadlock and concurrent Incident-refresh/dead-task regressions passed.
- Stale ready/expired work becomes dead without execution. Exhausted ready/expired work records attempt audit and marks the active Incident `needs_attention`.
- Dead replay creates a new generation and revalidates the current subject; it never changes the dead source row back to ready.
- Wording remains transactional enqueue plus MySQL-backed durable queue with at-least-once semantics. No exactly-once claim is made.

### Four-pool worker

- Frozen pools are `investigate=2`, `deliver=1`, `observe=2`, `verify=2`.
- Each pool owns its own semaphore. The runner obtains capacity before claim and never borrows capacity across pools or builds an in-memory pending queue.
- Handler, external-call, heartbeat and lease timing relations are typed and validated. The runner injects the pool external deadline and handlers must derive external contexts through `asyncjob.ExternalCallContext`.
- SIGTERM stops all claim loops synchronously, drains for at most 45 seconds, cancels remaining handlers and fits within the 55-second exit budget. Cancellation does not fabricate success/failure; lease expiry permits takeover.
- Worker readiness is separate from API readiness and checks MySQL, exact schema version 8, queue schema and runner initialization before claims. Positive lifecycle mechanics use explicit test operations; the incomplete production registry remains fail closed until Phase 6.

### Incident and Command compatibility paths

- Added a seven-state V3 Incident type with optimistic version/cycle fencing, monotonic severity, close guards, and resolution only from passing Verification.
- Added V3-only MySQL Signal/Incident transactions with correlation locks, duplicate Signal handling, create/reopen rules, cycle isolation, Event plus start-Task transactionality and generated-key enforcement.
- Added durable Command idempotency. Concurrent same actor/scope/key/hash returns one result; a different hash returns conflict. Completed expiry is 24 hours after completion.
- Added transactional start-investigation and close Commands. Close now cancels matching running Task and TaskAttempt together and increments lease generation.
- Durable command replays, including stored 4xx responses, propagate `Idempotent-Replay`; severity values are validated at both domain and store boundaries.

### Alertmanager ingress and process boundary

- Added strict Alertmanager v4 normalization with duplicate/normalized-key rejection, deterministic canonical identities, same-batch firing/resolved pairing, target allowlist resolution and durable unmatched/target rejection facts.
- External summaries, labels and annotations are bounded with UTF-8-safe redaction, private-key/bearer/assignment/high-entropy filtering, and no raw payload persistence.
- Added separate user and internal API listeners. The internal router exposes only webhook, livez, readyz and metrics; its bearer profile is optional only as an explicit Phase 2 compatibility boundary and fails closed when required credentials are absent or weak.
- Ingress group and rejection transactions retry only MySQL 1205/1213 conflicts, at most three attempts; no external system is mutated.

### API skeleton

- Added 15 `/api/v3` routes and a hand-written OpenAPI 3.1 contract.
- Added public UUID-only DTOs, stable `application/problem+json`, request/trace IDs, viewer/operator rules, signed provider/login-bound CSRF, Origin/CORS checks, `Idempotency-Key`, expected version/hash and incident-scoped refresh-only SSE.
- Query ports not yet implemented return explicit `501`; they do not fabricate empty `200` results.
- Start/close use the MySQL Command port. Remediation decision remains explicit `501` because its owning V3 semantics are outside Phase 2.
- `/api/v2` routes, representative responses and frontend behavior remain unchanged.

## 4. Migration Evidence

| File | Lines | Git blob | SHA-256 | Result |
|---|---:|---|---|---|
| `migrations/00008_expand_v3_async_runtime.sql` | 583 | `1a1e413be591f6dd7d6dd649e1fb56f19a656aec` | `a769354179532733b6216fbfde699cf756744f72dd75c98cf730feb2e093e96e` | PASS |

`00001` through `00007` retained their recorded blobs and SHA-256 values byte-for-byte. The immutable test and direct hash scan passed.

The final tree was rerun on a disposable MySQL 8.0 container (`mysql:8.0`, TCP 52819) with isolated databases for migration, queue, Incident, command, legacy V2 and DML-only startup tests. The container, databases and restricted test user were removed after verification; no deployed database was touched.

| Migration check | Result | Final evidence |
|---|---|---|
| Fresh `00001`-`00008` | PASS | Goose version 8 |
| Existing Phase 1 database to 8 | PASS | Every pre-`00008` affected-column value unchanged |
| Fresh/existing schema parity | PASS | Canonical schema SHA-256 `90b6973314684886e4b6fade62be5002e311620da50abb3d40a84f0af016d0e4` |
| Legacy sentinel preservation | PASS | Ten-table SHA-256 remained `01320e2b8b0ea4375fe5abc55b48bdc8cfbb17c3f878c9d47a021de4db10afcf` |
| Repeat/concurrent Up | PASS | One applied version-8 row |
| Advisory-lock block/release/failure release | PASS | Serialized migration ownership retained |
| Expand-only structure | PASS | No DML/drop/rename/backfill/conversion/marker |
| Generated-key negatives | PASS | Incident, AgentRun, Plan, ChangeRequest, VerificationRun and trigger identity conflicts |

Final JSON EXPLAIN evidence:

| Path | Access | Selected key | Used key parts | Full scan |
|---|---|---|---|---|
| ready claim | `range` | `idx_async_tasks_ready_claim` | `queue,status,available_at` | NO |
| expired takeover | `range` | `idx_async_tasks_expired_takeover` | `queue,status,lease_expires_at` | NO |

## 5. Concurrency and Recovery Evidence

| Check | Result | Evidence |
|---|---|---|
| SKIP LOCKED multi-worker claim | PASS | 12 concurrent workers claimed 12 distinct tasks |
| Expired takeover | PASS | owner and generation changed; old heartbeat/checkpoint/complete rejected |
| Actual subject fencing | PASS | changed version/cycle rejected heartbeat, checkpoint, retry/dead and completion |
| Lock ordering and deadlock recovery | PASS | Incident -> child -> Task ordering; concurrent refresh/dead transitions and a forced 1213 retried the whole transaction |
| Retry/dead/replay | PASS | bounded retry reached dead; concurrent replay created one generation-1 row |
| Exhausted task recovery | PASS | ready and expired max-attempt tasks became dead, retained attempt audit and set Incident attention |
| Shutdown takeover | PASS | unfinished task remained running, lease expired, new owner took over and completed |
| Pool maximums/backpressure | PASS | observed `2/1/2/2`; claim counts stopped at available semaphore capacity |
| Starvation isolation | PASS | saturated investigate and slow observe did not stop deliver/verify |
| Duplicate webhook | PASS | one Signal/Incident/start Task identity; duplicate returned original identity |
| Concurrent Incident create/reopen | PASS | one active Incident and exactly one start Task creator |
| Per-cycle start Task/AgentRun | PASS | one start Task and one AgentRun for each tested cycle |
| Multi-alert decision | PASS | one decision; warning-first/critical-second produced critical severity |
| Reopen boundaries | PASS | exact 30 minutes reopened; 30 minutes plus one second created new; newer closed prevented older resolved reopen |
| Command same payload | PASS | 8 callers produced one durable effect and 7 identical replays |
| Command different payload | PASS | one success and one payload conflict |
| Command close cancellation | PASS | Task and TaskAttempt cancelled atomically; generation fenced old writer |

## 6. Gate Results

| Gate | Status | Final command/evidence |
|---|---|---|
| Go ordinary tests | PASS | `go test -count=1 ./...` |
| Go race | PASS | `go test -race -count=1 ./...` |
| Go vet | PASS | `go vet ./...` |
| Three binary builds | PASS | `make build-go` |
| gofmt/goimports | PASS | `make check-gofmt check-goimports` |
| golangci-lint | PASS | `golangci-lint run ./...`, 0 issues |
| Module checks | PASS | `go mod tidy -diff`, `go mod verify`, readonly package list |
| Migration immutable/structure scans | PASS | migration tests, expand-only scan, no production AutoMigrate |
| Old lease claim dependency/source/symbol scans | PASS | worker links `asyncjob`/`taskhandler`; no legacy claim package or `ClaimNext`/`ClaimDelivery`/`ClaimRun` symbol |
| Real MySQL Gate | PASS | serialized migration/async/Incident/command suites, legacy V2 integration, and DML-only API/Worker startup on disposable MySQL 8.0 |
| API v3 contract/OpenAPI | PASS | runtime/OpenAPI route parity and transport/domain contract tests |
| API v2 regression | PASS | exact route set, representative middleware/body-limit/handler behavior |
| Frontend lint | PASS | `npm run lint` |
| Frontend typecheck | PASS | `npm exec -- vue-tsc --noEmit` |
| Frontend unit | PASS | 5 files, 18 tests |
| Frontend build | PASS | Vite build; existing annotation/large-chunk warnings retained |
| actionlint | PASS | actionlint `1.7.7` |
| shellcheck | PASS | all tracked shell scripts |
| Helm lint/template | PASS | local lint and render only |
| kubeconform | PASS | Chart 54/54 and raw manifests 54/54 |
| promtool | PASS | config plus both rule files |
| Compose render | PASS | config parse only |
| Docker targets | PASS | API/Worker/Migrate images built; all default to non-root `app:app` |
| External secret scanner | NOT RUN | `gitleaks`, `trufflehog` and `detect-secrets` are unavailable; `111.txt` remained ignored and was not read, logged, staged or committed |
| Final diff whitespace/scope | PASS | staged/unstaged checks clean; no unstaged/untracked path |
| Phase boundary correction | PASS | normative Phase 2 now owns runtime/registry/start only; Phase 4-6 business Gates remain unchanged |
| Missing-operation fail closed | PASS | production `NewRuntime` rejects construction before claim and identifies every absent operation |
| Subject-bound business-loop handlers | NOT RUN | explicitly owned by Phases 4-6; fixture/no-op handlers are not accepted as evidence |
| Production Worker positive startup/readiness | NOT RUN | owning Phase 6 Gate after all five subject-bound operations are registered |

Docker labels were honestly `revision=unknown`, `source=unknown`, `version=dev` because the local Make target did not inject VCS build arguments. These images prove Docker targets build and use a non-root user; they are not exact-SHA release provenance.

## 7. Deferred Owning-Phase Work

The remaining business work cannot be closed by wrapping the current V2 workers:

1. Agent `ProcessNext` self-claims and heartbeats the AgentRun legacy lease, then executes a multi-node in-memory graph. V3 requires one model decision or one bounded read tool per task and task-fenced checkpoint/state mutation.
2. Remediation delivery self-claims ChangeRequest and its writer can perform branch, commit and Draft PR as one monolithic call. Its V2 operations include `rollback_image`/`set_replicas`, which are not valid V3 remediation semantics.
3. Delivery observation self-claims a legacy lease and can read more than one authority in one step.
4. Verification self-claims/heartbeats VerificationRun and retains V2 profile/Loki semantics instead of one due V3 check.
5. No subject-bound, no-claim application operation exists for the Run-scoped `investigation.advance` / `investigation.step` transition, or for `remediation.prepare`, `change.ensure_pr`, `delivery.observe` and `verification.advance`.

Mechanically calling those V2 methods would reintroduce three claim paths, legacy lease writes, multi-operation handlers and later-phase semantics. That would violate the V3 design and the explicit Phase 2 prohibitions. The code therefore rejects production worker construction before it can claim a task.

The previous design text made these Phase 4-6 operations a Phase 2 prerequisite and therefore created an implementation cycle. The authorized amendment now assigns each body to its semantic owning Phase while preserving the final five-operation registry, task fencing, one-step bounds and all later hard Gates. A V2 wrapper would still violate the runtime contract. Placeholder, no-op or forced-dead handlers remain invalid evidence.

## 8. NOT RUN and Scope Boundaries

- No legacy row was backfilled, rewritten, compressed from 11 states, converted to a Task or used to create a V3 AgentRun.
- No legacy outbox row generated a Task.
- No `CUTOVER_V3` marker, maintenance window, old/new Worker coexistence, live task conversion or irreversible cutover was performed.
- No legacy schema/data/lease/checkpoint field was deleted.
- No GitHub write, Argo sync, Kubernetes repair, Registry push, hosted CI mutation, commit, push or deployment was performed.
- No Phase 3 Operator/observability stack/Demo, Phase 4 Agent refactor, Phase 5 Remediation, Phase 6 Verification/Workbench/frontend or Phase 7 cutover/contract work was started.
- No existing deployed database inventory or production row hash was collected. All MySQL evidence used disposable local databases.
- No positive production Worker readiness is claimed. Test-only explicit operation injection checks DML-only/no-DDL startup mechanics; it does not substitute for real handlers.

Local Docker image creation, one disposable MySQL container and disposable test databases were the only local environment mutations. No application or deployment container was run; the temporary MySQL container and all test databases were stopped/removed.

## 9. Remaining Work and Phase 3 Input

Phase 3 can start with the following explicit boundary:

1. Deploy and verify only the Phase 3 observability stack, Demo workload, Alertmanager ingress contract and baseline readiness probe.
2. Keep the incomplete production Worker disabled; do not claim Phase 4 Agent execution or create future task types.
3. Phase 4 implements task-fenced `investigation.step` with StateDelta/checkpoint semantics.
4. Phase 5 implements `remediation.prepare` and one-write-phase `change.ensure_pr`.
5. Phase 6 implements one-authority `delivery.observe` and one-due-check `verification.advance`, injects all five subject-bound operations, and proves positive production Worker startup/readiness.
6. Each owning Phase reruns the relevant MySQL/fencing/fault tests and retains the negative legacy dependency/symbol Gate.

Phase 3 may reuse schema version 8, the queue mechanics and API skeleton, but it must not treat the current Worker as an established executable business runtime.

## 10. Final Conclusion

Phase 2 produced a verified additive schema, durable MySQL task mechanics, four isolated pools, strong fencing, a seven-state/cycle compatibility path and a contract-tested `/api/v3` skeleton without changing `/api/v2` or converting legacy data. It also freezes a fail-closed registry and the only Phase 2-owned business transition, `investigation.start`. Subject-bound operations remain visibly unimplemented and gated in their owning phases.

`READY_FOR_PHASE_3=YES`.
