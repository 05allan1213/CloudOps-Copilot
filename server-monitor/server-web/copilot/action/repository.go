package action

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"server-web/model"
)

var (
	ErrNotFound    = errors.New("action not found")
	ErrUnavailable = errors.New("action service unavailable")
)

type repository interface {
	GetDiagnosisReport(ctx context.Context, id uint64) (model.DiagnosisReport, error)
	FindPendingByDedupeKey(ctx context.Context, key string) (model.PendingAction, bool, error)
	CreatePending(ctx context.Context, input NormalizedAction) (model.PendingAction, error)
	GetAction(ctx context.Context, id uint64) (model.PendingAction, error)
	TransitionAction(ctx context.Context, id uint64, event string, mutate func(*model.PendingAction) error) (model.PendingAction, error)
	ListActions(ctx context.Context, filter ListFilter) ([]model.PendingAction, int64, ListFilter, error)
	RecordAudit(ctx context.Context, entry AuditEntry) error
	ListAuditLogs(ctx context.Context, filter ListFilter) ([]model.AuditLog, int64, ListFilter, error)
	GetAuditLog(ctx context.Context, id uint64) (model.AuditLog, error)
}

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetDiagnosisReport(ctx context.Context, id uint64) (model.DiagnosisReport, error) {
	if r == nil || r.db == nil {
		return model.DiagnosisReport{}, ErrUnavailable
	}
	var report model.DiagnosisReport
	err := r.db.WithContext(ctx).First(&report, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.DiagnosisReport{}, ErrNotFound
	}
	return report, err
}

func (r *Repository) FindPendingByDedupeKey(ctx context.Context, key string) (model.PendingAction, bool, error) {
	if r == nil || r.db == nil {
		return model.PendingAction{}, false, ErrUnavailable
	}
	var action model.PendingAction
	terminalStatuses := []string{
		model.ActionStatusRejected,
		model.ActionStatusFailed,
		model.ActionStatusCancelled,
		model.ActionStatusExecuted,
	}
	err := r.db.WithContext(ctx).
		Where("dedupe_key = ? AND status NOT IN ?", key, terminalStatuses).
		First(&action).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.PendingAction{}, false, nil
	}
	return action, err == nil, err
}

func (r *Repository) CreatePending(ctx context.Context, input NormalizedAction) (model.PendingAction, error) {
	if r == nil || r.db == nil {
		return model.PendingAction{}, ErrUnavailable
	}
	action := model.PendingAction{
		DiagnosisReportID: input.DiagnosisReportID,
		ActionType:        input.ActionType,
		TargetKind:        input.TargetKind,
		TargetName:        input.TargetName,
		Namespace:         input.Namespace,
		ParamsJSON:        string(input.Params),
		DedupeKey:         input.DedupeKey,
		RiskLevel:         input.RiskLevel,
		Status:            model.ActionStatusPending,
		RequestedBy:       input.RequestedBy,
		ResultJSON:        "{}",
		ErrorMessage:      "",
	}
	if err := r.db.WithContext(ctx).Create(&action).Error; err != nil {
		return model.PendingAction{}, err
	}
	return action, nil
}

func (r *Repository) GetAction(ctx context.Context, id uint64) (model.PendingAction, error) {
	if r == nil || r.db == nil {
		return model.PendingAction{}, ErrUnavailable
	}
	var action model.PendingAction
	err := r.db.WithContext(ctx).First(&action, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.PendingAction{}, ErrNotFound
	}
	return action, err
}

func (r *Repository) TransitionAction(ctx context.Context, id uint64, event string, mutate func(*model.PendingAction) error) (model.PendingAction, error) {
	if r == nil || r.db == nil {
		return model.PendingAction{}, ErrUnavailable
	}
	var action model.PendingAction
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&action, id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		nextStatus, ok := CanTransition(action.Status, event)
		if !ok {
			return fmt.Errorf("%w: cannot %s action from status %s", ErrInvalidAction, event, action.Status)
		}
		if mutate != nil {
			if err := mutate(&action); err != nil {
				return err
			}
		}
		action.Status = nextStatus
		return tx.Save(&action).Error
	})
	return action, err
}

func (r *Repository) ListActions(ctx context.Context, filter ListFilter) ([]model.PendingAction, int64, ListFilter, error) {
	if r == nil || r.db == nil {
		return nil, 0, filter, ErrUnavailable
	}
	filter = normalizeListFilter(filter)
	stmt := r.db.WithContext(ctx).Model(&model.PendingAction{})
	if filter.Status != "" {
		stmt = stmt.Where("status = ?", filter.Status)
	}
	if filter.RiskLevel != "" {
		stmt = stmt.Where("risk_level = ?", filter.RiskLevel)
	}
	if filter.ActionType != "" {
		stmt = stmt.Where("action_type = ?", filter.ActionType)
	}
	var total int64
	if err := stmt.Count(&total).Error; err != nil {
		return nil, 0, filter, err
	}
	var actions []model.PendingAction
	err := stmt.Order("created_at DESC").Order("id DESC").Limit(filter.PageSize).Offset((filter.Page - 1) * filter.PageSize).Find(&actions).Error
	return actions, total, filter, err
}

func (r *Repository) RecordAudit(ctx context.Context, entry AuditEntry) error {
	if r == nil || r.db == nil {
		return ErrUnavailable
	}
	if entry.TraceID == "" {
		entry.TraceID = TraceIDFromContext(ctx)
	}
	audit := model.AuditLog{
		Actor:        entry.Actor,
		ActorRole:    entry.ActorRole,
		Action:       entry.Action,
		ResourceType: entry.ResourceType,
		ResourceID:   entry.ResourceID,
		RequestJSON:  string(SanitizeJSON(entry.Request)),
		Result:       entry.Result,
		ErrorMessage: entry.ErrorMessage,
		TraceID:      entry.TraceID,
	}
	return r.db.WithContext(ctx).Create(&audit).Error
}

func (r *Repository) ListAuditLogs(ctx context.Context, filter ListFilter) ([]model.AuditLog, int64, ListFilter, error) {
	if r == nil || r.db == nil {
		return nil, 0, filter, ErrUnavailable
	}
	filter = normalizeListFilter(filter)
	stmt := r.db.WithContext(ctx).Model(&model.AuditLog{})
	if filter.ActionType != "" {
		stmt = stmt.Where("action = ?", filter.ActionType)
	}
	if filter.Result != "" {
		stmt = stmt.Where("result = ?", filter.Result)
	}
	if filter.Actor != "" {
		stmt = stmt.Where("actor = ?", filter.Actor)
	}
	var total int64
	if err := stmt.Count(&total).Error; err != nil {
		return nil, 0, filter, err
	}
	var logs []model.AuditLog
	err := stmt.Order("created_at DESC").Order("id DESC").Limit(filter.PageSize).Offset((filter.Page - 1) * filter.PageSize).Find(&logs).Error
	return logs, total, filter, err
}

func (r *Repository) GetAuditLog(ctx context.Context, id uint64) (model.AuditLog, error) {
	if r == nil || r.db == nil {
		return model.AuditLog{}, ErrUnavailable
	}
	var audit model.AuditLog
	err := r.db.WithContext(ctx).First(&audit, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.AuditLog{}, ErrNotFound
	}
	return audit, err
}

func normalizeListFilter(filter ListFilter) ListFilter {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	return filter
}

func parseUintID(raw string) (uint64, error) {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("%w: invalid id", ErrInvalidAction)
	}
	return id, nil
}
