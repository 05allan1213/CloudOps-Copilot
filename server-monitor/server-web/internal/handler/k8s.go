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
	ListIngresses(ctx context.Context, opts k8ssvc.IngressListOptions) (*k8ssvc.IngressListResult, error)
	ListEvents(ctx context.Context, opts k8ssvc.EventListOptions) (*k8ssvc.EventListResult, error)
	ListConfigMaps(ctx context.Context, opts k8ssvc.ConfigMapListOptions) (*k8ssvc.ConfigMapListResult, error)
	ListResourceQuotas(ctx context.Context, opts k8ssvc.ResourceQuotaListOptions) (*k8ssvc.ResourceQuotaListResult, error)
	ListLimitRanges(ctx context.Context, opts k8ssvc.LimitRangeListOptions) (*k8ssvc.LimitRangeListResult, error)
	ListPVs(ctx context.Context, opts k8ssvc.PVListOptions) (*k8ssvc.PVListResult, error)
	ListPVCs(ctx context.Context, opts k8ssvc.PVCListOptions) (*k8ssvc.PVCListResult, error)
	ListHPAs(ctx context.Context, opts k8ssvc.HPAListOptions) (*k8ssvc.HPAListResult, error)
	ListDaemonSets(ctx context.Context, opts k8ssvc.DaemonSetListOptions) (*k8ssvc.DaemonSetListResult, error)
	ListStatefulSets(ctx context.Context, opts k8ssvc.StatefulSetListOptions) (*k8ssvc.StatefulSetListResult, error)
	ListJobs(ctx context.Context, opts k8ssvc.JobListOptions) (*k8ssvc.JobListResult, error)
	Topology(ctx context.Context, namespace string) (*copilotk8s.TopologyData, error)
	GetPodLogs(ctx context.Context, query copilotk8s.LogQuery) (*copilotk8s.LogSnippet, error)
	GetResourceYAML(ctx context.Context, kind, namespace, name string) (string, error)
	FindNodeByInstance(ctx context.Context, instance string) (*copilotk8s.NodeSummary, error)
}

type K8sHandler struct {
	k8sService     K8sService
	nodesEnabled   bool
	requestTimeout time.Duration
	clusters       map[string]K8sService
}

func NewK8sHandler(k8sService K8sService, nodesEnabled bool, requestTimeout time.Duration, clusters map[string]*k8ssvc.Service) *K8sHandler {
	clusterServices := make(map[string]K8sService, len(clusters))
	for name, svc := range clusters {
		clusterServices[name] = svc
	}
	return &K8sHandler{
		k8sService:     k8sService,
		nodesEnabled:   nodesEnabled,
		requestTimeout: requestTimeout,
		clusters:       clusterServices,
	}
}

func (h *K8sHandler) serviceForCluster(c *gin.Context) K8sService {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		return h.k8sService
	}
	if svc, ok := h.clusters[cluster]; ok {
		return svc
	}
	return h.k8sService
}

func (h *K8sHandler) clusterName(c *gin.Context) string {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		return "default"
	}
	if _, ok := h.clusters[cluster]; ok {
		return cluster
	}
	return "default"
}

