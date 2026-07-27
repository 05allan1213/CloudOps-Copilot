package k8schange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/operation"
)

type OperationScaleAdapter struct {
	executors map[string]*ControlledScaleExecutor
	freezes   operation.ChangeFreezeReader
	poll      time.Duration
}

func NewOperationScaleAdapter(
	executors map[string]*ControlledScaleExecutor,
	freezes operation.ChangeFreezeReader,
	poll time.Duration,
) (*OperationScaleAdapter, error) {
	if freezes == nil || poll <= 0 {
		return nil, operation.ErrInvalidArgument
	}
	copyExecutors := make(map[string]*ControlledScaleExecutor, len(executors))
	for clusterID, executor := range executors {
		clusterID = strings.TrimSpace(clusterID)
		if clusterID == "" || executor == nil {
			return nil, operation.ErrInvalidArgument
		}
		copyExecutors[clusterID] = executor
	}
	return &OperationScaleAdapter{executors: copyExecutors, freezes: freezes, poll: poll}, nil
}

func (*OperationScaleAdapter) OperationType() string { return operation.ActionScaleDeployment }

type scaleParameters struct {
	Replicas int32 `json:"replicas"`
}

type scaleVerificationIntent struct {
	Type             string `json:"type"`
	ExpectedReplicas int32  `json:"expected_replicas"`
}

type scalePrecondition struct {
	Type                    string  `json:"type"`
	ExpectedReplicas        *int32  `json:"expected_replicas,omitempty"`
	ExpectedResourceVersion *string `json:"expected_resource_version,omitempty"`
	ExpectedEnabled         *bool   `json:"expected_enabled,omitempty"`
	ExpectedVersion         *uint64 `json:"expected_version,omitempty"`
}

type scaleToken struct {
	Target                  operation.OperationTarget `json:"target"`
	Replicas                int32                     `json:"replicas"`
	ExpectedReplicas        int32                     `json:"expected_replicas"`
	ExpectedResourceVersion string                    `json:"expected_resource_version"`
	ExpectedFreezeVersion   uint64                    `json:"expected_freeze_version"`
}

func (a *OperationScaleAdapter) Prepare(ctx context.Context, subject operation.Subject) (operation.PreparedEffect, error) {
	if subject.SubjectType != operation.SubjectOperationPlan || subject.Authority != "high_impact" || subject.OperationType != operation.ActionScaleDeployment {
		return operation.PreparedEffect{}, operation.ErrInvalidArgument
	}
	var target operation.OperationTarget
	if err := operation.DecodeExact(subject.Target, &target); err != nil || operation.ValidateTarget(target) != nil {
		return operation.PreparedEffect{}, operation.ErrInvalidArgument
	}
	var parameters, intended scaleParameters
	if err := operation.DecodeExact(subject.Parameters, &parameters); err != nil {
		return operation.PreparedEffect{}, err
	}
	if err := operation.DecodeExact(subject.IntendedState, &intended); err != nil || intended.Replicas != parameters.Replicas {
		return operation.PreparedEffect{}, operation.ErrInvalidArgument
	}
	var intent scaleVerificationIntent
	if err := operation.DecodeExact(subject.VerificationIntent, &intent); err != nil ||
		intent.Type != operation.ActionScaleDeployment || intent.ExpectedReplicas != parameters.Replicas {
		return operation.PreparedEffect{}, operation.ErrInvalidArgument
	}
	var preconditions []scalePrecondition
	if err := operation.DecodeExact(subject.Preconditions, &preconditions); err != nil || len(preconditions) != 3 {
		return operation.PreparedEffect{}, operation.ErrInvalidArgument
	}
	var expectedReplicas int32
	var expectedResourceVersion string
	var expectedFreezeVersion uint64
	foundReplicas, foundVersion, foundFreeze := false, false, false
	for _, precondition := range preconditions {
		switch precondition.Type {
		case "deployment.replicas":
			if foundReplicas || precondition.ExpectedReplicas == nil {
				return operation.PreparedEffect{}, operation.ErrInvalidArgument
			}
			expectedReplicas, foundReplicas = *precondition.ExpectedReplicas, true
		case "deployment.resource_version":
			if foundVersion || precondition.ExpectedResourceVersion == nil || strings.TrimSpace(*precondition.ExpectedResourceVersion) == "" {
				return operation.PreparedEffect{}, operation.ErrInvalidArgument
			}
			expectedResourceVersion, foundVersion = *precondition.ExpectedResourceVersion, true
		case "local.change_freeze":
			if foundFreeze || precondition.ExpectedEnabled == nil || *precondition.ExpectedEnabled || precondition.ExpectedVersion == nil {
				return operation.PreparedEffect{}, operation.ErrInvalidArgument
			}
			expectedFreezeVersion, foundFreeze = *precondition.ExpectedVersion, true
		default:
			return operation.PreparedEffect{}, operation.ErrInvalidArgument
		}
	}
	if !foundReplicas || !foundVersion || !foundFreeze {
		return operation.PreparedEffect{}, operation.ErrInvalidArgument
	}
	executor := a.executors[target.ClusterID]
	if executor == nil {
		return operation.PreparedEffect{}, operation.ErrProviderUnavailable
	}
	current, err := executor.ObserveDeployment(ctx, target.Namespace, target.WorkloadName)
	if err != nil {
		return operation.PreparedEffect{}, fmt.Errorf("%w: %v", operation.ErrProviderUnavailable, err)
	}
	freeze, err := a.freezes.ChangeFreeze(ctx, target)
	if err != nil {
		return operation.PreparedEffect{}, err
	}
	if current.DesiredReplicas != expectedReplicas || current.ResourceVersion != expectedResourceVersion ||
		freeze.Enabled || freeze.RowVersion != expectedFreezeVersion {
		return operation.PreparedEffect{}, fmt.Errorf("%w: deployment or change-freeze state changed", operation.ErrPreconditionFailed)
	}
	token, err := json.Marshal(scaleToken{
		Target: target, Replicas: parameters.Replicas, ExpectedReplicas: expectedReplicas,
		ExpectedResourceVersion: expectedResourceVersion, ExpectedFreezeVersion: expectedFreezeVersion,
	})
	if err != nil {
		return operation.PreparedEffect{}, err
	}
	before, err := scaleObservation(target.ClusterID, current, freeze, expectedReplicas, "Kubernetes scale preconditions matched")
	if err != nil {
		return operation.PreparedEffect{}, err
	}
	return operation.PreparedEffect{External: true, Before: before, Token: token}, nil
}

