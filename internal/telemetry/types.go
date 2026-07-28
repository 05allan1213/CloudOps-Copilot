// Package telemetry owns bounded Logs and Traces workspace queries, retained
// Evidence selections, and immutable operational Context Snapshots.
package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

const (
	MaximumQueryBytes       = 8 * 1024
	MaximumResponseBytes    = 2 * 1024 * 1024
	MaximumLogRows          = 1_000
	MaximumTraceCount       = 200
	MaximumRetainedItems    = 32
	MaximumRetainedBytes    = 16 * 1024
	MaximumAttributeEntries = 96
)

var (
	ErrInvalid          = errors.New("invalid telemetry query")
	ErrBoundExceeded    = errors.New("telemetry query exceeds configured bounds")
	ErrNotFound         = errors.New("telemetry resource not found")
	ErrConflict         = errors.New("telemetry resource state conflict")
	ErrUnavailable      = errors.New("telemetry provider unavailable")
	ErrProviderDisabled = errors.New("telemetry provider disabled")
	ErrResultExpired    = errors.New("ephemeral telemetry result expired")
)

type QueryMode = observability.QueryMode

const (
	ModeGuided = observability.ModeGuided
	ModeExpert = observability.ModeExpert
)

type ResourceReference = observability.ResourceReference
type TimeRange = observability.TimeRange
type ProviderSource = observability.ProviderSource
type ContextLink = observability.ContextLink

type ProviderState string

const (
	ProviderAvailable   ProviderState = "available"
	ProviderPartial     ProviderState = "partial"
	ProviderUnavailable ProviderState = "unavailable"
	ProviderDisabled    ProviderState = "disabled"
)

type QueryBounds struct {
	MaxLookbackSeconds int `json:"max_lookback_seconds"`
	TimeoutMS          int `json:"timeout_ms"`
	MaxResponseBytes   int `json:"max_response_bytes"`
	MaxResults         int `json:"max_results"`
	ConcurrencyLimit   int `json:"concurrency_limit"`
}

type Catalog struct {
	Provider              string                    `json:"provider"`
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	ProviderState         ProviderState             `json:"provider_state"`
	ProviderDetail        string                    `json:"provider_detail"`
	Source                ProviderSource            `json:"source"`
	Bounds                QueryBounds               `json:"bounds"`
	CollectedAt           time.Time                 `json:"collected_at"`
}

type CatalogRequest struct {
	ClusterID string            `json:"cluster_id"`
	Namespace string            `json:"namespace"`
	Resource  ResourceReference `json:"resource"`
}

type LogFilter struct {
	Text    string   `json:"text,omitempty"`
	Levels  []string `json:"levels,omitempty"`
	TraceID string   `json:"trace_id,omitempty"`
}

type StartLogQueryRequest struct {
	Mode      QueryMode         `json:"mode"`
	Query     string            `json:"query,omitempty"`
	Filter    LogFilter         `json:"filter"`
	ClusterID string            `json:"cluster_id"`
	Namespace string            `json:"namespace"`
	Resource  ResourceReference `json:"resource"`
	From      time.Time         `json:"from"`
	To        time.Time         `json:"to"`
	Limit     int               `json:"limit"`
	Tail      bool              `json:"tail"`
}

type HistogramBucket struct {
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Count int       `json:"count"`
}

type LogEntry struct {
	ID         string            `json:"id"`
	Timestamp  time.Time         `json:"timestamp"`
	Level      string            `json:"level,omitempty"`
	Message    string            `json:"message"`
	Service    string            `json:"service,omitempty"`
	TraceID    string            `json:"trace_id,omitempty"`
	SpanID     string            `json:"span_id,omitempty"`
	Resource   ResourceReference `json:"resource"`
	Attributes map[string]string `json:"attributes"`
	Links      []ContextLink     `json:"links"`
}

