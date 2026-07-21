package taskhandler

// This file contains the durable adapter for verification.advance. The
// operation itself remains provider-neutral; this adapter owns only bounded
// MySQL reads/writes and the task transitions that follow a sample.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/baseline"
	"github.com/05allan1213/CloudOps-Copilot/internal/businessbudget"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

const (
	verificationContractVersion = 1
	verificationSampleSchema    = 1
	verificationReportSchema    = 1
	verificationMaxObserved     = 16 * 1024
)

// VerificationAdvanceTaskStore is implemented by asyncjob.Repository. It is
// kept as a small interface so the adapter cannot accidentally claim tasks or
// commit outside the runner-owned transaction.
type VerificationAdvanceTaskStore interface {
	EnqueueIn(context.Context, asyncjob.DBTX, asyncjob.NewTask) (*asyncjob.Task, error)
}

// VerificationObservationSource performs one bounded read for one check. It
// must never mutate MySQL or an external system.
type VerificationObservationSource interface {
	Observe(context.Context, verification.Run, verification.Check) (verification.Observation, error)
}

// ResolutionReportWriter is mandatory for a passing run. Keeping it explicit
// prevents a future caller from resolving an Incident without an immutable
// report transaction.
type ResolutionReportWriter interface {
	PersistIn(context.Context, asyncjob.DBTX, asyncjob.Task, VerificationAdvanceSnapshot, []verification.Check, *time.Time, time.Time) error
}

// VerificationBaselineStore is deliberately transaction-bound: a passing
// post-delivery VerificationRun, its promoted DeploymentBaseline, the resolved
// Incident projection, and the ResolutionReport must commit or roll back as a
// single durable state transition.
type VerificationBaselineStore interface {
	ActivateIn(context.Context, baseline.Transaction, baseline.Snapshot) (baseline.ActivationResult, error)
}

type MySQLVerificationAdvanceConfig struct {
	DB           *sql.DB
	Tasks        VerificationAdvanceTaskStore
	Observations VerificationObservationSource
	Reports      ResolutionReportWriter
	Baselines    VerificationBaselineStore
	Now          func() time.Time
	MaxAgentRuns int
}

