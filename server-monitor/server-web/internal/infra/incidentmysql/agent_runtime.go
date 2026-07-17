package incidentmysql

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"server-web/internal/agent"
	incidentdomain "server-web/internal/incident"
)

var _ agent.Store = (*Store)(nil)

func (s *Store) CreateRun(ctx context.Context, request agent.CreateRunRequest) (*agent.Run, error) {
	if strings.TrimSpace(request.IncidentPublicID) == "" || strings.TrimSpace(request.IdempotencyKey) == "" ||
		request.Limits.MaxSteps <= 0 || request.Limits.MaxToolCalls <= 0 || request.Limits.MaxModelCalls <= 0 ||
		request.Limits.TokenBudget <= 0 || request.Limits.MaxEvidenceItems <= 0 || request.Limits.MaxRuntime <= 0 {
		return nil, fmt.Errorf("%w: invalid run request", agent.ErrInvalidArgument)
	}
	if len(request.IdempotencyKey) > 128 || len(request.Checkpoint) > request.Limits.MaxCheckpointSize {
		return nil, fmt.Errorf("%w: idempotency key or checkpoint exceeds limit", agent.ErrInvalidArgument)
	}
	now := request.At.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var result agentRunRow
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var incident incidentRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", request.IncidentPublicID).First(&incident).Error; err != nil {
			return mapAgentError(err)
		}
		var existing agentRunRow
		err := tx.Where("incident_id = ? AND idempotency_key = ?", incident.ID, request.IdempotencyKey).First(&existing).Error
		if err == nil {
			result = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return mapAgentError(err)
		}
		if incident.Status != string(incidentdomain.StatusCorrelating) && incident.Status != string(incidentdomain.StatusDiagnosing) {
			return fmt.Errorf("%w: incident status %s cannot start diagnosis", agent.ErrConflict, incident.Status)
		}
		var attempt int64
		if err := tx.Model(&agentRunRow{}).Where("incident_id = ?", incident.ID).Select("COALESCE(MAX(attempt), 0)").Scan(&attempt).Error; err != nil {
			return mapAgentError(err)
		}
		key := request.IdempotencyKey
		checkpointSum := sha256.Sum256(request.Checkpoint)
		result = agentRunRow{
			PublicID: uuid.NewString(), IncidentID: incident.ID, IdempotencyKey: &key, Attempt: int(attempt) + 1,
			Status: string(agent.RunPending), Model: request.Model, PromptVersion: request.PromptVersion,
			MaxSteps: request.Limits.MaxSteps, MaxToolCalls: request.Limits.MaxToolCalls,
			MaxModelCalls: request.Limits.MaxModelCalls, TokenBudget: request.Limits.TokenBudget,
			MaxEvidenceItems: request.Limits.MaxEvidenceItems, MaxRuntimeMS: request.Limits.MaxRuntime.Milliseconds(),
			ToolTimeoutMS: request.Limits.ToolTimeout.Milliseconds(), MaxEvidenceBytes: request.Limits.MaxEvidenceBytes,
			MaxCheckpointBytes: request.Limits.MaxCheckpointSize, MaxStepRetries: request.Limits.MaxStepRetries,
			CurrentCheckpoint: request.Checkpoint, CheckpointVersion: 1, CheckpointSchemaVersion: 1, CheckpointHash: fmt.Sprintf("%x", checkpointSum[:]), RowVersion: 1,
			FailureCode: "", FailureSummary: "", CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&result).Error; err != nil {
			return mapAgentError(err)
		}
		incidentUpdates := map[string]any{"current_agent_run_id": result.ID, "version": gorm.Expr("version + 1"), "updated_at": now}
		startingDiagnosis := incident.Status == string(incidentdomain.StatusCorrelating)
		if startingDiagnosis {
			incidentUpdates["status"] = incidentdomain.StatusDiagnosing
		}
		if err := tx.Model(&incidentRow{}).Where("id = ?", incident.ID).Updates(incidentUpdates).Error; err != nil {
			return mapAgentError(err)
		}
		if startingDiagnosis {
			metadata, _ := json.Marshal(map[string]any{"from": incidentdomain.StatusCorrelating, "to": incidentdomain.StatusDiagnosing, "agent_run_id": result.PublicID})
			if err := tx.Create(&timelineRow{IncidentID: incident.ID, EventType: string(incidentdomain.EventStatusChanged), ActorType: string(incidentdomain.ActorUser), ActorID: "api", Summary: "Incident diagnosis started", MetadataJSON: metadata, OccurredAt: now, CreatedAt: now}).Error; err != nil {
				return mapAgentError(err)
			}
		}
		metadata, _ := json.Marshal(map[string]any{"agent_run_id": result.PublicID, "attempt": result.Attempt})
		return mapAgentError(tx.Create(&timelineRow{IncidentID: incident.ID, EventType: string(incidentdomain.EventAgentRunCreated), ActorType: string(incidentdomain.ActorUser), ActorID: "api", Summary: "Bounded incident agent run created", MetadataJSON: metadata, OccurredAt: now, CreatedAt: now}).Error)
	})
	if err != nil {
		return nil, err
	}
	return runFromRuntimeRow(result, request.IncidentPublicID), nil
}

