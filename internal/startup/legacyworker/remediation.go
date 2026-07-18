package legacyworker

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/githubwrite"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentmysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/service/changeintelligence"
	remediationservice "github.com/05allan1213/CloudOps-Copilot/internal/service/remediation"
)

func InitRemediation(cfg config.Config, container *di.Container) (*remediationservice.Worker, error) {
	if !cfg.RemediationEnabled {
		return nil, nil
	}
	if container.DB == nil {
		return nil, fmt.Errorf("remediation requires MySQL")
	}
	store, err := incidentmysql.NewStore(container.DB)
	if err != nil {
		return nil, err
	}
	repository, err := incidentmysql.NewRemediationRepository(container.DB)
	if err != nil {
		return nil, err
	}
	changes, err := incidentmysql.NewChangeRepository(container.DB)
	if err != nil {
		return nil, err
	}
	var token githubwrite.TokenProvider
	if cfg.GitHubWriteTokenFile != "" {
		token = githubwrite.FileTokenProvider{Path: cfg.GitHubWriteTokenFile}
	} else {
		repositoryNames := make([]string, 0, len(cfg.GitHubWriteAllowedRepositories))
		for _, value := range cfg.GitHubWriteAllowedRepositories {
			parts := strings.SplitN(value, "/", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid GitHub write repository %q", value)
			}
			repositoryNames = append(repositoryNames, parts[1])
		}
		token, err = githubwrite.NewAppTokenProvider(githubwrite.AppTokenConfig{BaseURL: cfg.GitHubWriteAPIBaseURL, AppID: cfg.GitHubWriteAppID, InstallationID: cfg.GitHubWriteInstallationID, PrivateKeyFile: cfg.GitHubWritePrivateKeyFile, AllowedRepositories: repositoryNames})
		if err != nil {
			return nil, err
		}
	}
	writer, err := githubwrite.New(githubwrite.Config{BaseURL: cfg.GitHubWriteAPIBaseURL, TokenProvider: token, AllowedRepositories: cfg.GitHubWriteAllowedRepositories, AllowedBaseBranches: cfg.GitHubWriteAllowedBaseBranches, AllowedPaths: cfg.GitHubWriteAllowedPaths, Timeout: cfg.GitHubWriteTimeout, MaxResponseBytes: cfg.GitHubWriteMaxResponseBytes, MaxContentBytes: cfg.GitHubWriteMaxContentBytes, Observer: container.Metrics})
	if err != nil {
		return nil, err
	}
	var changeMappings map[string]changeintelligence.ServiceMapping
	if err := json.Unmarshal([]byte(cfg.ChangeServiceMappingsJSON), &changeMappings); err != nil {
		return nil, fmt.Errorf("decode remediation service mappings: %w", err)
	}
	var baseBranches map[string]string
	if err := json.Unmarshal([]byte(cfg.GitOpsBaseBranchesJSON), &baseBranches); err != nil {
		return nil, fmt.Errorf("decode GitOps base branches: %w", err)
	}
	mappings := make(map[string]remediationservice.Mapping, len(changeMappings))
	for service, mapping := range changeMappings {
		repositoryName := mapping.Repository.FullName()
		branch, ok := baseBranches[repositoryName]
		if !ok {
			return nil, fmt.Errorf("missing base branch for %s", repositoryName)
		}
		mappings[service] = remediationservice.Mapping{Repository: repositoryName, Path: mapping.GitOpsPath, BaseBranch: branch}
	}
	operations := make([]remediation.OperationType, 0, len(cfg.RemediationAllowedOperations))
	for _, value := range cfg.RemediationAllowedOperations {
		operations = append(operations, remediation.OperationType(value))
	}
	service, err := remediationservice.New(remediationservice.Config{Enabled: true, Repository: repository, Incidents: store, Evidence: store.ReadRepositories().Evidence, Changes: changes, BaseReader: writer, Mappings: mappings, Policy: remediation.PolicyConfig{AllowedRepositories: cfg.GitHubWriteAllowedRepositories, AllowedPaths: cfg.GitHubWriteAllowedPaths, AllowedOperations: operations, MaxPatchBytes: cfg.RemediationMaxPatchBytes, MaxFiles: cfg.RemediationMaxFiles, MaxRisk: remediation.RiskLevel(cfg.RemediationMaxRisk), MinReplicas: cfg.RemediationMinReplicas, MaxReplicas: cfg.RemediationMaxReplicas, HPATargets: cfg.RemediationHPATargets}, Observer: container.Metrics})
	if err != nil {
		return nil, err
	}
	container.Handler.SetRemediation(service)
	worker, err := remediationservice.NewWorker(remediationservice.WorkerConfig{Enabled: cfg.GitOpsPREnabled && cfg.GitHubWriteEnabled, Owner: cfg.RemediationWorkerID, PollInterval: cfg.RemediationPollInterval, Lease: cfg.RemediationLeaseDuration, Repository: repository, GitHub: writer, BaseBranches: baseBranches, Observer: container.Metrics})
	if err != nil {
		return nil, err
	}
	return worker, nil
}
