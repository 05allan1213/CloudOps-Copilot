package asyncjob

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNoTask                   = errors.New("no async task available")
	ErrLeaseLost                = errors.New("async task lease lost")
	ErrSubjectVersionMismatch   = errors.New("async task subject version mismatch")
	ErrInvalidMutation          = errors.New("invalid async task domain mutation")
	ErrPolicyViolation          = errors.New("async task domain policy violation")
	ErrBusinessBudgetExceeded   = errors.New("async task business budget exceeded")
	ErrReplayValidationRequired = errors.New("async task replay subject validation is required")
	ErrInvalidResult            = errors.New("invalid async task handler result")
	ErrRunnerNotStarted         = errors.New("async task runner is not started")
	ErrClaimsStopped            = errors.New("async task claims are stopped")
	ErrDrainTimeout             = errors.New("async task drain timed out")
	ErrExternalDeadlineMissing  = errors.New("async task external call deadline is not configured")
)

type Queue string

const (
	QueueInvestigate Queue = "investigate"
	QueueDeliver     Queue = "deliver"
	QueueObserve     Queue = "observe"
	QueueVerify      Queue = "verify"
)

var queues = [...]Queue{QueueInvestigate, QueueDeliver, QueueObserve, QueueVerify}

func Queues() []Queue {
	result := make([]Queue, len(queues))
	copy(result, queues[:])
	return result
}

func (q Queue) Valid() bool {
	switch q {
	case QueueInvestigate, QueueDeliver, QueueObserve, QueueVerify:
		return true
	default:
		return false
	}
}

type TaskType string

const (
	TaskInvestigationAdvance TaskType = "investigation.advance"
	TaskRemediationPrepare   TaskType = "remediation.prepare"
	TaskChangeEnsurePR       TaskType = "change.ensure_pr"
	TaskDeliveryObserve      TaskType = "delivery.observe"
	TaskVerificationAdvance  TaskType = "verification.advance"
)

var taskTypes = [...]TaskType{
	TaskInvestigationAdvance,
	TaskRemediationPrepare,
	TaskChangeEnsurePR,
	TaskDeliveryObserve,
	TaskVerificationAdvance,
}

func TaskTypes() []TaskType {
	result := make([]TaskType, len(taskTypes))
	copy(result, taskTypes[:])
	return result
}

func (t TaskType) Valid() bool {
	_, ok := queueByTaskType[t]
	return ok
}

var queueByTaskType = map[TaskType]Queue{
	TaskInvestigationAdvance: QueueInvestigate,
	TaskRemediationPrepare:   QueueInvestigate,
	TaskChangeEnsurePR:       QueueDeliver,
	TaskDeliveryObserve:      QueueObserve,
	TaskVerificationAdvance:  QueueVerify,
}

func QueueForTaskType(taskType TaskType) (Queue, error) {
	queue, ok := queueByTaskType[taskType]
	if !ok {
		return "", fmt.Errorf("unsupported async task type %q", taskType)
	}
	return queue, nil
}

func validDispatchIdentity(taskType TaskType, subjectType, transition string) bool {
	subjectType = strings.TrimSpace(subjectType)
	transition = strings.TrimSpace(transition)
	switch taskType {
	case TaskInvestigationAdvance:
		return (subjectType == "incident" && transition == "investigation.start") ||
			(subjectType == "agent_run" && transition == "investigation.step")
	case TaskRemediationPrepare:
		return subjectType == "agent_run" && transition == "remediation.prepare"
	case TaskChangeEnsurePR:
		return (subjectType == "remediation_plan" || subjectType == "change_request") && transition == "change.ensure_pr"
	case TaskDeliveryObserve:
		return subjectType == "change_request" && transition == "delivery.observe"
	case TaskVerificationAdvance:
		return subjectType == "verification_run" && transition == "verification.advance"
	default:
		return false
	}
}

type Status string

const (
	StatusReady     Status = "ready"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusDead      Status = "dead"
	StatusCancelled Status = "cancelled"
)

