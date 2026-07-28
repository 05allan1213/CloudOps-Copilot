package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	alertdomain "github.com/05allan1213/CloudOps-Copilot/internal/alert"
	"github.com/gin-gonic/gin"
)

type unavailableAlertPort struct{}

func (unavailableAlertPort) List(context.Context, alertdomain.ListRequest) (alertdomain.Page, error) {
	return alertdomain.Page{}, alertdomain.ErrProviderUnavailable
}
func (unavailableAlertPort) Detail(context.Context, string) (alertdomain.Detail, error) {
	return alertdomain.Detail{}, alertdomain.ErrProviderUnavailable
}
func (unavailableAlertPort) Acknowledge(context.Context, alertdomain.AcknowledgeRequest) (alertdomain.View, error) {
	return alertdomain.View{}, alertdomain.ErrProviderUnavailable
}
func (unavailableAlertPort) CreateSilence(context.Context, alertdomain.CreateSilenceRequest) (alertdomain.Silence, error) {
	return alertdomain.Silence{}, alertdomain.ErrProviderUnavailable
}
func (unavailableAlertPort) ExpireSilence(context.Context, alertdomain.ExpireSilenceRequest) (alertdomain.Silence, error) {
	return alertdomain.Silence{}, alertdomain.ErrProviderUnavailable
}
func (unavailableAlertPort) LinkIncident(context.Context, alertdomain.LinkIncidentRequest) (alertdomain.View, error) {
	return alertdomain.View{}, alertdomain.ErrProviderUnavailable
}
func (unavailableAlertPort) StartInvestigation(context.Context, alertdomain.StartInvestigationRequest) (alertdomain.View, error) {
	return alertdomain.View{}, alertdomain.ErrProviderUnavailable
}

type alertExpectedVersionBody struct {
	ExpectedVersion uint64 `json:"expected_version"`
	Reason          string `json:"reason"`
}

type alertSilenceBody struct {
	ExpectedVersion uint64 `json:"expected_version"`
	DurationSeconds int64  `json:"duration_seconds"`
	Reason          string `json:"reason"`
}

type alertIncidentLinkBody struct {
	ExpectedVersion uint64 `json:"expected_version"`
	IncidentID      string `json:"incident_id"`
	Create          bool   `json:"create"`
}

func (h *Handler) listAlerts(c *gin.Context) {
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeProblem(c, http.StatusBadRequest, "INVALID_QUERY", "Alert limit is invalid")
			return
		}
		limit = parsed
	}
	incidentID := strings.TrimSpace(c.Query("incident"))
	if incidentID != "" {
		parsed, parseErr := ParsePublicUUID(incidentID)
		if parseErr != nil {
			h.writeProblem(c, http.StatusBadRequest, "INVALID_QUERY", "Incident filter must be a public UUID")
			return
		}
		incidentID = parsed
	}
	page, err := h.alerts.List(c.Request.Context(), alertdomain.ListRequest{
		Cursor: c.Query("cursor"), Limit: limit, Status: strings.TrimSpace(c.Query("status")),
		Severity: strings.TrimSpace(c.Query("severity")), Namespace: strings.TrimSpace(c.Query("namespace")),
		Search: strings.TrimSpace(c.Query("search")), IncidentID: incidentID,
	})
	if err != nil {
		h.writeAlertError(c, err)
		return
	}
	if page.Items == nil {
		page.Items = []alertdomain.View{}
	}
	h.writeJSON(c, http.StatusOK, page)
}

func (h *Handler) getAlert(c *gin.Context) {
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	detail, err := h.alerts.Detail(c.Request.Context(), id)
	if err != nil {
		h.writeAlertError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, detail)
}

func (h *Handler) acknowledgeAlert(c *gin.Context) {
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	var body alertExpectedVersionBody
	if !h.decodeAlertBody(c, &body) {
		return
	}
	key, ok := h.alertIdempotency(c, id, "acknowledge")
	if !ok {
		return
	}
	view, err := h.alerts.Acknowledge(c.Request.Context(), alertdomain.AcknowledgeRequest{
		AlertID: id, ExpectedVersion: body.ExpectedVersion, IdempotencyKey: key, Reason: body.Reason,
		Actor: alertOwner(),
	})
	if err != nil {
		h.writeAlertError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, view)
}

