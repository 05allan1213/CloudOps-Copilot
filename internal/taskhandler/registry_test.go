package taskhandler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
)

func TestRegistryContainsOnlyFrozenTaskTypes(t *testing.T) {
	handlers := New(Config{})
	if len(handlers) != len(asyncjob.TaskTypes()) {
		t.Fatalf("handler count=%d task types=%d", len(handlers), len(asyncjob.TaskTypes()))
	}
	for _, taskType := range asyncjob.TaskTypes() {
		if handlers[taskType] == nil {
			t.Errorf("missing handler for %s", taskType)
		}
	}
	for taskType := range handlers {
		if !taskType.Valid() {
			t.Errorf("unexpected handler type %s", taskType)
		}
	}
}

func TestRegistryContainsOnlyFrozenDispatchIdentities(t *testing.T) {
	operation := func(context.Context, asyncjob.Execution) asyncjob.Result { return asyncjob.Succeeded(nil) }
	registry, err := NewRegistry(Config{
		InvestigationStep: operation, RemediationPrepare: operation, ChangeEnsurePR: operation,
		DeliveryObserve: operation, VerificationAdvance: operation,
	})
	if err != nil {
		t.Fatal(err)
	}
	keys := registry.DispatchKeys()
	if len(keys) != 7 {
		t.Fatalf("dispatch identities=%d, want 7", len(keys))
	}
	seen := make(map[DispatchKey]struct{}, len(keys))
	for _, key := range keys {
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate dispatch identity %s", key)
		}
		seen[key] = struct{}{}
	}
	keys[0] = DispatchKey{}
	if registry.DispatchKeys()[0] == (DispatchKey{}) {
		t.Fatal("DispatchKeys returned mutable registry storage")
	}
}

func TestRegistryDispatchesOnlyExactInjectedOperationsAndFailsClosed(t *testing.T) {
	called := false
	handlers := New(Config{DeliveryObserve: func(context.Context, asyncjob.Execution) asyncjob.Result {
		called = true
		return asyncjob.Succeeded(nil)
	}})
	result := handlers[asyncjob.TaskDeliveryObserve].Handle(context.Background(), executionForKey(deliveryObserveKey))
	if !called || result.Disposition != asyncjob.DispositionSucceeded {
		t.Fatalf("injected handler result=%+v called=%v", result, called)
	}
	result = handlers[asyncjob.TaskChangeEnsurePR].Handle(context.Background(), executionForKey(changeEnsurePlanPRKey))
	if !errors.Is(result.Validate(), asyncjob.ErrInvalidResult) || result.Mutate != nil {
		t.Fatalf("non-migrated handler fabricated a durable result=%+v", result)
	}

	called = false
	malformed := executionForKey(investigationStepKey)
	malformed.Task.SubjectType = "incident"
	result = handlers[asyncjob.TaskInvestigationAdvance].Handle(context.Background(), malformed)
	if called || result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "invalid_task_dispatch" {
		t.Fatalf("malformed dispatch result=%+v called=%v", result, called)
	}

	result = handlers[asyncjob.TaskDeliveryObserve].Handle(context.Background(), executionForKey(verificationAdvanceKey))
	if result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "invalid_task_dispatch" {
		t.Fatalf("task-type adapter accepted a different task type: %+v", result)
	}
}

func TestNewRuntimeRequiresEveryOneStepOperation(t *testing.T) {
	operation := func(context.Context, asyncjob.Execution) asyncjob.Result { return asyncjob.Succeeded(nil) }
	complete := Config{
		InvestigationStep: operation, RemediationPrepare: operation, ChangeEnsurePR: operation,
		DeliveryObserve: operation, VerificationAdvance: operation,
	}
	handlers, err := NewRuntime(complete)
	if err != nil || len(handlers) != len(asyncjob.TaskTypes()) {
		t.Fatalf("complete runtime handlers=%d err=%v", len(handlers), err)
	}
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"investigation.step", func(config *Config) { config.InvestigationStep = nil }},
		{"remediation.prepare", func(config *Config) { config.RemediationPrepare = nil }},
		{"change.ensure_pr", func(config *Config) { config.ChangeEnsurePR = nil }},
		{"delivery.observe", func(config *Config) { config.DeliveryObserve = nil }},
		{"verification.advance", func(config *Config) { config.VerificationAdvance = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := complete
			test.edit(&config)
			if _, err := NewRuntime(config); err == nil || !strings.Contains(err.Error(), test.name) {
				t.Fatalf("missing operation error=%v", err)
			}
		})
	}
}

func TestInvestigationStartValidatesSubjectBeforeMutation(t *testing.T) {
	handler := New(Config{})[asyncjob.TaskInvestigationAdvance]
	execution := executionForKey(investigationStartKey)
	execution.Task.IncidentID = 1
	execution.Task.SubjectID = 2
	result := handler.Handle(context.Background(), execution)
	if result.Disposition != asyncjob.DispositionDead || result.Mutate != nil {
		t.Fatalf("invalid start result=%+v", result)
	}
}

func TestInvestigationStartUsesFrozenV3DefaultBudgets(t *testing.T) {
	want := []int{
		8, 8, 10, 16_000, 20, 180_000, 40_000, 16 * 1024, 64 * 1024, 1, 5,
	}
	got := []int{
		defaultSemanticIterations, defaultToolCalls, defaultModelCalls,
		defaultModelTokens, defaultEvidenceItems, defaultRuntimeMillis,
		defaultToolTimeoutMillis, defaultEvidenceBytes, defaultCheckpointBytes,
		defaultStepRetries, defaultStepMaxAttempts,
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("budget[%d]=%d, want %d", index, got[index], want[index])
		}
	}
}

func executionForKey(key DispatchKey) asyncjob.Execution {
	queue, _ := asyncjob.QueueForTaskType(key.TaskType)
	return asyncjob.Execution{Task: asyncjob.Task{
		ID: 1, PublicID: "task-1", IncidentID: 1, CycleNo: 1,
		Queue: queue, Type: key.TaskType,
		SubjectType: key.SubjectType, SubjectID: 1, Transition: key.Transition,
		ExpectedSubjectVersion: 1, PayloadSchemaVersion: 1,
	}}
}
