package cutover

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/schemaversion"
	"github.com/google/uuid"
)

const (
	phase7ALockName       = "cloudops-copilot:phase7a-prepare"
	phase7AConverter      = "phase7a-cutover/v1"
	phase7AControlName    = "release-a"
	phase7AOutboxRegistry = "incident.created,incident.updated,incident.signal_resolved,incident.status_changed,remediation_plan_policy_rejected,remediation_planning_started,remediation_plan_awaiting_approval,remediation_plan_approved,remediation_plan_rejected,remediation_draft_pr_created,controlled_direct_execution_delivered,delivery_argocd_revision_detected,delivery_pending,delivery_delivering,delivery_pr_created,delivery_ci_pending,delivery_ci_passed,delivery_ci_failed,delivery_merge_pending,delivery_pr_merged,delivery_pr_closed,delivery_argocd_pending,delivery_argocd_sync_started,delivery_argocd_sync_succeeded,delivery_argocd_sync_failed,delivery_argocd_timeout,delivery_kubernetes_rollout_started,delivery_rollout_failed,delivery_completed,delivery_merge_timeout,delivery_revision_mismatch,delivery_delivery_cancelled,delivery_failed,verification_started,verification_check_pending,verification_check_running,verification_check_passed,verification_check_failed,verification_check_timed_out,verification_check_unavailable,verification_check_invalid,verification_check_cancelled,verification_failed,verification_passed,incident_resolved_after_verification,incident_returned_to_investigation,verification_timed_out"
)

var phase7AEventTypes = strings.Split(phase7AOutboxRegistry, ",")

// PrepareRequest contains externally observed quiesce facts. All four counts
// are explicit because database state cannot prove that HTTP writers, old
// deployments, or an unknown GitHub operation are absent.
type PrepareRequest struct {
	PlanVersion                  uint64
	SourceExactSHA               string
	BinaryImageDigest            string
	ObservedIngressWriters       uint64
	ObservedMutationWriters      uint64
	ObservedLegacyWorkers        uint64
	ObservedUnknownExternalWrite uint64
}

func (r PrepareRequest) Validate() error {
	if r.PlanVersion == 0 || !isExactSHA(r.SourceExactSHA) || !imageDigestPattern.MatchString(r.BinaryImageDigest) {
		return errors.New("phase7a prepare requires a positive plan version, exact lowercase SHA, and exact sha256 image digest")
	}
	if r.ObservedIngressWriters+r.ObservedMutationWriters+r.ObservedLegacyWorkers+r.ObservedUnknownExternalWrite != 0 {
		return fmt.Errorf("quiesce observations must all be zero: ingress=%d mutations=%d legacy_workers=%d unknown_external_writes=%d",
			r.ObservedIngressWriters, r.ObservedMutationWriters, r.ObservedLegacyWorkers, r.ObservedUnknownExternalWrite)
	}
	return nil
}

type PrepareReport struct {
	PlanVersion                  uint64            `json:"plan_version"`
	SourceExactSHA               string            `json:"source_exact_sha"`
	BinaryImageDigest            string            `json:"binary_image_digest"`
	SchemaVersion                uint64            `json:"schema_version"`
	QuiesceLedgerPublicID        string            `json:"quiesce_ledger_public_id"`
	ReconciliationLedgerPublicID string            `json:"reconciliation_ledger_public_id"`
	ConverterAuditLedgerPublicID string            `json:"converter_audit_ledger_public_id"`
	Counts                       map[string]uint64 `json:"counts"`
	PreparedAt                   time.Time         `json:"prepared_at"`
}

type Phase7APreparer struct {
	db          *sql.DB
	lockTimeout time.Duration
}

func NewPhase7APreparer(db *sql.DB, lockTimeout time.Duration) (*Phase7APreparer, error) {
	if db == nil || lockTimeout <= 0 {
		return nil, errors.New("phase7a prepare requires a database and positive lock timeout")
	}
	return &Phase7APreparer{db: db, lockTimeout: lockTimeout}, nil
}

