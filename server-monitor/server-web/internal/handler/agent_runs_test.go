package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"server-web/internal/agent"
	"server-web/internal/middleware"
	authpkg "server-web/internal/service/auth"
)

const (
	testIncidentID = "11111111-1111-4111-8111-111111111111"
	testRunID      = "22222222-2222-4222-8222-222222222222"
)

func TestCreateAgentRunRequiresIdempotencyAndHidesPrivateRuntimeState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{agentRuntime: fakeAgentApplication{run: testAgentRun()}}
	router := gin.New()
	router.POST("/api/v2/incidents/:id/agent-runs", h.CreateAgentRun)

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodPost, "/api/v2/incidents/"+testIncidentID+"/agent-runs", nil))
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency status=%d", missing.Code)
	}

	created := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v2/incidents/"+testIncidentID+"/agent-runs", nil)
	request.Header.Set("Idempotency-Key", "request-1")
	router.ServeHTTP(created, request)
	if created.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", created.Code, created.Body.String())
	}
	body := created.Body.String()
	for _, private := range []string{"checkpoint", "lease_owner", "row_version", "idempotency_key"} {
		if strings.Contains(body, private) {
			t.Fatalf("private field %q leaked: %s", private, body)
		}
	}
}

func TestAgentStepDTOExposesHashNotArguments(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := fakeAgentApplication{run: testAgentRun(), steps: []agent.Step{{PublicID: "step-1", Sequence: 1, Node: agent.NodeExecuteTool, Status: agent.StepCompleted, Arguments: json.RawMessage(`{"secret":"value"}`), ArgumentsHash: strings.Repeat("a", 64)}}}
	h := &Handler{agentRuntime: app}
	router := gin.New()
	router.GET("/api/v2/agent-runs/:id/steps", h.ListAgentSteps)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/agent-runs/"+testRunID+"/steps", nil))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "secret") || !strings.Contains(response.Body.String(), strings.Repeat("a", 64)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAgentHTTPErrorAndRouteContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Run("invalid uuid", func(t *testing.T) {
		h := &Handler{agentRuntime: fakeAgentApplication{run: testAgentRun()}}
		router := gin.New()
		router.GET("/api/v2/agent-runs/:id", h.GetAgentRun)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/agent-runs/not-a-uuid", nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status=%d", response.Code)
		}
	})
	t.Run("dependency unavailable", func(t *testing.T) {
		h := &Handler{}
		router := gin.New()
		router.GET("/api/v2/agent-runs/:id", h.GetAgentRun)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/agent-runs/"+testRunID, nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d", response.Code)
		}
	})
	for name, appErr := range map[string]struct {
		err    error
		status int
	}{
		"unknown":  {agent.ErrNotFound, http.StatusNotFound},
		"conflict": {agent.ErrConflict, http.StatusConflict},
	} {
		t.Run(name, func(t *testing.T) {
			h := &Handler{agentRuntime: fakeAgentApplication{run: testAgentRun(), err: appErr.err}}
			router := gin.New()
			router.POST("/api/v2/incidents/:id/agent-runs", h.CreateAgentRun)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v2/incidents/"+testIncidentID+"/agent-runs", nil)
			request.Header.Set("Idempotency-Key", "same-key")
			router.ServeHTTP(response, request)
			if response.Code != appErr.status {
				t.Fatalf("status=%d", response.Code)
			}
		})
	}
}

func TestAgentRoutesAreProtectedByAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{agentRuntime: fakeAgentApplication{run: testAgentRun()}}
	router := gin.New()
	protected := router.Group("")
	protected.Use(middleware.Auth(rejectingAuth{}))
	protected.GET("/api/v2/agent-runs/:id", h.GetAgentRun)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/agent-runs/"+testRunID, nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestAgentHTTPSuccessMatrixAndDuplicateCreate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := fakeAgentApplication{run: testAgentRun(), steps: []agent.Step{{PublicID: "33333333-3333-4333-8333-333333333333", Sequence: 1, Node: agent.NodeLoadIncident, Status: agent.StepCompleted}}, evidence: []agent.EvidenceRecord{{PublicID: "44444444-4444-4444-8444-444444444444", ToolName: "alert.list_active", Summary: "bounded", Facts: json.RawMessage(`{"active":1}`), Valid: true}}}
	h := &Handler{agentRuntime: app}
	router := gin.New()
	router.POST("/api/v2/incidents/:id/agent-runs", h.CreateAgentRun)
	router.GET("/api/v2/incidents/:id/agent-runs", h.ListAgentRuns)
	router.GET("/api/v2/agent-runs/:id", h.GetAgentRun)
	router.GET("/api/v2/agent-runs/:id/steps", h.ListAgentSteps)
	router.GET("/api/v2/agent-runs/:id/evidence", h.ListAgentEvidence)
	router.POST("/api/v2/agent-runs/:id/cancel", h.CancelAgentRun)
	requests := []struct {
		method, path string
		status       int
		key          bool
	}{
		{http.MethodPost, "/api/v2/incidents/" + testIncidentID + "/agent-runs", http.StatusAccepted, true},
		{http.MethodPost, "/api/v2/incidents/" + testIncidentID + "/agent-runs", http.StatusAccepted, true},
		{http.MethodGet, "/api/v2/incidents/" + testIncidentID + "/agent-runs", http.StatusOK, false},
		{http.MethodGet, "/api/v2/agent-runs/" + testRunID, http.StatusOK, false},
		{http.MethodGet, "/api/v2/agent-runs/" + testRunID + "/steps", http.StatusOK, false},
		{http.MethodGet, "/api/v2/agent-runs/" + testRunID + "/evidence", http.StatusOK, false},
		{http.MethodPost, "/api/v2/agent-runs/" + testRunID + "/cancel", http.StatusAccepted, false},
	}
	for _, item := range requests {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(item.method, item.path, nil)
		if item.key {
			request.Header.Set("Idempotency-Key", "same-key")
		}
		router.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("%s %s status=%d body=%s", item.method, item.path, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "lease_owner") || strings.Contains(response.Body.String(), "checkpoint") {
			t.Fatalf("private DTO leak: %s", response.Body.String())
		}
	}
}

type fakeAgentApplication struct {
	run      *agent.Run
	steps    []agent.Step
	evidence []agent.EvidenceRecord
	err      error
}

func (f fakeAgentApplication) CreateRun(context.Context, string, string) (*agent.Run, error) {
	return f.run, f.err
}
func (f fakeAgentApplication) GetRun(context.Context, string) (*agent.Run, error) {
	return f.run, f.err
}
func (f fakeAgentApplication) ListRuns(context.Context, string, int, int) (agent.Page, error) {
	return agent.Page{Items: []agent.Run{*f.run}, Total: 1, Page: 1, PageSize: 20}, f.err
}
func (f fakeAgentApplication) ListSteps(context.Context, string, int) ([]agent.Step, error) {
	return f.steps, f.err
}
func (f fakeAgentApplication) ListEvidence(context.Context, string, int) ([]agent.EvidenceRecord, error) {
	return f.evidence, f.err
}
func (f fakeAgentApplication) Cancel(context.Context, string) error { return f.err }

type rejectingAuth struct{}

func (rejectingAuth) AuthenticateBearer(string) (authpkg.Identity, error) {
	return authpkg.Identity{}, errors.New("rejected")
}
func (rejectingAuth) AuthenticateToken(string) (authpkg.Identity, error) {
	return authpkg.Identity{}, errors.New("rejected")
}

func testAgentRun() *agent.Run {
	now := time.Now().UTC()
	return &agent.Run{PublicID: testRunID, IncidentPublicID: testIncidentID, Status: agent.RunPending, Limits: agent.Limits{MaxSteps: 12, MaxToolCalls: 6, MaxModelCalls: 8, TokenBudget: 12000, MaxEvidenceItems: 12}, Checkpoint: json.RawMessage(`{"private":true}`), LeaseOwner: "private-worker", RowVersion: 7, CreatedAt: now, UpdatedAt: now}
}
