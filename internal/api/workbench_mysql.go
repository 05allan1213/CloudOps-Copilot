package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const remediationPlansQuery = `
SELECT p.id, p.public_id, p.cycle_no, p.status, p.row_version,
       p.plan_version, p.plan_content_schema_version, p.incident_version,
       ar.public_id, p.operation_type, p.risk_level, p.patch_summary,
       p.rollback_plan, p.validation_plan, p.target_repository,
       p.target_base_branch, p.target_base_revision, p.last_known_good_sha,
       p.base_blob_sha, p.file_mode, p.target_path, p.target_field_ref,
       p.target_resource_json, p.hash_schema_version, p.diagnosis_hash,
       p.canonical_plan_hash, p.expected_before_hash,
       p.expected_post_image_hash, p.expected_tree_hash,
       p.proposed_patch_hash, p.canonical_change_manifest_json,
       p.bounded_diff, p.policy_version, p.policy_snapshot_hash,
       p.policy_snapshot_json, p.verification_plan_json,
       p.verification_plan_hash, p.evidence_bindings_json,
       p.evidence_set_hash, p.expires_at, p.created_at, p.updated_at,
       d.public_id, d.decision_schema_version, d.plan_version, d.decision,
       d.actor_provider, d.actor_login, d.actor_role, d.reason, d.request_id,
       d.request_authenticated_at, d.expires_at,
       d.approved_hash_schema_version, d.approved_plan_hash,
       d.approved_base_sha, d.approved_post_image_hash,
       d.approved_tree_hash, d.approved_patch_hash,
       d.approved_policy_hash, d.approved_verification_hash,
	       d.approved_evidence_set_hash, d.created_at, p.migrated_legacy, p.migrated_legacy_context
FROM remediation_plans p
JOIN agent_runs ar
  ON ar.id = p.created_by_agent_run_id
 AND ar.incident_id = p.incident_id
 AND ar.cycle_no = p.cycle_no
LEFT JOIN remediation_decisions d
  ON d.plan_id = p.id
 AND d.incident_id = p.incident_id
 AND d.cycle_no = p.cycle_no
WHERE %s
ORDER BY p.created_at DESC, p.id DESC
LIMIT ?`

type workbenchScanner interface {
	Scan(...any) error
}

type mysqlRemediationPlanProjection struct {
	ID                       uint64
	PublicID                 string
	Cycle                    uint64
	Status                   string
	Version                  uint64
	PlanVersion              uint64
	PlanContentSchemaVersion uint64
	IncidentVersion          uint64
	CreatedByAgentRunID      string
	OperationType            string
	RiskLevel                string
	PatchSummary             string
	RollbackPlan             string
	ValidationPlan           string
	Repository               string
	BaseBranch               string
	BaseRevision             string
	LastKnownGoodRevision    string
	BaseBlobSHA              string
	FileMode                 string
	Path                     string
	FieldRef                 string
	TargetJSON               []byte
	HashSchemaVersion        uint64
	DiagnosisHash            string
	CanonicalPlanHash        string
	ExpectedBeforeHash       string
	ExpectedPostImageHash    string
	ExpectedTreeHash         string
	ProposedPatchHash        string
	CanonicalManifestJSON    []byte
	BoundedDiff              string
	PolicyVersion            string
	PolicyHash               string
	PolicySnapshotJSON       []byte
	VerificationPlanJSON     []byte
	VerificationPlanHash     string
	EvidenceBindingsJSON     []byte
	EvidenceSetHash          string
	ExpiresAt                time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
	DecisionPublicID         sql.NullString
	DecisionSchemaVersion    sql.NullInt64
	DecisionPlanVersion      sql.NullInt64
	Decision                 sql.NullString
	ActorProvider            sql.NullString
	ActorLogin               sql.NullString
	ActorRole                sql.NullString
	DecisionReason           sql.NullString
	RequestID                sql.NullString
	RequestAuthenticatedAt   sql.NullTime
	DecisionExpiresAt        sql.NullTime
	ApprovedHashSchema       sql.NullInt64
	ApprovedPlanHash         sql.NullString
	ApprovedBaseSHA          sql.NullString
	ApprovedPostImageHash    sql.NullString
	ApprovedTreeHash         sql.NullString
	ApprovedPatchHash        sql.NullString
	ApprovedPolicyHash       sql.NullString
	ApprovedVerificationHash sql.NullString
	ApprovedEvidenceSetHash  sql.NullString
	DecisionCreatedAt        sql.NullTime
	MigratedLegacy           bool
	MigratedLegacyContext    bool
}

