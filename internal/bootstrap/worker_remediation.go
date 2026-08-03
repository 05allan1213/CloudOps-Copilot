package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/githubread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/remediationmysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

type remediationSettingsSource interface {
	ProviderAccess(context.Context, string, settings.Provider) (settings.ProviderAccess, error)
}

type settingsRemediationLoader struct {
	db            *sql.DB
	settings      remediationSettingsSource
	localScenario taskhandler.RemediationPrepareLoader
}

type settingsRemediationTarget struct {
	revisionID string
	policy     remediation.RestoreEnvPolicy
}

func newSettingsRemediationRunner(
	cfg WorkerConfig,
	db *sql.DB,
	tasks *asyncjob.Repository,
	settingsService *settings.Service,
	kubernetes infrastructure.Reader,
) (taskRunner, error) {
	if db == nil || tasks == nil || settingsService == nil {
		return nil, errors.New("Settings remediation runner dependencies are incomplete")
	}
	remediationRepository, err := remediationmysql.NewRepository(db)
	if err != nil {
		return nil, fmt.Errorf("initialize Settings remediation repository: %w", err)
	}
	store, err := taskhandler.NewMySQLRemediationPrepareStore(remediationRepository)
	if err != nil {
		return nil, fmt.Errorf("initialize Settings remediation store: %w", err)
	}
	localScenario, err := taskhandler.NewLocalScenarioRemediationPrepareLoader(db, kubernetes, 0)
	if err != nil {
		return nil, fmt.Errorf("initialize local Scenario remediation loader: %w", err)
	}
	operation, err := taskhandler.NewRemediationPrepare(taskhandler.RemediationPrepareConfig{
		Loader: &settingsRemediationLoader{db: db, settings: settingsService, localScenario: localScenario}, Store: store,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Settings remediation operation: %w", err)
	}
	investigatePool := asyncPoolConfigs(cfg.Async)[asyncjob.QueueInvestigate]
	return asyncjob.NewRunner(asyncjob.RunnerConfig{
		Owner: cfg.Async.WorkerID, Store: tasks,
		Handlers:     map[asyncjob.TaskType]asyncjob.Handler{asyncjob.TaskRemediationPrepare: asyncjob.HandlerFunc(operation)},
		TaskTypes:    []asyncjob.TaskType{asyncjob.TaskRemediationPrepare},
		Pools:        map[asyncjob.Queue]asyncjob.PoolConfig{asyncjob.QueueInvestigate: investigatePool},
		DrainTimeout: cfg.Async.DrainTimeout,
		CancelWait:   cfg.Async.ExitDeadline - cfg.Async.DrainTimeout,
		Boundary: asyncjob.BoundaryFunc(func(boundaryCtx context.Context, execution asyncjob.Execution) (context.Context, error) {
			return settingsService.ObserveTaskBoundary(boundaryCtx, execution.Task.ID, execution.Task.ConfigurationRevisionID)
		}),
	})
}

func (l *settingsRemediationLoader) Load(ctx context.Context, task asyncjob.Task) (taskhandler.RemediationPrepareInput, error) {
	if l == nil || l.db == nil || l.settings == nil || task.ConfigurationRevisionID == 0 {
		return taskhandler.RemediationPrepareInput{}, fmt.Errorf("%w: remediation task has no Settings authority", asyncjob.ErrPolicyViolation)
	}
	localScenario, err := l.isExactLocalScenarioTask(ctx, task)
	if err != nil {
		return taskhandler.RemediationPrepareInput{}, err
	}
	if localScenario {
		if l.localScenario == nil {
			return taskhandler.RemediationPrepareInput{}, fmt.Errorf("%w: local Scenario remediation loader is unavailable", asyncjob.ErrPolicyViolation)
		}
		return l.localScenario.Load(ctx, task)
	}
	target, err := l.loadTarget(ctx, task)
	if err != nil {
		return taskhandler.RemediationPrepareInput{}, err
	}
	access, err := l.settings.ProviderAccess(ctx, target.revisionID, settings.ProviderGitHub)
	if err != nil {
		if errors.Is(err, settings.ErrNotFound) {
			return taskhandler.RemediationPrepareInput{}, fmt.Errorf("%w: task-bound GitHub credential is absent", asyncjob.ErrPolicyViolation)
		}
		return taskhandler.RemediationPrepareInput{}, fmt.Errorf("load task-bound GitHub Provider: %w", err)
	}
	defer access.Clear()
	configuration := access.Configuration
	if access.Revision.ID != target.revisionID || configuration.Provider != settings.ProviderGitHub ||
		!configuration.Enabled || strings.TrimSpace(configuration.Endpoint) == "" ||
		configuration.TimeoutMS < 1000 || configuration.TimeoutMS > 60000 || configuration.MaxResults <= 0 ||
		len(strings.TrimSpace(string(access.Credential))) == 0 {
		return taskhandler.RemediationPrepareInput{}, fmt.Errorf("%w: task-bound GitHub Provider is disabled or incomplete", asyncjob.ErrPolicyViolation)
	}
	token := &settingsGitHubToken{value: access.Credential}
	client, err := githubread.New(githubread.Config{
		BaseURL: configuration.Endpoint, TokenProvider: token,
		AllowedRepositories: []string{target.policy.Repository}, AllowedBranches: []string{target.policy.BaseBranch},
		AllowedPaths: []string{target.policy.AllowedPath}, Timeout: time.Duration(configuration.TimeoutMS) * time.Millisecond,
		MaxRetries: 1, MaxPages: 3, MaxResponseBytes: 2 * 1024 * 1024,
		MaxDiffFiles: 100, MaxPatchFiles: 100, MaxPatchBytesPerFile: 16 * 1024,
		MaxDiffBytes: remediation.MaxPlanDiffBytes, APIVersion: "2022-11-28",
	})
	if err != nil {
		return taskhandler.RemediationPrepareInput{}, fmt.Errorf("%w: task-bound GitHub Provider is invalid: %v", asyncjob.ErrPolicyViolation, err)
	}
	loader, err := taskhandler.NewMySQLRemediationPrepareLoader(l.db, client, taskhandler.MySQLRemediationPrepareLoaderConfig{
		Policy: target.policy,
	})
	if err != nil {
		return taskhandler.RemediationPrepareInput{}, fmt.Errorf("%w: task-bound remediation policy is invalid: %v", asyncjob.ErrPolicyViolation, err)
	}
	return loader.Load(ctx, task)
}

func (l *settingsRemediationLoader) isExactLocalScenarioTask(ctx context.Context, task asyncjob.Task) (bool, error) {
	var matched bool
	err := l.db.QueryRowContext(ctx, `SELECT EXISTS(
  SELECT 1
  FROM agent_runs AS run
  JOIN incidents AS incident ON incident.id=run.incident_id AND incident.cycle_no=run.cycle_no
  WHERE run.id=? AND run.incident_id=? AND run.cycle_no=? AND run.configuration_revision_id=?
    AND incident.cluster='cloudops-local' AND incident.environment='local'
    AND incident.namespace='demo' AND incident.target_kind='Deployment'
    AND incident.target_name='cloudops-scenario-fault'
)`, task.SubjectID, task.IncidentID, task.CycleNo, task.ConfigurationRevisionID).Scan(&matched)
	if err != nil {
		return false, fmt.Errorf("identify local Scenario remediation task: %w", err)
	}
	return matched, nil
}

func (l *settingsRemediationLoader) loadTarget(ctx context.Context, task asyncjob.Task) (_ settingsRemediationTarget, retErr error) {
	rows, err := l.db.QueryContext(ctx, `SELECT revision.public_id,baseline.namespace,baseline.workload_name,
baseline.container_name,baseline.repository,baseline.base_branch,baseline.target_path
FROM agent_runs AS run
JOIN incidents AS incident ON incident.id=run.incident_id AND incident.cycle_no=run.cycle_no
JOIN configuration_revisions AS revision ON revision.id=run.configuration_revision_id
JOIN deployment_baselines AS baseline ON baseline.status='active'
 AND baseline.cluster=incident.cluster AND baseline.environment=incident.environment
 AND baseline.namespace=incident.namespace AND baseline.workload_kind=incident.target_kind
 AND baseline.workload_name=incident.target_name
WHERE run.id=? AND run.incident_id=? AND run.cycle_no=? AND run.configuration_revision_id=?
ORDER BY baseline.id LIMIT 2`, task.SubjectID, task.IncidentID, task.CycleNo, task.ConfigurationRevisionID)
	if err != nil {
		return settingsRemediationTarget{}, fmt.Errorf("load task-bound remediation target: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close task-bound remediation target: %w", closeErr))
		}
	}()
	matches := make([]settingsRemediationTarget, 0, 2)
	for rows.Next() {
		var target settingsRemediationTarget
		if err = rows.Scan(&target.revisionID, &target.policy.Namespace, &target.policy.Workload,
			&target.policy.Container, &target.policy.Repository, &target.policy.BaseBranch, &target.policy.AllowedPath); err != nil {
			return settingsRemediationTarget{}, fmt.Errorf("scan task-bound remediation target: %w", err)
		}
		target.policy.Version = remediation.RestoreRequiredEnvPolicyVersion
		target.policy.APIVersion = "apps/v1"
		target.policy.EnvKey = "REQUIRED_ENV"
		target.policy.MaxDiffBytes = remediation.MaxPlanDiffBytes
		target.policy.MaxPostImageBytes = remediation.MaxPostImageBytes
		target.policy.VerificationVersion = verification.GoldenRequiredEnvProfileID
		matches = append(matches, target)
	}
	if err = rows.Err(); err != nil {
		return settingsRemediationTarget{}, fmt.Errorf("iterate task-bound remediation target: %w", err)
	}
	if len(matches) != 1 || strings.TrimSpace(matches[0].revisionID) == "" ||
		strings.Count(strings.Trim(matches[0].policy.Repository, "/"), "/") != 1 ||
		strings.TrimSpace(matches[0].policy.BaseBranch) == "" || strings.TrimSpace(matches[0].policy.Container) == "" ||
		strings.TrimSpace(matches[0].policy.AllowedPath) == "" || change.SensitivePath(matches[0].policy.AllowedPath, nil) {
		return settingsRemediationTarget{}, fmt.Errorf("%w: remediation requires one exact active DeploymentBaseline", asyncjob.ErrPolicyViolation)
	}
	return matches[0], nil
}

type settingsGitHubToken struct{ value []byte }

func (t *settingsGitHubToken) Token(context.Context) (string, error) {
	if t == nil {
		return "", change.ErrUnavailable
	}
	value := strings.TrimSpace(string(t.value))
	if value == "" || len(value) > 4096 {
		return "", change.ErrInvalidArgument
	}
	return value, nil
}
