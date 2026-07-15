package adapter

import (
	"slices"
	"testing"

	copilottool "server-web/internal/copilot/tool"
)

func TestReadOnlyAdapterAdvertisesOnlyFrozenAllowlist(t *testing.T) {
	executor, err := copilottool.NewExecutor(copilottool.Options{})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewReadOnlyTools(executor)
	if err != nil {
		t.Fatal(err)
	}
	allowed := adapter.AllowedTools()
	for _, prohibited := range []string{copilottool.ToolHostList, copilottool.ToolK8sGetNodes, "k8s.delete_pod", "action.execute"} {
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
