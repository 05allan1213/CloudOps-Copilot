package bootstrap

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	agentllm "github.com/05allan1213/CloudOps-Copilot/internal/agent/llm"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/infrastructuregateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/monitoringgateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/telemetrygateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

type workspaceModelFactory struct {
	settings *settings.Service
	observer agentllm.LLMObserver
}

func (f workspaceModelFactory) Model(ctx context.Context, revisionID string) (agent.WorkspaceModel, string, string, error) {
	access, err := f.settings.ProviderAccess(ctx, revisionID, settings.ProviderLLM)
	if err != nil {
		if errors.Is(err, settings.ErrNotFound) {
			return nil, "", "", agent.ErrWorkspaceModelDisabled
		}
		return nil, "", "", err
	}
	defer access.Clear()
	configuration := access.Configuration
	if !configuration.Enabled || strings.TrimSpace(configuration.Endpoint) == "" ||
		strings.TrimSpace(configuration.Model) == "" || len(access.Credential) == 0 {
		return nil, "", "", agent.ErrWorkspaceModelDisabled
	}
	timeout := time.Duration(configuration.TimeoutMS) * time.Millisecond
	if timeout <= 0 || timeout > 2*time.Minute {
		timeout = 60 * time.Second
	}
	maxTokens := configuration.MaxResults
	if maxTokens < 128 {
		maxTokens = 800
	}
	if maxTokens > 4096 {
		maxTokens = 4096
	}
	zeroRetries := 0
	client := agentllm.NewClient(agentllm.Options{
		APIKey: string(access.Credential), APIURL: configuration.Endpoint, Model: configuration.Model,
		Timeout: timeout, MaxTokens: maxTokens, MaxRetries: &zeroRetries, Observer: f.observer,
	})
	return workspaceLLM{client: client}, string(settings.ProviderLLM), configuration.Model, nil
}

type workspaceLLM struct {
	client *agentllm.Client
}

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
	return agent.NewWorkspaceRunner(agent.WorkspaceRunnerConfig{
		Owner: cfg.Async.WorkerID, Store: repository, Revisions: settingsService,
		Kubernetes: kubernetesClient, Metrics: monitoringClient, Telemetry: telemetryClient,
		Models:      workspaceModelFactory{settings: settingsService, observer: metrics},
		MaxInFlight: cfg.Async.InvestigateMaxInFlight, PollInterval: 250 * time.Millisecond,
		LeaseDuration: cfg.Async.InvestigateLease, HeartbeatInterval: cfg.Async.InvestigateHeartbeat,
		CancellationPoll: time.Second, RetryDelay: 500 * time.Millisecond,
	})
}

var _ agent.WorkspaceModelFactory = workspaceModelFactory{}
var _ agent.WorkspaceModel = workspaceLLM{}
