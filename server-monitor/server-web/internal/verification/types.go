package verification

import (
	"encoding/json"
	"time"
)

type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunPassed    RunStatus = "passed"
	RunFailed    RunStatus = "failed"
	RunTimedOut  RunStatus = "timed_out"
	RunCancelled RunStatus = "cancelled"
)

type CheckStatus string

const (
	CheckPending     CheckStatus = "pending"
	CheckRunning     CheckStatus = "running"
	CheckPassed      CheckStatus = "passed"
	CheckFailed      CheckStatus = "failed"
	CheckUnavailable CheckStatus = "unavailable"
	CheckInvalid     CheckStatus = "invalid"
	CheckCancelled   CheckStatus = "cancelled"
)

type CheckType string

const (
	CheckArgoRevision      CheckType = "argocd_revision"
	CheckArgoSync          CheckType = "argocd_sync"
	CheckArgoHealth        CheckType = "argocd_health"
	CheckDeploymentRollout CheckType = "deployment_rollout"
	CheckWorkloadReady     CheckType = "workload_ready"
	CheckAlertResolved     CheckType = "alert_resolved"
	CheckMetricThreshold   CheckType = "metric_threshold"
	CheckLogErrorRate      CheckType = "log_error_rate"
	CheckTraceErrorRate    CheckType = "trace_error_rate"
)

type Subject struct {
	Repository       string `json:"repository,omitempty"`
	PullRequest      int64  `json:"pull_request,omitempty"`
	Revision         string `json:"revision"`
	ArgoApplication  string `json:"argocd_application,omitempty"`
	ArgoProject      string `json:"argocd_project,omitempty"`
	Cluster          string `json:"cluster,omitempty"`
	Environment      string `json:"environment,omitempty"`
	Namespace        string `json:"namespace,omitempty"`
	WorkloadKind     string `json:"workload_kind,omitempty"`
	WorkloadName     string `json:"workload_name,omitempty"`
	AlertFingerprint string `json:"alert_fingerprint,omitempty"`
}

type CheckSpec struct {
	Type            CheckType       `json:"type"`
	Subject         Subject         `json:"subject"`
	Expected        json.RawMessage `json:"expected"`
	Lookback        time.Duration   `json:"lookback"`
	StabilityWindow time.Duration   `json:"stability_window"`
	Timeout         time.Duration   `json:"timeout"`
	PollInterval    time.Duration   `json:"poll_interval"`
	Source          string          `json:"source"`
	Required        bool            `json:"required"`
}

type Plan struct {
	SchemaVersion  int         `json:"schema_version"`
	TargetRevision string      `json:"target_revision"`
	Checks         []CheckSpec `json:"checks"`
}

type Delivery struct {
	ID                      uint64
	PublicID                string
	IncidentID              uint64
	IncidentPublicID        string
	IncidentFingerprint     string
	ServiceName             string
	RemediationPlanID       uint64
	RemediationPlanPublicID string
	Repository              string
	PRNumber                int64
	PRURL                   string
	HeadBranch              string
	HeadCommitSHA           string
	Status                  string
	CIStatus                string
	PRState                 string
	MergedCommitSHA         string
	TargetRevision          string
	ArgoApplication         string
	ArgoProject             string
	DetectedRevision        string
	ArgoSyncStatus          string
	ArgoOperationPhase      string
	ArgoHealthStatus        string
	ResourceHealth          json.RawMessage
	SyncStartedAt           *time.Time
	SyncCompletedAt         *time.Time
	Cluster                 string
	Environment             string
	Namespace               string
	WorkloadKind            string
	WorkloadName            string
	DeploymentGeneration    int64
	ObservedGeneration      int64
	RolloutRevision         string
	DesiredReplicas         int32
	UpdatedReplicas         int32
	AvailableReplicas       int32
	UnavailableReplicas     int32
	DeliveryStartedAt       *time.Time
	DeliveryDeadlineAt      *time.Time
	DeliveryCompletedAt     *time.Time
	NextPollAt              *time.Time
	LastObservedAt          *time.Time
	LeaseOwner              string
	LeaseExpiresAt          *time.Time
	Attempt                 int
	FailureReason           string
	RowVersion              uint64
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type Run struct {
	ID                uint64
	PublicID          string
	IncidentID        uint64
	IncidentPublicID  string
	RemediationPlanID uint64
	ChangeRequestID   uint64
	Status            RunStatus
	TargetRevision    string
	Plan              Plan
	StartedAt         *time.Time
	DeadlineAt        time.Time
	CompletedAt       *time.Time
	Attempt           int
	LeaseOwner        string
	LeaseExpiresAt    *time.Time
	HeartbeatAt       *time.Time
	LeaseTakeover     bool
	RowVersion        uint64
	ResultSummary     string
	FailureReason     string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Check struct {
	ID                      uint64
	PublicID                string
	VerificationRunID       uint64
	Type                    CheckType
	Status                  CheckStatus
	Required                bool
	Subject                 Subject
	Expected                json.RawMessage
	Observed                json.RawMessage
	SourceReference         string
	Lookback                time.Duration
	StabilityWindow         time.Duration
	Timeout                 time.Duration
	PollInterval            time.Duration
	FirstCheckedAt          *time.Time
	LastCheckedAt           *time.Time
	PassedAt                *time.Time
	ConsecutiveSuccessSince *time.Time
	AttemptCount            int
	FailureReason           string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type SampleStatus string

const (
	SamplePassed      SampleStatus = "passed"
	SampleFailed      SampleStatus = "failed"
	SamplePending     SampleStatus = "pending"
	SampleUnavailable SampleStatus = "unavailable"
	SampleInvalid     SampleStatus = "invalid"
)

type Sample struct {
	Status          SampleStatus
	Observed        json.RawMessage
	SourceReference string
	ReasonCode      string
}

type RunPage struct {
	Items    []Run
	Total    int64
	Page     int
	PageSize int
}
