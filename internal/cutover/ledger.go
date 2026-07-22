package cutover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/schemaversion"
	"github.com/google/uuid"
)

const (
	BackfillOperationPrefix  = "BACKFILL-V3/"
	BackfillConverterVersion = "phase7a-backfill/v2"
)

var ErrLedgerAlreadyPassed = errors.New("migration ledger batch already passed")

type ReleaseIdentity struct {
	PlanVersion       uint64
	SourceExactSHA    string
	BinaryImageDigest string
	SourceSchema      uint64
	TargetSchema      uint64
}

func (r ReleaseIdentity) Validate() error {
	if r.PlanVersion == 0 || !isExactSHA(r.SourceExactSHA) || !imageDigestPattern.MatchString(r.BinaryImageDigest) {
		return errors.New("release identity requires plan version, exact source SHA, and exact image digest")
	}
	expected := uint64(schemaversion.Latest)
	if r.SourceSchema == 0 || r.TargetSchema == 0 || r.TargetSchema != expected || r.SourceSchema > r.TargetSchema {
		return fmt.Errorf("release schema source=%d target=%d latest=%d", r.SourceSchema, r.TargetSchema, expected)
	}
	return nil
}

type LedgerBatch struct {
	ID               uint64
	PublicID         string
	Operation        string
	Stage            string
	Attempt          uint64
	PreviousID       *uint64
	SourceSchema     uint64
	TargetSchema     uint64
	SourceTable      string
	TargetTable      string
	BatchNo          uint64
	IDMin            *uint64
	IDMax            *uint64
	SourceCount      uint64
	TargetCount      uint64
	SkippedCount     uint64
	RejectedCount    uint64
	SourceHash       string
	TargetHash       string
	ConverterVersion string
	ReleaseHash      string
	SourceExactSHA   string
	ImageDigest      string
	Status           string
	ReasonCode       string
	Summary          string
	StartedAt        time.Time
	CompletedAt      *time.Time
}

type LedgerCompletion struct {
	SourceCount             uint64
	TargetCount             uint64
	SkippedCount            uint64
	RejectedCount           uint64
	SourceHash              string
	TargetHash              string
	ReasonCode              string
	Summary                 string
	RequireParity           bool
	ConversionFailures      uint64
	UnknownExternalWrites   uint64
	ActiveLegacyLeases      uint64
	ObservedIngressWriters  uint64
	ObservedMutationWriters uint64
	ObservedLegacyWorkers   uint64
	MissingArchiveRows      uint64
	DuplicateTasks          uint64
}

func (c LedgerCompletion) passing() (bool, string) {
	if !isSHA256(c.SourceHash) || !isSHA256(c.TargetHash) {
		return false, "ledger_hash_missing"
	}
	if c.RequireParity && (c.SourceCount != c.TargetCount || c.SourceHash != c.TargetHash) {
		return false, "count_or_hash_mismatch"
	}
	if c.RejectedCount != 0 || c.ConversionFailures != 0 || c.UnknownExternalWrites != 0 ||
		c.ActiveLegacyLeases != 0 || c.ObservedIngressWriters != 0 || c.ObservedMutationWriters != 0 ||
		c.ObservedLegacyWorkers != 0 || c.MissingArchiveRows != 0 || c.DuplicateTasks != 0 {
		return false, "cutover_invariant_failed"
	}
	return true, ""
}