func (p *MySQLQueryPort) listRemediationPlans(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	incident, err := p.incidentRef(ctx, request.IncidentID)
	if err != nil {
		return QueryResponse{}, err
	}
	where := []string{
		"p.incident_id = ?", "p.cycle_no = ?",
		"p.plan_content_schema_version IS NOT NULL", "p.public_id IS NOT NULL",
	}
	args := []any{incident.ID, incident.CycleNo}
	if request.Cursor != "" {
		cursor, err := p.resourceCursor(ctx, incident, resourceQuerySpec{
			Kind: "remediation_plan", Table: "remediation_plans", SortColumn: "created_at",
		}, request.Cursor)
		if err != nil {
			return QueryResponse{}, err
		}
		where = append(where, "(p.created_at < ? OR (p.created_at = ? AND p.id < ?))")
		args = append(args, cursor.At, cursor.At, cursor.ID)
	}
	args = append(args, request.Limit+1)
	rows, err := p.db.QueryContext(ctx, fmt.Sprintf(remediationPlansQuery, strings.Join(where, " AND ")), args...)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("list remediation plan projections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	all := make([]RemediationPlanView, 0, request.Limit+1)
	for rows.Next() {
		_, item, err := scanRemediationPlanProjection(rows)
		if err != nil {
			return QueryResponse{}, fmt.Errorf("scan remediation plan projection: %w", err)
		}
		all = append(all, item)
	}
	if err := rows.Err(); err != nil {
		return QueryResponse{}, fmt.Errorf("iterate remediation plan projections: %w", err)
	}
	items := all
	next := ""
	if len(items) > request.Limit {
		items = items[:request.Limit]
		if len(items) > 0 {
			next = items[len(items)-1].ID
		}
	}
	return QueryResponse{RemediationPlans: items, NextCursor: next}, nil
}

func scanRemediationPlanProjection(scanner workbenchScanner) (uint64, RemediationPlanView, error) {
	if scanner == nil {
		return 0, RemediationPlanView{}, ErrUnavailable
	}
	var row mysqlRemediationPlanProjection
	if err := scanner.Scan(
		&row.ID, &row.PublicID, &row.Cycle, &row.Status, &row.Version,
		&row.PlanVersion, &row.PlanContentSchemaVersion, &row.IncidentVersion,
		&row.CreatedByAgentRunID, &row.OperationType, &row.RiskLevel, &row.PatchSummary,
		&row.RollbackPlan, &row.ValidationPlan, &row.Repository, &row.BaseBranch,
		&row.BaseRevision, &row.LastKnownGoodRevision, &row.BaseBlobSHA, &row.FileMode,
		&row.Path, &row.FieldRef, &row.TargetJSON, &row.HashSchemaVersion,
		&row.DiagnosisHash, &row.CanonicalPlanHash, &row.ExpectedBeforeHash,
		&row.ExpectedPostImageHash, &row.ExpectedTreeHash, &row.ProposedPatchHash,
		&row.CanonicalManifestJSON, &row.BoundedDiff, &row.PolicyVersion,
		&row.PolicyHash, &row.PolicySnapshotJSON, &row.VerificationPlanJSON,
		&row.VerificationPlanHash, &row.EvidenceBindingsJSON, &row.EvidenceSetHash,
		&row.ExpiresAt, &row.CreatedAt, &row.UpdatedAt,
		&row.DecisionPublicID, &row.DecisionSchemaVersion, &row.DecisionPlanVersion,
		&row.Decision, &row.ActorProvider, &row.ActorLogin, &row.ActorRole,
		&row.DecisionReason, &row.RequestID, &row.RequestAuthenticatedAt,
		&row.DecisionExpiresAt, &row.ApprovedHashSchema, &row.ApprovedPlanHash,
		&row.ApprovedBaseSHA, &row.ApprovedPostImageHash, &row.ApprovedTreeHash,
		&row.ApprovedPatchHash, &row.ApprovedPolicyHash, &row.ApprovedVerificationHash,
		&row.ApprovedEvidenceSetHash, &row.DecisionCreatedAt, &row.MigratedLegacy, &row.MigratedLegacyContext,
	); err != nil {
		return 0, RemediationPlanView{}, err
	}
	var target RemediationTargetResourceView
	if err := decodeWorkbenchObject(row.TargetJSON, maxWorkbenchTargetJSONBytes, &target); err != nil {
		return 0, RemediationPlanView{}, err
	}
	var evidence []EvidenceBindingView
	if err := decodeWorkbenchArray(row.EvidenceBindingsJSON, maxWorkbenchPolicyJSONBytes, &evidence); err != nil {
		return 0, RemediationPlanView{}, err
	}
	item := RemediationPlanView{
		ID: row.PublicID, Kind: "remediation_plan", Cycle: row.Cycle, Status: row.Status,
		Version: row.Version, PlanVersion: row.PlanVersion,
		PlanContentSchemaVersion: row.PlanContentSchemaVersion, IncidentVersion: row.IncidentVersion,
		CreatedByAgentRunID: row.CreatedByAgentRunID, OperationType: row.OperationType,
		RiskLevel: row.RiskLevel, PatchSummary: row.PatchSummary, RollbackPlan: row.RollbackPlan,
		ValidationPlan: row.ValidationPlan,
		Target: RemediationTargetView{
			Repository: row.Repository, BaseBranch: row.BaseBranch, BaseRevision: row.BaseRevision,
			LastKnownGoodRevision: row.LastKnownGoodRevision, BaseBlobSHA: row.BaseBlobSHA,
			FileMode: row.FileMode, Path: row.Path, FieldRef: row.FieldRef, Resource: target,
		},
		HashSchemaVersion: row.HashSchemaVersion, DiagnosisHash: row.DiagnosisHash,
		CanonicalPlanHash: row.CanonicalPlanHash, ExpectedBeforeHash: row.ExpectedBeforeHash,
		ExpectedPostImageHash: row.ExpectedPostImageHash, ExpectedTreeHash: row.ExpectedTreeHash,
		ProposedPatchHash: row.ProposedPatchHash,
		CanonicalManifest: append([]byte(nil), row.CanonicalManifestJSON...), BoundedDiff: row.BoundedDiff,
		PolicyVersion: row.PolicyVersion, PolicyHash: row.PolicyHash,
		PolicySnapshot:       append([]byte(nil), row.PolicySnapshotJSON...),
		VerificationPlan:     append([]byte(nil), row.VerificationPlanJSON...),
		VerificationPlanHash: row.VerificationPlanHash, EvidenceBindings: evidence,
		EvidenceSetHash: row.EvidenceSetHash, ExpiresAt: row.ExpiresAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		MigratedLegacy: row.MigratedLegacy, MigratedLegacyContext: row.MigratedLegacyContext,
	}
	if row.DecisionPublicID.Valid {
		if !completeDecisionProjection(row) {
			return 0, RemediationPlanView{}, fmt.Errorf("%w: incomplete remediation decision projection", ErrInvalidArgument)
		}
		item.Decision = &RemediationDecisionView{
			ID: row.DecisionPublicID.String, DecisionSchemaVersion: uint64(row.DecisionSchemaVersion.Int64),
			PlanVersion: uint64(row.DecisionPlanVersion.Int64), Decision: row.Decision.String,
			Actor: RemediationDecisionActorView{
				Provider: row.ActorProvider.String, Login: row.ActorLogin.String, Role: row.ActorRole.String,
			},
			Reason: row.DecisionReason.String, RequestID: row.RequestID.String,
			RequestAuthenticatedAt: row.RequestAuthenticatedAt.Time, ExpiresAt: row.DecisionExpiresAt.Time,
			ApprovedHashSchemaVersion: uint64(row.ApprovedHashSchema.Int64),
			ApprovedPlanHash:          row.ApprovedPlanHash.String, ApprovedBaseSHA: row.ApprovedBaseSHA.String,
			ApprovedPostImageHash: row.ApprovedPostImageHash.String, ApprovedTreeHash: row.ApprovedTreeHash.String,
			ApprovedPatchHash: row.ApprovedPatchHash.String, ApprovedPolicyHash: row.ApprovedPolicyHash.String,
			ApprovedVerificationHash: row.ApprovedVerificationHash.String,
			ApprovedEvidenceSetHash:  row.ApprovedEvidenceSetHash.String,
			CreatedAt:                row.DecisionCreatedAt.Time,
		}
	}
	if err := validateRemediationPlanView(&item); err != nil {
		return 0, RemediationPlanView{}, err
	}
	return row.ID, item, nil
}

func completeDecisionProjection(row mysqlRemediationPlanProjection) bool {
	return row.DecisionSchemaVersion.Valid && row.DecisionSchemaVersion.Int64 > 0 &&
		row.DecisionPlanVersion.Valid && row.DecisionPlanVersion.Int64 > 0 && row.Decision.Valid &&
		row.ActorProvider.Valid && row.ActorLogin.Valid && row.ActorRole.Valid && row.DecisionReason.Valid &&
		row.RequestID.Valid && row.RequestAuthenticatedAt.Valid && row.DecisionExpiresAt.Valid &&
		row.ApprovedHashSchema.Valid && row.ApprovedHashSchema.Int64 > 0 && row.ApprovedPlanHash.Valid &&
		row.ApprovedBaseSHA.Valid && row.ApprovedPostImageHash.Valid && row.ApprovedTreeHash.Valid &&
		row.ApprovedPatchHash.Valid && row.ApprovedPolicyHash.Valid && row.ApprovedVerificationHash.Valid &&
		row.ApprovedEvidenceSetHash.Valid && row.DecisionCreatedAt.Valid
}

const deliveryProjectionQuery = `
SELECT cr.public_id, cr.cycle_no, cr.status, cr.row_version, p.public_id,
       cr.repository, cr.base_revision, cr.head_branch, cr.commit_sha,
       cr.pr_number, cr.pr_url, cr.pr_state, cr.ci_status,
       cr.merged_commit_sha, cr.target_revision, cr.detected_revision,
       cr.argocd_application, cr.argocd_project, cr.argocd_sync_status,
       cr.argocd_operation_phase, cr.argocd_health_status,
       cr.resource_health_json, cr.cluster, cr.environment, cr.namespace,
       cr.workload_kind, cr.workload_name, cr.deployment_generation,
       cr.observed_generation, cr.rollout_revision, cr.desired_replicas,
       cr.updated_replicas, cr.available_replicas, cr.unavailable_replicas,
       cr.sync_started_at, cr.sync_completed_at, cr.delivery_started_at,
       cr.delivery_deadline_at, cr.delivery_completed_at, cr.last_observed_at,
	       cr.failure_code, cr.failure_reason, cr.created_at, cr.updated_at,
	       cr.migrated_legacy, cr.migrated_legacy_context
FROM change_requests cr
JOIN remediation_plans p
  ON p.id = cr.plan_id
 AND p.incident_id = cr.incident_id
 AND p.cycle_no = cr.cycle_no
WHERE cr.incident_id = ? AND cr.cycle_no = ?
  AND cr.public_id IS NOT NULL
ORDER BY cr.created_at DESC, cr.id DESC
LIMIT 1`

func (p *MySQLQueryPort) getDeliveryProjection(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	incident, err := p.incidentRef(ctx, request.IncidentID)
	if err != nil {
		return QueryResponse{}, err
	}
	item, err := scanDeliveryProjection(p.db.QueryRowContext(ctx, deliveryProjectionQuery, incident.ID, incident.CycleNo))
	if errors.Is(err, sql.ErrNoRows) {
		return QueryResponse{}, nil
	}
	if err != nil {
		return QueryResponse{}, fmt.Errorf("get delivery projection: %w", err)
	}
	return QueryResponse{Delivery: item}, nil
}

func scanDeliveryProjection(scanner workbenchScanner) (*DeliveryView, error) {
	if scanner == nil {
		return nil, ErrUnavailable
	}
	var item DeliveryView
	var resourceHealth []byte
	var syncStarted, syncCompleted, deliveryStarted, deliveryDeadline sql.NullTime
	var deliveryCompleted, lastObserved sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.Cycle, &item.Status, &item.Version, &item.RemediationPlanID,
		&item.Repository, &item.BaseRevision, &item.HeadBranch, &item.CommitSHA,
		&item.PRNumber, &item.PRURL, &item.PRState, &item.CIStatus,
		&item.MergedCommitSHA, &item.TargetRevision, &item.DetectedRevision,
		&item.ArgoApplication, &item.ArgoProject, &item.ArgoSyncStatus,
		&item.ArgoOperationPhase, &item.ArgoHealthStatus, &resourceHealth,
		&item.Cluster, &item.Environment, &item.Namespace, &item.WorkloadKind,
		&item.WorkloadName, &item.DeploymentGeneration, &item.ObservedGeneration,
		&item.RolloutRevision, &item.DesiredReplicas, &item.UpdatedReplicas,
		&item.AvailableReplicas, &item.UnavailableReplicas, &syncStarted,
		&syncCompleted, &deliveryStarted, &deliveryDeadline, &deliveryCompleted,
		&lastObserved, &item.FailureCode, &item.FailureReason,
		&item.CreatedAt, &item.UpdatedAt, &item.MigratedLegacy, &item.MigratedLegacyContext,
	); err != nil {
		return nil, err
	}
	item.Kind = "delivery"
	item.ResourceHealth = append([]byte(nil), resourceHealth...)
	item.SyncStartedAt = nullTimePointer(syncStarted)
	item.SyncCompletedAt = nullTimePointer(syncCompleted)
	item.DeliveryStartedAt = nullTimePointer(deliveryStarted)
	item.DeliveryDeadlineAt = nullTimePointer(deliveryDeadline)
	item.DeliveryCompletedAt = nullTimePointer(deliveryCompleted)
	item.LastObservedAt = nullTimePointer(lastObserved)
	if err := validateDeliveryView(&item); err != nil {
		return nil, err
	}
	return &item, nil
}

