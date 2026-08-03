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
	"github.com/05allan1213/CloudOps-Copilot/internal/notification"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/gin-gonic/gin"
)

type SettingsPort interface {
	Bootstrap(context.Context) (settings.BootstrapSnapshot, error)
	Scopes(context.Context) ([]settings.OperationalScope, error)
	ActivateScope(context.Context, string) (settings.OperationalScope, error)
	Settings(context.Context) (settings.SettingsSnapshot, error)
	Validate(context.Context, settings.Draft) (settings.Validation, error)
	Apply(context.Context, string, settings.Draft, settings.RevisionExpectation) (settings.Revision, error)
	Revisions(context.Context, int) ([]settings.Revision, error)
	WriteSecret(context.Context, settings.SecretInput) (settings.SecretVersion, error)
	TestProvider(context.Context, settings.ProviderConfiguration, []settings.SecretReference, string) (settings.ProviderResult, error)
	StorageStatus(context.Context) (settings.StorageStatus, error)
}

type NotificationPort interface {
	List(context.Context, string, int) (notification.Page, error)
	Events(context.Context, string, int) ([]notification.Item, error)
	MarkRead(context.Context, string) error
	MarkAllRead(context.Context, string) (int64, error)
}

type overviewResponse struct {
	Bootstrap           settings.BootstrapSnapshot      `json:"bootstrap"`
	UnreadNotifications int                             `json:"unread_notifications"`
	Atlas               infrastructure.TopologySnapshot `json:"atlas"`
}

type scopePage struct {
	Items []settings.OperationalScope `json:"items"`
}

type revisionPage struct {
	Items []settings.Revision `json:"items"`
}

type applyConfigurationRequest struct {
	ValidationID               string         `json:"validation_id"`
	ExpectedActiveRevisionID   string         `json:"expected_active_revision_id"`
	ExpectedActiveRevisionHash string         `json:"expected_active_revision_hash"`
	Draft                      settings.Draft `json:"draft"`
}

type providerTestRequest struct {
	Configuration    settings.ProviderConfiguration `json:"configuration"`
	SecretReferences []settings.SecretReference     `json:"secret_references"`
	ClusterID        string                         `json:"cluster_id"`
}

type readAllNotificationsRequest struct {
	Cursor string `json:"cursor"`
}

type readAllNotificationsResponse struct {
	Updated int64 `json:"updated"`
}

