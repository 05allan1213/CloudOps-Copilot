// Package observability owns bounded query definitions, authorizations,
// executions, and their provider-neutral audit contract.
package observability

import (
	"context"
	"errors"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

const (
	MaximumQueryBytes    = 8 * 1024
	MaximumASTNodes      = 256
	MaximumResponseBytes = 2 * 1024 * 1024
	MaximumSeries        = 200
	MaximumSamples       = 10_000
	MaximumConcurrent    = 2
)

var (
	ErrInvalid              = errors.New("invalid observability query")
	ErrBoundExceeded        = errors.New("observability query exceeds configured bounds")
	ErrNotFound             = errors.New("observability query resource not found")
	ErrConflict             = errors.New("observability query state conflict")
	ErrUnavailable          = errors.New("observability provider unavailable")
	ErrProviderDisabled     = errors.New("observability provider disabled")
	ErrUnauthorized         = errors.New("observability query is not authorized")
	ErrAuthorizationRevoked = errors.New("observability query authorization is revoked")
	ErrAuthorizationUsed    = errors.New("one-time observability query authorization is already used")
)

type QueryMode string

const (
	ModeGuided QueryMode = "guided"
	ModeExpert QueryMode = "expert"
)

type ExecutionActor string

const (
	ActorOwner ExecutionActor = "owner"
	ActorAgent ExecutionActor = "agent"
)

type ExecutionStatus string

const (
	ExecutionPending   ExecutionStatus = "pending"
	ExecutionRunning   ExecutionStatus = "running"
	ExecutionSucceeded ExecutionStatus = "succeeded"
	ExecutionFailed    ExecutionStatus = "failed"
	ExecutionCancelled ExecutionStatus = "cancelled"
)

type AuthorizationMode string

const (
	AuthorizationRunOnce    AuthorizationMode = "run_once"
	AuthorizationDefinition AuthorizationMode = "definition"
)

type ProviderState string

const (
	ProviderAvailable   ProviderState = "available"
	ProviderPartial     ProviderState = "partial"
	ProviderUnavailable ProviderState = "unavailable"
	ProviderDisabled    ProviderState = "disabled"
)

type ResourceReference struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type QueryBounds struct {
	MaxLookbackSeconds int `json:"max_lookback_seconds"`
	TimeoutMS          int `json:"timeout_ms"`
	MaxResponseBytes   int `json:"max_response_bytes"`
	MaxSeries          int `json:"max_series"`
	MaxSamples         int `json:"max_samples"`
	ConcurrencyLimit   int `json:"concurrency_limit"`
	StepSeconds        int `json:"step_seconds"`
}

type ProviderSource struct {
	Provider      string    `json:"provider"`
	Identity      string    `json:"identity"`
	ServerVersion string    `json:"server_version,omitempty"`
	CollectedAt   time.Time `json:"collected_at"`
}

type ContextLink struct {
	Kind         string    `json:"kind"`
	Label        string    `json:"label"`
	Href         string    `json:"href"`
	Target       string    `json:"target"`
	Provider     string    `json:"provider,omitempty"`
	ResourceRef  string    `json:"resource_ref,omitempty"`
	From         time.Time `json:"from,omitempty"`
	To           time.Time `json:"to,omitempty"`
	Availability string    `json:"availability"`
}

type CatalogEntry struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Unit        string `json:"unit"`
	Query       string `json:"query"`
}

type Catalog struct {
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	ProviderState         ProviderState             `json:"provider_state"`
	ProviderDetail        string                    `json:"provider_detail"`
	Source                ProviderSource            `json:"source"`
	MetricNames           []string                  `json:"metric_names"`
	Queries               []CatalogEntry            `json:"queries"`
	Bounds                QueryBounds               `json:"bounds"`
	Partial               bool                      `json:"partial"`
	CollectedAt           time.Time                 `json:"collected_at"`
}

type QueryPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type QuerySeries struct {
	Labels map[string]string `json:"labels"`
	Points []QueryPoint      `json:"points"`
}

type QueryResult struct {
	ResultType string        `json:"result_type"`
	Series     []QuerySeries `json:"series"`
}

type Execution struct {
	ID                    string                    `json:"id"`
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	DefinitionID          string                    `json:"query_definition_id,omitempty"`
	AuthorizationID       string                    `json:"query_authorization_id,omitempty"`
	Actor                 ExecutionActor            `json:"actor"`
	Provider              string                    `json:"provider"`
	Mode                  QueryMode                 `json:"mode"`
	CatalogKey            string                    `json:"catalog_key,omitempty"`
	Query                 string                    `json:"query"`
	QueryHash             string                    `json:"query_hash"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	TimeRange             TimeRange                 `json:"time_range"`
	Bounds                QueryBounds               `json:"bounds"`
	Status                ExecutionStatus           `json:"status"`
	Source                ProviderSource            `json:"source"`
	Result                *QueryResult              `json:"result,omitempty"`
	ResultExpired         bool                      `json:"result_expired"`
	SeriesCount           int                       `json:"series_count"`
	SampleCount           int                       `json:"sample_count"`
	ResponseBytes         int                       `json:"response_bytes"`
	Partial               bool                      `json:"partial"`
	Truncated             bool                      `json:"truncated"`
	ErrorCode             string                    `json:"error_code,omitempty"`
	ErrorDetail           string                    `json:"error_detail,omitempty"`
	Links                 []ContextLink             `json:"links"`
	Events                []ExecutionEvent          `json:"events"`
	CreatedAt             time.Time                 `json:"created_at"`
	StartedAt             *time.Time                `json:"started_at,omitempty"`
	CompletedAt           *time.Time                `json:"completed_at,omitempty"`
}

type ExecutionEvent struct {
	ID        string    `json:"id"`
	Sequence  uint64    `json:"sequence"`
	Type      string    `json:"type"`
	Actor     string    `json:"actor"`
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Definition struct {
	ID                    string                    `json:"id"`
	DefinitionKey         string                    `json:"definition_key"`
	Revision              uint64                    `json:"revision"`
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Provider              string                    `json:"provider"`
	Mode                  QueryMode                 `json:"mode"`
	CatalogKey            string                    `json:"catalog_key,omitempty"`
	Title                 string                    `json:"title"`
	Description           string                    `json:"description,omitempty"`
	Query                 string                    `json:"query"`
	QueryHash             string                    `json:"query_hash"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	MaxLookbackSeconds    int                       `json:"max_lookback_seconds"`
	MaxSeries             int                       `json:"max_series"`
	MaxSamples            int                       `json:"max_samples"`
	ContentHash           string                    `json:"content_hash"`
	CreatedBy             string                    `json:"created_by"`
	CreatedAt             time.Time                 `json:"created_at"`
	Links                 []ContextLink             `json:"links"`
}

