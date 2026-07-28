package api

import (
	"encoding/json"
	"time"
)

// IncidentOperationalContextView keeps the resource and time identity used by
// every Incident Context Link. It is a projection, not a second source of truth.
type IncidentOperationalContextView struct {
	OperationalScopeID string                  `json:"operational_scope_id,omitempty"`
	Cluster            string                  `json:"cluster"`
	Environment        string                  `json:"environment"`
	Namespace          string                  `json:"namespace"`
	Service            string                  `json:"service"`
	Resource           IncidentResourceRefView `json:"resource"`
	TimeRange          IncidentTimeRangeView   `json:"time_range"`
}

type IncidentResourceRefView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type IncidentTimeRangeView struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// IncidentContextLinkView follows the shared internal Context Link contract.
// Labels remain presentation-owned so all consumers use the same identity.
type IncidentContextLinkView struct {
	Workspace          string            `json:"workspace"`
	Path               string            `json:"path"`
	Query              map[string]string `json:"query"`
	OperationalScopeID string            `json:"operational_scope_id"`
	External           bool              `json:"external"`
}

type IncidentAttentionView struct {
	Required   bool   `json:"required"`
	ReasonCode string `json:"reason_code,omitempty"`
	Stage      string `json:"stage"`
}

type IncidentRecoveryView struct {
	State                    string     `json:"state"`
	VerificationAttempts     uint64     `json:"verification_attempts"`
	FailedVerificationCount  uint64     `json:"failed_verification_count"`
	LatestVerificationID     string     `json:"latest_verification_id,omitempty"`
	LatestVerificationStatus string     `json:"latest_verification_status,omitempty"`
	CommonWindowStartedAt    *time.Time `json:"common_window_started_at,omitempty"`
	CommonWindowCompletedAt  *time.Time `json:"common_window_completed_at,omitempty"`
	ResolutionReportID       string     `json:"resolution_report_id,omitempty"`
	CanClose                 bool       `json:"can_close"`
}

type IncidentAlertRelationView struct {
	ID                      string                  `json:"id"`
	Cycle                   uint64                  `json:"cycle"`
	AlertID                 string                  `json:"alert_id"`
	Status                  string                  `json:"status"`
	Severity                string                  `json:"severity"`
	Summary                 string                  `json:"summary"`
	Category                string                  `json:"category"`
	Source                  string                  `json:"source"`
	Cluster                 string                  `json:"cluster"`
	Environment             string                  `json:"environment"`
	Namespace               string                  `json:"namespace"`
	Service                 string                  `json:"service"`
	TargetKind              string                  `json:"target_kind"`
	TargetName              string                  `json:"target_name"`
	FirstSeenAt             time.Time               `json:"first_seen_at"`
	LastSeenAt              time.Time               `json:"last_seen_at"`
	ResolvedAt              *time.Time              `json:"resolved_at,omitempty"`
	Provenance              string                  `json:"provenance"`
	ConfigurationRevisionID string                  `json:"configuration_revision_id,omitempty"`
	EscalationPolicyID      string                  `json:"escalation_policy_id,omitempty"`
	ContextLink             IncidentContextLinkView `json:"context_link"`
	MigratedLegacy          bool                    `json:"migrated_legacy"`
	MigratedLegacyContext   bool                    `json:"migrated_legacy_context"`
	CreatedAt               time.Time               `json:"created_at"`
}

type IncidentTimelineEventView struct {
	ID                    string          `json:"id"`
	Cycle                 uint64          `json:"cycle"`
	Type                  string          `json:"type"`
	SourceStatus          string          `json:"source_status,omitempty"`
	TargetStatus          string          `json:"target_status,omitempty"`
	ReasonCode            string          `json:"reason_code,omitempty"`
	ActorType             string          `json:"actor_type"`
	ActorID               string          `json:"actor_id"`
	Summary               string          `json:"summary"`
	Metadata              json.RawMessage `json:"metadata"`
	OccurredAt            time.Time       `json:"occurred_at"`
	MigratedLegacy        bool            `json:"migrated_legacy"`
	MigratedLegacyContext bool            `json:"migrated_legacy_context"`
}

type IncidentEvidenceView struct {
	ID                    string                  `json:"id"`
	Cycle                 uint64                  `json:"cycle"`
	Type                  string                  `json:"type"`
	Source                string                  `json:"source"`
	ProducerType          string                  `json:"producer_type,omitempty"`
	ProducerID            string                  `json:"producer_id,omitempty"`
	ProducerVersion       string                  `json:"producer_version,omitempty"`
	ToolName              string                  `json:"tool_name,omitempty"`
	ResourceRef           string                  `json:"resource_ref"`
	TimeRange             json.RawMessage         `json:"time_range,omitempty"`
	QueryText             string                  `json:"query_text,omitempty"`
	Summary               string                  `json:"summary"`
	ContentHash           string                  `json:"content_hash"`
	Provenance            json.RawMessage         `json:"provenance,omitempty"`
	Valid                 bool                    `json:"valid"`
	Truncated             bool                    `json:"truncated"`
	CollectedAt           time.Time               `json:"collected_at"`
	ObservedAt            *time.Time              `json:"observed_at,omitempty"`
	ContextLink           IncidentContextLinkView `json:"context_link"`
	MigratedLegacy        bool                    `json:"migrated_legacy"`
	MigratedLegacyContext bool                    `json:"migrated_legacy_context"`
}

type IncidentInvestigationView struct {
	ID                    string                  `json:"id"`
	Cycle                 uint64                  `json:"cycle"`
	Status                string                  `json:"status"`
	Version               uint64                  `json:"version"`
	Objective             string                  `json:"objective"`
	Outcome               string                  `json:"outcome,omitempty"`
	FailureCode           string                  `json:"failure_code,omitempty"`
	FailureSummary        string                  `json:"failure_summary,omitempty"`
	ModelProvider         string                  `json:"model_provider,omitempty"`
	ActualModel           string                  `json:"actual_model,omitempty"`
	PromptVersion         string                  `json:"prompt_version"`
	UsedSteps             uint64                  `json:"used_steps"`
	MaxSteps              uint64                  `json:"max_steps"`
	StartedAt             *time.Time              `json:"started_at,omitempty"`
	CompletedAt           *time.Time              `json:"completed_at,omitempty"`
	CreatedAt             time.Time               `json:"created_at"`
	UpdatedAt             time.Time               `json:"updated_at"`
	ContextLink           IncidentContextLinkView `json:"context_link"`
	MigratedLegacy        bool                    `json:"migrated_legacy"`
	MigratedLegacyContext bool                    `json:"migrated_legacy_context"`
}

type IncidentDecisionView struct {
	Cycle             uint64                   `json:"cycle"`
	Kind              string                   `json:"kind"`
	Status            string                   `json:"status"`
	Summary           string                   `json:"summary"`
	InvestigationID   string                   `json:"investigation_id,omitempty"`
	RemediationPlanID string                   `json:"remediation_plan_id,omitempty"`
	DecisionID        string                   `json:"decision_id,omitempty"`
	Decision          string                   `json:"decision,omitempty"`
	Reason            string                   `json:"reason,omitempty"`
	Actor             string                   `json:"actor,omitempty"`
	DeliveryID        string                   `json:"delivery_id,omitempty"`
	VerificationID    string                   `json:"verification_id,omitempty"`
	ContextLink       *IncidentContextLinkView `json:"context_link,omitempty"`
	DecidedAt         *time.Time               `json:"decided_at,omitempty"`
}
