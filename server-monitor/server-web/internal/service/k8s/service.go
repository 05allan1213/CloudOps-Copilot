package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	copilotk8s "server-web/internal/copilot/k8s"
	rediscache "server-web/internal/infra/redis"
	cachesvc "server-web/internal/service/cache"
)

const (
	DefaultLimit = 50
	MaxLimit     = 200
)

var ErrNodesNotEnabled = errors.New("nodes query is not enabled")

var validNodeStatuses = map[string]struct{}{
	"ready":    {},
	"notready": {},
}

var validNodeRoles = map[string]struct{}{
	"control-plane": {},
	"worker":        {},
}

var validPodPhases = map[string]struct{}{
	"Running":   {},
	"Pending":   {},
	"Failed":    {},
	"Succeeded": {},
}

var validServiceTypes = map[string]struct{}{
	"ClusterIP":    {},
	"NodePort":     {},
	"LoadBalancer": {},
}

var validEventTypes = map[string]struct{}{
	"Normal":  {},
	"Warning": {},
}

var validPVStatuses = map[string]struct{}{
	"Available": {},
	"Bound":     {},
	"Released":  {},
	"Failed":    {},
	"Pending":   {},
}

var validPVCStatuses = map[string]struct{}{
	"Pending": {},
	"Bound":   {},
	"Lost":    {},
}

var validJobStatuses = map[string]struct{}{
	"Running":   {},
	"Completed": {},
	"Failed":    {},
	"Suspended": {},
}

type K8sReader interface {
	ListPods(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.PodSummary, error)
	ListDeployments(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.DeploymentSummary, error)
	ListServices(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.ServiceSummary, error)
	ListNodes(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.NodeSummary, error)
	ListEvents(ctx context.Context, query copilotk8s.EventQuery) ([]copilotk8s.EventSummary, error)
	GetPodLogs(ctx context.Context, query copilotk8s.LogQuery) (copilotk8s.LogSnippet, error)
	ListAllPods(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.PodSummary, error)
	ListAllNodes(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.NodeSummary, error)
	ListAllDeployments(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.DeploymentSummary, error)
	ListAllServices(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.ServiceSummary, error)
	ListIngresses(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.IngressSummary, error)
	ListAllIngresses(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.IngressSummary, error)
	ListAllEvents(ctx context.Context, query copilotk8s.EventQuery) ([]copilotk8s.EventSummary, error)
	ListConfigMaps(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.ConfigMapSummary, error)
	ListResourceQuotas(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.ResourceQuotaSummary, error)
	ListLimitRanges(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.LimitRangeSummary, error)
	ListPersistentVolumes(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.PVSummary, error)
	ListPersistentVolumeClaims(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.PVCSummary, error)
	ListHorizontalPodAutoscalers(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.HPASummary, error)
	ListDaemonSets(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.DaemonSetSummary, error)
	ListStatefulSets(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.StatefulSetSummary, error)
	ListJobs(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.JobSummary, error)
	GetResourceYAML(ctx context.Context, kind, namespace, name string) (string, error)
}

type PrometheusClient interface {
	GetHosts(ctx context.Context) ([]HostInfo, error)
}

type HostInfo struct {
	Instance   string
	Status     string
	LastScrape string
}

type Service struct {
	reader       K8sReader
	promClient   PrometheusClient
	timeout      time.Duration
	nodesEnabled bool
	cacheService *cachesvc.Service
	cacheTTL     time.Duration
	listCacheTTL time.Duration
	clusterName  string
}

type Options struct {
	RequestTimeout time.Duration
	NodesEnabled   bool
	CacheService   *cachesvc.Service
	CacheTTL       time.Duration
	ListCacheTTL   time.Duration
	ClusterName    string
}

func NewService(reader K8sReader, promClient PrometheusClient, opts Options) *Service {
	timeout := opts.RequestTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cacheTTL := opts.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = 30 * time.Second
	}
	listCacheTTL := opts.ListCacheTTL
	if listCacheTTL <= 0 {
		listCacheTTL = 15 * time.Second
	}
	return &Service{
		reader:       reader,
		promClient:   promClient,
		timeout:      timeout,
		nodesEnabled: opts.NodesEnabled,
		cacheService: opts.CacheService,
		cacheTTL:     cacheTTL,
		listCacheTTL: listCacheTTL,
		clusterName:  opts.ClusterName,
	}
}

type NodeListOptions struct {
	Status string
	Role   string
	Search string
	Limit  int
}

type PodListOptions struct {
	Namespace string
	Phase     string
	Search    string
	Limit     int
}

type DeploymentListOptions struct {
	Namespace string
	Search    string
	Limit     int
}

type ServiceListOptions struct {
	Namespace string
	Type      string
	Search    string
	Limit     int
}

type IngressListOptions struct {
	Namespace string
	Search    string
	Limit     int
}

type EventListOptions struct {
	Namespace string
	Type      string
	Search    string
	Limit     int
}

type ConfigMapListOptions struct {
	Namespace string
	Search    string
	Limit     int
}

type ClusterOverview struct {
	Nodes          NodeStats                 `json:"nodes"`
	NodesAvailable bool                      `json:"nodes_available"`
	Pods           PodStats                  `json:"pods"`
	Deployments    DeploymentStats           `json:"deployments"`
	RecentEvents   []copilotk8s.EventSummary `json:"recent_events"`
	HostCoverage   HostCoverageStats         `json:"host_coverage"`
	Truncated      bool                      `json:"truncated"`
	CollectedAt    time.Time                 `json:"collected_at"`
}

type NodeStats struct {
	Total    int `json:"total"`
	Ready    int `json:"ready"`
	NotReady int `json:"not_ready"`
}

