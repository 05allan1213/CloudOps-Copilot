package service

import (
	"context"
	"time"

	"server-web/internal/copilot/session"
)

type SummaryInput struct {
	UserMessage string
	ToolCalls   []ToolCall
	Intent      string
}

type SummaryResult struct {
	Reply       string
	Suggestions []string
}

type Summarizer interface {
	Summarize(ctx context.Context, input SummaryInput) (SummaryResult, error)
}

type ToolCall struct {
	Name   string      `json:"name"`
	Status string      `json:"status"`
	Error  string      `json:"error,omitempty"`
	Result interface{} `json:"result,omitempty"`
}

type IntentResult struct {
	Intent     string     `json:"intent"`
	Confidence float64    `json:"confidence"`
	ToolCalls  []ToolCall `json:"tool_calls"`
	Reply      string     `json:"reply"`
	Error      string     `json:"error,omitempty"`
}

type ToolSchema struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  []ToolParamSchema `json:"parameters"`
	RiskLevel   string            `json:"risk_level"`
	ReadOnly    bool              `json:"read_only"`
	Timeout     time.Duration     `json:"timeout"`
}

type ToolParamSchema struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Required    bool        `json:"required"`
	Description string      `json:"description,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Min         *float64    `json:"min,omitempty"`
	Max         *float64    `json:"max,omitempty"`
	Pattern     string      `json:"pattern,omitempty"`
}

type SessionSummary = session.Summary
type Message = session.Message
