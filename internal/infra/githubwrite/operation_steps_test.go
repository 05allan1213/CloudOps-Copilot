package githubwrite

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

func TestPhasedWriterPerformsAtMostOneWriteAndReconcilesEveryPhase(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	baseBlobSHA := strings.Repeat("b", 40)
	commitSHA := strings.Repeat("c", 40)
	treeSHA := strings.Repeat("d", 40)
	baseContent := []byte("kind: Deployment\nmetadata:\n  name: demo\n")
	postContent := []byte("kind: Deployment\nmetadata:\n  name: demo\nspec:\n  restored: true\n")
	request := changeWriteRequest(baseSHA, baseBlobSHA, treeSHA, baseContent, postContent)

	var mu sync.Mutex
	branchSHA := ""
	prCreated := false
	writes := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/gitops/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/gitops/pulls":
			if prCreated {
				_, _ = w.Write([]byte(`[{"number":17,"html_url":"https://github.example/pr/17","body":"` + request.Marker + `","draft":true,"head":{"sha":"` + commitSHA + `"}}]`))
			} else {
				_, _ = w.Write([]byte(`[]`))
			}
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/cloudops/"):
			if branchSHA == "" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"not found"}`))
			} else {
				_, _ = w.Write([]byte(`{"object":{"sha":"` + branchSHA + `"}}`))
			}
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/gitops/git/refs":
			writes = append(writes, "ensure_branch")
			branchSHA = baseSHA
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/gitops/contents/apps/demo.yaml" && r.URL.Query().Get("ref") == baseSHA:
			writeContentResponse(t, w, baseBlobSHA, baseContent)
		case r.Method == http.MethodPut && r.URL.Path == "/repos/acme/gitops/contents/apps/demo.yaml":
			var body struct {
				Message string `json:"message"`
				Content string `json:"content"`
				SHA     string `json:"sha"`
				Branch  string `json:"branch"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			decoded, _ := base64.StdEncoding.DecodeString(body.Content)
			if body.Message != request.CommitTitle || body.SHA != baseBlobSHA || body.Branch != request.Branch || string(decoded) != string(postContent) {
				t.Errorf("unexpected commit body: %+v content=%q", body, decoded)
			}
			writes = append(writes, "ensure_commit")
			branchSHA = commitSHA
			_, _ = w.Write([]byte(`{"commit":{"sha":"` + commitSHA + `","tree":{"sha":"` + treeSHA + `"}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/gitops/git/commits/"+commitSHA:
			_, _ = w.Write([]byte(`{"message":"` + request.CommitTitle + `","tree":{"sha":"` + treeSHA + `"},"parents":[{"sha":"` + baseSHA + `"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/gitops/contents/apps/demo.yaml" && r.URL.Query().Get("ref") == commitSHA:
			writeContentResponse(t, w, strings.Repeat("e", 40), postContent)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/gitops/pulls":
			writes = append(writes, "ensure_draft_pr")
			prCreated = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":17,"html_url":"https://github.example/pr/17","draft":true}`))
		default:
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, TokenProvider: staticToken("token"), AllowedRepositories: []string{"acme/gitops"}, AllowedBaseBranches: []string{"main"}, AllowedPaths: []string{"apps/demo.yaml"}, AllowInsecureForTests: true})
	if err != nil {
		t.Fatal(err)
	}
	assertOneWrite(t, &writes, func() error {
		observation, callErr := client.EnsureBranch(context.Background(), request)
		if callErr == nil && observation.Step != remediation.OperationStepEnsureCommit {
			t.Fatalf("branch observation=%+v", observation)
		}
		return callErr
	})
	assertNoWrite(t, &writes, func() error {
		observation, callErr := client.EnsureBranch(context.Background(), request)
		if callErr == nil && (!observation.Reconciled || observation.Step != remediation.OperationStepEnsureCommit) {
			t.Fatalf("reconciled branch observation=%+v", observation)
		}
		return callErr
	})
	assertOneWrite(t, &writes, func() error {
		observation, callErr := client.EnsureCommit(context.Background(), request)
		if callErr == nil && observation.Step != remediation.OperationStepEnsureDraftPR {
			t.Fatalf("commit observation=%+v", observation)
		}
		return callErr
	})
	assertNoWrite(t, &writes, func() error {
		observation, callErr := client.EnsureCommit(context.Background(), request)
		if callErr == nil && (!observation.Reconciled || observation.Step != remediation.OperationStepEnsureDraftPR) {
			t.Fatalf("reconciled commit observation=%+v", observation)
		}
		return callErr
	})
	assertOneWrite(t, &writes, func() error {
		observation, callErr := client.EnsureDraftPR(context.Background(), request)
		if callErr == nil && (observation.Step != remediation.OperationStepComplete || observation.PRNumber != 17) {
			t.Fatalf("PR observation=%+v", observation)
		}
		return callErr
	})
	assertNoWrite(t, &writes, func() error {
		observation, callErr := client.EnsureDraftPR(context.Background(), request)
		if callErr == nil && (!observation.Reconciled || observation.Step != remediation.OperationStepComplete || observation.PRNumber != 17) {
			t.Fatalf("reconciled PR observation=%+v", observation)
		}
		return callErr
	})
	if got := strings.Join(writes, ","); got != "ensure_branch,ensure_commit,ensure_draft_pr" {
		t.Fatalf("writes=%s", got)
	}
}

func TestPhasedWriterRejectsUnboundPostImageBeforeIO(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, TokenProvider: staticToken("token"), AllowedRepositories: []string{"acme/gitops"}, AllowedBaseBranches: []string{"main"}, AllowedPaths: []string{"apps/demo.yaml"}, AllowInsecureForTests: true})
	if err != nil {
		t.Fatal(err)
	}
	request := changeWriteRequest(strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("d", 40), []byte("before\n"), []byte("after\n"))
	request.ExpectedPostImageHash = strings.Repeat("f", 64)
	if _, err := client.EnsureBranch(context.Background(), request); err != remediation.ErrForbidden {
		t.Fatalf("err=%v", err)
	}
	if calls != 0 {
		t.Fatalf("unbound request reached GitHub: %d", calls)
	}
}