func (p *Phase7APreparer) Prepare(ctx context.Context, request PrepareRequest) (report PrepareReport, retErr error) {
	if p == nil || p.db == nil {
		return report, errors.New("phase7a preparer is not initialized")
	}
	if err := request.Validate(); err != nil {
		return report, err
	}
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return report, fmt.Errorf("reserve phase7a database connection: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, conn.Close()) }()
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", phase7ALockName, int64(math.Ceil(p.lockTimeout.Seconds()))).Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		return report, fmt.Errorf("acquire phase7a cutover lock: %w", err)
	}
	defer func() {
		var released sql.NullInt64
		if err := conn.QueryRowContext(context.WithoutCancel(ctx), "SELECT RELEASE_LOCK(?)", phase7ALockName).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			retErr = errors.Join(retErr, errors.New("release phase7a cutover lock failed"))
		}
	}()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return report, fmt.Errorf("begin phase7a transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var version uint64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_id),0) FROM goose_db_version WHERE is_applied=1").Scan(&version); err != nil || version != uint64(schemaversion.Latest) {
		return report, fmt.Errorf("phase7a schema version=%d want=%d: %w", version, schemaversion.Latest, err)
	}
	var markerCount, activeLegacyLeases, runningTasks uint64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM migration_ledger WHERE operation=?", MarkerOperation).Scan(&markerCount); err != nil || markerCount != 0 {
		return report, fmt.Errorf("CUTOVER-V3 marker count=%d want zero: %w", markerCount, err)
	}
	if err := tx.QueryRowContext(ctx, legacyActiveLeaseCountSQL).Scan(&activeLegacyLeases); err != nil || activeLegacyLeases != 0 {
		return report, fmt.Errorf("legacy active leases=%d want zero: %w", activeLegacyLeases, err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM async_tasks WHERE status='running'").Scan(&runningTasks); err != nil || runningTasks != 0 {
		return report, fmt.Errorf("running V3 tasks=%d want zero: %w", runningTasks, err)
	}
	if existing, found, err := existingPreparation(ctx, tx, request, version); err != nil {
		return report, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return report, fmt.Errorf("commit idempotent phase7a inspection: %w", err)
		}
		return existing, nil
	}

	now, err := databaseTime(ctx, tx)
	if err != nil {
		return report, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO cutover_controls (
control_name, plan_version, source_exact_sha, binary_image_digest,
ingress_quiesced, mutations_quiesced, legacy_workers_quiesced,
observed_ingress_writers, observed_mutation_writers, observed_legacy_workers,
observed_unknown_external_writes, prepared_at, completed_at)
VALUES (?, ?, ?, ?, TRUE, TRUE, TRUE, ?, ?, ?, ?, ?, NULL)
ON DUPLICATE KEY UPDATE plan_version=VALUES(plan_version), source_exact_sha=VALUES(source_exact_sha),
binary_image_digest=VALUES(binary_image_digest), ingress_quiesced=TRUE, mutations_quiesced=TRUE,
legacy_workers_quiesced=TRUE, observed_ingress_writers=VALUES(observed_ingress_writers),
observed_mutation_writers=VALUES(observed_mutation_writers), observed_legacy_workers=VALUES(observed_legacy_workers),
observed_unknown_external_writes=VALUES(observed_unknown_external_writes), prepared_at=VALUES(prepared_at), completed_at=NULL`,
		phase7AControlName, request.PlanVersion, request.SourceExactSHA, request.BinaryImageDigest,
		request.ObservedIngressWriters, request.ObservedMutationWriters, request.ObservedLegacyWorkers,
		request.ObservedUnknownExternalWrite, now); err != nil {
		return report, fmt.Errorf("persist phase7a quiesce control: %w", err)
	}

	unknown, err := unknownOutboxCount(ctx, tx)
	if err != nil || unknown != 0 {
		return report, fmt.Errorf("unknown or invalid legacy outbox rows=%d: %w", unknown, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_outbox_archive (
source_outbox_id,event_id,aggregate_type,aggregate_id,event_type,schema_version,publication_state,
payload_json,payload_hash,occurred_at,published_at,archived_at)
SELECT id,event_id,aggregate_type,aggregate_id,event_type,schema_version,
CASE WHEN published_at IS NULL THEN 'unpublished' ELSE 'published' END,
payload_json,SHA2(CAST(payload_json AS CHAR),256),occurred_at,published_at,?
FROM outbox_events ORDER BY id
ON DUPLICATE KEY UPDATE source_outbox_id=VALUES(source_outbox_id)`, now); err != nil {
		return report, fmt.Errorf("archive legacy outbox: %w", err)
	}
	var outboxSource, outboxArchived uint64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM outbox_events").Scan(&outboxSource); err != nil {
		return report, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM legacy_outbox_archive").Scan(&outboxArchived); err != nil || outboxArchived != outboxSource {
		return report, fmt.Errorf("outbox archive count source=%d target=%d: %w", outboxSource, outboxArchived, err)
	}

	if err := archiveAndConvertIncidents(ctx, tx, now); err != nil {
		return report, err
	}
	if err := archiveAndConvertAgentRuns(ctx, tx, now); err != nil {
		return report, err
	}
	if err := archiveAndConvertChangeRequests(ctx, tx, now); err != nil {
		return report, err
	}
	if err := archiveAndConvertVerification(ctx, tx, now); err != nil {
		return report, err
	}
	if err := archivePostmortems(ctx, tx, now); err != nil {
		return report, err
	}

	counts, err := collectCutoverCounts(ctx, tx)
	if err != nil {
		return report, err
	}
	quiesceID, err := insertLedger(ctx, tx, request, QuiesceOperation, "quiesce", 0, 0, now,
		"ingress, mutations, legacy workers, active leases, and unknown external writes are zero")
	if err != nil {
		return report, err
	}
	reconcileID, err := insertLedger(ctx, tx, request, ReconciliationOperation, "cutover", counts["outbox_source"], counts["outbox_archived"], now,
		"legacy outbox and external-write-bearing children archived and reconciled without payload-derived tasks")
	if err != nil {
		return report, err
	}
	auditID, err := insertLedger(ctx, tx, request, ConverterAuditOperation, "cutover", counts["conversion_records"], counts["conversion_records"], now,
		"versioned child and incident conversion records passed anti-join audit; migrated legacy provenance retained")
	if err != nil {
		return report, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE cutover_controls SET completed_at=? WHERE control_name=?", now, phase7AControlName); err != nil {
		return report, fmt.Errorf("complete phase7a control: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("commit phase7a preparation: %w", err)
	}
	return PrepareReport{PlanVersion: request.PlanVersion, SourceExactSHA: request.SourceExactSHA,
		BinaryImageDigest: request.BinaryImageDigest, SchemaVersion: version,
		QuiesceLedgerPublicID: quiesceID, ReconciliationLedgerPublicID: reconcileID,
		ConverterAuditLedgerPublicID: auditID, Counts: counts, PreparedAt: now}, nil
}

