package cutover

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/google/uuid"
)

type legacyIncidentRow struct {
	id           uint64
	publicID     string
	status       string
	version      uint64
	resolvedAt   sql.NullTime
	currentRunID sql.NullInt64
	createdAt    time.Time
	updatedAt    time.Time
}

func convertIncidentStates(ctx context.Context, tx *sql.Tx, at time.Time) (retErr error) {
	if tx == nil {
		return errors.New("legacy Incident converter transaction is required")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,public_id,status,version,resolved_at,current_agent_run_id,created_at,updated_at
FROM incidents WHERE domain_schema_version IS NULL ORDER BY id FOR UPDATE`)
	if err != nil {
		return err
	}
	defer joinRowsCloseError(&retErr, rows, "close legacy Incident rows")
	incidents := make([]legacyIncidentRow, 0)
	for rows.Next() {
		var row legacyIncidentRow
		if err := rows.Scan(&row.id, &row.publicID, &row.status, &row.version, &row.resolvedAt,
			&row.currentRunID, &row.createdAt, &row.updatedAt); err != nil {
			return err
		}
		incidents = append(incidents, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, row := range incidents {
		facts, err := loadIncidentConversionFacts(ctx, tx, row.id)
		if err != nil {
			return err
		}
		conversion := ConvertIncidentState(LegacyIncidentStateInput{
			IncidentID: row.id, IncidentPublicID: row.publicID, SourceStatus: row.status,
			SourceVersion: row.version, CycleNo: 1, ActiveVerification: facts.activeVerification,
			CompatibleActiveVerification:  facts.compatibleActiveVerification,
			CompatiblePassingVerification: facts.compatiblePassingVerification,
			VerificationTriggerValid:      facts.compatiblePassingVerification,
			VerificationRevisionsValid:    facts.compatiblePassingVerification,
			ObservedExternalChange:        facts.observedExternalChange,
			PlanApprovalWithoutWrite:      facts.planApprovalWithoutWrite,
		})
		if err := archiveIncidentState(ctx, tx, row, conversion, at); err != nil {
			return err
		}
		if err := persistConvertedIncident(ctx, tx, row, conversion, at); err != nil {
			return err
		}
		if err := appendIncidentConversionEvent(ctx, tx, row, conversion, at); err != nil {
			return err
		}
		if facts.incompatibleActiveChildren > 0 {
			if err := ensureIncidentFallbackTasks(ctx, tx, row, conversion, at); err != nil {
				return err
			}
		}
	}
	return nil
}

type incidentConversionFacts struct {
	activeVerification            bool
	compatibleActiveVerification  bool
	compatiblePassingVerification bool
	observedExternalChange        bool
	planApprovalWithoutWrite      bool
	incompatibleActiveChildren    uint64
}

func loadIncidentConversionFacts(ctx context.Context, tx *sql.Tx, incidentID uint64) (incidentConversionFacts, error) {
	var result incidentConversionFacts
	var active, compatibleActive, compatiblePassing, observed, planApproval, incompatible uint64
	if err := tx.QueryRowContext(ctx, `SELECT
COALESCE(SUM(source_status IN ('pending','running')),0),
COALESCE(SUM(source_status IN ('pending','running') AND conversion_status='passed'),0),
COALESCE(SUM(source_status='passed' AND conversion_status='passed'),0),
COALESCE(SUM(source_status IN ('pending','running') AND conversion_status<>'passed'),0)
FROM legacy_verification_archive WHERE incident_id=?`, incidentID).Scan(&active, &compatibleActive, &compatiblePassing, &incompatible); err != nil {
		return result, err
	}
	var incompatibleAgent uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(source_status IN ('PENDING','RUNNING') AND conversion_status<>'passed'),0)
FROM legacy_agent_checkpoint_archive WHERE incident_id=?`, incidentID).Scan(&incompatibleAgent); err != nil {
		return result, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM legacy_change_request_archive
WHERE incident_id=? AND external_state='observe-existing-pr' AND conversion_status='passed'
  AND source_status IN ('pending','delivering','pr_created','ci_pending','ci_passed','merge_pending',
    'merged','argocd_pending','syncing','synced','rollout_pending')`, incidentID).Scan(&observed); err != nil {
		return result, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM legacy_remediation_plan_archive p
LEFT JOIN legacy_change_request_archive c ON c.incident_id=p.incident_id AND c.external_state='observe-existing-pr' AND c.conversion_status='passed'
WHERE p.incident_id=? AND c.source_change_request_id IS NULL`, incidentID).Scan(&planApproval); err != nil {
		return result, err
	}
	result.activeVerification = active > 0
	result.compatibleActiveVerification = compatibleActive > 0
	result.compatiblePassingVerification = compatiblePassing > 0
	result.observedExternalChange = observed > 0
	result.planApprovalWithoutWrite = planApproval > 0
	result.incompatibleActiveChildren = incompatible + incompatibleAgent
	return result, nil
}

