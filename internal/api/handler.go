package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	requestIDContextKey = "api_request_id"
	traceIDContextKey   = "api_trace_id"
)

var localOwnerIdentity = OwnerIdentity{
	Subject: "local-owner", Provider: "local", Login: "owner", Role: "owner",
}

type Config struct {
	Queries        QueryPort
	Commands       CommandPort
	Settings       SettingsPort
	Notifications  NotificationPort
	AllowedOrigins []string
	Now            func() time.Time
}

type Handler struct {
	queries        QueryPort
	commands       CommandPort
	settings       SettingsPort
	notifications  NotificationPort
	allowedOrigins map[string]struct{}
	now            func() time.Time
	idempotency    *idempotencyStore
}

func NewHandler(config Config) *Handler {
	if config.Queries == nil {
		config.Queries = UnsupportedQueryPort{}
	}
	if config.Commands == nil {
		config.Commands = UnsupportedCommandPort{}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Handler{
		queries:        config.Queries,
		commands:       config.Commands,
		settings:       config.Settings,
		notifications:  config.Notifications,
		allowedOrigins: normalizeOrigins(config.AllowedOrigins),
		now:            config.Now,
		idempotency:    newIdempotencyStore(config.Now),
	}
}

type RouteSpec struct {
	Method string
	Path   string
}

var routes = []RouteSpec{
	{Method: http.MethodGet, Path: "/api/v1/bootstrap"},
	{Method: http.MethodGet, Path: "/api/v1/scopes"},
	{Method: http.MethodGet, Path: "/api/v1/overview"},
	{Method: http.MethodGet, Path: "/api/v1/settings"},
	{Method: http.MethodPost, Path: "/api/v1/settings/validate"},
	{Method: http.MethodGet, Path: "/api/v1/configuration-revisions"},
	{Method: http.MethodPost, Path: "/api/v1/configuration-revisions"},
	{Method: http.MethodPost, Path: "/api/v1/secrets"},
	{Method: http.MethodPost, Path: "/api/v1/providers/:provider/tests"},
	{Method: http.MethodGet, Path: "/api/v1/storage-status"},
	{Method: http.MethodGet, Path: "/api/v1/notifications"},
	{Method: http.MethodPost, Path: "/api/v1/notifications/:id/read"},
	{Method: http.MethodPost, Path: "/api/v1/notifications/read-all"},
	{Method: http.MethodGet, Path: "/api/v1/notification-events"},
	{Method: http.MethodGet, Path: "/api/v1/incidents"},
	{Method: http.MethodGet, Path: "/api/v1/incidents/:id"},
	{Method: http.MethodGet, Path: "/api/v1/incidents/:id/signals"},
	{Method: http.MethodGet, Path: "/api/v1/incidents/:id/timeline"},
	{Method: http.MethodGet, Path: "/api/v1/incidents/:id/evidence"},
	{Method: http.MethodGet, Path: "/api/v1/incidents/:id/investigations"},
	{Method: http.MethodGet, Path: "/api/v1/incidents/:id/remediation-plans"},
	{Method: http.MethodGet, Path: "/api/v1/incidents/:id/delivery"},
	{Method: http.MethodGet, Path: "/api/v1/incidents/:id/verifications"},
	{Method: http.MethodGet, Path: "/api/v1/incidents/:id/resolution-report"},
	{Method: http.MethodGet, Path: "/api/v1/incidents/:id/events"},
	{Method: http.MethodPost, Path: "/api/v1/incidents/:id/investigations"},
	{Method: http.MethodPost, Path: "/api/v1/incidents/:id/close"},
	{Method: http.MethodPost, Path: "/api/v1/remediation-plans/:id/decisions"},
}

func Routes() []RouteSpec {
	result := make([]RouteSpec, len(routes))
	copy(result, routes)
	return result
}

// RegisterRoutes installs the sole public CloudOps API contract.
func RegisterRoutes(group *gin.RouterGroup, handler *Handler) {
	if handler == nil {
		handler = NewHandler(Config{})
	}
	group.Use(handler.requestIdentity, handler.recoverProblems)

	queries := group.Group("")
	queries.GET("/bootstrap", handler.getBootstrap)
	queries.GET("/scopes", handler.listScopes)
	queries.GET("/overview", handler.getOverview)
	queries.GET("/settings", handler.getSettings)
	queries.GET("/configuration-revisions", handler.listConfigurationRevisions)
	queries.GET("/storage-status", handler.getStorageStatus)
	queries.GET("/notifications", handler.listNotifications)
	queries.GET("/notification-events", handler.streamNotificationEvents)
	queries.GET("/incidents", handler.listIncidents)
	queries.GET("/incidents/:id", handler.getIncident)
	queries.GET("/incidents/:id/signals", handler.listResources(QuerySignals))
	queries.GET("/incidents/:id/timeline", handler.listResources(QueryTimeline))
	queries.GET("/incidents/:id/evidence", handler.listResources(QueryEvidence))
	queries.GET("/incidents/:id/investigations", handler.listResources(QueryInvestigations))
	queries.GET("/incidents/:id/remediation-plans", handler.listRemediationPlans)
	queries.GET("/incidents/:id/delivery", handler.getDelivery)
	queries.GET("/incidents/:id/verifications", handler.listVerifications)
	queries.GET("/incidents/:id/resolution-report", handler.getResource(QueryResolutionReport))
	queries.GET("/incidents/:id/events", handler.streamEvents)

	commands := group.Group("")
	commands.Use(handler.requireMutationOrigin)
	commands.POST("/settings/validate", handler.validateSettings)
	commands.POST("/configuration-revisions", handler.createConfigurationRevision)
	commands.POST("/secrets", handler.createSecret)
	commands.POST("/providers/:provider/tests", handler.testProvider)
	commands.POST("/notifications/read-all", handler.readAllNotifications)
	commands.POST("/notifications/:id/read", handler.readNotification)
	commands.POST("/incidents/:id/investigations", handler.startInvestigation)
	commands.POST("/incidents/:id/close", handler.closeIncident)
	commands.POST("/remediation-plans/:id/decisions", handler.decideRemediation)
}

func (h *Handler) listIncidents(c *gin.Context) {
	cursor, afterID, limit, err := parseListOptions(c.Request)
	if err != nil {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_CURSOR", "cursor and limit parameters are invalid")
		return
	}
	status, severity, service, err := parseIncidentFilters(c.Request)
	if err != nil {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_FILTER", "status, severity, or service filter is invalid")
		return
	}
	result, err := h.queries.Query(c.Request.Context(), QueryRequest{
		Kind: QueryIncidents, Cursor: cursor, AfterID: afterID, Limit: limit,
		Status: status, Severity: severity, Service: service,
	})
	if err != nil {
		h.writeQueryError(c, err)
		return
	}
	if len(result.Incidents) > limit {
		result.Incidents = result.Incidents[:limit]
	}
	for index := range result.Incidents {
		if err := validateIncidentView(&result.Incidents[index]); err != nil {
			h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "query projection violated the public identifier contract")
			return
		}
	}
	h.writeJSON(c, http.StatusOK, collectionResponse[IncidentView]{Items: nonNilIncidents(result.Incidents), NextCursor: result.NextCursor})
}

