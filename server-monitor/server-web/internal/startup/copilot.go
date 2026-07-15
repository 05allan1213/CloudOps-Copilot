package startup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"

	"server-web/internal/agent"
	agentadapter "server-web/internal/agent/adapter"
	"server-web/internal/change"
	"server-web/internal/config"
	copilotaction "server-web/internal/copilot/action"
	copilotcontext "server-web/internal/copilot/context"
	copilotdiagnosis "server-web/internal/copilot/diagnosis"
	copilotfeedback "server-web/internal/copilot/feedback"
	copilothandler "server-web/internal/copilot/handler"
	copilotk8s "server-web/internal/copilot/k8s"
	copilotllm "server-web/internal/copilot/llm"
	copilotnlu "server-web/internal/copilot/nlu"
	copilotrunbook "server-web/internal/copilot/runbook"
	copilotservice "server-web/internal/copilot/service"
	copilotsession "server-web/internal/copilot/session"
	copilotsummary "server-web/internal/copilot/summary"
	copilottool "server-web/internal/copilot/tool"
	"server-web/internal/di"
	"server-web/internal/infra/argocdread"
	"server-web/internal/infra/githubread"
	"server-web/internal/infra/incidentmysql"
	"server-web/internal/infra/k8schange"
	rediscache "server-web/internal/infra/redis"
	"server-web/internal/infra/registryread"
	ws "server-web/internal/infra/websocket"
	"server-web/internal/middleware"
	"server-web/internal/router"
	agentruntime "server-web/internal/service/agentruntime"
	appalert "server-web/internal/service/alert"
	changeintelligence "server-web/internal/service/changeintelligence"

	eventbus "server-monitor/pkg/kafka"
)