func (s *Store) GetRun(ctx context.Context, publicID string) (*agent.Run, error) {
	var row agentRunRow
	if err := s.db.WithContext(ctx).Where("public_id = ?", publicID).First(&row).Error; err != nil {
		return nil, mapAgentError(err)
	}
	var incident incidentRow
	if err := s.db.WithContext(ctx).Select("public_id").First(&incident, row.IncidentID).Error; err != nil {
		return nil, mapAgentError(err)
	}
	return runFromRuntimeRow(row, incident.PublicID), nil
}

func (s *Store) ListRunsByIncident(ctx context.Context, incidentPublicID string, page, pageSize int) (agent.Page, error) {
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return agent.Page{}, fmt.Errorf("%w: invalid page", agent.ErrInvalidArgument)
	}
	var incident incidentRow
	if err := s.db.WithContext(ctx).Select("id", "public_id").Where("public_id = ?", incidentPublicID).First(&incident).Error; err != nil {
		return agent.Page{}, mapAgentError(err)
	}
	query := s.db.WithContext(ctx).Model(&agentRunRow{}).Where("incident_id = ?", incident.ID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return agent.Page{}, mapAgentError(err)
	}
	var rows []agentRunRow
	if err := query.Order("created_at DESC").Order("id DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
		return agent.Page{}, mapAgentError(err)
	}
	items := make([]agent.Run, 0, len(rows))
	for _, row := range rows {
		items = append(items, *runFromRuntimeRow(row, incident.PublicID))
	}
	return agent.Page{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Store) ListSteps(ctx context.Context, runPublicID string, limit int) ([]agent.Step, error) {
	var run agentRunRow
	if err := s.db.WithContext(ctx).Select("id").Where("public_id = ?", runPublicID).First(&run).Error; err != nil {
		return nil, mapAgentError(err)
	}
	var rows []agentStepRow
	if err := s.db.WithContext(ctx).Where("agent_run_id = ?", run.ID).Order("sequence ASC").Limit(boundedLimit(limit)).Find(&rows).Error; err != nil {
		return nil, mapAgentError(err)
	}
	result := make([]agent.Step, 0, len(rows))
	for _, row := range rows {
		result = append(result, stepFromRuntimeRow(row))
	}
	return result, nil
}

func (s *Store) ListEvidence(ctx context.Context, runPublicID string, limit int) ([]agent.EvidenceRecord, error) {
	var run agentRunRow
	if err := s.db.WithContext(ctx).Select("id").Where("public_id = ?", runPublicID).First(&run).Error; err != nil {
		return nil, mapAgentError(err)
	}
	var rows []evidenceRow
	if err := s.db.WithContext(ctx).Where("agent_run_id = ?", run.ID).Order("collected_at ASC").Order("id ASC").Limit(boundedLimit(limit)).Find(&rows).Error; err != nil {
		return nil, mapAgentError(err)
	}
	result := make([]agent.EvidenceRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, evidenceFromRuntimeRow(row))
	}
	return result, nil
}

