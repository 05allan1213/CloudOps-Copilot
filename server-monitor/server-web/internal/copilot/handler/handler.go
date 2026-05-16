package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	copilot "server-web/internal/copilot/service"
	"server-web/internal/copilot/session"
	"server-web/internal/copilot/tool"
	"server-web/internal/middleware"
)

type Handler struct {
	service *copilot.Service
}

type response struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

type toolSchemasResponse struct {
	Status string          `json:"status"`
	Data   []toolSchemaDoc `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type toolSchemaDoc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  []toolParamDoc `json:"parameters"`
	RiskLevel   string         `json:"risk_level"`
	ReadOnly    bool           `json:"read_only"`
	Timeout     string         `json:"timeout"`
}

type toolParamDoc struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Required    bool        `json:"required"`
	Description string      `json:"description,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Min         *float64    `json:"min,omitempty"`
	Max         *float64    `json:"max,omitempty"`
	Pattern     string      `json:"pattern,omitempty"`
}

func NewHandler(service *copilot.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Chat(c *gin.Context) {
	var req copilot.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  "无效的 Copilot 聊天请求",
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

func (h *Handler) GetSession(c *gin.Context) {
	session, err := h.service.GetSession(c.Request.Context(), currentUser(c), c.Param("id"))
	if err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   session,
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

// ListTools godoc
// @Summary      获取 Copilot 工具 Schema
// @Description  返回当前启用的只读工具；K8s 开启后包含 k8s.get_pods、k8s.get_deployments、k8s.get_services、k8s.get_nodes、k8s.get_events、k8s.get_logs。
// @Tags         copilot
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  toolSchemasResponse
// @Failure      403  {object}  response
// @Failure      500  {object}  response
// @Router       /copilot/tools [get]
func (h *Handler) ListTools(c *gin.Context) {
	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   h.service.ToolSchemas(),
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
	case errors.Is(err, tool.ErrToolUnavailable):
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: "Copilot 服务不可用"})
	}
}
