package taskhandler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
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
