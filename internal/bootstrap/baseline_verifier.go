package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/05allan1213/CloudOps-Copilot/internal/baseline"
	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/configutil"
	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/logger"
	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	appconfig "github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/argocdread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/baselinemysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/database"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/githubread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8schange"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8sread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/observabilityread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/registryread"
)

var forbiddenBaselineCredentialEnv = []string{
	"LLM_API_KEY", "LLM_API_KEY_FILE",
	"GITHUB_APP_ID", "GITHUB_INSTALLATION_ID", "GITHUB_PRIVATE_KEY_FILE", "GITHUB_TOKEN_FILE",
	"GITHUB_WRITE_APP_ID", "GITHUB_WRITE_INSTALLATION_ID", "GITHUB_WRITE_PRIVATE_KEY_FILE", "GITHUB_WRITE_TOKEN_FILE",
}

// BaselineVerifierConfig is intentionally narrower than the production Worker
// provider config. The one-shot Job has no LLM or GitHub App credential and
// can only read the single deployment target before activating a baseline.
type BaselineVerifierConfig struct {
	Application appconfig.Config
	Target      baseline.Target
	Service     string

	ArgoPath              string
	ArgoDestinationServer string
	ArgoApplication       string
	ArgoProject           string
	SourceRepository      change.RepositoryRef
	AlertNames            []string
	Lookback              time.Duration
	CommandTimeout        time.Duration

	ElasticsearchURL          string
	ElasticsearchIndex        string
	ElasticsearchBearerFile   string
	ElasticsearchUsernameFile string
	ElasticsearchPasswordFile string
	ElasticsearchCAFile       string
}

func LoadBaselineVerifierConfig() (BaselineVerifierConfig, error) {
	if err := rejectBaselineCredentials(); err != nil {
		return BaselineVerifierConfig{}, err
	}
	application := appconfig.Load()
	gitopsRepository, err := splitRepository(configutil.String("OPERATIONAL_TARGET_REPOSITORY", ""))
	if err != nil {
		return BaselineVerifierConfig{}, fmt.Errorf("OPERATIONAL_TARGET_REPOSITORY: %w", err)
	}
	sourceRepository, err := splitRepository(configutil.String("BASELINE_SOURCE_REPOSITORY", ""))
	if err != nil {
		return BaselineVerifierConfig{}, fmt.Errorf("BASELINE_SOURCE_REPOSITORY: %w", err)
	}
	alerts := configutil.List("BASELINE_ALERT_NAMES")
	if len(alerts) == 0 {
		alerts = []string{"CloudOpsDemoRequiredEnvMissing", "CloudOpsDemoErrorRateHigh"}
	}
	result := BaselineVerifierConfig{
		Application: application,
		Target: baseline.Target{
			Cluster: configutil.String("OPERATIONAL_TARGET_CLUSTER", "cloudops-local"), Environment: configutil.String("OPERATIONAL_TARGET_ENVIRONMENT", "local-demo"),
			Namespace: configutil.String("OPERATIONAL_TARGET_NAMESPACE", "demo"), WorkloadKind: "Deployment",
			WorkloadName: configutil.String("OPERATIONAL_TARGET_WORKLOAD", "demo"), ContainerName: configutil.String("OPERATIONAL_TARGET_CONTAINER", "demo"),
			Repository: gitopsRepository.FullName(), BaseBranch: configutil.String("OPERATIONAL_TARGET_BASE_BRANCH", "main"),
			TargetPath: configutil.String("OPERATIONAL_TARGET_GITOPS_PATH", ""),
		},
		Service:  configutil.String("OPERATIONAL_TARGET_SERVICE", "demo"),
		ArgoPath: configutil.String("OPERATIONAL_TARGET_ARGO_PATH", ""), ArgoDestinationServer: configutil.String("OPERATIONAL_TARGET_ARGO_DESTINATION_SERVER", "https://kubernetes.default.svc"),
		ArgoApplication: configutil.String("OPERATIONAL_TARGET_ARGO_APPLICATION", "cloudops-demo"), ArgoProject: configutil.String("OPERATIONAL_TARGET_ARGO_PROJECT", "cloudops-demo"),
		SourceRepository: sourceRepository, AlertNames: alerts,
		Lookback: configutil.DurationSeconds("BASELINE_LOOKBACK_SECONDS", 1800), CommandTimeout: configutil.DurationSeconds("BASELINE_TIMEOUT_SECONDS", 180),
		ElasticsearchURL: configutil.String("OBSERVABILITY_ELASTICSEARCH_URL", ""), ElasticsearchIndex: configutil.String("OBSERVABILITY_ELASTICSEARCH_INDEX", "logs-cloudops-*"),
		ElasticsearchBearerFile: configutil.String("OBSERVABILITY_ELASTICSEARCH_BEARER_TOKEN_FILE", ""), ElasticsearchUsernameFile: configutil.String("OBSERVABILITY_ELASTICSEARCH_USERNAME_FILE", ""),
		ElasticsearchPasswordFile: configutil.String("OBSERVABILITY_ELASTICSEARCH_PASSWORD_FILE", ""), ElasticsearchCAFile: configutil.String("OBSERVABILITY_ELASTICSEARCH_CA_FILE", ""),
	}
	if err := result.Validate(); err != nil {
		return BaselineVerifierConfig{}, err
	}
	return result, nil
}

