package settings

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestNewServicePreservesValidatedProviderTimeoutBudget(t *testing.T) {
	t.Parallel()

	service, err := NewService(&sql.DB{}, t.TempDir(), BootstrapDiagnostics{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := service.httpTimeout, 60*time.Second; got != want {
		t.Fatalf("provider probe timeout ceiling=%s want=%s", got, want)
	}
}

func TestProviderProbeURLNormalizesLLMChatEndpoint(t *testing.T) {
	t.Parallel()
	for name, endpoint := range map[string]string{
		"base":               "https://api.deepseek.com/v1",
		"chat endpoint":      "https://api.deepseek.com/v1/chat/completions",
		"root chat endpoint": "https://api.deepseek.com/chat/completions",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := providerProbeURL(ProviderConfiguration{Provider: ProviderLLM, Endpoint: endpoint})
			if err != nil {
				t.Fatal(err)
			}
			want := "https://api.deepseek.com/models"
			if name != "root chat endpoint" {
				want = "https://api.deepseek.com/v1/models"
			}
			if got != want {
				t.Fatalf("probe URL=%q want=%q", got, want)
			}
		})
	}
}

func TestProviderProbeURLUsesArgoCDVersionEndpoint(t *testing.T) {
	t.Parallel()

	got, err := providerProbeURL(ProviderConfiguration{
		Provider: ProviderArgoCD,
		Endpoint: "https://argocd.example.com/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://argocd.example.com/api/version"; got != want {
		t.Fatalf("probe URL=%q want=%q", got, want)
	}
}

func TestKubernetesValidationProbesEveryRegisteredScope(t *testing.T) {
	t.Parallel()
	service := &Service{
		now: func() time.Time { return time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC) },
		providerProbes: map[Provider]func(context.Context, string) (string, error){
			ProviderKubernetes: func(_ context.Context, clusterID string) (string, error) {
				if clusterID == "cluster-b" {
					return "", errors.New("not registered")
				}
				return "typed reader available", nil
			},
		},
	}
	result, fieldErrors := service.testKubernetesScopes(context.Background(), ProviderConfiguration{
		Provider: ProviderKubernetes, Enabled: true,
	}, nil, []OperationalScope{{ClusterID: "cluster-a"}, {ClusterID: "cluster-b"}})
	if result.State != "unavailable" || result.Detail != "Kubernetes typed readers 1/2 可用" {
		t.Fatalf("result = %#v", result)
	}
	if len(fieldErrors) != 1 || fieldErrors[0].Field != "scopes.1.cluster_id" || fieldErrors[0].Code != "KUBERNETES_READER_UNAVAILABLE" {
		t.Fatalf("field errors = %#v", fieldErrors)
	}
}
