package k8s

import (
	"time"

	"k8s.io/client-go/kubernetes"
)

type Deps struct {
	Reader Reader
	Client kubernetes.Interface
}

const (
	DefaultNamespace = "default"
	DefaultLimit     = 50
	MaxLimit         = 200
)

type Config struct {
	Enabled           bool
	WriteEnabled      bool
	InCluster         bool
	Kubeconfig        string
	AllowedNamespaces []string
	DefaultNamespace  string
	RequestTimeout    time.Duration
	LogTailLines      int
	LogMaxBytes       int
	EventLimit        int
}

type QueryOptions struct {
	Namespace     string `json:"namespace,omitempty"`
	Name          string `json:"name,omitempty"`
	LabelSelector string `json:"label_selector,omitempty"`
	FieldSelector string `json:"field_selector,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type EventQuery struct {
	Namespace    string `json:"namespace,omitempty"`
	InvolvedKind string `json:"involved_kind,omitempty"`
	InvolvedName string `json:"involved_name,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type LogQuery struct {
	Namespace string `json:"namespace,omitempty"`
	PodName   string `json:"pod_name"`
	Container string `json:"container,omitempty"`
	TailLines int    `json:"tail_lines,omitempty"`
}

type PodSummary struct {
	Namespace       string            `json:"namespace"`
	Name            string            `json:"name"`
	Phase           string            `json:"phase"`
	ReadyContainers int               `json:"ready_containers"`
	TotalContainers int               `json:"total_containers"`
	RestartCount    int32             `json:"restart_count"`
	NodeName        string            `json:"node_name,omitempty"`
	PodIP           string            `json:"pod_ip,omitempty"`
	OwnerKind       string            `json:"owner_kind,omitempty"`
	OwnerName       string            `json:"owner_name,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	StartTime       time.Time         `json:"start_time,omitempty"`
	CollectedAt     time.Time         `json:"collected_at"`
}

type DeploymentSummary struct {
	Namespace         string                `json:"namespace"`
	Name              string                `json:"name"`
	Selector          map[string]string     `json:"selector,omitempty"`
	Replicas          int32                 `json:"replicas"`
	ReadyReplicas     int32                 `json:"ready_replicas"`
	UpdatedReplicas   int32                 `json:"updated_replicas"`
	AvailableReplicas int32                 `json:"available_replicas"`
	Strategy          string                `json:"strategy,omitempty"`
	Conditions        []DeploymentCondition `json:"conditions,omitempty"`
	CollectedAt       time.Time             `json:"collected_at"`
}

type DeploymentCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type ServiceSummary struct {
	Namespace   string            `json:"namespace"`
	Name        string            `json:"name"`
	Selector    map[string]string `json:"selector,omitempty"`
	Type        string            `json:"type"`
	ClusterIP   string            `json:"cluster_ip,omitempty"`
	Ports       []ServicePort     `json:"ports,omitempty"`
	CollectedAt time.Time         `json:"collected_at"`
}

type ServicePort struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol"`
	Port       int32  `json:"port"`
	TargetPort string `json:"target_port,omitempty"`
}

type IngressSummary struct {
	Namespace   string        `json:"namespace"`
	Name        string        `json:"name"`
	Hosts       []string      `json:"hosts"`
	Paths       []IngressPath `json:"paths"`
	TLS         []IngressTLS  `json:"tls"`
	Age         time.Duration `json:"age"`
	CollectedAt time.Time     `json:"collected_at"`
}

type IngressPath struct {
	Host     string `json:"host"`
	Path     string `json:"path"`
	PathType string `json:"path_type"`
	Backend  string `json:"backend"`
}

type IngressTLS struct {
	Hosts      []string `json:"hosts"`
	SecretName string   `json:"secret_name"`
}

type NodeSummary struct {
	Name           string          `json:"name"`
	Ready          bool            `json:"ready"`
	Roles          []string        `json:"roles,omitempty"`
	KubeletVersion string          `json:"kubelet_version,omitempty"`
	Capacity       ResourceSummary `json:"capacity,omitempty"`
	Conditions     []NodeCondition `json:"conditions,omitempty"`
	CollectedAt    time.Time       `json:"collected_at"`
}

type ResourceSummary struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

type NodeCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type ConfigMapSummary struct {
	Namespace   string            `json:"namespace"`
	Name        string            `json:"name"`
	DataKeys    []string          `json:"data_keys"`
	Data        map[string]string `json:"data"`
	Age         time.Duration     `json:"age"`
	CollectedAt time.Time         `json:"collected_at"`
}

