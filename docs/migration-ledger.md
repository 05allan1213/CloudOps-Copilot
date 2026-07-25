# V3 Migration Ledger

> Status: forward migrations through schema 18 and the Phase 7A archive/converter/ledger tooling are implemented; live deployed-data preparation, irreversible marker, Golden, and Phase 7B contract deletion remain `NOT RUN`
>
> Normative source: [`CloudOps-Incident-Agent-V3-Refactor-Design.md`](CloudOps-Incident-Agent-V3-Refactor-Design.md)
>
> Phase 0 audited source: `main@2f7e426d69a4ed7d8d32ec3ca83c13af0c71586e`
>
> Phase 1 implementation started from `main@1ea0c3a21ed3ed1f822399f205afac225b1d5464`; its 247-path staged worktree is now preserved in the local commit recorded below
>
> Phase 2 live base: `main@748fa3f946321ead2cdbf6ef0e710e05566a620c`; this commit contains the 247-path Phase 1 diff whose binary SHA-256 is `f5905084a07f9c84a311f417ff1425a8e478bc0cdca12f9be2c581f301e70cb9`

## 1. Invariants

1. `00001` through `00008` are immutable history for the completed expand phases. Their contents, names and bytes must never be edited.
2. V3 uses forward-only migrations beginning after `00006`.
3. Runtime binaries never execute AutoMigrate. Kubernetes upgrades never execute Goose Down.
4. All schema/data transitions follow `expand -> backfill -> quiesce -> cutover -> contract`.
5. MySQL `NOW(6)` is authoritative for cutover locks and leases.
6. Backfill/conversion is restartable, batch-recorded and hash-verified. A mismatch is `FAIL`, not a warning.
7. Legacy rows marked `migrated_legacy=true` cannot satisfy V3 Golden E2E, Agent Quality or resume claims.
8. A V2 Approval never authorizes a new V3 external write.
9. An unknown external-write result blocks cutover until read-only reconciliation establishes a safe state.
10. Contract deletion is a later release after cutover and Golden evidence; it is never bundled with first cutover.

## 2. Goose History and Forward Migrations

The Git blob is the repository content address. SHA-256 is recorded for external tooling. The Phase 1 mechanical move changed the path of `00001` through `00006` but not one byte of their content; every historical blob and SHA-256 remains identical to the Phase 0 ledger.

