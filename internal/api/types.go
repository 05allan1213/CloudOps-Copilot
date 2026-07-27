package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	alertdomain "github.com/05allan1213/CloudOps-Copilot/internal/alert"
)

const (
	JSONMediaType    = "application/json"
	ProblemMediaType = "application/problem+json"
	SSEMediaType     = "text/event-stream"

	RequestIDHeader   = "X-Request-ID"
	TraceIDHeader     = "X-Trace-ID"
	IdempotencyHeader = "Idempotency-Key"
	ReplayHeader      = "Idempotent-Replay"
)

var (
	ErrInvalidArgument   = errors.New("invalid API argument")
	ErrNotFound          = errors.New("resource not found")
	ErrNotImplemented    = errors.New("command not implemented")
	ErrConflict          = errors.New("command conflict")
	ErrStaleVersion      = errors.New("expected version or hash is stale")
	ErrInvalidTransition = errors.New("business transition is invalid")
	ErrForbidden         = errors.New("command forbidden")
	ErrUnavailable       = errors.New("dependency unavailable")
)

// OwnerIdentity is the fixed audit identity of the trusted loopback Owner.
// It is never supplied by a browser header or persisted credential.
type OwnerIdentity struct {
	Subject  string
	Provider string
	Login    string
	Role     string
}

// IncidentView is a projection DTO. ID is always a public UUID; numeric
// database keys are intentionally absent from transport types.
type IncidentView struct {
	ID                    string    `json:"id"`
	Cycle                 uint64    `json:"cycle"`
	Status                string    `json:"status"`
	Severity              string    `json:"severity"`
	Summary               string    `json:"summary,omitempty"`
	Version               uint64    `json:"version"`
	NeedsAttention        bool      `json:"needs_attention"`
	BlockingReasonCode    string    `json:"blocking_reason_code,omitempty"`
	MigratedLegacy        bool      `json:"migrated_legacy"`
	MigratedLegacyContext bool      `json:"migrated_legacy_context"`
	CreatedAt             time.Time `json:"created_at,omitempty"`
	UpdatedAt             time.Time `json:"updated_at,omitempty"`
}

// ResourceView is the bounded common shape used by child Query
// skeletons. Future phases can replace it with richer typed projections without
// changing the read-only Query port boundary.
type ResourceView struct {
	ID                    string    `json:"id"`
	Kind                  string    `json:"kind"`
	Status                string    `json:"status,omitempty"`
	Version               uint64    `json:"version,omitempty"`
	Cycle                 uint64    `json:"cycle,omitempty"`
	Summary               string    `json:"summary,omitempty"`
	Hash                  string    `json:"hash,omitempty"`
	MigratedLegacy        bool      `json:"migrated_legacy"`
	MigratedLegacyContext bool      `json:"migrated_legacy_context"`
	CreatedAt             time.Time `json:"created_at,omitempty"`
	UpdatedAt             time.Time `json:"updated_at,omitempty"`
}

// ResolutionReportView is the immutable, current-cycle recovery projection.
// Its identifier-bearing fields are public UUIDs only; the bounded JSON
// sections are sanitized before they cross the Query port boundary.
type ResolutionReportView struct {
	ID                    string                            `json:"id"`
	Kind                  string                            `json:"kind"`
	Status                string                            `json:"status"`
	Cycle                 uint64                            `json:"cycle"`
	TriggerType           string                            `json:"trigger_type"`
	ResolutionReason      string                            `json:"resolution_reason"`
	Service               string                            `json:"service"`
	Workload              string                            `json:"workload"`
	Environment           string                            `json:"environment"`
	ImpactSummary         string                            `json:"impact_summary"`
	Summary               string                            `json:"summary"`
	Hash                  string                            `json:"hash"`
	CycleStartedAt        time.Time                         `json:"cycle_started_at"`
	ResolvedAt            time.Time                         `json:"resolved_at"`
	MeasuredDurationMS    uint64                            `json:"measured_duration_ms"`
	GeneratedAt           time.Time                         `json:"generated_at"`
	Revisions             ResolutionRevisionsView           `json:"revisions"`
	VerificationProfile   ResolutionVerificationProfileView `json:"verification_profile"`
	Stability             ResolutionStabilityView           `json:"stability"`
	TriggerSignal         json.RawMessage                   `json:"trigger_signal"`
	Diagnosis             json.RawMessage                   `json:"diagnosis"`
	Evidence              json.RawMessage                   `json:"evidence"`
	RemediationPlan       json.RawMessage                   `json:"remediation_plan"`
	RemediationDecision   json.RawMessage                   `json:"remediation_decision"`
	Delivery              json.RawMessage                   `json:"delivery"`
	Verification          json.RawMessage                   `json:"verification"`
	Timeline              json.RawMessage                   `json:"timeline"`
	AgentUsage            json.RawMessage                   `json:"agent_usage"`
	MigratedLegacyContext bool                              `json:"migrated_legacy_context"`
}

