package startup

import (
	"k8s.io/client-go/kubernetes"

	"server-web/internal/config"
	"server-web/internal/infra/k8sread"
)

// InitK8sRuntime creates the shared read adapter used by the Incident Agent,
// incident-scoped Workbench context, Verification, and guarded Fast Demo. The
// removed generic Kubernetes Dashboard has no HTTP handler or route wiring.
func InitK8sRuntime(cfg config.Config) (k8sread.Reader, kubernetes.Interface, error) {
	if !cfg.K8SEnabled {
		return nil, nil, nil
	}
	k8sCfg := k8sConfigFromApp(cfg)
	client, err := k8sread.NewClient(k8sCfg)
	if err != nil {
		return nil, nil, err
	}
	return k8sread.NewServiceWithClient(client, k8sCfg), client, nil
}

func k8sConfigFromApp(cfg config.Config) k8sread.Config {
	return k8sread.Config{
		Enabled:           cfg.K8SEnabled,
		WriteEnabled:      cfg.K8SWriteEnabled,
		InCluster:         cfg.K8SInCluster,
		Kubeconfig:        cfg.K8SKubeconfig,
		AllowedNamespaces: cfg.K8SAllowedNamespaces,
		DefaultNamespace:  cfg.K8SDefaultNamespace,
		RequestTimeout:    cfg.K8SRequestTimeout,
		LogTailLines:      cfg.K8SLogTailLines,
		LogMaxBytes:       cfg.K8SLogMaxBytes,
		EventLimit:        cfg.K8SEventLimit,
	}
}