func (h *Handler) getIncident(c *gin.Context) {
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	result, err := h.queries.Query(c.Request.Context(), QueryRequest{Kind: QueryIncident, IncidentID: id, Limit: 1})
	if err != nil {
		h.writeQueryError(c, err)
		return
	}
	if result.Incident == nil {
		h.writeProblem(c, http.StatusNotFound, "RESOURCE_NOT_FOUND", "incident was not found")
		return
	}
	if err := validateIncidentView(result.Incident); err != nil {
		h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "query projection violated the public identifier contract")
		return
	}
	h.writeJSON(c, http.StatusOK, incidentResponse{Incident: *result.Incident})
}

func (h *Handler) listResources(kind QueryKind) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := h.publicID(c)
		if !ok {
			return
		}
		cursor, afterID, limit, err := parseListOptions(c.Request)
		if err != nil {
			h.writeProblem(c, http.StatusBadRequest, "INVALID_CURSOR", "cursor and limit parameters are invalid")
			return
		}
		result, err := h.queries.Query(c.Request.Context(), QueryRequest{Kind: kind, IncidentID: id, Cursor: cursor, AfterID: afterID, Limit: limit})
		if err != nil {
			h.writeQueryError(c, err)
			return
		}
		if len(result.Items) > limit {
			result.Items = result.Items[:limit]
		}
		if err := validateResources(result.Items); err != nil {
			h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "query projection violated the public identifier contract")
			return
		}
		h.writeJSON(c, http.StatusOK, collectionResponse[ResourceView]{Items: nonNilResources(result.Items), NextCursor: result.NextCursor})
	}
}

