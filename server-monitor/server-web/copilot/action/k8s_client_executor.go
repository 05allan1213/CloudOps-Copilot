package action

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	k8sreader "server-web/copilot/k8s"
)

type ClientK8sExecutorConfig struct {
	AllowedNamespaces []string
	MaxReplicas       int
	RequestTimeout    time.Duration
	Now               func() time.Time
}

type ClientK8sExecutor struct {
	client            kubernetes.Interface
	allowedNamespaces map[string]struct{}
	maxReplicas       int
	requestTimeout    time.Duration
	now               func() time.Time
}

func NewClientK8sExecutor(client kubernetes.Interface, cfg ClientK8sExecutorConfig) *ClientK8sExecutor {
	maxReplicas := cfg.MaxReplicas
	if maxReplicas <= 0 {
		maxReplicas = 10
	}
	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = 10 * time.Second
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
	return &ClientK8sExecutor{client: client, allowedNamespaces: allowed, maxReplicas: maxReplicas, requestTimeout: requestTimeout, now: now}
}

func (e *ClientK8sExecutor) RestartDeployment(ctx context.Context, namespace, name string) (ActionResult, error) {
	if err := e.validateTarget(namespace, name); err != nil {
		return ActionResult{}, err
	}
	getCtx, getCancel := e.withRequestTimeout(ctx)
	deployment, err := e.client.AppsV1().Deployments(namespace).Get(getCtx, name, metav1.GetOptions{})
	getCancel()
	if err != nil {
		return ActionResult{}, classifyK8sError(err)
	}
	oldAnnotation := deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]
	restartedAt := e.now().UTC().Format(time.RFC3339)
	patchBytes, err := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]string{
						"kubectl.kubernetes.io/restartedAt": restartedAt,
					},
				},
			},
		},
	})
	if err != nil {
		return ActionResult{}, fmt.Errorf("build restart patch: %w", err)
	}
	patchCtx, patchCancel := e.withRequestTimeout(ctx)
	updated, err := e.client.AppsV1().Deployments(namespace).Patch(patchCtx, name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	patchCancel()
	if err != nil {
		return ActionResult{}, classifyK8sError(err)
	}
	if updated == nil {
		updated = deployment
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
	getCtx, getCancel := e.withRequestTimeout(ctx)
	scale, err := e.client.AppsV1().Deployments(namespace).GetScale(getCtx, name, metav1.GetOptions{})
	getCancel()
	if err != nil {
		return ActionResult{}, classifyK8sError(err)
	}
	oldReplicas := int(scale.Spec.Replicas)
	scale.Spec.Replicas = replicas
	updateCtx, updateCancel := e.withRequestTimeout(ctx)
	updated, err := e.client.AppsV1().Deployments(namespace).UpdateScale(updateCtx, name, scale, metav1.UpdateOptions{})
	updateCancel()
	if err != nil {
		return ActionResult{}, classifyK8sError(err)
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

func (e *ClientK8sExecutor) withRequestTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := 10 * time.Second
	if e != nil && e.requestTimeout > 0 {
		timeout = e.requestTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func classifyK8sError(err error) error {
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
