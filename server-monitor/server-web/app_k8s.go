package main

import (
	"context"
	"errors"

	"k8s.io/client-go/kubernetes"

	"server-web/internal/config"
	copilotk8s "server-web/internal/copilot/k8s"
	"server-web/internal/handler"
	k8ssvc "server-web/internal/service/k8s"
	promclient "server-web/internal/infra/prometheus"
)

type promClientAdapter struct {
	client *promclient.Client
}

func (a *promClientAdapter) GetHosts(ctx context.Context) ([]k8ssvc.HostInfo, error) {
	hosts, err := a.client.GetHosts(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]k8ssvc.HostInfo, len(hosts))
	for i, h := range hosts {
		result[i] = k8ssvc.HostInfo{
			Instance:   h.Instance,
			Status:     h.Status,
			LastScrape: h.LastScrape,
		}
	}
	return result, nil
}

func initK8sRuntime(cfg config.Config, infra infrastructure) (copilotk8s.Reader, kubernetes.Interface, *k8ssvc.Service, *handler.K8sHandler, error) {
	if !cfg.K8SEnabled {
		return nil, nil, nil, nil, nil
	}
	k8sCfg := k8sConfigFromApp(cfg)
	k8sClient, err := copilotk8s.NewClient(k8sCfg)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var k8sReader copilotk8s.Reader
	k8sReader = copilotk8s.NewServiceWithClient(k8sClient, k8sCfg)

	if !cfg.K8SAPIEnabled {
		return k8sReader, k8sClient, nil, nil, nil
	}
	promAdapter := &promClientAdapter{client: infra.prometheusClient}
	svc := k8ssvc.NewService(k8sReader, promAdapter, k8ssvc.Options{
		RequestTimeout: cfg.K8SRequestTimeout,
		NodesEnabled:   cfg.K8SNodesEnabled,
	})
	k8sHandler := handler.NewK8sHandler(svc, cfg.K8SNodesEnabled, cfg.K8SRequestTimeout)
	return k8sReader, k8sClient, svc, k8sHandler, nil
}

func k8sConfigFromApp(cfg config.Config) copilotk8s.Config {
	return copilotk8s.Config{
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

func errK8sClientRequired() error {
	return errors.New("k8s client is required when k8s write execution is enabled")
}
