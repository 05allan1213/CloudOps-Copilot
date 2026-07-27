package k8schange

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/operation"
)

func TestOperationScaleAdapterUsesExactPreconditionsAndCurrentVerification(t *testing.T) {
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cloudops-api", Namespace: "demo", ResourceVersion: "rv-1", Generation: 1,
		},
		Spec: appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1,
		},
	}
	client := fake.NewSimpleClientset(deployment)
	client.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		return true, &autoscalingv1.Scale{
			ObjectMeta: metav1.ObjectMeta{Name: "cloudops-api", Namespace: "demo", ResourceVersion: "rv-1"},
			Spec:       autoscalingv1.ScaleSpec{Replicas: 1},
		}, nil
	})
	client.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		scale, ok := action.(k8stesting.UpdateAction).GetObject().(*autoscalingv1.Scale)
		if !ok {
			t.Fatalf("scale update object=%T", action.(k8stesting.UpdateAction).GetObject())
		}
		updated := deployment.DeepCopy()
		updated.ResourceVersion = "rv-2"
		updated.Generation = 2
		updated.Spec.Replicas = &scale.Spec.Replicas
		updated.Status.ObservedGeneration = 2
		updated.Status.UpdatedReplicas = scale.Spec.Replicas
		updated.Status.ReadyReplicas = scale.Spec.Replicas
		updated.Status.AvailableReplicas = scale.Spec.Replicas
		if err := client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), updated, "demo"); err != nil {
			t.Fatal(err)
		}
		return true, &autoscalingv1.Scale{
			ObjectMeta: metav1.ObjectMeta{Name: scale.Name, Namespace: scale.Namespace, ResourceVersion: "rv-2"},
			Spec:       scale.Spec,
		}, nil
	})
	executor, err := NewControlledScaleExecutor(client, ControlledScaleConfig{
		AllowedNamespaces: []string{"demo"}, MaxReplicas: 5, RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	target := operation.OperationTarget{
		ClusterID: "cloudops-local", Environment: "local", Namespace: "demo",
		WorkloadKind: "Deployment", WorkloadName: "cloudops-api",
	}
	freezes := &fixedChangeFreezeReader{state: operation.ChangeFreezeState{Target: target}}
	adapter, err := NewOperationScaleAdapter(map[string]*ControlledScaleExecutor{
		"cloudops-local": executor,
	}, freezes, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	subject := scaleOperationSubject(t, target, 2, "rv-1", 1, 0)
	prepared, err := adapter.Prepare(context.Background(), subject)
	if err != nil || !prepared.External || !prepared.Before.Verified {
		t.Fatalf("prepared=%#v error=%v", prepared, err)
	}
	observation, err := adapter.Apply(context.Background(), subject, prepared)
	if err != nil || !observation.Verified || observation.Source != "kubernetes" {
		t.Fatalf("observation=%#v error=%v", observation, err)
	}
	var evidence struct {
		Deployment DeploymentScaleObservation `json:"deployment"`
	}
	if err = json.Unmarshal(observation.Evidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Deployment.ResourceVersion != "rv-2" || evidence.Deployment.DesiredReplicas != 2 || evidence.Deployment.AvailableReplicas != 2 {
		t.Fatalf("current verification evidence=%#v", evidence)
	}
	updates := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource == "deployments" && action.GetSubresource() == "scale" {
			updates++
		}
	}
	if updates != 1 {
		t.Fatalf("scale updates=%d actions=%#v", updates, client.Actions())
	}

	stale := scaleOperationSubject(t, target, 3, "stale-rv", 2, 0)
	if _, err = adapter.Prepare(context.Background(), stale); !errors.Is(err, operation.ErrPreconditionFailed) {
		t.Fatalf("stale resourceVersion error=%v", err)
	}
	if _, err = executor.ObserveDeployment(context.Background(), "outside", "cloudops-api"); err == nil {
		t.Fatal("namespace outside explicit write allowlist unexpectedly observed")
	}
}

type fixedChangeFreezeReader struct {
	state operation.ChangeFreezeState
}

func (reader *fixedChangeFreezeReader) ChangeFreeze(context.Context, operation.OperationTarget) (operation.ChangeFreezeState, error) {
	return reader.state, nil
}

func scaleOperationSubject(
	t *testing.T,
	target operation.OperationTarget,
	replicas int32,
	resourceVersion string,
	expectedReplicas int32,
	freezeVersion uint64,
) operation.Subject {
	t.Helper()
	targetJSON, _ := json.Marshal(target)
	parameters, _ := json.Marshal(map[string]any{"replicas": replicas})
	preconditions, _ := json.Marshal([]map[string]any{
		{"type": "deployment.replicas", "expected_replicas": expectedReplicas},
		{"type": "deployment.resource_version", "expected_resource_version": resourceVersion},
		{"type": "local.change_freeze", "expected_enabled": false, "expected_version": freezeVersion},
	})
	verification, _ := json.Marshal(map[string]any{
		"type": operation.ActionScaleDeployment, "expected_replicas": replicas,
	})
	return operation.Subject{
		SubjectType: operation.SubjectOperationPlan, Authority: "high_impact",
		OperationType: operation.ActionScaleDeployment, Target: targetJSON,
		Parameters: parameters, IntendedState: parameters, Preconditions: preconditions,
		VerificationIntent: verification,
	}
}
