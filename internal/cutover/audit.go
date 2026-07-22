package cutover

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

type AuditReleaseIdentity struct {
	PlanVersion       uint64 `json:"plan_version"`
	SourceExactSHA    string `json:"source_exact_sha"`
	BinaryImageDigest string `json:"binary_image_digest"`
	IdentityHash      string `json:"identity_hash"`
}

type AuditSchemaVersions struct {
	Database uint64 `json:"database"`
	Source   uint64 `json:"source"`
	Target   uint64 `json:"target"`
}

type AuditLedgerUnit struct {
	PublicID             string     `json:"public_id"`
	Stage                string     `json:"stage"`
	Operation            string     `json:"operation"`
	Attempt              uint64     `json:"attempt"`
	PreviousLedgerID     *uint64    `json:"previous_ledger_id,omitempty"`
	SourceSchema         uint64     `json:"source_schema_version"`
	TargetSchema         uint64     `json:"target_schema_version"`
	SourceTable          string     `json:"source_table"`
	TargetTable          string     `json:"target_table"`
	BatchNo              uint64     `json:"batch_no"`
	IDMin                *uint64    `json:"id_min,omitempty"`
	IDMax                *uint64    `json:"id_max,omitempty"`
	SourceCount          uint64     `json:"source_count"`
	TargetCount          uint64     `json:"target_count"`
	SkippedCount         uint64     `json:"skipped_count"`
	RejectedCount        uint64     `json:"rejected_count"`
	SourceHash           string     `json:"source_hash,omitempty"`
	TargetHash           string     `json:"target_hash,omitempty"`
	CanonicalHashVersion uint64     `json:"canonical_hash_version"`
	ReleaseIdentityHash  string     `json:"release_identity_hash,omitempty"`
	ConverterVersion     string     `json:"converter_version"`
	Status               string     `json:"status"`
	ReasonCode           string     `json:"reason_code,omitempty"`
	BoundedSummary       string     `json:"bounded_summary,omitempty"`
	StartedAt            time.Time  `json:"started_at"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
}

type AuditOutboxCount struct {
	EventType        string `json:"event_type"`
	SchemaVersion    uint64 `json:"schema_version"`
	PublicationState string `json:"publication_state"`
	Count            uint64 `json:"count"`
}

type AuditConversionCount struct {
	SubjectType    string `json:"subject_type"`
	Status         string `json:"status"`
	ReasonCode     string `json:"reason_code"`
	AntiJoinResult string `json:"anti_join_result"`
	Count          uint64 `json:"count"`
}

type AuditMarkerState struct {
	Count             uint64 `json:"count"`
	Status            string `json:"status,omitempty"`
	SourceExactSHA    string `json:"source_exact_sha,omitempty"`
	BinaryImageDigest string `json:"binary_image_digest,omitempty"`
}

type AuditExport struct {
	ReleaseIdentity AuditReleaseIdentity   `json:"release_identity"`
	SchemaVersions  AuditSchemaVersions    `json:"schema_versions"`
	Control         map[string]any         `json:"control"`
	Ledger          []AuditLedgerUnit      `json:"ledger"`
	Outbox          []AuditOutboxCount     `json:"outbox_event_counts"`
	Conversions     []AuditConversionCount `json:"conversion_counts"`
	Counts          map[string]uint64      `json:"counts"`
	Marker          AuditMarkerState       `json:"marker"`
	ExportedAt      time.Time              `json:"exported_at"`
}

// ExportAudit emits only bounded release metadata, counts, hashes, statuses,
// ID ranges, and reason codes. It never emits outbox payloads, checkpoints,
// Evidence facts, narrative fields, provider responses, or credentials.
func ExportAudit(ctx context.Context, db *sql.DB, output io.Writer) (retErr error) {
	if db == nil || output == nil {
		return errors.New("cutover audit export requires database and output")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			retErr = errors.Join(retErr, fmt.Errorf("rollback cutover audit transaction: %w", rollbackErr))
		}
	}()
	var controlName, sourceSHA, digest string
	var plan uint64
	var ingress, mutations, workers, unknown uint64
	var prepared, completed sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT control_name,plan_version,source_exact_sha,binary_image_digest,
observed_ingress_writers,observed_mutation_writers,observed_legacy_workers,observed_unknown_external_writes,
prepared_at,completed_at FROM cutover_controls WHERE control_name=?`, phase7AControlName).Scan(
		&controlName, &plan, &sourceSHA, &digest, &ingress, &mutations, &workers, &unknown,
		&prepared, &completed); err != nil {
		return fmt.Errorf("read cutover control: %w", err)
	}
	var databaseVersion uint64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_id),0) FROM goose_db_version WHERE is_applied=1").Scan(&databaseVersion); err != nil {
		return err
	}
	request := PrepareRequest{PlanVersion: plan, SourceExactSHA: sourceSHA, BinaryImageDigest: digest,
		ObservedIngressWriters: ingress, ObservedMutationWriters: mutations, ObservedLegacyWorkers: workers,
		ObservedUnknownExternalWrite: unknown}
	counts, err := collectCutoverCountsV2(ctx, tx, request)
	if err != nil {
		return err
	}
	ledger, sourceSchema, targetSchema, err := exportLedger(ctx, tx, plan)
	if err != nil {
		return err
	}
	outbox, err := exportOutboxCounts(ctx, tx)
	if err != nil {
		return err
	}
	conversions, err := exportConversionCounts(ctx, tx)
	if err != nil {
		return err
	}
	marker, err := exportMarkerState(ctx, tx)
	if err != nil {
		return err
	}
	var exportedAt time.Time
	if err := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&exportedAt); err != nil {
		return err
	}
	control := map[string]any{
		"control_name": controlName, "ingress_writers": ingress, "mutation_writers": mutations,
		"legacy_workers": workers, "unknown_external_writes": unknown,
		"prepared_at": nullableAuditTime(prepared), "completed_at": nullableAuditTime(completed),
	}
	export := AuditExport{
		ReleaseIdentity: AuditReleaseIdentity{PlanVersion: plan, SourceExactSHA: sourceSHA,
			BinaryImageDigest: digest, IdentityHash: releaseIdentityHash(sourceSHA, digest, sourceSchema, targetSchema)},
		SchemaVersions: AuditSchemaVersions{Database: databaseVersion, Source: sourceSchema, Target: targetSchema},
		Control:        control, Ledger: ledger, Outbox: outbox, Conversions: conversions, Counts: counts,
		Marker: marker, ExportedAt: exportedAt.UTC(),
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(export)
}

