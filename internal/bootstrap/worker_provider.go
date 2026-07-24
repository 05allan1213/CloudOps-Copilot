package bootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	agentadapter "github.com/05allan1213/CloudOps-Copilot/internal/agent/adapter"
	agentllm "github.com/05allan1213/CloudOps-Copilot/internal/agent/llm"
	"github.com/05allan1213/CloudOps-Copilot/internal/agent/runbook"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/configutil"
	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	appconfig "github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/argocdread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/deliveryread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/githubread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/githubwrite"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/investigationread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8schange"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8sread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/observabilityread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/registryread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/remediationmysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/verificationread"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

type V3WorkerTargetConfig struct {
	Cluster, Environment, Namespace string
	Service, Workload, Container    string
	RepositoryOwner, RepositoryName string
	BaseBranch, GitOpsPath          string
	ArgoPath                        string
	ArgoApplication, ArgoProject    string
	ArgoRepositoryURL               string
	RequiredEnvKey                  string
	ReadyURL                        string
	GrafanaURL, KibanaURL, TempoURL string
}

func (t V3WorkerTargetConfig) repository() string { return t.RepositoryOwner + "/" + t.RepositoryName }

type V3WorkerProviderConfig struct {
	Application appconfig.Config
	Target      V3WorkerTargetConfig

	LLMAPIKeyFile string

	ElasticsearchURL          string
	ElasticsearchIndex        string
	ElasticsearchBearerFile   string
	ElasticsearchUsernameFile string
	ElasticsearchPasswordFile string
	ElasticsearchCAFile       string

	RequiredCheckName  string
	CheckProducerAppID int64
	WorkflowID         int64
	WorkflowPath       string

	PlanTTL            time.Duration
	MaxCheckpointBytes int
}

func LoadV3WorkerProviderConfig(application appconfig.Config) (V3WorkerProviderConfig, error) {
	repository := strings.Trim(strings.TrimSpace(os.Getenv("V3_TARGET_REPOSITORY")), "/")
	owner, name := "", ""
	if parts := strings.Split(repository, "/"); len(parts) == 2 {
		owner, name = parts[0], parts[1]
	}
	result := V3WorkerProviderConfig{
		Application: application,
		Target: V3WorkerTargetConfig{
			Cluster: configutil.String("V3_TARGET_CLUSTER", "kind-cloudops-v3"), Environment: configutil.String("V3_TARGET_ENVIRONMENT", "local-demo"),
			Namespace: configutil.String("V3_TARGET_NAMESPACE", "demo"), Service: configutil.String("V3_TARGET_SERVICE", "demo"),
			Workload: configutil.String("V3_TARGET_WORKLOAD", "demo"), Container: configutil.String("V3_TARGET_CONTAINER", "demo"),
			RepositoryOwner: owner, RepositoryName: name, BaseBranch: configutil.String("V3_TARGET_BASE_BRANCH", "main"),
			GitOpsPath: configutil.String("V3_TARGET_GITOPS_PATH", ""), ArgoPath: configutil.String("V3_TARGET_ARGO_PATH", ""),
			ArgoApplication: configutil.String("V3_TARGET_ARGO_APPLICATION", "cloudops-demo"),
			ArgoProject:     configutil.String("V3_TARGET_ARGO_PROJECT", "cloudops-demo"), ArgoRepositoryURL: configutil.String("V3_TARGET_ARGO_REPOSITORY_URL", ""),
			RequiredEnvKey: configutil.String("V3_TARGET_REQUIRED_ENV_KEY", "REQUIRED_ENV"),
			ReadyURL:       configutil.String("V3_TARGET_READY_URL", "http://demo-diagnostics.demo.svc:8080/readyz"),
			GrafanaURL:     configutil.String("V3_GRAFANA_URL", ""), KibanaURL: configutil.String("V3_KIBANA_URL", ""), TempoURL: configutil.String("V3_TEMPO_URL", ""),
		},
		LLMAPIKeyFile:    configutil.String("V3_LLM_API_KEY_FILE", ""),
		ElasticsearchURL: configutil.String("OBSERVABILITY_ELASTICSEARCH_URL", ""), ElasticsearchIndex: configutil.String("OBSERVABILITY_ELASTICSEARCH_INDEX", "logs-cloudops-*"),
		ElasticsearchBearerFile: configutil.String("OBSERVABILITY_ELASTICSEARCH_BEARER_TOKEN_FILE", ""), ElasticsearchUsernameFile: configutil.String("OBSERVABILITY_ELASTICSEARCH_USERNAME_FILE", ""),
		ElasticsearchPasswordFile: configutil.String("OBSERVABILITY_ELASTICSEARCH_PASSWORD_FILE", ""), ElasticsearchCAFile: configutil.String("OBSERVABILITY_ELASTICSEARCH_CA_FILE", ""),
		RequiredCheckName: configutil.String("V3_REQUIRED_CHECK_NAME", "gitops-required-check"), CheckProducerAppID: int64(configutil.NonNegativeInt("V3_REQUIRED_CHECK_APP_ID", 0)),
		WorkflowID: int64(configutil.NonNegativeInt("V3_REQUIRED_WORKFLOW_ID", 0)), WorkflowPath: configutil.String("V3_REQUIRED_WORKFLOW_PATH", ".github/workflows/gitops-required-check.yml"),
		PlanTTL: configutil.DurationSeconds("V3_REMEDIATION_PLAN_TTL_SECONDS", 1800), MaxCheckpointBytes: configutil.PositiveInt("V3_INVESTIGATION_MAX_CHECKPOINT_BYTES", 64*1024),
	}
	if err := result.Validate(); err != nil {
		return V3WorkerProviderConfig{}, err
	}
	return result, nil
}

