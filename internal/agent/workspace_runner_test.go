package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

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

func TestWorkspaceScenarioLogFactsRequireExactScenarioIdentity(t *testing.T) {
	const scenarioID = "scenario-20260803104650-1133f132"
	execution := WorkspaceExecutionContext{Snapshot: WorkspaceContextSnapshot{
		Scope: settings.OperationalScope{ClusterID: "cloudops-local", Environment: "local", Namespaces: []string{"demo"}},
		Resources: []telemetry.ResourceReference{{
			ID: "resource-1", Kind: "Deployment", Namespace: "demo", Name: "cloudops-scenario-fault",
		}},
		Filters: json.RawMessage(`{"scenario_id":"` + scenarioID + `"}`),
	}}
	resource := execution.Snapshot.Resources[0]
	entry := telemetry.LogEntry{Attributes: map[string]string{
		"scenario_id": scenarioID,
		"reason":      "required_env_missing",
	}}
	facts := workspaceScenarioLogFacts(execution, resource, []telemetry.LogEntry{entry})
	if len(facts) != 1 || facts[0].Type != "log.required_env_missing" || facts[0].Attributes["scenario_id"] != scenarioID {
		t.Fatalf("exact Scenario log fact=%+v", facts)
	}
	entry.Attributes["scenario_id"] = "scenario-20260803103759-545fe4e0"
	if facts = workspaceScenarioLogFacts(execution, resource, []telemetry.LogEntry{entry}); len(facts) != 0 {
		t.Fatalf("cross-Scenario log produced facts=%+v", facts)
	}
}

func TestWorkspaceScenarioLogRangeStaysCompleteWithinProviderBound(t *testing.T) {
	from := time.Date(2026, 8, 3, 10, 42, 0, 0, time.UTC)
	to := time.Date(2026, 8, 3, 10, 56, 0, 0, time.UTC)
	if got := workspaceBoundedLogFrom(from, to, "scenario-20260803104650-1133f132"); !got.Equal(to.Add(-30 * time.Second)) {
		t.Fatalf("focused Scenario log from=%s", got)
	}
	if got := workspaceBoundedLogFrom(from, to, ""); !got.Equal(from) {
		t.Fatalf("ordinary workload log from=%s want=%s", got, from)
	}
	shortFrom := to.Add(-20 * time.Second)
	if got := workspaceBoundedLogFrom(shortFrom, to, "scenario-20260803104650-1133f132"); !got.Equal(shortFrom) {
		t.Fatalf("short Scenario log window expanded from=%s want=%s", got, shortFrom)
	}
}

func TestWorkspaceCancellationCompletionPreservesModelUsage(t *testing.T) {
	usage := Usage{ModelCalls: 2, InputTokens: 321, OutputTokens: 123}
	err := workspaceErrorWithExecutionUsage(ErrCancelled, "llm", "deepseek-v4-flash", usage)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("wrapped cancellation no longer matches ErrCancelled: %v", err)
	}
	completion := workspaceCompletionWithExecutionUsage(WorkspaceCompletion{
		Outcome: WorkspaceOutcomeCancelled, Uncertainty: "unknown", Answer: "cancelled",
	}, err)
	if completion.ModelProvider != "llm" || completion.ActualModel != "deepseek-v4-flash" ||
		completion.ModelCalls != 2 || completion.InputTokens != 321 || completion.OutputTokens != 123 {
		t.Fatalf("cancel completion lost model usage: %+v", completion)
	}
}

func TestWorkspaceModelUsageReservationFailsClosedAtImmutableBudget(t *testing.T) {
	limits := WorkspaceExecutionLimits{MaxModelCalls: 3, TokenBudget: 1000}
	current := Usage{ModelCalls: 1, InputTokens: 200, OutputTokens: 100}
	if err := workspaceReserveModelUsage(current, Usage{ModelCalls: 2, InputTokens: 500, OutputTokens: 200}, limits); err != nil {
		t.Fatalf("exact remaining budget was rejected: %v", err)
	}
	if err := workspaceReserveModelUsage(current, Usage{ModelCalls: 3, InputTokens: 1, OutputTokens: 1}, limits); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("model-call overrun error=%v", err)
	}
	if err := workspaceReserveModelUsage(current, Usage{ModelCalls: 1, InputTokens: 701, OutputTokens: 0}, limits); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("token overrun error=%v", err)
	}
}

