package action

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"server-web/internal/model"
)

var (
	ErrInvalidAction = errors.New("invalid action")
	ErrForbidden     = errors.New("action forbidden")
)

var k8sNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

type PolicyConfig struct {
	MaxReplicas int
}

type Policy struct {
	maxReplicas int
}

func NewPolicy(cfg PolicyConfig) *Policy {
	maxReplicas := cfg.MaxReplicas
	if maxReplicas <= 0 {
		maxReplicas = 10
	}
	return &Policy{maxReplicas: maxReplicas}
}

func (p *Policy) ValidateCreate(input CreateActionInput) (NormalizedAction, error) {
	if p == nil {
		p = NewPolicy(PolicyConfig{})
	}
	input.ActionType = strings.TrimSpace(input.ActionType)
	input.TargetKind = strings.TrimSpace(input.TargetKind)
	input.Namespace = strings.TrimSpace(input.Namespace)
	input.TargetName = strings.TrimSpace(input.TargetName)
	input.RiskLevel = strings.TrimSpace(input.RiskLevel)
	if input.RequestedBy == "" {
		input.RequestedBy = "ai-copilot"
	}
	if input.TargetKind == "" {
		input.TargetKind = TargetKindK8sDeployment
	}
	if input.Params == nil || len(strings.TrimSpace(string(input.Params))) == 0 {
		input.Params = json.RawMessage(`{}`)
	}
	if containsSensitiveKey(input.Params) {
		return NormalizedAction{}, fmt.Errorf("%w: sensitive action params are not allowed", ErrInvalidAction)
	}
	if input.TargetKind != TargetKindK8sDeployment {
		return NormalizedAction{}, fmt.Errorf("%w: unsupported target kind", ErrInvalidAction)
	}
	if err := validateK8sName("namespace", input.Namespace, 128); err != nil {
		return NormalizedAction{}, err
	}
	if err := validateK8sName("target_name", input.TargetName, 256); err != nil {
		return NormalizedAction{}, err
	}
	if input.RiskLevel != "" && input.RiskLevel != RiskLevelMedium {
		return NormalizedAction{}, fmt.Errorf("%w: only medium risk actions can be pending", ErrInvalidAction)
	}

	params, err := decodeObject(input.Params)
	if err != nil {
		return NormalizedAction{}, err
	}

	normalizedParams := map[string]interface{}{
		"namespace": input.Namespace,
		"name":      input.TargetName,
	}
	switch input.ActionType {
	case ActionTypeRestartDeployment:
	case ActionTypeScaleDeployment:
		replicas, err := intParam(params, "replicas")
		if err != nil {
			return NormalizedAction{}, err
		}
		if replicas < 1 || replicas > p.maxReplicas {
			return NormalizedAction{}, fmt.Errorf("%w: replicas must be in range 1-%d", ErrInvalidAction, p.maxReplicas)
		}
		normalizedParams["replicas"] = replicas
	default:
		return NormalizedAction{}, fmt.Errorf("%w: action type is not allowed", ErrInvalidAction)
	}

	paramsJSON := mustMarshal(normalizedParams)
	normalized := NormalizedAction{
		DiagnosisReportID: input.DiagnosisReportID,
		ActionType:        input.ActionType,
		TargetKind:        TargetKindK8sDeployment,
		TargetName:        input.TargetName,
		Namespace:         input.Namespace,
		RiskLevel:         RiskLevelMedium,
		RequestedBy:       input.RequestedBy,
		Params:            paramsJSON,
	}
	normalized.DedupeKey = dedupeKey(normalized)
	return normalized, nil
}

func (p *Policy) ValidateApprove(action model.PendingAction, actor Actor) error {
	if actor.Role != "admin" {
		return ErrForbidden
	}
	if _, ok := CanTransition(action.Status, EventApprove); !ok {
		return fmt.Errorf("%w: cannot approve action from status %s", ErrInvalidAction, action.Status)
	}
	return nil
}

func (p *Policy) ValidateReject(action model.PendingAction, actor Actor) error {
	if actor.Role != "admin" {
		return ErrForbidden
	}
	if _, ok := CanTransition(action.Status, EventReject); !ok {
		return fmt.Errorf("%w: cannot reject action from status %s", ErrInvalidAction, action.Status)
	}
	return nil
}

func (p *Policy) ValidateExecute(action model.PendingAction, actor Actor) error {
	if actor.Role != "admin" && actor.Role != "system" {
		return ErrForbidden
	}
	if _, ok := CanTransition(action.Status, EventExecute); !ok {
		return fmt.Errorf("%w: cannot execute action from status %s", ErrInvalidAction, action.Status)
	}
	_, err := p.ValidateCreate(CreateActionInput{
		DiagnosisReportID: action.DiagnosisReportID,
		ActionType:        action.ActionType,
		TargetKind:        action.TargetKind,
		TargetName:        action.TargetName,
		Namespace:         action.Namespace,
		RiskLevel:         action.RiskLevel,
		RequestedBy:       action.RequestedBy,
		Params:            json.RawMessage(action.ParamsJSON),
	})
	return err
}

func CanTransition(from, event string) (string, bool) {
	switch from {
	case model.ActionStatusPending:
		switch event {
		case EventApprove:
			return model.ActionStatusApproved, true
		case EventReject:
			return model.ActionStatusRejected, true
		case EventCancel:
			return model.ActionStatusCancelled, true
		}
	case model.ActionStatusApproved:
		switch event {
		case EventExecute:
			return model.ActionStatusExecuting, true
		case EventExecuteFailure:
			return model.ActionStatusFailed, true
		}
	case model.ActionStatusExecuting:
		switch event {
		case EventExecuteSuccess:
			return model.ActionStatusExecuted, true
		case EventExecuteFailure:
			return model.ActionStatusFailed, true
		}
	}
	return "", false
}

func decodeObject(raw json.RawMessage) (map[string]interface{}, error) {
	var object map[string]interface{}
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("%w: params_json must be a json object", ErrInvalidAction)
	}
	if object == nil {
		return nil, fmt.Errorf("%w: params_json must be a json object", ErrInvalidAction)
	}
	return object, nil
}

func intParam(params map[string]interface{}, key string) (int, error) {
	value, ok := params[key]
	if !ok {
		return 0, fmt.Errorf("%w: %s is required", ErrInvalidAction, key)
	}
	switch typed := value.(type) {
	case float64:
		asInt := int(typed)
		if typed != float64(asInt) {
			return 0, fmt.Errorf("%w: %s must be an integer", ErrInvalidAction, key)
		}
		return asInt, nil
	case int:
		return typed, nil
	default:
		return 0, fmt.Errorf("%w: %s must be an integer", ErrInvalidAction, key)
	}
}

func validateK8sName(field, value string, maxLen int) error {
	if value == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidAction, field)
	}
	if len(value) > maxLen {
		return fmt.Errorf("%w: %s is too long", ErrInvalidAction, field)
	}
	if !k8sNamePattern.MatchString(value) {
		return fmt.Errorf("%w: %s contains invalid characters", ErrInvalidAction, field)
	}
	return nil
}

func dedupeKey(action NormalizedAction) string {
	source := fmt.Sprintf("%d|%s|%s|%s|%s", action.DiagnosisReportID, action.ActionType, action.Namespace, action.TargetName, string(action.Params))
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}