func existingPreparation(ctx context.Context, tx *sql.Tx, request PrepareRequest, version uint64) (PrepareReport, bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT public_id,operation,status,source_exact_sha,binary_image_digest
FROM migration_ledger WHERE plan_version=? AND operation IN (?,?,?) ORDER BY id FOR UPDATE`, request.PlanVersion,
		QuiesceOperation, ReconciliationOperation, ConverterAuditOperation)
	if err != nil {
		return PrepareReport{}, false, fmt.Errorf("inspect existing phase7a preparation: %w", err)
	}
	defer rows.Close()
	ids := map[string]string{}
	for rows.Next() {
		var id, operation, status, sourceSHA, digest string
		if err := rows.Scan(&id, &operation, &status, &sourceSHA, &digest); err != nil {
			return PrepareReport{}, false, err
		}
		if status != "passed" || sourceSHA != request.SourceExactSHA || digest != request.BinaryImageDigest {
			return PrepareReport{}, false, fmt.Errorf("existing phase7a ledger %s has a different or incomplete release identity", operation)
		}
		if _, duplicate := ids[operation]; duplicate {
			return PrepareReport{}, false, fmt.Errorf("duplicate passed phase7a ledger operation %s", operation)
		}
		ids[operation] = id
	}
	if err := rows.Err(); err != nil {
		return PrepareReport{}, false, err
	}
	if len(ids) == 0 {
		return PrepareReport{}, false, nil
	}
	if len(ids) != 3 || ids[QuiesceOperation] == "" || ids[ReconciliationOperation] == "" || ids[ConverterAuditOperation] == "" {
		return PrepareReport{}, false, fmt.Errorf("partial phase7a preparation ledger count=%d; manual audit is required", len(ids))
	}
	var completed sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT completed_at FROM cutover_controls WHERE control_name=? AND plan_version=? AND source_exact_sha=? AND binary_image_digest=? FOR UPDATE`,
		phase7AControlName, request.PlanVersion, request.SourceExactSHA, request.BinaryImageDigest).Scan(&completed); err != nil {
		return PrepareReport{}, false, fmt.Errorf("read completed phase7a control: %w", err)
	}
	if !completed.Valid {
		return PrepareReport{}, false, errors.New("phase7a prerequisite ledgers exist without a completed quiesce control")
	}
	counts, err := collectCutoverCounts(ctx, tx)
	if err != nil {
		return PrepareReport{}, false, err
	}
	return PrepareReport{PlanVersion: request.PlanVersion, SourceExactSHA: request.SourceExactSHA, BinaryImageDigest: request.BinaryImageDigest,
		SchemaVersion: version, QuiesceLedgerPublicID: ids[QuiesceOperation], ReconciliationLedgerPublicID: ids[ReconciliationOperation],
		ConverterAuditLedgerPublicID: ids[ConverterAuditOperation], Counts: counts, PreparedAt: completed.Time.UTC()}, true, nil
}

