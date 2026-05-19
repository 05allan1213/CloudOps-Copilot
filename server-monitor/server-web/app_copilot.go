package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"

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
	rediscache "server-web/internal/infra/redis"
	ws "server-web/internal/infra/websocket"
	"server-web/internal/middleware"
	"server-web/internal/router"
	appalert "server-web/internal/service/alert"

	eventbus "server-monitor/pkg/kafka"
)

func initCopilot(ctx context.Context, cfg config.Config, container *di.Container, k8sDeps copilotk8s.Deps) (*router.CopilotRuntime, *router.CopilotDeps, error) {
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
	return &router.CopilotRuntime{DiagnosisService: diagnosisService, KafkaObserver: container.Metrics}, deps, nil
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

func initDiagnosisConsumer(cfg config.Config, redisClient *rediscache.Client, runtime *router.CopilotRuntime, hub *ws.Hub) (*eventbus.Consumer, error) {
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
