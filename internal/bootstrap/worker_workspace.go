package bootstrap

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	agentadapter "github.com/05allan1213/CloudOps-Copilot/internal/agent/adapter"
	agentllm "github.com/05allan1213/CloudOps-Copilot/internal/agent/llm"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/infrastructuregateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/monitoringgateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/telemetrygateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

const workspaceDiagnosisMaxTokens = 2048

type workspaceModelFactory struct {
	settings *settings.Service
	observer agentllm.LLMObserver
}

func (f workspaceModelFactory) Model(ctx context.Context, revisionID string) (agent.WorkspaceModel, string, string, error) {
	client, model, maxTokens, err := f.client(ctx, revisionID, 0)
	if err != nil {
		return nil, "", "", err
	}
	return workspaceLLM{client: client, maxTokens: int64(maxTokens)}, string(settings.ProviderLLM), model, nil
}

func (f workspaceModelFactory) DiagnosisModel(ctx context.Context, revisionID string) (agent.WorkspaceDiagnosisModel, string, string, error) {
	client, model, maxTokens, err := f.client(ctx, revisionID, workspaceDiagnosisMaxTokens)
	if err != nil {
		return nil, "", "", err
	}
	structured, err := agentadapter.NewLLMModel(client)
	if err != nil {
		return nil, "", "", err
	}
	return workspaceDiagnosisLLM{model: structured, maxTokens: int64(maxTokens)}, string(settings.ProviderLLM), model, nil
}

func (f workspaceModelFactory) client(ctx context.Context, revisionID string, maxTokensCap int) (*agentllm.Client, string, int, error) {
	access, err := f.settings.ProviderAccess(ctx, revisionID, settings.ProviderLLM)
	if err != nil {
		if errors.Is(err, settings.ErrNotFound) {
			return nil, "", 0, agent.ErrWorkspaceModelDisabled
		}
		return nil, "", 0, err
	}
	defer access.Clear()
	configuration := access.Configuration
	if !configuration.Enabled || strings.TrimSpace(configuration.Endpoint) == "" ||
		strings.TrimSpace(configuration.Model) == "" || len(access.Credential) == 0 {
		return nil, "", 0, agent.ErrWorkspaceModelDisabled
	}
	timeout := workspaceLLMTimeout(configuration.TimeoutMS)
	maxTokens := workspaceModelMaxTokens(configuration.MaxResults, maxTokensCap)
	zeroRetries := 0
	client := agentllm.NewClient(agentllm.Options{
		APIKey: string(access.Credential), APIURL: configuration.Endpoint, Model: configuration.Model,
		Timeout: timeout, MaxTokens: maxTokens, MaxRetries: &zeroRetries, ReasoningEffort: "low", Observer: f.observer,
	})
	return client, configuration.Model, maxTokens, nil
}

func workspaceModelMaxTokens(configured, cap int) int {
	maxTokens := configured
	if maxTokens < 128 {
		maxTokens = 800
	}
	if maxTokens > 4096 {
		maxTokens = 4096
	}
	if cap > 0 && maxTokens > cap {
		maxTokens = cap
	}
	return maxTokens
}

func workspaceLLMTimeout(timeoutMS int) time.Duration {
	timeout := time.Duration(timeoutMS) * time.Millisecond
	if timeout <= 0 || timeout > 5*time.Minute {
		return 60 * time.Second
	}
	return timeout
}

type workspaceLLM struct {
	client    *agentllm.Client
	maxTokens int64
}

func (m workspaceLLM) MaxOutputTokensPerInvocation() int64 { return m.maxTokens }

