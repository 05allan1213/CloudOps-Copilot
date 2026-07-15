package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"server-monitor/pkg/logger"
	domain "server-web/internal/incident"
	"server-web/internal/infra/webhook"
	"server-web/internal/middleware"
	appincident "server-web/internal/service/incident"
)

// IncidentApplication is the narrow transport-facing Incident use-case contract.
type IncidentApplication interface {
	IngestAlertmanager(context.Context, webhook.AlertmanagerWebhookRequest) ([]appincident.IngestResult, error)
	GetIncident(context.Context, string) (*domain.Incident, error)
	ListIncidents(context.Context, domain.ListFilter) (domain.Page, error)
	ListSignals(context.Context, string) ([]domain.Signal, error)
	ListTimeline(context.Context, string) ([]domain.TimelineEvent, error)
	ListEvidence(context.Context, string) ([]domain.EvidenceItem, error)
}

type incidentDTO struct {
	ID             string          `json:"id"`
	Fingerprint    string          `json:"fingerprint"`
	CorrelationKey string          `json:"correlation_key"`
	Cluster        string          `json:"cluster"`
	Namespace      string          `json:"namespace"`
	ServiceName    string          `json:"service_name"`
	Environment    string          `json:"environment"`
	TargetKind     string          `json:"target_kind"`
	TargetName     string          `json:"target_name"`
	Severity       domain.Severity `json:"severity"`
	Status         domain.Status   `json:"status"`
	Summary        string          `json:"summary"`
	FirstSeenAt    time.Time       `json:"first_seen_at"`
	LastSeenAt     time.Time       `json:"last_seen_at"`
	ResolvedAt     *time.Time      `json:"resolved_at,omitempty"`
	Version        uint64          `json:"version"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type signalDTO struct {
	Source        string              `json:"source"`
	SourceEventID string              `json:"source_event_id"`
	Fingerprint   string              `json:"fingerprint"`
	Status        domain.SignalStatus `json:"status"`
	Severity      domain.Severity     `json:"severity"`
	Cluster       string              `json:"cluster"`
	Namespace     string              `json:"namespace"`
	ServiceName   string              `json:"service_name"`
	Environment   string              `json:"environment"`
	TargetKind    string              `json:"target_kind"`
	TargetName    string              `json:"target_name"`
	Category      string              `json:"category"`
	OccurredAt    time.Time           `json:"occurred_at"`
	ReceivedAt    time.Time           `json:"received_at"`
	Summary       string              `json:"summary"`
	Labels        json.RawMessage     `json:"labels"`
	Annotations   json.RawMessage     `json:"annotations"`
	CreatedAt     time.Time           `json:"created_at"`
}

type timelineDTO struct {
	EventType  domain.EventType `json:"event_type"`
	ActorType  domain.ActorType `json:"actor_type"`
	ActorID    string           `json:"actor_id"`
	Summary    string           `json:"summary"`
	Metadata   json.RawMessage  `json:"metadata"`
	OccurredAt time.Time        `json:"occurred_at"`
	CreatedAt  time.Time        `json:"created_at"`
}

type evidenceDTO struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Source      string          `json:"source"`
	ResourceRef string          `json:"resource_ref"`
	TimeRange   json.RawMessage `json:"time_range"`
	Query       string          `json:"query"`
	Summary     string          `json:"summary"`
	Facts       json.RawMessage `json:"facts"`
	RawRef      string          `json:"raw_ref"`
	Truncated   bool            `json:"truncated"`
	CollectedAt time.Time       `json:"collected_at"`
	CreatedAt   time.Time       `json:"created_at"`
}

// IncidentAlertmanagerWebhook accepts the isolated V2 Alertmanager contract.
func (h *Handler) IncidentAlertmanagerWebhook(c *gin.Context) {
	if h.incidentService == nil {
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "incident persistence is unavailable"})
		return
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		c.JSON(http.StatusUnsupportedMediaType, response{Status: "error", Error: "Content-Type must be application/json"})
		return
	}
	var payload webhook.AlertmanagerWebhookRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		status := http.StatusBadRequest
		message := "invalid alertmanager JSON payload"
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
			message = "alertmanager payload too large"
		}
		c.JSON(status, response{Status: "error", Error: message})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()
	results, err := h.incidentService.IngestAlertmanager(ctx, payload)
	if err != nil {
		writeIncidentError(c, err)
		return
	}
	traceID := traceIDFromContext(ctx)
	for _, result := range results {
		logger.FromContext(ctx).Info("incident signal accepted",
			zap.String("incident_id", result.IncidentPublicID),
			zap.String("incident_public_id", result.IncidentPublicID),
			zap.String("source", "alertmanager"),
			zap.String("source_event_id", result.SourceEventID),
			zap.String("correlation_key", result.CorrelationKey),
			zap.String("correlation_id", middleware.RequestID(c)),
			zap.String("trace_id", traceID),
			zap.Bool("duplicate", result.Duplicate),
			zap.Bool("unmatched_resolved", result.NoMatchingIncident),
		)
	}
	c.JSON(http.StatusAccepted, response{Status: "accepted", Data: results})
}

// ListIncidents returns a filtered, bounded Incident page.
func (h *Handler) ListIncidents(c *gin.Context) {
	if h.incidentService == nil {
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "incident persistence is unavailable"})
		return
	}
	filter, err := parseIncidentFilter(c)
	if err != nil {
		writeIncidentError(c, err)
		return
	}
	page, err := h.incidentService.ListIncidents(c.Request.Context(), filter)
	if err != nil {
		writeIncidentError(c, err)
		return
	}
	items := make([]incidentDTO, 0, len(page.Items))
	for index := range page.Items {
		items = append(items, toIncidentDTO(&page.Items[index]))
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"items": items, "total": page.Total, "page": page.Page, "page_size": page.PageSize}})
}

// GetIncident returns one Incident by unpredictable public ID.
func (h *Handler) GetIncident(c *gin.Context) {
	item, ok := h.loadIncident(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: toIncidentDTO(item)})
}

// ListIncidentSignals returns bounded normalized signals without raw payloads.
func (h *Handler) ListIncidentSignals(c *gin.Context) {
	if !h.requireIncidentService(c) {
		return
	}
	items, err := h.incidentService.ListSignals(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeIncidentError(c, err)
		return
	}
	dtos := make([]signalDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, signalDTO{Source: item.Source, SourceEventID: item.SourceEventID, Fingerprint: item.Fingerprint, Status: item.Status, Severity: item.Severity, Cluster: item.Cluster, Namespace: item.Namespace, ServiceName: item.ServiceName, Environment: item.Environment, TargetKind: item.TargetKind, TargetName: item.TargetName, Category: item.Category, OccurredAt: item.OccurredAt, ReceivedAt: item.ReceivedAt, Summary: item.Summary, Labels: item.Labels, Annotations: item.Annotations, CreatedAt: item.CreatedAt})
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: dtos})
}

// ListIncidentTimeline returns bounded typed domain events.
func (h *Handler) ListIncidentTimeline(c *gin.Context) {
	if !h.requireIncidentService(c) {
		return
	}
	items, err := h.incidentService.ListTimeline(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeIncidentError(c, err)
		return
	}
	dtos := make([]timelineDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, timelineDTO{EventType: item.EventType, ActorType: item.ActorType, ActorID: item.ActorID, Summary: item.Summary, Metadata: item.Metadata, OccurredAt: item.OccurredAt, CreatedAt: item.CreatedAt})
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: dtos})
}

// ListIncidentEvidence returns bounded evidence metadata and external references.
func (h *Handler) ListIncidentEvidence(c *gin.Context) {
	if !h.requireIncidentService(c) {
		return
	}
	items, err := h.incidentService.ListEvidence(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeIncidentError(c, err)
		return
	}
	dtos := make([]evidenceDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, evidenceDTO{ID: item.PublicID, Type: item.Type, Source: item.Source, ResourceRef: item.ResourceRef, TimeRange: item.TimeRange, Query: item.Query, Summary: item.Summary, Facts: item.Facts, RawRef: item.RawRef, Truncated: item.Truncated, CollectedAt: item.CollectedAt, CreatedAt: item.CreatedAt})
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: dtos})
}

func (h *Handler) loadIncident(c *gin.Context) (*domain.Incident, bool) {
	if !h.requireIncidentService(c) {
		return nil, false
	}
	item, err := h.incidentService.GetIncident(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeIncidentError(c, err)
		return nil, false
	}
	return item, true
}

func (h *Handler) requireIncidentService(c *gin.Context) bool {
	if h.incidentService != nil {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: "incident persistence is unavailable"})
	return false
}

func parseIncidentFilter(c *gin.Context) (domain.ListFilter, error) {
	for name, maximum := range map[string]int{"cluster": 128, "namespace": 128, "service": 128, "environment": 128, "workload": 253, "q": 128} {
		if len(strings.TrimSpace(c.Query(name))) > maximum {
			return domain.ListFilter{}, errors.Join(domain.ErrInvalidArgument, fmt.Errorf("%s exceeds maximum length %d", name, maximum))
		}
	}
	filter := domain.ListFilter{
		Page:        1,
		PageSize:    20,
		Cluster:     boundedQuery(c.Query("cluster"), 128),
		Namespace:   boundedQuery(c.Query("namespace"), 128),
		ServiceName: boundedQuery(c.Query("service"), 128),
		Environment: boundedQuery(c.Query("environment"), 128),
		Workload:    boundedQuery(c.Query("workload"), 253),
		Search:      boundedQuery(c.Query("q"), 128),
	}
	for _, value := range []struct {
		raw    string
		target **time.Time
		name   string
	}{
		{raw: c.Query("created_from"), target: &filter.CreatedFrom, name: "created_from"},
		{raw: c.Query("created_to"), target: &filter.CreatedTo, name: "created_to"},
	} {
		if strings.TrimSpace(value.raw) == "" {
			continue
		}
		parsed, parseErr := time.Parse(time.RFC3339, value.raw)
		if parseErr != nil {
			return filter, errors.Join(domain.ErrInvalidArgument, fmt.Errorf("%s must be RFC3339", value.name))
		}
		utc := parsed.UTC()
		*value.target = &utc
	}
	if filter.CreatedFrom != nil && filter.CreatedTo != nil && filter.CreatedFrom.After(*filter.CreatedTo) {
		return filter, errors.Join(domain.ErrInvalidArgument, errors.New("created_from must not be after created_to"))
	}
	var err error
	if raw := c.Query("page"); raw != "" {
		filter.Page, err = strconv.Atoi(raw)
		if err != nil || filter.Page < 1 {
			return filter, errors.Join(domain.ErrInvalidArgument, errors.New("page must be a positive integer"))
		}
	}
	if raw := c.Query("page_size"); raw != "" {
		filter.PageSize, err = strconv.Atoi(raw)
		if err != nil || filter.PageSize < 1 {
			return filter, errors.Join(domain.ErrInvalidArgument, errors.New("page_size must be a positive integer"))
		}
	}
	if raw := strings.ToUpper(strings.TrimSpace(c.Query("status"))); raw != "" {
		filter.Status = domain.Status(raw)
		valid := false
		for _, status := range domain.AllStatuses() {
			valid = valid || status == filter.Status
		}
		if !valid {
			return filter, errors.Join(domain.ErrInvalidArgument, errors.New("invalid status filter"))
		}
	}
	if raw := strings.ToLower(strings.TrimSpace(c.Query("severity"))); raw != "" {
		filter.Severity = domain.Severity(raw)
		if normalized := domain.NormalizeSeverity(raw); normalized != filter.Severity || normalized == domain.SeverityUnknown {
			return filter, errors.Join(domain.ErrInvalidArgument, errors.New("invalid severity filter"))
		}
	}
	return filter, nil
}

func boundedQuery(value string, maximum int) string {
	return strings.TrimSpace(value)
}

func writeIncidentError(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, "incident operation failed"
	switch {
	case errors.Is(err, domain.ErrInvalidArgument), errors.Is(err, domain.ErrInvalidTransition):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, domain.ErrNotFound):
		status, message = http.StatusNotFound, "incident not found"
	case errors.Is(err, domain.ErrConflict):
		status, message = http.StatusConflict, "incident state conflict"
	case errors.Is(err, domain.ErrUnavailable):
		status, message = http.StatusServiceUnavailable, "incident persistence is unavailable"
	}
	c.JSON(status, response{Status: "error", Error: message})
}

func toIncidentDTO(item *domain.Incident) incidentDTO {
	return incidentDTO{ID: item.PublicID, Fingerprint: item.Fingerprint, CorrelationKey: item.CorrelationKey, Cluster: item.Cluster, Namespace: item.Namespace, ServiceName: item.ServiceName, Environment: item.Environment, TargetKind: item.TargetKind, TargetName: item.TargetName, Severity: item.Severity, Status: item.Status, Summary: item.Summary, FirstSeenAt: item.FirstSeenAt, LastSeenAt: item.LastSeenAt, ResolvedAt: item.ResolvedAt, Version: item.Version, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func traceIDFromContext(ctx context.Context) string {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.HasTraceID() {
		return ""
	}
	return spanContext.TraceID().String()
}