func (h *Handler) getResource(kind QueryKind) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := h.publicID(c)
		if !ok {
			return
		}
		result, err := h.queries.Query(c.Request.Context(), QueryRequest{Kind: kind, IncidentID: id, Limit: 1})
		if err != nil {
			h.writeQueryError(c, err)
			return
		}
		if kind == QueryResolutionReport {
			if result.ResolutionReport != nil {
				if err := validateResolutionReportView(result.ResolutionReport); err != nil {
					h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "query projection violated the public identifier contract")
					return
				}
			}
			h.writeJSON(c, http.StatusOK, resolutionReportResponse{Resource: result.ResolutionReport})
			return
		}
		if result.Resource == nil {
			h.writeProblem(c, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource was not found")
			return
		}
		if err := validateResource(result.Resource); err != nil {
			h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "query projection violated the public identifier contract")
			return
		}
		h.writeJSON(c, http.StatusOK, resourceResponse{Resource: *result.Resource})
	}
}

func (h *Handler) streamEvents(c *gin.Context) {
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	lastEventID := strings.TrimSpace(c.GetHeader("Last-Event-ID"))
	if len(lastEventID) > maxCursorBytes || containsControl(lastEventID) {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_EVENT_CURSOR", "Last-Event-ID is invalid")
		return
	}
	result, err := h.queries.Query(c.Request.Context(), QueryRequest{Kind: QueryEvents, IncidentID: id, LastEventID: lastEventID, Limit: maxPageSize})
	if err != nil {
		h.writeQueryError(c, err)
		return
	}
	c.Header("Content-Type", SSEMediaType)
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	for _, event := range result.Events {
		if event.IncidentID != id || len(event.Cursor) > maxCursorBytes || containsControl(event.Cursor) || !validRefreshResource(event.Resource) {
			continue
		}
		payload, err := json.Marshal(RefreshEvent{IncidentID: id, Resource: event.Resource})
		if err != nil {
			continue
		}
		if event.Cursor != "" {
			_, _ = fmt.Fprintf(c.Writer, "id: %s\n", event.Cursor)
		}
		_, _ = fmt.Fprintf(c.Writer, "event: incident.refresh\ndata: %s\n\n", payload)
	}
	c.Writer.Flush()
}

type versionedCommandBody struct {
	ExpectedVersion uint64 `json:"expected_version"`
	Reason          string `json:"reason,omitempty"`
}

type decisionCommandBody struct {
	Decision        string `json:"decision"`
	ExpectedVersion uint64 `json:"expected_version"`
	ExpectedHash    string `json:"expected_hash"`
	Reason          string `json:"reason,omitempty"`
}

