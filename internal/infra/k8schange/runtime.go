package k8schange

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

type Reader struct {
	client            kubernetes.Interface
	allowedNamespaces map[string]struct{}
	allowAll          bool
	timeout           time.Duration
}

var _ change.RuntimeReader = (*Reader)(nil)
var _ verification.RolloutReader = (*Reader)(nil)

func New(client kubernetes.Interface, allowedNamespaces []string, timeout time.Duration) (*Reader, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: kubernetes client required", change.ErrInvalidArgument)
	}
	allowed := map[string]struct{}{}
	allowAll := false
	for _, namespace := range allowedNamespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "*" {
			allowAll = true
		}
		if namespace != "" {
			allowed[namespace] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("%w: namespace allowlist required", change.ErrInvalidArgument)
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Reader{client: client, allowedNamespaces: allowed, allowAll: allowAll, timeout: timeout}, nil
}

// ObserveDeployment returns a bounded typed rollout summary. It exposes no
// mutation, logs, exec, generic resource, or JSONPath surface.
func (r *Reader) ObserveDeployment(ctx context.Context, cluster, namespace, name string) (verification.RolloutObservation, error) {
	if strings.TrimSpace(cluster) == "" || strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return verification.RolloutObservation{}, verification.ErrInvalidArgument
	}
	if !r.allowAll {
		if _, ok := r.allowedNamespaces[namespace]; !ok {
			return verification.RolloutObservation{}, verification.ErrNotAllowed
		}
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	deployment, err := r.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return verification.RolloutObservation{}, err
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return verification.RolloutObservation{}, verification.ErrInvalidArgument
	}
	pods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String(), Limit: 200})
	if err != nil {
		return verification.RolloutObservation{}, err
	}
	ready := int32(0)
	for _, pod := range pods.Items {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				ready++
				break
			}
		}
	}
	progressing, available, progressDeadlineExceeded := false, false, false
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == "Progressing" && condition.Status == corev1.ConditionTrue {
			progressing = true
		}
		if condition.Type == "Progressing" && condition.Reason == "ProgressDeadlineExceeded" {
			progressDeadlineExceeded = true
		}
		if condition.Type == "Available" && condition.Status == corev1.ConditionTrue {
			available = true
		}
	}
	deadline := time.Duration(0)
	if deployment.Spec.ProgressDeadlineSeconds != nil {
		deadline = time.Duration(*deployment.Spec.ProgressDeadlineSeconds) * time.Second
	}
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	return verification.RolloutObservation{Generation: deployment.Generation, ObservedGeneration: deployment.Status.ObservedGeneration, RolloutRevision: deployment.Annotations["deployment.kubernetes.io/revision"], DesiredReplicas: desired, UpdatedReplicas: deployment.Status.UpdatedReplicas, AvailableReplicas: deployment.Status.AvailableReplicas, UnavailableReplicas: deployment.Status.UnavailableReplicas, Progressing: progressing, Available: available, ProgressDeadline: deadline, ProgressDeadlineExceeded: progressDeadlineExceeded, PodsReady: ready, PodsTotal: int32(len(pods.Items))}, nil
}

func (r *Reader) ResolveRuntime(ctx context.Context, namespace, kind, name string) ([]change.ContainerRuntime, error) {
	if !r.allowAll {
		if _, ok := r.allowedNamespaces[namespace]; !ok {
			return nil, fmt.Errorf("%w: namespace", change.ErrNotAllowed)
		}
	}
	if namespace == "" || name == "" {
		return nil, change.ErrInvalidArgument
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var template corev1.PodTemplateSpec
	var selector *metav1.LabelSelector
	var revision string
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "deployment":
		item, err := r.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		template, selector, revision = item.Spec.Template, item.Spec.Selector, item.Annotations["deployment.kubernetes.io/revision"]
	case "statefulset":
		item, err := r.client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		template, selector = item.Spec.Template, item.Spec.Selector
	case "daemonset":
		item, err := r.client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		template, selector = item.Spec.Template, item.Spec.Selector
	default:
		return nil, fmt.Errorf("%w: unsupported workload kind", change.ErrInvalidArgument)
	}
	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, change.ErrInvalidArgument
	}
	pods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector.String(), Limit: 200})
	if err != nil {
		return nil, err
	}
	digests := containerDigests(pods.Items)
	result := make([]change.ContainerRuntime, 0, len(template.Spec.Containers))
	for _, container := range template.Spec.Containers {
		result = append(result, change.ContainerRuntime{ContainerName: container.Name, Image: container.Image, ImageDigest: digests[container.Name], WorkloadKind: strings.ToLower(kind), WorkloadName: name, Namespace: namespace, DeploymentRevision: revision, Labels: safeMetadata(template.Labels), Annotations: safeMetadata(template.Annotations)})
	}
	return result, nil
}

func containerDigests(pods []corev1.Pod) map[string]string {
	result := map[string]string{}
	for _, pod := range pods {
		for _, status := range pod.Status.ContainerStatuses {
			digest := status.ImageID
			if index := strings.Index(digest, "@"); index >= 0 {
				digest = digest[index+1:]
			} else if index := strings.Index(digest, "sha256:"); index >= 0 {
				digest = digest[index:]
			} else {
				digest = ""
			}
			if digest != "" {
				if prior := result[status.Name]; prior == "" || prior == digest {
					result[status.Name] = digest
				} else {
					result[status.Name] = ""
				}
			}
		}
	}
	return result
}
func safeMetadata(values map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range values {
		if len(utilvalidation.IsValidLabelValue(value)) == 0 && (strings.HasPrefix(key, "app.kubernetes.io/") || strings.HasPrefix(key, "argocd.argoproj.io/") || strings.HasPrefix(key, "cloudops.io/")) {
			result[key] = change.BoundUTF8(value, 255)
		}
	}
	return result
}
