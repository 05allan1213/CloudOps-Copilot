package incidentmysql

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	domain "server-web/internal/incident"
	"server-web/internal/verification"
)

type verificationRunRow struct {
	ID                uint64          `gorm:"column:id;primaryKey"`
	PublicID          string          `gorm:"column:public_id"`
	IncidentID        uint64          `gorm:"column:incident_id"`
	RemediationPlanID uint64          `gorm:"column:remediation_plan_id"`
	ChangeRequestID   uint64          `gorm:"column:change_request_id"`
	Status            string          `gorm:"column:status"`
	TargetRevision    string          `gorm:"column:target_revision"`
	PlanJSON          json.RawMessage `gorm:"column:plan_json"`
	StartedAt         *time.Time      `gorm:"column:started_at"`
	DeadlineAt        time.Time       `gorm:"column:deadline_at"`
	CompletedAt       *time.Time      `gorm:"column:completed_at"`
	Attempt           int             `gorm:"column:attempt"`
	LeaseOwner        string          `gorm:"column:lease_owner"`
	LeaseExpiresAt    *time.Time      `gorm:"column:lease_expires_at"`
	HeartbeatAt       *time.Time      `gorm:"column:heartbeat_at"`
	RowVersion        uint64          `gorm:"column:row_version"`
	ResultSummary     string          `gorm:"column:result_summary"`
	FailureReason     string          `gorm:"column:failure_reason"`
	CreatedAt         time.Time       `gorm:"column:created_at"`
	UpdatedAt         time.Time       `gorm:"column:updated_at"`
}

func (verificationRunRow) TableName() string { return "verification_runs" }

type verificationCheckRow struct {
	ID                      uint64          `gorm:"column:id;primaryKey"`
	PublicID                string          `gorm:"column:public_id"`
	VerificationRunID       uint64          `gorm:"column:verification_run_id"`
	CheckType               string          `gorm:"column:check_type"`
	Status                  string          `gorm:"column:status"`
	RequiredCheck           bool            `gorm:"column:required_check"`
	SubjectJSON             json.RawMessage `gorm:"column:subject_json"`
	ExpectedJSON            json.RawMessage `gorm:"column:expected_json"`
	ObservedJSON            json.RawMessage `gorm:"column:observed_json"`
	SourceReference         string          `gorm:"column:source_reference"`
	LookbackMS              int64           `gorm:"column:lookback_ms"`
	StabilityWindowMS       int64           `gorm:"column:stability_window_ms"`
	TimeoutMS               int64           `gorm:"column:timeout_ms"`
	PollIntervalMS          int64           `gorm:"column:poll_interval_ms"`
	FirstCheckedAt          *time.Time      `gorm:"column:first_checked_at"`
	LastCheckedAt           *time.Time      `gorm:"column:last_checked_at"`
	PassedAt                *time.Time      `gorm:"column:passed_at"`
	ConsecutiveSuccessSince *time.Time      `gorm:"column:consecutive_success_since"`
	AttemptCount            int             `gorm:"column:attempt_count"`
	FailureReason           string          `gorm:"column:failure_reason"`
	CreatedAt               time.Time       `gorm:"column:created_at"`
	UpdatedAt               time.Time       `gorm:"column:updated_at"`
}

type postmortemRow struct {
	ID                     uint64          `gorm:"column:id;primaryKey"`
	PublicID               string          `gorm:"column:public_id"`
	IncidentID             uint64          `gorm:"column:incident_id"`
	VerificationRunID      uint64          `gorm:"column:verification_run_id"`
	Title                  string          `gorm:"column:title"`
	ImpactSummary          string          `gorm:"column:impact_summary"`
	DetectedAt             time.Time       `gorm:"column:detected_at"`
	MitigatedAt            *time.Time      `gorm:"column:mitigated_at"`
	ResolvedAt             time.Time       `gorm:"column:resolved_at"`
	DurationSeconds        int64           `gorm:"column:duration_seconds"`
	Service                string          `gorm:"column:service"`
	Workload               string          `gorm:"column:workload"`
	Environment            string          `gorm:"column:environment"`
	TriggeringSignalJSON   json.RawMessage `gorm:"column:triggering_signal_json"`
	ChangeCorrelationJSON  json.RawMessage `gorm:"column:change_correlation_json"`
	RootCauseJSON          json.RawMessage `gorm:"column:root_cause_json"`
	RemediationSummaryJSON json.RawMessage `gorm:"column:remediation_summary_json"`
	ApprovalSummaryJSON    json.RawMessage `gorm:"column:approval_summary_json"`
	DeliveryRevision       string          `gorm:"column:delivery_revision"`
	VerificationSummary    string          `gorm:"column:verification_summary"`
	ChecksJSON             json.RawMessage `gorm:"column:checks_json"`
	TimelineJSON           json.RawMessage `gorm:"column:timeline_json"`
	FollowUpActionsJSON    json.RawMessage `gorm:"column:follow_up_actions_json"`
	GeneratedAt            time.Time       `gorm:"column:generated_at"`
	GenerationVersion      int             `gorm:"column:generation_version"`
	CreatedAt              time.Time       `gorm:"column:created_at"`
	UpdatedAt              time.Time       `gorm:"column:updated_at"`
}

func (postmortemRow) TableName() string { return "postmortems" }

// phase5TimelineRow is deliberately separate from timelineRow so the Phase 1-4
// repository model continues to operate when migration 00005 is rolled back.
type phase5TimelineRow struct {
	ID             uint64 `gorm:"primaryKey"`
	IncidentID     uint64
	EventType      string
	IdempotencyKey *string
	ActorType      string
	ActorID        string
	Summary        string
	MetadataJSON   json.RawMessage `gorm:"column:metadata_json;type:json"`
	OccurredAt     time.Time
	CreatedAt      time.Time
}

func (phase5TimelineRow) TableName() string { return "incident_events" }

func (verificationCheckRow) TableName() string { return "verification_checks" }

