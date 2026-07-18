package k8sread

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestListPodsIsNamespaceAllowlistedAndBounded(t *testing.T) {
	started := metav1.NewTime(time.Date(2026, time.July, 17, 8, 0, 0, 0, time.UTC))
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "cloudops-demo",
			Name:      "workload-0",
			Labels:    map[string]string{"app": "cloudops-demo-workload"},
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "ReplicaSet",
				Name: "workload-abc",
			}},
		},
		Spec: corev1.PodSpec{
			NodeName:   "demo-node",
			Containers: []corev1.Container{{Name: "workload"}},
		},
		Status: corev1.PodStatus{
			Phase:     corev1.PodRunning,
			PodIP:     "10.0.0.8",
			StartTime: &started,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         "workload",
				Ready:        true,
				RestartCount: 2,
			}},
		},
	})
	service := NewServiceWithClient(client, Config{
		Enabled:           true,
		AllowedNamespaces: []string{"cloudops-demo"},
		DefaultNamespace:  "cloudops-demo",
		RequestTimeout:    time.Second,
	})
	service.now = func() time.Time { return time.Date(2026, time.July, 17, 8, 5, 0, 0, time.UTC) }

	pods, err := service.ListPods(context.Background(), QueryOptions{Limit: MaxLimit + 100})
	if err != nil {
		t.Fatalf("ListPods() error = %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("ListPods() returned %d pods, want 1", len(pods))
	}
	got := pods[0]
	if got.Name != "workload-0" || got.ReadyContainers != 1 || got.RestartCount != 2 || got.OwnerKind != "ReplicaSet" || got.OwnerName != "workload-abc" {
		t.Fatalf("unexpected bounded pod summary: %+v", got)
	}
	if _, err := service.ListPods(context.Background(), QueryOptions{Namespace: "default"}); !errors.Is(err, ErrNamespaceNotAllowed) {
		t.Fatalf("ListPods(default) error = %v, want ErrNamespaceNotAllowed", err)
	}
	if _, err := service.ListPods(context.Background(), QueryOptions{LabelSelector: "app=$(unsafe)"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ListPods(unsafe selector) error = %v, want ErrInvalidArgument", err)
	}
}

func TestSanitizeTextRedactsSecretsAndPreservesUTF8Boundary(t *testing.T) {
	value := "Authorization=secret-value\npassword: hunter2\nBearer abc.def.ghi\n正常文本"
	sanitized, truncated := SanitizeText(value, 48)
	if !truncated {
		t.Fatal("SanitizeText() truncated = false, want true")
	}
	for _, secret := range []string{"secret-value", "hunter2", "abc.def.ghi"} {
		if strings.Contains(sanitized, secret) {
			t.Fatalf("SanitizeText() leaked %q in %q", secret, sanitized)
		}
	}
	if !strings.Contains(sanitized, "[REDACTED]") {
		t.Fatalf("SanitizeText() = %q, want redaction marker", sanitized)
	}
}
