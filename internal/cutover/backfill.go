package cutover

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/google/uuid"
)

const (
	defaultBackfillBatchSize = 100
	maxBackfillBatchSize     = 1000
	backfillLockName         = "cloudops-copilot:phase7a-backfill"
)

type BackfillFaultInjector func(operation string, batchNo uint64, point string) error

type BackfillRequest struct {
	Identity  ReleaseIdentity
	BatchSize uint64
}

func (r BackfillRequest) Validate() error {
	if err := r.Identity.Validate(); err != nil {
		return err
	}
	if r.BatchSize == 0 {
		r.BatchSize = defaultBackfillBatchSize
	}
	if r.BatchSize > maxBackfillBatchSize {
		return fmt.Errorf("backfill batch size=%d exceeds %d", r.BatchSize, maxBackfillBatchSize)
	}
	return nil
}

type BackfillReport struct {
	PlanVersion       uint64            `json:"plan_version"`
	SourceExactSHA    string            `json:"source_exact_sha"`
	BinaryImageDigest string            `json:"binary_image_digest"`
	BatchSize         uint64            `json:"batch_size"`
	Batches           []LedgerBatch     `json:"batches"`
	Counts            map[string]uint64 `json:"counts"`
	CompletedAt       time.Time         `json:"completed_at"`
}

type Phase7ABackfiller struct {
	db          *sql.DB
	lockTimeout time.Duration
	now         func() time.Time
	fault       BackfillFaultInjector
}

