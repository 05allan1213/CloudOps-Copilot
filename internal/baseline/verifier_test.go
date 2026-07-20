package baseline

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/observabilityread"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

type verifierArgoStub struct {
	application change.ArgoApplication
	err         error
}

func (s verifierArgoStub) GetApplication(context.Context, string, string) (change.ArgoApplication, error) {
	return s.application, s.err
}

type verifierRuntimeStub struct {
	runtimes []change.ContainerRuntime
	err      error
}

func (s verifierRuntimeStub) ResolveRuntime(context.Context, string, string, string) ([]change.ContainerRuntime, error) {
	return s.runtimes, s.err
}

type verifierRegistryStub struct {
	metadata change.RegistryMetadata
	err      error
}

func (s verifierRegistryStub) ReadMetadata(context.Context, string, string) (change.RegistryMetadata, error) {
	return s.metadata, s.err
}

type verifierGitStub struct {
	file change.FileContent
	err  error
}

type verifierSourceGitStub struct {
	commit change.Commit
	err    error
}

func (s verifierSourceGitStub) GetCommit(context.Context, change.RepositoryRef, string) (change.Commit, error) {
	return s.commit, s.err
}

func (s verifierGitStub) GetFileContent(context.Context, change.RepositoryRef, string, string) (change.FileContent, error) {
	return s.file, s.err
}

type verifierRolloutStub struct {
	value verification.RolloutObservation
	err   error
}

func (s verifierRolloutStub) ObserveDeployment(context.Context, string, string, string) (verification.RolloutObservation, error) {
	return s.value, s.err
}

type verifierPrometheusStub struct {
	alerts, errors, availability verification.Observation
}

func (s verifierPrometheusStub) ObserveV3(_ context.Context, query observabilityread.V3MetricQuery) (verification.Observation, error) {
	switch query.Kind {
	case observabilityread.V3MetricFiringAlerts:
		return s.alerts, nil
	case observabilityread.V3MetricErrorRate:
		return s.errors, nil
	case observabilityread.V3MetricAvailability:
		return s.availability, nil
	default:
		return verification.Observation{}, errors.New("unexpected metric query")
	}
}

type verifierLogStub struct{ observation verification.Observation }

func (s verifierLogStub) ObserveLogErrorRate(context.Context, verification.SignalQuery) (verification.SignalResult, error) {
	return verification.SignalResult{Observation: s.observation}, nil
}

type verifierTraceStub struct{ observation verification.Observation }

func (s verifierTraceStub) ObserveTraceErrorRate(context.Context, verification.SignalQuery) (verification.SignalResult, error) {
	return verification.SignalResult{Observation: s.observation}, nil
}

type verifierStoreStub struct {
	snapshot Snapshot
	calls    int
}

func (s *verifierStoreStub) Activate(_ context.Context, snapshot Snapshot) (ActivationResult, error) {
	s.snapshot = snapshot
	s.calls++
	return ActivationResult{BaselineID: 7, PublicID: snapshot.PublicID(), Created: true}, nil
}

func verifierObservation(status verification.ObservationStatus, value float64, samples, matched int, at time.Time) verification.Observation {
	return verification.Observation{
		Status: status, Value: value, SampleCount: samples, MatchedCount: matched,
		SampledAt: at, QueryValid: true, SourceHealthy: true, RetentionCovered: true,
		SourceReference: "stub://provider",
	}
}

