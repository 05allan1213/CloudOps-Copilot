package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/operation"
)

func TestControlledOperationExecutionAndDevOpsContracts(t *testing.T) {
	hash := strings.Repeat("a", 64)
	execution := operation.Execution{
		ID: contractAgentResourceID, SubjectType: operation.SubjectActionCard,
		SubjectID: contractAgentResourceID, RunID: contractAgentResourceID,
		ConfigurationRevisionID: contractAgentResourceID, OperationType: operation.ActionSetChangeFreeze,
		ExpectedContentHash: hash, Status: "ready", CreatedAt: time.Now().UTC(),
		Events: []operation.AuditEvent{}, Links: []operation.ContextLink{},
	}
	stub := &operationPortStub{
		execution: execution,
		workspace: operation.DevOpsWorkspace{
			OperationPlans: []agent.OperationPlan{}, ActionCards: []agent.ActionCard{},
			Executions: []operation.Execution{execution},
			ChangeFreezes: []operation.ChangeFreezeState{{
				Target: operation.OperationTarget{
					ClusterID: "cloudops-local", Environment: "local", Namespace: "demo",
					WorkloadKind: "Deployment", WorkloadName: "cloudops-api",
				},
				Enabled: true, Reason: "maintenance", RowVersion: 1,
			}},
			ChangeCandidates: []operation.ChangeCandidate{}, DeploymentBaselines: []operation.DeploymentBaseline{},
			Deliveries: []operation.DeliveryProjection{}, Providers: []operation.ProviderBranch{}, CollectedAt: time.Now().UTC(),
		},
	}
	engine := newContractEngine(NewHandler(Config{Operations: stub}))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/action-cards/"+contractAgentResourceID+"/executions",
		strings.NewReader(`{"expected_hash":"`+hash+`"}`))
	request.Header.Set("Content-Type", JSONMediaType)
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || stub.cardCalls != 1 || stub.lastHash != hash {
		t.Fatalf("action execution status=%d calls=%d hash=%q body=%s", response.Code, stub.cardCalls, stub.lastHash, response.Body.String())
	}

	stub.err = operation.ErrUnauthorized
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/operation-plans/"+contractAgentResourceID+"/executions",
		strings.NewReader(`{"expected_hash":"`+hash+`"}`))
	request.Header.Set("Content-Type", JSONMediaType)
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("unauthorized Plan execution status=%d body=%s", response.Code, response.Body.String())
	}
	assertProblem(t, response, "ACTION_AUTHORIZATION_REQUIRED")
	stub.err = nil

	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/operations?limit=20", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), contractAgentResourceID) {
		t.Fatalf("operation list status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/devops?limit=20", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("DevOps status=%d body=%s", response.Code, response.Body.String())
	}
	var workspace operation.DevOpsWorkspace
	if err := json.Unmarshal(response.Body.Bytes(), &workspace); err != nil {
		t.Fatal(err)
	}
	if len(workspace.Executions) != 1 || len(workspace.ChangeFreezes) != 1 || !workspace.ChangeFreezes[0].Enabled {
		t.Fatalf("DevOps workspace=%#v", workspace)
	}
}

func TestControlledOperationErrorsAndUnwiredCapabilityFailClosed(t *testing.T) {
	for name, value := range map[string]struct {
		err  error
		code string
	}{
		"expired":          {operation.ErrExpired, "ACTION_AUTHORIZATION_EXPIRED"},
		"revision changed": {operation.ErrRevisionChanged, "CONFIGURATION_REVISION_CHANGED"},
		"state conflict":   {operation.ErrConflict, "OPERATION_STATE_CONFLICT"},
	} {
		t.Run(name, func(t *testing.T) {
			stub := &operationPortStub{err: value.err}
			engine := newContractEngine(NewHandler(Config{Operations: stub}))
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/operations/"+contractAgentResourceID, nil))
			if response.Code != http.StatusConflict {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			assertProblem(t, response, value.code)
		})
	}
	engine := newContractEngine(NewHandler(Config{}))
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/devops", nil))
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("unwired DevOps status=%d body=%s", response.Code, response.Body.String())
	}
	assertProblem(t, response, "NOT_IMPLEMENTED")
}

type operationPortStub struct {
	execution operation.Execution
	workspace operation.DevOpsWorkspace
	err       error
	lastHash  string
	cardCalls int
	planCalls int
}

func (stub *operationPortStub) EnqueueActionCard(_ context.Context, _ string, request operation.ExecuteRequest) (operation.Execution, error) {
	stub.cardCalls++
	stub.lastHash = request.ExpectedHash
	return stub.execution, stub.err
}

func (stub *operationPortStub) EnqueueOperationPlan(_ context.Context, _ string, request operation.ExecuteRequest) (operation.Execution, error) {
	stub.planCalls++
	stub.lastHash = request.ExpectedHash
	return stub.execution, stub.err
}

func (stub *operationPortStub) Execution(context.Context, string) (operation.Execution, error) {
	return stub.execution, stub.err
}

func (stub *operationPortStub) Executions(context.Context, int) ([]operation.Execution, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	return []operation.Execution{stub.execution}, nil
}

func (stub *operationPortStub) Workspace(context.Context, int) (operation.DevOpsWorkspace, error) {
	return stub.workspace, stub.err
}

var _ OperationPort = (*operationPortStub)(nil)
