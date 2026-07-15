package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"server-web/internal/change"
	"server-web/internal/middleware"
	"server-web/internal/service/changeintelligence"
)

type fakeChangeApplication struct {
	enabled bool
	page    change.Page
	context changeintelligence.Context
}

func (f *fakeChangeApplication) Enabled() bool { return f.enabled }
func (f *fakeChangeApplication) ListChanges(context.Context, string, change.ListFilter) (change.Page, error) {
	return f.page, nil
}
func (f *fakeChangeApplication) GetContext(context.Context, string) (changeintelligence.Context, error) {
	return f.context, nil
}

func TestChangeEndpointsReturnPublicBoundedDTOs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	item := change.Change{ID: 42, PublicID: "3f3995d8-85d8-4b1c-95af-ea64a329af93", IncidentID: 7, SourceType: change.SourceGitHubCommit, Repository: "acme/api", CommitSHA: "abcdef1", Status: change.StatusMatched, Category: change.CategoryConfirmed, CorrelationScore: 100, CorrelationReasons: []string{"revision_exact"}, Metadata: []byte(`{"safe":true}`), IdempotencyKey: strings.Repeat("a", 64), CreatedAt: now}
	app := &fakeChangeApplication{enabled: true, page: change.Page{Items: []change.Change{item}, Total: 1, Page: 1, PageSize: 20}, context: changeintelligence.Context{Enabled: true, Status: "ok", Candidates: []change.Change{item}}}
	h := &Handler{changeService: app}
	router := gin.New()
	router.GET("/api/v2/incidents/:id/changes", h.ListIncidentChanges)
	router.GET("/api/v2/incidents/:id/change-context", h.GetIncidentChangeContext)
	for _, path := range []string{"/api/v2/incidents/" + testIncidentID + "/changes", "/api/v2/incidents/" + testIncidentID + "/change-context"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"3f3995d8-85d8-4b1c-95af-ea64a329af93"`) {
			t.Fatalf("path=%s code=%d body=%s", path, response.Code, response.Body.String())
		}
		for _, forbidden := range []string{"IdempotencyKey", "idempotency_key", `"IncidentID"`, `"ID":42`} {
			if strings.Contains(response.Body.String(), forbidden) {
				t.Fatalf("private persistence field %q leaked: %s", forbidden, response.Body.String())
			}
		}
	}
	bad := httptest.NewRecorder()
	router.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/api/v2/incidents/"+testIncidentID+"/changes?page=0", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", bad.Code, bad.Body.String())
	}
}

func TestChangeFeatureOffSemantics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{changeService: &fakeChangeApplication{}}
	router := gin.New()
	router.GET("/changes/:id", h.ListIncidentChanges)
	router.GET("/context/:id", h.GetIncidentChangeContext)
	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/changes/"+testIncidentID, nil))
	if list.Code != http.StatusServiceUnavailable {
		t.Fatalf("list code=%d body=%s", list.Code, list.Body.String())
	}
	contextResponse := httptest.NewRecorder()
	router.ServeHTTP(contextResponse, httptest.NewRequest(http.MethodGet, "/context/"+testIncidentID, nil))
	if contextResponse.Code != http.StatusOK || !strings.Contains(contextResponse.Body.String(), `"enabled":false`) {
		t.Fatalf("context code=%d body=%s", contextResponse.Code, contextResponse.Body.String())
	}
}

func TestChangeEndpointsAreProtectedByAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{changeService: &fakeChangeApplication{enabled: true}}
	router := gin.New()
	protected := router.Group("")
	protected.Use(middleware.Auth(rejectingAuth{}))
	protected.GET("/api/v2/incidents/:id/changes", h.ListIncidentChanges)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/incidents/"+testIncidentID+"/changes", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