func unknownOutboxCount(ctx context.Context, tx *sql.Tx) (uint64, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(phase7AEventTypes)), ",")
	args := make([]any, 0, len(phase7AEventTypes))
	for _, item := range phase7AEventTypes {
		args = append(args, item)
	}
	query := "SELECT COUNT(*) FROM outbox_events WHERE aggregate_type <> 'incident' OR schema_version <> 1 OR NOT JSON_VALID(payload_json) OR event_type NOT IN (" + placeholders + ")"
	var count uint64
	err := tx.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func archiveAndConvertIncidents(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO legacy_incident_state_archive (
source_incident_id,incident_public_id,source_status,source_version,snapshot_json,snapshot_hash,target_status,reason_code,archived_at)
SELECT id,public_id,status,version,
JSON_OBJECT('status',status,'version',version,'resolved_at',resolved_at,'current_agent_run_id',current_agent_run_id),
SHA2(CONCAT_WS('|',status,version,COALESCE(CAST(resolved_at AS CHAR),''),COALESCE(current_agent_run_id,0)),256),
'investigating',CASE WHEN status='RESOLVED' THEN 'legacy_resolution_unverified' WHEN status='FAILED' THEN 'legacy_failed_blocked' ELSE 'legacy_state_converted' END,?
FROM incidents WHERE domain_schema_version IS NULL ORDER BY id
ON DUPLICATE KEY UPDATE source_incident_id=VALUES(source_incident_id)`, now)
	if err != nil {
		return fmt.Errorf("archive legacy incidents: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE incidents i
JOIN legacy_incident_state_archive a ON a.source_incident_id=i.id
SET i.domain_schema_version=3,i.v3_status='investigating',i.cycle_no=1,i.correlation_key_version=1,
i.needs_attention=TRUE,i.blocking_reason_code=a.reason_code,i.migrated_legacy=TRUE,
i.resolved_at=NULL,i.terminal_at=NULL,i.version=i.version+1,i.updated_at=?
WHERE i.domain_schema_version IS NULL`, now)
	if err != nil {
		return fmt.Errorf("convert legacy incidents: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO incident_events (
public_id,incident_id,domain_schema_version,cycle_no,event_schema_version,event_type,actor_type,actor_id,summary,metadata_json,occurred_at,created_at)
SELECT UUID(),a.source_incident_id,3,1,1,'legacy_state_converted','system','phase7a-cutover',
'Legacy Incident state converted conservatively for V3 cutover',
JSON_OBJECT('source_status',a.source_status,'target_status',a.target_status,'reason_code',a.reason_code,'migrated_legacy',TRUE),?,?
FROM legacy_incident_state_archive a
LEFT JOIN incident_events e ON e.incident_id=a.source_incident_id AND e.event_type='legacy_state_converted' AND e.actor_id='phase7a-cutover'
WHERE e.id IS NULL`, now, now)
	if err != nil {
		return fmt.Errorf("append legacy incident conversion events: %w", err)
	}
	return nil
}

func archiveAndConvertAgentRuns(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO legacy_agent_checkpoint_archive (
source_agent_run_id,incident_id,source_status,source_schema_version,checkpoint_json,checkpoint_hash,conversion_status,reason_code,archived_at)
SELECT id,incident_id,status,checkpoint_schema_version,current_checkpoint,
SHA2(COALESCE(CAST(current_checkpoint AS CHAR),'null'),256),'cancelled','legacy_checkpoint_incompatible',?
FROM agent_runs WHERE domain_schema_version IS NULL ORDER BY id
ON DUPLICATE KEY UPDATE source_agent_run_id=VALUES(source_agent_run_id)`, now)
	if err != nil {
		return fmt.Errorf("archive legacy agent checkpoints: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE agent_runs r JOIN legacy_agent_checkpoint_archive a ON a.source_agent_run_id=r.id
SET r.status='CANCELLED',r.failure_code='legacy_checkpoint_incompatible',r.failure_summary='Archived at Phase 7A; V2 GraphState is not a V3 StateDelta checkpoint',
r.lease_owner='',r.lease_expires_at=NULL,r.heartbeat_at=NULL,r.updated_at=?
WHERE r.domain_schema_version IS NULL AND r.status IN ('PENDING','RUNNING')`, now)
	if err != nil {
		return fmt.Errorf("cancel incompatible legacy agent runs: %w", err)
	}
	return createInvestigationTasks(ctx, tx, "agent_run", "legacy_agent_checkpoint_archive", "source_agent_run_id", now)
}

func archiveAndConvertChangeRequests(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO legacy_change_request_archive (
source_change_request_id,incident_id,source_status,snapshot_json,snapshot_hash,external_state,conversion_status,reason_code,archived_at)
SELECT c.id,p.incident_id,c.status,
JSON_OBJECT('repository',c.repository,'base_revision',c.base_revision,'head_branch',c.head_branch,'commit_sha',c.commit_sha,
'pr_number',c.pr_number,'pr_url',c.pr_url,'pr_state',c.pr_state,'merged_commit_sha',c.merged_commit_sha),
SHA2(CONCAT_WS('|',c.repository,c.base_revision,c.head_branch,c.commit_sha,c.pr_number,c.pr_url,c.pr_state,c.merged_commit_sha),256),
CASE WHEN c.pr_number>0 AND c.pr_url<>'' AND (c.pr_state<>'' OR c.status IN ('pr_created','merged','delivered')) THEN 'complete-pr'
WHEN c.commit_sha<>'' OR c.head_branch<>'' THEN 'partial-write' ELSE 'no-write' END,
CASE WHEN c.pr_number>0 AND c.pr_url<>'' AND (c.pr_state<>'' OR c.status IN ('pr_created','merged','delivered')) THEN 'passed' ELSE 'failed' END,
CASE WHEN c.pr_number>0 AND c.pr_url<>'' AND (c.pr_state<>'' OR c.status IN ('pr_created','merged','delivered')) THEN 'legacy_pr_read_only_observe'
WHEN c.commit_sha<>'' OR c.head_branch<>'' THEN 'legacy_partial_external_write' ELSE 'legacy_approval_incomplete' END,?
FROM change_requests c JOIN remediation_plans p ON p.id=c.plan_id
WHERE c.domain_schema_version IS NULL ORDER BY c.id
ON DUPLICATE KEY UPDATE source_change_request_id=VALUES(source_change_request_id)`, now)
	if err != nil {
		return fmt.Errorf("archive legacy change requests: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE change_requests c JOIN legacy_change_request_archive a ON a.source_change_request_id=c.id
SET c.domain_schema_version=3,c.incident_id=a.incident_id,c.cycle_no=1,
c.v3_status=CASE WHEN a.external_state='complete-pr' AND c.merged_commit_sha<>'' THEN 'merged' WHEN a.external_state='complete-pr' THEN 'pr_open' ELSE 'superseded' END,
c.write_phase=CASE WHEN a.external_state='complete-pr' THEN 'complete' ELSE NULL END,
c.expected_subject_version=c.row_version,c.migrated_legacy=TRUE,c.lease_owner='',c.lease_expires_at=NULL,c.heartbeat_at=NULL,
c.failure_code=CASE WHEN a.external_state='complete-pr' THEN c.failure_code ELSE a.reason_code END,c.updated_at=?
WHERE c.domain_schema_version IS NULL`, now)
	if err != nil {
		return fmt.Errorf("convert legacy change requests: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO async_tasks (
public_id,incident_id,cycle_no,queue,task_type,subject_type,subject_id,transition,expected_subject_version,
payload_schema_version,payload_json,dedupe_key,logical_operation_key,migrated_legacy,status,priority,available_at,attempt,max_attempts,lease_generation,created_at,updated_at)
SELECT UUID(),a.incident_id,1,'observe','delivery.observe','change_request',c.id,'delivery.observe',c.row_version,1,
JSON_OBJECT('change_request_id',c.public_id,'phase','observe','legacy_read_only',TRUE),
SHA2(CONCAT('legacy-change-request/v1|',c.id,'|',c.row_version,'|delivery.observe'),256),
SHA2(CONCAT('legacy-delivery-observe|',c.id),256),TRUE,'ready',0,?,0,8,0,?,?
FROM legacy_change_request_archive a JOIN change_requests c ON c.id=a.source_change_request_id
LEFT JOIN async_tasks t ON t.subject_type='change_request' AND t.subject_id=c.id AND t.task_type='delivery.observe' AND t.status IN ('ready','running','succeeded')
WHERE a.external_state='complete-pr' AND t.id IS NULL`, now, now, now)
	if err != nil {
		return fmt.Errorf("anti-join legacy delivery observation tasks: %w", err)
	}
	return recordChangeConversions(ctx, tx, now)
}

func archiveAndConvertVerification(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO legacy_verification_archive (
source_verification_run_id,incident_id,source_status,profile_json,profile_hash,conversion_status,reason_code,archived_at)
SELECT id,incident_id,status,plan_json,SHA2(CAST(plan_json AS CHAR),256),'cancelled',
CASE WHEN CAST(plan_json AS CHAR) LIKE '%loki%' THEN 'legacy_loki_profile_incompatible' ELSE 'legacy_verification_profile_unproven' END,?
FROM verification_runs WHERE domain_schema_version IS NULL ORDER BY id
ON DUPLICATE KEY UPDATE source_verification_run_id=VALUES(source_verification_run_id)`, now)
	if err != nil {
		return fmt.Errorf("archive legacy verification: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE verification_runs r JOIN legacy_verification_archive a ON a.source_verification_run_id=r.id
SET r.status='cancelled',r.failure_reason=a.reason_code,r.lease_owner='',r.lease_expires_at=NULL,r.heartbeat_at=NULL,r.updated_at=?
WHERE r.domain_schema_version IS NULL AND r.status IN ('pending','running')`, now)
	if err != nil {
		return fmt.Errorf("cancel incompatible legacy verification: %w", err)
	}
	return createInvestigationTasks(ctx, tx, "verification_run", "legacy_verification_archive", "source_verification_run_id", now)
}

