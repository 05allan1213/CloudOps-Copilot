package taskhandler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/businessbudget"
	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

// deliveryObservePayloadSchema is deliberately independent from the provider
// payloads. A delivery task only carries the durable subject identity; all
// provider facts are read again after the task is claimed.
const deliveryObservePayloadSchema uint32 = 1

const (
	defaultDeliveryObservePoll    = 5 * time.Second
	defaultDeliveryObserveTimeout = 30 * time.Minute
	deliveryObserveMaxAttempts    = 8
)

var (
	deliveryRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
	deliveryHashPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	deliveryDigestPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// DeliveryObservationKind identifies the one authoritative source queried by
// a delivery.observe step. A handler never calls more than one source for one
// claimed task.
type DeliveryObservationKind string

const (
	DeliveryObservePullRequest DeliveryObservationKind = "github.pull_request"
	DeliveryObserveCI          DeliveryObservationKind = "github.ci"
	DeliveryObserveArgo        DeliveryObservationKind = "argocd.application"
	DeliveryObserveRollout     DeliveryObservationKind = "kubernetes.rollout"
)

// DeliveryObserveRequest is a bounded, provider-neutral read contract. The
// observer may use the expected identities to select fixed API paths, but it
// must not accept provider query language from a task payload.
type DeliveryObserveRequest struct {
	Kind DeliveryObservationKind

	Repository  string
	BaseBranch  string
	TargetPath  string
	PullRequest int64
	HeadSHA     string

	ArgoApplication string
	ArgoProject     string
	ArgoRepository  string
	ArgoPath        string

	Cluster      string
	Environment  string
	Namespace    string
	WorkloadKind string
	WorkloadName string
	Container    string

	ExpectedBaseSHA       string
	ExpectedTreeSHA       string
	ExpectedPostImageHash string
	ExpectedSourceSHA     string
	ExpectedImageDigest   string
	ExpectedGitOpsSHA     string
	ExpectedRevision      string
}

// DeliveryObserver is intentionally a single method: an implementation must
// perform one bounded read for the requested source and return only typed,
// bounded facts. GitHub, Argo and Kubernetes adapters can be composed behind
// this interface without making the task handler aware of SDK response types.
type DeliveryObserver interface {
	Observe(context.Context, DeliveryObserveRequest) (DeliveryObservation, error)
}

type DeliveryPullRequestObservation struct {
	Repository          string `json:"repository,omitempty"`
	PullRequest         int64  `json:"pull_request,omitempty"`
	State               string `json:"state"`
	Merged              bool   `json:"merged"`
	MergeCommitSHA      string `json:"merge_commit_sha"`
	BaseSHA             string `json:"base_sha"`
	HeadSHA             string `json:"head_sha"`
	HeadBranch          string `json:"head_branch,omitempty"`
	HeadTreeSHA         string `json:"head_tree_sha"`
	HeadPostImageHash   string `json:"head_post_image_hash"`
	MergedTreeSHA       string `json:"merged_tree_sha"`
	MergedPostImageHash string `json:"merged_post_image_hash"`
	HumanMerged         bool   `json:"human_merged"`
	MergedBy            string `json:"merged_by,omitempty"`
	MergedByType        string `json:"merged_by_type,omitempty"`
	MergeMethod         string `json:"merge_method,omitempty"`
	URL                 string `json:"url,omitempty"`
}

type DeliveryCIObservation struct {
	HeadSHA             string `json:"head_sha"`
	HeadTreeSHA         string `json:"head_tree_sha"`
	HeadPostImageHash   string `json:"head_post_image_hash"`
	Status              string `json:"status"`
	Conclusion          string `json:"conclusion"`
	RequiredChecksValid bool   `json:"required_checks_valid"`
	RequiredCheckName   string `json:"required_check_name,omitempty"`
	ProducerAppID       int64  `json:"producer_app_id,omitempty"`
	WorkflowID          int64  `json:"workflow_id,omitempty"`
	WorkflowPath        string `json:"workflow_path,omitempty"`
}

type DeliveryArgoObservation struct {
	Application        string          `json:"application"`
	Project            string          `json:"project"`
	Repository         string          `json:"repository"`
	Path               string          `json:"path"`
	TargetRevision     string          `json:"target_revision"`
	SyncRevision       string          `json:"sync_revision"`
	SyncResultRevision string          `json:"sync_result_revision"`
	SyncStatus         string          `json:"sync_status"`
	OperationPhase     string          `json:"operation_phase"`
	OperationMessage   string          `json:"operation_message,omitempty"`
	HealthStatus       string          `json:"health_status"`
	ResourceHealth     json.RawMessage `json:"resource_health,omitempty"`
	LastSyncedAt       *time.Time      `json:"last_synced_at,omitempty"`
}

type DeliveryRolloutObservation struct {
	Cluster                  string `json:"cluster"`
	Environment              string `json:"environment"`
	Namespace                string `json:"namespace"`
	WorkloadKind             string `json:"workload_kind"`
	WorkloadName             string `json:"workload_name"`
	Container                string `json:"container"`
	SourceRevision           string `json:"source_revision"`
	ImageDigest              string `json:"image_digest"`
	GitOpsRevision           string `json:"gitops_revision"`
	Generation               int64  `json:"generation"`
	ObservedGeneration       int64  `json:"observed_generation"`
	RolloutRevision          string `json:"rollout_revision"`
	DesiredReplicas          int32  `json:"desired_replicas"`
	UpdatedReplicas          int32  `json:"updated_replicas"`
	ReadyReplicas            int32  `json:"ready_replicas"`
	AvailableReplicas        int32  `json:"available_replicas"`
	UnavailableReplicas      int32  `json:"unavailable_replicas"`
	PodsReady                int32  `json:"pods_ready"`
	PodsTotal                int32  `json:"pods_total"`
	Progressing              bool   `json:"progressing"`
	Available                bool   `json:"available"`
	ProgressDeadlineExceeded bool   `json:"progress_deadline_exceeded"`
}

type DeliveryObservation struct {
	Kind        DeliveryObservationKind         `json:"kind"`
	PullRequest *DeliveryPullRequestObservation `json:"pull_request,omitempty"`
	CI          *DeliveryCIObservation          `json:"ci,omitempty"`
	Argo        *DeliveryArgoObservation        `json:"argocd,omitempty"`
	Rollout     *DeliveryRolloutObservation     `json:"rollout,omitempty"`
}

// DeliveryProjection is the durable ChangeRequest projection updated by one
// observation. Revisions remain separate fields throughout the operation.
type DeliveryProjection struct {
	Status               string
	CIStatus             string
	PRState              string
	PRURL                string
	HeadCommitSHA        string
	MergedCommitSHA      string
	TargetRevision       string
	DetectedRevision     string
	ArgoSyncStatus       string
	ArgoOperationPhase   string
	ArgoHealthStatus     string
	ResourceHealth       json.RawMessage
	SyncStartedAt        *time.Time
	SyncCompletedAt      *time.Time
	DeploymentGeneration int64
	ObservedGeneration   int64
	RolloutRevision      string
	DesiredReplicas      int32
	UpdatedReplicas      int32
	AvailableReplicas    int32
	UnavailableReplicas  int32
	DeliveryStartedAt    *time.Time
	DeliveryDeadlineAt   *time.Time
	DeliveryCompletedAt  *time.Time
	NextPollAt           *time.Time
	LastObservedAt       *time.Time
	FailureReason        string
}

type DeliveryObserveSnapshot struct {
	ChangeRequestID       uint64
	ChangeRequestPublicID string
	IncidentID            uint64
	IncidentPublicID      string
	IncidentFingerprint   string
	IncidentVersion       uint64
	IncidentStatus        string
	CycleNo               uint32
	PlanID                uint64
	PlanPublicID          string
	PlanVersion           uint64
	PlanStatus            string
	Decision              string
	DecisionPlanVersion   uint64
	Repository            string
	BaseBranch            string
	TargetPath            string
	PRNumber              int64
	BaseRevision          string
	LastKnownGoodSHA      string
	ExpectedBeforeHash    string
	ExpectedPostImageHash string
	ExpectedTreeSHA       string
	PolicyHash            string
	VerificationHash      string
	EvidenceSetHash       string
	EvidenceBindings      []remediation.EvidenceBinding
	TargetResource        remediation.TargetResource
	ServiceName           string
	Cluster               string
	Environment           string
	Namespace             string
	WorkloadKind          string
	WorkloadName          string
	Container             string
	ExpectedReplicas      int32
	AlertNames            []string
	SourceRevision        string
	ImageDigest           string
	BaselineGitOpsSHA     string
	ArgoApplication       string
	ArgoProject           string
	ArgoRepository        string
	ArgoPath              string
	Projection            DeliveryProjection
	RowVersion            uint64
	MigratedLegacy        bool
	MigratedLegacyContext bool
	Now                   time.Time
}

type DeliveryObserveOutcome struct {
	Kind             DeliveryObservationKind
	SourceSystem     string
	EventType        string
	FailureCode      string
	FailureSummary   string
	Requeue          bool
	ObservedAt       time.Time
	NextPollAt       time.Time
	Projection       DeliveryProjection
	Observation      DeliveryObservation
	VerificationPlan *verification.Plan
}

type DeliveryObserveStore interface {
	Load(context.Context, asyncjob.Task) (DeliveryObserveSnapshot, error)
	PersistIn(context.Context, asyncjob.DBTX, asyncjob.Task, DeliveryObserveSnapshot, DeliveryObserveOutcome) error
}

type DeliveryObserveConfig struct {
	Observer     DeliveryObserver
	Store        DeliveryObserveStore
	Now          func() time.Time
	PollInterval time.Duration
}

func NewDeliveryObserve(config DeliveryObserveConfig) (Operation, error) {
	if config.Observer == nil || config.Store == nil {
		return nil, errors.New("delivery.observe requires a bounded observer and durable store")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaultDeliveryObservePoll
	}
	if config.PollInterval > time.Minute {
		return nil, errors.New("delivery.observe poll interval exceeds its bound")
	}
	return (&deliveryObserveOperation{cfg: config}).handle, nil
}

type deliveryObserveOperation struct{ cfg DeliveryObserveConfig }

type deliveryObservePayload struct {
	ChangeRequestID string `json:"change_request_id"`
	Step            string `json:"step"`
}

func (o *deliveryObserveOperation) handle(ctx context.Context, execution asyncjob.Execution) asyncjob.Result {
	task := execution.Task
	if dispatchKey(task) != deliveryObserveKey || task.SubjectID == 0 || task.CycleNo == 0 ||
		task.ExpectedSubjectVersion == 0 || task.PayloadSchemaVersion != deliveryObservePayloadSchema ||
		execution.Lease.TaskID != task.ID || execution.Lease.ExpectedSubjectVersion != task.ExpectedSubjectVersion {
		return asyncjob.Dead("invalid_task_subject", "delivery.observe task identity is invalid", nil)
	}
	payload, err := decodeDeliveryObservePayload(task)
	if err != nil {
		return asyncjob.Dead("invalid_delivery_payload", boundChange(err.Error(), 2048), nil)
	}
	snapshot, err := o.cfg.Store.Load(ctx, task)
	if err != nil {
		return deliveryObserveLoadFailure(err)
	}
	if snapshot.ChangeRequestID != task.SubjectID || snapshot.IncidentID != task.IncidentID ||
		snapshot.CycleNo != task.CycleNo || snapshot.RowVersion != task.ExpectedSubjectVersion ||
		(payload.ChangeRequestID != "" && payload.ChangeRequestID != snapshot.ChangeRequestPublicID) || payload.Step != "observe" {
		return asyncjob.Dead("subject_version_mismatch", "delivery subject version or payload is stale", nil)
	}
	if snapshot.Projection.Status == "delivered" || snapshot.Projection.Status == "failed" ||
		snapshot.Projection.Status == "cancelled" || snapshot.Projection.Status == "superseded" {
		return asyncjob.Dead("delivery_terminal", "delivery subject is already terminal", nil)
	}
	now := o.cfg.Now().UTC()
	if !snapshot.Now.IsZero() {
		now = snapshot.Now.UTC()
	}
	kind := deliveryObservationKind(snapshot)
	if kind == "" {
		return asyncjob.Dead("delivery_status_invalid", "ChangeRequest status has no authorized observer", nil)
	}
	request := deliveryObserveRequest(snapshot, kind)
	externalCtx, cancel, err := asyncjob.ExternalCallContext(ctx)
	if err != nil {
		return asyncjob.RetryAfter(0, "external_deadline_missing", "delivery observer deadline is unavailable", nil)
	}
	observation, observeErr := o.cfg.Observer.Observe(externalCtx, request)
	cancel()
	if observeErr != nil {
		return deliveryObserveProviderFailure(observeErr)
	}
	if observation.Kind != kind {
		return asyncjob.RetryAfter(0, "delivery_observation_invalid", "observer returned a different source kind", nil)
	}
	encodedObservation, encodeErr := json.Marshal(observation)
	if encodeErr != nil || len(encodedObservation) > 12*1024 {
		return asyncjob.RetryAfter(0, "delivery_observation_invalid", "observer returned an unbounded observation", nil)
	}
	outcome, err := evaluateDeliveryObservation(snapshot, observation, now, o.cfg.PollInterval)
	if err != nil {
		if errors.Is(err, errDeliveryMalformedObservation) {
			return asyncjob.RetryAfter(0, "delivery_observation_invalid", boundChange(err.Error(), 2048), nil)
		}
		return asyncjob.Dead("delivery_observation_rejected", boundChange(err.Error(), 2048), nil)
	}
	return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
		return o.cfg.Store.PersistIn(ctx, tx, task, snapshot, outcome)
	})
}