func (s *Store) RequestCancel(ctx context.Context, publicID string, at time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row agentRunRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", publicID).First(&row).Error; err != nil {
			return mapAgentError(err)
		}
		updates := map[string]any{"cancel_requested_at": at.UTC(), "row_version": gorm.Expr("row_version + 1"), "updated_at": at.UTC()}
		switch agent.RunStatus(row.Status) {
		case agent.RunPending:
			updates["status"] = agent.RunCancelled
			updates["failure_code"] = agent.ErrorCancelled
			updates["failure_summary"] = "cancelled before execution"
			updates["completed_at"] = at.UTC()
		case agent.RunRunning:
		default:
			return agent.ErrConflict
		}
		result := tx.Model(&agentRunRow{}).Where("id = ? AND row_version = ?", row.ID, row.RowVersion).Updates(updates)
		if result.Error != nil {
			return mapAgentError(result.Error)
		}
		if result.RowsAffected != 1 {
			return agent.ErrConflict
		}
		return nil
	})
}

func (s *Store) ClaimNext(ctx context.Context, owner string, now time.Time, lease time.Duration) (*agent.Run, error) {
	if owner == "" || lease <= 0 {
		return nil, agent.ErrInvalidArgument
	}
	var claimed agentRunRow
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("(status = ?) OR (status = ? AND lease_expires_at < ?)", agent.RunPending, agent.RunRunning, now.UTC()).
			Where("cancel_requested_at IS NULL").Order("created_at ASC").Order("id ASC")
		if err := query.First(&claimed).Error; err != nil {
			return mapAgentError(err)
		}
		if claimed.Status == string(agent.RunRunning) {
			if err := tx.Model(&agentStepRow{}).Where("agent_run_id = ? AND status = ?", claimed.ID, agent.StepRunning).Updates(map[string]any{
				"status": agent.StepFailed, "error_code": agent.ErrorLeaseLost, "result_summary": "worker lease expired before step completion", "finished_at": now.UTC(),
			}).Error; err != nil {
				return mapAgentError(err)
			}
		}
		if claimed.Status == string(agent.RunPending) && !agent.CanTransitionRun(agent.RunPending, agent.RunRunning) {
			return agent.ErrInvalidArgument
		}
		deadline := now.UTC().Add(time.Duration(claimed.MaxRuntimeMS) * time.Millisecond)
		updates := map[string]any{
			"status": agent.RunRunning, "lease_owner": owner, "lease_expires_at": now.UTC().Add(lease),
			"heartbeat_at": now.UTC(), "row_version": gorm.Expr("row_version + 1"), "updated_at": now.UTC(),
		}
		if claimed.StartedAt == nil {
			updates["started_at"] = now.UTC()
			updates["deadline_at"] = deadline
		}
		if err := tx.Model(&agentRunRow{}).Where("id = ? AND row_version = ?", claimed.ID, claimed.RowVersion).Updates(updates).Error; err != nil {
			return mapAgentError(err)
		}
		if claimed.StartedAt == nil {
			var incident incidentRow
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&incident, claimed.IncidentID).Error; err != nil {
				return mapAgentError(err)
			}
			if incident.Status == string(incidentdomain.StatusCorrelating) {
				if err := tx.Model(&incidentRow{}).Where("id = ? AND version = ?", incident.ID, incident.Version).Updates(map[string]any{"status": incidentdomain.StatusDiagnosing, "version": incident.Version + 1, "updated_at": now.UTC()}).Error; err != nil {
					return mapAgentError(err)
				}
				metadata, _ := json.Marshal(map[string]any{"agent_run_id": claimed.PublicID})
				if err := tx.Create(&timelineRow{IncidentID: incident.ID, EventType: string(incidentdomain.EventStatusChanged), ActorType: string(incidentdomain.ActorAgent), ActorID: claimed.PublicID, Summary: "Incident agent diagnosis started", MetadataJSON: metadata, OccurredAt: now.UTC(), CreatedAt: now.UTC()}).Error; err != nil {
					return mapAgentError(err)
				}
			}
		}
		return tx.First(&claimed, claimed.ID).Error
	})
	if err != nil {
		return nil, err
	}
	var incident incidentRow
	if err := s.db.WithContext(ctx).Select("public_id").First(&incident, claimed.IncidentID).Error; err != nil {
		return nil, mapAgentError(err)
	}
	return runFromRuntimeRow(claimed, incident.PublicID), nil
}