func (c V3WorkerProviderConfig) Validate() error {
	t := c.Target
	for name, value := range map[string]string{
		"cluster": t.Cluster, "environment": t.Environment, "namespace": t.Namespace, "service": t.Service,
		"workload": t.Workload, "container": t.Container, "repository owner": t.RepositoryOwner,
		"repository name": t.RepositoryName, "base branch": t.BaseBranch, "GitOps path": t.GitOpsPath, "Argo path": t.ArgoPath,
		"Argo application": t.ArgoApplication, "Argo project": t.ArgoProject, "Argo repository URL": t.ArgoRepositoryURL,
		"required env key": t.RequiredEnvKey, "ready URL": t.ReadyURL,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("V3 worker target %s is required", name)
		}
	}
	argoPath := strings.Trim(strings.TrimSpace(t.ArgoPath), "/")
	gitOpsPath := strings.Trim(strings.TrimSpace(t.GitOpsPath), "/")
	if argoPath == gitOpsPath || argoPath != strings.TrimSpace(t.ArgoPath) || gitOpsPath != strings.TrimSpace(t.GitOpsPath) ||
		strings.Contains(argoPath, "..") || strings.Contains(gitOpsPath, "..") {
		return errors.New("V3 worker Argo directory and GitOps file paths must be distinct bounded paths")
	}
	if !slices.Contains(c.Application.K8SAllowedNamespaces, t.Namespace) && !slices.Contains(c.Application.K8SAllowedNamespaces, "*") {
		return errors.New("V3 worker target namespace is not in K8S_ALLOWED_NAMESPACES")
	}
	if c.Application.K8SRequestTimeout <= 0 || c.Application.LLMTimeout <= 0 || c.Application.LLMMaxTokens <= 0 ||
		c.Application.ObservabilityRequestTimeout <= 0 || c.Application.ObservabilityMaxLookback < 30*time.Minute {
		return errors.New("V3 worker provider timeouts and observability bounds are invalid")
	}
	if strings.TrimSpace(c.Application.LLMAPIURL) == "" || strings.TrimSpace(c.Application.LLMProvider) == "" || strings.TrimSpace(c.Application.LLMModel) == "" ||
		(strings.TrimSpace(c.Application.LLMAPIKey) == "" && strings.TrimSpace(c.LLMAPIKeyFile) == "") {
		return errors.New("V3 worker requires one configured LLM credential source")
	}
	if err := validateProviderURL("LLM_API_URL", c.Application.LLMAPIURL, true); err != nil {
		return err
	}
	if err := validateGitHubRead(c.Application, t.repository(), t.BaseBranch, t.GitOpsPath); err != nil {
		return err
	}
	if err := validateGitHubWrite(c.Application, t.repository(), t.BaseBranch, t.GitOpsPath); err != nil {
		return err
	}
	if c.Application.GitHubPrivateKeyFile != "" && c.Application.GitHubPrivateKeyFile == c.Application.GitHubWritePrivateKeyFile ||
		c.Application.GitHubTokenFile != "" && c.Application.GitHubTokenFile == c.Application.GitHubWriteTokenFile {
		return errors.New("V3 GitHub read and write credentials must be isolated")
	}
	if err := validateProviderURL("ARGOCD_SERVER", c.Application.ArgoCDServer, true); err != nil {
		return err
	}
	if strings.TrimSpace(c.Application.ArgoCDTokenFile) == "" || !slices.Contains(c.Application.ArgoCDAllowedApplications, t.ArgoApplication) || !slices.Contains(c.Application.ArgoCDAllowedProjects, t.ArgoProject) {
		return errors.New("V3 Argo target and credential file must be allowlisted")
	}
	if err := validateProviderURL("REGISTRY_BASE_URL", c.Application.RegistryBaseURL, true); err != nil {
		return err
	}
	if len(c.Application.RegistryAllowedHosts) == 0 || len(c.Application.RegistryAllowedRepos) == 0 || len(c.Application.OCIAllowedSources) == 0 {
		return errors.New("V3 Registry identity allowlists are required")
	}
	for name, raw := range map[string]string{
		"OBSERVABILITY_PROMETHEUS_URL":    c.Application.ObservabilityPrometheusURL,
		"OBSERVABILITY_ELASTICSEARCH_URL": c.ElasticsearchURL,
		"OBSERVABILITY_TEMPO_URL":         c.Application.ObservabilityTempoURL,
	} {
		if err := validateProviderURL(name, raw, false); err != nil {
			return err
		}
	}
	if c.ElasticsearchIndex != "logs-cloudops-*" || strings.TrimSpace(c.Application.RunbookDir) == "" {
		return errors.New("V3 worker requires the fixed Elasticsearch index and Git-managed runbook directory")
	}
	if c.RequiredCheckName == "" || c.CheckProducerAppID <= 0 || c.WorkflowID <= 0 ||
		!strings.HasPrefix(c.WorkflowPath, ".github/workflows/") || (!strings.HasSuffix(c.WorkflowPath, ".yaml") && !strings.HasSuffix(c.WorkflowPath, ".yml")) {
		return errors.New("V3 worker required-check App and workflow identity is incomplete")
	}
	if c.PlanTTL <= 0 || c.PlanTTL > 24*time.Hour || c.MaxCheckpointBytes < 1024 || c.MaxCheckpointBytes > 128*1024 {
		return errors.New("V3 worker Plan TTL or checkpoint bound is invalid")
	}
	return nil
}

