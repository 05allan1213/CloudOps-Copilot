package k8sread

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func NewClient(cfg Config) (kubernetes.Interface, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	restConfig, err := loadRESTConfig(cfg)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create k8s client: %w", err)
	}
	return client, nil
}

func loadRESTConfig(cfg Config) (*rest.Config, error) {
	if cfg.InCluster {
		restConfig, err := rest.InClusterConfig()
		if err == nil {
			return restConfig, nil
		}
		if stringsTrim(cfg.Kubeconfig) == "" {
			return nil, fmt.Errorf("load in-cluster k8s config: %w", err)
		}
	}
	kubeconfig := stringsTrim(cfg.Kubeconfig)
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve user home for kubeconfig: %w", err)
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: stringsTrim(cfg.Context)}
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load k8s kubeconfig: %w", err)
	}
	return restConfig, nil
}

func stringsTrim(value string) string {
	return strings.TrimSpace(value)
}
