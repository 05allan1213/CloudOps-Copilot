package incidentstore

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestV3ActiveRuntimeDoesNotUseLegacyCurrentRunPointer(t *testing.T) {
	t.Parallel()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve ownership contract source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	forbidden := "current_" + "agent_run_id"
	for _, relative := range []string{
		"internal/businessbudget/budget.go",
		"internal/infra/incidentstore/no_change.go",
		"internal/infra/incidentstore/store.go",
		"internal/taskhandler/change_ensure_pr.go",
		"internal/taskhandler/investigation_start.go",
	} {
		source, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("%s depends on the legacy circular Incident pointer", relative)
		}
	}

	budgetSource, err := os.ReadFile(filepath.Join(root, "internal/businessbudget/budget.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"active_incident_cycle_key = UNHEX(CONCAT('01'",
		"AND incident_id = ? AND cycle_no = ?",
		"status IN ('pending','running')",
	} {
		if !strings.Contains(string(budgetSource), required) {
			t.Fatalf("active AgentRun lookup is missing indexed cycle/status guard %q", required)
		}
	}
}