func (h *Handler) createAlertSilence(c *gin.Context) {
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	var body alertSilenceBody
	if !h.decodeAlertBody(c, &body) {
		return
	}
	minimumSeconds := int64(alertdomain.MinimumSilenceDuration / time.Second)
	maximumSeconds := int64(alertdomain.MaximumSilenceDuration / time.Second)
	if body.DurationSeconds < minimumSeconds || body.DurationSeconds > maximumSeconds {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_REQUEST", "Alert silence duration must be between 300 and 86400 seconds")
		return
	}
	key, ok := h.alertIdempotency(c, id, "silence")
	if !ok {
		return
	}
	silence, err := h.alerts.CreateSilence(c.Request.Context(), alertdomain.CreateSilenceRequest{
		AlertID: id, ExpectedVersion: body.ExpectedVersion, IdempotencyKey: key,
		Duration: time.Duration(body.DurationSeconds) * time.Second, Reason: body.Reason, Actor: alertOwner(),
	})
	if err != nil {
		h.writeAlertError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, silence)
}

func (h *Handler) expireAlertSilence(c *gin.Context) {
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	var body struct {
		ExpectedVersion uint64 `json:"expected_version"`
	}
	if !h.decodeAlertBody(c, &body) {
		return
	}
	key, ok := h.alertIdempotency(c, id, "expire-silence")
	if !ok {
		return
	}
	silence, err := h.alerts.ExpireSilence(c.Request.Context(), alertdomain.ExpireSilenceRequest{
		SilenceID: id, ExpectedVersion: body.ExpectedVersion, IdempotencyKey: key, Actor: alertOwner(),
	})
	if err != nil {
		h.writeAlertError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, silence)
}

func (h *Handler) linkAlertIncident(c *gin.Context) {
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	var body alertIncidentLinkBody
	if !h.decodeAlertBody(c, &body) {
		return
	}
	key, ok := h.alertIdempotency(c, id, "incident-link")
	if !ok {
		return
	}
	view, err := h.alerts.LinkIncident(c.Request.Context(), alertdomain.LinkIncidentRequest{
		AlertID: id, ExpectedVersion: body.ExpectedVersion, IdempotencyKey: key,
		IncidentID: body.IncidentID, Create: body.Create, Actor: alertOwner(),
	})
	if err != nil {
		h.writeAlertError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, view)
}

func (h *Handler) startAlertInvestigation(c *gin.Context) {
	id, ok := h.publicID(c)
	if !ok {
		return
	}
	var body alertExpectedVersionBody
	if !h.decodeAlertBody(c, &body) {
		return
	}
	key, ok := h.alertIdempotency(c, id, "investigation")
	if !ok {
		return
	}
	view, err := h.alerts.StartInvestigation(c.Request.Context(), alertdomain.StartInvestigationRequest{
		AlertID: id, ExpectedVersion: body.ExpectedVersion, IdempotencyKey: key,
		Reason: body.Reason, Actor: alertOwner(),
	})
	if err != nil {
		h.writeAlertError(c, err)
		return
	}
	h.writeJSON(c, http.StatusAccepted, view)
}

func (h *Handler) decodeAlertBody(c *gin.Context, target any) bool {
	return h.decodeCommand(c, target)
}

func (h *Handler) alertIdempotency(c *gin.Context, resourceID, operation string) (string, bool) {
	raw := strings.TrimSpace(c.GetHeader(IdempotencyHeader))
	if raw == "" || len(raw) > 128 {
		h.writeProblem(c, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key must contain 1..128 characters")
		return "", false
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{"alert", resourceID, operation, raw}, "\x00")))
	return hex.EncodeToString(digest[:]), true
}

func alertOwner() alertdomain.Actor {
	return alertdomain.Actor{Provider: "local", Login: "owner", Role: "owner"}
}

func (h *Handler) writeAlertError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, alertdomain.ErrInvalid):
		h.writeProblem(c, http.StatusBadRequest, "INVALID_REQUEST", "Alert request violates the bounded command contract")
	case errors.Is(err, alertdomain.ErrNotFound):
		h.writeProblem(c, http.StatusNotFound, "RESOURCE_NOT_FOUND", "Alert resource was not found")
	case errors.Is(err, alertdomain.ErrStaleVersion):
		h.writeProblem(c, http.StatusConflict, "STALE_EXPECTATION", "Alert expected version is stale")
	case errors.Is(err, alertdomain.ErrConflict):
		h.writeProblem(c, http.StatusConflict, "COMMAND_CONFLICT", "Alert command conflicts with current lifecycle facets")
	case errors.Is(err, alertdomain.ErrProviderDisabled):
		h.writeProblem(c, http.StatusServiceUnavailable, "ALERTMANAGER_PROVIDER_DISABLED", "Alertmanager is disabled in the active Configuration Revision")
	case errors.Is(err, alertdomain.ErrProviderUnavailable), errors.Is(err, context.DeadlineExceeded):
		h.writeProblem(c, http.StatusServiceUnavailable, "ALERTMANAGER_PROVIDER_UNAVAILABLE", "Alertmanager or Alert command dependency is unavailable")
	default:
		h.writeProblem(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Alert operation failed")
	}
}