const verificationRunsQuery = `
SELECT vr.id, vr.public_id, vr.cycle_no, vr.status, vr.row_version,
       vr.trigger_type, p.public_id, cr.public_id, s.public_id, vr.attempt,
       vr.verification_profile_id, vr.verification_profile_version,
       vr.verification_profile_hash, vr.verification_contract_version,
	       vr.target_revision, vr.source_revision, vr.image_digest,
	       vr.gitops_revision, configuration.public_id, scope.public_id,
	       investigation.public_id, recovery_decision.public_id,
	       vr.started_at, vr.deadline_at, vr.completed_at,
       vr.common_stability_window_ms, vr.common_success_since,
       vr.common_window_completed_at, vr.result_summary, vr.failure_reason,
	       vr.created_at, vr.updated_at, vr.migrated_legacy, vr.migrated_legacy_context
FROM verification_runs vr
LEFT JOIN remediation_plans p
  ON p.id = vr.remediation_plan_id
 AND p.incident_id = vr.incident_id
 AND p.cycle_no = vr.cycle_no
LEFT JOIN change_requests cr
  ON cr.id = vr.change_request_id
 AND cr.incident_id = vr.incident_id
 AND cr.cycle_no = vr.cycle_no
LEFT JOIN incident_signals s
  ON s.id = vr.trigger_signal_id
 AND s.incident_id = vr.incident_id
 AND s.cycle_no = vr.cycle_no
LEFT JOIN configuration_revisions configuration
  ON configuration.id = vr.configuration_revision_id
LEFT JOIN operational_scopes scope
  ON scope.id = vr.operational_scope_id
 AND scope.configuration_revision_id = vr.configuration_revision_id
LEFT JOIN agent_runs investigation
  ON investigation.id = vr.originating_agent_run_id
 AND investigation.incident_id = vr.incident_id
 AND investigation.cycle_no = vr.cycle_no
LEFT JOIN incident_events recovery_decision
  ON recovery_decision.id = vr.decision_event_id
 AND recovery_decision.incident_id = vr.incident_id
 AND recovery_decision.cycle_no = vr.cycle_no
 AND recovery_decision.event_type = 'incident_recovery_decided'
WHERE %s
ORDER BY vr.created_at DESC, vr.id DESC
LIMIT ?`

