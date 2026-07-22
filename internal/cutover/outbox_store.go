package cutover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type outboxArchiveSummary struct {
	Published         uint64
	Unpublished       uint64
	ExternalWriteRows uint64
	SourceHash        string
	ArchiveHash       string
}

func ensureOutboxRegistry(ctx context.Context, tx *sql.Tx) error {
	if tx == nil {
		return errors.New("outbox registry transaction is required")
	}
	for _, entry := range OutboxRegistry() {
		var aggregateType, mapper, fixtureHash string
		var external bool
		err := tx.QueryRowContext(ctx, `SELECT aggregate_type,archive_mapper,external_write_event,fixture_hash
FROM legacy_outbox_event_registry
WHERE registry_version=? AND event_type=? AND schema_version=? FOR UPDATE`, OutboxRegistryVersion,
			entry.EventType, entry.SchemaVersion).Scan(&aggregateType, &mapper, &external, &fixtureHash)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_outbox_event_registry
(registry_version,event_type,schema_version,aggregate_type,archive_mapper,external_write_event,fixture_hash)
VALUES (?,?,?,?,?,?,?)`, OutboxRegistryVersion, entry.EventType, entry.SchemaVersion, entry.AggregateType,
				entry.ArchiveMapper, entry.ExternalWrite, entry.FixtureHash); err != nil {
				return fmt.Errorf("insert outbox registry %s/v%d: %w", entry.EventType, entry.SchemaVersion, err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if aggregateType != entry.AggregateType || mapper != entry.ArchiveMapper || external != entry.ExternalWrite || fixtureHash != entry.FixtureHash {
			return fmt.Errorf("outbox registry drift for %s/v%d", entry.EventType, entry.SchemaVersion)
		}
	}
	var count uint64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM legacy_outbox_event_registry WHERE registry_version=?", OutboxRegistryVersion).Scan(&count); err != nil {
		return err
	}
	if count != uint64(len(OutboxRegistry())) {
		return fmt.Errorf("outbox registry version=%d rows=%d want=%d", OutboxRegistryVersion, count, len(OutboxRegistry()))
	}
	return nil
}

func archiveOutboxRows(ctx context.Context, tx *sql.Tx, externalReconciliationConfigured bool, at time.Time) (outboxArchiveSummary, error) {
	if tx == nil {
		return outboxArchiveSummary{}, errors.New("outbox archive transaction is required")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,event_id,aggregate_type,aggregate_id,event_type,schema_version,
payload_json,occurred_at,published_at,attempts,last_error,created_at
FROM outbox_events ORDER BY id FOR UPDATE`)
	if err != nil {
		return outboxArchiveSummary{}, err
	}
	sourceRows := make([]LegacyOutboxRow, 0)
	for rows.Next() {
		var row LegacyOutboxRow
		var payload []byte
		var published sql.NullTime
		if err := rows.Scan(&row.ID, &row.EventID, &row.AggregateType, &row.AggregateID, &row.EventType,
			&row.SchemaVersion, &payload, &row.OccurredAt, &published, &row.Attempts, &row.LastError, &row.CreatedAt); err != nil {
			_ = rows.Close()
			return outboxArchiveSummary{}, err
		}
		row.Payload = append(row.Payload[:0], payload...)
		if published.Valid {
			value := published.Time.UTC()
			row.PublishedAt = &value
		}
		sourceRows = append(sourceRows, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return outboxArchiveSummary{}, err
	}
	if err := rows.Close(); err != nil {
		return outboxArchiveSummary{}, err
	}

	var summary outboxArchiveSummary
	sourceHashes := make([]string, 0, len(sourceRows))
	for _, row := range sourceRows {
		decision, err := ValidateOutboxArchive(row, externalReconciliationConfigured)
		if err != nil {
			return outboxArchiveSummary{}, fmt.Errorf("archive outbox id=%d type=%s schema=%d: %w", row.ID, row.EventType, row.SchemaVersion, err)
		}
		payloadHash, _, err := canonicalHashJSON(row.Payload)
		if err != nil {
			return outboxArchiveSummary{}, fmt.Errorf("canonicalize outbox payload id=%d: %w", row.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_outbox_archive (
source_outbox_id,source_schema_version,event_id,aggregate_type,aggregate_id,event_type,schema_version,
publication_state,payload_json,row_snapshot_json,row_hash,registry_version,conversion_status,reason_code,
payload_hash,occurred_at,published_at,archived_at)
VALUES (?,1,?,?,?,?,?,?,?,?,?,?,'passed',?,?,?,?,?)
ON DUPLICATE KEY UPDATE source_outbox_id=VALUES(source_outbox_id)`, row.ID, row.EventID, row.AggregateType,
			row.AggregateID, row.EventType, row.SchemaVersion, decision.Publication, []byte(row.Payload), []byte(decision.Snapshot),
			decision.RowHash, decision.RegistryVersion, decision.ReasonCode, payloadHash, row.OccurredAt.UTC(), row.PublishedAt, at.UTC()); err != nil {
			return outboxArchiveSummary{}, fmt.Errorf("persist outbox archive id=%d: %w", row.ID, err)
		}
		var archivedEventID, archivedAggregateType, archivedAggregateID, archivedEventType, publication, archivedRowHash, status, reason string
		var archivedSchema uint32
		var registryVersion uint16
		if err := tx.QueryRowContext(ctx, `SELECT event_id,aggregate_type,aggregate_id,event_type,schema_version,
publication_state,row_hash,registry_version,conversion_status,reason_code
FROM legacy_outbox_archive WHERE source_outbox_id=? FOR UPDATE`, row.ID).Scan(&archivedEventID, &archivedAggregateType,
			&archivedAggregateID, &archivedEventType, &archivedSchema, &publication, &archivedRowHash,
			&registryVersion, &status, &reason); err != nil {
			return outboxArchiveSummary{}, err
		}
		if archivedEventID != row.EventID || archivedAggregateType != row.AggregateType || archivedAggregateID != row.AggregateID ||
			archivedEventType != row.EventType || archivedSchema != row.SchemaVersion || publication != decision.Publication ||
			archivedRowHash != decision.RowHash || registryVersion != decision.RegistryVersion || status != "passed" || reason != decision.ReasonCode {
			return outboxArchiveSummary{}, fmt.Errorf("outbox archive drift for source id=%d", row.ID)
		}
		sourceHashes = append(sourceHashes, decision.RowHash)
		if decision.Publication == "published" {
			summary.Published++
		} else {
			summary.Unpublished++
		}
		if decision.RequiresReconciliation {
			summary.ExternalWriteRows++
		}
	}
	var sourceCount, archiveCount uint64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM outbox_events").Scan(&sourceCount); err != nil {
		return outboxArchiveSummary{}, err
	}
	archiveRows, err := tx.QueryContext(ctx, `SELECT row_hash FROM legacy_outbox_archive ORDER BY source_outbox_id`)
	if err != nil {
		return outboxArchiveSummary{}, err
	}
	archiveHashes := make([]string, 0, sourceCount)
	for archiveRows.Next() {
		var hash sql.NullString
		if err := archiveRows.Scan(&hash); err != nil {
			_ = archiveRows.Close()
			return outboxArchiveSummary{}, err
		}
		if !hash.Valid || !isSHA256(hash.String) {
			_ = archiveRows.Close()
			return outboxArchiveSummary{}, errors.New("outbox archive contains a row without a canonical row hash")
		}
		archiveHashes = append(archiveHashes, hash.String)
	}
	if err := archiveRows.Err(); err != nil {
		_ = archiveRows.Close()
		return outboxArchiveSummary{}, err
	}
	if err := archiveRows.Close(); err != nil {
		return outboxArchiveSummary{}, err
	}
	archiveCount = uint64(len(archiveHashes))
	summary.SourceHash = canonicalHashSet(sourceHashes)
	summary.ArchiveHash = canonicalHashSet(archiveHashes)
	if sourceCount != archiveCount || summary.SourceHash != summary.ArchiveHash {
		return outboxArchiveSummary{}, fmt.Errorf("outbox archive parity source=%d target=%d source_hash=%s target_hash=%s", sourceCount, archiveCount, summary.SourceHash, summary.ArchiveHash)
	}
	return summary, nil
}