func archiveIncidentState(ctx context.Context, tx *sql.Tx, row legacyIncidentRow, conversion IncidentStateConversion, at time.Time) error {
	snapshot, err := json.Marshal(map[string]any{
		"status": row.status, "version": row.version, "resolved_at": nullableArchivedTime(row.resolvedAt),
		"current_agent_run_id": nullableArchivedInt(row.currentRunID), "created_at": row.createdAt.UTC(), "updated_at": row.updatedAt.UTC(),
	})
	if err != nil {
		return err
	}
	hash, canonical, err := canonicalHashJSON(snapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO legacy_incident_state_archive
(source_incident_id,incident_public_id,source_status,source_version,snapshot_json,snapshot_hash,target_status,reason_code,archived_at)
VALUES (?,?,?,?,?,?,?,?,?)
ON DUPLICATE KEY UPDATE source_incident_id=VALUES(source_incident_id)`, row.id, row.publicID, row.status,
		row.version, canonical, hash, conversion.TargetV3Status, conversion.ReasonCode, at.UTC())
	if err != nil {
		return fmt.Errorf("archive legacy Incident id=%d: %w", row.id, err)
	}
	var archivedHash, target, reason string
	if err := tx.QueryRowContext(ctx, `SELECT snapshot_hash,target_status,reason_code
FROM legacy_incident_state_archive WHERE source_incident_id=? FOR UPDATE`, row.id).Scan(&archivedHash, &target, &reason); err != nil {
		return err
	}
	if archivedHash != hash || target != conversion.TargetV3Status || reason != conversion.ReasonCode {
		return fmt.Errorf("legacy Incident archive drift id=%d", row.id)
	}
	return nil
}

func persistConvertedIncident(ctx context.Context, tx *sql.Tx, row legacyIncidentRow, conversion IncidentStateConversion, at time.Time) error {
	var blocking, blockedAt any
	if conversion.NeedsAttention {
		blocking, blockedAt = conversion.ReasonCode, at.UTC()
	}
	var resolvedAt, terminalAt any
	if conversion.PreserveResolved && row.resolvedAt.Valid {
		resolvedAt, terminalAt = row.resolvedAt.Time.UTC(), row.resolvedAt.Time.UTC()
	} else if conversion.TargetV3Status == "closed" {
		terminalAt = at.UTC()
	}
	result, err := tx.ExecContext(ctx, `UPDATE incidents SET status=?,domain_schema_version=3,v3_status=?,cycle_no=1,
correlation_key_version=COALESCE(correlation_key_version,1),needs_attention=?,blocking_reason_code=?,blocked_at=?,
resolved_at=?,terminal_at=?,migrated_legacy=TRUE,migrated_legacy_context=TRUE,version=version+1,updated_at=?
WHERE id=? AND domain_schema_version IS NULL AND version=?`, conversion.TargetLegacyStatus,
		conversion.TargetV3Status, conversion.NeedsAttention, blocking, blockedAt, resolvedAt, terminalAt,
		at.UTC(), row.id, row.version)
	if err != nil {
		return fmt.Errorf("convert legacy Incident id=%d: %w", row.id, err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("legacy Incident id=%d changed during conversion", row.id)
	}
	var mismatchedRuns uint64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs
WHERE incident_id=? AND domain_schema_version=3 AND migrated_legacy=TRUE AND expected_incident_version<>?`,
		row.id, row.version+1).Scan(&mismatchedRuns); err != nil {
		return err
	}
	if mismatchedRuns != 0 {
		return fmt.Errorf("legacy Incident id=%d has %d AgentRuns with stale expected incident versions", row.id, mismatchedRuns)
	}
	return nil
}

