package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"

	"server-web/internal/config"
	copilotk8s "server-web/internal/copilot/k8s"
	"server-web/internal/di"
	"server-web/internal/handler"
	promclient "server-web/internal/infra/prometheus"
	ws "server-web/internal/infra/websocket"
	k8ssvc "server-web/internal/service/k8s"
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

func initK8sRuntime(cfg config.Config, container *di.Container) (copilotk8s.Reader, kubernetes.Interface, *k8ssvc.Service, *handler.K8sHandler, error) {
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
	promAdapter := &promClientAdapter{client: container.PromClient}
	svc := k8ssvc.NewService(k8sReader, promAdapter, k8ssvc.Options{
		RequestTimeout: cfg.K8SRequestTimeout,
		NodesEnabled:   cfg.K8SNodesEnabled,
		CacheService:   container.Cache(),
		CacheTTL:       cfg.K8sCacheTTL,
		ListCacheTTL:   cfg.K8sListCacheTTL,
		ClusterName:    "default",
	})

	var extraClusters map[string]*k8ssvc.Service
	if len(cfg.K8SClusters) > 0 {
		extraClusters = make(map[string]*k8ssvc.Service, len(cfg.K8SClusters))
		for _, cc := range cfg.K8SClusters {
			ccCfg := copilotk8s.Config{
				Enabled:           true,
				InCluster:         cc.InCluster,
				Kubeconfig:        cc.Kubeconfig,
				AllowedNamespaces: cc.AllowedNamespaces,
				DefaultNamespace:  cc.DefaultNamespace,
				RequestTimeout:    cc.RequestTimeout,
				LogTailLines:      cfg.K8SLogTailLines,
				LogMaxBytes:       cfg.K8SLogMaxBytes,
				EventLimit:        cfg.K8SEventLimit,
			}
			ccClient, ccErr := copilotk8s.NewClient(ccCfg)
			if ccErr != nil {
				zap.L().Warn("k8s extra cluster client init failed", zap.String("cluster", cc.Name), zap.Error(ccErr))
				continue
			}
			ccReader := copilotk8s.NewServiceWithClient(ccClient, ccCfg)
			ccSvc := k8ssvc.NewService(ccReader, promAdapter, k8ssvc.Options{
				RequestTimeout: cc.RequestTimeout,
				NodesEnabled:   cfg.K8SNodesEnabled,
				CacheService:   container.Cache(),
				CacheTTL:       cfg.K8sCacheTTL,
				ListCacheTTL:   cfg.K8sListCacheTTL,
				ClusterName:    cc.Name,
			})
			extraClusters[cc.Name] = ccSvc
		}
	}

	k8sHandler := handler.NewK8sHandler(svc, cfg.K8SNodesEnabled, cfg.K8SRequestTimeout, extraClusters)
	return k8sReader, k8sClient, svc, k8sHandler, nil
}

func startK8sEventWatcher(ctx context.Context, k8sReader copilotk8s.Reader, hub *ws.Hub, interval time.Duration) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				zap.L().Error("k8s event watcher recovered from panic", zap.Any("error", r))
			}
		}()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var lastEventTime time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				events, err := k8sReader.ListEvents(ctx, copilotk8s.EventQuery{Limit: 10})
				if err != nil {
					zap.L().Warn("k8s event watcher list events failed", zap.Error(err))
					continue
				}
				for i := len(events) - 1; i >= 0; i-- {
					e := events[i]
					if !lastEventTime.IsZero() && e.LastSeen.After(lastEventTime) {
						msg, _ := json.Marshal(map[string]interface{}{
							"type": "k8s_event",
							"data": e,
						})
						hub.Broadcast(msg)
					}
				}
				if len(events) > 0 {
					lastEventTime = events[0].LastSeen
				}
			}
		}
	}()
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