package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

type AgentWorkspacePort interface {
	WorkspaceRuns(context.Context, int) ([]agent.WorkspaceRun, error)
	WorkspaceRun(context.Context, string) (agent.WorkspaceRun, error)
	RequestCancel(context.Context, string) (agent.WorkspaceRun, error)
	Consultations(context.Context, int) ([]agent.ConsultationSummary, error)
	Consultation(context.Context, string) (agent.ConsultationDetail, error)
	CreateConsultationTurn(context.Context, string, agent.SendMessageRequest) (agent.ConsultationMessage, agent.WorkspaceRun, error)
	RequestConsultationCancel(context.Context, string) (agent.WorkspaceRun, error)
	StreamEvents(context.Context, string, string, int) ([]agent.StreamEvent, error)
	KnowledgeItems(context.Context, int, bool) ([]agent.KnowledgeItem, error)
	KnowledgeItem(context.Context, string) (agent.KnowledgeItem, error)
	CreateKnowledge(context.Context, agent.SaveKnowledgeRequest) (agent.KnowledgeItem, error)
	UpdateKnowledge(context.Context, string, agent.UpdateKnowledgeRequest) (agent.KnowledgeItem, error)
	DeleteKnowledge(context.Context, string) error
	RunbookGuidance(context.Context) ([]agent.RunbookGuidance, error)
	ProposeActionCard(context.Context, agent.ActionProposalRequest) (agent.ActionCard, error)
	AuthorizeActionCard(context.Context, string, agent.AuthorizeActionRequest) (agent.ActionCard, error)
	ActionCard(context.Context, string) (agent.ActionCard, error)
	ProposeOperationPlan(context.Context, agent.ActionProposalRequest) (agent.OperationPlan, error)
	AuthorizeOperationPlan(context.Context, string, agent.AuthorizeActionRequest) (agent.OperationPlan, error)
	OperationPlan(context.Context, string) (agent.OperationPlan, error)
	OperationPlans(context.Context, int) ([]agent.OperationPlan, error)
}

type agentRunPage struct {
	Items []agent.WorkspaceRun `json:"items"`
}

type consultationPage struct {
	Items []agent.ConsultationSummary `json:"items"`
}

type knowledgePage struct {
	Items []agent.KnowledgeItem `json:"items"`
}

type runbookPage struct {
	Items []agent.RunbookGuidance `json:"items"`
}

type operationPlanPage struct {
	Items []agent.OperationPlan `json:"items"`
}

func (h *Handler) listAgentInvestigations(c *gin.Context) {
	limit, ok := h.agentLimit(c)
	if !ok || !h.requireAgent(c) {
		return
	}
	items, err := h.agentWorkspace.WorkspaceRuns(c.Request.Context(), limit)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	result := make([]agent.WorkspaceRun, 0, len(items))
	for _, item := range items {
		if item.SubjectType != agent.WorkspaceSubjectConsultation {
			result = append(result, item)
		}
	}
	h.writeJSON(c, http.StatusOK, agentRunPage{Items: result})
}

func (h *Handler) getAgentInvestigation(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	value, err := h.agentWorkspace.WorkspaceRun(c.Request.Context(), id)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	if value.SubjectType == agent.WorkspaceSubjectConsultation {
		h.writeProblem(c, http.StatusNotFound, "AGENT_RUN_NOT_FOUND", "Investigation was not found")
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) cancelAgentInvestigation(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	value, err := h.agentWorkspace.RequestCancel(c.Request.Context(), id)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusAccepted, value)
}

func (h *Handler) listAgentConsultations(c *gin.Context) {
	limit, ok := h.agentLimit(c)
	if !ok || !h.requireAgent(c) {
		return
	}
	items, err := h.agentWorkspace.Consultations(c.Request.Context(), limit)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, consultationPage{Items: items})
}

