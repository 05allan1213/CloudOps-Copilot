package action

import (
	"context"
	"errors"
	"fmt"
)

var ErrExecutionDisabled = errors.New("action execution disabled")

type K8sExecutor interface {
	RestartDeployment(ctx context.Context, namespace, name string) (ActionResult, error)
	ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) (ActionResult, error)
}

type DisabledK8sExecutor struct{}

func (DisabledK8sExecutor) RestartDeployment(ctx context.Context, namespace, name string) (ActionResult, error) {
	return ActionResult{ActionType: ActionTypeRestartDeployment, Target: fmt.Sprintf("%s/%s", namespace, name)}, ErrExecutionDisabled
}

func (DisabledK8sExecutor) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) (ActionResult, error) {
	replicasValue := int(replicas)
	return ActionResult{ActionType: ActionTypeScaleDeployment, Target: fmt.Sprintf("%s/%s", namespace, name), Replicas: &replicasValue}, ErrExecutionDisabled
}