type mysqlVerificationRunProjection struct {
	ID                   uint64
	View                 VerificationRunView
	RemediationPlanID    sql.NullString
	ChangeRequestID      sql.NullString
	TriggerSignalID      sql.NullString
	ConfigurationID      sql.NullString
	OperationalScopeID   sql.NullString
	InvestigationID      sql.NullString
	RecoveryDecisionID   sql.NullString
	TargetRevision       sql.NullString
	SourceRevision       sql.NullString
	ImageDigest          sql.NullString
	GitOpsRevision       sql.NullString
	StartedAt            sql.NullTime
	CompletedAt          sql.NullTime
	CommonSuccessSince   sql.NullTime
	CommonWindowComplete sql.NullTime
}

type verificationCheckLocation struct {
	run   int
	check int
}

func (p *MySQLQueryPort) listVerificationRuns(ctx context.Context, request QueryRequest) (QueryResponse, error) {
	incident, err := p.incidentRef(ctx, request.IncidentID)
	if err != nil {
		return QueryResponse{}, err
	}
	where := []string{
		"vr.incident_id = ?", "vr.cycle_no = ?",
		"vr.verification_contract_version IS NOT NULL", "vr.public_id IS NOT NULL",
	}
	args := []any{incident.ID, incident.CycleNo}
	if request.Cursor != "" {
		cursor, err := p.resourceCursor(ctx, incident, resourceQuerySpec{
			Kind: "verification", Table: "verification_runs", SortColumn: "created_at",
		}, request.Cursor)
		if err != nil {
			return QueryResponse{}, err
		}
		where = append(where, "(vr.created_at < ? OR (vr.created_at = ? AND vr.id < ?))")
		args = append(args, cursor.At, cursor.At, cursor.ID)
	}
	args = append(args, request.Limit+1)
	rows, err := p.db.QueryContext(ctx, fmt.Sprintf(verificationRunsQuery, strings.Join(where, " AND ")), args...)
	if err != nil {
		return QueryResponse{}, fmt.Errorf("list verification projections: %w", err)
	}
	defer func() { _ = rows.Close() }()
	all := make([]mysqlVerificationRunProjection, 0, request.Limit+1)
	for rows.Next() {
		item, err := scanVerificationRunProjection(rows)
		if err != nil {
			return QueryResponse{}, fmt.Errorf("scan verification projection: %w", err)
		}
		all = append(all, item)
	}
	if err := rows.Err(); err != nil {
		return QueryResponse{}, fmt.Errorf("iterate verification projections: %w", err)
	}
	next := ""
	if len(all) > request.Limit {
		all = all[:request.Limit]
		if len(all) > 0 {
			next = all[len(all)-1].View.ID
		}
	}
	if len(all) == 0 {
		return QueryResponse{Verifications: []VerificationRunView{}}, nil
	}
	if err := p.loadVerificationChecks(ctx, incident, all); err != nil {
		return QueryResponse{}, err
	}
	items := make([]VerificationRunView, len(all))
	for index := range all {
		all[index].View.Checks = nonNilVerificationChecks(all[index].View.Checks)
		items[index] = all[index].View
	}
	if err := validateVerificationRunViews(items); err != nil {
		return QueryResponse{}, fmt.Errorf("invalid verification projection: %w", err)
	}
	return QueryResponse{Verifications: items, NextCursor: next}, nil
}