func decodeDeliveryObservePayload(task asyncjob.Task) (deliveryObservePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(task.Payload)))
	decoder.DisallowUnknownFields()
	var payload deliveryObservePayload
	if err := decoder.Decode(&payload); err != nil {
		return deliveryObservePayload{}, errors.New("delivery.observe payload is malformed")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return deliveryObservePayload{}, errors.New("delivery.observe payload has multiple JSON values")
	}
	if strings.TrimSpace(payload.ChangeRequestID) == "" || payload.Step == "" {
		return deliveryObservePayload{}, errors.New("delivery.observe payload requires change_request_id and step")
	}
	return payload, nil
}

func deliveryObserveLoadFailure(err error) asyncjob.Result {
	switch {
	case errors.Is(err, asyncjob.ErrSubjectVersionMismatch), errors.Is(err, asyncjob.ErrLeaseLost):
		return asyncjob.Dead("subject_version_mismatch", boundChange(err.Error(), 2048), nil)
	case errors.Is(err, asyncjob.ErrPolicyViolation), errors.Is(err, asyncjob.ErrInvalidMutation), errors.Is(err, verification.ErrInvalidArgument), errors.Is(err, verification.ErrNotAllowed):
		return asyncjob.Dead("delivery_preflight_rejected", boundChange(err.Error(), 2048), nil)
	case errors.Is(err, sql.ErrNoRows):
		return asyncjob.Dead("delivery_subject_missing", "change_request or its approved delivery facts no longer exist", nil)
	default:
		return asyncjob.RetryAfter(0, "delivery_store_unavailable", boundChange(err.Error(), 2048), nil)
	}
}

func deliveryObserveProviderFailure(err error) asyncjob.Result {
	switch {
	case errors.Is(err, change.ErrPermission):
		return asyncjob.Dead("delivery_config_error", boundChange(err.Error(), 2048), nil)
	case errors.Is(err, change.ErrNotAllowed), errors.Is(err, verification.ErrNotAllowed):
		return asyncjob.Dead("delivery_scope_rejected", boundChange(err.Error(), 2048), nil)
	case errors.Is(err, verification.ErrInvalidArgument):
		return asyncjob.RetryAfter(0, "delivery_observation_invalid", boundChange(err.Error(), 2048), nil)
	default:
		return asyncjob.RetryAfter(0, "delivery_provider_unavailable", boundChange(err.Error(), 2048), nil)
	}
}

var errDeliveryMalformedObservation = errors.New("malformed delivery observation")

func deliveryObservationKind(snapshot DeliveryObserveSnapshot) DeliveryObservationKind {
	switch snapshot.Projection.Status {
	case "pr_open":
		switch snapshot.Projection.CIStatus {
		case "pending":
			return DeliveryObserveCI
		case "passing":
			return DeliveryObservePullRequest
		default:
			return ""
		}
	case "merged", "syncing":
		return DeliveryObserveArgo
	case "rolling_out":
		return DeliveryObserveRollout
	default:
		return ""
	}
}

func deliveryObserveRequest(snapshot DeliveryObserveSnapshot, kind DeliveryObservationKind) DeliveryObserveRequest {
	return DeliveryObserveRequest{
		Kind: kind, Repository: snapshot.Repository, BaseBranch: snapshot.BaseBranch, TargetPath: snapshot.TargetPath,
		PullRequest: snapshot.PRNumber, HeadSHA: snapshot.Projection.HeadCommitSHA,
		ArgoApplication: snapshot.ArgoApplication, ArgoProject: snapshot.ArgoProject, ArgoRepository: snapshot.ArgoRepository, ArgoPath: snapshot.ArgoPath,
		Cluster: snapshot.Cluster, Environment: snapshot.Environment, Namespace: snapshot.Namespace, WorkloadKind: snapshot.WorkloadKind, WorkloadName: snapshot.WorkloadName, Container: snapshot.Container,
		ExpectedBaseSHA: snapshot.BaseRevision, ExpectedTreeSHA: snapshot.ExpectedTreeSHA, ExpectedPostImageHash: snapshot.ExpectedPostImageHash,
		ExpectedSourceSHA: snapshot.SourceRevision, ExpectedImageDigest: snapshot.ImageDigest, ExpectedGitOpsSHA: snapshot.Projection.TargetRevision, ExpectedRevision: snapshot.Projection.MergedCommitSHA,
	}
}

func evaluateDeliveryObservation(snapshot DeliveryObserveSnapshot, observation DeliveryObservation, now time.Time, poll time.Duration) (DeliveryObserveOutcome, error) {
	next := cloneDeliveryProjection(snapshot.Projection)
	if next.DeliveryStartedAt == nil {
		started := now
		next.DeliveryStartedAt = &started
	}
	if next.DeliveryDeadlineAt == nil {
		deadline := now.Add(defaultDeliveryObserveTimeout)
		next.DeliveryDeadlineAt = &deadline
	}
	next.LastObservedAt = ptrDeliveryTime(now)
	next.NextPollAt = ptrDeliveryTime(now.Add(poll))
	outcome := DeliveryObserveOutcome{Kind: observation.Kind, SourceSystem: deliverySourceSystem(observation.Kind), EventType: "delivery_observed", Requeue: true, ObservedAt: now, NextPollAt: now.Add(poll), Projection: next, Observation: observation}
	if next.DeliveryDeadlineAt != nil && !now.Before(next.DeliveryDeadlineAt.UTC()) {
		code := deliveryTimeoutCode(snapshot.Projection.Status)
		return deliveryBusinessFailure(outcome, code, code), nil
	}
	switch observation.Kind {
	case DeliveryObservePullRequest:
		if observation.PullRequest == nil {
			return DeliveryObserveOutcome{}, fmt.Errorf("%w: pull request facts are absent", errDeliveryMalformedObservation)
		}
		return evaluateDeliveryPullRequest(snapshot, *observation.PullRequest, outcome)
	case DeliveryObserveCI:
		if observation.CI == nil {
			return DeliveryObserveOutcome{}, fmt.Errorf("%w: CI facts are absent", errDeliveryMalformedObservation)
		}
		return evaluateDeliveryCI(snapshot, *observation.CI, outcome)
	case DeliveryObserveArgo:
		if observation.Argo == nil {
			return DeliveryObserveOutcome{}, fmt.Errorf("%w: Argo facts are absent", errDeliveryMalformedObservation)
		}
		return evaluateDeliveryArgo(snapshot, *observation.Argo, outcome)
	case DeliveryObserveRollout:
		if observation.Rollout == nil {
			return DeliveryObserveOutcome{}, fmt.Errorf("%w: rollout facts are absent", errDeliveryMalformedObservation)
		}
		return evaluateDeliveryRollout(snapshot, *observation.Rollout, outcome)
	default:
		return DeliveryObserveOutcome{}, fmt.Errorf("%w: unsupported source kind %q", errDeliveryMalformedObservation, observation.Kind)
	}
}