func NewPhase7ABackfiller(db *sql.DB, lockTimeout time.Duration) (*Phase7ABackfiller, error) {
	if db == nil || lockTimeout <= 0 {
		return nil, errors.New("phase7a backfill requires a database and positive lock timeout")
	}
	return &Phase7ABackfiller{db: db, lockTimeout: lockTimeout, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (b *Phase7ABackfiller) WithFaultInjector(injector BackfillFaultInjector) *Phase7ABackfiller {
	if b != nil {
		b.fault = injector
	}
	return b
}

type backfillRow struct {
	ID         uint64
	IncidentID *uint64
	CreatedAt  time.Time
	Snapshot   json.RawMessage
	SourceHash string
	TargetHash string
}

type backfillUnit struct {
	name        string
	sourceTable string
	targetTable string
	load        func(context.Context, *sql.Tx, uint64, uint64) ([]backfillRow, error)
	apply       func(context.Context, *sql.Tx, *backfillRow, time.Time) error
}

func (b *Phase7ABackfiller) Run(ctx context.Context, request BackfillRequest) (report BackfillReport, retErr error) {
	if b == nil || b.db == nil {
		return report, errors.New("phase7a backfiller is not initialized")
	}
	if request.BatchSize == 0 {
		request.BatchSize = defaultBackfillBatchSize
	}
	if err := request.Validate(); err != nil {
		return report, err
	}
	conn, err := b.db.Conn(ctx)
	if err != nil {
		return report, fmt.Errorf("reserve phase7a backfill connection: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, conn.Close()) }()
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", backfillLockName, int64(math.Ceil(b.lockTimeout.Seconds()))).Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		return report, fmt.Errorf("acquire phase7a backfill lock: %w", err)
	}
	defer func() {
		var released sql.NullInt64
		if err := conn.QueryRowContext(context.WithoutCancel(ctx), "SELECT RELEASE_LOCK(?)", backfillLockName).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			retErr = errors.Join(retErr, errors.New("release phase7a backfill lock failed"))
		}
	}()

	units := b.units()
	report = BackfillReport{PlanVersion: request.Identity.PlanVersion, SourceExactSHA: request.Identity.SourceExactSHA,
		BinaryImageDigest: request.Identity.BinaryImageDigest, BatchSize: request.BatchSize,
		Batches: []LedgerBatch{}, Counts: map[string]uint64{}}
	for _, unit := range units {
		batches, count, err := b.runUnit(ctx, conn, request, unit)
		report.Batches = append(report.Batches, batches...)
		report.Counts[unit.name] = count
		if err != nil {
			return report, err
		}
	}
	report.CompletedAt = b.now().UTC()
	return report, nil
}

func (b *Phase7ABackfiller) runUnit(ctx context.Context, conn *sql.Conn, request BackfillRequest, unit backfillUnit) ([]LedgerBatch, uint64, error) {
	operation := BackfillOperationPrefix + unit.name
	cursor, passed, err := loadBackfillCursor(ctx, conn, request.Identity, operation)
	if err != nil {
		return nil, 0, err
	}
	if passed {
		probe, probeErr := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
		if probeErr != nil {
			return nil, 0, probeErr
		}
		remaining, loadErr := unit.load(ctx, probe, cursor, 1)
		rollbackErr := probe.Rollback()
		if loadErr != nil || (rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone)) {
			return nil, 0, errors.Join(loadErr, rollbackErr)
		}
		if len(remaining) == 0 {
			count, countErr := passedBackfillCount(ctx, conn, request.Identity.PlanVersion, operation)
			return nil, count, countErr
		}
	}
	lastID := cursor
	batchNo, err := nextBackfillBatchNo(ctx, conn, request.Identity.PlanVersion, operation)
	if err != nil {
		return nil, cursor, err
	}
	result := []LedgerBatch{}
	total := uint64(0)
	for {
		tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return result, total, err
		}
		rows, err := unit.load(ctx, tx, lastID, request.BatchSize)
		if err != nil {
			_ = tx.Rollback()
			return result, total, err
		}
		if len(rows) == 0 {
			started := b.now().UTC()
			ledger, ledgerErr := beginLedgerBatch(ctx, tx, request.Identity, "backfill", operation, unit.sourceTable, unit.targetTable, batchNo, nil, nil, BackfillConverterVersion, started)
			if ledgerErr == nil {
				emptyHash := canonicalHashSet(nil)
				ledger, ledgerErr = finishLedgerBatch(ctx, tx, ledger, LedgerCompletion{SourceHash: emptyHash, TargetHash: emptyHash, RequireParity: true,
					Summary: fmt.Sprintf("%s contains no remaining legacy rows", unit.name)}, b.now())
				if ledgerErr == nil {
					result = append(result, ledger)
				}
			} else if errors.Is(ledgerErr, ErrLedgerAlreadyPassed) {
				ledgerErr = validatePassedEmptyBackfillBatch(ledger, request.Identity, unit)
			}
			if ledgerErr != nil {
				_ = tx.Rollback()
				return result, total, ledgerErr
			}
			if err := markBackfillCursor(ctx, tx, request.Identity, operation, unit, batchNo, lastID, "passed", b.now()); err != nil {
				_ = tx.Rollback()
				return result, total, err
			}
			if err := tx.Commit(); err != nil {
				return result, total, err
			}
			return result, total, nil
		}
		idMin, idMax := rows[0].ID, rows[len(rows)-1].ID
		sourceHashes := make([]string, 0, len(rows))
		for index := range rows {
			sourceHashes = append(sourceHashes, rows[index].SourceHash)
		}
		started := b.now().UTC()
		ledger, err := beginLedgerBatch(ctx, tx, request.Identity, "backfill", operation, unit.sourceTable, unit.targetTable, batchNo, &idMin, &idMax, BackfillConverterVersion, started)
		if errors.Is(err, ErrLedgerAlreadyPassed) {
			if validateErr := validatePassedBackfillBatch(ledger, request.Identity, unit, idMin, idMax, sourceHashes); validateErr != nil {
				_ = tx.Rollback()
				return result, total, validateErr
			}
			_ = tx.Rollback()
			lastID = idMax
			batchNo++
			continue
		}
		if err != nil {
			_ = tx.Rollback()
			return result, total, err
		}
		targetHashes := make([]string, 0, len(rows))
		var applyErr error
		for index := range rows {
			if err := unit.apply(ctx, tx, &rows[index], started); err != nil {
				applyErr = err
				break
			}
			targetHashes = append(targetHashes, rows[index].TargetHash)
		}
		if applyErr == nil && b.fault != nil {
			applyErr = b.fault(operation, batchNo, "after-target-write")
		}
		if applyErr != nil {
			_ = tx.Rollback()
			failed, recordErr := b.recordFailedBatch(ctx, conn, request.Identity, unit, operation, batchNo, idMin, idMax, rows, sourceHashes, applyErr)
			if failed.ID != 0 {
				result = append(result, failed)
			}
			return result, total, errors.Join(applyErr, recordErr)
		}
		completion := LedgerCompletion{SourceCount: uint64(len(rows)), TargetCount: uint64(len(rows)), SourceHash: canonicalHashSet(sourceHashes), TargetHash: canonicalHashSet(targetHashes), RequireParity: true,
			Summary: fmt.Sprintf("%s batch %d preserved %d canonical rows", unit.name, batchNo, len(rows))}
		ledger, err = finishLedgerBatch(ctx, tx, ledger, completion, b.now())
		if err != nil {
			_ = tx.Rollback()
			failed, recordErr := b.recordFailedBatch(ctx, conn, request.Identity, unit, operation, batchNo, idMin, idMax, rows, sourceHashes, err)
			if failed.ID != 0 {
				result = append(result, failed)
			}
			return result, total, errors.Join(err, recordErr)
		}
		if err := markBackfillCursor(ctx, tx, request.Identity, operation, unit, batchNo+1, idMax, "running", b.now()); err != nil {
			_ = tx.Rollback()
			return result, total, err
		}
		if err := tx.Commit(); err != nil {
			return result, total, err
		}
		result = append(result, ledger)
		total += uint64(len(rows))
		lastID, batchNo = idMax, batchNo+1
	}
}