func scanVerificationRunProjection(scanner workbenchScanner) (mysqlVerificationRunProjection, error) {
	if scanner == nil {
		return mysqlVerificationRunProjection{}, ErrUnavailable
	}
	var row mysqlVerificationRunProjection
	view := &row.View
	if err := scanner.Scan(
		&row.ID, &view.ID, &view.Cycle, &view.Status, &view.Version,
		&view.TriggerType, &row.RemediationPlanID, &row.ChangeRequestID,
		&row.TriggerSignalID, &view.Attempt, &view.Profile.ID,
		&view.Profile.Version, &view.Profile.Hash, &view.Profile.ContractVersion,
		&row.TargetRevision, &row.SourceRevision, &row.ImageDigest, &row.GitOpsRevision,
		&row.ConfigurationID, &row.OperationalScopeID, &row.InvestigationID, &row.RecoveryDecisionID,
		&row.StartedAt, &view.DeadlineAt, &row.CompletedAt,
		&view.CommonWindow.StabilityWindowMS, &row.CommonSuccessSince,
		&row.CommonWindowComplete, &view.ResultSummary, &view.FailureReason,
		&view.CreatedAt, &view.UpdatedAt, &view.MigratedLegacy, &view.MigratedLegacyContext,
	); err != nil {
		return mysqlVerificationRunProjection{}, err
	}
	view.Kind = "verification"
	view.RemediationPlanID = nullStringValue(row.RemediationPlanID)
	view.ChangeRequestID = nullStringValue(row.ChangeRequestID)
	view.TriggerSignalID = nullStringValue(row.TriggerSignalID)
	view.Revisions = VerificationRevisionsView{
		TargetRevision: nullStringValue(row.TargetRevision), SourceRevision: nullStringValue(row.SourceRevision),
		ImageDigest: nullStringValue(row.ImageDigest), GitOpsRevision: nullStringValue(row.GitOpsRevision),
	}
	if view.TriggerType == "operational_recovery" {
		view.RecoveryProvenance = &RecoveryProvenanceView{
			ConfigurationRevisionID: nullStringValue(row.ConfigurationID),
			OperationalScopeID:      nullStringValue(row.OperationalScopeID),
			InvestigationID:         nullStringValue(row.InvestigationID),
			DecisionID:              nullStringValue(row.RecoveryDecisionID),
		}
	}
	view.StartedAt = nullTimePointer(row.StartedAt)
	view.CompletedAt = nullTimePointer(row.CompletedAt)
	view.CommonWindow.SuccessSince = nullTimePointer(row.CommonSuccessSince)
	view.CommonWindow.CompletedAt = nullTimePointer(row.CommonWindowComplete)
	view.Checks = []VerificationCheckView{}
	return row, nil
}