func exportLedger(ctx context.Context, tx *sql.Tx, plan uint64) (result []AuditLedgerUnit, sourceSchema, targetSchema uint64, retErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT public_id,stage,operation,attempt,previous_ledger_id,
source_schema_version,target_schema_version,source_table,target_table,batch_no,id_min,id_max,
source_count,target_count,skipped_count,rejected_count,source_hash,target_hash,canonical_hash_version,
release_identity_hash,converter_version,status,reason_code,bounded_summary,started_at,completed_at
FROM migration_ledger WHERE plan_version=? ORDER BY id`, plan)
	if err != nil {
		return nil, 0, 0, err
	}
	defer joinRowsCloseError(&retErr, rows, "close cutover audit ledger rows")
	result = make([]AuditLedgerUnit, 0)
	for rows.Next() {
		var item AuditLedgerUnit
		var previous, idMin, idMax sql.NullInt64
		var sourceHash, targetHash, releaseHash, reason, summary sql.NullString
		var completed sql.NullTime
		if err := rows.Scan(&item.PublicID, &item.Stage, &item.Operation, &item.Attempt, &previous,
			&item.SourceSchema, &item.TargetSchema, &item.SourceTable, &item.TargetTable, &item.BatchNo,
			&idMin, &idMax, &item.SourceCount, &item.TargetCount, &item.SkippedCount, &item.RejectedCount,
			&sourceHash, &targetHash, &item.CanonicalHashVersion, &releaseHash, &item.ConverterVersion,
			&item.Status, &reason, &summary, &item.StartedAt, &completed); err != nil {
			return nil, 0, 0, err
		}
		item.PreviousLedgerID = auditOptionalUint(previous)
		item.IDMin, item.IDMax = auditOptionalUint(idMin), auditOptionalUint(idMax)
		item.SourceHash, item.TargetHash, item.ReleaseIdentityHash = sourceHash.String, targetHash.String, releaseHash.String
		item.ReasonCode, item.BoundedSummary = reason.String, summary.String
		if completed.Valid {
			value := completed.Time.UTC()
			item.CompletedAt = &value
		}
		item.StartedAt = item.StartedAt.UTC()
		result = append(result, item)
		if item.Operation == QuiesceOperation || item.Operation == ReconciliationOperation || item.Operation == ConverterAuditOperation {
			sourceSchema, targetSchema = item.SourceSchema, item.TargetSchema
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	return result, sourceSchema, targetSchema, nil
}

func exportOutboxCounts(ctx context.Context, tx *sql.Tx) (result []AuditOutboxCount, retErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT event_type,schema_version,publication_state,COUNT(*)
FROM legacy_outbox_archive GROUP BY event_type,schema_version,publication_state
ORDER BY event_type,schema_version,publication_state`)
	if err != nil {
		return nil, err
	}
	defer joinRowsCloseError(&retErr, rows, "close cutover audit outbox rows")
	result = make([]AuditOutboxCount, 0)
	for rows.Next() {
		var item AuditOutboxCount
		if err := rows.Scan(&item.EventType, &item.SchemaVersion, &item.PublicationState, &item.Count); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func exportConversionCounts(ctx context.Context, tx *sql.Tx) (result []AuditConversionCount, retErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT r.subject_type,r.status,r.reason_code,r.anti_join_result,COUNT(*)`+
		latestConversionBaseSQL+` GROUP BY r.subject_type,r.status,r.reason_code,r.anti_join_result
ORDER BY r.subject_type,r.status,r.reason_code,r.anti_join_result`)
	if err != nil {
		return nil, err
	}
	defer joinRowsCloseError(&retErr, rows, "close cutover audit conversion rows")
	result = make([]AuditConversionCount, 0)
	for rows.Next() {
		var item AuditConversionCount
		if err := rows.Scan(&item.SubjectType, &item.Status, &item.ReasonCode, &item.AntiJoinResult, &item.Count); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func exportMarkerState(ctx context.Context, tx *sql.Tx) (AuditMarkerState, error) {
	var result AuditMarkerState
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM migration_ledger WHERE operation=?", MarkerOperation).Scan(&result.Count); err != nil {
		return result, err
	}
	if result.Count == 0 {
		return result, nil
	}
	if result.Count != 1 {
		return result, fmt.Errorf("CUTOVER-V3 marker rows=%d", result.Count)
	}
	if err := tx.QueryRowContext(ctx, `SELECT status,source_exact_sha,binary_image_digest
FROM migration_ledger WHERE operation=?`, MarkerOperation).Scan(&result.Status, &result.SourceExactSHA, &result.BinaryImageDigest); err != nil {
		return result, err
	}
	return result, nil
}

func nullableAuditTime(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time.UTC()
}

func auditOptionalUint(value sql.NullInt64) *uint64 {
	if !value.Valid || value.Int64 < 0 {
		return nil
	}
	result := uint64(value.Int64)
	return &result
}