func validateProviderURL(name, raw string, requireHTTPS bool) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return fmt.Errorf("%s is not a fixed allowed endpoint", name)
	}
	invalidScheme := parsed.Scheme != "https" && parsed.Scheme != "http"
	if requireHTTPS {
		invalidScheme = parsed.Scheme != "https"
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || invalidScheme {
		return fmt.Errorf("%s is not a fixed allowed endpoint", name)
	}
	return nil
}

func validateGitHubRead(c appconfig.Config, repository, branch, path string) error {
	if err := validateProviderURL("GITHUB_API_BASE_URL", c.GitHubAPIBaseURL, true); err != nil {
		return err
	}
	appAuth := c.GitHubAppID > 0 && c.GitHubInstallationID > 0 && c.GitHubPrivateKeyFile != ""
	fileAuth := c.GitHubTokenFile != ""
	if appAuth == fileAuth || !slices.Contains(c.GitHubAllowedRepositories, repository) || !slices.Contains(c.GitHubAllowedBranches, branch) || !slices.Contains(c.GitHubAllowedPaths, path) {
		return errors.New("V3 GitHub read auth and target allowlists are incomplete")
	}
	return nil
}

func validateGitHubWrite(c appconfig.Config, repository, branch, path string) error {
	if err := validateProviderURL("GITHUB_WRITE_API_BASE_URL", c.GitHubWriteAPIBaseURL, true); err != nil {
		return err
	}
	appAuth := c.GitHubWriteAppID > 0 && c.GitHubWriteInstallationID > 0 && c.GitHubWritePrivateKeyFile != ""
	fileAuth := c.GitHubWriteTokenFile != ""
	if appAuth == fileAuth || !slices.Contains(c.GitHubWriteAllowedRepositories, repository) || !slices.Contains(c.GitHubWriteAllowedBaseBranches, branch) || !slices.Contains(c.GitHubWriteAllowedPaths, path) {
		return errors.New("V3 GitHub write auth and target allowlists are incomplete")
	}
	return nil
}

