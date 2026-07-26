package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

type TelemetryPort interface {
	Catalog(context.Context, string, telemetry.CatalogRequest) (telemetry.Catalog, error)
	QueryLogs(context.Context, telemetry.StartLogQueryRequest) (telemetry.LogQuery, error)
	LogQuery(context.Context, string) (telemetry.LogQuery, error)
	LogQueries(context.Context, string, string, string, int) ([]telemetry.LogQuery, error)
	SearchTraces(context.Context, telemetry.StartTraceSearchRequest) (telemetry.TraceSearch, error)
	TraceSearch(context.Context, string) (telemetry.TraceSearch, error)
	TraceSearches(context.Context, string, string, string, int) ([]telemetry.TraceSearch, error)
	Trace(context.Context, telemetry.TraceDetailRequest) (telemetry.TraceDetail, error)
	SaveLogEvidence(context.Context, string, telemetry.SaveEvidenceRequest) (telemetry.Evidence, error)
	SaveTraceEvidence(context.Context, string, string, telemetry.SaveEvidenceRequest) (telemetry.Evidence, error)
	CreateConsultation(context.Context, telemetry.CreateConsultationRequest) (telemetry.Consultation, error)
}

type logQueryPage struct {
	Items []telemetry.LogQuery `json:"items"`
}

type traceSearchPage struct {
	Items []telemetry.TraceSearch `json:"items"`
}

func (h *Handler) getLogCatalog(c *gin.Context)   { h.getTelemetryCatalog(c, "elasticsearch") }
func (h *Handler) getTraceCatalog(c *gin.Context) { h.getTelemetryCatalog(c, "tempo") }

