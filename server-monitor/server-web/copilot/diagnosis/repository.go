package diagnosis

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"server-web/model"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, report *model.DiagnosisReport) error {
	if r == nil || r.db == nil {
		return ErrUnavailable
	}
	if report == nil {
		return fmt.Errorf("%w: report is required", ErrInvalidRequest)
	}
	return r.db.WithContext(ctx).Create(report).Error
}

func (r *Repository) UpdateStatus(ctx context.Context, id uint64, status string, fields map[string]interface{}) error {
	if r == nil || r.db == nil {
		return ErrUnavailable
	}
	if id == 0 {
		return fmt.Errorf("%w: id is required", ErrInvalidRequest)
	}
	if !validReportStatus(status) {
		return fmt.Errorf("%w: invalid status", ErrInvalidRequest)
	}
	updates := make(map[string]interface{}, len(fields)+1)
	for key, value := range fields {
		updates[key] = value
	}
	updates["status"] = status
	return r.db.WithContext(ctx).Model(&model.DiagnosisReport{}).Where("id = ?", id).Updates(updates).Error
}

func (r *Repository) GetByID(ctx context.Context, id uint64, user User) (model.DiagnosisReport, error) {
	if r == nil || r.db == nil {
		return model.DiagnosisReport{}, ErrUnavailable
	}
	if id == 0 {
		return model.DiagnosisReport{}, fmt.Errorf("%w: id is required", ErrInvalidRequest)
	}
	var report model.DiagnosisReport
	err := r.db.WithContext(ctx).First(&report, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.DiagnosisReport{}, ErrNotFound
	}
	if err != nil {
		return model.DiagnosisReport{}, err
	}
	if !canAccessReport(user, report) {
		return model.DiagnosisReport{}, ErrForbidden
	}
	return report, nil
}

func (r *Repository) List(ctx context.Context, filter ListFilter, user User) ([]model.DiagnosisReport, int64, ListFilter, error) {
	if r == nil || r.db == nil {
		return nil, 0, filter, ErrUnavailable
	}
	filter, err := NormalizeListFilter(filter)
	if err != nil {
		return nil, 0, filter, err
	}

	stmt := r.db.WithContext(ctx).Model(&model.DiagnosisReport{})
	if filter.Status != "" {
		stmt = stmt.Where("status = ?", filter.Status)
	}
	if filter.TriggerType != "" {
		stmt = stmt.Where("trigger_type = ?", filter.TriggerType)
	}
	if user.Role != "admin" {
		stmt = stmt.Where("created_by = ?", user.ID)
	}

	var total int64
	if err := stmt.Count(&total).Error; err != nil {
		return nil, 0, filter, err
	}

	var reports []model.DiagnosisReport
	err = stmt.
		Order("created_at DESC").
		Order("id DESC").
		Limit(filter.PageSize).
		Offset((filter.Page - 1) * filter.PageSize).
		Find(&reports).Error
	if err != nil {
		return nil, 0, filter, err
	}
	return reports, total, filter, nil
}

func (r *Repository) FindLatestByFingerprint(ctx context.Context, fingerprint string) (model.DiagnosisReport, error) {
	if r == nil || r.db == nil {
		return model.DiagnosisReport{}, ErrUnavailable
	}
	var report model.DiagnosisReport
	err := r.db.WithContext(ctx).
		Where("fingerprint = ?", fingerprint).
		Order("created_at DESC").
		Order("id DESC").
		First(&report).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.DiagnosisReport{}, ErrNotFound
	}
	return report, err
}

func canAccessReport(user User, report model.DiagnosisReport) bool {
	return user.Role == "admin" || report.CreatedBy == user.ID
}

func validReportStatus(status string) bool {
	switch status {
	case StatusPending, StatusRunning, StatusCompleted, StatusFailed:
		return true
	default:
		return false
	}
}

func toReportResponse(report model.DiagnosisReport) ReportResponse {
	return ReportResponse{
		ID:                 report.ID,
		AlertHistoryID:     report.AlertHistoryID,
		Fingerprint:        report.Fingerprint,
		AlertName:          report.AlertName,
		TargetKind:         report.TargetKind,
		TargetName:         report.TargetName,
		Namespace:          report.Namespace,
		Severity:           report.Severity,
		Status:             report.Status,
		Summary:            report.Summary,
		RootCause:          report.RootCause,
		Evidence:           rawJSON(report.EvidenceJSON),
		Runbooks:           rawJSON(report.RunbooksJSON),
		RecommendedActions: rawJSON(report.RecommendedActionsJSON),
		RuleAnalysis:       rawJSON(report.RuleAnalysisJSON),
		Confidence:         report.Confidence,
		ConfidenceLevel:    ConfidenceLevel(report.Confidence),
		LLMPromptHash:      report.LLMPromptHash,
		LLMModel:           report.LLMModel,
		TriggerType:        report.TriggerType,
		CreatedBy:          report.CreatedBy,
		CreatedAt:          report.CreatedAt,
		UpdatedAt:          report.UpdatedAt,
	}
}