func (h *Handler) startInvestigation(c *gin.Context) {
	var body versionedCommandBody
	if !h.decodeCommand(c, &body) {
		return
	}
	if body.ExpectedVersion == 0 {
		h.writeProblem(c, http.StatusBadRequest, "EXPECTED_VERSION_REQUIRED", "expected_version must be positive")
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if len(body.Reason) > 1024 {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_REQUEST", "reason exceeds the maximum length")
		return
	}
	h.executeCommand(c, CommandStartInvestigation, body.ExpectedVersion, "", body)
}

func (h *Handler) closeIncident(c *gin.Context) {
	var body versionedCommandBody
	if !h.decodeCommand(c, &body) {
		return
	}
	if body.ExpectedVersion == 0 {
		h.writeProblem(c, http.StatusBadRequest, "EXPECTED_VERSION_REQUIRED", "expected_version must be positive")
		return
	}
	if len(body.Reason) > 2048 {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_REQUEST", "reason exceeds the maximum length")
		return
	}
	h.executeCommand(c, CommandCloseIncident, body.ExpectedVersion, "", body)
}

func (h *Handler) decideRemediation(c *gin.Context) {
	var body decisionCommandBody
	if !h.decodeCommand(c, &body) {
		return
	}
	body.Decision = strings.ToLower(strings.TrimSpace(body.Decision))
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Decision != "approved" && body.Decision != "rejected" {
		h.writeProblem(c, http.StatusUnprocessableEntity, "INVALID_TRANSITION", "decision must be approved or rejected")
		return
	}
	if body.ExpectedVersion == 0 {
		h.writeProblem(c, http.StatusBadRequest, "EXPECTED_VERSION_REQUIRED", "expected_version must be positive")
		return
	}
	if err := validateExpectedHash(body.ExpectedHash); err != nil {
		h.writeProblem(c, http.StatusBadRequest, "EXPECTED_HASH_REQUIRED", "expected_hash must be a lowercase SHA-256 digest")
		return
	}
	if body.Reason == "" || len(body.Reason) > 1024 {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_REQUEST", "reason is required and must not exceed 1024 bytes")
		return
	}
	h.executeCommand(c, CommandDecideRemediation, body.ExpectedVersion, body.ExpectedHash, body)
}

func (h *Handler) decodeCommand(c *gin.Context, target any) bool {
	if err := requireJSON(c.Request); err != nil {
		h.writeProblem(c, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
		return false
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			h.writeProblem(c, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request body exceeds the configured limit")
			return false
		}
		h.writeProblem(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be one valid JSON object")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must contain exactly one JSON object")
		return false
	}
	return true
}

func (h *Handler) executeCommand(c *gin.Context, kind CommandKind, expectedVersion uint64, expectedHash string, body any) {
	resourceID, ok := h.publicID(c)
	if !ok {
		return
	}
	idempotencyKey, err := validateIdempotencyKey(c.GetHeader(IdempotencyHeader))
	if err != nil {
		h.writeProblem(c, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "a valid Idempotency-Key header is required")
		return
	}
	canonical, payloadHash, err := canonicalPayload(body)
	if err != nil {
		h.writeProblem(c, http.StatusInternalServerError, "CANONICALIZATION_FAILED", "request could not be canonicalized")
		return
	}
	requestID, traceID := requestAndTraceID(c)
	request := CommandRequest{
		Kind:            kind,
		ResourceID:      resourceID,
		Actor:           localOwnerIdentity,
		IdempotencyKey:  idempotencyKey,
		ExpectedVersion: expectedVersion,
		ExpectedHash:    expectedHash,
		CanonicalBody:   canonical,
		RequestID:       requestID,
		TraceID:         traceID,
	}
	scopeKey := idempotencyScopeKey(localOwnerIdentity, kind, resourceID, idempotencyKey)
	reservation, replay, err := h.idempotency.reserve(c.Request.Context(), scopeKey, payloadHash)
	if errors.Is(err, errIdempotencyKeyReused) {
		h.writeProblem(c, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "Idempotency-Key was already used with a different payload")
		return
	}
	if err != nil {
		h.writeProblem(c, http.StatusInternalServerError, "IDEMPOTENCY_UNAVAILABLE", "idempotency contract is unavailable")
		return
	}
	if replay != nil {
		c.Header(ReplayHeader, "true")
		h.writeStored(c, *replay)
		return
	}

	result, executeErr := h.commands.Execute(c.Request.Context(), request)
	if result.Replayed {
		c.Header(ReplayHeader, "true")
	}
	if executeErr != nil {
		status, code, detail := commandProblem(executeErr)
		response := h.problemResponse(c, status, code, detail)
		h.idempotency.complete(reservation, response, status < http.StatusInternalServerError)
		h.writeStored(c, response)
		return
	}
	result.HTTPStatus = normalizeCommandStatus(result.HTTPStatus)
	if result.HTTPStatus != http.StatusAccepted {
		response := h.problemResponse(c, http.StatusInternalServerError, "INVALID_COMMAND_RESULT", "command port returned an invalid status")
		h.idempotency.complete(reservation, response, false)
		h.writeStored(c, response)
		return
	}
	if result.ResourceID == "" {
		result.ResourceID = resourceID
	}
	result.ResourceID, err = ParsePublicUUID(result.ResourceID)
	if err != nil {
		response := h.problemResponse(c, http.StatusInternalServerError, "INVALID_COMMAND_RESULT", "command port returned a non-public identifier")
		h.idempotency.complete(reservation, response, false)
		h.writeStored(c, response)
		return
	}
	if result.Status == "" {
		result.Status = "accepted"
	}
	encoded, err := json.Marshal(commandResponse{ID: result.ResourceID, Command: string(kind), Status: result.Status, Version: result.Version, Cycle: result.Cycle})
	if err != nil {
		response := h.problemResponse(c, http.StatusInternalServerError, "ENCODING_FAILED", "command response could not be encoded")
		h.idempotency.complete(reservation, response, false)
		h.writeStored(c, response)
		return
	}
	response := storedHTTPResponse{Status: http.StatusAccepted, ContentType: JSONMediaType, Body: encoded}
	h.idempotency.complete(reservation, response, true)
	h.writeStored(c, response)
}

func (h *Handler) publicID(c *gin.Context) (string, bool) {
	id, err := ParsePublicUUID(c.Param("id"))
	if err != nil {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_PUBLIC_ID", "id must be a public UUID")
		return "", false
	}
	return id, true
}

func (h *Handler) writeQueryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidArgument):
		h.writeProblem(c, http.StatusBadRequest, "INVALID_QUERY", "query parameters are invalid")
	case errors.Is(err, ErrNotFound):
		h.writeProblem(c, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource was not found")
	case errors.Is(err, ErrNotImplemented):
		h.writeProblem(c, http.StatusNotImplemented, "NOT_IMPLEMENTED", "query is not implemented")
	default:
		h.writeProblem(c, http.StatusServiceUnavailable, "QUERY_UNAVAILABLE", "query projection is unavailable")
	}
}

