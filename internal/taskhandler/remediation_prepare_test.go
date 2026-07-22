package taskhandler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

func TestRemediationPrepareCompilesBeforeAtomicPersistence(t *testing.T) {
	request := prepareTestRequest()
	store := &prepareTestStore{}
	operation, err := NewRemediationPrepare(RemediationPrepareConfig{
		Loader: RemediationPrepareLoaderFunc(func(context.Context, asyncjob.Task) (RemediationPrepareInput, error) {
			return RemediationPrepareInput{
				AgentRunID: 30, PlanPublicID: "44444444-4444-4444-8444-444444444444",
				Baseline: RemediationPrepareBaselineFence{
					ID: 50, PublicID: "55555555-5555-4555-8555-555555555555", RowVersion: 1,
					GitOpsRevision: request.LastKnownGoodRevision, ConfigHash: strings.Repeat("6", 64),
					ObservationID: 60, ObservationHash: strings.Repeat("6", 64),
				},
				Request: request,
			}, nil
		}),
		Store: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(remediationPreparePayload{AgentRunID: request.CreatedByAgentRunID, CycleNo: request.CycleNo})
	task := asyncjob.Task{
		ID: 40, IncidentID: request.IncidentID, CycleNo: uint32(request.CycleNo), Queue: asyncjob.QueueInvestigate,
		Type: asyncjob.TaskRemediationPrepare, SubjectType: "agent_run", SubjectID: 30,
		Transition: "remediation.prepare", ExpectedSubjectVersion: 7,
		PayloadSchemaVersion: remediationPreparePayloadSchema, Payload: payload,
	}
	execution := asyncjob.Execution{Task: task, Lease: asyncjob.Lease{TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: 5}}
	result := operation(context.Background(), execution)
	if result.Disposition != asyncjob.DispositionSucceeded || result.Mutate == nil || store.plan != nil {
		t.Fatalf("result=%+v persisted=%+v", result, store.plan)
	}
	if err := result.Mutate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if store.plan == nil || store.plan.OperationType != remediation.OperationRestoreRequiredEnv || len(store.plan.CanonicalPlanHash) != 64 || store.plan.Status != remediation.PlanAwaitingApproval {
		t.Fatalf("persisted plan=%+v", store.plan)
	}
}

func TestRemediationPrepareRejectsTaskSuppliedGitQueryBeforeLoader(t *testing.T) {
	called := false
	operation, err := NewRemediationPrepare(RemediationPrepareConfig{
		Loader: RemediationPrepareLoaderFunc(func(context.Context, asyncjob.Task) (RemediationPrepareInput, error) {
			called = true
			return RemediationPrepareInput{}, nil
		}),
		Store: &prepareTestStore{},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := asyncjob.Task{
		ID: 40, IncidentID: 11, CycleNo: 2, Queue: asyncjob.QueueInvestigate,
		Type: asyncjob.TaskRemediationPrepare, SubjectType: "agent_run", SubjectID: 30,
		Transition: "remediation.prepare", ExpectedSubjectVersion: 7,
		PayloadSchemaVersion: remediationPreparePayloadSchema,
		Payload:              json.RawMessage(`{"agent_run_id":"22222222-2222-4222-8222-222222222222","cycle_no":2,"repository":"attacker/controlled"}`),
	}
	result := operation(context.Background(), asyncjob.Execution{
		Task: task, Lease: asyncjob.Lease{TaskID: task.ID, Owner: "worker", Generation: 1, ExpectedSubjectVersion: task.ExpectedSubjectVersion, Attempt: 1, MaxAttempts: 3},
	})
	if result.Disposition != asyncjob.DispositionDead || result.ErrorCode != "invalid_remediation_payload" || called {
		t.Fatalf("result=%+v loader_called=%v", result, called)
	}
}

type prepareTestStore struct {
	plan *remediation.RemediationPlan
}

func (s *prepareTestStore) PersistIn(_ context.Context, _ asyncjob.DBTX, _ asyncjob.Task, _ RemediationPrepareInput, plan *remediation.RemediationPlan) error {
	copyPlan := *plan
	s.plan = &copyPlan
	return nil
}

func prepareTestRequest() remediation.RestoreEnvCompileRequest {
	current := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
  namespace: demo
spec:
  template:
    spec:
      containers:
        - name: demo
          image: example/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`)
	baseline := append(append([]byte(nil), current...), []byte("          env:\n            - name: REQUIRED_ENV\n              value: healthy\n")...)
	policy := remediation.RestoreEnvPolicy{
		Version: "restore-required-env-policy/v1", Repository: "acme/gitops", BaseBranch: "main",
		AllowedPath: "apps/demo.yaml", APIVersion: "apps/v1", Namespace: "demo",
		Workload: "demo", Container: "demo", EnvKey: "REQUIRED_ENV",
		MaxDiffBytes: remediation.MaxV3PlanDiffBytes, MaxPostImageBytes: remediation.MaxV3PostImageBytes,
		VerificationVersion: "golden-required-env/v1",
	}
	now := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	return remediation.RestoreEnvCompileRequest{
		IncidentPublicID: "11111111-1111-4111-8111-111111111111", IncidentID: 11,
		CycleNo: 2, IncidentVersion: 9, CreatedByAgentRunID: "22222222-2222-4222-8222-222222222222",
		DiagnosisHash: strings.Repeat("d", 64), Repository: policy.Repository, BaseBranch: policy.BaseBranch,
		BaseRevision: strings.Repeat("a", 40), LastKnownGoodRevision: strings.Repeat("b", 40),
		TargetPath: policy.AllowedPath, BaseBlobSHA: strings.Repeat("c", 40), ExpectedTreeHash: strings.Repeat("e", 40), FileMode: "100644",
		Target: remediation.TargetResource{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "demo", Name: "demo", Container: "demo"},
		EnvKey: "REQUIRED_ENV", CurrentContent: current, BaselineContent: baseline, Policy: policy,
		VerificationPlan:   json.RawMessage(`{"profile":"golden-required-env/v1","stability_window_seconds":60}`),
		Evidence:           []remediation.EvidenceBinding{{ID: "33333333-3333-4333-8333-333333333333", ContentHash: strings.Repeat("1", 64)}},
		BaselineIsAncestor: true, CreatedAt: now, ExpiresAt: now.Add(30 * time.Minute), PlanVersion: 1,
	}
}