func InitCopilot(ctx context.Context, cfg config.Config, container *di.Container, k8sDeps copilotk8s.Deps) (*router.CopilotRuntime, *router.CopilotDeps, error) {
	changeService, changeTools, err := initChangeIntelligence(cfg, container, k8sDeps.Client)
	if err != nil {
		return nil, nil, err
	}
	if changeService != nil {
		container.Handler.SetChangeIntelligence(changeService)
	}
	if !cfg.CopilotEnabled {
		return nil, nil, nil
	}

	runbookDocs, err := copilotrunbook.LoadDir(context.Background(), cfg.RunbookDir, copilotrunbook.LoadOptions{
		MaxFiles:     cfg.RunbookMaxFiles,
		MaxFileBytes: cfg.RunbookMaxFileBytes,
	})
	if err != nil {
		return nil, nil, err
	}

	llmClient := copilotllm.NewClient(copilotllm.Options{
		APIKey:    cfg.LLMAPIKey,
		APIURL:    cfg.LLMAPIURL,
		Model:     cfg.LLMModel,
		Timeout:   cfg.LLMTimeout,
		MaxTokens: cfg.LLMMaxTokens,
		Observer:  container.Metrics,
	})
	runbookEmbedder, runbookVectorStore := initRunbookEmbedding(ctx, cfg, runbookDocs)
	runbookRetriever := copilotrunbook.NewRetriever(runbookDocs, copilotrunbook.RetrieverOptions{
		DefaultLimit: cfg.RunbookSearchTopN,
		MaxLimit:     5,
		BM25Weight:   cfg.RunbookBM25Weight,
		BM25K1:       cfg.RunbookBM25K1,
		BM25B:        cfg.RunbookBM25B,
		Observer:     container.Metrics,
		Embedder:     runbookEmbedder,
		VectorStore:  runbookVectorStore,
		RRFK:         cfg.RunbookRRFK,
		Reranker:     buildRunbookReranker(cfg, llmClient),
	})

	var tools copilotservice.ToolExecutor
	var toolExecutor *copilottool.Executor
	if cfg.CopilotToolRegistryEnabled {
		toolExecutor, err = copilottool.NewExecutor(copilottool.Options{
			HostService:     container.Host(),
			AlertService:    container.AlertService,
			PromClient:      container.PromClient,
			RunbookSearcher: runbookRetriever,
			K8sReader:       k8sDeps.Reader,
			K8sNodesEnabled: cfg.K8SNodesEnabled,
			DB:              container.DB,
			Observer:        container.Metrics,
			Timeout:         cfg.CopilotToolDefaultTimeout,
			LogArgs:         cfg.CopilotToolLogArgs,
			AdditionalTools: changeTools,
		})
		if err != nil {
			return nil, nil, err
		}
		tools = toolExecutor
	} else {
		tools = copilottool.NewDisabledExecutor()
	}

	diagnosisRepo := copilotdiagnosis.NewRepository(container.DB)
	feedbackRepo := copilotfeedback.NewMySQLRepository(container.DB)
	diagnosisService := initDiagnosisService(cfg, container.AlertService, llmClient, toolExecutor, diagnosisRepo, feedbackRepo, container.DB, container.Metrics)
	copilotStore := copilotsession.NewRedisStore(container.RedisClient)
	copilotContextMgr := copilotcontext.NewManager(copilotcontext.Options{
		Store:     copilotStore,
		MaxRounds: cfg.CopilotChatHistoryMaxRounds,
	})
	var copilotSummarizer *copilotsummary.Summarizer
	if llmClient != nil {
		copilotSummarizer = copilotsummary.NewSummarizer(copilotsummary.Options{
			LLM:       llmClient,
			Timeout:   cfg.CopilotSummaryTimeout,
			MaxPrompt: cfg.CopilotSummaryMaxPromptBytes,
		})
	}
	copilotHandler := copilothandler.NewHandler(copilotservice.NewService(copilotservice.Config{
		MaxMessageLength:     cfg.CopilotMaxMessageLength,
		SessionTTL:           cfg.CopilotSessionTTL,
		MaxSessionMessages:   cfg.CopilotMaxSessionMessages,
		Store:                copilotStore,
		Classifier:           copilotnlu.NewClassifier(copilotnlu.WithNLUObserver(container.Metrics)),
		LLM:                  llmClient,
		Tools:                tools,
		Diagnosis:            diagnosisService,
		Summarizer:           copilotSummarizer,
		SummaryEnabled:       cfg.CopilotSummaryEnabled,
		ContextManager:       copilotContextMgr,
		ToolDefs:             toolDefinitions(toolExecutor),
		ToolsClassifyEnabled: cfg.CopilotToolsClassifyEnabled,
		LLMClassifyThreshold: cfg.CopilotLLMClassifyThreshold,
		MultiIntentEnabled:   cfg.CopilotMultiIntentEnabled,
		MultiIntentMax:       cfg.CopilotMultiIntentMax,
	}))

	deps := &router.CopilotDeps{
		Handler:          copilotHandler,
		DiagnosisHandler: copilotdiagnosis.NewHandler(diagnosisService),
		FeedbackHandler:  copilotfeedback.NewHandler(copilotfeedback.NewService(feedbackRepo, container.Metrics), copilotdiagnosis.NewReportAccessChecker(diagnosisRepo), cfg.FeedbackCommentMaxLength),
	}
	if cfg.ActionApprovalEnabled {
		actionHandler, err := initActionHandler(cfg, container, k8sDeps.Client)
		if err != nil {
			return nil, nil, err
		}
		deps.ActionHandler = actionHandler
	}
	runtime := &router.CopilotRuntime{DiagnosisService: diagnosisService, KafkaObserver: container.Metrics}
	if cfg.IncidentAgentEnabled {
		if container.DB == nil || toolExecutor == nil {
			return nil, nil, fmt.Errorf("incident agent runtime requires MySQL and the tool registry")
		}
		store, storeErr := incidentmysql.NewStore(container.DB)
		if storeErr != nil {
			return nil, nil, fmt.Errorf("incident agent store init failed: %w", storeErr)
		}
		zeroRetries := 0
		agentLLM := copilotllm.NewClient(copilotllm.Options{APIKey: cfg.LLMAPIKey, APIURL: cfg.LLMAPIURL, Model: cfg.LLMModel, Timeout: cfg.LLMTimeout, MaxTokens: cfg.LLMMaxTokens, MaxRetries: &zeroRetries, Observer: container.Metrics})
		model, modelErr := agentadapter.NewLLMModel(agentLLM)
		if modelErr != nil {
			return nil, nil, fmt.Errorf("incident agent model init failed: %w", modelErr)
		}
		readOnlyTools, toolErr := agentadapter.NewReadOnlyTools(toolExecutor)
		if toolErr != nil {
			return nil, nil, fmt.Errorf("incident agent read-only tools init failed: %w", toolErr)
		}
		agentService, serviceErr := agentruntime.New(ctx, store, model, readOnlyTools, agentruntime.Config{
			Enabled: true, WorkerID: cfg.IncidentAgentWorkerID, PollInterval: cfg.IncidentAgentPollInterval,
			LeaseDuration: cfg.IncidentAgentLeaseDuration, HeartbeatPeriod: cfg.IncidentAgentHeartbeatPeriod,
			Model: cfg.LLMModel, PromptVersion: "incident-agent-v3-change-readonly", MaxGraphRunSteps: 96,
			Observer: container.Metrics,
			Limits: agent.Limits{MaxSteps: cfg.IncidentAgentMaxSteps, MaxToolCalls: cfg.IncidentAgentMaxToolCalls,
				MaxModelCalls: cfg.IncidentAgentMaxModelCalls, TokenBudget: cfg.IncidentAgentTokenBudget,
				MaxEvidenceItems: cfg.IncidentAgentMaxEvidenceItems, MaxRuntime: cfg.IncidentAgentMaxRuntime,
				ToolTimeout: cfg.IncidentAgentToolTimeout, MaxEvidenceBytes: cfg.IncidentAgentMaxEvidenceBytes,
				MaxCheckpointSize: cfg.IncidentAgentMaxCheckpointBytes, MaxStepRetries: cfg.IncidentAgentMaxStepRetries},
		})
		if serviceErr != nil {
			return nil, nil, fmt.Errorf("incident agent runtime init failed: %w", serviceErr)
		}
		zap.L().Info("incident agent worker initialized", zap.String("worker_id", cfg.IncidentAgentWorkerID))
		container.Handler.SetAgentRuntime(agentService)
		runtime.AgentWorker = agentService.NewWorker()
	}
	return runtime, deps, nil
}