func TestWorkspaceModelCompletionNeverPromotesPlainAnswerToDiagnosis(t *testing.T) {
	evidence := []EvidenceCitation{
		{EvidenceID: "evidence-kubernetes", Source: "kubernetes"},
		{EvidenceID: "evidence-prometheus", Source: "prometheus"},
		{EvidenceID: "evidence-prometheus-second", Source: "prometheus"},
	}

	tests := []struct {
		name   string
		answer string
	}{
		{name: "no citation", answer: "没有可追溯引用。"},
		{name: "one source", answer: "[Evidence: evidence-kubernetes]"},
		{name: "two citations from one source", answer: "evidence-prometheus evidence-prometheus-second"},
		{name: "two distinct current sources", answer: "evidence-kubernetes evidence-prometheus"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			completion := workspaceModelCompletion(WorkspaceModelResponse{Answer: test.answer}, WorkspaceRun{Evidence: evidence}, "llm", "deepseek-v4-flash")
			if completion.Outcome != WorkspaceOutcomeInsufficient || completion.Uncertainty != "high" {
				t.Fatalf("plain answer outcome=%q uncertainty=%q", completion.Outcome, completion.Uncertainty)
			}
			if completion.Diagnosis != nil {
				t.Fatalf("plain answer unexpectedly produced Diagnosis: %+v", completion.Diagnosis)
			}
		})
	}
}

func TestWorkspaceModelCompletionFailsClosedOnEmptyAnswer(t *testing.T) {
	projected := WorkspaceRun{Evidence: []EvidenceCitation{
		{EvidenceID: "evidence-kubernetes", Source: "kubernetes"},
		{EvidenceID: "evidence-prometheus", Source: "prometheus"},
	}}
	completion := workspaceModelCompletion(WorkspaceModelResponse{
		InputTokens: 1871, OutputTokens: 800,
	}, projected, "llm", "deepseek-v4-flash")

	if completion.Outcome != WorkspaceOutcomeInsufficient || completion.Uncertainty != "high" {
		t.Fatalf("empty model completion outcome=%q uncertainty=%q", completion.Outcome, completion.Uncertainty)
	}
	if completion.FailureCode != workspaceModelUnavailableCode || completion.FailureSummary == "" {
		t.Fatalf("empty model completion failure=%q summary=%q", completion.FailureCode, completion.FailureSummary)
	}
	if completion.InputTokens != 1871 || completion.OutputTokens != 800 ||
		completion.ModelProvider != "llm" || completion.ActualModel != "deepseek-v4-flash" {
		t.Fatalf("empty model completion provenance=%+v", completion)
	}
	if !strings.Contains(completion.Answer, "没有将空响应当作诊断") {
		t.Fatalf("empty model completion answer=%q", completion.Answer)
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
	if len(prompt) > 9000 {
		t.Fatalf("Workspace prompt length=%d exceeds 9000 bytes", len(prompt))
	}
	for _, expected := range []string{
		"CloudOpsScenarioRequiredEnvMissing", "REQUIRED_ENV", "cloudops-scenario-fault", "0/1 ready",
		"evidence-kubernetes", "absolute_time_window", "Subject Context",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("Workspace prompt does not contain %q:\n%s", expected, prompt)
		}
	}
}

func TestWorkspacePromptBoundPreservesUTF8(t *testing.T) {
	result := workspacePromptBound("调查 cloudops-scenario-fault", 5)
	if !utf8.ValidString(result) || result != "调" {
		t.Fatalf("bounded prompt=%q is not the expected valid UTF-8 prefix", result)
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
