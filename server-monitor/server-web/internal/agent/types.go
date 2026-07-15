package agent

import (
	"encoding/json"
	"time"

	domain "server-web/internal/incident"
)

type RunStatus = domain.AgentRunStatus

const (
	RunPending   = domain.AgentRunPending
	RunRunning   = domain.AgentRunRunning
	RunCompleted = domain.AgentRunCompleted
	RunFailed    = domain.AgentRunFailed
	RunCancelled = domain.AgentRunCancelled
)

type StepStatus = domain.AgentStepStatus

const (
	StepPending   = domain.AgentStepPending
	StepRunning   = domain.AgentStepRunning
	StepCompleted = domain.AgentStepCompleted
	StepFailed    = domain.AgentStepFailed
	StepCancelled = domain.AgentStepCancelled
)

// Node is a stable logical Agent graph node identifier.
type Node string

const (
	NodeLoadIncident       Node = "load_incident"
	NodeBuildObjective     Node = "build_objective"
	NodePlanInvestigation  Node = "plan_investigation"
	NodeSelectAction       Node = "select_action"
	NodeExecuteTool        Node = "execute_tool"
	NodePersistObservation Node = "persist_observation"
	NodeEvaluateCoverage   Node = "evaluate_coverage"
	NodeReplan             Node = "replan"
	NodeProduceDiagnosis   Node = "produce_diagnosis"
	NodeValidateDiagnosis  Node = "validate_diagnosis"
	NodeCompleteRun        Node = "complete_run"
	NodeRetryableFailure   Node = "retryable_failure"
	NodeTerminalFailure    Node = "terminal_failure"
	NodeBudgetExceeded     Node = "budget_exceeded"
	NodeCancelled          Node = "cancelled"
	NodeEnd                Node = "end"
)

// Limits is the immutable budget snapshot persisted with a Run.
type Limits struct {
	MaxSteps          int           `json:"max_steps"`
	MaxToolCalls      int           `json:"max_tool_calls"`
	MaxModelCalls     int           `json:"max_model_calls"`
	TokenBudget       int64         `json:"token_budget"`
	MaxEvidenceItems  int           `json:"max_evidence_items"`
	MaxRuntime        time.Duration `json:"max_runtime"`
	ToolTimeout       time.Duration `json:"tool_timeout"`
	MaxEvidenceBytes  int           `json:"max_evidence_bytes"`
	MaxCheckpointSize int           `json:"max_checkpoint_bytes"`
	MaxStepRetries    int           `json:"max_step_retries"`
}

// Usage is charged deterministically by semantic work.
type Usage struct {
	Steps        int   `json:"steps"`
	ToolCalls    int   `json:"tool_calls"`
	ModelCalls   int   `json:"model_calls"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	Evidence     int   `json:"evidence"`
}

func (u Usage) TotalTokens() int64 { return u.InputTokens + u.OutputTokens }

// CanCharge returns the first budget that would be exceeded.
func (u Usage) CanCharge(delta Usage, limits Limits) error {
	switch {
	case u.Steps+delta.Steps > limits.MaxSteps:
		return NewRuntimeError(ErrorBudgetExceeded, "step budget exhausted", ErrBudgetExceeded)
	case u.ToolCalls+delta.ToolCalls > limits.MaxToolCalls:
		return NewRuntimeError(ErrorBudgetExceeded, "tool-call budget exhausted", ErrBudgetExceeded)
	case u.ModelCalls+delta.ModelCalls > limits.MaxModelCalls:
		return NewRuntimeError(ErrorBudgetExceeded, "model-call budget exhausted", ErrBudgetExceeded)
	case u.TotalTokens()+delta.TotalTokens() > limits.TokenBudget:
		return NewRuntimeError(ErrorBudgetExceeded, "token budget exhausted", ErrBudgetExceeded)
	case u.Evidence+delta.Evidence > limits.MaxEvidenceItems:
		return NewRuntimeError(ErrorBudgetExceeded, "evidence budget exhausted", ErrBudgetExceeded)
	default:
		return nil
	}
}

func (u *Usage) Charge(delta Usage) {
	u.Steps += delta.Steps
	u.ToolCalls += delta.ToolCalls
	u.ModelCalls += delta.ModelCalls
	u.InputTokens += delta.InputTokens
	u.OutputTokens += delta.OutputTokens
	u.Evidence += delta.Evidence
}

// Run is the complete private runtime record. HTTP DTOs expose only a subset.
type Run struct {
	ID                uint64
	PublicID          string
	IncidentID        uint64
	IncidentPublicID  string
	IdempotencyKey    string
	Attempt           int
	Status            RunStatus
	Objective         string
	Model             string
	PromptVersion     string
	Limits            Limits
	Usage             Usage
	Checkpoint        json.RawMessage
	CheckpointVersion uint64
	CheckpointSchema  int
	CheckpointHash    string
	LeaseOwner        string
	LeaseExpiresAt    *time.Time
	HeartbeatAt       *time.Time
	CancelRequestedAt *time.Time
	FailureCode       ErrorCode
	FailureSummary    string
	FinalDiagnosis    json.RawMessage
	RowVersion        uint64
	StartedAt         *time.Time
	FinishedAt        *time.Time
	DeadlineAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Step is one durable graph-node execution summary.
type Step struct {
	ID               uint64
	PublicID         string
	RunID            uint64
	Sequence         int
	Node             Node
	Status           StepStatus
	ShortReason      string
	SelectedTool     string
	Arguments        json.RawMessage
	ArgumentsHash    string
	ResultSummary    string
	ResultRef        string
	EvidencePublicID string
	RetryCount       int
	DurationMS       int64
	InputTokens      int64
	OutputTokens     int64
	ErrorCode        ErrorCode
	StartedAt        *time.Time
	FinishedAt       *time.Time
	CreatedAt        time.Time
}

// IncidentContext is the bounded Incident input available to the model.
type IncidentContext struct {
	PublicID    string    `json:"incident_id"`
	Status      string    `json:"status"`
	Severity    string    `json:"severity"`
	Cluster     string    `json:"cluster"`
	Namespace   string    `json:"namespace"`
	ServiceName string    `json:"service_name"`
	TargetKind  string    `json:"target_kind"`
	TargetName  string    `json:"target_name"`
	Summary     string    `json:"summary"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

type Plan struct {
	Summary   string   `json:"summary"`
	Questions []string `json:"questions"`
}

type Action struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Reason    string          `json:"reason"`
}

