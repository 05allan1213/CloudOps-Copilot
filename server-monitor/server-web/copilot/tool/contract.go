package tool

import (
	"context"
	"encoding/json"
	"time"
)

type RiskLevel string

const (
	RiskLevelLow    RiskLevel = "low"
	RiskLevelMedium RiskLevel = "medium"
	RiskLevelHigh   RiskLevel = "high"
)

type ParamType string

const (
	ParamTypeString  ParamType = "string"
	ParamTypeNumber  ParamType = "number"
	ParamTypeInteger ParamType = "integer"
	ParamTypeBoolean ParamType = "boolean"
	ParamTypeArray   ParamType = "array"
	ParamTypeObject  ParamType = "object"
)

type Tool interface {
	Name() string
	Description() string
	Schema() ToolSchema
	Run(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

type Observer interface {
	ObserveToolExecution(name, result string, seconds float64)
}

type ToolSchema struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Parameters  []ParamSchema `json:"parameters"`
	RiskLevel   RiskLevel     `json:"risk_level"`
	ReadOnly    bool          `json:"read_only"`
	Timeout     time.Duration `json:"timeout"`
}

type ParamSchema struct {
	Name        string      `json:"name"`
	Type        ParamType   `json:"type"`
	Required    bool        `json:"required"`
	Description string      `json:"description,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Min         *float64    `json:"min,omitempty"`
	Max         *float64    `json:"max,omitempty"`
	Pattern     string      `json:"pattern,omitempty"`
}

type ToolResult struct {
	Success  bool                   `json:"success"`
	Data     interface{}            `json:"data,omitempty"`
	Error    *ToolError             `json:"error,omitempty"`
	Duration time.Duration          `json:"duration"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}
