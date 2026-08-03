// Package infrastructure owns the bounded Kubernetes resource and topology
// projection consumed by native CloudOps Workspaces.
package infrastructure

import (
	"context"
	"errors"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

const (
	DefaultLimit = 200
	MaximumLimit = 500
)

var (
	ErrInvalid     = errors.New("invalid infrastructure query")
	ErrNotFound    = errors.New("infrastructure resource not found")
	ErrUnavailable = errors.New("infrastructure provider unavailable")
)

type ProviderState string

const (
	ProviderAvailable   ProviderState = "available"
	ProviderPartial     ProviderState = "partial"
	ProviderUnavailable ProviderState = "unavailable"
	ProviderDisabled    ProviderState = "disabled"
)

type HealthState string

const (
	HealthHealthy  HealthState = "healthy"
	HealthWarning  HealthState = "warning"
	HealthCritical HealthState = "critical"
	HealthUnknown  HealthState = "unknown"
)

type ResourceLayer string

const (
	LayerNamespace ResourceLayer = "namespace"
	LayerService   ResourceLayer = "service"
	LayerWorkload  ResourceLayer = "workload"
	LayerPod       ResourceLayer = "pod"
	LayerNode      ResourceLayer = "node"
	LayerGateway   ResourceLayer = "gateway"
)

type Query struct {
	ClusterID string
	Namespace string
	Kinds     []string
	Search    string
	Cursor    string
	Limit     int
	From      time.Time
	To        time.Time
}

type ReadRequest struct {
	ClusterID  string   `json:"cluster_id"`
	Namespaces []string `json:"namespaces"`
	Limit      int      `json:"limit"`
}

type ProviderSource struct {
	Provider      string    `json:"provider"`
	ClusterID     string    `json:"cluster_id"`
	Identity      string    `json:"identity"`
	ServerVersion string    `json:"server_version,omitempty"`
	CollectedAt   time.Time `json:"collected_at"`
}

type Freshness struct {
	State      string    `json:"state"`
	FreshUntil time.Time `json:"fresh_until"`
	AgeSeconds int64     `json:"age_seconds"`
}

type ResourceHealth struct {
	State   HealthState `json:"state"`
	Summary string      `json:"summary"`
}

type ResourceReference struct {
	ID        string `json:"id,omitempty"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

type ResourcePort struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol"`
	Port       int32  `json:"port"`
	TargetPort string `json:"target_port,omitempty"`
}

type ResourceEndpoint struct {
	Address   string `json:"address"`
	Ready     *bool  `json:"ready,omitempty"`
	TargetID  string `json:"target_id,omitempty"`
	TargetRef string `json:"target_ref,omitempty"`
}

type ResourceCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"last_transition_time,omitempty"`
}

// ContainerEnvironment projects only bounded environment variable names. It
// intentionally excludes literal values and valueFrom payloads.
type ContainerEnvironment struct {
	Name               string   `json:"name"`
	EnvNames           []string `json:"env_names"`
	EnvNamesTruncated  bool     `json:"env_names_truncated,omitempty"`
	HasEnvFrom         bool     `json:"has_env_from,omitempty"`
	HasValueFrom       bool     `json:"has_value_from,omitempty"`
	HasSecretReference bool     `json:"has_secret_reference,omitempty"`
}

type WorkloadStatus struct {
	DesiredReplicas    int32 `json:"desired_replicas"`
	UpdatedReplicas    int32 `json:"updated_replicas"`
	ReadyReplicas      int32 `json:"ready_replicas"`
	AvailableReplicas  int32 `json:"available_replicas"`
	ObservedGeneration int64 `json:"observed_generation"`
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

type Resource struct {
	ID                  string                 `json:"id"`
	SourceUID           string                 `json:"source_uid,omitempty"`
	APIVersion          string                 `json:"api_version"`
	Kind                string                 `json:"kind"`
	Layer               ResourceLayer          `json:"layer"`
	Namespace           string                 `json:"namespace,omitempty"`
	Name                string                 `json:"name"`
	ResourceVersion     string                 `json:"resource_version,omitempty"`
	Generation          int64                  `json:"generation,omitempty"`
	Status              string                 `json:"status,omitempty"`
	Workload            *WorkloadStatus        `json:"workload,omitempty"`
	Health              ResourceHealth         `json:"health"`
	OwnerReferences     []ResourceReference    `json:"owner_references"`
	Selector            map[string]string      `json:"selector"`
	Labels              map[string]string      `json:"labels"`
	Endpoints           []ResourceEndpoint     `json:"endpoints"`
	Ports               []ResourcePort         `json:"ports"`
	Conditions          []ResourceCondition    `json:"conditions"`
	Containers          []ContainerEnvironment `json:"containers,omitempty"`
	ContainersTruncated bool                   `json:"containers_truncated,omitempty"`
	InitContainerCount  int                    `json:"init_container_count,omitempty"`
	EphemeralCount      int                    `json:"ephemeral_container_count,omitempty"`
	NodeName            string                 `json:"node_name,omitempty"`
	Addresses           []string               `json:"addresses"`
	CreatedAt           time.Time              `json:"created_at,omitempty"`
	Links               []ContextLink          `json:"links"`
}

type TopologyEdge struct {
	ID         string `json:"id"`
	SourceID   string `json:"source_id"`
	TargetID   string `json:"target_id"`
	Relation   string `json:"relation"`
	SourceFact string `json:"source_fact"`
}

type ProviderIssue struct {
	Namespace string `json:"namespace,omitempty"`
	Operation string `json:"operation"`
	Code      string `json:"code"`
	Detail    string `json:"detail"`
}

type Projection struct {
	Source    ProviderSource
	Nodes     []Resource
	Edges     []TopologyEdge
	Issues    []ProviderIssue
	Partial   bool
	Truncated bool
}

type TopologySnapshot struct {
	ID                    string                    `json:"id,omitempty"`
	ContentHash           string                    `json:"content_hash,omitempty"`
	ConfigurationRevision string                    `json:"configuration_revision_id"`
	Scope                 settings.OperationalScope `json:"scope"`
	ProviderState         ProviderState             `json:"provider_state"`
	ProviderDetail        string                    `json:"provider_detail"`
	Source                ProviderSource            `json:"source"`
	Freshness             Freshness                 `json:"freshness"`
	Nodes                 []Resource                `json:"nodes"`
	Edges                 []TopologyEdge            `json:"edges"`
	Issues                []ProviderIssue           `json:"issues"`
	Partial               bool                      `json:"partial"`
	Truncated             bool                      `json:"truncated"`
	CollectedAt           time.Time                 `json:"collected_at"`
}

type ResourcePage struct {
	SnapshotID    string                    `json:"snapshot_id,omitempty"`
	Scope         settings.OperationalScope `json:"scope"`
	ProviderState ProviderState             `json:"provider_state"`
	Source        ProviderSource            `json:"source"`
	Freshness     Freshness                 `json:"freshness"`
	Items         []Resource                `json:"items"`
	NextCursor    string                    `json:"next_cursor,omitempty"`
	Partial       bool                      `json:"partial"`
	Truncated     bool                      `json:"truncated"`
	CollectedAt   time.Time                 `json:"collected_at"`
}

type ResourceDetail struct {
	SnapshotID    string                    `json:"snapshot_id"`
	Scope         settings.OperationalScope `json:"scope"`
	ProviderState ProviderState             `json:"provider_state"`
	Source        ProviderSource            `json:"source"`
	Freshness     Freshness                 `json:"freshness"`
	Resource      Resource                  `json:"resource"`
	Related       []Resource                `json:"related"`
	Edges         []TopologyEdge            `json:"edges"`
	Partial       bool                      `json:"partial"`
	CollectedAt   time.Time                 `json:"collected_at"`
}

type Event struct {
	ID           string    `json:"id"`
	Type         string    `json:"type,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	Message      string    `json:"message,omitempty"`
	Count        int32     `json:"count,omitempty"`
	ResourceKind string    `json:"resource_kind"`
	ResourceName string    `json:"resource_name"`
	Namespace    string    `json:"namespace,omitempty"`
	ObservedAt   time.Time `json:"observed_at"`
	CollectedAt  time.Time `json:"collected_at"`
}

type EventPage struct {
	SnapshotID    string                    `json:"snapshot_id"`
	Scope         settings.OperationalScope `json:"scope"`
	ProviderState ProviderState             `json:"provider_state"`
	Source        ProviderSource            `json:"source"`
	ResourceID    string                    `json:"resource_id"`
	Items         []Event                   `json:"items"`
	Partial       bool                      `json:"partial"`
	Truncated     bool                      `json:"truncated"`
	CollectedAt   time.Time                 `json:"collected_at"`
}

type Reader interface {
	Probe(context.Context, string) (ProviderSource, error)
	Read(context.Context, ReadRequest) (Projection, error)
	Events(context.Context, string, Resource, int) ([]Event, bool, error)
}

type ConfigurationSource interface {
	ActiveRevision(context.Context) (settings.Revision, error)
	ObserveProviderHealth(context.Context, string, settings.ProviderResult) error
}

type SnapshotRepository interface {
	Store(context.Context, string, *TopologySnapshot) error
}
