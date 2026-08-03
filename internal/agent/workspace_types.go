package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

const (
	WorkspacePromptVersion = "agent-workspace/v2"
	WorkspaceToolVersion   = "cloudops-bounded-tools/v1"
)

type WorkspaceSubject string

const (
	WorkspaceSubjectAlert        WorkspaceSubject = "alert"
	WorkspaceSubjectIncident     WorkspaceSubject = "incident"
	WorkspaceSubjectConsultation WorkspaceSubject = "consultation"
)

type WorkspaceOutcome string

const (
	WorkspaceOutcomeDiagnosed    WorkspaceOutcome = "diagnosed"
	WorkspaceOutcomeInsufficient WorkspaceOutcome = "insufficient"
	WorkspaceOutcomeCancelled    WorkspaceOutcome = "cancelled"
	WorkspaceOutcomeFailed       WorkspaceOutcome = "failed"
)

type WorkspaceRun struct {
	ID                      string             `json:"id"`
	SubjectType             WorkspaceSubject   `json:"subject_type"`
	AlertID                 string             `json:"alert_id,omitempty"`
	IncidentID              string             `json:"incident_id,omitempty"`
	ConsultationID          string             `json:"consultation_id,omitempty"`
	ConfigurationRevisionID string             `json:"configuration_revision_id"`
	ContextSnapshotID       string             `json:"context_snapshot_id"`
	ScenarioID              string             `json:"scenario_id,omitempty"`
	Status                  RunStatus          `json:"status"`
	Outcome                 WorkspaceOutcome   `json:"outcome,omitempty"`
	Uncertainty             string             `json:"uncertainty"`
	Objective               string             `json:"objective"`
	Answer                  string             `json:"answer,omitempty"`
	ModelProvider           string             `json:"model_provider,omitempty"`
	ActualModel             string             `json:"actual_model,omitempty"`
	PromptVersion           string             `json:"prompt_version"`
	ToolSchemaVersion       string             `json:"tool_schema_version,omitempty"`
	FailureCode             string             `json:"failure_code,omitempty"`
	FailureSummary          string             `json:"failure_summary,omitempty"`
	CancelRequestedAt       *time.Time         `json:"cancel_requested_at,omitempty"`
	StartedAt               *time.Time         `json:"started_at,omitempty"`
	CompletedAt             *time.Time         `json:"completed_at,omitempty"`
	CreatedAt               time.Time          `json:"created_at"`
	UpdatedAt               time.Time          `json:"updated_at"`
	EvidenceCount           int                `json:"evidence_count"`
	Steps                   []WorkspaceStep    `json:"steps"`
	Evidence                []EvidenceCitation `json:"evidence_citations"`
	Guidance                []GuidanceCitation `json:"guidance_citations"`
	ActionCards             []ActionCard       `json:"action_cards"`
	OperationPlans          []OperationPlan    `json:"operation_plans"`
}

type WorkspaceStep struct {
	ID            string          `json:"id"`
	Sequence      int             `json:"sequence"`
	Type          string          `json:"type"`
	Tool          string          `json:"tool"`
	Target        string          `json:"target"`
	Scope         json.RawMessage `json:"scope"`
	Status        StepStatus      `json:"status"`
	ResultSummary string          `json:"result_summary,omitempty"`
	EvidenceID    string          `json:"evidence_id,omitempty"`
	DurationMS    int64           `json:"duration_ms"`
	ErrorCode     string          `json:"error_code,omitempty"`
	StartedAt     *time.Time      `json:"started_at,omitempty"`
	FinishedAt    *time.Time      `json:"finished_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

type EvidenceCitation struct {
	ID                      string                    `json:"id"`
	EvidenceID              string                    `json:"evidence_id"`
	Use                     string                    `json:"use"`
	Source                  string                    `json:"source"`
	Summary                 string                    `json:"summary"`
	QueryExecutionID        string                    `json:"query_execution_id,omitempty"`
	ConfigurationRevisionID string                    `json:"configuration_revision_id"`
	ResourceRef             string                    `json:"resource_ref"`
	TimeRange               json.RawMessage           `json:"time_range"`
	ObservedAt              *time.Time                `json:"observed_at,omitempty"`
	CollectedAt             time.Time                 `json:"collected_at"`
	ContentHash             string                    `json:"content_hash"`
	Facts                   json.RawMessage           `json:"facts,omitempty"`
	TrustAxes               json.RawMessage           `json:"trust_axes,omitempty"`
	Scope                   settings.OperationalScope `json:"scope,omitempty"`
}

type GuidanceCitation struct {
	ID              string     `json:"id"`
	Type            string     `json:"type"`
	KnowledgeItemID string     `json:"knowledge_item_id,omitempty"`
	RevisionID      string     `json:"revision_id"`
	Revision        uint64     `json:"revision"`
	Title           string     `json:"title"`
	Source          string     `json:"source"`
	AgeSeconds      int64      `json:"age_seconds"`
	Stale           bool       `json:"stale"`
	CreatedAt       time.Time  `json:"created_at"`
	ReviewAt        *time.Time `json:"review_at,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
}