| Version | File | Lines | Git blob | SHA-256 | Current schema effect | V3 treatment |
|---|---|---:|---|---|---|---|
| 00001 | `migrations/00001_incident_foundation.sql` | 200 | `042bda4fd6a0ddce32e7ab61ab6f0a1c1c06cb4f` | `8f7dd2e188fba00f6a7cce45f7c2f2319a4f80c7df7882c1a3e4c6471bce080d` | Creates Incident, AgentRun/Step, Signal, Event, Evidence, outbox and correlation-lock tables; adds `incidents.current_agent_run_id` | KEEP history; expand cycle/identity/generated keys; eventually remove circular current-run FK and archive outbox |
| 00002 | `migrations/00002_agent_runtime.sql` | 116 | `98942847d3c28c02d32d48c6cbeac69a7358f165` | `f515765f604391c933bc59b6fd3b7c7d3cfd17f3c429a403bcc014b82d370411` | Adds Agent budgets, whole checkpoint, idempotency, active generated key and AgentRun lease/heartbeat | KEEP history; versioned checkpoint conversion; task owns future lease; contract removes legacy lease |
| 00003 | `migrations/00003_change_intelligence.sql` | 77 | `7fc0aa8f3a5e3f6b49a369b52a7c0d2bf52ee4ae` | `584fda2c41ca7657228aba5f0b4b0e2d62bd2a4cfd54ec386198c22e163fd0f6` | Creates mutable `changes`; adds one-to-one-ish `evidence_items.change_id` | KEEP history; backfill immutable Candidate and append-only Assessment; contract removes legacy coupling/table |
| 00004 | `migrations/00004_gitops_remediation.sql` | 98 | `b47e1a3bb8aeffb523ab45c3e2f75fc67a3eace7` | `fc4773b65f89626bcfb678526d3c631bef89188007df00cda7a96813d7f95c84` | Creates Plans, Approvals and ChangeRequests; operations are `rollback_image`/`set_replicas`; ChangeRequest owns a lease | KEEP history; add immutable V3 hashes/cycle/phase; archive old Approval; contract removes lease and forbidden operation path |
| 00005 | `migrations/00005_delivery_verification.sql` | 178 | `7f142552d88e0f4760491cd86a1bf6430aa1cb19` | `8168e75a60f18ba5b8818c82b6dda71131adc8b9cca1f52eb93abce111f4a61d` | Expands many delivery statuses; creates leased VerificationRun and VerificationCheck | KEEP history; add cycle/trigger/profile/sample/inconclusive; compatible conversion only; contract removes lease |
| 00006 | `migrations/00006_observability_verification_postmortem.sql` | 69 | `f3857c1a46c16cedc6ae8be81be7bd462ad2d613` | `8e1a0b1f0a1125ffb54f8049be17617c1130cebbf446ecc371a281b4a7301fdb` | Adds observability check types/status and creates `postmortems` | KEEP history; archive as legacy Postmortem; never convert narrative into Evidence/Diagnosis/ResolutionReport |
| 00007 | `migrations/00007_expand_legacy_schema.sql` | 190 | `ba66c7c8f69465cdaa6232f9d68b0bc41003550e` | `e254655698086f7ff3679fe615d0d7b6c2bd58158eb44501086ca37f44c54f45` | Creates the ten compatibility tables previously owned by runtime GORM, using idempotent `CREATE TABLE IF NOT EXISTS`; contains no DML, ALTER, conversion or Down section | Phase 1 `EXPAND-LEGACY-SCHEMA`; retain all legacy rows and semantics until their later owning archive/contract Gates |
| 00008 | `migrations/00008_expand_v3_async_runtime.sql` | 583 | `1a1e413be591f6dd7d6dd649e1fb56f19a656aec` | `a769354179532733b6216fbfde699cf756744f72dd75c98cf730feb2e093e96e` | Additive V3 compatibility columns/generated keys and the five Phase 2 tables: `async_tasks`, `async_task_attempts`, `signal_rejections`, `command_idempotency_records`, `migration_ledger` | Phase 2 `EXPAND-INCIDENT-TASK`; no DML, backfill, outbox/task conversion, legacy deletion, Down section or `CUTOVER_V3` marker |
| 00009 | `migrations/00009_expand_v3_remediation_verification.sql` | 526 | `7c5ab3bef5edee0b38dcb8f54a84fe732fbe88f9` | `944ca629c190fe82a77e36ac7b35bfff4017fb8b59f3fb4db333918337d9c927` | Adds complete immutable Plan bindings, V3 `remediation_decisions`, append-only ChangeRequest events, frozen verification profile/check fields, `verification_samples` and `resolution_reports`; old approvals/Postmortems remain untouched | Phase 5/6 expand; nullable contract versions preserve 00008 partial rows; no backfill, cutover marker, legacy deletion or Goose Down |
| 00016 | `migrations/00016_phase7a_cutover_archives.sql` | 160 | `a6b61d2015a47223755add12c987e18fa7672e10` | `69884451e4f6b2b6031469bd41c160acf9f79cdc3858dff5cc1f9fb6e03dc629` | Adds quiesce controls, outbox/Incident/Agent/Change/Verification/Postmortem archives, conversion records and `migrated_legacy` provenance | Phase 7A tooling only; forward-only, retains every legacy source table/lease, writes no marker and performs no live conversion by itself |
| 00018 | `migrations/00018_reconcile_dead_investigation_runs.sql` | 84 | `87e86e966379dc81ce0e0c7c7fdf03d6a3e6d8d1` | `e76d724b619a692a9991794e82775533fb6d543c2d351b9c1972f2580f660f8f` | Reconciles current-version AgentRuns left active after their matching `investigation.step` Task reached `dead`; appends an idempotent failure event before making the Run terminal | Forward-only historical repair; a current ready/running technical replay prevents reconciliation, and no external write or cutover marker is involved |