type LogQuery struct {
	ID                    string                    `json:"id"`
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Provider              string                    `json:"provider"`
	Mode                  QueryMode                 `json:"mode"`
	Query                 string                    `json:"query"`
	QueryHash             string                    `json:"query_hash"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	TimeRange             TimeRange                 `json:"time_range"`
	Bounds                QueryBounds               `json:"bounds"`
	Status                string                    `json:"status"`
	Source                ProviderSource            `json:"source"`
	Histogram             []HistogramBucket         `json:"histogram"`
	Entries               []LogEntry                `json:"entries"`
	Fields                []string                  `json:"fields"`
	ResultExpired         bool                      `json:"result_expired"`
	ResultCount           int                       `json:"result_count"`
	ResponseBytes         int                       `json:"response_bytes"`
	Partial               bool                      `json:"partial"`
	Truncated             bool                      `json:"truncated"`
	Stale                 bool                      `json:"stale"`
	Tail                  bool                      `json:"tail"`
	ErrorCode             string                    `json:"error_code,omitempty"`
	ErrorDetail           string                    `json:"error_detail,omitempty"`
	Links                 []ContextLink             `json:"links"`
	CreatedAt             time.Time                 `json:"created_at"`
	CompletedAt           *time.Time                `json:"completed_at,omitempty"`
}

type TraceFilter struct {
	Service   string `json:"service,omitempty"`
	Operation string `json:"operation,omitempty"`
	Status    string `json:"status,omitempty"`
	MinMS     int64  `json:"min_duration_ms,omitempty"`
	MaxMS     int64  `json:"max_duration_ms,omitempty"`
}

type StartTraceSearchRequest struct {
	Mode      QueryMode         `json:"mode"`
	Query     string            `json:"query,omitempty"`
	Filter    TraceFilter       `json:"filter"`
	ClusterID string            `json:"cluster_id"`
	Namespace string            `json:"namespace"`
	Resource  ResourceReference `json:"resource"`
	From      time.Time         `json:"from"`
	To        time.Time         `json:"to"`
	Limit     int               `json:"limit"`
}

type TraceSummary struct {
	TraceID        string            `json:"trace_id"`
	RootService    string            `json:"root_service"`
	RootOperation  string            `json:"root_operation"`
	StartTime      time.Time         `json:"start_time"`
	DurationMS     float64           `json:"duration_ms"`
	SpanCount      int               `json:"span_count"`
	ErrorSpanCount int               `json:"error_span_count"`
	Resource       ResourceReference `json:"resource"`
	Links          []ContextLink     `json:"links"`
}

type TraceSearch struct {
	ID                    string                    `json:"id"`
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Provider              string                    `json:"provider"`
	Mode                  QueryMode                 `json:"mode"`
	Query                 string                    `json:"query"`
	QueryHash             string                    `json:"query_hash"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	TimeRange             TimeRange                 `json:"time_range"`
	Bounds                QueryBounds               `json:"bounds"`
	Status                string                    `json:"status"`
	Source                ProviderSource            `json:"source"`
	Traces                []TraceSummary            `json:"traces"`
	ResultExpired         bool                      `json:"result_expired"`
	ResultCount           int                       `json:"result_count"`
	ResponseBytes         int                       `json:"response_bytes"`
	Partial               bool                      `json:"partial"`
	Truncated             bool                      `json:"truncated"`
	Stale                 bool                      `json:"stale"`
	ErrorCode             string                    `json:"error_code,omitempty"`
	ErrorDetail           string                    `json:"error_detail,omitempty"`
	Links                 []ContextLink             `json:"links"`
	CreatedAt             time.Time                 `json:"created_at"`
	CompletedAt           *time.Time                `json:"completed_at,omitempty"`
}

type SpanEvent struct {
	Name       string            `json:"name"`
	Timestamp  time.Time         `json:"timestamp"`
	Attributes map[string]string `json:"attributes"`
}

type Span struct {
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Service      string            `json:"service"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind,omitempty"`
	StartTime    time.Time         `json:"start_time"`
	DurationMS   float64           `json:"duration_ms"`
	Depth        int               `json:"depth"`
	Status       string            `json:"status"`
	CriticalPath bool              `json:"critical_path"`
	Attributes   map[string]string `json:"attributes"`
	Events       []SpanEvent       `json:"events"`
	Resource     ResourceReference `json:"resource"`
	Links        []ContextLink     `json:"links"`
}

type TraceDetailRequest struct {
	TraceID   string            `json:"trace_id"`
	SearchID  string            `json:"search_id,omitempty"`
	ClusterID string            `json:"cluster_id"`
	Namespace string            `json:"namespace"`
	Resource  ResourceReference `json:"resource"`
	From      time.Time         `json:"from"`
	To        time.Time         `json:"to"`
}

type TraceDetail struct {
	QueryID               string                    `json:"query_id"`
	TraceID               string                    `json:"trace_id"`
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	TimeRange             TimeRange                 `json:"time_range"`
	Source                ProviderSource            `json:"source"`
	RootService           string                    `json:"root_service"`
	RootOperation         string                    `json:"root_operation"`
	StartTime             time.Time                 `json:"start_time"`
	DurationMS            float64                   `json:"duration_ms"`
	Spans                 []Span                    `json:"spans"`
	Attributes            map[string]string         `json:"attributes"`
	Partial               bool                      `json:"partial"`
	Truncated             bool                      `json:"truncated"`
	ResponseBytes         int                       `json:"response_bytes"`
	Links                 []ContextLink             `json:"links"`
}

