// Package kubernetestopology adapts the typed Kubernetes client into the
// bounded CloudOps topology projection. It never exposes raw objects or YAML.
package kubernetestopology

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8sread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	ClusterID         string
	AllowedNamespaces []string
	RequestTimeout    time.Duration
}

type Reader struct {
	client             kubernetes.Interface
	clusterID          string
	allowedNamespaces  map[string]struct{}
	allowAllNamespaces bool
	requestTimeout     time.Duration
	now                func() time.Time
}

func New(client kubernetes.Interface, cfg Config) (*Reader, error) {
	if client == nil {
		return nil, errors.New("Kubernetes topology client is required")
	}
	clusterID := strings.TrimSpace(cfg.ClusterID)
	if clusterID == "" || len(clusterID) > 128 {
		return nil, errors.New("Kubernetes topology cluster identity is invalid")
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedNamespaces))
	allowAll := false
	for _, namespace := range cfg.AllowedNamespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "*" {
			allowAll = true
			continue
		}
		if namespace == "" {
			continue
		}
		if err := k8sread.ValidateName("namespace", namespace); err != nil {
			return nil, err
		}
		allowed[namespace] = struct{}{}
	}
	if !allowAll && len(allowed) == 0 {
		return nil, errors.New("Kubernetes topology Namespace allowlist is empty")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Reader{
		client: client, clusterID: clusterID, allowedNamespaces: allowed,
		allowAllNamespaces: allowAll, requestTimeout: timeout,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (r *Reader) Probe(ctx context.Context, clusterID string) (infrastructure.ProviderSource, error) {
	if strings.TrimSpace(clusterID) != r.clusterID {
		return infrastructure.ProviderSource{}, errors.New("requested cluster is not served by this Kubernetes client")
	}
	ctx, cancel := context.WithTimeout(ctx, r.requestTimeout)
	defer cancel()
	version, err := r.client.Discovery().ServerVersion()
	if err != nil {
		return infrastructure.ProviderSource{}, fmt.Errorf("probe Kubernetes API: %w", err)
	}
	return infrastructure.ProviderSource{
		Provider: "kubernetes", ClusterID: r.clusterID,
		Identity: "kubernetes://" + r.clusterID, ServerVersion: version.GitVersion,
		CollectedAt: r.now(),
	}, nil
}

func (r *Reader) Read(ctx context.Context, request infrastructure.ReadRequest) (infrastructure.Projection, error) {
	if strings.TrimSpace(request.ClusterID) != r.clusterID {
		return infrastructure.Projection{}, errors.New("requested cluster is not served by this Kubernetes client")
	}
	namespaces, err := r.namespaces(request.Namespaces)
	if err != nil {
		return infrastructure.Projection{}, err
	}
	limit := request.Limit
	if limit <= 0 || limit > infrastructure.MaximumLimit {
		limit = infrastructure.MaximumLimit
	}
	ctx, cancel := context.WithTimeout(ctx, r.requestTimeout)
	defer cancel()
	source, err := r.Probe(ctx, request.ClusterID)
	if err != nil {
		return infrastructure.Projection{}, err
	}
	b := newBuilder(r.clusterID, limit)
	for _, namespace := range namespaces {
		r.readNamespace(ctx, namespace, b)
	}
	b.attachNodes(ctx)
	return b.projection(source), nil
}

func (r *Reader) Events(ctx context.Context, clusterID string, resource infrastructure.Resource, limit int) ([]infrastructure.Event, bool, error) {
	if strings.TrimSpace(clusterID) != r.clusterID {
		return nil, false, errors.New("requested cluster is not served by this Kubernetes client")
	}
	namespace := strings.TrimSpace(resource.Namespace)
	if namespace == "" {
		// The reader is intentionally bounded to the active Operational Scope
		// namespaces. A cluster-scoped resource has no safe namespace in which to
		// issue an Event list, and widening this request to all namespaces would
		// bypass that boundary. Project the scoped result as an honest empty list
		// instead of turning normal Namespace/Node inspection into Provider failure.
		return []infrastructure.Event{}, false, nil
	}
	if _, err := r.namespaces([]string{namespace}); err != nil {
		return nil, false, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	ctx, cancel := context.WithTimeout(ctx, r.requestTimeout)
	defer cancel()
	selector := "involvedObject.kind=" + resource.Kind + ",involvedObject.name=" + resource.Name
	list, err := r.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: selector, Limit: int64(limit + 1)})
	if err != nil {
		return nil, false, fmt.Errorf("list Kubernetes events: %w", err)
	}
	collectedAt := r.now()
	result := make([]infrastructure.Event, 0, minInt(len(list.Items), limit))
	for _, item := range list.Items {
		if item.InvolvedObject.UID != "" && resource.SourceUID != "" && string(item.InvolvedObject.UID) != resource.SourceUID {
			continue
		}
		count := item.Count
		if item.Series != nil && item.Series.Count > count {
			count = item.Series.Count
		}
		result = append(result, infrastructure.Event{
			ID: eventID(item), Type: bounded(item.Type, 32), Reason: bounded(item.Reason, 128),
			Message: sanitized(item.Message, 512), Count: count,
			ResourceKind: bounded(item.InvolvedObject.Kind, 64), ResourceName: bounded(item.InvolvedObject.Name, 253),
			Namespace: namespace, ObservedAt: eventTime(item), CollectedAt: collectedAt,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ObservedAt.After(result[j].ObservedAt) })
	truncated := len(result) > limit || len(list.Continue) > 0
	if len(result) > limit {
		result = result[:limit]
	}
	return result, truncated, nil
}

func (r *Reader) namespaces(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("at least one Operational Scope Namespace is required")
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, namespace := range values {
		namespace = strings.TrimSpace(namespace)
		if err := k8sread.ValidateName("namespace", namespace); err != nil {
			return nil, err
		}
		if !r.allowAllNamespaces {
			if _, ok := r.allowedNamespaces[namespace]; !ok {
				return nil, fmt.Errorf("Namespace %q is outside the Kubernetes reader allowlist", namespace)
			}
		}
		if _, ok := seen[namespace]; ok {
			continue
		}
		seen[namespace] = struct{}{}
		result = append(result, namespace)
	}
	sort.Strings(result)
	return result, nil
}

type builder struct {
	clusterID string
	limit     int
	client    kubernetes.Interface
	nodes     map[string]infrastructure.Resource
	edges     map[string]infrastructure.TopologyEdge
	issues    []infrastructure.ProviderIssue
	partial   bool
	truncated bool
	nodeNames map[string]struct{}
}

func newBuilder(clusterID string, limit int) *builder {
	return &builder{
		clusterID: clusterID, limit: limit,
		nodes: make(map[string]infrastructure.Resource), edges: make(map[string]infrastructure.TopologyEdge),
		nodeNames: make(map[string]struct{}),
	}
}

func (r *Reader) readNamespace(ctx context.Context, namespace string, b *builder) {
	b.client = r.client
	namespaceObject, err := r.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		b.issue(namespace, "namespaces.get", err)
	} else {
		b.add(namespaceResource(r.clusterID, *namespaceObject))
	}
	namespaceID := resourceID(r.clusterID, "v1", "Namespace", "", namespace)

	services, err := r.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{Limit: int64(b.limit)})
	if err != nil {
		b.issue(namespace, "services.list", err)
	}
	deployments, err := r.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{Limit: int64(b.limit)})
	if err != nil {
		b.issue(namespace, "deployments.list", err)
	}
	statefulSets, err := r.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{Limit: int64(b.limit)})
	if err != nil {
		b.issue(namespace, "statefulsets.list", err)
	}
	daemonSets, err := r.client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{Limit: int64(b.limit)})
	if err != nil {
		b.issue(namespace, "daemonsets.list", err)
	}
	replicaSets, err := r.client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{Limit: int64(b.limit)})
	if err != nil {
		b.issue(namespace, "replicasets.list", err)
	}
	ingresses, err := r.client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{Limit: int64(b.limit)})
	if err != nil {
		b.issue(namespace, "ingresses.list", err)
	}
	pods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: int64(b.limit)})
	if err != nil {
		b.issue(namespace, "pods.list", err)
	}
	endpointSlices, err := r.client.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{Limit: int64(b.limit)})
	if err != nil {
		b.issue(namespace, "endpointslices.list", err)
	}

	workloadSelectors := make(map[string]labels.Selector)
	workloadRefs := make(map[string]infrastructure.ResourceReference)
	for _, item := range deployments.Items {
		resource := deploymentResource(r.clusterID, item)
		b.addWithNamespace(resource, namespaceID)
		selector, selectorErr := metav1.LabelSelectorAsSelector(item.Spec.Selector)
		if selectorErr == nil {
			workloadSelectors[resource.ID] = selector
		}
		workloadRefs["Deployment/"+item.Name] = reference(resource)
	}
	for _, item := range statefulSets.Items {
		resource := statefulSetResource(r.clusterID, item)
		b.addWithNamespace(resource, namespaceID)
		selector, selectorErr := metav1.LabelSelectorAsSelector(item.Spec.Selector)
		if selectorErr == nil {
			workloadSelectors[resource.ID] = selector
		}
		workloadRefs["StatefulSet/"+item.Name] = reference(resource)
	}
	for _, item := range daemonSets.Items {
		resource := daemonSetResource(r.clusterID, item)
		b.addWithNamespace(resource, namespaceID)
		selector, selectorErr := metav1.LabelSelectorAsSelector(item.Spec.Selector)
		if selectorErr == nil {
			workloadSelectors[resource.ID] = selector
		}
		workloadRefs["DaemonSet/"+item.Name] = reference(resource)
	}
	replicaSetOwners := make(map[string]infrastructure.ResourceReference)
	for _, item := range replicaSets.Items {
		for _, owner := range item.OwnerReferences {
			if owner.Kind == "Deployment" {
				if ref, ok := workloadRefs["Deployment/"+owner.Name]; ok {
					replicaSetOwners[item.Name] = ref
				}
			}
		}
	}

	serviceSelectors := make(map[string]labels.Selector)
	serviceIDs := make(map[string]string)
	for _, item := range services.Items {
		resource := serviceResource(r.clusterID, item)
		b.addWithNamespace(resource, namespaceID)
		serviceIDs[item.Name] = resource.ID
		if len(item.Spec.Selector) > 0 {
			serviceSelectors[resource.ID] = labels.SelectorFromSet(item.Spec.Selector)
		}
	}

	podIDs := make(map[string]string)
	for _, item := range pods.Items {
		resource := podResource(r.clusterID, item, replicaSetOwners, workloadRefs)
		b.addWithNamespace(resource, namespaceID)
		podIDs[item.Name] = resource.ID
		if item.Spec.NodeName != "" {
			b.nodeNames[item.Spec.NodeName] = struct{}{}
		}
		for _, owner := range resource.OwnerReferences {
			if owner.ID != "" {
				b.edge(owner.ID, resource.ID, "owns", "metadata.ownerReferences")
			}
		}
		for workloadID, selector := range workloadSelectors {
			if selector.Matches(labels.Set(item.Labels)) {
				b.edge(workloadID, resource.ID, "selects", "spec.selector")
			}
		}
		for serviceID, selector := range serviceSelectors {
			if selector.Matches(labels.Set(item.Labels)) {
				b.edge(serviceID, resource.ID, "selects", "Service.spec.selector")
			}
		}
	}

	for _, item := range endpointSlices.Items {
		serviceName := item.Labels[discoveryv1.LabelServiceName]
		serviceID := serviceIDs[serviceName]
		if serviceID == "" {
			continue
		}
		service := b.nodes[serviceID]
		for _, endpoint := range item.Endpoints {
			ready := endpoint.Conditions.Ready
			for _, address := range endpoint.Addresses {
				value := infrastructure.ResourceEndpoint{Address: bounded(address, 256), Ready: ready}
				if endpoint.TargetRef != nil {
					value.TargetRef = endpoint.TargetRef.Kind + "/" + endpoint.TargetRef.Name
					value.TargetID = podIDs[endpoint.TargetRef.Name]
					if value.TargetID != "" {
						b.edge(serviceID, value.TargetID, "routes_to", "EndpointSlice.endpoints.targetRef")
					}
				}
				service.Endpoints = append(service.Endpoints, value)
			}
		}
		b.nodes[serviceID] = service
	}

	for _, item := range ingresses.Items {
		resource := ingressResource(r.clusterID, item)
		b.addWithNamespace(resource, namespaceID)
		for _, backend := range ingressBackends(item) {
			if serviceID := serviceIDs[backend]; serviceID != "" {
				b.edge(resource.ID, serviceID, "backend_ref", "Ingress.spec.rules.backend.service")
			}
		}
	}
}