func newVerifierFixture(t *testing.T) (*Verifier, *verifierStoreStub) {
	t.Helper()
	now := time.Date(2026, 7, 20, 13, 0, 0, 0, time.UTC)
	gitopsRevision := strings.Repeat("a", 40)
	sourceRevision := strings.Repeat("d", 40)
	imageDigest := "sha256:" + strings.Repeat("b", 64)
	content := []byte("apiVersion: apps/v1\nkind: Deployment\n")
	store := &verifierStoreStub{}
	rollout := verification.RolloutObservation{
		Generation: 3, ObservedGeneration: 3, DesiredReplicas: 2, UpdatedReplicas: 2,
		ReadyReplicas: 2, AvailableReplicas: 2, UnavailableReplicas: 0, PodsReady: 2, PodsTotal: 2,
		Progressing: true, Available: true,
	}
	prom := verifierPrometheusStub{
		alerts:       verifierObservation(verification.ObservationAvailable, 1, 1, 0, now),
		errors:       verifierObservation(verification.ObservationAvailable, .001, 100, 0, now),
		availability: verifierObservation(verification.ObservationAvailable, .999, 100, 0, now),
	}
	cfg := VerifierConfig{
		Target: Target{
			Cluster: "kind-cloudops-v3", Environment: "local-demo", Namespace: "cloudops-demo",
			WorkloadKind: "Deployment", WorkloadName: "demo", ContainerName: "demo",
			Repository: "acme/gitops", BaseBranch: "main", TargetPath: "apps/demo.yaml",
		},
		Service: "demo", ArgoApplication: "demo", ArgoProject: "demo-project", ArgoPath: "apps",
		ArgoDestinationServer: "https://kubernetes.default.svc",
		SourceRepository:      change.RepositoryRef{Owner: "acme", Name: "source"},
		AllowedOCISources:     []string{"https://github.com/acme/source"},
		AlertNames:            []string{"DemoErrorRateHigh"}, Lookback: 30 * time.Minute, Now: func() time.Time { return now },
		Argo: verifierArgoStub{application: change.ArgoApplication{
			Name: "demo", Project: "demo-project", Repository: "https://github.com/acme/gitops",
			Path: "apps", DeployedRevision: gitopsRevision, SyncStatus: "Synced",
			DestinationServer: "https://kubernetes.default.svc", Namespace: "cloudops-demo",
			HealthStatus: "Healthy", OperationPhase: "Succeeded", ResultHash: strings.Repeat("c", 64),
		}},
		Runtime: verifierRuntimeStub{runtimes: []change.ContainerRuntime{{
			ContainerName: "demo", Image: "ghcr.io/acme/demo@" + imageDigest, ImageDigest: imageDigest,
		}}},
		Registry: verifierRegistryStub{metadata: change.RegistryMetadata{
			Repository: "acme/demo", ManifestDigest: imageDigest, Revision: sourceRevision,
			ConfigDigest: "sha256:" + strings.Repeat("2", 64),
			Source:       "https://github.com/acme/source", Version: sourceRevision,
			Integrity: change.RegistryIntegrityVerified, Valid: true, ResultHash: strings.Repeat("f", 64),
		}},
		SourceGit: verifierSourceGitStub{commit: change.Commit{
			Repository: "acme/source", SHA: sourceRevision, TreeSHA: strings.Repeat("1", 40),
		}},
		Git: verifierGitStub{file: change.FileContent{
			Repository: "acme/gitops", Revision: gitopsRevision, Path: "apps/demo.yaml",
			BlobSHA: strings.Repeat("e", 40), Content: content,
		}},
		Rollout: verifierRolloutStub{value: rollout}, Prometheus: prom,
		Logs:   verifierLogStub{observation: verifierObservation(verification.ObservationAvailable, 0, 1, 0, now)},
		Traces: verifierTraceStub{observation: verifierObservation(verification.ObservationAvailable, .001, 20, 0, now)},
		Store:  store,
	}
	verifier, err := NewVerifier(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return verifier, store
}

func TestVerifierRequiresEveryBoundedReadAndPersistsSnapshot(t *testing.T) {
	verifier, store := newVerifierFixture(t)
	result, err := verifier.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.BaselineID != 7 || store.calls != 1 {
		t.Fatalf("unexpected activation result=%+v calls=%d", result, store.calls)
	}
	if err := store.snapshot.Validate(); err != nil {
		t.Fatalf("stored snapshot invalid: %v", err)
	}
	if len(store.snapshot.Observations) != 7 {
		t.Fatalf("observations=%d, want 7", len(store.snapshot.Observations))
	}
}

func TestVerifierFailsClosedBeforeStoreOnUnhealthySignal(t *testing.T) {
	verifier, store := newVerifierFixture(t)
	verifier.cfg.Prometheus = verifierPrometheusStub{
		alerts:       verifierObservation(verification.ObservationAvailable, 0, 1, 1, time.Now()),
		errors:       verifierObservation(verification.ObservationAvailable, .001, 100, 0, time.Now()),
		availability: verifierObservation(verification.ObservationAvailable, .999, 100, 0, time.Now()),
	}
	if _, err := verifier.Verify(context.Background()); err == nil {
		t.Fatal("unhealthy alerts were accepted")
	}
	if store.calls != 0 {
		t.Fatalf("store calls=%d, want 0", store.calls)
	}
}

func TestVerifierRejectsRegistryDegradation(t *testing.T) {
	verifier, store := newVerifierFixture(t)
	verifier.cfg.Registry = verifierRegistryStub{metadata: change.RegistryMetadata{
		ManifestDigest: "sha256:" + strings.Repeat("b", 64), Revision: strings.Repeat("d", 40),
		ConfigDigest: "sha256:" + strings.Repeat("2", 64),
		Source:       "https://github.com/acme/source", Version: strings.Repeat("d", 40),
		Integrity: change.RegistryIntegrityUnknown, Valid: false, Degraded: true,
	}}
	if _, err := verifier.Verify(context.Background()); err == nil {
		t.Fatal("degraded registry metadata was accepted")
	}
	if store.calls != 0 {
		t.Fatalf("store calls=%d, want 0", store.calls)
	}
}

func TestVerifierRejectsUnboundSourceIdentityAndArgoPath(t *testing.T) {
	verifier, store := newVerifierFixture(t)
	metadata := verifier.cfg.Registry.(verifierRegistryStub).metadata
	metadata.Source = "https://github.com/other/source"
	verifier.cfg.Registry = verifierRegistryStub{metadata: metadata}
	if _, err := verifier.Verify(context.Background()); err == nil {
		t.Fatal("unbound OCI source was accepted")
	}
	if store.calls != 0 {
		t.Fatalf("store calls=%d, want 0", store.calls)
	}

	verifier, store = newVerifierFixture(t)
	argo := verifier.cfg.Argo.(verifierArgoStub).application
	argo.Path = "apps/other"
	verifier.cfg.Argo = verifierArgoStub{application: argo}
	if _, err := verifier.Verify(context.Background()); err == nil {
		t.Fatal("mismatched Argo application path was accepted")
	}
	if store.calls != 0 {
		t.Fatalf("store calls=%d, want 0", store.calls)
	}
}

func TestVerifierObservationPayloadIsJSON(t *testing.T) {
	verifier, store := newVerifierFixture(t)
	if _, err := verifier.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, item := range store.snapshot.Observations {
		var value map[string]any
		if err := json.Unmarshal(item.ObservedJSON, &value); err != nil || value == nil {
			t.Fatalf("observation %s is not an object: %s", item.Type, item.ObservedJSON)
		}
	}
}
