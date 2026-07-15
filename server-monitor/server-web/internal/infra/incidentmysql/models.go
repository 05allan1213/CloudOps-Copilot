package incidentmysql

import (
	"encoding/json"
	"time"
)

type incidentRow struct {
	ID                uint64 `gorm:"primaryKey"`
	PublicID          string
	Fingerprint       string
	CorrelationKey    string
	Cluster           string
	Namespace         string
	ServiceName       string
	Environment       string
	TargetKind        string
	TargetName        string
	Severity          string
	Status            string
	Summary           string
	FirstSeenAt       time.Time
	LastSeenAt        time.Time
	ResolvedAt        *time.Time
	CurrentAgentRunID *uint64
	Version           uint64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (incidentRow) TableName() string { return "incidents" }

type signalRow struct {
	ID              uint64 `gorm:"primaryKey"`
	IncidentID      *uint64
	Source          string
	SourceEventID   string
	Fingerprint     string
	Status          string
	Severity        string
	Cluster         string
	Namespace       string
	ServiceName     string
	Environment     string
	TargetKind      string
	TargetName      string
	Category        string
	OccurredAt      time.Time
	ReceivedAt      time.Time
	Summary         string
	LabelsJSON      json.RawMessage `gorm:"column:labels_json;type:json"`
	AnnotationsJSON json.RawMessage `gorm:"column:annotations_json;type:json"`
	RawPayload      json.RawMessage `gorm:"type:json"`
	CreatedAt       time.Time
}

func (signalRow) TableName() string { return "incident_signals" }

type timelineRow struct {
	ID           uint64 `gorm:"primaryKey"`
	IncidentID   uint64
	EventType    string
	ActorType    string
	ActorID      string
	Summary      string
	MetadataJSON json.RawMessage `gorm:"column:metadata_json;type:json"`
	OccurredAt   time.Time
	CreatedAt    time.Time
}

func (timelineRow) TableName() string { return "incident_events" }

type evidenceRow struct {
	ID             uint64 `gorm:"primaryKey"`
	PublicID       string
	IncidentID     uint64
	AgentRunID     *uint64
	Type           string
	Source         string
	ToolName       string
	ResourceRef    string
	TimeRangeJSON  json.RawMessage `gorm:"column:time_range_json;type:json"`
	QueryText      string          `gorm:"column:query_text"`
	Summary        string
	FactsJSON      json.RawMessage `gorm:"column:facts_json;type:json"`
	ResultHash     string
	RawRef         string
	RedactionJSON  json.RawMessage `gorm:"column:redaction_json;type:json"`
	Truncated      bool
	Valid          bool
	IdempotencyKey *string
	CollectedAt    time.Time
	CreatedAt      time.Time
}

func (evidenceRow) TableName() string { return "evidence_items" }

type agentRunRow struct {
	ID                      uint64 `gorm:"primaryKey"`
	PublicID                string
	IncidentID              uint64
	IdempotencyKey          *string
	Attempt                 int
	Status                  string
	Objective               string
	Model                   string
	PromptVersion           string
	MaxSteps                int
	UsedSteps               int
	MaxToolCalls            int
	UsedToolCalls           int
	MaxModelCalls           int
	UsedModelCalls          int
	TokenBudget             int64
	InputTokens             int64
	OutputTokens            int64
	MaxEvidenceItems        int
	UsedEvidenceItems       int
	MaxRuntimeMS            int64
	ToolTimeoutMS           int64
	MaxEvidenceBytes        int
	MaxCheckpointBytes      int
	MaxStepRetries          int
	CurrentCheckpoint       json.RawMessage `gorm:"type:json"`
	CheckpointVersion       uint64
	CheckpointSchemaVersion int
	CheckpointHash          string
	LeaseOwner              string
	LeaseExpiresAt          *time.Time
	HeartbeatAt             *time.Time
	CancelRequestedAt       *time.Time
	FinalDiagnosis          json.RawMessage `gorm:"type:json"`
	FailureCode             string
	FailureSummary          string
	StartedAt               *time.Time
	CompletedAt             *time.Time
	DeadlineAt              *time.Time
	RowVersion              uint64
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (agentRunRow) TableName() string { return "agent_runs" }

type agentStepRow struct {
	ID               uint64 `gorm:"primaryKey"`
	PublicID         string
	AgentRunID       uint64
	Sequence         int
	StepType         string
	ShortReason      string
	SelectedTool     string
	ArgumentsJSON    json.RawMessage `gorm:"column:arguments_json;type:json"`
	ArgumentsHash    string
	ResultSummary    string
	ResultRef        string
	EvidencePublicID string
	Status           string
	RetryCount       int
	DurationMS       int64
	InputTokens      int64
	OutputTokens     int64
	ErrorCode        string
	StartedAt        *time.Time
	FinishedAt       *time.Time
	CreatedAt        time.Time
}

func (agentStepRow) TableName() string { return "agent_steps" }

type outboxRow struct {
	ID            uint64 `gorm:"primaryKey"`
	EventID       string
	AggregateType string
	AggregateID   string
	EventType     string
	SchemaVersion int
	PayloadJSON   json.RawMessage `gorm:"column:payload_json;type:json"`
	OccurredAt    time.Time
	PublishedAt   *time.Time
	Attempts      int
	LastError     string
	CreatedAt     time.Time
}

func (outboxRow) TableName() string { return "outbox_events" }

type correlationLockRow struct {
	CorrelationKey string `gorm:"primaryKey"`
	TouchedAt      time.Time
}

func (correlationLockRow) TableName() string { return "incident_correlation_locks" }
