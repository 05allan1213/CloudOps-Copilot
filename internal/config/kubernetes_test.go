package config

import (
	"strings"
	"testing"
	"time"
)

func TestKubernetesTopologyConnectionsPreservesSingleClientCompatibility(t *testing.T) {
	t.Parallel()
	cfg := Config{
		K8SClusterID: "cluster-a", K8SInCluster: true,
		K8SAllowedNamespaces: []string{"ops", "demo"}, K8SDefaultNamespace: "ops",
		K8SRequestTimeout: 7 * time.Second,
	}
	connections, err := cfg.KubernetesTopologyConnections()
	if err != nil {
		t.Fatalf("KubernetesTopologyConnections() error = %v", err)
	}
	if len(connections) != 1 || connections[0].ClusterID != "cluster-a" || !connections[0].InCluster || connections[0].RequestTimeout != 7*time.Second {
		t.Fatalf("connections = %#v", connections)
	}
}

func TestKubernetesTopologyConnectionsParsesBoundedRegistry(t *testing.T) {
	t.Parallel()
	cfg := Config{
		K8SRequestTimeout: 10 * time.Second,
		K8SConnectionsJSON: `[
			{"cluster_id":"cluster-b","kubeconfig":"/var/run/secrets/cloudops/b/config","context":"context-b","in_cluster":false,"allowed_namespaces":["ops"],"default_namespace":"ops","request_timeout_seconds":12},
			{"cluster_id":"cluster-a","in_cluster":true,"allowed_namespaces":["demo","ops","demo"],"default_namespace":"demo"}
		]`,
	}
	connections, err := cfg.KubernetesTopologyConnections()
	if err != nil {
		t.Fatalf("KubernetesTopologyConnections() error = %v", err)
	}
	if len(connections) != 2 || connections[0].ClusterID != "cluster-a" || connections[1].ClusterID != "cluster-b" {
		t.Fatalf("connections = %#v", connections)
	}
	if connections[0].RequestTimeout != 10*time.Second || len(connections[0].AllowedNamespaces) != 2 || connections[1].RequestTimeout != 12*time.Second || connections[1].Context != "context-b" {
		t.Fatalf("normalized connections = %#v", connections)
	}
}

func TestKubernetesTopologyConnectionsRejectsAmbiguousOrUnsafeRegistry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: `[]`, want: "1 to 10"},
		{name: "duplicate", raw: `[{"cluster_id":"same","in_cluster":true,"allowed_namespaces":["ops"],"default_namespace":"ops"},{"cluster_id":"same","kubeconfig":"/tmp/config","allowed_namespaces":["ops"],"default_namespace":"ops"}]`, want: "duplicate"},
		{name: "two in cluster", raw: `[{"cluster_id":"a","in_cluster":true,"allowed_namespaces":["ops"],"default_namespace":"ops"},{"cluster_id":"b","in_cluster":true,"allowed_namespaces":["ops"],"default_namespace":"ops"}]`, want: "only one in_cluster"},
		{name: "relative kubeconfig", raw: `[{"cluster_id":"a","kubeconfig":"relative/config","allowed_namespaces":["ops"],"default_namespace":"ops"}]`, want: "absolute"},
		{name: "default outside allowlist", raw: `[{"cluster_id":"a","kubeconfig":"/tmp/config","allowed_namespaces":["ops"],"default_namespace":"demo"}]`, want: "must be allowed"},
		{name: "unknown field", raw: `[{"cluster_id":"a","kubeconfig":"/tmp/config","allowed_namespaces":["ops"],"default_namespace":"ops","token":"secret"}]`, want: "unknown field"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := Config{K8SConnectionsJSON: test.raw, K8SRequestTimeout: 10 * time.Second}
			_, err := cfg.KubernetesTopologyConnections()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
