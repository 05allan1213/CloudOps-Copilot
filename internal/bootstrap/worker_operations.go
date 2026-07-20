package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/baselinemysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
)

// WorkerOperationDependencies is the complete provider surface required by
// the five subject-bound V3 operations. Every dependency is a real bounded
// port; assembly deliberately supplies no no-op, forced-dead, or legacy
// wrapper fallback.
type WorkerOperationDependencies struct {
	InvestigationModel agent.InvestigationModel
	InvestigationTools agent.InvestigationReadTool

	RemediationLoader taskhandler.RemediationPrepareLoader
	RemediationStore  taskhandler.RemediationPrepareStore
	GitHubWriter      remediation.PhasedGitHubWriter

	DeliveryObserver         taskhandler.DeliveryObserver
	VerificationObservations taskhandler.VerificationObservationSource
	ResolutionReports        taskhandler.ResolutionReportWriter
}

// WorkerOperationConfig freezes the deterministic policy and target identity
// shared by the operation constructors. Provider credentials do not belong in
// this value; their adapters own credential-file validation.
type WorkerOperationConfig struct {
	ClaimPolicy        agent.ClaimPolicy
	ActionPolicies     map[string]agent.ToolActionPolicy
	RequiredSources    []string
	MaxCheckpointBytes int

	CurrentPolicyHash string

	DeliveryTarget       taskhandler.DeliveryObserveTarget
	DeliveryPollInterval time.Duration
	DeliveryTimeout      time.Duration
	MaxAgentRuns         int

	Now func() time.Time
}

// AssembleWorkerTaskOperations constructs every owning operation against the
// same DML connection and async task repository used by the Runner. Returning
// a taskhandler.Config rather than handlers preserves NewRuntime as the final
// exact-dispatch completeness gate.
func AssembleWorkerTaskOperations(
	db *sql.DB,
	tasks *asyncjob.Repository,
	config WorkerOperationConfig,
	dependencies WorkerOperationDependencies,
) (taskhandler.Config, error) {
	if db == nil || tasks == nil {
		return taskhandler.Config{}, errors.New("production task operations require MySQL and the unified async task repository")
	}
	if err := validateWorkerOperationConfig(config); err != nil {
		return taskhandler.Config{}, err
	}
	if err := validateWorkerOperationDependencies(dependencies); err != nil {
		return taskhandler.Config{}, err
	}

	investigationStep, err := taskhandler.NewInvestigationStep(taskhandler.InvestigationStepConfig{
		DB: db, Tasks: tasks, Model: dependencies.InvestigationModel, Tools: dependencies.InvestigationTools,
		ClaimPolicy: config.ClaimPolicy, ActionPolicies: config.ActionPolicies,
		RequiredSources: config.RequiredSources, MaxCheckpointBytes: config.MaxCheckpointBytes, Now: config.Now,
	})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("assemble investigation.step: %w", err)
	}
	remediationPrepare, err := taskhandler.NewRemediationPrepare(taskhandler.RemediationPrepareConfig{
		Loader: dependencies.RemediationLoader, Store: dependencies.RemediationStore, Now: config.Now,
	})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("assemble remediation.prepare: %w", err)
	}
	changeEnsurePR, err := taskhandler.NewChangeEnsurePR(taskhandler.ChangeEnsurePRConfig{
		DB: db, Tasks: tasks, Writer: dependencies.GitHubWriter,
		CurrentPolicyHash: config.CurrentPolicyHash, Now: config.Now,
	})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("assemble change.ensure_pr: %w", err)
	}
	deliveryStore, err := taskhandler.NewMySQLDeliveryObserveStore(taskhandler.MySQLDeliveryObserveConfig{
		DB: db, Tasks: tasks, Target: config.DeliveryTarget, Now: config.Now,
		PollInterval: config.DeliveryPollInterval, DeliveryTimeout: config.DeliveryTimeout,
	})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("assemble delivery.observe store: %w", err)
	}
	deliveryObserve, err := taskhandler.NewDeliveryObserve(taskhandler.DeliveryObserveConfig{
		Observer: dependencies.DeliveryObserver, Store: deliveryStore,
		Now: config.Now, PollInterval: config.DeliveryPollInterval,
	})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("assemble delivery.observe: %w", err)
	}
	baselineStore, err := baselinemysql.NewRepository(db)
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("assemble DeploymentBaseline store: %w", err)
	}
	verificationAdvance, err := taskhandler.NewMySQLVerificationAdvance(taskhandler.MySQLVerificationAdvanceConfig{
		DB: db, Tasks: tasks, Observations: dependencies.VerificationObservations,
		Reports: dependencies.ResolutionReports, Baselines: baselineStore,
		Now: config.Now, MaxAgentRuns: config.MaxAgentRuns,
	})
	if err != nil {
		return taskhandler.Config{}, fmt.Errorf("assemble verification.advance: %w", err)
	}

	return taskhandler.Config{
		InvestigationStep: investigationStep, RemediationPrepare: remediationPrepare,
		ChangeEnsurePR: changeEnsurePR, DeliveryObserve: deliveryObserve,
		VerificationAdvance: verificationAdvance,
	}, nil
}