type ProductionTaskOperationFactory struct{ Config V3WorkerProviderConfig }

func (f ProductionTaskOperationFactory) Validate() error { return f.Config.Validate() }

func (f ProductionTaskOperationFactory) Build(ctx context.Context, db *sql.DB, tasks *asyncjob.Repository) (taskhandler.Config, error) {
	if err := f.Validate(); err != nil {
		return taskhandler.Config{}, err
	}
	if db == nil || tasks == nil {
		return taskhandler.Config{}, errors.New("V3 production factory requires MySQL and the unified task repository")
	}
	c, target := f.Config.Application, f.Config.Target

	kubernetesClient, err := k8sread.NewClient(k8sread.Config{Enabled: true, InCluster: c.K8SInCluster, Kubeconfig: c.K8SKubeconfig, AllowedNamespaces: c.K8SAllowedNamespaces, DefaultNamespace: target.Namespace, RequestTimeout: c.K8SRequestTimeout})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 Kubernetes client: %w", err)
	}
	runtimeReader, err := k8schange.New(kubernetesClient, c.K8SAllowedNamespaces, c.K8SRequestTimeout)
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 Kubernetes identity reader: %w", err)
	}

	githubClient, err := buildGitHubRead(c, target.repository())
	if err != nil {
		return taskhandler.Config{}, err
	}
	githubWriter, err := buildGitHubWrite(c, target.repository())
	if err != nil {
		return taskhandler.Config{}, err
	}
	argoClient, err := argocdread.New(argocdread.Config{Server: c.ArgoCDServer, TokenFile: c.ArgoCDTokenFile, AllowedApplications: c.ArgoCDAllowedApplications, AllowedProjects: c.ArgoCDAllowedProjects, Timeout: c.ArgoCDTimeout, MaxRetries: 1, MaxResources: c.ArgoCDMaxResources, MaxDiffBytes: c.ArgoCDMaxDiffBytes})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 Argo reader: %w", err)
	}
	registryClient, err := registryread.New(registryread.Config{
		BaseURL: c.RegistryBaseURL, AllowedHosts: c.RegistryAllowedHosts, AllowedRepositories: c.RegistryAllowedRepos,
		AllowedAuthRealms: c.RegistryAllowedAuthRealms, AllowedRedirectHosts: c.RegistryAllowedRedirects,
		BearerTokenFile: c.RegistryBearerTokenFile, UsernameFile: c.RegistryUsernameFile, PasswordFile: c.RegistryPasswordFile,
		Timeout: c.RegistryTimeout, MaxRetries: c.RegistryMaxRetries, ManifestMaxBytes: c.RegistryManifestMaxBytes,
		ConfigMaxBytes: c.RegistryConfigMaxBytes, CacheTTL: c.RegistryCacheTTL, CacheMaxItems: c.RegistryCacheMaxItems,
	})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 Registry reader: %w", err)
	}

	services, namespaces, environments := oneSet(target.Service), oneSet(target.Namespace), oneSet(target.Environment)
	baseObservability := observabilityread.Config{
		Timeout: c.ObservabilityRequestTimeout, MaxResponseBytes: c.ObservabilityMaxResponseBytes,
		MaxSamples: c.ObservabilityMaxSamples, MaxSeries: c.ObservabilityMaxSeries, MaxTraces: c.ObservabilityMaxTraces,
		MaxLookback: c.ObservabilityMaxLookback, Retries: c.ObservabilityMaxRetries,
		AllowedServices: services, AllowedNamespaces: namespaces, AllowedEnvironments: environments,
	}
	prometheusConfig := baseObservability
	prometheusConfig.BaseURL, prometheusConfig.TokenFile = c.ObservabilityPrometheusURL, c.ObservabilityPromTokenFile
	prometheusClient, err := observabilityread.NewPrometheus(prometheusConfig)
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 Prometheus reader: %w", err)
	}
	tempoConfig := baseObservability
	tempoConfig.BaseURL, tempoConfig.TokenFile = c.ObservabilityTempoURL, c.ObservabilityTempoTokenFile
	tempoClient, err := observabilityread.NewTempo(tempoConfig)
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 Tempo reader: %w", err)
	}
	elasticClient, err := observabilityread.NewElasticsearch(observabilityread.ElasticConfig{
		BaseURL: f.Config.ElasticsearchURL, KibanaURL: target.KibanaURL, IndexPattern: f.Config.ElasticsearchIndex,
		BearerTokenFile: f.Config.ElasticsearchBearerFile, UsernameFile: f.Config.ElasticsearchUsernameFile,
		PasswordFile: f.Config.ElasticsearchPasswordFile, CAFile: f.Config.ElasticsearchCAFile,
		Timeout: c.ObservabilityRequestTimeout, MaxResponseBytes: c.ObservabilityMaxResponseBytes,
		MaxSamples: c.ObservabilityMaxSamples, MaxLookback: c.ObservabilityMaxLookback,
		AllowedServices: services, AllowedNamespaces: namespaces, AllowedEnvironments: environments,
	})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 Elasticsearch reader: %w", err)
	}

	documents, err := runbook.LoadDir(ctx, c.RunbookDir, runbook.LoadOptions{MaxFiles: c.RunbookMaxFiles, MaxFileBytes: c.RunbookMaxFileBytes})
	if err != nil || len(documents) == 0 {
		return taskhandler.Config{}, fmt.Errorf("load V3 runbooks: %w", errors.Join(err, errors.New("at least one Git-managed runbook is required")))
	}
	retriever := runbook.NewRetriever(documents, runbook.RetrieverOptions{DefaultLimit: min(c.RunbookSearchTopN, 3), MaxLimit: 3, BM25Weight: 1, BM25K1: c.RunbookBM25K1, BM25B: c.RunbookBM25B, RRFK: c.RunbookRRFK})
	investigationTools, err := investigationread.New(investigationread.Config{
		DB: db, Kubernetes: kubernetesClient, Prometheus: prometheusClient, Elasticsearch: elasticClient, Tempo: tempoClient,
		GitHub: githubClient, Argo: argoClient, Runtime: runtimeReader, Registry: registryClient, Runbooks: retriever,
		Target: investigationread.Target{
			Service: target.Service, Cluster: target.Cluster, Environment: target.Environment, Namespace: target.Namespace,
			Workload: target.Workload, Container: target.Container, Repository: changeRepository(target), BaseBranch: target.BaseBranch,
			GitOpsPath: target.GitOpsPath, ArgoPath: target.ArgoPath, ArgoApplication: target.ArgoApplication, ArgoProject: target.ArgoProject,
			EnvKey: target.RequiredEnvKey, GrafanaURL: target.GrafanaURL, KibanaURL: target.KibanaURL, TempoURL: target.TempoURL,
		}, RequestTimeout: c.K8SRequestTimeout,
	})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 investigation tools: %w", err)
	}

	verificationSource, err := verificationread.New(verificationread.Config{
		DB: db, Prometheus: prometheusClient, Elasticsearch: elasticClient, Tempo: tempoClient,
		Argo: argoClient, Rollout: runtimeReader, Runtime: runtimeReader, Registry: registryClient,
		Target: verificationread.Target{Cluster: target.Cluster, Environment: target.Environment, Namespace: target.Namespace,
			Service: target.Service, Workload: target.Workload, Container: target.Container,
			ArgoApplication: target.ArgoApplication, ArgoProject: target.ArgoProject, ReadyURL: target.ReadyURL},
	})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 verification source: %w", err)
	}

	policy := v3RestoreEnvPolicy(target)
	policyHash, err := restorePolicyHash(policy)
	if err != nil {
		return taskhandler.Config{}, err
	}
	remediationRepository, err := remediationmysql.NewV3RemediationRepository(db)
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 remediation repository: %w", err)
	}
	remediationLoader, err := taskhandler.NewMySQLRemediationPrepareLoader(db, githubClient, taskhandler.MySQLRemediationPrepareLoaderConfig{Policy: policy, PlanTTL: f.Config.PlanTTL})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 remediation loader: %w", err)
	}
	remediationStore, err := taskhandler.NewMySQLRemediationPrepareStore(remediationRepository)
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 remediation store: %w", err)
	}

	v3GitHub, err := deliveryread.NewV3GitHub(deliveryread.V3GitHubConfig{Reader: githubClient, RequiredCheckName: f.Config.RequiredCheckName, ProducerAppID: f.Config.CheckProducerAppID, WorkflowID: f.Config.WorkflowID, WorkflowPath: f.Config.WorkflowPath})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 delivery GitHub reader: %w", err)
	}
	deliveryObserver, err := deliveryread.NewV3Observer(v3GitHub, argoClient, runtimeReader, runtimeReader, registryClient)
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 delivery observer: %w", err)
	}

	apiKey, err := f.Config.llmAPIKey()
	if err != nil {
		return taskhandler.Config{}, err
	}
	zeroRetries := 0
	modelClient := agentllm.NewClient(agentllm.Options{APIKey: apiKey, APIURL: c.LLMAPIURL, Model: c.LLMModel, Timeout: c.LLMTimeout, MaxTokens: c.LLMMaxTokens, MaxRetries: &zeroRetries})
	model, err := agentadapter.NewLLMModel(modelClient)
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("initialize V3 investigation model: %w", err)
	}
	actionPolicies := investigationread.GoldenActionPolicies()
	modelIdentity, err := model.RuntimeModelIdentity(c.LLMProvider, actionPolicies)
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("freeze V3 investigation model identity: %w", err)
	}

	return AssembleWorkerTaskOperations(db, tasks, WorkerOperationConfig{
		ClaimPolicy: agent.GoldenRequiredEnvClaimPolicy(), ActionPolicies: actionPolicies, AgentRunIdentity: modelIdentity,
		RequiredSources: investigationread.RequiredSources(), MaxCheckpointBytes: f.Config.MaxCheckpointBytes,
		CurrentPolicyHash:    policyHash,
		DeliveryTarget:       taskhandler.DeliveryObserveTarget{ArgoApplication: target.ArgoApplication, ArgoProject: target.ArgoProject, ArgoRepository: target.ArgoRepositoryURL, ArgoPath: target.ArgoPath, DesiredReplicas: 2},
		DeliveryPollInterval: c.DeliveryPollInterval, DeliveryTimeout: c.DeliveryTimeout, MaxAgentRuns: taskhandler.DefaultAgentRunBudget,
	}, WorkerOperationDependencies{
		InvestigationModel: model, InvestigationTools: investigationTools,
		RemediationLoader: remediationLoader, RemediationStore: remediationStore,
		GitHubReader: githubClient, GitHubWriter: githubWriter,
		DeliveryObserver: deliveryObserver, VerificationObservations: verificationSource,
		ResolutionReports: taskhandler.NewMySQLResolutionReportWriter(),
	})
}

