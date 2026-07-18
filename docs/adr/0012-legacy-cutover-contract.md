# ADR 0012: Legacy Cutover and Contract Deletion

- Status: Accepted target decision; data inventory/cutover NOT RUN
- Date: 2026-07-18
- Owner: Phase 7A and Phase 7B

## Context

V2 has immutable migrations 00001-00006, runtime AutoMigrate tables, 11 Incident states, three leases, incomplete approvals, external artifacts and an audit outbox. Destructive cleanup before conversion and exact Golden proof could lose facts or repeat effects.

## Decision

Never edit 00001-00006. Use forward expand, restartable hashed backfill, quiesce, external reconciliation, task/state conversion, irreversible CUTOVER_V3 marker and later contract.

The current outbox is archived by published state/event type and never generates tasks. Tasks derive only from existing V3 tasks or compatible non-terminal child converters with anti-join.

Old Approval cannot authorize new writes. Only an existing complete Draft PR/merged request may receive read-only delivery observation. Incompatible Agent/Verification checkpoints are cancelled and a new Incident-scoped investigation.start Task is created.

Phase 7A Release A performs quiesce/conversion/marker and exact-SHA Golden/audit while retaining legacy schema/claim assets. After the marker/new state/task claim, rollback to an old binary is forbidden.

Phase 7B Release B is a separate exact-SHA contract release. It deletes legacy paths only after Release A evidence is accepted, old images/leases/callers are zero and exports/counts/hashes pass.

## Consequences

- Conversion mismatch or unknown external state keeps ingress closed and fails cutover.
- V2 migrated facts are marked and never count as new V3 quality/Golden evidence.
- Failure after cutover requires a forward fix.
- Cleanup includes code, schema, Compose/raw assets and stale runtime inventory, not just tables.

## Rejected Alternatives

- Rename outbox_events into async_tasks.
- Map V2 approvals or narratives into V3 authority.
- Cut over and drop legacy assets in one release.
- Manual database edits to make counts match.

## Evidence Required

Ledger batch hashes, converter fixtures, zero active leases, task uniqueness, old-binary marker refusal, exact-SHA Golden Release A and independent Release B cleanup/smoke evidence.