func (h *Handler) getAgentConsultation(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	value, err := h.agentWorkspace.Consultation(c.Request.Context(), id)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) attachAgentSnapshot(c *gin.Context) {
	if !h.requireAgent(c) || !h.requireTelemetry(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	var request telemetry.AttachContextSnapshotRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	value, err := h.telemetry.AttachContextSnapshot(c.Request.Context(), id, request)
	if err != nil {
		h.writeTelemetryError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, value)
}

func (h *Handler) sendAgentMessage(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if !h.decodeCommand(c, &body) {
		return
	}
	body.Content = strings.TrimSpace(body.Content)
	if body.Content == "" || utf8.RuneCountInString(body.Content) > 16000 {
		h.writeProblem(c, http.StatusUnprocessableEntity, "INVALID_AGENT_REQUEST", "Consultation message must contain 1..16000 characters")
		return
	}
	key, ok := h.agentIdempotency(c, id, "message")
	if !ok {
		return
	}
	message, run, err := h.agentWorkspace.CreateConsultationTurn(c.Request.Context(), id, agent.SendMessageRequest{
		Content: body.Content, IdempotencyKey: key,
	})
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusAccepted, gin.H{"message": message, "run": run})
}

func (h *Handler) cancelAgentConsultation(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	value, err := h.agentWorkspace.RequestConsultationCancel(c.Request.Context(), id)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusAccepted, value)
}

func (h *Handler) streamAgentEvents(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	lastEventID := strings.TrimSpace(c.GetHeader("Last-Event-ID"))
	if len(lastEventID) > maxCursorBytes || containsControl(lastEventID) {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_EVENT_CURSOR", "Last-Event-ID is invalid")
		return
	}
	items, err := h.agentWorkspace.StreamEvents(c.Request.Context(), id, lastEventID, 100)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	c.Header("Content-Type", SSEMediaType)
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString("retry: 1500\n\n")
	lastEventID = writeAgentEvents(c, items, lastEventID)
	c.Writer.Flush()
	poll, heartbeat, deadline := time.NewTicker(500*time.Millisecond), time.NewTicker(10*time.Second), time.NewTimer(25*time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()
	defer deadline.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-deadline.C:
			return
		case <-heartbeat.C:
			_, _ = c.Writer.WriteString(": keep-alive\n\n")
			c.Writer.Flush()
		case <-poll.C:
			items, err = h.agentWorkspace.StreamEvents(c.Request.Context(), id, lastEventID, 100)
			if err != nil {
				return
			}
			lastEventID = writeAgentEvents(c, items, lastEventID)
			if len(items) > 0 {
				c.Writer.Flush()
			}
		}
	}
}

func writeAgentEvents(c *gin.Context, items []agent.StreamEvent, last string) string {
	for _, item := range items {
		payload, err := json.Marshal(item)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(c.Writer, "id: %s\nevent: %s\ndata: %s\n\n", item.ID, item.Type, payload)
		last = item.ID
	}
	return last
}

func (h *Handler) listKnowledgeItems(c *gin.Context) {
	limit, ok := h.agentLimit(c)
	if !ok || !h.requireAgent(c) {
		return
	}
	items, err := h.agentWorkspace.KnowledgeItems(c.Request.Context(), limit, c.Query("include_deleted") == "true")
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, knowledgePage{Items: items})
}

func (h *Handler) getKnowledgeItem(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	value, err := h.agentWorkspace.KnowledgeItem(c.Request.Context(), id)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) createKnowledgeItem(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	var request agent.SaveKnowledgeRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	value, err := h.agentWorkspace.CreateKnowledge(c.Request.Context(), request)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, value)
}

func (h *Handler) updateKnowledgeItem(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	var request agent.UpdateKnowledgeRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	value, err := h.agentWorkspace.UpdateKnowledge(c.Request.Context(), id, request)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) deleteKnowledgeItem(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	if err := h.agentWorkspace.DeleteKnowledge(c.Request.Context(), id); err != nil {
		h.writeAgentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) listRunbookGuidance(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	items, err := h.agentWorkspace.RunbookGuidance(c.Request.Context())
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, runbookPage{Items: items})
}

func (h *Handler) proposeActionCard(c *gin.Context) {
	h.proposeAgentAction(c, false)
}