type Task struct {
	ID                     uint64
	PublicID               string
	IncidentID             uint64
	CycleNo                uint32
	Queue                  Queue
	Type                   TaskType
	SubjectType            string
	SubjectID              uint64
	Transition             string
	ExpectedSubjectVersion uint64
	PayloadSchemaVersion   uint32
	Payload                json.RawMessage
	CheckpointSchema       uint32
	CheckpointVersion      uint64
	CheckpointHash         string
	Checkpoint             json.RawMessage
	DedupeKey              string
	ReplayGeneration       uint32
	LogicalOperationKey    string
	Status                 Status
	Priority               int
	AvailableAt            time.Time
	Attempt                uint32
	MaxAttempts            uint32
	LeaseOwner             string
	LeaseGeneration        uint64
	LeaseExpiresAt         *time.Time
	HeartbeatAt            *time.Time
	LastErrorCode          string
	LastErrorSummary       string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	StartedAt              *time.Time
	CompletedAt            *time.Time
	DeadAt                 *time.Time
	CancelledAt            *time.Time
	ReplayedFromTaskID     *uint64
}

type NewTask struct {
	IncidentID             uint64
	CycleNo                uint32
	Type                   TaskType
	SubjectType            string
	SubjectID              uint64
	Transition             string
	ExpectedSubjectVersion uint64
	PayloadSchemaVersion   uint32
	Payload                json.RawMessage
	DedupeKey              string
	LogicalOperationKey    string
	Priority               int
	AvailableAt            *time.Time
	MaxAttempts            uint32
}

func (t NewTask) Validate() error {
	queue, err := QueueForTaskType(t.Type)
	if err != nil {
		return err
	}
	if !queue.Valid() {
		return fmt.Errorf("invalid queue %q", queue)
	}
	if t.IncidentID == 0 || t.CycleNo == 0 {
		return errors.New("incident id and cycle number must be positive")
	}
	if strings.TrimSpace(t.SubjectType) == "" || len(t.SubjectType) > 64 || t.SubjectID == 0 {
		return errors.New("subject type and id are required")
	}
	switch t.SubjectType {
	case "incident", "agent_run", "remediation_plan", "change_request", "verification_run":
	default:
		return fmt.Errorf("unsupported async task subject type %q", t.SubjectType)
	}
	if len(t.SubjectType) > 32 {
		return errors.New("subject type exceeds 32 bytes")
	}
	if strings.TrimSpace(t.Transition) == "" || len(t.Transition) > 64 {
		return errors.New("transition is required")
	}
	if !validDispatchIdentity(t.Type, t.SubjectType, t.Transition) {
		return fmt.Errorf("unsupported async task dispatch identity %q/%q/%q", t.Type, t.SubjectType, t.Transition)
	}
	if t.ExpectedSubjectVersion == 0 {
		return errors.New("expected subject version must be positive")
	}
	if t.PayloadSchemaVersion == 0 || t.PayloadSchemaVersion > 65535 {
		return errors.New("payload schema version must be positive")
	}
	payload := t.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if len(payload) > 8*1024 || !json.Valid(payload) {
		return errors.New("payload must be valid JSON no larger than 8192 bytes")
	}
	if !validSHA256(t.DedupeKey) {
		return errors.New("dedupe key must be a lowercase SHA-256 hex digest")
	}
	if t.LogicalOperationKey != "" && !validSHA256(t.LogicalOperationKey) {
		return errors.New("logical operation key must be empty or a lowercase SHA-256 hex digest")
	}
	if t.MaxAttempts == 0 {
		return errors.New("max attempts must be positive")
	}
	return nil
}

type Lease struct {
	TaskID                 uint64
	Owner                  string
	Generation             uint64
	ExpectedSubjectVersion uint64
	Attempt                uint32
	MaxAttempts            uint32
}

func (l Lease) Validate() error {
	if l.TaskID == 0 || strings.TrimSpace(l.Owner) == "" || len(l.Owner) > 128 || l.Generation == 0 || l.ExpectedSubjectVersion == 0 || l.Attempt == 0 || l.MaxAttempts == 0 {
		return errors.New("incomplete async task lease token")
	}
	return nil
}

type Execution struct {
	Task  Task
	Lease Lease
}

type externalCallPolicy struct {
	timeout time.Duration
}

type externalCallPolicyKey struct{}

func withExternalCallPolicy(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, externalCallPolicyKey{}, externalCallPolicy{timeout: timeout})
}

