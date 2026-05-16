package diagnosis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"server-web/internal/model"
)

type FeedbackQueryRepository interface {
	GetByDiagnosisIDAndUserID(ctx context.Context, diagnosisID uint64, userID uint64) (*model.DiagnosisFeedback, error)
}

type Service struct {
	repo         *Repository
	resolver     *Resolver
	collector    *EvidenceCollector
	analyzer     RuleAnalyzer
	summarizer   Summarizer
	feedbackRepo FeedbackQueryRepository
	now          func() time.Time
}

type Config struct {
	Repository         *Repository
	Resolver           *Resolver
	Collector          *EvidenceCollector
	Analyzer           RuleAnalyzer
	Summarizer         Summarizer
	FeedbackRepository FeedbackQueryRepository
	Now                func() time.Time
}

func NewService(cfg Config) *Service {
	analyzer := cfg.Analyzer
	if analyzer == nil {
		analyzer = NewRuleAnalyzer()
	}
	summarizer := cfg.Summarizer
	if summarizer == nil {
		summarizer = NewLLMSummarizer(nil)
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		repo:         cfg.Repository,
		resolver:     cfg.Resolver,
		collector:    cfg.Collector,
		analyzer:     analyzer,
		summarizer:   summarizer,
		feedbackRepo: cfg.FeedbackRepository,
		now:          now,
	}
}

func (s *Service) Trigger(ctx context.Context, user User, req Request) (ReportResponse, error) {
	req, err := NormalizeRequest(req)
	if err != nil {
		return ReportResponse{}, err
	}
	if s == nil || s.repo == nil || s.resolver == nil {
		return ReportResponse{}, ErrUnavailable
	}
	ctx = WithUser(ctx, user)

	report := model.DiagnosisReport{
		Status:                 StatusPending,
		TriggerType:            req.TriggerType,
		CreatedBy:              user.ID,
		Fingerprint:            req.Fingerprint,
		AlertHistoryID:         req.AlertHistoryID,
		AlertName:              req.AlertName,
		TargetName:             req.Instance,
		TargetKind:             "host",
		EvidenceJSON:           "{}",
		RunbooksJSON:           "[]",
		RecommendedActionsJSON: "[]",
		RuleAnalysisJSON:       "{}",
	}
	if err := s.repo.Create(ctx, &report); err != nil {
		return ReportResponse{}, err
	}

	alert, err := s.resolver.Resolve(ctx, req)
	if err != nil {
		_ = s.repo.UpdateStatus(ctx, report.ID, StatusFailed, map[string]interface{}{
			"summary": publicError(err),
		})
		return ReportResponse{}, err
	}

	if err := s.repo.UpdateStatus(ctx, report.ID, StatusRunning, map[string]interface{}{
		"alert_history_id": alert.AlertHistoryID,
		"fingerprint":      alert.Fingerprint,
		"alert_name":       alert.AlertName,
		"target_kind":      alert.TargetKind,
		"target_name":      alert.TargetName,
		"namespace":        alert.Namespace,
		"severity":         alert.Severity,
	}); err != nil {
		return ReportResponse{}, err
	}

	evidence := EvidenceBundle{AlertContext: alert, Runbooks: []RunbookEvidence{}, CollectedAt: s.now().UTC()}
	if s.collector != nil {
		evidence = s.collector.Collect(ctx, alert)
	}
	rules := s.analyzer.Analyze(ctx, alert, evidence)
	summary, meta, err := s.summarizer.Summarize(ctx, alert, evidence, rules)
	if err != nil {
		evidence.CollectionErrors = append(evidence.CollectionErrors, CollectionError{Source: "llm", Error: err.Error()})
		summary = RuleOnlySummary(alert, rules)
		meta.Model = "rule-only"
	}

	reportFields, err := completedFields(alert, evidence, rules, summary, meta)
	if err != nil {
		_ = s.repo.UpdateStatus(ctx, report.ID, StatusFailed, map[string]interface{}{
			"summary": "serialize diagnosis report failed",
		})
		return ReportResponse{}, err
	}
	if err := s.repo.UpdateStatus(ctx, report.ID, StatusCompleted, reportFields); err != nil {
		return ReportResponse{}, err
	}
	updated, err := s.repo.GetByID(ctx, report.ID, user)
	if err != nil {
		return ReportResponse{}, err
	}
	return toReportResponse(updated, nil), nil
}

