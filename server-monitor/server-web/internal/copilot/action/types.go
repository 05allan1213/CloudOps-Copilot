package action

import (
	"encoding/json"
	"time"

	"server-web/internal/model"
)

const (
	ActionTypeRestartDeployment = "k8s.restart_deployment"
	ActionTypeScaleDeployment   = "k8s.scale_deployment"

	TargetKindK8sDeployment = "k8s_deployment"

	RiskLevelLow    = model.ActionRiskLow
	RiskLevelMedium = model.ActionRiskMedium
	RiskLevelHigh   = model.ActionRiskHigh

	EventApprove        = "approve"
	EventReject         = "reject"
	EventCancel         = "cancel"
	EventExecute        = "execute"
	EventExecuteSuccess = "execute_success"
	EventExecuteFailure = "execute_failure"
)

type Actor struct {
	ID       uint64
	Username string
	Role     string
}

type CreateActionInput struct {
	DiagnosisReportID uint64          `json:"diagnosis_report_id,omitempty"`
	ActionType        string          `json:"action_type"`
	TargetKind        string          `json:"target_kind"`
	TargetName        string          `json:"target_name"`
	Namespace         string          `json:"namespace"`
	RiskLevel         string          `json:"risk_level"`
	RequestedBy       string          `json:"requested_by,omitempty"`
	Params            json.RawMessage `json:"params,omitempty"`
}

type NormalizedAction struct {
	DiagnosisReportID uint64
	ActionType        string
	TargetKind        string
	TargetName        string
	Namespace         string
	RiskLevel         string
	RequestedBy       string
	Params            json.RawMessage
	DedupeKey         string
}

type ListFilter struct {
	Status     string
	RiskLevel  string
	ActionType string
	Actor      string
	Result     string
	Page       int
	PageSize   int
}

type CreateFromDiagnosisRequest struct {
	Source              string   `json:"source"`
	SelectedActionTypes []string `json:"selected_action_types"`
}

type CreateActionRequest struct {
	DiagnosisReportID uint64          `json:"diagnosis_report_id,omitempty"`
	ActionType        string          `json:"action_type"`
	TargetKind        string          `json:"target_kind,omitempty"`
	TargetName        string          `json:"target_name"`
	Namespace         string          `json:"namespace"`
	RiskLevel         string          `json:"risk_level,omitempty"`
	Params            json.RawMessage `json:"params,omitempty"`
}

type CreateActionRequestDoc struct {
	DiagnosisReportID uint64                 `json:"diagnosis_report_id,omitempty"`
	ActionType        string                 `json:"action_type"`
	TargetKind        string                 `json:"target_kind,omitempty"`
	TargetName        string                 `json:"target_name"`
	Namespace         string                 `json:"namespace"`
	RiskLevel         string                 `json:"risk_level,omitempty"`
	Params            map[string]interface{} `json:"params,omitempty"`
}

type CreateFromDiagnosisResult struct {
	Created []ActionResponse `json:"created"`
	Skipped []SkippedAction  `json:"skipped"`
}

type SkippedAction struct {
	ActionType string `json:"action_type"`
	Reason     string `json:"reason"`
}

type ApproveRequest struct {
	Comment string `json:"comment"`
}

type RejectRequest struct {
	Reason string `json:"reason"`
}

type ExecuteRequest struct {
	Confirm bool `json:"confirm"`
}

type ActionResponse struct {
	ID                uint64          `json:"id"`
	DiagnosisReportID uint64          `json:"diagnosis_report_id"`
	ActionType        string          `json:"action_type"`
	TargetKind        string          `json:"target_kind"`
	TargetName        string          `json:"target_name"`
	Namespace         string          `json:"namespace"`
	Params            json.RawMessage `json:"params,omitempty"`
	RiskLevel         string          `json:"risk_level"`
	Status            string          `json:"status"`
	RequestedBy       string          `json:"requested_by"`
	ApprovedBy        uint64          `json:"approved_by,omitempty"`
	ExecutedBy        uint64          `json:"executed_by,omitempty"`
	Result            json.RawMessage `json:"result,omitempty"`
	ErrorMessage      string          `json:"error_message,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	ApprovedAt        *time.Time      `json:"approved_at,omitempty"`
	ExecutedAt        *time.Time      `json:"executed_at,omitempty"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type AuditLogResponse struct {
	ID           uint64          `json:"id"`
	Actor        string          `json:"actor"`
	ActorRole    string          `json:"actor_role"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Request      json.RawMessage `json:"request,omitempty"`
	Result       string          `json:"result"`
	ErrorMessage string          `json:"error_message,omitempty"`
	TraceID      string          `json:"trace_id,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

type ActionResult struct {
	ActionType    string `json:"action_type"`
	Target        string `json:"target"`
	Replicas      *int   `json:"replicas,omitempty"`
	OldReplicas   *int   `json:"old_replicas,omitempty"`
	NewReplicas   *int   `json:"new_replicas,omitempty"`
	ReadyReplicas *int   `json:"ready_replicas,omitempty"`
	OldAnnotation string `json:"old_annotation,omitempty"`
	NewAnnotation string `json:"new_annotation,omitempty"`
	Message       string `json:"message,omitempty"`
}