func commandProblem(err error) (int, string, string) {
	switch {
	case errors.Is(err, ErrNotImplemented):
		return http.StatusNotImplemented, "NOT_IMPLEMENTED", "domain command is not implemented"
	case errors.Is(err, ErrStaleVersion):
		return http.StatusConflict, "STALE_EXPECTATION", "expected version or hash is stale"
	case errors.Is(err, ErrConflict):
		return http.StatusConflict, "COMMAND_CONFLICT", "command conflicts with current aggregate state"
	case errors.Is(err, ErrInvalidTransition):
		return http.StatusUnprocessableEntity, "INVALID_TRANSITION", "business transition is not allowed"
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource was not found"
	case errors.Is(err, ErrInvalidArgument):
		return http.StatusBadRequest, "INVALID_REQUEST", "command request is invalid"
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden, "COMMAND_FORBIDDEN", "the local Owner is not allowed to execute this command"
	case errors.Is(err, ErrUnavailable):
		return http.StatusServiceUnavailable, "COMMAND_UNAVAILABLE", "command service is unavailable"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "command could not be completed"
	}
}

func (h *Handler) writeProblem(c *gin.Context, status int, code, detail string) {
	h.writeStored(c, h.problemResponse(c, status, code, detail))
}

// WriteRouteNotFound keeps unknown API paths inside the Problem Details contract.
func WriteRouteNotFound(c *gin.Context) {
	ensureRequestIdentity(c)
	NewHandler(Config{}).writeProblem(c, http.StatusNotFound, "ROUTE_NOT_FOUND", "API route was not found")
}

