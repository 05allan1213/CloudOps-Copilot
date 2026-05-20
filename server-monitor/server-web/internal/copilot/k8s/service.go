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
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
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
	ListAllPods(ctx context.Context, options QueryOptions) ([]PodSummary, error)
	ListAllNodes(ctx context.Context, options QueryOptions) ([]NodeSummary, error)
	ListAllDeployments(ctx context.Context, options QueryOptions) ([]DeploymentSummary, error)
	ListAllServices(ctx context.Context, options QueryOptions) ([]ServiceSummary, error)
	ListIngresses(ctx context.Context, options QueryOptions) ([]IngressSummary, error)
	ListAllIngresses(ctx context.Context, options QueryOptions) ([]IngressSummary, error)
	ListAllEvents(ctx context.Context, query EventQuery) ([]EventSummary, error)
	ListConfigMaps(ctx context.Context, options QueryOptions) ([]ConfigMapSummary, error)
	ListPersistentVolumes(ctx context.Context, options QueryOptions) ([]PVSummary, error)
	ListPersistentVolumeClaims(ctx context.Context, options QueryOptions) ([]PVCSummary, error)
	ListResourceQuotas(ctx context.Context, options QueryOptions) ([]ResourceQuotaSummary, error)
	ListLimitRanges(ctx context.Context, options QueryOptions) ([]LimitRangeSummary, error)
	ListHorizontalPodAutoscalers(ctx context.Context, options QueryOptions) ([]HPASummary, error)
	ListDaemonSets(ctx context.Context, options QueryOptions) ([]DaemonSetSummary, error)
	ListStatefulSets(ctx context.Context, options QueryOptions) ([]StatefulSetSummary, error)
	ListJobs(ctx context.Context, options QueryOptions) ([]JobSummary, error)
	GetResourceYAML(ctx context.Context, kind, namespace, name string) (string, error)
}

type Service struct {
	client              kubernetes.Interface
	enabled             bool
	allowedNamespaces   map[string]struct{}
	allowAllNamespaces  bool
	defaultNamespace    string
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
	return &Service{
		client:              client,
		enabled:             cfg.Enabled,
		allowedNamespaces:   allowed,
		allowAllNamespaces:  allowAll,
		defaultNamespace:    defaultNamespace,
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
	if options.Name != "" {
		item, err := s.client.CoreV1().Services(options.Namespace).Get(ctx, options.Name, metav1.GetOptions{})
		if err != nil {
			return nil, classifyError(err)
		}
		return []ServiceSummary{serviceSummary(*item, s.now())}, nil
	}
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
	list, err := s.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: eventFieldSelector(query),
		Limit:         int64(limit),
	})
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

func (s *Service) ListAllPods(ctx context.Context, options QueryOptions) ([]PodSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var allPods []PodSummary
	continueToken := ""
	for {
		list, err := s.client.CoreV1().Pods(options.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: options.LabelSelector,
			FieldSelector: options.FieldSelector,
			Limit:         500,
			Continue:      continueToken,
		})
		if err != nil {
			return nil, classifyError(err)
		}
		collectedAt := s.now()
		for _, pod := range list.Items {
			allPods = append(allPods, podSummary(pod, collectedAt))
		}
		if list.Continue == "" {
			break
		}
		continueToken = list.Continue
	}
	return allPods, nil
}

func (s *Service) ListAllNodes(ctx context.Context, options QueryOptions) ([]NodeSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options = normalizeLimit(options)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var allNodes []NodeSummary
	continueToken := ""
	for {
		list, err := s.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
			LabelSelector: options.LabelSelector,
			FieldSelector: options.FieldSelector,
			Limit:         500,
			Continue:      continueToken,
		})
		if err != nil {
			return nil, classifyError(err)
		}
		collectedAt := s.now()
		for _, node := range list.Items {
			allNodes = append(allNodes, nodeSummary(node, collectedAt))
		}
		if list.Continue == "" {
			break
		}
		continueToken = list.Continue
	}
	return allNodes, nil
}

