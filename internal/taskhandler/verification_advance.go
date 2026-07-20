package taskhandler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

const verificationAdvancePayloadSchema = 1

type VerificationAdvanceSnapshot struct {
	Run         verification.Run
	Checks      []verification.Check
	Observation verification.Observation
	CheckID     string
	Now         time.Time
	// NextDueAt is populated by durable readers when no check is ready. It is the
	// earliest selected-check due/deadline boundary, so a poll cannot sleep past
	// a timeout while waiting for its normal interval.
	NextDueAt time.Time
	// CheckDeadlineAt lets the handler turn a due check into an explicit timeout
	// sample without treating an absent provider response as a passing value.
	CheckDeadlineAt time.Time

	// Durable identity copied from verification_runs. These fields are kept out
	// of verification.Run for compatibility with the legacy repository model.
	CycleNo               uint32
	IncidentVersion       uint64
	IncidentStatus        string
	TriggerType           string
	TriggerSignalID       uint64
	RemediationPlanID     uint64
	ChangeRequestID       uint64
	SourceRevision        string
	ImageDigest           string
	GitOpsRevision        string
	ProfileID             string
	ProfileHash           string
	ContractVersion       int
	CommonStabilityWindow time.Duration
}

type VerificationAdvanceReader interface {
	Load(context.Context, asyncjob.Task) (VerificationAdvanceSnapshot, error)
}

type VerificationAdvanceStore interface {
	PersistIn(context.Context, asyncjob.DBTX, asyncjob.Task, VerificationAdvanceSnapshot, verification.Check, verification.Sample, verification.RunStatus, string, *time.Time) error
}

type VerificationAdvanceConfig struct {
	Reader VerificationAdvanceReader
	Store  VerificationAdvanceStore
	Now    func() time.Time
}

func NewVerificationAdvance(config VerificationAdvanceConfig) (Operation, error) {
	if config.Reader == nil || config.Store == nil {
		return nil, errors.New("verification.advance requires observation reader and durable store")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	operation := &verificationAdvanceOperation{cfg: config}
	return operation.handle, nil
}

type verificationAdvanceOperation struct {
	cfg VerificationAdvanceConfig
}

type verificationAdvancePayload struct {
	VerificationRunID string `json:"verification_run_id"`
	CheckID           string `json:"check_id,omitempty"`
	CycleNo           uint64 `json:"cycle_no"`
}

func (o *verificationAdvanceOperation) handle(ctx context.Context, execution asyncjob.Execution) asyncjob.Result {
	task := execution.Task
	if dispatchKey(task) != verificationAdvanceKey || task.SubjectID == 0 || task.CycleNo == 0 ||
		task.ExpectedSubjectVersion == 0 || task.PayloadSchemaVersion != verificationAdvancePayloadSchema ||
		execution.Lease.TaskID != task.ID || execution.Lease.ExpectedSubjectVersion != task.ExpectedSubjectVersion {
		return asyncjob.Dead("invalid_task_subject", "verification.advance task identity is invalid", nil)
	}
	payload, err := decodeVerificationAdvancePayload(task)
	if err != nil {
		return asyncjob.Dead("invalid_verification_payload", boundChange(err.Error(), 2048), nil)
	}
	if payload.CycleNo != uint64(task.CycleNo) {
		return asyncjob.Dead("invalid_verification_payload", "verification payload cycle does not match its subject", nil)
	}
	snapshot, err := o.cfg.Reader.Load(ctx, task)
	if err != nil {
		return verificationAdvanceLoadFailure(err)
	}
	if snapshot.Run.ID != task.SubjectID || snapshot.Run.IncidentID != task.IncidentID || snapshot.Run.RowVersion != task.ExpectedSubjectVersion || snapshot.Run.Status == verification.RunPassed || snapshot.Run.Status == verification.RunFailed || snapshot.Run.Status == verification.RunTimedOut || snapshot.Run.Status == verification.RunInconclusive || snapshot.Run.Status == verification.RunCancelled {
		return asyncjob.Dead("subject_version_mismatch", "verification run is stale or terminal", nil)
	}
	if payload.VerificationRunID != "" && payload.VerificationRunID != snapshot.Run.PublicID {
		return asyncjob.Dead("invalid_verification_payload", "verification run payload does not match subject", nil)
	}
	checkIndex := selectVerificationCheck(snapshot.Checks, payload.CheckID)
	if checkIndex < 0 {
		return asyncjob.Dead("verification_checks_exhausted", "no non-terminal verification check remains", nil)
	}
	check := snapshot.Checks[checkIndex]
	now := snapshot.Now
	if now.IsZero() {
		now = o.cfg.Now().UTC()
	}
	if !snapshot.NextDueAt.IsZero() && now.Before(snapshot.NextDueAt) {
		return asyncjob.RetryAfter(
			snapshot.NextDueAt.Sub(now),
			"verification_check_not_due",
			"verification check is scheduled for a later poll",
			nil,
		)
	}
	sample := verification.EvaluateV3Observation(check, snapshot.Observation, now)
	if !snapshot.CheckDeadlineAt.IsZero() && !now.Before(snapshot.CheckDeadlineAt) {
		sample = verification.Sample{
			Status:          verification.SampleTimedOut,
			Observed:        json.RawMessage(`{"status":"timed_out"}`),
			SourceReference: snapshot.Observation.SourceReference,
			ReasonCode:      "check_deadline_exceeded",
		}
	}
	if err := verification.ApplyV3Sample(&check, sample, now); err != nil {
		return asyncjob.Dead("verification_sample_rejected", boundChange(err.Error(), 2048), nil)
	}
	snapshot.Checks[checkIndex] = check
	status, reason, terminal, commonStart := verification.CommonWindowResult(snapshot.Checks, now, snapshot.Run.DeadlineAt)
	if !terminal && !now.Before(snapshot.Run.DeadlineAt) {
		status, reason, terminal = verification.RunTimedOut, "verification_deadline_exceeded", true
	}
	return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
		return o.cfg.Store.PersistIn(ctx, tx, task, snapshot, check, sample, status, reason, commonStart)
	})
}