func initChangeIntelligence(cfg config.Config, container *di.Container, k8sClient kubernetes.Interface) (*changeintelligence.Service, []copilottool.Tool, error) {
	if !cfg.ChangeIntelligenceEnabled {
		return nil, nil, nil
	}
	zap.L().Info("change intelligence effective configuration", zap.Any("config", cfg.EffectiveChangeConfig()))
	if container.DB == nil {
		return nil, nil, fmt.Errorf("change intelligence requires MySQL")
	}
	incidentStore, err := incidentmysql.NewStore(container.DB)
	if err != nil {
		return nil, nil, fmt.Errorf("change incident store init failed: %w", err)
	}
	changeRepository, err := incidentmysql.NewChangeRepository(container.DB)
	if err != nil {
		return nil, nil, fmt.Errorf("change repository init failed: %w", err)
	}
	var mappings map[string]changeintelligence.ServiceMapping
	if err := json.Unmarshal([]byte(cfg.ChangeServiceMappingsJSON), &mappings); err != nil || len(mappings) == 0 {
		return nil, nil, fmt.Errorf("invalid CHANGE_SERVICE_MAPPINGS_JSON")
	}
	for service, mapping := range mappings {
		if strings.TrimSpace(service) == "" || strings.TrimSpace(mapping.Repository.Owner) == "" || strings.TrimSpace(mapping.Repository.Name) == "" || strings.TrimSpace(mapping.ArgoApplication) == "" || strings.TrimSpace(mapping.ArgoProject) == "" || strings.TrimSpace(mapping.GitOpsPath) == "" || path.Clean(mapping.GitOpsPath) == "." || strings.HasPrefix(path.Clean(mapping.GitOpsPath), "../") {
			return nil, nil, fmt.Errorf("invalid change mapping for service %q", service)
		}
		if mapping.ContainerName != "" && (len(mapping.ContainerName) > 63 || !containerNamePattern.MatchString(mapping.ContainerName)) {
			return nil, nil, fmt.Errorf("invalid container_name in change mapping for service %q", service)
		}
		if cfg.RegistryMetadataEnabled {
			source := strings.TrimSpace(mapping.SourceRepository)
			if source == "" {
				source = "https://github.com/" + mapping.Repository.FullName()
			}
			if !sourceRepositoryMatches(source, mapping.Repository) || !exactListContains(cfg.OCIAllowedSources, source) {
				return nil, nil, fmt.Errorf("change mapping source repository is not an exact allowlisted service repository for %q", service)
			}
		}
	}
	var githubClient change.GitHubReader
	if cfg.GitHubEnabled {
		repositories, repositoryNames, err := normalizedGitHubRepositories(cfg.GitHubAllowedOwners, cfg.GitHubAllowedRepositories)
		if err != nil {
			return nil, nil, err
		}
		var token githubread.TokenProvider
		if cfg.GitHubTokenFile != "" {
			token = githubread.FileTokenProvider{Path: cfg.GitHubTokenFile}
		} else {
			token, err = githubread.NewAppTokenProvider(githubread.AppTokenConfig{BaseURL: cfg.GitHubAPIBaseURL, AppID: cfg.GitHubAppID, InstallationID: cfg.GitHubInstallationID, PrivateKeyFile: cfg.GitHubPrivateKeyFile, APIVersion: "2022-11-28", AllowedRepositories: repositoryNames})
			if err != nil {
				return nil, nil, fmt.Errorf("GitHub App provider init failed: %w", err)
			}
		}
		githubClient, err = githubread.New(githubread.Config{BaseURL: cfg.GitHubAPIBaseURL, TokenProvider: token, AllowedRepositories: repositories, AllowedBranches: cfg.GitHubAllowedBranches, AllowedPaths: cfg.GitHubAllowedPaths, DeniedPathPatterns: cfg.GitHubDeniedPathPatterns, Timeout: cfg.GitHubTimeout, MaxRetries: cfg.GitHubMaxRetries, MaxPages: 3, MaxDiffFiles: cfg.GitHubMaxDiffFiles, MaxPatchFiles: cfg.GitHubMaxDiffFiles, MaxPatchBytesPerFile: 8192, MaxDiffBytes: cfg.GitHubMaxDiffBytes, APIVersion: "2022-11-28", Observer: container.Metrics})
		if err != nil {
			return nil, nil, fmt.Errorf("GitHub read adapter init failed: %w", err)
		}
	}
	var argoClient change.ArgoCDReader
	if cfg.ArgoCDEnabled {
		argoClient, err = argocdread.New(argocdread.Config{Server: cfg.ArgoCDServer, TokenFile: cfg.ArgoCDTokenFile, AllowedApplications: cfg.ArgoCDAllowedApplications, AllowedProjects: cfg.ArgoCDAllowedProjects, Timeout: cfg.ArgoCDTimeout, MaxRetries: 1, MaxResources: cfg.ArgoCDMaxResources, MaxDiffBytes: cfg.ArgoCDMaxDiffBytes, Observer: container.Metrics})
		if err != nil {
			return nil, nil, fmt.Errorf("argo CD read adapter init failed: %w", err)
		}
	}
	var runtimeReader change.RuntimeReader
	if k8sClient != nil {
		runtimeReader, err = k8schange.New(k8sClient, cfg.K8SAllowedNamespaces, cfg.K8SRequestTimeout)
		if err != nil {
			return nil, nil, fmt.Errorf("runtime image reader init failed: %w", err)
		}
	}
	var registryClient change.RegistryMetadataReader
	if cfg.RegistryMetadataEnabled {
		registryClient, err = registryread.New(registryread.Config{
			BaseURL: cfg.RegistryBaseURL, AllowedHosts: cfg.RegistryAllowedHosts, AllowedRepositories: cfg.RegistryAllowedRepos,
			AllowedAuthRealms: cfg.RegistryAllowedAuthRealms, AllowedRedirectHosts: cfg.RegistryAllowedRedirects,
			BearerTokenFile: cfg.RegistryBearerTokenFile, UsernameFile: cfg.RegistryUsernameFile, PasswordFile: cfg.RegistryPasswordFile,
			Timeout: cfg.RegistryTimeout, MaxRetries: cfg.RegistryMaxRetries, ManifestMaxBytes: cfg.RegistryManifestMaxBytes,
			ConfigMaxBytes: cfg.RegistryConfigMaxBytes, CacheTTL: cfg.RegistryCacheTTL, CacheMaxItems: cfg.RegistryCacheMaxItems,
			Observer: container.Metrics,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("registry metadata adapter init failed: %w", err)
		}
	}
	service, err := changeintelligence.New(changeintelligence.Config{Enabled: true, Lookback: cfg.ChangeLookback, MaxCandidates: cfg.ChangeMaxCandidates, Incidents: incidentStore, Changes: changeRepository, GitHub: githubClient, ArgoCD: argoClient, Runtime: runtimeReader, Registry: registryClient, RegistryHosts: cfg.RegistryAllowedHosts, AllowedOCISources: cfg.OCIAllowedSources, Mappings: mappings, Observer: container.Metrics})
	if err != nil {
		return nil, nil, fmt.Errorf("change intelligence service init failed: %w", err)
	}
	tools := copilottool.NewPhase3ReadOnlyTools(copilottool.ChangeToolConfig{Service: service, GitHub: githubClient, ArgoCD: argoClient, Timeout: cfg.IncidentAgentToolTimeout})
	return service, tools, nil
}

var containerNamePattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)

