package githubread

import (
	"context"
	"crypto/sha1" // #nosec G505 -- test fixture computes Git SHA-1 object identities.
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

func TestReadRestoreFactsUsesOnlyExactAllowlistedGETs(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	baselineSHA := strings.Repeat("9", 40)
	current := []byte("kind: Deployment\nmetadata:\n  name: demo\n")
	baseline := []byte("kind: Deployment\nmetadata:\n  name: demo\nspec: {}\n")
	currentBlob := testGitObjectHash("blob", current)
	baselineBlob := testGitObjectHash("blob", baseline)
	deployTree := testGitObjectHash("tree", testRawTree("100644", "app.yaml", currentBlob))
	rootTree := testGitObjectHash("tree", testRawTree("40000", "deploy", deployTree))

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.Body == nil || r.ContentLength > 0 {
			t.Fatalf("non-read GitHub request: method=%s length=%d", r.Method, r.ContentLength)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/app/commits/main":
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": baseSHA, "commit": map[string]any{"tree": map[string]any{"sha": rootTree}}})
		case "/repos/acme/app/contents/deploy/app.yaml":
			content, sha := current, currentBlob
			if r.URL.Query().Get("ref") == baselineSHA {
				content, sha = baseline, baselineBlob
			} else if r.URL.Query().Get("ref") != baseSHA {
				t.Fatalf("unexpected content ref %q", r.URL.Query().Get("ref"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type": "file", "path": "deploy/app.yaml", "sha": sha, "size": len(content),
				"encoding": "base64", "content": base64.StdEncoding.EncodeToString(content),
			})
		case "/repos/acme/app/git/trees/" + rootTree:
			if r.URL.Query().Get("recursive") != "1" {
				t.Fatal("recursive tree flag missing")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": rootTree, "truncated": false, "tree": []map[string]any{
				{"path": "deploy", "mode": "040000", "type": "tree", "sha": deployTree},
				{"path": "deploy/app.yaml", "mode": "100644", "type": "blob", "sha": currentBlob},
			}})
		case "/repos/acme/app/compare/" + baselineSHA + "..." + baseSHA:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ahead", "ahead_by": 1, "behind_by": 0,
				"base_commit": map[string]any{"sha": baselineSHA}, "merge_base_commit": map[string]any{"sha": baselineSHA},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, Config{
		AllowedRepositories: []string{"acme/app"}, AllowedBranches: []string{"main"}, AllowedPaths: []string{"deploy/app.yaml"},
	})
	facts, err := client.ReadRestoreFacts(context.Background(), remediation.ExactGitRestoreQuery{
		Repository: "acme/app", BaseBranch: "main", TargetPath: "deploy/app.yaml", BaselineRevision: baselineSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 5 || facts.BaseRevision != baseSHA || facts.BaseTreeSHA != rootTree || facts.BaseBlobSHA != currentBlob ||
		facts.BaselineBlobSHA != baselineBlob || !facts.BaselineIsAncestor || string(facts.BaselineContent) != string(baseline) {
		t.Fatalf("calls=%d facts=%+v", calls.Load(), facts)
	}
	before := calls.Load()
	_, err = client.ReadRestoreFacts(context.Background(), remediation.ExactGitRestoreQuery{
		Repository: "acme/app", BaseBranch: "main", TargetPath: "deploy/other.yaml", BaselineRevision: baselineSHA,
	})
	if !errors.Is(err, change.ErrNotAllowed) || calls.Load() != before {
		t.Fatalf("out-of-scope query err=%v calls=%d", err, calls.Load())
	}
}

func testGitObjectHash(kind string, content []byte) string {
	payload := append([]byte(fmt.Sprintf("%s %d\x00", kind, len(content))), content...)
	sum := sha1.Sum(payload) // #nosec G401 -- test fixture computes Git SHA-1 object identities.
	return hex.EncodeToString(sum[:])
}

func testRawTree(mode, name, objectID string) []byte {
	raw, _ := hex.DecodeString(objectID)
	result := append([]byte(mode+" "+name+"\x00"), raw...)
	return result
}
