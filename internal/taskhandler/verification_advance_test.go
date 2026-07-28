package taskhandler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/baseline"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestVerificationAdvanceEvaluatesOneSampleAndDefersResolution(t *testing.T) {
	now := time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)
	check := verification.Check{
		ID: 7, PublicID: "check-1", Type: verification.CheckMetricErrorRateBelow, Status: verification.CheckPending,
		Required: true, Comparison: verification.CompareLT, Threshold: .01, MinSamples: 50, SampleUnit: "requests",
		FailureMode: verification.FailureResets, Lookback: time.Minute, StabilityWindow: time.Minute,
	}
	storeCalled := false
	operation, err := NewVerificationAdvance(VerificationAdvanceConfig{
		Reader: VerificationAdvanceReaderFunc(func(context.Context, asyncjob.Task) (VerificationAdvanceSnapshot, error) {
			return VerificationAdvanceSnapshot{
				Run:    verification.Run{ID: 9, PublicID: "run-1", IncidentID: 4, Status: verification.RunRunning, RowVersion: 2, DeadlineAt: now.Add(5 * time.Minute)},
				Checks: []verification.Check{check}, Observation: verification.Observation{
					Status: verification.ObservationAvailable, Value: .001, Denominator: 50, SampleCount: 50,
					SampledAt: now, QueryValid: true, SourceHealthy: true, RetentionCovered: true,
				}, Now: now,
			}, nil
		}),
		Store: VerificationAdvanceStoreFunc(func(_ context.Context, _ asyncjob.DBTX, _ asyncjob.Task, _ VerificationAdvanceSnapshot, updated verification.Check, sample verification.Sample, status verification.RunStatus, _ string, _ *time.Time) error {
			storeCalled = true
			if updated.Status != verification.CheckRunning || sample.Status != verification.SamplePassed || status != verification.RunRunning {
				t.Fatalf("unexpected persisted state check=%+v sample=%+v run=%s", updated, sample, status)
			}
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(verificationAdvancePayload{VerificationRunID: "run-1", CheckID: check.PublicID, CycleNo: 1})
	task := asyncjob.Task{
		ID: 12, IncidentID: 4, CycleNo: 1, Queue: asyncjob.QueueVerify, Type: asyncjob.TaskVerificationAdvance,
		SubjectType: "verification_run", SubjectID: 9, Transition: "verification.advance", ExpectedSubjectVersion: 2,
		PayloadSchemaVersion: verificationAdvancePayloadSchema, Payload: payload,
	}
	execution := asyncjob.Execution{Task: task, Lease: asyncjob.Lease{TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: 2, Attempt: 1, MaxAttempts: 5}}
	result := operation(context.Background(), execution)
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
		t.Fatalf("result=%+v", result)
	}
	if err := result.Mutate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !storeCalled {
		t.Fatal("verification store was not called")
	}
}

func TestVerificationAdvanceRejectsUnknownCheckWithoutMutation(t *testing.T) {
	now := time.Now().UTC()
	operation, err := NewVerificationAdvance(VerificationAdvanceConfig{
		Reader: VerificationAdvanceReaderFunc(func(context.Context, asyncjob.Task) (VerificationAdvanceSnapshot, error) {
			return VerificationAdvanceSnapshot{Run: verification.Run{ID: 1, PublicID: "run", IncidentID: 1, Status: verification.RunRunning, RowVersion: 1, DeadlineAt: now.Add(time.Minute)}, Checks: []verification.Check{{PublicID: "known", Status: verification.CheckPassed}}, Now: now}, nil
		}),
		Store: VerificationAdvanceStoreFunc(func(context.Context, asyncjob.DBTX, asyncjob.Task, VerificationAdvanceSnapshot, verification.Check, verification.Sample, verification.RunStatus, string, *time.Time) error {
			t.Fatal("store called for unknown check")
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(verificationAdvancePayload{VerificationRunID: "run", CheckID: "missing", CycleNo: 1})
	task := asyncjob.Task{ID: 1, IncidentID: 1, CycleNo: 1, Queue: asyncjob.QueueVerify, Type: asyncjob.TaskVerificationAdvance, SubjectType: "verification_run", SubjectID: 1, Transition: "verification.advance", ExpectedSubjectVersion: 1, PayloadSchemaVersion: verificationAdvancePayloadSchema, Payload: payload}
	result := operation(context.Background(), asyncjob.Execution{Task: task, Lease: asyncjob.Lease{TaskID: 1, Owner: "w", Generation: 1, ExpectedSubjectVersion: 1, Attempt: 1, MaxAttempts: 1}})
	if result.Disposition != asyncjob.DispositionDead || !strings.Contains(result.ErrorCode, "verification_checks_exhausted") {
		t.Fatalf("result=%+v", result)
	}
}

func TestVerificationAdvanceDefersUntilSelectedCheckIsDue(t *testing.T) {
	now := time.Date(2026, 7, 20, 4, 0, 0, 0, time.UTC)
	due := now.Add(7 * time.Second)
	storeCalled := false
	operation, err := NewVerificationAdvance(VerificationAdvanceConfig{
		Reader: VerificationAdvanceReaderFunc(func(context.Context, asyncjob.Task) (VerificationAdvanceSnapshot, error) {
			return VerificationAdvanceSnapshot{
				Run:       verification.Run{ID: 9, PublicID: "run-1", IncidentID: 4, Status: verification.RunRunning, RowVersion: 2, DeadlineAt: now.Add(time.Minute)},
				Checks:    []verification.Check{{ID: 7, PublicID: "check-1", Status: verification.CheckPending}},
				NextDueAt: due, Now: now,
			}, nil
		}),
		Store: VerificationAdvanceStoreFunc(func(context.Context, asyncjob.DBTX, asyncjob.Task, VerificationAdvanceSnapshot, verification.Check, verification.Sample, verification.RunStatus, string, *time.Time) error {
			storeCalled = true
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	task := verificationTask(t, 9, 4, 2, "run-1", "")
	result := operation(context.Background(), asyncjob.Execution{Task: task, Lease: verificationLease(task)})
	if result.Disposition != asyncjob.DispositionRetry || result.ErrorCode != "verification_check_not_due" || result.RetryAfter != due.Sub(now) || result.Mutate != nil {
		t.Fatalf("result=%+v", result)
	}
	if storeCalled {
		t.Fatal("not-due check reached persistence")
	}
}

func TestVerificationAdvancePersistsExplicitCheckTimeout(t *testing.T) {
	now := time.Date(2026, 7, 20, 4, 1, 0, 0, time.UTC)
	check := verification.Check{
		ID: 7, PublicID: "check-1", VerificationRunID: 9,
		Type: verification.CheckMetricErrorRateBelow, Status: verification.CheckRunning,
		Required: true, Comparison: verification.CompareLT, Threshold: .01,
		MinSamples: 50, SampleUnit: "requests", FailureMode: verification.FailureResets,
		PollInterval: time.Second, StabilityWindow: time.Minute,
	}
	operation, err := NewVerificationAdvance(VerificationAdvanceConfig{
		Reader: VerificationAdvanceReaderFunc(func(context.Context, asyncjob.Task) (VerificationAdvanceSnapshot, error) {
			return VerificationAdvanceSnapshot{
				Run:    verification.Run{ID: 9, PublicID: "run-1", IncidentID: 4, Status: verification.RunRunning, RowVersion: 2, DeadlineAt: now.Add(time.Minute)},
				Checks: []verification.Check{check}, CheckDeadlineAt: now, Now: now,
				Observation: verification.Observation{Status: verification.ObservationAvailable, Value: 0, SampleCount: 100, SampledAt: now, QueryValid: true, SourceHealthy: true, RetentionCovered: true},
			}, nil
		}),
		Store: VerificationAdvanceStoreFunc(func(_ context.Context, _ asyncjob.DBTX, _ asyncjob.Task, _ VerificationAdvanceSnapshot, updated verification.Check, sample verification.Sample, status verification.RunStatus, reason string, _ *time.Time) error {
			if updated.Status != verification.CheckTimedOut || sample.Status != verification.SampleTimedOut || status != verification.RunTimedOut || reason != "required_check_timed_out" {
				t.Fatalf("updated=%+v sample=%+v status=%s reason=%s", updated, sample, status, reason)
			}
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	task := verificationTask(t, 9, 4, 2, "run-1", "check-1")
	result := operation(context.Background(), asyncjob.Execution{Task: task, Lease: verificationLease(task)})
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil {
		t.Fatalf("result=%+v", result)
	}
	if err := result.Mutate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestVerificationReaderRotatesAcrossNonTerminalChecks(t *testing.T) {
	start := time.Date(2026, 7, 20, 4, 0, 0, 0, time.UTC)
	oldest := start.Add(time.Second)
	newer := start.Add(2 * time.Second)
	checks := []verification.Check{
		{PublicID: "terminal", Status: verification.CheckPassed},
		{PublicID: "newer", Status: verification.CheckRunning, LastCheckedAt: &newer, PollInterval: 5 * time.Second, Timeout: time.Minute},
		{PublicID: "never", Status: verification.CheckPending, InitialDelay: 3 * time.Second, PollInterval: 10 * time.Second, Timeout: time.Minute},
		{PublicID: "oldest", Status: verification.CheckRunning, LastCheckedAt: &oldest, PollInterval: 10 * time.Second, Timeout: time.Minute},
	}
	if index := selectVerificationReaderCheck(checks, "", start, start.Add(time.Minute)); index != 2 {
		t.Fatalf("initial-delay rotation selected index %d", index)
	}
	checks[2].LastCheckedAt = &newer
	if index := selectVerificationReaderCheck(checks, "", start, start.Add(time.Minute)); index != 1 {
		t.Fatalf("earliest-due rotation selected index %d", index)
	}
	if index := selectVerificationReaderCheck(checks, "newer", start, start.Add(time.Minute)); index != 1 {
		t.Fatalf("explicit check selected index %d", index)
	}
}

func TestVerificationReaderWakesAtDeadlineBeforeNextPoll(t *testing.T) {
	start := time.Date(2026, 7, 20, 4, 0, 0, 0, time.UTC)
	deadline := start.Add(100 * time.Second)
	lastChecked := start.Add(96 * time.Second)
	check := verification.Check{
		Status: verification.CheckRunning, LastCheckedAt: &lastChecked,
		PollInterval: 10 * time.Second, Timeout: 100 * time.Second,
	}
	dueAt, checkDeadline, nextAt := verificationReaderCheckSchedule(check, start, deadline)
	now := start.Add(99 * time.Second)
	if dueAt != start.Add(106*time.Second) || checkDeadline != deadline || nextAt != deadline || !now.Before(nextAt) {
		t.Fatalf("due=%s deadline=%s next=%s now=%s", dueAt, checkDeadline, nextAt, now)
	}
}

func TestMySQLVerificationAdvanceRequiresReportWriter(t *testing.T) {
	_, err := NewMySQLVerificationAdvance(MySQLVerificationAdvanceConfig{
		DB: new(sql.DB), Tasks: verificationTaskStoreStub{}, Observations: verificationObservationStub{}, Baselines: verificationBaselineStoreStub{},
	})
	if err == nil || !strings.Contains(err.Error(), "resolution-report") {
		t.Fatalf("err=%v", err)
	}
	_, err = NewMySQLVerificationAdvance(MySQLVerificationAdvanceConfig{
		DB: new(sql.DB), Tasks: verificationTaskStoreStub{}, Observations: verificationObservationStub{},
		Reports: NewMySQLResolutionReportWriter(),
	})
	if err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("missing baseline adapter err=%v", err)
	}
	_, err = NewMySQLVerificationAdvance(MySQLVerificationAdvanceConfig{
		DB: new(sql.DB), Tasks: verificationTaskStoreStub{}, Observations: verificationObservationStub{},
		Reports: NewMySQLResolutionReportWriter(), Baselines: verificationBaselineStoreStub{}, MaxAgentRuns: HardAgentRunBudget,
	})
	if err == nil || !strings.Contains(err.Error(), "fixed investigation.start budget") {
		t.Fatalf("err=%v", err)
	}
}

func TestReportEvidenceEnvelopeAllowsEmptyNoChangeCycle(t *testing.T) {
	payload, err := newReportEvidenceAccumulator().encode()
	if err != nil || !json.Valid(payload) {
		t.Fatalf("payload=%s err=%v", payload, err)
	}
	var envelope struct {
		Count     int              `json:"evidence_count"`
		SetHash   string           `json:"evidence_set_hash"`
		Items     []map[string]any `json:"items"`
		Snapshots []map[string]any `json:"fact_snapshots"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Count != 0 || len(envelope.SetHash) != 64 || len(envelope.Items) != 0 || len(envelope.Snapshots) != 0 || reportEvidenceNonEmpty(payload) {
		t.Fatalf("envelope=%+v", envelope)
	}
}

func TestReportEvidenceEnvelopeProjectsFullLargeSet(t *testing.T) {
	projection := newReportEvidenceAccumulator()
	for index := 0; index < 360; index++ {
		id := fmt.Sprintf("evidence-%03d", index)
		contentHash := hashVerificationTask(id)
		projection.add(id, contentHash,
			map[string]any{"id": id, "content_hash": contentHash},
			json.RawMessage(fmt.Sprintf(`{"sequence":%d}`, index)))
	}
	payload, err := projection.encode()
	if err != nil || !json.Valid(payload) || len(payload) > 32768 {
		t.Fatalf("payload_size=%d err=%v", len(payload), err)
	}
	var envelope struct {
		Count          int              `json:"evidence_count"`
		SetHash        string           `json:"evidence_set_hash"`
		Items          []map[string]any `json:"items"`
		ItemsTruncated bool             `json:"items_truncated"`
		Snapshots      []map[string]any `json:"fact_snapshots"`
		FactsTruncated bool             `json:"facts_truncated"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Count != 360 || len(envelope.SetHash) != 64 || len(envelope.Items) != 16 || !envelope.ItemsTruncated || len(envelope.Snapshots) != 4 || !envelope.FactsTruncated {
		t.Fatalf("envelope=%+v", envelope)
	}
	if envelope.Items[0]["id"] != "evidence-000" || envelope.Items[len(envelope.Items)-1]["id"] != "evidence-359" {
		t.Fatalf("boundary items=%v ... %v", envelope.Items[0], envelope.Items[len(envelope.Items)-1])
	}
}

func TestReportJSONProjectionBoundsLargeObservations(t *testing.T) {
	large := []byte(`{"payload":"` + strings.Repeat("x", verificationMaxObserved-32) + `"}`)
	if !json.Valid(large) || len(large) <= 1024 {
		t.Fatalf("fixture size=%d valid=%v", len(large), json.Valid(large))
	}
	items := make([]any, 0, 10)
	for index := 0; index < 10; index++ {
		items = append(items, reportJSONProjection(large, 1024))
	}
	payload, err := boundedJSON(map[string]any{"latest_by_check": items}, 32768)
	if err != nil || !json.Valid(payload) || len(payload) > 32768 {
		t.Fatalf("payload_size=%d err=%v", len(payload), err)
	}
	var envelope struct {
		Items []struct {
			ByteCount   int    `json:"byte_count"`
			ContentHash string `json:"content_hash"`
			Truncated   bool   `json:"truncated"`
		} `json:"latest_by_check"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Items) != 10 || !envelope.Items[0].Truncated || envelope.Items[0].ByteCount != len(large) || len(envelope.Items[0].ContentHash) != 64 {
		t.Fatalf("envelope=%+v", envelope)
	}
}

type verificationTaskStoreStub struct{}

func (verificationTaskStoreStub) EnqueueIn(context.Context, asyncjob.DBTX, asyncjob.NewTask) (*asyncjob.Task, error) {
	return nil, errors.New("not used")
}

type verificationObservationStub struct{}

func (verificationObservationStub) Observe(context.Context, verification.Run, verification.Check) (verification.Observation, error) {
	return verification.Observation{}, errors.New("not used")
}

type verificationBaselineStoreStub struct{}

func (verificationBaselineStoreStub) ActivateIn(context.Context, baseline.Transaction, baseline.Snapshot) (baseline.ActivationResult, error) {
	return baseline.ActivationResult{}, errors.New("not used")
}

func verificationTask(t *testing.T, runID, incidentID, version uint64, runPublicID, checkID string) asyncjob.Task {
	t.Helper()
	payload, err := json.Marshal(verificationAdvancePayload{VerificationRunID: runPublicID, CheckID: checkID, CycleNo: 1})
	if err != nil {
		t.Fatal(err)
	}
	return asyncjob.Task{
		ID: 12, IncidentID: incidentID, CycleNo: 1, Queue: asyncjob.QueueVerify,
		Type: asyncjob.TaskVerificationAdvance, SubjectType: "verification_run", SubjectID: runID,
		Transition: "verification.advance", ExpectedSubjectVersion: version,
		PayloadSchemaVersion: verificationAdvancePayloadSchema, Payload: payload,
	}
}

func verificationLease(task asyncjob.Task) asyncjob.Lease {
	return asyncjob.Lease{TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: 5}
}
