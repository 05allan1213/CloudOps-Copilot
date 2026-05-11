package action

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"server-web/api/middleware"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) CreateFromDiagnosis(c *gin.Context) {
	id, err := parseUintID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid diagnosis id")
		return
	}
	var req CreateFromDiagnosisRequest
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	result, err := h.service.CreateFromDiagnosis(c.Request.Context(), id, req, actorFromGin(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, result)
}

func (h *Handler) ListPending(c *gin.Context) {
	filter := listFilterFromQuery(c)
	filter.Status = "pending"
	h.listActions(c, filter)
}

func (h *Handler) ListActions(c *gin.Context) {
	h.listActions(c, listFilterFromQuery(c))
}

func (h *Handler) GetAction(c *gin.Context) {
	id, err := parseUintID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid action id")
		return
	}
	action, err := h.service.GetAction(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, action)
}

func (h *Handler) Approve(c *gin.Context) {
	id, err := parseUintID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid action id")
		return
	}
	var req ApproveRequest
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	action, err := h.service.Approve(c.Request.Context(), id, req, actorFromGin(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, action)
}

func (h *Handler) Reject(c *gin.Context) {
	id, err := parseUintID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid action id")
		return
	}
	var req RejectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	action, err := h.service.Reject(c.Request.Context(), id, req, actorFromGin(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, action)
}

func (h *Handler) Execute(c *gin.Context) {
	id, err := parseUintID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid action id")
		return
	}
	var req ExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.Confirm {
		writeError(c, http.StatusBadRequest, "confirm must be true")
		return
	}
	action, err := h.service.Execute(c.Request.Context(), id, actorFromGin(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "action execution failed; see audit log for details", "data": action})
		return
	}
	writeSuccess(c, action)
}

func (h *Handler) ListAuditLogs(c *gin.Context) {
	logs, total, filter, err := h.service.ListAuditLogs(c.Request.Context(), listFilterFromQuery(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, gin.H{"items": logs, "total": total, "page": filter.Page, "page_size": filter.PageSize})
}

func (h *Handler) GetAuditLog(c *gin.Context) {
	id, err := parseUintID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid audit log id")
		return
	}
	log, err := h.service.GetAuditLog(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, log)
}

func (h *Handler) listActions(c *gin.Context, filter ListFilter) {
	actions, total, filter, err := h.service.ListActions(c.Request.Context(), filter)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, gin.H{"items": actions, "total": total, "page": filter.Page, "page_size": filter.PageSize})
}

func listFilterFromQuery(c *gin.Context) ListFilter {
	return ListFilter{
		Status:     c.Query("status"),
		RiskLevel:  c.Query("risk_level"),
		ActionType: firstNonEmpty(c.Query("action_type"), c.Query("action")),
		Actor:      c.Query("actor"),
		Result:     c.Query("result"),
		Page:       intQuery(c, "page"),
		PageSize:   intQuery(c, "page_size"),
	}
}

func actorFromGin(c *gin.Context) Actor {
	actor := Actor{}
	if value, ok := c.Get(middleware.ContextUserID); ok {
		if id, ok := value.(uint64); ok {
			actor.ID = id
		}
	}
	if value, ok := c.Get(middleware.ContextUsername); ok {
		if username, ok := value.(string); ok {
			actor.Username = username
		}
	}
	if value, ok := c.Get(middleware.ContextRole); ok {
		if role, ok := value.(string); ok {
			actor.Role = role
		}
	}
	if actor.Role == "" {
		actor.Role = "viewer"
	}
	return actor
}

func intQuery(c *gin.Context, key string) int {
	value := c.Query(key)
	if value == "" {
		return 0
	}
	var result int
	if _, err := fmt.Sscanf(value, "%d", &result); err != nil {
		return 0
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		writeError(c, http.StatusForbidden, "insufficient permissions")
	case errors.Is(err, ErrNotFound):
		writeError(c, http.StatusNotFound, "not found")
	case errors.Is(err, ErrInvalidAction):
		writeError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "action service unavailable")
	default:
		writeError(c, http.StatusInternalServerError, "internal server error")
	}
}

func writeSuccess(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": data})
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"status": "error", "error": message})
}
