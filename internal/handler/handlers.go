package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type mysqlClient interface {
	Enabled() bool
	Ping(ctx context.Context) error
	Ready(ctx context.Context) error
}

type RuntimeReadiness func(context.Context) error

type Handler struct {
	mysqlClient  mysqlClient
	readyTimeout time.Duration
}

type Config struct {
	ReadyTimeout time.Duration
	MySQLClient  mysqlClient
}

func NewHandler(cfg Config) (*Handler, error) {
	if cfg.ReadyTimeout <= 0 {
		return nil, fmt.Errorf("ready timeout must be positive")
	}
	return &Handler{
		mysqlClient: cfg.MySQLClient, readyTimeout: cfg.ReadyTimeout,
	}, nil
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
	dependencies := gin.H{"mysql": "unavailable"}
	failures := make([]string, 0, 1)
	if h.mysqlClient == nil || !h.mysqlClient.Enabled() {
		failures = append(failures, "mysql is not initialized")
	} else {
		if err := h.mysqlClient.Ready(ctx); err != nil {
			dependencies["mysql"] = "unreachable"
			failures = append(failures, err.Error())
		} else {
			dependencies["mysql"] = "ok"
		}
	}
	writeReadiness(c, dependencies, failures)
}

func (h *Handler) ReadyzFull(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.readyTimeout)
	defer cancel()
	dependencies := gin.H{"mysql": "unavailable"}
	failures := make([]string, 0, 1)
	if h.mysqlClient != nil && h.mysqlClient.Enabled() {
		if err := h.mysqlClient.Ping(ctx); err != nil {
			dependencies["mysql"] = "unreachable"
			failures = append(failures, err.Error())
		} else {
			dependencies["mysql"] = "ok"
		}
	} else {
		failures = append(failures, "mysql is not initialized")
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