func sourceRepositoryMatches(source string, repository change.RepositoryRef) bool {
	parsed, err := url.Parse(strings.TrimSuffix(strings.TrimSpace(source), ".git"))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(strings.Trim(parsed.Path, "/"), repository.FullName())
}

func exactListContains(values []string, expected string) bool {
	expected = strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(expected), "/"), ".git")
	for _, value := range values {
		value = strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(value), "/"), ".git")
		if strings.EqualFold(value, expected) {
			return true
		}
	}
	return false
}

func normalizedGitHubRepositories(owners, repositories []string) ([]string, []string, error) {
	allowedOwners := map[string]struct{}{}
	for _, owner := range owners {
		allowedOwners[strings.ToLower(strings.TrimSpace(owner))] = struct{}{}
	}
	full := make([]string, 0, len(repositories))
	names := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		parts := strings.Split(strings.Trim(strings.TrimSpace(repository), "/"), "/")
		if len(parts) != 2 {
			return nil, nil, fmt.Errorf("GITHUB_ALLOWED_REPOSITORIES must use owner/repository")
		}
		if _, ok := allowedOwners[strings.ToLower(parts[0])]; !ok {
			return nil, nil, fmt.Errorf("repository owner %q is not allowlisted", parts[0])
		}
		full = append(full, parts[0]+"/"+parts[1])
		names = append(names, parts[1])
	}
	return full, names, nil
}

