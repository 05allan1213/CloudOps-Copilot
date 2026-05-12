package k8s

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var (
	ErrDisabled             = errors.New("k8s integration disabled")
	ErrInvalidArgument      = errors.New("invalid k8s argument")
	ErrNamespaceNotAllowed  = errors.New("k8s namespace is not allowed")
	dnsLabelPattern         = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	labelSelectorSafeRegexp = regexp.MustCompile(`^[A-Za-z0-9_.\-/=!,() ]*$`)
)

type Reader interface {
	ListPods(ctx context.Context, options QueryOptions) ([]PodSummary, error)
	ListDeployments(ctx context.Context, options QueryOptions) ([]DeploymentSummary, error)
	ListServices(ctx context.Context, options QueryOptions) ([]ServiceSummary, error)
	ListNodes(ctx context.Context, options QueryOptions) ([]NodeSummary, error)
	ListEvents(ctx context.Context, query EventQuery) ([]EventSummary, error)
	GetPodLogs(ctx context.Context, query LogQuery) (LogSnippet, error)
}

type Service struct {
	client            kubernetes.Interface
	enabled           bool
	allowedNamespaces map[string]struct{}
	defaultNamespace  string
	requestTimeout    time.Duration
	logTailLines      int
	logMaxBytes       int
	eventLimit        int
	now               func() time.Time
}

