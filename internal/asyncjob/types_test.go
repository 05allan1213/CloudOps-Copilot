package asyncjob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFrozenTaskTypeQueueMapping(t *testing.T) {
	t.Parallel()
	want := map[TaskType]Queue{
		TaskInvestigationAdvance: QueueInvestigate,
		TaskRemediationPrepare:   QueueInvestigate,
		TaskChangeEnsurePR:       QueueDeliver,
		TaskDeliveryObserve:      QueueObserve,
		TaskVerificationAdvance:  QueueVerify,
	}
	if got := TaskTypes(); len(got) != 5 {
		t.Fatalf("task types=%d, want 5", len(got))
	}
	for taskType, queue := range want {
		got, err := QueueForTaskType(taskType)
		if err != nil || got != queue {
			t.Fatalf("queue for %q=%q err=%v, want %q", taskType, got, err, queue)
		}
	}
	if _, err := QueueForTaskType("postmortem.generate"); err == nil {
		t.Fatal("unsupported task type was accepted")
	}
}

func TestNewTaskValidationBoundsPayloadAndKeys(t *testing.T) {
	t.Parallel()
	valid := NewTask{
		IncidentID:             1,
		CycleNo:                1,
		Type:                   TaskInvestigationAdvance,
		SubjectType:            "incident",
		SubjectID:              1,
		Transition:             "investigation.start",
		ExpectedSubjectVersion: 1,
		PayloadSchemaVersion:   1,
		Payload:                json.RawMessage(`{"mode":"start"}`),
		DedupeKey:              strings.Repeat("a", 64),
		MaxAttempts:            3,
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*NewTask){
		"unknown type":               func(task *NewTask) { task.Type = "unknown" },
		"illegal subject transition": func(task *NewTask) { task.Transition = "investigation.step" },
		"illegal type subject":       func(task *NewTask) { task.Type = TaskRemediationPrepare },
		"zero version":               func(task *NewTask) { task.ExpectedSubjectVersion = 0 },
		"invalid payload":            func(task *NewTask) { task.Payload = json.RawMessage(`{`) },
		"large payload":              func(task *NewTask) { task.Payload = json.RawMessage(`"` + strings.Repeat("x", 8192) + `"`) },
		"invalid dedupe":             func(task *NewTask) { task.DedupeKey = strings.Repeat("z", 64) },
		"zero attempts":              func(task *NewTask) { task.MaxAttempts = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("invalid task was accepted")
			}
		})
	}
}

func TestCheckpointValidationBindsHash(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{"next":"tool"}`)
	digest := sha256.Sum256(payload)
	checkpoint := Checkpoint{SchemaVersion: 1, Version: 2, Hash: hex.EncodeToString(digest[:]), Payload: payload}
	if err := checkpoint.Validate(); err != nil {
		t.Fatal(err)
	}
	checkpoint.Hash = strings.Repeat("0", 64)
	if err := checkpoint.Validate(); err == nil {
		t.Fatal("mismatched checkpoint hash was accepted")
	}
}

func TestResultValidation(t *testing.T) {
	t.Parallel()
	valid := []Result{
		Succeeded(nil),
		RetryAfter(time.Second, "transient", "bounded", nil),
		Dead("invalid", "bounded", nil),
	}
	for _, result := range valid {
		if err := result.Validate(); err != nil {
			t.Fatalf("valid result %+v: %v", result, err)
		}
	}
	invalid := []Result{
		{},
		RetryAfter(-time.Second, "transient", "bounded", nil),
		RetryAfter(time.Second, "", "bounded", nil),
		Dead("", "bounded", nil),
		{Disposition: DispositionSucceeded, ErrorCode: "unexpected"},
	}
	for _, result := range invalid {
		if err := result.Validate(); err == nil {
			t.Fatalf("invalid result accepted: %+v", result)
		}
	}
}

func TestBackoffPolicyExponentialCapAndJitter(t *testing.T) {
	t.Parallel()
	policy := BackoffPolicy{Initial: time.Second, Maximum: 8 * time.Second}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for index, expected := range want {
		if got := policy.Delay(uint32(index + 1)); got != expected {
			t.Fatalf("attempt %d delay=%v, want %v", index+1, got, expected)
		}
	}
	policy.Jitter = .25
	policy.Random = func() float64 { return 0 }
	if got := policy.Delay(1); got != 750*time.Millisecond {
		t.Fatalf("jittered delay=%v, want 750ms", got)
	}
	policy.Jitter = 1
	if err := policy.Validate(); err == nil {
		t.Fatal("jitter of one permits a zero retry delay")
	}
}

func TestExternalCallContextFailsWithoutRunnerPolicy(t *testing.T) {
	t.Parallel()
	if _, _, err := ExternalCallContext(context.Background()); !errors.Is(err, ErrExternalDeadlineMissing) {
		t.Fatalf("external context error=%v", err)
	}
}
