package incidentmysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	domain "server-web/internal/incident"
	"server-web/internal/remediation"
)

type remediationPlanRow struct {
	ID                     uint64          `gorm:"column:id;primaryKey"`
	PublicID               string          `gorm:"column:public_id"`
	IncidentID             uint64          `gorm:"column:incident_id"`
	IncidentPublicID       string          `gorm:"column:incident_public_id;->"`
	PlanVersion            int             `gorm:"column:plan_version"`
	PlanHash               string          `gorm:"column:plan_hash"`
	Status                 string          `gorm:"column:status"`
	OperationType          string          `gorm:"column:operation_type"`
	TargetRepository       string          `gorm:"column:target_repository"`
	TargetBaseRevision     string          `gorm:"column:target_base_revision"`
	TargetPath             string          `gorm:"column:target_path"`
	ParametersJSON         json.RawMessage `gorm:"column:parameters_json"`
	EvidenceReferencesJSON json.RawMessage `gorm:"column:evidence_references_json"`
	RiskLevel              string          `gorm:"column:risk_level"`
	PolicySnapshotHash     string          `gorm:"column:policy_snapshot_hash"`
	ExpectedBeforeHash     string          `gorm:"column:expected_before_hash"`
	ProposedPatchHash      string          `gorm:"column:proposed_patch_hash"`
	PatchSummary           string          `gorm:"column:patch_summary"`
	RollbackPlan           string          `gorm:"column:rollback_plan"`
	ValidationPlan         string          `gorm:"column:validation_plan"`
	RowVersion             uint64          `gorm:"column:row_version"`
	CreatedAt              time.Time       `gorm:"column:created_at"`
	UpdatedAt              time.Time       `gorm:"column:updated_at"`
}

func (remediationPlanRow) TableName() string { return "remediation_plans" }

type remediationApprovalRow struct {
	ID                uint64    `gorm:"column:id;primaryKey"`
	PublicID          string    `gorm:"column:public_id"`
	PlanID            uint64    `gorm:"column:plan_id"`
	Decision          string    `gorm:"column:decision"`
	Actor             string    `gorm:"column:actor"`
	ApprovedPlanHash  string    `gorm:"column:approved_plan_hash"`
	ApprovedPatchHash string    `gorm:"column:approved_patch_hash"`
	CreatedAt         time.Time `gorm:"column:created_at"`
}

func (remediationApprovalRow) TableName() string { return "remediation_approvals" }