func (s *Service) Get(ctx context.Context, user User, id uint64) (ReportResponse, error) {
	if s == nil || s.repo == nil {
		return ReportResponse{}, ErrUnavailable
	}
	report, err := s.repo.GetByID(ctx, id, user)
	if err != nil {
		return ReportResponse{}, err
	}
	var myFeedback *model.DiagnosisFeedback
	if s.feedbackRepo != nil {
		if fb, fbErr := s.feedbackRepo.GetByDiagnosisIDAndUserID(ctx, id, user.ID); fbErr == nil && fb != nil {
			myFeedback = fb
		}
	}
	return toReportResponse(report, myFeedback), nil
}

func (s *Service) List(ctx context.Context, user User, filter ListFilter) (ListResult, error) {
	if s == nil || s.repo == nil {
		return ListResult{}, ErrUnavailable
	}
	reports, total, normalized, err := s.repo.List(ctx, filter, user)
	if err != nil {
		return ListResult{}, err
	}
	items := make([]ReportResponse, 0, len(reports))
	for _, report := range reports {
		items = append(items, toReportResponse(report, nil))
	}
	return ListResult{Items: items, Total: total, Page: normalized.Page, PageSize: normalized.PageSize}, nil
}

func completedFields(alert AlertContext, evidence EvidenceBundle, rules RuleAnalysis, summary DiagnosisSummary, meta LLMMetadata) (map[string]interface{}, error) {
	evidenceJSON, err := marshalJSON(evidence)
	if err != nil {
		return nil, fmt.Errorf("marshal evidence: %w", err)
	}
	rulesJSON, err := marshalJSON(rules)
	if err != nil {
		return nil, fmt.Errorf("marshal rule analysis: %w", err)
	}
	actionsJSON, err := marshalJSON(summary.RecommendedActions)
	if err != nil {
		return nil, fmt.Errorf("marshal recommended actions: %w", err)
	}
	runbooksJSON, err := marshalJSON(evidence.Runbooks)
	if err != nil {
		return nil, fmt.Errorf("marshal runbooks: %w", err)
	}
	rootCause := ""
	if len(summary.RootCauseHypotheses) > 0 {
		rootCause = summary.RootCauseHypotheses[0].Cause
	}
	if rootCause == "" {
		rootCause = rules.Summary
	}
	return map[string]interface{}{
		"alert_history_id":         alert.AlertHistoryID,
		"fingerprint":              alert.Fingerprint,
		"alert_name":               alert.AlertName,
		"target_kind":              alert.TargetKind,
		"target_name":              alert.TargetName,
		"namespace":                alert.Namespace,
		"severity":                 alert.Severity,
		"summary":                  strings.TrimSpace(summary.Summary),
		"root_cause":               rootCause,
		"evidence_json":            evidenceJSON,
		"runbooks_json":            runbooksJSON,
		"recommended_actions_json": actionsJSON,
		"rule_analysis_json":       rulesJSON,
		"confidence":               rules.Confidence,
		"llm_prompt_hash":          meta.PromptHash,
		"llm_model":                meta.Model,
	}, nil
}

func publicError(err error) string {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return err.Error()
	case errors.Is(err, ErrNotFound):
		return "diagnosis target not found"
	case errors.Is(err, ErrConflict):
		return "diagnosis target is ambiguous"
	default:
		return "diagnosis failed"
	}
}
