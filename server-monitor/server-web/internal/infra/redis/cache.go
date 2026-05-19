package rediscache

const (
	HostsListKey         = "hosts:list"
	DashboardOverviewKey = "dashboard:overview"
	ActiveAlertsKey      = "alert:active"
	AlertEventsKey       = "alert:events"
	AlertEventDedupeKey  = "alert:event:dedupe"
	AlertEventPayload    = "payload"
	AlertChannel         = "alert:channel"
	AlertEventsMax       = 200
	RateLimitKeyPrefix   = "ratelimit"

	K8sOverviewKey       = "k8s:overview"
	K8sNodesKey          = "k8s:nodes"
	K8sPodsKey           = "k8s:pods"
	K8sDeploymentsKey    = "k8s:deployments"
	K8sServicesKey       = "k8s:services"
	K8sEventsKey         = "k8s:events"
	K8sConfigMapsKey     = "k8s:configmaps"
	K8sIngressesKey      = "k8s:ingresses"
	K8sPVsKey            = "k8s:pvs"
	K8sPVCsKey           = "k8s:pvcs"
	K8sResourceQuotasKey = "k8s:resource-quotas"
	K8sLimitRangesKey    = "k8s:limit-ranges"
	K8sHPAsKey           = "k8s:hpas"
	K8sDaemonSetsKey     = "k8s:daemonsets"
	K8sStatefulSetsKey   = "k8s:statefulsets"
	K8sJobsKey           = "k8s:jobs"
)
