# V3 Migration Ledger

> Status: Phase 0 migration contract
>
> Normative source: [`CloudOps-Incident-Agent-V3-Refactor-Design.md`](CloudOps-Incident-Agent-V3-Refactor-Design.md)
>
> Audited source: `main@2f7e426d69a4ed7d8d32ec3ca83c13af0c71586e`

## 1. Invariants

1. `00001` through `00006` are immutable history. Their contents, names and Down sections must never be edited.
2. V3 uses forward-only migrations beginning after `00006`.
3. Runtime binaries never execute AutoMigrate. Kubernetes upgrades never execute Goose Down.
4. All schema/data transitions follow `expand -> backfill -> quiesce -> cutover -> contract`.
5. MySQL `NOW(6)` is authoritative for cutover locks and leases.
6. Backfill/conversion is restartable, batch-recorded and hash-verified. A mismatch is `FAIL`, not a warning.
7. Legacy rows marked `migrated_legacy=true` cannot satisfy V3 Golden E2E, Agent Quality or resume claims.
8. A V2 Approval never authorizes a new V3 external write.
9. An unknown external-write result blocks cutover until read-only reconciliation establishes a safe state.
10. Contract deletion is a later release after cutover and Golden evidence; it is never bundled with first cutover.

## 2. Immutable Goose History

The Git blob is the repository content address. SHA-256 is recorded for external tooling. `Current data` is `NOT RUN` because Phase 0 did not connect to either local MySQL container.

| Version | File | Lines | Git blob | SHA-256 | Current schema effect | V3 treatment |
|---|---|---:|---|---|---|---|
| 00001 | `server-monitor/server-web/migrations/00001_incident_foundation.sql` | 200 | `042bda4fd6a0ddce32e7ab61ab6f0a1c1c06cb4f` | `8f7dd2e188fba00f6a7cce45f7c2f2319a4f80c7df7882c1a3e4c6471bce080d` | Creates Incident, AgentRun/Step, Signal, Event, Evidence, outbox and correlation-lock tables; adds `incidents.current_agent_run_id` | KEEP history; expand cycle/identity/generated keys; eventually remove circular current-run FK and archive outbox |
| 00002 | `server-monitor/server-web/migrations/00002_agent_runtime.sql` | 116 | `98942847d3c28c02d32d48c6cbeac69a7358f165` | `f515765f604391c933bc59b6fd3b7c7d3cfd17f3c429a403bcc014b82d370411` | Adds Agent budgets, whole checkpoint, idempotency, active generated key and AgentRun lease/heartbeat | KEEP history; versioned checkpoint conversion; task owns future lease; contract removes legacy lease |
| 00003 | `server-monitor/server-web/migrations/00003_change_intelligence.sql` | 77 | `7fc0aa8f3a5e3f6b49a369b52a7c0d2bf52ee4ae` | `584fda2c41ca7657228aba5f0b4b0e2d62bd2a4cfd54ec386198c22e163fd0f6` | Creates mutable `changes`; adds one-to-one-ish `evidence_items.change_id` | KEEP history; backfill immutable Candidate and append-only Assessment; contract removes legacy coupling/table |
| 00004 | `server-monitor/server-web/migrations/00004_gitops_remediation.sql` | 98 | `b47e1a3bb8aeffb523ab45c3e2f75fc67a3eace7` | `fc4773b65f89626bcfb678526d3c631bef89188007df00cda7a96813d7f95c84` | Creates Plans, Approvals and ChangeRequests; operations are `rollback_image`/`set_replicas`; ChangeRequest owns a lease | KEEP history; add immutable V3 hashes/cycle/phase; archive old Approval; contract removes lease and forbidden operation path |
| 00005 | `server-monitor/server-web/migrations/00005_delivery_verification.sql` | 178 | `7f142552d88e0f4760491cd86a1bf6430aa1cb19` | `8168e75a60f18ba5b8818c82b6dda71131adc8b9cca1f52eb93abce111f4a61d` | Expands many delivery statuses; creates leased VerificationRun and VerificationCheck | KEEP history; add cycle/trigger/profile/sample/inconclusive; compatible conversion only; contract removes lease |
| 00006 | `server-monitor/server-web/migrations/00006_observability_verification_postmortem.sql` | 69 | `f3857c1a46c16cedc6ae8be81be7bd462ad2d613` | `8e1a0b1f0a1125ffb54f8049be17617c1130cebbf446ecc371a281b4a7301fdb` | Adds observability check types/status and creates `postmortems` | KEEP history; archive as legacy Postmortem; never convert narrative into Evidence/Diagnosis/ResolutionReport |

