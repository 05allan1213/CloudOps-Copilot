package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

type transientCancellationPollStore struct {
	workspaceRunnerStore
	calls   atomic.Uint32
	retried chan struct{}
}

func (s *transientCancellationPollStore) WorkspaceCancellationRequested(context.Context, WorkspaceLease) (bool, error) {
	call := s.calls.Add(1)
	if call == 1 {
		return false, context.DeadlineExceeded
	}
	if call == 2 {
		close(s.retried)
	}
	return false, nil
}

func TestWorkspaceLeaseMonitorRetriesTransientCancellationPollDeadline(t *testing.T) {
	store := &transientCancellationPollStore{retried: make(chan struct{})}
	runner := &WorkspaceRunner{config: WorkspaceRunnerConfig{
		Store: store, CancellationPoll: 5 * time.Millisecond,
		HeartbeatInterval: time.Second, LeaseDuration: 5 * time.Second,
	}}
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan struct{})
	go runner.maintainLease(ctx, cancel, WorkspaceLease{}, done)

	select {
	case <-store.retried:
		if cause := context.Cause(ctx); cause != nil {
			t.Fatalf("transient cancellation poll deadline cancelled Workspace: %v", cause)
		}
	case <-ctx.Done():
		t.Fatalf("transient cancellation poll deadline cancelled Workspace: %v", context.Cause(ctx))
	case <-time.After(time.Second):
		t.Fatal("cancellation poll was not retried")
	}

	cancel(nil)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("lease monitor did not stop after cancellation")
	}
}

func TestWorkspaceRuntimeExceededRequiresOwnedDeadline(t *testing.T) {
	if workspaceRuntimeExceeded(context.DeadlineExceeded, context.DeadlineExceeded) {
		t.Fatal("dependency deadline was classified as the Workspace runtime deadline")
	}
	if !workspaceRuntimeExceeded(errWorkspaceRuntimeExceeded, nil) {
		t.Fatal("owned Workspace runtime deadline was not classified")
	}
	if !workspaceRuntimeExceeded(nil, errors.Join(errors.New("execute Workspace"), errWorkspaceRuntimeExceeded)) {
		t.Fatal("wrapped Workspace runtime deadline was not classified")
	}
}

func TestWorkspaceModelOutcomeRequiresTwoCitedEvidenceSources(t *testing.T) {
	evidence := []EvidenceCitation{
		{EvidenceID: "evidence-kubernetes", Source: "kubernetes"},
		{EvidenceID: "evidence-prometheus", Source: "prometheus"},
		{EvidenceID: "evidence-prometheus-second", Source: "prometheus"},
	}

	tests := []struct {
		name   string
		answer string
		want   WorkspaceOutcome
	}{
		{name: "no citation", answer: "没有可追溯引用。", want: WorkspaceOutcomeInsufficient},
		{name: "one source", answer: "[Evidence: evidence-kubernetes]", want: WorkspaceOutcomeInsufficient},
		{name: "two citations from one source", answer: "evidence-prometheus evidence-prometheus-second", want: WorkspaceOutcomeInsufficient},
		{name: "two distinct current sources", answer: "evidence-kubernetes evidence-prometheus", want: WorkspaceOutcomeDiagnosed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome, uncertainty := workspaceModelOutcome(test.answer, evidence)
			if outcome != test.want {
				t.Fatalf("outcome=%q want=%q", outcome, test.want)
			}
			if outcome == WorkspaceOutcomeDiagnosed && uncertainty != "medium" {
				t.Fatalf("diagnosed uncertainty=%q want medium", uncertainty)
			}
			if outcome == WorkspaceOutcomeInsufficient && uncertainty != "high" {
				t.Fatalf("insufficient uncertainty=%q want high", uncertainty)
			}
		})
	}
}

func TestWorkspaceModelPromptIncludesImmutableSubjectAndEvidenceFacts(t *testing.T) {
	execution := WorkspaceExecutionContext{
		Run: WorkspaceRun{Objective: "调查当前 firing condition"},
		Snapshot: WorkspaceContextSnapshot{
			ID: "snapshot-1", SubjectType: WorkspaceSubjectAlert, ConfigurationRevisionID: "revision-1",
			Scope:     settings.OperationalScope{ClusterID: "cloudops-local", Environment: "local", Namespaces: []string{"demo"}},
			Resources: []telemetry.ResourceReference{{ID: "resource-1", Kind: "Deployment", Namespace: "demo", Name: "cloudops-scenario-fault"}},
			Filters:   json.RawMessage(`{"alert_id":"alert-1","alert_name":"CloudOpsScenarioRequiredEnvMissing","subject_summary":"Scenario workload is failing because REQUIRED_ENV is missing"}`),
			TimeRange: telemetry.TimeRange{From: time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC)},
		},
		AlertName: "CloudOpsScenarioRequiredEnvMissing",
	}
	run := WorkspaceRun{Evidence: []EvidenceCitation{{
		EvidenceID: "evidence-kubernetes", Source: "kubernetes", Summary: "Kubernetes returned bounded resources.",
		CollectedAt: time.Date(2026, 8, 2, 20, 30, 0, 0, time.UTC),
		Facts:       json.RawMessage(`{"facts":[{"kind":"Deployment","name":"cloudops-scenario-fault","status":"0/1 ready","health":"critical"}]}`),
	}}}

	prompt := workspaceModelPrompt(execution, run, workspaceGuidanceInput{})
	for _, expected := range []string{
		"CloudOpsScenarioRequiredEnvMissing", "REQUIRED_ENV", "cloudops-scenario-fault", "0/1 ready",
		"evidence-kubernetes", "absolute_time_window", "Subject Context",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("Workspace prompt does not contain %q:\n%s", expected, prompt)
		}
	}
}

func TestWorkspacePromptFactsKeepsValidBoundedJSON(t *testing.T) {
	raw := json.RawMessage(`{"facts":[{"name":"first"},{"name":"second"}]}`)
	result := workspacePromptFacts(raw, 45)
	if !json.Valid([]byte(result)) {
		t.Fatalf("bounded facts are not valid JSON: %s", result)
	}
	if !strings.Contains(result, `"name":"first"`) || !strings.Contains(result, `"truncated":true`) {
		t.Fatalf("bounded facts did not preserve the first fact and truncation marker: %s", result)
	}
}
