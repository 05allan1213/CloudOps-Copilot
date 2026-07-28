package investigationread

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/agent/runbook"
	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/observabilityread"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestInspectWorkloadResolvesOnlyServerOwnedScope(t *testing.T) {
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "demo", Labels: map[string]string{"app": "demo"}}, Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "demo"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}},
		}},
		&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "demo-rs", Namespace: "demo", Labels: map[string]string{"app": "demo"}}, Spec: appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "demo-rs-pod", Namespace: "demo", Labels: map[string]string{"app": "demo"}}, Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "demo"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "demo"}, Ports: []corev1.ServicePort{{Port: 8080, TargetPort: intstr.FromInt(8080)}}}},
		&discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: "demo-a", Namespace: "demo", Labels: map[string]string{discoveryv1.LabelServiceName: "demo"}}},
	)
	tools, err := New(Config{
		DB: new(sql.DB), Kubernetes: client, Prometheus: promStub{}, Elasticsearch: elasticStub{}, Tempo: tempoStub{},
		GitHub: githubStub{}, Argo: argoStub{}, Runtime: runtimeStub{}, Registry: registryStub{}, Runbooks: runbookStub{},
		Target: Target{Service: "demo", Cluster: "kind", Environment: "local", Namespace: "demo", Workload: "demo", Container: "app",
			Repository: change.RepositoryRef{Owner: "acme", Name: "gitops"}, BaseBranch: "main", GitOpsPath: "demo/deployment.yaml",
			ArgoPath: "demo", ArgoApplication: "demo", ArgoProject: "demo", EnvKey: "REQUIRED_ENV"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := agent.InvestigationToolRequest{
		Action:           agent.ProposedAction{Tool: ToolInspectWorkload, ScopeRef: "opaque", TemplateID: TemplateWorkloadV1, BoundedParameters: json.RawMessage(`{}`), ExpectedFactTypes: GoldenActionPolicies()[ToolInspectWorkload].ExpectedFactTypes},
		IncidentPublicID: "incident", CycleNo: 1,
		Correlation: agent.CorrelationSnapshot{Cluster: "kind", Environment: "local", Namespace: "demo", Workload: "demo", TargetKind: "Deployment"},
	}
	observation, err := tools.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Status != agent.CollectionAvailable || len(observation.Facts) != 2 || observation.Facts[0].Type != "workload.subject_confirmed" || observation.Facts[1].Type != "kubernetes.required_env_absent" {
		t.Fatalf("observation=%+v", observation)
	}
	request.Correlation.Namespace = "foreign"
	if _, err := tools.Execute(context.Background(), request); err == nil {
		t.Fatal("foreign Incident scope was accepted")
	}
}

func TestGoldenActionPoliciesFreezeEightToolsWithoutQueryLanguages(t *testing.T) {
	policies := GoldenActionPolicies()
	if len(policies) != 8 {
		t.Fatalf("policies=%d", len(policies))
	}
	for name, policy := range policies {
		if policy.AllowCompositeProvenance != (name == ToolGetDeploymentContext) {
			t.Fatalf("tool %s composite provenance policy=%v", name, policy.AllowCompositeProvenance)
		}
		for _, key := range policy.ParameterKeys {
			switch key {
			case "promql", "dsl", "traceql", "url", "namespace", "repository", "sha":
				t.Fatalf("tool %s exposes forbidden key %s", name, key)
			}
			spec, ok := policy.ParameterSpecs[key]
			if !ok {
				t.Fatalf("tool %s parameter %s has no scalar value contract", name, key)
			}
			if key == "window" && (spec.Type != agent.ParameterString || len(spec.Enum) != 4) {
				t.Fatalf("tool %s window contract=%+v", name, spec)
			}
		}
	}
}

func TestDeploymentApplicationMatchesArgoDirectoryNotGitOpsFile(t *testing.T) {
	target := Target{
		Repository: change.RepositoryRef{Owner: "acme", Name: "gitops"}, GitOpsPath: "apps/demo/deployment.yaml",
		ArgoPath: "apps/demo", ArgoApplication: "cloudops-demo", ArgoProject: "cloudops-demo",
	}
	application := change.ArgoApplication{
		Name: "cloudops-demo", Project: "cloudops-demo", Repository: "https://github.com/acme/gitops.git", Path: "apps/demo",
	}
	if !deploymentApplicationMatches(application, target) {
		t.Fatal("valid Argo directory identity was rejected")
	}
	application.Path = target.GitOpsPath
	if deploymentApplicationMatches(application, target) {
		t.Fatal("GitOps manifest file was accepted as the Argo Application path")
	}
}

type promStub struct{}

func (promStub) ObserveBoundedMetric(context.Context, observabilityread.MetricQuery) (verification.Observation, error) {
	return verification.Observation{}, nil
}

type elasticStub struct{}

func (elasticStub) Search(context.Context, observabilityread.ElasticQuery) (verification.Observation, error) {
	return verification.Observation{}, nil
}

type tempoStub struct{}

func (tempoStub) ObserveTraceErrorRate(context.Context, verification.SignalQuery) (verification.SignalResult, error) {
	return verification.SignalResult{}, nil
}

type githubStub struct{ change.GitHubReader }

func (githubStub) GetFileContent(context.Context, change.RepositoryRef, string, string) (change.FileContent, error) {
	return change.FileContent{}, nil
}

type argoStub struct{ change.ArgoCDReader }
type runtimeStub struct{ change.RuntimeReader }
type registryStub struct{ change.RegistryMetadataReader }
type runbookStub struct{}

func (runbookStub) Search(context.Context, runbook.SearchRequest) ([]runbook.SearchResult, error) {
	return nil, nil
}

var _ = time.Second