All migrations use Goose `NO TRANSACTION`; DDL is not atomically rolled back. `00007` is restartable because every statement is an additive `CREATE TABLE IF NOT EXISTS`; later forward migrations rely on Goose version ownership plus the canonical schema check below. Every release must verify pre/post schema and ledger state explicitly.

## 3. Current Table Ledger

### 3.1 Goose-owned tables

| Current table | Current mutable/lease facts | Target | Migration rule | Contract deletion |
|---|---|---|---|---|
| `incidents` | 11-state status, no cycle, circular current run FK | `incidents` | Add nullable compatibility columns/generated key, backfill `cycle_no=1`, convert state under lock | Drop legacy status support/current-run FK only after all readers are V3 |
| `incident_signals` | Source event id/fingerprint, no alert-instance/cycle/key version | `incident_signals` | Preserve immutable row; add canonical v2 identities and cycle through deterministic converter | Never delete audit rows |
| `incident_events` | Append-only but no cycle/global event id | `incident_events` | Preserve; add cycle/idempotent event identity and bounded metadata projection | Never delete audit rows |
| `incident_correlation_locks` | V1 key | same table/contract | Create v2 key rows independently; never rewrite an active V1 key in place | Remove unused V1 rows only after retention/export policy exists |
| `agent_runs` | Whole GraphState, local lease, one-active Incident key | `agent_runs` | Backfill cycle; run checkpoint converter; incompatible Run becomes cancelled/archive + new start Task | Drop lease/current V2 fields in contract migration |
| `agent_steps` | Ordered V2 node/tool records | `agent_steps` | Preserve as migrated legacy facts; new V3 fields are additive | Do not delete audit rows |
| `evidence_items` | V2 facts, mutable validity, limited provenance | `evidence_items` | Preserve bounded content; add producer/cycle/trust hashes; invalid ownership stays archive-only | Drop incompatible query/legacy relation fields at contract |
| `outbox_events` | Event audit with `published_at`, `attempts`, `last_error`; no claim/lease/relay API | legacy archive only | Snapshot by event type/schema/published state/count/hash. Never create `async_tasks` directly | Drop only after export, count/hash proof and no V3 reader |
| `changes` | Mutable candidate/matched/excluded/unknown row | `change_candidates` + `change_candidate_assessments` | Copy immutable payload; emit latest deterministic assessment separately; record source/hash | Drop after projection parity and audit export |
| `remediation_plans` | V2 operations/states and partial hashes | `remediation_plans` | Preserve original; add cycle/schema/hash fields. V2 plan cannot become V3 actionable | Drop forbidden columns/statuses only at contract |
| `remediation_approvals` | Actor + plan/patch hash only | `remediation_decisions` | Archive original; no V3 Decision or write authority is inferred | Drop after audit export and zero caller proof |
| `change_requests` | One row/plan, monolithic external write, many delivery states, lease | `change_requests` + `change_request_events` | Reconcile external state first; convert only complete Draft PR/merged state to read-only observe | Drop legacy lease/status/fields after terminal reconciliation |
| `verification_runs` | Delivery-only trigger, local lease, no cycle/inconclusive | `verification_runs` | Convert only with valid trigger/revisions/profile/check/sample semantics | Drop legacy lease/plan fields after converted/terminal proof |
| `verification_checks` | Mutable latest observations, no sample table | `verification_checks` + `verification_samples` | Preserve terminal facts; do not fabricate samples; incompatible profile cancels Run | Drop legacy observation fields after projection parity |
| `postmortems` | One deterministic V2 narrative per Incident | `legacy_postmortem_archive` | Preserve source id, content hash and timestamps; expose only in internal migration audit | Drop source table only after archive hash/count proof |

### 3.2 Runtime AutoMigrate tables

Phase 1 disposable-MySQL schema parity and sentinel preservation are `PASS`. Row counts and hashes from any existing deployed database remain `NOT RUN`; the local sentinel fixture is not a production inventory or pre-contract archive.

