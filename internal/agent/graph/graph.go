// Package graph composes the bounded incident loop with CloudWeGo Eino.
package graph

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/compose"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

const routerNode = "route"

// Runtime owns the durable semantics of every graph node.
type Runtime interface {
	ExecuteNode(context.Context, agent.Node, *agent.GraphState) (*agent.GraphState, error)
}

// Executor is a compiled Eino graph. Project-owned checkpoints remain the source of truth.
type Executor struct {
	runnable compose.Runnable[*agent.GraphState, *agent.GraphState]
}

func New(ctx context.Context, runtime Runtime, maxGraphSteps int) (*Executor, error) {
	if runtime == nil || maxGraphSteps < 1 {
		return nil, fmt.Errorf("%w: graph runtime and max steps are required", agent.ErrInvalidArgument)
	}
	g := compose.NewGraph[*agent.GraphState, *agent.GraphState]()
	if err := g.AddLambdaNode(routerNode, compose.InvokableLambda(func(_ context.Context, state *agent.GraphState) (*agent.GraphState, error) {
		if state == nil {
			return nil, agent.ErrInvalidArgument
		}
		return state, nil
	})); err != nil {
		return nil, err
	}
	nodes := []agent.Node{
		agent.NodeLoadIncident, agent.NodeBuildObjective, agent.NodePlanInvestigation, agent.NodeSelectAction,
		agent.NodeExecuteTool, agent.NodePersistObservation, agent.NodeEvaluateCoverage, agent.NodeReplan,
		agent.NodeProduceDiagnosis, agent.NodeValidateDiagnosis, agent.NodeCompleteRun, agent.NodeRetryableFailure,
		agent.NodeTerminalFailure, agent.NodeBudgetExceeded, agent.NodeCancelled,
	}
	ends := make(map[string]bool, len(nodes)+1)
	for _, node := range nodes {
		node := node
		key := string(node)
		ends[key] = true
		if err := g.AddLambdaNode(key, compose.InvokableLambda(func(ctx context.Context, state *agent.GraphState) (*agent.GraphState, error) {
			return runtime.ExecuteNode(ctx, node, state)
		})); err != nil {
			return nil, err
		}
		if err := g.AddEdge(key, routerNode); err != nil {
			return nil, err
		}
	}
	ends[compose.END] = true
	if err := g.AddEdge(compose.START, routerNode); err != nil {
		return nil, err
	}
	if err := g.AddBranch(routerNode, compose.NewGraphBranch(func(_ context.Context, state *agent.GraphState) (string, error) {
		if state.NextNode == agent.NodeEnd {
			return compose.END, nil
		}
		if !ends[string(state.NextNode)] {
			return "", fmt.Errorf("%w: unknown next node %q", agent.ErrInvalidArgument, state.NextNode)
		}
		return string(state.NextNode), nil
	}, ends)); err != nil {
		return nil, err
	}
	runnable, err := g.Compile(ctx, compose.WithGraphName("incident_agent_runtime"), compose.WithMaxRunSteps(maxGraphSteps))
	if err != nil {
		return nil, err
	}
	return &Executor{runnable: runnable}, nil
}

func (e *Executor) Invoke(ctx context.Context, state *agent.GraphState) (*agent.GraphState, error) {
	if e == nil || e.runnable == nil || state == nil {
		return nil, agent.ErrInvalidArgument
	}
	return e.runnable.Invoke(ctx, state)
}