// ExternalCallContext returns the per-pool external-call context injected by
// Runner. Handlers retain their longer handler context for reconciliation and
// durable persistence after an external timeout.
func ExternalCallContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	policy, ok := ctx.Value(externalCallPolicyKey{}).(externalCallPolicy)
	if !ok || policy.timeout <= 0 {
		return nil, nil, ErrExternalDeadlineMissing
	}
	externalCtx, cancel := context.WithTimeout(ctx, policy.timeout)
	return externalCtx, cancel, nil
}

func ExternalCallTimeout(ctx context.Context) (time.Duration, bool) {
	policy, ok := ctx.Value(externalCallPolicyKey{}).(externalCallPolicy)
	return policy.timeout, ok && policy.timeout > 0
}

type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Mutation is a short domain transaction callback. It must use optimistic
// subject-version predicates, must not perform external or non-transactional
// side effects, and must tolerate replay after MySQL lock conflicts.
type Mutation func(context.Context, DBTX) error

type Disposition string

const (
	DispositionSucceeded Disposition = "succeeded"
	DispositionRetry     Disposition = "retry"
	DispositionDead      Disposition = "dead"
)

type Result struct {
	Disposition  Disposition
	RetryAfter   time.Duration
	ErrorCode    string
	ErrorSummary string
	Mutate       Mutation
}

func Succeeded(mutate Mutation) Result {
	return Result{Disposition: DispositionSucceeded, Mutate: mutate}
}

func RetryAfter(delay time.Duration, code, summary string, mutate Mutation) Result {
	return Result{Disposition: DispositionRetry, RetryAfter: delay, ErrorCode: code, ErrorSummary: summary, Mutate: mutate}
}

func Dead(code, summary string, mutate Mutation) Result {
	return Result{Disposition: DispositionDead, ErrorCode: code, ErrorSummary: summary, Mutate: mutate}
}

func (r Result) Validate() error {
	switch r.Disposition {
	case DispositionSucceeded:
		if r.RetryAfter != 0 || r.ErrorCode != "" || r.ErrorSummary != "" {
			return fmt.Errorf("%w: succeeded result contains failure fields", ErrInvalidResult)
		}
	case DispositionRetry:
		if r.RetryAfter < 0 || strings.TrimSpace(r.ErrorCode) == "" {
			return fmt.Errorf("%w: retry result requires a non-negative delay and error code", ErrInvalidResult)
		}
	case DispositionDead:
		if r.RetryAfter != 0 || strings.TrimSpace(r.ErrorCode) == "" {
			return fmt.Errorf("%w: dead result requires an error code and no retry delay", ErrInvalidResult)
		}
	default:
		return fmt.Errorf("%w: unknown disposition %q", ErrInvalidResult, r.Disposition)
	}
	if len(r.ErrorCode) > 64 || len(r.ErrorSummary) > 2048 {
		return fmt.Errorf("%w: error fields exceed their bounded size", ErrInvalidResult)
	}
	return nil
}

type Checkpoint struct {
	SchemaVersion uint32
	Version       uint64
	Hash          string
	Payload       json.RawMessage
}

func (c Checkpoint) Validate() error {
	if c.SchemaVersion == 0 || c.SchemaVersion > 65535 || c.Version == 0 {
		return errors.New("checkpoint schema and version must be positive")
	}
	if len(c.Payload) == 0 || len(c.Payload) > 128*1024 || !json.Valid(c.Payload) {
		return errors.New("checkpoint must be valid JSON no larger than 131072 bytes")
	}
	if !validSHA256(c.Hash) {
		return errors.New("checkpoint hash must be a lowercase SHA-256 hex digest")
	}
	digest := sha256.Sum256(c.Payload)
	if hex.EncodeToString(digest[:]) != c.Hash {
		return errors.New("checkpoint hash does not match checkpoint payload")
	}
	return nil
}

// ReplayValidator is a retry-safe, database-only validation callback.
type ReplayValidator func(context.Context, DBTX, Task) error

type ReplayRequest struct {
	DeadTaskID             uint64
	ExpectedSubjectVersion uint64
	ValidateSubject        ReplayValidator
}

func validSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
