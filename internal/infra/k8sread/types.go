package k8sread

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
	Context           string
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