func appendIncidentConversionEvent(ctx context.Context, tx *sql.Tx, row legacyIncidentRow, conversion IncidentStateConversion, at time.Time) error {
	metadata, err := json.Marshal(map[string]any{
		"source_status": conversion.SourceStatus, "target_status": conversion.TargetV3Status,
		"reason_code": conversion.ReasonCode, "converter_version": conversion.ConverterVersion,
		"input_hash": conversion.InputHash, "output_hash": conversion.OutputHash,
		"migrated_legacy": true, "migrated_legacy_context": true,
	})
	if err != nil || len(metadata) > 8*1024 {
		return errors.New("legacy Incident conversion event metadata exceeds its bound")
	}
	publicID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("phase7a-incident-state:%d:%s", row.id, conversion.InputHash))).String()
	idempotency := canonicalHashFields("legacy-incident-state-event/v2", fmt.Sprint(row.id), conversion.InputHash)
	_, err = tx.ExecContext(ctx, `INSERT INTO incident_events (
public_id,incident_id,domain_schema_version,cycle_no,event_schema_version,event_type,source_status,target_status,
reason_code,converter_version,conversion_input_hash,conversion_output_hash,migrated_legacy_context,idempotency_key,
actor_type,actor_id,summary,metadata_json,occurred_at,created_at,migrated_legacy)
VALUES (?,?,3,1,1,'legacy_state_converted',?,?,?,?,?,?,TRUE,?,'system','phase7a-cutover',?,?,?,?,TRUE)`,
		publicID, row.id, conversion.SourceStatus, conversion.TargetV3Status, conversion.ReasonCode,
		conversion.ConverterVersion, conversion.InputHash, conversion.OutputHash, idempotency,
		"Legacy Incident state converted under the Phase 7A contract", metadata, at.UTC(), at.UTC())
	if err != nil {
		return fmt.Errorf("append legacy Incident conversion event id=%d: %w", row.id, err)
	}
	return nil
}

func ensureIncidentFallbackTasks(ctx context.Context, tx *sql.Tx, incident legacyIncidentRow, state IncidentStateConversion, at time.Time) (retErr error) {
	type fallbackSubject struct {
		subjectType string
		subjectID   uint64
	}
	rows, err := tx.QueryContext(ctx, `SELECT 'agent_run',source_agent_run_id
FROM legacy_agent_checkpoint_archive
WHERE incident_id=? AND source_status IN ('PENDING','RUNNING') AND conversion_status<>'passed'
UNION ALL
SELECT 'verification_run',source_verification_run_id
FROM legacy_verification_archive
WHERE incident_id=? AND source_status IN ('pending','running') AND conversion_status<>'passed'
ORDER BY 1,2`, incident.id, incident.id)
	if err != nil {
		return err
	}
	defer joinRowsCloseError(&retErr, rows, "close legacy Incident fallback subject rows")
	subjects := make([]fallbackSubject, 0)
	for rows.Next() {
		var item fallbackSubject
		if err := rows.Scan(&item.subjectType, &item.subjectID); err != nil {
			return err
		}
		subjects = append(subjects, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, subject := range subjects {
		payload, err := canonicalTaskPayload(map[string]any{
			"mode": "start", "incident_public_id": incident.publicID, "cycle_no": 1,
			"migrated_legacy_context": true,
		})
		if err != nil {
			return err
		}
		outcome, err := ensureConversionTask(ctx, tx, conversionTaskSpec{
			IncidentID: incident.id, CycleNo: 1, TaskType: asyncjob.TaskInvestigationAdvance,
			SubjectType: "incident", SubjectID: incident.id, Transition: "investigation.start",
			ExpectedSubjectVersion: incident.version + 1, Payload: payload,
			LegacySubjectType: "incident", LegacySubjectID: incident.id, LegacySourceVersion: incident.version,
			ConverterVersion: phase7AConverter, MigratedLegacy: false,
			MigratedLegacyContext: true, Priority: 70,
		}, at)
		if err != nil {
			return err
		}
		if err := attachFallbackTaskToConversion(ctx, tx, subject.subjectType, subject.subjectID, outcome, at); err != nil {
			return err
		}
	}
	return nil
}

func attachFallbackTaskToConversion(ctx context.Context, tx *sql.Tx, subjectType string, subjectID uint64, task conversionTaskOutcome, at time.Time) error {
	var input conversionRecordInput
	input.SubjectType, input.SubjectID = subjectType, subjectID
	err := tx.QueryRowContext(ctx, `SELECT incident_id,cycle_no,converter_version,input_hash,output_hash,status,reason_code,
source_schema_version,target_schema_version,source_table,target_table,migrated_legacy_context
FROM legacy_conversion_records WHERE subject_type=? AND subject_id=? ORDER BY attempt DESC LIMIT 1 FOR UPDATE`,
		subjectType, subjectID).Scan(&input.IncidentID, &input.CycleNo, &input.ConverterVersion,
		&input.InputHash, &input.OutputHash, &input.Status, &input.ReasonCode, &input.SourceSchemaVersion,
		&input.TargetSchemaVersion, &input.SourceTable, &input.TargetTable, &input.MigratedLegacyContext)
	if err != nil {
		return fmt.Errorf("load fallback conversion %s/%d: %w", subjectType, subjectID, err)
	}
	input.TargetTaskID, input.AntiJoinResult, input.CreatedAt = task.TaskID, task.AntiJoin, at
	_, err = recordConversion(ctx, tx, input)
	return err
}

func nullableArchivedTime(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time.UTC()
}

func nullableArchivedInt(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}
