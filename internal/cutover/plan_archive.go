package cutover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func archiveLegacyPlansAndApprovals(ctx context.Context, tx *sql.Tx, at time.Time) error {
	if tx == nil {
		return errors.New("legacy plan archive transaction is required")
	}
	planRows, err := tx.QueryContext(ctx, `SELECT id,incident_id,status,created_at,updated_at,CAST(JSON_OBJECT(
'id',id,'public_id',public_id,'incident_id',incident_id,'plan_version',plan_version,'plan_hash',plan_hash,
'status',status,'operation_type',operation_type,'target_repository',target_repository,
'target_base_revision',target_base_revision,'target_path',target_path,'parameters_json',parameters_json,
'evidence_references_json',evidence_references_json,'risk_level',risk_level,
'policy_snapshot_hash',policy_snapshot_hash,'expected_before_hash',expected_before_hash,
'proposed_patch_hash',proposed_patch_hash,'patch_summary',patch_summary,'rollback_plan',rollback_plan,
'validation_plan',validation_plan,'row_version',row_version,'created_at',created_at,'updated_at',updated_at) AS CHAR)
FROM remediation_plans WHERE domain_schema_version IS NULL ORDER BY id FOR UPDATE`)
	if err != nil {
		return err
	}
	type planRow struct {
		id, incidentID uint64
		status         string
		createdAt      time.Time
		updatedAt      time.Time
		snapshot       []byte
		hash           string
	}
	plans := make([]planRow, 0)
	for planRows.Next() {
		var row planRow
		if err := planRows.Scan(&row.id, &row.incidentID, &row.status, &row.createdAt, &row.updatedAt, &row.snapshot); err != nil {
			_ = planRows.Close()
			return err
		}
		hash, canonical, err := canonicalHashJSON(row.snapshot)
		if err != nil {
			_ = planRows.Close()
			return fmt.Errorf("canonicalize legacy Plan id=%d: %w", row.id, err)
		}
		row.hash, row.snapshot = hash, canonical
		plans = append(plans, row)
	}
	if err := planRows.Err(); err != nil {
		_ = planRows.Close()
		return err
	}
	if err := planRows.Close(); err != nil {
		return err
	}
	for _, row := range plans {
		if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_remediation_plan_archive
(source_plan_id,incident_id,cycle_no,source_status,source_snapshot_json,source_content_hash,
converter_result,reason_code,source_created_at,source_updated_at,archived_at)
VALUES (?,?,1,?,?,?,'superseded','legacy_plan_superseded',?,?,?)
ON DUPLICATE KEY UPDATE source_plan_id=VALUES(source_plan_id)`, row.id, row.incidentID, row.status,
			row.snapshot, row.hash, row.createdAt.UTC(), row.updatedAt.UTC(), at.UTC()); err != nil {
			return fmt.Errorf("archive legacy Plan id=%d: %w", row.id, err)
		}
		var archivedHash string
		if err := tx.QueryRowContext(ctx, `SELECT source_content_hash FROM legacy_remediation_plan_archive
WHERE source_plan_id=? FOR UPDATE`, row.id).Scan(&archivedHash); err != nil || archivedHash != row.hash {
			return fmt.Errorf("legacy Plan archive hash drift id=%d: %w", row.id, err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE remediation_plans SET domain_schema_version=3,cycle_no=1,
v3_status='superseded',status='superseded',hash_schema_version=1,canonical_plan_hash=?,migrated_legacy=TRUE,migrated_legacy_context=TRUE,
row_version=row_version+1,updated_at=? WHERE id=? AND domain_schema_version IS NULL`, row.hash, at.UTC(), row.id)
		if err != nil {
			return fmt.Errorf("supersede legacy Plan id=%d: %w", row.id, err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return fmt.Errorf("legacy Plan id=%d changed during archive", row.id)
		}
	}

	approvalRows, err := tx.QueryContext(ctx, `SELECT a.id,a.plan_id,p.incident_id,a.decision,a.actor,a.created_at,
CAST(JSON_OBJECT('id',a.id,'public_id',a.public_id,'plan_id',a.plan_id,'decision',a.decision,'actor',a.actor,
'approved_plan_hash',a.approved_plan_hash,'approved_patch_hash',a.approved_patch_hash,'created_at',a.created_at) AS CHAR)
FROM remediation_approvals a JOIN remediation_plans p ON p.id=a.plan_id
WHERE a.domain_schema_version IS NULL ORDER BY a.id FOR UPDATE`)
	if err != nil {
		return err
	}
	type approvalRow struct {
		id, planID, incidentID uint64
		decision, actor        string
		createdAt              time.Time
		snapshot               []byte
		hash                   string
	}
	approvals := make([]approvalRow, 0)
	for approvalRows.Next() {
		var row approvalRow
		if err := approvalRows.Scan(&row.id, &row.planID, &row.incidentID, &row.decision, &row.actor, &row.createdAt, &row.snapshot); err != nil {
			_ = approvalRows.Close()
			return err
		}
		hash, canonical, err := canonicalHashJSON(row.snapshot)
		if err != nil {
			_ = approvalRows.Close()
			return fmt.Errorf("canonicalize legacy Approval id=%d: %w", row.id, err)
		}
		row.hash, row.snapshot = hash, canonical
		approvals = append(approvals, row)
	}
	if err := approvalRows.Err(); err != nil {
		_ = approvalRows.Close()
		return err
	}
	if err := approvalRows.Close(); err != nil {
		return err
	}
	for _, row := range approvals {
		if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_approval_archive
(source_approval_id,source_plan_id,incident_id,cycle_no,decision,actor,source_snapshot_json,source_content_hash,
converter_result,reason_code,source_created_at,archived_at)
VALUES (?,?,?,1,?,?,?,?,'non_authoritative','legacy_approval_non_authoritative',?,?)
ON DUPLICATE KEY UPDATE source_approval_id=VALUES(source_approval_id)`, row.id, row.planID, row.incidentID,
			row.decision, row.actor, row.snapshot, row.hash, row.createdAt.UTC(), at.UTC()); err != nil {
			return fmt.Errorf("archive legacy Approval id=%d: %w", row.id, err)
		}
		var archivedHash string
		if err := tx.QueryRowContext(ctx, "SELECT source_content_hash FROM legacy_approval_archive WHERE source_approval_id=? FOR UPDATE", row.id).Scan(&archivedHash); err != nil || archivedHash != row.hash {
			return fmt.Errorf("legacy Approval archive hash drift id=%d: %w", row.id, err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE remediation_approvals SET domain_schema_version=3,
incident_id=?,cycle_no=1,migrated_legacy=TRUE WHERE id=? AND domain_schema_version IS NULL`, row.incidentID, row.id)
		if err != nil {
			return fmt.Errorf("mark legacy Approval id=%d non-authoritative: %w", row.id, err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return fmt.Errorf("legacy Approval id=%d changed during archive", row.id)
		}
	}
	return nil
}