| Current model/table | Decision | Required pre-contract evidence |
|---|---|---|
| `users` | Archive identity mapping, then DELETE | row count/hash, no local-login callers, OAuth actor mapping documented |
| `host_groups`, `host_group_members` | DELETE | export/count/hash; no V3 host-monitoring caller |
| `alert_rules`, `notification_channels` | DELETE | export/count/hash; Prometheus/Alertmanager configuration is authoritative |
| `alert_histories` | Archive only; do not promote to V3 Signal | count/hash and duplicate/correlation analysis |
| `diagnosis_reports`, `diagnosis_feedback` | Archive; never use as V3 Evidence or confirmed Diagnosis | id/content hash/time and zero V3 projection use |
| `pending_actions` | Archive; old approvals/actions never authorize V3 writes | external-side-effect audit and zero executor caller |
| `audit_logs` | Export bounded immutable audit, then DELETE legacy writer | row count/hash and V3 Timeline/Decision coverage |

Before runtime AutoMigrate is removed, the first new forward migration(s), starting after `00006`, must explicitly own any legacy schema still required by the compatibility binary and validate an existing AutoMigrated schema. A fresh database and an existing V2 database must both reach the same verified schema.

Phase 1 satisfied that expand-only boundary on disposable MySQL `8.0.46-0ubuntu0.24.04.3`:

| Phase 1 migration control | Status | Evidence |
|---|---|---|
| Fresh database `00001` through `00007` | PASS | Forward Up reached schema version 7 and created the Goose-owned and ten compatibility tables |
| Existing V2 database upgrade | PASS | `00001` through `00006` plus the actual legacy GORM model fixture upgraded to 7 without conversion |
| Fresh/existing target schema parity | PASS | Canonical `information_schema` snapshot SHA-256 `ac1a0e1cd0882d55eea7d4d13356581f0c309d7d9001e0d994d23efd7d1d3e3c`; columns, defaults, indexes, constraints, engines and collations are included |
| Existing data preservation | PASS | All ten sentinel tables had identical pre/post canonical data SHA-256 `01320e2b8b0ea4375fe5abc55b48bdc8cfbb17c3f878c9d47a021de4db10afcf` |
| Repeated Up | PASS | Applied zero new migrations; schema and data hashes remained unchanged |
| Concurrent Up | PASS | Two independent runners completed; `goose_db_version` contains exactly one applied row for version 7 |
| Advisory lock blocking/release | PASS | A pre-held legacy-compatible lock kept the database at version 6; release allowed version 7; an intentionally invalid test-only migration also proved failure releases the lock for another connection |
| Forward-only command | PASS | DDL identity reached version 7; repeat `up` exited 0; `cloudops-migrate down` exited non-zero and no production Down/Redo/Reset call exists |
| DML-only runtime startup | PASS | `phase1_dml@127.0.0.1` had only USAGE plus SELECT/INSERT/UPDATE/DELETE on the disposable runtime database; API and Worker returned ready, shut down, and left the table count unchanged |
| Production AutoMigrate call scan | PASS | No non-test Go source contains an `AutoMigrate` call; the remaining calls are test-only existing-V2 schema fixtures |

Phase 2 validated `00008` again after the final code changes on a disposable MySQL 8.0 container (`mysql:8.0`). This is local disposable evidence, not an inventory or mutation of a deployed database.

