package action

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"server-web/model"
)

type ServiceConfig struct {
	Repository              repository
	Policy                  *Policy
	Executor                K8sExecutor
	Notifier                StatusNotifier
	OperationEvents         OperationEventPublisher
	Observer                Observer
	OperationEventsEnabled  bool
	StatusPushEnabled       bool
	ActionExecutionEnabled  bool
	ExecutionTimeoutSeconds int
}

type Service struct {
	repo                   repository
	policy                 *Policy
	executor               K8sExecutor
	notifier               StatusNotifier
	events                 OperationEventPublisher
	observer               Observer
	operationEventsEnabled bool
	statusPushEnabled      bool
	executionTimeout       time.Duration
}

func NewService(cfg ServiceConfig) *Service {
	policy := cfg.Policy
	if policy == nil {
		policy = NewPolicy(PolicyConfig{})
	}
	executor := cfg.Executor
	if executor == nil || !cfg.ActionExecutionEnabled {
		executor = DisabledK8sExecutor{}
	}
	executionTimeout := time.Duration(cfg.ExecutionTimeoutSeconds) * time.Second
	if executionTimeout <= 0 {
		executionTimeout = 30 * time.Second
	}
	return &Service{
		repo:                   cfg.Repository,
		policy:                 policy,
		executor:               executor,
		notifier:               cfg.Notifier,
		events:                 cfg.OperationEvents,
		observer:               cfg.Observer,
		operationEventsEnabled: cfg.OperationEventsEnabled,
		statusPushEnabled:      cfg.StatusPushEnabled,
		executionTimeout:       executionTimeout,
	}
}

func (s *Service) CreateFromDiagnosis(ctx context.Context, reportID uint64, req CreateFromDiagnosisRequest, actor Actor) (CreateFromDiagnosisResult, error) {
	if err := s.requireReady(); err != nil {
		return CreateFromDiagnosisResult{}, err
	}
	if actor.Role != "admin" {
		if auditErr := s.audit(ctx, actor, "action.create_pending", "diagnosis_report", strconv.FormatUint(reportID, 10), mustMarshal(req), model.AuditResultDenied, "insufficient permissions"); auditErr != nil {
			return CreateFromDiagnosisResult{}, auditErr
		}
		s.observeAction("create_pending", model.AuditResultDenied)
		return CreateFromDiagnosisResult{}, ErrForbidden
	}
	report, err := s.repo.GetDiagnosisReport(ctx, reportID)
	if err != nil {
		return CreateFromDiagnosisResult{}, err
	}

	recommendations, err := parseRecommendations(report.RecommendedActionsJSON)
	if err != nil {
		return CreateFromDiagnosisResult{}, err
	}
	selected := map[string]struct{}{}
	for _, actionType := range req.SelectedActionTypes {
		selected[strings.TrimSpace(actionType)] = struct{}{}
	}

	result := CreateFromDiagnosisResult{}
	for _, recommendation := range recommendations {
		if len(selected) > 0 {
			if _, ok := selected[recommendation.Type]; !ok {
				result.Skipped = append(result.Skipped, SkippedAction{ActionType: recommendation.Type, Reason: "not selected"})
				continue
			}
		}
		input, skipReason := recommendation.toCreateInput(report)
		if skipReason != "" {
			result.Skipped = append(result.Skipped, SkippedAction{ActionType: recommendation.Type, Reason: skipReason})
			continue
		}
		normalized, err := s.policy.ValidateCreate(input)
		if err != nil {
			result.Skipped = append(result.Skipped, SkippedAction{ActionType: recommendation.Type, Reason: err.Error()})
			continue
		}
		existing, ok, err := s.repo.FindPendingByDedupeKey(ctx, normalized.DedupeKey)
		if err != nil {
			return result, err
		}
		if ok {
			result.Created = append(result.Created, toActionResponse(existing))
			continue
		}
		created, err := s.repo.CreatePending(ctx, normalized)
		if err != nil {
			return result, err
		}
		if err := s.audit(ctx, actorOrCopilot(actor), "action.create_pending", "pending_action", strconv.FormatUint(created.ID, 10), json.RawMessage(created.ParamsJSON), model.AuditResultSuccess, ""); err != nil {
			return result, err
		}
		s.observeAction("create_pending", model.AuditResultSuccess)
		s.notifyPending(ctx, created)
		result.Created = append(result.Created, toActionResponse(created))
	}
	return result, nil
}

