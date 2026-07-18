package changeintelligence

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
)

type fakeIncidents struct{ item *domain.Incident }

func (f fakeIncidents) GetByPublicID(context.Context, string) (*domain.Incident, error) {
	if f.item == nil {
		return nil, domain.ErrNotFound
	}
	copy := *f.item
	return &copy, nil
}

type memoryChanges struct{ items []change.Change }

func (m *memoryChanges) CreateIfAbsent(_ context.Context, item *change.Change) (bool, error) {
	for _, existing := range m.items {
		if existing.IncidentID == item.IncidentID && existing.IdempotencyKey == item.IdempotencyKey {
			*item = existing
			return false, nil
		}
	}
	m.items = append(m.items, *item)
	return true, nil
}
func (m *memoryChanges) GetByPublicID(_ context.Context, id string) (*change.Change, error) {
	for _, item := range m.items {
		if item.PublicID == id {
			copy := item
			return &copy, nil
		}
	}
	return nil, change.ErrNotFound
}
func (m *memoryChanges) ListByIncident(_ context.Context, _ string, filter change.ListFilter) (change.Page, error) {
	return change.Page{Items: append([]change.Change(nil), m.items...), Total: int64(len(m.items)), Page: filter.Page, PageSize: filter.PageSize}, nil
}

type fakeRuntime struct {
	items []change.ContainerRuntime
	err   error
}

type fakeRegistry struct {
	metadata change.RegistryMetadata
	err      error
}

func (f fakeRegistry) ReadMetadata(_ context.Context, repository, digest string) (change.RegistryMetadata, error) {
	result := f.metadata
	result.Repository = repository
	if result.ManifestDigest == "" {
		result.ManifestDigest = digest
	}
	return result, f.err
}

func (f fakeRuntime) ResolveRuntime(context.Context, string, string, string) ([]change.ContainerRuntime, error) {
	return f.items, f.err
}

type fakeArgo struct {
	app change.ArgoApplication
	err error
}

func (f fakeArgo) GetApplication(context.Context, string, string) (change.ArgoApplication, error) {
	return f.app, f.err
}
func (f fakeArgo) GetResourceStatus(context.Context, string, string) ([]change.ArgoResource, bool, string, error) {
	return f.app.Resources, false, f.app.ResultHash, f.err
}

type fakeGitHub struct {
	unavailable bool
}

func (f fakeGitHub) GetCommit(context.Context, change.RepositoryRef, string) (change.Commit, error) {
	if f.unavailable {
		return change.Commit{}, change.ErrUnavailable
	}
	return change.Commit{SHA: strings.Repeat("b", 40), Parents: []string{strings.Repeat("a", 40)}, Message: "remove required config", HTMLURL: "https://github.example/commit"}, nil
}
func (f fakeGitHub) GetCommitDiff(context.Context, change.RepositoryRef, string) (change.DiffSummary, error) {
	return change.DiffSummary{Files: []change.FileChange{{Filename: "deploy/app.yaml", Status: "modified"}}, TotalFiles: 1, ResultHash: strings.Repeat("d", 64)}, nil
}
func (f fakeGitHub) ListPullRequestsForCommit(context.Context, change.RepositoryRef, string) ([]change.PullRequest, error) {
	return []change.PullRequest{{Number: 7, Merged: true, MergeCommitSHA: strings.Repeat("b", 40), HTMLURL: "https://github.example/pr/7"}}, nil
}
func (f fakeGitHub) GetPullRequest(context.Context, change.RepositoryRef, int64) (change.PullRequest, error) {
	return change.PullRequest{}, nil
}
func (f fakeGitHub) GetPullRequestFiles(context.Context, change.RepositoryRef, int64) (change.DiffSummary, error) {
	return change.DiffSummary{}, nil
}
func (f fakeGitHub) GetCIStatus(context.Context, change.RepositoryRef, string) (change.CIStatus, error) {
	return change.CIStatus{Conclusion: "success", WorkflowRuns: []change.WorkflowRun{{ID: 9, Name: "build"}}}, nil
}

