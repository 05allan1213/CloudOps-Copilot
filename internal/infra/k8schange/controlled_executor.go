package k8schange

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	k8sreader "github.com/05allan1213/CloudOps-Copilot/internal/infra/k8sread"
)

// ControlledScaleExecutor is available only while the bounded local Scenario
// write gate is active. Formal remediation continues to use GitOps delivery.
type ControlledScaleExecutor struct {
	client            kubernetes.Interface
	allowedNamespaces map[string]struct{}
	targetNamespace   string
	targetDeployment  string
	scenarioID        string
	maxReplicas       int
	requestTimeout    time.Duration
}

type ControlledScaleConfig struct {
	AllowedNamespaces []string
	TargetNamespace   string
	TargetDeployment  string
	ScenarioID        string
	MaxReplicas       int
	RequestTimeout    time.Duration
}

type DeploymentScaleObservation struct {
	Namespace          string    `json:"namespace"`
	Name               string    `json:"name"`
	ResourceVersion    string    `json:"resource_version"`
	Generation         int64     `json:"generation"`
	ObservedGeneration int64     `json:"observed_generation"`
	DesiredReplicas    int32     `json:"desired_replicas"`
	UpdatedReplicas    int32     `json:"updated_replicas"`
	ReadyReplicas      int32     `json:"ready_replicas"`
	AvailableReplicas  int32     `json:"available_replicas"`
	ObservedAt         time.Time `json:"observed_at"`
}

func NewControlledScaleExecutor(client kubernetes.Interface, cfg ControlledScaleConfig) (*ControlledScaleExecutor, error) {
	if client == nil {
		return nil, errors.New("controlled scale executor requires a Kubernetes client")
	}
	if cfg.MaxReplicas < 1 {
		return nil, errors.New("controlled scale executor requires a positive replica limit")
	}
	if cfg.RequestTimeout <= 0 {
		return nil, errors.New("controlled scale executor requires a request timeout")
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedNamespaces))
	for _, namespace := range cfg.AllowedNamespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace != "" {
			allowed[namespace] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil, errors.New("controlled scale executor requires an explicit namespace allowlist")
	}
	targetNamespace := strings.TrimSpace(cfg.TargetNamespace)
	targetDeployment := strings.TrimSpace(cfg.TargetDeployment)
	scenarioID := strings.TrimSpace(cfg.ScenarioID)
	if _, ok := allowed[targetNamespace]; !ok || targetDeployment == "" || scenarioID == "" {
		return nil, errors.New("controlled scale executor requires one exact allowed Scenario target")
	}
	if err := k8sreader.ValidateName("target namespace", targetNamespace); err != nil {
		return nil, err
	}
	if err := k8sreader.ValidateName("target deployment", targetDeployment); err != nil {
		return nil, err
	}
	return &ControlledScaleExecutor{
		client: client, allowedNamespaces: allowed,
		targetNamespace: targetNamespace, targetDeployment: targetDeployment, scenarioID: scenarioID,
		maxReplicas: cfg.MaxReplicas, requestTimeout: cfg.RequestTimeout,
	}, nil
}

func (e *ControlledScaleExecutor) AllowsScenario(scenarioID string) bool {
	return e != nil && e.scenarioID != "" && scenarioID == e.scenarioID
}

func (e *ControlledScaleExecutor) validateDeployment(namespace, name string) error {
	if _, ok := e.allowedNamespaces[namespace]; !ok {
		return fmt.Errorf("namespace %q is outside the controlled operation allowlist", namespace)
	}
	if namespace != e.targetNamespace || name != e.targetDeployment {
		return errors.New("deployment is outside the exact bounded Scenario target")
	}
	return nil
}

func (e *ControlledScaleExecutor) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	_, err := e.ScaleDeploymentExact(ctx, namespace, name, replicas, "", -1)
	return err
}

func (e *ControlledScaleExecutor) ObserveDeployment(ctx context.Context, namespace, name string) (DeploymentScaleObservation, error) {
	if e == nil || e.client == nil {
		return DeploymentScaleObservation{}, errors.New("controlled scale executor is unavailable")
	}
	if err := e.validateDeployment(namespace, name); err != nil {
		return DeploymentScaleObservation{}, err
	}
	if err := k8sreader.ValidateName("namespace", namespace); err != nil {
		return DeploymentScaleObservation{}, err
	}
	if err := k8sreader.ValidateName("name", name); err != nil {
		return DeploymentScaleObservation{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, e.requestTimeout)
	defer cancel()
	deployment, err := e.client.AppsV1().Deployments(namespace).Get(requestCtx, name, metav1.GetOptions{})
	if err != nil {
		return DeploymentScaleObservation{}, controlledK8sError(err)
	}
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	return DeploymentScaleObservation{
		Namespace: namespace, Name: name, ResourceVersion: deployment.ResourceVersion,
		Generation: deployment.Generation, ObservedGeneration: deployment.Status.ObservedGeneration,
		DesiredReplicas: desired, UpdatedReplicas: deployment.Status.UpdatedReplicas,
		ReadyReplicas: deployment.Status.ReadyReplicas, AvailableReplicas: deployment.Status.AvailableReplicas,
		ObservedAt: time.Now().UTC(),
	}, nil
}

func (e *ControlledScaleExecutor) ScaleDeploymentExact(
	ctx context.Context,
	namespace, name string,
	replicas int32,
	expectedResourceVersion string,
	expectedReplicas int32,
) (DeploymentScaleObservation, error) {
	if e == nil || e.client == nil {
		return DeploymentScaleObservation{}, errors.New("controlled scale executor is unavailable")
	}
	if err := e.validateDeployment(namespace, name); err != nil {
		return DeploymentScaleObservation{}, err
	}
	if err := k8sreader.ValidateName("namespace", namespace); err != nil {
		return DeploymentScaleObservation{}, err
	}
	if err := k8sreader.ValidateName("name", name); err != nil {
		return DeploymentScaleObservation{}, err
	}
	if replicas < 0 || replicas > int32(e.maxReplicas) {
		return DeploymentScaleObservation{}, fmt.Errorf("replicas must be in range 0-%d", e.maxReplicas)
	}
	requestCtx, cancel := context.WithTimeout(ctx, e.requestTimeout)
	defer cancel()
	scale, err := e.client.AppsV1().Deployments(namespace).GetScale(requestCtx, name, metav1.GetOptions{})
	if err != nil {
		return DeploymentScaleObservation{}, controlledK8sError(err)
	}
	if expectedResourceVersion != "" && scale.ResourceVersion != expectedResourceVersion {
		return DeploymentScaleObservation{}, errors.New("controlled Kubernetes resource version precondition failed")
	}
	if expectedReplicas >= 0 && scale.Spec.Replicas != expectedReplicas {
		return DeploymentScaleObservation{}, errors.New("controlled Kubernetes replica precondition failed")
	}
	scale.Spec.Replicas = replicas
	if _, err := e.client.AppsV1().Deployments(namespace).UpdateScale(requestCtx, name, scale, metav1.UpdateOptions{}); err != nil {
		return DeploymentScaleObservation{}, controlledK8sError(err)
	}
	return e.ObserveDeployment(ctx, namespace, name)
}

func controlledK8sError(err error) error {
	switch {
	case apierrors.IsNotFound(err):
		return errors.New("controlled demo deployment not found")
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return errors.New("controlled demo Kubernetes permission denied")
	case apierrors.IsConflict(err):
		return errors.New("controlled demo Kubernetes resource conflict")
	default:
		return err
	}
}
