package incident

import (
	"encoding/json"
	"time"
)

// Status is the durable Incident lifecycle state.
type Status string

const (
	StatusDetected            Status = "DETECTED"
	StatusCorrelating         Status = "CORRELATING"
	StatusDiagnosing          Status = "DIAGNOSING"
	StatusDiagnosisCompleted  Status = "DIAGNOSIS_COMPLETED"
	StatusPlanningRemediation Status = "PLANNING_REMEDIATION"
	StatusAwaitingApproval    Status = "AWAITING_APPROVAL"
	StatusApplyingChange      Status = "APPLYING_CHANGE"
	StatusVerifying           Status = "VERIFYING"
	StatusResolved            Status = "RESOLVED"
	StatusFailed              Status = "FAILED"
	StatusClosedNoAction      Status = "CLOSED_NO_ACTION"
)

// AllStatuses returns every frozen Incident state.
func AllStatuses() []Status {
	return []Status{
		StatusDetected, StatusCorrelating, StatusDiagnosing, StatusDiagnosisCompleted,
		StatusPlanningRemediation, StatusAwaitingApproval, StatusApplyingChange,
		StatusVerifying, StatusResolved, StatusFailed, StatusClosedNoAction,
	}
}

// Severity is the normalized Incident severity.
type Severity string

const (
	SeverityUnknown  Severity = "unknown"
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// SignalStatus is the normalized lifecycle status of an incoming signal.
type SignalStatus string

const (
	SignalStatusFiring   SignalStatus = "firing"
	SignalStatusResolved SignalStatus = "resolved"
)

// EventType is a typed Incident timeline event.
type EventType string

const (
	EventSignalReceived    EventType = "signal_received"
	EventIncidentCreated   EventType = "incident_created"
	EventIncidentUpdated   EventType = "incident_updated"
	EventStatusChanged     EventType = "status_changed"
	EventIncidentResolved  EventType = "incident_resolved"
	EventIncidentReopened  EventType = "incident_reopened"
	EventAgentRunCreated   EventType = "agent_run_created"
	EventAgentStepRecorded EventType = "agent_step_recorded"
)

// ActorType identifies the source of a timeline event.
type ActorType string

const (
	ActorSystem ActorType = "system"
	ActorSource ActorType = "source"
	ActorUser   ActorType = "user"
	ActorAgent  ActorType = "agent"
)

// Incident is the canonical aggregate for a service failure lifecycle.
type Incident struct {
	ID                uint64
	PublicID          string
	Fingerprint       string
	CorrelationKey    string
	Cluster           string
	Namespace         string
	ServiceName       string
	Environment       string
	TargetKind        string
	TargetName        string
	Severity          Severity
	Status            Status
	Summary           string
	FirstSeenAt       time.Time
	LastSeenAt        time.Time
	ResolvedAt        *time.Time
	CurrentAgentRunID *uint64
	Version           uint64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Signal is a bounded, normalized external signal.
type Signal struct {
	ID            uint64
	IncidentID    *uint64
	Source        string
	SourceEventID string
	Fingerprint   string
	Status        SignalStatus
	Severity      Severity
	Cluster       string
	Namespace     string
	ServiceName   string
	Environment   string
	TargetKind    string
	TargetName    string
	Category      string
	OccurredAt    time.Time
	ReceivedAt    time.Time
	Summary       string
	Labels        json.RawMessage
	Annotations   json.RawMessage
	RawPayload    json.RawMessage
	CreatedAt     time.Time
}

// TimelineEvent records a domain fact, not an application log message.
type TimelineEvent struct {
	ID         uint64
	IncidentID uint64
	EventType  EventType
	ActorType  ActorType
	ActorID    string
	Summary    string
	Metadata   json.RawMessage
	OccurredAt time.Time
	CreatedAt  time.Time
}

// EvidenceItem stores bounded facts and external references for later Agent use.
type EvidenceItem struct {
	ID          uint64
	PublicID    string
	IncidentID  uint64
	AgentRunID  *uint64
	Type        string
	Source      string
	ToolName    string
	ResourceRef string
	TimeRange   json.RawMessage
	Query       string
	Summary     string
	Facts       json.RawMessage
	ResultHash  string
	RawRef      string
	Redaction   json.RawMessage
	Truncated   bool
	Valid       bool
	CollectedAt time.Time
	CreatedAt   time.Time
}

// AgentRunStatus is the durable execution status contract for a future Agent run.
type AgentRunStatus string

const (
	AgentRunPending   AgentRunStatus = "PENDING"
	AgentRunRunning   AgentRunStatus = "RUNNING"
	AgentRunCompleted AgentRunStatus = "COMPLETED"
	AgentRunFailed    AgentRunStatus = "FAILED"
	AgentRunCancelled AgentRunStatus = "CANCELLED"
)

// AgentRun is the bounded, auditable state contract for future Agent execution.
type AgentRun struct {
	ID                uint64
	PublicID          string
	IncidentID        uint64
	Status            AgentRunStatus
	Model             string
	PromptVersion     string
	MaxSteps          int
	UsedSteps         int
	InputTokens       int64
	OutputTokens      int64
	CurrentCheckpoint json.RawMessage
	FinalDiagnosis    json.RawMessage
	FailureCode       string
	StartedAt         *time.Time
	CompletedAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// AgentStepStatus is the durable status of one future Agent step.
type AgentStepStatus string

const (
	AgentStepPending   AgentStepStatus = "PENDING"
	AgentStepRunning   AgentStepStatus = "RUNNING"
	AgentStepCompleted AgentStepStatus = "COMPLETED"
	AgentStepFailed    AgentStepStatus = "FAILED"
	AgentStepCancelled AgentStepStatus = "CANCELLED"
)

// AgentStep stores an auditable summary and never private model chain-of-thought.
type AgentStep struct {
	ID            uint64
	PublicID      string
	AgentRunID    uint64
	Sequence      int
	StepType      string
	ShortReason   string
	SelectedTool  string
	Arguments     json.RawMessage
	ResultSummary string
	ResultRef     string
	Status        AgentStepStatus
	DurationMS    int64
	InputTokens   int64
	OutputTokens  int64
	CreatedAt     time.Time
}

// OutboxEvent is a durable event waiting for a future relay.
type OutboxEvent struct {
	ID            uint64
	EventID       string
	AggregateType string
	AggregateID   string
	EventType     string
	SchemaVersion int
	Payload       json.RawMessage
	OccurredAt    time.Time
	PublishedAt   *time.Time
	Attempts      int
	LastError     string
	CreatedAt     time.Time
}

// ListFilter controls bounded Incident queries.
type ListFilter struct {
	Status      Status
	Severity    Severity
	Cluster     string
	Namespace   string
	ServiceName string
	Environment string
	Workload    string
	Search      string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Page        int
	PageSize    int
}

// Page is a bounded Incident list result.
type Page struct {
	Items    []Incident
	Total    int64
	Page     int
	PageSize int
}
