package agent

import (
	"context"
	"encoding/json"
	"time"
)

type CreateRunRequest struct {
	IncidentPublicID string
	IdempotencyKey   string
	Model            string
	PromptVersion    string
	Limits           Limits
	Checkpoint       json.RawMessage
	At               time.Time
}

type Page struct {
	Items    []Run
	Total    int64
	Page     int
	PageSize int
}

type StepStart struct {
	Node          Node
	Reason        string
	SelectedTool  string
	Arguments     json.RawMessage
	ArgumentsHash string
	Budgeted      bool
	At            time.Time
}

type StepFinish struct {
	Status           StepStatus
	ResultSummary    string
	ResultRef        string
	EvidencePublicID string
	RetryCount       int
	DurationMS       int64
	ErrorCode        ErrorCode
	Usage            Usage
	Checkpoint       json.RawMessage
	CheckpointHash   string
	CheckpointSchema int
	At               time.Time
}

type EvidenceRecord struct {
	PublicID       string
	IncidentID     uint64
	RunID          uint64
	SourceType     string
	ToolName       string
	ResourceScope  string
	TimeRange      json.RawMessage
	Query          string
	Summary        string
	Facts          json.RawMessage
	ResultHash     string
	RawRef         string
	Redaction      json.RawMessage
	Truncated      bool
	Valid          bool
	IdempotencyKey string
	CollectedAt    time.Time
}

// Store is the durable Agent application port implemented by MySQL.
type Store interface {
	CreateRun(context.Context, CreateRunRequest) (*Run, error)
	GetRun(context.Context, string) (*Run, error)
	ListRunsByIncident(context.Context, string, int, int) (Page, error)
	ListSteps(context.Context, string, int) ([]Step, error)
	ListEvidence(context.Context, string, int) ([]EvidenceRecord, error)
	RequestCancel(context.Context, string, time.Time) error
	ClaimNext(context.Context, string, time.Time, time.Duration) (*Run, error)
	Heartbeat(context.Context, uint64, string, time.Time, time.Duration) error
	BeginStep(context.Context, *Run, StepStart) (*Step, error)
	FinishStep(context.Context, *Run, *Step, StepFinish) error
	PersistEvidence(context.Context, *Run, *Step, EvidenceRecord, StepFinish) (*EvidenceRecord, error)
	FinishRun(context.Context, *Run, RunStatus, Diagnosis, ErrorCode, string, time.Time) error
	LoadIncident(context.Context, uint64) (IncidentContext, error)
}

// Model is a provider-neutral structured decision port.
type Model interface {
	Plan(context.Context, IncidentContext, string) (Plan, ModelUsage, error)
	SelectAction(context.Context, GraphState, []string) (Action, ModelUsage, error)
	EvaluateCoverage(context.Context, GraphState) (Coverage, ModelUsage, error)
	Diagnose(context.Context, GraphState) (Diagnosis, ModelUsage, error)
}

type ToolResult struct {
	Summary    string
	Facts      json.RawMessage
	ResultHash string
	RawRef     string
	Redaction  json.RawMessage
	Truncated  bool
	Valid      bool
}

// ToolExecutor exposes only the Agent read-only allowlist.
type ToolExecutor interface {
	AllowedTools() []string
	Execute(context.Context, string, json.RawMessage, time.Duration, int) (ToolResult, error)
}
