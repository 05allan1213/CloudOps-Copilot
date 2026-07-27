package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent/runbook"
	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

const (
	workspaceModelDisabledCode    = "MODEL_PROVIDER_DISABLED"
	workspaceModelUnavailableCode = "MODEL_PROVIDER_UNAVAILABLE"
)

type workspaceRunnerStore interface {
	WorkspaceTaskReady(context.Context) error
	ClaimWorkspaceTask(context.Context, string, time.Duration) (WorkspaceLease, bool, error)
	HeartbeatWorkspaceTask(context.Context, WorkspaceLease, time.Duration) error
	WorkspaceCancellationRequested(context.Context, WorkspaceLease) (bool, error)
	WorkspaceExecution(context.Context, WorkspaceLease) (WorkspaceExecutionContext, error)
	CiteWorkspaceSnapshotEvidence(context.Context, WorkspaceLease, string) error
	StartWorkspaceTool(context.Context, WorkspaceLease, string, json.RawMessage) (workspaceStepLease, error)
	CompleteWorkspaceTool(context.Context, WorkspaceLease, workspaceStepLease, WorkspaceToolObservation) (string, error)
	FailWorkspaceTool(context.Context, WorkspaceLease, workspaceStepLease, string, string) error
	ApplicableKnowledge(context.Context, string, int) ([]KnowledgeRevision, error)
	CiteWorkspaceKnowledge(context.Context, WorkspaceLease, string) error
	RunbookGuidance(context.Context) ([]RunbookGuidance, error)
	CiteWorkspaceRunbook(context.Context, WorkspaceLease, string, string) error
	AppendWorkspaceAnswerDelta(context.Context, WorkspaceLease, string) error
	WorkspaceRun(context.Context, string) (WorkspaceRun, error)
	CompleteWorkspaceTask(context.Context, WorkspaceLease, WorkspaceCompletion) error
	RetryWorkspaceTask(context.Context, WorkspaceLease, string, string, time.Duration) error
}

type workspaceRevisionSource interface {
	Revision(context.Context, string) (settings.Revision, error)
}

type WorkspaceRunnerConfig struct {
	Owner             string
	Store             workspaceRunnerStore
	Revisions         workspaceRevisionSource
	Kubernetes        infrastructure.Reader
	Metrics           observability.Provider
	Telemetry         telemetry.Provider
	Models            WorkspaceModelFactory
	MaxInFlight       int
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	CancellationPoll  time.Duration
	RetryDelay        time.Duration
}

type WorkspaceRunner struct {
	config WorkspaceRunnerConfig

	started       atomic.Bool
	claimsStopped atomic.Bool
	claimCancel   context.CancelFunc
	runCancel     context.CancelFunc
	semaphore     chan struct{}
	wait          sync.WaitGroup
}

func NewWorkspaceRunner(config WorkspaceRunnerConfig) (*WorkspaceRunner, error) {
	if strings.TrimSpace(config.Owner) == "" || config.Store == nil || config.Revisions == nil ||
		config.Kubernetes == nil || config.Metrics == nil || config.Telemetry == nil || config.Models == nil {
		return nil, errors.New("agent workspace runner dependencies are incomplete")
	}
	if config.MaxInFlight <= 0 {
		config.MaxInFlight = 2
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 250 * time.Millisecond
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 90 * time.Second
	}
	if config.HeartbeatInterval <= 0 || config.HeartbeatInterval > config.LeaseDuration/3 {
		return nil, errors.New("agent workspace heartbeat must be positive and no greater than one third of the lease")
	}
	if config.CancellationPoll <= 0 {
		config.CancellationPoll = time.Second
	}
	if config.CancellationPoll > config.HeartbeatInterval {
		config.CancellationPoll = config.HeartbeatInterval
	}
	if config.RetryDelay < 0 {
		return nil, errors.New("agent workspace retry delay cannot be negative")
	}
	return &WorkspaceRunner{config: config, semaphore: make(chan struct{}, config.MaxInFlight)}, nil
}