func v3RestoreEnvPolicy(target V3WorkerTargetConfig) remediation.RestoreEnvPolicy {
	return remediation.RestoreEnvPolicy{
		Version: remediation.RestoreRequiredEnvPolicyVersion, Repository: target.repository(), BaseBranch: target.BaseBranch,
		AllowedPath: target.GitOpsPath, APIVersion: "apps/v1", Namespace: target.Namespace,
		Workload: target.Workload, Container: target.Container, EnvKey: target.RequiredEnvKey,
		MaxDiffBytes: remediation.MaxV3PlanDiffBytes, MaxPostImageBytes: remediation.MaxV3PostImageBytes,
		VerificationVersion: verification.GoldenRequiredEnvProfileID,
	}
}

func buildGitHubRead(c appconfig.Config, repository string) (*githubread.Client, error) {
	var token githubread.TokenProvider
	var err error
	if c.GitHubTokenFile != "" {
		token = githubread.FileTokenProvider{Path: c.GitHubTokenFile}
	} else {
		token, err = githubread.NewAppTokenProvider(githubread.AppTokenConfig{BaseURL: c.GitHubAPIBaseURL, AppID: c.GitHubAppID, InstallationID: c.GitHubInstallationID, PrivateKeyFile: c.GitHubPrivateKeyFile, APIVersion: "2022-11-28", AllowedRepositories: []string{strings.Split(repository, "/")[1]}})
		if err != nil {
			return nil, fmt.Errorf("initialize V3 GitHub read auth: %w", err)
		}
	}
	client, err := githubread.New(githubread.Config{BaseURL: c.GitHubAPIBaseURL, TokenProvider: token, AllowedRepositories: []string{repository}, AllowedBranches: c.GitHubAllowedBranches, AllowedPaths: c.GitHubAllowedPaths, DeniedPathPatterns: c.GitHubDeniedPathPatterns, Timeout: c.GitHubTimeout, MaxRetries: c.GitHubMaxRetries, MaxPages: 3, MaxResponseBytes: 2 * 1024 * 1024, MaxDiffFiles: c.GitHubMaxDiffFiles, MaxPatchFiles: c.GitHubMaxDiffFiles, MaxPatchBytesPerFile: 16 * 1024, MaxDiffBytes: c.GitHubMaxDiffBytes, APIVersion: "2022-11-28"})
	if err != nil {
		return nil, fmt.Errorf("initialize V3 GitHub read client: %w", err)
	}
	return client, nil
}

