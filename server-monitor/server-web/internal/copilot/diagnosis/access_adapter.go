package diagnosis

import (
	"context"

	"server-web/internal/model"
)

type ReportAccessChecker struct {
	repo *Repository
}

func NewReportAccessChecker(repo *Repository) *ReportAccessChecker {
	return &ReportAccessChecker{repo: repo}
}

func (a *ReportAccessChecker) GetAccessibleReport(ctx context.Context, id uint64, userID uint64, role string) (model.DiagnosisReport, error) {
	return a.repo.GetByID(ctx, id, User{ID: userID, Role: role})
}