func (s *Service) ListActions(ctx context.Context, filter ListFilter) ([]ActionResponse, int64, ListFilter, error) {
	if err := s.requireReady(); err != nil {
		return nil, 0, filter, err
	}
	actions, total, normalized, err := s.repo.ListActions(ctx, filter)
	if err != nil {
		return nil, 0, normalized, err
	}
	responses := make([]ActionResponse, 0, len(actions))
	for _, action := range actions {
		responses = append(responses, toActionResponse(action))
	}
	return responses, total, normalized, nil
}

func (s *Service) GetAction(ctx context.Context, id uint64) (ActionResponse, error) {
	if err := s.requireReady(); err != nil {
		return ActionResponse{}, err
	}
	action, err := s.repo.GetAction(ctx, id)
	return toActionResponse(action), err
}

func (s *Service) Approve(ctx context.Context, id uint64, req ApproveRequest, actor Actor) (ActionResponse, error) {
	if err := s.requireReady(); err != nil {
		return ActionResponse{}, err
	}
	saved, err := s.repo.TransitionAction(ctx, id, EventApprove, func(action *model.PendingAction) error {
		if err := s.policy.ValidateApprove(*action, actor); err != nil {
			return err
		}
		now := time.Now()
		action.ApprovedBy = actor.ID
		action.ApprovedAt = &now
		action.ErrorMessage = ""
		return nil
	})
	if err != nil {
		if auditErr := s.audit(ctx, actor, "action.approve", "pending_action", strconv.FormatUint(id, 10), mustMarshal(req), model.AuditResultDenied, err.Error()); auditErr != nil {
			return ActionResponse{}, auditErr
		}
		s.observeAction("approve", model.AuditResultDenied)
		return ActionResponse{}, err
	}
	if err := s.audit(ctx, actor, "action.approve", "pending_action", strconv.FormatUint(id, 10), mustMarshal(req), model.AuditResultSuccess, ""); err != nil {
		return ActionResponse{}, err
	}
	s.observeAction("approve", model.AuditResultSuccess)
	s.notifyStatus(ctx, saved, model.AuditResultSuccess)
	return toActionResponse(saved), nil
}

func (s *Service) Reject(ctx context.Context, id uint64, req RejectRequest, actor Actor) (ActionResponse, error) {
	if err := s.requireReady(); err != nil {
		return ActionResponse{}, err
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" || len(req.Reason) > 500 {
		return ActionResponse{}, fmt.Errorf("%w: reason must be 1-500 characters", ErrInvalidAction)
	}
	saved, err := s.repo.TransitionAction(ctx, id, EventReject, func(action *model.PendingAction) error {
		if err := s.policy.ValidateReject(*action, actor); err != nil {
			return err
		}
		action.ErrorMessage = req.Reason
		return nil
	})
	if err != nil {
		if auditErr := s.audit(ctx, actor, "action.reject", "pending_action", strconv.FormatUint(id, 10), mustMarshal(req), model.AuditResultDenied, err.Error()); auditErr != nil {
			return ActionResponse{}, auditErr
		}
		s.observeAction("reject", model.AuditResultDenied)
		return ActionResponse{}, err
	}
	if err := s.audit(ctx, actor, "action.reject", "pending_action", strconv.FormatUint(id, 10), mustMarshal(req), model.AuditResultSuccess, ""); err != nil {
		return ActionResponse{}, err
	}
	s.observeAction("reject", model.AuditResultSuccess)
	s.notifyStatus(ctx, saved, model.AuditResultSuccess)
	return toActionResponse(saved), nil
}

