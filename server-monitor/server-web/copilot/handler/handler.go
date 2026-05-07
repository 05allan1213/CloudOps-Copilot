package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"server-web/api/middleware"
	copilot "server-web/copilot/service"
	"server-web/copilot/session"
)

type Handler struct {
	service *copilot.Service
}

type response struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

func NewHandler(service *copilot.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Chat(c *gin.Context) {
	var req copilot.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  "invalid copilot chat request",
		})
		return
	}

	result, err := h.service.Chat(c.Request.Context(), currentUser(c), req)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *Handler) ListSessions(c *gin.Context) {
	sessions, err := h.service.ListSessions(c.Request.Context(), currentUser(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   sessions,
	})
}

func (h *Handler) ListMessages(c *gin.Context) {
	messages, err := h.service.ListMessages(c.Request.Context(), currentUser(c), c.Param("id"))
	if err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   messages,
	})
}

func (h *Handler) DeleteSession(c *gin.Context) {
	if err := h.service.DeleteSession(c.Request.Context(), currentUser(c), c.Param("id")); err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data: gin.H{
			"deleted": true,
		},
	})
}

func currentUser(c *gin.Context) copilot.User {
	userID, _ := c.Get(middleware.ContextUserID)
	username, _ := c.Get(middleware.ContextUsername)
	role, _ := c.Get(middleware.ContextRole)

	user := copilot.User{}
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

func writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, copilot.ErrMessageRequired), errors.Is(err, copilot.ErrMessageTooLong), errors.Is(err, copilot.ErrSessionRequired):
		c.JSON(http.StatusBadRequest, response{Status: "error", Error: err.Error()})
	case errors.Is(err, copilot.ErrSessionNotFound):
		c.JSON(http.StatusNotFound, response{Status: "error", Error: err.Error()})
	case errors.Is(err, copilot.ErrSessionForbidden):
		c.JSON(http.StatusForbidden, response{Status: "error", Error: err.Error()})
	case errors.Is(err, session.ErrUnavailable):
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "copilot service unavailable"})
	}
}