func validatePassedEmptyBackfillBatch(batch LedgerBatch, identity ReleaseIdentity, unit backfillUnit) error {
	emptyHash := canonicalHashSet(nil)
	if batch.Status != "passed" || batch.SourceSchema != identity.SourceSchema || batch.TargetSchema != identity.TargetSchema ||
		batch.SourceTable != unit.sourceTable || batch.TargetTable != unit.targetTable || batch.IDMin != nil || batch.IDMax != nil ||
		batch.SourceCount != 0 || batch.TargetCount != 0 || batch.SkippedCount != 0 || batch.RejectedCount != 0 ||
		batch.SourceHash != emptyHash || batch.TargetHash != emptyHash || batch.ConverterVersion != BackfillConverterVersion ||
		batch.SourceExactSHA != identity.SourceExactSHA || batch.ImageDigest != identity.BinaryImageDigest ||
		batch.ReleaseHash != releaseIdentityHash(identity.SourceExactSHA, identity.BinaryImageDigest, identity.SourceSchema, identity.TargetSchema) {
		return fmt.Errorf("passed empty BACKFILL-V3 ledger %s batch=%d differs from current source rows", batch.Operation, batch.BatchNo)
	}
	return nil
}

func validatePassedBackfillBatch(batch LedgerBatch, identity ReleaseIdentity, unit backfillUnit, idMin, idMax uint64, sourceHashes []string) error {
	expectedHash := canonicalHashSet(sourceHashes)
	if batch.Status != "passed" || batch.SourceSchema != identity.SourceSchema || batch.TargetSchema != identity.TargetSchema ||
		batch.SourceTable != unit.sourceTable || batch.TargetTable != unit.targetTable ||
		batch.IDMin == nil || batch.IDMax == nil || *batch.IDMin != idMin || *batch.IDMax != idMax ||
		batch.SourceCount != uint64(len(sourceHashes)) || batch.TargetCount != uint64(len(sourceHashes)) ||
		batch.SkippedCount != 0 || batch.RejectedCount != 0 || batch.SourceHash != expectedHash || batch.TargetHash != expectedHash ||
		batch.ConverterVersion != BackfillConverterVersion || batch.SourceExactSHA != identity.SourceExactSHA ||
		batch.ImageDigest != identity.BinaryImageDigest ||
		batch.ReleaseHash != releaseIdentityHash(identity.SourceExactSHA, identity.BinaryImageDigest, identity.SourceSchema, identity.TargetSchema) {
		return fmt.Errorf("passed BACKFILL-V3 ledger %s batch=%d differs from current source rows", batch.Operation, batch.BatchNo)
	}
	return nil
}

func (b *Phase7ABackfiller) recordFailedBatch(ctx context.Context, conn *sql.Conn, identity ReleaseIdentity, unit backfillUnit, operation string, batchNo, idMin, idMax uint64, rows []backfillRow, sourceHashes []string, cause error) (result LedgerBatch, retErr error) {
	tx, err := conn.BeginTx(context.WithoutCancel(ctx), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return LedgerBatch{}, err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			retErr = errors.Join(retErr, fmt.Errorf("rollback failed backfill ledger transaction: %w", rollbackErr))
		}
	}()
	started := b.now().UTC()
	batch, err := beginLedgerBatch(context.WithoutCancel(ctx), tx, identity, "backfill", operation, unit.sourceTable, unit.targetTable, batchNo, &idMin, &idMax, BackfillConverterVersion, started)
	if err != nil {
		return LedgerBatch{}, err
	}
	targetHash := canonicalHashSet(nil)
	completion := LedgerCompletion{SourceCount: uint64(len(rows)), TargetCount: 0, RejectedCount: 1,
		SourceHash: canonicalHashSet(sourceHashes), TargetHash: targetHash, RequireParity: true,
		ReasonCode: "batch_conversion_failed", Summary: boundSummary("batch conversion failed: " + cause.Error())}
	batch, finishErr := finishLedgerBatch(context.WithoutCancel(ctx), tx, batch, completion, b.now())
	if commitErr := tx.Commit(); commitErr != nil {
		return batch, errors.Join(finishErr, commitErr)
	}
	return batch, finishErr
}