type changeRequestRow struct {
	ID             uint64     `gorm:"column:id;primaryKey"`
	PublicID       string     `gorm:"column:public_id"`
	PlanID         uint64     `gorm:"column:plan_id"`
	Repository     string     `gorm:"column:repository"`
	BaseRevision   string     `gorm:"column:base_revision"`
	HeadBranch     string     `gorm:"column:head_branch"`
	CommitSHA      string     `gorm:"column:commit_sha"`
	PRNumber       int64      `gorm:"column:pr_number"`
	PRURL          string     `gorm:"column:pr_url"`
	Status         string     `gorm:"column:status"`
	CIStatus       string     `gorm:"column:ci_status"`
	IdempotencyKey string     `gorm:"column:idempotency_key"`
	LeaseOwner     string     `gorm:"column:lease_owner"`
	LeaseExpiresAt *time.Time `gorm:"column:lease_expires_at"`
	HeartbeatAt    *time.Time `gorm:"column:heartbeat_at"`
	Attempts       int        `gorm:"column:attempts"`
	FailureCode    string     `gorm:"column:failure_code"`
	RowVersion     uint64     `gorm:"column:row_version"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
}

func (changeRequestRow) TableName() string { return "change_requests" }

type RemediationRepository struct{ db *gorm.DB }

var _ remediation.Repository = (*RemediationRepository)(nil)

func NewRemediationRepository(db *gorm.DB) (*RemediationRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: database required", remediation.ErrInvalidArgument)
	}
	return &RemediationRepository{db: db}, nil
}

func (r *RemediationRepository) CreatePlan(ctx context.Context, plan *remediation.RemediationPlan) error {
	if err := validatePlanForPersistence(plan); err != nil {
		return err
	}
	row, err := remediationPlanToRow(plan)
	if err != nil {
		return err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return classifyRemediationError(err)
		}
		if plan.Status == remediation.PlanPolicyRejected {
			return appendRemediationAudit(tx, plan.IncidentID, "remediation_plan_policy_rejected", "system", plan.PublicID, "Remediation plan rejected by deterministic policy", map[string]any{"plan_id": plan.PublicID, "plan_hash": plan.PlanHash, "patch_hash": plan.ProposedPatchHash})
		}
		var incident incidentRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&incident, plan.IncidentID).Error; err != nil {
			return classifyRemediationError(err)
		}
		if incident.Status == string(domain.StatusDiagnosisCompleted) {
			if err := updateIncidentStatus(tx, incident.ID, domain.StatusDiagnosisCompleted, domain.StatusPlanningRemediation); err != nil {
				return err
			}
			incident.Status = string(domain.StatusPlanningRemediation)
			if err := appendRemediationAudit(tx, plan.IncidentID, "remediation_planning_started", "system", plan.PublicID, "Evidence-backed remediation planning started", map[string]any{"plan_id": plan.PublicID}); err != nil {
				return err
			}
		}
		if incident.Status != string(domain.StatusPlanningRemediation) {
			return remediation.ErrInvalidTransition
		}
		if err := updateIncidentStatus(tx, incident.ID, domain.StatusPlanningRemediation, domain.StatusAwaitingApproval); err != nil {
			return err
		}
		return appendRemediationAudit(tx, plan.IncidentID, "remediation_plan_awaiting_approval", "system", plan.PublicID, "Remediation plan awaits human approval", map[string]any{"plan_id": plan.PublicID, "plan_hash": plan.PlanHash, "patch_hash": plan.ProposedPatchHash})
	})
	if err != nil {
		return err
	}
	plan.ID, plan.CreatedAt, plan.UpdatedAt = row.ID, row.CreatedAt, row.UpdatedAt
	return nil
}

func (r *RemediationRepository) GetPlan(ctx context.Context, publicID string) (*remediation.RemediationPlan, error) {
	var row remediationPlanRow
	if err := r.db.WithContext(ctx).Model(&remediationPlanRow{}).Select("remediation_plans.*, incidents.public_id AS incident_public_id").Joins("JOIN incidents ON incidents.id = remediation_plans.incident_id").Where("remediation_plans.public_id = ?", publicID).First(&row).Error; err != nil {
		return nil, classifyRemediationError(err)
	}
	return remediationPlanFromRow(row)
}

func (r *RemediationRepository) ListPlans(ctx context.Context, filter remediation.ListFilter) (remediation.Page, error) {
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return remediation.Page{}, remediation.ErrInvalidArgument
	}
	query := r.db.WithContext(ctx).Model(&remediationPlanRow{})
	if filter.IncidentPublicID != "" {
		query = query.Joins("JOIN incidents ON incidents.id = remediation_plans.incident_id").Where("incidents.public_id = ?", filter.IncidentPublicID)
	}
	if filter.Status != "" {
		query = query.Where("remediation_plans.status = ?", filter.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return remediation.Page{}, classifyRemediationError(err)
	}
	var rows []remediationPlanRow
	query = query.Joins("JOIN incidents plan_incidents ON plan_incidents.id = remediation_plans.incident_id")
	if err := query.Select("remediation_plans.*, plan_incidents.public_id AS incident_public_id").Order("remediation_plans.created_at DESC, remediation_plans.id DESC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&rows).Error; err != nil {
		return remediation.Page{}, classifyRemediationError(err)
	}
	items := make([]remediation.RemediationPlan, 0, len(rows))
	for _, row := range rows {
		item, err := remediationPlanFromRow(row)
		if err != nil {
			return remediation.Page{}, err
		}
		items = append(items, *item)
	}
	return remediation.Page{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (r *RemediationRepository) ApprovePlan(ctx context.Context, publicID string, expectedVersion uint64, approval remediation.Approval, delivery *remediation.ChangeRequest) (*remediation.RemediationPlan, *remediation.ChangeRequest, error) {
	if delivery == nil || approval.Decision != remediation.DecisionApproved || strings.TrimSpace(approval.Actor) == "" {
		return nil, nil, remediation.ErrInvalidArgument
	}
	var planResult *remediation.RemediationPlan
	var deliveryResult *remediation.ChangeRequest
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := lockPlan(tx, publicID)
		if err != nil {
			return err
		}
		if row.Status != string(remediation.PlanAwaitingApproval) {
			var existingApproval remediationApprovalRow
			var existingDelivery changeRequestRow
			approvalErr := tx.Where("plan_id = ?", row.ID).First(&existingApproval).Error
			deliveryErr := tx.Where("plan_id = ?", row.ID).First(&existingDelivery).Error
			if approvalErr == nil && deliveryErr == nil && existingApproval.Decision == string(remediation.DecisionApproved) && existingApproval.Actor == approval.Actor && existingApproval.ApprovedPlanHash == approval.ApprovedPlanHash && existingApproval.ApprovedPatchHash == approval.ApprovedPatchHash {
				row.IncidentPublicID = incidentPublicID(tx, row.IncidentID)
				planResult, err = remediationPlanFromRow(row)
				deliveryResult = deliveryFromRow(existingDelivery)
				return err
			}
			return remediation.ErrConflict
		}
		if row.RowVersion != expectedVersion {
			return remediation.ErrConflict
		}
		if row.PlanHash != approval.ApprovedPlanHash || row.ProposedPatchHash != approval.ApprovedPatchHash {
			return remediation.ErrApprovalMismatch
		}
		approval.PlanID = row.ID
		approvalRow := approvalToRow(approval)
		if err := tx.Create(&approvalRow).Error; err != nil {
			return classifyRemediationError(err)
		}
		delivery.PlanID = row.ID
		deliveryRow := deliveryToRow(delivery)
		if err := tx.Create(&deliveryRow).Error; err != nil {
			return classifyRemediationError(err)
		}
		result := tx.Model(&remediationPlanRow{}).Where("id = ? AND row_version = ? AND status = ?", row.ID, expectedVersion, remediation.PlanAwaitingApproval).Updates(map[string]any{"status": remediation.PlanApproved, "row_version": gorm.Expr("row_version + 1")})
		if result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return classifyRemediationError(result.Error)
			}
			return remediation.ErrConflict
		}
		result = tx.Model(&remediationPlanRow{}).Where("id = ? AND row_version = ? AND status = ?", row.ID, expectedVersion+1, remediation.PlanApproved).Updates(map[string]any{"status": remediation.PlanDeliveryPending, "row_version": gorm.Expr("row_version + 1")})
		if result.Error != nil || result.RowsAffected != 1 {
			return remediation.ErrConflict
		}
		if err := appendRemediationAudit(tx, row.IncidentID, "remediation_plan_approved", "user", approval.Actor, "Human approved exact remediation plan and patch", map[string]any{"plan_id": row.PublicID, "plan_hash": row.PlanHash, "patch_hash": row.ProposedPatchHash, "change_request_id": delivery.PublicID}); err != nil {
			return err
		}
		if err := updateIncidentStatus(tx, row.IncidentID, domain.StatusAwaitingApproval, domain.StatusApplyingChange); err != nil {
			return err
		}
		row.Status, row.RowVersion = string(remediation.PlanDeliveryPending), row.RowVersion+2
		planResult, err = remediationPlanFromRow(row)
		if err != nil {
			return err
		}
		deliveryResult = deliveryFromRow(deliveryRow)
		return nil
	})
	return planResult, deliveryResult, err
}

func (r *RemediationRepository) RejectPlan(ctx context.Context, publicID string, expectedVersion uint64, approval remediation.Approval) (*remediation.RemediationPlan, error) {
	if approval.Decision != remediation.DecisionRejected || strings.TrimSpace(approval.Actor) == "" {
		return nil, remediation.ErrInvalidArgument
	}
	var resultPlan *remediation.RemediationPlan
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := lockPlan(tx, publicID)
		if err != nil {
			return err
		}
		if row.Status == string(remediation.PlanRejected) {
			var existing remediationApprovalRow
			if tx.Where("plan_id = ?", row.ID).First(&existing).Error == nil && existing.Decision == string(remediation.DecisionRejected) && existing.Actor == approval.Actor && existing.ApprovedPlanHash == approval.ApprovedPlanHash && existing.ApprovedPatchHash == approval.ApprovedPatchHash {
				row.IncidentPublicID = incidentPublicID(tx, row.IncidentID)
				resultPlan, err = remediationPlanFromRow(row)
				return err
			}
			return remediation.ErrConflict
		}
		if row.RowVersion != expectedVersion || row.Status != string(remediation.PlanAwaitingApproval) {
			return remediation.ErrConflict
		}
		if row.PlanHash != approval.ApprovedPlanHash || row.ProposedPatchHash != approval.ApprovedPatchHash {
			return remediation.ErrApprovalMismatch
		}
		approval.PlanID = row.ID
		if err := tx.Create(ptrApprovalRow(approvalToRow(approval))).Error; err != nil {
			return classifyRemediationError(err)
		}
		res := tx.Model(&remediationPlanRow{}).Where("id = ? AND row_version = ?", row.ID, expectedVersion).Updates(map[string]any{"status": remediation.PlanRejected, "row_version": gorm.Expr("row_version + 1")})
		if res.Error != nil || res.RowsAffected != 1 {
			return remediation.ErrConflict
		}
		if err := appendRemediationAudit(tx, row.IncidentID, "remediation_plan_rejected", "user", approval.Actor, "Human rejected remediation plan", map[string]any{"plan_id": row.PublicID}); err != nil {
			return err
		}
		row.Status, row.RowVersion = string(remediation.PlanRejected), row.RowVersion+1
		resultPlan, err = remediationPlanFromRow(row)
		return err
	})
	return resultPlan, err
}

func (r *RemediationRepository) CreateDelivery(ctx context.Context, delivery *remediation.ChangeRequest) error {
	if delivery == nil || delivery.PlanID == 0 || delivery.PublicID == "" || delivery.Status != remediation.DeliveryPending || len(delivery.IdempotencyKey) != 64 {
		return remediation.ErrInvalidArgument
	}
	row := deliveryToRow(delivery)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var plan remediationPlanRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, delivery.PlanID).Error; err != nil {
			return classifyRemediationError(err)
		}
		if plan.Status != string(remediation.PlanApproved) {
			return remediation.ErrInvalidTransition
		}
		if err := tx.Create(&row).Error; err != nil {
			return classifyRemediationError(err)
		}
		result := tx.Model(&remediationPlanRow{}).Where("id = ? AND row_version = ?", plan.ID, plan.RowVersion).Updates(map[string]any{"status": remediation.PlanDeliveryPending, "row_version": gorm.Expr("row_version + 1")})
		if result.Error != nil || result.RowsAffected != 1 {
			return remediation.ErrConflict
		}
		return nil
	})
	if err == nil {
		delivery.ID, delivery.CreatedAt, delivery.UpdatedAt = row.ID, row.CreatedAt, row.UpdatedAt
	}
	return err
}

func (r *RemediationRepository) ClaimDelivery(ctx context.Context, owner string, now time.Time, lease time.Duration) (*remediation.ChangeRequest, *remediation.RemediationPlan, error) {
	if strings.TrimSpace(owner) == "" || lease <= 0 {
		return nil, nil, remediation.ErrInvalidArgument
	}
	var delivery *remediation.ChangeRequest
	var plan *remediation.RemediationPlan
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row changeRequestRow
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("(status = ?) OR (status = ? AND lease_expires_at < ?)", remediation.DeliveryPending, remediation.DeliveryDelivering, now.UTC()).Order("created_at ASC, id ASC").First(&row).Error
		if err != nil {
			return classifyRemediationError(err)
		}
		expires := now.UTC().Add(lease)
		res := tx.Model(&changeRequestRow{}).Where("id = ? AND row_version = ?", row.ID, row.RowVersion).Updates(map[string]any{"status": remediation.DeliveryDelivering, "lease_owner": owner, "lease_expires_at": expires, "heartbeat_at": now.UTC(), "attempts": gorm.Expr("attempts + 1"), "row_version": gorm.Expr("row_version + 1")})
		if res.Error != nil || res.RowsAffected != 1 {
			return remediation.ErrConflict
		}
		var planRow remediationPlanRow
		if err := tx.First(&planRow, row.PlanID).Error; err != nil {
			return classifyRemediationError(err)
		}
		var incidentRow incidentRow
		if err := tx.Select("public_id").First(&incidentRow, planRow.IncidentID).Error; err != nil {
			return classifyRemediationError(err)
		}
		planRow.IncidentPublicID = incidentRow.PublicID
		if planRow.Status != string(remediation.PlanDeliveryPending) && planRow.Status != string(remediation.PlanDelivering) {
			return remediation.ErrInvalidTransition
		}
		if err := tx.Model(&remediationPlanRow{}).Where("id = ?", planRow.ID).Updates(map[string]any{"status": remediation.PlanDelivering, "row_version": gorm.Expr("row_version + 1")}).Error; err != nil {
			return classifyRemediationError(err)
		}
		row.Status, row.LeaseOwner, row.LeaseExpiresAt, row.HeartbeatAt, row.Attempts, row.RowVersion = string(remediation.DeliveryDelivering), owner, &expires, ptrTime(now.UTC()), row.Attempts+1, row.RowVersion+1
		planRow.Status, planRow.RowVersion = string(remediation.PlanDelivering), planRow.RowVersion+1
		delivery = deliveryFromRow(row)
		plan, err = remediationPlanFromRow(planRow)
		return err
	})
	return delivery, plan, err
}

func (r *RemediationRepository) ReleaseDelivery(ctx context.Context, id, version uint64, owner, failure string) error {
	result := r.db.WithContext(ctx).Model(&changeRequestRow{}).Where("id = ? AND row_version = ? AND lease_owner = ? AND status = ?", id, version, owner, remediation.DeliveryDelivering).Updates(map[string]any{"status": remediation.DeliveryPending, "lease_owner": "", "lease_expires_at": nil, "heartbeat_at": nil, "failure_code": bounded(failure, 64), "row_version": gorm.Expr("row_version + 1")})
	if result.Error != nil {
		return classifyRemediationError(result.Error)
	}
	if result.RowsAffected != 1 {
		return remediation.ErrConflict
	}
	return nil
}

func (r *RemediationRepository) MarkPRCreated(ctx context.Context, id, version uint64, owner, commit string, number int64, url string) error {
	if commit == "" || number <= 0 || len(url) > 1024 {
		return remediation.ErrInvalidArgument
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row changeRequestRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
			return classifyRemediationError(err)
		}
		if row.RowVersion != version || row.LeaseOwner != owner || row.Status != string(remediation.DeliveryDelivering) {
			return remediation.ErrConflict
		}
		res := tx.Model(&changeRequestRow{}).Where("id = ? AND row_version = ?", id, version).Updates(map[string]any{"status": remediation.DeliveryPRCreated, "ci_status": remediation.CIPending, "commit_sha": commit, "pr_number": number, "pr_url": url, "lease_owner": "", "lease_expires_at": nil, "heartbeat_at": nil, "failure_code": "", "row_version": gorm.Expr("row_version + 1")})
		if res.Error != nil || res.RowsAffected != 1 {
			return remediation.ErrConflict
		}
		var planRow remediationPlanRow
		if err := tx.First(&planRow, row.PlanID).Error; err != nil {
			return classifyRemediationError(err)
		}
		if err := tx.Model(&remediationPlanRow{}).Where("id = ?", planRow.ID).Updates(map[string]any{"status": remediation.PlanCIPending, "row_version": gorm.Expr("row_version + 1")}).Error; err != nil {
			return classifyRemediationError(err)
		}
		return appendRemediationAudit(tx, planRow.IncidentID, "remediation_draft_pr_created", "system", "delivery-worker", "Constrained Draft Pull Request created", map[string]any{"plan_id": planRow.PublicID, "change_request_id": row.PublicID, "commit_sha": commit, "pr_number": number, "pr_url": url})
	})
}

func (r *RemediationRepository) UpdateCI(ctx context.Context, id, version uint64, status remediation.CIStatus) error {
	if status != remediation.CIPending && status != remediation.CIPassing && status != remediation.CIFailing && status != remediation.CICancelled {
		return remediation.ErrInvalidArgument
	}
	planStatus := remediation.PlanCIPending
	switch status {
	case remediation.CIPassing:
		planStatus = remediation.PlanCIPassed
	case remediation.CIFailing, remediation.CICancelled:
		planStatus = remediation.PlanCIFailed
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row changeRequestRow
		if err := tx.First(&row, id).Error; err != nil {
			return classifyRemediationError(err)
		}
		res := tx.Model(&changeRequestRow{}).Where("id = ? AND row_version = ? AND status = ?", id, version, remediation.DeliveryPRCreated).Updates(map[string]any{"ci_status": status, "row_version": gorm.Expr("row_version + 1")})
		if res.Error != nil || res.RowsAffected != 1 {
			return remediation.ErrConflict
		}
		return tx.Model(&remediationPlanRow{}).Where("id = ?", row.PlanID).Updates(map[string]any{"status": planStatus, "row_version": gorm.Expr("row_version + 1")}).Error
	})
}

func validatePlanForPersistence(plan *remediation.RemediationPlan) error {
	if plan == nil || plan.PublicID == "" || plan.IncidentID == 0 || plan.PlanVersion <= 0 || (plan.Status != remediation.PlanAwaitingApproval && plan.Status != remediation.PlanPolicyRejected) || len(plan.PlanHash) != 64 || len(plan.ProposedPatchHash) != 64 || len(plan.ExpectedBeforeHash) != 64 || len(plan.PolicySnapshotHash) != 64 || len(plan.PatchSummary) > 2048 || len(plan.RollbackPlan) > 4096 || len(plan.ValidationPlan) > 4096 {
		return remediation.ErrInvalidArgument
	}
	return nil
}

func remediationPlanToRow(plan *remediation.RemediationPlan) (remediationPlanRow, error) {
	parameters, err := json.Marshal(plan.Parameters)
	if err != nil || len(parameters) > remediation.MaxPlanJSONBytes {
		return remediationPlanRow{}, remediation.ErrInvalidArgument
	}
	evidence, err := json.Marshal(plan.EvidenceReferences)
	if err != nil || len(evidence) > remediation.MaxPlanJSONBytes {
		return remediationPlanRow{}, remediation.ErrInvalidArgument
	}
	return remediationPlanRow{ID: plan.ID, PublicID: plan.PublicID, IncidentID: plan.IncidentID, PlanVersion: plan.PlanVersion, PlanHash: plan.PlanHash, Status: string(plan.Status), OperationType: string(plan.OperationType), TargetRepository: plan.TargetRepository, TargetBaseRevision: plan.TargetBaseRevision, TargetPath: plan.TargetPath, ParametersJSON: parameters, EvidenceReferencesJSON: evidence, RiskLevel: string(plan.RiskLevel), PolicySnapshotHash: plan.PolicySnapshotHash, ExpectedBeforeHash: plan.ExpectedBeforeHash, ProposedPatchHash: plan.ProposedPatchHash, PatchSummary: plan.PatchSummary, RollbackPlan: plan.RollbackPlan, ValidationPlan: plan.ValidationPlan, RowVersion: plan.RowVersion, CreatedAt: plan.CreatedAt, UpdatedAt: plan.UpdatedAt}, nil
}

func remediationPlanFromRow(row remediationPlanRow) (*remediation.RemediationPlan, error) {
	var parameters remediation.Parameters
	var evidence []string
	if err := json.Unmarshal(row.ParametersJSON, &parameters); err != nil {
		return nil, remediation.ErrInvalidArgument
	}
	if err := json.Unmarshal(row.EvidenceReferencesJSON, &evidence); err != nil {
		return nil, remediation.ErrInvalidArgument
	}
	return &remediation.RemediationPlan{ID: row.ID, PublicID: row.PublicID, IncidentID: row.IncidentID, IncidentPublicID: row.IncidentPublicID, PlanVersion: row.PlanVersion, PlanHash: row.PlanHash, Status: remediation.PlanStatus(row.Status), OperationType: remediation.OperationType(row.OperationType), TargetRepository: row.TargetRepository, TargetBaseRevision: row.TargetBaseRevision, TargetPath: row.TargetPath, Parameters: parameters, EvidenceReferences: evidence, RiskLevel: remediation.RiskLevel(row.RiskLevel), PolicySnapshotHash: row.PolicySnapshotHash, ExpectedBeforeHash: row.ExpectedBeforeHash, ProposedPatchHash: row.ProposedPatchHash, PatchSummary: row.PatchSummary, RollbackPlan: row.RollbackPlan, ValidationPlan: row.ValidationPlan, RowVersion: row.RowVersion, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}

func approvalToRow(value remediation.Approval) remediationApprovalRow {
	return remediationApprovalRow{ID: value.ID, PublicID: value.PublicID, PlanID: value.PlanID, Decision: string(value.Decision), Actor: value.Actor, ApprovedPlanHash: value.ApprovedPlanHash, ApprovedPatchHash: value.ApprovedPatchHash, CreatedAt: value.CreatedAt}
}

func ptrApprovalRow(value remediationApprovalRow) *remediationApprovalRow { return &value }

func deliveryToRow(value *remediation.ChangeRequest) changeRequestRow {
	return changeRequestRow{ID: value.ID, PublicID: value.PublicID, PlanID: value.PlanID, Repository: value.Repository, BaseRevision: value.BaseRevision, HeadBranch: value.HeadBranch, CommitSHA: value.CommitSHA, PRNumber: value.PRNumber, PRURL: value.PRURL, Status: string(value.Status), CIStatus: string(value.CIStatus), IdempotencyKey: value.IdempotencyKey, LeaseOwner: value.LeaseOwner, LeaseExpiresAt: value.LeaseExpiresAt, HeartbeatAt: value.HeartbeatAt, Attempts: value.Attempts, FailureCode: value.FailureCode, RowVersion: value.RowVersion, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func deliveryFromRow(row changeRequestRow) *remediation.ChangeRequest {
	return &remediation.ChangeRequest{ID: row.ID, PublicID: row.PublicID, PlanID: row.PlanID, Repository: row.Repository, BaseRevision: row.BaseRevision, HeadBranch: row.HeadBranch, CommitSHA: row.CommitSHA, PRNumber: row.PRNumber, PRURL: row.PRURL, Status: remediation.ChangeRequestStatus(row.Status), CIStatus: remediation.CIStatus(row.CIStatus), IdempotencyKey: row.IdempotencyKey, LeaseOwner: row.LeaseOwner, LeaseExpiresAt: row.LeaseExpiresAt, HeartbeatAt: row.HeartbeatAt, Attempts: row.Attempts, FailureCode: row.FailureCode, RowVersion: row.RowVersion, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func lockPlan(tx *gorm.DB, publicID string) (remediationPlanRow, error) {
	var row remediationPlanRow
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", publicID).First(&row).Error; err != nil {
		return row, classifyRemediationError(err)
	}
	return row, nil
}

func incidentPublicID(tx *gorm.DB, incidentID uint64) string {
	var row incidentRow
	if tx.Select("public_id").First(&row, incidentID).Error != nil {
		return ""
	}
	return row.PublicID
}

func appendRemediationAudit(tx *gorm.DB, incidentID uint64, eventType, actorType, actorID, summary string, metadata map[string]any) error {
	payload, err := json.Marshal(metadata)
	if err != nil || len(payload) > 8*1024 || len(summary) > 2048 {
		return remediation.ErrInvalidArgument
	}
	now := time.Now().UTC()
	event := timelineRow{IncidentID: incidentID, EventType: eventType, ActorType: actorType, ActorID: bounded(actorID, 128), Summary: summary, MetadataJSON: payload, OccurredAt: now}
	if err := tx.Create(&event).Error; err != nil {
		return classifyRemediationError(err)
	}
	outboxPayload, _ := json.Marshal(map[string]any{"incident_id": incidentID, "event_type": eventType, "metadata": metadata})
	outbox := outboxRow{EventID: uuid.NewString(), AggregateType: "incident", AggregateID: fmt.Sprint(incidentID), EventType: eventType, SchemaVersion: 1, PayloadJSON: outboxPayload, OccurredAt: now, LastError: ""}
	return classifyRemediationError(tx.Create(&outbox).Error)
}

func updateIncidentStatus(tx *gorm.DB, incidentID uint64, from, to domain.Status) error {
	result := tx.Model(&incidentRow{}).Where("id = ? AND status = ?", incidentID, from).Updates(map[string]any{"status": to, "version": gorm.Expr("version + 1")})
	if result.Error != nil {
		return classifyRemediationError(result.Error)
	}
	if result.RowsAffected != 1 {
		return remediation.ErrConflict
	}
	return nil
}

func classifyRemediationError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return remediation.ErrNotFound
	}
	if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		return remediation.ErrConflict
	}
	return err
}

func bounded(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func ptrTime(value time.Time) *time.Time { return &value }
