package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"server-web/internal/agent"
	agenttool "server-web/internal/agent/tool"
)

var phase2ReadOnlyTools = []string{
	agenttool.ToolAlertListActive,
	agenttool.ToolAlertHistory,
	agenttool.ToolRunbookSearch,
	agenttool.ToolK8sGetPods,
	agenttool.ToolK8sGetDeployments,
	agenttool.ToolK8sGetServices,
	agenttool.ToolK8sGetEvents,
	agenttool.ToolK8sGetLogs,
}

var phase3ReadOnlyTools = agenttool.Phase3ToolNames()

type ReadOnlyTools struct {
	executor *agenttool.Executor
	allowed  []string
}

func NewReadOnlyTools(executor *agenttool.Executor) (*ReadOnlyTools, error) {
	if executor == nil {
		return nil, agent.ErrInvalidArgument
	}
	registered := map[string]bool{}
	for _, schema := range executor.ToolSchemas() {
		if schema.ReadOnly {
			registered[schema.Name] = true
		}
	}
	allowed := make([]string, 0, len(phase2ReadOnlyTools))
	for _, name := range phase2ReadOnlyTools {
		if registered[name] {
			allowed = append(allowed, name)
		}
	}
	for _, name := range phase3ReadOnlyTools {
		if registered[name] {
			allowed = append(allowed, name)
		}
	}
	sort.Strings(allowed)
	if len(allowed) == 0 {
		return nil, agent.ErrUnavailable
	}
	return &ReadOnlyTools{executor: executor, allowed: allowed}, nil
}

func (a *ReadOnlyTools) AllowedTools() []string { return slices.Clone(a.allowed) }

func (a *ReadOnlyTools) Execute(ctx context.Context, name string, args json.RawMessage, timeout time.Duration, maxBytes int) (agent.ToolResult, error) {
	if !slices.Contains(a.allowed, name) {
		return agent.ToolResult{}, agent.NewRuntimeError(agent.ErrorPermission, "tool is not in the fixed read-only allowlist", agent.ErrInvalidArgument)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx = agenttool.WithActor(ctx, agenttool.Actor{Name: "incident-agent-runtime", Role: "admin"})
	result, err := a.executor.ExecuteTool(ctx, name, args)
	if err != nil {
		code := agent.ErrorTemporary
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, agenttool.ErrToolTimeout) {
			code = agent.ErrorTimeout
		}
		return agent.ToolResult{}, agent.NewRuntimeError(code, "read-only tool execution failed", err)
	}
	if !result.Success || result.Error != nil {
		message := "read-only tool returned an unsuccessful result"
		if result.Error != nil {
			message = result.Error.Error()
		}
		return agent.ToolResult{}, agent.NewRuntimeError(agent.ErrorValidation, message, agent.ErrInvalidArgument)
	}
	data, err := json.Marshal(result.Data)
	if err != nil {
		return agent.ToolResult{}, agent.NewRuntimeError(agent.ErrorInvariant, "tool result is not serializable", err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	truncated := false
	if len(data) > maxBytes {
		truncated = true
		data, _ = json.Marshal(map[string]any{"truncated": true, "result_hash": hash, "original_bytes": len(data)})
	}
	return agent.ToolResult{Summary: fmt.Sprintf("%s returned %d bounded bytes", name, len(data)), Facts: data, ResultHash: hash, Redaction: json.RawMessage(`{"policy":"agent_tool_sanitizer"}`), Truncated: truncated, Valid: true}, nil
}