func (b *Phase7ABackfiller) units() []backfillUnit {
	return []backfillUnit{
		{name: "incident-signals", sourceTable: "incident_signals", targetTable: "incident_signals+legacy_signal_archive", load: loadSignalRows, apply: applySignalRow},
		{name: "incident-events", sourceTable: "incident_events", targetTable: "incident_events+legacy_event_archive", load: loadEventRows, apply: applyEventRow},
		{name: "evidence", sourceTable: "evidence_items", targetTable: "evidence_items+legacy_evidence_archive", load: loadEvidenceRows, apply: applyEvidenceRow},
		{name: "agent-steps", sourceTable: "agent_steps", targetTable: "agent_steps+legacy_agent_step_archive", load: loadAgentStepRows, apply: applyAgentStepRow},
		{name: "change-candidates", sourceTable: "changes", targetTable: "changes+legacy_change_candidate_archive+legacy_change_assessment_archive", load: loadChangeRows, apply: applyChangeRow},
	}
}

func loadSignalRows(ctx context.Context, tx *sql.Tx, after, limit uint64) ([]backfillRow, error) {
	return loadBackfillRows(ctx, tx, `SELECT id,incident_id,created_at,JSON_OBJECT(
'id',id,'incident_id',incident_id,'source',source,'source_event_id',source_event_id,'fingerprint',fingerprint,
'status',status,'severity',severity,'cluster',cluster,'namespace',namespace,'service_name',service_name,
'environment',environment,'target_kind',target_kind,'target_name',target_name,'category',category,
'occurred_at',occurred_at,'received_at',received_at,'summary',summary,'labels_json',labels_json,
'annotations_json',annotations_json,'raw_payload',raw_payload,'created_at',created_at)
FROM incident_signals WHERE domain_schema_version IS NULL AND id>? ORDER BY id LIMIT ?`, after, limit)
}

func applySignalRow(ctx context.Context, tx *sql.Tx, row *backfillRow, at time.Time) error {
	if row.IncidentID == nil {
		return errors.New("legacy Signal has no Incident owner")
	}
	var source, eventID, fingerprint, status string
	var occurred time.Time
	if err := tx.QueryRowContext(ctx, `SELECT source,source_event_id,fingerprint,status,occurred_at FROM incident_signals WHERE id=? FOR UPDATE`, row.ID).Scan(&source, &eventID, &fingerprint, &status, &occurred); err != nil {
		return err
	}
	publicID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("phase7a-signal:%d", row.ID))).String()
	instanceKey := canonicalHashFields("legacy-alert-instance/v1", source, fingerprint, occurred.UTC().Format(time.RFC3339Nano))
	var endsAt any
	if strings.EqualFold(status, "resolved") {
		endsAt = occurred.UTC()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_signal_archive
(source_signal_id,incident_id,cycle_no,source_schema_version,target_schema_version,source_snapshot_json,source_hash,
target_public_id,target_hash,conversion_status,reason_code,source_created_at,archived_at)
VALUES (?,?,1,1,3,?,?,?,?, 'passed','signal_backfilled',?,?)
ON DUPLICATE KEY UPDATE source_signal_id=VALUES(source_signal_id)`, row.ID, *row.IncidentID, row.Snapshot, row.SourceHash, publicID, row.SourceHash, row.CreatedAt.UTC(), at.UTC()); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE incident_signals SET public_id=?,domain_schema_version=3,cycle_no=1,
canonical_schema_version=2,correlation_key_version=2,alert_instance_key=?,starts_at=occurred_at,ends_at=?,
migrated_legacy=TRUE,migrated_legacy_context=TRUE
WHERE id=? AND domain_schema_version IS NULL`, publicID, instanceKey, endsAt, row.ID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("legacy Signal changed during backfill")
	}
	if err := verifyV3BackfillIdentity(ctx, tx, "incident_signals", row.ID); err != nil {
		return err
	}
	row.TargetHash, err = verifyBackfillArchive(ctx, tx, "legacy_signal_archive", "source_signal_id", row.ID, row.SourceHash)
	if err != nil {
		return err
	}
	return nil
}

func loadEventRows(ctx context.Context, tx *sql.Tx, after, limit uint64) ([]backfillRow, error) {
	return loadBackfillRows(ctx, tx, `SELECT id,incident_id,created_at,JSON_OBJECT(
'id',id,'incident_id',incident_id,'event_type',event_type,'idempotency_key',idempotency_key,
'actor_type',actor_type,'actor_id',actor_id,'summary',summary,'metadata_json',metadata_json,
'occurred_at',occurred_at,'created_at',created_at)
FROM incident_events WHERE domain_schema_version IS NULL AND id>? ORDER BY id LIMIT ?`, after, limit)
}

func applyEventRow(ctx context.Context, tx *sql.Tx, row *backfillRow, at time.Time) error {
	if row.IncidentID == nil {
		return errors.New("legacy IncidentEvent has no Incident owner")
	}
	publicID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("phase7a-event:%d", row.ID))).String()
	if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_event_archive