func createInvestigationTasks(ctx context.Context, tx *sql.Tx, subjectType, archiveTable, idColumn string, now time.Time) error {
	query := fmt.Sprintf(`INSERT INTO async_tasks (
public_id,incident_id,cycle_no,queue,task_type,subject_type,subject_id,transition,expected_subject_version,
payload_schema_version,payload_json,dedupe_key,logical_operation_key,migrated_legacy,status,priority,available_at,attempt,max_attempts,lease_generation,created_at,updated_at)
SELECT UUID(),a.incident_id,1,'investigate','investigation.advance','incident',a.incident_id,'investigation.start',i.version,1,
JSON_OBJECT('reason','%s_incompatible','legacy_subject_id',a.%s,'migrated_legacy_context',TRUE),
SHA2(CONCAT('phase7a-investigation-start/v1|',a.incident_id,'|1'),256),
SHA2(CONCAT('phase7a-investigation-start|',a.incident_id,'|1'),256),TRUE,'ready',10,?,0,8,0,?,?
FROM %s a JOIN incidents i ON i.id=a.incident_id
LEFT JOIN async_tasks t ON t.incident_id=a.incident_id AND t.cycle_no=1 AND t.task_type='investigation.advance'
 AND t.subject_type='incident' AND t.transition='investigation.start' AND t.status IN ('ready','running','succeeded')
WHERE t.id IS NULL`, subjectType, idColumn, archiveTable)
	if _, err := tx.ExecContext(ctx, query, now, now, now); err != nil {
		return fmt.Errorf("anti-join %s investigation tasks: %w", subjectType, err)
	}
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO legacy_conversion_records (
public_id,subject_type,subject_id,incident_id,cycle_no,converter_version,input_hash,output_hash,status,reason_code,target_task_id,anti_join_result,created_at)
SELECT UUID(),'%s',a.%s,a.incident_id,1,?,a.%s,
SHA2(CONCAT('investigation.start|',a.incident_id,'|1'),256),'failed',a.reason_code,t.id,
CASE WHEN t.migrated_legacy THEN 'created' ELSE 'existing-target-task' END,?
FROM %s a JOIN async_tasks t ON t.incident_id=a.incident_id AND t.cycle_no=1
 AND t.task_type='investigation.advance' AND t.subject_type='incident' AND t.transition='investigation.start'
WHERE t.status IN ('ready','running','succeeded')
ON DUPLICATE KEY UPDATE subject_id=VALUES(subject_id)`, subjectType, idColumn, hashColumnForArchive(subjectType), archiveTable), phase7AConverter, now)
	return err
}

func hashColumnForArchive(subjectType string) string {
	if subjectType == "agent_run" {
		return "checkpoint_hash"
	}
	return "profile_hash"
}

func recordChangeConversions(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO legacy_conversion_records (
public_id,subject_type,subject_id,incident_id,cycle_no,converter_version,input_hash,output_hash,status,reason_code,target_task_id,anti_join_result,created_at)
SELECT UUID(),'change_request',a.source_change_request_id,a.incident_id,1,?,a.snapshot_hash,
SHA2(CONCAT(a.external_state,'|',a.reason_code),256),a.conversion_status,a.reason_code,t.id,
CASE WHEN t.id IS NULL THEN 'not-applicable' WHEN t.migrated_legacy THEN 'created' ELSE 'existing-target-task' END,?
FROM legacy_change_request_archive a
LEFT JOIN async_tasks t ON t.subject_type='change_request' AND t.subject_id=a.source_change_request_id AND t.task_type='delivery.observe'
ON DUPLICATE KEY UPDATE subject_id=VALUES(subject_id)`, phase7AConverter, now)
	return err
}