func (s *Service) ListAllDeployments(ctx context.Context, options QueryOptions) ([]DeploymentSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var allDeployments []DeploymentSummary
	continueToken := ""
	for {
		list, err := s.client.AppsV1().Deployments(options.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: options.LabelSelector,
			FieldSelector: options.FieldSelector,
			Limit:         500,
			Continue:      continueToken,
		})
		if err != nil {
			return nil, classifyError(err)
		}
		collectedAt := s.now()
		for _, deployment := range list.Items {
			allDeployments = append(allDeployments, deploymentSummary(deployment, collectedAt))
		}
		if list.Continue == "" {
			break
		}
		continueToken = list.Continue
	}
	return allDeployments, nil
}

func (s *Service) ListAllServices(ctx context.Context, options QueryOptions) ([]ServiceSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var allServices []ServiceSummary
	continueToken := ""
	for {
		list, err := s.client.CoreV1().Services(options.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: options.LabelSelector,
			FieldSelector: options.FieldSelector,
			Limit:         500,
			Continue:      continueToken,
		})
		if err != nil {
			return nil, classifyError(err)
		}
		collectedAt := s.now()
		for _, service := range list.Items {
			allServices = append(allServices, serviceSummary(service, collectedAt))
		}
		if list.Continue == "" {
			break
		}
		continueToken = list.Continue
	}
	return allServices, nil
}

func (s *Service) ListIngresses(ctx context.Context, options QueryOptions) ([]IngressSummary, error) {
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
		item, err := s.client.NetworkingV1().Ingresses(options.Namespace).Get(ctx, options.Name, metav1.GetOptions{})
		if err != nil {
			return nil, classifyError(err)
		}
		return []IngressSummary{ingressSummary(*item, s.now())}, nil
	}
	list, err := s.client.NetworkingV1().Ingresses(options.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		FieldSelector: options.FieldSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	ingresses := make([]IngressSummary, 0, len(list.Items))
	for _, ingress := range list.Items {
		ingresses = append(ingresses, ingressSummary(ingress, collectedAt))
	}
	return ingresses, nil
}

func (s *Service) ListAllIngresses(ctx context.Context, options QueryOptions) ([]IngressSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var allIngresses []IngressSummary
	continueToken := ""
	for {
		list, err := s.client.NetworkingV1().Ingresses(options.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: options.LabelSelector,
			FieldSelector: options.FieldSelector,
			Limit:         500,
			Continue:      continueToken,
		})
		if err != nil {
			return nil, classifyError(err)
		}
		collectedAt := s.now()
		for _, ingress := range list.Items {
			allIngresses = append(allIngresses, ingressSummary(ingress, collectedAt))
		}
		if list.Continue == "" {
			break
		}
		continueToken = list.Continue
	}
	return allIngresses, nil
}

func (s *Service) ListAllEvents(ctx context.Context, query EventQuery) ([]EventSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	namespace, err := s.normalizeNamespace(query.Namespace)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var allEvents []EventSummary
	continueToken := ""
	for {
		list, err := s.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
			FieldSelector: eventFieldSelector(query),
			Limit:         500,
			Continue:      continueToken,
		})
		if err != nil {
			return nil, classifyError(err)
		}
		collectedAt := s.now()
		for _, event := range list.Items {
			if query.InvolvedKind != "" && !strings.EqualFold(event.InvolvedObject.Kind, query.InvolvedKind) {
				continue
			}
			if query.InvolvedName != "" && event.InvolvedObject.Name != query.InvolvedName {
				continue
			}
			allEvents = append(allEvents, eventSummary(event, collectedAt))
		}
		if list.Continue == "" {
			break
		}
		continueToken = list.Continue
	}
	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].LastSeen.After(allEvents[j].LastSeen)
	})
	return allEvents, nil
}