(source_event_id,incident_id,cycle_no,source_schema_version,target_schema_version,source_snapshot_json,source_hash,
target_event_public_id,target_hash,conversion_status,reason_code,source_created_at,archived_at)
VALUES (?,?,1,1,3,?,?,?,?, 'passed','event_backfilled',?,?)
ON DUPLICATE KEY UPDATE source_event_id=VALUES(source_event_id)`, row.ID, *row.IncidentID, row.Snapshot, row.SourceHash, publicID, row.SourceHash, row.CreatedAt.UTC(), at.UTC()); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE incident_events SET public_id=?,domain_schema_version=3,cycle_no=1,event_schema_version=1,
migrated_legacy=TRUE,migrated_legacy_context=TRUE
WHERE id=? AND domain_schema_version IS NULL`, publicID, row.ID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("legacy IncidentEvent changed during backfill")
	}
	if err := verifyV3BackfillIdentity(ctx, tx, "incident_events", row.ID); err != nil {
		return err
	}
	row.TargetHash, err = verifyBackfillArchive(ctx, tx, "legacy_event_archive", "source_event_id", row.ID, row.SourceHash)
	if err != nil {
		return err
	}
	return nil
}

func loadEvidenceRows(ctx context.Context, tx *sql.Tx, after, limit uint64) ([]backfillRow, error) {
	return loadBackfillRows(ctx, tx, `SELECT id,incident_id,created_at,JSON_OBJECT(
'id',id,'public_id',public_id,'incident_id',incident_id,'agent_run_id',agent_run_id,'change_id',change_id,
'type',type,'source',source,'tool_name',tool_name,'resource_ref',resource_ref,'time_range_json',time_range_json,
'query_text',query_text,'summary',summary,'facts_json',facts_json,'result_hash',result_hash,'raw_ref',raw_ref,
'redaction_json',redaction_json,'truncated',truncated,'valid',valid,'idempotency_key',idempotency_key,
'collected_at',collected_at,'created_at',created_at)
FROM evidence_items WHERE domain_schema_version IS NULL AND id>? ORDER BY id LIMIT ?`, after, limit)
}

func applyEvidenceRow(ctx context.Context, tx *sql.Tx, row *backfillRow, at time.Time) error {
	if row.IncidentID == nil {
		return errors.New("legacy Evidence has no Incident owner")
	}
	var evidencePublicID, incidentPublicID, source, evidenceType, summary string
	var collected time.Time
	var valid, truncated bool
	if err := tx.QueryRowContext(ctx, `SELECT e.public_id,i.public_id,e.source,e.type,e.summary,e.collected_at,e.valid,e.truncated
FROM evidence_items e JOIN incidents i ON i.id=e.incident_id WHERE e.id=? FOR UPDATE`, row.ID).Scan(
		&evidencePublicID, &incidentPublicID, &source, &evidenceType, &summary, &collected, &valid, &truncated); err != nil {
		return err
	}
	if evidencePublicID == "" {
		evidencePublicID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("phase7a-evidence:%d", row.ID))).String()
	}
	fact := agent.EvidenceFact{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("phase7a-fact:"+evidencePublicID)).String(), EvidenceID: evidencePublicID,
		IncidentID: incidentPublicID, CycleNo: 1, Type: "legacy.audit_context", SourceSystem: boundSummary(source),
		CollectionPath: "legacy/backfill", CorroborationGroup: "legacy-audit/" + evidencePublicID,
		Authority: "legacy_archive", Integrity: "verified", Freshness: "fresh", Completeness: "complete",
		ClaimUse: "forbidden", CollectionStatus: agent.CollectionAvailable, Direct: false, Truncated: truncated,
		MigratedLegacy: true, Attributes: map[string]string{"legacy_type": evidenceType, "source_hash": row.SourceHash}}
	envelope, err := json.Marshal(map[string]any{"schema_version": 1, "status": agent.CollectionAvailable, "source_system": source,
		"collection_path": "legacy/backfill", "template_version": "v1", "summary": boundSummary(summary),
		"facts": []agent.EvidenceFact{fact}, "truncated": truncated, "migrated_legacy": true})
	if err != nil || len(envelope) > 16*1024 {
		return errors.New("legacy Evidence projection exceeds its bound")
	}
	contentHash := canonicalHashFields("legacy-evidence-projection/v1", string(envelope))
	if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_evidence_archive