type WorkspaceContextSnapshot struct {
	ID                      string                        `json:"id"`
	ConsultationID          string                        `json:"consultation_id,omitempty"`
	RunID                   string                        `json:"run_id,omitempty"`
	SubjectType             WorkspaceSubject              `json:"subject_type"`
	ConfigurationRevisionID string                        `json:"configuration_revision_id"`
	Scope                   settings.OperationalScope     `json:"scope"`
	Resources               []telemetry.ResourceReference `json:"resource_refs"`
	Filters                 json.RawMessage               `json:"filters"`
	TimeRange               telemetry.TimeRange           `json:"time_range"`
	QueryDefinitionIDs      []string                      `json:"query_definition_refs"`
	QueryExecutionIDs       []string                      `json:"query_execution_refs"`
	EvidenceIDs             []string                      `json:"evidence_refs"`
	ContentHash             string                        `json:"content_hash"`
	CreatedAt               time.Time                     `json:"created_at"`
}

type ConsultationSummary struct {
	ID               string                    `json:"id"`
	Title            string                    `json:"title"`
	Status           string                    `json:"status"`
	ActiveSnapshotID string                    `json:"active_snapshot_id"`
	ActiveRun        *WorkspaceRun             `json:"active_run,omitempty"`
	Scope            settings.OperationalScope `json:"scope"`
	MessageCount     int                       `json:"message_count"`
	CreatedAt        time.Time                 `json:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at"`
}

type ConsultationDetail struct {
	ConsultationSummary
	Snapshots []WorkspaceContextSnapshot `json:"snapshots"`
	Messages  []ConsultationMessage      `json:"messages"`
}

type ConsultationMessage struct {
	ID                string             `json:"id"`
	ConsultationID    string             `json:"consultation_id"`
	RunID             string             `json:"run_id,omitempty"`
	ContextSnapshotID string             `json:"context_snapshot_id"`
	Sequence          uint64             `json:"sequence"`
	Role              string             `json:"role"`
	Content           string             `json:"content"`
	Status            string             `json:"status"`
	CreatedAt         time.Time          `json:"created_at"`
	CompletedAt       *time.Time         `json:"completed_at,omitempty"`
	Evidence          []EvidenceCitation `json:"evidence_citations"`
	Guidance          []GuidanceCitation `json:"guidance_citations"`
}

type SendMessageRequest struct {
	Content        string `json:"content"`
	IdempotencyKey string `json:"-"`
}

type CreateWorkspaceConsultationRequest struct {
	Title              string                        `json:"title"`
	ClusterID          string                        `json:"cluster_id"`
	Environment        string                        `json:"environment"`
	Namespaces         []string                      `json:"namespaces"`
	Resources          []telemetry.ResourceReference `json:"resource_refs"`
	Filters            json.RawMessage               `json:"filters,omitempty"`
	From               time.Time                     `json:"from"`
	To                 time.Time                     `json:"to"`
	QueryDefinitionIDs []string                      `json:"query_definition_refs"`
	QueryExecutionIDs  []string                      `json:"query_execution_refs"`
	EvidenceIDs        []string                      `json:"evidence_refs"`
}

type AttachWorkspaceSnapshotRequest = CreateWorkspaceConsultationRequest