func (r *WorkspaceRunner) Start(ctx context.Context) error {
	if r == nil || !r.started.CompareAndSwap(false, true) {
		return errors.New("agent workspace runner is already started")
	}
	if err := r.config.Store.WorkspaceTaskReady(ctx); err != nil {
		r.started.Store(false)
		return err
	}
	claimCtx, claimCancel := context.WithCancel(context.Background())
	runCtx, runCancel := context.WithCancel(context.Background())
	r.claimCancel, r.runCancel = claimCancel, runCancel
	r.wait.Add(1)
	go r.claimLoop(claimCtx, runCtx)
	go func() {
		select {
		case <-ctx.Done():
			r.StopClaims()
		case <-claimCtx.Done():
		}
	}()
	return nil
}

func (r *WorkspaceRunner) StopClaims() {
	if r == nil || !r.claimsStopped.CompareAndSwap(false, true) {
		return
	}
	if r.claimCancel != nil {
		r.claimCancel()
	}
}

func (r *WorkspaceRunner) Shutdown(ctx context.Context) error {
	if r == nil || !r.started.Load() {
		return nil
	}
	r.StopClaims()
	done := make(chan struct{})
	go func() {
		r.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		if r.runCancel != nil {
			r.runCancel()
		}
		return ctx.Err()
	}
}

func (r *WorkspaceRunner) Ready(ctx context.Context) error {
	if r == nil || !r.started.Load() {
		return errors.New("agent workspace runner is not started")
	}
	if r.claimsStopped.Load() {
		return errors.New("agent workspace claims are stopped")
	}
	return r.config.Store.WorkspaceTaskReady(ctx)
}

func (r *WorkspaceRunner) claimLoop(claimCtx, runCtx context.Context) {
	defer r.wait.Done()
	ticker := time.NewTicker(r.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-claimCtx.Done():
			return
		case <-ticker.C:
		}
		select {
		case r.semaphore <- struct{}{}:
		case <-claimCtx.Done():
			return
		}
		lease, found, err := r.config.Store.ClaimWorkspaceTask(claimCtx, r.config.Owner, r.config.LeaseDuration)
		if err != nil || !found {
			<-r.semaphore
			continue
		}
		r.wait.Add(1)
		go func() {
			defer r.wait.Done()
			defer func() { <-r.semaphore }()
			r.executeLease(runCtx, lease)
		}()
	}
}

func (r *WorkspaceRunner) executeLease(base context.Context, lease WorkspaceLease) {
	ctx, cancel := context.WithCancelCause(base)
	defer cancel(nil)
	heartbeatDone := make(chan struct{})
	go r.maintainLease(ctx, cancel, lease, heartbeatDone)

	execution, err := r.config.Store.WorkspaceExecution(ctx, lease)
	if err == nil {
		maxRuntime := execution.Limits.MaxRuntime
		if maxRuntime <= 0 || maxRuntime > 5*time.Minute {
			maxRuntime = 2 * time.Minute
		}
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithTimeout(ctx, maxRuntime)
		defer deadlineCancel()
		err = r.executeWorkspace(ctx, lease, execution)
	}
	cancel(nil)
	<-heartbeatDone

	cause := context.Cause(ctx)
	if errors.Is(cause, ErrLeaseLost) || errors.Is(err, ErrLeaseLost) {
		return
	}
	auditCtx, auditCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer auditCancel()
	if errors.Is(cause, ErrCancelled) || errors.Is(err, ErrCancelled) {
		_ = r.config.Store.CompleteWorkspaceTask(auditCtx, lease, WorkspaceCompletion{
			Outcome: WorkspaceOutcomeCancelled, Uncertainty: "unknown", Answer: "本次 Agent 工作已取消。",
			FailureCode: "OWNER_CANCELLED", FailureSummary: "Owner requested cancellation",
		})
		return
	}
	if errors.Is(cause, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		_ = r.config.Store.CompleteWorkspaceTask(auditCtx, lease, WorkspaceCompletion{
			Outcome: WorkspaceOutcomeFailed, Uncertainty: "high", Answer: "Agent 工作超过了本次运行时限。",
			FailureCode: "WORKSPACE_RUNTIME_EXCEEDED", FailureSummary: "Agent Workspace runtime deadline exceeded",
		})
		return
	}
	if err == nil {
		return
	}
	if lease.Attempt < lease.MaxAttempts {
		_ = r.config.Store.RetryWorkspaceTask(auditCtx, lease, "WORKSPACE_RUNTIME_ERROR", err.Error(), r.config.RetryDelay)
		return
	}
	_ = r.config.Store.CompleteWorkspaceTask(auditCtx, lease, WorkspaceCompletion{
		Outcome: WorkspaceOutcomeFailed, Uncertainty: "high", Answer: "Agent 工作因内部依赖错误而失败。",
		FailureCode: "WORKSPACE_RUNTIME_ERROR", FailureSummary: err.Error(),
	})
}