(source_evidence_id,incident_id,cycle_no,source_schema_version,target_schema_version,source_snapshot_json,source_hash,
target_evidence_public_id,target_hash,conversion_status,reason_code,source_created_at,archived_at)
VALUES (?,?,1,1,3,?,?,?,?, 'passed','evidence_archived_and_projected',?,?)
ON DUPLICATE KEY UPDATE source_evidence_id=VALUES(source_evidence_id)`, row.ID, *row.IncidentID, row.Snapshot, row.SourceHash, evidencePublicID, row.SourceHash, row.CreatedAt.UTC(), at.UTC()); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE evidence_items SET public_id=?,domain_schema_version=3,cycle_no=1,
producer_type='system_enrichment',producer_dedupe_key=?,facts_json=?,result_hash=?,content_hash=?,migrated_legacy=TRUE,
migrated_legacy_context=TRUE,valid=?,collected_at=? WHERE id=? AND domain_schema_version IS NULL`, evidencePublicID,
		canonicalHashFields("legacy-evidence", fmt.Sprint(row.ID), row.SourceHash), envelope, contentHash, contentHash, valid, collected.UTC(), row.ID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("legacy Evidence changed during backfill")
	}
	if err := verifyV3BackfillIdentity(ctx, tx, "evidence_items", row.ID); err != nil {
		return err
	}
	row.TargetHash, err = verifyBackfillArchive(ctx, tx, "legacy_evidence_archive", "source_evidence_id", row.ID, row.SourceHash)
	if err != nil {
		return err
	}
	return nil
}

func loadAgentStepRows(ctx context.Context, tx *sql.Tx, after, limit uint64) ([]backfillRow, error) {
	return loadBackfillRows(ctx, tx, `SELECT s.id,r.incident_id,s.created_at,JSON_OBJECT(
'id',s.id,'public_id',s.public_id,'agent_run_id',s.agent_run_id,'sequence',s.sequence,'step_type',s.step_type,
'short_reason',s.short_reason,'selected_tool',s.selected_tool,'arguments_json',s.arguments_json,
'arguments_hash',s.arguments_hash,'result_summary',s.result_summary,'result_ref',s.result_ref,
'evidence_public_id',s.evidence_public_id,'status',s.status,'retry_count',s.retry_count,'duration_ms',s.duration_ms,
'input_tokens',s.input_tokens,'output_tokens',s.output_tokens,'error_code',s.error_code,'started_at',s.started_at,
'finished_at',s.finished_at,'created_at',s.created_at)
FROM agent_steps s JOIN agent_runs r ON r.id=s.agent_run_id
WHERE s.domain_schema_version IS NULL AND s.id>? ORDER BY s.id LIMIT ?`, after, limit)
}

func applyAgentStepRow(ctx context.Context, tx *sql.Tx, row *backfillRow, at time.Time) error {
	if row.IncidentID == nil {
		return errors.New("legacy AgentStep has no Incident owner")
	}
	var publicID string
	if err := tx.QueryRowContext(ctx, "SELECT public_id FROM agent_steps WHERE id=? FOR UPDATE", row.ID).Scan(&publicID); err != nil {
		return err
	}
	if publicID == "" {
		publicID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("phase7a-agent-step:%d", row.ID))).String()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_agent_step_archive
(source_agent_step_id,incident_id,cycle_no,source_schema_version,target_schema_version,source_snapshot_json,source_hash,
target_agent_step_public_id,target_hash,conversion_status,reason_code,source_created_at,archived_at)
VALUES (?,?,1,1,3,?,?,?,?, 'passed','agent_step_backfilled',?,?)
ON DUPLICATE KEY UPDATE source_agent_step_id=VALUES(source_agent_step_id)`, row.ID, *row.IncidentID, row.Snapshot, row.SourceHash, publicID, row.SourceHash, row.CreatedAt.UTC(), at.UTC()); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_steps SET domain_schema_version=3,incident_id=?,cycle_no=1,migrated_legacy=TRUE
WHERE id=? AND domain_schema_version IS NULL`, *row.IncidentID, row.ID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("legacy AgentStep changed during backfill")
	}
	if err := verifyV3BackfillIdentity(ctx, tx, "agent_steps", row.ID); err != nil {
		return err
	}
	row.TargetHash, err = verifyBackfillArchive(ctx, tx, "legacy_agent_step_archive", "source_agent_step_id", row.ID, row.SourceHash)
	if err != nil {
		return err
	}
	return nil
}

func loadChangeRows(ctx context.Context, tx *sql.Tx, after, limit uint64) ([]backfillRow, error) {
	return loadBackfillRows(ctx, tx, `SELECT id,incident_id,created_at,JSON_OBJECT(
'id',id,'public_id',public_id,'incident_id',incident_id,'source_type',source_type,'repository',repository,
'repository_owner',repository_owner,'commit_sha',commit_sha,'base_commit_sha',base_commit_sha,
'pull_request_number',pull_request_number,'workflow_run_id',workflow_run_id,'workflow_name',workflow_name,
'workflow_conclusion',workflow_conclusion,'image_repository',image_repository,'image_tag',image_tag,
'image_digest',image_digest,'image_revision',image_revision,'argocd_application',argocd_application,
'argocd_project',argocd_project,'argocd_target_revision',argocd_target_revision,
'argocd_deployed_revision',argocd_deployed_revision,'environment',environment,'cluster',cluster,
'namespace',namespace,'service_name',service_name,'workload_kind',workload_kind,'workload_name',workload_name,
'gitops_path',gitops_path,'started_at',started_at,'completed_at',completed_at,'deployed_at',deployed_at,
'status',status,'category',category,'change_summary',change_summary,'risk_summary',risk_summary,
'correlation_score',correlation_score,'correlation_reasons_json',correlation_reasons_json,
'metadata_json',metadata_json,'truncated',truncated,'degraded',degraded,'idempotency_key',idempotency_key,
'created_at',created_at,'updated_at',updated_at)
FROM changes WHERE domain_schema_version IS NULL AND id>? ORDER BY id LIMIT ?`, after, limit)
}