func (h *Handler) getTelemetryCatalog(c *gin.Context, provider string) {
	if !h.requireTelemetry(c) {
		return
	}
	request, ok := telemetryContext(c)
	if !ok {
		h.writeProblem(c, http.StatusUnprocessableEntity, "INVALID_TELEMETRY_CONTEXT", "cluster, Namespace, and Workload identity are required")
		return
	}
	value, err := h.telemetry.Catalog(c.Request.Context(), provider, request)
	if err != nil {
		h.writeTelemetryError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) startLogQuery(c *gin.Context) {
	if !h.requireTelemetry(c) {
		return
	}
	var request telemetry.StartLogQueryRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	value, err := h.telemetry.QueryLogs(c.Request.Context(), request)
	if err != nil {
		h.writeTelemetryError(c, err)
		return
	}
	h.writeJSON(c, http.StatusAccepted, value)
}

func (h *Handler) getLogQuery(c *gin.Context) {
	if !h.requireTelemetry(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	value, err := h.telemetry.LogQuery(c.Request.Context(), id)
	if err != nil {
		h.writeTelemetryError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) listLogQueries(c *gin.Context) {
	if !h.requireTelemetry(c) {
		return
	}
	clusterID, namespace, resourceID, limit, ok := telemetryHistory(c)
	if !ok {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_QUERY", "limit must be between 1 and 100")
		return
	}
	items, err := h.telemetry.LogQueries(c.Request.Context(), clusterID, namespace, resourceID, limit)
	if err != nil {
		h.writeTelemetryError(c, err)
		return
	}
	if items == nil {
		items = []telemetry.LogQuery{}
	}
	h.writeJSON(c, http.StatusOK, logQueryPage{Items: items})
}

func (h *Handler) saveLogEvidence(c *gin.Context) {
	if !h.requireTelemetry(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	var request telemetry.SaveEvidenceRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	value, err := h.telemetry.SaveLogEvidence(c.Request.Context(), id, request)
	if err != nil {
		h.writeTelemetryError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, value)
}

func (h *Handler) startTraceSearch(c *gin.Context) {
	if !h.requireTelemetry(c) {
		return
	}
	var request telemetry.StartTraceSearchRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	value, err := h.telemetry.SearchTraces(c.Request.Context(), request)
	if err != nil {
		h.writeTelemetryError(c, err)
		return
	}
	h.writeJSON(c, http.StatusAccepted, value)
}

func (h *Handler) getTraceSearch(c *gin.Context) {
	if !h.requireTelemetry(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	value, err := h.telemetry.TraceSearch(c.Request.Context(), id)
	if err != nil {
		h.writeTelemetryError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) listTraceSearches(c *gin.Context) {
	if !h.requireTelemetry(c) {
		return
	}
	clusterID, namespace, resourceID, limit, ok := telemetryHistory(c)
	if !ok {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_QUERY", "limit must be between 1 and 100")
		return
	}
	items, err := h.telemetry.TraceSearches(c.Request.Context(), clusterID, namespace, resourceID, limit)
	if err != nil {
		h.writeTelemetryError(c, err)
		return
	}
	if items == nil {
		items = []telemetry.TraceSearch{}
	}
	h.writeJSON(c, http.StatusOK, traceSearchPage{Items: items})
}

func (h *Handler) getTraceDetail(c *gin.Context) {
	if !h.requireTelemetry(c) {
		return
	}
	request := telemetry.TraceDetailRequest{TraceID: strings.TrimSpace(c.Param("trace_id")), SearchID: strings.TrimSpace(c.Query("search_id"))}
	if request.SearchID == "" {
		contextRequest, ok := telemetryContext(c)
		if !ok {
			h.writeProblem(c, http.StatusUnprocessableEntity, "INVALID_TELEMETRY_CONTEXT", "a trace search or full resource context is required")
			return
		}
		request.ClusterID, request.Namespace, request.Resource = contextRequest.ClusterID, contextRequest.Namespace, contextRequest.Resource
		var err error
		request.From, request.To, err = telemetryRange(c)
		if err != nil {
			h.writeProblem(c, http.StatusUnprocessableEntity, "INVALID_TELEMETRY_CONTEXT", "an absolute trace time range is required")
			return
		}
	}
	value, err := h.telemetry.Trace(c.Request.Context(), request)
	if err != nil {
		h.writeTelemetryError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) saveTraceEvidence(c *gin.Context) {
	if !h.requireTelemetry(c) {
		return
	}
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	var request telemetry.SaveEvidenceRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	value, err := h.telemetry.SaveTraceEvidence(c.Request.Context(), id, strings.TrimSpace(c.Param("trace_id")), request)
	if err != nil {
		h.writeTelemetryError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, value)
}

func (h *Handler) createTelemetryConsultation(c *gin.Context) {
	if !h.requireTelemetry(c) {
		return
	}
	var request telemetry.CreateConsultationRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	value, err := h.telemetry.CreateConsultation(c.Request.Context(), request)
	if err != nil {
		h.writeTelemetryError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, value)
}

func telemetryContext(c *gin.Context) (telemetry.CatalogRequest, bool) {
	request := telemetry.CatalogRequest{
		ClusterID: strings.TrimSpace(c.Query("cluster_id")), Namespace: strings.TrimSpace(c.Query("namespace")),
		Resource: telemetry.ResourceReference{
			ID: strings.TrimSpace(c.Query("resource_id")), Kind: strings.TrimSpace(c.Query("resource_kind")),
			Namespace: strings.TrimSpace(c.Query("namespace")), Name: strings.TrimSpace(c.Query("resource_name")),
		},
	}
	return request, request.ClusterID != "" && request.Namespace != "" && request.Resource.ID != "" && request.Resource.Kind != "" && request.Resource.Name != ""
}

func telemetryHistory(c *gin.Context) (string, string, string, int, bool) {
	limit := 30
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return "", "", "", 0, false
		}
		limit = value
	}
	return strings.TrimSpace(c.Query("cluster_id")), strings.TrimSpace(c.Query("namespace")), strings.TrimSpace(c.Query("resource_id")), limit, true
}

func telemetryRange(c *gin.Context) (time.Time, time.Time, error) {
	from, fromErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(c.Query("from")))
	to, toErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(c.Query("to")))
	if fromErr != nil || toErr != nil || !to.After(from) {
		return time.Time{}, time.Time{}, telemetry.ErrInvalid
	}
	return from.UTC(), to.UTC(), nil
}

func (h *Handler) requireTelemetry(c *gin.Context) bool {
	if h.telemetry != nil {
		return true
	}
	h.writeProblem(c, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Logs and Traces capability is not wired")
	return false
}

func (h *Handler) writeTelemetryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, telemetry.ErrInvalid), errors.Is(err, telemetry.ErrBoundExceeded):
		h.writeProblem(c, http.StatusUnprocessableEntity, "INVALID_TELEMETRY_QUERY", "query is invalid or exceeds the active bounded contract")
	case errors.Is(err, telemetry.ErrNotFound):
		h.writeProblem(c, http.StatusNotFound, "TELEMETRY_RESOURCE_NOT_FOUND", "Logs or Traces resource was not found")
	case errors.Is(err, telemetry.ErrConflict):
		h.writeProblem(c, http.StatusConflict, "TELEMETRY_STATE_CONFLICT", "telemetry query state does not allow this operation")
	case errors.Is(err, telemetry.ErrResultExpired):
		h.writeProblem(c, http.StatusGone, "TELEMETRY_RESULT_EXPIRED", "ephemeral Provider rows expired; rerun the query before retaining Evidence")
	case errors.Is(err, telemetry.ErrProviderDisabled):
		h.writeProblem(c, http.StatusServiceUnavailable, "TELEMETRY_PROVIDER_DISABLED", "the requested Provider is disabled in the active Configuration Revision")
	default:
		h.writeProblem(c, http.StatusServiceUnavailable, "TELEMETRY_PROVIDER_UNAVAILABLE", "Elasticsearch, Tempo, or telemetry persistence is unavailable")
	}
}

var _ TelemetryPort = (*telemetry.Service)(nil)
