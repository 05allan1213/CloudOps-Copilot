package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	appalert "server-web/alert"
	authpkg "server-web/auth"
	appcache "server-web/cache"
	apphost "server-web/host"
	eventbus "server-web/kafka"
	promclient "server-web/prometheus"
	"server-web/webhook"
	ws "server-web/websocket"
)

type AuthService interface {
	Login(ctx context.Context, username string, password string) (authpkg.LoginResult, error)
	AuthenticateBearer(authHeader string) (authpkg.Identity, error)
	AuthenticateToken(token string) (authpkg.Identity, error)
	Register(ctx context.Context, username, password, role string) (authpkg.Identity, error)
	ListUsers(ctx context.Context) ([]authpkg.Identity, error)
	DeleteUser(ctx context.Context, id uint64) error
	VerifyTokenVersion(ctx context.Context, identity authpkg.Identity) error
}

type cacheClient interface {
	Enabled() bool
	Ping(ctx context.Context) error
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	HSet(ctx context.Context, key, field string, value []byte) error
	HDel(ctx context.Context, key, field string) error
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	AddAlertEventOnce(ctx context.Context, streamKey, dedupeKey string, maxLen int64, value, dedupeValue []byte, ttl time.Duration) (bool, error)
	XRevRangeN(ctx context.Context, key string, count int64) ([]string, error)
	Publish(ctx context.Context, channel string, message []byte) error
}

type mysqlClient interface {
	Enabled() bool
	Ping(ctx context.Context) error
}

type alertProducer interface {
	SendAlertEvent(eventbus.AlertEvent) error
}

type Handler struct {
	promClient     *promclient.Client
	db             *gorm.DB
	cacheClient    cacheClient
	cacheService   *appcache.Service
	alertService   *appalert.Service
	hostService    *apphost.Service
	mysqlClient    mysqlClient
	authService    AuthService
	readyTimeout   time.Duration
	requestTimeout time.Duration
	cacheTimeout   time.Duration
	ruleSync       AlertRuleSyncConfig
	websocketHub   *ws.Hub
}

type response struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

var validAlertEventStatuses = map[string]struct{}{
	"firing":   {},
	"resolved": {},
}

var validAlertEventSeverities = map[string]struct{}{
	"critical": {},
	"warning":  {},
	"info":     {},
}

type Config struct {
	ReadyTimeout   time.Duration
	RequestTimeout time.Duration
	HostsTTL       time.Duration
	DashboardTTL   time.Duration
	DedupeTTL      time.Duration
	CacheTimeout   time.Duration
	RuleSync       AlertRuleSyncConfig
	AlertProducer  alertProducer
	MySQLClient    mysqlClient
	DB             *gorm.DB
	AuthService    AuthService
}

func NewHandler(promClient *promclient.Client, cacheClient cacheClient, cfg Config, websocketHub *ws.Hub) (*Handler, error) {
	if promClient == nil {
		return nil, errors.New("prometheus client is required")
	}
	cacheService := appcache.NewService(cacheClient, appcache.Options{
		HostsTTL:     cfg.HostsTTL,
		DashboardTTL: cfg.DashboardTTL,
	})
	return &Handler{
		promClient:   promClient,
		db:           cfg.DB,
		cacheClient:  cacheClient,
		cacheService: cacheService,
		alertService: appalert.NewService(cacheClient, appalert.Options{
			DedupeTTL: cfg.DedupeTTL,
			DB:        cfg.DB,
			Producer:  cfg.AlertProducer,
		}),
		hostService: apphost.NewService(promClient, cacheService, apphost.Options{
			RequestTimeout: cfg.RequestTimeout,
			CacheTimeout:   cfg.CacheTimeout,
		}),
		mysqlClient:    cfg.MySQLClient,
		authService:    cfg.AuthService,
		readyTimeout:   cfg.ReadyTimeout,
		requestTimeout: cfg.RequestTimeout,
		cacheTimeout:   cfg.CacheTimeout,
		ruleSync:       cfg.RuleSync,
		websocketHub:   websocketHub,
	}, nil
}

func (h *Handler) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, response{
		Status: "success",
		Data: gin.H{
			"healthy": true,
		},
	})
}