func (s *Store) Heartbeat(ctx context.Context, runID uint64, owner string, now time.Time, lease time.Duration) error {
	result := s.db.WithContext(ctx).Model(&agentRunRow{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_expires_at >= ?", runID, agent.RunRunning, owner, now.UTC()).Updates(map[string]any{
		"heartbeat_at": now.UTC(), "lease_expires_at": now.UTC().Add(lease), "updated_at": now.UTC(),
	})
	if result.Error != nil {
		return mapAgentError(result.Error)
	}
	if result.RowsAffected != 1 {
		return agent.ErrLeaseLost
	}
	return nil
}

func (s *Store) BeginStep(ctx context.Context, run *agent.Run, start agent.StepStart) (*agent.Step, error) {
	if run == nil || run.ID == 0 || start.Node == "" {
		return nil, agent.ErrInvalidArgument
	}
	var row agentStepRow
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current agentRunRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, run.ID).Error; err != nil {
			return mapAgentError(err)
		}
		if current.Status != string(agent.RunRunning) || current.LeaseOwner != run.LeaseOwner || current.RowVersion != run.RowVersion || current.CancelRequestedAt != nil || current.LeaseExpiresAt == nil || current.LeaseExpiresAt.Before(start.At.UTC()) {
			return agent.ErrLeaseLost
		}
		delta := 0
		if start.Budgeted {
			delta = 1
			if current.UsedSteps+delta > current.MaxSteps {
				return agent.ErrBudgetExceeded
			}
		}
		var sequence int64
		if err := tx.Model(&agentStepRow{}).Where("agent_run_id = ?", run.ID).Select("COALESCE(MAX(sequence), 0)").Scan(&sequence).Error; err != nil {
			return mapAgentError(err)
		}
		started := start.At.UTC()
		row = agentStepRow{PublicID: uuid.NewString(), AgentRunID: run.ID, Sequence: int(sequence) + 1, StepType: string(start.Node), ShortReason: bound(start.Reason, 1024), SelectedTool: start.SelectedTool, ArgumentsJSON: start.Arguments, ArgumentsHash: start.ArgumentsHash, Status: string(agent.StepPending), CreatedAt: started}
		if err := tx.Create(&row).Error; err != nil {
			return mapAgentError(err)
		}
		if !agent.CanTransitionStep(agent.StepPending, agent.StepRunning) {
			return agent.ErrInvalidArgument
		}
		if err := tx.Model(&agentStepRow{}).Where("id = ? AND status = ?", row.ID, agent.StepPending).Updates(map[string]any{"status": agent.StepRunning, "started_at": started}).Error; err != nil {
			return mapAgentError(err)
		}
		row.Status, row.StartedAt = string(agent.StepRunning), &started
		result := tx.Model(&agentRunRow{}).Where("id = ? AND row_version = ? AND lease_owner = ?", run.ID, run.RowVersion, run.LeaseOwner).Updates(map[string]any{"used_steps": gorm.Expr("used_steps + ?", delta), "row_version": gorm.Expr("row_version + 1"), "updated_at": started})
		if result.Error != nil {
			return mapAgentError(result.Error)
		}
		if result.RowsAffected != 1 {
			return agent.ErrLeaseLost
		}
		run.RowVersion++
		run.Usage.Steps += delta
		return nil
	})
	if err != nil {
		return nil, err
	}
	step := stepFromRuntimeRow(row)
	return &step, nil
}

func (s *Store) FinishStep(ctx context.Context, run *agent.Run, step *agent.Step, finish agent.StepFinish) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return finishStepTx(tx, run, step, finish)
	})
}

