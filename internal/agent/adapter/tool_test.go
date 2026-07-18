package adapter

import (
	"slices"
	"testing"

	agenttool "github.com/05allan1213/CloudOps-Copilot/internal/agent/tool"
)

func TestReadOnlyAdapterAdvertisesOnlyFrozenAllowlist(t *testing.T) {
	executor, err := agenttool.NewExecutor(agenttool.Options{})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewReadOnlyTools(executor)
	if err != nil {
		t.Fatal(err)
	}
	allowed := adapter.AllowedTools()
	for _, prohibited := range []string{agenttool.ToolHostList, agenttool.ToolPromQueryRange, agenttool.ToolK8sGetNodes, "k8s.delete_pod", "action.execute"} {
		if slices.Contains(allowed, prohibited) {
			t.Fatalf("prohibited tool advertised: %s", prohibited)
		}
	}
	readOnly := map[string]bool{}
	for _, schema := range executor.ToolSchemas() {
		readOnly[schema.Name] = schema.ReadOnly
	}
	for _, name := range allowed {
		if !slices.Contains(phase2ReadOnlyTools, name) || !readOnly[name] {
			t.Fatalf("unsafe tool advertised: %s", name)
		}
	}
}
