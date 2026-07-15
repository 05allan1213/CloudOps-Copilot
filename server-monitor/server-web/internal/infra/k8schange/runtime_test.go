package k8schange

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"

	"server-web/internal/change"
)

func TestResolveRuntimeDigestRevisionAndAllowlist(t *testing.T) {
	selector := map[string]string{"app": "checkout"}
	objects := []runtime.Object{
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "checkout", Namespace: "payments", Annotations: map[string]string{"deployment.kubernetes.io/revision": "7"}},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: selector},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "checkout", "app.kubernetes.io/version": "v2"}, Annotations: map[string]string{"argocd.argoproj.io/instance": "checkout-prod", "password": "must-not-copy"}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "registry/checkout:v2", ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromInt32(8080)}}}}}},
				},
			},
		},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "checkout-1", Namespace: "payments", Labels: selector}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "app", ImageID: "docker-pullable://registry/checkout@sha256:" + strings.Repeat("a", 64)}}}},
	}
	reader, err := New(fake.NewSimpleClientset(objects...), []string{"payments"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reader.ResolveRuntime(context.Background(), "payments", "Deployment", "checkout")
	if err != nil || len(result) != 1 || result[0].ImageDigest != "sha256:"+strings.Repeat("a", 64) || result[0].DeploymentRevision != "7" || result[0].Annotations["password"] != "" || result[0].Annotations["argocd.argoproj.io/instance"] != "checkout-prod" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := reader.ResolveRuntime(context.Background(), "other", "Deployment", "checkout"); !errors.Is(err, change.ErrNotAllowed) {
		t.Fatalf("allowlist err=%v", err)
	}
}

func TestConflictingPodDigestsBecomeUnknown(t *testing.T) {
	selector := map[string]string{"app": "checkout"}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "checkout", Namespace: "payments"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: selector}, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "registry/checkout:latest"}}}}}}
	pod := func(name, digest string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "payments", Labels: selector}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "app", ImageID: "registry/checkout@sha256:" + digest}}}}
	}
	reader, _ := New(fake.NewSimpleClientset(deployment, pod("one", strings.Repeat("a", 64)), pod("two", strings.Repeat("b", 64))), []string{"payments"}, time.Second)
	result, err := reader.ResolveRuntime(context.Background(), "payments", "deployment", "checkout")
	if err != nil || result[0].ImageDigest != "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
