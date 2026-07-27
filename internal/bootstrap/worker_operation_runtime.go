package bootstrap

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8schange"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8sread"
	"github.com/05allan1213/CloudOps-Copilot/internal/operation"
)

func newWorkerOperationRunner(cfg WorkerConfig, db *sql.DB) (*operation.Runner, error) {
	repository, err := operation.NewRepository(db)
	if err != nil {
		return nil, err
	}
	localAdapter, err := operation.NewLocalChangeFreezeAdapter(repository)
	if err != nil {
		return nil, err
	}
	adapters := []operation.Adapter{localAdapter}
	if cfg.Application.K8SWriteEnabled {
		connections, connectionErr := cfg.Application.KubernetesTopologyConnections()
		if connectionErr != nil {
			return nil, connectionErr
		}
		executors := make(map[string]*k8schange.ControlledScaleExecutor, len(connections))
		for _, connection := range connections {
			if slices.Contains(connection.AllowedNamespaces, "*") {
				return nil, errors.New("operation Kubernetes writer requires explicit namespace allowlists")
			}
			client, clientErr := k8sread.NewClient(k8sread.Config{
				Enabled: true, InCluster: connection.InCluster, Kubeconfig: connection.Kubeconfig,
				Context: connection.Context, AllowedNamespaces: connection.AllowedNamespaces,
				DefaultNamespace: connection.DefaultNamespace, RequestTimeout: connection.RequestTimeout,
			})
			if clientErr != nil {
				return nil, fmt.Errorf("initialize operation Kubernetes client %q: %w", connection.ClusterID, clientErr)
			}
			executor, executorErr := k8schange.NewControlledScaleExecutor(client, k8schange.ControlledScaleConfig{
				AllowedNamespaces: connection.AllowedNamespaces,
				MaxReplicas:       cfg.Application.OperationMaxReplicas,
				RequestTimeout:    connection.RequestTimeout,
			})
			if executorErr != nil {
				return nil, fmt.Errorf("initialize operation Kubernetes executor %q: %w", connection.ClusterID, executorErr)
			}
			executors[connection.ClusterID] = executor
		}
		kubernetesAdapter, adapterErr := k8schange.NewOperationScaleAdapter(executors, repository, 500*time.Millisecond)
		if adapterErr != nil {
			return nil, adapterErr
		}
		adapters = append(adapters, kubernetesAdapter)
	}
	registry, err := operation.NewAdapterRegistry(adapters...)
	if err != nil {
		return nil, err
	}
	return operation.NewRunner(operation.RunnerConfig{
		Owner: cfg.Async.WorkerID, Repository: repository, Adapters: registry,
		PollInterval: 250 * time.Millisecond, LeaseDuration: cfg.Async.DeliverLease,
		HeartbeatInterval: cfg.Async.DeliverHeartbeat, ExecutionTimeout: cfg.Async.DeliverHandlerDeadline,
	})
}