func (c BaselineVerifierConfig) Validate() error {
	a := c.Application
	if err := c.Target.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Service) == "" || !validBaselinePath(c.ArgoPath) || strings.TrimSpace(c.ArgoDestinationServer) == "" ||
		strings.TrimSpace(c.ArgoApplication) == "" || strings.TrimSpace(c.ArgoProject) == "" {
		return errors.New("baseline target and Argo identity are required")
	}
	if _, err := splitRepository(c.SourceRepository.FullName()); err != nil {
		return fmt.Errorf("invalid baseline source repository: %w", err)
	}
	if a.MySQLHost == "" || a.MySQLUser == "" || a.MySQLPassword == "" || a.MySQLDatabase == "" || strings.EqualFold(a.MySQLUser, "root") ||
		a.MySQLStartupTimeout <= 0 || a.MySQLPingTimeout <= 0 {
		return errors.New("baseline verifier requires a dedicated non-root MySQL identity")
	}
	if !a.K8SEnabled || a.K8SWriteEnabled || !a.K8SInCluster || !slices.Contains(a.K8SAllowedNamespaces, c.Target.Namespace) || a.K8SRequestTimeout <= 0 {
		return errors.New("baseline verifier requires an in-cluster namespace-scoped read-only Kubernetes identity")
	}
	if a.LLMAPIKey != "" || a.GitHubAppID != 0 || a.GitHubInstallationID != 0 || a.GitHubPrivateKeyFile != "" || a.GitHubTokenFile != "" ||
		a.GitHubWriteAppID != 0 || a.GitHubWriteInstallationID != 0 || a.GitHubWritePrivateKeyFile != "" || a.GitHubWriteTokenFile != "" {
		return errors.New("baseline verifier must not receive LLM or GitHub App credentials")
	}
	if err := validateProviderURL("GITHUB_API_BASE_URL", a.GitHubAPIBaseURL, true); err != nil {
		return err
	}
	if err := validateProviderURL("ARGOCD_SERVER", a.ArgoCDServer, true); err != nil {
		return err
	}
	if err := validateProviderURL("OPERATIONAL_TARGET_ARGO_DESTINATION_SERVER", c.ArgoDestinationServer, true); err != nil {
		return err
	}
	if a.ArgoCDTokenFile == "" || !slices.Contains(a.ArgoCDAllowedApplications, c.ArgoApplication) || !slices.Contains(a.ArgoCDAllowedProjects, c.ArgoProject) {
		return errors.New("baseline Argo credential and target allowlists are incomplete")
	}
	if err := validateProviderURL("REGISTRY_BASE_URL", a.RegistryBaseURL, true); err != nil {
		return err
	}
	if len(a.RegistryAllowedHosts) == 0 || len(a.RegistryAllowedRepos) == 0 || len(a.OCIAllowedSources) == 0 ||
		!sourceAllowlisted(c.SourceRepository, a.OCIAllowedSources) {
		return errors.New("baseline Registry and OCI source allowlists are incomplete")
	}
	allowHTTP := a.AppEnv == "local-demo"
	for name, raw := range map[string]string{
		"OBSERVABILITY_PROMETHEUS_URL":    a.ObservabilityPrometheusURL,
		"OBSERVABILITY_TEMPO_URL":         a.ObservabilityTempoURL,
		"OBSERVABILITY_ELASTICSEARCH_URL": c.ElasticsearchURL,
	} {
		if err := validateBaselineEndpoint(name, raw, allowHTTP && name != "OBSERVABILITY_ELASTICSEARCH_URL"); err != nil {
			return err
		}
	}
	if c.ElasticsearchIndex != "logs-cloudops-*" || a.ObservabilityRequestTimeout < time.Second || a.ObservabilityRequestTimeout > time.Minute ||
		a.ObservabilityMaxLookback < c.Lookback || c.Lookback < 30*time.Minute || c.Lookback > 24*time.Hour ||
		c.CommandTimeout <= 0 || c.CommandTimeout > 10*time.Minute || len(c.AlertNames) == 0 || len(c.AlertNames) > 20 {
		return errors.New("baseline observability bounds are invalid")
	}
	bearerAuth := strings.TrimSpace(c.ElasticsearchBearerFile) != ""
	basicAny := strings.TrimSpace(c.ElasticsearchUsernameFile) != "" || strings.TrimSpace(c.ElasticsearchPasswordFile) != ""
	basicAuth := strings.TrimSpace(c.ElasticsearchUsernameFile) != "" && strings.TrimSpace(c.ElasticsearchPasswordFile) != ""
	if bearerAuth == basicAuth || basicAny != basicAuth || strings.TrimSpace(c.ElasticsearchCAFile) == "" {
		return errors.New("baseline Elasticsearch requires one read-only credential mode and a CA file")
	}
	for _, alert := range c.AlertNames {
		if strings.TrimSpace(alert) == "" || len(alert) > 255 {
			return errors.New("baseline alert allowlist contains an invalid name")
		}
	}
	return nil
}