func beginLedgerBatch(ctx context.Context, tx *sql.Tx, identity ReleaseIdentity, stage, operation, sourceTable, targetTable string, batchNo uint64, idMin, idMax *uint64, converter string, startedAt time.Time) (returnBatch LedgerBatch, retErr error) {
	if tx == nil || strings.TrimSpace(stage) == "" || strings.TrimSpace(operation) == "" || strings.TrimSpace(sourceTable) == "" || strings.TrimSpace(targetTable) == "" || batchNo == 0 || strings.TrimSpace(converter) == "" {
		return LedgerBatch{}, errors.New("ledger batch identity is incomplete")
	}
	if err := identity.Validate(); err != nil {
		return LedgerBatch{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,public_id,attempt,previous_ledger_id,stage,
source_schema_version,target_schema_version,source_table,target_table,batch_no,id_min,id_max,
status,source_count,target_count,skipped_count,rejected_count,source_hash,target_hash,reason_code,bounded_summary,
converter_version,release_identity_hash,source_exact_sha,binary_image_digest,started_at,completed_at
FROM migration_ledger WHERE plan_version=? AND operation=? AND batch_no=? ORDER BY attempt FOR UPDATE`, identity.PlanVersion, operation, batchNo)
	if err != nil {
		return LedgerBatch{}, err
	}
	defer joinRowsCloseError(&retErr, rows, "close migration ledger batch rows")
	var previous *LedgerBatch
	for rows.Next() {
		var item LedgerBatch
		var previousID, idMinValue, idMaxValue sql.NullInt64
		var sourceHash, targetHash, reason, summary, releaseHash sql.NullString
		var completed sql.NullTime
		if err := rows.Scan(&item.ID, &item.PublicID, &item.Attempt, &previousID, &item.Stage,
			&item.SourceSchema, &item.TargetSchema, &item.SourceTable, &item.TargetTable, &item.BatchNo,
			&idMinValue, &idMaxValue, &item.Status, &item.SourceCount, &item.TargetCount,
			&item.SkippedCount, &item.RejectedCount, &sourceHash, &targetHash, &reason, &summary,
			&item.ConverterVersion, &releaseHash, &item.SourceExactSHA, &item.ImageDigest,
			&item.StartedAt, &completed); err != nil {
			return LedgerBatch{}, err
		}
		item.PreviousID, _ = optionalUint64(previousID)
		item.IDMin, _ = optionalUint64(idMinValue)
		item.IDMax, _ = optionalUint64(idMaxValue)
		item.Operation = operation
		item.SourceHash, item.TargetHash, item.ReasonCode, item.Summary = sourceHash.String, targetHash.String, reason.String, summary.String
		item.ReleaseHash = releaseHash.String
		if completed.Valid {
			value := completed.Time.UTC()
			item.CompletedAt = &value
		}
		copyItem := item
		previous = &copyItem
	}
	if err := rows.Err(); err != nil {
		return LedgerBatch{}, err
	}
	if previous != nil && previous.Status == "passed" {
		return *previous, ErrLedgerAlreadyPassed
	}
	if previous != nil && previous.Status == "running" {
		return LedgerBatch{}, errors.New("migration ledger batch has an unfinished running attempt")
	}
	attempt := uint64(1)
	var previousID any
	var previousPointer *uint64
	if previous != nil {
		attempt = previous.Attempt + 1
		previousID = previous.ID
		value := previous.ID
		previousPointer = &value
	}
	publicID := uuid.NewString()
	result, err := tx.ExecContext(ctx, `INSERT INTO migration_ledger (
public_id,plan_version,stage,operation,attempt,previous_ledger_id,source_schema_version,target_schema_version,
source_table,target_table,batch_no,id_min,id_max,source_count,target_count,skipped_count,rejected_count,
source_hash,target_hash,canonical_hash_version,release_identity_hash,converter_version,started_at,completed_at,status,
reason_code,bounded_summary,source_exact_sha,binary_image_digest)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,0,NULL,NULL,1,?,?,?,NULL,'running',NULL,NULL,?,?)`, publicID,
		identity.PlanVersion, stage, operation, attempt, previousID, identity.SourceSchema, identity.TargetSchema,
		sourceTable, targetTable, batchNo, nullableUint64(idMin), nullableUint64(idMax),
		releaseIdentityHash(identity.SourceExactSHA, identity.BinaryImageDigest, identity.SourceSchema, identity.TargetSchema),
		converter, startedAt.UTC(), identity.SourceExactSHA, identity.BinaryImageDigest)
	if err != nil {
		return LedgerBatch{}, err
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return LedgerBatch{}, fmt.Errorf("read migration ledger id: %w", err)
	}
	return LedgerBatch{ID: uint64(id), PublicID: publicID, Operation: operation, Stage: stage, Attempt: attempt,
		PreviousID: previousPointer, SourceSchema: identity.SourceSchema, TargetSchema: identity.TargetSchema,
		SourceTable: sourceTable, TargetTable: targetTable, BatchNo: batchNo,
		IDMin: idMin, IDMax: idMax, ConverterVersion: converter,
		ReleaseHash:    releaseIdentityHash(identity.SourceExactSHA, identity.BinaryImageDigest, identity.SourceSchema, identity.TargetSchema),
		SourceExactSHA: identity.SourceExactSHA, ImageDigest: identity.BinaryImageDigest,
		Status: "running", StartedAt: startedAt.UTC()}, nil
}

func beginLedgerRetryAfterPassed(ctx context.Context, tx *sql.Tx, identity ReleaseIdentity, previous LedgerBatch,
	stage, operation, sourceTable, targetTable string, batchNo uint64, idMin, idMax *uint64,
	converter string, startedAt time.Time) (LedgerBatch, error) {
	if tx == nil || previous.ID == 0 || previous.Status != "passed" || previous.Attempt == 0 {
		return LedgerBatch{}, errors.New("passed ledger retry identity is invalid")
	}
	if previous.SourceExactSHA != identity.SourceExactSHA || previous.ImageDigest != identity.BinaryImageDigest ||
		previous.SourceSchema != identity.SourceSchema || previous.TargetSchema != identity.TargetSchema ||
		previous.Operation != operation || previous.BatchNo != batchNo {
		return LedgerBatch{}, errors.New("passed ledger retry release identity differs")
	}
	attempt := previous.Attempt + 1
	publicID := uuid.NewString()
	result, err := tx.ExecContext(ctx, `INSERT INTO migration_ledger (
public_id,plan_version,stage,operation,attempt,previous_ledger_id,source_schema_version,target_schema_version,
source_table,target_table,batch_no,id_min,id_max,source_count,target_count,skipped_count,rejected_count,
source_hash,target_hash,canonical_hash_version,release_identity_hash,converter_version,started_at,completed_at,status,
reason_code,bounded_summary,source_exact_sha,binary_image_digest)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,0,0,NULL,NULL,1,?,?,?,NULL,'running',NULL,NULL,?,?)`, publicID,
		identity.PlanVersion, stage, operation, attempt, previous.ID, identity.SourceSchema, identity.TargetSchema,
		sourceTable, targetTable, batchNo, nullableUint64(idMin), nullableUint64(idMax),
		releaseIdentityHash(identity.SourceExactSHA, identity.BinaryImageDigest, identity.SourceSchema, identity.TargetSchema),
		converter, startedAt.UTC(), identity.SourceExactSHA, identity.BinaryImageDigest)
	if err != nil {
		return LedgerBatch{}, err
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return LedgerBatch{}, fmt.Errorf("read passed ledger retry id: %w", err)
	}
	previousID := previous.ID
	return LedgerBatch{ID: uint64(id), PublicID: publicID, Operation: operation, Stage: stage, Attempt: attempt,
		PreviousID: &previousID, SourceSchema: identity.SourceSchema, TargetSchema: identity.TargetSchema,
		SourceTable: sourceTable, TargetTable: targetTable, BatchNo: batchNo, IDMin: idMin, IDMax: idMax,
		ConverterVersion: converter,
		ReleaseHash:      releaseIdentityHash(identity.SourceExactSHA, identity.BinaryImageDigest, identity.SourceSchema, identity.TargetSchema),
		SourceExactSHA:   identity.SourceExactSHA, ImageDigest: identity.BinaryImageDigest,
		Status: "running", StartedAt: startedAt.UTC()}, nil
}

func finishLedgerBatch(ctx context.Context, tx *sql.Tx, batch LedgerBatch, completion LedgerCompletion, completedAt time.Time) (LedgerBatch, error) {
	if tx == nil || batch.ID == 0 || batch.Status != "running" || completedAt.Before(batch.StartedAt) {
		return LedgerBatch{}, errors.New("ledger completion identity is invalid")
	}
	passed, failureReason := completion.passing()
	status := "passed"
	reason := strings.TrimSpace(completion.ReasonCode)
	if !passed {
		status = "failed"
		if reason == "" {
			reason = failureReason
		}
	} else if reason != "" {
		return LedgerBatch{}, errors.New("passed ledger completion cannot carry a reason code")
	}
	summary := strings.TrimSpace(completion.Summary)
	if summary == "" || len(summary) > 2048 {
		return LedgerBatch{}, errors.New("ledger completion requires a bounded summary")
	}
	result, err := tx.ExecContext(ctx, `UPDATE migration_ledger SET source_count=?,target_count=?,skipped_count=?,rejected_count=?,
source_hash=?,target_hash=?,completed_at=?,status=?,reason_code=?,bounded_summary=? WHERE id=? AND status='running' AND completed_at IS NULL`,
		completion.SourceCount, completion.TargetCount, completion.SkippedCount, completion.RejectedCount,
		completion.SourceHash, completion.TargetHash, completedAt.UTC(), status, nullableString(reason), summary, batch.ID)
	if err != nil {
		return LedgerBatch{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return LedgerBatch{}, errors.New("migration ledger terminal row was already changed")
	}
	batch.SourceCount, batch.TargetCount, batch.SkippedCount, batch.RejectedCount = completion.SourceCount, completion.TargetCount, completion.SkippedCount, completion.RejectedCount
	batch.SourceHash, batch.TargetHash, batch.Status, batch.ReasonCode, batch.Summary = completion.SourceHash, completion.TargetHash, status, reason, summary
	finished := completedAt.UTC()
	batch.CompletedAt = &finished
	if !passed {
		return batch, fmt.Errorf("migration ledger %s batch=%d failed: %s", batch.Operation, batch.BatchNo, reason)
	}
	return batch, nil
}

func nullableUint64(value *uint64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
