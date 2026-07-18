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

func TestRegistryDispatchesInjectedOperationsAndFailsClosed(t *testing.T) {
	called := false
	handlers := New(Config{DeliveryObserve: func(context.Context, asyncjob.Execution) asyncjob.Result {
		called = true
		return asyncjob.Succeeded(nil)
	}})
	result := handlers[asyncjob.TaskDeliveryObserve].Handle(context.Background(), asyncjob.Execution{})
	if !called || result.Disposition != asyncjob.DispositionSucceeded {
		t.Fatalf("injected handler result=%+v called=%v", result, called)
	}
	result = handlers[asyncjob.TaskChangeEnsurePR].Handle(context.Background(), asyncjob.Execution{})
	if !errors.Is(result.Validate(), asyncjob.ErrInvalidResult) || result.Mutate != nil {
		t.Fatalf("non-migrated handler fabricated a durable result=%+v", result)
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
	result := handler.Handle(context.Background(), asyncjob.Execution{Task: asyncjob.Task{
		IncidentID: 1, SubjectID: 2, SubjectType: "incident", Transition: "investigation.start",
		CycleNo: 1, ExpectedSubjectVersion: 1,
	}})
	if result.Disposition != asyncjob.DispositionDead || result.Mutate != nil {
		t.Fatalf("invalid start result=%+v", result)
	}
}