func NewServiceWithClient(client kubernetes.Interface, cfg Config) *Service {
	defaultNamespace := strings.TrimSpace(cfg.DefaultNamespace)
	if defaultNamespace == "" {
		defaultNamespace = DefaultNamespace
	}
	allowed := normalizeAllowedNamespaces(cfg.AllowedNamespaces, defaultNamespace)
	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = 10 * time.Second
	}
	logTailLines := cfg.LogTailLines
	if logTailLines <= 0 {
		logTailLines = 100
	}
	logMaxBytes := cfg.LogMaxBytes
	if logMaxBytes <= 0 {
		logMaxBytes = 32768
	}
	eventLimit := cfg.EventLimit
	if eventLimit <= 0 {
		eventLimit = 50
	}
	return &Service{
		client:            client,
		enabled:           cfg.Enabled,
		allowedNamespaces: allowed,
		defaultNamespace:  defaultNamespace,
		requestTimeout:    requestTimeout,
		logTailLines:      logTailLines,
		logMaxBytes:       logMaxBytes,
		eventLimit:        eventLimit,
		now:               func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) ListPods(ctx context.Context, options QueryOptions) ([]PodSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.CoreV1().Pods(options.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		FieldSelector: options.FieldSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	pods := make([]PodSummary, 0, len(list.Items))
	for _, pod := range list.Items {
		pods = append(pods, podSummary(pod, collectedAt))
	}
	return pods, nil
}

func (s *Service) ListDeployments(ctx context.Context, options QueryOptions) ([]DeploymentSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	collectedAt := s.now()
	if options.Name != "" {
		item, err := s.client.AppsV1().Deployments(options.Namespace).Get(ctx, options.Name, metav1.GetOptions{})
		if err != nil {
			return nil, classifyError(err)
		}
		return []DeploymentSummary{deploymentSummary(*item, collectedAt)}, nil
	}
	list, err := s.client.AppsV1().Deployments(options.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		FieldSelector: options.FieldSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	deployments := make([]DeploymentSummary, 0, len(list.Items))
	for _, deployment := range list.Items {
		deployments = append(deployments, deploymentSummary(deployment, collectedAt))
	}
	return deployments, nil
}

func (s *Service) ListServices(ctx context.Context, options QueryOptions) ([]ServiceSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.CoreV1().Services(options.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		FieldSelector: options.FieldSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	services := make([]ServiceSummary, 0, len(list.Items))
	for _, service := range list.Items {
		services = append(services, serviceSummary(service, collectedAt))
	}
	return services, nil
}

func (s *Service) ListNodes(ctx context.Context, options QueryOptions) ([]NodeSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options = normalizeLimit(options)
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		FieldSelector: options.FieldSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	nodes := make([]NodeSummary, 0, len(list.Items))
	for _, node := range list.Items {
		nodes = append(nodes, nodeSummary(node, collectedAt))
	}
	return nodes, nil
}

func (s *Service) ListEvents(ctx context.Context, query EventQuery) ([]EventSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	namespace, err := s.normalizeNamespace(query.Namespace)
	if err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = s.eventLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{Limit: int64(limit)})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	events := make([]EventSummary, 0, len(list.Items))
	for _, event := range list.Items {
		if query.InvolvedKind != "" && !strings.EqualFold(event.InvolvedObject.Kind, query.InvolvedKind) {
			continue
		}
		if query.InvolvedName != "" && event.InvolvedObject.Name != query.InvolvedName {
			continue
		}
		events = append(events, eventSummary(event, collectedAt))
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].LastSeen.After(events[j].LastSeen)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func (s *Service) GetPodLogs(ctx context.Context, query LogQuery) (LogSnippet, error) {
	if err := s.ready(); err != nil {
		return LogSnippet{}, err
	}
	namespace, err := s.normalizeNamespace(query.Namespace)
	if err != nil {
		return LogSnippet{}, err
	}
	podName := strings.TrimSpace(query.PodName)
	if err := ValidateName("pod_name", podName); err != nil {
		return LogSnippet{}, err
	}
	tailLines := query.TailLines
	if tailLines <= 0 {
		tailLines = s.logTailLines
	}
	if tailLines > 1000 {
		return LogSnippet{}, fmt.Errorf("%w: tail_lines must be in range 1-1000", ErrInvalidArgument)
	}
	tail := int64(tailLines)
	limit := int64(s.logMaxBytes)
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	req := s.client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container:  strings.TrimSpace(query.Container),
		TailLines:  &tail,
		LimitBytes: &limit,
	})
	body, err := req.Stream(ctx)
	if err != nil {
		return LogSnippet{}, classifyError(err)
	}
	defer body.Close()
	content, readErr := io.ReadAll(io.LimitReader(body, int64(s.logMaxBytes)+1))
	if readErr != nil {
		return LogSnippet{}, readErr
	}
	text, truncated := SanitizeText(string(content), s.logMaxBytes)
	lines := splitLogLines(text)
	return LogSnippet{Namespace: namespace, PodName: podName, Container: strings.TrimSpace(query.Container), Lines: lines, Truncated: truncated, CollectedAt: s.now()}, nil
}

func (s *Service) ready() error {
	if s == nil || !s.enabled {
		return ErrDisabled
	}
	if s.client == nil {
		return fmt.Errorf("%w: k8s client is nil", ErrDisabled)
	}
	return nil
}

func (s *Service) normalizeQuery(options QueryOptions) (QueryOptions, error) {
	namespace, err := s.normalizeNamespace(options.Namespace)
	if err != nil {
		return QueryOptions{}, err
	}
	options.Namespace = namespace
	options = normalizeLimit(options)
	if options.Name != "" {
		if err := ValidateName("name", options.Name); err != nil {
			return QueryOptions{}, err
		}
	}
	if options.LabelSelector != "" && !labelSelectorSafeRegexp.MatchString(options.LabelSelector) {
		return QueryOptions{}, fmt.Errorf("%w: label_selector contains invalid characters", ErrInvalidArgument)
	}
	if options.FieldSelector != "" && !strings.HasPrefix(options.FieldSelector, "metadata.name=") && !strings.HasPrefix(options.FieldSelector, "status.phase=") {
		return QueryOptions{}, fmt.Errorf("%w: field_selector is not allowed", ErrInvalidArgument)
	}
	return options, nil
}

func (s *Service) normalizeNamespace(namespace string) (string, error) {
	value := strings.TrimSpace(namespace)
	if value == "" {
		value = s.defaultNamespace
	}
	if err := ValidateName("namespace", value); err != nil {
		return "", err
	}
	if _, ok := s.allowedNamespaces[value]; !ok {
		return "", fmt.Errorf("%w: %s", ErrNamespaceNotAllowed, value)
	}
	return value, nil
}

func normalizeLimit(options QueryOptions) QueryOptions {
	if options.Limit <= 0 {
		options.Limit = DefaultLimit
	}
	if options.Limit > MaxLimit {
		options.Limit = MaxLimit
	}
	return options
}

func ValidateName(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidArgument, field)
	}
	if !dnsLabelPattern.MatchString(value) || len(value) > 253 {
		return fmt.Errorf("%w: %s contains invalid characters", ErrInvalidArgument, field)
	}
	return nil
}

func normalizeAllowedNamespaces(values []string, fallback string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(values)+1)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			allowed[value] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		allowed[fallback] = struct{}{}
	}
	return allowed
}