type SaveEvidenceRequest struct {
	ItemIDs []string `json:"item_ids"`
}

type Evidence struct {
	ID                    string                    `json:"id"`
	Type                  string                    `json:"type"`
	Source                string                    `json:"source"`
	QueryID               string                    `json:"query_id"`
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	TimeRange             TimeRange                 `json:"time_range"`
	Summary               string                    `json:"summary"`
	ItemCount             int                       `json:"item_count"`
	ContentHash           string                    `json:"content_hash"`
	Truncated             bool                      `json:"truncated"`
	CollectedAt           time.Time                 `json:"collected_at"`
}

type CreateConsultationRequest struct {
	Title         string              `json:"title"`
	ClusterID     string              `json:"cluster_id"`
	Environment   string              `json:"environment"`
	Namespaces    []string            `json:"namespaces"`
	Resources     []ResourceReference `json:"resource_refs"`
	Filters       json.RawMessage     `json:"filters,omitempty"`
	From          time.Time           `json:"from"`
	To            time.Time           `json:"to"`
	DefinitionIDs []string            `json:"query_definition_refs"`
	QueryIDs      []string            `json:"query_execution_refs"`
	EvidenceIDs   []string            `json:"evidence_refs"`
}

type AttachContextSnapshotRequest = CreateConsultationRequest

type Consultation struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Status    string          `json:"status"`
	Snapshot  ContextSnapshot `json:"context_snapshot"`
	CreatedAt time.Time       `json:"created_at"`
}

type ContextSnapshot struct {
	ID                    string                    `json:"id"`
	ConsultationID        string                    `json:"consultation_id"`
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resources             []ResourceReference       `json:"resource_refs"`
	Filters               json.RawMessage           `json:"filters"`
	TimeRange             TimeRange                 `json:"time_range"`
	DefinitionIDs         []string                  `json:"query_definition_refs"`
	QueryIDs              []string                  `json:"query_execution_refs"`
	EvidenceIDs           []string                  `json:"evidence_refs"`
	ContentHash           string                    `json:"content_hash"`
	CreatedAt             time.Time                 `json:"created_at"`
}

type ProviderCatalogRequest struct {
	Provider              string                    `json:"provider"`
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	Bounds                QueryBounds               `json:"bounds"`
}

type ProviderCatalog struct {
	Source  ProviderSource `json:"source"`
	Partial bool           `json:"partial"`
}

type ProviderLogRequest struct {
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	Query                 string                    `json:"query"`
	TimeRange             TimeRange                 `json:"time_range"`
	Bounds                QueryBounds               `json:"bounds"`
	Tail                  bool                      `json:"tail"`
}

type ProviderLogResult struct {
	Source        ProviderSource    `json:"source"`
	Histogram     []HistogramBucket `json:"histogram"`
	Entries       []LogEntry        `json:"entries"`
	Fields        []string          `json:"fields"`
	ResponseBytes int               `json:"response_bytes"`
	Partial       bool              `json:"partial"`
	Truncated     bool              `json:"truncated"`
}

type ProviderTraceSearchRequest struct {
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	Query                 string                    `json:"query"`
	TimeRange             TimeRange                 `json:"time_range"`
	Bounds                QueryBounds               `json:"bounds"`
}

type ProviderTraceSearchResult struct {
	Source        ProviderSource `json:"source"`
	Traces        []TraceSummary `json:"traces"`
	ResponseBytes int            `json:"response_bytes"`
	Partial       bool           `json:"partial"`
	Truncated     bool           `json:"truncated"`
}

type ProviderTraceDetailRequest struct {
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	TraceID               string                    `json:"trace_id"`
	TimeRange             TimeRange                 `json:"time_range"`
	Bounds                QueryBounds               `json:"bounds"`
}

type ProviderTraceDetailResult struct {
	Source        ProviderSource `json:"source"`
	Detail        TraceDetail    `json:"detail"`
	ResponseBytes int            `json:"response_bytes"`
	Partial       bool           `json:"partial"`
	Truncated     bool           `json:"truncated"`
}

type Provider interface {
	Catalog(context.Context, ProviderCatalogRequest) (ProviderCatalog, error)
	QueryLogs(context.Context, ProviderLogRequest) (ProviderLogResult, error)
	SearchTraces(context.Context, ProviderTraceSearchRequest) (ProviderTraceSearchResult, error)
	Trace(context.Context, ProviderTraceDetailRequest) (ProviderTraceDetailResult, error)
}