func (r *WorkspaceRunner) maintainLease(ctx context.Context, cancel context.CancelCauseFunc, lease WorkspaceLease, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(r.config.CancellationPoll)
	defer ticker.Stop()
	nextHeartbeat := time.Now().Add(r.config.HeartbeatInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		checkCtx, checkCancel := context.WithTimeout(context.Background(), min(r.config.CancellationPoll, 2*time.Second))
		requested, err := r.config.Store.WorkspaceCancellationRequested(checkCtx, lease)
		checkCancel()
		if err != nil {
			cancel(err)
			return
		}
		if requested {
			cancel(ErrCancelled)
			return
		}
		if time.Now().Before(nextHeartbeat) {
			continue
		}
		heartbeatCtx, heartbeatCancel := context.WithTimeout(context.Background(), min(r.config.HeartbeatInterval, 5*time.Second))
		err = r.config.Store.HeartbeatWorkspaceTask(heartbeatCtx, lease, r.config.LeaseDuration)
		heartbeatCancel()
		if err != nil {
			cancel(err)
			return
		}
		nextHeartbeat = time.Now().Add(r.config.HeartbeatInterval)
	}
}

type workspaceGuidanceInput struct {
	Knowledge []KnowledgeRevision
	Runbooks  []RunbookGuidance
}

func (r *WorkspaceRunner) executeWorkspace(ctx context.Context, lease WorkspaceLease, execution WorkspaceExecutionContext) error {
	revision, err := r.config.Revisions.Revision(ctx, lease.ConfigurationRevisionID)
	if err != nil {
		return fmt.Errorf("load exact Configuration Revision: %w", err)
	}
	if revision.ID != execution.Snapshot.ConfigurationRevisionID || revision.ID != execution.Run.ConfigurationRevisionID ||
		execution.Snapshot.Scope.ClusterID == "" || len(execution.Snapshot.Scope.Namespaces) == 0 {
		return errors.New("agent workspace revision or snapshot identity is inconsistent")
	}
	for _, evidenceID := range execution.Snapshot.EvidenceIDs {
		if err = r.config.Store.CiteWorkspaceSnapshotEvidence(ctx, lease, evidenceID); err != nil {
			return fmt.Errorf("cite Context Snapshot Evidence: %w", err)
		}
	}
	guidance, err := r.retrieveGuidance(ctx, lease, execution)
	if err != nil {
		return err
	}
	if err = r.runBoundedTools(ctx, lease, execution, revision); err != nil {
		return err
	}
	projected, err := r.config.Store.WorkspaceRun(ctx, lease.RunPublicID)
	if err != nil {
		return err
	}
	model, provider, actualModel, modelErr := r.config.Models.Model(ctx, revision.ID)
	if errors.Is(modelErr, ErrWorkspaceModelDisabled) {
		return r.config.Store.CompleteWorkspaceTask(ctx, lease, WorkspaceCompletion{
			Outcome: WorkspaceOutcomeInsufficient, Uncertainty: "high",
			Answer:      workspaceUnavailableAnswer(projected, "当前 Configuration Revision 未启用可用的模型 Provider；以上仅为真实 bounded tools 收集的 Evidence。"),
			FailureCode: workspaceModelDisabledCode, FailureSummary: "LLM Provider is disabled or has no configured credential",
		})
	}
	if modelErr != nil {
		return r.config.Store.CompleteWorkspaceTask(ctx, lease, WorkspaceCompletion{
			Outcome: WorkspaceOutcomeInsufficient, Uncertainty: "high",
			Answer:      workspaceUnavailableAnswer(projected, "模型 Provider 当前不可用；没有生成模型诊断。"),
			FailureCode: workspaceModelUnavailableCode, FailureSummary: modelErr.Error(),
		})
	}
	request := WorkspaceModelRequest{
		Objective: execution.Run.Objective, Snapshot: execution.Snapshot,
		Evidence: projected.Evidence, Guidance: projected.Guidance,
		Prompt: workspaceModelPrompt(execution, projected, guidance),
	}
	response, modelErr := model.Stream(ctx, request, func(delta string) error {
		return r.config.Store.AppendWorkspaceAnswerDelta(ctx, lease, delta)
	})
	if modelErr != nil {
		if errors.Is(modelErr, context.Canceled) || errors.Is(modelErr, context.DeadlineExceeded) {
			return modelErr
		}
		return r.config.Store.CompleteWorkspaceTask(ctx, lease, WorkspaceCompletion{
			Outcome: WorkspaceOutcomeInsufficient, Uncertainty: "high",
			Answer:      workspaceUnavailableAnswer(projected, "模型 Provider 调用失败；没有将不完整输出当作诊断。"),
			FailureCode: workspaceModelUnavailableCode, FailureSummary: modelErr.Error(),
			ModelProvider: provider, ActualModel: actualModel,
		})
	}
	answer := strings.TrimSpace(response.Answer)
	if answer == "" {
		answer = workspaceUnavailableAnswer(projected, "模型没有返回可用答案。")
	}
	outcome, uncertainty := workspaceModelOutcome(answer, projected.Evidence)
	return r.config.Store.CompleteWorkspaceTask(ctx, lease, WorkspaceCompletion{
		Outcome: outcome, Uncertainty: uncertainty, Answer: answer,
		ModelProvider: provider, ActualModel: actualModel,
		InputTokens: response.InputTokens, OutputTokens: response.OutputTokens,
	})
}