func (s *Store) PersistEvidence(ctx context.Context, run *agent.Run, step *agent.Step, item agent.EvidenceRecord, finish agent.StepFinish) (*agent.EvidenceRecord, error) {
	if run == nil || step == nil || item.IdempotencyKey == "" {
		return nil, agent.ErrInvalidArgument
	}
	if len(item.IdempotencyKey) != 64 || len(item.Facts) > run.Limits.MaxEvidenceBytes || !json.Valid(item.Facts) || len(item.ToolName) > 128 || len(item.ResultHash) != 64 {
		return nil, fmt.Errorf("%w: evidence violates deterministic bounds", agent.ErrInvalidArgument)
	}
	var row evidenceRow
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		key := item.IdempotencyKey
		runID := run.ID
		row = evidenceRow{PublicID: item.PublicID, IncidentID: run.IncidentID, AgentRunID: &runID, Type: bound(item.SourceType, 64), Source: bound(item.SourceType, 128), ToolName: item.ToolName, ResourceRef: bound(item.ResourceScope, 1024), TimeRangeJSON: item.TimeRange, QueryText: bound(item.Query, 4096), Summary: bound(item.Summary, 4096), FactsJSON: item.Facts, ResultHash: item.ResultHash, RawRef: bound(item.RawRef, 1024), RedactionJSON: item.Redaction, Truncated: item.Truncated, Valid: item.Valid, IdempotencyKey: &key, CollectedAt: item.CollectedAt.UTC(), CreatedAt: item.CollectedAt.UTC()}
		if row.PublicID == "" {
			row.PublicID = uuid.NewString()
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if result.Error != nil {
			return mapAgentError(result.Error)
		}
		if result.RowsAffected == 0 {
			if err := tx.Where("agent_run_id = ? AND idempotency_key = ?", run.ID, key).First(&row).Error; err != nil {
				return mapAgentError(err)
			}
			finish.Usage.Evidence = 0
		} else {
			finish.Usage.Evidence = 1
		}
		finish.EvidencePublicID = row.PublicID
		return finishStepTx(tx, run, step, finish)
	})
	if err != nil {
		return nil, err
	}
	result := evidenceFromRuntimeRow(row)
	return &result, nil
}

