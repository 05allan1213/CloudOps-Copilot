package kubernetestopology

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestReaderProjectsTypedRelationshipsAndSanitizedEvents(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 26, 4, 0, 0, 0, time.UTC)
	replicas := int32(1)
	ready := true
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ops", UID: types.UID("namespace-uid")}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "ops", UID: types.UID("deployment-uid"), Labels: map[string]string{"app": "api"}},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "api", Env: []corev1.EnvVar{{Name: "PUBLIC_MODE", Value: "sensitive-value-not-projected"}, {Name: "REQUIRED_ENV", Value: "present"}},
				}}}},
			},
			Status: appsv1.DeploymentStatus{ReadyReplicas: 1},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: "api-rs", Namespace: "ops", UID: types.UID("replicaset-uid"), OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", UID: types.UID("deployment-uid")}}},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-pod", Namespace: "ops", UID: types.UID("pod-uid"), Labels: map[string]string{"app": "api"}, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-rs", UID: types.UID("replicaset-uid")}}},
			Spec:       corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "api", Image: "example.invalid/api@sha256:deadbeef"}}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.8", ContainerStatuses: []corev1.ContainerStatus{{Name: "api", Ready: true}}},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "ops", UID: types.UID("service-uid")},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "api"}, ClusterIP: "10.96.0.8", Ports: []corev1.ServicePort{{Name: "http", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.FromInt32(8080)}}},
		},
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{Name: "api-endpoints", Namespace: "ops", Labels: map[string]string{discoveryv1.LabelServiceName: "api"}},
			Endpoints:  []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.8"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}, TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "api-pod", UID: types.UID("pod-uid")}}},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "ops", UID: types.UID("ingress-uid")},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{{
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{Name: "api", Port: networkingv1.ServiceBackendPort{Number: 80}},
								},
							}},
						},
					},
				}},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: types.UID("node-uid")},
			Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}, Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "192.0.2.10"}}},
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "api-pod-warning", Namespace: "ops", UID: types.UID("event-uid"), CreationTimestamp: metav1.NewTime(now)},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-pod", Namespace: "ops", UID: types.UID("pod-uid")},
			Type:           "Warning", Reason: "BackOff", Message: "token=supersecret container restarted", Count: 2, LastTimestamp: metav1.NewTime(now),
		},
	)
	client.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.36.1"}
	reader, err := New(client, Config{ClusterID: "cluster-a", AllowedNamespaces: []string{"ops"}, RequestTimeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	reader.now = func() time.Time { return now }

	projection, err := reader.Read(context.Background(), infrastructure.ReadRequest{ClusterID: "cluster-a", Namespaces: []string{"ops"}, Limit: 100})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if projection.Partial || projection.Truncated {
		t.Fatalf("Read() partial=%v truncated=%v issues=%v", projection.Partial, projection.Truncated, projection.Issues)
	}
	if projection.Source.ServerVersion != "v1.36.1" {
		t.Fatalf("ServerVersion = %q, want v1.36.1", projection.Source.ServerVersion)
	}

	deployment := resourceNamed(t, projection.Nodes, "Deployment", "api")
	pod := resourceNamed(t, projection.Nodes, "Pod", "api-pod")
	service := resourceNamed(t, projection.Nodes, "Service", "api")
	ingress := resourceNamed(t, projection.Nodes, "Ingress", "api")
	node := resourceNamed(t, projection.Nodes, "Node", "node-a")
	namespace := resourceNamed(t, projection.Nodes, "Namespace", "ops")
	assertEdge(t, projection.Edges, deployment.ID, pod.ID, "owns")
	assertEdge(t, projection.Edges, deployment.ID, pod.ID, "selects")
	assertEdge(t, projection.Edges, service.ID, pod.ID, "selects")
	assertEdge(t, projection.Edges, service.ID, pod.ID, "routes_to")
	assertEdge(t, projection.Edges, ingress.ID, service.ID, "backend_ref")
	assertEdge(t, projection.Edges, pod.ID, node.ID, "scheduled_on")
	assertEdge(t, projection.Edges, namespace.ID, deployment.ID, "contains")
	if len(service.Endpoints) != 1 || service.Endpoints[0].TargetID != pod.ID {
		t.Fatalf("Service endpoints = %#v, want typed Pod target", service.Endpoints)
	}
	if len(pod.OwnerReferences) != 1 || pod.OwnerReferences[0].ID != deployment.ID {
		t.Fatalf("Pod owner references = %#v, want converged Deployment owner", pod.OwnerReferences)
	}
	if len(deployment.Containers) != 1 || deployment.Containers[0].Name != "api" ||
		len(deployment.Containers[0].EnvNames) != 2 || deployment.Containers[0].EnvNames[0] != "PUBLIC_MODE" ||
		deployment.Containers[0].EnvNames[1] != "REQUIRED_ENV" {
		t.Fatalf("Deployment container env-name projection = %#v", deployment.Containers)
	}
	if deployment.ContainersTruncated || deployment.Containers[0].EnvNamesTruncated ||
		deployment.Containers[0].HasEnvFrom || deployment.Containers[0].HasValueFrom ||
		deployment.Containers[0].HasSecretReference {
		t.Fatalf("Deployment container projection reported unexpected uncertainty: %#v", deployment.Containers[0])
	}
	encoded, err := json.Marshal(deployment.Containers)
	if err != nil || strings.Contains(string(encoded), "sensitive-value-not-projected") {
		t.Fatalf("Deployment container projection leaked an env value: %s (err=%v)", encoded, err)
	}

	events, truncated, err := reader.Events(context.Background(), "cluster-a", pod, 10)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if truncated || len(events) != 1 {
		t.Fatalf("Events() length=%d truncated=%v", len(events), truncated)
	}
	if strings.Contains(events[0].Message, "supersecret") || !strings.Contains(events[0].Message, "[REDACTED]") {
		t.Fatalf("Event message was not sanitized: %q", events[0].Message)
	}
}

func TestReaderRejectsNamespaceOutsideAllowlist(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	client.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.36.1"}
	reader, err := New(client, Config{ClusterID: "cluster-a", AllowedNamespaces: []string{"ops"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = reader.Read(context.Background(), infrastructure.ReadRequest{ClusterID: "cluster-a", Namespaces: []string{"default"}})
	if err == nil || !strings.Contains(err.Error(), "outside the Kubernetes reader allowlist") {
		t.Fatalf("Read() error = %v, want allowlist rejection", err)
	}
}

func TestReaderReturnsEmptyEventsForClusterScopedResource(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	reader, err := New(client, Config{ClusterID: "cluster-a", AllowedNamespaces: []string{"ops"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	events, truncated, err := reader.Events(context.Background(), "cluster-a", infrastructure.Resource{
		ID: "resource-namespace", SourceUID: "namespace-uid", APIVersion: "v1",
		Kind: "Namespace", Layer: infrastructure.LayerNamespace, Name: "ops",
	}, 10)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if events == nil || len(events) != 0 || truncated {
		t.Fatalf("Events() values=%#v truncated=%v, want non-nil scoped empty result", events, truncated)
	}
}

func resourceNamed(t *testing.T, values []infrastructure.Resource, kind, name string) infrastructure.Resource {
	t.Helper()
	for _, value := range values {
		if value.Kind == kind && value.Name == name {
			return value
		}
	}
	t.Fatalf("resource %s/%s not found in %#v", kind, name, values)
	return infrastructure.Resource{}
}

func assertEdge(t *testing.T, values []infrastructure.TopologyEdge, sourceID, targetID, relation string) {
	t.Helper()
	for _, value := range values {
		if value.SourceID == sourceID && value.TargetID == targetID && value.Relation == relation {
			return
		}
	}
	t.Fatalf("edge %s --%s--> %s not found in %#v", sourceID, relation, targetID, values)
}