func archivePostmortems(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO legacy_postmortem_archive (
source_postmortem_id,incident_id,source_public_id,content_json,content_hash,generated_at,archived_at)
SELECT id,incident_id,public_id,
JSON_OBJECT('title',title,'impact_summary',impact_summary,'root_cause',root_cause_json,'remediation_summary',remediation_summary_json,
'approval_summary',approval_summary_json,'delivery_revision',delivery_revision,'verification_summary',verification_summary,
'checks',checks_json,'timeline',timeline_json,'follow_up_actions',follow_up_actions_json,'generation_version',generation_version),
SHA2(CONCAT_WS('|',title,impact_summary,CAST(root_cause_json AS CHAR),CAST(remediation_summary_json AS CHAR),
CAST(approval_summary_json AS CHAR),delivery_revision,verification_summary,CAST(checks_json AS CHAR),CAST(timeline_json AS CHAR),
CAST(follow_up_actions_json AS CHAR),generation_version),256),generated_at,?
FROM postmortems ORDER BY id
ON DUPLICATE KEY UPDATE source_postmortem_id=VALUES(source_postmortem_id)`, now)
	if err != nil {
		return fmt.Errorf("archive legacy postmortems: %w", err)
	}
	return nil
}

func collectCutoverCounts(ctx context.Context, tx *sql.Tx) (map[string]uint64, error) {
	queries := map[string]string{
		"outbox_source": "SELECT COUNT(*) FROM outbox_events", "outbox_archived": "SELECT COUNT(*) FROM legacy_outbox_archive",
		"incidents_archived": "SELECT COUNT(*) FROM legacy_incident_state_archive", "agent_checkpoints_archived": "SELECT COUNT(*) FROM legacy_agent_checkpoint_archive",
		"change_requests_archived": "SELECT COUNT(*) FROM legacy_change_request_archive", "verification_archived": "SELECT COUNT(*) FROM legacy_verification_archive",
		"postmortems_archived": "SELECT COUNT(*) FROM legacy_postmortem_archive", "conversion_records": "SELECT COUNT(*) FROM legacy_conversion_records",
		"migrated_tasks": "SELECT COUNT(*) FROM async_tasks WHERE migrated_legacy=TRUE",
	}
	result := make(map[string]uint64, len(queries))
	for key, query := range queries {
		var value uint64
		if err := tx.QueryRowContext(ctx, query).Scan(&value); err != nil {
			return nil, fmt.Errorf("collect cutover count %s: %w", key, err)
		}
		result[key] = value
	}
	return result, nil
}

func insertLedger(ctx context.Context, tx *sql.Tx, request PrepareRequest, operation, stage string, sourceCount, targetCount uint64, now time.Time, summary string) (string, error) {
	publicID := uuid.NewString()
	canonical := fmt.Sprintf("%s|%s|%d|%d|%s|%s", phase7AConverter, operation, sourceCount, targetCount, request.SourceExactSHA, request.BinaryImageDigest)
	sourceHash := sha256.Sum256([]byte(canonical))
	targetHash := sha256.Sum256(append(sourceHash[:], []byte("|passed")...))
	_, err := tx.ExecContext(ctx, `INSERT INTO migration_ledger (
public_id,plan_version,stage,operation,attempt,source_schema_version,target_schema_version,source_table,target_table,batch_no,
source_count,target_count,skipped_count,rejected_count,source_hash,target_hash,converter_version,started_at,completed_at,status,bounded_summary,source_exact_sha,binary_image_digest)
VALUES (?,?,?,?,1,?,?,?, ?,0,?,?,0,0,?,?,?, ?,?,'passed',?,?,?)`, publicID, request.PlanVersion, stage, operation,
		schemaversion.Latest, schemaversion.Latest, "legacy_runtime", "v3_runtime", sourceCount, targetCount,
		hex.EncodeToString(sourceHash[:]), hex.EncodeToString(targetHash[:]), phase7AConverter, now, now, summary, request.SourceExactSHA, request.BinaryImageDigest)
	if err != nil {
		return "", fmt.Errorf("write %s ledger: %w", operation, err)
	}
	return publicID, nil
}

func databaseTime(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read database time: %w", err)
	}
	return now.UTC(), nil
}

type AuditExport struct {
	Control map[string]any    `json:"control"`
	Ledger  []map[string]any  `json:"ledger"`
	Counts  map[string]uint64 `json:"counts"`
}

// ExportAudit emits bounded metadata and hashes only. Raw payloads,
// checkpoints, postmortem narratives, and credentials never leave MySQL.
func ExportAudit(ctx context.Context, db *sql.DB, output io.Writer) error {
	if db == nil || output == nil {
		return errors.New("cutover audit export requires database and output")
	}
	var controlName, sha, digest string
	var plan uint64
	var prepared, completed sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT control_name,plan_version,source_exact_sha,binary_image_digest,prepared_at,completed_at
FROM cutover_controls WHERE control_name=?`, phase7AControlName).Scan(&controlName, &plan, &sha, &digest, &prepared, &completed); err != nil {
		return fmt.Errorf("read cutover control: %w", err)
	}
	control := map[string]any{"control_name": controlName, "plan_version": plan, "source_exact_sha": sha, "binary_image_digest": digest,
		"prepared_at": prepared.Time.UTC(), "completed_at": completed.Time.UTC()}
	rows, err := db.QueryContext(ctx, `SELECT public_id,operation,status,source_count,target_count,skipped_count,rejected_count,source_hash,target_hash,converter_version,completed_at
FROM migration_ledger WHERE plan_version=? AND operation IN (?,?,?,?) ORDER BY id`, plan, QuiesceOperation, ReconciliationOperation, ConverterAuditOperation, MarkerOperation)
	if err != nil {
		return err
	}
	defer rows.Close()
	ledger := []map[string]any{}
	for rows.Next() {
		var id, operation, status, sourceHash, targetHash, converter string
		var source, target, skipped, rejected uint64
		var at sql.NullTime
		if err := rows.Scan(&id, &operation, &status, &source, &target, &skipped, &rejected, &sourceHash, &targetHash, &converter, &at); err != nil {
			return err
		}
		ledger = append(ledger, map[string]any{"public_id": id, "operation": operation, "status": status, "source_count": source, "target_count": target,
			"skipped_count": skipped, "rejected_count": rejected, "source_hash": sourceHash, "target_hash": targetHash, "converter_version": converter, "completed_at": at.Time.UTC()})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	counts, err := collectCutoverCounts(ctx, tx)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(AuditExport{Control: control, Ledger: ledger, Counts: counts})
}