func applyChangeRow(ctx context.Context, tx *sql.Tx, row *backfillRow, at time.Time) error {
	if row.IncidentID == nil {
		return errors.New("legacy Change has no Incident owner")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_change_candidate_archive
(source_change_id,incident_id,cycle_no,source_schema_version,target_schema_version,source_snapshot_json,source_hash,
target_candidate_id,target_assessment_id,target_hash,conversion_status,reason_code,source_created_at,archived_at)
VALUES (?,?,1,1,3,?,?,NULL,NULL,?,'passed','change_archived_as_legacy_candidate',?,?)
ON DUPLICATE KEY UPDATE source_change_id=VALUES(source_change_id)`, row.ID, *row.IncidentID, row.Snapshot,
		row.SourceHash, row.SourceHash, row.CreatedAt.UTC(), at.UTC()); err != nil {
		return err
	}
	assessment, assessmentHash, err := legacyChangeAssessment(row)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_change_assessment_archive
(source_change_id,incident_id,cycle_no,source_schema_version,target_schema_version,assessment_status,
assessment_snapshot_json,source_change_hash,assessment_hash,conversion_status,reason_code,source_created_at,archived_at)
VALUES (?,?,1,1,3,'unknown',?,?,?,'passed','legacy_change_assessment_archived',?,?)
ON DUPLICATE KEY UPDATE source_change_id=VALUES(source_change_id)`, row.ID, *row.IncidentID, assessment,
		row.SourceHash, assessmentHash, row.CreatedAt.UTC(), at.UTC()); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE changes SET domain_schema_version=3,cycle_no=1,migrated_legacy=TRUE,migrated_legacy_context=TRUE
WHERE id=? AND domain_schema_version IS NULL`, row.ID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("legacy Change changed during backfill")
	}
	if err := verifyV3BackfillIdentity(ctx, tx, "changes", row.ID); err != nil {
		return err
	}
	row.TargetHash, err = verifyBackfillArchive(ctx, tx, "legacy_change_candidate_archive", "source_change_id", row.ID, row.SourceHash)
	if err != nil {
		return err
	}
	var storedAssessment []byte
	var storedSourceHash, storedAssessmentHash, storedStatus, storedReason string
	if err := tx.QueryRowContext(ctx, `SELECT assessment_snapshot_json,source_change_hash,assessment_hash,
conversion_status,reason_code FROM legacy_change_assessment_archive WHERE source_change_id=? FOR UPDATE`, row.ID).Scan(
		&storedAssessment, &storedSourceHash, &storedAssessmentHash, &storedStatus, &storedReason); err != nil {
		return err
	}
	recomputedAssessmentHash, _, err := canonicalHashJSON(storedAssessment)
	if err != nil || storedSourceHash != row.SourceHash || storedAssessmentHash != assessmentHash ||
		recomputedAssessmentHash != assessmentHash || storedStatus != "passed" || storedReason != "legacy_change_assessment_archived" {
		return fmt.Errorf("legacy Change assessment archive drift for source id=%d", row.ID)
	}
	return nil
}

func legacyChangeAssessment(row *backfillRow) (json.RawMessage, string, error) {
	if row == nil || row.ID == 0 || !isSHA256(row.SourceHash) {
		return nil, "", errors.New("legacy Change assessment identity is invalid")
	}
	encoded, err := json.Marshal(map[string]any{
		"assessment_schema_version": 1,
		"source_change_id":          row.ID,
		"status":                    "unknown",
		"reason":                    "migrated_legacy_audit_only",
		"source_change_hash":        row.SourceHash,
		"converter_version":         BackfillConverterVersion,
	})
	if err != nil {
		return nil, "", err
	}
	hash, canonical, err := canonicalHashJSON(encoded)
	return canonical, hash, err
}

func loadBackfillRows(ctx context.Context, tx *sql.Tx, query string, after, limit uint64) (result []backfillRow, retErr error) {
	rows, err := tx.QueryContext(ctx, query, after, limit)
	if err != nil {
		return nil, err
	}
	defer joinRowsCloseError(&retErr, rows, "close legacy backfill source rows")
	result = make([]backfillRow, 0, limit)
	for rows.Next() {
		var item backfillRow
		var incident sql.NullInt64
		var snapshot []byte
		if err := rows.Scan(&item.ID, &incident, &item.CreatedAt, &snapshot); err != nil {
			return nil, err
		}
		if incident.Valid {
			value := uint64(incident.Int64)
			item.IncidentID = &value
		}
		hash, canonical, err := canonicalHashJSON(snapshot)
		if err != nil {
			return nil, fmt.Errorf("canonicalize %d: %w", item.ID, err)
		}
		item.Snapshot, item.SourceHash = canonical, hash
		result = append(result, item)
	}
	return result, rows.Err()
}

func verifyBackfillArchive(ctx context.Context, tx *sql.Tx, table, sourceIDColumn string, sourceID uint64, expectedHash string) (string, error) {
	query := fmt.Sprintf(`SELECT source_snapshot_json,source_hash,target_hash,conversion_status
FROM %s WHERE %s=? FOR UPDATE`, table, sourceIDColumn)
	var snapshot []byte
	var sourceHash, targetHash, status string
	if err := tx.QueryRowContext(ctx, query, sourceID).Scan(&snapshot, &sourceHash, &targetHash, &status); err != nil {
		return "", err
	}
	recomputed, _, err := canonicalHashJSON(snapshot)
	if err != nil {
		return "", err
	}
	if status != "passed" || sourceHash != expectedHash || targetHash != expectedHash || recomputed != expectedHash {
		return "", fmt.Errorf("%s archive parity drift for source id=%d", table, sourceID)
	}
	return recomputed, nil
}

func verifyV3BackfillIdentity(ctx context.Context, tx *sql.Tx, table string, id uint64) error {
	query := fmt.Sprintf("SELECT domain_schema_version,cycle_no FROM %s WHERE id=? FOR UPDATE", table)
	var schema, cycle sql.NullInt64
	if err := tx.QueryRowContext(ctx, query, id).Scan(&schema, &cycle); err != nil {
		return err
	}
	if !schema.Valid || schema.Int64 != 3 || !cycle.Valid || cycle.Int64 != 1 {
		return fmt.Errorf("%s source id=%d has invalid V3 identity", table, id)
	}
	return nil
}

func loadBackfillCursor(ctx context.Context, conn *sql.Conn, identity ReleaseIdentity, operation string) (uint64, bool, error) {
	var lastID uint64
	var status, sha, digest string
	err := conn.QueryRowContext(ctx, `SELECT last_source_id,status,source_exact_sha,binary_image_digest
FROM migration_backfill_cursors WHERE plan_version=? AND operation=?`, identity.PlanVersion, operation).Scan(&lastID, &status, &sha, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if sha != identity.SourceExactSHA || digest != identity.BinaryImageDigest {
		return 0, false, errors.New("backfill cursor release identity mismatch")
	}
	return lastID, status == "passed", nil
}

func nextBackfillBatchNo(ctx context.Context, conn *sql.Conn, plan uint64, operation string) (uint64, error) {
	var batch uint64
	var status string
	err := conn.QueryRowContext(ctx, `SELECT batch_no,status FROM migration_ledger
WHERE plan_version=? AND operation=? ORDER BY batch_no DESC,attempt DESC LIMIT 1`, plan, operation).Scan(&batch, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	if status == "failed" {
		return batch, nil
	}
	return batch + 1, nil
}

func passedBackfillCount(ctx context.Context, conn *sql.Conn, plan uint64, operation string) (uint64, error) {
	var count uint64
	err := conn.QueryRowContext(ctx, `SELECT COALESCE(SUM(source_count),0) FROM migration_ledger
WHERE plan_version=? AND operation=? AND status='passed'`, plan, operation).Scan(&count)
	return count, err
}

func markBackfillCursor(ctx context.Context, tx *sql.Tx, identity ReleaseIdentity, operation string, unit backfillUnit, nextBatch, lastID uint64, status string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO migration_backfill_cursors
(plan_version,operation,source_table,target_table,next_batch_no,last_source_id,status,source_exact_sha,binary_image_digest,updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?)
ON DUPLICATE KEY UPDATE source_table=VALUES(source_table),target_table=VALUES(target_table),next_batch_no=VALUES(next_batch_no),
last_source_id=VALUES(last_source_id),status=VALUES(status),source_exact_sha=VALUES(source_exact_sha),
binary_image_digest=VALUES(binary_image_digest),updated_at=VALUES(updated_at)`, identity.PlanVersion, operation,
		unit.sourceTable, unit.targetTable, nextBatch, lastID, status, identity.SourceExactSHA, identity.BinaryImageDigest, at.UTC())
	return err
}

func boundSummary(value string) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "")
	if len(value) <= 2048 {
		return value
	}
	return value[:2048]
}
