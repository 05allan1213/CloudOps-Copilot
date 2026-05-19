package router

import (
	"github.com/gin-gonic/gin"

	"server-web/internal/config"
	"server-web/internal/middleware"
)

func registerK8sRoutes(router *gin.Engine, cfg config.Config, deps Dependencies) {
	if !cfg.K8SEnabled || !cfg.K8SAPIEnabled || deps.K8sHandler == nil {
		return
	}
	protected := router.Group("")
	if cfg.AuthEnabled {
		protected.Use(middleware.Auth(deps.AuthService), middleware.VerifyTokenVersion(deps.AuthService))
	}
	k8s := protected.Group("/api/v1/k8s")
	k8s.GET("/overview", deps.K8sHandler.Overview)
	if cfg.K8SNodesEnabled {
		k8s.GET("/nodes/by-instance/:instance", deps.K8sHandler.GetNodeByInstance)
		k8s.GET("/nodes/:name", deps.K8sHandler.GetNodeDetail)
		k8s.GET("/nodes", deps.K8sHandler.ListNodes)
	}
	k8s.GET("/pods", deps.K8sHandler.ListPods)
	k8s.GET("/deployments", deps.K8sHandler.ListDeployments)
	k8s.GET("/services", deps.K8sHandler.ListServices)
	k8s.GET("/ingresses", deps.K8sHandler.ListIngresses)
	k8s.GET("/configmaps", deps.K8sHandler.ListConfigMaps)
	k8s.GET("/persistent-volumes", deps.K8sHandler.ListPVs)
	k8s.GET("/persistent-volume-claims", deps.K8sHandler.ListPVCs)
	k8s.GET("/hpas", deps.K8sHandler.ListHPAs)
	k8s.GET("/daemonsets", deps.K8sHandler.ListDaemonSets)
	k8s.GET("/statefulsets", deps.K8sHandler.ListStatefulSets)
	k8s.GET("/jobs", deps.K8sHandler.ListJobs)
	k8s.GET("/topology", deps.K8sHandler.Topology)
	k8s.GET("/clusters", deps.K8sHandler.ListClusters)
	k8s.GET("/resource-quotas", deps.K8sHandler.ListResourceQuotas)
	k8s.GET("/limit-ranges", deps.K8sHandler.ListLimitRanges)
	k8s.GET("/events", deps.K8sHandler.ListEvents)
	k8s.GET("/pods/:namespace/:name/logs", deps.K8sHandler.GetPodLogs)
	k8s.GET("/resources/:kind/:namespace/:name/yaml", deps.K8sHandler.GetResourceYAML)
}
