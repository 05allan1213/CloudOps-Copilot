package legacyworker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	agentadapter "github.com/05allan1213/CloudOps-Copilot/internal/agent/adapter"
	agentllm "github.com/05allan1213/CloudOps-Copilot/internal/agent/llm"
	agentrunbook "github.com/05allan1213/CloudOps-Copilot/internal/agent/runbook"
	agenttool "github.com/05allan1213/CloudOps-Copilot/internal/agent/tool"
	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/argocdread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/githubread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentmysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8schange"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8sread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/registryread"
	agentruntime "github.com/05allan1213/CloudOps-Copilot/internal/service/agentruntime"
	changeintelligence "github.com/05allan1213/CloudOps-Copilot/internal/service/changeintelligence"
)

// InitAgentRuntime assembles the V2 investigation runtime. Shared LLM,
// runbook retrieval, tool contracts, and Kubernetes read adapters remain
// internal implementation details of the Incident Agent.
func InitAgentRuntime(ctx context.Context, cfg config.Config, container *di.Container, k8sDeps k8sread.Deps) (*agentruntime.Worker, error) {
	changeService, changeTools, err := initChangeIntelligence(cfg, container, k8sDeps.Client)
	if err != nil {
		return nil, err
	}
	if changeService != nil {
		container.Handler.SetChangeIntelligence(changeService)
	}
	if !cfg.IncidentAgentEnabled {
		return nil, nil
	}
	if container.DB == nil {
		return nil, fmt.Errorf("incident agent runtime requires MySQL")
	}

	runbookDocs, err := agentrunbook.LoadDir(context.Background(), cfg.RunbookDir, agentrunbook.LoadOptions{
		MaxFiles:     cfg.RunbookMaxFiles,
		MaxFileBytes: cfg.RunbookMaxFileBytes,
	})
	if err != nil {
		return nil, err
	}

	llmClient := agentllm.NewClient(agentllm.Options{
		APIKey:    cfg.LLMAPIKey,
		APIURL:    cfg.LLMAPIURL,
		Model:     cfg.LLMModel,
		Timeout:   cfg.LLMTimeout,
		MaxTokens: cfg.LLMMaxTokens,
		Observer:  container.Metrics,
	})
	runbookEmbedder, runbookVectorStore := initRunbookEmbedding(ctx, cfg, runbookDocs)
	runbookRetriever := agentrunbook.NewRetriever(runbookDocs, agentrunbook.RetrieverOptions{
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

	toolExecutor, err := agenttool.NewExecutor(agenttool.Options{
		AlertService:    container.AlertService,
		PromClient:      container.PromClient,
		RunbookSearcher: runbookRetriever,
		K8sReader:       k8sDeps.Reader,
		DB:              container.DB,
		Observer:        container.Metrics,
		Timeout:         cfg.AgentToolDefaultTimeout,
		LogArgs:         cfg.AgentToolLogArgs,
		AdditionalTools: changeTools,
	})
	if err != nil {
		return nil, err
	}
	store, err := incidentmysql.NewStore(container.DB)
	if err != nil {
		return nil, fmt.Errorf("incident agent store init failed: %w", err)
	}
	zeroRetries := 0
	var model agent.Model
	modelName, promptVersion := cfg.LLMModel, "incident-agent-v3-change-readonly"
	if cfg.FastDemoEnabled {
		model = agentadapter.NewDemoModel()
		modelName, promptVersion = "fast-demo-deterministic", "incident-agent-fast-demo-v1"
	} else {
		agentLLM := agentllm.NewClient(agentllm.Options{APIKey: cfg.LLMAPIKey, APIURL: cfg.LLMAPIURL, Model: cfg.LLMModel, Timeout: cfg.LLMTimeout, MaxTokens: cfg.LLMMaxTokens, MaxRetries: &zeroRetries, Observer: container.Metrics})
		llmModel, modelErr := agentadapter.NewLLMModel(agentLLM)
		if modelErr != nil {
			return nil, fmt.Errorf("incident agent model init failed: %w", modelErr)
		}
		model = llmModel
	}
	readOnlyTools, err := agentadapter.NewReadOnlyTools(toolExecutor)
	if err != nil {
		return nil, fmt.Errorf("incident agent read-only tools init failed: %w", err)
	}
	runtimeConfig := agentRuntimeConfig(cfg, container)
	runtimeConfig.Model = modelName
	runtimeConfig.PromptVersion = promptVersion
	agentService, err := agentruntime.New(ctx, store, model, readOnlyTools, runtimeConfig)
	if err != nil {
		return nil, fmt.Errorf("incident agent runtime init failed: %w", err)
	}
	zap.L().Info("incident agent worker initialized", zap.String("worker_id", cfg.IncidentAgentWorkerID))
	container.Handler.SetAgentRuntime(agentService)
	return agentService.NewWorker(), nil
}

func initChangeIntelligence(cfg config.Config, container *di.Container, k8sClient kubernetes.Interface) (*changeintelligence.Service, []agenttool.Tool, error) {
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
		container.ChangeGitHub = githubClient
	}
	var argoClient change.ArgoCDReader
	if cfg.ArgoCDEnabled {
		argoClient, err = argocdread.New(argocdread.Config{Server: cfg.ArgoCDServer, TokenFile: cfg.ArgoCDTokenFile, AllowedApplications: cfg.ArgoCDAllowedApplications, AllowedProjects: cfg.ArgoCDAllowedProjects, Timeout: cfg.ArgoCDTimeout, MaxRetries: 1, MaxResources: cfg.ArgoCDMaxResources, MaxDiffBytes: cfg.ArgoCDMaxDiffBytes, Observer: container.Metrics})
		if err != nil {
			return nil, nil, fmt.Errorf("argo CD read adapter init failed: %w", err)
		}
		container.ChangeArgoCD = argoClient
	}
	var runtimeReader change.RuntimeReader
	if k8sClient != nil {
		reader, readerErr := k8schange.New(k8sClient, cfg.K8SAllowedNamespaces, cfg.K8SRequestTimeout)
		err = readerErr
		runtimeReader = reader
		if err != nil {
			return nil, nil, fmt.Errorf("runtime image reader init failed: %w", err)
		}
		container.DeliveryRollout = reader
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
	tools := agenttool.NewPhase3ReadOnlyTools(agenttool.ChangeToolConfig{Service: service, GitHub: githubClient, ArgoCD: argoClient, Timeout: cfg.IncidentAgentToolTimeout})
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

func initRunbookEmbedding(ctx context.Context, cfg config.Config, docs []agentrunbook.Document) (*agentrunbook.Embedder, *agentrunbook.MemoryVectorStore) {
	if cfg.EmbeddingAPIURL == "" || cfg.EmbeddingAPIKey == "" || cfg.EmbeddingModel == "" {
		return nil, nil
	}
	embedder := agentrunbook.NewEmbedder(agentrunbook.EmbedderOptions{
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

func buildRunbookVectorStore(ctx context.Context, docs []agentrunbook.Document, embedder *agentrunbook.Embedder, observers ...agentrunbook.BuildIndexObserver) (*agentrunbook.MemoryVectorStore, error) {
	if embedder == nil {
		return nil, nil
	}
	chunks := agentrunbook.ChunkDocuments(docs)
	store, err := agentrunbook.BuildMemoryIndex(ctx, embedder, chunks, observers...)
	if err != nil {
		return nil, fmt.Errorf("build runbook vector index: %w", err)
	}
	return store, nil
}

func buildRunbookReranker(cfg config.Config, llmClient *agentllm.Client) *agentrunbook.Reranker {
	if !cfg.RerankerEnabled || llmClient == nil {
		return nil
	}
	return agentrunbook.NewReranker(agentrunbook.RerankerOptions{
		LLM:     llmClient,
		TopN:    cfg.RerankerTopN,
		Timeout: cfg.RerankerTimeout,
	})
}

type buildIndexLogger struct{}

func (l *buildIndexLogger) ObserveBuildIndexBatchError(batchStart, batchEnd int, err error) {
	zap.L().Warn("runbook embedding batch failed", zap.Int("batch_start", batchStart), zap.Int("batch_end", batchEnd), zap.Error(err))
}
