package action

import (
	"context"
	"fmt"
	"time"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	k8sreader "server-web/copilot/k8s"
)

type ClientK8sExecutorConfig struct {
	AllowedNamespaces []string
	MaxReplicas       int
	Now               func() time.Time
}

type ClientK8sExecutor struct {
	client            kubernetes.Interface
	allowedNamespaces map[string]struct{}
	maxReplicas       int
	now               func() time.Time
}

func NewClientK8sExecutor(client kubernetes.Interface, cfg ClientK8sExecutorConfig) *ClientK8sExecutor {
	maxReplicas := cfg.MaxReplicas
	if maxReplicas <= 0 {
		maxReplicas = 10
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedNamespaces))
	for _, namespace := range cfg.AllowedNamespaces {
		if namespace != "" {
			allowed[namespace] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		allowed["default"] = struct{}{}
	}
	return &ClientK8sExecutor{client: client, allowedNamespaces: allowed, maxReplicas: maxReplicas, now: now}
}

func (e *ClientK8sExecutor) RestartDeployment(ctx context.Context, namespace, name string) (ActionResult, error) {
	if err := e.validateTarget(namespace, name); err != nil {
		return ActionResult{}, err
	}
	deployment, err := e.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ActionResult{}, err
	}
	oldAnnotation := deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]
	annotations := deployment.Spec.Template.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	restartedAt := e.now().UTC().Format(time.RFC3339)
	annotations["kubectl.kubernetes.io/restartedAt"] = restartedAt
	deployment.Spec.Template.Annotations = annotations
	updated, err := e.client.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return ActionResult{}, err
	}
	replicas := 0
	if updated.Spec.Replicas != nil {
		replicas = int(*updated.Spec.Replicas)
	}
	readyReplicas := int(updated.Status.ReadyReplicas)
	return ActionResult{
		ActionType:    ActionTypeRestartDeployment,
		Target:        fmt.Sprintf("%s/%s", namespace, name),
		Replicas:      &replicas,
		OldReplicas:   &replicas,
		NewReplicas:   &replicas,
		ReadyReplicas: &readyReplicas,
		OldAnnotation: oldAnnotation,
		NewAnnotation: restartedAt,
		Message:       fmt.Sprintf("deployment restart requested at %s", restartedAt),
	}, nil
}

func (e *ClientK8sExecutor) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) (ActionResult, error) {
	if err := e.validateTarget(namespace, name); err != nil {
		return ActionResult{}, err
	}
	if replicas < 1 || replicas > int32(e.maxReplicas) {
		return ActionResult{}, fmt.Errorf("%w: replicas must be in range 1-%d", ErrInvalidAction, e.maxReplicas)
	}
	scale, err := e.client.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ActionResult{}, err
	}
	oldReplicas := int(scale.Spec.Replicas)
	scale.Spec.Replicas = replicas
	updated, err := e.client.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	if err != nil {
		return ActionResult{}, err
	}
	if updated == nil {
		updated = &autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: replicas}}
	}
	replicasValue := int(replicas)
	readyReplicas := int(updated.Status.Replicas)
	return ActionResult{
		ActionType:    ActionTypeScaleDeployment,
		Target:        fmt.Sprintf("%s/%s", namespace, name),
		Replicas:      &replicasValue,
		OldReplicas:   &oldReplicas,
		NewReplicas:   &replicasValue,
		ReadyReplicas: &readyReplicas,
		Message:       fmt.Sprintf("deployment scaled from %d to %d replicas", oldReplicas, replicas),
	}, nil
}

func (e *ClientK8sExecutor) validateTarget(namespace, name string) error {
	if e == nil || e.client == nil {
		return ErrExecutionDisabled
	}
	if _, ok := e.allowedNamespaces[namespace]; !ok {
		return fmt.Errorf("%w: namespace %s", ErrForbidden, namespace)
	}
	if err := k8sreader.ValidateName("namespace", namespace); err != nil {
		return err
	}
	if err := k8sreader.ValidateName("name", name); err != nil {
		return err
	}
	return nil
}