func changeWriteRequest(baseSHA, baseBlobSHA, treeSHA string, baseContent, postContent []byte) remediation.ChangeWriteRequest {
	incidentID := "11111111-1111-4111-8111-111111111111"
	planID := "22222222-2222-4222-8222-222222222222"
	patchHash := strings.Repeat("3", 64)
	marker := "<!-- cloudops-remediation:" + planID + ":" + patchHash + " -->"
	return remediation.ChangeWriteRequest{
		DeliveryRequest: remediation.DeliveryRequest{
			Repository: "acme/gitops", BaseRevision: baseSHA, BaseBranch: "main", Path: "apps/demo.yaml",
			Content: postContent, Branch: "cloudops/incident-" + incidentID + "/plan-" + strings.Repeat("2", 64),
			CommitTitle: "cloudops: approved remediation " + planID, PRTitle: "[Draft] restore REQUIRED_ENV",
			PRBody: "Bounded exact diff.\n\n" + marker, Marker: marker,
		},
		BaseBlobSHA: baseBlobSHA, ExpectedBeforeHash: remediation.HashBytes(baseContent),
		ExpectedPostImageHash: remediation.HashBytes(postContent), ExpectedTreeHash: treeSHA,
		LogicalOperationKey: strings.Repeat("4", 64),
	}
}

func writeContentResponse(t *testing.T, w http.ResponseWriter, sha string, content []byte) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(content), "sha": sha, "size": len(content)})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(payload)
}

func assertOneWrite(t *testing.T, writes *[]string, call func() error) {
	t.Helper()
	before := len(*writes)
	if err := call(); err != nil {
		t.Fatal(err)
	}
	if delta := len(*writes) - before; delta != 1 {
		t.Fatalf("write delta=%d want=1", delta)
	}
}

func assertNoWrite(t *testing.T, writes *[]string, call func() error) {
	t.Helper()
	before := len(*writes)
	if err := call(); err != nil {
		t.Fatal(err)
	}
	if delta := len(*writes) - before; delta != 0 {
		t.Fatalf("write delta=%d want=0", delta)
	}
}
