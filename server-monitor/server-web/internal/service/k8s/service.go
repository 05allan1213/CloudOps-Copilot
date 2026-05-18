package k8s

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	copilotk8s "server-web/internal/copilot/k8s"
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

type K8sReader interface {
	ListPods(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.PodSummary, error)
	ListDeployments(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.DeploymentSummary, error)
	ListServices(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.ServiceSummary, error)
	ListNodes(ctx context.Context, options copilotk8s.QueryOptions) ([]copilotk8s.NodeSummary, error)
	ListEvents(ctx context.Context, query copilotk8s.EventQuery) ([]copilotk8s.EventSummary, error)
	GetPodLogs(ctx context.Context, query copilotk8s.LogQuery) (copilotk8s.LogSnippet, error)
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
}

type Options struct {
	RequestTimeout time.Duration
	NodesEnabled   bool
}

func NewService(reader K8sReader, promClient PrometheusClient, opts Options) *Service {
	timeout := opts.RequestTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Service{
		reader:       reader,
		promClient:   promClient,
		timeout:      timeout,
		nodesEnabled: opts.NodesEnabled,
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

type EventListOptions struct {
	Namespace string
	Type      string
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

type EventListResult struct {
	Items []copilotk8s.EventSummary `json:"items"`
	Total int                       `json:"total"`
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

func (s *Service) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.timeout)
}

func (s *Service) Overview(ctx context.Context) (*ClusterOverview, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	overview := &ClusterOverview{
		CollectedAt: time.Now().UTC(),
	}

	if s.nodesEnabled {
		nodes, err := s.reader.ListNodes(ctx, copilotk8s.QueryOptions{Limit: MaxLimit})
		if err != nil {
			return nil, fmt.Errorf("list nodes for overview: %w", err)
		}
		overview.Nodes = buildNodeStats(nodes)
		overview.NodesAvailable = true
		if len(nodes) >= MaxLimit {
			overview.Truncated = true
		}
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

	pods, err := s.reader.ListPods(ctx, copilotk8s.QueryOptions{Limit: MaxLimit})
	if err != nil {
		return nil, fmt.Errorf("list pods for overview: %w", err)
	}
	overview.Pods = buildPodStats(pods)
	if len(pods) >= MaxLimit {
		overview.Truncated = true
	}

	deployments, err := s.reader.ListDeployments(ctx, copilotk8s.QueryOptions{Limit: MaxLimit})
	if err != nil {
		return nil, fmt.Errorf("list deployments for overview: %w", err)
	}
	overview.Deployments = buildDeploymentStats(deployments)
	if len(deployments) >= MaxLimit {
		overview.Truncated = true
	}

	events, err := s.reader.ListEvents(ctx, copilotk8s.EventQuery{Limit: MaxLimit})
	if err != nil {
		return nil, fmt.Errorf("list events for overview: %w", err)
	}
	if len(events) >= MaxLimit {
		overview.Truncated = true
	}
	overview.RecentEvents = takeLast(events, 10)

	return overview, nil
}

func (s *Service) ListNodes(ctx context.Context, opts NodeListOptions) (*NodeListResult, error) {
	if !s.nodesEnabled {
		return nil, ErrNodesNotEnabled
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
	return &NodeListResult{Items: items, Total: len(items)}, nil
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
	return &PodListResult{Items: pods, Total: len(pods)}, nil
}

func (s *Service) ListDeployments(ctx context.Context, opts DeploymentListOptions) (*DeploymentListResult, error) {
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
	return &DeploymentListResult{Items: deployments, Total: len(deployments)}, nil
}

func (s *Service) ListServices(ctx context.Context, opts ServiceListOptions) (*ServiceListResult, error) {
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
	return &ServiceListResult{Items: services, Total: len(services)}, nil
}

func (s *Service) ListEvents(ctx context.Context, opts EventListOptions) (*EventListResult, error) {
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
	return &EventListResult{Items: events, Total: len(events)}, nil
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