func initRunbookEmbedding(ctx context.Context, cfg config.Config, docs []copilotrunbook.Document) (*copilotrunbook.Embedder, *copilotrunbook.MemoryVectorStore) {
	if cfg.EmbeddingAPIURL == "" || cfg.EmbeddingAPIKey == "" || cfg.EmbeddingModel == "" {
		return nil, nil
	}
	embedder := copilotrunbook.NewEmbedder(copilotrunbook.EmbedderOptions{
		APIURL:     cfg.EmbeddingAPIURL,
		APIKey:     cfg.EmbeddingAPIKey,
		Model:      cfg.EmbeddingModel,
		Timeout:    cfg.EmbeddingTimeout,
		Dimensions: cfg.EmbeddingDims,
	})
	buildCtx, buildCancel := context.WithTimeout(ctx, cfg.EmbeddingIndexBuildTimeout)
	store, err := buildRunbookVectorStore(buildCtx, docs, embedder, &buildIndexLogger{})
	buildCancel()
	if err != nil {
		zap.L().Warn("failed to build runbook vector index, falling back to structured+BM25", zap.Error(err))
		return embedder, nil
	}
	return embedder, store
}

func initDiagnosisService(cfg config.Config, alertService *appalert.Service, llmClient *copilotllm.Client, toolExecutor *copilottool.Executor, diagnosisRepo *copilotdiagnosis.Repository, feedbackRepo copilotfeedback.Repository, db *gorm.DB, metrics *middleware.Metrics) *copilotdiagnosis.Service {
	var runner copilotdiagnosis.ToolRunner
	if toolExecutor != nil {
		runner = diagnosisToolRunner{executor: toolExecutor}
	}
	return copilotdiagnosis.NewService(copilotdiagnosis.Config{
		Repository: diagnosisRepo,
		Resolver: copilotdiagnosis.NewResolver(copilotdiagnosis.ResolverOptions{
			DB:           db,
			AlertService: alertService,
			Timeout:      cfg.CopilotToolDefaultTimeout,
		}),
		Collector: copilotdiagnosis.NewEvidenceCollector(copilotdiagnosis.EvidenceOptions{
			Runner:        runner,
			Timeout:       45 * time.Second,
			RunbookLimit:  cfg.RunbookSearchTopN,
			RerankEnabled: cfg.RerankerEnabled,
		}),
		Summarizer: copilotdiagnosis.NewLLMSummarizerWithOptions(llmClient, copilotdiagnosis.LLMSummarizerOptions{
			Timeout:  cfg.DiagnosisLLMTimeout,
			Observer: metrics,
		}),
		FeedbackRepository: feedbackRepo,
	})
}

