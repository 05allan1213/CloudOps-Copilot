package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/05allan1213/CloudOps-Copilot/internal/operation"
)

type OperationPort interface {
	EnqueueActionCard(context.Context, string, operation.ExecuteRequest) (operation.Execution, error)
	EnqueueOperationPlan(context.Context, string, operation.ExecuteRequest) (operation.Execution, error)
	Execution(context.Context, string) (operation.Execution, error)
	Executions(context.Context, int) ([]operation.Execution, error)
	Workspace(context.Context, int) (operation.DevOpsWorkspace, error)
}

type operationPage struct {
	Items []operation.Execution `json:"items"`
}

func (h *Handler) executeActionCard(c *gin.Context) {
	h.executeAgentSubject(c, false)
}

func (h *Handler) executeOperationPlan(c *gin.Context) {
	h.executeAgentSubject(c, true)
}

func (h *Handler) executeAgentSubject(c *gin.Context, highImpact bool) {
	if !h.requireOperations(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	var request operation.ExecuteRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	var value operation.Execution
	var err error
	if highImpact {
		value, err = h.operations.EnqueueOperationPlan(c.Request.Context(), id, request)
	} else {
		value, err = h.operations.EnqueueActionCard(c.Request.Context(), id, request)
	}
	if err != nil {
		h.writeOperationError(c, err)
		return
	}
	h.writeJSON(c, http.StatusAccepted, value)
}

func (h *Handler) getOperation(c *gin.Context) {
	if !h.requireOperations(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	value, err := h.operations.Execution(c.Request.Context(), id)
	if err != nil {
		h.writeOperationError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) listOperations(c *gin.Context) {
	limit, ok := h.agentLimit(c)
	if !ok || !h.requireOperations(c) {
		return
	}
	items, err := h.operations.Executions(c.Request.Context(), limit)
	if err != nil {
		h.writeOperationError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, operationPage{Items: items})
}

func (h *Handler) getDevOpsWorkspace(c *gin.Context) {
	limit, ok := h.agentLimit(c)
	if !ok || !h.requireOperations(c) {
		return
	}
	value, err := h.operations.Workspace(c.Request.Context(), limit)
	if err != nil {
		h.writeOperationError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) requireOperations(c *gin.Context) bool {
	if h.operations != nil {
		return true
	}
	h.writeProblem(c, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Controlled Operations capability is not wired")
	return false
}

func (h *Handler) writeOperationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, operation.ErrInvalidArgument):
		h.writeProblem(c, http.StatusBadRequest, "OPERATION_INVALID_ARGUMENT", "Operation request is invalid")
	case errors.Is(err, operation.ErrNotFound):
		h.writeProblem(c, http.StatusNotFound, "OPERATION_NOT_FOUND", "Operation resource was not found")
	case errors.Is(err, operation.ErrUnauthorized):
		h.writeProblem(c, http.StatusConflict, "ACTION_AUTHORIZATION_REQUIRED", "An exact current Action Authorization is required")
	case errors.Is(err, operation.ErrExpired):
		h.writeProblem(c, http.StatusConflict, "ACTION_AUTHORIZATION_EXPIRED", "Action Authorization or proposal has expired")
	case errors.Is(err, operation.ErrRevisionChanged):
		h.writeProblem(c, http.StatusConflict, "CONFIGURATION_REVISION_CHANGED", "Operation Plan does not match the active Configuration Revision")
	case errors.Is(err, operation.ErrConflict):
		h.writeProblem(c, http.StatusConflict, "OPERATION_STATE_CONFLICT", "Operation state changed")
	case errors.Is(err, operation.ErrProviderUnavailable):
		h.writeProblem(c, http.StatusServiceUnavailable, "OPERATION_PROVIDER_UNAVAILABLE", "Operation provider is unavailable")
	default:
		h.writeProblem(c, http.StatusInternalServerError, "OPERATION_INTERNAL_ERROR", "Controlled Operation failed")
	}
}
