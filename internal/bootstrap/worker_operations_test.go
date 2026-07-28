package bootstrap

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestAssembleWorkerTaskOperationsRegistersCompleteRuntime(t *testing.T) {
	db := new(sql.DB)
	tasks, err := asyncjob.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	operations, err := AssembleWorkerTaskOperations(db, tasks, completeWorkerOperationConfig(), completeWorkerOperationDependencies())
	if err != nil {
		t.Fatal(err)
	}
	handlers, err := taskhandler.NewRuntime(operations)
	if err != nil {
		t.Fatal(err)
	}
	if len(handlers) != len(asyncjob.TaskTypes()) {
		t.Fatalf("handlers=%d task types=%d", len(handlers), len(asyncjob.TaskTypes()))
	}
}

func TestAssembleWorkerTaskOperationsFailsClosedForEveryMissingDependency(t *testing.T) {
	db := new(sql.DB)
	tasks, err := asyncjob.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*WorkerOperationDependencies)
	}{
		{"investigation model", func(value *WorkerOperationDependencies) { value.InvestigationModel = nil }},
		{"investigation read tools", func(value *WorkerOperationDependencies) { value.InvestigationTools = nil }},
		{"investigation planner", func(value *WorkerOperationDependencies) { value.InvestigationPlanner = nil }},
		{"remediation loader", func(value *WorkerOperationDependencies) { value.RemediationLoader = nil }},
		{"remediation store", func(value *WorkerOperationDependencies) { value.RemediationStore = nil }},
		{"GitHub reader", func(value *WorkerOperationDependencies) { value.GitHubReader = nil }},
		{"GitHub writer", func(value *WorkerOperationDependencies) { value.GitHubWriter = nil }},
		{"delivery observer", func(value *WorkerOperationDependencies) { value.DeliveryObserver = nil }},
		{"verification observations", func(value *WorkerOperationDependencies) { value.VerificationObservations = nil }},
		{"resolution report writer", func(value *WorkerOperationDependencies) { value.ResolutionReports = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dependencies := completeWorkerOperationDependencies()
			test.edit(&dependencies)
			_, err := AssembleWorkerTaskOperations(db, tasks, completeWorkerOperationConfig(), dependencies)
			if err == nil || !strings.Contains(err.Error(), test.name) {
				t.Fatalf("missing dependency error=%v", err)
			}
		})
	}
}

func TestStaticTaskOperationFactoryValidatesBeforeMySQLStartup(t *testing.T) {
	factory := StaticTaskOperationFactory{
		Config:       completeWorkerOperationConfig(),
		Dependencies: completeWorkerOperationDependencies(),
	}
	if err := factory.Validate(); err != nil {
		t.Fatal(err)
	}
	factory.Dependencies.GitHubWriter = nil
	if err := factory.Validate(); err == nil || !strings.Contains(err.Error(), "GitHub writer") {
		t.Fatalf("missing provider identity error=%v", err)
	}
	factory.Dependencies = completeWorkerOperationDependencies()
	factory.Config.CurrentPolicyHash = strings.Repeat("g", 64)
	if err := factory.Validate(); err == nil || !strings.Contains(err.Error(), "policy hash") {
		t.Fatalf("invalid policy identity error=%v", err)
	}
}

func completeWorkerOperationConfig() WorkerOperationConfig {
	return WorkerOperationConfig{
		AgentRunIdentity: agent.RunModelIdentity{
			Provider: "fixture", ActualModel: "fixture-model", PromptVersion: "prompt/v1",
			PromptHash: strings.Repeat("a", 64), ToolSchemaVersion: "tools/v1", ToolSchemaHash: strings.Repeat("b", 64),
		},
		ClaimPolicy: agent.GoldenRequiredEnvClaimPolicy(),
		ActionPolicies: map[string]agent.ToolActionPolicy{
			"inspect_workload": {TemplateIDs: []string{"workload/v1"}, ExpectedFactTypes: []string{"workload.subject_confirmed"}},
		},
		RequiredSources:    []string{"kubernetes"},
		MaxCheckpointBytes: 64 * 1024,
		CurrentPolicyHash:  strings.Repeat("a", 64),
		DeliveryTarget: taskhandler.DeliveryObserveTarget{
			ArgoApplication: "cloudops-demo", ArgoProject: "cloudops-demo",
			ArgoRepository: "https://github.com/acme/gitops", ArgoPath: "environments/demo",
			DesiredReplicas: 2,
		},
		DeliveryPollInterval: 5 * time.Second,
		DeliveryTimeout:      30 * time.Minute,
		MaxAgentRuns:         taskhandler.DefaultAgentRunBudget,
	}
}

