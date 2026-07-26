package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maximumKubernetesConnections = 10
	maximumConnectionsJSONBytes  = 64 * 1024
)

var kubernetesClusterIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type kubernetesConnectionJSON struct {
	ClusterID             string   `json:"cluster_id"`
	Kubeconfig            string   `json:"kubeconfig,omitempty"`
	Context               string   `json:"context,omitempty"`
	InCluster             bool     `json:"in_cluster"`
	AllowedNamespaces     []string `json:"allowed_namespaces"`
	DefaultNamespace      string   `json:"default_namespace"`
	RequestTimeoutSeconds int      `json:"request_timeout_seconds,omitempty"`
}

// KubernetesTopologyConnections returns the immutable bootstrap registry used
// by the Worker topology gateway. Empty JSON retains the original single-client
// environment contract.
func (c Config) KubernetesTopologyConnections() ([]K8SClusterConfig, error) {
	raw := strings.TrimSpace(c.K8SConnectionsJSON)
	if raw == "" {
		connections := []K8SClusterConfig{{
			ClusterID: strings.TrimSpace(c.K8SClusterID), Kubeconfig: strings.TrimSpace(c.K8SKubeconfig),
			InCluster: c.K8SInCluster, AllowedNamespaces: append([]string(nil), c.K8SAllowedNamespaces...),
			DefaultNamespace: strings.TrimSpace(c.K8SDefaultNamespace), RequestTimeout: c.K8SRequestTimeout,
		}}
		return normalizeKubernetesConnections(connections, false)
	}
	if len(raw) > maximumConnectionsJSONBytes {
		return nil, errors.New("K8S_CONNECTIONS_JSON exceeds 65536 bytes")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var encoded []kubernetesConnectionJSON
	if err := decoder.Decode(&encoded); err != nil {
		return nil, fmt.Errorf("K8S_CONNECTIONS_JSON must be a typed JSON array: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("K8S_CONNECTIONS_JSON must contain one JSON value")
	}
	connections := make([]K8SClusterConfig, 0, len(encoded))
	for _, value := range encoded {
		timeout := time.Duration(value.RequestTimeoutSeconds) * time.Second
		if value.RequestTimeoutSeconds == 0 {
			timeout = c.K8SRequestTimeout
		}
		connections = append(connections, K8SClusterConfig{
			ClusterID: strings.TrimSpace(value.ClusterID), Kubeconfig: strings.TrimSpace(value.Kubeconfig),
			Context: strings.TrimSpace(value.Context), InCluster: value.InCluster,
			AllowedNamespaces: append([]string(nil), value.AllowedNamespaces...),
			DefaultNamespace:  strings.TrimSpace(value.DefaultNamespace), RequestTimeout: timeout,
		})
	}
	return normalizeKubernetesConnections(connections, true)
}

func normalizeKubernetesConnections(values []K8SClusterConfig, explicit bool) ([]K8SClusterConfig, error) {
	if len(values) < 1 || len(values) > maximumKubernetesConnections {
		return nil, errors.New("K8S_CONNECTIONS_JSON must register 1 to 10 clusters")
	}
	seenClusters := make(map[string]struct{}, len(values))
	inClusterConnections := 0
	for index := range values {
		value := &values[index]
		value.ClusterID = strings.TrimSpace(value.ClusterID)
		value.Kubeconfig = strings.TrimSpace(value.Kubeconfig)
		value.Context = strings.TrimSpace(value.Context)
		value.DefaultNamespace = strings.TrimSpace(value.DefaultNamespace)
		if !kubernetesClusterIdentityPattern.MatchString(value.ClusterID) {
			return nil, fmt.Errorf("K8S_CONNECTIONS_JSON cluster %d has an invalid cluster_id", index)
		}
		if _, exists := seenClusters[value.ClusterID]; exists {
			return nil, fmt.Errorf("K8S_CONNECTIONS_JSON contains duplicate cluster_id %q", value.ClusterID)
		}
		seenClusters[value.ClusterID] = struct{}{}
		if value.InCluster {
			inClusterConnections++
		}
		if explicit && !value.InCluster && value.Kubeconfig == "" {
			return nil, fmt.Errorf("K8S_CONNECTIONS_JSON cluster %q requires an absolute kubeconfig file", value.ClusterID)
		}
		if value.Kubeconfig != "" {
			cleaned := filepath.Clean(value.Kubeconfig)
			if !filepath.IsAbs(cleaned) || cleaned == string(filepath.Separator) {
				return nil, fmt.Errorf("K8S_CONNECTIONS_JSON cluster %q kubeconfig must be an absolute file path", value.ClusterID)
			}
			value.Kubeconfig = cleaned
		}
		if value.Context != "" && (value.Kubeconfig == "" || len(value.Context) > 253 || strings.ContainsAny(value.Context, "\r\n\x00")) {
			return nil, fmt.Errorf("K8S_CONNECTIONS_JSON cluster %q has an invalid kubeconfig context", value.ClusterID)
		}
		if len(value.AllowedNamespaces) < 1 || len(value.AllowedNamespaces) > 100 {
			return nil, fmt.Errorf("K8S_CONNECTIONS_JSON cluster %q requires 1 to 100 allowed namespaces", value.ClusterID)
		}
		seenNamespaces := make(map[string]struct{}, len(value.AllowedNamespaces))
		namespaces := make([]string, 0, len(value.AllowedNamespaces))
		for _, namespace := range value.AllowedNamespaces {
			namespace = strings.TrimSpace(namespace)
			if err := checkK8SNamespace("K8S_CONNECTIONS_JSON allowed_namespaces", namespace); err != nil {
				return nil, err
			}
			if _, exists := seenNamespaces[namespace]; exists {
				continue
			}
			seenNamespaces[namespace] = struct{}{}
			namespaces = append(namespaces, namespace)
		}
		sort.Strings(namespaces)
		value.AllowedNamespaces = namespaces
		if err := checkK8SNamespace("K8S_CONNECTIONS_JSON default_namespace", value.DefaultNamespace); err != nil {
			return nil, err
		}
		if _, exists := seenNamespaces[value.DefaultNamespace]; !exists {
			if _, allowsAll := seenNamespaces["*"]; !allowsAll {
				return nil, fmt.Errorf("K8S_CONNECTIONS_JSON cluster %q default_namespace must be allowed", value.ClusterID)
			}
		}
		if value.RequestTimeout < time.Second || value.RequestTimeout > 60*time.Second {
			return nil, fmt.Errorf("K8S_CONNECTIONS_JSON cluster %q request timeout must be 1 to 60 seconds", value.ClusterID)
		}
	}
	if inClusterConnections > 1 {
		return nil, errors.New("K8S_CONNECTIONS_JSON may register only one in_cluster connection")
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ClusterID < values[j].ClusterID })
	return values, nil
}