func (r *WorkspaceRunner) retrieveGuidance(ctx context.Context, lease WorkspaceLease, execution WorkspaceExecutionContext) (workspaceGuidanceInput, error) {
	knowledge, err := r.config.Store.ApplicableKnowledge(ctx, execution.Snapshot.ID, 5)
	if err != nil {
		return workspaceGuidanceInput{}, fmt.Errorf("load scoped Knowledge Items: %w", err)
	}
	for _, item := range knowledge {
		if err = r.config.Store.CiteWorkspaceKnowledge(ctx, lease, item.ID); err != nil {
			return workspaceGuidanceInput{}, fmt.Errorf("cite Knowledge Item revision: %w", err)
		}
	}
	guides, guideErr := r.config.Store.RunbookGuidance(ctx)
	if guideErr != nil || len(guides) == 0 {
		return workspaceGuidanceInput{Knowledge: knowledge, Runbooks: []RunbookGuidance{}}, nil
	}
	documents := make([]runbook.Document, 0, len(guides))
	byPath := make(map[string]RunbookGuidance, len(guides))
	for _, guide := range guides {
		document, parseErr := runbook.ParseMarkdown(guide.Path, []byte(guide.Content))
		if parseErr != nil {
			continue
		}
		documents = append(documents, document)
		byPath[guide.Path] = guide
	}
	if len(documents) == 0 {
		return workspaceGuidanceInput{Knowledge: knowledge, Runbooks: []RunbookGuidance{}}, nil
	}
	retriever := runbook.NewRetriever(documents, runbook.RetrieverOptions{
		DefaultLimit: 2, MaxLimit: 2, BM25Weight: 1, BM25K1: 1.2, BM25B: 0.75,
	})
	results, searchErr := retriever.Search(ctx, runbook.SearchRequest{
		AlertName: execution.AlertName,
		Keywords:  runbook.Tokenize(execution.Run.Objective + " " + execution.OwnerPrompt),
		Limit:     2,
	})
	if searchErr != nil && !errors.Is(searchErr, runbook.ErrUnavailable) {
		return workspaceGuidanceInput{}, searchErr
	}
	selected := make([]RunbookGuidance, 0, len(results))
	for _, result := range results {
		guide, ok := byPath[result.File]
		if !ok {
			continue
		}
		if err = r.config.Store.CiteWorkspaceRunbook(ctx, lease, guide.Path, guide.Revision); err != nil {
			return workspaceGuidanceInput{}, fmt.Errorf("cite Runbook revision: %w", err)
		}
		guide.Content = result.Snippet
		selected = append(selected, guide)
	}
	return workspaceGuidanceInput{Knowledge: knowledge, Runbooks: selected}, nil
}