| Phase 2 migration control | Status | Evidence |
|---|---|---|
| Forward file identity | PASS | `migrations/00008_expand_v3_async_runtime.sql`, 583 lines, Git blob `1a1e413be591f6dd7d6dd649e1fb56f19a656aec`, SHA-256 `a769354179532733b6216fbfde699cf756744f72dd75c98cf730feb2e093e96e` |
| `00001` through `00007` immutable | PASS | All seven blob and SHA-256 identities in section 2 remained byte-identical; `migrations.TestImmutableMigrationHistory` passed |
| Expand-only structure | PASS | Static test found no DML, table/column drop, rename, backfill, conversion, Down section or `CUTOVER_V3` marker |
| Fresh database `00001` through `00008` | PASS | Goose reached schema version 8 and all five Phase 2 tables were InnoDB |
| Existing Phase 1 database upgrade | PASS | Version-7 schema plus the complete legacy sentinel fixture upgraded to 8; every pre-`00008` affected-table column value was unchanged |
| Fresh/existing target schema parity | PASS | Canonical `information_schema` SHA-256 `90b6973314684886e4b6fade62be5002e311620da50abb3d40a84f0af016d0e4` |
| Existing legacy data preservation | PASS | Phase 1 ten-table sentinel SHA-256 remained `01320e2b8b0ea4375fe5abc55b48bdc8cfbb17c3f878c9d47a021de4db10afcf`; the Phase 2 affected-domain snapshot was also unchanged |
| Generated-key conflicts | PASS | Real duplicate-key negatives covered active Incident, AgentRun, RemediationPlan, ChangeRequest, VerificationRun and versioned verification trigger identity; generated expressions were compared with their status enums |
| Ready/takeover query plans | PASS | MySQL JSON EXPLAIN selected `idx_async_tasks_ready_claim` and `idx_async_tasks_expired_takeover` with `range` access; neither plan used `ALL` |
| Repeat/concurrent/advisory-lock behavior | PASS | Repeated and concurrent Up converged on one applied version-8 row; held-lock and failure-release tests passed |
| Data conversion/cutover | NOT RUN | No legacy row, state, lease, outbox or task was converted; no marker was written and no old/new Worker pair was run concurrently |

Phase 5/6 validated the `00009` schema expansion on disposable MySQL `8.0.46-0ubuntu0.24.04.3`. This evidence covers DDL and bounded persistence contracts only; it is not a deployed-database inventory and does not claim that external remediation, delivery, verification workers or cutover are enabled.

| Phase 5/6 migration control | Status | Evidence |
|---|---|---|
| Forward file identity | PASS | `migrations/00009_expand_v3_remediation_verification.sql`, 526 lines, Git blob `7c5ab3bef5edee0b38dcb8f54a84fe732fbe88f9`, SHA-256 `944ca629c190fe82a77e36ac7b35bfff4017fb8b59f3fb4db333918337d9c927` |
| `00001` through `00008` immutable | PASS | Recorded historical blobs remain byte-identical; `migrations.TestImmutableMigrationHistory` covers `00001` through `00008` |
| Forward-only structure | PASS | Static contract rejects DML, table/column drops, rename, cutover marker and Down section; CHECK replacement only broadens compatibility enums |
| Fresh database `00001` through `00009` | PASS | Disposable MySQL reached schema version 9; existing Phase 2 generated-key migration tests also passed against the new latest version |
| Existing partial V3 rows | PASS | A database stopped at version 8 with partial Plan/Verification rows upgraded to 9; nullable contract versions remained NULL and rows stayed readable |
| Complete Plan/Decision persistence | PASS | Full restore Plan bindings and hash-bound `remediation_decisions` insert; partial opt-in rejected by `chk_remediation_plans_v3_complete` |
| Verification Sample/ResolutionReport persistence | PASS | Frozen CheckSpec fields, one bounded Sample and no-change ResolutionReport inserted with FK/unique/check constraints |
| External backfill, state conversion and cutover | NOT RUN | No legacy row conversion, external write, marker, or old/new Worker concurrency was executed |

## 4. Forward Units

Phase 1 allocated `00007`; Phase 2 allocated `00008`; later expand work reached `00015`; Phase 7A archive ownership is allocated by `00016`. The live `cutover-prepare` and irreversible `cutover-write` operations remain operator-run units, not migration-time side effects.