func (m workspaceLLM) Stream(ctx context.Context, request agent.WorkspaceModelRequest, onDelta func(string) error) (agent.WorkspaceModelResponse, error) {
	answer, usage, err := m.client.ChatStream(ctx, []agentllm.ChatMessage{
		{Role: "system", Content: "你是 CloudOps 的证据约束 Agent。只输出给 Owner 的结论，不输出思维链、隐藏推理或未经请求的内部分析。"},
		{Role: "user", Content: request.Prompt},
	}, func(delta string) error {
		return emitWorkspaceDeltas(delta, 1024, onDelta)
	})
	if err != nil {
		if errors.Is(err, agentllm.ErrDisabled) {
			return agent.WorkspaceModelResponse{}, agent.ErrWorkspaceModelDisabled
		}
		return agent.WorkspaceModelResponse{}, err
	}
	response := agent.WorkspaceModelResponse{Answer: answer}
	if usage != nil {
		response.InputTokens = int64(usage.PromptTokens)
		response.OutputTokens = int64(usage.CompletionTokens)
	}
	return response, nil
}

type workspaceDiagnosisLLM struct {
	model     *agentadapter.LLMModel
	maxTokens int64
}

func (m workspaceDiagnosisLLM) SynthesizeDiagnosis(ctx context.Context, view agent.DiagnosisView) (agent.DiagnosisCandidate, agent.ModelUsage, error) {
	return m.model.SynthesizeDiagnosis(ctx, view)
}

func (workspaceDiagnosisLLM) MaxProviderCallsPerInvocation() int { return 2 }

func (m workspaceDiagnosisLLM) MaxOutputTokensPerInvocation() int64 { return m.maxTokens }

func emitWorkspaceDeltas(delta string, maxBytes int, emit func(string) error) error {
	if maxBytes < utf8.UTFMax || emit == nil {
		return agent.ErrInvalidArgument
	}
	delta = strings.ToValidUTF8(delta, "\uFFFD")
	for len(delta) > maxBytes {
		end := maxBytes
		for end > 0 && !utf8.RuneStart(delta[end]) {
			end--
		}
		if end == 0 {
			return agent.ErrInvalidArgument
		}
		if err := emit(delta[:end]); err != nil {
			return err
		}
		delta = delta[end:]
	}
	if delta == "" {
		return nil
	}
	return emit(delta)
}

func newWorkerWorkspaceRunner(cfg WorkerConfig, settingsService *settings.Service, repository *agent.WorkspaceRepository, metrics *middleware.Metrics) (*agent.WorkspaceRunner, error) {
	kubernetesClient, err := infrastructuregateway.NewClient(cfg.Application.WorkerManagementTarget, cfg.Application.K8SRequestTimeout)
	if err != nil {
		return nil, err
	}
	monitoringClient, err := monitoringgateway.NewClient(cfg.Application.WorkerManagementTarget, cfg.Application.ObservabilityRequestTimeout+2*time.Second)
	if err != nil {
		return nil, err
	}
	telemetryClient, err := telemetrygateway.NewClient(cfg.Application.WorkerManagementTarget, cfg.Application.ObservabilityRequestTimeout+2*time.Second)
	if err != nil {
		return nil, err
	}
	repository.SetRunbookDir(cfg.Application.RunbookDir)
	models := workspaceModelFactory{settings: settingsService, observer: metrics}
	return agent.NewWorkspaceRunner(agent.WorkspaceRunnerConfig{
		Owner: cfg.Async.WorkerID, Store: repository, Revisions: settingsService,
		Kubernetes: kubernetesClient, Metrics: monitoringClient, Telemetry: telemetryClient,
		Models: models, DiagnosisModels: models,
		MaxInFlight: cfg.Async.InvestigateMaxInFlight, PollInterval: 250 * time.Millisecond,
		LeaseDuration: cfg.Async.InvestigateLease, HeartbeatInterval: cfg.Async.InvestigateHeartbeat,
		CancellationPoll: time.Second, RetryDelay: 500 * time.Millisecond,
	})
}

var _ agent.WorkspaceModelFactory = workspaceModelFactory{}
var _ agent.WorkspaceDiagnosisModelFactory = workspaceModelFactory{}
var _ agent.WorkspaceModel = workspaceLLM{}
var _ agent.WorkspaceDiagnosisModel = workspaceDiagnosisLLM{}
