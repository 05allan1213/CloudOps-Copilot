package githubwrite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

type staticToken string

func (t staticToken) Token(context.Context) (string, error) { return string(t), nil }

func TestBranchPatternMatchesExternalRequiredCheck(t *testing.T) {
	incidentID := "11111111-1111-4111-8111-111111111111"
	for _, branch := range []string{
		"cloudops/incident-" + incidentID + "/plan-" + strings.Repeat("a", 12),
		"cloudops/incident-" + incidentID + "/plan-" + strings.Repeat("b", 64),
	} {
		if !branchPattern.MatchString(branch) {
			t.Fatalf("required-check branch was rejected: %s", branch)
		}
	}
	for _, branch := range []string{
		"cloudops/incident-" + incidentID + "/remediation-22222222-2222-4222-8222-222222222222",
		"cloudops/incident-" + incidentID + "/plan-" + strings.Repeat("a", 11),
		"cloudops/incident-" + incidentID + "/plan-" + strings.Repeat("A", 64),
	} {
		if branchPattern.MatchString(branch) {
			t.Fatalf("out-of-contract branch was accepted: %s", branch)
		}
	}
}

func TestClientCreatesOnlyOneCommitBranchAndDraftPR(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	commitSHA := strings.Repeat("b", 40)
	marker := "<!-- cloudops-remediation:22222222-2222-4222-8222-222222222222:" + strings.Repeat("c", 64) + " -->"
	var mu sync.Mutex
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer isolated-write-token" {
			t.Error("missing isolated authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/gitops/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/gitops/pulls":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/gitops/git/commits/"+baseSHA:
			_, _ = w.Write([]byte(`{"tree":{"sha":"tree-base"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/gitops/git/blobs":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"blob-new"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/gitops/git/trees":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"tree-new"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/gitops/git/commits":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sha":"` + commitSHA + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/gitops/git/refs":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/gitops/pulls":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["draft"] != true || !strings.Contains(body["body"].(string), marker) {
				t.Error("PR is not a marker-bound draft")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":17,"html_url":"https://github.example/pr/17","draft":true}`))
		default:
			t.Errorf("unexpected GitHub endpoint %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, TokenProvider: staticToken("isolated-write-token"), AllowedRepositories: []string{"acme/gitops"}, AllowedBaseBranches: []string{"main"}, AllowedPaths: []string{"apps/api.yaml"}, AllowInsecureForTests: true})
	if err != nil {
		t.Fatal(err)
	}
	request := remediation.DeliveryRequest{Repository: "acme/gitops", BaseRevision: baseSHA, BaseBranch: "main", Path: "apps/api.yaml", Content: []byte("kind: Deployment\n"), Branch: "cloudops/incident-11111111-1111-4111-8111-111111111111/plan-" + strings.Repeat("2", 64), CommitTitle: "approved remediation", PRTitle: "draft", PRBody: "bounded\n" + marker, Marker: marker}
	result, err := client.DeliverDraftPR(context.Background(), request)
	if err != nil || result.CommitSHA != commitSHA || result.PRNumber != 17 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, call := range calls {
		if strings.Contains(call, "merge") || strings.Contains(call, "workflows") || strings.Contains(call, "secrets") || strings.Contains(call, "DELETE") || strings.Contains(call, "PATCH") || strings.Contains(call, "PUT") {
			t.Fatalf("prohibited endpoint called: %s", call)
		}
	}
}

func TestClientRejectsSensitiveAndUnallowlistedPathsBeforeIO(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, TokenProvider: staticToken("token"), AllowedRepositories: []string{"acme/gitops"}, AllowedBaseBranches: []string{"main"}, AllowedPaths: []string{"apps/api.yaml"}, AllowInsecureForTests: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{".github/workflows/deploy.yml", "apps/secret.yaml", "../../rbac.yaml"} {
		if _, err := client.ReadBaseFile(context.Background(), "acme/gitops", strings.Repeat("a", 40), value); err == nil {
			t.Fatalf("sensitive path accepted: %s", value)
		}
	}
	if calls != 0 {
		t.Fatalf("unauthorized request reached network: %d", calls)
	}
}

func TestWritePermissionValidationRejectsExtraAuthority(t *testing.T) {
	if err := validateWritePermissions(map[string]string{"metadata": "read", "contents": "write", "pull_requests": "write"}); err != nil {
		t.Fatal(err)
	}
	if err := validateWritePermissions(map[string]string{"metadata": "read", "contents": "write", "pull_requests": "write", "workflows": "write"}); err == nil {
		t.Fatal("workflow permission accepted")
	}
}

func TestClientRejectsBaseRevisionDriftBeforeWrite(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":{"sha":"` + strings.Repeat("d", 40) + `"}}`))
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, TokenProvider: staticToken("token"), AllowedRepositories: []string{"acme/gitops"}, AllowedBaseBranches: []string{"main"}, AllowedPaths: []string{"apps/api.yaml"}, AllowInsecureForTests: true})
	if err != nil {
		t.Fatal(err)
	}
	id := "11111111-1111-4111-8111-111111111111"
	marker := "<!-- cloudops-remediation:" + id + ":" + strings.Repeat("c", 64) + " -->"
	_, err = client.DeliverDraftPR(context.Background(), remediation.DeliveryRequest{Repository: "acme/gitops", BaseRevision: strings.Repeat("a", 40), BaseBranch: "main", Path: "apps/api.yaml", Content: []byte("kind: Deployment\n"), Branch: "cloudops/incident-" + id + "/plan-" + strings.Repeat("2", 64), CommitTitle: "approved", PRTitle: "draft", PRBody: marker, Marker: marker})
	if err != remediation.ErrDrift || calls != 1 {
		t.Fatalf("base drift err=%v calls=%d", err, calls)
	}
}