type StreamEvent struct {
	ID             string          `json:"id"`
	RunID          string          `json:"run_id"`
	ConsultationID string          `json:"consultation_id,omitempty"`
	Sequence       uint64          `json:"sequence"`
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"created_at"`
}

type WorkspaceTaskStatus string

const (
	WorkspaceTaskReady     WorkspaceTaskStatus = "ready"
	WorkspaceTaskRunning   WorkspaceTaskStatus = "running"
	WorkspaceTaskSucceeded WorkspaceTaskStatus = "succeeded"
	WorkspaceTaskDead      WorkspaceTaskStatus = "dead"
	WorkspaceTaskCancelled WorkspaceTaskStatus = "cancelled"
)

// WorkspaceTask is the durable, Incident-independent work envelope claimed by
// cloudops-worker. It carries only immutable identities; page state and chat
// history are loaded through the Run's Context Snapshot after claim.
type WorkspaceTask struct {
	ID                      string              `json:"id"`
	RunID                   string              `json:"run_id"`
	ConfigurationRevisionID string              `json:"configuration_revision_id"`
	SubjectType             WorkspaceSubject    `json:"subject_type"`
	Status                  WorkspaceTaskStatus `json:"status"`
	Attempt                 uint32              `json:"attempt"`
	MaxAttempts             uint32              `json:"max_attempts"`
	AvailableAt             time.Time           `json:"available_at"`
	CreatedAt               time.Time           `json:"created_at"`
}

type WorkspaceLease struct {
	TaskID                  uint64
	TaskPublicID            string
	RunID                   uint64
	RunPublicID             string
	ConfigurationRevisionID string
	Owner                   string
	Generation              uint64
	Attempt                 uint32
	MaxAttempts             uint32
}

type WorkspaceExecutionContext struct {
	Task        WorkspaceTask
	Run         WorkspaceRun
	Snapshot    WorkspaceContextSnapshot
	Limits      WorkspaceExecutionLimits
	OwnerPrompt string
	AlertName   string
}

type WorkspaceExecutionLimits struct {
	MaxToolCalls     int
	MaxModelCalls    int
	TokenBudget      int64
	MaxEvidenceItems int
	MaxRuntime       time.Duration
	ToolTimeout      time.Duration
}

type WorkspaceToolObservation struct {
	Tool           string
	Source         string
	ResourceRef    string
	Query          string
	Summary        string
	Facts          json.RawMessage
	Provenance     json.RawMessage
	TrustAxes      json.RawMessage
	ObservedAt     time.Time
	CollectedAt    time.Time
	Truncated      bool
	Partial        bool
	SourceRevision string
	TypedFacts     []EvidenceFact
}

type WorkspaceCompletion struct {
	Outcome        WorkspaceOutcome
	Uncertainty    string
	Answer         string
	Diagnosis      *DiagnosisRecord
	FailureCode    string
	FailureSummary string
	ModelProvider  string
	ActualModel    string
	ModelCalls     int
	InputTokens    int64
	OutputTokens   int64
}

type WorkspaceDiagnosisContext struct {
	IncidentID  string
	CycleNo     uint64
	Facts       []EvidenceFact
	Policy      ClaimPolicy
	Sufficiency SufficiencyResult
}

type WorkspaceModelRequest struct {
	Objective string
	Snapshot  WorkspaceContextSnapshot
	Evidence  []EvidenceCitation
	Guidance  []GuidanceCitation
	Prompt    string
}

type WorkspaceModelResponse struct {
	Answer       string
	InputTokens  int64
	OutputTokens int64
}

type WorkspaceModel interface {
	Stream(context.Context, WorkspaceModelRequest, func(string) error) (WorkspaceModelResponse, error)
}

type WorkspaceModelFactory interface {
	Model(context.Context, string) (WorkspaceModel, string, string, error)
}

type WorkspaceDiagnosisModel interface {
	SynthesizeDiagnosis(context.Context, DiagnosisView) (DiagnosisCandidate, ModelUsage, error)
}

type WorkspaceDiagnosisModelFactory interface {
	DiagnosisModel(context.Context, string) (WorkspaceDiagnosisModel, string, string, error)
}

type KnowledgeRevision struct {
	ID                   string                        `json:"id"`
	Revision             uint64                        `json:"revision"`
	Content              string                        `json:"content"`
	ContentHash          string                        `json:"content_hash"`
	SourceType           string                        `json:"source_type"`
	SourceConsultationID string                        `json:"source_consultation_id,omitempty"`
	SourceMessageID      string                        `json:"source_message_id,omitempty"`
	Scope                settings.OperationalScope     `json:"scope"`
	Resources            []telemetry.ResourceReference `json:"resource_refs"`
	ReviewAt             *time.Time                    `json:"review_at,omitempty"`
	ExpiresAt            *time.Time                    `json:"expires_at,omitempty"`
	ConfirmedBy          string                        `json:"confirmed_by"`
	CreatedAt            time.Time                     `json:"created_at"`
}

type KnowledgeItem struct {
	ID        string              `json:"id"`
	Title     string              `json:"title"`
	Status    string              `json:"status"`
	Revision  KnowledgeRevision   `json:"current_revision"`
	Revisions []KnowledgeRevision `json:"revisions,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

type SaveKnowledgeRequest struct {
	Title                string                        `json:"title"`
	Content              string                        `json:"content"`
	SourceConsultationID string                        `json:"source_consultation_id,omitempty"`
	SourceMessageID      string                        `json:"source_message_id,omitempty"`
	ClusterID            string                        `json:"cluster_id"`
	Environment          string                        `json:"environment"`
	Namespaces           []string                      `json:"namespaces"`
	Resources            []telemetry.ResourceReference `json:"resource_refs"`
	ReviewAt             *time.Time                    `json:"review_at,omitempty"`
	ExpiresAt            *time.Time                    `json:"expires_at,omitempty"`
}

type UpdateKnowledgeRequest struct {
	Title       string                        `json:"title,omitempty"`
	Content     string                        `json:"content,omitempty"`
	Status      string                        `json:"status,omitempty"`
	ClusterID   string                        `json:"cluster_id,omitempty"`
	Environment string                        `json:"environment,omitempty"`
	Namespaces  []string                      `json:"namespaces,omitempty"`
	Resources   []telemetry.ResourceReference `json:"resource_refs,omitempty"`
	ReviewAt    *time.Time                    `json:"review_at,omitempty"`
	ExpiresAt   *time.Time                    `json:"expires_at,omitempty"`
}

type RunbookGuidance struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Path       string    `json:"path"`
	Revision   string    `json:"revision"`
	Content    string    `json:"content,omitempty"`
	ModifiedAt time.Time `json:"modified_at"`
}