type Phase5ChangeRequestRow struct {
	ID                   uint64          `gorm:"column:id;primaryKey"`
	PublicID             string          `gorm:"column:public_id"`
	PlanID               uint64          `gorm:"column:plan_id"`
	Repository           string          `gorm:"column:repository"`
	BaseRevision         string          `gorm:"column:base_revision"`
	HeadBranch           string          `gorm:"column:head_branch"`
	CommitSHA            string          `gorm:"column:commit_sha"`
	PRNumber             int64           `gorm:"column:pr_number"`
	PRURL                string          `gorm:"column:pr_url"`
	PRState              string          `gorm:"column:pr_state"`
	MergedCommitSHA      string          `gorm:"column:merged_commit_sha"`
	TargetRevision       string          `gorm:"column:target_revision"`
	ArgoCDApplication    string          `gorm:"column:argocd_application"`
	ArgoCDProject        string          `gorm:"column:argocd_project"`
	DetectedRevision     string          `gorm:"column:detected_revision"`
	ArgoCDSyncStatus     string          `gorm:"column:argocd_sync_status"`
	ArgoCDOperationPhase string          `gorm:"column:argocd_operation_phase"`
	ArgoCDHealthStatus   string          `gorm:"column:argocd_health_status"`
	ResourceHealthJSON   json.RawMessage `gorm:"column:resource_health_json"`
	SyncStartedAt        *time.Time      `gorm:"column:sync_started_at"`
	SyncCompletedAt      *time.Time      `gorm:"column:sync_completed_at"`
	Cluster              string          `gorm:"column:cluster"`
	Environment          string          `gorm:"column:environment"`
	Namespace            string          `gorm:"column:namespace"`
	WorkloadKind         string          `gorm:"column:workload_kind"`
	WorkloadName         string          `gorm:"column:workload_name"`
	DeploymentGeneration int64           `gorm:"column:deployment_generation"`
	ObservedGeneration   int64           `gorm:"column:observed_generation"`
	RolloutRevision      string          `gorm:"column:rollout_revision"`
	DesiredReplicas      int32           `gorm:"column:desired_replicas"`
	UpdatedReplicas      int32           `gorm:"column:updated_replicas"`
	AvailableReplicas    int32           `gorm:"column:available_replicas"`
	UnavailableReplicas  int32           `gorm:"column:unavailable_replicas"`
	DeliveryStartedAt    *time.Time      `gorm:"column:delivery_started_at"`
	DeliveryDeadlineAt   *time.Time      `gorm:"column:delivery_deadline_at"`
	DeliveryCompletedAt  *time.Time      `gorm:"column:delivery_completed_at"`
	NextPollAt           *time.Time      `gorm:"column:next_poll_at"`
	LastObservedAt       *time.Time      `gorm:"column:last_observed_at"`
	Status               string          `gorm:"column:status"`
	CIStatus             string          `gorm:"column:ci_status"`
	IdempotencyKey       string          `gorm:"column:idempotency_key"`
	LeaseOwner           string          `gorm:"column:lease_owner"`
	LeaseExpiresAt       *time.Time      `gorm:"column:lease_expires_at"`
	HeartbeatAt          *time.Time      `gorm:"column:heartbeat_at"`
	Attempts             int             `gorm:"column:attempts"`
	FailureCode          string          `gorm:"column:failure_code"`
	FailureReason        string          `gorm:"column:failure_reason"`
	RowVersion           uint64          `gorm:"column:row_version"`
	CreatedAt            time.Time       `gorm:"column:created_at"`
	UpdatedAt            time.Time       `gorm:"column:updated_at"`
}

func (Phase5ChangeRequestRow) TableName() string { return "change_requests" }

type deliveryJoinRow struct {
	Phase5ChangeRequestRow `gorm:"embedded"`
	IncidentID             uint64 `gorm:"column:incident_id"`
	IncidentPublicID       string `gorm:"column:incident_public_id"`
	IncidentFingerprint    string `gorm:"column:incident_fingerprint"`
	ServiceName            string `gorm:"column:incident_service_name"`
	IncidentCluster        string `gorm:"column:incident_cluster"`
	IncidentEnvironment    string `gorm:"column:incident_environment"`
	IncidentNamespace      string `gorm:"column:incident_namespace"`
	IncidentTargetKind     string `gorm:"column:incident_target_kind"`
	IncidentTargetName     string `gorm:"column:incident_target_name"`
	PlanPublicID           string `gorm:"column:plan_public_id"`
}

type VerificationRepository struct{ db *gorm.DB }

var _ verification.Repository = (*VerificationRepository)(nil)
var _ verification.AlertReader = (*VerificationRepository)(nil)

func NewVerificationRepository(db *gorm.DB) (*VerificationRepository, error) {
	if db == nil {
		return nil, verification.ErrInvalidArgument
	}
	return &VerificationRepository{db: db}, nil
}

func deliverySelect(tx *gorm.DB) *gorm.DB {
	return tx.Table("change_requests cr").Select("cr.*, rp.incident_id AS incident_id, rp.public_id AS plan_public_id, i.public_id AS incident_public_id, i.fingerprint AS incident_fingerprint, i.service_name AS incident_service_name, i.cluster AS incident_cluster, i.environment AS incident_environment, i.namespace AS incident_namespace, i.target_kind AS incident_target_kind, i.target_name AS incident_target_name").Joins("JOIN remediation_plans rp ON rp.id = cr.plan_id").Joins("JOIN incidents i ON i.id = rp.incident_id")
}

func (r *VerificationRepository) ClaimDelivery(ctx context.Context, owner string, now time.Time, lease, timeout time.Duration) (*verification.Delivery, error) {
	if strings.TrimSpace(owner) == "" || lease <= 0 || timeout <= 0 {
		return nil, verification.ErrInvalidArgument
	}
	var joined deliveryJoinRow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		statuses := []string{"pr_created", "ci_pending", "ci_passed", "merge_pending", "merged", "argocd_pending", "syncing", "synced", "rollout_pending"}
		query := deliverySelect(tx).Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "cr"}, Options: "SKIP LOCKED"}).Where("cr.status IN ?", statuses).Where("(cr.next_poll_at IS NULL OR cr.next_poll_at <= ?)", now.UTC()).Where("(cr.lease_expires_at IS NULL OR cr.lease_expires_at < ?)", now.UTC()).Order("cr.updated_at ASC, cr.id ASC")
		if err := query.Take(&joined).Error; err != nil {
			return mapVerificationError(err)
		}
		updates := map[string]any{"lease_owner": owner, "lease_expires_at": now.UTC().Add(lease), "heartbeat_at": now.UTC(), "attempts": gorm.Expr("attempts + 1"), "row_version": gorm.Expr("row_version + 1")}
		if joined.DeliveryStartedAt == nil {
			updates["delivery_started_at"] = now.UTC()
			updates["delivery_deadline_at"] = now.UTC().Add(timeout)
		}
		result := tx.Model(&changeRequestRow{}).Where("id = ? AND row_version = ?", joined.ID, joined.RowVersion).Updates(updates)
		if result.Error != nil || result.RowsAffected != 1 {
			return verification.ErrConflict
		}
		joined.LeaseOwner = owner
		expires := now.UTC().Add(lease)
		joined.LeaseExpiresAt, joined.HeartbeatAt = &expires, ptrTime(now.UTC())
		joined.Attempts++
		joined.RowVersion++
		if joined.DeliveryStartedAt == nil {
			started, deadline := now.UTC(), now.UTC().Add(timeout)
			joined.DeliveryStartedAt, joined.DeliveryDeadlineAt = &started, &deadline
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deliveryFromJoin(joined), nil
}

func (r *VerificationRepository) HeartbeatDelivery(ctx context.Context, id, version uint64, owner string, now time.Time, lease time.Duration) error {
	result := r.db.WithContext(ctx).Model(&changeRequestRow{}).Where("id = ? AND row_version = ? AND lease_owner = ? AND lease_expires_at >= ?", id, version, owner, now.UTC()).Updates(map[string]any{"heartbeat_at": now.UTC(), "lease_expires_at": now.UTC().Add(lease)})
	if result.Error != nil {
		return mapVerificationError(result.Error)
	}
	if result.RowsAffected != 1 {
		return verification.ErrLeaseLost
	}
	return nil
}