// NewMySQLVerificationAdvance creates the production verification operation.
// All dependencies are required; nil adapters are not replaced by a no-op.
func NewMySQLVerificationAdvance(config MySQLVerificationAdvanceConfig) (Operation, error) {
	if config.DB == nil || config.Tasks == nil || config.Observations == nil || config.Reports == nil || config.Baselines == nil {
		return nil, errors.New("verification.advance requires MySQL, task, observation, resolution-report, and baseline adapters")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.MaxAgentRuns == 0 {
		config.MaxAgentRuns = DefaultAgentRunBudget
	}
	if config.MaxAgentRuns != DefaultAgentRunBudget {
		return nil, errors.New("verification.advance agent-run limit must match the fixed investigation.start budget")
	}
	reader := &mysqlVerificationAdvanceReader{db: config.DB, observations: config.Observations}
	store := &mysqlVerificationAdvanceStore{tasks: config.Tasks, reports: config.Reports, baselines: config.Baselines, now: config.Now, maxAgentRuns: config.MaxAgentRuns}
	return NewVerificationAdvance(VerificationAdvanceConfig{Reader: reader, Store: store, Now: config.Now})
}

type mysqlVerificationAdvanceReader struct {
	db           *sql.DB
	observations VerificationObservationSource
}

type nullableUint64 struct{ sql.NullInt64 }

func (n *nullableUint64) value() uint64 {
	if n == nil || !n.Valid || n.Int64 <= 0 {
		return 0
	}
	return uint64(n.Int64)
}

func (r *mysqlVerificationAdvanceReader) Load(ctx context.Context, task asyncjob.Task) (VerificationAdvanceSnapshot, error) {
	if r == nil || r.db == nil || r.observations == nil {
		return VerificationAdvanceSnapshot{}, errors.New("verification reader is not configured")
	}
	payload, err := decodeVerificationAdvancePayload(task)
	if err != nil {
		return VerificationAdvanceSnapshot{}, err
	}
	var (
		run               verification.Run
		status            string
		cycleNo           uint32
		triggerType       sql.NullString
		profileID         sql.NullString
		profileHash       sql.NullString
		profileVersion    sql.NullInt64
		contractVersion   sql.NullInt64
		commonWindowMS    sql.NullInt64
		sourceRevision    sql.NullString
		imageDigest       sql.NullString
		gitopsRevision    sql.NullString
		planJSON          []byte
		startedAt         sql.NullTime
		deadlineAt        time.Time
		completedAt       sql.NullTime
		remediationPlanID nullableUint64
		changeRequestID   nullableUint64
		triggerSignalID   nullableUint64
		rowVersion        uint64
		createdAt         time.Time
		updatedAt         time.Time
		observedNow       time.Time
		incidentVersion   uint64
		incidentStatus    string
	)
	err = r.db.QueryRowContext(ctx, `
SELECT vr.public_id, vr.incident_id, i.public_id, i.version, i.v3_status, vr.remediation_plan_id,
       vr.change_request_id, COALESCE(vr.v3_status, vr.status), vr.target_revision,
       vr.plan_json, vr.started_at, vr.deadline_at, vr.completed_at, vr.row_version,
       vr.created_at, vr.updated_at, vr.cycle_no, vr.trigger_type, vr.trigger_signal_id,
       vr.source_revision, vr.image_digest, vr.gitops_revision,
		vr.verification_contract_version, vr.verification_profile_id,
	       vr.verification_profile_hash, vr.verification_profile_version, vr.common_stability_window_ms, NOW(6)
FROM verification_runs vr
JOIN incidents i ON i.id = vr.incident_id AND i.domain_schema_version = 3
WHERE vr.id = ? AND vr.incident_id = ? AND vr.cycle_no = ? AND vr.domain_schema_version = 3`, task.SubjectID, task.IncidentID, task.CycleNo).Scan(
		&run.PublicID, &run.IncidentID, &run.IncidentPublicID, &incidentVersion, &incidentStatus, &remediationPlanID,
		&changeRequestID, &status, &run.TargetRevision, &planJSON, &startedAt, &deadlineAt,
		&completedAt, &rowVersion, &createdAt, &updatedAt, &cycleNo, &triggerType,
		&triggerSignalID, &sourceRevision, &imageDigest, &gitopsRevision, &contractVersion,
		&profileID, &profileHash, &profileVersion, &commonWindowMS, &observedNow,
	)
	if err != nil {
		return VerificationAdvanceSnapshot{}, err
	}
	if run.PublicID == "" || run.IncidentID != task.IncidentID || cycleNo != task.CycleNo || rowVersion != task.ExpectedSubjectVersion ||
		incidentVersion == 0 || incidentStatus != "verifying" {
		return VerificationAdvanceSnapshot{}, asyncjob.ErrSubjectVersionMismatch
	}
	if len(planJSON) == 0 || !json.Valid(planJSON) {
		return VerificationAdvanceSnapshot{}, fmt.Errorf("%w: verification plan is malformed", verification.ErrInvalidArgument)
	}
	if err := json.Unmarshal(planJSON, &run.Plan); err != nil {
		return VerificationAdvanceSnapshot{}, fmt.Errorf("decode verification plan: %w", err)
	}
	if err := verification.ValidateV3Plan(run.Plan); err != nil {
		return VerificationAdvanceSnapshot{}, fmt.Errorf("validate frozen verification plan: %w", err)
	}
	if !contractVersion.Valid || contractVersion.Int64 != verificationContractVersion ||
		!commonWindowMS.Valid || time.Duration(commonWindowMS.Int64)*time.Millisecond != verification.V3CommonStabilityWindow ||
		!profileID.Valid || !profileHash.Valid || len(profileHash.String) != 64 || !profileVersion.Valid || profileVersion.Int64 != 1 ||
		(run.Plan.ProfileID != "" && run.Plan.ProfileID != profileID.String) ||
		(run.Plan.ProfileHash != "" && run.Plan.ProfileHash != profileHash.String) {
		return VerificationAdvanceSnapshot{}, fmt.Errorf("%w: verification contract identity is incomplete", verification.ErrInvalidArgument)
	}
	expectedTrigger := run.Plan.TriggerType
	if expectedTrigger == "no_change" {
		expectedTrigger = "no_change_signal"
	}
	if triggerType.String != expectedTrigger || run.Plan.TargetRevision != run.TargetRevision ||
		run.Plan.SourceRevision != sourceRevision.String || run.Plan.ImageDigest != imageDigest.String ||
		run.Plan.GitOpsRevision != gitopsRevision.String || run.Plan.ProfileVersion != int(profileVersion.Int64) {
		return VerificationAdvanceSnapshot{}, fmt.Errorf("%w: verification run differs from its frozen plan", verification.ErrInvalidArgument)
	}
	run.ID = task.SubjectID
	run.Status = verification.RunStatus(status)
	run.StartedAt, run.CompletedAt = verificationNullableTime(startedAt), verificationNullableTime(completedAt)
	run.DeadlineAt, run.RowVersion, run.CreatedAt, run.UpdatedAt = deadlineAt, rowVersion, createdAt, updatedAt
	run.RemediationPlanID, run.ChangeRequestID = remediationPlanID.value(), changeRequestID.value()

	checks, err := r.loadChecks(ctx, task)
	if err != nil {
		return VerificationAdvanceSnapshot{}, err
	}
	if len(checks) == 0 {
		return VerificationAdvanceSnapshot{}, fmt.Errorf("%w: verification run has no checks", verification.ErrInvalidArgument)
	}
	if err := validateDurableVerificationChecks(run.Plan, checks); err != nil {
		return VerificationAdvanceSnapshot{}, err
	}
	now := observedNow.UTC()
	start := createdAt.UTC()
	if run.StartedAt != nil {
		start = run.StartedAt.UTC()
	}
	selected := selectVerificationReaderCheck(checks, payload.CheckID, start, run.DeadlineAt)
	if selected < 0 {
		return VerificationAdvanceSnapshot{Run: run, Checks: checks, Now: observedNow}, nil
	}
	checks[0], checks[selected] = checks[selected], checks[0]
	check := checks[0]
	_, deadline, nextAt := verificationReaderCheckSchedule(check, start, run.DeadlineAt)
	snapshot := VerificationAdvanceSnapshot{
		Run: run, Checks: checks, CheckID: check.PublicID, Now: now,
		CheckDeadlineAt: deadline,
		CycleNo:         cycleNo, IncidentVersion: incidentVersion, IncidentStatus: incidentStatus,
		TriggerType: triggerType.String, TriggerSignalID: triggerSignalID.value(),
		RemediationPlanID: remediationPlanID.value(), ChangeRequestID: changeRequestID.value(),
		SourceRevision: sourceRevision.String, ImageDigest: imageDigest.String, GitOpsRevision: gitopsRevision.String,
		ProfileID: profileID.String, ProfileHash: profileHash.String, ContractVersion: int(contractVersion.Int64),
		CommonStabilityWindow: time.Duration(commonWindowMS.Int64) * time.Millisecond,
	}
	if !now.Before(deadline) {
		return snapshot, nil
	}
	if now.Before(nextAt) {
		snapshot.NextDueAt = nextAt
		return snapshot, nil
	}
	observationCtx, cancel, err := asyncjob.ExternalCallContext(ctx)
	if err != nil {
		return VerificationAdvanceSnapshot{}, err
	}
	observation, err := r.observations.Observe(observationCtx, run, check)
	cancel()
	if err != nil {
		return VerificationAdvanceSnapshot{}, fmt.Errorf("observe verification check: %w", err)
	}
	snapshot.Observation = observation
	return snapshot, nil
}

func (r *mysqlVerificationAdvanceReader) loadChecks(ctx context.Context, task asyncjob.Task) ([]verification.Check, error) {
	return loadVerificationChecks(ctx, r.db, task)
}

type verificationCheckQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadVerificationChecks(ctx context.Context, queryer verificationCheckQueryer, task asyncjob.Task) ([]verification.Check, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT id, public_id, verification_run_id, COALESCE(check_spec_schema_version,0), check_type, status, required_check,
	       subject_json, expected_json, COALESCE(observed_json, JSON_OBJECT()),
	       source_reference, COALESCE(source_identity,''), lookback_ms, stability_window_ms, timeout_ms,
       poll_interval_ms, first_checked_at, last_checked_at, passed_at,
       consecutive_success_since, attempt_count, failure_reason,
       COALESCE(profile_id,''), COALESCE(template_id,''), COALESCE(template_version,''),
	       COALESCE(comparison,''), COALESCE(threshold,0), COALESCE(initial_delay_ms,0),
	       COALESCE(min_samples,0), COALESCE(sample_unit,''), COALESCE(failure_mode,'')
FROM verification_checks
WHERE verification_run_id = ? AND incident_id = ? AND cycle_no = ?
ORDER BY id`, task.SubjectID, task.IncidentID, task.CycleNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	checks := make([]verification.Check, 0, 12)
	for rows.Next() {
		var (
			check                 verification.Check
			status, checkType     string
			required              bool
			subjectJSON, expected []byte
			observed              []byte
			lookbackMS, windowMS  int64
			timeoutMS, pollMS     int64
			first, last, passed   sql.NullTime
			successSince          sql.NullTime
			failure, profile      string
			templateID, templateV string
			comparison, unit      string
			threshold             float64
			initialMS, minSamples int64
			failureMode           string
		)
		var checkSchemaVersion int
		if err := rows.Scan(&check.ID, &check.PublicID, &check.VerificationRunID, &checkSchemaVersion, &checkType, &status, &required,
			&subjectJSON, &expected, &observed, &check.SourceReference, &check.SourceIdentity, &lookbackMS, &windowMS, &timeoutMS,
			&pollMS, &first, &last, &passed, &successSince, &check.AttemptCount, &failure, &profile,
			&templateID, &templateV, &comparison, &threshold, &initialMS, &minSamples, &unit, &failureMode); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(subjectJSON, &check.Subject); err != nil {
			return nil, fmt.Errorf("decode verification subject: %w", err)
		}
		check.Expected = append(json.RawMessage(nil), expected...)
		check.Observed = append(json.RawMessage(nil), observed...)
		check.SpecSchemaVersion = checkSchemaVersion
		check.Type, check.Status, check.Required = verification.CheckType(checkType), verification.CheckStatus(status), required
		check.Lookback, check.StabilityWindow, check.Timeout, check.PollInterval = msDuration(lookbackMS), msDuration(windowMS), msDuration(timeoutMS), msDuration(pollMS)
		check.FirstCheckedAt, check.LastCheckedAt, check.PassedAt, check.ConsecutiveSuccessSince = verificationNullableTime(first), verificationNullableTime(last), verificationNullableTime(passed), verificationNullableTime(successSince)
		check.FailureReason, check.ProfileID, check.TemplateID, check.TemplateVersion = failure, profile, templateID, templateV
		check.Comparison, check.Threshold, check.InitialDelay = verification.Comparison(comparison), threshold, msDuration(initialMS)
		check.MinSamples, check.SampleUnit, check.FailureMode = int(minSamples), unit, verification.FailureMode(failureMode)
		checks = append(checks, check)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return checks, nil
}

func selectVerificationReaderCheck(checks []verification.Check, requested string, start, runDeadline time.Time) int {
	if requested != "" {
		return selectVerificationCheck(checks, requested)
	}
	selected := -1
	var selectedAt time.Time
	for index := range checks {
		if verification.TerminalCheck(checks[index].Status) {
			continue
		}
		_, _, nextAt := verificationReaderCheckSchedule(checks[index], start, runDeadline)
		if selected < 0 || nextAt.Before(selectedAt) {
			selected = index
			selectedAt = nextAt
		}
	}
	return selected
}

func verificationReaderCheckSchedule(check verification.Check, start, runDeadline time.Time) (time.Time, time.Time, time.Time) {
	start = start.UTC()
	dueAt := start.Add(check.InitialDelay)
	if check.LastCheckedAt != nil {
		dueAt = check.LastCheckedAt.UTC().Add(check.PollInterval)
	}
	deadline := start.Add(check.InitialDelay + check.Timeout)
	if !runDeadline.IsZero() && runDeadline.UTC().Before(deadline) {
		deadline = runDeadline.UTC()
	}
	nextAt := dueAt
	if deadline.Before(nextAt) {
		nextAt = deadline
	}
	return dueAt, deadline, nextAt
}

func validateDurableVerificationChecks(plan verification.Plan, checks []verification.Check) error {
	if len(checks) != len(plan.Checks) {
		return fmt.Errorf("%w: durable verification check count differs from the frozen plan", verification.ErrInvalidArgument)
	}
	for index, spec := range plan.Checks {
		check := checks[index]
		if check.SpecSchemaVersion != 1 || check.Type != spec.Type || check.Subject != spec.Subject || !jsonVerificationEqual(check.Expected, spec.Expected) ||
			check.Required != spec.Required || check.ProfileID != spec.ProfileID || check.TemplateID != spec.TemplateID ||
			check.TemplateVersion != spec.TemplateVersion || check.Comparison != spec.Comparison || check.Threshold != spec.Threshold ||
			check.Lookback != spec.Lookback || check.StabilityWindow != spec.StabilityWindow || check.Timeout != spec.Timeout ||
			check.PollInterval != spec.PollInterval || check.InitialDelay != spec.InitialDelay || check.MinSamples != spec.MinSamples ||
			check.SampleUnit != spec.SampleUnit || check.FailureMode != spec.FailureMode || check.SourceIdentity != spec.SourceIdentity {
			return fmt.Errorf("%w: durable verification check %d differs from the frozen plan", verification.ErrInvalidArgument, index)
		}
	}
	return nil
}

func jsonVerificationEqual(left, right []byte) bool {
	if bytes.Equal(left, right) {
		return true
	}
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func verificationNullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time.UTC()
	return &t
}

func msDuration(value int64) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value) * time.Millisecond
}

// Metadata fields below are intentionally explicit instead of being smuggled
// through verification.Run.Plan. They are required to bind reports and task
// requeues to the same Incident cycle.
//
// (The fields are declared in verification_advance.go so test fixtures can
// construct snapshots without depending on this SQL adapter.)

type mysqlVerificationAdvanceStore struct {
	tasks        VerificationAdvanceTaskStore
	reports      ResolutionReportWriter
	baselines    VerificationBaselineStore
	now          func() time.Time
	maxAgentRuns int
}

func (s *mysqlVerificationAdvanceStore) PersistIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot VerificationAdvanceSnapshot, check verification.Check, sample verification.Sample, status verification.RunStatus, reason string, commonStart *time.Time) error {
	if s == nil || tx == nil || snapshot.Run.ID != task.SubjectID || snapshot.Run.IncidentID != task.IncidentID || snapshot.Run.RowVersion != task.ExpectedSubjectVersion || snapshot.CycleNo != task.CycleNo {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if len(sample.Observed) == 0 || len(sample.Observed) > verificationMaxObserved || !json.Valid(sample.Observed) {
		return asyncjob.ErrInvalidMutation
	}
	now := snapshot.Now.UTC()
	if now.IsZero() {
		now = s.now().UTC()
	}
	if err := validateVerificationMutation(check, sample, status, now); err != nil {
		return err
	}
	var rowVersion uint64
	var currentStatus string
	var storedPlanID, storedChangeID nullableUint64
	var storedTrigger, storedSource, storedImage, storedGitOps string
	var storedProfileID, storedProfileHash string
	if err := tx.QueryRowContext(ctx, `
SELECT row_version, COALESCE(v3_status, status), remediation_plan_id, change_request_id,
       COALESCE(trigger_type,''), COALESCE(source_revision,''), COALESCE(image_digest,''),
       COALESCE(gitops_revision,''), COALESCE(verification_profile_id,''),
       COALESCE(verification_profile_hash,'')
FROM verification_runs
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3
FOR UPDATE`, task.SubjectID, task.IncidentID, task.CycleNo).Scan(
		&rowVersion, &currentStatus, &storedPlanID, &storedChangeID, &storedTrigger,
		&storedSource, &storedImage, &storedGitOps, &storedProfileID, &storedProfileHash); err != nil {
		return err
	}
	if rowVersion != task.ExpectedSubjectVersion || (currentStatus != string(verification.RunPending) && currentStatus != string(verification.RunRunning)) {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if storedPlanID.value() != snapshot.RemediationPlanID || storedChangeID.value() != snapshot.ChangeRequestID ||
		storedTrigger != snapshot.TriggerType || storedSource != snapshot.SourceRevision || storedImage != snapshot.ImageDigest ||
		storedGitOps != snapshot.GitOpsRevision || storedProfileID != snapshot.ProfileID || storedProfileHash != snapshot.ProfileHash {
		return asyncjob.ErrSubjectVersionMismatch
	}
	var storedAttempt int
	var storedCheckStatus string
	if err := tx.QueryRowContext(ctx, `
SELECT attempt_count, status
FROM verification_checks
WHERE id = ? AND verification_run_id = ? AND incident_id = ? AND cycle_no = ?
FOR UPDATE`, check.ID, task.SubjectID, task.IncidentID, task.CycleNo).Scan(&storedAttempt, &storedCheckStatus); err != nil {
		return err
	}
	if storedAttempt+1 != check.AttemptCount || verification.TerminalCheck(verification.CheckStatus(storedCheckStatus)) {
		return asyncjob.ErrSubjectVersionMismatch
	}
	contentHash := hashVerificationSample(sample, check, storedAttempt+1)
	windowStart, windowEnd := sampleWindow(check, sample, now)
	sampleID, samplePublicID, err := insertVerificationSample(ctx, tx, task, check, sample, storedAttempt+1, contentHash, windowStart, windowEnd, now)
	if err != nil {
		return err
	}
	if err := updateVerificationCheck(ctx, tx, task, check, sample, now); err != nil {
		return err
	}
	newVersion := rowVersion + 1
	legacyStatus := string(status)
	if status == verification.RunInconclusive {
		legacyStatus = string(verification.RunFailed)
	}
	completed := any(nil)
	if verification.TerminalRun(status) {
		completed = now
	}
	commonWindowCompleted := any(nil)
	if status == verification.RunPassed {
		commonWindowCompleted = now
	}
	result, err := tx.ExecContext(ctx, `
UPDATE verification_runs
SET status = ?, v3_status = ?, started_at = COALESCE(started_at, ?),
    completed_at = ?, common_success_since = ?, common_window_completed_at = ?,
    result_summary = ?, failure_reason = ?, row_version = ?, updated_at = NOW(6)
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND row_version = ?`,
		legacyStatus, string(status), now, completed, nullableTimeValue(commonStart), commonWindowCompleted,
		boundVerificationText(reason, 2048), boundVerificationText(failureReason(status, reason), 128), newVersion,
		task.SubjectID, task.IncidentID, task.CycleNo, rowVersion)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if status == verification.RunPassed {
		if err := markRemainingChecksPassed(ctx, tx, task, now); err != nil {
			return err
		}
		if err := promotePassingDeploymentBaseline(ctx, tx, s.baselines, task, snapshot, commonStart, now); err != nil {
			return fmt.Errorf("promote passing DeploymentBaseline: %w", err)
		}
		if err := resolveIncident(ctx, tx, task, snapshot, now); err != nil {
			return err
		}
		if err := s.reports.PersistIn(ctx, tx, task, snapshot, nil, commonStart, now); err != nil {
			return fmt.Errorf("persist resolution report: %w", err)
		}
		return nil
	}
	if verification.TerminalRun(status) {
		if err := persistVerificationFailureEvidence(ctx, tx, task, snapshot, check, sample, sampleID, samplePublicID, contentHash, now); err != nil {
			return err
		}
		return requeueInvestigation(ctx, tx, s.tasks, task, status, reason, s.maxAgentRuns)
	}
	return enqueueVerificationAdvance(ctx, tx, s.tasks, task, snapshot.Run.PublicID, newVersion, now)
}

func validateVerificationMutation(check verification.Check, sample verification.Sample, status verification.RunStatus, now time.Time) error {
	if check.ID == 0 || check.AttemptCount <= 0 || now.IsZero() {
		return asyncjob.ErrInvalidMutation
	}
	if sample.Status == verification.SamplePending && status != verification.RunRunning {
		return asyncjob.ErrInvalidMutation
	}
	if !verification.TerminalRun(status) && status != verification.RunRunning {
		return asyncjob.ErrInvalidMutation
	}
	return nil
}

func insertVerificationSample(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, check verification.Check, sample verification.Sample, sequence int, contentHash string, windowStart, windowEnd *time.Time, now time.Time) (uint64, string, error) {
	publicID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("verification-sample\x00%d\x00%d\x00%d\x00%s", task.SubjectID, check.ID, sequence, contentHash))).String()
	result, err := tx.ExecContext(ctx, `
INSERT INTO verification_samples
 (public_id, domain_schema_version, sample_schema_version, incident_id, cycle_no,
  verification_run_id, verification_check_id, sample_sequence, status, observed_json,
  source_reference, reason_code, window_start_at, window_end_at, sampled_at, content_hash)
VALUES (?, 3, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		publicID, verificationSampleSchema, task.IncidentID, task.CycleNo, task.SubjectID, check.ID,
		sequence, sample.Status, sample.Observed, boundVerificationText(sample.SourceReference, 1024),
		boundVerificationText(sample.ReasonCode, 128), nullableTimeValue(windowStart), nullableTimeValue(windowEnd), now, contentHash)
	if err != nil {
		return 0, "", err
	}
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return 0, "", fmt.Errorf("read verification sample id: %w", err)
	}
	return uint64(id), publicID, nil
}

