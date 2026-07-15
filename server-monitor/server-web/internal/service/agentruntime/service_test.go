package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"server-web/internal/agent"
)

func TestValidateConfigRejectsUnboundedWorkerID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WorkerID = strings.Repeat("w", 129)
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected oversized worker id rejection")
	}
}

func TestObservationChangesNextDecisionAndDiagnosisIsEvidenceBound(t *testing.T) {
	store := newMemoryStore()
	model := &scriptedModel{}
	tools := &scriptedTools{}
	service := newTestService(t, store, model, tools, testLimits())
	run, err := service.CreateRun(context.Background(), "incident-1", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	processed, err := service.ProcessNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	if got.Status != agent.RunCompleted {
		t.Fatalf("status=%s failure=%s", got.Status, got.FailureSummary)
	}
	if len(model.actions) != 2 || model.actions[0] != "alert.list_active" || model.actions[1] != "prom.query_range" {
		t.Fatalf("persisted observation did not affect the next decision: %v", model.actions)
	}
	if len(tools.calls) != 2 {
		t.Fatalf("one-shot function calling detected: tool calls=%v", tools.calls)
	}
	evidence, _ := store.ListEvidence(context.Background(), run.PublicID, 100)
	if len(evidence) != 2 || evidence[0].PublicID == "" || evidence[1].PublicID == "" {
		t.Fatalf("durable evidence=%+v", evidence)
	}
	var diagnosis agent.Diagnosis
	if err := json.Unmarshal(got.FinalDiagnosis, &diagnosis); err != nil {
		t.Fatal(err)
	}
	if len(diagnosis.ConfirmedFacts) != 1 || diagnosis.ConfirmedFacts[0].EvidenceIDs[0] != evidence[0].PublicID {
		t.Fatalf("diagnosis is not evidence-bound: %+v", diagnosis)
	}
	steps, _ := store.ListSteps(context.Background(), run.PublicID, 100)
	if len(steps) < 10 || got.CheckpointVersion < 10 {
		t.Fatalf("steps=%d checkpoint_version=%d", len(steps), got.CheckpointVersion)
	}
}

func TestConfirmedChangeEvidenceCanSupportReleaseDiagnosis(t *testing.T) {
	store := newMemoryStore()
	model := &changeAwareModel{confirmed: true}
	tools := &changeTools{confirmed: true}
	service := newTestService(t, store, model, tools, testLimits())
	run, _ := service.CreateRun(context.Background(), "incident-1", "key-change-confirmed")
	processed, err := service.ProcessNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	if got.Status != agent.RunCompleted {
		t.Fatalf("status=%s failure=%s", got.Status, got.FailureSummary)
	}
	var diagnosis agent.Diagnosis
	if err := json.Unmarshal(got.FinalDiagnosis, &diagnosis); err != nil {
		t.Fatal(err)
	}
	if diagnosis.Degraded || len(diagnosis.ConfirmedFacts) != 1 {
		t.Fatalf("diagnosis=%+v", diagnosis)
	}
	evidence, _ := store.ListEvidence(context.Background(), run.PublicID, 10)
	if len(evidence) != 1 || evidence[0].ToolName != "change.list_recent" || diagnosis.ConfirmedFacts[0].EvidenceIDs[0] != evidence[0].PublicID {
		t.Fatalf("evidence=%+v diagnosis=%+v", evidence, diagnosis)
	}
}

func TestExcludedForeignChangeDoesNotBecomeRootCause(t *testing.T) {
	store := newMemoryStore()
	model := &changeAwareModel{}
	tools := &changeTools{}
	service := newTestService(t, store, model, tools, testLimits())
	run, _ := service.CreateRun(context.Background(), "incident-1", "key-change-excluded")
	_, err := service.ProcessNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	var diagnosis agent.Diagnosis
	if err := json.Unmarshal(got.FinalDiagnosis, &diagnosis); err != nil {
		t.Fatal(err)
	}
	if got.Status != agent.RunCompleted || len(diagnosis.ConfirmedFacts) != 0 || len(diagnosis.Unknowns) == 0 {
		t.Fatalf("foreign change was promoted: status=%s diagnosis=%+v", got.Status, diagnosis)
	}
}

func TestMalformedDiagnosisGetsOneCorrectionThenDeterministicDegradedResult(t *testing.T) {
	store := newMemoryStore()
	model := &scriptedModel{alwaysMalformed: true}
	service := newTestService(t, store, model, &scriptedTools{}, testLimits())
	run, _ := service.CreateRun(context.Background(), "incident-1", "key-malformed")
	_, err := service.ProcessNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	if got.Status != agent.RunCompleted {
		t.Fatalf("status=%s failure=%s", got.Status, got.FailureSummary)
	}
	var diagnosis agent.Diagnosis
	_ = json.Unmarshal(got.FinalDiagnosis, &diagnosis)
	if !diagnosis.Degraded || model.diagnosisCalls != 2 {
		t.Fatalf("degraded=%v diagnosis_calls=%d diagnosis=%+v", diagnosis.Degraded, model.diagnosisCalls, diagnosis)
	}
}

func TestToolBudgetTerminatesRunBeforeSecondExecution(t *testing.T) {
	limits := testLimits()
	limits.MaxToolCalls = 1
	store := newMemoryStore()
	tools := &scriptedTools{}
	service := newTestService(t, store, &scriptedModel{}, tools, limits)
	run, _ := service.CreateRun(context.Background(), "incident-1", "key-budget")
	_, err := service.ProcessNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	if got.Status != agent.RunFailed || got.FailureCode != agent.ErrorBudgetExceeded || len(tools.calls) != 1 {
		t.Fatalf("run=%+v calls=%v", got, tools.calls)
	}
}

func TestSingleToolCanCompleteDiagnosis(t *testing.T) {
	store := newMemoryStore()
	model := &scriptedModel{coverageAfter: 1}
	tools := &scriptedTools{}
	service := newTestService(t, store, model, tools, testLimits())
	run, _ := service.CreateRun(context.Background(), "incident-1", "key-single")
	_, err := service.ProcessNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	if got.Status != agent.RunCompleted || len(tools.calls) != 1 {
		t.Fatalf("status=%s calls=%v", got.Status, tools.calls)
	}
}

func TestStepAndTokenBudgetsStopGraph(t *testing.T) {
	for name, mutate := range map[string]func(*agent.Limits){
		"step":  func(l *agent.Limits) { l.MaxSteps = 2 },
		"token": func(l *agent.Limits) { l.TokenBudget = 20 },
	} {
		t.Run(name, func(t *testing.T) {
			limits := testLimits()
			mutate(&limits)
			store := newMemoryStore()
			tools := &scriptedTools{}
			service := newTestService(t, store, &scriptedModel{}, tools, limits)
			run, _ := service.CreateRun(context.Background(), "incident-1", "key-"+name)
			_, err := service.ProcessNext(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got, _ := store.GetRun(context.Background(), run.PublicID)
			if got.Status != agent.RunFailed || got.FailureCode != agent.ErrorBudgetExceeded {
				t.Fatalf("run=%+v", got)
			}
		})
	}
}

func TestToolTimeoutRetriesOnceThenFails(t *testing.T) {
	store := newMemoryStore()
	tools := &timeoutTools{}
	service := newTestService(t, store, &scriptedModel{}, tools, testLimits())
	run, _ := service.CreateRun(context.Background(), "incident-1", "key-timeout")
	_, err := service.ProcessNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	if got.Status != agent.RunFailed || got.FailureCode != agent.ErrorTimeout || tools.calls != 2 {
		t.Fatalf("run=%+v calls=%d", got, tools.calls)
	}
}

func TestMalformedModelOutputIsNotRetried(t *testing.T) {
	store := newMemoryStore()
	model := &scriptedModel{malformedPlan: true}
	tools := &scriptedTools{}
	service := newTestService(t, store, model, tools, testLimits())
	run, _ := service.CreateRun(context.Background(), "incident-1", "key-malformed-plan")
	_, err := service.ProcessNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	if got.Status != agent.RunFailed || got.FailureCode != agent.ErrorMalformedModel || model.planCalls != 1 {
		t.Fatalf("run=%+v plan_calls=%d", got, model.planCalls)
	}
}

func TestTransientModelErrorRetriesOnceAndCompletedRunIsNotReclaimed(t *testing.T) {
	store := newMemoryStore()
	model := &scriptedModel{planFailures: 1}
	service := newTestService(t, store, model, &scriptedTools{}, testLimits())
	run, _ := service.CreateRun(context.Background(), "incident-1", "key-retry")
	_, err := service.ProcessNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	if got.Status != agent.RunCompleted || model.planCalls < 3 { // initial failure, retry, replan
		t.Fatalf("status=%s plan_calls=%d", got.Status, model.planCalls)
	}
	processed, err := service.ProcessNext(context.Background())
	if err != nil || processed {
		t.Fatalf("completed run reclaimed: processed=%v err=%v", processed, err)
	}
}

func TestCancellationIsDurable(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(t, store, &scriptedModel{}, &scriptedTools{}, testLimits())
	run, _ := service.CreateRun(context.Background(), "incident-1", "key-cancel")
	if err := service.Cancel(context.Background(), run.PublicID); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	if got.Status != agent.RunCancelled {
		t.Fatalf("status=%s", got.Status)
	}
}

func TestCancellationDuringExternalCallTerminatesAtSafePoint(t *testing.T) {
	store := newMemoryStore()
	tools := &cancellingTools{store: store}
	service := newTestService(t, store, &scriptedModel{}, tools, testLimits())
	run, _ := service.CreateRun(context.Background(), "incident-1", "key-cancel-running")
	tools.runID = run.PublicID
	processed, err := service.ProcessNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	if got.Status != agent.RunCancelled || tools.calls != 1 {
		t.Fatalf("run=%+v calls=%d", got, tools.calls)
	}
}

func TestModelCannotBypassReadOnlyAllowlist(t *testing.T) {
	store := newMemoryStore()
	model := &writeSelectingModel{scriptedModel: scriptedModel{}}
	tools := &scriptedTools{}
	service := newTestService(t, store, model, tools, testLimits())
	run, _ := service.CreateRun(context.Background(), "incident-1", "key-write-tool")
	_, err := service.ProcessNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	if got.Status != agent.RunFailed || got.FailureCode != agent.ErrorPermission || len(tools.calls) != 0 {
		t.Fatalf("allowlist bypass: run=%+v calls=%v", got, tools.calls)
	}
}

func TestExpiredLeaseResumesFromDurableCheckpoint(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(t, store, &scriptedModel{}, &scriptedTools{}, testLimits())
	run, _ := service.CreateRun(context.Background(), "incident-1", "key-resume")
	store.mu.Lock()
	stored := store.runs[run.PublicID]
	past := time.Now().Add(-time.Minute)
	started := time.Now().Add(-2 * time.Minute)
	deadline := time.Now().Add(time.Minute)
	state := agent.GraphState{SchemaVersion: 1, RunPublicID: run.PublicID, IncidentPublicID: "incident-1", NextNode: agent.NodeBuildObjective, Incident: store.incident, Limits: stored.Limits, StartedAt: started, DeadlineAt: deadline, RowVersion: stored.RowVersion}
	stored.Checkpoint, _ = json.Marshal(state)
	stored.Status, stored.LeaseOwner, stored.LeaseExpiresAt, stored.StartedAt, stored.DeadlineAt = agent.RunRunning, "crashed-worker", &past, &started, &deadline
	store.mu.Unlock()
	processed, err := service.ProcessNext(context.Background())
	if err != nil || !processed {
		t.Fatalf("resume processed=%v err=%v", processed, err)
	}
	got, _ := store.GetRun(context.Background(), run.PublicID)
	if got.Status != agent.RunCompleted {
		t.Fatalf("resumed status=%s failure=%s", got.Status, got.FailureSummary)
	}
}

func newTestService(t *testing.T, store *memoryStore, model agent.Model, tools agent.ToolExecutor, limits agent.Limits) *Service {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled, cfg.WorkerID, cfg.Limits = true, "worker-a", limits
	cfg.LeaseDuration, cfg.HeartbeatPeriod = time.Minute, 20*time.Second
	service, err := New(context.Background(), store, model, tools, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func testLimits() agent.Limits {
	return agent.Limits{MaxSteps: 12, MaxToolCalls: 6, MaxModelCalls: 8, TokenBudget: 12000, MaxEvidenceItems: 12, MaxRuntime: time.Minute, ToolTimeout: time.Second, MaxEvidenceBytes: 4096, MaxCheckpointSize: 32768, MaxStepRetries: 1}
}

type scriptedModel struct {
	mu              sync.Mutex
	actions         []string
	planCalls       int
	planFailures    int
	diagnosisCalls  int
	alwaysMalformed bool
	malformedPlan   bool
	coverageAfter   int
}

type writeSelectingModel struct{ scriptedModel }

type changeAwareModel struct{ confirmed bool }

func (m *changeAwareModel) Plan(context.Context, agent.IncidentContext, string) (agent.Plan, agent.ModelUsage, error) {
	return agent.Plan{Summary: "inspect deterministically correlated changes", Questions: []string{"was a matching revision deployed"}}, agent.ModelUsage{InputTokens: 4, OutputTokens: 4}, nil
}

func (m *changeAwareModel) SelectAction(context.Context, agent.GraphState, []string) (agent.Action, agent.ModelUsage, error) {
	return agent.Action{Tool: "change.list_recent", Arguments: json.RawMessage(`{"incident_id":"incident-1"}`), Reason: "read persisted change context"}, agent.ModelUsage{InputTokens: 4, OutputTokens: 4}, nil
}

func (m *changeAwareModel) EvaluateCoverage(_ context.Context, state agent.GraphState) (agent.Coverage, agent.ModelUsage, error) {
	return agent.Coverage{Sufficient: len(state.Observations) == 1, Reason: "one bounded change observation"}, agent.ModelUsage{InputTokens: 4, OutputTokens: 4}, nil
}

func (m *changeAwareModel) Diagnose(_ context.Context, state agent.GraphState) (agent.Diagnosis, agent.ModelUsage, error) {
	id := "unknown"
	if len(state.EvidenceIDs) > 0 {
		id = state.EvidenceIDs[0]
	}
	if !m.confirmed {
		return agent.Diagnosis{Summary: "No deployed change was confirmed for this service.", Unknowns: []string{"The foreign pull request was excluded by service and revision matching."}, Confidence: .2}, agent.ModelUsage{InputTokens: 4, OutputTokens: 4}, nil
	}
	return agent.Diagnosis{Summary: "A matching deployed revision overlaps the incident window.", Confidence: .9, ConfirmedFacts: []agent.Claim{{Statement: "The deployed commit is a confirmed release candidate.", EvidenceIDs: []string{id}, Strong: true}}, RecommendedNextActions: []string{"Review the read-only change evidence."}}, agent.ModelUsage{InputTokens: 4, OutputTokens: 4}, nil
}

type changeTools struct{ confirmed bool }

func (t *changeTools) AllowedTools() []string { return []string{"change.list_recent"} }

func (t *changeTools) Execute(_ context.Context, name string, _ json.RawMessage, _ time.Duration, _ int) (agent.ToolResult, error) {
	facts := json.RawMessage(`{"candidates":[{"status":"excluded","category":"excluded","reason":"foreign service and revision"}]}`)
	if t.confirmed {
		facts = json.RawMessage(`{"candidates":[{"status":"matched","category":"confirmed_match","revision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}],"image_resolution":{"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","revision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","source":"https://github.com/acme/app","version":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","status":"confirmed","valid":true,"truncated":false,"degraded":false,"registry_metadata":{"registry_id":"registry:abcdef123456","repository":"acme/app","manifest_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","config_digest":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","revision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","source":"https://github.com/acme/app","version":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","integrity_status":"verified","result_hash":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","valid":true,"truncated":false,"degraded":false,"redaction":{"auth_material_omitted":true,"responses_omitted":true,"policy":"registry_metadata_bounded"}}}}`)
	}
	return agent.ToolResult{Summary: name + " observation", Facts: facts, ResultHash: "change-hash", Valid: true}, nil
}

func (m *writeSelectingModel) SelectAction(context.Context, agent.GraphState, []string) (agent.Action, agent.ModelUsage, error) {
	return agent.Action{Tool: "k8s.delete_pod", Arguments: json.RawMessage(`{"name":"api"}`), Reason: "attempt write"}, agent.ModelUsage{InputTokens: 1, OutputTokens: 1}, nil
}

func (m *scriptedModel) Plan(context.Context, agent.IncidentContext, string) (agent.Plan, agent.ModelUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.planCalls++
	if m.malformedPlan {
		return agent.Plan{}, agent.ModelUsage{}, agent.NewRuntimeError(agent.ErrorMalformedModel, "invalid plan schema", agent.ErrInvalidArgument)
	}
	if m.planFailures > 0 {
		m.planFailures--
		return agent.Plan{}, agent.ModelUsage{}, agent.NewRuntimeError(agent.ErrorModelUnavailable, "temporary model outage", errors.New("outage"))
	}
	return agent.Plan{Summary: "inspect alert then metric", Questions: []string{"is alert firing", "does metric corroborate"}}, agent.ModelUsage{InputTokens: 10, OutputTokens: 5}, nil
}

func (m *scriptedModel) SelectAction(_ context.Context, state agent.GraphState, _ []string) (agent.Action, agent.ModelUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name, args := "alert.list_active", json.RawMessage(`{}`)
	if len(state.Observations) > 0 {
		name, args = "prom.query_range", json.RawMessage(`{"query":"up == 0"}`)
	}
	m.actions = append(m.actions, name)
	return agent.Action{Tool: name, Arguments: args, Reason: fmt.Sprintf("observations=%d", len(state.Observations))}, agent.ModelUsage{InputTokens: 10, OutputTokens: 5}, nil
}

func (m *scriptedModel) EvaluateCoverage(_ context.Context, state agent.GraphState) (agent.Coverage, agent.ModelUsage, error) {
	threshold := m.coverageAfter
	if threshold == 0 {
		threshold = 2
	}
	return agent.Coverage{Sufficient: len(state.Observations) >= threshold, Reason: fmt.Sprintf("observations=%d", len(state.Observations))}, agent.ModelUsage{InputTokens: 10, OutputTokens: 5}, nil
}

func (m *scriptedModel) Diagnose(_ context.Context, state agent.GraphState) (agent.Diagnosis, agent.ModelUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.diagnosisCalls++
	evidenceID := "unknown"
	if !m.alwaysMalformed && len(state.EvidenceIDs) > 0 {
		evidenceID = state.EvidenceIDs[0]
	}
	return agent.Diagnosis{Summary: "Metric corroborates the active alert.", Confidence: .8, ConfirmedFacts: []agent.Claim{{Statement: "alert and metric corroborate", EvidenceIDs: []string{evidenceID}, Strong: true}}, RecommendedNextActions: []string{"Review the evidence dashboard."}}, agent.ModelUsage{InputTokens: 10, OutputTokens: 5}, nil
}

type scriptedTools struct {
	mu    sync.Mutex
	calls []string
}

type timeoutTools struct{ calls int }

func (t *timeoutTools) AllowedTools() []string {
	return []string{"alert.list_active", "prom.query_range"}
}

type cancellingTools struct {
	store *memoryStore
	runID string
	calls int
}

func (t *cancellingTools) AllowedTools() []string {
	return []string{"alert.list_active", "prom.query_range"}
}
func (t *cancellingTools) Execute(_ context.Context, name string, _ json.RawMessage, _ time.Duration, _ int) (agent.ToolResult, error) {
	t.calls++
	_ = t.store.RequestCancel(context.Background(), t.runID, time.Now().UTC())
	facts := json.RawMessage(`{"cancelled_after_read":true}`)
	return agent.ToolResult{Summary: name, Facts: facts, ResultHash: "cancel-hash", Valid: true}, nil
}
func (t *timeoutTools) Execute(context.Context, string, json.RawMessage, time.Duration, int) (agent.ToolResult, error) {
	t.calls++
	return agent.ToolResult{}, agent.NewRuntimeError(agent.ErrorTimeout, "tool deadline", context.DeadlineExceeded)
}

func (t *scriptedTools) AllowedTools() []string {
	return []string{"alert.list_active", "prom.query_range"}
}
func (t *scriptedTools) Execute(_ context.Context, name string, _ json.RawMessage, _ time.Duration, _ int) (agent.ToolResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, name)
	facts := json.RawMessage(fmt.Sprintf(`{"tool":%q,"call":%d}`, name, len(t.calls)))
	return agent.ToolResult{Summary: name + " observation", Facts: facts, ResultHash: fmt.Sprintf("hash-%d", len(t.calls)), Valid: true}, nil
}

type memoryStore struct {
	mu       sync.Mutex
	runs     map[string]*agent.Run
	steps    map[string][]agent.Step
	evidence map[string][]agent.EvidenceRecord
	nextID   uint64
	incident agent.IncidentContext
}

func newMemoryStore() *memoryStore {
	return &memoryStore{runs: map[string]*agent.Run{}, steps: map[string][]agent.Step{}, evidence: map[string][]agent.EvidenceRecord{}, incident: agent.IncidentContext{PublicID: "incident-1", Status: "CORRELATING", Severity: "critical", Cluster: "cluster-a", Namespace: "default", ServiceName: "api", TargetKind: "Deployment", TargetName: "api", Summary: "API unavailable", FirstSeenAt: time.Now().Add(-time.Minute), LastSeenAt: time.Now()}}
}

func (s *memoryStore) CreateRun(_ context.Context, request agent.CreateRunRequest) (*agent.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range s.runs {
		if run.IdempotencyKey == request.IdempotencyKey {
			return cloneRun(run), nil
		}
		if agent.IsActiveRun(run.Status) {
			return nil, agent.ErrConflict
		}
	}
	s.nextID++
	id := fmt.Sprintf("run-%d", s.nextID)
	run := &agent.Run{ID: s.nextID, PublicID: id, IncidentID: 1, IncidentPublicID: request.IncidentPublicID, IdempotencyKey: request.IdempotencyKey, Attempt: int(s.nextID), Status: agent.RunPending, Model: request.Model, PromptVersion: request.PromptVersion, Limits: request.Limits, Checkpoint: append([]byte(nil), request.Checkpoint...), CheckpointSchema: 1, RowVersion: 1, CreatedAt: request.At, UpdatedAt: request.At}
	s.runs[id] = run
	return cloneRun(run), nil
}

func (s *memoryStore) GetRun(_ context.Context, id string) (*agent.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[id]
	if run == nil {
		return nil, agent.ErrNotFound
	}
	return cloneRun(run), nil
}

func (s *memoryStore) ListRunsByIncident(context.Context, string, int, int) (agent.Page, error) {
	return agent.Page{}, nil
}
func (s *memoryStore) ListSteps(_ context.Context, id string, _ int) ([]agent.Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]agent.Step(nil), s.steps[id]...), nil
}
func (s *memoryStore) ListEvidence(_ context.Context, id string, _ int) ([]agent.EvidenceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]agent.EvidenceRecord(nil), s.evidence[id]...), nil
}
func (s *memoryStore) RequestCancel(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[id]
	if run == nil {
		return agent.ErrNotFound
	}
	run.CancelRequestedAt, run.RowVersion = &at, run.RowVersion+1
	if run.Status == agent.RunPending {
		run.Status, run.FinishedAt = agent.RunCancelled, &at
	}
	return nil
}
func (s *memoryStore) ClaimNext(_ context.Context, owner string, at time.Time, lease time.Duration) (*agent.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range s.runs {
		if run.Status == agent.RunPending || (run.Status == agent.RunRunning && run.LeaseExpiresAt != nil && run.LeaseExpiresAt.Before(at)) {
			expires, deadline := at.Add(lease), at.Add(run.Limits.MaxRuntime)
			run.Status, run.LeaseOwner, run.LeaseExpiresAt, run.HeartbeatAt, run.RowVersion = agent.RunRunning, owner, &expires, &at, run.RowVersion+1
			if run.StartedAt == nil {
				run.StartedAt, run.DeadlineAt = &at, &deadline
			}
			return cloneRun(run), nil
		}
	}
	return nil, agent.ErrNotFound
}
func (s *memoryStore) Heartbeat(context.Context, uint64, string, time.Time, time.Duration) error {
	return nil
}
func (s *memoryStore) BeginStep(_ context.Context, run *agent.Run, start agent.StepStart) (*agent.Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.runs[run.PublicID]
	if stored.RowVersion != run.RowVersion || stored.LeaseOwner != run.LeaseOwner {
		return nil, agent.ErrLeaseLost
	}
	if start.Budgeted && stored.Usage.Steps >= stored.Limits.MaxSteps {
		return nil, agent.ErrBudgetExceeded
	}
	s.nextID++
	started := start.At
	step := agent.Step{ID: s.nextID, PublicID: fmt.Sprintf("step-%d", s.nextID), RunID: run.ID, Sequence: len(s.steps[run.PublicID]) + 1, Node: start.Node, Status: agent.StepRunning, ShortReason: start.Reason, SelectedTool: start.SelectedTool, Arguments: start.Arguments, ArgumentsHash: start.ArgumentsHash, StartedAt: &started, CreatedAt: started}
	if start.Budgeted {
		stored.Usage.Steps++
	}
	stored.RowVersion++
	run.Usage, run.RowVersion = stored.Usage, stored.RowVersion
	s.steps[run.PublicID] = append(s.steps[run.PublicID], step)
	return &step, nil
}
func (s *memoryStore) FinishStep(_ context.Context, run *agent.Run, step *agent.Step, finish agent.StepFinish) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finish(run, step, finish)
}
func (s *memoryStore) finish(run *agent.Run, step *agent.Step, finish agent.StepFinish) error {
	stored := s.runs[run.PublicID]
	if stored.RowVersion != run.RowVersion || stored.LeaseOwner != run.LeaseOwner {
		return agent.ErrLeaseLost
	}
	if err := stored.Usage.CanCharge(finish.Usage, stored.Limits); err != nil {
		return err
	}
	items := s.steps[run.PublicID]
	for index := range items {
		if items[index].ID == step.ID {
			items[index].Status, items[index].ResultSummary, items[index].EvidencePublicID, items[index].ErrorCode, items[index].FinishedAt = finish.Status, finish.ResultSummary, finish.EvidencePublicID, finish.ErrorCode, &finish.At
		}
	}
	s.steps[run.PublicID] = items
	stored.Usage.Charge(finish.Usage)
	stored.Checkpoint, stored.CheckpointHash = append([]byte(nil), finish.Checkpoint...), finish.CheckpointHash
	stored.CheckpointVersion++
	stored.RowVersion++
	run.Usage, run.RowVersion, run.CheckpointVersion = stored.Usage, stored.RowVersion, stored.CheckpointVersion
	return nil
}
func (s *memoryStore) PersistEvidence(_ context.Context, run *agent.Run, step *agent.Step, item agent.EvidenceRecord, finish agent.StepFinish) (*agent.EvidenceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.evidence[run.PublicID] {
		if existing.IdempotencyKey == item.IdempotencyKey {
			finish.Usage.Evidence = 0
			finish.EvidencePublicID = existing.PublicID
			return &existing, s.finish(run, step, finish)
		}
	}
	s.nextID++
	item.PublicID = fmt.Sprintf("evidence-%d", s.nextID)
	s.evidence[run.PublicID] = append(s.evidence[run.PublicID], item)
	finish.Usage.Evidence, finish.EvidencePublicID = 1, item.PublicID
	return &item, s.finish(run, step, finish)
}
func (s *memoryStore) FinishRun(_ context.Context, run *agent.Run, status agent.RunStatus, diagnosis agent.Diagnosis, code agent.ErrorCode, summary string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.runs[run.PublicID]
	if stored.RowVersion != run.RowVersion {
		return agent.ErrLeaseLost
	}
	data, _ := json.Marshal(diagnosis)
	stored.Status, stored.FinalDiagnosis, stored.FailureCode, stored.FailureSummary, stored.FinishedAt, stored.RowVersion = status, data, code, summary, &at, stored.RowVersion+1
	run.Status, run.RowVersion = status, stored.RowVersion
	return nil
}
func (s *memoryStore) LoadIncident(context.Context, uint64) (agent.IncidentContext, error) {
	return s.incident, nil
}

func cloneRun(run *agent.Run) *agent.Run {
	copy := *run
	copy.Checkpoint = append([]byte(nil), run.Checkpoint...)
	copy.FinalDiagnosis = append([]byte(nil), run.FinalDiagnosis...)
	return &copy
}
