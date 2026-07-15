package graph

import (
	"context"
	"reflect"
	"testing"

	"server-web/internal/agent"
)

func TestEinoGraphRoutesCyclesAndStops(t *testing.T) {
	runtime := &recordingRuntime{}
	executor, err := New(context.Background(), runtime, 16)
	if err != nil {
		t.Fatal(err)
	}
	state := &agent.GraphState{NextNode: agent.NodeLoadIncident}
	result, err := executor.Invoke(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	want := []agent.Node{agent.NodeLoadIncident, agent.NodeBuildObjective}
	if !reflect.DeepEqual(runtime.nodes, want) || result.NextNode != agent.NodeEnd {
		t.Fatalf("nodes=%v next=%s", runtime.nodes, result.NextNode)
	}
}

type recordingRuntime struct{ nodes []agent.Node }

func (r *recordingRuntime) ExecuteNode(_ context.Context, node agent.Node, state *agent.GraphState) (*agent.GraphState, error) {
	r.nodes = append(r.nodes, node)
	if node == agent.NodeLoadIncident {
		state.NextNode = agent.NodeBuildObjective
	} else {
		state.NextNode = agent.NodeEnd
	}
	return state, nil
}
