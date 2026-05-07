package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Registry interface {
	Register(tool Tool) error
	Get(name string) (Tool, error)
	List() []ToolSchema
	Validate(name string, args json.RawMessage) error
	Execute(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
	HealthCheck(ctx context.Context) map[string]bool
}

type MemoryRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *MemoryRegistry {
	return &MemoryRegistry{
		tools: map[string]Tool{},
	}
}

func (r *MemoryRegistry) Register(tool Tool) error {
	if tool == nil {
		return fmt.Errorf("%w: tool is nil", ErrInvalidArgs)
	}
	name := normalizeToolName(tool.Name())
	if name == "" {
		return fmt.Errorf("%w: tool name is required", ErrInvalidArgs)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("%w: %s", ErrToolAlreadyRegistered, name)
	}
	r.tools[name] = tool
	return nil
}

func (r *MemoryRegistry) Get(name string) (Tool, error) {
	tool, ok := r.get(name)
	if !ok {
		return nil, NewToolNotFoundError(name)
	}
	return tool, nil
}

func (r *MemoryRegistry) List() []ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	schemas := make([]ToolSchema, 0, len(names))
	for _, name := range names {
		schemas = append(schemas, r.tools[name].Schema())
	}
	return schemas
}

func (r *MemoryRegistry) Validate(name string, _ json.RawMessage) error {
	if _, ok := r.get(name); !ok {
		return NewToolNotFoundError(name)
	}
	return nil
}

func (r *MemoryRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
	tool, err := r.Get(name)
	if err != nil {
		return ToolResult{Success: false, Error: errorResult(err)}, err
	}
	if err := r.Validate(name, args); err != nil {
		return ToolResult{Success: false, Error: errorResult(err)}, err
	}

	start := time.Now()
	result, err := tool.Run(ctx, args)
	duration := time.Since(start)
	if result.Duration == 0 {
		result.Duration = duration
	}
	if err != nil {
		result.Success = false
		if result.Error == nil {
			result.Error = errorResult(err)
		}
		return result, err
	}
	result.Success = true
	return result, nil
}

func (r *MemoryRegistry) HealthCheck(context.Context) map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	health := make(map[string]bool, len(names))
	for _, name := range names {
		health[name] = true
	}
	return health
}

func (r *MemoryRegistry) get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.tools[normalizeToolName(name)]
	return tool, ok
}

func normalizeToolName(name string) string {
	return strings.TrimSpace(name)
}

func errorResult(err error) *ToolError {
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr
	}
	return NewToolError(ErrorCodeToolExecution, "", err.Error(), err)
}