func TestRefreshBuildsAndPersistsConfirmedCandidateIdempotently(t *testing.T) {
	incidentAt := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	deployed := incidentAt.Add(-3 * time.Minute)
	revision := strings.Repeat("b", 40)
	digest := "sha256:" + strings.Repeat("c", 64)
	incident := &domain.Incident{ID: 1, PublicID: "incident", Environment: "prod", Cluster: "prod", Namespace: "payments", ServiceName: "checkout", TargetKind: "Deployment", TargetName: "checkout", FirstSeenAt: incidentAt}
	store := &memoryChanges{}
	service, err := New(Config{
		Enabled:       true,
		Lookback:      24 * time.Hour,
		MaxCandidates: 5,
		Incidents:     fakeIncidents{incident},
		Changes:       store,
		GitHub:        fakeGitHub{},
		ArgoCD: fakeArgo{app: change.ArgoApplication{
			Name: "checkout-prod", Project: "prod", TargetRevision: "main", DeployedRevision: revision,
			SyncStatus: "Synced", HealthStatus: "Healthy", History: []change.ArgoHistory{{Revision: revision, DeployedAt: deployed}},
		}},
		Runtime: fakeRuntime{items: []change.ContainerRuntime{{
			ContainerName: "app", Image: "registry/checkout:v2", ImageDigest: digest, WorkloadKind: "deployment", WorkloadName: "checkout",
			Annotations: map[string]string{"argocd.argoproj.io/instance": "checkout-prod"},
		}}},
		Registry: fakeRegistry{metadata: change.RegistryMetadata{
			ConfigDigest: "sha256:" + strings.Repeat("d", 64), ManifestMediaType: "application/vnd.oci.image.manifest.v1+json", ConfigMediaType: "application/vnd.oci.image.config.v1+json",
			Revision: revision, Source: "https://github.com/acme/app", Version: revision, Integrity: change.RegistryIntegrityVerified, ResultHash: strings.Repeat("e", 64), Valid: true,
		}},
		RegistryHosts:     []string{"registry"},
		AllowedOCISources: []string{"https://github.com/acme/app"},
		Mappings: map[string]ServiceMapping{"prod/payments/checkout": {
			Repository: change.RepositoryRef{Owner: "acme", Name: "app"}, ArgoApplication: "checkout-prod", ArgoProject: "prod", GitOpsPath: "apps/checkout", ContainerName: "app",
		}},
		Now: func() time.Time { return incidentAt.Add(time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Refresh(context.Background(), "incident")
	if err != nil || result.ImageResolution.Status != change.ImageConfirmed || len(result.Candidates) != 1 || result.Candidates[0].Category != change.CategoryConfirmed || result.Candidates[0].PullRequestNumber != 7 || result.Candidates[0].WorkflowConclusion != "success" || len(store.items) != 1 {
		t.Fatalf("result=%+v store=%+v err=%v", result, store.items, err)
	}
	if _, err := service.Refresh(context.Background(), "incident"); err != nil || len(store.items) != 1 {
		t.Fatalf("idempotency len=%d err=%v", len(store.items), err)
	}
}

func TestRefreshDegradesWithoutAdaptersAndExcludesNegativeCandidates(t *testing.T) {
	incidentAt := time.Now().UTC()
	incident := &domain.Incident{ID: 1, PublicID: "incident", Environment: "prod", Namespace: "payments", ServiceName: "checkout", TargetKind: "deployment", TargetName: "checkout", FirstSeenAt: incidentAt}
	store := &memoryChanges{}
	service, _ := New(Config{Enabled: true, Incidents: fakeIncidents{incident}, Changes: store, Mappings: map[string]ServiceMapping{"checkout": {ArgoApplication: "app", ArgoProject: "prod"}}})
	result, err := service.Refresh(context.Background(), "incident")
	if err != nil || !result.Degraded || result.Status != "unavailable" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	after := incidentAt.Add(time.Minute)
	candidate := change.Change{PublicID: "candidate", ServiceName: "checkout", Namespace: "payments", DeployedAt: &after}
	correlated := change.Correlate(runtimeFromIncident(incident), []change.Change{candidate}, time.Hour, incidentAt)
	if !correlated.Candidates[0].Excluded {
		t.Fatalf("future candidate accepted: %+v", correlated)
	}
	service.cfg.ArgoCD = fakeArgo{err: errors.New("down")}
	result, err = service.Refresh(context.Background(), "incident")
	if err != nil || !result.Degraded {
		t.Fatalf("adapter failure result=%+v err=%v", result, err)
	}
}