type ActionProposalRequest struct {
	RunID              string          `json:"run_id"`
	ActionType         string          `json:"action_type"`
	Target             json.RawMessage `json:"target"`
	Parameters         json.RawMessage `json:"parameters"`
	Preconditions      json.RawMessage `json:"preconditions"`
	Risk               string          `json:"risk"`
	IntendedState      json.RawMessage `json:"intended_state,omitempty"`
	VerificationIntent json.RawMessage `json:"verification_intent,omitempty"`
	ExpiresAt          time.Time       `json:"expires_at"`
}

type ActionCard struct {
	ID            string               `json:"id"`
	RunID         string               `json:"run_id"`
	Authority     string               `json:"authority"`
	ActionType    string               `json:"action_type"`
	Target        json.RawMessage      `json:"target"`
	Parameters    json.RawMessage      `json:"parameters"`
	Preconditions json.RawMessage      `json:"preconditions"`
	Risk          string               `json:"risk"`
	ContentHash   string               `json:"content_hash"`
	Status        string               `json:"status"`
	ExpiresAt     time.Time            `json:"expires_at"`
	Authorization *ActionAuthorization `json:"authorization,omitempty"`
	CreatedAt     time.Time            `json:"created_at"`
}

type OperationPlan struct {
	ID                      string               `json:"id"`
	RunID                   string               `json:"run_id"`
	ConfigurationRevisionID string               `json:"configuration_revision_id"`
	Authority               string               `json:"authority"`
	OperationType           string               `json:"operation_type"`
	Target                  json.RawMessage      `json:"target"`
	Parameters              json.RawMessage      `json:"parameters"`
	IntendedState           json.RawMessage      `json:"intended_state"`
	Preconditions           json.RawMessage      `json:"preconditions"`
	Risk                    string               `json:"risk"`
	VerificationIntent      json.RawMessage      `json:"verification_intent"`
	ContentHash             string               `json:"content_hash"`
	Status                  string               `json:"status"`
	ExpiresAt               time.Time            `json:"expires_at"`
	Authorization           *ActionAuthorization `json:"authorization,omitempty"`
	CreatedAt               time.Time            `json:"created_at"`
}

type AuthorizeActionRequest struct {
	ExpectedHash string `json:"expected_hash"`
	Reason       string `json:"reason"`
}

type ActionAuthorization struct {
	ID             string    `json:"id"`
	SubjectType    string    `json:"subject_type"`
	SubjectID      string    `json:"subject_id"`
	AuthorizedHash string    `json:"authorized_content_hash"`
	AuthorizedBy   string    `json:"authorized_by"`
	Reason         string    `json:"reason"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
}