type Authorization struct {
	ID                    string                    `json:"id"`
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Mode                  AuthorizationMode         `json:"mode"`
	DefinitionID          string                    `json:"query_definition_id,omitempty"`
	Provider              string                    `json:"provider"`
	QueryMode             QueryMode                 `json:"query_mode"`
	CatalogKey            string                    `json:"catalog_key,omitempty"`
	Query                 string                    `json:"query"`
	QueryHash             string                    `json:"query_hash"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	MaxLookbackSeconds    int                       `json:"max_lookback_seconds"`
	MaxSeries             int                       `json:"max_series"`
	MaxSamples            int                       `json:"max_samples"`
	ConsumedExecutionID   string                    `json:"consumed_execution_id,omitempty"`
	RevokedAt             *time.Time                `json:"revoked_at,omitempty"`
	CreatedBy             string                    `json:"created_by"`
	CreatedAt             time.Time                 `json:"created_at"`
}

type CatalogRequest struct {
	ClusterID string            `json:"cluster_id"`
	Namespace string            `json:"namespace"`
	Resource  ResourceReference `json:"resource"`
}

type HistoryFilter struct {
	ClusterID  string
	Namespace  string
	ResourceID string
	Limit      int
}

type StartQueryRequest struct {
	Mode         QueryMode         `json:"mode"`
	CatalogKey   string            `json:"catalog_key,omitempty"`
	Query        string            `json:"query,omitempty"`
	ClusterID    string            `json:"cluster_id"`
	Namespace    string            `json:"namespace"`
	Resource     ResourceReference `json:"resource"`
	From         time.Time         `json:"from"`
	To           time.Time         `json:"to"`
	StepSeconds  int               `json:"step_seconds"`
	DefinitionID string            `json:"query_definition_id,omitempty"`
}

type AgentQueryRequest struct {
	AuthorizationID string    `json:"query_authorization_id"`
	From            time.Time `json:"from"`
	To              time.Time `json:"to"`
	StepSeconds     int       `json:"step_seconds"`
}

type SaveDefinitionRequest struct {
	ExecutionID          string `json:"query_execution_id"`
	PreviousDefinitionID string `json:"previous_query_definition_id,omitempty"`
	Title                string `json:"title"`
	Description          string `json:"description,omitempty"`
}

type CreateAuthorizationRequest struct {
	Mode         AuthorizationMode `json:"mode"`
	ExecutionID  string            `json:"query_execution_id,omitempty"`
	DefinitionID string            `json:"query_definition_id,omitempty"`
}

type ProviderCatalogRequest struct {
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	Bounds                QueryBounds               `json:"bounds"`
}

type ProviderQueryRequest struct {
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Scope                 settings.OperationalScope `json:"scope"`
	Resource              ResourceReference         `json:"resource"`
	Query                 string                    `json:"query"`
	QueryHash             string                    `json:"query_hash"`
	TimeRange             TimeRange                 `json:"time_range"`
	Bounds                QueryBounds               `json:"bounds"`
}

type ProviderCatalog struct {
	Source      ProviderSource `json:"source"`
	MetricNames []string       `json:"metric_names"`
	Partial     bool           `json:"partial"`
}

type ProviderQueryResult struct {
	Source        ProviderSource `json:"source"`
	Result        QueryResult    `json:"result"`
	SeriesCount   int            `json:"series_count"`
	SampleCount   int            `json:"sample_count"`
	ResponseBytes int            `json:"response_bytes"`
	Partial       bool           `json:"partial"`
	Truncated     bool           `json:"truncated"`
}

type Provider interface {
	Catalog(context.Context, ProviderCatalogRequest) (ProviderCatalog, error)
	Query(context.Context, ProviderQueryRequest) (ProviderQueryResult, error)
}

type PreparedQuery struct {
	ConfigurationRevision string
	DefinitionID          string
	AuthorizationID       string
	Actor                 ExecutionActor
	Mode                  QueryMode
	CatalogKey            string
	Query                 string
	QueryHash             string
	Scope                 settings.OperationalScope
	Resource              ResourceReference
	TimeRange             TimeRange
	Bounds                QueryBounds
}
