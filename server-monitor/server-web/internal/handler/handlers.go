package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"server-web/internal/infra/k8sread"
	promclient "server-web/internal/infra/prometheus"
	authpkg "server-web/internal/service/auth"
)

type AuthService interface {
	Login(ctx context.Context, username string, password string) (authpkg.LoginResult, error)
	AuthenticateBearer(authHeader string) (authpkg.Identity, error)
	VerifyTokenVersion(ctx context.Context, identity authpkg.Identity) error
}

type cacheClient interface {
	Enabled() bool
	Ping(ctx context.Context) error
}

type mysqlClient interface {
	Enabled() bool
	Ping(ctx context.Context) error
}

type Handler struct {
	promClient           *promclient.Client
	cacheClient          cacheClient
	incidentService      IncidentApplication
	agentRuntime         AgentApplication
	changeService        ChangeApplication
	remediation          RemediationApplication
	deliveryVerification DeliveryVerificationApplication
	fastDemo             FastDemoApplication
	fastDemoActor        string
	k8sReader            k8sread.Reader
	mysqlClient          mysqlClient
	authService          AuthService
	readyTimeout         time.Duration
	requestTimeout       time.Duration
}

type Config struct {
	ReadyTimeout    time.Duration
	RequestTimeout  time.Duration
	IncidentService IncidentApplication
	ChangeService   ChangeApplication
	MySQLClient     mysqlClient
	AuthService     AuthService
}

func NewHandler(promClient *promclient.Client, cache cacheClient, cfg Config) (*Handler, error) {
	if promClient == nil {
		return nil, errors.New("prometheus client is required")
	}
	return &Handler{
		promClient:      promClient,
		cacheClient:     cache,
		incidentService: cfg.IncidentService,
		changeService:   cfg.ChangeService,
		mysqlClient:     cfg.MySQLClient,
		authService:     cfg.AuthService,
		readyTimeout:    cfg.ReadyTimeout,
		requestTimeout:  cfg.RequestTimeout,
	}, nil
}

func (h *Handler) SetIncidentK8sReader(reader k8sread.Reader) {
	h.k8sReader = reader
}

type response struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

func (h *Handler) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"healthy": true}})
}

func (h *Handler) Readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.readyTimeout)
	defer cancel()
	dependencies := gin.H{"prometheus": "ok", "redis": "disabled"}
	failures := make([]string, 0, 2)
	if err := h.promClient.Ready(ctx); err != nil {
		dependencies["prometheus"] = "unreachable"
		failures = append(failures, err.Error())
	}
	if h.cacheClient != nil && h.cacheClient.Enabled() {
		if err := h.cacheClient.Ping(ctx); err != nil {
			dependencies["redis"] = "unreachable"
			failures = append(failures, err.Error())
		} else {
			dependencies["redis"] = "ok"
		}
	}
	writeReadiness(c, dependencies, failures)
}

func (h *Handler) ReadyzFull(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.readyTimeout)
	defer cancel()
	dependencies := gin.H{"prometheus": "ok", "redis": "disabled", "mysql": "disabled"}
	failures := make([]string, 0, 3)
	if err := h.promClient.Ready(ctx); err != nil {
		dependencies["prometheus"] = "unreachable"
		failures = append(failures, err.Error())
	}
	if h.cacheClient != nil && h.cacheClient.Enabled() {
		if err := h.cacheClient.Ping(ctx); err != nil {
			dependencies["redis"] = "unreachable"
			failures = append(failures, err.Error())
		} else {
			dependencies["redis"] = "ok"
		}
	}
	if h.mysqlClient != nil && h.mysqlClient.Enabled() {
		if err := h.mysqlClient.Ping(ctx); err != nil {
			dependencies["mysql"] = "unreachable"
			failures = append(failures, err.Error())
		} else {
			dependencies["mysql"] = "ok"
		}
	}
	writeReadiness(c, dependencies, failures)
}

func writeReadiness(c *gin.Context, dependencies gin.H, failures []string) {
	if len(failures) > 0 {
		c.JSON(http.StatusServiceUnavailable, response{Status: "error", Error: fmt.Sprintf("readiness check failed: %s", strings.Join(failures, "; ")), Data: gin.H{"ready": false, "dependencies": dependencies}})
		return
	}
	c.JSON(http.StatusOK, response{Status: "success", Data: gin.H{"ready": true, "dependencies": dependencies}})
}