func (r *VerificationRepository) PersistDelivery(ctx context.Context, delivery *verification.Delivery, update verification.DeliveryUpdate) error {
	if delivery == nil || !verification.CanTransitionDelivery(delivery.Status, update.Status) || update.ObservedAt.IsZero() || len(update.FailureReason) > 128 || len(update.ResourceHealth) > 16*1024 || (len(update.ResourceHealth) > 0 && !json.Valid(update.ResourceHealth)) {
		return verification.ErrInvalidArgument
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current changeRequestRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, delivery.ID).Error; err != nil {
			return mapVerificationError(err)
		}
		if current.RowVersion != delivery.RowVersion || current.LeaseOwner != delivery.LeaseOwner || current.LeaseExpiresAt == nil || current.LeaseExpiresAt.Before(update.ObservedAt.UTC()) || current.Status != delivery.Status {
			return verification.ErrLeaseLost
		}
		updates := map[string]any{"status": update.Status, "ci_status": update.CIStatus, "pr_state": bound(update.PRState, 16), "merged_commit_sha": strings.ToLower(update.MergedCommitSHA), "target_revision": strings.ToLower(update.TargetRevision), "detected_revision": bound(update.DetectedRevision, 255), "argocd_sync_status": bound(update.ArgoSyncStatus, 32), "argocd_operation_phase": bound(update.ArgoOperationPhase, 32), "argocd_health_status": bound(update.ArgoHealthStatus, 32), "resource_health_json": update.ResourceHealth, "sync_started_at": update.SyncStartedAt, "sync_completed_at": update.SyncCompletedAt, "deployment_generation": update.DeploymentGeneration, "observed_generation": update.ObservedGeneration, "rollout_revision": bound(update.RolloutRevision, 64), "desired_replicas": update.DesiredReplicas, "updated_replicas": update.UpdatedReplicas, "available_replicas": update.AvailableReplicas, "unavailable_replicas": update.UnavailableReplicas, "next_poll_at": update.NextPollAt.UTC(), "last_observed_at": update.ObservedAt.UTC(), "failure_reason": bound(update.FailureReason, 128), "argocd_application": bound(update.ArgoApplication, 255), "argocd_project": bound(update.ArgoProject, 255), "cluster": bound(update.Cluster, 255), "environment": bound(update.Environment, 255), "namespace": bound(update.Namespace, 255), "workload_kind": bound(update.WorkloadKind, 64), "workload_name": bound(update.WorkloadName, 255), "lease_owner": "", "lease_expires_at": nil, "heartbeat_at": nil, "row_version": gorm.Expr("row_version + 1")}
		if verification.TerminalDelivery(update.Status) {
			updates["delivery_completed_at"] = update.ObservedAt.UTC()
		}
		result := tx.Model(&changeRequestRow{}).Where("id = ? AND row_version = ? AND lease_owner = ?", current.ID, current.RowVersion, current.LeaseOwner).Updates(updates)
		if result.Error != nil || result.RowsAffected != 1 {
			return verification.ErrLeaseLost
		}
		if strings.EqualFold(update.DetectedRevision, update.TargetRevision) && !strings.EqualFold(delivery.DetectedRevision, update.DetectedRevision) {
			if err := appendVerificationAudit(tx, delivery.IncidentID, delivery.IncidentPublicID, "delivery_argocd_revision_detected", delivery.PublicID+":"+strings.ToLower(update.DetectedRevision), "Argo CD detected exact merged revision", map[string]any{"change_request_id": delivery.PublicID, "revision": safeRevision(update.DetectedRevision)}, update.ObservedAt); err != nil {
				return err
			}
		}
		if update.Status != delivery.Status {
			eventType, summary := deliveryEvent(update.Status)
			if err := appendVerificationAudit(tx, delivery.IncidentID, delivery.IncidentPublicID, eventType, delivery.PublicID+":"+update.Status, summary, map[string]any{"change_request_id": delivery.PublicID, "from": delivery.Status, "to": update.Status, "revision": safeRevision(update.TargetRevision), "reason": update.FailureReason}, update.ObservedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *VerificationRepository) ReleaseDelivery(ctx context.Context, delivery *verification.Delivery, next time.Time, reason string) error {
	if delivery == nil {
		return verification.ErrInvalidArgument
	}
	result := r.db.WithContext(ctx).Model(&changeRequestRow{}).Where("id = ? AND row_version = ? AND lease_owner = ?", delivery.ID, delivery.RowVersion, delivery.LeaseOwner).Updates(map[string]any{"lease_owner": "", "lease_expires_at": nil, "heartbeat_at": nil, "next_poll_at": next.UTC(), "failure_reason": bound(reason, 128), "row_version": gorm.Expr("row_version + 1")})
	if result.Error != nil || result.RowsAffected != 1 {
		return verification.ErrLeaseLost
	}
	return nil
}

func (r *VerificationRepository) GetDeliveryByIncident(ctx context.Context, incidentPublicID string) (*verification.Delivery, error) {
	var joined deliveryJoinRow
	if err := deliverySelect(r.db.WithContext(ctx)).Where("i.public_id = ?", incidentPublicID).Order("cr.created_at DESC, cr.id DESC").Take(&joined).Error; err != nil {
		return nil, mapVerificationError(err)
	}
	return deliveryFromJoin(joined), nil
}

func (r *VerificationRepository) FindDeliveredWithoutRun(ctx context.Context) (*verification.Delivery, error) {
	var joined deliveryJoinRow
	err := deliverySelect(r.db.WithContext(ctx)).Where("cr.status = 'delivered'").Where("NOT EXISTS (SELECT 1 FROM verification_runs vr WHERE vr.change_request_id = cr.id AND vr.target_revision = cr.target_revision)").Order("cr.delivery_completed_at ASC, cr.id ASC").Take(&joined).Error
	if err != nil {
		return nil, mapVerificationError(err)
	}
	return deliveryFromJoin(joined), nil
}

func (r *VerificationRepository) CreateRun(ctx context.Context, delivery *verification.Delivery, plan verification.Plan, now time.Time) (*verification.Run, error) {
	return r.createRun(ctx, delivery, plan, now, false)
}

// CreateRetryRun is intentionally not exposed by the HTTP application. A
// future explicitly-authorized operator workflow may call it after returning
// the Incident to APPLYING_CHANGE; it always appends an attempt and never
// rewrites a terminal audit record.
func (r *VerificationRepository) CreateRetryRun(ctx context.Context, delivery *verification.Delivery, plan verification.Plan, now time.Time) (*verification.Run, error) {
	return r.createRun(ctx, delivery, plan, now, true)
}

func (r *VerificationRepository) createRun(ctx context.Context, delivery *verification.Delivery, plan verification.Plan, now time.Time, retry bool) (*verification.Run, error) {
	if delivery == nil || delivery.Status != "delivered" || plan.TargetRevision != delivery.TargetRevision || verification.ValidatePlan(plan) != nil {
		return nil, verification.ErrInvalidArgument
	}
	planJSON, _ := json.Marshal(plan)
	if len(planJSON) > 32*1024 {
		return nil, verification.ErrInvalidArgument
	}
	var result verificationRunRow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		attempt := 1
		var existing verificationRunRow
		if err := tx.Where("change_request_id = ? AND target_revision = ?", delivery.ID, delivery.TargetRevision).Order("attempt DESC").First(&existing).Error; err == nil {
			if !retry {
				result = existing
				return nil
			}
			if !verification.TerminalRun(verification.RunStatus(existing.Status)) {
				return verification.ErrConflict
			}
			attempt = existing.Attempt + 1
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return mapVerificationError(err)
		}
		deadline := now.UTC().Add(plan.Checks[0].Timeout)
		result = verificationRunRow{PublicID: uuid.NewString(), IncidentID: delivery.IncidentID, RemediationPlanID: delivery.RemediationPlanID, ChangeRequestID: delivery.ID, Status: string(verification.RunPending), TargetRevision: delivery.TargetRevision, PlanJSON: planJSON, DeadlineAt: deadline, Attempt: attempt, RowVersion: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
		if err := tx.Create(&result).Error; err != nil {
			return mapVerificationError(err)
		}
		for _, spec := range plan.Checks {
			subject, _ := json.Marshal(spec.Subject)
			row := verificationCheckRow{PublicID: uuid.NewString(), VerificationRunID: result.ID, CheckType: string(spec.Type), Status: string(verification.CheckPending), RequiredCheck: spec.Required, SubjectJSON: subject, ExpectedJSON: spec.Expected, LookbackMS: spec.Lookback.Milliseconds(), StabilityWindowMS: spec.StabilityWindow.Milliseconds(), TimeoutMS: spec.Timeout.Milliseconds(), PollIntervalMS: spec.PollInterval.Milliseconds(), CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
			if err := tx.Create(&row).Error; err != nil {
				return mapVerificationError(err)
			}
		}
		var incident incidentRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&incident, delivery.IncidentID).Error; err != nil {
			return mapVerificationError(err)
		}
		if incident.Status != string(domain.StatusApplyingChange) && incident.Status != string(domain.StatusVerifying) {
			return verification.ErrConflict
		}
		if incident.Status == string(domain.StatusApplyingChange) {
			if err := tx.Model(&incidentRow{}).Where("id = ? AND version = ?", incident.ID, incident.Version).Updates(map[string]any{"status": domain.StatusVerifying, "version": incident.Version + 1, "updated_at": now.UTC()}).Error; err != nil {
				return mapVerificationError(err)
			}
		}
		return appendVerificationAudit(tx, delivery.IncidentID, delivery.IncidentPublicID, "verification_started", result.PublicID, "Deterministic recovery verification started", map[string]any{"verification_id": result.PublicID, "change_request_id": delivery.PublicID, "revision": safeRevision(delivery.TargetRevision)}, now)
	})
	if err != nil {
		return nil, err
	}
	return runFromVerificationRow(result, delivery.IncidentPublicID), nil
}

func (r *VerificationRepository) ClaimRun(ctx context.Context, owner string, now time.Time, lease time.Duration) (*verification.Run, error) {
	if strings.TrimSpace(owner) == "" || lease <= 0 {
		return nil, verification.ErrInvalidArgument
	}
	var row verificationRunRow
	takeover := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("(status = ?) OR (status = ? AND (lease_expires_at IS NULL OR lease_expires_at < ?))", verification.RunPending, verification.RunRunning, now.UTC()).Order("created_at ASC, id ASC")
		if err := query.First(&row).Error; err != nil {
			return mapVerificationError(err)
		}
		takeover = row.Status == string(verification.RunRunning)
		updates := map[string]any{"status": verification.RunRunning, "lease_owner": owner, "lease_expires_at": now.UTC().Add(lease), "heartbeat_at": now.UTC(), "row_version": gorm.Expr("row_version + 1")}
		if row.StartedAt == nil {
			updates["started_at"] = now.UTC()
		}
		result := tx.Model(&verificationRunRow{}).Where("id = ? AND row_version = ?", row.ID, row.RowVersion).Updates(updates)
		if result.Error != nil || result.RowsAffected != 1 {
			return verification.ErrConflict
		}
		row.Status, row.LeaseOwner, row.RowVersion = string(verification.RunRunning), owner, row.RowVersion+1
		expires := now.UTC().Add(lease)
		row.LeaseExpiresAt, row.HeartbeatAt = &expires, ptrTime(now.UTC())
		if row.StartedAt == nil {
			row.StartedAt = ptrTime(now.UTC())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var incident incidentRow
	if err := r.db.WithContext(ctx).Select("public_id").First(&incident, row.IncidentID).Error; err != nil {
		return nil, mapVerificationError(err)
	}
	run := runFromVerificationRow(row, incident.PublicID)
	run.LeaseTakeover = takeover
	return run, nil
}

func (r *VerificationRepository) HeartbeatRun(ctx context.Context, id, version uint64, owner string, now time.Time, lease time.Duration) error {
	result := r.db.WithContext(ctx).Model(&verificationRunRow{}).Where("id = ? AND row_version = ? AND status = ? AND lease_owner = ? AND lease_expires_at >= ?", id, version, verification.RunRunning, owner, now.UTC()).Updates(map[string]any{"heartbeat_at": now.UTC(), "lease_expires_at": now.UTC().Add(lease)})
	if result.Error != nil || result.RowsAffected != 1 {
		return verification.ErrLeaseLost
	}
	return nil
}

func (r *VerificationRepository) ReleaseRun(ctx context.Context, run *verification.Run, now time.Time) error {
	if run == nil || run.LeaseOwner == "" || now.IsZero() {
		return verification.ErrInvalidArgument
	}
	result := r.db.WithContext(ctx).Model(&verificationRunRow{}).
		Where("id = ? AND row_version = ? AND status = ? AND lease_owner = ?", run.ID, run.RowVersion, verification.RunRunning, run.LeaseOwner).
		Updates(map[string]any{"lease_owner": "", "lease_expires_at": nil, "heartbeat_at": nil, "row_version": gorm.Expr("row_version + 1"), "updated_at": now.UTC()})
	if result.Error != nil {
		return mapVerificationError(result.Error)
	}
	if result.RowsAffected != 1 {
		return verification.ErrLeaseLost
	}
	run.RowVersion++
	run.LeaseOwner, run.LeaseExpiresAt, run.HeartbeatAt = "", nil, nil
	return nil
}

func (r *VerificationRepository) ListChecks(ctx context.Context, runID uint64) ([]verification.Check, error) {
	var rows []verificationCheckRow
	if err := r.db.WithContext(ctx).Where("verification_run_id = ?", runID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, mapVerificationError(err)
	}
	return checksFromRows(rows)
}

func (r *VerificationRepository) PersistCheckSample(ctx context.Context, run *verification.Run, check *verification.Check, sample verification.Sample, now, nextPoll time.Time) error {
	if run == nil || check == nil || check.VerificationRunID != run.ID || len(sample.Observed) > 16*1024 || (len(sample.Observed) > 0 && !json.Valid(sample.Observed)) {
		return verification.ErrInvalidArgument
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var currentRun verificationRunRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&currentRun, run.ID).Error; err != nil {
			return mapVerificationError(err)
		}
		if currentRun.RowVersion != run.RowVersion || currentRun.Status != string(verification.RunRunning) || currentRun.LeaseOwner != run.LeaseOwner || currentRun.LeaseExpiresAt == nil || currentRun.LeaseExpiresAt.Before(now.UTC()) {
			return verification.ErrLeaseLost
		}
		var row verificationCheckRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, check.ID).Error; err != nil {
			return mapVerificationError(err)
		}
		current, err := checkFromRow(row)
		if err != nil {
			return err
		}
		before := current.Status
		if err := verification.ApplySample(&current, sample, now); err != nil {
			return err
		}
		result := tx.Model(&verificationCheckRow{}).Where("id = ?", row.ID).Updates(map[string]any{"status": current.Status, "observed_json": current.Observed, "source_reference": current.SourceReference, "first_checked_at": current.FirstCheckedAt, "last_checked_at": current.LastCheckedAt, "passed_at": current.PassedAt, "consecutive_success_since": current.ConsecutiveSuccessSince, "attempt_count": current.AttemptCount, "failure_reason": current.FailureReason})
		if result.Error != nil || result.RowsAffected != 1 {
			return verification.ErrConflict
		}
		result = tx.Model(&verificationRunRow{}).Where("id = ? AND row_version = ? AND lease_owner = ?", run.ID, run.RowVersion, run.LeaseOwner).Updates(map[string]any{"lease_owner": "", "lease_expires_at": nil, "heartbeat_at": nil, "row_version": gorm.Expr("row_version + 1"), "updated_at": now.UTC()})
		if result.Error != nil || result.RowsAffected != 1 {
			return verification.ErrLeaseLost
		}
		run.RowVersion++
		*check = current
		if before != current.Status && (current.Status == verification.CheckPassed || current.Status == verification.CheckFailed || current.Status == verification.CheckTimedOut || current.Status == verification.CheckInvalid) {
			eventType := "verification_check_passed"
			if current.Status != verification.CheckPassed {
				eventType = "verification_check_failed"
			}
			return appendVerificationAudit(tx, run.IncidentID, run.IncidentPublicID, eventType, current.PublicID+":"+string(current.Status), "Deterministic verification check "+string(current.Status), map[string]any{"verification_id": run.PublicID, "check_id": current.PublicID, "check_type": current.Type, "status": current.Status, "reason": current.FailureReason, "next_poll_at": nextPoll.UTC()}, now)
		}
		return nil
	})
}

