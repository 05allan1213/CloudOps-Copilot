package apiv3

import (
	"crypto/rand"
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
	requestIDContextKey = "apiv3_request_id"
	traceIDContextKey   = "apiv3_trace_id"
)

type Config struct {
	Queries        QueryPort
	Commands       CommandPort
	Authenticator  Authenticator
	RequireAuth    bool
	RequireCSRF    bool
	CSRFSecret     []byte
	AllowedOrigins []string
	CSRFTTL        time.Duration
	Now            func() time.Time
}

type Handler struct {
	queries        QueryPort
	commands       CommandPort
	authenticator  Authenticator
	requireAuth    bool
	requireCSRF    bool
	csrfSecret     []byte
	allowedOrigins map[string]struct{}
	csrfTTL        time.Duration
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
	if config.CSRFTTL <= 0 {
		config.CSRFTTL = 15 * time.Minute
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	csrfSecret := append([]byte(nil), config.CSRFSecret...)
	if len(csrfSecret) < sha256.Size {
		csrfSecret = make([]byte, sha256.Size)
		if _, err := rand.Read(csrfSecret); err != nil {
			csrfSecret = nil
		}
	}
	return &Handler{
		queries:        config.Queries,
		commands:       config.Commands,
		authenticator:  config.Authenticator,
		requireAuth:    config.RequireAuth,
		requireCSRF:    config.RequireCSRF,
		csrfSecret:     csrfSecret,
		allowedOrigins: normalizeOrigins(config.AllowedOrigins),
		csrfTTL:        config.CSRFTTL,
		now:            config.Now,
		idempotency:    newIdempotencyStore(config.Now),
	}
}

type RouteSpec struct {
	Method string
	Path   string
}

var phase2Routes = []RouteSpec{
	{Method: http.MethodGet, Path: "/api/v3/session/csrf"},
	{Method: http.MethodGet, Path: "/api/v3/incidents"},
	{Method: http.MethodGet, Path: "/api/v3/incidents/:id"},
	{Method: http.MethodGet, Path: "/api/v3/incidents/:id/signals"},
	{Method: http.MethodGet, Path: "/api/v3/incidents/:id/timeline"},
	{Method: http.MethodGet, Path: "/api/v3/incidents/:id/evidence"},
	{Method: http.MethodGet, Path: "/api/v3/incidents/:id/investigations"},
	{Method: http.MethodGet, Path: "/api/v3/incidents/:id/remediation-plans"},
	{Method: http.MethodGet, Path: "/api/v3/incidents/:id/delivery"},
	{Method: http.MethodGet, Path: "/api/v3/incidents/:id/verifications"},
	{Method: http.MethodGet, Path: "/api/v3/incidents/:id/resolution-report"},
	{Method: http.MethodGet, Path: "/api/v3/incidents/:id/events"},
	{Method: http.MethodPost, Path: "/api/v3/incidents/:id/investigations"},
	{Method: http.MethodPost, Path: "/api/v3/incidents/:id/close"},
	{Method: http.MethodPost, Path: "/api/v3/remediation-plans/:id/decisions"},
}

func Phase2Routes() []RouteSpec {
	result := make([]RouteSpec, len(phase2Routes))
	copy(result, phase2Routes)
	return result
}

// RegisterRoutes installs the complete Phase 2 V3 skeleton on a group rooted
// at /api/v3. The caller retains ownership of the V2 router and static assets.
func RegisterRoutes(group *gin.RouterGroup, handler *Handler) {
	if handler == nil {
		handler = NewHandler(Config{})
	}
	group.Use(handler.requestIdentity, handler.recoverProblems, handler.authenticate)

	queries := group.Group("")
	queries.Use(handler.requireViewer)
	queries.GET("/session/csrf", handler.issueCSRF)
	queries.GET("/incidents", handler.listIncidents)
	queries.GET("/incidents/:id", handler.getIncident)
	queries.GET("/incidents/:id/signals", handler.listResources(QuerySignals))
	queries.GET("/incidents/:id/timeline", handler.listResources(QueryTimeline))
	queries.GET("/incidents/:id/evidence", handler.listResources(QueryEvidence))
	queries.GET("/incidents/:id/investigations", handler.listResources(QueryInvestigations))
	queries.GET("/incidents/:id/remediation-plans", handler.listResources(QueryRemediationPlans))
	queries.GET("/incidents/:id/delivery", handler.getResource(QueryDelivery))
	queries.GET("/incidents/:id/verifications", handler.listResources(QueryVerifications))
	queries.GET("/incidents/:id/resolution-report", handler.getResource(QueryResolutionReport))
	queries.GET("/incidents/:id/events", handler.streamEvents)

	commands := group.Group("")
	commands.Use(handler.requireOperator, handler.requireCSRFToken)
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
			if result.ResolutionReport == nil {
				h.writeProblem(c, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource was not found")
				return
			}
			if err := validateResolutionReportView(result.ResolutionReport); err != nil {
				h.writeProblem(c, http.StatusInternalServerError, "INVALID_PROJECTION", "query projection violated the public identifier contract")
				return
			}
			h.writeJSON(c, http.StatusOK, resolutionReportResponse{Resource: *result.ResolutionReport})
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
	if len(body.Reason) > 2048 {
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
	identity, ok := IdentityFromContext(c.Request.Context())
	if !ok {
		h.writeProblem(c, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "authenticated session is required")
		return
	}
	requestID, traceID := requestAndTraceID(c)
	request := CommandRequest{
		Kind:            kind,
		ResourceID:      resourceID,
		Actor:           identity,
		IdempotencyKey:  idempotencyKey,
		ExpectedVersion: expectedVersion,
		ExpectedHash:    expectedHash,
		CanonicalBody:   canonical,
		RequestID:       requestID,
		TraceID:         traceID,
	}
	scopeKey := idempotencyScopeKey(identity, kind, resourceID, idempotencyKey)
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
		return http.StatusNotImplemented, "NOT_IMPLEMENTED", "domain command is not implemented in the Phase 2 skeleton"
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
		return http.StatusForbidden, "COMMAND_FORBIDDEN", "authenticated identity is not allowed to execute this command"
	case errors.Is(err, ErrUnavailable):
		return http.StatusServiceUnavailable, "COMMAND_UNAVAILABLE", "command service is unavailable"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "command could not be completed"
	}
}

func (h *Handler) writeProblem(c *gin.Context, status int, code, detail string) {
	h.writeStored(c, h.problemResponse(c, status, code, detail))
}

// WriteRouteNotFound is used by a mixed-version root router so unmatched V3
// paths keep the V3 error representation without changing legacy 404 behavior.
func WriteRouteNotFound(c *gin.Context) {
	ensureRequestIdentity(c)
	NewHandler(Config{}).writeProblem(c, http.StatusNotFound, "ROUTE_NOT_FOUND", "V3 API route was not found")
}

func (h *Handler) problemResponse(c *gin.Context, status int, code, detail string) storedHTTPResponse {
	requestID, traceID := requestAndTraceID(c)
	instance := c.FullPath()
	if instance == "" {
		instance = "/api/v3"
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
	case "INVALID_PUBLIC_ID", "INVALID_CURSOR", "INVALID_EVENT_CURSOR", "INVALID_FILTER", "INVALID_QUERY", "INVALID_REQUEST", "IDEMPOTENCY_KEY_REQUIRED", "EXPECTED_VERSION_REQUIRED", "EXPECTED_HASH_REQUIRED", "REQUEST_TOO_LARGE":
		return "Invalid request"
	case "UNSUPPORTED_MEDIA_TYPE":
		return "Unsupported media type"
	case "AUTHENTICATION_REQUIRED", "AUTHENTICATION_REVOKED":
		return "Authentication required"
	case "ROLE_FORBIDDEN", "COMMAND_FORBIDDEN", "CSRF_REQUIRED", "CSRF_INVALID", "ORIGIN_REQUIRED", "ORIGIN_FORBIDDEN":
		return "Forbidden"
	case "RESOURCE_NOT_FOUND", "ROUTE_NOT_FOUND":
		return "Resource not found"
	case "IDEMPOTENCY_KEY_REUSED", "STALE_EXPECTATION", "COMMAND_CONFLICT":
		return "Command conflict"
	case "INVALID_TRANSITION":
		return "Invalid transition"
	case "NOT_IMPLEMENTED":
		return "Not implemented"
	case "RATE_LIMITED":
		return "Rate limit exceeded"
	case "QUERY_UNAVAILABLE", "COMMAND_UNAVAILABLE", "AUTH_UNAVAILABLE", "CSRF_UNAVAILABLE":
		return "Service unavailable"
	default:
		return "Internal error"
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

func idempotencyScopeKey(identity Identity, kind CommandKind, resourceID, key string) string {
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
