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
		return NormalizedAction{}, fmt.Errorf("%w: 不允许敏感动作参数", ErrInvalidAction)
	}
	if input.TargetKind != TargetKindK8sDeployment {
		return NormalizedAction{}, fmt.Errorf("%w: 不支持的目标类型", ErrInvalidAction)
	}
	if err := validateK8sName("namespace", input.Namespace, 128); err != nil {
		return NormalizedAction{}, err
	}
	if err := validateK8sName("target_name", input.TargetName, 256); err != nil {
		return NormalizedAction{}, err
	}
	if input.RiskLevel != "" && input.RiskLevel != RiskLevelMedium {
		return NormalizedAction{}, fmt.Errorf("%w: 仅中等风险动作可以待审批", ErrInvalidAction)
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
			return NormalizedAction{}, fmt.Errorf("%w: 副本数须在 1-%d 范围内", ErrInvalidAction, p.maxReplicas)
		}
		normalizedParams["replicas"] = replicas
	default:
		return NormalizedAction{}, fmt.Errorf("%w: 不允许的动作类型", ErrInvalidAction)
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
		return fmt.Errorf("%w: 无法从状态 %s 审批动作", ErrInvalidAction, action.Status)
	}
	return nil
}

func (p *Policy) ValidateReject(action model.PendingAction, actor Actor) error {
	if actor.Role != "admin" {
		return ErrForbidden
	}
	if _, ok := CanTransition(action.Status, EventReject); !ok {
		return fmt.Errorf("%w: 无法从状态 %s 拒绝动作", ErrInvalidAction, action.Status)
	}
	return nil
}

func (p *Policy) ValidateExecute(action model.PendingAction, actor Actor) error {
	if actor.Role != "admin" && actor.Role != "system" {
		return ErrForbidden
	}
	if _, ok := CanTransition(action.Status, EventExecute); !ok {
		return fmt.Errorf("%w: 无法从状态 %s 执行动作", ErrInvalidAction, action.Status)
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
		return nil, fmt.Errorf("%w: params_json 必须为 JSON 对象", ErrInvalidAction)
	}
	if object == nil {
		return nil, fmt.Errorf("%w: params_json 必须为 JSON 对象", ErrInvalidAction)
	}
	return object, nil
}

func intParam(params map[string]interface{}, key string) (int, error) {
	value, ok := params[key]
	if !ok {
		return 0, fmt.Errorf("%w: %s 不能为空", ErrInvalidAction, key)
	}
	switch typed := value.(type) {
	case float64:
		asInt := int(typed)
		if typed != float64(asInt) {
			return 0, fmt.Errorf("%w: %s 必须为整数", ErrInvalidAction, key)
		}
		return asInt, nil
	case int:
		return typed, nil
	default:
		return 0, fmt.Errorf("%w: %s 必须为整数", ErrInvalidAction, key)
	}
}

func validateK8sName(field, value string, maxLen int) error {
	if value == "" {
		return fmt.Errorf("%w: %s 不能为空", ErrInvalidAction, field)
	}
	if len(value) > maxLen {
		return fmt.Errorf("%w: %s 过长", ErrInvalidAction, field)
	}
	if !k8sNamePattern.MatchString(value) {
		return fmt.Errorf("%w: %s 包含无效字符", ErrInvalidAction, field)
	}
	return nil
}

func dedupeKey(action NormalizedAction) string {
	source := fmt.Sprintf("%d|%s|%s|%s|%s", action.DiagnosisReportID, action.ActionType, action.Namespace, action.TargetName, string(action.Params))
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}
