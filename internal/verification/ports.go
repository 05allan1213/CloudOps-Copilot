package verification

import (
	"context"
	"time"
)

type DeliveryUpdate struct {
	Status               string
	CIStatus             string
	PRState              string
	MergedCommitSHA      string
	TargetRevision       string
	DetectedRevision     string
	ArgoSyncStatus       string
	ArgoOperationPhase   string
	ArgoHealthStatus     string
	ResourceHealth       []byte
	SyncStartedAt        *time.Time
	SyncCompletedAt      *time.Time
	DeploymentGeneration int64
	ObservedGeneration   int64
	RolloutRevision      string
	DesiredReplicas      int32
	UpdatedReplicas      int32
	AvailableReplicas    int32
	UnavailableReplicas  int32
	NextPollAt           time.Time
	ObservedAt           time.Time
	FailureReason        string
	ArgoApplication      string
	ArgoProject          string
	Cluster              string
	Environment          string
	Namespace            string
	WorkloadKind         string
	WorkloadName         string
}

type Repository interface {
	ClaimDelivery(context.Context, string, time.Time, time.Duration, time.Duration) (*Delivery, error)
	HeartbeatDelivery(context.Context, uint64, uint64, string, time.Time, time.Duration) error
	PersistDelivery(context.Context, *Delivery, DeliveryUpdate) error
	ReleaseDelivery(context.Context, *Delivery, time.Time, string) error
	GetDeliveryByIncident(context.Context, string) (*Delivery, error)
	FindDeliveredWithoutRun(context.Context) (*Delivery, error)
	CreateRun(context.Context, *Delivery, Plan, time.Time) (*Run, error)
	ClaimRun(context.Context, string, time.Time, time.Duration) (*Run, error)
	HeartbeatRun(context.Context, uint64, uint64, string, time.Time, time.Duration) error
	ReleaseRun(context.Context, *Run, time.Time) error
	ListChecks(context.Context, uint64) ([]Check, error)
	PersistCheckSample(context.Context, *Run, *Check, Sample, time.Time, time.Time) error
	AggregateRun(context.Context, *Run, time.Time) (*Run, error)
	TimeoutRun(context.Context, *Run, time.Time) error
	GetRun(context.Context, string, string) (*Run, error)
	ListRuns(context.Context, string, int, int) (RunPage, error)
	ListRunChecks(context.Context, string, string) ([]Check, error)
	GetPostmortem(context.Context, string) (*Postmortem, error)
}

type PullRequestObservation struct {
	State          string
	Merged         bool
	MergeCommitSHA string
	BaseSHA        string
	HeadSHA        string
}

type CIObservation struct {
	Conclusion string
}

type GitHubReader interface {
	ObservePullRequest(context.Context, string, int64) (PullRequestObservation, error)
	ObserveCI(context.Context, string, string) (CIObservation, error)
}

type ArgoObservation struct {
	TargetRevision   string
	DeployedRevision string
	SyncStatus       string
	HealthStatus     string
	OperationPhase   string
	OperationMessage string
	ResourceHealth   []byte
	LastSyncedAt     *time.Time
}

type ArgoReader interface {
	ObserveApplication(context.Context, string, string) (ArgoObservation, error)
}

type RolloutObservation struct {
	Generation               int64
	ObservedGeneration       int64
	RolloutRevision          string
	DesiredReplicas          int32
	UpdatedReplicas          int32
	AvailableReplicas        int32
	UnavailableReplicas      int32
	Progressing              bool
	Available                bool
	ProgressDeadline         time.Duration
	ProgressDeadlineExceeded bool
	PodsReady                int32
	PodsTotal                int32
}

type RolloutReader interface {
	ObserveDeployment(context.Context, string, string, string) (RolloutObservation, error)
}

type AlertReader interface {
	ResolvedSignal(context.Context, uint64, string, time.Time) (bool, time.Time, error)
}

// The signal ports deliberately accept typed templates and trusted dimensions,
// never provider query language. Phase 5 does not wire them until a bounded
// read adapter exists; unsupported checks are rejected by ValidatePlan.
type MetricTemplate string

const (
	MetricErrorRateBelow     MetricTemplate = "error_rate_below"
	MetricAvailabilityAbove  MetricTemplate = "availability_above"
	MetricReadyEqualsDesired MetricTemplate = "ready_replicas_equal_desired"
	MetricSuccessRateAbove   MetricTemplate = "request_success_rate_above"
)

type SignalQuery struct {
	Template    string
	Service     string
	Namespace   string
	Environment string
	Revision    string
	Lookback    time.Duration
	Step        time.Duration
	MaxSeries   int
	MaxSamples  int
}

type SignalResult struct {
	Value       float64
	SampleCount int
	SeriesCount int
	Truncated   bool
	Observation Observation
}

type MetricReader interface {
	ObserveMetric(context.Context, SignalQuery) (SignalResult, error)
}

type LogReader interface {
	ObserveLogErrorRate(context.Context, SignalQuery) (SignalResult, error)
}

type TraceReader interface {
	ObserveTraceErrorRate(context.Context, SignalQuery) (SignalResult, error)
}