func (r *VerificationRepository) AggregateRun(ctx context.Context, run *verification.Run, now time.Time) (*verification.Run, error) {
	if run == nil {
		return nil, verification.ErrInvalidArgument
	}
	var result verificationRunRow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&result, run.ID).Error; err != nil {
			return mapVerificationError(err)
		}
		if result.RowVersion != run.RowVersion || result.Status != string(verification.RunRunning) || result.LeaseOwner != run.LeaseOwner || result.LeaseExpiresAt == nil || result.LeaseExpiresAt.Before(now.UTC()) {
			return verification.ErrLeaseLost
		}
		var rows []verificationCheckRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("verification_run_id = ?", run.ID).Order("id ASC").Find(&rows).Error; err != nil {
			return mapVerificationError(err)
		}
		checks, err := checksFromRows(rows)
		if err != nil {
			return err
		}
		status, reason, terminal := verification.Aggregate(checks)
		if !terminal {
			return verification.ErrConflict
		}
		completed := now.UTC()
		updates := map[string]any{"status": status, "result_summary": reason, "failure_reason": "", "completed_at": completed, "lease_owner": "", "lease_expires_at": nil, "heartbeat_at": nil, "row_version": gorm.Expr("row_version + 1")}
		if status != verification.RunPassed {
			updates["failure_reason"] = reason
		}
		res := tx.Model(&verificationRunRow{}).Where("id = ? AND row_version = ?", result.ID, result.RowVersion).Updates(updates)
		if res.Error != nil || res.RowsAffected != 1 {
			return verification.ErrLeaseLost
		}
		var incident incidentRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&incident, result.IncidentID).Error; err != nil {
			return mapVerificationError(err)
		}
		to := domain.StatusDiagnosing
		if status == verification.RunPassed {
			to = domain.StatusResolved
		}
		if incident.Status != string(domain.StatusVerifying) {
			return verification.ErrConflict
		}
		incidentUpdates := map[string]any{"status": to, "version": incident.Version + 1, "updated_at": completed}
		if to == domain.StatusResolved {
			incidentUpdates["resolved_at"] = completed
		} else {
			incidentUpdates["resolved_at"] = nil
		}
		if err := tx.Model(&incidentRow{}).Where("id = ? AND version = ?", incident.ID, incident.Version).Updates(incidentUpdates).Error; err != nil {
			return mapVerificationError(err)
		}
		eventType, summary := "verification_failed", "Verification failed; Incident returned to investigation"
		if status == verification.RunPassed {
			eventType, summary = "verification_passed", "Verification passed; Incident resolved"
		}
		if err := appendVerificationAudit(tx, result.IncidentID, run.IncidentPublicID, eventType, result.PublicID+":"+string(status), summary, map[string]any{"verification_id": result.PublicID, "status": status, "reason": reason, "revision": safeRevision(result.TargetRevision)}, completed); err != nil {
			return err
		}
		incidentEvent := "incident_returned_to_investigation"
		if status == verification.RunPassed {
			incidentEvent = "incident_resolved_after_verification"
		}
		if err := appendVerificationAudit(tx, result.IncidentID, run.IncidentPublicID, incidentEvent, result.PublicID+":"+incidentEvent, summary, map[string]any{"verification_id": result.PublicID, "from": domain.StatusVerifying, "to": to}, completed); err != nil {
			return err
		}
		if status == verification.RunPassed {
			if err := generatePostmortem(tx, result, incident, checks, run.IncidentPublicID, completed); err != nil {
				return err
			}
		}
		result.Status, result.ResultSummary, result.CompletedAt, result.RowVersion = string(status), reason, &completed, result.RowVersion+1
		result.LeaseOwner, result.LeaseExpiresAt = "", nil
		if status != verification.RunPassed {
			result.FailureReason = reason
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return runFromVerificationRow(result, run.IncidentPublicID), nil
}

