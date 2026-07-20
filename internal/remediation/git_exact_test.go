package remediation

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestExpectedGitTreeHashReplacesOnlyBoundedBlob(t *testing.T) {
	current := []byte("kind: Deployment\nmetadata:\n  name: demo\n")
	baseline := []byte("kind: Deployment\nmetadata:\n  name: demo\n# baseline\n")
	post := []byte("kind: Deployment\nmetadata:\n  name: demo\n# restored\n")
	readme := []byte("demo repository\n")
	currentBlob := mustGitObjectHash(t, "blob", current)
	baselineBlob := mustGitObjectHash(t, "blob", baseline)
	readmeBlob := mustGitObjectHash(t, "blob", readme)
	appsTree := mustGitObjectHash(t, "tree", rawTreeEntry(t, "100644", "demo.yaml", currentBlob))
	rootContent := append(rawTreeEntry(t, "100644", "README.md", readmeBlob), rawTreeEntry(t, "40000", "apps", appsTree)...)
	rootTree := mustGitObjectHash(t, "tree", rootContent)

	facts := ExactGitRestoreFacts{
		Repository: "acme/gitops", BaseBranch: "main", TargetPath: "apps/demo.yaml",
		BaseRevision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BaseTreeSHA: rootTree,
		BaseBlobSHA: currentBlob, FileMode: "100644", CurrentContent: current,
		BaselineRevision: "9999999999999999999999999999999999999999",
		BaselineBlobSHA:  baselineBlob, BaselineContent: baseline, BaselineIsAncestor: true,
		TreeEntries: []GitTreeEntry{
			{Path: "README.md", Mode: "100644", Type: "blob", ObjectID: readmeBlob},
			{Path: "apps", Mode: "040000", Type: "tree", ObjectID: appsTree},
			{Path: "apps/demo.yaml", Mode: "100644", Type: "blob", ObjectID: currentBlob},
		},
	}
	if err := ValidateExactGitRestoreFacts(facts); err != nil {
		t.Fatal(err)
	}
	postBlob := mustGitObjectHash(t, "blob", post)
	postAppsTree := mustGitObjectHash(t, "tree", rawTreeEntry(t, "100644", "demo.yaml", postBlob))
	wantRoot := mustGitObjectHash(t, "tree", append(rawTreeEntry(t, "100644", "README.md", readmeBlob), rawTreeEntry(t, "40000", "apps", postAppsTree)...))
	got, err := ExpectedGitTreeHash(facts, post)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantRoot {
		t.Fatalf("tree=%s want=%s", got, wantRoot)
	}

	tampered := facts
	tampered.TreeEntries = append([]GitTreeEntry(nil), facts.TreeEntries...)
	tampered.TreeEntries[1].ObjectID = currentBlob
	if err := ValidateExactGitRestoreFacts(tampered); err == nil {
		t.Fatal("tampered nested tree was accepted")
	}
}

func mustGitObjectHash(t *testing.T, kind string, content []byte) string {
	t.Helper()
	value, err := gitObjectHash(kind, content, 40)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func rawTreeEntry(t *testing.T, mode, name, objectID string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(objectID)
	if err != nil {
		t.Fatal(err)
	}
	var result bytes.Buffer
	result.WriteString(mode + " " + name)
	result.WriteByte(0)
	result.Write(raw)
	return result.Bytes()
}