func (b *builder) attachNodes(ctx context.Context) {
	if b.client == nil {
		return
	}
	names := make([]string, 0, len(b.nodeNames))
	for name := range b.nodeNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		item, err := b.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			b.issue("", "nodes.get", err)
			continue
		}
		resource := nodeResource(b.clusterID, *item)
		b.add(resource)
		for _, pod := range b.nodes {
			if pod.Kind == "Pod" && pod.NodeName == name {
				b.edge(pod.ID, resource.ID, "scheduled_on", "Pod.spec.nodeName")
			}
		}
	}
}

func (b *builder) add(resource infrastructure.Resource) {
	if _, exists := b.nodes[resource.ID]; exists {
		return
	}
	if len(b.nodes) >= b.limit {
		b.truncated = true
		return
	}
	b.nodes[resource.ID] = resource
}

func (b *builder) addWithNamespace(resource infrastructure.Resource, namespaceID string) {
	b.add(resource)
	if _, ok := b.nodes[resource.ID]; ok {
		b.edge(namespaceID, resource.ID, "contains", "metadata.namespace")
	}
}

func (b *builder) edge(source, target, relation, fact string) {
	if source == "" || target == "" || source == target {
		return
	}
	if _, ok := b.nodes[source]; !ok {
		return
	}
	if _, ok := b.nodes[target]; !ok {
		return
	}
	id := edgeID(source, target, relation, fact)
	b.edges[id] = infrastructure.TopologyEdge{ID: id, SourceID: source, TargetID: target, Relation: relation, SourceFact: fact}
}

