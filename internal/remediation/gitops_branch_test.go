package remediation

import (
	"strings"
	"testing"
)

func TestGitOpsBranchMatchesExternalRequiredCheck(t *testing.T) {
	incidentID := "11111111-1111-4111-8111-111111111111"
	planHash := strings.Repeat("a", 64)
	want := "cloudops/incident-" + incidentID + "/plan-" + planHash
	if got := GitOpsBranch(incidentID, planHash); got != want {
		t.Fatalf("branch=%q want=%q", got, want)
	}
}