func RunBaselineVerifier(ctx context.Context) (retErr error) {
	cfg, err := LoadBaselineVerifierConfig()
	if err != nil {
		return err
	}
	log, err := logger.Init("cloudops-baseline-verifier")
	if err != nil {
		return err
	}
	defer logger.Sync(log)

	commandCtx, cancel := context.WithTimeout(ctx, cfg.CommandTimeout)
	defer cancel()
	mysqlCtx, mysqlCancel := context.WithTimeout(commandCtx, cfg.Application.MySQLStartupTimeout)
	mysql, err := database.OpenMySQL(mysqlCtx, database.MySQLConfig{
		Host: cfg.Application.MySQLHost, Port: cfg.Application.MySQLPort, User: cfg.Application.MySQLUser,
		Password: cfg.Application.MySQLPassword, Database: cfg.Application.MySQLDatabase, PingTimeout: cfg.Application.MySQLPingTimeout,
	})
	mysqlCancel()
	if err != nil {
		return fmt.Errorf("initialize baseline MySQL: %w", err)
	}
	if mysql == nil || !mysql.Enabled() || mysql.SQLDB() == nil {
		return errors.New("baseline verifier requires MySQL")
	}
	defer func() { retErr = errors.Join(retErr, mysql.Close()) }()
	if err := mysql.Ready(commandCtx); err != nil {
		return fmt.Errorf("baseline MySQL readiness: %w", err)
	}

	verifier, err := buildBaselineVerifier(cfg, mysql.SQLDB())
	if err != nil {
		return err
	}
	result, err := verifier.Verify(commandCtx)
	if err != nil {
		return err
	}
	log.Info("deployment baseline active",
		zap.String("baseline_public_id", result.PublicID),
		zap.Bool("created", result.Created),
		zap.Uint64("superseded_baseline_id", result.SupersededBaselineID),
		zap.Int("observation_count", len(result.ObservationIDs)),
	)
	return nil
}