func (s *Service) Execute(ctx context.Context, id uint64, actor Actor) (ActionResponse, error) {
	if err := s.requireReady(); err != nil {
		return ActionResponse{}, err
	}
	saved, err := s.repo.TransitionAction(ctx, id, EventExecute, func(action *model.PendingAction) error {
		if err := s.policy.ValidateExecute(*action, actor); err != nil {
			return err
		}
		action.ExecutedBy = actor.ID
		return nil
	})
	if err != nil {
		if auditErr := s.audit(ctx, actor, "action.execute", "pending_action", strconv.FormatUint(id, 10), json.RawMessage(`{}`), model.AuditResultDenied, err.Error()); auditErr != nil {
			return ActionResponse{}, auditErr
		}
		s.observeAction("execute", model.AuditResultDenied)
		return ActionResponse{}, err
	}

	s.notifyStatus(ctx, saved, "executing")

	execCtx, cancel := context.WithTimeout(ctx, s.executionTimeout)
	defer cancel()
	start := time.Now()
	result, execErr := s.executeK8s(execCtx, saved)
	executionSeconds := time.Since(start).Seconds()
	now := time.Now()
	event := EventExecuteSuccess
	if execErr != nil {
		event = EventExecuteFailure
	}
	final, saveErr := s.repo.TransitionAction(ctx, saved.ID, event, func(action *model.PendingAction) error {
		action.ExecutedAt = &now
		if execErr != nil {
			action.ErrorMessage = sanitizeError(execErr)
			action.ResultJSON = "{}"
			return nil
		}
		action.ErrorMessage = ""
		action.ResultJSON = string(mustMarshal(result))
		return nil
	})
	if saveErr != nil {
		return toActionResponse(saved), saveErr
	}
	auditResult := model.AuditResultSuccess
	errorMessage := ""
	if execErr != nil {
		auditResult = model.AuditResultFailure
		errorMessage = sanitizeError(execErr)
	}
	s.observeAction("execute", auditResult)
	s.observeExecution(saved.ActionType, final.Status, executionSeconds)
	if err := s.audit(ctx, actor, "action.execute", "pending_action", strconv.FormatUint(id, 10), json.RawMessage(final.ParamsJSON), auditResult, errorMessage); err != nil {
		return toActionResponse(final), err
	}
	s.publishOperationEvent(ctx, final, actor)
	s.notifyStatus(ctx, final, auditResult)
	return toActionResponse(final), execErr
}

func (s *Service) ListAuditLogs(ctx context.Context, filter ListFilter) ([]AuditLogResponse, int64, ListFilter, error) {
	if err := s.requireReady(); err != nil {
		return nil, 0, filter, err
	}
	logs, total, normalized, err := s.repo.ListAuditLogs(ctx, filter)
	if err != nil {
		return nil, 0, normalized, err
	}
	responses := make([]AuditLogResponse, 0, len(logs))
	for _, log := range logs {
		responses = append(responses, toAuditLogResponse(log))
	}
	return responses, total, normalized, nil
}

func (s *Service) GetAuditLog(ctx context.Context, id uint64) (AuditLogResponse, error) {
	if err := s.requireReady(); err != nil {
		return AuditLogResponse{}, err
	}
	log, err := s.repo.GetAuditLog(ctx, id)
	return toAuditLogResponse(log), err
}

func (s *Service) requireReady() error {
	if s == nil || s.repo == nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) audit(ctx context.Context, actor Actor, actionName, resourceType, resourceID string, request json.RawMessage, result, errorMessage string) error {
	return s.repo.RecordAudit(ctx, AuditEntry{
		Actor:        actorName(actor),
		ActorRole:    actorRole(actor),
		Action:       actionName,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Request:      request,
		Result:       result,
		ErrorMessage: errorMessage,
	})
}

func (s *Service) executeK8s(ctx context.Context, action model.PendingAction) (ActionResult, error) {
	params, err := decodeObject(json.RawMessage(action.ParamsJSON))
	if err != nil {
		return ActionResult{}, err
	}
	switch action.ActionType {
	case ActionTypeRestartDeployment:
		return s.executor.RestartDeployment(ctx, action.Namespace, action.TargetName)
	case ActionTypeScaleDeployment:
		replicas, err := intParam(params, "replicas")
		if err != nil {
			return ActionResult{}, err
		}
		return s.executor.ScaleDeployment(ctx, action.Namespace, action.TargetName, int32(replicas))
	default:
		return ActionResult{}, fmt.Errorf("%w: unsupported action type", ErrInvalidAction)
	}
}

func (s *Service) publishOperationEvent(ctx context.Context, action model.PendingAction, actor Actor) {
	if !s.operationEventsEnabled || s.events == nil {
		return
	}
	event := OperationEvent{
		Type:       "action",
		ActionID:   action.ID,
		ActionType: action.ActionType,
		Target:     action.Namespace + "/" + action.TargetName,
		Status:     action.Status,
		Actor:      actorName(actor),
		TraceID:    TraceIDFromContext(ctx),
		OccurredAt: time.Now(),
	}
	if err := s.events.SendOperationEvent(event); err != nil {
		zap.L().Warn("publish operation event failed", zap.Error(err), zap.Uint64("action_id", action.ID))
	}
}

func (s *Service) notifyPending(ctx context.Context, action model.PendingAction) {
	if s.statusPushEnabled && s.notifier != nil {
		s.notifier.NotifyActionPending(ctx, action)
	}
}

