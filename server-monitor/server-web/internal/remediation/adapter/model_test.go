package adapter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"server-web/internal/agent/llm"
)

type fakeChat struct{ output string }

func (f fakeChat) Chat(context.Context, []llm.ChatMessage) (string, *llm.ChatUsage, error) {
	return f.output, nil, nil
}

func TestModelPlannerCannotExpandAuthority(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	valid := `{"operation_type":"set_replicas","target_resource":{"api_version":"apps/v1","kind":"Deployment","namespace":"prod","name":"api"},"proposed_value":{"replicas":3},"evidence_ids":["` + id + `"]}`
	planner, _ := NewModelPlanner(fakeChat{output: valid})
	if _, err := planner.Plan(context.Background(), json.RawMessage(`{"id":"incident"}`), json.RawMessage(`{"summary":"bounded"}`), []string{id}); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{strings.TrimSuffix(valid, "}") + `,"repository":"evil/repo"}`, strings.Replace(valid, id, "22222222-2222-4222-8222-222222222222", 1)} {
		planner, _ = NewModelPlanner(fakeChat{output: output})
		if _, err := planner.Plan(context.Background(), json.RawMessage(`{"id":"incident"}`), json.RawMessage(`{"summary":"bounded"}`), []string{id}); err == nil {
			t.Fatalf("expanded planner authority accepted: %s", output)
		}
	}
}
