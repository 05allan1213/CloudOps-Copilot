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

// CreateFromDiagnosis godoc
// @Summary      从诊断报告生成待审批动作
// @Description  根据诊断报告 recommended_actions 创建 PendingAction，只有 admin 可调用，不会执行真实动作
// @Tags         actions
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int  true  "诊断报告 ID"
// @Param        body  body  CreateFromDiagnosisRequest  false  "创建请求"
// @Success      200  {object}  map[string]interface{}
// @Failure      400  {object}  map[string]interface{}
// @Failure      403  {object}  map[string]interface{}
// @Failure      404  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /diagnosis/{id}/actions [post]
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

// ListPending godoc
// @Summary      获取待审批动作列表
// @Description  查询 pending 状态的动作审批项
// @Tags         actions
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页数量"
// @Success      200  {object}  map[string]interface{}
// @Failure      403  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /actions/pending [get]
func (h *Handler) ListPending(c *gin.Context) {
	filter := listFilterFromQuery(c)
	filter.Status = "pending"
	h.listActions(c, filter)
}

// ListActions godoc
// @Summary      获取动作列表
// @Description  按状态、风险等级、动作类型查询 PendingAction
// @Tags         actions
// @Produce      json
// @Security     BearerAuth
// @Param        status       query  string  false  "状态"
// @Param        risk_level   query  string  false  "风险等级"
// @Param        action_type  query  string  false  "动作类型"
// @Param        page         query  int     false  "页码"
// @Param        page_size    query  int     false  "每页数量"
// @Success      200  {object}  map[string]interface{}
// @Failure      403  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /actions [get]
func (h *Handler) ListActions(c *gin.Context) {
	h.listActions(c, listFilterFromQuery(c))
}

// GetAction godoc
// @Summary      获取动作详情
// @Description  查询单个 PendingAction 的参数、状态、审批和执行结果
// @Tags         actions
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "动作 ID"
// @Success      200  {object}  map[string]interface{}
// @Failure      403  {object}  map[string]interface{}
// @Failure      404  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /actions/{id} [get]
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

// Approve godoc
// @Summary      审批通过动作
// @Description  将 pending 动作审批为 approved，只有 admin 可调用
// @Tags         actions
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int  true  "动作 ID"
// @Param        body  body  ApproveRequest  false  "审批备注"
// @Success      200  {object}  map[string]interface{}
// @Failure      400  {object}  map[string]interface{}
// @Failure      403  {object}  map[string]interface{}
// @Failure      404  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /actions/{id}/approve [post]
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

// Reject godoc
// @Summary      拒绝动作
// @Description  将 pending 动作拒绝为 rejected，拒绝原因必填
// @Tags         actions
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int  true  "动作 ID"
// @Param        body  body  RejectRequest  true  "拒绝原因"
// @Success      200  {object}  map[string]interface{}
// @Failure      400  {object}  map[string]interface{}
// @Failure      403  {object}  map[string]interface{}
// @Failure      404  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /actions/{id}/reject [post]
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

// Execute godoc
// @Summary      执行动作
// @Description  执行 approved 动作；Phase 6 默认 disabled executor，会记录失败审计
// @Tags         actions
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int  true  "动作 ID"
// @Param        body  body  ExecuteRequest  true  "执行确认"
// @Success      200  {object}  map[string]interface{}
// @Failure      400  {object}  map[string]interface{}
// @Failure      403  {object}  map[string]interface{}
// @Failure      404  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /actions/{id}/execute [post]
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

// ListAuditLogs godoc
// @Summary      获取审计日志列表
// @Description  查询动作审批、拒绝、执行、失败和权限拒绝审计日志
// @Tags         audit
// @Produce      json
// @Security     BearerAuth
// @Param        action     query  string  false  "审计动作"
// @Param        result     query  string  false  "结果"
// @Param        actor      query  string  false  "操作者"
// @Param        page       query  int     false  "页码"
// @Param        page_size  query  int     false  "每页数量"
// @Success      200  {object}  map[string]interface{}
// @Failure      403  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /audit-logs [get]
func (h *Handler) ListAuditLogs(c *gin.Context) {
	logs, total, filter, err := h.service.ListAuditLogs(c.Request.Context(), listFilterFromQuery(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writeSuccess(c, gin.H{"items": logs, "total": total, "page": filter.Page, "page_size": filter.PageSize})
}

// GetAuditLog godoc
// @Summary      获取审计日志详情
// @Description  查询单条审计日志详情
// @Tags         audit
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "审计日志 ID"
// @Success      200  {object}  map[string]interface{}
// @Failure      403  {object}  map[string]interface{}
// @Failure      404  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /audit-logs/{id} [get]
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