func updateVerificationCheck(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, check verification.Check, sample verification.Sample, now time.Time) error {
	result, err := tx.ExecContext(ctx, `
UPDATE verification_checks
SET status = ?, observed_json = ?, source_reference = ?, last_checked_at = ?,
    first_checked_at = COALESCE(first_checked_at, ?), passed_at = ?,
    consecutive_success_since = ?, attempt_count = ?, failure_reason = ?, updated_at = NOW(6)
WHERE id = ? AND verification_run_id = ? AND incident_id = ? AND cycle_no = ?`,
		check.Status, sample.Observed, boundVerificationText(sample.SourceReference, 1024), now, now,
		nullableTimeValue(check.PassedAt), nullableTimeValue(check.ConsecutiveSuccessSince), check.AttemptCount,
		boundVerificationText(sample.ReasonCode, 128), check.ID, check.VerificationRunID, task.IncidentID, task.CycleNo)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}
	return nil
}

func markRemainingChecksPassed(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
UPDATE verification_checks
SET status = 'passed', passed_at = COALESCE(passed_at, ?), failure_reason = '', updated_at = NOW(6)
WHERE verification_run_id = ? AND incident_id = ? AND cycle_no = ?
  AND required_check = TRUE AND status IN ('running','unavailable')`, now, task.SubjectID, task.IncidentID, task.CycleNo)
	return err
}

func sampleWindow(check verification.Check, sample verification.Sample, now time.Time) (*time.Time, *time.Time) {
	if sample.Status != verification.SamplePassed || check.ConsecutiveSuccessSince == nil || check.StabilityWindow <= 0 || now.Sub(check.ConsecutiveSuccessSince.UTC()) < check.StabilityWindow {
		return nil, nil
	}
	start := check.ConsecutiveSuccessSince.UTC()
	end := now.UTC()
	return &start, &end
}

func enqueueVerificationAdvance(ctx context.Context, tx asyncjob.DBTX, tasks VerificationAdvanceTaskStore, task asyncjob.Task, runPublicID string, expectedVersion uint64, available time.Time) error {
	payload, _ := json.Marshal(map[string]any{"verification_run_id": runPublicID, "cycle_no": task.CycleNo})
	if tasks == nil {
		return asyncjob.ErrInvalidMutation
	}
	_, err := tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
		IncidentID: task.IncidentID, CycleNo: task.CycleNo, Type: asyncjob.TaskVerificationAdvance,
		SubjectType: "verification_run", SubjectID: task.SubjectID, Transition: "verification.advance",
		ExpectedSubjectVersion: expectedVersion, PayloadSchemaVersion: 1, Payload: payload,
		DedupeKey: hashVerificationTask(runPublicID, expectedVersion), Priority: 50,
		AvailableAt: verificationTimePtr(available.UTC()), MaxAttempts: 5,
	})
	return err
}

func requeueInvestigation(ctx context.Context, tx asyncjob.DBTX, tasks VerificationAdvanceTaskStore, task asyncjob.Task, status verification.RunStatus, reason string, _ int) error {
	var incidentVersion uint64
	var incidentStatus string
	if err := tx.QueryRowContext(ctx, `
SELECT version, v3_status FROM incidents
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 FOR UPDATE`, task.IncidentID, task.CycleNo).Scan(&incidentVersion, &incidentStatus); err != nil {
		return err
	}
	if incidentStatus != "verifying" && incidentStatus != "investigating" {
		return asyncjob.ErrSubjectVersionMismatch
	}
	newIncidentVersion := incidentVersion + 1
	result, err := tx.ExecContext(ctx, `
UPDATE incidents
SET v3_status = 'investigating', status = 'DIAGNOSING', needs_attention = FALSE,
    blocking_reason_code = ?, version = ?, updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3 AND version = ?`,
		boundVerificationText("verification_"+string(status)+":"+reason, 128), newIncidentVersion,
		task.IncidentID, task.CycleNo, incidentVersion)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}
	metadata, _ := json.Marshal(map[string]any{"verification_run_id": task.SubjectID, "status": status, "reason": reason, "cycle_no": task.CycleNo})
	if _, err := tx.ExecContext(ctx, `
INSERT IGNORE INTO incident_events
 (public_id, incident_id, domain_schema_version, cycle_no, event_schema_version,
  event_type, idempotency_key, actor_type, actor_id, summary, metadata_json, occurred_at, created_at)
VALUES (?, ?, 3, ?, 1, 'verification_failed', ?, 'system', 'verification.advance', ?, ?, NOW(6), NOW(6))`,
		uuid.NewString(), task.IncidentID, task.CycleNo,
		hashVerificationTask("verification_failed", task.SubjectID, status, reason),
		"Verification did not establish recovery; investigation resumed", metadata); err != nil {
		return err
	}
	budget, err := businessbudget.GuardAutomatic(ctx, tx, businessbudget.KindAgentRun, task.IncidentID, task.CycleNo)
	if err != nil {
		return err
	}
	if budget.IncidentVersion != newIncidentVersion {
		return asyncjob.ErrSubjectVersionMismatch
	}
	if !budget.Allowed() {
		return businessbudget.MarkExhausted(ctx, tx, budget, task.IncidentID, task.CycleNo, "verification.advance")
	}
	if tasks == nil {
		return asyncjob.ErrInvalidMutation
	}
	payload, _ := json.Marshal(map[string]any{"mode": "start", "cycle_no": task.CycleNo})
	_, err = tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
		IncidentID: task.IncidentID, CycleNo: task.CycleNo, Type: asyncjob.TaskInvestigationAdvance,
		SubjectType: "incident", SubjectID: task.IncidentID, Transition: "investigation.start",
		ExpectedSubjectVersion: newIncidentVersion, PayloadSchemaVersion: 1, Payload: payload,
		DedupeKey: hashVerificationTask(fmt.Sprint(task.IncidentID), newIncidentVersion, "investigation.start"),
		Priority:  100, MaxAttempts: 5,
	})
	return err
}

func resolveIncident(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot VerificationAdvanceSnapshot, now time.Time) error {
	if snapshot.IncidentVersion == 0 || snapshot.IncidentStatus != "verifying" {
		return asyncjob.ErrSubjectVersionMismatch
	}
	result, err := tx.ExecContext(ctx, `
UPDATE incidents
SET v3_status = 'resolved', status = 'RESOLVED', resolved_at = ?, terminal_at = ?,
    needs_attention = FALSE, blocking_reason_code = NULL, version = version + 1, updated_at = NOW(6)
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3
	  AND v3_status = 'verifying' AND version = ?`, now, now, task.IncidentID, task.CycleNo, snapshot.IncidentVersion)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return asyncjob.ErrSubjectVersionMismatch
	}
	metadata, _ := json.Marshal(map[string]any{"verification_run_id": task.SubjectID, "cycle_no": task.CycleNo})
	_, err = tx.ExecContext(ctx, `
INSERT IGNORE INTO incident_events
 (public_id, incident_id, domain_schema_version, cycle_no, event_schema_version,
  event_type, idempotency_key, actor_type, actor_id, summary, metadata_json, occurred_at, created_at)
VALUES (?, ?, 3, ?, 1, 'incident_resolved', ?, 'system', 'verification.advance', ?, ?, ?, NOW(6))`,
		uuid.NewString(), task.IncidentID, task.CycleNo,
		hashVerificationTask("resolved", fmt.Sprint(task.IncidentID), fmt.Sprint(task.CycleNo), fmt.Sprint(task.SubjectID)),
		"Incident resolved after a passing verification window", metadata, now)
	return err
}

func persistVerificationFailureEvidence(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot VerificationAdvanceSnapshot, check verification.Check, sample verification.Sample, sampleID uint64, samplePublicID, sampleHash string, now time.Time) error {
	evidencePublicID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("verification-evidence\x00"+samplePublicID)).String()
	fact := agent.EvidenceFact{
		ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("verification-fact\x00"+samplePublicID)).String(), EvidenceID: evidencePublicID,
		IncidentID: snapshot.Run.IncidentPublicID, CycleNo: uint64(task.CycleNo), Type: "verification.recovery_not_established",
		SourceSystem: "verification", CollectionPath: "verification/" + string(check.Type), CorroborationGroup: "verification/" + check.PublicID,
		Authority: "runtime_observation", Integrity: "verified", Freshness: "fresh", Completeness: "complete",
		ClaimUse: "blocking", CollectionStatus: verificationEvidenceCollectionStatus(sample.Status), Direct: true,
		Attributes: map[string]string{"check_id": check.PublicID, "check_type": string(check.Type), "sample_status": string(sample.Status), "reason_code": sample.ReasonCode},
	}
	provenance := map[string]string{"verification_run_id": snapshot.Run.PublicID, "verification_check_id": check.PublicID, "verification_sample_id": samplePublicID, "source_reference": sample.SourceReference}
	metadata, err := buildDurableEvidenceMetadata([]agent.EvidenceFact{fact}, provenance, nil, []string{samplePublicID}, []string{sampleHash})
	if err != nil {
		return err
	}
	facts, _ := canonicalEvidenceJSON(map[string]any{
		"schema_version": evidenceFactSchema, "status": fact.CollectionStatus, "source_system": "verification",
		"collection_path": fact.CollectionPath, "template_version": check.TemplateVersion, "facts": []agent.EvidenceFact{fact},
		"check_id": check.PublicID, "check_type": check.Type, "sample_status": sample.Status,
		"reason": sample.ReasonCode, "observed": json.RawMessage(sample.Observed),
	})
	if len(facts) > 16*1024 {
		return asyncjob.ErrInvalidMutation
	}
	contentHash := hashVerificationTask(string(facts))
	producerKey := hashVerificationTask("verification-check-evidence/v1", check.PublicID, samplePublicID, contentHash)
	templateID, templateVersion := strings.TrimSpace(check.TemplateID), strings.TrimSpace(check.TemplateVersion)
	if templateID == "" || templateVersion == "" {
		return fmt.Errorf("%w: verification Evidence template identity is incomplete", asyncjob.ErrInvalidMutation)
	}
	scopeHash := hashVerificationTask("verification-scope", snapshot.Run.IncidentPublicID, task.CycleNo, snapshot.Run.PublicID, check.PublicID)
	argumentsHash := hashVerificationTask("verification-arguments", string(check.Type), string(check.Expected), check.SourceIdentity)
	_, err = tx.ExecContext(ctx, `
INSERT INTO evidence_items
 (public_id, incident_id, domain_schema_version, evidence_contract_version, cycle_no,
  verification_run_id, verification_check_id, type, source, producer_type, producer_id,
  producer_version, producer_dedupe_key, adapter_version, query_template_id,
  query_template_version, scope_snapshot_hash, arguments_hash, tool_name, resource_ref,
  query_text, summary, facts_json, fact_schema_version, fact_schema_hash, provenance_json,
  provenance_hash, trust_axes_json, claim_use, corroboration_groups_json,
  input_evidence_ids_json, input_sample_ids_json, input_hashes_json, result_hash,
  content_hash, raw_ref, safe_raw_reference, redaction_json, redaction_policy_version,
  redaction_counts_json, prompt_safety_flags_json, truncated, valid, idempotency_key,
  collected_at, observed_at, created_at)
VALUES (?, ?, 3, 1, ?, ?, ?, 'verification_failure', 'verification', 'verification_check', ?,
       'verification-check-evidence/v1', ?, 'verification-observer/v1', ?, ?, ?, ?,
       'verification.advance', ?, '', ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
       'verification-redaction/v1', ?, ?, FALSE, TRUE, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		evidencePublicID, task.IncidentID, task.CycleNo, task.SubjectID, check.ID, check.PublicID,
		producerKey, templateID, templateVersion, scopeHash, argumentsHash, "verification/"+check.PublicID,
		"Verification check did not establish recovery", facts, metadata.FactSchemaHash, metadata.ProvenanceJSON,
		metadata.ProvenanceHash, metadata.TrustAxesJSON, metadata.ClaimUse, metadata.CorroborationGroups,
		metadata.InputEvidenceIDs, metadata.InputSampleIDs, metadata.InputHashes, contentHash, contentHash,
		boundVerificationText(sample.SourceReference, 1024), boundVerificationText(sample.SourceReference, 1024),
		json.RawMessage(`{"policy":"verification-redaction/v1"}`), metadata.RedactionCounts, metadata.PromptSafetyFlags,
		producerKey, now, now, now)
	return err
}