func (h *Handler) Readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.readyTimeout)
	defer cancel()

	dependencies := gin.H{
		"prometheus": "ok",
		"redis":      "disabled",
	}

	var errors []string

	if err := h.promClient.Ready(ctx); err != nil {
		dependencies["prometheus"] = "unreachable"
		errors = append(errors, err.Error())
	}

	if h.cacheClient != nil && h.cacheClient.Enabled() {
		if err := h.cacheClient.Ping(ctx); err != nil {
			dependencies["redis"] = "unreachable"
			errors = append(errors, err.Error())
		} else {
			dependencies["redis"] = "ok"
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  fmt.Sprintf("readiness check failed: %s", strings.Join(errors, "; ")),
			Data: gin.H{
				"ready":        false,
				"dependencies": dependencies,
			},
		})
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data: gin.H{
			"ready":        true,
			"dependencies": dependencies,
		},
	})
}

func (h *Handler) ReadyzFull(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.readyTimeout)
	defer cancel()

	dependencies := gin.H{
		"prometheus": "ok",
		"redis":      "disabled",
		"mysql":      "disabled",
	}

	var errors []string

	if err := h.promClient.Ready(ctx); err != nil {
		dependencies["prometheus"] = "unreachable"
		errors = append(errors, err.Error())
	}

	if h.cacheClient != nil && h.cacheClient.Enabled() {
		if err := h.cacheClient.Ping(ctx); err != nil {
			dependencies["redis"] = "unreachable"
			errors = append(errors, err.Error())
		} else {
			dependencies["redis"] = "ok"
		}
	}

	if h.mysqlClient != nil && h.mysqlClient.Enabled() {
		if err := h.mysqlClient.Ping(ctx); err != nil {
			dependencies["mysql"] = "unreachable"
			errors = append(errors, err.Error())
		} else {
			dependencies["mysql"] = "ok"
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  fmt.Sprintf("readiness check failed: %s", strings.Join(errors, "; ")),
			Data: gin.H{
				"ready":        false,
				"dependencies": dependencies,
			},
		})
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data: gin.H{
			"ready":        true,
			"dependencies": dependencies,
		},
	})
}

// @Summary      获取主机列表
// @Description  查询 Prometheus 获取所有监控主机列表
// @Tags         hosts
// @Produce      json
// @Security     BearerAuth
// @Param        group  query  string  false  "主机组名称过滤"
// @Success      200  {object}  response
// @Failure      401  {object}  response
// @Failure      500  {object}  response
// @Router       /hosts [get]
func (h *Handler) Hosts(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	groupInstances, groupFiltered, ok := h.parseHostGroupFilter(c)
	if !ok {
		return
	}

	hosts, err := h.hostService.Hosts(ctx, apphost.ListOptions{
		Status:         apphost.ParseStatus(c.Query("status")),
		Query:          apphost.NormalizeQuery(c.Query("q")),
		Sort:           apphost.ParseSort(c.Query("sort")),
		Risk:           apphost.ParseRisk(c.Query("risk")),
		GroupFiltered:  groupFiltered,
		GroupInstances: groupInstances,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   hosts,
	})
}

func (h *Handler) HostMetrics(c *gin.Context) {
	instance := strings.TrimSpace(c.Param("instance"))
	if instance == "" {
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  "instance is required",
		})
		return
	}

	metrics, ok, err := h.hostService.Metrics(c.Request.Context(), instance, c.Query("range"), c.Query("mountpoint"), time.Now().UTC())
	if !ok {
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  "invalid range, allowed values: 15m, 1h, 6h, 24h",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   metrics,
	})
}

func (h *Handler) DashboardOverview(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	if cached, ok := h.cacheService.GetDashboardOverview(ctx); ok {
		c.JSON(http.StatusOK, response{
			Status: "success",
			Data:   cached,
		})
		return
	}

	hosts, err := h.promClient.GetHosts(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}

	overview := apphost.BuildDashboardOverview(hosts)
	activeAlerts, degraded := h.cacheService.CountActiveAlerts(ctx)
	overview.ActiveAlerts = activeAlerts
	overview.AlertDegraded = degraded
	overview.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	cacheCtx, cacheCancel := context.WithTimeout(context.Background(), h.cacheTimeout)
	defer cacheCancel()
	if !degraded {
		h.cacheService.CacheDashboardOverview(cacheCtx, overview)
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   overview,
	})
}

