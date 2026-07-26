package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
	"github.com/gin-gonic/gin"
)

type MonitoringPort interface {
	Catalog(context.Context, observability.CatalogRequest) (observability.Catalog, error)
	StartOwner(context.Context, observability.StartQueryRequest) (observability.Execution, error)
	Execution(context.Context, string) (observability.Execution, error)
	Executions(context.Context, observability.HistoryFilter) ([]observability.Execution, error)
	Cancel(context.Context, string) (observability.Execution, error)
	SaveDefinition(context.Context, observability.SaveDefinitionRequest) (observability.Definition, error)
	Definitions(context.Context, int) ([]observability.Definition, error)
	CreateAuthorization(context.Context, observability.CreateAuthorizationRequest) (observability.Authorization, error)
	Authorizations(context.Context, int) ([]observability.Authorization, error)
	RevokeAuthorization(context.Context, string) error
}

type monitoringExecutionPage struct {
	Items []observability.Execution `json:"items"`
}

type queryDefinitionPage struct {
	Items []observability.Definition `json:"items"`
}

type queryAuthorizationPage struct {
	Items []observability.Authorization `json:"items"`
}

func (h *Handler) getMonitoringCatalog(c *gin.Context) {
	if !h.requireMonitoring(c) {
		return
	}
	request, ok := monitoringContext(c)
	if !ok {
		h.writeProblem(c, http.StatusUnprocessableEntity, "INVALID_MONITORING_QUERY", "cluster, Namespace, and Workload identity are required")
		return
	}
	value, err := h.monitoring.Catalog(c.Request.Context(), request)
	if err != nil {
		h.writeMonitoringError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) startMonitoringQuery(c *gin.Context) {
	if !h.requireMonitoring(c) {
		return
	}
	var request observability.StartQueryRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	value, err := h.monitoring.StartOwner(c.Request.Context(), request)
	if err != nil {
		h.writeMonitoringError(c, err)
		return
	}
	h.writeJSON(c, http.StatusAccepted, value)
}

func (h *Handler) getMonitoringQuery(c *gin.Context) {
	if !h.requireMonitoring(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	value, err := h.monitoring.Execution(c.Request.Context(), id)
	if err != nil {
		h.writeMonitoringError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) listMonitoringQueries(c *gin.Context) {
	if !h.requireMonitoring(c) {
		return
	}
	limit, ok := monitoringLimit(c)
	if !ok {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_QUERY", "limit must be between 1 and 100")
		return
	}
	items, err := h.monitoring.Executions(c.Request.Context(), observability.HistoryFilter{
		ClusterID: strings.TrimSpace(c.Query("cluster_id")), Namespace: strings.TrimSpace(c.Query("namespace")),
		ResourceID: strings.TrimSpace(c.Query("resource_id")), Limit: limit,
	})
	if err != nil {
		h.writeMonitoringError(c, err)
		return
	}
	if items == nil {
		items = []observability.Execution{}
	}
	h.writeJSON(c, http.StatusOK, monitoringExecutionPage{Items: items})
}

func (h *Handler) cancelMonitoringQuery(c *gin.Context) {
	if !h.requireMonitoring(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	value, err := h.monitoring.Cancel(c.Request.Context(), id)
	if err != nil {
		h.writeMonitoringError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) saveQueryDefinition(c *gin.Context) {
	if !h.requireMonitoring(c) {
		return
	}
	var request observability.SaveDefinitionRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	value, err := h.monitoring.SaveDefinition(c.Request.Context(), request)
	if err != nil {
		h.writeMonitoringError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, value)
}

func (h *Handler) listQueryDefinitions(c *gin.Context) {
	if !h.requireMonitoring(c) {
		return
	}
	limit, ok := monitoringLimit(c)
	if !ok {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_QUERY", "limit must be between 1 and 100")
		return
	}
	items, err := h.monitoring.Definitions(c.Request.Context(), limit)
	if err != nil {
		h.writeMonitoringError(c, err)
		return
	}
	if items == nil {
		items = []observability.Definition{}
	}
	h.writeJSON(c, http.StatusOK, queryDefinitionPage{Items: items})
}

func (h *Handler) createQueryAuthorization(c *gin.Context) {
	if !h.requireMonitoring(c) {
		return
	}
	var request observability.CreateAuthorizationRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	value, err := h.monitoring.CreateAuthorization(c.Request.Context(), request)
	if err != nil {
		h.writeMonitoringError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, value)
}

func (h *Handler) listQueryAuthorizations(c *gin.Context) {
	if !h.requireMonitoring(c) {
		return
	}
	limit, ok := monitoringLimit(c)
	if !ok {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_QUERY", "limit must be between 1 and 100")
		return
	}
	items, err := h.monitoring.Authorizations(c.Request.Context(), limit)
	if err != nil {
		h.writeMonitoringError(c, err)
		return
	}
	if items == nil {
		items = []observability.Authorization{}
	}
	h.writeJSON(c, http.StatusOK, queryAuthorizationPage{Items: items})
}

func (h *Handler) revokeQueryAuthorization(c *gin.Context) {
	if !h.requireMonitoring(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	if err := h.monitoring.RevokeAuthorization(c.Request.Context(), id); err != nil {
		h.writeMonitoringError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func monitoringContext(c *gin.Context) (observability.CatalogRequest, bool) {
	request := observability.CatalogRequest{
		ClusterID: strings.TrimSpace(c.Query("cluster_id")), Namespace: strings.TrimSpace(c.Query("namespace")),
		Resource: observability.ResourceReference{
			ID: strings.TrimSpace(c.Query("resource_id")), Kind: strings.TrimSpace(c.Query("resource_kind")),
			Namespace: strings.TrimSpace(c.Query("namespace")), Name: strings.TrimSpace(c.Query("resource_name")),
		},
	}
	return request, request.ClusterID != "" && request.Namespace != "" && request.Resource.ID != "" && request.Resource.Kind != "" && request.Resource.Name != ""
}

func monitoringLimit(c *gin.Context) (int, bool) {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		return 50, true
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil && value >= 1 && value <= 100
}

func (h *Handler) requireMonitoring(c *gin.Context) bool {
	if h.monitoring != nil {
		return true
	}
	h.writeProblem(c, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Monitoring capability is not wired")
	return false
}

func (h *Handler) writeMonitoringError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, observability.ErrInvalid), errors.Is(err, observability.ErrBoundExceeded):
		h.writeProblem(c, http.StatusUnprocessableEntity, "INVALID_MONITORING_QUERY", "query is invalid or exceeds the active bounded contract")
	case errors.Is(err, observability.ErrUnauthorized), errors.Is(err, observability.ErrAuthorizationRevoked), errors.Is(err, observability.ErrAuthorizationUsed):
		h.writeProblem(c, http.StatusForbidden, "QUERY_NOT_AUTHORIZED", "Agent query does not have a usable exact authorization")
	case errors.Is(err, observability.ErrNotFound):
		h.writeProblem(c, http.StatusNotFound, "MONITORING_RESOURCE_NOT_FOUND", "Monitoring query resource was not found")
	case errors.Is(err, observability.ErrConflict):
		h.writeProblem(c, http.StatusConflict, "QUERY_STATE_CONFLICT", "Monitoring query state does not allow this operation")
	case errors.Is(err, observability.ErrProviderDisabled):
		h.writeProblem(c, http.StatusServiceUnavailable, "PROMETHEUS_PROVIDER_DISABLED", "Prometheus is disabled in the active Configuration Revision")
	default:
		h.writeProblem(c, http.StatusServiceUnavailable, "PROMETHEUS_PROVIDER_UNAVAILABLE", "Prometheus Provider or Monitoring persistence is unavailable")
	}
}

var _ MonitoringPort = (*observability.Service)(nil)