func generatePostmortem(tx *gorm.DB, run verificationRunRow, incident incidentRow, checks []verification.Check, incidentPublicID string, at time.Time) error {
	_, span := otel.Tracer("server-web/incidentmysql").Start(tx.Statement.Context, "postmortem.generate")
	defer span.End()
	// Keep a Phase 5 binary/schema rollback readable. Phase 6 startup/migration
	// validation requires this table before observability profiles are enabled.
	if !tx.Migrator().HasTable(&postmortemRow{}) {
		return nil
	}
	var signal signalRow
	_ = tx.Where("incident_id = ?", incident.ID).Order("occurred_at ASC, id ASC").First(&signal).Error
	var change changeRow
	_ = tx.Where("incident_id = ?", incident.ID).Order("correlation_score DESC, id DESC").First(&change).Error
	var evidenceIDs []string
	if err := tx.Model(&evidenceRow{}).Where("incident_id = ? AND valid = ?", incident.ID, true).Order("id ASC").Limit(50).Pluck("public_id", &evidenceIDs).Error; err != nil {
		return mapVerificationError(err)
	}
	rootCause := verification.ClassifiedFact{Classification: "unknown", Summary: "Root cause was not deterministically confirmed by persisted evidence.", EvidenceIDs: evidenceIDs}
	var diagnosisRow struct {
		FinalDiagnosis json.RawMessage `gorm:"column:final_diagnosis"`
	}
	if err := tx.Table("agent_runs").Select("final_diagnosis").Where("incident_id = ? AND status = 'COMPLETED'", incident.ID).Order("completed_at DESC, id DESC").Take(&diagnosisRow).Error; err == nil && len(diagnosisRow.FinalDiagnosis) > 0 {
		rootCause = decodePostmortemRootCause(diagnosisRow.FinalDiagnosis, evidenceIDs)
	}
	checkFacts := make([]verification.PostmortemCheckFact, 0, len(checks))
	for _, check := range checks {
		checkFacts = append(checkFacts, verification.PostmortemCheckFact{CheckID: check.PublicID, Type: check.Type, Status: check.Status, Required: check.Required, TemplateID: check.TemplateID, Reason: check.FailureReason})
	}
	var eventRows []phase5TimelineRow
	if err := tx.Where("incident_id = ?", incident.ID).Order("occurred_at ASC, id ASC").Limit(100).Find(&eventRows).Error; err != nil {
		return mapVerificationError(err)
	}
	timeline := make([]verification.TimelineFact, 0, len(eventRows))
	for _, event := range eventRows {
		timeline = append(timeline, verification.TimelineFact{EventType: event.EventType, Summary: bound(event.Summary, 512), OccurredAt: event.OccurredAt.UTC()})
	}
	trigger := verification.ClassifiedFact{Classification: "fact", Summary: bound(signal.Summary, 2048)}
	changeFact := verification.ClassifiedFact{Classification: "unknown", Summary: "No confirmed triggering change was persisted."}
	if change.ID != 0 {
		changeFact = verification.ClassifiedFact{Classification: "fact", Summary: bound(change.ChangeSummary, 2048), EvidenceIDs: evidenceIDs}
	}
	remediation := verification.ClassifiedFact{Classification: "fact", Summary: "Approved remediation was delivered at exact revision " + safeRevision(run.TargetRevision), EvidenceIDs: evidenceIDs}
	approval := verification.ClassifiedFact{Classification: "fact", Summary: "Human approval and delivery records are bound to the persisted remediation attempt."}
	triggerJSON, _ := json.Marshal(trigger)
	changeJSON, _ := json.Marshal(changeFact)
	rootJSON, _ := json.Marshal(rootCause)
	remediationJSON, _ := json.Marshal(remediation)
	approvalJSON, _ := json.Marshal(approval)
	checksJSON, _ := json.Marshal(checkFacts)
	timelineJSON, _ := json.Marshal(timeline)
	followupsJSON, _ := json.Marshal([]string{"Complete credentialed staging verification before production enablement."})
	row := postmortemRow{PublicID: uuid.NewString(), IncidentID: incident.ID, VerificationRunID: run.ID, Title: bound("Incident "+incidentPublicID+" postmortem", 512), ImpactSummary: bound(incident.Summary, 2048), DetectedAt: incident.FirstSeenAt.UTC(), MitigatedAt: ptrTime(at.UTC()), ResolvedAt: at.UTC(), DurationSeconds: max(0, int64(at.Sub(incident.FirstSeenAt).Seconds())), Service: bound(incident.ServiceName, 255), Workload: bound(incident.TargetName, 255), Environment: bound(incident.Environment, 255), TriggeringSignalJSON: triggerJSON, ChangeCorrelationJSON: changeJSON, RootCauseJSON: rootJSON, RemediationSummaryJSON: remediationJSON, ApprovalSummaryJSON: approvalJSON, DeliveryRevision: run.TargetRevision, VerificationSummary: "All required deterministic checks satisfied their stability windows.", ChecksJSON: checksJSON, TimelineJSON: timelineJSON, FollowUpActionsJSON: followupsJSON, GeneratedAt: at.UTC(), GenerationVersion: 1, CreatedAt: at.UTC(), UpdatedAt: at.UTC()}
	var existing postmortemRow
	if err := tx.Where("incident_id = ?", incident.ID).First(&existing).Error; err == nil {
		row.ID, row.PublicID, row.CreatedAt = existing.ID, existing.PublicID, existing.CreatedAt
		return mapVerificationError(tx.Save(&row).Error)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return mapVerificationError(err)
	}
	return mapVerificationError(tx.Create(&row).Error)
}