func validateWorkerOperationConfig(config WorkerOperationConfig) error {
	if strings.TrimSpace(config.ClaimPolicy.Version) == "" || strings.TrimSpace(config.ClaimPolicy.ClaimType) == "" ||
		len(config.ClaimPolicy.Requirements) == 0 || config.ClaimPolicy.MinIndependentCollectors < 1 {
		return errors.New("production task operations require a versioned investigation claim policy")
	}
	if len(config.ActionPolicies) == 0 {
		return errors.New("production task operations require an investigation read-tool allowlist")
	}
	for name, policy := range config.ActionPolicies {
		if strings.TrimSpace(name) == "" || len(policy.TemplateIDs) == 0 || len(policy.ExpectedFactTypes) == 0 {
			return fmt.Errorf("production task operations contain invalid investigation policy %q", name)
		}
	}
	if len(config.RequiredSources) == 0 {
		return errors.New("production task operations require explicit investigation sources")
	}
	if config.MaxCheckpointBytes != 0 && (config.MaxCheckpointBytes < 1024 || config.MaxCheckpointBytes > 128*1024) {
		return errors.New("production investigation checkpoint limit is outside 1024..131072 bytes")
	}
	if !lowerHex(config.CurrentPolicyHash, 64) {
		return errors.New("production task operations require the current lowercase remediation policy hash")
	}
	target := config.DeliveryTarget
	if strings.TrimSpace(target.ArgoApplication) == "" || strings.TrimSpace(target.ArgoProject) == "" ||
		strings.TrimSpace(target.ArgoRepository) == "" || strings.TrimSpace(target.ArgoPath) == "" {
		return errors.New("production delivery target requires fixed Argo application, project, repository, and path identity")
	}
	if target.DesiredReplicas != 0 && target.DesiredReplicas != 2 {
		return errors.New("production Golden delivery target requires exactly two replicas")
	}
	if config.DeliveryPollInterval < 0 || config.DeliveryPollInterval > time.Minute {
		return errors.New("production delivery poll interval is outside its bound")
	}
	if config.DeliveryTimeout < 0 || config.DeliveryTimeout > 24*time.Hour ||
		(config.DeliveryTimeout > 0 && config.DeliveryTimeout < time.Minute) {
		return errors.New("production delivery timeout is outside its bound")
	}
	if config.MaxAgentRuns != 0 && config.MaxAgentRuns != taskhandler.DefaultAgentRunBudget {
		return errors.New("production verification agent-run limit must match investigation.start")
	}
	return nil
}

func lowerHex(value string, length int) bool {
	if len(value) != length || strings.ToLower(value) != value {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validateWorkerOperationDependencies(dependencies WorkerOperationDependencies) error {
	missing := make([]string, 0, 8)
	if dependencies.InvestigationModel == nil {
		missing = append(missing, "investigation model")
	}
	if dependencies.InvestigationTools == nil {
		missing = append(missing, "investigation read tools")
	}
	if dependencies.RemediationLoader == nil {
		missing = append(missing, "remediation loader")
	}
	if dependencies.RemediationStore == nil {
		missing = append(missing, "remediation store")
	}
	if dependencies.GitHubWriter == nil {
		missing = append(missing, "GitHub writer")
	}
	if dependencies.DeliveryObserver == nil {
		missing = append(missing, "delivery observer")
	}
	if dependencies.VerificationObservations == nil {
		missing = append(missing, "verification observations")
	}
	if dependencies.ResolutionReports == nil {
		missing = append(missing, "resolution report writer")
	}
	if len(missing) > 0 {
		return fmt.Errorf("production task operation dependencies are incomplete: %s", strings.Join(missing, ", "))
	}
	return nil
}

// StaticTaskOperationFactory is useful for a fully constructed production
// provider set and for startup contract tests. Provider construction itself
// remains outside the generic Worker so temporary external health is not
// folded into /readyz after startup.
type StaticTaskOperationFactory struct {
	Config       WorkerOperationConfig
	Dependencies WorkerOperationDependencies
}

func (f StaticTaskOperationFactory) Validate() error {
	if err := validateWorkerOperationConfig(f.Config); err != nil {
		return err
	}
	return validateWorkerOperationDependencies(f.Dependencies)
}

func (f StaticTaskOperationFactory) Build(_ context.Context, db *sql.DB, tasks *asyncjob.Repository) (taskhandler.Config, error) {
	return AssembleWorkerTaskOperations(db, tasks, f.Config, f.Dependencies)
}