type EventSummary struct {
	Namespace    string    `json:"namespace,omitempty"`
	Name         string    `json:"name"`
	Type         string    `json:"type,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	Message      string    `json:"message,omitempty"`
	InvolvedKind string    `json:"involved_kind,omitempty"`
	InvolvedName string    `json:"involved_name,omitempty"`
	Count        int32     `json:"count,omitempty"`
	LastSeen     time.Time `json:"last_seen,omitempty"`
	CollectedAt  time.Time `json:"collected_at"`
}

type LogSnippet struct {
	Namespace   string    `json:"namespace"`
	PodName     string    `json:"pod_name"`
	Container   string    `json:"container,omitempty"`
	Lines       []string  `json:"lines"`
	Truncated   bool      `json:"truncated"`
	CollectedAt time.Time `json:"collected_at"`
}

type PVSummary struct {
	Name         string    `json:"name"`
	Capacity     string    `json:"capacity"`
	AccessModes  []string  `json:"access_modes"`
	Status       string    `json:"status"`
	ClaimRef     string    `json:"claim_ref,omitempty"`
	StorageClass string    `json:"storage_class,omitempty"`
	Age          string    `json:"age"`
	CollectedAt  time.Time `json:"collected_at"`
}

type PVCSummary struct {
	Namespace    string    `json:"namespace"`
	Name         string    `json:"name"`
	StorageClass string    `json:"storage_class,omitempty"`
	VolumeName   string    `json:"volume_name,omitempty"`
	AccessModes  []string  `json:"access_modes"`
	Status       string    `json:"status"`
	Age          string    `json:"age"`
	CollectedAt  time.Time `json:"collected_at"`
}

type ResourceQuotaSummary struct {
	Namespace   string                      `json:"namespace"`
	Name        string                      `json:"name"`
	Hard        map[string]ResourceQuantity `json:"hard"`
	Used        map[string]ResourceQuantity `json:"used"`
	Age         time.Duration               `json:"age"`
	CollectedAt time.Time                   `json:"collected_at"`
}

type ResourceQuantity struct {
	Value string `json:"value"`
}

type LimitRangeSummary struct {
	Namespace   string           `json:"namespace"`
	Name        string           `json:"name"`
	Limits      []LimitRangeItem `json:"limits"`
	Age         time.Duration    `json:"age"`
	CollectedAt time.Time        `json:"collected_at"`
}

type LimitRangeItem struct {
	Type    string `json:"type"`
	Min     string `json:"min,omitempty"`
	Max     string `json:"max,omitempty"`
	Default string `json:"default,omitempty"`
}

type HPASummary struct {
	Namespace         string        `json:"namespace"`
	Name              string        `json:"name"`
	Reference         string        `json:"reference"`
	MinReplicas       int32         `json:"min_replicas"`
	MaxReplicas       int32         `json:"max_replicas"`
	CurrentReplicas   int32         `json:"current_replicas"`
	TargetUtilization string        `json:"target_utilization"`
	Age               time.Duration `json:"age"`
	CollectedAt       time.Time     `json:"collected_at"`
}

type DaemonSetSummary struct {
	Namespace    string        `json:"namespace"`
	Name         string        `json:"name"`
	Desired      int32         `json:"desired"`
	Current      int32         `json:"current"`
	Ready        int32         `json:"ready"`
	Updated      int32         `json:"updated"`
	NodeSelector string        `json:"node_selector"`
	Age          time.Duration `json:"age"`
	CollectedAt  time.Time     `json:"collected_at"`
}

type StatefulSetSummary struct {
	Namespace       string        `json:"namespace"`
	Name            string        `json:"name"`
	ReplicasReady   int32         `json:"replicas_ready"`
	ReplicasDesired int32         `json:"replicas_desired"`
	ServiceName     string        `json:"service_name"`
	Age             time.Duration `json:"age"`
	CollectedAt     time.Time     `json:"collected_at"`
}

type JobSummary struct {
	Namespace   string        `json:"namespace"`
	Name        string        `json:"name"`
	Completions string        `json:"completions"`
	Duration    string        `json:"duration"`
	Status      string        `json:"status"`
	Age         time.Duration `json:"age"`
	CollectedAt time.Time     `json:"collected_at"`
}

type EvidenceError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

type TopologyNode struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	Status     string `json:"status,omitempty"`
	DetailPath string `json:"detail_path,omitempty"`
}

type TopologyEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

type TopologyData struct {
	Nodes []TopologyNode `json:"nodes"`
	Edges []TopologyEdge `json:"edges"`
}