func decodePostmortemRootCause(raw json.RawMessage, fallbackEvidence []string) verification.ClassifiedFact {
	unknown := func(summary string) verification.ClassifiedFact {
		return verification.ClassifiedFact{Classification: "unknown", Summary: summary, EvidenceIDs: dedupePostmortemEvidence(fallbackEvidence)}
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return unknown("No persisted confirmed facts were available for Postmortem root-cause classification.")
	}
	var envelope struct {
		ConfirmedFacts json.RawMessage `json:"confirmed_facts"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return unknown("Persisted final diagnosis JSON was malformed; Postmortem root-cause information is degraded.")
	}
	facts := strings.TrimSpace(string(envelope.ConfirmedFacts))
	if facts == "" || facts == "null" || facts == "[]" {
		return unknown("The persisted diagnosis contained no confirmed facts; Postmortem root cause remains unknown.")
	}
	type persistedClaim struct {
		Statement   string   `json:"statement"`
		EvidenceIDs []string `json:"evidence_ids"`
	}
	var claims []persistedClaim
	if err := json.Unmarshal(envelope.ConfirmedFacts, &claims); err == nil {
		statements := make([]string, 0, len(claims))
		evidence := make([]string, 0)
		for _, claim := range claims {
			if statement := strings.TrimSpace(claim.Statement); statement != "" {
				statements = append(statements, statement)
				evidence = append(evidence, claim.EvidenceIDs...)
			}
		}
		if len(statements) == 0 {
			return unknown("Persisted structured confirmed facts contained no usable statements; Postmortem root-cause information is degraded.")
		}
		if len(evidence) == 0 {
			evidence = fallbackEvidence
		}
		return verification.ClassifiedFact{Classification: "fact", Summary: bound(strings.Join(statements, "; "), 2048), EvidenceIDs: dedupePostmortemEvidence(evidence)}
	}
	var historical []string
	if err := json.Unmarshal(envelope.ConfirmedFacts, &historical); err == nil {
		statements := make([]string, 0, len(historical))
		for _, statement := range historical {
			if statement = strings.TrimSpace(statement); statement != "" {
				statements = append(statements, statement)
			}
		}
		if len(statements) == 0 {
			return unknown("Persisted historical confirmed facts contained no usable statements; Postmortem root-cause information is degraded.")
		}
		return verification.ClassifiedFact{Classification: "fact", Summary: bound(strings.Join(statements, "; "), 2048), EvidenceIDs: dedupePostmortemEvidence(fallbackEvidence)}
	}
	return unknown("Persisted confirmed_facts could not be decoded as structured claims or historical strings; Postmortem root-cause information is degraded.")
}

func dedupePostmortemEvidence(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (r *VerificationRepository) GetPostmortem(ctx context.Context, incidentPublicID string) (*verification.Postmortem, error) {
	var row postmortemRow
	if err := r.db.WithContext(ctx).Table("postmortems p").Select("p.*").Joins("JOIN incidents i ON i.id = p.incident_id").Where("i.public_id = ?", incidentPublicID).First(&row).Error; err != nil {
		return nil, mapVerificationError(err)
	}
	var incident incidentRow
	if err := r.db.WithContext(ctx).Select("public_id").First(&incident, row.IncidentID).Error; err != nil {
		return nil, mapVerificationError(err)
	}
	var run verificationRunRow
	if err := r.db.WithContext(ctx).Select("public_id").First(&run, row.VerificationRunID).Error; err != nil {
		return nil, mapVerificationError(err)
	}
	result := &verification.Postmortem{PublicID: row.PublicID, IncidentPublicID: incident.PublicID, VerificationRunPublicID: run.PublicID, Title: row.Title, ImpactSummary: row.ImpactSummary, DetectedAt: row.DetectedAt.UTC(), MitigatedAt: row.MitigatedAt, ResolvedAt: row.ResolvedAt.UTC(), DurationSeconds: row.DurationSeconds, Service: row.Service, Workload: row.Workload, Environment: row.Environment, DeliveryRevision: row.DeliveryRevision, VerificationSummary: row.VerificationSummary, GeneratedAt: row.GeneratedAt.UTC(), GenerationVersion: row.GenerationVersion, CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC()}
	if json.Unmarshal(row.TriggeringSignalJSON, &result.TriggeringSignal) != nil || json.Unmarshal(row.ChangeCorrelationJSON, &result.ChangeCorrelation) != nil || json.Unmarshal(row.RootCauseJSON, &result.RootCause) != nil || json.Unmarshal(row.RemediationSummaryJSON, &result.RemediationSummary) != nil || json.Unmarshal(row.ApprovalSummaryJSON, &result.ApprovalSummary) != nil || json.Unmarshal(row.ChecksJSON, &result.Checks) != nil || json.Unmarshal(row.TimelineJSON, &result.Timeline) != nil || json.Unmarshal(row.FollowUpActionsJSON, &result.FollowUpActions) != nil {
		return nil, verification.ErrInvalidArgument
	}
	return result, nil
}

func (r *VerificationRepository) TimeoutRun(ctx context.Context, run *verification.Run, now time.Time) error {
	if run == nil {
		return verification.ErrInvalidArgument
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&verificationRunRow{}).Where("id = ? AND row_version = ? AND status = ? AND lease_owner = ?", run.ID, run.RowVersion, verification.RunRunning, run.LeaseOwner).Updates(map[string]any{"status": verification.RunTimedOut, "failure_reason": "verification_timeout", "result_summary": "verification deadline exceeded", "completed_at": now.UTC(), "lease_owner": "", "lease_expires_at": nil, "heartbeat_at": nil, "row_version": gorm.Expr("row_version + 1")})
		if res.Error != nil || res.RowsAffected != 1 {
			return verification.ErrLeaseLost
		}
		var incident incidentRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&incident, run.IncidentID).Error; err != nil {
			return mapVerificationError(err)
		}
		if incident.Status != string(domain.StatusVerifying) {
			return verification.ErrConflict
		}
		if err := tx.Model(&incidentRow{}).Where("id = ? AND version = ?", incident.ID, incident.Version).Updates(map[string]any{"status": domain.StatusDiagnosing, "resolved_at": nil, "version": incident.Version + 1, "updated_at": now.UTC()}).Error; err != nil {
			return mapVerificationError(err)
		}
		if err := appendVerificationAudit(tx, run.IncidentID, run.IncidentPublicID, "verification_timed_out", run.PublicID+":timed_out", "Verification timed out", map[string]any{"verification_id": run.PublicID, "reason": "verification_timeout"}, now); err != nil {
			return err
		}
		return appendVerificationAudit(tx, run.IncidentID, run.IncidentPublicID, "incident_returned_to_investigation", run.PublicID+":timeout:reinvestigate", "Verification timed out; Incident returned to investigation", map[string]any{"verification_id": run.PublicID, "from": domain.StatusVerifying, "to": domain.StatusDiagnosing}, now)
	})
}

func (r *VerificationRepository) GetRun(ctx context.Context, incidentPublicID, runPublicID string) (*verification.Run, error) {
	var row verificationRunRow
	if err := r.db.WithContext(ctx).Table("verification_runs vr").Select("vr.*").Joins("JOIN incidents i ON i.id = vr.incident_id").Where("i.public_id = ? AND vr.public_id = ?", incidentPublicID, runPublicID).First(&row).Error; err != nil {
		return nil, mapVerificationError(err)
	}
	return runFromVerificationRow(row, incidentPublicID), nil
}

func (r *VerificationRepository) ListRuns(ctx context.Context, incidentPublicID string, page, pageSize int) (verification.RunPage, error) {
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return verification.RunPage{}, verification.ErrInvalidArgument
	}
	query := r.db.WithContext(ctx).Table("verification_runs vr").Joins("JOIN incidents i ON i.id = vr.incident_id").Where("i.public_id = ?", incidentPublicID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return verification.RunPage{}, mapVerificationError(err)
	}
	var rows []verificationRunRow
	if err := query.Select("vr.*").Order("vr.created_at DESC, vr.id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return verification.RunPage{}, mapVerificationError(err)
	}
	items := make([]verification.Run, 0, len(rows))
	for _, row := range rows {
		items = append(items, *runFromVerificationRow(row, incidentPublicID))
	}
	return verification.RunPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *VerificationRepository) ListRunChecks(ctx context.Context, incidentPublicID, runPublicID string) ([]verification.Check, error) {
	run, err := r.GetRun(ctx, incidentPublicID, runPublicID)
	if err != nil {
		return nil, err
	}
	return r.ListChecks(ctx, run.ID)
}

func (r *VerificationRepository) ResolvedSignal(ctx context.Context, incidentID uint64, fingerprint string, since time.Time) (bool, time.Time, error) {
	var row signalRow
	err := r.db.WithContext(ctx).Where("incident_id = ? AND fingerprint = ? AND occurred_at >= ?", incidentID, fingerprint, since.UTC()).Order("occurred_at DESC, id DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, mapVerificationError(err)
	}
	return row.Status == string(domain.SignalStatusResolved), row.OccurredAt.UTC(), nil
}

func deliveryFromJoin(row deliveryJoinRow) *verification.Delivery {
	cluster, environment, namespace, kind, name := row.Cluster, row.Environment, row.Namespace, row.WorkloadKind, row.WorkloadName
	if cluster == "" {
		cluster = row.IncidentCluster
	}
	if environment == "" {
		environment = row.IncidentEnvironment
	}
	if namespace == "" {
		namespace = row.IncidentNamespace
	}
	if kind == "" {
		kind = row.IncidentTargetKind
	}
	if name == "" {
		name = row.IncidentTargetName
	}
	if strings.EqualFold(kind, "deployment") {
		kind = "Deployment"
	}
	return &verification.Delivery{ID: row.ID, PublicID: row.PublicID, IncidentID: row.IncidentID, IncidentPublicID: row.IncidentPublicID, IncidentFingerprint: row.IncidentFingerprint, ServiceName: row.ServiceName, RemediationPlanID: row.PlanID, RemediationPlanPublicID: row.PlanPublicID, Repository: row.Repository, PRNumber: row.PRNumber, PRURL: row.PRURL, HeadBranch: row.HeadBranch, HeadCommitSHA: row.CommitSHA, Status: row.Status, CIStatus: row.CIStatus, PRState: row.PRState, MergedCommitSHA: row.MergedCommitSHA, TargetRevision: row.TargetRevision, ArgoApplication: row.ArgoCDApplication, ArgoProject: row.ArgoCDProject, DetectedRevision: row.DetectedRevision, ArgoSyncStatus: row.ArgoCDSyncStatus, ArgoOperationPhase: row.ArgoCDOperationPhase, ArgoHealthStatus: row.ArgoCDHealthStatus, ResourceHealth: row.ResourceHealthJSON, SyncStartedAt: row.SyncStartedAt, SyncCompletedAt: row.SyncCompletedAt, Cluster: cluster, Environment: environment, Namespace: namespace, WorkloadKind: kind, WorkloadName: name, DeploymentGeneration: row.DeploymentGeneration, ObservedGeneration: row.ObservedGeneration, RolloutRevision: row.RolloutRevision, DesiredReplicas: row.DesiredReplicas, UpdatedReplicas: row.UpdatedReplicas, AvailableReplicas: row.AvailableReplicas, UnavailableReplicas: row.UnavailableReplicas, DeliveryStartedAt: row.DeliveryStartedAt, DeliveryDeadlineAt: row.DeliveryDeadlineAt, DeliveryCompletedAt: row.DeliveryCompletedAt, NextPollAt: row.NextPollAt, LastObservedAt: row.LastObservedAt, LeaseOwner: row.LeaseOwner, LeaseExpiresAt: row.LeaseExpiresAt, Attempt: row.Attempts, FailureReason: row.FailureReason, RowVersion: row.RowVersion, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func runFromVerificationRow(row verificationRunRow, incidentPublicID string) *verification.Run {
	var plan verification.Plan
	_ = json.Unmarshal(row.PlanJSON, &plan)
	return &verification.Run{ID: row.ID, PublicID: row.PublicID, IncidentID: row.IncidentID, IncidentPublicID: incidentPublicID, RemediationPlanID: row.RemediationPlanID, ChangeRequestID: row.ChangeRequestID, Status: verification.RunStatus(row.Status), TargetRevision: row.TargetRevision, Plan: plan, StartedAt: row.StartedAt, DeadlineAt: row.DeadlineAt, CompletedAt: row.CompletedAt, Attempt: row.Attempt, LeaseOwner: row.LeaseOwner, LeaseExpiresAt: row.LeaseExpiresAt, HeartbeatAt: row.HeartbeatAt, RowVersion: row.RowVersion, ResultSummary: row.ResultSummary, FailureReason: row.FailureReason, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func checksFromRows(rows []verificationCheckRow) ([]verification.Check, error) {
	result := make([]verification.Check, 0, len(rows))
	for _, row := range rows {
		item, err := checkFromRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func checkFromRow(row verificationCheckRow) (verification.Check, error) {
	var subject verification.Subject
	if json.Unmarshal(row.SubjectJSON, &subject) != nil || !json.Valid(row.ExpectedJSON) {
		return verification.Check{}, verification.ErrInvalidArgument
	}
	var condition struct {
		ProfileID  string                  `json:"profile_id"`
		TemplateID string                  `json:"template_id"`
		Comparison verification.Comparison `json:"comparison"`
		Threshold  float64                 `json:"threshold"`
	}
	_ = json.Unmarshal(row.ExpectedJSON, &condition)
	return verification.Check{ID: row.ID, PublicID: row.PublicID, VerificationRunID: row.VerificationRunID, Type: verification.CheckType(row.CheckType), Status: verification.CheckStatus(row.Status), Required: row.RequiredCheck, Subject: subject, Expected: row.ExpectedJSON, Observed: row.ObservedJSON, SourceReference: row.SourceReference, Lookback: time.Duration(row.LookbackMS) * time.Millisecond, StabilityWindow: time.Duration(row.StabilityWindowMS) * time.Millisecond, Timeout: time.Duration(row.TimeoutMS) * time.Millisecond, PollInterval: time.Duration(row.PollIntervalMS) * time.Millisecond, FirstCheckedAt: row.FirstCheckedAt, LastCheckedAt: row.LastCheckedAt, PassedAt: row.PassedAt, ConsecutiveSuccessSince: row.ConsecutiveSuccessSince, AttemptCount: row.AttemptCount, FailureReason: row.FailureReason, ProfileID: condition.ProfileID, TemplateID: condition.TemplateID, Comparison: condition.Comparison, Threshold: condition.Threshold, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}

func appendVerificationAudit(tx *gorm.DB, incidentID uint64, incidentPublicID, eventType, identity, summary string, metadata map[string]any, at time.Time) error {
	payload, err := json.Marshal(metadata)
	if err != nil || len(payload) > 8*1024 || len(summary) > 2048 {
		return verification.ErrInvalidArgument
	}
	keySum := sha256.Sum256([]byte("phase5:" + eventType + ":" + identity))
	key := hex.EncodeToString(keySum[:])
	event := phase5TimelineRow{IncidentID: incidentID, EventType: eventType, IdempotencyKey: &key, ActorType: string(domain.ActorSystem), ActorID: "delivery-verification", Summary: summary, MetadataJSON: payload, OccurredAt: at.UTC(), CreatedAt: at.UTC()}
	created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event)
	if created.Error != nil {
		return mapVerificationError(created.Error)
	}
	if created.RowsAffected == 0 {
		return nil
	}
	outboxPayload, _ := json.Marshal(map[string]any{"incident_id": incidentPublicID, "event_type": eventType, "metadata": metadata})
	eventID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)).String()
	outbox := outboxRow{EventID: eventID, AggregateType: "incident", AggregateID: incidentPublicID, EventType: eventType, SchemaVersion: 1, PayloadJSON: outboxPayload, OccurredAt: at.UTC(), CreatedAt: at.UTC()}
	return mapVerificationError(tx.Create(&outbox).Error)
}

func deliveryEvent(status string) (string, string) {
	switch status {
	case "ci_pending":
		return "delivery_ci_pending", "CI checks are pending"
	case "ci_passed":
		return "delivery_ci_passed", "Required CI checks passed"
	case "ci_failed":
		return "delivery_ci_failed", "Required CI checks failed"
	case "merged":
		return "delivery_pr_merged", "Pull Request merged and exact commit bound"
	case "pr_closed":
		return "delivery_pr_closed", "Pull Request closed without merge"
	case "syncing":
		return "delivery_argocd_sync_started", "Argo CD sync operation started"
	case "synced":
		return "delivery_argocd_sync_succeeded", "Argo CD synced exact revision"
	case "argocd_failed":
		return "delivery_argocd_sync_failed", "Argo CD sync operation failed"
	case "rollout_pending":
		return "delivery_kubernetes_rollout_started", "Kubernetes Deployment rollout started"
	case "delivered":
		return "delivery_completed", "Exact revision delivery completed"
	default:
		return "delivery_" + status, "Delivery state changed to " + status
	}
}

func safeRevision(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func mapVerificationError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return verification.ErrNotFound
	}
	if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		return verification.ErrConflict
	}
	return err
}
