package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"server-web/internal/agent"
)

type AgentApplication interface {
	CreateRun(context.Context, string, string) (*agent.Run, error)
	GetRun(context.Context, string) (*agent.Run, error)
	ListRuns(context.Context, string, int, int) (agent.Page, error)
	ListSteps(context.Context, string, int) ([]agent.Step, error)
	ListEvidence(context.Context, string, int) ([]agent.EvidenceRecord, error)
	Cancel(context.Context, string) error
}

func (h *Handler) SetAgentRuntime(runtime AgentApplication) { h.agentRuntime = runtime }

type agentRunDTO struct {
	ID                string          `json:"id"`
	IncidentID        string          `json:"incident_id"`
	Attempt           int             `json:"attempt"`
	Status            agent.RunStatus `json:"status"`
	Model             string          `json:"model"`
	PromptVersion     string          `json:"prompt_version"`
	MaxSteps          int             `json:"max_steps"`
	UsedSteps         int             `json:"used_steps"`
	MaxToolCalls      int             `json:"max_tool_calls"`
	UsedToolCalls     int             `json:"used_tool_calls"`
	MaxModelCalls     int             `json:"max_model_calls"`
	UsedModelCalls    int             `json:"used_model_calls"`
	TokenBudget       int64           `json:"token_budget"`
	UsedTokens        int64           `json:"used_tokens"`
	MaxEvidenceItems  int             `json:"max_evidence_items"`
	UsedEvidenceItems int             `json:"used_evidence_items"`
	FailureCode       agent.ErrorCode `json:"failure_code,omitempty"`
	FailureSummary    string          `json:"failure_summary,omitempty"`
	FinalDiagnosis    json.RawMessage `json:"final_diagnosis,omitempty"`
	StartedAt         *time.Time      `json:"started_at,omitempty"`
	FinishedAt        *time.Time      `json:"finished_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type agentStepDTO struct {
	ID            string           `json:"id"`
	Sequence      int              `json:"sequence"`
	Node          agent.Node       `json:"node"`
	Status        agent.StepStatus `json:"status"`
	ShortReason   string           `json:"short_reason"`
	SelectedTool  string           `json:"selected_tool,omitempty"`
	ArgumentsHash string           `json:"arguments_hash,omitempty"`
	ResultSummary string           `json:"result_summary,omitempty"`
	EvidenceID    string           `json:"evidence_id,omitempty"`
	RetryCount    int              `json:"retry_count"`
	DurationMS    int64            `json:"duration_ms"`
	InputTokens   int64            `json:"input_tokens"`
	OutputTokens  int64            `json:"output_tokens"`
	ErrorCode     agent.ErrorCode  `json:"error_code,omitempty"`
	StartedAt     *time.Time       `json:"started_at,omitempty"`
	FinishedAt    *time.Time       `json:"finished_at,omitempty"`
}

type agentEvidenceDTO struct {
	ID            string          `json:"id"`
	ToolName      string          `json:"tool_name"`
	ResourceScope string          `json:"resource_scope"`
	Summary       string          `json:"summary"`
	Facts         json.RawMessage `json:"facts"`
	ResultHash    string          `json:"result_hash"`
	RawRef        string          `json:"raw_ref,omitempty"`
	Truncated     bool            `json:"truncated"`
	Valid         bool            `json:"valid"`
	CollectedAt   time.Time       `json:"collected_at"`
}

func (h *Handler) CreateAgentRun(c *gin.Context) {
	if !h.requireAgentRuntime(c) {
		return
	}
	if !requireAgentPublicID(c) {
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "Idempotency-Key header is required and must be at most 128 bytes"})
		return
	}
	run, err := h.agentRuntime.CreateRun(c.Request.Context(), c.Param("id"), key)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, response{Status: "accepted", Data: toAgentRunDTO(run)})
}

func (h *Handler) ListAgentRuns(c *gin.Context) {
	if !h.requireAgentRuntime(c) {
		return
	}
	if !requireAgentPublicID(c) {
		return
	}
	page, pageSize, ok := parsePage(c)
	if !ok {
		return
	}
	result, err := h.agentRuntime.ListRuns(c.Request.Context(), c.Param("id"), page, pageSize)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	items := make([]agentRunDTO, 0, len(result.Items))
	for index := range result.Items {
		items = append(items, toAgentRunDTO(&result.Items[index]))
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"items": items, "total": result.Total, "page": result.Page, "page_size": result.PageSize}})
}

func (h *Handler) GetAgentRun(c *gin.Context) {
	if !h.requireAgentRuntime(c) {
		return
	}
	if !requireAgentPublicID(c) {
		return
	}
	run, err := h.agentRuntime.GetRun(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: toAgentRunDTO(run)})
}

func (h *Handler) ListAgentSteps(c *gin.Context) {
	if !h.requireAgentRuntime(c) {
		return
	}
	if !requireAgentPublicID(c) {
		return
	}
	items, err := h.agentRuntime.ListSteps(c.Request.Context(), c.Param("id"), 100)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	dtos := make([]agentStepDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, agentStepDTO{ID: item.PublicID, Sequence: item.Sequence, Node: item.Node, Status: item.Status, ShortReason: item.ShortReason, SelectedTool: item.SelectedTool, ArgumentsHash: item.ArgumentsHash, ResultSummary: item.ResultSummary, EvidenceID: item.EvidencePublicID, RetryCount: item.RetryCount, DurationMS: item.DurationMS, InputTokens: item.InputTokens, OutputTokens: item.OutputTokens, ErrorCode: item.ErrorCode, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt})
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: dtos})
}

func (h *Handler) ListAgentEvidence(c *gin.Context) {
	if !h.requireAgentRuntime(c) {
		return
	}
	if !requireAgentPublicID(c) {
		return
	}
	items, err := h.agentRuntime.ListEvidence(c.Request.Context(), c.Param("id"), 100)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	dtos := make([]agentEvidenceDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, agentEvidenceDTO{ID: item.PublicID, ToolName: item.ToolName, ResourceScope: item.ResourceScope, Summary: item.Summary, Facts: item.Facts, ResultHash: item.ResultHash, RawRef: item.RawRef, Truncated: item.Truncated, Valid: item.Valid, CollectedAt: item.CollectedAt})
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: dtos})
}

func (h *Handler) CancelAgentRun(c *gin.Context) {
	if !h.requireAgentRuntime(c) {
		return
	}
	if !requireAgentPublicID(c) {
		return
	}
	if err := h.agentRuntime.Cancel(c.Request.Context(), c.Param("id")); err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, response{Status: "accepted"})
}

func (h *Handler) requireAgentRuntime(c *gin.Context) bool {
	if h.agentRuntime != nil {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "incident agent runtime is unavailable"})
	return false
}

func parsePage(c *gin.Context) (int, int, bool) {
	page, pageSize := 1, 20
	var err error
	if raw := c.Query("page"); raw != "" {
		page, err = strconv.Atoi(raw)
	}
	if err == nil {
		if raw := c.Query("page_size"); raw != "" {
			pageSize, err = strconv.Atoi(raw)
		}
	}
	if err != nil || page < 1 || pageSize < 1 || pageSize > 100 {
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: "invalid page or page_size"})
		return 0, 0, false
	}
	return page, pageSize, true
}

func requireAgentPublicID(c *gin.Context) bool {
	if _, err := uuid.Parse(c.Param("id")); err == nil {
		return true
	}
	c.JSON(http.StatusBadRequest, response{Status: "error", Error: "id must be a valid public UUID"})
	return false
}

func toAgentRunDTO(run *agent.Run) agentRunDTO {
	return agentRunDTO{ID: run.PublicID, IncidentID: run.IncidentPublicID, Attempt: run.Attempt, Status: run.Status, Model: run.Model, PromptVersion: run.PromptVersion, MaxSteps: run.Limits.MaxSteps, UsedSteps: run.Usage.Steps, MaxToolCalls: run.Limits.MaxToolCalls, UsedToolCalls: run.Usage.ToolCalls, MaxModelCalls: run.Limits.MaxModelCalls, UsedModelCalls: run.Usage.ModelCalls, TokenBudget: run.Limits.TokenBudget, UsedTokens: run.Usage.TotalTokens(), MaxEvidenceItems: run.Limits.MaxEvidenceItems, UsedEvidenceItems: run.Usage.Evidence, FailureCode: run.FailureCode, FailureSummary: run.FailureSummary, FinalDiagnosis: run.FinalDiagnosis, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt}
}

func writeAgentError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "incident agent request failed"
	switch {
	case errors.Is(err, agent.ErrInvalidArgument):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, agent.ErrNotFound):
		status, message = http.StatusNotFound, "agent resource not found"
	case errors.Is(err, agent.ErrConflict), errors.Is(err, agent.ErrLeaseLost):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, agent.ErrUnavailable):
		status, message = http.StatusServiceUnavailable, "incident agent runtime is unavailable"
	}
	c.JSON(status, response{Status: "error", Error: message})
}