func (p *MySQLQueryPort) loadVerificationChecks(ctx context.Context, incident mysqlIncidentRef, runs []mysqlVerificationRunProjection) error {
	runIDs := make([]uint64, len(runs))
	runIndexes := make(map[uint64]int, len(runs))
	for index := range runs {
		runIDs[index] = runs[index].ID
		runIndexes[runs[index].ID] = index
	}
	placeholders := workbenchPlaceholders(len(runIDs))
	args := []any{incident.ID, incident.CycleNo}
	for _, id := range runIDs {
		args = append(args, id)
	}
	args = append(args, maxWorkbenchChecksPerRun+1)
	rows, err := p.db.QueryContext(ctx, `
SELECT id, public_id, verification_run_id, check_spec_schema_version,
       check_type, status, required_check, profile_id, template_id,
       template_version, subject_json, expected_json, observed_json,
       comparison, threshold, source_reference, source_identity,
       lookback_ms, initial_delay_ms, stability_window_ms, timeout_ms,
       poll_interval_ms, min_samples, sample_unit, failure_mode,
       first_checked_at, last_checked_at, passed_at,
       consecutive_success_since, attempt_count, failure_reason,
	       created_at, updated_at, migrated_legacy, migrated_legacy_context, row_no
FROM (
    SELECT vc.id, vc.public_id, vc.verification_run_id,
           vc.check_spec_schema_version, vc.check_type, vc.status,
           vc.required_check, vc.profile_id, vc.template_id,
           vc.template_version, vc.subject_json, vc.expected_json,
           vc.observed_json, vc.comparison, vc.threshold,
           vc.source_reference, vc.source_identity, vc.lookback_ms,
           vc.initial_delay_ms, vc.stability_window_ms, vc.timeout_ms,
           vc.poll_interval_ms, vc.min_samples, vc.sample_unit,
           vc.failure_mode, vc.first_checked_at, vc.last_checked_at,
           vc.passed_at, vc.consecutive_success_since, vc.attempt_count,
	           vc.failure_reason, vc.created_at, vc.updated_at, vc.migrated_legacy,
	           vc.migrated_legacy_context,
           ROW_NUMBER() OVER (PARTITION BY vc.verification_run_id ORDER BY vc.id) AS row_no
    FROM verification_checks vc
    WHERE vc.incident_id = ? AND vc.cycle_no = ?
      AND vc.check_spec_schema_version IS NOT NULL
      AND vc.verification_run_id IN (`+placeholders+`)
) bounded_checks
WHERE row_no <= ?
ORDER BY verification_run_id, id`, args...)
	if err != nil {
		return fmt.Errorf("list verification check projections: %w", err)
	}
	defer func() { _ = rows.Close() }()
	checkLocations := make(map[uint64]verificationCheckLocation)
	for rows.Next() {
		internalID, runID, item, rowNo, err := scanVerificationCheckProjection(rows)
		if err != nil {
			return fmt.Errorf("scan verification check projection: %w", err)
		}
		if rowNo > maxWorkbenchChecksPerRun {
			return fmt.Errorf("%w: verification check count exceeds its bound", ErrInvalidArgument)
		}
		runIndex, ok := runIndexes[runID]
		if !ok {
			return fmt.Errorf("%w: verification check has no projected run", ErrInvalidArgument)
		}
		runs[runIndex].View.Checks = append(runs[runIndex].View.Checks, item)
		checkLocations[internalID] = verificationCheckLocation{run: runIndex, check: len(runs[runIndex].View.Checks) - 1}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate verification check projections: %w", err)
	}
	return p.loadVerificationSamples(ctx, incident, runIDs, runs, checkLocations)
}

