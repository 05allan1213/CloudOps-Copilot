// Package k8sread provides the bounded, namespace-allowlisted Kubernetes reads
// used by Agent tools and the incident-scoped Workbench resource view.
package k8sread

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
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

var (
	ErrDisabled             = errors.New("k8s read integration disabled")
	ErrInvalidArgument      = errors.New("invalid k8s read argument")
	ErrNamespaceNotAllowed  = errors.New("k8s namespace is not allowed")
	dnsLabelPattern         = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	labelSelectorSafeRegexp = regexp.MustCompile(`^[A-Za-z0-9_.\-/=!,() ]*$`)
)

type Reader interface {
	ListPods(context.Context, QueryOptions) ([]PodSummary, error)
	ListDeployments(context.Context, QueryOptions) ([]DeploymentSummary, error)
	ListServices(context.Context, QueryOptions) ([]ServiceSummary, error)
	ListEvents(context.Context, EventQuery) ([]EventSummary, error)
	GetPodLogs(context.Context, LogQuery) (LogSnippet, error)
}

type Service struct {
	client             kubernetes.Interface
	enabled            bool
	allowedNamespaces  map[string]struct{}
	allowAllNamespaces bool
	defaultNamespace   string
	requestTimeout     time.Duration
	logTailLines       int
	logMaxBytes        int
	eventLimit         int
	now                func() time.Time
}

func NewServiceWithClient(client kubernetes.Interface, cfg Config) *Service {
	defaultNamespace := strings.TrimSpace(cfg.DefaultNamespace)
	if defaultNamespace == "" {
		defaultNamespace = DefaultNamespace
	}
	allowed, allowAll := normalizeAllowedNamespaces(cfg.AllowedNamespaces, defaultNamespace)
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
	return &Service{client: client, enabled: cfg.Enabled, allowedNamespaces: allowed, allowAllNamespaces: allowAll, defaultNamespace: defaultNamespace, requestTimeout: requestTimeout, logTailLines: logTailLines, logMaxBytes: logMaxBytes, eventLimit: eventLimit, now: func() time.Time { return time.Now().UTC() }}
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
	list, err := s.client.CoreV1().Pods(options.Namespace).List(ctx, metav1.ListOptions{LabelSelector: options.LabelSelector, FieldSelector: options.FieldSelector, Limit: int64(options.Limit)})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	result := make([]PodSummary, 0, len(list.Items))
	for _, item := range list.Items {
		result = append(result, podSummary(item, collectedAt))
	}
	return result, nil
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
	list, err := s.client.AppsV1().Deployments(options.Namespace).List(ctx, metav1.ListOptions{LabelSelector: options.LabelSelector, FieldSelector: options.FieldSelector, Limit: int64(options.Limit)})
	if err != nil {
		return nil, classifyError(err)
	}
	result := make([]DeploymentSummary, 0, len(list.Items))
	for _, item := range list.Items {
		result = append(result, deploymentSummary(item, collectedAt))
	}
	return result, nil
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
	if options.Name != "" {
		item, err := s.client.CoreV1().Services(options.Namespace).Get(ctx, options.Name, metav1.GetOptions{})
		if err != nil {
			return nil, classifyError(err)
		}
		return []ServiceSummary{serviceSummary(*item, s.now())}, nil
	}
	list, err := s.client.CoreV1().Services(options.Namespace).List(ctx, metav1.ListOptions{LabelSelector: options.LabelSelector, FieldSelector: options.FieldSelector, Limit: int64(options.Limit)})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	result := make([]ServiceSummary, 0, len(list.Items))
	for _, item := range list.Items {
		result = append(result, serviceSummary(item, collectedAt))
	}
	return result, nil
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
	list, err := s.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: eventFieldSelector(query), Limit: int64(limit)})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	result := make([]EventSummary, 0, len(list.Items))
	for _, item := range list.Items {
		if query.InvolvedKind != "" && !strings.EqualFold(item.InvolvedObject.Kind, query.InvolvedKind) {
			continue
		}
		if query.InvolvedName != "" && item.InvolvedObject.Name != query.InvolvedName {
			continue
		}
		result = append(result, eventSummary(item, collectedAt))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LastSeen.After(result[j].LastSeen) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *Service) GetPodLogs(ctx context.Context, query LogQuery) (snippet LogSnippet, retErr error) {
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
	tail, limit := int64(tailLines), int64(s.logMaxBytes)
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	body, err := s.client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{Container: strings.TrimSpace(query.Container), TailLines: &tail, LimitBytes: &limit}).Stream(ctx)
	if err != nil {
		return LogSnippet{}, classifyError(err)
	}
	defer func() { retErr = errors.Join(retErr, body.Close()) }()
	content, err := io.ReadAll(io.LimitReader(body, int64(s.logMaxBytes)+1))
	if err != nil {
		return LogSnippet{}, err
	}
	text, truncated := SanitizeText(string(content), s.logMaxBytes)
	return LogSnippet{Namespace: namespace, PodName: podName, Container: strings.TrimSpace(query.Container), Lines: splitLogLines(text), Truncated: truncated, CollectedAt: s.now()}, nil
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
	if !s.allowAllNamespaces {
		if _, ok := s.allowedNamespaces[value]; !ok {
			return "", fmt.Errorf("%w: %s", ErrNamespaceNotAllowed, value)
		}
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

func normalizeAllowedNamespaces(values []string, fallback string) (map[string]struct{}, bool) {
	allowed := make(map[string]struct{}, len(values)+1)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "*" {
			return nil, true
		}
		if value != "" {
			allowed[value] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		allowed[fallback] = struct{}{}
	}
	return allowed, false
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
	result := PodSummary{Namespace: pod.Namespace, Name: pod.Name, Phase: string(pod.Status.Phase), ReadyContainers: ready, TotalContainers: len(pod.Spec.Containers), RestartCount: restarts, NodeName: pod.Spec.NodeName, PodIP: pod.Status.PodIP, Labels: copyStringMap(pod.Labels), CollectedAt: collectedAt}
	if pod.Status.StartTime != nil {
		result.StartTime = pod.Status.StartTime.UTC()
	}
	if len(pod.OwnerReferences) > 0 {
		result.OwnerKind, result.OwnerName = pod.OwnerReferences[0].Kind, pod.OwnerReferences[0].Name
	}
	return result
}

func deploymentSummary(deployment appsv1.Deployment, collectedAt time.Time) DeploymentSummary {
	var replicas int32
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}
	var selector map[string]string
	if deployment.Spec.Selector != nil {
		selector = copyStringMap(deployment.Spec.Selector.MatchLabels)
	}
	conditions := make([]DeploymentCondition, 0, len(deployment.Status.Conditions))
	for _, condition := range deployment.Status.Conditions {
		conditions = append(conditions, DeploymentCondition{Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason, Message: sanitizeMessage(condition.Message, 512)})
	}
	return DeploymentSummary{Namespace: deployment.Namespace, Name: deployment.Name, Selector: selector, Replicas: replicas, ReadyReplicas: deployment.Status.ReadyReplicas, UpdatedReplicas: deployment.Status.UpdatedReplicas, AvailableReplicas: deployment.Status.AvailableReplicas, Strategy: string(deployment.Spec.Strategy.Type), Conditions: conditions, CollectedAt: collectedAt}
}

