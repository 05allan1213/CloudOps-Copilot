package recoveryverificationread

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestSourceObservesBoundedWorkloadHealth(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		projection infrastructure.Projection
		readErr    error
		wantStatus verification.ObservationStatus
		wantValue  float64
		wantCount  int
		wantReason string
	}{
		{
			name: "one healthy workload",
			projection: recoveryTopology(now, infrastructure.Resource{
				Kind: "Deployment", Namespace: "demo", Name: "checkout",
				Health: infrastructure.ResourceHealth{State: infrastructure.HealthHealthy},
			}),
			wantStatus: verification.ObservationAvailable,
			wantValue:  1,
		},
		{
			name: "duplicate workload identity fails closed",
			projection: recoveryTopology(now,
				infrastructure.Resource{Kind: "Deployment", Namespace: "demo", Name: "checkout", Health: infrastructure.ResourceHealth{State: infrastructure.HealthHealthy}},
				infrastructure.Resource{Kind: "deployment", Namespace: "demo", Name: "checkout", Health: infrastructure.ResourceHealth{State: infrastructure.HealthHealthy}},
			),
			wantStatus: verification.ObservationAvailable,
			wantCount:  1,
		},
		{
			name: "partial topology is unavailable",
			projection: func() infrastructure.Projection {
				projection := recoveryTopology(now)
				projection.Partial = true
				return projection
			}(),
			wantStatus: verification.ObservationUnavailable,
			wantReason: "topology_incomplete",
		},
		{
			name:       "provider failure is unavailable",
			readErr:    errors.New("kubernetes is unavailable"),
			wantStatus: verification.ObservationUnavailable,
			wantReason: "provider_unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &recoveryTopologyReader{projection: test.projection, err: test.readErr}
			source, err := New(Config{DB: &sql.DB{}, Kubernetes: reader, Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			observation, err := source.Observe(context.Background(), recoveryRun(), recoveryCheck(verification.CheckWorkloadReady))
			if err != nil {
				t.Fatal(err)
			}
			if observation.Status != test.wantStatus || observation.Value != test.wantValue ||
				observation.MatchedCount != test.wantCount || observation.ReasonCode != test.wantReason {
				t.Fatalf("observation=%+v", observation)
			}
			if reader.calls != 1 || reader.request.ClusterID != "cloudops-local" ||
				len(reader.request.Namespaces) != 1 || reader.request.Namespaces[0] != "demo" ||
				reader.request.Limit != infrastructure.DefaultLimit {
				t.Fatalf("topology read calls=%d request=%+v", reader.calls, reader.request)
			}
		})
	}
}

func TestSourceRejectsNonRecoveryOrProviderBoundIdentity(t *testing.T) {
	reader := &recoveryTopologyReader{}
	source, err := New(Config{DB: &sql.DB{}, Kubernetes: reader})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		run   verification.Run
		check verification.Check
	}{
		{name: "non recovery run", run: func() verification.Run {
			run := recoveryRun()
			run.Plan.TriggerType = "post_delivery"
			return run
		}(), check: recoveryCheck(verification.CheckWorkloadReady)},
		{name: "mismatched run", run: recoveryRun(), check: func() verification.Check {
			check := recoveryCheck(verification.CheckWorkloadReady)
			check.VerificationRunID++
			return check
		}()},
		{name: "provider identity", run: recoveryRun(), check: func() verification.Check {
			check := recoveryCheck(verification.CheckWorkloadReady)
			check.Subject.Repository = "acme/gitops"
			return check
		}()},
		{name: "unsupported check", run: recoveryRun(), check: recoveryCheck(verification.CheckMetricErrorRateBelow)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := source.Observe(context.Background(), test.run, test.check); !errors.Is(err, verification.ErrInvalidArgument) {
				t.Fatalf("error=%v, want ErrInvalidArgument", err)
			}
		})
	}
	if reader.calls != 0 {
		t.Fatalf("invalid recovery inputs performed %d topology reads", reader.calls)
	}
}

func TestSourceRequiresMySQLAndKubernetes(t *testing.T) {
	if _, err := New(Config{Kubernetes: &recoveryTopologyReader{}}); err == nil {
		t.Fatal("New accepted a nil MySQL dependency")
	}
	if _, err := New(Config{DB: &sql.DB{}}); err == nil {
		t.Fatal("New accepted a nil Kubernetes dependency")
	}
}

type recoveryTopologyReader struct {
	projection infrastructure.Projection
	err        error
	request    infrastructure.ReadRequest
	calls      int
}

func (r *recoveryTopologyReader) Read(_ context.Context, request infrastructure.ReadRequest) (infrastructure.Projection, error) {
	r.calls++
	r.request = request
	return r.projection, r.err
}

func recoveryTopology(now time.Time, nodes ...infrastructure.Resource) infrastructure.Projection {
	return infrastructure.Projection{
		Source: infrastructure.ProviderSource{ClusterID: "cloudops-local", CollectedAt: now},
		Nodes:  nodes,
	}
}

func recoveryRun() verification.Run {
	return verification.Run{
		ID: 17, IncidentID: 29, IncidentPublicID: "11111111-1111-4111-8111-111111111111",
		Plan: verification.Plan{TriggerType: "operational_recovery", ProfileID: verification.OperationalRecoveryProfileID},
	}
}

func recoveryCheck(checkType verification.CheckType) verification.Check {
	return verification.Check{
		VerificationRunID: 17, Type: checkType, ProfileID: verification.OperationalRecoveryProfileID,
		Subject: verification.Subject{
			Cluster: "cloudops-local", Environment: "local", Namespace: "demo", Service: "checkout",
			WorkloadKind: "Deployment", WorkloadName: "checkout",
		},
	}
}