func (h *Handler) getBootstrap(c *gin.Context) {
	if !h.requireSettings(c) {
		return
	}
	value, err := h.settings.Bootstrap(c.Request.Context())
	if err != nil {
		h.writeSettingsError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) listScopes(c *gin.Context) {
	if !h.requireSettings(c) {
		return
	}
	items, err := h.settings.Scopes(c.Request.Context())
	if err != nil {
		h.writeSettingsError(c, err)
		return
	}
	if items == nil {
		items = []settings.OperationalScope{}
	}
	h.writeJSON(c, http.StatusOK, scopePage{Items: items})
}

func (h *Handler) activateScope(c *gin.Context) {
	if !h.requireSettings(c) {
		return
	}
	id, err := ParsePublicUUID(c.Param("id"))
	if err != nil {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_PUBLIC_ID", "id must be a public UUID")
		return
	}
	value, err := h.settings.ActivateScope(c.Request.Context(), id)
	if err != nil {
		h.writeSettingsError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) getOverview(c *gin.Context) {
	if !h.requireSettings(c) || !h.requireNotifications(c) {
		return
	}
	query, ok := h.infrastructureQuery(c)
	if !ok {
		return
	}
	atlas, err := h.infrastructure.Topology(c.Request.Context(), query)
	if err != nil {
		h.writeInfrastructureError(c, err)
		return
	}
	bootstrap, err := h.settings.Bootstrap(c.Request.Context())
	if err != nil {
		h.writeSettingsError(c, err)
		return
	}
	page, err := h.notifications.List(c.Request.Context(), "", 1)
	if err != nil {
		h.writeNotificationError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, overviewResponse{Bootstrap: bootstrap, UnreadNotifications: page.UnreadCount, Atlas: atlas})
}

func (h *Handler) getSettings(c *gin.Context) {
	if !h.requireSettings(c) {
		return
	}
	value, err := h.settings.Settings(c.Request.Context())
	if err != nil {
		h.writeSettingsError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) validateSettings(c *gin.Context) {
	if !h.requireSettings(c) {
		return
	}
	var draft settings.Draft
	if !h.decodeCommand(c, &draft) {
		return
	}
	value, err := h.settings.Validate(c.Request.Context(), draft)
	if err != nil {
		h.writeSettingsError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) listConfigurationRevisions(c *gin.Context) {
	if !h.requireSettings(c) {
		return
	}
	limit, ok := h.platformLimit(c)
	if !ok {
		return
	}
	items, err := h.settings.Revisions(c.Request.Context(), limit)
	if err != nil {
		h.writeSettingsError(c, err)
		return
	}
	if items == nil {
		items = []settings.Revision{}
	}
	h.writeJSON(c, http.StatusOK, revisionPage{Items: items})
}

func (h *Handler) createConfigurationRevision(c *gin.Context) {
	if !h.requireSettings(c) {
		return
	}
	var request applyConfigurationRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	request.ValidationID = strings.TrimSpace(request.ValidationID)
	request.ExpectedActiveRevisionID = strings.TrimSpace(request.ExpectedActiveRevisionID)
	request.ExpectedActiveRevisionHash = strings.TrimSpace(request.ExpectedActiveRevisionHash)
	if request.ValidationID == "" || request.ExpectedActiveRevisionID == "" || request.ExpectedActiveRevisionHash == "" {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_CONFIGURATION", "validation_id and expected active revision identity are required")
		return
	}
	value, err := h.settings.Apply(c.Request.Context(), request.ValidationID, request.Draft, settings.RevisionExpectation{
		ID: request.ExpectedActiveRevisionID, Hash: request.ExpectedActiveRevisionHash,
	})
	if err != nil {
		h.writeSettingsError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, value)
}

func (h *Handler) createSecret(c *gin.Context) {
	if !h.requireSettings(c) {
		return
	}
	var request settings.SecretInput
	if !h.decodeCommand(c, &request) {
		return
	}
	value, err := h.settings.WriteSecret(c.Request.Context(), request)
	request.Value = ""
	if err != nil {
		h.writeSettingsError(c, err)
		return
	}
	h.writeJSON(c, http.StatusCreated, value)
}

func (h *Handler) testProvider(c *gin.Context) {
	if !h.requireSettings(c) {
		return
	}
	provider := settings.Provider(strings.ToLower(strings.TrimSpace(c.Param("provider"))))
	if !provider.Operational() {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_PROVIDER", "provider path identity is invalid")
		return
	}
	var request providerTestRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	if request.Configuration.Provider != provider {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_PROVIDER", "provider path and configuration identity must match")
		return
	}
	value, err := h.settings.TestProvider(c.Request.Context(), request.Configuration, request.SecretReferences, strings.TrimSpace(request.ClusterID))
	if err != nil {
		h.writeSettingsError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) getStorageStatus(c *gin.Context) {
	if !h.requireSettings(c) {
		return
	}
	value, err := h.settings.StorageStatus(c.Request.Context())
	if err != nil {
		h.writeSettingsError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) listNotifications(c *gin.Context) {
	if !h.requireNotifications(c) {
		return
	}
	limit, ok := h.platformLimit(c)
	if !ok {
		return
	}
	value, err := h.notifications.List(c.Request.Context(), strings.TrimSpace(c.Query("cursor")), limit)
	if err != nil {
		h.writeNotificationError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, value)
}

func (h *Handler) readNotification(c *gin.Context) {
	if !h.requireNotifications(c) {
		return
	}
	id, err := ParsePublicUUID(c.Param("id"))
	if err != nil {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_PUBLIC_ID", "id must be a public UUID")
		return
	}
	if err := h.notifications.MarkRead(c.Request.Context(), id); err != nil {
		h.writeNotificationError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) readAllNotifications(c *gin.Context) {
	if !h.requireNotifications(c) {
		return
	}
	var request readAllNotificationsRequest
	if !h.decodeCommand(c, &request) {
		return
	}
	updated, err := h.notifications.MarkAllRead(c.Request.Context(), strings.TrimSpace(request.Cursor))
	if err != nil {
		h.writeNotificationError(c, err)
		return
	}
	h.writeJSON(c, http.StatusOK, readAllNotificationsResponse{Updated: updated})
}

func (h *Handler) streamNotificationEvents(c *gin.Context) {
	if !h.requireNotifications(c) {
		return
	}
	lastEventID := strings.TrimSpace(c.GetHeader("Last-Event-ID"))
	if len(lastEventID) > maxCursorBytes || containsControl(lastEventID) {
		h.writeProblem(c, http.StatusBadRequest, "INVALID_EVENT_CURSOR", "Last-Event-ID is invalid")
		return
	}
	items, err := h.notifications.Events(c.Request.Context(), lastEventID, 100)
	if err != nil {
		h.writeNotificationError(c, err)
		return
	}
	c.Header("Content-Type", SSEMediaType)
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString("retry: 3000\n\n")
	lastEventID = writeNotificationEvents(c, items, lastEventID)
	c.Writer.Flush()

	poll := time.NewTicker(time.Second)
	heartbeat := time.NewTicker(10 * time.Second)
	deadline := time.NewTimer(25 * time.Second)
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
			items, err = h.notifications.Events(c.Request.Context(), lastEventID, 100)
			if err != nil {
				return
			}
			lastEventID = writeNotificationEvents(c, items, lastEventID)
			if len(items) > 0 {
				c.Writer.Flush()
			}
		}
	}
}

func writeNotificationEvents(c *gin.Context, items []notification.Item, lastEventID string) string {
	for _, item := range items {
		payload, err := json.Marshal(item)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(c.Writer, "id: %s\nevent: owner_notification.created\ndata: %s\n\n", item.ID, payload)
		lastEventID = item.ID
	}
	return lastEventID
}

func (h *Handler) platformLimit(c *gin.Context) (int, bool) {
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

func (h *Handler) requireSettings(c *gin.Context) bool {
	if h.settings != nil {
		return true
	}
	h.writeProblem(c, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Settings capability is not wired")
	return false
}

func (h *Handler) requireNotifications(c *gin.Context) bool {
	if h.notifications != nil {
		return true
	}
	h.writeProblem(c, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Notification capability is not wired")
	return false
}

func (h *Handler) writeSettingsError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, settings.ErrValidationStale):
		h.writeProblem(c, http.StatusConflict, "STALE_VALIDATION", "validation_id does not match the submitted draft")
	case errors.Is(err, settings.ErrValidationExpired):
		h.writeProblem(c, http.StatusConflict, "VALIDATION_EXPIRED", "configuration validation has expired")
	case errors.Is(err, settings.ErrValidationFailed):
		h.writeProblem(c, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "configuration validation did not pass")
	case errors.Is(err, settings.ErrRevisionChanged):
		h.writeProblem(c, http.StatusConflict, "CONFIGURATION_REVISION_CHANGED", "active Configuration Revision no longer matches the expected identity")
	case errors.Is(err, settings.ErrInvalidDraft):
		h.writeProblem(c, http.StatusBadRequest, "INVALID_CONFIGURATION", "operational configuration is invalid")
	case errors.Is(err, settings.ErrNotFound):
		h.writeProblem(c, http.StatusNotFound, "SETTINGS_RESOURCE_NOT_FOUND", "settings resource was not found")
	case errors.Is(err, settings.ErrUnavailable):
		h.writeProblem(c, http.StatusServiceUnavailable, "SCOPE_PROVIDER_UNAVAILABLE", "selected Operational Scope is not served by an available Provider connection")
	default:
		h.writeProblem(c, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "durable Settings state is unavailable")
	}
}

func (h *Handler) writeNotificationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, notification.ErrInvalid):
		h.writeProblem(c, http.StatusBadRequest, "INVALID_QUERY", "notification cursor or request is invalid")
	case errors.Is(err, notification.ErrNotFound):
		h.writeProblem(c, http.StatusNotFound, "NOTIFICATION_NOT_FOUND", "Owner Notification was not found")
	default:
		h.writeProblem(c, http.StatusServiceUnavailable, "NOTIFICATIONS_UNAVAILABLE", "durable Notification Inbox is unavailable")
	}
}