func evaluateDeliveryPullRequest(snapshot DeliveryObserveSnapshot, pr DeliveryPullRequestObservation, outcome DeliveryObserveOutcome) (DeliveryObserveOutcome, error) {
	state := strings.ToLower(strings.TrimSpace(pr.State))
	if state != "open" && state != "closed" {
		return DeliveryObserveOutcome{}, fmt.Errorf("%w: pull request state is invalid", errDeliveryMalformedObservation)
	}
	if !deliveryRevisionPattern.MatchString(strings.ToLower(pr.HeadSHA)) || !deliveryRevisionPattern.MatchString(strings.ToLower(snapshot.Projection.HeadCommitSHA)) ||
		!deliveryRevisionPattern.MatchString(strings.ToLower(pr.BaseSHA)) || !strings.EqualFold(pr.HeadSHA, snapshot.Projection.HeadCommitSHA) || !strings.EqualFold(pr.BaseSHA, snapshot.BaseRevision) {
		return deliveryBusinessFailure(outcome, "revision_mismatch", "pull request head or base revision does not match the approved change"), nil
	}
	if !deliveryRevisionPattern.MatchString(strings.ToLower(pr.HeadTreeSHA)) || !strings.EqualFold(pr.HeadTreeSHA, snapshot.ExpectedTreeSHA) ||
		!deliveryHashPattern.MatchString(strings.ToLower(pr.HeadPostImageHash)) || !strings.EqualFold(pr.HeadPostImageHash, snapshot.ExpectedPostImageHash) {
		return deliveryBusinessFailure(outcome, "approved_tree_mismatch", "pull request tree or post-image differs from the approved plan"), nil
	}
	if strings.EqualFold(pr.State, "closed") && !pr.Merged {
		return deliveryBusinessFailure(outcome, "pr_closed_without_merge", "the approved pull request was closed without a merge"), nil
	}
	if pr.Merged {
		if snapshot.Projection.Status != "pr_open" || snapshot.Projection.CIStatus != "passing" {
			return deliveryBusinessFailure(outcome, "merge_before_required_ci", "pull request merged before the required CI observation"), nil
		}
		if !deliveryRevisionPattern.MatchString(strings.ToLower(pr.MergeCommitSHA)) ||
			!deliveryRevisionPattern.MatchString(strings.ToLower(pr.MergedTreeSHA)) ||
			!deliveryHashPattern.MatchString(strings.ToLower(pr.MergedPostImageHash)) ||
			!strings.EqualFold(pr.MergedTreeSHA, snapshot.ExpectedTreeSHA) ||
			!strings.EqualFold(pr.MergedPostImageHash, snapshot.ExpectedPostImageHash) || !pr.HumanMerged || strings.TrimSpace(pr.MergedBy) == "" || !strings.EqualFold(pr.MergedByType, "User") || !strings.EqualFold(pr.MergeMethod, "squash") {
			return deliveryBusinessFailure(outcome, "merged_tree_mismatch", "human merge did not preserve the approved tree/post-image"), nil
		}
		outcome.Projection.Status = "merged"
		outcome.Projection.MergedCommitSHA = strings.ToLower(pr.MergeCommitSHA)
		outcome.Projection.TargetRevision = strings.ToLower(pr.MergeCommitSHA)
		outcome.Projection.PRState = strings.ToLower(pr.State)
		if strings.TrimSpace(pr.URL) != "" {
			outcome.Projection.PRURL = boundChange(pr.URL, 1024)
		}
		outcome.Requeue = true
		return outcome, nil
	}
	outcome.Projection.Status = "pr_open"
	outcome.Projection.PRState = strings.ToLower(strings.TrimSpace(pr.State))
	if strings.TrimSpace(pr.URL) != "" {
		outcome.Projection.PRURL = boundChange(pr.URL, 1024)
	}
	return outcome, nil
}

func evaluateDeliveryCI(snapshot DeliveryObserveSnapshot, ci DeliveryCIObservation, outcome DeliveryObserveOutcome) (DeliveryObserveOutcome, error) {
	if !deliveryRevisionPattern.MatchString(strings.ToLower(ci.HeadSHA)) || !strings.EqualFold(ci.HeadSHA, snapshot.Projection.HeadCommitSHA) ||
		!deliveryRevisionPattern.MatchString(strings.ToLower(ci.HeadTreeSHA)) || !strings.EqualFold(ci.HeadTreeSHA, snapshot.ExpectedTreeSHA) ||
		!deliveryHashPattern.MatchString(strings.ToLower(ci.HeadPostImageHash)) || !strings.EqualFold(ci.HeadPostImageHash, snapshot.ExpectedPostImageHash) {
		return deliveryBusinessFailure(outcome, "ci_tree_mismatch", "CI did not validate the approved head tree/post-image"), nil
	}
	if !ci.RequiredChecksValid || strings.TrimSpace(ci.RequiredCheckName) == "" || ci.ProducerAppID <= 0 || ci.WorkflowID <= 0 || strings.TrimSpace(ci.WorkflowPath) == "" {
		return deliveryBusinessFailure(outcome, "ci_contract_mismatch", "required CI producer/workflow contract did not match"), nil
	}
	status, conclusion := strings.ToLower(strings.TrimSpace(ci.Status)), strings.ToLower(strings.TrimSpace(ci.Conclusion))
	if status != "queued" && status != "in_progress" && status != "completed" {
		return DeliveryObserveOutcome{}, fmt.Errorf("%w: CI status is invalid", errDeliveryMalformedObservation)
	}
	if status == "completed" && conclusion != "success" && conclusion != "failure" && conclusion != "cancelled" && conclusion != "timed_out" {
		return DeliveryObserveOutcome{}, fmt.Errorf("%w: CI conclusion is invalid", errDeliveryMalformedObservation)
	}
	outcome.Projection.Status = "pr_open"
	switch {
	case conclusion == "failure" || conclusion == "cancelled" || conclusion == "timed_out":
		outcome.Projection.CIStatus = "failing"
		return deliveryBusinessFailure(outcome, "required_ci_failed", "required CI did not pass"), nil
	case status == "completed" && conclusion == "success":
		outcome.Projection.CIStatus = "passing"
	default:
		outcome.Projection.CIStatus = "pending"
	}
	return outcome, nil
}

func evaluateDeliveryArgo(snapshot DeliveryObserveSnapshot, app DeliveryArgoObservation, outcome DeliveryObserveOutcome) (DeliveryObserveOutcome, error) {
	if strings.TrimSpace(app.Application) == "" || strings.TrimSpace(app.Project) == "" || strings.TrimSpace(app.Repository) == "" || strings.TrimSpace(app.Path) == "" || strings.TrimSpace(app.TargetRevision) == "" {
		return DeliveryObserveOutcome{}, fmt.Errorf("%w: Argo application identity is incomplete", errDeliveryMalformedObservation)
	}
	if app.Application != snapshot.ArgoApplication || app.Project != snapshot.ArgoProject || app.Repository != snapshot.ArgoRepository || app.Path != snapshot.ArgoPath || app.TargetRevision != snapshot.BaseBranch {
		return deliveryBusinessFailure(outcome, "argocd_application_mismatch", "Argo application source is outside the fixed GitOps boundary"), nil
	}
	expected := strings.ToLower(snapshot.Projection.TargetRevision)
	if !deliveryRevisionPattern.MatchString(expected) {
		return deliveryBusinessFailure(outcome, "merged_revision_invalid", "merged commit SHA is not an exact revision"), nil
	}
	syncRevision, resultRevision := strings.ToLower(strings.TrimSpace(app.SyncRevision)), strings.ToLower(strings.TrimSpace(app.SyncResultRevision))
	if strings.EqualFold(app.OperationPhase, "Failed") || strings.EqualFold(app.OperationPhase, "Error") {
		return deliveryBusinessFailure(outcome, "argocd_operation_failed", boundChange(app.OperationMessage, 2048)), nil
	}
	if syncRevision != "" && syncRevision != expected && syncRevision != strings.ToLower(snapshot.BaselineGitOpsSHA) {
		return deliveryBusinessFailure(outcome, "revision_superseded", "Argo observed a revision other than the merged SHA"), nil
	}
	if resultRevision != "" && resultRevision != expected && resultRevision != strings.ToLower(snapshot.BaselineGitOpsSHA) {
		return deliveryBusinessFailure(outcome, "revision_superseded", "Argo syncResult observed a revision other than the merged SHA"), nil
	}
	outcome.Projection.DetectedRevision = syncRevision
	outcome.Projection.ArgoSyncStatus = boundChange(app.SyncStatus, 32)
	outcome.Projection.ArgoOperationPhase = boundChange(app.OperationPhase, 32)
	outcome.Projection.ArgoHealthStatus = boundChange(app.HealthStatus, 32)
	outcome.Projection.ResourceHealth = cloneJSON(app.ResourceHealth)
	outcome.Projection.Status = "syncing"
	if syncRevision == expected && resultRevision == expected && strings.EqualFold(app.SyncStatus, "Synced") && strings.EqualFold(app.OperationPhase, "Succeeded") {
		outcome.Projection.Status = "rolling_out"
		outcome.Projection.SyncCompletedAt = cloneTime(app.LastSyncedAt)
	} else if outcome.Projection.SyncStartedAt == nil {
		outcome.Projection.SyncStartedAt = ptrDeliveryTime(outcome.ObservedAt)
	}
	return outcome, nil
}