func (b *builder) issue(namespace, operation string, err error) {
	b.partial = true
	b.issues = append(b.issues, infrastructure.ProviderIssue{
		Namespace: namespace, Operation: operation, Code: "KUBERNETES_READ_FAILED", Detail: sanitized(err.Error(), 384),
	})
}

func (b *builder) projection(source infrastructure.ProviderSource) infrastructure.Projection {
	nodes := make([]infrastructure.Resource, 0, len(b.nodes))
	for _, item := range b.nodes {
		nodes = append(nodes, item)
	}
	edges := make([]infrastructure.TopologyEdge, 0, len(b.edges))
	for _, item := range b.edges {
		edges = append(edges, item)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	if b.issues == nil {
		b.issues = []infrastructure.ProviderIssue{}
	}
	return infrastructure.Projection{Source: source, Nodes: nodes, Edges: edges, Issues: b.issues, Partial: b.partial, Truncated: b.truncated}
}

func namespaceResource(clusterID string, item corev1.Namespace) infrastructure.Resource {
	health := infrastructure.ResourceHealth{State: infrastructure.HealthHealthy, Summary: "Namespace Active"}
	if item.Status.Phase != corev1.NamespaceActive || item.DeletionTimestamp != nil {
		health = infrastructure.ResourceHealth{State: infrastructure.HealthWarning, Summary: "Namespace 正在终止或不可用"}
	}
	return baseResource(clusterID, "v1", "Namespace", infrastructure.LayerNamespace, "", item.Name, string(item.UID), string(item.Status.Phase), health, item.Labels, item.CreationTimestamp.Time)
}

func deploymentResource(clusterID string, item appsv1.Deployment) infrastructure.Resource {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	health := replicaHealth(desired, item.Status.ReadyReplicas, "Deployment")
	resource := baseResource(clusterID, "apps/v1", "Deployment", infrastructure.LayerWorkload, item.Namespace, item.Name, string(item.UID), fmt.Sprintf("%d/%d ready", item.Status.ReadyReplicas, desired), health, item.Labels, item.CreationTimestamp.Time)
	resource.Selector = labelSelector(item.Spec.Selector)
	for _, condition := range item.Status.Conditions {
		resource.Conditions = append(resource.Conditions, infrastructure.ResourceCondition{Type: string(condition.Type), Status: string(condition.Status), Reason: bounded(condition.Reason, 128), Message: sanitized(condition.Message, 512), LastTransitionTime: condition.LastTransitionTime.Time.UTC()})
	}
	return resource
}

func statefulSetResource(clusterID string, item appsv1.StatefulSet) infrastructure.Resource {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	resource := baseResource(clusterID, "apps/v1", "StatefulSet", infrastructure.LayerWorkload, item.Namespace, item.Name, string(item.UID), fmt.Sprintf("%d/%d ready", item.Status.ReadyReplicas, desired), replicaHealth(desired, item.Status.ReadyReplicas, "StatefulSet"), item.Labels, item.CreationTimestamp.Time)
	resource.Selector = labelSelector(item.Spec.Selector)
	for _, condition := range item.Status.Conditions {
		resource.Conditions = append(resource.Conditions, infrastructure.ResourceCondition{Type: string(condition.Type), Status: string(condition.Status), Reason: bounded(condition.Reason, 128), Message: sanitized(condition.Message, 512), LastTransitionTime: condition.LastTransitionTime.Time.UTC()})
	}
	return resource
}

func daemonSetResource(clusterID string, item appsv1.DaemonSet) infrastructure.Resource {
	desired := item.Status.DesiredNumberScheduled
	resource := baseResource(clusterID, "apps/v1", "DaemonSet", infrastructure.LayerWorkload, item.Namespace, item.Name, string(item.UID), fmt.Sprintf("%d/%d ready", item.Status.NumberReady, desired), replicaHealth(desired, item.Status.NumberReady, "DaemonSet"), item.Labels, item.CreationTimestamp.Time)
	resource.Selector = labelSelector(item.Spec.Selector)
	for _, condition := range item.Status.Conditions {
		resource.Conditions = append(resource.Conditions, infrastructure.ResourceCondition{Type: string(condition.Type), Status: string(condition.Status), Reason: bounded(condition.Reason, 128), Message: sanitized(condition.Message, 512), LastTransitionTime: condition.LastTransitionTime.Time.UTC()})
	}
	return resource
}

func serviceResource(clusterID string, item corev1.Service) infrastructure.Resource {
	health := infrastructure.ResourceHealth{State: infrastructure.HealthHealthy, Summary: "Service 配置可用"}
	resource := baseResource(clusterID, "v1", "Service", infrastructure.LayerService, item.Namespace, item.Name, string(item.UID), string(item.Spec.Type), health, item.Labels, item.CreationTimestamp.Time)
	resource.Selector = copyMap(item.Spec.Selector)
	if item.Spec.ClusterIP != "" && item.Spec.ClusterIP != corev1.ClusterIPNone {
		resource.Addresses = []string{bounded(item.Spec.ClusterIP, 256)}
	}
	for _, port := range item.Spec.Ports {
		resource.Ports = append(resource.Ports, infrastructure.ResourcePort{Name: bounded(port.Name, 63), Protocol: string(port.Protocol), Port: port.Port, TargetPort: bounded(port.TargetPort.String(), 64)})
	}
	return resource
}

func podResource(clusterID string, item corev1.Pod, replicaSetOwners map[string]infrastructure.ResourceReference, workloadRefs map[string]infrastructure.ResourceReference) infrastructure.Resource {
	ready := 0
	restarts := int32(0)
	for _, status := range item.Status.ContainerStatuses {
		if status.Ready {
			ready++
		}
		restarts += status.RestartCount
	}
	health := infrastructure.ResourceHealth{State: infrastructure.HealthHealthy, Summary: fmt.Sprintf("%d/%d containers ready", ready, len(item.Spec.Containers))}
	if item.Status.Phase == corev1.PodFailed || item.Status.Phase == corev1.PodUnknown {
		health = infrastructure.ResourceHealth{State: infrastructure.HealthCritical, Summary: "Pod " + string(item.Status.Phase)}
	} else if item.Status.Phase != corev1.PodRunning || ready != len(item.Spec.Containers) || restarts > 0 {
		health = infrastructure.ResourceHealth{State: infrastructure.HealthWarning, Summary: fmt.Sprintf("%s · restarts %d", item.Status.Phase, restarts)}
	}
	resource := baseResource(clusterID, "v1", "Pod", infrastructure.LayerPod, item.Namespace, item.Name, string(item.UID), string(item.Status.Phase), health, item.Labels, item.CreationTimestamp.Time)
	resource.NodeName = bounded(item.Spec.NodeName, 253)
	if item.Status.PodIP != "" {
		resource.Addresses = []string{bounded(item.Status.PodIP, 256)}
	}
	for _, owner := range item.OwnerReferences {
		if owner.Kind == "ReplicaSet" {
			if ref, ok := replicaSetOwners[owner.Name]; ok {
				resource.OwnerReferences = append(resource.OwnerReferences, ref)
			}
			continue
		}
		if ref, ok := workloadRefs[owner.Kind+"/"+owner.Name]; ok {
			resource.OwnerReferences = append(resource.OwnerReferences, ref)
		}
	}
	for _, condition := range item.Status.Conditions {
		resource.Conditions = append(resource.Conditions, infrastructure.ResourceCondition{Type: string(condition.Type), Status: string(condition.Status), Reason: bounded(condition.Reason, 128), Message: sanitized(condition.Message, 512), LastTransitionTime: condition.LastTransitionTime.Time.UTC()})
	}
	return resource
}

func ingressResource(clusterID string, item networkingv1.Ingress) infrastructure.Resource {
	health := infrastructure.ResourceHealth{State: infrastructure.HealthUnknown, Summary: "Ingress 状态由负载均衡器报告"}
	if len(item.Status.LoadBalancer.Ingress) > 0 {
		health = infrastructure.ResourceHealth{State: infrastructure.HealthHealthy, Summary: "Ingress endpoint 可用"}
	}
	resource := baseResource(clusterID, "networking.k8s.io/v1", "Ingress", infrastructure.LayerGateway, item.Namespace, item.Name, string(item.UID), "configured", health, item.Labels, item.CreationTimestamp.Time)
	for _, endpoint := range item.Status.LoadBalancer.Ingress {
		if endpoint.IP != "" {
			resource.Addresses = append(resource.Addresses, bounded(endpoint.IP, 256))
		}
		if endpoint.Hostname != "" {
			resource.Addresses = append(resource.Addresses, bounded(endpoint.Hostname, 256))
		}
	}
	return resource
}

func nodeResource(clusterID string, item corev1.Node) infrastructure.Resource {
	health := infrastructure.ResourceHealth{State: infrastructure.HealthUnknown, Summary: "Node Ready 状态未知"}
	resource := baseResource(clusterID, "v1", "Node", infrastructure.LayerNode, "", item.Name, string(item.UID), "", health, item.Labels, item.CreationTimestamp.Time)
	for _, address := range item.Status.Addresses {
		if address.Type == corev1.NodeInternalIP || address.Type == corev1.NodeHostName {
			resource.Addresses = append(resource.Addresses, bounded(address.Address, 256))
		}
	}
	for _, condition := range item.Status.Conditions {
		resource.Conditions = append(resource.Conditions, infrastructure.ResourceCondition{Type: string(condition.Type), Status: string(condition.Status), Reason: bounded(condition.Reason, 128), Message: sanitized(condition.Message, 512), LastTransitionTime: condition.LastTransitionTime.Time.UTC()})
		if condition.Type == corev1.NodeReady {
			if condition.Status == corev1.ConditionTrue {
				resource.Health = infrastructure.ResourceHealth{State: infrastructure.HealthHealthy, Summary: "Node Ready"}
			} else {
				resource.Health = infrastructure.ResourceHealth{State: infrastructure.HealthCritical, Summary: "Node NotReady"}
			}
			resource.Status = string(condition.Status)
		}
	}
	return resource
}

func baseResource(clusterID, apiVersion, kind string, layer infrastructure.ResourceLayer, namespace, name, uid, status string, health infrastructure.ResourceHealth, objectLabels map[string]string, createdAt time.Time) infrastructure.Resource {
	return infrastructure.Resource{
		ID: resourceID(clusterID, apiVersion, kind, namespace, name), SourceUID: bounded(uid, 128), APIVersion: apiVersion,
		Kind: kind, Layer: layer, Namespace: namespace, Name: name, Status: status, Health: health,
		OwnerReferences: []infrastructure.ResourceReference{}, Selector: map[string]string{}, Labels: copyMap(objectLabels),
		Endpoints: []infrastructure.ResourceEndpoint{}, Ports: []infrastructure.ResourcePort{}, Conditions: []infrastructure.ResourceCondition{},
		Addresses: []string{}, CreatedAt: createdAt.UTC(), Links: []infrastructure.ContextLink{},
	}
}

func replicaHealth(desired, ready int32, kind string) infrastructure.ResourceHealth {
	if desired == ready {
		return infrastructure.ResourceHealth{State: infrastructure.HealthHealthy, Summary: fmt.Sprintf("%s %d/%d ready", kind, ready, desired)}
	}
	state := infrastructure.HealthWarning
	if desired > 0 && ready == 0 {
		state = infrastructure.HealthCritical
	}
	return infrastructure.ResourceHealth{State: state, Summary: fmt.Sprintf("%s %d/%d ready", kind, ready, desired)}
}

func reference(resource infrastructure.Resource) infrastructure.ResourceReference {
	return infrastructure.ResourceReference{ID: resource.ID, Kind: resource.Kind, Namespace: resource.Namespace, Name: resource.Name}
}

func labelSelector(selector *metav1.LabelSelector) map[string]string {
	if selector == nil {
		return map[string]string{}
	}
	return copyMap(selector.MatchLabels)
}

func copyMap(values map[string]string) map[string]string {
	result := make(map[string]string)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(result) >= 64 {
			break
		}
		result[bounded(key, 128)] = sanitized(values[key], 256)
	}
	return result
}

