package investigationread

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

func (t *Toolset) inspectWorkload(ctx context.Context, request agent.InvestigationToolRequest) (agent.ToolObservation, error) {
	if request.Action.TemplateID != TemplateWorkloadV1 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	var params map[string]json.RawMessage
	if err := decodeParameters(request.Action.BoundedParameters, &params); err != nil || len(params) != 0 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	deployment, err := t.cfg.Kubernetes.AppsV1().Deployments(t.cfg.Target.Namespace).Get(ctx, t.cfg.Target.Workload, metav1.GetOptions{})
	if err != nil {
		return unavailable(request.Action, "kubernetes", "kubernetes/workload-snapshot", err), nil
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil || selector.Empty() {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	replicaSets, rsErr := t.cfg.Kubernetes.AppsV1().ReplicaSets(t.cfg.Target.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String(), Limit: 100})
	pods, podErr := t.cfg.Kubernetes.CoreV1().Pods(t.cfg.Target.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String(), Limit: 200})
	services, svcErr := t.cfg.Kubernetes.CoreV1().Services(t.cfg.Target.Namespace).List(ctx, metav1.ListOptions{Limit: 100})
	if rsErr != nil || podErr != nil || svcErr != nil {
		return unavailable(request.Action, "kubernetes", "kubernetes/workload-snapshot", errorsJoin(rsErr, podErr, svcErr)), nil
	}
	matchedServices := matchingServices(services.Items, deployment.Spec.Template.Labels)
	endpointCount := 0
	for _, service := range matchedServices {
		slices, readErr := t.cfg.Kubernetes.DiscoveryV1().EndpointSlices(t.cfg.Target.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.Set{discoveryv1.LabelServiceName: service.Name}.String(), Limit: 100})
		if readErr != nil {
			return unavailable(request.Action, "kubernetes", "kubernetes/workload-snapshot", readErr), nil
		}
		endpointCount += len(slices.Items)
	}
	container, count := deploymentContainer(deployment, t.cfg.Target.Container)
	if count != 1 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	envPresent := false
	for _, item := range container.Env {
		if item.Name == t.cfg.Target.EnvKey {
			envPresent = true
		}
	}
	readyPods := 0
	for _, pod := range pods.Items {
		if podReady(pod) {
			readyPods++
		}
	}
	attributes := map[string]string{
		"deployment": deployment.Name, "namespace": deployment.Namespace,
		"generation": strconv.FormatInt(deployment.Generation, 10), "observed_generation": strconv.FormatInt(deployment.Status.ObservedGeneration, 10),
		"replicasets": strconv.Itoa(len(replicaSets.Items)), "pods": strconv.Itoa(len(pods.Items)), "ready_pods": strconv.Itoa(readyPods),
		"services": strconv.Itoa(len(matchedServices)), "endpoint_slices": strconv.Itoa(endpointCount), "container": t.cfg.Target.Container,
	}
	facts := []agent.EvidenceFact{typedFact(request, "workload.subject_confirmed", "kubernetes", "kubernetes/workload-snapshot", "authoritative", "support", true, attributes)}
	envType := "kubernetes.required_env_absent"
	if envPresent {
		envType = "kubernetes.required_env_present"
	}
	facts = append(facts, typedFact(request, envType, "kubernetes", "kubernetes/workload-snapshot", "authoritative", "support", true, map[string]string{"env_key": t.cfg.Target.EnvKey, "container": t.cfg.Target.Container}))
	return available(request.Action, "kubernetes", "kubernetes/workload-snapshot", fmt.Sprintf("Deployment snapshot includes %d ReplicaSets, %d Pods, %d Services and %d EndpointSlices", len(replicaSets.Items), len(pods.Items), len(matchedServices), endpointCount), facts, ""), nil
}

func (t *Toolset) inspectEvents(ctx context.Context, request agent.InvestigationToolRequest) (agent.ToolObservation, error) {
	if request.Action.TemplateID != TemplateEventsV1 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	var params struct {
		Window string `json:"window"`
		Limit  int    `json:"limit"`
	}
	if err := decodeParameters(request.Action.BoundedParameters, &params); err != nil {
		return agent.ToolObservation{}, err
	}
	window, err := boundedWindow(params.Window, 15*time.Minute)
	if err != nil {
		return agent.ToolObservation{}, err
	}
	if params.Limit == 0 {
		params.Limit = 50
	}
	if params.Limit < 1 || params.Limit > 100 {
		return agent.ToolObservation{}, agent.ErrInvalidArgument
	}
	selector := fmt.Sprintf("involvedObject.kind=Deployment,involvedObject.name=%s", t.cfg.Target.Workload)
	events, readErr := t.cfg.Kubernetes.CoreV1().Events(t.cfg.Target.Namespace).List(ctx, metav1.ListOptions{FieldSelector: selector, Limit: int64(params.Limit)})
	if readErr != nil {
		return unavailable(request.Action, "kubernetes", "kubernetes/events", readErr), nil
	}
	cutoff, warnings, reasons := t.cfg.Now().Add(-window), 0, map[string]struct{}{}
	for _, event := range events.Items {
		seen := event.LastTimestamp.Time
		if seen.IsZero() {
			seen = event.EventTime.Time
		}
		if seen.Before(cutoff) {
			continue
		}
		if strings.EqualFold(event.Type, corev1.EventTypeWarning) {
			warnings++
			if event.Reason != "" {
				reasons[safeText(event.Reason, 64)] = struct{}{}
			}
		}
	}
	reasonList := make([]string, 0, len(reasons))
	for reason := range reasons {
		reasonList = append(reasonList, reason)
	}
	sort.Strings(reasonList)
	factType := "kubernetes.no_warning_events"
	if warnings > 0 {
		factType = "kubernetes.warning_events_present"
	}
	fact := typedFact(request, factType, "kubernetes", "kubernetes/events", "authoritative", "support", true, map[string]string{"warning_count": strconv.Itoa(warnings), "reasons": strings.Join(reasonList, ","), "window": window.String()})
	return available(request.Action, "kubernetes", "kubernetes/events", "bounded Kubernetes Event query returned normalized reason counts", []agent.EvidenceFact{fact}, ""), nil
}

func deploymentContainer(deployment *appsv1.Deployment, name string) (corev1.Container, int) {
	var result corev1.Container
	count := 0
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == name {
			result, count = container, count+1
		}
	}
	return result, count
}
func podReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
func matchingServices(items []corev1.Service, podLabels map[string]string) []corev1.Service {
	result := make([]corev1.Service, 0)
	for _, service := range items {
		if len(service.Spec.Selector) > 0 && labels.SelectorFromSet(service.Spec.Selector).Matches(labels.Set(podLabels)) {
			result = append(result, service)
		}
	}
	return result
}
func errorsJoin(values ...error) error {
	var parts []string
	for _, value := range values {
		if value != nil {
			parts = append(parts, value.Error())
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}
