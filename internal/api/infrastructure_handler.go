package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"github.com/gin-gonic/gin"
)

type InfrastructurePort interface {
	Topology(context.Context, infrastructure.Query) (infrastructure.TopologySnapshot, error)
	Resources(context.Context, infrastructure.Query) (infrastructure.ResourcePage, error)
	Resource(context.Context, string, infrastructure.Query) (infrastructure.ResourceDetail, error)
	ResourceEvents(context.Context, string, infrastructure.Query) (infrastructure.EventPage, error)
	Probe(context.Context) (string, error)
}

type unavailableInfrastructurePort struct{}

func (unavailableInfrastructurePort) Topology(context.Context, infrastructure.Query) (infrastructure.TopologySnapshot, error) {
	now := time.Now().UTC()
	return infrastructure.TopologySnapshot{
		ProviderState: infrastructure.ProviderUnavailable, ProviderDetail: "Kubernetes Provider Gateway 未装配",
		Source:    infrastructure.ProviderSource{Provider: "kubernetes", Identity: "kubernetes://unavailable", CollectedAt: now},
		Freshness: infrastructure.Freshness{State: "unavailable", FreshUntil: now},
		Nodes:     []infrastructure.Resource{}, Edges: []infrastructure.TopologyEdge{}, Issues: []infrastructure.ProviderIssue{}, Partial: true, CollectedAt: now,
	}, nil
}
func (p unavailableInfrastructurePort) Resources(ctx context.Context, query infrastructure.Query) (infrastructure.ResourcePage, error) {
	topology, _ := p.Topology(ctx, query)
	return infrastructure.ResourcePage{Scope: topology.Scope, ProviderState: topology.ProviderState, Source: topology.Source, Freshness: topology.Freshness, Items: []infrastructure.Resource{}, Partial: true, CollectedAt: topology.CollectedAt}, nil
}
func (unavailableInfrastructurePort) Resource(context.Context, string, infrastructure.Query) (infrastructure.ResourceDetail, error) {
	return infrastructure.ResourceDetail{}, infrastructure.ErrUnavailable
}
func (unavailableInfrastructurePort) ResourceEvents(context.Context, string, infrastructure.Query) (infrastructure.EventPage, error) {
	return infrastructure.EventPage{}, infrastructure.ErrUnavailable
}
func (unavailableInfrastructurePort) Probe(context.Context) (string, error) {
	return "", infrastructure.ErrUnavailable
}