func podSummary(pod corev1.Pod, collectedAt time.Time) PodSummary {
	ready := 0
	var restarts int32
	for _, status := range pod.Status.ContainerStatuses {
		if status.Ready {
			ready++
		}
		restarts += status.RestartCount
	}
	summary := PodSummary{
		Namespace:       pod.Namespace,
		Name:            pod.Name,
		Phase:           string(pod.Status.Phase),
		ReadyContainers: ready,
		TotalContainers: len(pod.Spec.Containers),
		RestartCount:    restarts,
		NodeName:        pod.Spec.NodeName,
		PodIP:           pod.Status.PodIP,
		CollectedAt:     collectedAt,
	}
	if pod.Status.StartTime != nil {
		summary.StartTime = pod.Status.StartTime.Time.UTC()
	}
	if len(pod.OwnerReferences) > 0 {
		summary.OwnerKind = pod.OwnerReferences[0].Kind
		summary.OwnerName = pod.OwnerReferences[0].Name
	}
	return summary
}

func deploymentSummary(deployment appsv1.Deployment, collectedAt time.Time) DeploymentSummary {
	replicas := int32(0)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}
	conditions := make([]DeploymentCondition, 0, len(deployment.Status.Conditions))
	for _, condition := range deployment.Status.Conditions {
		conditions = append(conditions, DeploymentCondition{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: sanitizeMessage(condition.Message, 512),
		})
	}
	return DeploymentSummary{
		Namespace:         deployment.Namespace,
		Name:              deployment.Name,
		Replicas:          replicas,
		ReadyReplicas:     deployment.Status.ReadyReplicas,
		UpdatedReplicas:   deployment.Status.UpdatedReplicas,
		AvailableReplicas: deployment.Status.AvailableReplicas,
		Strategy:          string(deployment.Spec.Strategy.Type),
		Conditions:        conditions,
		CollectedAt:       collectedAt,
	}
}

func serviceSummary(service corev1.Service, collectedAt time.Time) ServiceSummary {
	ports := make([]ServicePort, 0, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		ports = append(ports, ServicePort{
			Name:       port.Name,
			Protocol:   string(port.Protocol),
			Port:       port.Port,
			TargetPort: port.TargetPort.String(),
		})
	}
	return ServiceSummary{Namespace: service.Namespace, Name: service.Name, Type: string(service.Spec.Type), ClusterIP: service.Spec.ClusterIP, Ports: ports, CollectedAt: collectedAt}
}

func nodeSummary(node corev1.Node, collectedAt time.Time) NodeSummary {
	roles := make([]string, 0)
	for key := range node.Labels {
		if strings.HasPrefix(key, "node-role.kubernetes.io/") {
			role := strings.TrimPrefix(key, "node-role.kubernetes.io/")
			if role != "" {
				roles = append(roles, role)
			}
		}
	}
	sort.Strings(roles)
	conditions := make([]NodeCondition, 0, len(node.Status.Conditions))
	ready := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			ready = true
		}
		conditions = append(conditions, NodeCondition{Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason, Message: sanitizeMessage(condition.Message, 512)})
	}
	return NodeSummary{
		Name:           node.Name,
		Ready:          ready,
		Roles:          roles,
		KubeletVersion: node.Status.NodeInfo.KubeletVersion,
		Capacity:       ResourceSummary{CPU: node.Status.Capacity.Cpu().String(), Memory: node.Status.Capacity.Memory().String()},
		Conditions:     conditions,
		CollectedAt:    collectedAt,
	}
}

func eventSummary(event corev1.Event, collectedAt time.Time) EventSummary {
	lastSeen := event.LastTimestamp.Time
	if lastSeen.IsZero() {
		lastSeen = event.EventTime.Time
	}
	if lastSeen.IsZero() {
		lastSeen = event.CreationTimestamp.Time
	}
	return EventSummary{
		Namespace:    event.Namespace,
		Name:         event.Name,
		Type:         event.Type,
		Reason:       event.Reason,
		Message:      sanitizeMessage(event.Message, 512),
		InvolvedKind: event.InvolvedObject.Kind,
		InvolvedName: event.InvolvedObject.Name,
		Count:        event.Count,
		LastSeen:     lastSeen.UTC(),
		CollectedAt:  collectedAt,
	}
}

func splitLogLines(text string) []string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	lines := make([]string, 0)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

func classifyError(err error) error {
	switch {
	case apierrors.IsNotFound(err):
		return fmt.Errorf("k8s resource not found: %w", err)
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return fmt.Errorf("k8s permission denied: %w", err)
	default:
		return err
	}
}