func evaluateDeliveryRollout(snapshot DeliveryObserveSnapshot, rollout DeliveryRolloutObservation, outcome DeliveryObserveOutcome) (DeliveryObserveOutcome, error) {
	if rollout.Cluster == "" || rollout.Environment == "" || rollout.Namespace == "" || rollout.WorkloadKind == "" || rollout.WorkloadName == "" || rollout.Container == "" {
		return DeliveryObserveOutcome{}, fmt.Errorf("%w: Kubernetes rollout identity is incomplete", errDeliveryMalformedObservation)
	}
	if rollout.Generation < 0 || rollout.ObservedGeneration < 0 || rollout.DesiredReplicas < 0 || rollout.UpdatedReplicas < 0 || rollout.ReadyReplicas < 0 || rollout.AvailableReplicas < 0 || rollout.UnavailableReplicas < 0 || rollout.PodsReady < 0 || rollout.PodsTotal < 0 {
		return DeliveryObserveOutcome{}, fmt.Errorf("%w: Kubernetes rollout counts are invalid", errDeliveryMalformedObservation)
	}
	if rollout.Cluster != snapshot.Cluster || rollout.Environment != snapshot.Environment || rollout.Namespace != snapshot.Namespace || rollout.WorkloadKind != snapshot.WorkloadKind ||
		rollout.WorkloadName != snapshot.WorkloadName || rollout.Container != snapshot.Container {
		return deliveryBusinessFailure(outcome, "rollout_target_mismatch", "Kubernetes rollout target is outside the approved workload"), nil
	}
	if !deliveryRevisionPattern.MatchString(strings.ToLower(rollout.SourceRevision)) || !strings.EqualFold(rollout.SourceRevision, snapshot.SourceRevision) ||
		!deliveryDigestPattern.MatchString(strings.ToLower(rollout.ImageDigest)) || !strings.EqualFold(rollout.ImageDigest, snapshot.ImageDigest) ||
		!deliveryRevisionPattern.MatchString(strings.ToLower(rollout.GitOpsRevision)) || !strings.EqualFold(rollout.GitOpsRevision, snapshot.Projection.TargetRevision) {
		return deliveryBusinessFailure(outcome, "deployment_identity_mismatch", "Kubernetes rollout did not preserve source/image and merged GitOps identities"), nil
	}
	outcome.Projection.DeploymentGeneration = rollout.Generation
	outcome.Projection.ObservedGeneration = rollout.ObservedGeneration
	outcome.Projection.RolloutRevision = boundChange(rollout.RolloutRevision, 64)
	outcome.Projection.DesiredReplicas = rollout.DesiredReplicas
	outcome.Projection.UpdatedReplicas = rollout.UpdatedReplicas
	outcome.Projection.AvailableReplicas = rollout.AvailableReplicas
	outcome.Projection.UnavailableReplicas = rollout.UnavailableReplicas
	if rollout.ProgressDeadlineExceeded {
		return deliveryBusinessFailure(outcome, "progress_deadline_exceeded", "Kubernetes rollout exceeded its progress deadline"), nil
	}
	if rollout.ObservedGeneration >= rollout.Generation && rollout.Generation > 0 &&
		rollout.DesiredReplicas == snapshot.ExpectedReplicas && rollout.UpdatedReplicas == rollout.DesiredReplicas && rollout.ReadyReplicas == rollout.DesiredReplicas && rollout.AvailableReplicas == rollout.DesiredReplicas && rollout.UnavailableReplicas == 0 &&
		rollout.PodsReady == rollout.PodsTotal && rollout.PodsTotal == rollout.DesiredReplicas && rollout.Progressing && rollout.Available {
		outcome.Projection.Status = "delivered"
		completed := outcome.ObservedAt
		outcome.Projection.DeliveryCompletedAt = &completed
		outcome.Projection.NextPollAt = nil
		outcome.EventType = "delivery_delivered"
		outcome.Requeue = false
		plan, err := verification.CompilePlan(verification.CompileInput{
			TriggerType: "post_delivery", Repository: snapshot.Repository, PullRequest: snapshot.PRNumber, TargetRevision: snapshot.Projection.TargetRevision,
			SourceRevision: snapshot.SourceRevision, ImageDigest: snapshot.ImageDigest, GitOpsRevision: snapshot.Projection.TargetRevision,
			ArgoApplication: snapshot.ArgoApplication, ArgoProject: snapshot.ArgoProject, Cluster: snapshot.Cluster, Environment: snapshot.Environment,
			Namespace: snapshot.Namespace, Service: snapshot.ServiceName, WorkloadName: snapshot.WorkloadName, AlertNames: snapshot.AlertNames,
		})
		if err != nil {
			return DeliveryObserveOutcome{}, fmt.Errorf("%w: compile post-delivery verification plan: %v", errDeliveryMalformedObservation, err)
		}
		outcome.VerificationPlan = &plan
	}
	return outcome, nil
}

func deliveryBusinessFailure(outcome DeliveryObserveOutcome, code, summary string) DeliveryObserveOutcome {
	outcome.FailureCode, outcome.FailureSummary = boundChange(code, 128), boundChange(summary, 2048)
	outcome.Projection.Status = "failed"
	outcome.Projection.FailureReason = outcome.FailureCode
	outcome.EventType, outcome.Requeue = "delivery_failed", false
	completed := outcome.ObservedAt
	outcome.Projection.DeliveryCompletedAt = &completed
	outcome.Projection.NextPollAt = nil
	return outcome
}

func deliveryTimeoutCode(status string) string {
	switch status {
	case "pr_open":
		return "merge_timeout"
	case "merged", "syncing":
		return "argocd_timeout"
	default:
		return "rollout_timeout"
	}
}

func deliverySourceSystem(kind DeliveryObservationKind) string {
	switch kind {
	case DeliveryObservePullRequest, DeliveryObserveCI:
		return "github"
	case DeliveryObserveArgo:
		return "argocd"
	case DeliveryObserveRollout:
		return "kubernetes"
	default:
		return "system"
	}
}

func cloneDeliveryProjection(in DeliveryProjection) DeliveryProjection {
	out := in
	out.ResourceHealth = cloneJSON(in.ResourceHealth)
	out.SyncStartedAt = cloneTime(in.SyncStartedAt)
	out.SyncCompletedAt = cloneTime(in.SyncCompletedAt)
	out.DeliveryStartedAt = cloneTime(in.DeliveryStartedAt)
	out.DeliveryDeadlineAt = cloneTime(in.DeliveryDeadlineAt)
	out.DeliveryCompletedAt = cloneTime(in.DeliveryCompletedAt)
	out.NextPollAt = cloneTime(in.NextPollAt)
	out.LastObservedAt = cloneTime(in.LastObservedAt)
	return out
}

func cloneJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := value.UTC()
	return &copyValue
}