// @Summary      Alertmanager Webhook 接收
// @Description  接收 Alertmanager 发送的告警通知，转发到 Kafka 和 WebSocket
// @Tags         webhook
// @Accept       json
// @Produce      json
// @Param        body  body  webhook.AlertmanagerWebhookRequest  true  "Alertmanager Webhook 请求"
// @Success      200  {object}  response
// @Failure      400  {object}  response
// @Failure      503  {object}  response
// @Router       /webhook/alertmanager [post]
func (h *Handler) AlertmanagerWebhook(c *gin.Context) {
	if !h.alertService.Enabled() {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  "redis is required for alert webhook handling",
		})
		return
	}

	var payload webhook.AlertmanagerWebhookRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		status := http.StatusBadRequest
		message := fmt.Sprintf("invalid alertmanager payload: %v", err)
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
			message = "alertmanager payload too large"
		}
		c.JSON(status, response{
			Status: "error",
			Error:  message,
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	receivedAt := time.Now().UTC()

	if err := h.alertService.HandleWebhook(ctx, payload, receivedAt); err != nil {
		writeAlertServiceError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, response{
		Status: "accepted",
	})
}

// @Summary      获取活跃告警列表
// @Description  从 Redis 获取当前活跃的告警列表
// @Tags         alerts
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response
// @Failure      401  {object}  response
// @Failure      500  {object}  response
// @Router       /alerts/active [get]
func (h *Handler) ActiveAlerts(c *gin.Context) {
	if !h.alertService.Enabled() {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  "redis is required for active alerts query",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	alerts, err := h.alertService.ActiveAlerts(ctx, appalert.ParseActiveSeverityFilter(c.Query("severity")))
	if err != nil {
		writeAlertServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   alerts,
	})
}

// @Summary      获取告警事件列表
// @Description  查询告警事件历史，支持按状态和严重程度过滤
// @Tags         alerts
// @Produce      json
// @Security     BearerAuth
// @Param        status     query  string  false  "状态过滤 (firing/resolved)"
// @Param        severity   query  string  false  "严重程度过滤 (critical/warning/info)"
// @Param        search     query  string  false  "搜索关键词"
// @Param        page       query  int     false  "页码"
// @Param        page_size  query  int     false  "每页数量"
// @Success      200  {object}  response
// @Failure      401  {object}  response
// @Failure      500  {object}  response
// @Router       /alerts/events [get]
func (h *Handler) AlertEvents(c *gin.Context) {
	if !h.alertService.Enabled() {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  "redis is required for alert events query",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	events, err := h.alertService.AlertEvents(
		ctx,
		appalert.ParseEventsLimit(c.Query("limit")),
		appalert.ParseEventFilter(c.Query("status")),
		appalert.ParseEventSeverityFilter(c.Query("severity")),
	)
	if err != nil {
		writeAlertServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   events,
	})
}

func writeAlertServiceError(c *gin.Context, err error) {
	var serviceErr *appalert.ServiceError
	if !errors.As(err, &serviceErr) {
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: err.Error()})
		return
	}

	switch serviceErr.Kind {
	case appalert.ErrorUnsupportedStatus:
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  fmt.Sprintf("unsupported alert status %q", serviceErr.Status),
		})
	case appalert.ErrorMarshalAlert:
		c.JSON(http.StatusInternalServerError, response{
			Status: "error",
			Error:  fmt.Sprintf("marshal alert payload failed: %v", serviceErr.Err),
		})
	case appalert.ErrorStoreActiveAlert:
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  fmt.Sprintf("store active alert failed: %v", serviceErr.Err),
		})
	case appalert.ErrorDeleteActiveAlert:
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  fmt.Sprintf("delete active alert failed: %v", serviceErr.Err),
		})
	case appalert.ErrorMarshalAlertEvent:
		c.JSON(http.StatusInternalServerError, response{
			Status: "error",
			Error:  fmt.Sprintf("marshal alert event failed: %v", serviceErr.Err),
		})
	case appalert.ErrorStoreAlertEvent:
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  fmt.Sprintf("store alert event failed: %v", serviceErr.Err),
		})
	case appalert.ErrorLoadActiveAlerts:
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  fmt.Sprintf("load active alerts failed: %v", serviceErr.Err),
		})
	case appalert.ErrorLoadAlertEvents:
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  fmt.Sprintf("load alert events failed: %v", serviceErr.Err),
		})
	default:
		c.JSON(http.StatusInternalServerError, response{Status: "error", Error: err.Error()})
	}
}

func (h *Handler) AlertsWebSocket(c *gin.Context) {
	if h.websocketHub == nil {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  "websocket hub is unavailable",
		})
		return
	}

	if err := h.websocketHub.ServeWS(c.Writer, c.Request); err != nil {
		zap.L().Warn("websocket upgrade failed", zap.Error(err))
	}
}
