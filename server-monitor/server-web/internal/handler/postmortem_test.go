package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"server-web/internal/verification"
)

type fakePostmortemApp struct {
	item *verification.Postmortem
	err  error
}

func (fakePostmortemApp) DeliveryEnabled() bool     { return true }
func (fakePostmortemApp) VerificationEnabled() bool { return true }
func (fakePostmortemApp) GetDelivery(context.Context, string) (*verification.Delivery, error) {
	return nil, verification.ErrNotFound
}
func (fakePostmortemApp) ListRuns(context.Context, string, int, int) (verification.RunPage, error) {
	return verification.RunPage{}, nil
}
func (fakePostmortemApp) GetRun(context.Context, string, string) (*verification.Run, []verification.Check, error) {
	return nil, nil, verification.ErrNotFound
}
func (f fakePostmortemApp) GetPostmortem(context.Context, string) (*verification.Postmortem, error) {
	return f.item, f.err
}

func TestPostmortemAPIIsBoundedAndNotFoundIsExplicit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	incidentID := "11111111-1111-4111-8111-111111111111"
	item := &verification.Postmortem{PublicID: "22222222-2222-4222-8222-222222222222", IncidentPublicID: incidentID, VerificationRunPublicID: "33333333-3333-4333-8333-333333333333", Title: "bounded", RootCause: verification.ClassifiedFact{Classification: "unknown", Summary: "unknown"}, GeneratedAt: time.Now().UTC()}
	for name, app := range map[string]fakePostmortemApp{"success": {item: item}, "not_found": {err: verification.ErrNotFound}} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v2/incidents/"+incidentID+"/postmortem", nil)
			ctx.Params = gin.Params{{Key: "id", Value: incidentID}}
			h := &Handler{deliveryVerification: app}
			h.GetIncidentPostmortem(ctx)
			if name == "not_found" {
				if recorder.Code != http.StatusNotFound {
					t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
				}
				return
			}
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d", recorder.Code)
			}
			var body map[string]any
			if json.Unmarshal(recorder.Body.Bytes(), &body) != nil {
				t.Fatal("invalid response")
			}
			encoded := recorder.Body.String()
			for _, forbidden := range []string{"lease_owner", "row_version", "idempotency_key", "raw_query", "token", "private_reasoning"} {
				if containsJSONField(encoded, forbidden) {
					t.Fatalf("forbidden field %s", forbidden)
				}
			}
		})
	}
}

func containsJSONField(body, field string) bool {
	return len(body) > 0 && json.Valid([]byte(body)) && stringContains(body, `"`+field+`"`)
}
func stringContains(body, value string) bool {
	for i := 0; i+len(value) <= len(body); i++ {
		if body[i:i+len(value)] == value {
			return true
		}
	}
	return false
}