func verificationEvidenceCollectionStatus(status verification.SampleStatus) agent.CollectionStatus {
	switch status {
	case verification.SampleUnavailable:
		return agent.CollectionUnavailable
	case verification.SampleInvalid:
		return agent.CollectionInvalid
	default:
		return agent.CollectionAvailable
	}
}

func hashVerificationSample(sample verification.Sample, check verification.Check, sequence int) string {
	return hashVerificationTask(string(sample.Status), string(sample.Observed), sample.SourceReference, sample.ReasonCode, check.PublicID, fmt.Sprint(sequence))
}

func hashVerificationTask(parts ...any) string {
	h := sha256.New()
	appendVerificationHash(h, parts...)
	return hex.EncodeToString(h.Sum(nil))
}

func appendVerificationHash(h hash.Hash, parts ...any) {
	for _, part := range parts {
		value := fmt.Sprint(part)
		var length [8]byte
		length[0] = byte(len(value) >> 56)
		length[1] = byte(len(value) >> 48)
		length[2] = byte(len(value) >> 40)
		length[3] = byte(len(value) >> 32)
		length[4] = byte(len(value) >> 24)
		length[5] = byte(len(value) >> 16)
		length[6] = byte(len(value) >> 8)
		length[7] = byte(len(value))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(value))
	}
}

func nullableTimeValue(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC()
}

func verificationTimePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func boundVerificationText(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func failureReason(status verification.RunStatus, reason string) string {
	if status == verification.RunRunning || status == verification.RunPassed {
		return ""
	}
	return reason
}

// mysqlResolutionReportWriter builds the report from durable facts rather
// than copying an in-memory model response. This is intentionally conservative:
// a missing required fact blocks resolution and rolls back the run update.
type mysqlResolutionReportWriter struct{}

func NewMySQLResolutionReportWriter() ResolutionReportWriter {
	return &mysqlResolutionReportWriter{}
}

func (w *mysqlResolutionReportWriter) PersistIn(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot VerificationAdvanceSnapshot, _ []verification.Check, commonStart *time.Time, resolvedAt time.Time) error {
	if tx == nil || snapshot.Run.ID != task.SubjectID || snapshot.Run.IncidentID != task.IncidentID || snapshot.CycleNo != task.CycleNo ||
		commonStart == nil || commonStart.IsZero() || resolvedAt.Sub(commonStart.UTC()) < verification.V3CommonStabilityWindow {
		return asyncjob.ErrInvalidMutation
	}
	var (
		incidentPublicID, service, workload, environment, incidentSummary string
		incidentStatus                                                    string
		incidentVersion                                                   uint64
		incidentResolvedAt                                                sql.NullTime
	)
	if err := tx.QueryRowContext(ctx, `
SELECT public_id, service_name, target_name, environment, summary, version, v3_status, resolved_at
FROM incidents
WHERE id = ? AND cycle_no = ? AND domain_schema_version = 3
FOR UPDATE`, task.IncidentID, task.CycleNo).Scan(
		&incidentPublicID, &service, &workload, &environment, &incidentSummary, &incidentVersion,
		&incidentStatus, &incidentResolvedAt); err != nil {
		return err
	}
	if incidentStatus != "resolved" || !incidentResolvedAt.Valid ||
		!incidentResolvedAt.Time.UTC().Truncate(time.Microsecond).Equal(resolvedAt.UTC().Truncate(time.Microsecond)) {
		return fmt.Errorf("%w: resolution report Incident is not the matching terminal projection", asyncjob.ErrInvalidMutation)
	}
	triggerType := snapshot.TriggerType
	if triggerType == "" {
		if snapshot.ChangeRequestID != 0 {
			triggerType = "post_delivery"
		} else {
			triggerType = "no_change_signal"
		}
	}
	initialID, triggerSignal, cycleStartedAt, err := reportSignals(ctx, tx, task.IncidentID, task.CycleNo, triggerType, snapshot.TriggerSignalID)
	if err != nil {
		return err
	}
	evidence, err := reportEvidence(ctx, tx, task)
	if err != nil {
		return err
	}
	creatorAgentRunID, expectedDiagnosisHash, err := reportDiagnosisIdentity(ctx, tx, task, snapshot.RemediationPlanID)
	if err != nil {
		return err
	}
	diagnosis, err := reportDiagnosis(ctx, tx, task, creatorAgentRunID, expectedDiagnosisHash)
	if err != nil {
		return err
	}
	plan, decision, delivery, decisionID, badRevision, fixRevision, err := reportRemediation(ctx, tx, task, snapshot)
	if err != nil {
		return err
	}
	samples, err := reportSamples(ctx, tx, task)
	if err != nil {
		return err
	}
	checks, err := loadVerificationChecks(ctx, tx, task)
	if err != nil {
		return err
	}
	if err := validateDurableVerificationChecks(snapshot.Run.Plan, checks); err != nil {
		return err
	}
	for _, check := range checks {
		if check.Required && check.Status != verification.CheckPassed {
			return fmt.Errorf("%w: resolution report observed a non-passing required check", asyncjob.ErrInvalidMutation)
		}
	}
	timeline, err := reportTimeline(ctx, tx, task)
	if err != nil {
		return err
	}
	usage, err := reportAgentUsage(ctx, tx, task)
	if err != nil {
		return err
	}
	profileID := snapshot.ProfileID
	if profileID == "" {
		profileID = snapshot.Run.Plan.ProfileID
	}
	profileHash := snapshot.ProfileHash
	if profileHash == "" {
		profileHash = snapshot.Run.Plan.ProfileHash
	}
	if len(profileHash) != 64 || profileID == "" || snapshot.SourceRevision == "" || snapshot.ImageDigest == "" || snapshot.GitOpsRevision == "" {
		return fmt.Errorf("%w: resolution report lacks revision/profile identity", asyncjob.ErrInvalidMutation)
	}
	reason := "recovered_after_remediation"
	if triggerType == "no_change_signal" {
		reason = "recovered_without_change"
		if len(diagnosis) == 0 {
			reason = "recovered_before_diagnosis"
		}
	}
	if triggerType == "post_delivery" && (len(diagnosis) == 0 || len(plan) == 0 || len(decision) == 0 || len(delivery) == 0 || decisionID == 0 || badRevision == "" || fixRevision == "") {
		return fmt.Errorf("%w: post-delivery resolution report is incomplete", asyncjob.ErrInvalidMutation)
	}
	if triggerType == "no_change_signal" && (snapshot.TriggerSignalID == 0 || len(plan) != 0 || len(decision) != 0 || len(delivery) != 0) {
		return fmt.Errorf("%w: no-change resolution report has an invalid remediation path", asyncjob.ErrInvalidMutation)
	}
	if triggerType == "post_delivery" && !reportEvidenceNonEmpty(evidence) {
		return fmt.Errorf("%w: post-delivery resolution report requires Incident evidence", asyncjob.ErrInvalidMutation)
	}
	if len(samples) == 0 || len(timeline) == 0 {
		return fmt.Errorf("%w: resolution report requires verification samples and timeline", asyncjob.ErrInvalidMutation)
	}
	triggerJSON, _ := json.Marshal(triggerSignal)
	verificationJSON, _ := json.Marshal(map[string]any{
		"run_id": snapshot.Run.PublicID, "status": verification.RunPassed, "checks": reportCheckSummaries(checks),
		"samples": json.RawMessage(samples), "common_window_started_at": commonStart.UTC().Format(time.RFC3339Nano),
		"common_window_completed_at": resolvedAt.UTC().Format(time.RFC3339Nano),
	})
	if len(triggerJSON) > 8192 || len(verificationJSON) > 32768 {
		return asyncjob.ErrInvalidMutation
	}
	service = boundVerificationText(service, 255)
	workload = boundVerificationText(workload, 255)
	environment = boundVerificationText(environment, 255)
	incidentSummary = boundVerificationText(incidentSummary, 2048)
	cycleStartedAt = cycleStartedAt.UTC().Truncate(time.Microsecond)
	resolvedAt = resolvedAt.UTC().Truncate(time.Microsecond)
	commonStartedAt := commonStart.UTC().Truncate(time.Microsecond)
	resolution := map[string]any{
		"incident_id": incidentPublicID, "cycle_no": task.CycleNo, "trigger_type": triggerType,
		"resolution_reason": reason, "summary": incidentSummary, "incident_version": incidentVersion,
	}
	resolutionJSON, _ := json.Marshal(resolution)
	durationMS := resolvedAt.Sub(cycleStartedAt).Milliseconds()
	if durationMS < 0 {
		durationMS = 0
	}
	var triggerID any
	if snapshot.TriggerSignalID != 0 {
		triggerID = snapshot.TriggerSignalID
	}
	var planID, remediationDecisionID, changeID any
	if snapshot.RemediationPlanID != 0 {
		planID = snapshot.RemediationPlanID
	}
	if decisionID != 0 {
		remediationDecisionID = decisionID
	}
	if snapshot.ChangeRequestID != 0 {
		changeID = snapshot.ChangeRequestID
	}
	if triggerType == "no_change_signal" {
		planID, remediationDecisionID, changeID, badRevision, fixRevision = nil, nil, nil, "", ""
	}
	summary := boundVerificationText("Verification passed after the common stability window", 2048)
	contentHash := hashVerificationTask(
		"resolution-report-row-v1", 3, verificationReportSchema, task.IncidentID, task.CycleNo,
		task.SubjectID, initialID, triggerID, planID, remediationDecisionID, changeID,
		triggerType, reason, service, workload, environment, incidentSummary,
		cycleStartedAt.Format(time.RFC3339Nano), resolvedAt.Format(time.RFC3339Nano),
		durationMS, badRevision, fixRevision, snapshot.SourceRevision, snapshot.ImageDigest,
		snapshot.GitOpsRevision, profileID, profileHash, commonStartedAt.Format(time.RFC3339Nano),
		resolvedAt.Format(time.RFC3339Nano),
		string(triggerJSON), string(evidence), string(diagnosis), string(plan), string(decision), string(delivery),
		string(verificationJSON), string(timeline), string(usage), summary, resolvedAt.Format(time.RFC3339Nano),
		incidentPublicID, incidentVersion, string(resolutionJSON), snapshot.Run.PublicID,
	)
	insertResult, err := tx.ExecContext(ctx, `
INSERT INTO resolution_reports
 (public_id, domain_schema_version, report_schema_version, incident_id, cycle_no,
  verification_run_id, initial_signal_id, trigger_signal_id, remediation_plan_id,
  remediation_decision_id, change_request_id, trigger_type, resolution_reason,
  service, workload, environment, impact_summary, cycle_started_at, resolved_at,
  measured_duration_ms, bad_gitops_revision, fix_gitops_revision, source_revision,
  image_digest, gitops_revision, verification_profile_id, verification_profile_hash,
  common_window_started_at, common_window_completed_at, trigger_signal_json,
  diagnosis_json, evidence_json, remediation_plan_json, remediation_decision_json,
  delivery_json, verification_json, timeline_json, agent_usage_json, summary,
  content_hash, generated_at)
	VALUES (?, 3, ?, ?, ?,
	        ?, ?, ?, ?,
	        ?, ?, ?, ?,
	        ?, ?, ?, ?, ?, ?,
	        ?, ?, ?, ?,
	        ?, ?, ?, ?,
	        ?, ?, ?,
	        ?, ?, ?, ?,
	        ?, ?, ?, ?, ?,
	        ?, ?)
	ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		uuid.NewString(), verificationReportSchema, task.IncidentID, task.CycleNo, task.SubjectID,
		initialID, triggerID, planID, remediationDecisionID, changeID, triggerType, reason,
		service, workload, environment, incidentSummary, cycleStartedAt, resolvedAt, durationMS,
		verificationNullableString(badRevision), verificationNullableString(fixRevision), snapshot.SourceRevision, snapshot.ImageDigest,
		snapshot.GitOpsRevision, profileID, profileHash, commonStartedAt, resolvedAt, triggerJSON,
		verificationNullableJSON(diagnosis), evidence, verificationNullableJSON(plan), verificationNullableJSON(decision), verificationNullableJSON(delivery),
		verificationJSON, timeline, usage, summary, contentHash, resolvedAt)
	if err != nil {
		return err
	}
	reportID, err := insertResult.LastInsertId()
	if err != nil || reportID <= 0 {
		return asyncjob.ErrInvalidMutation
	}
	var storedIncidentID, storedVerificationRunID uint64
	var storedCycle uint32
	var storedContentHash string
	if err := tx.QueryRowContext(ctx, `SELECT incident_id, cycle_no, verification_run_id, content_hash FROM resolution_reports WHERE id = ? FOR UPDATE`, reportID).
		Scan(&storedIncidentID, &storedCycle, &storedVerificationRunID, &storedContentHash); err != nil {
		return err
	}
	if storedIncidentID != task.IncidentID || storedCycle != task.CycleNo || storedVerificationRunID != task.SubjectID || storedContentHash != contentHash {
		return fmt.Errorf("%w: immutable resolution report conflicts with passing verification", asyncjob.ErrInvalidMutation)
	}
	return nil
}

func reportCheckSummaries(checks []verification.Check) []map[string]any {
	result := make([]map[string]any, 0, len(checks))
	for _, check := range checks {
		result = append(result, map[string]any{
			"id": check.PublicID, "type": check.Type, "status": check.Status,
			"required": check.Required, "attempt_count": check.AttemptCount,
			"failure_reason":            boundVerificationText(check.FailureReason, 128),
			"consecutive_success_since": reportTime(check.ConsecutiveSuccessSince),
			"last_checked_at":           reportTime(check.LastCheckedAt),
		})
	}
	return result
}

func reportTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func reportSignals(ctx context.Context, tx asyncjob.DBTX, incidentID uint64, cycle uint32, triggerType string, triggerID uint64) (uint64, map[string]any, time.Time, error) {
	initialID, initial, initialStatus, initialStart, err := queryReportSignal(ctx, tx,
		`SELECT id, public_id, source, source_event_id, fingerprint, status, summary, occurred_at, starts_at, ends_at, labels_json, annotations_json FROM incident_signals WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 AND status = 'firing' ORDER BY starts_at, id LIMIT 1`,
		incidentID, cycle)
	if err != nil {
		return 0, nil, time.Time{}, err
	}
	if initialID == 0 || initialStatus != "firing" {
		return 0, nil, time.Time{}, asyncjob.ErrInvalidMutation
	}
	if triggerType != "no_change_signal" {
		return initialID, initial, initialStart, nil
	}
	if triggerID == 0 {
		return 0, nil, time.Time{}, asyncjob.ErrInvalidMutation
	}
	resolvedID, resolved, resolvedStatus, _, err := queryReportSignal(ctx, tx,
		`SELECT id, public_id, source, source_event_id, fingerprint, status, summary, occurred_at, starts_at, ends_at, labels_json, annotations_json FROM incident_signals WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 LIMIT 1`,
		triggerID, incidentID, cycle)
	if err != nil {
		return 0, nil, time.Time{}, err
	}
	if resolvedID != triggerID || resolvedStatus != "resolved" {
		return 0, nil, time.Time{}, asyncjob.ErrInvalidMutation
	}
	return initialID, resolved, initialStart, nil
}

func queryReportSignal(ctx context.Context, tx asyncjob.DBTX, query string, args ...any) (uint64, map[string]any, string, time.Time, error) {
	var id uint64
	var publicID, source, eventID, fingerprint, status, summary string
	var occurred, starts time.Time
	var ends sql.NullTime
	var labels, annotations []byte
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&id, &publicID, &source, &eventID, &fingerprint, &status, &summary, &occurred, &starts, &ends, &labels, &annotations); err != nil {
		return 0, nil, "", time.Time{}, err
	}
	if !json.Valid(labels) || !json.Valid(annotations) {
		return 0, nil, "", time.Time{}, asyncjob.ErrInvalidMutation
	}
	return id, map[string]any{"public_id": publicID, "source": source, "source_event_id": eventID, "fingerprint": fingerprint, "status": status, "summary": summary, "occurred_at": occurred.UTC().Format(time.RFC3339Nano), "starts_at": starts.UTC().Format(time.RFC3339Nano), "ends_at": nullableTimeString(ends), "labels": json.RawMessage(labels), "annotations": json.RawMessage(annotations)}, status, starts.UTC(), nil
}

type reportEvidenceAccumulator struct {
	count                              int
	setHash                            hash.Hash
	firstMetadata, latestMetadata      []map[string]any
	firstFactSnapshots, latestFactData []map[string]any
}

func newReportEvidenceAccumulator() *reportEvidenceAccumulator {
	return &reportEvidenceAccumulator{setHash: sha256.New()}
}

func (a *reportEvidenceAccumulator) add(id, contentHash string, metadata map[string]any, facts json.RawMessage) {
	a.count++
	appendVerificationHash(a.setHash, id, contentHash)
	appendReportBoundary(&a.firstMetadata, &a.latestMetadata, metadata, 8, 8)
	appendReportBoundary(&a.firstFactSnapshots, &a.latestFactData, map[string]any{"id": id, "facts": facts}, 2, 2)
}

func (a *reportEvidenceAccumulator) encode() ([]byte, error) {
	metadata := append(append([]map[string]any(nil), a.firstMetadata...), a.latestMetadata...)
	facts := append(append([]map[string]any(nil), a.firstFactSnapshots...), a.latestFactData...)
	for {
		result, err := boundedJSON(map[string]any{
			"evidence_count": a.count, "evidence_set_hash": hex.EncodeToString(a.setHash.Sum(nil)),
			"items": metadata, "items_truncated": len(metadata) < a.count,
			"fact_snapshots": facts, "facts_truncated": len(facts) < a.count,
		}, 32768)
		if err == nil {
			return result, nil
		}
		if len(facts) == 0 {
			return nil, err
		}
		facts = facts[1:]
	}
}

func appendReportBoundary(first, latest *[]map[string]any, item map[string]any, firstLimit, latestLimit int) {
	if len(*first) < firstLimit {
		*first = append(*first, item)
		return
	}
	if len(*latest) == latestLimit {
		copy(*latest, (*latest)[1:])
		(*latest)[len(*latest)-1] = item
		return
	}
	*latest = append(*latest, item)
}

func reportEvidence(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task) ([]byte, error) {
	rows, err := tx.QueryContext(ctx, `SELECT public_id, type, source, resource_ref, summary, facts_json, result_hash, content_hash, collected_at FROM evidence_items WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 AND valid = TRUE ORDER BY collected_at, id`, task.IncidentID, task.CycleNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projection := newReportEvidenceAccumulator()
	for rows.Next() {
		var id, typ, source, resource, summary, resultHash, contentHash string
		var facts []byte
		var collected time.Time
		if err := rows.Scan(&id, &typ, &source, &resource, &summary, &facts, &resultHash, &contentHash, &collected); err != nil {
			return nil, err
		}
		if !json.Valid(facts) || len(contentHash) != 64 {
			return nil, asyncjob.ErrInvalidMutation
		}
		projection.add(id, contentHash,
			map[string]any{"id": id, "type": typ, "source": source, "resource_ref": boundVerificationText(resource, 256), "summary": boundVerificationText(summary, 256), "result_hash": resultHash, "content_hash": contentHash, "collected_at": collected.UTC().Format(time.RFC3339Nano)},
			append(json.RawMessage(nil), facts...))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return projection.encode()
}

func reportDiagnosisIdentity(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, planID uint64) (uint64, string, error) {
	if planID == 0 {
		return 0, "", nil
	}
	var creatorID uint64
	var diagnosisHash string
	if err := tx.QueryRowContext(ctx, `SELECT created_by_agent_run_id, diagnosis_hash FROM remediation_plans WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 AND plan_content_schema_version = 2`, planID, task.IncidentID, task.CycleNo).
		Scan(&creatorID, &diagnosisHash); err != nil {
		return 0, "", err
	}
	if creatorID == 0 || len(diagnosisHash) != 64 {
		return 0, "", asyncjob.ErrInvalidMutation
	}
	return creatorID, diagnosisHash, nil
}

func reportDiagnosis(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, creatorAgentRunID uint64, expectedDiagnosisHash string) ([]byte, error) {
	var diagnosis []byte
	var err error
	if creatorAgentRunID == 0 {
		err = tx.QueryRowContext(ctx, `SELECT final_diagnosis FROM agent_runs WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 AND final_diagnosis IS NOT NULL ORDER BY completed_at DESC, id DESC LIMIT 1`, task.IncidentID, task.CycleNo).Scan(&diagnosis)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT final_diagnosis FROM agent_runs WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 AND v3_status = 'completed' AND final_diagnosis IS NOT NULL`, creatorAgentRunID, task.IncidentID, task.CycleNo).Scan(&diagnosis)
	}
	if errors.Is(err, sql.ErrNoRows) {
		if creatorAgentRunID != 0 {
			return nil, asyncjob.ErrInvalidMutation
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(diagnosis) == 0 || !json.Valid(diagnosis) {
		return nil, asyncjob.ErrInvalidMutation
	}
	if creatorAgentRunID != 0 {
		var identity struct {
			DiagnosisHash string `json:"diagnosis_hash"`
		}
		if json.Unmarshal(diagnosis, &identity) != nil || identity.DiagnosisHash != expectedDiagnosisHash {
			return nil, fmt.Errorf("%w: Plan diagnosis does not match its creator AgentRun", asyncjob.ErrInvalidMutation)
		}
	}
	return boundedJSON(json.RawMessage(diagnosis), 16384)
}

func reportRemediation(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, snapshot VerificationAdvanceSnapshot) (plan, decision, delivery []byte, decisionID uint64, badRevision, fixRevision string, err error) {
	if snapshot.RemediationPlanID == 0 || snapshot.ChangeRequestID == 0 {
		return nil, nil, nil, 0, "", "", nil
	}
	var planID, creatorAgentRunID uint64
	var planContentSchemaVersion, planVersion, hashSchemaVersion int
	var planPublic, planStatus, operation, creatorAgentRunPublicID, creatorDiagnosisHash string
	var targetRepository, baseBranch, baseRevision, lastKnownGood, baseSHA, fileMode, targetPath, targetFieldRef string
	var beforeHash, postHash, treeHash, patchHash, boundedDiff string
	var policyVersion, policyHash, verificationHash, evidenceSetHash, planHash, riskLevel string
	var targetResource, changeManifest, evidenceBindings []byte
	var planCreatedAt, planExpiresAt time.Time
	if err = tx.QueryRowContext(ctx, `SELECT id, public_id, plan_content_schema_version, plan_version,
       hash_schema_version, v3_status, operation_type, created_by_agent_run_id,
       (SELECT ar.public_id FROM agent_runs ar WHERE ar.id = remediation_plans.created_by_agent_run_id
          AND ar.incident_id = remediation_plans.incident_id AND ar.cycle_no = remediation_plans.cycle_no
          AND ar.domain_schema_version = 3),
       diagnosis_hash, target_repository, target_base_branch, target_base_revision,
       last_known_good_sha, base_blob_sha, file_mode, target_path, target_resource_json,
       target_field_ref, expected_before_hash, expected_post_image_hash, expected_tree_hash,
       canonical_change_manifest_json, proposed_patch_hash, bounded_diff, policy_version,
       policy_snapshot_hash, verification_plan_hash, evidence_bindings_json,
       evidence_set_hash, canonical_plan_hash, risk_level, created_at, expires_at
FROM remediation_plans
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`,
		snapshot.RemediationPlanID, task.IncidentID, task.CycleNo).Scan(
		&planID, &planPublic, &planContentSchemaVersion, &planVersion, &hashSchemaVersion,
		&planStatus, &operation, &creatorAgentRunID, &creatorAgentRunPublicID, &creatorDiagnosisHash, &targetRepository,
		&baseBranch, &baseRevision, &lastKnownGood, &baseSHA, &fileMode, &targetPath,
		&targetResource, &targetFieldRef, &beforeHash, &postHash, &treeHash, &changeManifest,
		&patchHash, &boundedDiff, &policyVersion, &policyHash, &verificationHash,
		&evidenceBindings, &evidenceSetHash, &planHash, &riskLevel, &planCreatedAt, &planExpiresAt); err != nil {
		return
	}
	var bindings []struct {
		ID          string `json:"id"`
		ContentHash string `json:"content_hash"`
	}
	if planID != snapshot.RemediationPlanID || planContentSchemaVersion != 2 || planVersion <= 0 || hashSchemaVersion <= 0 ||
		planStatus != "consumed" || creatorAgentRunID == 0 || creatorAgentRunPublicID == "" || len(creatorDiagnosisHash) != 64 ||
		!json.Valid(targetResource) || !json.Valid(changeManifest) || json.Unmarshal(evidenceBindings, &bindings) != nil ||
		len(bindings) == 0 || len(beforeHash) != 64 || len(postHash) != 64 || len(patchHash) != 64 ||
		len(policyHash) != 64 || len(verificationHash) != 64 || len(evidenceSetHash) != 64 || len(planHash) != 64 {
		return nil, nil, nil, 0, "", "", asyncjob.ErrInvalidMutation
	}
	plan, err = boundedJSON(map[string]any{
		"id": planPublic, "plan_version": planVersion, "content_schema_version": planContentSchemaVersion,
		"hash_schema_version": hashSchemaVersion, "status": planStatus, "operation": operation,
		"creator_agent_run_id": creatorAgentRunPublicID, "diagnosis_hash": creatorDiagnosisHash,
		"target": map[string]any{
			"repository": targetRepository, "base_branch": baseBranch, "base_revision": baseRevision,
			"last_known_good_revision": lastKnownGood, "base_blob_sha": baseSHA, "file_mode": fileMode,
			"path": targetPath, "resource": json.RawMessage(targetResource), "field_ref": targetFieldRef,
		},
		"expected_before_hash": beforeHash, "expected_post_image_hash": postHash,
		"expected_tree_hash": treeHash, "proposed_patch_hash": patchHash,
		"change_manifest_bytes": len(changeManifest), "change_manifest_hash": sha256Hex(changeManifest),
		"bounded_diff":   reportTextProjection(boundedDiff, 1024, 1024),
		"policy_version": policyVersion, "policy_snapshot_hash": policyHash,
		"verification_plan_hash": verificationHash,
		"evidence_binding_count": len(bindings), "evidence_bindings_hash": sha256Hex(evidenceBindings),
		"evidence_set_hash": evidenceSetHash, "canonical_plan_hash": planHash, "risk_level": riskLevel,
		"created_at": planCreatedAt.UTC().Format(time.RFC3339Nano), "expires_at": planExpiresAt.UTC().Format(time.RFC3339Nano),
	}, 16384)
	if err != nil {
		return nil, nil, nil, 0, "", "", err
	}
	var decisionSchemaVersion, decisionPlanVersion, approvedHashSchemaVersion int
	var decisionPublic, decisionValue, actorProvider, actorLogin, actorRole, decisionReason, requestID string
	var approvedHash, approvedBase, approvedPost, approvedTree, approvedPatch string
	var approvedPolicy, approvedVerification, approvedEvidence string
	var requestAuthenticatedAt, decisionExpiresAt, decisionCreatedAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT id, public_id, decision_schema_version, plan_version,
       decision, actor_provider, actor_login, actor_role, reason, request_id,
       request_authenticated_at, expires_at, approved_hash_schema_version,
       approved_plan_hash, approved_base_sha, approved_post_image_hash,
       approved_tree_hash, approved_patch_hash, approved_policy_hash,
       approved_verification_hash, approved_evidence_set_hash, created_at
FROM remediation_decisions
WHERE plan_id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3
ORDER BY id DESC LIMIT 1`, snapshot.RemediationPlanID, task.IncidentID, task.CycleNo).Scan(
		&decisionID, &decisionPublic, &decisionSchemaVersion, &decisionPlanVersion,
		&decisionValue, &actorProvider, &actorLogin, &actorRole, &decisionReason, &requestID,
		&requestAuthenticatedAt, &decisionExpiresAt, &approvedHashSchemaVersion,
		&approvedHash, &approvedBase, &approvedPost, &approvedTree, &approvedPatch,
		&approvedPolicy, &approvedVerification, &approvedEvidence, &decisionCreatedAt)
	if err != nil {
		return
	}
	if decisionID == 0 || decisionSchemaVersion <= 0 || decisionPlanVersion != planVersion || decisionValue != "approved" ||
		approvedHashSchemaVersion != hashSchemaVersion || approvedHash != planHash || approvedBase != baseRevision ||
		approvedPost != postHash || approvedTree != treeHash || approvedPatch != patchHash || approvedPolicy != policyHash ||
		approvedVerification != verificationHash || approvedEvidence != evidenceSetHash {
		return nil, nil, nil, 0, "", "", asyncjob.ErrInvalidMutation
	}
	decision, err = boundedJSON(map[string]any{
		"id": decisionPublic, "schema_version": decisionSchemaVersion, "plan_version": decisionPlanVersion,
		"decision": decisionValue, "actor": map[string]any{"provider": actorProvider, "login": actorLogin, "role": actorRole},
		"reason": decisionReason, "request_id": requestID,
		"request_authenticated_at": requestAuthenticatedAt.UTC().Format(time.RFC3339Nano),
		"expires_at":               decisionExpiresAt.UTC().Format(time.RFC3339Nano), "created_at": decisionCreatedAt.UTC().Format(time.RFC3339Nano),
		"approved_hash_schema_version": approvedHashSchemaVersion, "approved_plan_hash": approvedHash,
		"approved_base_sha": approvedBase, "approved_post_image_hash": approvedPost,
		"approved_tree_hash": approvedTree, "approved_patch_hash": approvedPatch,
		"approved_policy_hash": approvedPolicy, "approved_verification_hash": approvedVerification,
		"approved_evidence_set_hash": approvedEvidence,
	}, 8192)
	if err != nil {
		return nil, nil, nil, 0, "", "", err
	}
	var changePublic, repository, prURL, mergedSHA, targetRevision, prState, v3Status, legacyStatus string
	var changePlanID uint64
	var ciStatus, detectedRevision, argoSync, argoPhase, argoHealth string
	var cluster, environment, namespace, workloadKind, workloadName, rolloutRevision string
	var resourceHealth []byte
	var syncStarted, syncCompleted, deliveryStarted, deliveryCompleted sql.NullTime
	var generation, observedGeneration int64
	var desired, updated, available, unavailable int
	err = tx.QueryRowContext(ctx, `
SELECT public_id, plan_id, repository, pr_url, merged_commit_sha, target_revision, pr_state,
       v3_status, status, ci_status, detected_revision, argocd_sync_status,
       argocd_operation_phase, argocd_health_status, COALESCE(resource_health_json, JSON_OBJECT()),
       sync_started_at, sync_completed_at, cluster, environment, namespace, workload_kind,
       workload_name, deployment_generation, observed_generation, rollout_revision,
       desired_replicas, updated_replicas, available_replicas, unavailable_replicas,
       delivery_started_at, delivery_completed_at
FROM change_requests
WHERE id = ? AND incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`,
		snapshot.ChangeRequestID, task.IncidentID, task.CycleNo).Scan(
		&changePublic, &changePlanID, &repository, &prURL, &mergedSHA, &targetRevision, &prState,
		&v3Status, &legacyStatus, &ciStatus, &detectedRevision, &argoSync, &argoPhase,
		&argoHealth, &resourceHealth, &syncStarted, &syncCompleted, &cluster, &environment,
		&namespace, &workloadKind, &workloadName, &generation, &observedGeneration,
		&rolloutRevision, &desired, &updated, &available, &unavailable, &deliveryStarted,
		&deliveryCompleted)
	if err != nil {
		return
	}
	if !json.Valid(resourceHealth) {
		return nil, nil, nil, 0, "", "", asyncjob.ErrInvalidMutation
	}
	if changePlanID != snapshot.RemediationPlanID {
		return nil, nil, nil, 0, "", "", asyncjob.ErrSubjectVersionMismatch
	}
	if ciStatus != "passing" || !strings.EqualFold(detectedRevision, targetRevision) ||
		!strings.EqualFold(argoSync, "Synced") || !strings.EqualFold(argoPhase, "Succeeded") ||
		generation <= 0 || observedGeneration < generation || desired <= 0 || updated != desired ||
		available != desired || unavailable != 0 || !deliveryStarted.Valid || !deliveryCompleted.Valid ||
		deliveryCompleted.Time.Before(deliveryStarted.Time) || cluster == "" || environment == "" || namespace == "" ||
		workloadKind == "" || workloadName == "" || rolloutRevision == "" || !strings.EqualFold(targetRevision, snapshot.GitOpsRevision) {
		return nil, nil, nil, 0, "", "", fmt.Errorf("%w: delivered change projection is incomplete", asyncjob.ErrInvalidMutation)
	}
	deliveryObservations, observationErr := reportDeliveryObservations(ctx, tx, task, changePublic)
	if observationErr != nil {
		return nil, nil, nil, 0, "", "", observationErr
	}
	delivery, err = boundedJSON(map[string]any{
		"id": changePublic, "repository": repository, "pr_url": prURL,
		"merged_commit_sha": mergedSHA, "target_revision": targetRevision,
		"pr_state": prState, "status": v3Status, "ci_status": ciStatus,
		"argocd":              map[string]any{"detected_revision": detectedRevision, "sync_status": argoSync, "operation_phase": argoPhase, "health_status": argoHealth, "resource_health": reportJSONProjection(resourceHealth, 4096), "sync_started_at": nullableTimeString(syncStarted), "sync_completed_at": nullableTimeString(syncCompleted)},
		"rollout":             map[string]any{"cluster": cluster, "environment": environment, "namespace": namespace, "workload_kind": workloadKind, "workload_name": workloadName, "generation": generation, "observed_generation": observedGeneration, "rollout_revision": rolloutRevision, "desired": desired, "updated": updated, "available": available, "unavailable": unavailable},
		"delivery_started_at": nullableTimeString(deliveryStarted), "delivery_completed_at": nullableTimeString(deliveryCompleted),
		"observations": json.RawMessage(deliveryObservations),
	}, 16384)
	if err != nil {
		return nil, nil, nil, 0, "", "", err
	}
	if v3Status != "delivered" || legacyStatus != "delivered" || (prState != "closed" && prState != "merged") || mergedSHA == "" || targetRevision == "" || mergedSHA != targetRevision {
		return nil, nil, nil, 0, "", "", asyncjob.ErrInvalidMutation
	}
	return plan, decision, delivery, decisionID, baseRevision, targetRevision, nil
}

func reportDeliveryObservations(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task, changeRequestPublicID string) ([]byte, error) {
	if changeRequestPublicID == "" {
		return nil, asyncjob.ErrInvalidMutation
	}
	rows, err := tx.QueryContext(ctx, `SELECT public_id, facts_json, content_hash, producer_dedupe_key, collected_at FROM evidence_items WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 AND producer_type IN ('delivery.observe','delivery_observation') AND valid = TRUE ORDER BY collected_at, id`, task.IncidentID, task.CycleNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type observationRecord struct {
		id          string
		kind        DeliveryObservationKind
		contentHash string
		collectedAt time.Time
	}
	latest := make(map[DeliveryObservationKind]observationRecord, 4)
	setHash := sha256.New()
	count := 0
	for rows.Next() {
		var id, contentHash, producerKey string
		var facts []byte
		var collectedAt time.Time
		if err := rows.Scan(&id, &facts, &contentHash, &producerKey, &collectedAt); err != nil {
			return nil, err
		}
		var envelope struct {
			Kind DeliveryObservationKind `json:"kind"`
		}
		if !json.Valid(facts) || json.Unmarshal(facts, &envelope) != nil || !validDeliveryObservationKind(envelope.Kind) ||
			len(contentHash) != 64 {
			return nil, fmt.Errorf("%w: delivery observation %s has an invalid envelope or content hash", asyncjob.ErrInvalidMutation, id)
		}
		if producerKey != hashCanonical("delivery.observe", changeRequestPublicID, string(envelope.Kind), contentHash) {
			continue
		}
		count++
		collected := collectedAt.UTC().Format(time.RFC3339Nano)
		appendVerificationHash(setHash, id, envelope.Kind, contentHash, producerKey, collected)
		latest[envelope.Kind] = observationRecord{id: id, kind: envelope.Kind, contentHash: contentHash, collectedAt: collectedAt.UTC()}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, fmt.Errorf("%w: current ChangeRequest has no delivery observation evidence", asyncjob.ErrInvalidMutation)
	}
	kinds := []DeliveryObservationKind{DeliveryObservePullRequest, DeliveryObserveCI, DeliveryObserveArgo, DeliveryObserveRollout}
	latestItems := make([]map[string]any, 0, len(latest))
	for _, kind := range kinds {
		record, ok := latest[kind]
		if !ok {
			return nil, fmt.Errorf("%w: delivered change lacks %s observation evidence", asyncjob.ErrInvalidMutation, kind)
		}
		latestItems = append(latestItems, map[string]any{
			"id": record.id, "kind": record.kind, "content_hash": record.contentHash,
			"collected_at": record.collectedAt.Format(time.RFC3339Nano),
		})
	}
	return boundedJSON(map[string]any{
		"observation_count":    count,
		"observation_set_hash": hex.EncodeToString(setHash.Sum(nil)),
		"latest_by_kind":       latestItems,
	}, 4096)
}

func validDeliveryObservationKind(kind DeliveryObservationKind) bool {
	switch kind {
	case DeliveryObservePullRequest, DeliveryObserveCI, DeliveryObserveArgo, DeliveryObserveRollout:
		return true
	default:
		return false
	}
}

func reportSamples(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task) ([]byte, error) {
	rows, err := tx.QueryContext(ctx, `SELECT public_id, sample_schema_version, verification_check_id, sample_sequence, status, observed_json, source_reference, reason_code, window_start_at, window_end_at, sampled_at, content_hash FROM verification_samples WHERE verification_run_id = ? AND incident_id = ? AND cycle_no = ? ORDER BY verification_check_id, sample_sequence, id LIMIT 2001`, task.SubjectID, task.IncidentID, task.CycleNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	count := 0
	latest := make(map[uint64]map[string]any)
	latestIDs := make([]uint64, 0, 16)
	contentParts := make([]any, 0, 128)
	var firstSample, lastSample time.Time
	for rows.Next() {
		var id, status, source, reason, hash string
		var sampleSchemaVersion int
		var checkID uint64
		var sequence int
		var observed []byte
		var windowStart, windowEnd, sampled sql.NullTime
		if err := rows.Scan(&id, &sampleSchemaVersion, &checkID, &sequence, &status, &observed, &source, &reason, &windowStart, &windowEnd, &sampled, &hash); err != nil {
			return nil, err
		}
		count++
		if count > 2000 {
			return nil, fmt.Errorf("%w: verification sample count exceeds report bound", asyncjob.ErrInvalidMutation)
		}
		if sampleSchemaVersion != verificationSampleSchema || !json.Valid(observed) || !sampled.Valid || len(hash) != 64 {
			return nil, asyncjob.ErrInvalidMutation
		}
		if firstSample.IsZero() || sampled.Time.Before(firstSample) {
			firstSample = sampled.Time
		}
		if lastSample.IsZero() || sampled.Time.After(lastSample) {
			lastSample = sampled.Time
		}
		contentParts = append(contentParts, checkID, sequence, hash)
		item := map[string]any{"id": id, "check_id": checkID, "sequence": sequence, "status": status, "observed": reportJSONProjection(observed, 1024), "source_reference": boundVerificationText(source, 128), "reason_code": boundVerificationText(reason, 128), "sampled_at": sampled.Time.UTC().Format(time.RFC3339Nano), "content_hash": hash}
		if windowStart.Valid {
			item["window_start_at"] = windowStart.Time.UTC().Format(time.RFC3339Nano)
		}
		if windowEnd.Valid {
			item["window_end_at"] = windowEnd.Time.UTC().Format(time.RFC3339Nano)
		}
		if _, exists := latest[checkID]; !exists {
			latestIDs = append(latestIDs, checkID)
		}
		latest[checkID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, asyncjob.ErrInvalidMutation
	}
	sort.Slice(latestIDs, func(i, j int) bool { return latestIDs[i] < latestIDs[j] })
	latestItems := make([]map[string]any, 0, len(latestIDs))
	for _, checkID := range latestIDs {
		latestItems = append(latestItems, latest[checkID])
	}
	return boundedJSON(map[string]any{
		"sample_count":     count,
		"sample_set_hash":  hashVerificationTask(contentParts...),
		"first_sampled_at": firstSample.UTC().Format(time.RFC3339Nano),
		"last_sampled_at":  lastSample.UTC().Format(time.RFC3339Nano),
		"latest_by_check":  latestItems,
	}, 32768)
}

func reportTimeline(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task) ([]byte, error) {
	rows, err := tx.QueryContext(ctx, `SELECT public_id, event_type, actor_type, actor_id, summary, metadata_json, occurred_at FROM incident_events WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3 ORDER BY occurred_at, id`, task.IncidentID, task.CycleNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	const (
		firstTimelineItems = 8
		lastTimelineItems  = 16
	)
	first := make([]map[string]any, 0, firstTimelineItems)
	latest := make([]map[string]any, 0, lastTimelineItems)
	setHash := sha256.New()
	count := 0
	resolvedSeen := false
	var resolvedItem map[string]any
	for rows.Next() {
		var id, typ, actorType, actorID, summary string
		var metadata []byte
		var occurred time.Time
		if err := rows.Scan(&id, &typ, &actorType, &actorID, &summary, &metadata, &occurred); err != nil {
			return nil, err
		}
		if !json.Valid(metadata) {
			return nil, asyncjob.ErrInvalidMutation
		}
		count++
		occurredAt := occurred.UTC().Format(time.RFC3339Nano)
		metadataHash := sha256.Sum256(metadata)
		appendVerificationHash(setHash, id, typ, actorType, actorID, summary, string(metadata), occurredAt)
		item := map[string]any{
			"id": id, "event_type": typ, "actor_type": actorType, "actor_id": actorID,
			"summary": boundVerificationText(summary, 256), "metadata_hash": hex.EncodeToString(metadataHash[:]),
			"occurred_at": occurredAt,
		}
		if typ == "incident_resolved" {
			resolvedSeen = true
			resolvedItem = item
		}
		if len(first) < firstTimelineItems {
			first = append(first, item)
			continue
		}
		if len(latest) == lastTimelineItems {
			copy(latest, latest[1:])
			latest[len(latest)-1] = item
		} else {
			latest = append(latest, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if count == 0 || !resolvedSeen {
		return nil, asyncjob.ErrInvalidMutation
	}
	items := append(append([]map[string]any(nil), first...), latest...)
	resolvedRetained := false
	for _, item := range items {
		if item["event_type"] == "incident_resolved" {
			resolvedRetained = true
			break
		}
	}
	if !resolvedRetained {
		items = append(items, resolvedItem)
	}
	return boundedJSON(map[string]any{
		"event_count": count, "event_set_hash": hex.EncodeToString(setHash.Sum(nil)),
		"events": items, "events_truncated": len(items) < count,
	}, 32768)
}

func reportAgentUsage(ctx context.Context, tx asyncjob.DBTX, task asyncjob.Task) ([]byte, error) {
	var runs, steps, toolCalls, modelCalls, tokens int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(used_steps),0), COALESCE(SUM(used_tool_calls),0), COALESCE(SUM(used_model_calls),0), COALESCE(SUM(input_tokens + output_tokens),0) FROM agent_runs WHERE incident_id = ? AND cycle_no = ? AND domain_schema_version = 3`, task.IncidentID, task.CycleNo).Scan(&runs, &steps, &toolCalls, &modelCalls, &tokens); err != nil {
		return nil, err
	}
	return boundedJSON(map[string]any{"agent_runs": runs, "steps": steps, "tool_calls": toolCalls, "model_calls": modelCalls, "tokens": tokens}, 8192)
}

func boundedJSON(value any, max int) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil || len(data) > max || !json.Valid(data) {
		return nil, asyncjob.ErrInvalidMutation
	}
	return data, nil
}

func reportEvidenceNonEmpty(value []byte) bool {
	var envelope struct {
		Count int `json:"evidence_count"`
	}
	return json.Unmarshal(value, &envelope) == nil && envelope.Count > 0
}

func reportTextProjection(value string, firstBytes, lastBytes int) map[string]any {
	data := []byte(value)
	result := map[string]any{
		"byte_count": len(data), "content_hash": sha256Hex(data), "truncated": false,
	}
	if len(data) <= firstBytes+lastBytes {
		result["text"] = value
		return result
	}
	result["truncated"] = true
	result["first"] = string(data[:firstBytes])
	result["last"] = string(data[len(data)-lastBytes:])
	return result
}

func reportJSONProjection(value []byte, maxBytes int) any {
	if len(value) <= maxBytes {
		return json.RawMessage(value)
	}
	return map[string]any{
		"byte_count": len(value), "content_hash": sha256Hex(value), "truncated": true,
	}
}

func verificationNullableJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func verificationNullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTimeString(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time.UTC().Format(time.RFC3339Nano)
}
