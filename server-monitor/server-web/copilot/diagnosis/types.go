package diagnosis

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	k8sreader "server-web/copilot/k8s"
)

const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"

	TriggerManual = "manual"
	TriggerChat   = "chat"
	TriggerAuto   = "auto"

	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"

	TargetKindK8sDeployment = "k8s_deployment"
	TargetKindK8sPod        = "k8s_pod"
	TargetKindK8sNode       = "k8s_node"
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
	Runbooks         []RunbookEvidence `json:"runbooks"`
	K8s              K8sEvidence       `json:"k8s,omitempty"`
	CollectionErrors []CollectionError `json:"collection_errors"`
	CollectedAt      time.Time         `json:"collected_at"`
}

type K8sEvidence struct {
	Enabled     bool                          `json:"enabled"`
	Namespace   string                        `json:"namespace,omitempty"`
	TargetKind  string                        `json:"target_kind,omitempty"`
	TargetName  string                        `json:"target_name,omitempty"`
	Pods        []k8sreader.PodSummary        `json:"pods,omitempty"`
	Deployments []k8sreader.DeploymentSummary `json:"deployments,omitempty"`
	Services    []k8sreader.ServiceSummary    `json:"services,omitempty"`
	Nodes       []k8sreader.NodeSummary       `json:"nodes,omitempty"`
	Events      []k8sreader.EventSummary      `json:"events,omitempty"`
	Logs        []k8sreader.LogSnippet        `json:"logs,omitempty"`
	Errors      []CollectionError             `json:"errors,omitempty"`
	CollectedAt time.Time                     `json:"collected_at,omitempty"`
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

type RunbookEvidence struct {
	Title           string    `json:"title"`
	File            string    `json:"file"`
	Score           float64   `json:"score"`
	MatchedAlerts   []string  `json:"matched_alerts,omitempty"`
	MatchedKeywords []string  `json:"matched_keywords,omitempty"`
	MatchedMetrics  []string  `json:"matched_metrics,omitempty"`
	Snippet         string    `json:"snippet"`
	Source          string    `json:"source"`
	CollectedAt     time.Time `json:"collected_at"`
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

func (h *RootCauseHypothesis) UnmarshalJSON(data []byte) error {
	var raw struct {
		Cause      string          `json:"cause"`
		Confidence json.RawMessage `json:"confidence"`
		Evidence   json.RawMessage `json:"evidence"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	h.Cause = raw.Cause
	h.Evidence = nil
	if len(raw.Evidence) > 0 && string(raw.Evidence) != "null" {
		var evidence []string
		if err := json.Unmarshal(raw.Evidence, &evidence); err == nil {
			h.Evidence = evidence
		} else {
			var single string
			if err := json.Unmarshal(raw.Evidence, &single); err != nil {
				return fmt.Errorf("evidence must be string or string array")
			}
			if strings.TrimSpace(single) != "" {
				h.Evidence = []string{single}
			}
		}
	}
	h.Confidence = ""
	if len(raw.Confidence) == 0 || string(raw.Confidence) == "null" {
		return nil
	}
	var confidence string
	if err := json.Unmarshal(raw.Confidence, &confidence); err == nil {
		h.Confidence = confidence
		return nil
	}
	value := strings.TrimSpace(string(raw.Confidence))
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("confidence must be string or number")
	}
	h.Confidence = strconv.FormatFloat(parsed, 'f', -1, 64)
	return nil
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
	Status      string
	TriggerType string
	Page        int
	PageSize    int
}

type ListResult struct {
	Items    []ReportResponse `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

type FeedbackBrief struct {
	Rating  string `json:"rating"`
	Comment string `json:"comment,omitempty"`
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
	MyFeedback         *FeedbackBrief  `json:"my_feedback,omitempty"`
}