func serviceSummary(service corev1.Service, collectedAt time.Time) ServiceSummary {
	ports := make([]ServicePort, 0, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		ports = append(ports, ServicePort{Name: port.Name, Protocol: string(port.Protocol), Port: port.Port, TargetPort: port.TargetPort.String()})
	}
	return ServiceSummary{Namespace: service.Namespace, Name: service.Name, Selector: copyStringMap(service.Spec.Selector), Type: string(service.Spec.Type), ClusterIP: service.Spec.ClusterIP, Ports: ports, CollectedAt: collectedAt}
}

func eventSummary(event corev1.Event, collectedAt time.Time) EventSummary {
	lastSeen := event.LastTimestamp.Time
	if lastSeen.IsZero() {
		lastSeen = event.EventTime.Time
	}
	if lastSeen.IsZero() {
		lastSeen = event.CreationTimestamp.Time
	}
	return EventSummary{Namespace: event.Namespace, Name: event.Name, Type: event.Type, Reason: event.Reason, Message: sanitizeMessage(event.Message, 512), InvolvedKind: event.InvolvedObject.Kind, InvolvedName: event.InvolvedObject.Name, Count: event.Count, LastSeen: lastSeen.UTC(), CollectedAt: collectedAt}
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func splitLogLines(text string) []string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

func eventFieldSelector(query EventQuery) string {
	selectors := make([]fields.Selector, 0, 2)
	if query.InvolvedKind != "" {
		selectors = append(selectors, fields.OneTermEqualSelector("involvedObject.kind", query.InvolvedKind))
	}
	if query.InvolvedName != "" {
		selectors = append(selectors, fields.OneTermEqualSelector("involvedObject.name", query.InvolvedName))
	}
	if len(selectors) == 0 {
		return ""
	}
	return fields.AndSelectors(selectors...).String()
}

func classifyError(err error) error {
	switch {
	case apierrors.IsNotFound(err):
		return errors.New("k8s resource not found")
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return errors.New("k8s permission denied")
	case apierrors.IsConflict(err):
		return errors.New("k8s resource conflict")
	default:
		return err
	}
}