func ingressBackends(item networkingv1.Ingress) []string {
	seen := map[string]struct{}{}
	if item.Spec.DefaultBackend != nil && item.Spec.DefaultBackend.Service != nil {
		seen[item.Spec.DefaultBackend.Service.Name] = struct{}{}
	}
	for _, rule := range item.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				seen[path.Backend.Service.Name] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func resourceID(clusterID, apiVersion, kind, namespace, name string) string {
	identity := strings.Join([]string{clusterID, apiVersion, kind, namespace, name}, "\x00")
	return "k8s_" + base64.RawURLEncoding.EncodeToString([]byte(identity))
}

func edgeID(source, target, relation, fact string) string {
	digest := sha256.Sum256([]byte(source + "\x00" + relation + "\x00" + target + "\x00" + fact))
	return "edge_" + hex.EncodeToString(digest[:12])
}

func eventID(item corev1.Event) string {
	identity := string(item.UID)
	if identity == "" {
		identity = item.Namespace + "/" + item.Name
	}
	return "event_" + base64.RawURLEncoding.EncodeToString([]byte(identity))
}

func eventTime(item corev1.Event) time.Time {
	if !item.EventTime.IsZero() {
		return item.EventTime.Time.UTC()
	}
	if item.Series != nil && !item.Series.LastObservedTime.IsZero() {
		return item.Series.LastObservedTime.Time.UTC()
	}
	if !item.LastTimestamp.IsZero() {
		return item.LastTimestamp.Time.UTC()
	}
	if !item.FirstTimestamp.IsZero() {
		return item.FirstTimestamp.Time.UTC()
	}
	return item.CreationTimestamp.Time.UTC()
}

func bounded(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func sanitized(value string, max int) string {
	value, _ = k8sread.SanitizeText(value, max)
	return strings.TrimSpace(value)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

var _ infrastructure.Reader = (*Reader)(nil)
