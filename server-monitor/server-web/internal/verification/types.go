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
	CheckTimedOut    CheckStatus = "timed_out"
	CheckUnavailable CheckStatus = "unavailable"
	CheckInvalid     CheckStatus = "invalid"
	CheckCancelled   CheckStatus = "cancelled"
)

type CheckType string

const (
	CheckArgoRevision            CheckType = "argocd_revision"
	CheckArgoSync                CheckType = "argocd_sync"
	CheckArgoHealth              CheckType = "argocd_health"
	CheckDeploymentRollout       CheckType = "deployment_rollout"
	CheckWorkloadReady           CheckType = "workload_ready"
	CheckAlertResolved           CheckType = "alert_resolved"
	CheckMetricErrorRateBelow    CheckType = "metric_error_rate_below"
	CheckMetricAvailabilityAbove CheckType = "metric_availability_above"
	CheckMetricLatencyP95Below   CheckType = "metric_latency_p95_below"
	CheckLogErrorAbsent          CheckType = "log_error_absent"
	CheckLogErrorRateBelow       CheckType = "log_error_rate_below"
	CheckTraceErrorRateBelow     CheckType = "trace_error_rate_below"
	CheckTraceLatencyP95Below    CheckType = "trace_latency_p95_below"
	// Legacy reserved identifiers remain compile-time compatible with Phase 5
	// tests, but ValidatePlan never authorizes them for execution.
	CheckMetricThreshold CheckType = "metric_threshold"
	CheckLogErrorRate    CheckType = "log_error_rate"
	CheckTraceErrorRate  CheckType = "trace_error_rate"
)

type Comparison string

const (
	CompareLT     Comparison = "lt"
	CompareLTE    Comparison = "lte"
	CompareGT     Comparison = "gt"
	CompareGTE    Comparison = "gte"
	CompareAbsent Comparison = "absent"
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
	Service          string `json:"service,omitempty"`
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
	SourceIdentity  string          `json:"source_identity"`
	ProfileID       string          `json:"profile_id,omitempty"`
	TemplateID      string          `json:"template_id,omitempty"`
	Comparison      Comparison      `json:"comparison,omitempty"`
	Threshold       float64         `json:"threshold,omitempty"`
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
	ProfileID               string
	TemplateID              string
	Comparison              Comparison
	Threshold               float64
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
	SampleTimedOut    SampleStatus = "timed_out"
)

type Sample struct {
	Status          SampleStatus
	Observed        json.RawMessage
	SourceReference string
	ReasonCode      string
}

type ObservationStatus string

const (
	ObservationAvailable   ObservationStatus = "available"
	ObservationNoData      ObservationStatus = "no_data"
	ObservationUnavailable ObservationStatus = "unavailable"
	ObservationMalformed   ObservationStatus = "malformed"
)

// Observation is the bounded provider-neutral fact evaluated by deterministic
// code. It never contains provider query language or an unbounded response.
type Observation struct {
	Status           ObservationStatus `json:"status"`
	Value            float64           `json:"value,omitempty"`
	Numerator        float64           `json:"numerator,omitempty"`
	Denominator      float64           `json:"denominator,omitempty"`
	SampleCount      int               `json:"sample_count"`
	SeriesCount      int               `json:"series_count,omitempty"`
	MatchedCount     int               `json:"matched_count,omitempty"`
	FirstSeen        *time.Time        `json:"first_seen,omitempty"`
	LastSeen         *time.Time        `json:"last_seen,omitempty"`
	SampledAt        time.Time         `json:"sampled_at,omitempty"`
	RedactedExamples []string          `json:"redacted_examples,omitempty"`
	SourceReference  string            `json:"source_reference,omitempty"`
	ReasonCode       string            `json:"reason_code,omitempty"`
}

type RunPage struct {
	Items    []Run
	Total    int64
	Page     int
	PageSize int
}