func initActionHandler(cfg config.Config, container *di.Container, k8sClient kubernetes.Interface) (*copilotaction.Handler, error) {
	actionExecutionEnabled := cfg.ActionExecutionEnabled && cfg.K8SWriteEnabled
	actionExecutor := copilotaction.K8sExecutor(copilotaction.DisabledK8sExecutor{})
	if actionExecutionEnabled {
		if k8sClient == nil {
			return nil, errK8sClientRequired()
		}
		actionExecutor = copilotaction.NewClientK8sExecutor(k8sClient, copilotaction.ClientK8sExecutorConfig{
			AllowedNamespaces: cfg.K8SAllowedNamespaces,
			MaxReplicas:       cfg.ActionMaxReplicas,
			RequestTimeout:    cfg.K8SRequestTimeout,
		})
	} else if cfg.ActionExecutionEnabled && !cfg.K8SWriteEnabled {
		zap.L().Warn("ACTION_EXECUTION_ENABLED=true but K8S_WRITE_ENABLED=false; forcing k8s action execution disabled")
	}
	actionService := copilotaction.NewService(copilotaction.ServiceConfig{
		Repository:             copilotaction.NewRepository(container.DB),
		Policy:                 copilotaction.NewPolicy(copilotaction.PolicyConfig{MaxReplicas: cfg.ActionMaxReplicas}),
		Executor:               actionExecutor,
		Notifier:               copilotaction.NewWebSocketNotifier(container.WSHub),
		OperationEvents:        operationEventProducer{producer: container.KafkaProducer},
		Observer:               container.Metrics,
		OperationEventsEnabled: cfg.ActionOperationEventsEnabled,
		StatusPushEnabled:      cfg.ActionStatusPushEnabled,
		ActionExecutionEnabled: actionExecutionEnabled,
		PendingTTL:             cfg.ActionPendingTTL,
	})
	return copilotaction.NewHandler(actionService), nil
}

func toolDefinitions(toolExecutor *copilottool.Executor) []copilotllm.ToolDefinition {
	if toolExecutor == nil {
		return nil
	}
	return copilottool.ConvertToOpenAITools(toolExecutor.Registry().List())
}