func (a *OperationScaleAdapter) Apply(ctx context.Context, _ operation.Subject, prepared operation.PreparedEffect) (operation.Observation, error) {
	if !prepared.External {
		return operation.Observation{}, operation.ErrInvalidArgument
	}
	var token scaleToken
	if err := operation.DecodeExact(prepared.Token, &token); err != nil || operation.ValidateTarget(token.Target) != nil {
		return operation.Observation{}, operation.ErrInvalidArgument
	}
	executor := a.executors[token.Target.ClusterID]
	if executor == nil {
		return operation.Observation{}, operation.ErrProviderUnavailable
	}
	freeze, err := a.freezes.ChangeFreeze(ctx, token.Target)
	if err != nil {
		return operation.Observation{}, err
	}
	if freeze.Enabled || freeze.RowVersion != token.ExpectedFreezeVersion {
		return operation.Observation{}, fmt.Errorf("%w: change freeze changed before Kubernetes effect", operation.ErrPreconditionFailed)
	}
	current, err := executor.ScaleDeploymentExact(ctx, token.Target.Namespace, token.Target.WorkloadName,
		token.Replicas, token.ExpectedResourceVersion, token.ExpectedReplicas)
	if err != nil {
		if strings.Contains(err.Error(), "precondition failed") || strings.Contains(err.Error(), "conflict") {
			return operation.Observation{}, fmt.Errorf("%w: %v", operation.ErrPreconditionFailed, err)
		}
		return operation.Observation{}, err
	}
	for !scaleReady(current, token.Replicas) {
		select {
		case <-ctx.Done():
			observation, observeErr := scaleObservation(token.Target.ClusterID, current, freeze, token.Replicas,
				"current Kubernetes rollout has not reached the authorized replica state")
			if observeErr != nil {
				return operation.Observation{}, observeErr
			}
			observation.Verified = false
			return observation, nil
		case <-time.After(a.poll):
		}
		current, err = executor.ObserveDeployment(ctx, token.Target.Namespace, token.Target.WorkloadName)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			return operation.Observation{}, err
		}
	}
	return scaleObservation(token.Target.ClusterID, current, freeze, token.Replicas,
		"current Kubernetes rollout matches the authorized replica state")
}

func scaleReady(value DeploymentScaleObservation, replicas int32) bool {
	return value.DesiredReplicas == replicas && value.UpdatedReplicas == replicas &&
		value.ReadyReplicas == replicas && value.AvailableReplicas == replicas &&
		value.ObservedGeneration >= value.Generation
}

func scaleObservation(
	clusterID string,
	value DeploymentScaleObservation,
	freeze operation.ChangeFreezeState,
	expectedReplicas int32,
	summary string,
) (operation.Observation, error) {
	identity, err := json.Marshal(map[string]any{
		"provider": "kubernetes", "cluster_id": clusterID, "namespace": value.Namespace,
		"workload_kind": "Deployment", "workload_name": value.Name,
		"resource_version": value.ResourceVersion, "generation": value.Generation,
	})
	if err != nil {
		return operation.Observation{}, err
	}
	evidence, err := json.Marshal(map[string]any{
		"deployment": value, "expected_replicas": expectedReplicas,
		"change_freeze": map[string]any{"enabled": freeze.Enabled, "row_version": freeze.RowVersion},
	})
	if err != nil {
		return operation.Observation{}, err
	}
	return operation.Observation{
		Source: "kubernetes", ProviderIdentity: identity, Evidence: evidence,
		Verified: scaleReady(value, expectedReplicas), Summary: summary, ObservedAt: value.ObservedAt.UTC(),
	}, nil
}
