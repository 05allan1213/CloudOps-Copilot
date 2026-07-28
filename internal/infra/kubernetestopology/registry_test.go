package kubernetestopology

import (
	"context"
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRegistryDispatchesByExplicitClusterIdentity(t *testing.T) {
	t.Parallel()
	readerA := registryReader(t, "cluster-a", "v1.35.0")
	readerB := registryReader(t, "cluster-b", "v1.36.1")
	registry, err := NewRegistry(readerA, readerB)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	source, err := registry.Probe(context.Background(), "cluster-b")
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if source.ClusterID != "cluster-b" || source.ServerVersion != "v1.36.1" {
		t.Fatalf("Probe() source = %#v", source)
	}

	projection, err := registry.Read(context.Background(), infrastructure.ReadRequest{
		ClusterID: "cluster-a", Namespaces: []string{"ops"}, Limit: 10,
	})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if projection.Source.ClusterID != "cluster-a" || projection.Source.ServerVersion != "v1.35.0" {
		t.Fatalf("Read() source = %#v", projection.Source)
	}
}

func TestRegistryRejectsUnknownAndDuplicateClusters(t *testing.T) {
	t.Parallel()
	reader := registryReader(t, "cluster-a", "v1.36.1")
	if _, err := NewRegistry(reader, reader); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("NewRegistry() duplicate error = %v", err)
	}
	registry, err := NewRegistry(reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Probe(context.Background(), "cluster-missing"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("Probe() unknown cluster error = %v", err)
	}
}

func registryReader(t *testing.T, clusterID, serverVersion string) *Reader {
	t.Helper()
	client := fake.NewSimpleClientset()
	client.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: serverVersion}
	reader, err := New(client, Config{ClusterID: clusterID, AllowedNamespaces: []string{"ops"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return reader
}