func (h *Handler) getTopology(c *gin.Context) {
	query, ok := h.infrastructureQuery(c)
	if !ok {
		return
	}
	value, err := h.infrastructure.Topology(c.Request.Context(), query)
	if err != nil {
		h.writeInfrastructureError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) listInfrastructureResources(c *gin.Context) {
	query, ok := h.infrastructureQuery(c)
	if !ok {
		return
	}
	value, err := h.infrastructure.Resources(c.Request.Context(), query)
	if err != nil {
		h.writeInfrastructureError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) getInfrastructureResource(c *gin.Context) {
	id, ok := h.infrastructureResourceID(c)
	if !ok {
		return
	}
	query, ok := h.infrastructureQuery(c)
	if !ok {
		return
	}
	value, err := h.infrastructure.Resource(c.Request.Context(), id, query)
	if err != nil {
		h.writeInfrastructureError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) listInfrastructureEvents(c *gin.Context) {
	id, ok := h.infrastructureResourceID(c)
	if !ok {
		return
	}
	query, ok := h.infrastructureQuery(c)
	if !ok {
		return
	}
	value, err := h.infrastructure.ResourceEvents(c.Request.Context(), id, query)
	if err != nil {
		h.writeInfrastructureError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) streamTopologyEvents(c *gin.Context) {
	lastEventID := strings.TrimSpace(c.GetHeader("Last-Event-ID"))
	if len(lastEventID) > 128 || containsControl(lastEventID) {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_EVENT_CURSOR", "Last-Event-ID is invalid")
		return
	}
	query, ok := h.infrastructureQuery(c)
	if !ok {
		return
	}
	value, err := h.infrastructure.Topology(c.Request.Context(), query)
	if err != nil {
		h.writeInfrastructureError(c, err)
		return
	}
	cursor := value.ContentHash
	if cursor == "" {
		cursor = strconv.FormatInt(value.CollectedAt.UnixNano(), 10)
	}
	c.Header("Content-Type", SSEMediaType)
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString("retry: 10000\n\n")
	if cursor != lastEventID {
		payload, _ := json.Marshal(struct {
			SnapshotID    string                       `json:"snapshot_id,omitempty"`
			ContentHash   string                       `json:"content_hash,omitempty"`
			ProviderState infrastructure.ProviderState `json:"provider_state"`
			CollectedAt   time.Time                    `json:"collected_at"`
		}{value.ID, value.ContentHash, value.ProviderState, value.CollectedAt})
		_, _ = fmt.Fprintf(c.Writer, "id: %s\nevent: topology.refresh\ndata: %s\n\n", cursor, payload)
	}
	c.Writer.Flush()
}

func (h *Handler) infrastructureResourceID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" || len(id) > 512 || containsControl(id) {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_RESOURCE_ID", "resource id is invalid")
		return "", false
	}
	return id, true
}

func (h *Handler) infrastructureQuery(c *gin.Context) (infrastructure.Query, bool) {
	query := infrastructure.Query{
		ClusterID: strings.TrimSpace(c.Query("cluster")), Namespace: strings.TrimSpace(c.Query("namespace")),
		Search: strings.TrimSpace(c.Query("search")), Cursor: strings.TrimSpace(c.Query("cursor")),
	}
	for _, raw := range c.QueryArray("kind") {
		for _, kind := range strings.Split(raw, ",") {
			if kind = strings.TrimSpace(kind); kind != "" {
				query.Kinds = append(query.Kinds, kind)
			}
		}
	}
	if len(query.Kinds) > 16 || len(query.Cursor) > 2048 || containsControl(query.Cursor) {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_INFRASTRUCTURE_QUERY", "infrastructure filters are invalid")
		return infrastructure.Query{}, false
	}
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > infrastructure.MaximumLimit {
			h.writeProblem(c, http.StatusBadRequest, "INVALID_INFRASTRUCTURE_QUERY", "limit must be between 1 and 500")
			return infrastructure.Query{}, false
		}
		query.Limit = limit
	}
	for name, target := range map[string]*time.Time{"from": &query.From, "to": &query.To} {
		value := strings.TrimSpace(c.Query(name))
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			h.writeProblem(c, http.StatusBadRequest, "INVALID_INFRASTRUCTURE_QUERY", name+" must be RFC3339 UTC time")
			return infrastructure.Query{}, false
		}
		*target = parsed.UTC()
	}
	return query, true
}

func (h *Handler) writeInfrastructureError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, infrastructure.ErrInvalid):
		h.writeProblem(c, http.StatusUnprocessableEntity, "INVALID_INFRASTRUCTURE_QUERY", "Operational Scope or resource query is invalid")
	case errors.Is(err, infrastructure.ErrNotFound):
		h.writeProblem(c, http.StatusNotFound, "KUBERNETES_RESOURCE_NOT_FOUND", "resource is not present in the current topology projection")
	case errors.Is(err, infrastructure.ErrUnavailable):
		h.writeProblem(c, http.StatusServiceUnavailable, "KUBERNETES_PROVIDER_UNAVAILABLE", "Kubernetes Provider is unavailable for this resource query")
	default:
		h.writeProblem(c, http.StatusServiceUnavailable, "INFRASTRUCTURE_UNAVAILABLE", "Infrastructure projection is unavailable")
	}
}

var _ InfrastructurePort = (*infrastructure.Service)(nil)
