package feedback

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"server-web/internal/model"
)

type Repository interface {
	Upsert(ctx context.Context, feedback *model.DiagnosisFeedback) error
	GetByDiagnosisID(ctx context.Context, diagnosisID uint64) ([]model.DiagnosisFeedback, error)
	GetByDiagnosisIDAndUserID(ctx context.Context, diagnosisID uint64, userID uint64) (*model.DiagnosisFeedback, error)
	ListNotUseful(ctx context.Context, limit int) ([]model.DiagnosisFeedback, error)
	ListLowConfidence(ctx context.Context, confidenceThreshold float64, limit int) ([]model.DiagnosisFeedback, error)
}

type mysqlRepository struct {
	db *gorm.DB
}

func NewMySQLRepository(db *gorm.DB) Repository {
	return &mysqlRepository{db: db}
}

func (r *mysqlRepository) Upsert(ctx context.Context, feedback *model.DiagnosisFeedback) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "diagnosis_id"}, {Name: "created_by"}},
			DoUpdates: clause.AssignmentColumns([]string{"rating", "comment", "updated_at"}),
		}).
		Create(feedback).Error
}

func (r *mysqlRepository) GetByDiagnosisID(ctx context.Context, diagnosisID uint64) ([]model.DiagnosisFeedback, error) {
	var feedbacks []model.DiagnosisFeedback
	err := r.db.WithContext(ctx).
		Where("diagnosis_id = ?", diagnosisID).
		Order("created_at DESC").
		Find(&feedbacks).Error
	return feedbacks, err
}

func (r *mysqlRepository) GetByDiagnosisIDAndUserID(ctx context.Context, diagnosisID uint64, userID uint64) (*model.DiagnosisFeedback, error) {
	var feedback model.DiagnosisFeedback
	err := r.db.WithContext(ctx).
		Where("diagnosis_id = ? AND created_by = ?", diagnosisID, userID).
		First(&feedback).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &feedback, nil
}

func (r *mysqlRepository) ListNotUseful(ctx context.Context, limit int) ([]model.DiagnosisFeedback, error) {
	var feedbacks []model.DiagnosisFeedback
	err := r.db.WithContext(ctx).
		Where("rating = ?", RatingNotUseful).
		Order("created_at DESC").
		Limit(limit).
		Find(&feedbacks).Error
	return feedbacks, err
}

func (r *mysqlRepository) ListLowConfidence(ctx context.Context, confidenceThreshold float64, limit int) ([]model.DiagnosisFeedback, error) {
	var feedbacks []model.DiagnosisFeedback
	err := r.db.WithContext(ctx).
		Joins("JOIN diagnosis_reports ON diagnosis_reports.id = diagnosis_feedback.diagnosis_id").
		Where("diagnosis_reports.confidence < ? AND diagnosis_reports.status = ?", confidenceThreshold, "completed").
		Order("diagnosis_reports.confidence ASC").
		Limit(limit).
		Find(&feedbacks).Error
	return feedbacks, err
}