func workspaceModelOutcome(answer string, evidence []EvidenceCitation) (WorkspaceOutcome, string) {
	citedSources := make(map[string]struct{})
	for _, citation := range evidence {
		if !strings.Contains(answer, citation.EvidenceID) {
			continue
		}
		source := strings.TrimSpace(citation.Source)
		if source != "" {
			citedSources[source] = struct{}{}
		}
	}
	if len(citedSources) >= 2 {
		return WorkspaceOutcomeDiagnosed, "medium"
	}
	return WorkspaceOutcomeInsufficient, "high"
}

func workspaceUnavailableAnswer(run WorkspaceRun, reason string) string {
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(reason))
	if len(run.Evidence) == 0 {
		builder.WriteString(" 当前没有可引用的 Evidence，不能建立根因结论。")
		return builder.String()
	}
	builder.WriteString("\n\n本次已保留的当前 Evidence：")
	for _, item := range run.Evidence {
		builder.WriteString("\n- ")
		builder.WriteString(item.EvidenceID)
		builder.WriteString(" · ")
		builder.WriteString(item.Source)
		builder.WriteString(" · ")
		builder.WriteString(item.CollectedAt.UTC().Format(time.RFC3339))
	}
	return builder.String()
}

func workspaceModelPrompt(execution WorkspaceExecutionContext, run WorkspaceRun, guidance workspaceGuidanceInput) string {
	var builder strings.Builder
	builder.WriteString("请用中文给出简洁的云原生诊断。不得输出思维链、隐藏推理、任意 shell 或 kubectl 命令，不得声称执行了 mutation。")
	builder.WriteString(" 只有下列 Evidence 是当前运行事实；Knowledge 与 Runbook 仅是 guidance，必须分别标注 exact revision 和 age，不能作为根因证明。")
	builder.WriteString(" 每个事实结论必须原样引用 [Evidence: <ID>]。无法由当前 Evidence 支持时明确说明不确定性。\n\n目标：")
	builder.WriteString(workspaceBound(execution.Run.Objective, 2048))
	builder.WriteString("\nSnapshot: ")
	builder.WriteString(execution.Snapshot.ID)
	builder.WriteString(" / Configuration Revision: ")
	builder.WriteString(execution.Snapshot.ConfigurationRevisionID)
	builder.WriteString("\n\n当前 Evidence：")
	for _, item := range run.Evidence {
		builder.WriteString("\n- [Evidence: ")
		builder.WriteString(item.EvidenceID)
		builder.WriteString("] source=")
		builder.WriteString(item.Source)
		builder.WriteString(" collected_at=")
		builder.WriteString(item.CollectedAt.UTC().Format(time.RFC3339))
		builder.WriteString(" summary=")
		builder.WriteString(workspaceBound(item.Summary, 512))
	}
	if len(guidance.Knowledge)+len(guidance.Runbooks) > 0 {
		builder.WriteString("\n\nGuidance（非 Evidence）：")
	}
	for _, item := range guidance.Knowledge {
		builder.WriteString("\n- Knowledge revision=")
		builder.WriteString(item.ID)
		builder.WriteString(" created_at=")
		builder.WriteString(item.CreatedAt.UTC().Format(time.RFC3339))
		builder.WriteString(" content=")
		builder.WriteString(workspaceBound(item.Content, 1200))
	}
	for _, item := range guidance.Runbooks {
		builder.WriteString("\n- Runbook path=")
		builder.WriteString(item.Path)
		builder.WriteString(" revision=")
		builder.WriteString(item.Revision)
		builder.WriteString(" content=")
		builder.WriteString(workspaceBound(item.Content, 1200))
	}
	return workspaceBound(builder.String(), 16000)
}