func completeWorkerOperationDependencies() WorkerOperationDependencies {
	return WorkerOperationDependencies{
		InvestigationModel:       workerModelStub{},
		InvestigationTools:       workerToolStub{},
		InvestigationPlanner:     workerPlannerStub{},
		RemediationLoader:        workerRemediationLoaderStub{},
		RemediationStore:         workerRemediationStoreStub{},
		GitHubReader:             workerExactGitReaderStub{},
		GitHubWriter:             workerGitHubWriterStub{},
		DeliveryObserver:         workerDeliveryObserverStub{},
		VerificationObservations: workerVerificationObservationStub{},
		ResolutionReports:        workerResolutionReportStub{},
	}
}

type workerModelStub struct{}

func (workerModelStub) ProposeDelta(context.Context, agent.ModelView) (agent.StateDelta, agent.ModelUsage, error) {
	return agent.StateDelta{}, agent.ModelUsage{}, nil
}
func (workerModelStub) SynthesizeDiagnosis(context.Context, agent.DiagnosisView) (agent.DiagnosisCandidate, agent.ModelUsage, error) {
	return agent.DiagnosisCandidate{}, agent.ModelUsage{}, nil
}

type workerToolStub struct{}

func (workerToolStub) Execute(context.Context, agent.InvestigationToolRequest) (agent.ToolObservation, error) {
	return agent.ToolObservation{}, nil
}

type workerPlannerStub struct{}

func (workerPlannerStub) NextAction(agent.InvestigationState, []agent.EvidenceFact, string) (*agent.ProposedAction, error) {
	return nil, nil
}

type workerRemediationLoaderStub struct{}

func (workerRemediationLoaderStub) Load(context.Context, asyncjob.Task) (taskhandler.RemediationPrepareInput, error) {
	return taskhandler.RemediationPrepareInput{}, nil
}

type workerRemediationStoreStub struct{}

func (workerRemediationStoreStub) PersistIn(context.Context, asyncjob.DBTX, asyncjob.Task, taskhandler.RemediationPrepareInput, *remediation.RemediationPlan) error {
	return nil
}

type workerGitHubWriterStub struct{}

type workerExactGitReaderStub struct{}

func (workerExactGitReaderStub) ReadRestoreFacts(context.Context, remediation.ExactGitRestoreQuery) (remediation.ExactGitRestoreFacts, error) {
	return remediation.ExactGitRestoreFacts{}, nil
}

func (workerGitHubWriterStub) ReconcileDraftPR(context.Context, remediation.ChangeWriteRequest) (remediation.WriteObservation, error) {
	return remediation.WriteObservation{}, nil
}
func (workerGitHubWriterStub) EnsureBranch(context.Context, remediation.ChangeWriteRequest) (remediation.WriteObservation, error) {
	return remediation.WriteObservation{}, nil
}
func (workerGitHubWriterStub) EnsureCommit(context.Context, remediation.ChangeWriteRequest) (remediation.WriteObservation, error) {
	return remediation.WriteObservation{}, nil
}
func (workerGitHubWriterStub) EnsureDraftPR(context.Context, remediation.ChangeWriteRequest) (remediation.WriteObservation, error) {
	return remediation.WriteObservation{}, nil
}

type workerDeliveryObserverStub struct{}

func (workerDeliveryObserverStub) Observe(context.Context, taskhandler.DeliveryObserveRequest) (taskhandler.DeliveryObservation, error) {
	return taskhandler.DeliveryObservation{}, nil
}

type workerVerificationObservationStub struct{}

func (workerVerificationObservationStub) Observe(context.Context, verification.Run, verification.Check) (verification.Observation, error) {
	return verification.Observation{}, nil
}

type workerResolutionReportStub struct{}

func (workerResolutionReportStub) PersistIn(context.Context, asyncjob.DBTX, asyncjob.Task, taskhandler.VerificationAdvanceSnapshot, []verification.Check, *time.Time, time.Time) error {
	return nil
}