func ptrDeliveryTime(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

// mysqlDeliveryObserveStore is deliberately private behind the small store
// interface above. This keeps task-fenced business logic testable without a
// fake provider or a second persistence implementation.
type DeliveryObserveTaskStore interface {
	EnqueueIn(context.Context, asyncjob.DBTX, asyncjob.NewTask) (*asyncjob.Task, error)
}

type DeliveryObserveTarget struct {
	ArgoApplication string
	ArgoProject     string
	ArgoRepository  string
	ArgoPath        string
	DesiredReplicas int32
}

type MySQLDeliveryObserveConfig struct {
	DB              *sql.DB
	Tasks           DeliveryObserveTaskStore
	Target          DeliveryObserveTarget
	Now             func() time.Time
	PollInterval    time.Duration
	DeliveryTimeout time.Duration
}

func NewMySQLDeliveryObserveStore(config MySQLDeliveryObserveConfig) (DeliveryObserveStore, error) {
	if config.DB == nil || config.Tasks == nil {
		return nil, errors.New("delivery.observe MySQL store requires database and async task store")
	}
	if strings.TrimSpace(config.Target.ArgoApplication) == "" || strings.TrimSpace(config.Target.ArgoProject) == "" || strings.TrimSpace(config.Target.ArgoRepository) == "" || strings.TrimSpace(config.Target.ArgoPath) == "" {
		return nil, errors.New("delivery.observe MySQL store requires fixed Argo source identity")
	}
	if config.Target.DesiredReplicas <= 0 {
		config.Target.DesiredReplicas = 2
	}
	if config.Target.DesiredReplicas != 2 {
		return nil, errors.New("delivery.observe Golden rollout requires exactly two replicas")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaultDeliveryObservePoll
	}
	if config.PollInterval > time.Minute {
		return nil, errors.New("delivery.observe MySQL poll interval exceeds its bound")
	}
	if config.DeliveryTimeout <= 0 {
		config.DeliveryTimeout = defaultDeliveryObserveTimeout
	}
	if config.DeliveryTimeout < time.Minute || config.DeliveryTimeout > 24*time.Hour {
		return nil, errors.New("delivery.observe MySQL delivery timeout is outside its bound")
	}
	return &mysqlDeliveryObserveStore{cfg: config}, nil
}

type mysqlDeliveryObserveStore struct{ cfg MySQLDeliveryObserveConfig }

func (s *mysqlDeliveryObserveStore) Load(ctx context.Context, task asyncjob.Task) (result DeliveryObserveSnapshot, retErr error) {
	if task.SubjectType != "change_request" || task.SubjectID == 0 || task.CycleNo == 0 {
		return DeliveryObserveSnapshot{}, asyncjob.ErrInvalidMutation
	}
	var snapshot DeliveryObserveSnapshot
	var targetJSON, evidenceJSON []byte
	var decisionPlanVersion uint64
	var decision, planStatus string
	var approvedBase, approvedPost, approvedTree, approvedPolicy, approvedVerification, approvedEvidence string
	var prNumber int64
	var prState, prURL, status, ciStatus string
	var resourceHealth []byte
	var syncStarted, syncCompleted, deliveryStarted, deliveryDeadline, deliveryCompleted, nextPoll, lastObserved sql.NullTime
	var argoApplication, argoProject string
	var changeMigratedLegacy, changeMigratedLegacyContext bool
	var planMigratedLegacy, planMigratedLegacyContext bool
	row := s.cfg.DB.QueryRowContext(ctx, `
	SELECT cr.id, cr.public_id, cr.incident_id, i.public_id, i.fingerprint, i.version, i.status,
	       cr.cycle_no, p.id, p.public_id, p.plan_version, p.status,
	       cr.migrated_legacy, cr.migrated_legacy_context, p.migrated_legacy, p.migrated_legacy_context,
       d.decision, d.plan_version, d.approved_base_sha, d.approved_post_image_hash,
       d.approved_tree_hash, d.approved_policy_hash, d.approved_verification_hash, d.approved_evidence_set_hash,
       cr.repository, p.target_base_branch, p.target_path,
       cr.base_revision, p.last_known_good_sha, p.expected_before_hash, p.expected_post_image_hash,
       p.expected_tree_hash, p.policy_snapshot_hash, p.verification_plan_hash, p.evidence_set_hash,
       p.target_resource_json, i.service_name, i.cluster, i.environment, i.namespace, i.target_kind, i.target_name,
       p.evidence_bindings_json, cr.status, cr.ci_status, cr.pr_state, cr.pr_url, cr.commit_sha, cr.pr_number,
       cr.merged_commit_sha, cr.target_revision, cr.detected_revision, cr.argocd_sync_status,
       cr.argocd_operation_phase, cr.argocd_health_status, cr.resource_health_json, cr.sync_started_at,
       cr.sync_completed_at, cr.deployment_generation, cr.observed_generation, cr.rollout_revision,
       cr.desired_replicas, cr.updated_replicas, cr.available_replicas, cr.unavailable_replicas,
       cr.delivery_started_at, cr.delivery_deadline_at, cr.delivery_completed_at, cr.next_poll_at,
	       cr.last_observed_at, cr.failure_reason, cr.row_version, cr.argocd_application, cr.argocd_project
	FROM change_requests cr
	JOIN incidents i ON i.id = cr.incident_id
	JOIN remediation_plans p ON p.id = cr.plan_id AND p.incident_id = cr.incident_id AND p.cycle_no = cr.cycle_no
	JOIN remediation_decisions d ON d.plan_id = p.id AND d.incident_id = p.incident_id AND d.cycle_no = p.cycle_no
	  AND d.imported_history = FALSE
	WHERE cr.id = ? AND cr.incident_id = ? AND cr.cycle_no = ?`, task.SubjectID, task.IncidentID, task.CycleNo)
	if err := row.Scan(&snapshot.ChangeRequestID, &snapshot.ChangeRequestPublicID, &snapshot.IncidentID, &snapshot.IncidentPublicID, &snapshot.IncidentFingerprint, &snapshot.IncidentVersion, &snapshot.IncidentStatus,
		&snapshot.CycleNo, &snapshot.PlanID, &snapshot.PlanPublicID, &snapshot.PlanVersion, &planStatus,
		&changeMigratedLegacy, &changeMigratedLegacyContext, &planMigratedLegacy, &planMigratedLegacyContext,
		&decision, &decisionPlanVersion, &approvedBase, &approvedPost, &approvedTree, &approvedPolicy, &approvedVerification, &approvedEvidence,
		&snapshot.Repository, &snapshot.BaseBranch, &snapshot.TargetPath,
		&snapshot.BaseRevision, &snapshot.LastKnownGoodSHA, &snapshot.ExpectedBeforeHash, &snapshot.ExpectedPostImageHash,
		&snapshot.ExpectedTreeSHA, &snapshot.PolicyHash, &snapshot.VerificationHash, &snapshot.EvidenceSetHash,
		&targetJSON, &snapshot.ServiceName, &snapshot.Cluster, &snapshot.Environment, &snapshot.Namespace, &snapshot.WorkloadKind, &snapshot.WorkloadName,
		&evidenceJSON, &status, &ciStatus, &prState, &prURL, &snapshot.Projection.HeadCommitSHA, &prNumber,
		&snapshot.Projection.MergedCommitSHA, &snapshot.Projection.TargetRevision, &snapshot.Projection.DetectedRevision, &snapshot.Projection.ArgoSyncStatus,
		&snapshot.Projection.ArgoOperationPhase, &snapshot.Projection.ArgoHealthStatus, &resourceHealth, &syncStarted,
		&syncCompleted, &snapshot.Projection.DeploymentGeneration, &snapshot.Projection.ObservedGeneration, &snapshot.Projection.RolloutRevision,
		&snapshot.Projection.DesiredReplicas, &snapshot.Projection.UpdatedReplicas, &snapshot.Projection.AvailableReplicas, &snapshot.Projection.UnavailableReplicas,
		&deliveryStarted, &deliveryDeadline, &deliveryCompleted, &nextPoll, &lastObserved, &snapshot.Projection.FailureReason, &snapshot.RowVersion, &argoApplication, &argoProject); err != nil {
		return DeliveryObserveSnapshot{}, err
	}
	snapshot.PlanStatus, snapshot.Decision, snapshot.DecisionPlanVersion = planStatus, decision, decisionPlanVersion
	snapshot.MigratedLegacy, snapshot.MigratedLegacyContext = changeMigratedLegacy, changeMigratedLegacyContext
	if planMigratedLegacy != changeMigratedLegacy || planMigratedLegacyContext != changeMigratedLegacyContext ||
		changeMigratedLegacy != task.MigratedLegacy || changeMigratedLegacyContext != task.MigratedLegacyContext {
		return DeliveryObserveSnapshot{}, asyncjob.ErrSubjectVersionMismatch
	}
	snapshot.Projection.Status, snapshot.Projection.CIStatus, snapshot.Projection.PRState, snapshot.Projection.PRURL = status, ciStatus, prState, prURL
	snapshot.Projection.ResourceHealth = cloneJSON(resourceHealth)
	snapshot.Projection.SyncStartedAt, snapshot.Projection.SyncCompletedAt = nullTimeValue(syncStarted), nullTimeValue(syncCompleted)
	snapshot.Projection.DeliveryStartedAt, snapshot.Projection.DeliveryDeadlineAt, snapshot.Projection.DeliveryCompletedAt = nullTimeValue(deliveryStarted), nullTimeValue(deliveryDeadline), nullTimeValue(deliveryCompleted)
	snapshot.Projection.NextPollAt, snapshot.Projection.LastObservedAt = nullTimeValue(nextPoll), nullTimeValue(lastObserved)
	snapshot.PRNumber = prNumber
	if snapshot.IncidentStatus != "delivering" || snapshot.Projection.Status == "" || snapshot.Decision != "approved" {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: delivery plan or approval is not active", asyncjob.ErrPolicyViolation)
	}
	if snapshot.PlanStatus != "consumed" {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: delivery Plan is not consumed", asyncjob.ErrPolicyViolation)
	}
	if snapshot.Projection.Status != "pr_open" && snapshot.Projection.Status != "merged" && snapshot.Projection.Status != "syncing" && snapshot.Projection.Status != "rolling_out" {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: unsupported ChangeRequest delivery status %q", asyncjob.ErrPolicyViolation, snapshot.Projection.Status)
	}
	if snapshot.DecisionPlanVersion != snapshot.PlanVersion {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: approval plan version drift", asyncjob.ErrPolicyViolation)
	}
	if approvedBase != snapshot.BaseRevision || approvedPost != snapshot.ExpectedPostImageHash || approvedTree != snapshot.ExpectedTreeSHA ||
		approvedPolicy != snapshot.PolicyHash || approvedVerification != snapshot.VerificationHash || approvedEvidence != snapshot.EvidenceSetHash {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: approval hashes no longer bind the delivery Plan", asyncjob.ErrPolicyViolation)
	}
	if err := json.Unmarshal(targetJSON, &snapshot.TargetResource); err != nil || snapshot.TargetResource.Kind != "Deployment" || snapshot.TargetResource.Name == "" || snapshot.TargetResource.Container == "" || snapshot.WorkloadKind != "Deployment" || snapshot.WorkloadName != snapshot.TargetResource.Name {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: target resource is malformed", verification.ErrInvalidArgument)
	}
	if snapshot.TargetResource.Namespace != "" && snapshot.TargetResource.Namespace != snapshot.Namespace {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: target namespace drift", asyncjob.ErrPolicyViolation)
	}
	snapshot.Container = snapshot.TargetResource.Container
	if snapshot.PRNumber <= 0 || !deliveryRevisionPattern.MatchString(strings.ToLower(snapshot.Projection.HeadCommitSHA)) {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: pull request identity is incomplete", asyncjob.ErrPolicyViolation)
	}
	snapshot.ArgoApplication, snapshot.ArgoProject, snapshot.ArgoRepository, snapshot.ArgoPath = s.cfg.Target.ArgoApplication, s.cfg.Target.ArgoProject, s.cfg.Target.ArgoRepository, s.cfg.Target.ArgoPath
	snapshot.ExpectedReplicas = s.cfg.Target.DesiredReplicas
	if snapshot.ExpectedReplicas <= 0 {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: desired replica contract is invalid", verification.ErrInvalidArgument)
	}
	if snapshot.Repository != s.cfg.Target.ArgoRepository {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: change repository is outside fixed Argo repository", asyncjob.ErrPolicyViolation)
	}
	var baselineCount int
	baselineRows, err := s.cfg.DB.QueryContext(ctx, `
	SELECT source_revision, image_digest, gitops_revision
	FROM deployment_baselines
	WHERE status = 'active' AND cluster = ? AND environment = ? AND namespace = ?
  AND workload_kind = ? AND workload_name = ? AND container_name = ? AND repository = ?
  AND base_branch = ? AND target_path = ? AND gitops_revision = ?
LIMIT 2`, snapshot.Cluster, snapshot.Environment, snapshot.Namespace, snapshot.WorkloadKind, snapshot.WorkloadName, snapshot.Container,
		snapshot.Repository, snapshot.BaseBranch, snapshot.TargetPath, snapshot.LastKnownGoodSHA)
	if err != nil {
		return DeliveryObserveSnapshot{}, err
	}
	defer func() {
		if closeErr := baselineRows.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close deployment baseline rows: %w", closeErr))
		}
	}()
	for baselineRows.Next() {
		baselineCount++
		if err := baselineRows.Scan(&snapshot.SourceRevision, &snapshot.ImageDigest, &snapshot.BaselineGitOpsSHA); err != nil {
			return DeliveryObserveSnapshot{}, err
		}
	}
	if err := baselineRows.Err(); err != nil {
		return DeliveryObserveSnapshot{}, err
	}
	if baselineCount != 1 || !deliveryRevisionPattern.MatchString(strings.ToLower(snapshot.SourceRevision)) || !deliveryDigestPattern.MatchString(strings.ToLower(snapshot.ImageDigest)) || !deliveryRevisionPattern.MatchString(strings.ToLower(snapshot.BaselineGitOpsSHA)) {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: exactly one verified deployment baseline is required", asyncjob.ErrPolicyViolation)
	}
	var bindings []remediation.EvidenceBinding
	if err := json.Unmarshal(evidenceJSON, &bindings); err != nil || len(bindings) == 0 {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: approved Evidence bindings are malformed", asyncjob.ErrPolicyViolation)
	}
	snapshot.EvidenceBindings = append([]remediation.EvidenceBinding(nil), bindings...)
	if err := validateApprovedEvidenceCurrent(ctx, s.cfg.DB, snapshot.IncidentID, uint64(snapshot.CycleNo), bindings); err != nil {
		return DeliveryObserveSnapshot{}, err
	}
	// The fixed config remains authoritative; persisted values are only accepted
	// if they agree, never silently replaced.
	if argoApplication != "" && argoApplication != snapshot.ArgoApplication || argoProject != "" && argoProject != snapshot.ArgoProject {
		return DeliveryObserveSnapshot{}, fmt.Errorf("%w: persisted Argo identity drift", asyncjob.ErrPolicyViolation)
	}
	if snapshot.Projection.DeliveryDeadlineAt == nil {
		deadline := s.cfg.Now().UTC().Add(s.cfg.DeliveryTimeout)
		snapshot.Projection.DeliveryDeadlineAt = &deadline
	}
	if snapshot.Projection.TargetRevision == "" && snapshot.Projection.Status == "merged" {
		snapshot.Projection.TargetRevision = snapshot.Projection.MergedCommitSHA
	}
	snapshot.Now = s.cfg.Now().UTC()
	// Incident fingerprints are canonical alert identities for the verification compiler;
	// no provider query language is accepted here.
	snapshot.AlertNames = []string{snapshot.IncidentFingerprint}
	return snapshot, nil
}