type PodStats struct {
	Total     int `json:"total"`
	Running   int `json:"running"`
	Pending   int `json:"pending"`
	Failed    int `json:"failed"`
	Succeeded int `json:"succeeded"`
}

type DeploymentStats struct {
	Total       int `json:"total"`
	Available   int `json:"available"`
	Unavailable int `json:"unavailable"`
}

type HostCoverageStats struct {
	TotalNodes     int `json:"total_nodes"`
	CoveredNodes   int `json:"covered_nodes"`
	UncoveredNodes int `json:"uncovered_nodes"`
}

type NodeListResult struct {
	Items []NodeWithHost `json:"items"`
	Total int            `json:"total"`
}

type NodeWithHost struct {
	Node       copilotk8s.NodeSummary `json:"node"`
	HostOnline bool                   `json:"host_online"`
	LastScrape string                 `json:"last_scrape,omitempty"`
}

type NodeDetail struct {
	Node             copilotk8s.NodeSummary    `json:"node"`
	HostOnline       bool                      `json:"host_online"`
	LastScrape       string                    `json:"last_scrape,omitempty"`
	Pods             []copilotk8s.PodSummary   `json:"pods"`
	Events           []copilotk8s.EventSummary `json:"events"`
	CollectionErrors []string                  `json:"collection_errors,omitempty"`
}

type HostAssociation struct {
	Online     bool   `json:"online"`
	LastScrape string `json:"last_scrape,omitempty"`
}

type PodListResult struct {
	Items []copilotk8s.PodSummary `json:"items"`
	Total int                     `json:"total"`
}

type DeploymentListResult struct {
	Items []copilotk8s.DeploymentSummary `json:"items"`
	Total int                            `json:"total"`
}

type ServiceListResult struct {
	Items []copilotk8s.ServiceSummary `json:"items"`
	Total int                         `json:"total"`
}

type IngressListResult struct {
	Items []copilotk8s.IngressSummary `json:"items"`
	Total int                         `json:"total"`
}

type EventListResult struct {
	Items []copilotk8s.EventSummary `json:"items"`
	Total int                       `json:"total"`
}

type ConfigMapListResult struct {
	Items []copilotk8s.ConfigMapSummary `json:"items"`
	Total int                           `json:"total"`
}

type ResourceQuotaListOptions struct {
	Namespace string
	Search    string
	Limit     int
}

type ResourceQuotaListResult struct {
	Items []copilotk8s.ResourceQuotaSummary `json:"items"`
	Total int                               `json:"total"`
}

type LimitRangeListOptions struct {
	Namespace string
	Search    string
	Limit     int
}

type LimitRangeListResult struct {
	Items []copilotk8s.LimitRangeSummary `json:"items"`
	Total int                            `json:"total"`
}

type PVListOptions struct {
	Status string
	Search string
	Limit  int
}

type PVListResult struct {
	Items []copilotk8s.PVSummary `json:"items"`
	Total int                    `json:"total"`
}

type PVCListOptions struct {
	Namespace string
	Status    string
	Search    string
	Limit     int
}

type PVCListResult struct {
	Items []copilotk8s.PVCSummary `json:"items"`
	Total int                     `json:"total"`
}

type HPAListOptions struct {
	Namespace string
	Search    string
	Limit     int
}

type HPAListResult struct {
	Items []copilotk8s.HPASummary `json:"items"`
	Total int                     `json:"total"`
}

type DaemonSetListOptions struct {
	Namespace string
	Search    string
	Limit     int
}

type DaemonSetListResult struct {
	Items []copilotk8s.DaemonSetSummary `json:"items"`
	Total int                           `json:"total"`
}

type StatefulSetListOptions struct {
	Namespace string
	Search    string
	Limit     int
}

type StatefulSetListResult struct {
	Items []copilotk8s.StatefulSetSummary `json:"items"`
	Total int                             `json:"total"`
}

type JobListOptions struct {
	Namespace string
	Status    string
	Search    string
	Limit     int
}

type JobListResult struct {
	Items []copilotk8s.JobSummary `json:"items"`
	Total int                     `json:"total"`
}

func parseNodeStatus(s string) string {
	if _, ok := validNodeStatuses[s]; !ok {
		return ""
	}
	return s
}

func parseNodeRole(s string) string {
	if _, ok := validNodeRoles[s]; !ok {
		return ""
	}
	return s
}

func parsePodPhase(s string) string {
	if _, ok := validPodPhases[s]; !ok {
		return ""
	}
	return s
}

func parseServiceType(s string) string {
	if _, ok := validServiceTypes[s]; !ok {
		return ""
	}
	return s
}

func parseEventType(s string) string {
	if _, ok := validEventTypes[s]; !ok {
		return ""
	}
	return s
}

func parsePVStatus(s string) string {
	if _, ok := validPVStatuses[s]; !ok {
		return ""
	}
	return s
}