All six migrations use Goose `NO TRANSACTION`; DDL is not atomically rolled back. Every future release must verify pre/post schema and ledger state explicitly.

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

Current row counts and schema parity are `NOT RUN` until an authorized real-MySQL audit.

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

## 4. Planned Forward Units

Exact filenames are allocated by the owning phase immediately before implementation. No Phase 0 document fabricates checksums for files that do not yet exist. The next numeric Goose version is `00007`.

| Ledger unit | Earliest owner | Purpose | Compatibility boundary | Required evidence |
|---|---|---|---|---|
| `EXPAND-LEGACY-SCHEMA` | Phase 1 | Transfer required legacy tables from AutoMigrate to explicit Goose ownership; add binary/schema compatibility check | V2 behavior remains readable; no state conversion | fresh/existing MySQL schema parity; runtime no AutoMigrate |
| `EXPAND-INCIDENT-TASK` | Phase 2 | Add cycle, active generated keys, `async_tasks`, attempts, command idempotency, signal rejection and migration ledger | Old binary can read old columns only; new enums live in new fields/tables | generated-key negative tests, EXPLAIN, queue concurrency |
| `EXPAND-INVESTIGATION` | Phase 4 | Add V3 checkpoint/StateDelta/Evidence producer/trust fields and assessment tables | V2 facts remain archive-readable | converter fixtures and cross-cycle rejection |
| `EXPAND-OBSERVABILITY` | Phase 3-4 | Add typed source/template/provenance fields needed by real Metric/Log/Trace/K8s Evidence | No external raw data copy | adapter contract and bounded payload proof |
| `EXPAND-REMEDIATION` | Phase 5 | Add baseline tables, immutable Plan/Decision hashes, ChangeRequest events/write phase | V2 Approval remains non-actionable | hash/policy/preflight and zero-write negatives |
| `EXPAND-VERIFICATION` | Phase 6 | Add trigger identity, profile/hash, samples, common-window fields and ResolutionReport | V2 Postmortem remains separate | no-change/post-delivery concurrency and stable-window tests |
| `BACKFILL-V3` | Phase 7A pre-cutover | Batch immutable facts/cycle/references/projections and archive legacy narratives/outbox | Old Worker is still sole live executor during batch work | per-batch count/range/input-output hash/status |
| `CUTOVER-V3` | Phase 7A release A | Quiesce, reconcile external state, derive tasks from compatible child rows, convert states, write marker, start only V3 Worker | Old binary must refuse marker; no rollback after claim/new state | maintenance transcript, zero old lease, task anti-join, marker test |
| `GOLDEN-AUDIT` | Phase 7A release A | Run exact-SHA Agent Quality and Golden E2E after cutover; export audit | No deletion yet | manifest, exact images/revisions, full stable-window PASS |
| `CONTRACT-V3` | Phase 7B release B | Delete legacy claim paths/lease columns/tables/deploy assets after an independent review | Forward fix only | Golden/audit accepted; zero old image/lease/caller; backup/export hashes |

Phase 2's "V3 task is the only claim path" is a code/test Gate for the V3 compatibility binary. It does not authorize live data cutover or concurrent old/new workers. The live task/state conversion and irreversible marker occur only in Phase 7A after all converters exist.

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

## 10. Phase 0 Status

| Control | Status |
|---|---|
| 00001-00006 identity and purpose recorded | PASS |
| Legacy state/lease/outbox ownership recorded | PASS |
| AutoMigrate table risk recorded | PASS |
| Expand/backfill/quiesce/cutover/contract path executable on paper | PASS |
| Existing database row counts/hashes/schema parity | NOT RUN |
| Any migration, DDL, DML, backfill or cutover executed | NOT RUN |

This ledger is a required implementation input. It is not evidence that any database has been migrated.