// Observation is a bounded in-checkpoint summary, never the full raw result.
type Observation struct {
	Tool             string          `json:"tool"`
	ArgumentsHash    string          `json:"arguments_hash"`
	Summary          string          `json:"summary"`
	Facts            json.RawMessage `json:"facts"`
	ResultHash       string          `json:"result_hash"`
	EvidencePublicID string          `json:"evidence_id,omitempty"`
	Truncated        bool            `json:"truncated"`
	Valid            bool            `json:"valid"`
	ObservedAt       time.Time       `json:"observed_at"`
}

type Coverage struct {
	Sufficient       bool     `json:"sufficient"`
	Reason           string   `json:"reason"`
	MissingQuestions []string `json:"missing_questions"`
}

type Claim struct {
	Statement   string   `json:"statement"`
	EvidenceIDs []string `json:"evidence_ids"`
	Strong      bool     `json:"strong"`
}

type Hypothesis struct {
	Statement   string   `json:"statement"`
	Confidence  float64  `json:"confidence"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type Diagnosis struct {
	Summary                string       `json:"summary"`
	Hypotheses             []Hypothesis `json:"hypotheses"`
	ConfirmedFacts         []Claim      `json:"confirmed_facts"`
	Unknowns               []string     `json:"unknowns"`
	Confidence             float64      `json:"confidence"`
	AffectedResources      []string     `json:"affected_resources"`
	RecommendedNextActions []string     `json:"recommended_next_actions"`
	Degraded               bool         `json:"degraded"`
	BudgetSummary          string       `json:"budget_summary"`
	CoverageSummary        string       `json:"coverage_summary"`
}

// GraphState is the versioned serializable checkpoint payload.
type GraphState struct {
	SchemaVersion      int             `json:"schema_version"`
	RunPublicID        string          `json:"run_id"`
	IncidentPublicID   string          `json:"incident_id"`
	NextNode           Node            `json:"next_node"`
	RetryNode          Node            `json:"retry_node,omitempty"`
	Objective          string          `json:"objective,omitempty"`
	Incident           IncidentContext `json:"incident"`
	Plan               Plan            `json:"plan"`
	CurrentAction      Action          `json:"current_action"`
	PendingObservation Observation     `json:"pending_observation"`
	Observations       []Observation   `json:"observations"`
	EvidenceIDs        []string        `json:"evidence_ids"`
	Coverage           Coverage        `json:"coverage"`
	Diagnosis          Diagnosis       `json:"diagnosis"`
	ValidationErrors   []string        `json:"validation_errors,omitempty"`
	CorrectionAttempts int             `json:"correction_attempts"`
	CurrentRetryCount  int             `json:"current_retry_count"`
	Usage              Usage           `json:"usage"`
	Limits             Limits          `json:"limits"`
	RowVersion         uint64          `json:"row_version"`
	CheckpointVersion  uint64          `json:"checkpoint_version"`
	LastErrorCode      ErrorCode       `json:"last_error_code,omitempty"`
	LastErrorSummary   string          `json:"last_error_summary,omitempty"`
	StartedAt          time.Time       `json:"started_at"`
	DeadlineAt         time.Time       `json:"deadline_at"`
	LastCompletedNode  Node            `json:"last_completed_node,omitempty"`
	LastStepPublicID   string          `json:"last_step_id,omitempty"`
}

// ModelUsage is returned by provider adapters.
type ModelUsage struct {
	InputTokens  int64
	OutputTokens int64
}
