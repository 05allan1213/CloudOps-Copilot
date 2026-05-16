package feedback

import (
	"context"

	"server-web/internal/model"
)

type MetricsObserver interface {
	ObserveFeedback(rating string, hasComment bool)
}

type Service struct {
	repo    Repository
	metrics MetricsObserver
}

func NewService(repo Repository, metrics MetricsObserver) *Service {
	return &Service{repo: repo, metrics: metrics}
}

func (s *Service) Submit(ctx context.Context, diagnosisID uint64, userID uint64, req FeedbackRequest) (*FeedbackResponse, error) {
	comment := sanitizeComment(req.Comment)
	feedback := &model.DiagnosisFeedback{
		DiagnosisID: diagnosisID,
		Rating:      req.Rating,
		Comment:     comment,
		CreatedBy:   userID,
	}
	if err := s.repo.Upsert(ctx, feedback); err != nil {
		return nil, err
	}
	if s.metrics != nil {
		s.metrics.ObserveFeedback(req.Rating, req.Comment != "")
	}
	return &FeedbackResponse{
		ID:          feedback.ID,
		DiagnosisID: feedback.DiagnosisID,
		Rating:      feedback.Rating,
		Comment:     feedback.Comment,
		CreatedBy:   feedback.CreatedBy,
		CreatedAt:   feedback.CreatedAt,
	}, nil
}
