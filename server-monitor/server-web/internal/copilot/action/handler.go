package action

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"server-web/internal/middleware"
)

type Handler struct {
	service *Service
}

type actionResponseEnvelope struct {
	Status string    `json:"status"`
	Data   actionDoc `json:"data,omitempty"`
	Error  string    `json:"error,omitempty"`
}

type actionListResponseEnvelope struct {
	Status string           `json:"status"`
	Data   actionListResult `json:"data,omitempty"`
	Error  string           `json:"error,omitempty"`
}

type actionListResult struct {
	Items    []actionDoc `json:"items"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

type actionDoc struct {
	ID                uint64       `json:"id"`
	DiagnosisReportID uint64       `json:"diagnosis_report_id"`
	ActionType        string       `json:"action_type"`
	TargetKind        string       `json:"target_kind"`
	TargetName        string       `json:"target_name"`
	Namespace         string       `json:"namespace"`
	Params            interface{}  `json:"params,omitempty"`
	RiskLevel         string       `json:"risk_level"`
	Status            string       `json:"status"`
	RequestedBy       string       `json:"requested_by"`
	ApprovedBy        uint64       `json:"approved_by,omitempty"`
	ExecutedBy        uint64       `json:"executed_by,omitempty"`
	Result            ActionResult `json:"result,omitempty"`
	ErrorMessage      string       `json:"error_message,omitempty"`
	CreatedAt         string       `json:"created_at"`
	ApprovedAt        string       `json:"approved_at,omitempty"`
	ExecutedAt        string       `json:"executed_at,omitempty"`
	UpdatedAt         string       `json:"updated_at"`
}

type auditListResponseEnvelope struct {
	Status string          `json:"status"`
	Data   auditListResult `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type auditListResult struct {
	Items    []auditLogDoc `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

type auditLogDoc struct {
	ID           uint64      `json:"id"`
	Actor        string      `json:"actor"`
	ActorRole    string      `json:"actor_role"`
	Action       string      `json:"action"`
	ResourceType string      `json:"resource_type"`
	ResourceID   string      `json:"resource_id"`
	Request      interface{} `json:"request,omitempty"`
	Result       string      `json:"result"`
	ErrorMessage string      `json:"error_message,omitempty"`
	TraceID      string      `json:"trace_id,omitempty"`
	CreatedAt    string      `json:"created_at"`
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
		writeError(c, http.StatusBadRequest, "无效的诊断 ID")
		return
	}
	var req CreateFromDiagnosisRequest
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, "无效的请求体")
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
		writeError(c, http.StatusBadRequest, "无效的动作 ID")
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
		writeError(c, http.StatusBadRequest, "无效的动作 ID")
		return
	}
	var req ApproveRequest
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, "无效的请求体")
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
		writeError(c, http.StatusBadRequest, "无效的动作 ID")
		return
	}
	var req RejectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "无效的请求体")
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
// @Description  执行 approved 动作；K8s restart/scale 只有在 ACTION_EXECUTION_ENABLED 和 K8S_WRITE_ENABLED 同时开启时使用真实执行器，结果包含 target、old/new replicas、ready replicas 和 restart annotation 摘要。
// @Tags         actions
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int  true  "动作 ID"
// @Param        body  body  ExecuteRequest  true  "执行确认"
// @Success      200  {object}  actionResponseEnvelope
// @Failure      400  {object}  map[string]interface{}
// @Failure      403  {object}  map[string]interface{}
// @Failure      404  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /actions/{id}/execute [post]
func (h *Handler) Execute(c *gin.Context) {
	id, err := parseUintID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, "无效的动作 ID")
		return
	}
	var req ExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "无效的请求体")
		return
	}
	if !req.Confirm {
		writeError(c, http.StatusBadRequest, "confirm 必须为 true")
		return
	}
	action, err := h.service.Execute(c.Request.Context(), id, actorFromGin(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "动作执行失败，详情请查看审计日志", "data": action})
		return
	}
	writeSuccess(c, action)
}

// ListAuditLogs godoc
// @Summary      获取审计日志列表
// @Description  查询动作审批、拒绝、执行、失败和权限拒绝审计日志；action/action_type 支持 action.execute 等审计动作，也支持 k8s.restart_deployment、k8s.scale_deployment 过滤 K8s 动作类型。
// @Tags         audit
// @Produce      json
// @Security     BearerAuth
// @Param        action     query  string  false  "审计动作或 K8s 动作类型"
// @Param        action_type query string  false  "审计动作或 K8s 动作类型"
// @Param        result     query  string  false  "结果"
// @Param        actor      query  string  false  "操作者"
// @Param        page       query  int     false  "页码"
// @Param        page_size  query  int     false  "每页数量"
// @Success      200  {object}  auditListResponseEnvelope
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
		writeError(c, http.StatusBadRequest, "无效的审计日志 ID")
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
		writeError(c, http.StatusForbidden, "权限不足")
	case errors.Is(err, ErrNotFound):
		writeError(c, http.StatusNotFound, "未找到")
	case errors.Is(err, ErrInvalidAction):
		writeError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "动作服务不可用")
	default:
		writeError(c, http.StatusInternalServerError, "内部服务器错误")
	}
}

func writeSuccess(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": data})
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"status": "error", "error": message})
}
