package api

import (
	"context"

	"server-web/copilot/diagnosis"
	"server-web/copilot/feedback"
	"server-web/internal/model"
)

type diagnosisAccessAdapter struct {
	repo *diagnosis.Repository
}

func NewReportAccessChecker(repo *diagnosis.Repository) feedback.ReportAccessChecker {
	return &diagnosisAccessAdapter{repo: repo}
}

func (a *diagnosisAccessAdapter) GetAccessibleReport(ctx context.Context, id uint64, userID uint64, role string) (model.DiagnosisReport, error) {
	return a.repo.GetByID(ctx, id, diagnosis.User{ID: userID, Role: role})
}