| Ledger unit | Earliest owner | Purpose | Compatibility boundary | Required evidence |
|---|---|---|---|---|
| `EXPAND-LEGACY-SCHEMA` | Phase 1 | Implemented by `migrations/00007_expand_legacy_schema.sql`; transfers all ten compatibility tables from AutoMigrate to explicit Goose ownership | V2 behavior remains readable; no state/data/lease/outbox conversion | PASS locally: fresh/existing parity, data preservation, repeat/concurrent/lock and DML-only runtime evidence in section 3.2 |
| `EXPAND-INCIDENT-TASK` | Phase 2 | Implemented by `migrations/00008_expand_v3_async_runtime.sql`; adds cycle, active generated keys, `async_tasks`, attempts, command idempotency, signal rejection and migration ledger | Old rows remain unmodified; new enums live only in nullable compatibility fields/new tables; no cutover | PASS locally: fresh/existing parity, preservation, generated-key negatives, EXPLAIN, queue concurrency, typed registry and fail-closed missing-operation boundary |
| `EXPAND-INVESTIGATION` | Phase 4 | Add V3 checkpoint/StateDelta/Evidence producer/trust fields and assessment tables | V2 facts remain archive-readable | converter fixtures and cross-cycle rejection |
| `EXPAND-OBSERVABILITY` | Phase 3-4 | Add typed source/template/provenance fields needed by real Metric/Log/Trace/K8s Evidence | No external raw data copy | adapter contract and bounded payload proof |
| `EXPAND-REMEDIATION` | Phase 5 | Implemented by `migrations/00009_expand_v3_remediation_verification.sql`; adds complete Plan bindings, immutable `remediation_decisions` and append-only ChangeRequest events | V2 Approval remains non-actionable; partial 00008 rows remain readable | hash-bound Decision, contract CHECK/FK and partial-opt-in negative |
| `EXPAND-VERIFICATION` | Phase 6 | Implemented by `migrations/00009_expand_v3_remediation_verification.sql`; adds frozen profile/check fields, `verification_samples`, common-window projection and `resolution_reports` | V2 Postmortem remains separate; legacy partial rows retain NULL contract version | CheckSpec/min-sample/failure-mode, Sample FK and no-change report persistence |
| `EXPAND-CUTOVER-ARCHIVE` | Phase 7A | Implemented by `migrations/00016_phase7a_cutover_archives.sql`; creates archive/control/audit ownership and provenance columns | No source row is converted and no marker is written during Goose Up | forward-only static contract and schema-version binding |
| `BACKFILL-V3` | Phase 7A pre-cutover | Batch immutable facts/cycle/references/projections and archive legacy narratives/outbox | Old Worker is still sole live executor during batch work | per-batch count/range/input-output hash/status |
| `CUTOVER-V3` | Phase 7A release A | Quiesce, reconcile external state, derive tasks from compatible child rows, convert states, write marker, start only V3 Worker | Old binary must refuse marker; no rollback after claim/new state | maintenance transcript, zero old lease, task anti-join, marker test |
| `GOLDEN-AUDIT` | Phase 7A release A | Run exact-SHA Agent Quality and Golden E2E after cutover; export audit | No deletion yet | manifest, exact images/revisions, full stable-window PASS |
| `CONTRACT-V3` | Phase 7B release B | Delete legacy claim paths/lease columns/tables/deploy assets after an independent review | Forward fix only | Golden/audit accepted; zero old image/lease/caller; backup/export hashes |

Phase 2's "V3 task is the only claim path" is a code/test Gate for the V3 compatibility binary. It does not authorize live data cutover or concurrent old/new workers. The live task/state conversion and irreversible marker occur only in Phase 7A after all converters exist.

The one-shot operator flow is now explicit:

```text
cloudops-migrate cutover-prepare <exact release identity and four observed zero counts>
cloudops-migrate cutover-export
cloudops-migrate cutover-write <three returned ledger IDs and CUTOVER-V3 confirmation>
```

`cutover-prepare` holds a named MySQL lock and one serializable transaction, rejects unknown outbox types, active legacy leases/running V3 tasks and non-zero external observations, archives before conversion, uses subject anti-joins, and emits `QUIESCE-V3`, `RECONCILE-EXTERNAL-WRITES-V3`, and `CONVERTER-AUDIT-V3`. The command is implemented but has not been executed against a deployed database in this report.

## 5. Backfill Ledger Contract

`migration_ledger` must record at least:

```text
id / public_id
plan_version / stage / operation
source_schema_version / target_schema_version
source_table / target_table
batch_no / id_min / id_max
source_count / target_count / skipped_count / rejected_count
source_hash / target_hash
converter_version
started_at / completed_at
status: running | passed | failed
reason_code / bounded_summary
source_exact_sha / binary_image_digest
```