func (s *Service) notifyStatus(ctx context.Context, action model.PendingAction, result string) {
	if s.statusPushEnabled && s.notifier != nil {
		s.notifier.NotifyActionStatus(ctx, action, result)
	}
}

func (s *Service) observeAction(operation, result string) {
	if s.observer != nil {
		s.observer.ObserveActionEvent(operation, result)
	}
}

func (s *Service) observeExecution(actionType, status string, seconds float64) {
	if s.observer != nil {
		s.observer.ObserveActionExecutionDuration(actionType, status, seconds)
	}
}

func parseRecommendations(raw string) ([]rawRecommendation, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var recommendations []rawRecommendation
	if err := json.Unmarshal([]byte(trimmed), &recommendations); err != nil {
		return nil, fmt.Errorf("%w: recommended_actions_json is invalid", ErrInvalidAction)
	}
	return recommendations, nil
}

type rawRecommendation struct {
	Type             string          `json:"type"`
	Risk             string          `json:"risk"`
	RequiresApproval bool            `json:"requires_approval"`
	TargetKind       string          `json:"target_kind"`
	TargetName       string          `json:"target_name"`
	Namespace        string          `json:"namespace"`
	Params           json.RawMessage `json:"params"`
}

func (r rawRecommendation) toCreateInput(report model.DiagnosisReport) (CreateActionInput, string) {
	if !r.RequiresApproval {
		return CreateActionInput{}, "read-only action does not require approval"
	}
	if r.Risk == RiskLevelHigh {
		return CreateActionInput{}, "high risk action is not allowed"
	}
	if r.Risk != RiskLevelMedium {
		return CreateActionInput{}, "only medium risk actions require approval"
	}
	namespace := strings.TrimSpace(r.Namespace)
	if namespace == "" {
		namespace = report.Namespace
	}
	targetName := strings.TrimSpace(r.TargetName)
	if targetName == "" {
		targetName = report.TargetName
	}
	targetKind := strings.TrimSpace(r.TargetKind)
	if targetKind == "" {
		targetKind = report.TargetKind
	}
	if targetKind == "" {
		targetKind = TargetKindK8sDeployment
	}
	params := r.Params
	if len(strings.TrimSpace(string(params))) == 0 {
		params = json.RawMessage(`{}`)
	}
	return CreateActionInput{
		DiagnosisReportID: report.ID,
		ActionType:        r.Type,
		TargetKind:        targetKind,
		TargetName:        targetName,
		Namespace:         namespace,
		RiskLevel:         r.Risk,
		RequestedBy:       "ai-copilot",
		Params:            params,
	}, ""
}

func toActionResponse(action model.PendingAction) ActionResponse {
	return ActionResponse{
		ID:                action.ID,
		DiagnosisReportID: action.DiagnosisReportID,
		ActionType:        action.ActionType,
		TargetKind:        action.TargetKind,
		TargetName:        action.TargetName,
		Namespace:         action.Namespace,
		Params:            rawJSON(action.ParamsJSON),
		RiskLevel:         action.RiskLevel,
		Status:            action.Status,
		RequestedBy:       action.RequestedBy,
		ApprovedBy:        action.ApprovedBy,
		ExecutedBy:        action.ExecutedBy,
		Result:            rawJSON(action.ResultJSON),
		ErrorMessage:      action.ErrorMessage,
		CreatedAt:         action.CreatedAt,
		ApprovedAt:        action.ApprovedAt,
		ExecutedAt:        action.ExecutedAt,
		UpdatedAt:         action.UpdatedAt,
	}
}

func toAuditLogResponse(log model.AuditLog) AuditLogResponse {
	return AuditLogResponse{
		ID:           log.ID,
		Actor:        log.Actor,
		ActorRole:    log.ActorRole,
		Action:       log.Action,
		ResourceType: log.ResourceType,
		ResourceID:   log.ResourceID,
		Request:      rawJSON(log.RequestJSON),
		Result:       log.Result,
		ErrorMessage: log.ErrorMessage,
		TraceID:      log.TraceID,
		CreatedAt:    log.CreatedAt,
	}
}

func rawJSON(value string) json.RawMessage {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return json.RawMessage(value)
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 500 {
		return message[:500]
	}
	return message
}

func actorOrCopilot(actor Actor) Actor {
	if actor.Username == "" {
		return Actor{Username: "ai-copilot", Role: "system"}
	}
	return actor
}

func actorName(actor Actor) string {
	if actor.Username == "" {
		return "system"
	}
	return actor.Username
}

func actorRole(actor Actor) string {
	if actor.Role == "" {
		return "system"
	}
	return actor.Role
}