func scanVerificationCheckProjection(scanner workbenchScanner) (uint64, uint64, VerificationCheckView, uint64, error) {
	if scanner == nil {
		return 0, 0, VerificationCheckView{}, 0, ErrUnavailable
	}
	var internalID, runID, rowNo uint64
	var item VerificationCheckView
	var subjectJSON, expectedJSON, observedJSON []byte
	var comparison sql.NullString
	var threshold sql.NullFloat64
	var firstChecked, lastChecked, passedAt, consecutiveSuccess sql.NullTime
	if err := scanner.Scan(
		&internalID, &item.ID, &runID, &item.SpecSchemaVersion, &item.Type,
		&item.Status, &item.Required, &item.ProfileID, &item.TemplateID,
		&item.TemplateVersion, &subjectJSON, &expectedJSON, &observedJSON,
		&comparison, &threshold, &item.SourceReference, &item.SourceIdentity,
		&item.LookbackMS, &item.InitialDelayMS, &item.StabilityWindowMS,
		&item.TimeoutMS, &item.PollIntervalMS, &item.MinSamples,
		&item.SampleUnit, &item.FailureMode, &firstChecked, &lastChecked,
		&passedAt, &consecutiveSuccess, &item.AttemptCount, &item.FailureReason,
		&item.CreatedAt, &item.UpdatedAt, &item.MigratedLegacy, &item.MigratedLegacyContext, &rowNo,
	); err != nil {
		return 0, 0, VerificationCheckView{}, 0, err
	}
	if err := decodeWorkbenchObject(subjectJSON, maxWorkbenchTargetJSONBytes, &item.Subject); err != nil {
		return 0, 0, VerificationCheckView{}, 0, err
	}
	item.Expected = append([]byte(nil), expectedJSON...)
	item.Observed = append([]byte(nil), observedJSON...)
	item.Comparison = nullStringValue(comparison)
	if threshold.Valid {
		value := threshold.Float64
		item.Threshold = &value
	}
	item.FirstCheckedAt = nullTimePointer(firstChecked)
	item.LastCheckedAt = nullTimePointer(lastChecked)
	item.PassedAt = nullTimePointer(passedAt)
	item.ConsecutiveSuccessSince = nullTimePointer(consecutiveSuccess)
	item.Samples = []VerificationSampleView{}
	return internalID, runID, item, rowNo, nil
}

