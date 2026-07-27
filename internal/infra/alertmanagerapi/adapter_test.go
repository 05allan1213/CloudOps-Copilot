package alertmanagerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	alertdomain "github.com/05allan1213/CloudOps-Copilot/internal/alert"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/google/uuid"
)

type fakeAccessStore struct {
	access      settings.ProviderAccess
	gotID       string
	gotProvider settings.Provider
}

func (s *fakeAccessStore) ProviderAccess(_ context.Context, revisionID string, provider settings.Provider) (settings.ProviderAccess, error) {
	s.gotID, s.gotProvider = revisionID, provider
	return s.access, nil
}

func TestCreateSilenceUsesAlertmanagerWireContractAndClearsCredential(t *testing.T) {
	var postBody map[string]any
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v2/silences":
			_, _ = w.Write([]byte(`[]`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v2/silences":
			if err := json.NewDecoder(request.Body).Decode(&postBody); err != nil {
				t.Fatalf("decode provider request: %v", err)
			}
			_, _ = w.Write([]byte(`{"silenceID":"provider-silence-1"}`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	revisionID := uuid.NewString()
	credential := []byte("provider-token")
	store := &fakeAccessStore{access: settings.ProviderAccess{
		Configuration: settings.ProviderConfiguration{
			Provider: settings.ProviderAlertmanager, Enabled: true, Endpoint: server.URL, TimeoutMS: 2000,
		},
		Credential: credential,
	}}
	adapter, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	publicID := uuid.NewString()
	startsAt := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	providerID, err := adapter.CreateSilence(context.Background(), alertdomain.SilenceProviderRequest{
		ExternalID: "cloudops-silence:" + publicID, ConfigurationRevisionID: revisionID,
		Matchers: []alertdomain.Matcher{{Name: "alertname", Value: "CloudOpsTask5", IsEqual: true}},
		StartsAt: startsAt, EndsAt: startsAt.Add(30 * time.Minute),
		Comment: "cloudops-silence:" + publicID, CreatedBy: "owner",
	})
	if err != nil || providerID != "provider-silence-1" {
		t.Fatalf("CreateSilence() = %q, %v", providerID, err)
	}
	if authorization != "Bearer provider-token" || store.gotID != revisionID || store.gotProvider != settings.ProviderAlertmanager {
		t.Fatalf("access boundary mismatch: authorization=%q revision=%q provider=%q", authorization, store.gotID, store.gotProvider)
	}
	matchers, ok := postBody["matchers"].([]any)
	if !ok || len(matchers) != 1 {
		t.Fatalf("matchers = %#v", postBody["matchers"])
	}
	matcher, ok := matchers[0].(map[string]any)
	if !ok || matcher["isRegex"] != false || matcher["isEqual"] != true {
		t.Fatalf("Alertmanager matcher wire shape = %#v", matchers[0])
	}
	if _, exists := matcher["is_regex"]; exists {
		t.Fatalf("snake_case matcher leaked to Provider: %#v", matcher)
	}
	if strings.Trim(string(credential), "\x00") != "" {
		t.Fatalf("resolved credential was not cleared: %v", credential)
	}
}

func TestCreateSilenceReusesActiveExternalIdentity(t *testing.T) {
	postCount := 0
	publicID := uuid.NewString()
	comment := "cloudops-silence:" + publicID
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			postCount++
		}
		_, _ = w.Write([]byte(`[{"id":"existing-id","comment":"` + comment + `","status":{"state":"active"}}]`))
	}))
	defer server.Close()
	store := &fakeAccessStore{access: settings.ProviderAccess{Configuration: settings.ProviderConfiguration{
		Provider: settings.ProviderAlertmanager, Enabled: true, Endpoint: server.URL, TimeoutMS: 2000,
	}}}
	adapter, _ := New(store)
	startsAt := time.Now().UTC()
	providerID, err := adapter.CreateSilence(context.Background(), alertdomain.SilenceProviderRequest{
		ExternalID: comment, ConfigurationRevisionID: uuid.NewString(),
		Matchers: []alertdomain.Matcher{{Name: "alertname", Value: "CloudOpsTask5", IsEqual: true}},
		StartsAt: startsAt, EndsAt: startsAt.Add(5 * time.Minute), Comment: comment, CreatedBy: "owner",
	})
	if err != nil || providerID != "existing-id" || postCount != 0 {
		t.Fatalf("idempotent CreateSilence() = %q, %v, postCount=%d", providerID, err, postCount)
	}
}
