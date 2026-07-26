package settings

import (
	"context"
	"errors"
	"testing"
	"time"
)

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
