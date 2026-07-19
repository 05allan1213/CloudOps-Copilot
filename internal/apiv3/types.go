package apiv3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

const (
	JSONMediaType    = "application/json"
	ProblemMediaType = "application/problem+json"
	SSEMediaType     = "text/event-stream"

	RequestIDHeader   = "X-Request-ID"
	TraceIDHeader     = "X-Trace-ID"
	IdempotencyHeader = "Idempotency-Key"
	CSRFHeader        = "X-CSRF-Token"
	ReplayHeader      = "Idempotent-Replay"
)

var (
	ErrInvalidArgument   = errors.New("invalid v3 API argument")
	ErrNotFound          = errors.New("v3 resource not found")
	ErrNotImplemented    = errors.New("v3 command not implemented")
	ErrConflict          = errors.New("v3 command conflict")
	ErrStaleVersion      = errors.New("v3 expected version or hash is stale")
	ErrInvalidTransition = errors.New("v3 business transition is invalid")
	ErrUnavailable       = errors.New("v3 dependency unavailable")
)

// Identity is the V3 API identity boundary. It deliberately carries no
// internal user key; adapters translate the trusted provider identity into it.
type Identity struct {
	Subject  string
	Provider string
	Login    string
	Role     string
}

// Authenticator is implemented by the API bootstrap. Authentication and token
// revocation checks stay outside the Query and Command ports.
type Authenticator interface {
	AuthenticateBearer(context.Context, string) (Identity, error)
	Verify(context.Context, Identity) error
}

// IncidentView is a projection DTO. ID is always a public UUID; numeric
// database keys are intentionally absent from all V3 transport types.
type IncidentView struct {
	ID                 string    `json:"id"`
	Cycle              uint64    `json:"cycle"`
	Status             string    `json:"status"`
	Severity           string    `json:"severity"`
	Summary            string    `json:"summary,omitempty"`
	Version            uint64    `json:"version"`
	NeedsAttention     bool      `json:"needs_attention"`
	BlockingReasonCode string    `json:"blocking_reason_code,omitempty"`
	CreatedAt          time.Time `json:"created_at,omitempty"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
}

// ResourceView is the bounded common shape used by Phase 2 child Query
// skeletons. Future phases can replace it with richer typed projections without
// changing the read-only Query port boundary.
type ResourceView struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status,omitempty"`
	Version   uint64    `json:"version,omitempty"`
	Cycle     uint64    `json:"cycle,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	Hash      string    `json:"hash,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
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
	Incident   *IncidentView
	Resource   *ResourceView
	Incidents  []IncidentView
	Items      []ResourceView
	Events     []RefreshEvent
	NextCursor string
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
	Actor           Identity
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

// Problem is the stable RFC 9457-compatible V3 error envelope.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	Instance  string `json:"instance"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
	TraceID   string `json:"trace_id"`
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

type csrfResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
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