type ResolutionRevisionsView struct {
	BadGitOpsRevision string `json:"bad_gitops_revision,omitempty"`
	FixGitOpsRevision string `json:"fix_gitops_revision,omitempty"`
	SourceRevision    string `json:"source_revision"`
	ImageDigest       string `json:"image_digest"`
	GitOpsRevision    string `json:"gitops_revision"`
}

type ResolutionVerificationProfileView struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

type ResolutionStabilityView struct {
	CommonWindowStartedAt   time.Time `json:"common_window_started_at"`
	CommonWindowCompletedAt time.Time `json:"common_window_completed_at"`
}

// RefreshEvent is the only SSE payload kind exposed by this skeleton. Cursor
// is opaque and is emitted as the SSE id for Last-Event-ID resumption.
type RefreshEvent struct {
	Cursor     string `json:"-"`
	IncidentID string `json:"incident_id"`
	Resource   string `json:"resource"`
}

type QueryKind string

const (
	QueryIncidents        QueryKind = "incidents"
	QueryIncident         QueryKind = "incident"
	QuerySignals          QueryKind = "signals"
	QueryTimeline         QueryKind = "timeline"
	QueryEvidence         QueryKind = "evidence"
	QueryInvestigations   QueryKind = "investigations"
	QueryRemediationPlans QueryKind = "remediation_plans"
	QueryDelivery         QueryKind = "delivery"
	QueryVerifications    QueryKind = "verifications"
	QueryResolutionReport QueryKind = "resolution_report"
	QueryEvents           QueryKind = "events"
)

type QueryRequest struct {
	Kind        QueryKind
	IncidentID  string
	Cursor      string
	AfterID     string
	LastEventID string
	Limit       int
	Status      string
	Severity    string
	Service     string
}

type QueryResponse struct {
	Incident         *IncidentView
	Resource         *ResourceView
	ResolutionReport *ResolutionReportView
	Delivery         *DeliveryView
	Incidents        []IncidentView
	Items            []ResourceView
	RemediationPlans []RemediationPlanView
	Verifications    []VerificationRunView
	Events           []RefreshEvent
	NextCursor       string
}

// QueryPort reads a durable projection only. Implementations must not call
// external systems or trigger reconciliation.
type QueryPort interface {
	Query(context.Context, QueryRequest) (QueryResponse, error)
}

type CommandKind string

const (
	CommandStartInvestigation CommandKind = "investigation.start"
	CommandCloseIncident      CommandKind = "incident.close"
	CommandDecideRemediation  CommandKind = "remediation_plan.decide"
)

type CommandRequest struct {
	Kind            CommandKind
	ResourceID      string
	Actor           OwnerIdentity
	IdempotencyKey  string
	ExpectedVersion uint64
	ExpectedHash    string
	CanonicalBody   json.RawMessage
	RequestID       string
	TraceID         string
}

// CommandResult is intentionally narrow so implementations cannot accidentally
// serialize an internal numeric identifier, lease, checkpoint, or raw result.
type CommandResult struct {
	HTTPStatus int
	ResourceID string
	Status     string
	Version    uint64
	Cycle      uint64
	Replayed   bool `json:"-"`
}

type CommandPort interface {
	Execute(context.Context, CommandRequest) (CommandResult, error)
}

type AlertPort interface {
	List(context.Context, alertdomain.ListRequest) (alertdomain.Page, error)
	Detail(context.Context, string) (alertdomain.Detail, error)
	Acknowledge(context.Context, alertdomain.AcknowledgeRequest) (alertdomain.View, error)
	CreateSilence(context.Context, alertdomain.CreateSilenceRequest) (alertdomain.Silence, error)
	ExpireSilence(context.Context, alertdomain.ExpireSilenceRequest) (alertdomain.Silence, error)
	LinkIncident(context.Context, alertdomain.LinkIncidentRequest) (alertdomain.View, error)
	StartInvestigation(context.Context, alertdomain.StartInvestigationRequest) (alertdomain.View, error)
}

// Problem is the stable RFC 9457-compatible error envelope.
type Problem struct {
	Type      string   `json:"type"`
	Title     string   `json:"title"`
	Status    int      `json:"status"`
	Detail    string   `json:"detail"`
	Instance  string   `json:"instance"`
	Code      string   `json:"code"`
	RequestID string   `json:"request_id"`
	TraceID   string   `json:"trace_id"`
	NextSteps []string `json:"next_steps"`
}

type commandResponse struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	Status  string `json:"status"`
	Version uint64 `json:"version,omitempty"`
	Cycle   uint64 `json:"cycle,omitempty"`
}

type collectionResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type incidentResponse struct {
	Incident IncidentView `json:"incident"`
}

type resourceResponse struct {
	Resource ResourceView `json:"resource"`
}

type resolutionReportResponse struct {
	Resource *ResolutionReportView `json:"resource"`
}

type storedHTTPResponse struct {
	Status      int
	ContentType string
	Body        []byte
}

func normalizeCommandStatus(status int) int {
	if status == 0 {
		return http.StatusAccepted
	}
	return status
}
