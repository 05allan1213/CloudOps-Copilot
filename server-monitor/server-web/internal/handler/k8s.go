package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	copilotk8s "server-web/internal/copilot/k8s"
	k8ssvc "server-web/internal/service/k8s"
)

type K8sService interface {
	Overview(ctx context.Context) (*k8ssvc.ClusterOverview, error)
	ListNodes(ctx context.Context, opts k8ssvc.NodeListOptions) (*k8ssvc.NodeListResult, error)
	GetNodeDetail(ctx context.Context, name string) (*k8ssvc.NodeDetail, error)
	ListPods(ctx context.Context, opts k8ssvc.PodListOptions) (*k8ssvc.PodListResult, error)
	ListDeployments(ctx context.Context, opts k8ssvc.DeploymentListOptions) (*k8ssvc.DeploymentListResult, error)
	ListServices(ctx context.Context, opts k8ssvc.ServiceListOptions) (*k8ssvc.ServiceListResult, error)
	ListEvents(ctx context.Context, opts k8ssvc.EventListOptions) (*k8ssvc.EventListResult, error)
	GetPodLogs(ctx context.Context, query copilotk8s.LogQuery) (*copilotk8s.LogSnippet, error)
	FindNodeByInstance(ctx context.Context, instance string) (*copilotk8s.NodeSummary, error)
}

type K8sHandler struct {
	k8sService     K8sService
	nodesEnabled   bool
	requestTimeout time.Duration
}

func NewK8sHandler(k8sService K8sService, nodesEnabled bool, requestTimeout time.Duration) *K8sHandler {
	return &K8sHandler{
		k8sService:     k8sService,
		nodesEnabled:   nodesEnabled,
		requestTimeout: requestTimeout,
	}
}

func (h *K8sHandler) requireK8s(c *gin.Context) bool {
	if h.k8sService == nil {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  "kubernetes integration is not enabled",
		})
		return false
	}
	return true
}

func (h *K8sHandler) requireNodes(c *gin.Context) bool {
	if !h.requireK8s(c) {
		return false
	}
	if !h.nodesEnabled {
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  "kubernetes nodes query is not enabled",
		})
		return false
	}
	return true
}

func (h *K8sHandler) Overview(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	result, err := h.k8sService.Overview(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) ListNodes(c *gin.Context) {
	if !h.requireNodes(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.k8sService.ListNodes(ctx, k8ssvc.NodeListOptions{
		Status: strings.TrimSpace(c.Query("status")),
		Role:   strings.TrimSpace(c.Query("role")),
		Search: strings.TrimSpace(c.Query("search")),
		Limit:  limit,
	})
	if err != nil {
		if errors.Is(err, k8ssvc.ErrNodesNotEnabled) {
			c.JSON(http.StatusServiceUnavailable, response{
				Status: "error",
				Error:  err.Error(),
			})
			return
		}
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) GetNodeDetail(c *gin.Context) {
	if !h.requireNodes(c) {
		return
	}

	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  "node name is required",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	result, err := h.k8sService.GetNodeDetail(ctx, name)
	if err != nil {
		if errors.Is(err, k8ssvc.ErrNodesNotEnabled) {
			c.JSON(http.StatusServiceUnavailable, response{
				Status: "error",
				Error:  err.Error(),
			})
			return
		}
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}
	if result == nil {
		c.JSON(http.StatusNotFound, response{
			Status: "error",
			Error:  "node not found",
		})
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) GetNodeByInstance(c *gin.Context) {
	if !h.requireNodes(c) {
		return
	}

	instance := strings.TrimSpace(c.Param("instance"))

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	result, err := h.k8sService.FindNodeByInstance(ctx, instance)
	if err != nil {
		if errors.Is(err, k8ssvc.ErrNodesNotEnabled) {
			c.JSON(http.StatusServiceUnavailable, response{
				Status: "error",
				Error:  err.Error(),
			})
			return
		}
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}
	if result == nil {
		c.JSON(http.StatusNotFound, response{
			Status: "error",
			Error:  fmt.Sprintf("node not found for instance %q", instance),
		})
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) ListPods(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.k8sService.ListPods(ctx, k8ssvc.PodListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Phase:     strings.TrimSpace(c.Query("phase")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
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
		Data:   result,
	})
}

func (h *K8sHandler) ListDeployments(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.k8sService.ListDeployments(ctx, k8ssvc.DeploymentListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
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
		Data:   result,
	})
}

func (h *K8sHandler) ListServices(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.k8sService.ListServices(ctx, k8ssvc.ServiceListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Type:      strings.TrimSpace(c.Query("type")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
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
		Data:   result,
	})
}

func (h *K8sHandler) ListEvents(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.k8sService.ListEvents(ctx, k8ssvc.EventListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Type:      strings.TrimSpace(c.Query("type")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
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
		Data:   result,
	})
}

func (h *K8sHandler) GetPodLogs(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	namespace := strings.TrimSpace(c.Param("namespace"))
	podName := strings.TrimSpace(c.Param("name"))
	if namespace == "" || podName == "" {
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  "namespace and name are required",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	tailLines := 0
	if v := c.Query("tail_lines"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tailLines = n
		}
	}

	result, err := h.k8sService.GetPodLogs(ctx, copilotk8s.LogQuery{
		Namespace: namespace,
		PodName:   podName,
		Container: strings.TrimSpace(c.Query("container")),
		TailLines: tailLines,
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
		Data:   result,
	})
}

func parseLimit(raw string) int {
	if raw == "" {
		return 50
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 50
	}
	if n > 200 {
		return 200
	}
	return n
}

func parsePage(c *gin.Context) int {
	v := strings.TrimSpace(c.Query("page"))
	if v == "" {
		return 1
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 1
	}
	return n
}

func parsePageSize(c *gin.Context) int {
	v := strings.TrimSpace(c.Query("page_size"))
	if v == "" {
		return 20
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 20
	}
	if n > 100 {
		return 100
	}
	return n
}