func (s *mysqlDeliveryObserveStore) PersistIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot DeliveryObserveSnapshot, outcome DeliveryObserveOutcome) error {
	if tx == nil || task.SubjectType != "change_request" || task.SubjectID != snapshot.ChangeRequestID || task.ExpectedSubjectVersion != snapshot.RowVersion {
		return asyncjob.ErrInvalidMutation
	}
	if outcome.ObservedAt.IsZero() || outcome.Projection.Status == "" {
		return asyncjob.ErrInvalidMutation
	}
	var incidentVersion uint64
	var incidentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT version, status FROM incidents WHERE id = ? AND cycle_no = ? FOR UPDATE`, task.IncidentID, task.CycleNo).Scan(&incidentVersion, &incidentStatus); err != nil {
		return err
	}
	if incidentStatus != "delivering" || incidentVersion != snapshot.IncidentVersion {
		return asyncjob.ErrSubjectVersionMismatch
	}
	var currentVersion uint64
	var currentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT row_version, status FROM change_requests WHERE id = ? AND incident_id = ? AND cycle_no = ? FOR UPDATE`, task.SubjectID, task.IncidentID, task.CycleNo).Scan(&currentVersion, &currentStatus); err != nil {
		return err
	}
	if currentVersion != task.ExpectedSubjectVersion || currentVersion != snapshot.RowVersion || currentStatus != snapshot.Projection.Status {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if outcome.FailureCode == "" && outcome.VerificationPlan == nil && outcome.Projection.Status == "delivered" {
		return asyncjob.ErrInvalidMutation
	}
	if err := s.validateApprovalIn(ctx, tx, snapshot); err != nil {
		return err
	}
	nextVersion := currentVersion + 1
	p := outcome.Projection
	if p.DeliveryDeadlineAt == nil {
		p.DeliveryDeadlineAt = snapshot.Projection.DeliveryDeadlineAt
	}
	result, err := tx.ExecContext(ctx, `UPDATE change_requests SET
	 status = ?, ci_status = ?, pr_state = ?, pr_url = ?, commit_sha = ?, merged_commit_sha = ?, target_revision = ?,
 argocd_application = ?, argocd_project = ?, detected_revision = ?, argocd_sync_status = ?, argocd_operation_phase = ?, argocd_health_status = ?, resource_health_json = ?,
 sync_started_at = ?, sync_completed_at = ?, cluster = ?, environment = ?, namespace = ?, workload_kind = ?, workload_name = ?,
 deployment_generation = ?, observed_generation = ?, rollout_revision = ?, desired_replicas = ?, updated_replicas = ?, available_replicas = ?, unavailable_replicas = ?,
 delivery_started_at = ?, delivery_deadline_at = ?, delivery_completed_at = ?, next_poll_at = ?, last_observed_at = ?, failure_code = ?, failure_reason = ?,
 row_version = ?, expected_subject_version = ?, updated_at = ?
	WHERE id = ? AND incident_id = ? AND cycle_no = ? AND row_version = ? AND status = ?`,
		p.Status, p.CIStatus, p.PRState, p.PRURL, p.HeadCommitSHA, p.MergedCommitSHA, p.TargetRevision,
		snapshot.ArgoApplication, snapshot.ArgoProject, p.DetectedRevision, p.ArgoSyncStatus, p.ArgoOperationPhase, p.ArgoHealthStatus, deliveryNullableJSON(p.ResourceHealth), nullableTime(p.SyncStartedAt), nullableTime(p.SyncCompletedAt),
		snapshot.Cluster, snapshot.Environment, snapshot.Namespace, snapshot.WorkloadKind, snapshot.WorkloadName, p.DeploymentGeneration, p.ObservedGeneration, p.RolloutRevision,
		p.DesiredReplicas, p.UpdatedReplicas, p.AvailableReplicas, p.UnavailableReplicas, nullableTime(p.DeliveryStartedAt), nullableTime(p.DeliveryDeadlineAt), nullableTime(p.DeliveryCompletedAt), nullableTime(p.NextPollAt), nullableTime(p.LastObservedAt),
		outcome.FailureCode, p.FailureReason, nextVersion, nextVersion, outcome.ObservedAt.UTC(), task.SubjectID, task.IncidentID, task.CycleNo, currentVersion, currentStatus)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return err
		}
		return asyncjob.ErrSubjectVersionMismatch
	}
	if err := appendDeliveryEvidence(ctx, tx, snapshot, outcome, outcome.ObservedAt); err != nil {
		return err
	}
	sequence, err := nextChangeSequence(ctx, tx, snapshot.ChangeRequestID)
	if err != nil {
		return err
	}
	eventPayload := map[string]any{
		"kind": outcome.Kind, "status": outcome.Projection.Status,
		"source_revision": snapshot.SourceRevision, "image_digest": snapshot.ImageDigest,
		"gitops_revision": outcome.Projection.TargetRevision, "failure_code": outcome.FailureCode, "failure_summary": outcome.FailureSummary,
	}
	if err := appendChangeEvent(ctx, tx, snapshot.ChangeRequestID, snapshot.IncidentID, snapshot.CycleNo, sequence, outcome.EventType, outcome.SourceSystem, remediation.OperationStep("observe"), false, "", eventPayload); err != nil {
		return err
	}
	if outcome.FailureCode != "" {
		return s.persistDeliveryFailure(ctx, tx, task, snapshot, outcome, incidentVersion)
	}
	if outcome.Projection.Status == "delivered" {
		if outcome.VerificationPlan == nil {
			return asyncjob.ErrInvalidMutation
		}
		return s.persistDelivered(ctx, tx, task, snapshot, outcome, incidentVersion)
	}
	return s.enqueueObserve(ctx, tx, task, snapshot, nextVersion, outcome.NextPollAt)
}

func (s *mysqlDeliveryObserveStore) validateApprovalIn(ctx context.Context, tx asyncjob.DBTX, snapshot DeliveryObserveSnapshot) error {
	var planStatus, decision string
	var planVersion uint64
	var expectedPost, expectedTree, policyHash, verificationHash, evidenceSetHash string
	if err := tx.QueryRowContext(ctx, `SELECT status, plan_version, expected_post_image_hash, expected_tree_hash, policy_snapshot_hash, verification_plan_hash, evidence_set_hash FROM remediation_plans WHERE id = ? AND incident_id = ? AND cycle_no = ? FOR UPDATE`, snapshot.PlanID, snapshot.IncidentID, snapshot.CycleNo).Scan(&planStatus, &planVersion, &expectedPost, &expectedTree, &policyHash, &verificationHash, &evidenceSetHash); err != nil {
		return err
	}
	if planStatus != "consumed" || planVersion != snapshot.PlanVersion || expectedPost != snapshot.ExpectedPostImageHash || expectedTree != snapshot.ExpectedTreeSHA || policyHash != snapshot.PolicyHash || verificationHash != snapshot.VerificationHash || evidenceSetHash != snapshot.EvidenceSetHash {
		return fmt.Errorf("%w: consumed Plan is no longer active", asyncjob.ErrPolicyViolation)
	}
	var approvedBase, approvedPost, approvedTree, approvedPolicy, approvedVerification, approvedEvidence string
	if err := tx.QueryRowContext(ctx, `SELECT decision, approved_base_sha, approved_post_image_hash, approved_tree_hash, approved_policy_hash, approved_verification_hash, approved_evidence_set_hash FROM remediation_decisions WHERE plan_id = ? AND incident_id = ? AND cycle_no = ? AND imported_history = FALSE FOR UPDATE`, snapshot.PlanID, snapshot.IncidentID, snapshot.CycleNo).Scan(&decision, &approvedBase, &approvedPost, &approvedTree, &approvedPolicy, &approvedVerification, &approvedEvidence); err != nil {
		return err
	}
	if decision != "approved" || approvedBase != snapshot.BaseRevision || approvedPost != snapshot.ExpectedPostImageHash || approvedTree != snapshot.ExpectedTreeSHA || approvedPolicy != snapshot.PolicyHash || approvedVerification != snapshot.VerificationHash || approvedEvidence != snapshot.EvidenceSetHash {
		return fmt.Errorf("%w: approval is no longer valid", asyncjob.ErrPolicyViolation)
	}
	return validateApprovedEvidenceCurrentForUpdate(ctx, tx, snapshot.IncidentID, uint64(snapshot.CycleNo), snapshot.EvidenceBindings)
}

