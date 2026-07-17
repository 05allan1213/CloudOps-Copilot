package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"server-web/internal/middleware"
	"server-web/internal/remediation"
	"server-web/internal/verification"
)

type fakeFastDemoApplication struct{}

func (fakeFastDemoApplication) CreatePlan(context.Context, string) (*remediation.RemediationPlan, error) {
	return nil, nil
}
func (fakeFastDemoApplication) Execute(context.Context, string) (*verification.Run, error) {
	return nil, nil
}
func (fakeFastDemoApplication) Verify(context.Context, string) (*verification.Run, error) {
	return nil, nil
}

type fakeRemediationApplication struct {
	plan         remediation.RemediationPlan
	delivery     remediation.ChangeRequest
	approveCalls int
	approveActor string
}

func (f *fakeRemediationApplication) Enabled() bool { return true }
func (f *fakeRemediationApplication) List(context.Context, remediation.ListFilter) (remediation.Page, error) {
	return remediation.Page{Items: []remediation.RemediationPlan{f.plan}, Total: 1, Page: 1, PageSize: 20}, nil
}
func (f *fakeRemediationApplication) Get(context.Context, string) (*remediation.RemediationPlan, error) {
	return &f.plan, nil
}
func (f *fakeRemediationApplication) GetApproval(context.Context, string) (*remediation.Approval, error) {
	return nil, remediation.ErrNotFound
}
func (f *fakeRemediationApplication) Approve(_ context.Context, _, actor, role, _, _ string, _ uint64) (*remediation.RemediationPlan, *remediation.ChangeRequest, error) {
	f.approveCalls++
	f.approveActor = actor
	if role != "admin" {
		return nil, nil, remediation.ErrForbidden
	}
	return &f.plan, &f.delivery, nil
}
func (f *fakeRemediationApplication) Reject(context.Context, string, string, string, string, string, uint64) (*remediation.RemediationPlan, error) {
	return &f.plan, nil
}

func TestRemediationHTTPDTOAndApproval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := "11111111-1111-4111-8111-111111111111"
	app := &fakeRemediationApplication{plan: remediation.RemediationPlan{ID: 99, PublicID: id, IncidentPublicID: "22222222-2222-4222-8222-222222222222", PlanHash: strings.Repeat("a", 64), ProposedPatchHash: strings.Repeat("b", 64), Status: remediation.PlanAwaitingApproval, RowVersion: 1}, delivery: remediation.ChangeRequest{ID: 88, PublicID: "33333333-3333-4333-8333-333333333333", IdempotencyKey: strings.Repeat("c", 64), Status: remediation.DeliveryPending}}
	h := &Handler{remediation: app}
	router := gin.New()
	router.GET("/api/v2/remediations/:id", h.GetRemediation)
	router.POST("/api/v2/remediations/:id/approve", func(c *gin.Context) {
		c.Set(middleware.ContextUsername, "admin")
		c.Set(middleware.ContextRole, "admin")
		h.ApproveRemediation(c)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v2/remediations/"+id, nil))
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), `"ID":99`) || strings.Contains(recorder.Body.String(), "idempotency") {
		t.Fatalf("unsafe DTO response: %d %s", recorder.Code, recorder.Body.String())
	}

	body, _ := json.Marshal(approvalRequest{PlanHash: app.plan.PlanHash, PatchHash: app.plan.ProposedPatchHash, Version: 1})
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v2/remediations/"+id+"/approve", strings.NewReader(string(body))))
	if recorder.Code != http.StatusOK || app.approveCalls != 1 || strings.Contains(recorder.Body.String(), app.delivery.IdempotencyKey) {
		t.Fatalf("approval response: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestRemediationHTTPRejectsMissingFormalIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := "11111111-1111-4111-8111-111111111111"
	app := &fakeRemediationApplication{plan: remediation.RemediationPlan{PlanHash: strings.Repeat("a", 64), ProposedPatchHash: strings.Repeat("b", 64)}}
	h := &Handler{remediation: app}
	router := gin.New()
	router.POST("/api/v2/remediations/:id/approve", h.ApproveRemediation)
	body, _ := json.Marshal(approvalRequest{PlanHash: app.plan.PlanHash, PatchHash: app.plan.ProposedPatchHash, Version: 1})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v2/remediations/"+id+"/approve", strings.NewReader(string(body))))
	if recorder.Code != http.StatusForbidden || app.approveCalls != 0 {
		t.Fatalf("missing formal identity accepted: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestRemediationHTTPUsesExplicitDemoActorOnlyWhenDemoIsInstalled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := "11111111-1111-4111-8111-111111111111"
	app := &fakeRemediationApplication{plan: remediation.RemediationPlan{PlanHash: strings.Repeat("a", 64), ProposedPatchHash: strings.Repeat("b", 64)}}
	h := &Handler{remediation: app, fastDemo: fakeFastDemoApplication{}, fastDemoActor: "demo-operator"}
	router := gin.New()
	router.POST("/api/v2/remediations/:id/approve", h.ApproveRemediation)
	body, _ := json.Marshal(approvalRequest{PlanHash: app.plan.PlanHash, PatchHash: app.plan.ProposedPatchHash, Version: 1})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v2/remediations/"+id+"/approve", strings.NewReader(string(body))))
	if recorder.Code != http.StatusOK || app.approveActor != "demo-operator" {
		t.Fatalf("explicit demo actor not used: actor=%q response=%d %s", app.approveActor, recorder.Code, recorder.Body.String())
	}
}

func TestRemediationHTTPRejectsUnknownApprovalFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := "11111111-1111-4111-8111-111111111111"
	app := &fakeRemediationApplication{}
	h := &Handler{remediation: app}
	router := gin.New()
	router.POST("/api/v2/remediations/:id/approve", h.ApproveRemediation)
	recorder := httptest.NewRecorder()
	body := `{"plan_hash":"` + strings.Repeat("a", 64) + `","patch_hash":"` + strings.Repeat("b", 64) + `","version":1,"actor":"attacker"}`
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v2/remediations/"+id+"/approve", strings.NewReader(body)))
	if recorder.Code != http.StatusBadRequest || app.approveCalls != 0 {
		t.Fatalf("unknown field accepted: %d %s", recorder.Code, recorder.Body.String())
	}
}