func buildGitHubWrite(c appconfig.Config, repository string) (*githubwrite.Client, error) {
	var token githubwrite.TokenProvider
	var err error
	if c.GitHubWriteTokenFile != "" {
		token = githubwrite.FileTokenProvider{Path: c.GitHubWriteTokenFile}
	} else {
		token, err = githubwrite.NewAppTokenProvider(githubwrite.AppTokenConfig{BaseURL: c.GitHubWriteAPIBaseURL, AppID: c.GitHubWriteAppID, InstallationID: c.GitHubWriteInstallationID, PrivateKeyFile: c.GitHubWritePrivateKeyFile, AllowedRepositories: []string{strings.Split(repository, "/")[1]}})
		if err != nil {
			return nil, fmt.Errorf("initialize V3 GitHub write auth: %w", err)
		}
	}
	client, err := githubwrite.New(githubwrite.Config{BaseURL: c.GitHubWriteAPIBaseURL, TokenProvider: token, AllowedRepositories: []string{repository}, AllowedBaseBranches: c.GitHubWriteAllowedBaseBranches, AllowedPaths: c.GitHubWriteAllowedPaths, Timeout: c.GitHubWriteTimeout, MaxResponseBytes: c.GitHubWriteMaxResponseBytes, MaxContentBytes: c.GitHubWriteMaxContentBytes})
	if err != nil {
		return nil, fmt.Errorf("initialize V3 GitHub write client: %w", err)
	}
	return client, nil
}

func restorePolicyHash(policy remediation.RestoreEnvPolicy) (string, error) {
	payload, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
func oneSet(value string) map[string]struct{} { return map[string]struct{}{value: {}} }
func changeRepository(target V3WorkerTargetConfig) change.RepositoryRef {
	return change.RepositoryRef{Owner: target.RepositoryOwner, Name: target.RepositoryName}
}

func (c V3WorkerProviderConfig) llmAPIKey() (string, error) {
	if strings.TrimSpace(c.LLMAPIKeyFile) == "" {
		return strings.TrimSpace(c.Application.LLMAPIKey), nil
	}
	data, err := os.ReadFile(c.LLMAPIKeyFile)
	if err != nil || len(data) == 0 || len(data) > 16*1024 {
		return "", fmt.Errorf("read V3 LLM key file: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", errors.New("V3 LLM key file is empty")
	}
	return value, nil
}

func strictProviderFlag() (bool, error) {
	raw, exists := os.LookupEnv("V3_WORKER_PROVIDERS_ENABLED")
	if !exists || strings.TrimSpace(raw) == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("V3_WORKER_PROVIDERS_ENABLED must be a boolean: %w", err)
	}
	return value, nil
}