Hashing uses versioned canonical encoding, not `CONCAT`. Each batch is idempotent by `(plan_version, operation, batch_no)` and cannot be overwritten after `passed` or `failed`; retries create a new attempt record linked to the prior row.

Required backfill order:

1. Inventory schema version, binary images, active leases, child states, outbox event types and external-write markers.
2. Backfill immutable Signal/Event/Evidence/Step/Candidate facts with `cycle_no=1` and `migrated_legacy=true`.
3. Backfill child references and read projections.
4. Archive all legacy outbox and Postmortem records with count/hash proof.
5. Run converter fixtures against sampled and boundary rows before maintenance mode.
6. Do not create/claim V3 work until quiesce and cutover lock.

## 6. Outbox and Task Conversion

### 6.1 Actual outbox rule

The audited V2 `outbox_events` table is a domain-event audit outbox, not a job queue. Current code has no relay/claim/mark-published implementation. Therefore:

| Actual row predicate | V3 action |
|---|---|
| `published_at IS NULL` | Archive as `legacy_outbox_unpublished`; record event type/schema/count/hash; do not create a Task |
| `published_at IS NOT NULL` | Archive as `legacy_outbox_published`; do not create a Task |
| unknown event type/schema or invalid payload | `CUTOVER FAIL`; retain source row and ingress stopped |
| any event type that appears to encode an external operation | `CUTOVER FAIL` until an explicit versioned mapper and external reconciliation prove whether the operation exists; never infer from payload text |

`attempts` and `last_error` are audit metadata. They do not prove ready/running/dead semantics.

### 6.2 Task derivation

Tasks are derived only from target `async_tasks` already created by the V3 compatibility path or from compatible non-terminal child aggregates under the cutover lock. An anti-join must prove exactly one active logical operation.

| Legacy subject | Converter outcome |
|---|---|
| pending/running AgentRun | Valid versioned checkpoint -> one `investigation.advance`; invalid checkpoint -> cancel/archive old Run and enqueue one Incident-scoped `investigation.start` |
| ChangeRequest with complete Draft PR or merged state | Reconcile first; create one read-only `delivery.observe`; never create a write task from old Approval |
| ChangeRequest with only branch/commit | Mark failed/superseded + attention; archive artifact; no commit/PR completion write |
| ChangeRequest with no external write but only V2 Approval | Cancel/fail and supersede Plan; return Incident to investigating; no PR |
| pending/running VerificationRun | Valid versioned trigger/revision/profile/check/sample conversion -> one `verification.advance`; otherwise cancel and return Incident to investigating + attention |

Each dedupe key includes legacy subject ID, source version, cycle, expected transition and converter version. Ledger counts are `subject-derived`, `existing-target-task`, `anti-join-skipped`, `conversion-failed` and `task-created`; there is no `outbox-derived-task` category.

## 7. State Conversion

| V2 state | V3 conversion |
|---|---|
| `DETECTED` | `detected` |
| `CORRELATING`, `DIAGNOSING`, `DIAGNOSIS_COMPLETED`, `PLANNING_REMEDIATION` | `investigating` |
| `AWAITING_APPROVAL` | `awaiting_approval`; old Plan/Approval remains non-actionable |
| `APPLYING_CHANGE` | `delivering` only when external write is observed/unknown; otherwise reconcile/cancel and return `investigating` |
| `VERIFYING` | `verifying` only for a compatible active VerificationRun; otherwise cancel + `investigating`/attention |
| `RESOLVED` | `resolved` only with a compatible passed Verification; otherwise `investigating` + `legacy_resolution_unverified` |
| `CLOSED_NO_ACTION` | `closed` |
| `FAILED` + active VerificationRun | `verifying` + `legacy_failed_blocked` |
| `FAILED` + observed/unknown ChangeRequest write | `delivering` + `legacy_failed_blocked` |
| `FAILED` + Plan/Approval but no external write | supersede Plan; `investigating` + `legacy_approval_incomplete` |
| other `FAILED` | `investigating` + `legacy_failed_blocked` |

All converted records are `needs_attention` until the migration audit validates child state. A status conversion appends an IncidentEvent; no source row is silently rewritten without ledger evidence.