func InitDiagnosisConsumer(cfg config.Config, redisClient *rediscache.Client, runtime *router.CopilotRuntime, hub *ws.Hub) (*eventbus.Consumer, error) {
	if !cfg.DiagnosisEnabled {
		zap.L().Info("diagnosis worker disabled")
		return nil, nil
	}
	if runtime == nil || runtime.DiagnosisService == nil {
		return nil, fmt.Errorf("diagnosis worker requires copilot diagnosis service")
	}
	if redisClient == nil || !redisClient.Enabled() {
		return nil, fmt.Errorf("diagnosis worker requires redis")
	}

	var notifier copilotdiagnosis.Notifier
	if cfg.DiagnosisStatusPushEnabled {
		notifier = copilotdiagnosis.NewWebSocketNotifier(hub)
	}
	worker := copilotdiagnosis.NewWorker(copilotdiagnosis.WorkerConfig{
		Service:     runtime.DiagnosisService,
		TaskStore:   copilotdiagnosis.NewRedisTaskStore(redisClient, nil),
		Notifier:    notifier,
		Timeout:     cfg.DiagnosisTaskTimeout,
		TTL:         cfg.DiagnosisTaskTTL,
		Concurrency: cfg.DiagnosisWorkerCount,
		Logger:      zap.L(),
	})
	consumer, err := eventbus.NewConsumer(eventbus.ConsumerConfig{
		Brokers:      cfg.KafkaBrokers,
		GroupID:      cfg.DiagnosisKafkaGroupID,
		RetryBackoff: eventbus.DefaultConsumeRetryBackoff,
	}, worker)
	if err != nil {
		return nil, fmt.Errorf("diagnosis kafka consumer init failed: %w", err)
	}
	consumer.SetRetryableErrors(cfg.DiagnosisRetryableErrors)
	consumer.SetObserver(runtime.KafkaObserver)
	zap.L().Info("diagnosis worker initialized",
		zap.Strings("brokers", cfg.KafkaBrokers),
		zap.String("group_id", cfg.DiagnosisKafkaGroupID),
		zap.Int("worker_count", cfg.DiagnosisWorkerCount),
	)
	return consumer, nil
}

type copilotToolExecutor interface {
	ExecuteTool(ctx context.Context, name string, args json.RawMessage) (copilottool.ToolResult, error)
}

type diagnosisToolRunner struct {
	executor copilotToolExecutor
}

func (r diagnosisToolRunner) ExecuteTool(ctx context.Context, name string, args json.RawMessage) (copilotdiagnosis.ToolResult, error) {
	if r.executor == nil {
		return copilotdiagnosis.ToolResult{Success: false, Error: "tool registry unavailable"}, nil
	}
	if _, ok := copilotservice.UserFromContext(ctx); !ok {
		if user, ok := copilotdiagnosis.UserFromContext(ctx); ok {
			ctx = copilotservice.WithUser(ctx, copilotservice.User{
				ID:       user.ID,
				Username: user.Username,
				Role:     user.Role,
			})
		}
	}
	result, err := r.executor.ExecuteTool(ctx, name, args)
	errorMessage := ""
	if result.Error != nil {
		errorMessage = result.Error.Error()
	}
	return copilotdiagnosis.ToolResult{
		Success: result.Success,
		Data:    result.Data,
		Error:   errorMessage,
	}, err
}

type operationEventProducer struct {
	producer *eventbus.Producer
}

func (p operationEventProducer) SendOperationEvent(event copilotaction.OperationEvent) error {
	if p.producer == nil {
		return nil
	}
	return p.producer.SendOperationEvent(event.ActionType, event)
}

func buildRunbookVectorStore(ctx context.Context, docs []copilotrunbook.Document, embedder *copilotrunbook.Embedder, observers ...copilotrunbook.BuildIndexObserver) (*copilotrunbook.MemoryVectorStore, error) {
	if embedder == nil {
		return nil, nil
	}
	chunks := copilotrunbook.ChunkDocuments(docs)
	store, err := copilotrunbook.BuildMemoryIndex(ctx, embedder, chunks, observers...)
	if err != nil {
		return nil, fmt.Errorf("build runbook vector index: %w", err)
	}
	return store, nil
}

func buildRunbookReranker(cfg config.Config, llmClient *copilotllm.Client) *copilotrunbook.Reranker {
	if !cfg.RerankerEnabled || llmClient == nil {
		return nil
	}
	return copilotrunbook.NewReranker(copilotrunbook.RerankerOptions{
		LLM:     llmClient,
		TopN:    cfg.RerankerTopN,
		Timeout: cfg.RerankerTimeout,
	})
}

type buildIndexLogger struct{}

func (l *buildIndexLogger) ObserveBuildIndexBatchError(batchStart, batchEnd int, err error) {
	zap.L().Warn("runbook embedding batch failed", zap.Int("batch_start", batchStart), zap.Int("batch_end", batchEnd), zap.Error(err))
}
