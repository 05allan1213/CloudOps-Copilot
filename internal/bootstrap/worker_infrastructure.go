package bootstrap

import (
	"errors"
	"net/http"

	appconfig "github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/infrastructuregateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8sread"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/kubernetestopology"
)

func infrastructureGatewayHandler(cfg appconfig.Config) (http.Handler, error) {
	if !cfg.K8SEnabled {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"KUBERNETES_PROVIDER_DISABLED","detail":"Kubernetes Provider Gateway is disabled"}`))
		}), nil
	}
	connections, err := cfg.KubernetesTopologyConnections()
	if err != nil {
		return nil, err
	}
	readers := make([]*kubernetestopology.Reader, 0, len(connections))
	for _, connection := range connections {
		client, clientErr := k8sread.NewClient(k8sread.Config{
			Enabled: true, InCluster: connection.InCluster, Kubeconfig: connection.Kubeconfig,
			Context: connection.Context, AllowedNamespaces: connection.AllowedNamespaces,
			DefaultNamespace: connection.DefaultNamespace, RequestTimeout: connection.RequestTimeout,
		})
		if clientErr != nil {
			return nil, clientErr
		}
		if client == nil {
			return nil, errors.New("kubernetes Provider client was not constructed")
		}
		reader, readerErr := kubernetestopology.New(client, kubernetestopology.Config{
			ClusterID: connection.ClusterID, AllowedNamespaces: connection.AllowedNamespaces,
			RequestTimeout: connection.RequestTimeout,
		})
		if readerErr != nil {
			return nil, readerErr
		}
		readers = append(readers, reader)
	}
	registry, err := kubernetestopology.NewRegistry(readers...)
	if err != nil {
		return nil, err
	}
	return infrastructuregateway.NewServer(registry)
}
