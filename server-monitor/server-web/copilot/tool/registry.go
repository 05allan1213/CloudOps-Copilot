package tool

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	copilot "server-web/copilot/service"
)

const defaultRegistryTimeout = 30 * time.Second

type Registry interface {
	Register(tool Tool) error
	Get(name string) (Tool, error)
	List() []ToolSchema
	Validate(name string, args json.RawMessage) error
	Execute(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
	HealthCheck(ctx context.Context) map[string]bool
}

type healthCheckedTool interface {
	HealthCheck(ctx context.Context) bool
}

type MemoryRegistry struct {
	mu      sync.RWMutex
	tools   map[string]Tool
	logArgs bool
}

type RegistryOption func(*MemoryRegistry)

func WithLogArgs(enabled bool) RegistryOption {
	return func(registry *MemoryRegistry) {
		registry.logArgs = enabled
	}
}

func NewRegistry(options ...RegistryOption) *MemoryRegistry {
	registry := &MemoryRegistry{
		tools: map[string]Tool{},
	}
	for _, option := range options {
		option(registry)
	}
	return registry
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

func (r *MemoryRegistry) Validate(name string, args json.RawMessage) error {
	tool, ok := r.get(name)
	if !ok {
		return NewToolNotFoundError(name)
	}
	return ValidateArgs(tool.Schema(), args)
}

func (r *MemoryRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
	tool, err := r.Get(name)
	if err != nil {
		return ToolResult{Success: false, Error: errorResult(err)}, err
	}

	schema := tool.Schema()
	normalizedArgs, err := NormalizeArgs(schema, args)
	if err != nil {
		return ToolResult{Success: false, Error: errorResult(err)}, err
	}
	if err := authorizeTool(ctx, schema); err != nil {
		result := ToolResult{Success: false, Error: errorResult(err)}
		r.logToolCall(ctx, schema, normalizedArgs, result, 0)
		return result, err
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, toolTimeout(schema))
	defer cancel()
	ctx, span := otel.Tracer("server-web/copilot/tool").Start(ctx, "copilot.tool."+normalizeToolName(name))
	span.SetAttributes(
		attribute.String("tool.name", schema.Name),
		attribute.String("tool.risk_level", string(schema.RiskLevel)),
		attribute.Bool("tool.read_only", schema.ReadOnly),
	)

	result, err := tool.Run(ctx, normalizedArgs)
	duration := time.Since(start)
	if result.Duration == 0 {
		result.Duration = duration
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && err == nil && result.Error == nil {
		err = ErrToolTimeout
		result.Error = NewToolError(ErrorCodeToolTimeout, "", "tool execution timed out", ErrToolTimeout)
	}
	if err != nil {
		result.Success = false
		if result.Error == nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
				err = ErrToolTimeout
				result.Error = NewToolError(ErrorCodeToolTimeout, "", "tool execution timed out", ErrToolTimeout)
			} else {
				result.Error = errorResult(err)
			}
		}
		result = sanitizeToolResult(result)
		span.RecordError(err)
		span.SetStatus(codes.Error, result.Error.Code.String())
		span.SetAttributes(attribute.Bool("tool.success", false), attribute.Int64("tool.duration_ms", duration.Milliseconds()))
		r.logToolCall(ctx, schema, normalizedArgs, result, duration)
		span.End()
		return result, err
	}
	result.Success = result.Error == nil
	result = sanitizeToolResult(result)
	if result.Success {
		span.SetStatus(codes.Ok, "")
	} else if result.Error != nil {
		span.SetStatus(codes.Error, result.Error.Code.String())
	}
	span.SetAttributes(attribute.Bool("tool.success", result.Success), attribute.Int64("tool.duration_ms", duration.Milliseconds()))
	r.logToolCall(ctx, schema, normalizedArgs, result, duration)
	span.End()
	return result, nil
}

func (r *MemoryRegistry) HealthCheck(ctx context.Context) map[string]bool {
	r.mu.RLock()
	names := make([]string, 0, len(r.tools))
	tools := make(map[string]Tool, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
		tools[name] = r.tools[name]
	}
	r.mu.RUnlock()

	sort.Strings(names)

	health := make(map[string]bool, len(names))
	for _, name := range names {
		checker, ok := tools[name].(healthCheckedTool)
		if !ok {
			health[name] = true
			continue
		}
		health[name] = checker.HealthCheck(ctx)
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
	return publicToolError(err)
}

func publicToolError(err error) *ToolError {
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr
	}
	switch {
	case errors.Is(err, ErrToolNotFound):
		return NewToolError(ErrorCodeToolNotFound, "", "tool not found", err)
	case errors.Is(err, ErrInvalidArgs):
		return NewToolError(ErrorCodeInvalidArgs, "", "invalid tool arguments", err)
	case errors.Is(err, ErrPermissionDenied):
		return NewToolError(ErrorCodePermissionDenied, "", "tool permission denied", err)
	case errors.Is(err, ErrToolTimeout), errors.Is(err, context.DeadlineExceeded):
		return NewToolError(ErrorCodeToolTimeout, "", "tool execution timed out", err)
	case errors.Is(err, ErrToolUnavailable):
		return NewToolError(ErrorCodeToolUnavailable, "", "tool unavailable", err)
	default:
		return NewToolError(ErrorCodeToolExecution, "", "tool execution failed", err)
	}
}

func authorizeTool(ctx context.Context, schema ToolSchema) error {
	user, ok := copilot.UserFromContext(ctx)
	role := strings.ToLower(strings.TrimSpace(user.Role))
	if !ok || role == "" {
		return NewToolError(ErrorCodePermissionDenied, "", "user context is required to execute tools", ErrPermissionDenied)
	}
	if !schema.ReadOnly {
		return NewToolError(ErrorCodePermissionDenied, "", "write tools are disabled in phase 2", ErrPermissionDenied)
	}
	if role == "viewer" || role == "admin" {
		return nil
	}
	return NewToolError(ErrorCodePermissionDenied, "", "role is not allowed to execute tools", ErrPermissionDenied)
}

func toolTimeout(schema ToolSchema) time.Duration {
	if schema.Timeout > 0 {
		return schema.Timeout
	}
	return defaultRegistryTimeout
}

func sanitizeToolResult(result ToolResult) ToolResult {
	result.Data = sanitizeResult(result.Data)
	if result.Metadata != nil {
		if sanitized, ok := redactSensitive(result.Metadata).(map[string]interface{}); ok {
			result.Metadata = sanitized
		}
	}
	return result
}

func (r *MemoryRegistry) logToolCall(ctx context.Context, schema ToolSchema, args json.RawMessage, result ToolResult, duration time.Duration) {
	errorType := ""
	if result.Error != nil {
		errorType = string(result.Error.Code)
	}
	fields := []zap.Field{
		zap.String("tool_name", schema.Name),
		zap.String("risk_level", string(schema.RiskLevel)),
		zap.Bool("read_only", schema.ReadOnly),
		zap.String("args_hash", hashArgs(args)),
		zap.Int64("duration_ms", duration.Milliseconds()),
		zap.Bool("success", result.Success),
		zap.String("error_type", errorType),
		zap.String("trace_id", traceIDFromContext(ctx)),
	}
	if r.logArgs {
		fields = append(fields, zap.Any("args", sanitizedLogArgs(args)))
	}
	zap.L().Info("copilot tool call", fields...)
}

func hashArgs(args json.RawMessage) string {
	sum := sha256.Sum256(args)
	return fmt.Sprintf("%x", sum[:])
}

func sanitizedLogArgs(args json.RawMessage) interface{} {
	var decoded interface{}
	if err := json.Unmarshal(args, &decoded); err != nil {
		return nil
	}
	return redactSensitive(decoded)
}

func traceIDFromContext(ctx context.Context) string {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.HasTraceID() {
		return ""
	}
	return spanContext.TraceID().String()
}