## 8. Quiesce and Cutover Checklist

| Order | Control | Required result |
|---:|---|---|
| 1 | Pin source SHA, API/Worker/Migrate image digests and schema version | PASS |
| 2 | Enable maintenance mode; stop webhook ingress and user mutation | PASS |
| 3 | Stop every old Worker claim loop | PASS |
| 4 | Wait for all legacy leases to expire; identify running external calls | zero active lease or explicit blocker |
| 5 | Reconcile every external write intent/result | no unknown unsafe write |
| 6 | Acquire cutover advisory/row lock with MySQL time | PASS |
| 7 | Verify every backfill batch count/hash | PASS |
| 8 | Archive outbox/Postmortem/legacy tables | PASS |
| 9 | Convert compatible child state and derive tasks with anti-join | exactly one task per live logical operation |
| 10 | Convert Incident/child state and append migration events | PASS |
| 11 | Write irreversible `CUTOVER_V3` marker | PASS |
| 12 | Prove old binary refuses startup | PASS |
| 13 | Start only V3 API/Worker; restore ingress | PASS |
| 14 | Run exact-SHA Golden and export audit | PASS before any contract deletion |

Any failed count/hash, unknown event type, incompatible generated expression, external write unknown, old active lease or duplicate/missing task keeps ingress closed and makes cutover `FAIL`.

## 9. Rollback and Contract

- Before `CUTOVER_V3`, the compatibility binary may roll back only against an expand-compatible schema.
- After the marker, a new V3 state or any V3 Task claim, database/binary rollback is forbidden. Only a forward fix is allowed.
- Release A performs cutover and Golden validation while retaining all legacy columns/tables/archives.
- Release B performs contract deletion only after independent review of Release A evidence.
- Release B must prove no old Deployment/Job image, no old-binary startup, zero active legacy lease, zero legacy caller, complete export hashes and all ledger rows `passed`.
- Goose Down remains a local recovery tool in immutable historical files; it is not a Kubernetes upgrade/rollback mechanism.

## 10. Migration Status

| Control | Status |
|---|---|
| 00001-00006 identity and purpose recorded | PASS |
| 00001-00006 root-path blobs and SHA-256 remain byte-identical | PASS |
| 00007 path, purpose, blob and SHA-256 recorded | PASS |
| 00008 path, purpose, blob and SHA-256 recorded | PASS |
| 00009 path, purpose, blob and SHA-256 recorded | PASS |
| 00018 path, reconciliation purpose, blob and SHA-256 recorded | PASS |
| 00018 orphaned-Run reconciliation and live-replay preservation on MySQL 8 | PASS |
| 00001-00007 remained byte-identical during Phase 2 | PASS |
| Legacy state/lease/outbox ownership recorded | PASS |
| AutoMigrate table risk recorded | PASS |
| Expand/backfill/quiesce/cutover/contract path executable on paper | PASS |
| Phase 1 disposable fresh/existing schema parity | PASS |
| Phase 1 disposable existing-data preservation | PASS |
| Repeat/concurrent/advisory-lock/failure-release behavior | PASS |
| DML-only API/Worker startup without schema mutation | PASS |
| Runtime production AutoMigrate call absence | PASS |
| Phase 2 disposable fresh/existing schema parity and legacy preservation | PASS |
| Phase 2 generated-key and ready/takeover EXPLAIN Gate | PASS |
| Phase 2 typed registry, `investigation.start` and missing-operation fail closed | PASS |
| Phase 5/6 disposable 00009 persistence expansion | PASS |
| Subject-bound task handlers | NOT RUN; owning Phases 4-6 |
| Existing deployed database row counts/hashes | NOT RUN |
| Backfill, state/lease/outbox conversion, CUTOVER-V3 or CONTRACT-V3 | NOT RUN |

This ledger records local Phase 1, Phase 2, and 00009 Phase 5/6 expand evidence only. It does not claim that any existing deployed database was migrated or that all subject-bound business operations exist: the V3 compatibility worker remains fail-closed until the owning operations are registered. Phase 7A backfill/cutover and Phase 7B contract work remain `NOT RUN`.