func (h *Handler) proposeOperationPlan(c *gin.Context) {
	h.proposeAgentAction(c, true)
}

func (h *Handler) proposeAgentAction(c *gin.Context, highImpact bool) {
	if !h.requireAgent(c) {
		return
	}
	var request agent.ActionProposalRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	if highImpact {
		value, err := h.agentWorkspace.ProposeOperationPlan(c.Request.Context(), request)
		if err != nil {
			h.writeAgentError(c, err)
			return
		}
		h.writeJSON(c, http.StatusCreated, value)
		return
	}
	value, err := h.agentWorkspace.ProposeActionCard(c.Request.Context(), request)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, value)
}

func (h *Handler) authorizeActionCard(c *gin.Context) {
	h.authorizeAgentAction(c, false)
}

func (h *Handler) authorizeOperationPlan(c *gin.Context) {
	h.authorizeAgentAction(c, true)
}

func (h *Handler) authorizeAgentAction(c *gin.Context, highImpact bool) {
	if !h.requireAgent(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	var request agent.AuthorizeActionRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	if highImpact {
		value, err := h.agentWorkspace.AuthorizeOperationPlan(c.Request.Context(), id, request)
		if err != nil {
			h.writeAgentError(c, err)
			return
		}
		h.writeJSON(c, http.StatusCreated, value)
		return
	}
	value, err := h.agentWorkspace.AuthorizeActionCard(c.Request.Context(), id, request)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, value)
}

func (h *Handler) getActionCard(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	value, err := h.agentWorkspace.ActionCard(c.Request.Context(), id)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) getOperationPlan(c *gin.Context) {
	if !h.requireAgent(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	value, err := h.agentWorkspace.OperationPlan(c.Request.Context(), id)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) listOperationPlans(c *gin.Context) {
	limit, ok := h.agentLimit(c)
	if !ok || !h.requireAgent(c) {
		return
	}
	items, err := h.agentWorkspace.OperationPlans(c.Request.Context(), limit)
	if err != nil {
		h.writeAgentError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, operationPlanPage{Items: items})
}

func (h *Handler) requireAgent(c *gin.Context) bool {
	if h.agentWorkspace != nil {
		return true
	}
	h.writeProblem(c, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Agent Workspace capability is not wired")
	return false
}

func (h *Handler) agentLimit(c *gin.Context) (int, bool) {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		return 50, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 100 {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_QUERY", "limit must be between 1 and 100")
		return 0, false
	}
	return limit, true
}

func (h *Handler) agentIdempotency(c *gin.Context, resourceID, operation string) (string, bool) {
	raw := strings.TrimSpace(c.GetHeader(IdempotencyHeader))
	if raw == "" || len(raw) > 128 || containsControl(raw) {
		h.writeProblem(c, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key must contain 1..128 characters")
		return "", false
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{"agent", resourceID, operation, raw}, "\x00")))
	return hex.EncodeToString(digest[:]), true
}

func (h *Handler) writeAgentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, agent.ErrInvalidArgument):
		h.writeProblem(c, http.StatusUnprocessableEntity, "INVALID_AGENT_REQUEST", "Agent request violates the bounded context or authority contract")
	case errors.Is(err, agent.ErrNotFound):
		h.writeProblem(c, http.StatusNotFound, "AGENT_RESOURCE_NOT_FOUND", "Agent resource was not found")
	case errors.Is(err, agent.ErrConflict), errors.Is(err, agent.ErrCancelled):
		h.writeProblem(c, http.StatusConflict, "AGENT_STATE_CONFLICT", "Agent resource state does not allow this operation")
	case errors.Is(err, agent.ErrPermission):
		h.writeProblem(c, http.StatusForbidden, "AGENT_AUTHORITY_REQUIRED", "Exact Owner authorization is required")
	case errors.Is(err, agent.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		h.writeProblem(c, http.StatusServiceUnavailable, "AGENT_RUNTIME_UNAVAILABLE", "Agent durable runtime or bounded Provider dependency is unavailable")
	default:
		h.writeProblem(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Agent operation failed")
	}
}