func decodeVerificationAdvancePayload(task asyncjob.Task) (verificationAdvancePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(task.Payload)))
	decoder.DisallowUnknownFields()
	var payload verificationAdvancePayload
	if err := decoder.Decode(&payload); err != nil || payload.CycleNo == 0 {
		return verificationAdvancePayload{}, errors.New("verification.advance payload is malformed")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return verificationAdvancePayload{}, errors.New("verification.advance payload has multiple JSON values")
	}
	return payload, nil
}

func selectVerificationCheck(checks []verification.Check, requested string) int {
	if requested != "" {
		for index := range checks {
			if checks[index].PublicID == requested && !verification.TerminalCheck(checks[index].Status) {
				return index
			}
		}
		return -1
	}
	for index := range checks {
		if !verification.TerminalCheck(checks[index].Status) {
			return index
		}
	}
	return -1
}

func verificationAdvanceLoadFailure(err error) asyncjob.Result {
	switch {
	case errors.Is(err, asyncjob.ErrSubjectVersionMismatch), errors.Is(err, asyncjob.ErrLeaseLost):
		return asyncjob.Dead("subject_version_mismatch", boundChange(err.Error(), 2048), nil)
	case errors.Is(err, asyncjob.ErrPolicyViolation), errors.Is(err, verification.ErrInvalidArgument), errors.Is(err, verification.ErrInvalidTransition):
		return asyncjob.Dead("verification_input_rejected", boundChange(err.Error(), 2048), nil)
	default:
		return asyncjob.RetryAfter(0, "verification_source_unavailable", boundChange(err.Error(), 2048), nil)
	}
}

type VerificationAdvanceReaderFunc func(context.Context, asyncjob.Task) (VerificationAdvanceSnapshot, error)

func (f VerificationAdvanceReaderFunc) Load(ctx context.Context, task asyncjob.Task) (VerificationAdvanceSnapshot, error) {
	return f(ctx, task)
}

type VerificationAdvanceStoreFunc func(context.Context, asyncjob.DBTX, asyncjob.Task, VerificationAdvanceSnapshot, verification.Check, verification.Sample, verification.RunStatus, string, *time.Time) error

func (f VerificationAdvanceStoreFunc) PersistIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot VerificationAdvanceSnapshot, check verification.Check, sample verification.Sample, status verification.RunStatus, reason string, commonStart *time.Time) error {
	return f(ctx, tx, task, snapshot, check, sample, status, reason, commonStart)
}
