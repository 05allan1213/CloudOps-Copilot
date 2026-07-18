package startup

import (
	"fmt"

	"k8s.io/client-go/kubernetes"

	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentmysql"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/k8schange"
	"github.com/05allan1213/CloudOps-Copilot/internal/service/fastdemo"
)

func InitFastDemo(cfg config.Config, container *di.Container, client kubernetes.Interface) (*fastdemo.Service, error) {
	if !cfg.FastDemoEnabled {
		return nil, nil
	}
	if container.DB == nil || client == nil {
		return nil, fmt.Errorf("fast demo requires MySQL and Kubernetes")
	}
	incidents, err := incidentmysql.NewStore(container.DB)
	if err != nil {
		return nil, err
	}
	remediations, err := incidentmysql.NewRemediationRepository(container.DB)
	if err != nil {
		return nil, err
	}
	verifications, err := incidentmysql.NewVerificationRepository(container.DB)
	if err != nil {
		return nil, err
	}
	rollout, err := k8schange.New(client, cfg.K8SAllowedNamespaces, cfg.K8SRequestTimeout)
	if err != nil {
		return nil, err
	}
	executor, err := k8schange.NewControlledScaleExecutor(client, k8schange.ControlledScaleConfig{AllowedNamespaces: cfg.K8SAllowedNamespaces, MaxReplicas: cfg.FastDemoMaxReplicas, RequestTimeout: cfg.K8SRequestTimeout})
	if err != nil {
		return nil, err
	}
	service, err := fastdemo.New(fastdemo.Config{Revision: cfg.FastDemoRevision, Cluster: cfg.FastDemoCluster, Environment: "local-demo", Namespace: cfg.FastDemoNamespace, Workload: cfg.FastDemoWorkload, RecoveryReplicas: cfg.FastDemoRecoveryReplicas, Executor: executor, Rollout: rollout, Incidents: incidents, Remediations: remediations, Verifications: verifications})
	if err != nil {
		return nil, err
	}
	container.Handler.SetRemediation(service)
	container.Handler.SetDeliveryVerification(service)
	container.Handler.SetFastDemo(service, "demo-operator")
	return service, nil
}
