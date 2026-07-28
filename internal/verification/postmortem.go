package verification

import "time"

type ClassifiedFact struct {
	Classification string   `json:"classification"`
	Summary        string   `json:"summary"`
	EvidenceIDs    []string `json:"evidence_ids,omitempty"`
}

type PostmortemCheckFact struct {
	CheckID    string      `json:"check_id"`
	Type       CheckType   `json:"type"`
	Status     CheckStatus `json:"status"`
	Required   bool        `json:"required"`
	TemplateID string      `json:"template_id,omitempty"`
	Reason     string      `json:"reason,omitempty"`
}

type TimelineFact struct {
	EventType  string    `json:"event_type"`
	Summary    string    `json:"summary"`
	OccurredAt time.Time `json:"occurred_at"`
}

type Postmortem struct {
	PublicID                string                `json:"id"`
	IncidentPublicID        string                `json:"incident_id"`
	VerificationRunPublicID string                `json:"verification_id"`
	Title                   string                `json:"title"`
	ImpactSummary           string                `json:"impact_summary"`
	DetectedAt              time.Time             `json:"detected_at"`
	MitigatedAt             *time.Time            `json:"mitigated_at,omitempty"`
	ResolvedAt              time.Time             `json:"resolved_at"`
	DurationSeconds         int64                 `json:"duration_seconds"`
	Service                 string                `json:"service"`
	Workload                string                `json:"workload"`
	Environment             string                `json:"environment"`
	TriggeringSignal        ClassifiedFact        `json:"triggering_signal"`
	ChangeCorrelation       ClassifiedFact        `json:"change_correlation"`
	RootCause               ClassifiedFact        `json:"root_cause"`
	RemediationSummary      ClassifiedFact        `json:"remediation_summary"`
	ApprovalSummary         ClassifiedFact        `json:"approval_summary"`
	DeliveryRevision        string                `json:"delivery_revision"`
	VerificationSummary     string                `json:"verification_summary"`
	Checks                  []PostmortemCheckFact `json:"checks"`
	Timeline                []TimelineFact        `json:"timeline"`
	FollowUpActions         []string              `json:"follow_up_actions"`
	GeneratedAt             time.Time             `json:"generated_at"`
	GenerationVersion       int                   `json:"generation_version"`
	CreatedAt               time.Time             `json:"created_at"`
	UpdatedAt               time.Time             `json:"updated_at"`
}
