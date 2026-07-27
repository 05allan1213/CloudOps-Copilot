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
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

const (
	contractConsultationID  = "123e4567-e89b-12d3-a456-426614174010"
	contractAgentResourceID = "123e4567-e89b-12d3-a456-426614174011"
)

func TestAgentSSEResumesAfterLastEventID(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	stub := &agentWorkspacePortStub{cancelStream: cancel}
	engine := newContractEngine(NewHandler(Config{AgentWorkspace: stub}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/consultations/"+contractConsultationID+"/events", nil).WithContext(requestContext)
	request.Header.Set("Last-Event-ID", "event-before")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != SSEMediaType {
		t.Fatalf("SSE status/content-type=%d/%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "id: event-after") || !strings.Contains(response.Body.String(), "event: tool.completed") {
		t.Fatalf("resumed SSE body=%s", response.Body.String())
	}
	if len(stub.streamCursors) < 2 || stub.streamCursors[0] != "event-before" || stub.streamCursors[1] != "event-after" {
		t.Fatalf("stream cursors=%#v", stub.streamCursors)
	}
}

func TestAgentMessageUsesStableScopedIdempotencyKey(t *testing.T) {
	stub := &agentWorkspacePortStub{}
	engine := newContractEngine(NewHandler(Config{AgentWorkspace: stub}))
	for index := 0; index < 2; index++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/consultations/"+contractConsultationID+"/messages", strings.NewReader(`{"content":"检查当前错误日志"}`))
		request.Header.Set("Content-Type", JSONMediaType)
		request.Header.Set(IdempotencyHeader, "owner-message-1")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("message %d status=%d body=%s", index, response.Code, response.Body.String())
		}
	}
	if len(stub.messageKeys) != 2 || stub.messageKeys[0] != stub.messageKeys[1] || len(stub.messageKeys[0]) != 64 {
		t.Fatalf("scoped idempotency keys=%#v", stub.messageKeys)
	}
}

func TestAgentMessageRejectsBlankAndOversizedContentBeforePersistence(t *testing.T) {
	stub := &agentWorkspacePortStub{}
	engine := newContractEngine(NewHandler(Config{AgentWorkspace: stub}))
	for name, body := range map[string]string{
		"blank":     `{"content":"   "}`,
		"oversized": `{"content":"` + strings.Repeat("界", 16001) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/consultations/"+contractConsultationID+"/messages", strings.NewReader(body))
			request.Header.Set("Content-Type", JSONMediaType)
			request.Header.Set(IdempotencyHeader, "invalid-message")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			assertProblem(t, response, "INVALID_AGENT_REQUEST")
		})
	}
	if len(stub.messageKeys) != 0 {
		t.Fatalf("invalid messages reached persistence: %#v", stub.messageKeys)
	}
}

