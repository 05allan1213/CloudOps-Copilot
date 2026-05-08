package diagnosis

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"

	TriggerManual = "manual"
	TriggerChat   = "chat"

	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

var (
	ErrInvalidRequest = errors.New("invalid diagnosis request")
	ErrNotFound       = errors.New("diagnosis target not found")
	ErrConflict       = errors.New("diagnosis target is ambiguous")
	ErrForbidden      = errors.New("diagnosis report forbidden")
	ErrUnavailable    = errors.New("diagnosis service unavailable")
)

type User struct {
	ID       uint64
	Username string
	Role     string
}

type Request struct {
	Fingerprint    string `json:"fingerprint,omitempty"`
	AlertHistoryID uint64 `json:"alert_history_id,omitempty"`
	AlertName      string `json:"alert_name,omitempty"`
	Instance       string `json:"instance,omitempty"`
	TriggerType    string `json:"trigger_type,omitempty"`
}

type AlertContext struct {
	AlertHistoryID uint64            `json:"alert_history_id,omitempty"`
	Fingerprint    string            `json:"fingerprint"`
	AlertName      string            `json:"alert_name"`
	Instance       string            `json:"instance"`
	TargetKind     string            `json:"target_kind"`
	TargetName     string            `json:"target_name"`
	Namespace      string            `json:"namespace,omitempty"`
	Severity       string            `json:"severity"`
	Status         string            `json:"status"`
	Summary        string            `json:"summary,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	StartsAt       time.Time         `json:"starts_at"`
	EndsAt         *time.Time        `json:"ends_at,omitempty"`
	Source         string            `json:"source"`
	CollectedAt    time.Time         `json:"collected_at"`
}

type DiagnosisCandidate struct {
	AlertHistoryID uint64    `json:"alert_history_id,omitempty"`
	Fingerprint    string    `json:"fingerprint"`
	AlertName      string    `json:"alert_name"`
	Instance       string    `json:"instance"`
	Severity       string    `json:"severity"`
	Status         string    `json:"status"`
	FiredAt        time.Time `json:"fired_at"`
	Source         string    `json:"source"`
}

type ConflictError struct {
	Candidates []DiagnosisCandidate
}

func (e ConflictError) Error() string {
	return ErrConflict.Error()
}

func (e ConflictError) Unwrap() error {
	return ErrConflict
}

type EvidenceBundle struct {
	AlertContext     AlertContext      `json:"alert_context"`
	ActiveAlerts     []AlertContext    `json:"active_alerts"`
	Metrics          []MetricEvidence  `json:"metrics"`
	History          []HistoryEvidence `json:"history"`
	Runbooks         []json.RawMessage `json:"runbooks"`
	CollectionErrors []CollectionError `json:"collection_errors"`
	CollectedAt      time.Time         `json:"collected_at"`
}

type MetricEvidence struct {
	Name        string    `json:"name"`
	Source      string    `json:"source"`
	Window      string    `json:"window"`
	Avg         float64   `json:"avg"`
	Max         float64   `json:"max"`
	Last        float64   `json:"last"`
	Trend       string    `json:"trend"`
	CollectedAt time.Time `json:"collected_at,omitempty"`
}

type HistoryEvidence struct {
	AlertHistoryID uint64     `json:"alert_history_id,omitempty"`
	Fingerprint    string     `json:"fingerprint"`
	AlertName      string     `json:"alert_name"`
	Instance       string     `json:"instance"`
	Severity       string     `json:"severity"`
	Status         string     `json:"status"`
	Summary        string     `json:"summary,omitempty"`
	FiredAt        time.Time  `json:"fired_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
}

type CollectionError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

type RuleAnalysis struct {
	Summary         string       `json:"summary"`
	Confidence      float64      `json:"confidence"`
	ConfidenceLevel string       `json:"confidence_level"`
	Results         []RuleResult `json:"results"`
	NextSteps       []string     `json:"next_steps"`
}

type RuleResult struct {
	Rule         string   `json:"rule"`
	Passed       bool     `json:"passed"`
	Detail       string   `json:"detail"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type DiagnosisSummary struct {
	Summary             string                `json:"summary"`
	SeverityAssessment  string                `json:"severity_assessment"`
	RootCauseHypotheses []RootCauseHypothesis `json:"root_cause_hypotheses"`
	RecommendedActions  []RecommendedAction   `json:"recommended_actions"`
	NextSteps           []string              `json:"next_steps"`
}

type RootCauseHypothesis struct {
	Cause      string   `json:"cause"`
	Confidence string   `json:"confidence"`
	Evidence   []string `json:"evidence"`
}

type RecommendedAction struct {
	Type             string `json:"type"`
	Description      string `json:"description"`
	Risk             string `json:"risk"`
	RequiresApproval bool   `json:"requires_approval"`
}

type LLMMetadata struct {
	Model      string
	PromptHash string
}

type ListFilter struct {
	Status   string
	Page     int
	PageSize int
}

type ListResult struct {
	Items    []ReportResponse `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

type ReportResponse struct {
	ID                 uint64          `json:"id"`
	AlertHistoryID     uint64          `json:"alert_history_id"`
	Fingerprint        string          `json:"fingerprint"`
	AlertName          string          `json:"alert_name"`
	TargetKind         string          `json:"target_kind"`
	TargetName         string          `json:"target_name"`
	Namespace          string          `json:"namespace"`
	Severity           string          `json:"severity"`
	Status             string          `json:"status"`
	Summary            string          `json:"summary"`
	RootCause          string          `json:"root_cause"`
	Evidence           json.RawMessage `json:"evidence,omitempty"`
	Runbooks           json.RawMessage `json:"runbooks,omitempty"`
	RecommendedActions json.RawMessage `json:"recommended_actions,omitempty"`
	RuleAnalysis       json.RawMessage `json:"rule_analysis,omitempty"`
	Confidence         float64         `json:"confidence"`
	ConfidenceLevel    string          `json:"confidence_level"`
	LLMPromptHash      string          `json:"llm_prompt_hash"`
	LLMModel           string          `json:"llm_model"`
	TriggerType        string          `json:"trigger_type"`
	CreatedBy          uint64          `json:"created_by"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}