func finishStepTx(tx *gorm.DB, run *agent.Run, step *agent.Step, finish agent.StepFinish) error {
	if run == nil || step == nil || !agent.CanTransitionStep(step.Status, finish.Status) {
		return agent.ErrInvalidArgument
	}
	if err := run.Usage.CanCharge(finish.Usage, run.Limits); err != nil {
		return err
	}
	if len(finish.Checkpoint) > run.Limits.MaxCheckpointSize {
		return agent.ErrBudgetExceeded
	}
	result := tx.Model(&agentStepRow{}).Where("id = ? AND status = ?", step.ID, agent.StepRunning).Updates(map[string]any{
		"status": finish.Status, "result_summary": bound(finish.ResultSummary, 4096), "result_ref": bound(finish.ResultRef, 1024), "evidence_public_id": finish.EvidencePublicID,
		"retry_count": finish.RetryCount, "duration_ms": finish.DurationMS, "input_tokens": finish.Usage.InputTokens, "output_tokens": finish.Usage.OutputTokens,
		"error_code": finish.ErrorCode, "finished_at": finish.At.UTC(),
	})
	if result.Error != nil {
		return mapAgentError(result.Error)
	}
	if result.RowsAffected != 1 {
		return agent.ErrConflict
	}
	updates := map[string]any{
		"used_tool_calls": gorm.Expr("used_tool_calls + ?", finish.Usage.ToolCalls), "used_model_calls": gorm.Expr("used_model_calls + ?", finish.Usage.ModelCalls),
		"input_tokens": gorm.Expr("input_tokens + ?", finish.Usage.InputTokens), "output_tokens": gorm.Expr("output_tokens + ?", finish.Usage.OutputTokens),
		"used_evidence_items": gorm.Expr("used_evidence_items + ?", finish.Usage.Evidence), "current_checkpoint": finish.Checkpoint,
		"checkpoint_version": gorm.Expr("checkpoint_version + 1"), "checkpoint_schema_version": finish.CheckpointSchema, "checkpoint_hash": finish.CheckpointHash,
		"row_version": gorm.Expr("row_version + 1"), "updated_at": finish.At.UTC(),
	}
	result = tx.Model(&agentRunRow{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_expires_at >= ? AND row_version = ?", run.ID, agent.RunRunning, run.LeaseOwner, finish.At.UTC(), run.RowVersion).Updates(updates)
	if result.Error != nil {
		return mapAgentError(result.Error)
	}
	if result.RowsAffected != 1 {
		return agent.ErrLeaseLost
	}
	run.Usage.Charge(finish.Usage)
	run.RowVersion++
	run.CheckpointVersion++
	run.Checkpoint = finish.Checkpoint
	step.Status = finish.Status
	return nil
}

func (s *Store) FinishRun(ctx context.Context, run *agent.Run, status agent.RunStatus, diagnosis agent.Diagnosis, code agent.ErrorCode, summary string, at time.Time) error {
	if run == nil || !agent.CanTransitionRun(run.Status, status) || !agent.IsTerminalRun(status) {
		return agent.ErrInvalidArgument
	}
	diagnosisJSON, err := json.Marshal(diagnosis)
	if err != nil || len(diagnosisJSON) > 32*1024 {
		return agent.ErrInvalidArgument
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&agentRunRow{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_expires_at >= ? AND row_version = ?", run.ID, agent.RunRunning, run.LeaseOwner, at.UTC(), run.RowVersion).Updates(map[string]any{
			"status": status, "final_diagnosis": diagnosisJSON, "failure_code": code, "failure_summary": bound(summary, 2048),
			"completed_at": at.UTC(), "lease_owner": "", "lease_expires_at": nil, "heartbeat_at": nil,
			"row_version": gorm.Expr("row_version + 1"), "updated_at": at.UTC(),
		})
		if result.Error != nil {
			return mapAgentError(result.Error)
		}
		if result.RowsAffected != 1 {
			return agent.ErrLeaseLost
		}
		if status == agent.RunCompleted {
			var incident incidentRow
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&incident, run.IncidentID).Error; err != nil {
				return mapAgentError(err)
			}
			if incident.Status != string(incidentdomain.StatusDiagnosing) {
				return fmt.Errorf("%w: incident no longer diagnosing", agent.ErrConflict)
			}
			if err := tx.Model(&incidentRow{}).Where("id = ? AND version = ?", incident.ID, incident.Version).Updates(map[string]any{"status": incidentdomain.StatusDiagnosisCompleted, "version": incident.Version + 1, "updated_at": at.UTC()}).Error; err != nil {
				return mapAgentError(err)
			}
			metadata, _ := json.Marshal(map[string]any{"agent_run_id": run.PublicID, "degraded": diagnosis.Degraded})
			if err := tx.Create(&timelineRow{IncidentID: incident.ID, EventType: string(incidentdomain.EventStatusChanged), ActorType: string(incidentdomain.ActorAgent), ActorID: run.PublicID, Summary: "Bounded diagnosis completed", MetadataJSON: metadata, OccurredAt: at.UTC(), CreatedAt: at.UTC()}).Error; err != nil {
				return mapAgentError(err)
			}
			payload, _ := json.Marshal(map[string]any{"incident_id": run.IncidentPublicID, "agent_run_id": run.PublicID, "status": status})
			if err := tx.Create(&outboxRow{EventID: uuid.NewString(), AggregateType: "incident", AggregateID: run.IncidentPublicID, EventType: "incident.diagnosis_completed", SchemaVersion: 1, PayloadJSON: payload, OccurredAt: at.UTC(), Attempts: 0, LastError: "", CreatedAt: at.UTC()}).Error; err != nil {
				return mapAgentError(err)
			}
		}
		run.Status = status
		run.RowVersion++
		return nil
	})
}

func (s *Store) LoadIncident(ctx context.Context, id uint64) (agent.IncidentContext, error) {
	var row incidentRow
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return agent.IncidentContext{}, mapAgentError(err)
	}
	return agent.IncidentContext{PublicID: row.PublicID, Status: row.Status, Severity: row.Severity, Cluster: row.Cluster, Namespace: row.Namespace, ServiceName: row.ServiceName, TargetKind: row.TargetKind, TargetName: row.TargetName, Summary: row.Summary, FirstSeenAt: row.FirstSeenAt, LastSeenAt: row.LastSeenAt}, nil
}