func (h *Handler) problemResponse(c *gin.Context, status int, code, detail string) storedHTTPResponse {
	requestID, traceID := requestAndTraceID(c)
	instance := c.FullPath()
	if instance == "" {
		instance = "/api/v1"
	}
	problem := Problem{
		Type:      "urn:cloudops:problem:" + strings.ToLower(strings.ReplaceAll(code, "_", "-")),
		Title:     problemTitle(code),
		Status:    status,
		Detail:    detail,
		Instance:  instance,
		Code:      code,
		RequestID: requestID,
		TraceID:   traceID,
		NextSteps: problemNextSteps(code),
	}
	body, err := json.Marshal(problem)
	if err != nil {
		body = []byte(`{"type":"urn:cloudops:problem:internal-error","title":"Internal error","status":500,"detail":"problem response encoding failed","code":"INTERNAL_ERROR"}`)
		status = http.StatusInternalServerError
	}
	return storedHTTPResponse{Status: status, ContentType: ProblemMediaType, Body: body}
}

func problemTitle(code string) string {
	switch code {
	case "INVALID_PUBLIC_ID", "INVALID_CURSOR", "INVALID_EVENT_CURSOR", "INVALID_FILTER", "INVALID_QUERY", "INVALID_REQUEST", "INVALID_CONFIGURATION", "INVALID_PROVIDER", "IDEMPOTENCY_KEY_REQUIRED", "EXPECTED_VERSION_REQUIRED", "EXPECTED_HASH_REQUIRED", "REQUEST_TOO_LARGE":
		return "Invalid request"
	case "UNSUPPORTED_MEDIA_TYPE":
		return "Unsupported media type"
	case "COMMAND_FORBIDDEN", "ORIGIN_REQUIRED", "ORIGIN_FORBIDDEN":
		return "Forbidden"
	case "RESOURCE_NOT_FOUND", "ROUTE_NOT_FOUND", "SETTINGS_RESOURCE_NOT_FOUND", "NOTIFICATION_NOT_FOUND":
		return "Resource not found"
	case "IDEMPOTENCY_KEY_REUSED", "STALE_EXPECTATION", "STALE_VALIDATION", "VALIDATION_EXPIRED", "COMMAND_CONFLICT":
		return "Command conflict"
	case "INVALID_TRANSITION", "VALIDATION_FAILED":
		return "Invalid transition"
	case "NOT_IMPLEMENTED":
		return "Not implemented"
	case "RATE_LIMITED":
		return "Rate limit exceeded"
	case "QUERY_UNAVAILABLE", "COMMAND_UNAVAILABLE", "SETTINGS_UNAVAILABLE", "NOTIFICATIONS_UNAVAILABLE":
		return "Service unavailable"
	default:
		return "Internal error"
	}
}