func (s *mysqlDeliveryObserveStore) enqueueObserve(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot DeliveryObserveSnapshot, version uint64, next time.Time) error {
	payload, err := json.Marshal(deliveryObservePayload{ChangeRequestID: snapshot.ChangeRequestPublicID, Step: "observe"})
	if err != nil {
		return err
	}
	_, err = s.cfg.Tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{IncidentID: task.IncidentID, CycleNo: task.CycleNo, Type: asyncjob.TaskDeliveryObserve, SubjectType: "change_request", SubjectID: task.SubjectID, Transition: "delivery.observe", ExpectedSubjectVersion: version, PayloadSchemaVersion: deliveryObservePayloadSchema, Payload: payload, DedupeKey: hashCanonical("delivery.observe", fmt.Sprint(task.SubjectID), fmt.Sprint(version)), LogicalOperationKey: task.LogicalOperationKey, MigratedLegacy: task.MigratedLegacy, MigratedLegacyContext: task.MigratedLegacyContext, Priority: 70, AvailableAt: &next, MaxAttempts: deliveryObserveMaxAttempts})
	return err
}

func (s *mysqlDeliveryObserveStore) persistDeliveryFailure(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot DeliveryObserveSnapshot, outcome DeliveryObserveOutcome, incidentVersion uint64) error {
	updated, err := tx.ExecContext(ctx, `UPDATE incidents SET status = 'investigating', version = version + 1, needs_attention = FALSE, blocking_reason_code = NULL, blocked_at = NULL, updated_at = ? WHERE id = ? AND cycle_no = ? AND version = ? AND status = 'delivering'`, outcome.ObservedAt.UTC(), task.IncidentID, task.CycleNo, incidentVersion)
	if err != nil {
		return err
	}
	if affected, err := updated.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return err
		}
		return asyncjob.ErrSubjectVersionMismatch
	}
	if err := appendDeliveryIncidentEvent(ctx, tx, snapshot, "delivery_failed", outcome.FailureCode, outcome.ObservedAt); err != nil {
		return err
	}
	budget, err := businessbudget.GuardAutomatic(ctx, tx, businessbudget.KindAgentRun, task.IncidentID, task.CycleNo)
	if err != nil {
		return err
	}
	if budget.IncidentVersion != incidentVersion+1 {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if !budget.Allowed() {
		return businessbudget.MarkExhausted(ctx, tx, budget, task.IncidentID, task.CycleNo, "delivery.observe")
	}
	// A failed delivery starts a fresh bounded investigation only after the
	// ChangeRequest and Incident transitions are in the same transaction.
	payload, _ := json.Marshal(map[string]any{
		"mode": "start", "cycle_no": task.CycleNo,
		"migrated_legacy_context": task.MigratedLegacyContext,
	})
	version := incidentVersion + 1
	_, err = s.cfg.Tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{IncidentID: task.IncidentID, CycleNo: task.CycleNo, Type: asyncjob.TaskInvestigationAdvance, SubjectType: "incident", SubjectID: task.IncidentID, Transition: "investigation.start", ExpectedSubjectVersion: version, PayloadSchemaVersion: 1, Payload: payload, DedupeKey: hashCanonical("delivery.failure", fmt.Sprint(task.IncidentID), fmt.Sprint(task.CycleNo), fmt.Sprint(version)), MigratedLegacyContext: task.MigratedLegacyContext, Priority: 80, MaxAttempts: 3})
	return err
}

func (s *mysqlDeliveryObserveStore) persistDelivered(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot DeliveryObserveSnapshot, outcome DeliveryObserveOutcome, incidentVersion uint64) error {
	plan := outcome.VerificationPlan
	planJSON, err := json.Marshal(plan)
	if err != nil || len(planJSON) > 16*1024 {
		return asyncjob.ErrInvalidMutation
	}
	planHash := plan.ProfileHash
	deadline := outcome.ObservedAt.UTC().Add(plan.Deadline)
	var runID uint64
	var runPublicID string
	var runVersion uint64
	var runMigratedLegacy, runMigratedLegacyContext bool
	if err := tx.QueryRowContext(ctx, `SELECT id, public_id, row_version, migrated_legacy, migrated_legacy_context FROM verification_runs WHERE change_request_id = ? AND incident_id = ? AND cycle_no = ? AND trigger_type = 'post_delivery' AND target_revision = ? ORDER BY attempt DESC LIMIT 1 FOR UPDATE`, task.SubjectID, task.IncidentID, task.CycleNo, plan.TargetRevision).Scan(&runID, &runPublicID, &runVersion, &runMigratedLegacy, &runMigratedLegacyContext); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var originatingAgentRunID uint64
		var planAuthorization sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT created_by_agent_run_id, business_budget_authorization_id
FROM remediation_plans
	WHERE id = ? AND incident_id = ? AND cycle_no = ?
FOR UPDATE`, snapshot.PlanID, task.IncidentID, task.CycleNo).Scan(&originatingAgentRunID, &planAuthorization); err != nil {
			return err
		}
		budget, budgetErr := businessbudget.GuardChild(ctx, tx, businessbudget.KindVerificationRun, task.IncidentID, task.CycleNo, originatingAgentRunID)
		if budgetErr != nil {
			return fmt.Errorf("%w: post-delivery Verification authorization rejected: %v", asyncjob.ErrPolicyViolation, budgetErr)
		}
		if budget.IncidentVersion != incidentVersion {
			return asyncjob.ErrSubjectVersionMismatch
		}
		if !budget.Allowed() {
			return businessbudget.MarkExhausted(ctx, tx, budget, task.IncidentID, task.CycleNo, "delivery.observe")
		}
		if (budget.AuthorizationID == 0 && planAuthorization.Valid) ||
			(budget.AuthorizationID != 0 && (!planAuthorization.Valid || uint64(planAuthorization.Int64) != budget.AuthorizationID)) {
			return fmt.Errorf("%w: post-delivery Verification escaped Plan authorization lineage", asyncjob.ErrPolicyViolation)
		}
		var authorizationValue any
		if budget.AuthorizationID != 0 {
			authorizationValue = budget.AuthorizationID
		}
		runPublicID = uuid.NewString()
		result, insertErr := tx.ExecContext(ctx, `INSERT INTO verification_runs
	 (public_id, incident_id, cycle_no, originating_agent_run_id, business_budget_authorization_id,
	  remediation_plan_id, change_request_id, status, trigger_type,
  target_revision, source_revision, image_digest, gitops_revision, plan_json, verification_profile_version, verification_profile_hash,
  verification_contract_version, verification_profile_id, common_stability_window_ms, deadline_at, attempt, row_version, expected_subject_version,
  migrated_legacy, migrated_legacy_context, created_at, updated_at)
	 VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', 'post_delivery', ?, ?, ?, ?, ?, ?, ?, 1, ?, 60000, ?, 1, 1, 1, ?, ?, ?, ?)`,
			runPublicID, task.IncidentID, task.CycleNo, originatingAgentRunID, authorizationValue, snapshot.PlanID, task.SubjectID, plan.TargetRevision, plan.SourceRevision, plan.ImageDigest, plan.GitOpsRevision,
			planJSON, plan.ProfileVersion, planHash, plan.ProfileID, deadline,
			task.MigratedLegacy, task.MigratedLegacyContext, outcome.ObservedAt.UTC(), outcome.ObservedAt.UTC())
		if insertErr != nil {
			return insertErr
		}
		insertedID, idErr := result.LastInsertId()
		if idErr != nil || insertedID <= 0 {
			err = idErr
			return fmt.Errorf("read verification run id: %w", err)
		}
		runID = uint64(insertedID)
		runVersion = 1
		runMigratedLegacy, runMigratedLegacyContext = task.MigratedLegacy, task.MigratedLegacyContext
		if budget.AuthorizationID != 0 {
			if err := appendDeliveryBudgetLineageEvent(ctx, tx, snapshot, runPublicID, budget, outcome.ObservedAt); err != nil {
				return err
			}
		}
		for _, spec := range plan.Checks {
			subjectJSON, _ := json.Marshal(spec.Subject)
			var comparison any = nil
			if spec.Comparison != "" {
				comparison = string(spec.Comparison)
			}
			var threshold any = nil
			if spec.Comparison != "" {
				threshold = spec.Threshold
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO verification_checks
	 (public_id, verification_run_id, incident_id, cycle_no, check_type, status, required_check, subject_json, expected_json,
  source_reference, lookback_ms, stability_window_ms, timeout_ms, poll_interval_ms, check_spec_schema_version, profile_id, template_id,
  template_version, comparison, threshold, source_identity, initial_delay_ms, min_samples, sample_unit, failure_mode,
  migrated_legacy, migrated_legacy_context, created_at, updated_at)
	 VALUES (?, ?, ?, ?, ?, 'pending', ?, ?, ?, '', ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				uuid.NewString(), runID, task.IncidentID, task.CycleNo, spec.Type, spec.Required, subjectJSON, spec.Expected,
				spec.Lookback.Milliseconds(), spec.StabilityWindow.Milliseconds(), spec.Timeout.Milliseconds(), spec.PollInterval.Milliseconds(), spec.ProfileID, spec.TemplateID,
				spec.TemplateVersion, comparison, threshold, spec.SourceIdentity, spec.InitialDelay.Milliseconds(), spec.MinSamples, spec.SampleUnit, spec.FailureMode,
				task.MigratedLegacy, task.MigratedLegacyContext, outcome.ObservedAt.UTC(), outcome.ObservedAt.UTC()); err != nil {
				return err
			}
		}
	}
	if runMigratedLegacy != task.MigratedLegacy || runMigratedLegacyContext != task.MigratedLegacyContext {
		return asyncjob.ErrSubjectVersionMismatch
	}
	updated, err := tx.ExecContext(ctx, `UPDATE incidents SET status = 'verifying', version = version + 1, updated_at = ? WHERE id = ? AND cycle_no = ? AND version = ? AND status = 'delivering'`, outcome.ObservedAt.UTC(), task.IncidentID, task.CycleNo, incidentVersion)
	if err != nil {
		return err
	}
	if affected, err := updated.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return err
		}
		return asyncjob.ErrSubjectVersionMismatch
	}
	if err := appendDeliveryIncidentEvent(ctx, tx, snapshot, "verification_started", runPublicID, outcome.ObservedAt); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"verification_run_id": runPublicID, "cycle_no": task.CycleNo})
	_, err = s.cfg.Tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{IncidentID: task.IncidentID, CycleNo: task.CycleNo, Type: asyncjob.TaskVerificationAdvance, SubjectType: "verification_run", SubjectID: runID, Transition: "verification.advance", ExpectedSubjectVersion: runVersion, PayloadSchemaVersion: 1, Payload: payload, DedupeKey: hashCanonical("verification.advance", fmt.Sprint(runID), fmt.Sprint(runVersion)), MigratedLegacy: task.MigratedLegacy, MigratedLegacyContext: task.MigratedLegacyContext, Priority: 60, MaxAttempts: 8})
	return err
}