func TestAgentSnapshotAttachmentIsExplicit(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	telemetryStub := &telemetryPortStub{snapshot: telemetry.ContextSnapshot{ID: contractAgentResourceID, ConsultationID: contractConsultationID}}
	engine := newContractEngine(NewHandler(Config{AgentWorkspace: &agentWorkspacePortStub{}, Telemetry: telemetryStub}))
	body := `{
  "title":"新的日志上下文",
  "cluster_id":"kind-cloudops-local",
  "environment":"local",
  "namespaces":["cloudops-system"],
  "resource_refs":[{"id":"workload-1","kind":"Deployment","namespace":"cloudops-system","name":"cloudops-api"}],
  "from":"` + now.Add(-15*time.Minute).Format(time.RFC3339) + `",
  "to":"` + now.Format(time.RFC3339) + `",
  "query_execution_refs":["123e4567-e89b-12d3-a456-426614174012"],
  "evidence_refs":[]
}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/consultations/"+contractConsultationID+"/snapshots", strings.NewReader(body))
	request.Header.Set("Content-Type", JSONMediaType)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || telemetryStub.lastSnapshot.ClusterID != "kind-cloudops-local" || len(telemetryStub.lastSnapshot.QueryIDs) != 1 {
		t.Fatalf("snapshot status=%d request=%#v body=%s", response.Code, telemetryStub.lastSnapshot, response.Body.String())
	}
}

func TestAgentOperationAuthorizationRejectsWrongExactHashWithoutExecution(t *testing.T) {
	stub := &agentWorkspacePortStub{expectedPlanHash: strings.Repeat("a", 64)}
	engine := newContractEngine(NewHandler(Config{AgentWorkspace: stub}))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/operation-plans/"+contractAgentResourceID+"/authorizations", strings.NewReader(`{"expected_hash":"`+strings.Repeat("b", 64)+`","reason":"reviewed exact plan"}`))
	request.Header.Set("Content-Type", JSONMediaType)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("wrong hash status=%d body=%s", response.Code, response.Body.String())
	}
	assertProblem(t, response, "AGENT_STATE_CONFLICT")
	if stub.authorizationCalls != 1 || stub.proposalCalls != 0 {
		t.Fatalf("authority calls=%d proposals=%d", stub.authorizationCalls, stub.proposalCalls)
	}
}

type agentWorkspacePortStub struct {
	streamCursors      []string
	cancelStream       context.CancelFunc
	messageKeys        []string
	expectedPlanHash   string
	authorizationCalls int
	proposalCalls      int
}

func (stub *agentWorkspacePortStub) WorkspaceRuns(context.Context, int) ([]agent.WorkspaceRun, error) {
	return []agent.WorkspaceRun{}, nil
}
func (stub *agentWorkspacePortStub) WorkspaceRun(context.Context, string) (agent.WorkspaceRun, error) {
	return agent.WorkspaceRun{}, nil
}
func (stub *agentWorkspacePortStub) RequestCancel(context.Context, string) (agent.WorkspaceRun, error) {
	return agent.WorkspaceRun{}, nil
}
func (stub *agentWorkspacePortStub) Consultations(context.Context, int) ([]agent.ConsultationSummary, error) {
	return []agent.ConsultationSummary{}, nil
}
func (stub *agentWorkspacePortStub) Consultation(context.Context, string) (agent.ConsultationDetail, error) {
	return agent.ConsultationDetail{}, nil
}
func (stub *agentWorkspacePortStub) CreateConsultationTurn(_ context.Context, _ string, request agent.SendMessageRequest) (agent.ConsultationMessage, agent.WorkspaceRun, error) {
	stub.messageKeys = append(stub.messageKeys, request.IdempotencyKey)
	return agent.ConsultationMessage{ID: contractAgentResourceID, Content: request.Content}, agent.WorkspaceRun{ID: contractAgentResourceID}, nil
}
func (stub *agentWorkspacePortStub) RequestConsultationCancel(context.Context, string) (agent.WorkspaceRun, error) {
	return agent.WorkspaceRun{}, nil
}
func (stub *agentWorkspacePortStub) StreamEvents(_ context.Context, _ string, lastEventID string, _ int) ([]agent.StreamEvent, error) {
	stub.streamCursors = append(stub.streamCursors, lastEventID)
	if len(stub.streamCursors) == 1 {
		return []agent.StreamEvent{{
			ID: "event-after", RunID: contractAgentResourceID, ConsultationID: contractConsultationID,
			Sequence: 2, Type: "tool.completed", Payload: json.RawMessage(`{"tool":"logs.query"}`), CreatedAt: time.Now().UTC(),
		}}, nil
	}
	if stub.cancelStream != nil {
		stub.cancelStream()
	}
	return []agent.StreamEvent{}, nil
}
func (stub *agentWorkspacePortStub) KnowledgeItems(context.Context, int, bool) ([]agent.KnowledgeItem, error) {
	return []agent.KnowledgeItem{}, nil
}
func (stub *agentWorkspacePortStub) KnowledgeItem(context.Context, string) (agent.KnowledgeItem, error) {
	return agent.KnowledgeItem{}, nil
}
func (stub *agentWorkspacePortStub) CreateKnowledge(context.Context, agent.SaveKnowledgeRequest) (agent.KnowledgeItem, error) {
	return agent.KnowledgeItem{}, nil
}
func (stub *agentWorkspacePortStub) UpdateKnowledge(context.Context, string, agent.UpdateKnowledgeRequest) (agent.KnowledgeItem, error) {
	return agent.KnowledgeItem{}, nil
}
func (stub *agentWorkspacePortStub) DeleteKnowledge(context.Context, string) error { return nil }
func (stub *agentWorkspacePortStub) RunbookGuidance(context.Context) ([]agent.RunbookGuidance, error) {
	return []agent.RunbookGuidance{}, nil
}
func (stub *agentWorkspacePortStub) ProposeActionCard(context.Context, agent.ActionProposalRequest) (agent.ActionCard, error) {
	stub.proposalCalls++
	return agent.ActionCard{}, nil
}
func (stub *agentWorkspacePortStub) AuthorizeActionCard(context.Context, string, agent.AuthorizeActionRequest) (agent.ActionCard, error) {
	stub.authorizationCalls++
	return agent.ActionCard{}, nil
}
func (stub *agentWorkspacePortStub) ActionCard(context.Context, string) (agent.ActionCard, error) {
	return agent.ActionCard{}, nil
}
func (stub *agentWorkspacePortStub) ProposeOperationPlan(context.Context, agent.ActionProposalRequest) (agent.OperationPlan, error) {
	stub.proposalCalls++
	return agent.OperationPlan{}, nil
}
func (stub *agentWorkspacePortStub) AuthorizeOperationPlan(_ context.Context, id string, request agent.AuthorizeActionRequest) (agent.OperationPlan, error) {
	stub.authorizationCalls++
	if request.ExpectedHash != stub.expectedPlanHash {
		return agent.OperationPlan{}, agent.ErrConflict
	}
	return agent.OperationPlan{ID: id, ContentHash: request.ExpectedHash, Status: "authorized"}, nil
}
func (stub *agentWorkspacePortStub) OperationPlan(context.Context, string) (agent.OperationPlan, error) {
	return agent.OperationPlan{}, nil
}
func (stub *agentWorkspacePortStub) OperationPlans(context.Context, int) ([]agent.OperationPlan, error) {
	return []agent.OperationPlan{}, nil
}

var _ AgentWorkspacePort = (*agentWorkspacePortStub)(nil)
