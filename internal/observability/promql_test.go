package observability

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

func testRevision() settings.Revision {
	scope := settings.OperationalScope{
		ID: "scope-a", Name: "local", ClusterID: "cloudops-local", Environment: "local",
		Namespaces: []string{"cloudops-system"}, Active: true,
	}
	return settings.Revision{
		ID: "revision-a", Scope: scope, Scopes: []settings.OperationalScope{scope},
		General: settings.GeneralConfiguration{QueryMaxLookbackSeconds: 3600, QueryMaxResults: 1000},
		Providers: []settings.ProviderConfiguration{{
			Provider: settings.ProviderPrometheus, Enabled: true, Endpoint: "http://prometheus:9090",
			TimeoutMS: 5000, MaxResults: 100,
		}},
	}
}

func testStartRequest() StartQueryRequest {
	to := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	return StartQueryRequest{
		Mode: ModeExpert, Query: "rate(http_requests_total[5m])", ClusterID: "cloudops-local",
		Namespace: "cloudops-system", Resource: ResourceReference{
			ID:   "kubernetes://cloudops-local/apps/v1/namespaces/cloudops-system/deployments/cloudops-api",
			Kind: "Deployment", Namespace: "cloudops-system", Name: "cloudops-api",
		}, From: to.Add(-30 * time.Minute), To: to, StepSeconds: 30,
	}
}

func TestPrepareOwnerQueryInjectsDeterministicExactScope(t *testing.T) {
	prepared, err := PrepareOwnerQuery(testStartRequest(), testRevision())
	if err != nil {
		t.Fatal(err)
	}
	for _, matcher := range []string{
		`cluster_id="cloudops-local"`, `environment="local"`, `namespace="cloudops-system"`,
		`workload_kind="Deployment"`, `workload="cloudops-api"`,
	} {
		if !strings.Contains(prepared.Query, matcher) {
			t.Fatalf("normalized query %q is missing %s", prepared.Query, matcher)
		}
	}
	again, err := PrepareOwnerQuery(testStartRequest(), testRevision())
	if err != nil || prepared.Query != again.Query || prepared.QueryHash != again.QueryHash {
		t.Fatalf("normalization is not deterministic: first=%#v second=%#v error=%v", prepared, again, err)
	}
}

func TestPrepareOwnerQueryRejectsScopeEscapeAndExcessiveCost(t *testing.T) {
	tests := []struct {
		name string
		edit func(*StartQueryRequest)
		want error
	}{
		{name: "scope matcher", edit: func(request *StartQueryRequest) {
			request.Query = `up{namespace="other"}`
		}, want: ErrInvalid},
		{name: "offset", edit: func(request *StartQueryRequest) {
			request.Query = "up offset 5m"
		}, want: ErrInvalid},
		{name: "at modifier", edit: func(request *StartQueryRequest) {
			request.Query = "up @ 1"
		}, want: ErrInvalid},
		{name: "oversized points", edit: func(request *StartQueryRequest) {
			request.StepSeconds = 1
		}, want: ErrBoundExceeded},
		{name: "outside revision", edit: func(request *StartQueryRequest) {
			request.Namespace = "default"
			request.Resource.Namespace = "default"
		}, want: ErrInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := testStartRequest()
			test.edit(&request)
			_, err := PrepareOwnerQuery(request, testRevision())
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want %v", err, test.want)
			}
		})
	}
}

func TestGuidedAndExpertUseTheSameBounds(t *testing.T) {
	expert := testStartRequest()
	preparedExpert, err := PrepareOwnerQuery(expert, testRevision())
	if err != nil {
		t.Fatal(err)
	}
	guided := expert
	guided.Mode, guided.Query, guided.CatalogKey = ModeGuided, "", "http_request_rate"
	preparedGuided, err := PrepareOwnerQuery(guided, testRevision())
	if err != nil {
		t.Fatal(err)
	}
	if preparedExpert.Bounds != preparedGuided.Bounds || preparedExpert.Scope.ClusterID != preparedGuided.Scope.ClusterID {
		t.Fatalf("expert=%#v guided=%#v", preparedExpert, preparedGuided)
	}
}

func TestOwnerDefinitionUsesItsExactBoundsWithinTheActivePolicy(t *testing.T) {
	request := testStartRequest()
	request.From = request.To.Add(-15 * time.Minute)
	request.DefinitionID = "definition-a"
	prepared, err := PrepareOwnerQuery(request, testRevision())
	if err != nil {
		t.Fatal(err)
	}
	definition := Definition{
		ID: "definition-a", ConfigurationRevision: prepared.ConfigurationRevision,
		Mode: prepared.Mode, CatalogKey: prepared.CatalogKey, Query: prepared.Query, QueryHash: prepared.QueryHash,
		Scope: prepared.Scope, Resource: prepared.Resource, MaxLookbackSeconds: 15 * 60,
		MaxSeries: 25, MaxSamples: 100,
	}
	bound, err := bindOwnerDefinition(prepared, definition)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Actor != ActorOwner || bound.AuthorizationID != "" || bound.Bounds.MaxLookbackSeconds != 15*60 ||
		bound.Bounds.MaxSeries != 25 || bound.Bounds.MaxSamples != 100 {
		t.Fatalf("bound Owner definition=%#v", bound)
	}

	tooLarge := prepared
	tooLarge.TimeRange.From = tooLarge.TimeRange.To.Add(-16 * time.Minute)
	if _, err := bindOwnerDefinition(tooLarge, definition); !errors.Is(err, ErrBoundExceeded) {
		t.Fatalf("oversized definition error=%v", err)
	}

	mismatched := definition
	mismatched.QueryHash = strings.Repeat("0", 64)
	if _, err := bindOwnerDefinition(prepared, mismatched); !errors.Is(err, ErrInvalid) || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("definition mismatch error=%v", err)
	}
}

func TestAgentQueryReusesContractWithinExactAuthorization(t *testing.T) {
	owner, err := PrepareOwnerQuery(testStartRequest(), testRevision())
	if err != nil {
		t.Fatal(err)
	}
	authorization := Authorization{
		ID: "authorization-a", ConfigurationRevision: owner.ConfigurationRevision,
		Mode: AuthorizationRunOnce, Provider: "prometheus", QueryMode: owner.Mode,
		Query: owner.Query, QueryHash: owner.QueryHash, Scope: owner.Scope, Resource: owner.Resource,
		MaxLookbackSeconds: owner.Bounds.MaxLookbackSeconds, MaxSeries: owner.Bounds.MaxSeries,
		MaxSamples: owner.Bounds.MaxSamples,
	}
	prepared, err := PrepareAgentQuery(AgentQueryRequest{
		AuthorizationID: authorization.ID, From: owner.TimeRange.From, To: owner.TimeRange.To,
		StepSeconds: owner.Bounds.StepSeconds,
	}, authorization, testRevision())
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Actor != ActorAgent || prepared.Query != owner.Query || prepared.QueryHash != owner.QueryHash || prepared.Bounds != owner.Bounds {
		t.Fatalf("owner=%#v agent=%#v", owner, prepared)
	}
	authorization.RevokedAt = new(time.Time)
	if _, err := PrepareAgentQuery(AgentQueryRequest{AuthorizationID: authorization.ID}, authorization, testRevision()); !errors.Is(err, ErrAuthorizationRevoked) {
		t.Fatalf("revoked authorization error=%v", err)
	}
}