func appendDeliveryBudgetLineageEvent(ctx context.Context, tx asyncjob.DBTX, snapshot DeliveryObserveSnapshot, runPublicID string, budget businessbudget.Result, at time.Time) error {
	metadata, err := json.Marshal(map[string]any{
		"verification_run_id":              runPublicID,
		"business_budget_authorization_id": budget.AuthorizationPublicID,
		"authorization_slot":               budget.AuthorizationSlot,
		"originating_agent_run_id":         budget.OriginatingAgentRunPublicID,
		"source":                           "delivery.observe",
	})
	if err != nil || len(metadata) > 8192 {
		return asyncjob.ErrInvalidMutation
	}
	_, err = tx.ExecContext(ctx, `INSERT IGNORE INTO incident_events
	 (public_id, incident_id, cycle_no, event_schema_version,
	  event_type, idempotency_key, migrated_legacy_context, migrated_legacy, actor_type, actor_id, summary, metadata_json, occurred_at, created_at)
	VALUES (?, ?, ?, 1, 'verification_budget_lineage_bound', ?, ?, ?, 'system', 'delivery.observe',
	        'post-delivery Verification bound to operator retry authorization', ?, ?, ?)`,
		uuid.NewString(), snapshot.IncidentID, snapshot.CycleNo,
		hashCanonical("delivery-budget-lineage", runPublicID, budget.AuthorizationPublicID), snapshot.MigratedLegacyContext,
		snapshot.MigratedLegacy, metadata, at.UTC(), at.UTC())
	return err
}

func appendDeliveryIncidentEvent(ctx context.Context, tx asyncjob.DBTX, snapshot DeliveryObserveSnapshot, eventType, reason string, at time.Time) error {
	metadata, err := json.Marshal(map[string]any{"change_request_id": snapshot.ChangeRequestPublicID, "reason": reason, "source": "delivery.observe"})
	if err != nil || len(metadata) > 8192 {
		return asyncjob.ErrInvalidMutation
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO incident_events
	 (public_id, incident_id, cycle_no, event_schema_version, event_type, idempotency_key, migrated_legacy_context, migrated_legacy, actor_type, actor_id, summary, metadata_json, occurred_at, created_at)
	 VALUES (?, ?, ?, 1, ?, ?, ?, ?, 'system', 'delivery.observe', ?, ?, ?, ?)
 ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`, uuid.NewString(), snapshot.IncidentID, snapshot.CycleNo, eventType,
		hashCanonical("delivery-event", snapshot.ChangeRequestPublicID, eventType, reason), snapshot.MigratedLegacyContext,
		snapshot.MigratedLegacy, boundChange(reason, 2048), metadata, at.UTC(), at.UTC())
	return err
}

func appendDeliveryEvidence(ctx context.Context, tx asyncjob.DBTX, snapshot DeliveryObserveSnapshot, outcome DeliveryObserveOutcome, at time.Time) error {
	evidencePublicID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("delivery-evidence\x00"+snapshot.ChangeRequestPublicID+"\x00"+string(outcome.Kind)+"\x00"+outcome.Projection.TargetRevision)).String()
	fact := agent.EvidenceFact{
		ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("delivery-fact\x00"+evidencePublicID)).String(), EvidenceID: evidencePublicID,
		IncidentID: snapshot.IncidentPublicID, CycleNo: uint64(snapshot.CycleNo), Type: "delivery." + string(outcome.Kind),
		SourceSystem: outcome.SourceSystem, CollectionPath: "delivery/" + string(outcome.Kind), CorroborationGroup: "delivery/" + snapshot.ChangeRequestPublicID,
		Authority: deliveryEvidenceAuthority(outcome.Kind), Integrity: "verified", Freshness: "fresh", Completeness: "complete",
		ClaimUse: "support", CollectionStatus: agent.CollectionAvailable, Direct: true,
		MigratedLegacy: snapshot.MigratedLegacy,
		Attributes: map[string]string{"source_revision": snapshot.SourceRevision, "image_digest": snapshot.ImageDigest,
			"baseline_gitops_revision": snapshot.BaselineGitOpsSHA, "target_gitops_revision": outcome.Projection.TargetRevision,
			"failure_code": outcome.FailureCode},
	}
	provenance := map[string]string{"change_request_id": snapshot.ChangeRequestPublicID, "source_system": outcome.SourceSystem, "kind": string(outcome.Kind)}
	metadata, err := buildDurableEvidenceMetadata([]agent.EvidenceFact{fact}, provenance, nil, nil, nil)
	if err != nil {
		return err
	}
	facts, err := canonicalEvidenceJSON(map[string]any{
		"schema_version": evidenceFactSchema, "status": agent.CollectionAvailable, "source_system": outcome.SourceSystem,
		"collection_path": fact.CollectionPath, "template_version": "v1", "facts": []agent.EvidenceFact{fact},
		"kind": outcome.Kind, "observation": outcome.Observation,
		"source_revision": snapshot.SourceRevision, "image_digest": snapshot.ImageDigest,
		"baseline_gitops_revision": snapshot.BaselineGitOpsSHA,
		"target_gitops_revision":   outcome.Projection.TargetRevision,
		"failure_code":             outcome.FailureCode,
	})
	if err != nil || len(facts) > 16*1024 {
		return asyncjob.ErrInvalidMutation
	}
	contentHash := sha256Hex(facts)
	producerKey := hashCanonical("delivery.observe", snapshot.ChangeRequestPublicID, string(outcome.Kind), contentHash)
	resourceRef := "change-request:" + snapshot.ChangeRequestPublicID
	switch outcome.Kind {
	case DeliveryObserveArgo:
		resourceRef = "argocd:" + snapshot.ArgoApplication
	case DeliveryObserveRollout:
		resourceRef = "kubernetes:" + snapshot.Namespace + "/" + snapshot.WorkloadName
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO evidence_items
	 (public_id, incident_id, evidence_contract_version, cycle_no, migrated_legacy, migrated_legacy_context,
  change_request_id, type, source, producer_type, producer_id, producer_version,
  producer_dedupe_key, adapter_version, query_template_id, query_template_version,
  scope_snapshot_hash, arguments_hash, tool_name, resource_ref, time_range_json, query_text,
  summary, facts_json, fact_schema_version, fact_schema_hash, provenance_json, provenance_hash,
  trust_axes_json, claim_use, corroboration_groups_json, input_evidence_ids_json,
  input_sample_ids_json, input_hashes_json, result_hash, content_hash, raw_ref,
  safe_raw_reference, redaction_json, redaction_policy_version, redaction_counts_json,
  prompt_safety_flags_json, source_revision, resource_version, truncated, valid,
  idempotency_key, collected_at, observed_at, created_at)
	 VALUES (?, ?, 1, ?, ?, ?, ?, 'delivery_observation', ?, 'delivery_observation', ?,
         'delivery-observation-evidence/v1', ?, 'delivery-observer/v1', ?, 'v1', ?, ?, '', ?,
         NULL, '', ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', ?,
         'delivery-observation-redaction/v1', ?, ?, ?, ?, FALSE, TRUE, ?, ?, ?, ?)
	 ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`, evidencePublicID, snapshot.IncidentID, snapshot.CycleNo,
		snapshot.MigratedLegacy, snapshot.MigratedLegacyContext, snapshot.ChangeRequestID, outcome.SourceSystem, snapshot.ChangeRequestPublicID, producerKey,
		"delivery/"+string(outcome.Kind), hashCanonical("delivery-scope", snapshot.IncidentPublicID, fmt.Sprint(snapshot.CycleNo), snapshot.ChangeRequestPublicID),
		hashCanonical("delivery-arguments", string(outcome.Kind), snapshot.SourceRevision, outcome.Projection.TargetRevision), resourceRef,
		boundChange(string(outcome.Kind)+" delivery observation", 4096), facts, metadata.FactSchemaHash,
		metadata.ProvenanceJSON, metadata.ProvenanceHash, metadata.TrustAxesJSON, metadata.ClaimUse,
		metadata.CorroborationGroups, metadata.InputEvidenceIDs, metadata.InputSampleIDs, metadata.InputHashes,
		contentHash, contentHash, json.RawMessage(`{"policy":"delivery-observation-redaction/v1"}`),
		metadata.RedactionCounts, metadata.PromptSafetyFlags, snapshot.SourceRevision, outcome.Projection.TargetRevision,
		producerKey, at.UTC(), at.UTC(), at.UTC())
	return err
}

func deliveryEvidenceAuthority(kind DeliveryObservationKind) string {
	if kind == DeliveryObserveRollout {
		return "runtime_observation"
	}
	return "authoritative"
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func nullTimeValue(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return ptrDeliveryTime(value.Time)
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func deliveryNullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