func parsePVCStatus(s string) string {
	if _, ok := validPVCStatuses[s]; !ok {
		return ""
	}
	return s
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

func filterNodesByStatus(nodes []copilotk8s.NodeSummary, status string) []copilotk8s.NodeSummary {
	if status == "" {
		return nodes
	}
	filtered := make([]copilotk8s.NodeSummary, 0, len(nodes))
	for _, n := range nodes {
		if status == "ready" && n.Ready {
			filtered = append(filtered, n)
		} else if status == "notready" && !n.Ready {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

func filterNodesByRole(nodes []copilotk8s.NodeSummary, role string) []copilotk8s.NodeSummary {
	if role == "" {
		return nodes
	}
	filtered := make([]copilotk8s.NodeSummary, 0, len(nodes))
	for _, n := range nodes {
		for _, r := range n.Roles {
			if r == role {
				filtered = append(filtered, n)
				break
			}
		}
	}
	return filtered
}

func filterNodesBySearch(nodes []copilotk8s.NodeSummary, search string) []copilotk8s.NodeSummary {
	if search == "" {
		return nodes
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.NodeSummary, 0, len(nodes))
	for _, n := range nodes {
		if strings.Contains(strings.ToLower(n.Name), lower) {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

func filterPodsByPhase(pods []copilotk8s.PodSummary, phase string) []copilotk8s.PodSummary {
	if phase == "" {
		return pods
	}
	filtered := make([]copilotk8s.PodSummary, 0, len(pods))
	for _, p := range pods {
		if p.Phase == phase {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func filterPodsBySearch(pods []copilotk8s.PodSummary, search string) []copilotk8s.PodSummary {
	if search == "" {
		return pods
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.PodSummary, 0, len(pods))
	for _, p := range pods {
		if strings.Contains(strings.ToLower(p.Name), lower) || strings.Contains(strings.ToLower(p.Namespace), lower) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func filterDeploymentsBySearch(deployments []copilotk8s.DeploymentSummary, search string) []copilotk8s.DeploymentSummary {
	if search == "" {
		return deployments
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.DeploymentSummary, 0, len(deployments))
	for _, d := range deployments {
		if strings.Contains(strings.ToLower(d.Name), lower) || strings.Contains(strings.ToLower(d.Namespace), lower) {
			filtered = append(filtered, d)
		}
	}
	return filtered
}

func filterServicesByType(services []copilotk8s.ServiceSummary, svcType string) []copilotk8s.ServiceSummary {
	if svcType == "" {
		return services
	}
	filtered := make([]copilotk8s.ServiceSummary, 0, len(services))
	for _, s := range services {
		if s.Type == svcType {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func filterServicesBySearch(services []copilotk8s.ServiceSummary, search string) []copilotk8s.ServiceSummary {
	if search == "" {
		return services
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.ServiceSummary, 0, len(services))
	for _, s := range services {
		if strings.Contains(strings.ToLower(s.Name), lower) || strings.Contains(strings.ToLower(s.Namespace), lower) {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func filterIngressesBySearch(ingresses []copilotk8s.IngressSummary, search string) []copilotk8s.IngressSummary {
	if search == "" {
		return ingresses
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.IngressSummary, 0, len(ingresses))
	for _, ing := range ingresses {
		if strings.Contains(strings.ToLower(ing.Name), lower) || strings.Contains(strings.ToLower(ing.Namespace), lower) {
			filtered = append(filtered, ing)
			continue
		}
		for _, h := range ing.Hosts {
			if strings.Contains(strings.ToLower(h), lower) {
				filtered = append(filtered, ing)
				break
			}
		}
	}
	return filtered
}

func filterEventsByType(events []copilotk8s.EventSummary, eventType string) []copilotk8s.EventSummary {
	if eventType == "" {
		return events
	}
	filtered := make([]copilotk8s.EventSummary, 0, len(events))
	for _, e := range events {
		if e.Type == eventType {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func filterEventsBySearch(events []copilotk8s.EventSummary, search string) []copilotk8s.EventSummary {
	if search == "" {
		return events
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.EventSummary, 0, len(events))
	for _, e := range events {
		if strings.Contains(strings.ToLower(e.Message), lower) ||
			strings.Contains(strings.ToLower(e.Namespace), lower) ||
			strings.Contains(strings.ToLower(e.Reason), lower) ||
			strings.Contains(strings.ToLower(e.InvolvedName), lower) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func filterConfigMapsBySearch(configMaps []copilotk8s.ConfigMapSummary, search string) []copilotk8s.ConfigMapSummary {
	if search == "" {
		return configMaps
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.ConfigMapSummary, 0, len(configMaps))
	for _, cm := range configMaps {
		if strings.Contains(strings.ToLower(cm.Name), lower) || strings.Contains(strings.ToLower(cm.Namespace), lower) {
			filtered = append(filtered, cm)
		}
	}
	return filtered
}

func filterPVsByStatus(pvs []copilotk8s.PVSummary, status string) []copilotk8s.PVSummary {
	if status == "" {
		return pvs
	}
	filtered := make([]copilotk8s.PVSummary, 0, len(pvs))
	for _, pv := range pvs {
		if pv.Status == status {
			filtered = append(filtered, pv)
		}
	}
	return filtered
}

func filterPVsBySearch(pvs []copilotk8s.PVSummary, search string) []copilotk8s.PVSummary {
	if search == "" {
		return pvs
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.PVSummary, 0, len(pvs))
	for _, pv := range pvs {
		if strings.Contains(strings.ToLower(pv.Name), lower) ||
			strings.Contains(strings.ToLower(pv.ClaimRef), lower) ||
			strings.Contains(strings.ToLower(pv.StorageClass), lower) {
			filtered = append(filtered, pv)
		}
	}
	return filtered
}

func filterPVCsByStatus(pvcs []copilotk8s.PVCSummary, status string) []copilotk8s.PVCSummary {
	if status == "" {
		return pvcs
	}
	filtered := make([]copilotk8s.PVCSummary, 0, len(pvcs))
	for _, pvc := range pvcs {
		if pvc.Status == status {
			filtered = append(filtered, pvc)
		}
	}
	return filtered
}

func filterPVCsBySearch(pvcs []copilotk8s.PVCSummary, search string) []copilotk8s.PVCSummary {
	if search == "" {
		return pvcs
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.PVCSummary, 0, len(pvcs))
	for _, pvc := range pvcs {
		if strings.Contains(strings.ToLower(pvc.Name), lower) ||
			strings.Contains(strings.ToLower(pvc.Namespace), lower) ||
			strings.Contains(strings.ToLower(pvc.VolumeName), lower) ||
			strings.Contains(strings.ToLower(pvc.StorageClass), lower) {
			filtered = append(filtered, pvc)
		}
	}
	return filtered
}

func filterResourceQuotasBySearch(quotas []copilotk8s.ResourceQuotaSummary, search string) []copilotk8s.ResourceQuotaSummary {
	if search == "" {
		return quotas
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.ResourceQuotaSummary, 0, len(quotas))
	for _, q := range quotas {
		if strings.Contains(strings.ToLower(q.Name), lower) || strings.Contains(strings.ToLower(q.Namespace), lower) {
			filtered = append(filtered, q)
		}
	}
	return filtered
}

func filterLimitRangesBySearch(limitRanges []copilotk8s.LimitRangeSummary, search string) []copilotk8s.LimitRangeSummary {
	if search == "" {
		return limitRanges
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.LimitRangeSummary, 0, len(limitRanges))
	for _, lr := range limitRanges {
		if strings.Contains(strings.ToLower(lr.Name), lower) || strings.Contains(strings.ToLower(lr.Namespace), lower) {
			filtered = append(filtered, lr)
		}
	}
	return filtered
}

func filterHPAsBySearch(hpas []copilotk8s.HPASummary, search string) []copilotk8s.HPASummary {
	if search == "" {
		return hpas
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.HPASummary, 0, len(hpas))
	for _, hpa := range hpas {
		if strings.Contains(strings.ToLower(hpa.Name), lower) ||
			strings.Contains(strings.ToLower(hpa.Namespace), lower) ||
			strings.Contains(strings.ToLower(hpa.Reference), lower) {
			filtered = append(filtered, hpa)
		}
	}
	return filtered
}

func filterDaemonSetsBySearch(daemonSets []copilotk8s.DaemonSetSummary, search string) []copilotk8s.DaemonSetSummary {
	if search == "" {
		return daemonSets
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.DaemonSetSummary, 0, len(daemonSets))
	for _, ds := range daemonSets {
		if strings.Contains(strings.ToLower(ds.Name), lower) || strings.Contains(strings.ToLower(ds.Namespace), lower) {
			filtered = append(filtered, ds)
		}
	}
	return filtered
}

func filterStatefulSetsBySearch(statefulSets []copilotk8s.StatefulSetSummary, search string) []copilotk8s.StatefulSetSummary {
	if search == "" {
		return statefulSets
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.StatefulSetSummary, 0, len(statefulSets))
	for _, sts := range statefulSets {
		if strings.Contains(strings.ToLower(sts.Name), lower) || strings.Contains(strings.ToLower(sts.Namespace), lower) {
			filtered = append(filtered, sts)
		}
	}
	return filtered
}

func filterJobsByStatus(jobs []copilotk8s.JobSummary, status string) []copilotk8s.JobSummary {
	if status == "" {
		return jobs
	}
	filtered := make([]copilotk8s.JobSummary, 0, len(jobs))
	for _, job := range jobs {
		if job.Status == status {
			filtered = append(filtered, job)
		}
	}
	return filtered
}

func filterJobsBySearch(jobs []copilotk8s.JobSummary, search string) []copilotk8s.JobSummary {
	if search == "" {
		return jobs
	}
	lower := strings.ToLower(search)
	filtered := make([]copilotk8s.JobSummary, 0, len(jobs))
	for _, job := range jobs {
		if strings.Contains(strings.ToLower(job.Name), lower) || strings.Contains(strings.ToLower(job.Namespace), lower) {
			filtered = append(filtered, job)
		}
	}
	return filtered
}

func parseJobStatus(s string) string {
	if _, ok := validJobStatuses[s]; !ok {
		return ""
	}
	return s
}

func (s *Service) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.timeout)
}

func (s *Service) cacheKey(key string) string {
	if s.clusterName != "" && s.clusterName != "default" {
		return s.clusterName + ":" + key
	}
	return key
}

func (s *Service) cacheGet(ctx context.Context, key string, dest interface{}) bool {
	if s.cacheService == nil || !s.cacheService.Enabled() {
		return false
	}
	data, ok := s.cacheService.Get(ctx, s.cacheKey(key))
	if !ok {
		return false
	}
	if err := json.Unmarshal(data, dest); err != nil {
		zap.L().Warn("k8s cache unmarshal failed", zap.String("key", key), zap.Error(err))
		return false
	}
	return true
}

func (s *Service) cacheSet(ctx context.Context, key string, value interface{}, ttl time.Duration) {
	if s.cacheService == nil || !s.cacheService.Enabled() {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		zap.L().Warn("k8s cache marshal failed", zap.String("key", key), zap.Error(err))
		return
	}
	if err := s.cacheService.Set(ctx, s.cacheKey(key), data, ttl); err != nil {
		zap.L().Warn("k8s cache set failed", zap.String("key", key), zap.Error(err))
	}
}

func (s *Service) Overview(ctx context.Context) (*ClusterOverview, error) {
	var cached ClusterOverview
	if s.cacheGet(ctx, rediscache.K8sOverviewKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	overview := &ClusterOverview{
		CollectedAt: time.Now().UTC(),
	}

	if s.nodesEnabled {
		nodes, err := s.reader.ListAllNodes(ctx, copilotk8s.QueryOptions{})
		if err != nil {
			return nil, fmt.Errorf("list nodes for overview: %w", err)
		}
		overview.Nodes = buildNodeStats(nodes)
		overview.NodesAvailable = true
		names := make([]string, len(nodes))
		for i, n := range nodes {
			names[i] = n.Name
		}
		assoc, err := s.NodeHostAssociation(ctx, names)
		if err != nil {
			return nil, fmt.Errorf("node host association: %w", err)
		}
		overview.HostCoverage = buildHostCoverage(nodes, assoc)
	}

	pods, err := s.reader.ListAllPods(ctx, copilotk8s.QueryOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods for overview: %w", err)
	}
	overview.Pods = buildPodStats(pods)

	deployments, err := s.reader.ListAllDeployments(ctx, copilotk8s.QueryOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments for overview: %w", err)
	}
	overview.Deployments = buildDeploymentStats(deployments)

	events, err := s.reader.ListAllEvents(ctx, copilotk8s.EventQuery{})
	if err != nil {
		return nil, fmt.Errorf("list events for overview: %w", err)
	}
	overview.RecentEvents = takeLast(events, 10)

	s.cacheSet(ctx, rediscache.K8sOverviewKey, overview, s.cacheTTL)
	return overview, nil
}

func (s *Service) ListNodes(ctx context.Context, opts NodeListOptions) (*NodeListResult, error) {
	if !s.nodesEnabled {
		return nil, ErrNodesNotEnabled
	}

	cacheKey := rediscache.K8sNodesKey + ":" + opts.Status + ":" + opts.Role + ":" + opts.Search
	var cached NodeListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	nodes, err := s.reader.ListNodes(ctx, copilotk8s.QueryOptions{Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	nodes = filterNodesByStatus(nodes, parseNodeStatus(opts.Status))
	nodes = filterNodesByRole(nodes, parseNodeRole(opts.Role))
	nodes = filterNodesBySearch(nodes, opts.Search)

	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
	}
	assoc, err := s.NodeHostAssociation(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("node host association: %w", err)
	}

	items := make([]NodeWithHost, len(nodes))
	for i, n := range nodes {
		items[i] = NodeWithHost{
			Node:       n,
			HostOnline: assoc[n.Name].Online,
			LastScrape: assoc[n.Name].LastScrape,
		}
	}
	result := &NodeListResult{Items: items, Total: len(items)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) GetNodeDetail(ctx context.Context, name string) (*NodeDetail, error) {
	if !s.nodesEnabled {
		return nil, ErrNodesNotEnabled
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	nodes, err := s.reader.ListNodes(ctx, copilotk8s.QueryOptions{Limit: MaxLimit})
	if err != nil {
		return nil, fmt.Errorf("list nodes for detail: %w", err)
	}
	var target *copilotk8s.NodeSummary
	for i := range nodes {
		if nodes[i].Name == name {
			target = &nodes[i]
			break
		}
	}
	if target == nil {
		return nil, nil
	}

	var collectionErrors []string

	pods, podsErr := s.reader.ListPods(ctx, copilotk8s.QueryOptions{
		FieldSelector: "spec.nodeName=" + name,
		Limit:         MaxLimit,
	})
	if podsErr != nil {
		collectionErrors = append(collectionErrors, fmt.Sprintf("pods: %v", podsErr))
	}

	events, eventsErr := s.reader.ListEvents(ctx, copilotk8s.EventQuery{
		InvolvedKind: "Node",
		InvolvedName: name,
		Limit:        MaxLimit,
	})
	if eventsErr != nil {
		collectionErrors = append(collectionErrors, fmt.Sprintf("events: %v", eventsErr))
	}

	assoc, assocErr := s.NodeHostAssociation(ctx, []string{name})
	if assocErr != nil {
		collectionErrors = append(collectionErrors, fmt.Sprintf("host_association: %v", assocErr))
	}

	detail := &NodeDetail{
		Node:             *target,
		HostOnline:       assoc[name].Online,
		LastScrape:       assoc[name].LastScrape,
		Pods:             pods,
		Events:           events,
		CollectionErrors: collectionErrors,
	}
	if detail.Pods == nil {
		detail.Pods = []copilotk8s.PodSummary{}
	}
	if detail.Events == nil {
		detail.Events = []copilotk8s.EventSummary{}
	}
	return detail, nil
}

func (s *Service) ListPods(ctx context.Context, opts PodListOptions) (*PodListResult, error) {
	cacheKey := rediscache.K8sPodsKey + ":" + opts.Namespace + ":" + opts.Phase + ":" + opts.Search
	var cached PodListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	queryOpts := copilotk8s.QueryOptions{Limit: limit}
	if opts.Namespace != "" {
		queryOpts.Namespace = opts.Namespace
	}
	pods, err := s.reader.ListPods(ctx, queryOpts)
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	pods = filterPodsByPhase(pods, parsePodPhase(opts.Phase))
	pods = filterPodsBySearch(pods, opts.Search)
	result := &PodListResult{Items: pods, Total: len(pods)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListDeployments(ctx context.Context, opts DeploymentListOptions) (*DeploymentListResult, error) {
	cacheKey := rediscache.K8sDeploymentsKey + ":" + opts.Namespace + ":" + opts.Search
	var cached DeploymentListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	queryOpts := copilotk8s.QueryOptions{Limit: limit}
	if opts.Namespace != "" {
		queryOpts.Namespace = opts.Namespace
	}
	deployments, err := s.reader.ListDeployments(ctx, queryOpts)
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	deployments = filterDeploymentsBySearch(deployments, opts.Search)
	result := &DeploymentListResult{Items: deployments, Total: len(deployments)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListServices(ctx context.Context, opts ServiceListOptions) (*ServiceListResult, error) {
	cacheKey := rediscache.K8sServicesKey + ":" + opts.Namespace + ":" + opts.Type + ":" + opts.Search
	var cached ServiceListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	queryOpts := copilotk8s.QueryOptions{Limit: limit}
	if opts.Namespace != "" {
		queryOpts.Namespace = opts.Namespace
	}
	services, err := s.reader.ListServices(ctx, queryOpts)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	services = filterServicesByType(services, parseServiceType(opts.Type))
	services = filterServicesBySearch(services, opts.Search)
	result := &ServiceListResult{Items: services, Total: len(services)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListIngresses(ctx context.Context, opts IngressListOptions) (*IngressListResult, error) {
	cacheKey := rediscache.K8sIngressesKey + ":" + opts.Namespace + ":" + opts.Search
	var cached IngressListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	queryOpts := copilotk8s.QueryOptions{Limit: limit}
	if opts.Namespace != "" {
		queryOpts.Namespace = opts.Namespace
	}
	ingresses, err := s.reader.ListIngresses(ctx, queryOpts)
	if err != nil {
		return nil, fmt.Errorf("list ingresses: %w", err)
	}
	ingresses = filterIngressesBySearch(ingresses, opts.Search)
	result := &IngressListResult{Items: ingresses, Total: len(ingresses)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListEvents(ctx context.Context, opts EventListOptions) (*EventListResult, error) {
	cacheKey := rediscache.K8sEventsKey + ":" + opts.Namespace + ":" + opts.Type + ":" + opts.Search
	var cached EventListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	query := copilotk8s.EventQuery{Limit: limit}
	if opts.Namespace != "" {
		query.Namespace = opts.Namespace
	}
	events, err := s.reader.ListEvents(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	events = filterEventsByType(events, parseEventType(opts.Type))
	events = filterEventsBySearch(events, opts.Search)
	result := &EventListResult{Items: events, Total: len(events)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListConfigMaps(ctx context.Context, opts ConfigMapListOptions) (*ConfigMapListResult, error) {
	cacheKey := rediscache.K8sConfigMapsKey + ":" + opts.Namespace + ":" + opts.Search
	var cached ConfigMapListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	queryOpts := copilotk8s.QueryOptions{Limit: limit}
	if opts.Namespace != "" {
		queryOpts.Namespace = opts.Namespace
	}
	configMaps, err := s.reader.ListConfigMaps(ctx, queryOpts)
	if err != nil {
		return nil, fmt.Errorf("list configmaps: %w", err)
	}
	configMaps = filterConfigMapsBySearch(configMaps, opts.Search)
	result := &ConfigMapListResult{Items: configMaps, Total: len(configMaps)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListResourceQuotas(ctx context.Context, opts ResourceQuotaListOptions) (*ResourceQuotaListResult, error) {
	cacheKey := rediscache.K8sResourceQuotasKey + ":" + opts.Namespace + ":" + opts.Search
	var cached ResourceQuotaListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	queryOpts := copilotk8s.QueryOptions{Limit: limit}
	if opts.Namespace != "" {
		queryOpts.Namespace = opts.Namespace
	}
	quotas, err := s.reader.ListResourceQuotas(ctx, queryOpts)
	if err != nil {
		return nil, fmt.Errorf("list resource quotas: %w", err)
	}
	quotas = filterResourceQuotasBySearch(quotas, opts.Search)
	result := &ResourceQuotaListResult{Items: quotas, Total: len(quotas)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListLimitRanges(ctx context.Context, opts LimitRangeListOptions) (*LimitRangeListResult, error) {
	cacheKey := rediscache.K8sLimitRangesKey + ":" + opts.Namespace + ":" + opts.Search
	var cached LimitRangeListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	queryOpts := copilotk8s.QueryOptions{Limit: limit}
	if opts.Namespace != "" {
		queryOpts.Namespace = opts.Namespace
	}
	limitRanges, err := s.reader.ListLimitRanges(ctx, queryOpts)
	if err != nil {
		return nil, fmt.Errorf("list limit ranges: %w", err)
	}
	limitRanges = filterLimitRangesBySearch(limitRanges, opts.Search)
	result := &LimitRangeListResult{Items: limitRanges, Total: len(limitRanges)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListPVs(ctx context.Context, opts PVListOptions) (*PVListResult, error) {
	cacheKey := rediscache.K8sPVsKey + ":" + opts.Status + ":" + opts.Search
	var cached PVListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	pvs, err := s.reader.ListPersistentVolumes(ctx, copilotk8s.QueryOptions{Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list persistent volumes: %w", err)
	}
	pvs = filterPVsByStatus(pvs, parsePVStatus(opts.Status))
	pvs = filterPVsBySearch(pvs, opts.Search)
	result := &PVListResult{Items: pvs, Total: len(pvs)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListPVCs(ctx context.Context, opts PVCListOptions) (*PVCListResult, error) {
	cacheKey := rediscache.K8sPVCsKey + ":" + opts.Namespace + ":" + opts.Status + ":" + opts.Search
	var cached PVCListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	queryOpts := copilotk8s.QueryOptions{Limit: limit}
	if opts.Namespace != "" {
		queryOpts.Namespace = opts.Namespace
	}
	pvcs, err := s.reader.ListPersistentVolumeClaims(ctx, queryOpts)
	if err != nil {
		return nil, fmt.Errorf("list persistent volume claims: %w", err)
	}
	pvcs = filterPVCsByStatus(pvcs, parsePVCStatus(opts.Status))
	pvcs = filterPVCsBySearch(pvcs, opts.Search)
	result := &PVCListResult{Items: pvcs, Total: len(pvcs)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListHPAs(ctx context.Context, opts HPAListOptions) (*HPAListResult, error) {
	cacheKey := rediscache.K8sHPAsKey + ":" + opts.Namespace + ":" + opts.Search
	var cached HPAListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	queryOpts := copilotk8s.QueryOptions{Limit: limit}
	if opts.Namespace != "" {
		queryOpts.Namespace = opts.Namespace
	}
	hpas, err := s.reader.ListHorizontalPodAutoscalers(ctx, queryOpts)
	if err != nil {
		return nil, fmt.Errorf("list horizontal pod autoscalers: %w", err)
	}
	hpas = filterHPAsBySearch(hpas, opts.Search)
	result := &HPAListResult{Items: hpas, Total: len(hpas)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListDaemonSets(ctx context.Context, opts DaemonSetListOptions) (*DaemonSetListResult, error) {
	cacheKey := rediscache.K8sDaemonSetsKey + ":" + opts.Namespace + ":" + opts.Search
	var cached DaemonSetListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	queryOpts := copilotk8s.QueryOptions{Limit: limit}
	if opts.Namespace != "" {
		queryOpts.Namespace = opts.Namespace
	}
	daemonSets, err := s.reader.ListDaemonSets(ctx, queryOpts)
	if err != nil {
		return nil, fmt.Errorf("list daemon sets: %w", err)
	}
	daemonSets = filterDaemonSetsBySearch(daemonSets, opts.Search)
	result := &DaemonSetListResult{Items: daemonSets, Total: len(daemonSets)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListStatefulSets(ctx context.Context, opts StatefulSetListOptions) (*StatefulSetListResult, error) {
	cacheKey := rediscache.K8sStatefulSetsKey + ":" + opts.Namespace + ":" + opts.Search
	var cached StatefulSetListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	queryOpts := copilotk8s.QueryOptions{Limit: limit}
	if opts.Namespace != "" {
		queryOpts.Namespace = opts.Namespace
	}
	statefulSets, err := s.reader.ListStatefulSets(ctx, queryOpts)
	if err != nil {
		return nil, fmt.Errorf("list stateful sets: %w", err)
	}
	statefulSets = filterStatefulSetsBySearch(statefulSets, opts.Search)
	result := &StatefulSetListResult{Items: statefulSets, Total: len(statefulSets)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) ListJobs(ctx context.Context, opts JobListOptions) (*JobListResult, error) {
	cacheKey := rediscache.K8sJobsKey + ":" + opts.Namespace + ":" + opts.Status + ":" + opts.Search
	var cached JobListResult
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	limit := normalizeLimit(opts.Limit)
	queryOpts := copilotk8s.QueryOptions{Limit: limit}
	if opts.Namespace != "" {
		queryOpts.Namespace = opts.Namespace
	}
	jobs, err := s.reader.ListJobs(ctx, queryOpts)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	jobs = filterJobsByStatus(jobs, parseJobStatus(opts.Status))
	jobs = filterJobsBySearch(jobs, opts.Search)
	result := &JobListResult{Items: jobs, Total: len(jobs)}
	s.cacheSet(ctx, cacheKey, result, s.listCacheTTL)
	return result, nil
}

func (s *Service) GetPodLogs(ctx context.Context, query copilotk8s.LogQuery) (*copilotk8s.LogSnippet, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	snippet, err := s.reader.GetPodLogs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get pod logs: %w", err)
	}
	return &snippet, nil
}

func buildNodeStats(nodes []copilotk8s.NodeSummary) NodeStats {
	stats := NodeStats{Total: len(nodes)}
	for _, n := range nodes {
		if n.Ready {
			stats.Ready++
		} else {
			stats.NotReady++
		}
	}
	return stats
}

func buildPodStats(pods []copilotk8s.PodSummary) PodStats {
	stats := PodStats{Total: len(pods)}
	for _, p := range pods {
		switch p.Phase {
		case "Running":
			stats.Running++
		case "Pending":
			stats.Pending++
		case "Failed":
			stats.Failed++
		case "Succeeded":
			stats.Succeeded++
		}
	}
	return stats
}

func buildDeploymentStats(deployments []copilotk8s.DeploymentSummary) DeploymentStats {
	stats := DeploymentStats{Total: len(deployments)}
	for _, d := range deployments {
		if d.AvailableReplicas >= d.Replicas {
			stats.Available++
		} else {
			stats.Unavailable++
		}
	}
	return stats
}

func buildHostCoverage(nodes []copilotk8s.NodeSummary, assoc map[string]HostAssociation) HostCoverageStats {
	stats := HostCoverageStats{TotalNodes: len(nodes)}
	for _, n := range nodes {
		if a, ok := assoc[n.Name]; ok && a.Online {
			stats.CoveredNodes++
		} else {
			stats.UncoveredNodes++
		}
	}
	return stats
}

func takeLast(events []copilotk8s.EventSummary, n int) []copilotk8s.EventSummary {
	if len(events) <= n {
		return events
	}
	return events[len(events)-n:]
}

func (s *Service) GetResourceYAML(ctx context.Context, kind, namespace, name string) (string, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	result, err := s.reader.GetResourceYAML(ctx, kind, namespace, name)
	if err != nil {
		return "", fmt.Errorf("get resource yaml: %w", err)
	}
	return result, nil
}

func (s *Service) Topology(ctx context.Context, namespace string) (*copilotk8s.TopologyData, error) {
	cacheKey := rediscache.K8sOverviewKey + ":topology:" + namespace
	var cached copilotk8s.TopologyData
	if s.cacheGet(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	data := &copilotk8s.TopologyData{}

	if s.nodesEnabled {
		nodes, err := s.reader.ListNodes(ctx, copilotk8s.QueryOptions{Limit: MaxLimit})
		if err != nil {
			return nil, fmt.Errorf("list nodes for topology: %w", err)
		}
		for _, n := range nodes {
			status := "NotReady"
			if n.Ready {
				status = "Ready"
			}
			data.Nodes = append(data.Nodes, copilotk8s.TopologyNode{
				ID:         "node/" + n.Name,
				Kind:       "Node",
				Name:       n.Name,
				Status:     status,
				DetailPath: "/k8s/nodes/" + n.Name,
			})
		}
	}

	queryOpts := copilotk8s.QueryOptions{Limit: MaxLimit}
	if namespace != "" {
		queryOpts.Namespace = namespace
	}

	deployments, err := s.reader.ListDeployments(ctx, queryOpts)
	if err != nil {
		zap.L().Warn("topology: list deployments failed", zap.Error(err))
	}
	for _, d := range deployments {
		status := "Unavailable"
		if d.AvailableReplicas >= d.Replicas {
			status = "Available"
		}
		data.Nodes = append(data.Nodes, copilotk8s.TopologyNode{
			ID:         "deployment/" + d.Namespace + "/" + d.Name,
			Kind:       "Deployment",
			Name:       d.Name,
			Namespace:  d.Namespace,
			Status:     status,
			DetailPath: "/k8s/workloads",
		})
	}

	pods, err := s.reader.ListPods(ctx, queryOpts)
	if err != nil {
		zap.L().Warn("topology: list pods failed", zap.Error(err))
	}
	for _, p := range pods {
		status := p.Phase
		data.Nodes = append(data.Nodes, copilotk8s.TopologyNode{
			ID:         "pod/" + p.Namespace + "/" + p.Name,
			Kind:       "Pod",
			Name:       p.Name,
			Namespace:  p.Namespace,
			Status:     status,
			DetailPath: "/k8s/workloads",
		})
		if p.NodeName != "" && s.nodesEnabled {
			data.Edges = append(data.Edges, copilotk8s.TopologyEdge{
				Source: "node/" + p.NodeName,
				Target: "pod/" + p.Namespace + "/" + p.Name,
				Type:   "scheduled",
			})
		}
		if p.OwnerKind != "" && p.OwnerName != "" {
			ownerID := strings.ToLower(p.OwnerKind) + "/" + p.Namespace + "/" + p.OwnerName
			data.Edges = append(data.Edges, copilotk8s.TopologyEdge{
				Source: ownerID,
				Target: "pod/" + p.Namespace + "/" + p.Name,
				Type:   "owns",
			})
		}
	}

	services, err := s.reader.ListServices(ctx, queryOpts)
	if err != nil {
		zap.L().Warn("topology: list services failed", zap.Error(err))
	}
	podsByNamespace := make(map[string][]copilotk8s.PodSummary)
	for _, p := range pods {
		podsByNamespace[p.Namespace] = append(podsByNamespace[p.Namespace], p)
	}
	for _, svc := range services {
		data.Nodes = append(data.Nodes, copilotk8s.TopologyNode{
			ID:         "service/" + svc.Namespace + "/" + svc.Name,
			Kind:       "Service",
			Name:       svc.Name,
			Namespace:  svc.Namespace,
			Status:     svc.Type,
			DetailPath: "/k8s/services",
		})
		if len(svc.Selector) > 0 {
			nsPods := podsByNamespace[svc.Namespace]
			for _, p := range nsPods {
				if labelsMatchSelector(p.Labels, svc.Selector) {
					data.Edges = append(data.Edges, copilotk8s.TopologyEdge{
						Source: "service/" + svc.Namespace + "/" + svc.Name,
						Target: "pod/" + p.Namespace + "/" + p.Name,
						Type:   "selects",
					})
				}
			}
		}
	}

	if len(data.Nodes) > 200 {
		sortTopologyNodes(data.Nodes)
		data.Nodes = data.Nodes[:200]
		data.Edges = filterEdgesForNodes(data.Edges, data.Nodes)
	}

	s.cacheSet(ctx, cacheKey, data, s.cacheTTL)
	return data, nil
}

var topologyKindPriority = map[string]int{
	"Node":       1,
	"Deployment": 2,
	"Service":    3,
	"Pod":        4,
}

func sortTopologyNodes(nodes []copilotk8s.TopologyNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		pi, oki := topologyKindPriority[nodes[i].Kind]
		pj, okj := topologyKindPriority[nodes[j].Kind]
		if !oki {
			pi = 99
		}
		if !okj {
			pj = 99
		}
		if pi != pj {
			return pi < pj
		}
		return nodes[i].ID < nodes[j].ID
	})
}

func filterEdgesForNodes(edges []copilotk8s.TopologyEdge, nodes []copilotk8s.TopologyNode) []copilotk8s.TopologyEdge {
	nodeSet := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		nodeSet[n.ID] = struct{}{}
	}
	filtered := make([]copilotk8s.TopologyEdge, 0, len(edges))
	for _, e := range edges {
		if _, ok := nodeSet[e.Source]; ok {
			if _, ok2 := nodeSet[e.Target]; ok2 {
				filtered = append(filtered, e)
			}
		}
	}
	return filtered
}

func labelsMatchSelector(labels, selector map[string]string) bool {
	for k, v := range selector {
		if lv, ok := labels[k]; !ok || lv != v {
			return false
		}
	}
	return true
}