func (p *MySQLQueryPort) loadVerificationSamples(
	ctx context.Context,
	incident mysqlIncidentRef,
	runIDs []uint64,
	runs []mysqlVerificationRunProjection,
	checkLocations map[uint64]verificationCheckLocation,
) error {
	placeholders := workbenchPlaceholders(len(runIDs))
	args := []any{incident.ID, incident.CycleNo}
	for _, id := range runIDs {
		args = append(args, id)
	}
	args = append(args, maxWorkbenchSamplesPerRun+1)
	rows, err := p.db.QueryContext(ctx, `
SELECT public_id, verification_run_id, verification_check_id,
       sample_schema_version, sample_sequence, status, observed_json,
       source_reference, reason_code, window_start_at, window_end_at,
	       sampled_at, content_hash, created_at, migrated_legacy, migrated_legacy_context, row_no
FROM (
    SELECT vs.public_id, vs.verification_run_id, vs.verification_check_id,
           vs.sample_schema_version, vs.sample_sequence, vs.status,
           vs.observed_json, vs.source_reference, vs.reason_code,
           vs.window_start_at, vs.window_end_at, vs.sampled_at,
	           vs.content_hash, vs.created_at, vs.migrated_legacy, vs.migrated_legacy_context,
           ROW_NUMBER() OVER (
               PARTITION BY vs.verification_run_id
               ORDER BY vs.verification_check_id, vs.sample_sequence, vs.id
           ) AS row_no
    FROM verification_samples vs
    WHERE vs.incident_id = ? AND vs.cycle_no = ?
      AND vs.verification_run_id IN (`+placeholders+`)
) bounded_samples
WHERE row_no <= ?
ORDER BY verification_run_id, verification_check_id, sample_sequence`, args...)
	if err != nil {
		return fmt.Errorf("list verification sample projections: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		runID, checkID, item, rowNo, err := scanVerificationSampleProjection(rows)
		if err != nil {
			return fmt.Errorf("scan verification sample projection: %w", err)
		}
		if rowNo > maxWorkbenchSamplesPerRun {
			return fmt.Errorf("%w: verification sample count exceeds its bound", ErrInvalidArgument)
		}
		location, ok := checkLocations[checkID]
		if !ok || location.run >= len(runs) || runs[location.run].ID != runID {
			return fmt.Errorf("%w: verification sample has no projected check", ErrInvalidArgument)
		}
		check := &runs[location.run].View.Checks[location.check]
		check.Samples = append(check.Samples, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate verification sample projections: %w", err)
	}
	for runIndex := range runs {
		for checkIndex := range runs[runIndex].View.Checks {
			runs[runIndex].View.Checks[checkIndex].Samples = nonNilVerificationSamples(runs[runIndex].View.Checks[checkIndex].Samples)
		}
	}
	return nil
}

func scanVerificationSampleProjection(scanner workbenchScanner) (uint64, uint64, VerificationSampleView, uint64, error) {
	if scanner == nil {
		return 0, 0, VerificationSampleView{}, 0, ErrUnavailable
	}
	var runID, checkID, rowNo uint64
	var item VerificationSampleView
	var observed []byte
	var windowStart, windowEnd sql.NullTime
	if err := scanner.Scan(
		&item.ID, &runID, &checkID, &item.SchemaVersion, &item.Sequence,
		&item.Status, &observed, &item.SourceReference, &item.ReasonCode,
		&windowStart, &windowEnd, &item.SampledAt, &item.ContentHash,
		&item.CreatedAt, &item.MigratedLegacy, &item.MigratedLegacyContext, &rowNo,
	); err != nil {
		return 0, 0, VerificationSampleView{}, 0, err
	}
	item.Observed = append([]byte(nil), observed...)
	item.WindowStartAt = nullTimePointer(windowStart)
	item.WindowEndAt = nullTimePointer(windowEnd)
	return runID, checkID, item, rowNo, nil
}

func workbenchPlaceholders(count int) string {
	if count <= 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}