func (s *Service) ListConfigMaps(ctx context.Context, options QueryOptions) ([]ConfigMapSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.CoreV1().ConfigMaps(options.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	result := make([]ConfigMapSummary, 0, len(list.Items))
	for _, cm := range list.Items {
		result = append(result, configMapSummary(cm, collectedAt))
	}
	return result, nil
}

func (s *Service) ListResourceQuotas(ctx context.Context, options QueryOptions) ([]ResourceQuotaSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.CoreV1().ResourceQuotas(options.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	result := make([]ResourceQuotaSummary, 0, len(list.Items))
	for _, rq := range list.Items {
		result = append(result, resourceQuotaSummary(rq, collectedAt))
	}
	return result, nil
}

func (s *Service) ListLimitRanges(ctx context.Context, options QueryOptions) ([]LimitRangeSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.CoreV1().LimitRanges(options.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	result := make([]LimitRangeSummary, 0, len(list.Items))
	for _, lr := range list.Items {
		result = append(result, limitRangeSummary(lr, collectedAt))
	}
	return result, nil
}

func (s *Service) ListHorizontalPodAutoscalers(ctx context.Context, options QueryOptions) ([]HPASummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.AutoscalingV2().HorizontalPodAutoscalers(options.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	result := make([]HPASummary, 0, len(list.Items))
	for _, hpa := range list.Items {
		result = append(result, hpaSummary(hpa, collectedAt))
	}
	return result, nil
}

func (s *Service) ListPersistentVolumes(ctx context.Context, options QueryOptions) ([]PVSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options = normalizeLimit(options)
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		FieldSelector: options.FieldSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	pvs := make([]PVSummary, 0, len(list.Items))
	for _, pv := range list.Items {
		pvs = append(pvs, pvSummary(pv, collectedAt))
	}
	return pvs, nil
}

func (s *Service) ListPersistentVolumeClaims(ctx context.Context, options QueryOptions) ([]PVCSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.CoreV1().PersistentVolumeClaims(options.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		FieldSelector: options.FieldSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	pvcs := make([]PVCSummary, 0, len(list.Items))
	for _, pvc := range list.Items {
		pvcs = append(pvcs, pvcSummary(pvc, collectedAt))
	}
	return pvcs, nil
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
	summary := PodSummary{
		Namespace:       pod.Namespace,
		Name:            pod.Name,
		Phase:           string(pod.Status.Phase),
		ReadyContainers: ready,
		TotalContainers: len(pod.Spec.Containers),
		RestartCount:    restarts,
		NodeName:        pod.Spec.NodeName,
		PodIP:           pod.Status.PodIP,
		Labels:          copyStringMap(pod.Labels),
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
	var selector map[string]string
	if deployment.Spec.Selector != nil {
		selector = copyStringMap(deployment.Spec.Selector.MatchLabels)
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
		Selector:          selector,
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
	return ServiceSummary{Namespace: service.Namespace, Name: service.Name, Selector: copyStringMap(service.Spec.Selector), Type: string(service.Spec.Type), ClusterIP: service.Spec.ClusterIP, Ports: ports, CollectedAt: collectedAt}
}

func ingressSummary(ingress networkingv1.Ingress, collectedAt time.Time) IngressSummary {
	hostSet := make(map[string]struct{})
	paths := make([]IngressPath, 0)
	for _, rule := range ingress.Spec.Rules {
		host := rule.Host
		if host != "" {
			hostSet[host] = struct{}{}
		}
		if rule.HTTP != nil {
			for _, p := range rule.HTTP.Paths {
				backend := formatIngressBackend(p.Backend)
				paths = append(paths, IngressPath{
					Host:     host,
					Path:     p.Path,
					PathType: formatPathType(p.PathType),
					Backend:  backend,
				})
			}
		}
	}
	hosts := make([]string, 0, len(hostSet))
	for h := range hostSet {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	tls := make([]IngressTLS, 0, len(ingress.Spec.TLS))
	for _, t := range ingress.Spec.TLS {
		tls = append(tls, IngressTLS{
			Hosts:      t.Hosts,
			SecretName: t.SecretName,
		})
	}
	age := collectedAt.Sub(ingress.CreationTimestamp.Time)
	return IngressSummary{
		Namespace:   ingress.Namespace,
		Name:        ingress.Name,
		Hosts:       hosts,
		Paths:       paths,
		TLS:         tls,
		Age:         age,
		CollectedAt: collectedAt,
	}
}

func formatIngressBackend(backend networkingv1.IngressBackend) string {
	if backend.Service != nil {
		name := backend.Service.Name
		if backend.Service.Port.Name != "" {
			return name + ":" + backend.Service.Port.Name
		}
		return fmt.Sprintf("%s:%d", name, backend.Service.Port.Number)
	}
	if backend.Resource != nil {
		return backend.Resource.Kind + "/" + backend.Resource.Name
	}
	return ""
}

func formatPathType(pt *networkingv1.PathType) string {
	if pt == nil {
		return ""
	}
	return string(*pt)
}

var sensitiveKeyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bpass(word|wd|phrase)?\b`),
	regexp.MustCompile(`(?i)\bpwd\b`),
	regexp.MustCompile(`(?i)\bsecret\b`),
	regexp.MustCompile(`(?i)\btoken\b`),
	regexp.MustCompile(`(?i)\bauth(entication|orization)?\b`),
	regexp.MustCompile(`(?i)\bcert(ificate)?\b`),
	regexp.MustCompile(`(?i)\bcredential`),
	regexp.MustCompile(`(?i)\bprivate[_-]?key\b`),
	regexp.MustCompile(`(?i)\bkubeconfig\b`),
	regexp.MustCompile(`(?i)\bapi[_-]?key\b`),
	regexp.MustCompile(`(?i)\blicense\b`),
	regexp.MustCompile(`(?i)\bencryption[_-]?key\b`),
}

func configMapSummary(cm corev1.ConfigMap, collectedAt time.Time) ConfigMapSummary {
	dataKeys := make([]string, 0, len(cm.Data))
	for k := range cm.Data {
		dataKeys = append(dataKeys, k)
	}
	sort.Strings(dataKeys)
	data := make(map[string]string, len(cm.Data))
	for k, v := range cm.Data {
		if isSensitiveKey(k) {
			data[k] = "***"
		} else {
			data[k] = v
		}
	}
	var age time.Duration
	if !cm.CreationTimestamp.IsZero() {
		age = collectedAt.Sub(cm.CreationTimestamp.Time)
	}
	return ConfigMapSummary{Namespace: cm.Namespace, Name: cm.Name, DataKeys: dataKeys, Data: data, Age: age, CollectedAt: collectedAt}
}

func isSensitiveKey(key string) bool {
	for _, pattern := range sensitiveKeyPatterns {
		if pattern.MatchString(key) {
			return true
		}
	}
	return false
}

func resourceQuotaSummary(rq corev1.ResourceQuota, collectedAt time.Time) ResourceQuotaSummary {
	hard := make(map[string]ResourceQuantity, len(rq.Status.Hard))
	for k, v := range rq.Status.Hard {
		hard[string(k)] = ResourceQuantity{Value: v.String()}
	}
	used := make(map[string]ResourceQuantity, len(rq.Status.Used))
	for k, v := range rq.Status.Used {
		used[string(k)] = ResourceQuantity{Value: v.String()}
	}
	var age time.Duration
	if !rq.CreationTimestamp.IsZero() {
		age = collectedAt.Sub(rq.CreationTimestamp.Time)
	}
	return ResourceQuotaSummary{
		Namespace:   rq.Namespace,
		Name:        rq.Name,
		Hard:        hard,
		Used:        used,
		Age:         age,
		CollectedAt: collectedAt,
	}
}

func limitRangeSummary(lr corev1.LimitRange, collectedAt time.Time) LimitRangeSummary {
	items := make([]LimitRangeItem, 0, len(lr.Spec.Limits))
	for _, limit := range lr.Spec.Limits {
		item := LimitRangeItem{
			Type: string(limit.Type),
		}
		if len(limit.Min) > 0 {
			parts := make([]string, 0, len(limit.Min))
			for k, v := range limit.Min {
				parts = append(parts, fmt.Sprintf("%s=%s", k, v.String()))
			}
			sort.Strings(parts)
			item.Min = strings.Join(parts, ", ")
		}
		if len(limit.Max) > 0 {
			parts := make([]string, 0, len(limit.Max))
			for k, v := range limit.Max {
				parts = append(parts, fmt.Sprintf("%s=%s", k, v.String()))
			}
			sort.Strings(parts)
			item.Max = strings.Join(parts, ", ")
		}
		if len(limit.Default) > 0 {
			parts := make([]string, 0, len(limit.Default))
			for k, v := range limit.Default {
				parts = append(parts, fmt.Sprintf("%s=%s", k, v.String()))
			}
			sort.Strings(parts)
			item.Default = strings.Join(parts, ", ")
		}
		items = append(items, item)
	}
	var age time.Duration
	if !lr.CreationTimestamp.IsZero() {
		age = collectedAt.Sub(lr.CreationTimestamp.Time)
	}
	return LimitRangeSummary{
		Namespace:   lr.Namespace,
		Name:        lr.Name,
		Limits:      items,
		Age:         age,
		CollectedAt: collectedAt,
	}
}

func hpaSummary(hpa autoscalingv2.HorizontalPodAutoscaler, collectedAt time.Time) HPASummary {
	reference := hpa.Spec.ScaleTargetRef.Kind + "/" + hpa.Spec.ScaleTargetRef.Name
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}
	targetUtilization := ""
	for _, metric := range hpa.Spec.Metrics {
		if metric.Resource.Target.AverageUtilization != nil {
			targetUtilization = fmt.Sprintf("%s: %d%%", metric.Resource.Name, *metric.Resource.Target.AverageUtilization)
			break
		}
	}
	var age time.Duration
	if !hpa.CreationTimestamp.IsZero() {
		age = collectedAt.Sub(hpa.CreationTimestamp.Time)
	}
	return HPASummary{
		Namespace:         hpa.Namespace,
		Name:              hpa.Name,
		Reference:         reference,
		MinReplicas:       minReplicas,
		MaxReplicas:       hpa.Spec.MaxReplicas,
		CurrentReplicas:   hpa.Status.CurrentReplicas,
		TargetUtilization: targetUtilization,
		Age:               age,
		CollectedAt:       collectedAt,
	}
}

func (s *Service) ListDaemonSets(ctx context.Context, options QueryOptions) ([]DaemonSetSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.AppsV1().DaemonSets(options.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	result := make([]DaemonSetSummary, 0, len(list.Items))
	for _, ds := range list.Items {
		result = append(result, daemonSetSummary(ds, collectedAt))
	}
	return result, nil
}

func (s *Service) ListStatefulSets(ctx context.Context, options QueryOptions) ([]StatefulSetSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.AppsV1().StatefulSets(options.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	result := make([]StatefulSetSummary, 0, len(list.Items))
	for _, sts := range list.Items {
		result = append(result, statefulSetSummary(sts, collectedAt))
	}
	return result, nil
}

func (s *Service) ListJobs(ctx context.Context, options QueryOptions) ([]JobSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	options, err := s.normalizeQuery(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	list, err := s.client.BatchV1().Jobs(options.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: options.LabelSelector,
		Limit:         int64(options.Limit),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	collectedAt := s.now()
	result := make([]JobSummary, 0, len(list.Items))
	for _, job := range list.Items {
		result = append(result, jobSummary(job, collectedAt))
	}
	return result, nil
}

func daemonSetSummary(ds appsv1.DaemonSet, collectedAt time.Time) DaemonSetSummary {
	var nodeSelector string
	if len(ds.Spec.Template.Spec.NodeSelector) > 0 {
		parts := make([]string, 0, len(ds.Spec.Template.Spec.NodeSelector))
		for k, v := range ds.Spec.Template.Spec.NodeSelector {
			parts = append(parts, k+"="+v)
		}
		sort.Strings(parts)
		nodeSelector = strings.Join(parts, ",")
	}
	var age time.Duration
	if !ds.CreationTimestamp.IsZero() {
		age = collectedAt.Sub(ds.CreationTimestamp.Time)
	}
	return DaemonSetSummary{
		Namespace:    ds.Namespace,
		Name:         ds.Name,
		Desired:      ds.Status.DesiredNumberScheduled,
		Current:      ds.Status.CurrentNumberScheduled,
		Ready:        ds.Status.NumberReady,
		Updated:      ds.Status.UpdatedNumberScheduled,
		NodeSelector: nodeSelector,
		Age:          age,
		CollectedAt:  collectedAt,
	}
}

func statefulSetSummary(sts appsv1.StatefulSet, collectedAt time.Time) StatefulSetSummary {
	var desired int32
	if sts.Spec.Replicas != nil {
		desired = *sts.Spec.Replicas
	}
	var age time.Duration
	if !sts.CreationTimestamp.IsZero() {
		age = collectedAt.Sub(sts.CreationTimestamp.Time)
	}
	return StatefulSetSummary{
		Namespace:       sts.Namespace,
		Name:            sts.Name,
		ReplicasReady:   sts.Status.ReadyReplicas,
		ReplicasDesired: desired,
		ServiceName:     sts.Spec.ServiceName,
		Age:             age,
		CollectedAt:     collectedAt,
	}
}

func jobSummary(job batchv1.Job, collectedAt time.Time) JobSummary {
	completions := "0/1"
	if job.Spec.Completions != nil {
		completions = fmt.Sprintf("%d/%d", job.Status.Succeeded, *job.Spec.Completions)
	} else if job.Status.Succeeded > 0 {
		completions = fmt.Sprintf("%d/1", job.Status.Succeeded)
	}
	status := "Running"
	for _, c := range job.Status.Conditions {
		if c.Status == corev1.ConditionTrue {
			switch c.Type {
			case batchv1.JobComplete:
				status = "Completed"
			case batchv1.JobFailed:
				status = "Failed"
			case batchv1.JobSuspended:
				status = "Suspended"
			}
			break
		}
	}
	var duration string
	if job.Status.StartTime != nil {
		endTime := collectedAt
		if job.Status.CompletionTime != nil {
			endTime = job.Status.CompletionTime.Time
		}
		duration = formatDuration(endTime.Sub(job.Status.StartTime.Time))
	}
	var age time.Duration
	if !job.CreationTimestamp.IsZero() {
		age = collectedAt.Sub(job.CreationTimestamp.Time)
	}
	return JobSummary{
		Namespace:   job.Namespace,
		Name:        job.Name,
		Completions: completions,
		Duration:    duration,
		Status:      status,
		Age:         age,
		CollectedAt: collectedAt,
	}
}

func pvSummary(pv corev1.PersistentVolume, collectedAt time.Time) PVSummary {
	accessModes := make([]string, 0, len(pv.Spec.AccessModes))
	for _, am := range pv.Spec.AccessModes {
		accessModes = append(accessModes, string(am))
	}
	var claimRef string
	if pv.Spec.ClaimRef != nil {
		claimRef = pv.Spec.ClaimRef.Namespace + "/" + pv.Spec.ClaimRef.Name
	}
	var age string
	if !pv.CreationTimestamp.IsZero() {
		age = formatDuration(collectedAt.Sub(pv.CreationTimestamp.Time))
	}
	return PVSummary{
		Name:         pv.Name,
		Capacity:     pv.Spec.Capacity.Storage().String(),
		AccessModes:  accessModes,
		Status:       string(pv.Status.Phase),
		ClaimRef:     claimRef,
		StorageClass: pv.Spec.StorageClassName,
		Age:          age,
		CollectedAt:  collectedAt,
	}
}

func pvcSummary(pvc corev1.PersistentVolumeClaim, collectedAt time.Time) PVCSummary {
	accessModes := make([]string, 0, len(pvc.Spec.AccessModes))
	for _, am := range pvc.Spec.AccessModes {
		accessModes = append(accessModes, string(am))
	}
	var age string
	if !pvc.CreationTimestamp.IsZero() {
		age = formatDuration(collectedAt.Sub(pvc.CreationTimestamp.Time))
	}
	return PVCSummary{
		Namespace:    pvc.Namespace,
		Name:         pvc.Name,
		StorageClass: stringPtr(pvc.Spec.StorageClassName),
		VolumeName:   pvc.Spec.VolumeName,
		AccessModes:  accessModes,
		Status:       string(pvc.Status.Phase),
		Age:          age,
		CollectedAt:  collectedAt,
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if hours == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd%dh", days, hours)
}

func stringPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func splitLogLines(text string) []string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	lines := make([]string, 0)
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

func (s *Service) GetResourceYAML(ctx context.Context, kind, namespace, name string) (string, error) {
	if err := s.ready(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()

	var obj interface{}
	var err error

	switch strings.ToLower(kind) {
	case "pod":
		obj, err = s.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	case "deployment":
		obj, err = s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	case "service":
		obj, err = s.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	case "configmap":
		obj, err = s.client.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	case "ingress":
		obj, err = s.client.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
	case "node":
		obj, err = s.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	case "persistentvolume":
		obj, err = s.client.CoreV1().PersistentVolumes().Get(ctx, name, metav1.GetOptions{})
	case "persistentvolumeclaim":
		obj, err = s.client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	case "horizontalpodautoscaler":
		obj, err = s.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, name, metav1.GetOptions{})
	case "daemonset":
		obj, err = s.client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	case "statefulset":
		obj, err = s.client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	case "job":
		obj, err = s.client.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	case "event":
		obj, err = s.client.CoreV1().Events(namespace).Get(ctx, name, metav1.GetOptions{})
	default:
		return "", fmt.Errorf("unsupported resource kind: %s", kind)
	}
	if err != nil {
		return "", classifyError(err)
	}

	// Remove managedFields for cleaner output
	if unstructured, ok := obj.(interface {
		SetManagedFields(managedFields []interface{})
	}); ok {
		unstructured.SetManagedFields(nil)
	}

	yamlBytes, err := yaml.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("marshal yaml: %w", err)
	}

	result := string(yamlBytes)

	if strings.ToLower(kind) == "configmap" {
		result = sanitizeConfigMapYAML(result)
	}

	result, _ = SanitizeText(result, 100*1024)

	if len(result) > 100*1024 {
		result = result[:100*1024] + "\n\n... YAML content truncated (exceeds 100KB)"
	}

	return result, nil
}

func sanitizeConfigMapYAML(yamlStr string) string {
	lines := strings.Split(yamlStr, "\n")
	inDataSection := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "data:" {
			inDataSection = true
			continue
		}
		if inDataSection {
			if trimmed == "" || (!strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "\t")) {
				inDataSection = false
				continue
			}
			if strings.HasPrefix(trimmed, "- ") {
				continue
			}
			colonIdx := strings.Index(trimmed, ":")
			if colonIdx > 0 {
				key := strings.TrimRight(trimmed[:colonIdx], " ")
				if isSensitiveKey(key) {
					lines[i] = line[:strings.Index(line, key)] + key + ": ***"
				}
			}
		}
	}
	return strings.Join(lines, "\n")
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