func runFromRuntimeRow(row agentRunRow, incidentPublicID string) *agent.Run {
	key := ""
	if row.IdempotencyKey != nil {
		key = *row.IdempotencyKey
	}
	return &agent.Run{ID: row.ID, PublicID: row.PublicID, IncidentID: row.IncidentID, IncidentPublicID: incidentPublicID, IdempotencyKey: key, Attempt: row.Attempt, Status: agent.RunStatus(row.Status), Objective: row.Objective, Model: row.Model, PromptVersion: row.PromptVersion, Limits: agent.Limits{MaxSteps: row.MaxSteps, MaxToolCalls: row.MaxToolCalls, MaxModelCalls: row.MaxModelCalls, TokenBudget: row.TokenBudget, MaxEvidenceItems: row.MaxEvidenceItems, MaxRuntime: time.Duration(row.MaxRuntimeMS) * time.Millisecond, ToolTimeout: time.Duration(row.ToolTimeoutMS) * time.Millisecond, MaxEvidenceBytes: row.MaxEvidenceBytes, MaxCheckpointSize: row.MaxCheckpointBytes, MaxStepRetries: row.MaxStepRetries}, Usage: agent.Usage{Steps: row.UsedSteps, ToolCalls: row.UsedToolCalls, ModelCalls: row.UsedModelCalls, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, Evidence: row.UsedEvidenceItems}, Checkpoint: row.CurrentCheckpoint, CheckpointVersion: row.CheckpointVersion, CheckpointSchema: row.CheckpointSchemaVersion, CheckpointHash: row.CheckpointHash, LeaseOwner: row.LeaseOwner, LeaseExpiresAt: row.LeaseExpiresAt, HeartbeatAt: row.HeartbeatAt, CancelRequestedAt: row.CancelRequestedAt, FailureCode: agent.ErrorCode(row.FailureCode), FailureSummary: row.FailureSummary, FinalDiagnosis: row.FinalDiagnosis, RowVersion: row.RowVersion, StartedAt: row.StartedAt, FinishedAt: row.CompletedAt, DeadlineAt: row.DeadlineAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func stepFromRuntimeRow(row agentStepRow) agent.Step {
	return agent.Step{ID: row.ID, PublicID: row.PublicID, RunID: row.AgentRunID, Sequence: row.Sequence, Node: agent.Node(row.StepType), Status: agent.StepStatus(row.Status), ShortReason: row.ShortReason, SelectedTool: row.SelectedTool, Arguments: row.ArgumentsJSON, ArgumentsHash: row.ArgumentsHash, ResultSummary: row.ResultSummary, ResultRef: row.ResultRef, EvidencePublicID: row.EvidencePublicID, RetryCount: row.RetryCount, DurationMS: row.DurationMS, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, ErrorCode: agent.ErrorCode(row.ErrorCode), StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, CreatedAt: row.CreatedAt}
}

func evidenceFromRuntimeRow(row evidenceRow) agent.EvidenceRecord {
	key := ""
	if row.IdempotencyKey != nil {
		key = *row.IdempotencyKey
	}
	runID := uint64(0)
	if row.AgentRunID != nil {
		runID = *row.AgentRunID
	}
	return agent.EvidenceRecord{PublicID: row.PublicID, IncidentID: row.IncidentID, RunID: runID, SourceType: row.Type, ToolName: row.ToolName, ResourceScope: row.ResourceRef, TimeRange: row.TimeRangeJSON, Query: row.QueryText, Summary: row.Summary, Facts: row.FactsJSON, ResultHash: row.ResultHash, RawRef: row.RawRef, Redaction: row.RedactionJSON, Truncated: row.Truncated, Valid: row.Valid, IdempotencyKey: key, CollectedAt: row.CollectedAt}
}

func mapAgentError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return agent.ErrNotFound
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return agent.ErrConflict
	}
	return err
}

func bound(value string, size int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= size {
		return value
	}
	for size > 0 && !utf8.ValidString(value[:size]) {
		size--
	}
	return value[:size]
}