func (h *K8sHandler) ListClusters(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}
	clusters := make([]map[string]string, 0)
	clusters = append(clusters, map[string]string{"name": "default"})
	for name := range h.clusters {
		clusters = append(clusters, map[string]string{"name": name})
	}
	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   clusters,
	})
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

	result, err := h.serviceForCluster(c).Overview(ctx)
	if err != nil {
		handleK8sError(c, err)
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
	result, err := h.serviceForCluster(c).ListNodes(ctx, k8ssvc.NodeListOptions{
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
		handleK8sError(c, err)
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

	result, err := h.serviceForCluster(c).GetNodeDetail(ctx, name)
	if err != nil {
		if errors.Is(err, k8ssvc.ErrNodesNotEnabled) {
			c.JSON(http.StatusServiceUnavailable, response{
				Status: "error",
				Error:  err.Error(),
			})
			return
		}
		handleK8sError(c, err)
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

	result, err := h.serviceForCluster(c).FindNodeByInstance(ctx, instance)
	if err != nil {
		if errors.Is(err, k8ssvc.ErrNodesNotEnabled) {
			c.JSON(http.StatusServiceUnavailable, response{
				Status: "error",
				Error:  err.Error(),
			})
			return
		}
		handleK8sError(c, err)
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
	result, err := h.serviceForCluster(c).ListPods(ctx, k8ssvc.PodListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Phase:     strings.TrimSpace(c.Query("phase")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
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
	result, err := h.serviceForCluster(c).ListDeployments(ctx, k8ssvc.DeploymentListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
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
	result, err := h.serviceForCluster(c).ListServices(ctx, k8ssvc.ServiceListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Type:      strings.TrimSpace(c.Query("type")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) ListIngresses(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.serviceForCluster(c).ListIngresses(ctx, k8ssvc.IngressListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
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
	result, err := h.serviceForCluster(c).ListEvents(ctx, k8ssvc.EventListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Type:      strings.TrimSpace(c.Query("type")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) ListConfigMaps(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.serviceForCluster(c).ListConfigMaps(ctx, k8ssvc.ConfigMapListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) ListResourceQuotas(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.serviceForCluster(c).ListResourceQuotas(ctx, k8ssvc.ResourceQuotaListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) ListLimitRanges(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.serviceForCluster(c).ListLimitRanges(ctx, k8ssvc.LimitRangeListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) ListPVs(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.serviceForCluster(c).ListPVs(ctx, k8ssvc.PVListOptions{
		Status: strings.TrimSpace(c.Query("status")),
		Search: strings.TrimSpace(c.Query("search")),
		Limit:  limit,
	})
	if err != nil {
		handleK8sError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) ListPVCs(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.serviceForCluster(c).ListPVCs(ctx, k8ssvc.PVCListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Status:    strings.TrimSpace(c.Query("status")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) ListHPAs(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.serviceForCluster(c).ListHPAs(ctx, k8ssvc.HPAListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) ListDaemonSets(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.serviceForCluster(c).ListDaemonSets(ctx, k8ssvc.DaemonSetListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) ListStatefulSets(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.serviceForCluster(c).ListStatefulSets(ctx, k8ssvc.StatefulSetListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) ListJobs(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	limit := parseLimit(c.Query("limit"))
	result, err := h.serviceForCluster(c).ListJobs(ctx, k8ssvc.JobListOptions{
		Namespace: strings.TrimSpace(c.Query("namespace")),
		Status:    strings.TrimSpace(c.Query("status")),
		Search:    strings.TrimSpace(c.Query("search")),
		Limit:     limit,
	})
	if err != nil {
		handleK8sError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) Topology(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	namespace := strings.TrimSpace(c.Query("namespace"))
	result, err := h.serviceForCluster(c).Topology(ctx, namespace)
	if err != nil {
		handleK8sError(c, err)
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

	result, err := h.serviceForCluster(c).GetPodLogs(ctx, copilotk8s.LogQuery{
		Namespace: namespace,
		PodName:   podName,
		Container: strings.TrimSpace(c.Query("container")),
		TailLines: tailLines,
	})
	if err != nil {
		handleK8sError(c, err)
		return
	}

	c.JSON(http.StatusOK, response{
		Status: "success",
		Data:   result,
	})
}

func (h *K8sHandler) GetResourceYAML(c *gin.Context) {
	if !h.requireK8s(c) {
		return
	}

	kind := strings.TrimSpace(c.Param("kind"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))

	if kind == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, response{
			Status: "error",
			Error:  "kind, namespace, and name are required",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	result, err := h.serviceForCluster(c).GetResourceYAML(ctx, kind, namespace, name)
	if err != nil {
		handleK8sError(c, err)
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

func handleK8sError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, copilotk8s.ErrNamespaceNotAllowed):
		c.JSON(http.StatusForbidden, response{
			Status: "error",
			Error:  err.Error(),
		})
	case errors.Is(err, k8ssvc.ErrNodesNotEnabled):
		c.JSON(http.StatusServiceUnavailable, response{
			Status: "error",
			Error:  err.Error(),
		})
	default:
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "resource not found") {
			c.JSON(http.StatusNotFound, response{
				Status: "error",
				Error:  errStr,
			})
			return
		}
		if strings.Contains(errStr, "permission denied") || strings.Contains(errStr, "forbidden") {
			c.JSON(http.StatusForbidden, response{
				Status: "error",
				Error:  errStr,
			})
			return
		}
		c.JSON(http.StatusBadGateway, response{
			Status: "error",
			Error:  errStr,
		})
	}
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
