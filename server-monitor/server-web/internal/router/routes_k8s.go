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
	k8s.GET("/events", deps.K8sHandler.ListEvents)
	k8s.GET("/pods/:namespace/:name/logs", deps.K8sHandler.GetPodLogs)
}