func problemNextSteps(code string) []string {
	switch code {
	case "STALE_VALIDATION", "VALIDATION_EXPIRED":
		return []string{"重新验证当前配置草稿", "确认 validation_id 与未修改的草稿匹配后再次应用"}
	case "VALIDATION_FAILED", "INVALID_CONFIGURATION":
		return []string{"检查响应中的字段错误", "修正配置或 Provider 状态后重新验证"}
	case "SETTINGS_UNAVAILABLE", "NOTIFICATIONS_UNAVAILABLE", "QUERY_UNAVAILABLE", "COMMAND_UNAVAILABLE":
		return []string{"检查 Bootstrap diagnostics 与 Provider health", "确认 MySQL、API 和 worker 均已就绪后重试"}
	case "INVALID_PROVIDER":
		return []string{"使用 Settings 返回的 Provider identity", "确认路径 Provider 与请求配置一致"}
	case "RESOURCE_NOT_FOUND", "SETTINGS_RESOURCE_NOT_FOUND", "NOTIFICATION_NOT_FOUND":
		return []string{"刷新当前 Workspace 数据", "确认 Context Link 或 public UUID 仍然有效"}
	default:
		return []string{"根据 code 修正请求后重试", "使用 request_id 和 trace_id 定位对应日志"}
	}
}

func (h *Handler) writeJSON(c *gin.Context, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		h.writeProblem(c, http.StatusInternalServerError, "ENCODING_FAILED", "response could not be encoded")
		return
	}
	h.writeStored(c, storedHTTPResponse{Status: status, ContentType: JSONMediaType, Body: body})
}

func (h *Handler) writeStored(c *gin.Context, response storedHTTPResponse) {
	c.Header("Content-Type", response.ContentType)
	c.Status(response.Status)
	_, _ = c.Writer.Write(response.Body)
}

func requestAndTraceID(c *gin.Context) (string, string) {
	requestID, _ := c.Get(requestIDContextKey)
	traceID, _ := c.Get(traceIDContextKey)
	request, _ := requestID.(string)
	traceValue, _ := traceID.(string)
	if request == "" {
		request = boundedRequestID(c.GetHeader(RequestIDHeader))
	}
	if traceValue == "" {
		traceValue = request
	}
	return request, traceValue
}

func idempotencyScopeKey(identity OwnerIdentity, kind CommandKind, resourceID, key string) string {
	canonical := strings.Join([]string{identity.Provider, identity.Subject, string(kind), resourceID, key}, "\x00")
	digest := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(digest[:])
}

func validateIncidentView(item *IncidentView) error {
	if item == nil {
		return ErrInvalidArgument
	}
	id, err := ParsePublicUUID(item.ID)
	if err != nil || item.Cycle == 0 || item.Version == 0 || !validIncidentStatus(item.Status) ||
		!validSeverity(item.Severity) || len(item.Summary) > 2048 || len(item.BlockingReasonCode) > 128 {
		return ErrInvalidArgument
	}
	item.ID = id
	return nil
}

func validateResources(items []ResourceView) error {
	for index := range items {
		if err := validateResource(&items[index]); err != nil {
			return err
		}
	}
	return nil
}

func validateResource(item *ResourceView) error {
	if item == nil {
		return ErrInvalidArgument
	}
	id, err := ParsePublicUUID(item.ID)
	if err != nil {
		return err
	}
	if item.Kind == "" || len(item.Kind) > 64 || len(item.Status) > 64 || len(item.Summary) > 2048 {
		return ErrInvalidArgument
	}
	if item.Hash != "" && validateExpectedHash(item.Hash) != nil {
		return ErrInvalidArgument
	}
	item.ID = id
	return nil
}

func nonNilIncidents(items []IncidentView) []IncidentView {
	if items == nil {
		return []IncidentView{}
	}
	return items
}

func nonNilResources(items []ResourceView) []ResourceView {
	if items == nil {
		return []ResourceView{}
	}
	return items
}

func validIncidentStatus(status string) bool {
	switch status {
	case "detected", "investigating", "awaiting_approval", "delivering", "verifying", "resolved", "closed":
		return true
	default:
		return false
	}
}

func validSeverity(severity string) bool {
	switch severity {
	case "unknown", "info", "warning", "critical":
		return true
	default:
		return false
	}
}

func validRefreshResource(resource string) bool {
	switch resource {
	case "incident", "signals", "timeline", "evidence", "investigations", "remediation_plans", "delivery", "verifications", "resolution_report":
		return true
	default:
		return false
	}
}