func buildBaselineVerifier(cfg BaselineVerifierConfig, db *sql.DB) (*baseline.Verifier, error) {
	if db == nil {
		return nil, errors.New("baseline database is required")
	}
	a, target := cfg.Application, cfg.Target
	kubernetesClient, err := k8sread.NewClient(k8sread.Config{
		Enabled: true, InCluster: a.K8SInCluster, Kubeconfig: a.K8SKubeconfig,
		AllowedNamespaces: a.K8SAllowedNamespaces, DefaultNamespace: target.Namespace, RequestTimeout: a.K8SRequestTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize baseline Kubernetes reader: %w", err)
	}
	runtimeReader, err := k8schange.New(kubernetesClient, a.K8SAllowedNamespaces, a.K8SRequestTimeout)
	if err != nil {
		return nil, fmt.Errorf("initialize baseline runtime reader: %w", err)
	}
	githubClient, err := githubread.New(githubread.Config{
		BaseURL: a.GitHubAPIBaseURL, AllowedRepositories: []string{target.Repository, cfg.SourceRepository.FullName()},
		AllowedBranches: []string{target.BaseBranch}, AllowedPaths: []string{target.TargetPath},
		DeniedPathPatterns: a.GitHubDeniedPathPatterns, Timeout: a.GitHubTimeout, MaxRetries: a.GitHubMaxRetries,
		MaxPages: 1, MaxResponseBytes: 512 * 1024, MaxDiffFiles: 1, MaxPatchFiles: 1,
		MaxPatchBytesPerFile: 1, MaxDiffBytes: 1, APIVersion: "2022-11-28",
	})
	if err != nil {
		return nil, fmt.Errorf("initialize public baseline GitHub reader: %w", err)
	}
	argoClient, err := argocdread.New(argocdread.Config{
		Server: a.ArgoCDServer, TokenFile: a.ArgoCDTokenFile, AllowedApplications: []string{cfg.ArgoApplication},
		AllowedProjects: []string{cfg.ArgoProject}, Timeout: a.ArgoCDTimeout, MaxRetries: 1,
		MaxResources: a.ArgoCDMaxResources, MaxDiffBytes: a.ArgoCDMaxDiffBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize baseline Argo reader: %w", err)
	}
	registryClient, err := registryread.New(registryread.Config{
		BaseURL: a.RegistryBaseURL, AllowedHosts: a.RegistryAllowedHosts, AllowedRepositories: a.RegistryAllowedRepos,
		AllowedAuthRealms: a.RegistryAllowedAuthRealms, AllowedRedirectHosts: a.RegistryAllowedRedirects,
		BearerTokenFile: a.RegistryBearerTokenFile, UsernameFile: a.RegistryUsernameFile, PasswordFile: a.RegistryPasswordFile,
		Timeout: a.RegistryTimeout, MaxRetries: a.RegistryMaxRetries, ManifestMaxBytes: a.RegistryManifestMaxBytes,
		ConfigMaxBytes: a.RegistryConfigMaxBytes, CacheTTL: a.RegistryCacheTTL, CacheMaxItems: a.RegistryCacheMaxItems,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize baseline Registry reader: %w", err)
	}
	services, namespaces, environments := oneSet(cfg.Service), oneSet(target.Namespace), oneSet(target.Environment)
	allowHTTP := a.AppEnv == "local-demo"
	baseObservability := observabilityread.Config{
		Timeout: a.ObservabilityRequestTimeout, MaxResponseBytes: a.ObservabilityMaxResponseBytes,
		MaxSamples: a.ObservabilityMaxSamples, MaxSeries: a.ObservabilityMaxSeries, MaxTraces: a.ObservabilityMaxTraces,
		MaxLookback: a.ObservabilityMaxLookback, Retries: a.ObservabilityMaxRetries,
		AllowedServices: services, AllowedNamespaces: namespaces, AllowedEnvironments: environments,
		AllowHTTPForTests: allowHTTP,
	}
	prometheusConfig := baseObservability
	prometheusConfig.BaseURL, prometheusConfig.TokenFile = a.ObservabilityPrometheusURL, a.ObservabilityPromTokenFile
	prometheusClient, err := observabilityread.NewPrometheus(prometheusConfig)
	if err != nil {
		return nil, fmt.Errorf("initialize baseline Prometheus reader: %w", err)
	}
	tempoConfig := baseObservability
	tempoConfig.BaseURL, tempoConfig.TokenFile = a.ObservabilityTempoURL, a.ObservabilityTempoTokenFile
	tempoClient, err := observabilityread.NewTempo(tempoConfig)
	if err != nil {
		return nil, fmt.Errorf("initialize baseline Tempo reader: %w", err)
	}
	elasticClient, err := observabilityread.NewElasticsearch(observabilityread.ElasticConfig{
		BaseURL: cfg.ElasticsearchURL, IndexPattern: cfg.ElasticsearchIndex,
		BearerTokenFile: cfg.ElasticsearchBearerFile, UsernameFile: cfg.ElasticsearchUsernameFile,
		PasswordFile: cfg.ElasticsearchPasswordFile, CAFile: cfg.ElasticsearchCAFile,
		Timeout: a.ObservabilityRequestTimeout, MaxResponseBytes: a.ObservabilityMaxResponseBytes,
		MaxSamples: min(a.ObservabilityMaxSamples, 100), MaxLookback: a.ObservabilityMaxLookback,
		AllowedServices: services, AllowedNamespaces: namespaces, AllowedEnvironments: environments,
		AllowHTTPForTests: false,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize baseline Elasticsearch reader: %w", err)
	}
	store, err := baselinemysql.NewRepository(db)
	if err != nil {
		return nil, err
	}
	return baseline.NewVerifier(baseline.VerifierConfig{
		Target: target, Service: cfg.Service, ArgoApplication: cfg.ArgoApplication, ArgoProject: cfg.ArgoProject,
		ArgoPath: cfg.ArgoPath, ArgoDestinationServer: cfg.ArgoDestinationServer,
		SourceRepository: cfg.SourceRepository, AllowedOCISources: a.OCIAllowedSources,
		AlertNames: cfg.AlertNames, Lookback: cfg.Lookback,
		Argo: argoClient, Runtime: runtimeReader, Registry: registryClient, Git: githubClient, SourceGit: githubClient,
		Rollout: runtimeReader, Prometheus: prometheusClient, Logs: elasticClient, Traces: tempoClient, Store: store,
	})
}

func rejectBaselineCredentials() error {
	for _, name := range forbiddenBaselineCredentialEnv {
		value := strings.TrimSpace(os.Getenv(name))
		if value != "" && value != "0" {
			return fmt.Errorf("%s must not be configured for baseline-verify", name)
		}
	}
	return nil
}

func splitRepository(value string) (change.RepositoryRef, error) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(value), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return change.RepositoryRef{}, errors.New("repository must be owner/name")
	}
	return change.RepositoryRef{Owner: parts[0], Name: parts[1]}, nil
}

func sourceAllowlisted(repository change.RepositoryRef, sources []string) bool {
	expected := "https://github.com/" + strings.ToLower(repository.FullName())
	for _, source := range sources {
		value := strings.ToLower(strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(source), "/"), ".git"))
		if value == expected {
			return true
		}
	}
	return false
}

func validBaselinePath(value string) bool {
	value = strings.Trim(strings.TrimSpace(value), "/")
	return value != "" && value != "." && !strings.Contains(value, "..")
}

func validateBaselineEndpoint(name, raw string, allowHTTP bool) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "https" && (!allowHTTP || parsed.Scheme != "http")) {
		return fmt.Errorf("%s is not a fixed allowed endpoint", name)
	}
	return nil
}
