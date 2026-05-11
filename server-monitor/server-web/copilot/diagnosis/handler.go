package diagnosis

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"server-web/api/middleware"
)

type Handler struct {
	service *Service
}

type apiResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Trigger(c *gin.Context) {
	var req Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apiResponse{Status: "error", Error: "invalid diagnosis request"})
		return
	}
	result, err := h.service.Trigger(c.Request.Context(), currentUser(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, apiResponse{Status: "success", Data: result})
}

func (h *Handler) List(c *gin.Context) {
	filter, ok := parseListFilter(c)
	if !ok {
		return
	}
	result, err := h.service.List(c.Request.Context(), currentUser(c), filter)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, apiResponse{Status: "success", Data: result})
}

func (h *Handler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, apiResponse{Status: "error", Error: "invalid diagnosis id"})
		return
	}
	result, err := h.service.Get(c.Request.Context(), currentUser(c), id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, apiResponse{Status: "success", Data: result})
}

func parseListFilter(c *gin.Context) (ListFilter, bool) {
	filter := ListFilter{
		Status:      strings.TrimSpace(c.Query("status")),
		TriggerType: strings.TrimSpace(c.Query("trigger_type")),
	}
	var err error
	filter.Page, err = parseIntQuery(c.Query("page"), defaultPage)
	if err != nil {
		c.JSON(http.StatusBadRequest, apiResponse{Status: "error", Error: "invalid page"})
		return ListFilter{}, false
	}
	filter.PageSize, err = parseIntQuery(c.Query("page_size"), defaultPageSize)
	if err != nil {
		c.JSON(http.StatusBadRequest, apiResponse{Status: "error", Error: "invalid page_size"})
		return ListFilter{}, false
	}
	normalized, err := NormalizeListFilter(filter)
	if err != nil {
		c.JSON(http.StatusBadRequest, apiResponse{Status: "error", Error: err.Error()})
		return ListFilter{}, false
	}
	return normalized, true
}

func parseIntQuery(raw string, defaultValue int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, strconv.ErrSyntax
	}
	return value, nil
}

func currentUser(c *gin.Context) User {
	userID, _ := c.Get(middleware.ContextUserID)
	username, _ := c.Get(middleware.ContextUsername)
	role, _ := c.Get(middleware.ContextRole)

	user := User{}
	if id, ok := userID.(uint64); ok {
		user.ID = id
	}
	if value, ok := username.(string); ok {
		user.Username = value
	}
	if value, ok := role.(string); ok {
		user.Role = value
	}
	return user
}

func writeError(c *gin.Context, err error) {
	var conflict ConflictError
	switch {
	case errors.Is(err, ErrInvalidRequest):
		c.JSON(http.StatusBadRequest, apiResponse{Status: "error", Error: err.Error()})
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, apiResponse{Status: "error", Error: "diagnosis target not found"})
	case errors.As(err, &conflict):
		c.JSON(http.StatusConflict, apiResponse{
			Status: "error",
			Error:  "diagnosis target is ambiguous",
			Data: gin.H{
				"candidates": conflict.Candidates,
			},
		})
	case errors.Is(err, ErrForbidden):
		c.JSON(http.StatusForbidden, apiResponse{Status: "error", Error: "diagnosis report forbidden"})
	case errors.Is(err, ErrUnavailable):
		c.JSON(http.StatusServiceUnavailable, apiResponse{Status: "error", Error: "diagnosis service unavailable"})
	default:
		c.JSON(http.StatusInternalServerError, apiResponse{Status: "error", Error: "diagnosis failed"})
	}
}
