# ADR 0042: Local Retention, Backup and Reset

- Status: Accepted local-data decision; implementation NOT RUN
- Date: 2026-07-26
- Refines: ADR 0030, ADR 0035, ADR 0040 and ADR 0041

## Context

The Make-managed local lifecycle needs predictable behavior when the stack stops, a Demonstration Scenario is removed or the Owner wants a clean environment. Keeping all raw telemetry forever would consume unbounded local disk, while deleting Agent, Alert or Incident history with routine shutdown would make the product unreliable and erase the Operational Loop's provenance.

## Decision

`make local-down` preserves CloudOps persistent state. MySQL domain records, Operational Configuration and Configuration Revisions, versioned secret files, Alerts, Incidents, Agent Consultations, Operation history and retained Evidence have no automatic age-based deletion by default. Their lifecycle is explicit archive, deletion, Scenario-history cleanup or `make local-reset`.

Provider engines continue to own raw telemetry. CloudOps retains only bounded sanitized Evidence and source references; it does not copy complete Metrics ranges, log streams or Traces into MySQL. The managed local defaults are 7 days for Prometheus Metrics, 7 days for Elasticsearch Logs and 72 hours for Tempo Traces, each with a configured storage-size ceiling exposed through Settings.

`make local-backup` creates a versioned, checksummed, Git-ignored backup containing MySQL domain data, Operational Configuration metadata and the backend-managed versioned secret files required to restore it. Backup files and directories use private local permissions. Raw Provider telemetry is not part of the default CloudOps backup.

`make local-restore BACKUP=...` validates backup version, checksums and target state before applying it. It restores configuration and secrets consistently with the database revision references rather than importing them independently.

`make local-reset` is the only general lifecycle command that removes persistent CloudOps state. It creates and verifies a backup first; backup failure aborts reset. Skipping that backup requires a separate explicit destructive flag and confirmation. Routine repair, restart, upgrade and down operations cannot invoke reset implicitly.

`make scenario-down` removes only the live Demonstration Scenario resources and preserves tagged CloudOps history. A separate confirmed Scenario-history cleanup can delete unreferenced Scenario records without affecting Live Mode records or Evidence retained by another lifecycle.

## Consequences

- Persistent volumes and backend-managed secret storage must survive normal local lifecycle commands.
- Retention status, storage use, backup recency and reset scope are visible in Settings and `make local-status` or `make local-doctor`.
- Deletion respects domain references: removing a Consultation or Scenario cannot erase Evidence retained by an Alert, Incident or Operation record.
- Restore and reset require full-stack integration validation because database and secret-version identities must remain aligned.
- Provider telemetry can expire while retained Evidence remains; the UI distinguishes a retained Evidence summary from a still-queryable raw source window.

## Rejected Alternatives

- Delete the kind cluster and all state on every `local-down`.
- Retain raw local telemetry indefinitely without size bounds.
- Back up MySQL while omitting secret versions referenced by its Configuration Revisions.
- Let Scenario cleanup or reset erase retained history without explicit scope and confirmation.
