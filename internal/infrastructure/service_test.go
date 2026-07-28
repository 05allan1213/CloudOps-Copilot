package infrastructure

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

type configurationStub struct {
	revision settings.Revision
	results  []settings.ProviderResult
}

func (s *configurationStub) ActiveRevision(context.Context) (settings.Revision, error) {
	return s.revision, nil
}

func (s *configurationStub) ObserveProviderHealth(_ context.Context, revisionID string, result settings.ProviderResult) error {
	if revisionID != s.revision.ID {
		return errors.New("unexpected revision")
	}
	s.results = append(s.results, result)
	return nil
}

type readerStub struct {
	projection   Projection
	request      ReadRequest
	events       []Event
	truncated    bool
	readErr      error
	eventCluster string
}

func (s *readerStub) Probe(context.Context, string) (ProviderSource, error) {
	return s.projection.Source, s.readErr
}

func (s *readerStub) Read(_ context.Context, request ReadRequest) (Projection, error) {
	s.request = request
	return s.projection, s.readErr
}

func (s *readerStub) Events(_ context.Context, clusterID string, _ Resource, _ int) ([]Event, bool, error) {
	s.eventCluster = clusterID
	return s.events, s.truncated, nil
}

type repositoryStub struct {
	stores int
}

func (s *repositoryStub) Store(_ context.Context, _ string, snapshot *TopologySnapshot) error {
	s.stores++
	hash, err := ProjectionHash(snapshot.Nodes, snapshot.Edges)
	if err != nil {
		return err
	}
	snapshot.ID = "snapshot-1"
	snapshot.ContentHash = hash
	return nil
}

func TestServiceProjectsAvailableResourcesEventsAndContextLinks(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 26, 5, 0, 0, 0, time.UTC)
	configuration := &configurationStub{revision: testRevision(true)}
	resources := []Resource{
		testResource("resource-deployment", "Deployment", LayerWorkload, "api"),
		testResource("resource-pod", "Pod", LayerPod, "api-pod"),
		testResource("resource-service", "Service", LayerService, "api"),
	}
	edges := []TopologyEdge{{ID: "edge-1", SourceID: "resource-deployment", TargetID: "resource-pod", Relation: "owns", SourceFact: "metadata.ownerReferences"}}
	reader := &readerStub{
		projection: Projection{
			Source: ProviderSource{Provider: "kubernetes", ClusterID: "cluster-a", Identity: "kubernetes://cluster-a", ServerVersion: "v1.36.1", CollectedAt: now},
			Nodes:  resources, Edges: edges, Issues: []ProviderIssue{},
		},
		events: []Event{{ID: "event-1", Type: "Normal", Reason: "Started", ResourceKind: "Pod", ResourceName: "api-pod", Namespace: "ops", ObservedAt: now}},
	}
	repository := &repositoryStub{}
	service, err := NewService(configuration, reader, repository, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.now = func() time.Time { return now }
	query := Query{ClusterID: "cluster-a", Namespace: "ops", Limit: 2, From: now.Add(-time.Hour), To: now}

	topology, err := service.Topology(context.Background(), query)
	if err != nil {
		t.Fatalf("Topology() error = %v", err)
	}
	if topology.ProviderState != ProviderAvailable || topology.ID != "snapshot-1" || topology.ContentHash == "" {
		t.Fatalf("Topology() = %#v", topology)
	}
	if len(topology.Scope.Namespaces) != 1 || topology.Scope.Namespaces[0] != "ops" || reader.request.ClusterID != "cluster-a" || len(reader.request.Namespaces) != 1 {
		t.Fatalf("scoped topology=%#v reader request=%#v", topology.Scope, reader.request)
	}
	if len(topology.Nodes[0].Links) != 5 {
		t.Fatalf("resource links = %#v", topology.Nodes[0].Links)
	}
	internalLink, err := url.Parse(topology.Nodes[0].Links[0].Href)
	if err != nil {
		t.Fatalf("parse context link: %v", err)
	}
	if internalLink.Path != "/infrastructure" || internalLink.Query().Get("cluster") != "cluster-a" || internalLink.Query().Get("namespace") != "ops" || internalLink.Query().Get("resource") != topology.Nodes[0].ID || internalLink.Query().Get("from") == "" || internalLink.Query().Get("to") == "" {
		t.Fatalf("context link = %q", topology.Nodes[0].Links[0].Href)
	}
	for _, link := range topology.Nodes[0].Links[1:] {
		if link.Availability != "unavailable" {
			t.Fatalf("downstream placeholder link %#v must stay unavailable", link)
		}
	}

	page, err := service.Resources(context.Background(), Query{ClusterID: "cluster-a", Namespace: "ops", Limit: 1, From: query.From, To: query.To})
	if err != nil {
		t.Fatalf("Resources() error = %v", err)
	}
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("first page = %#v", page)
	}
	nextPage, err := service.Resources(context.Background(), Query{ClusterID: "cluster-a", Namespace: "ops", Limit: 1, Cursor: page.NextCursor, From: query.From, To: query.To})
	if err != nil || len(nextPage.Items) != 1 || nextPage.Items[0].ID <= page.Items[0].ID {
		t.Fatalf("next page=%#v error=%v", nextPage, err)
	}
	detail, err := service.Resource(context.Background(), "resource-pod", query)
	if err != nil || detail.Resource.ID != "resource-pod" || len(detail.Related) != 1 || detail.Related[0].ID != "resource-deployment" {
		t.Fatalf("Resource() detail=%#v error=%v", detail, err)
	}
	eventPage, err := service.ResourceEvents(context.Background(), "resource-pod", query)
	if err != nil || len(eventPage.Items) != 1 || eventPage.ResourceID != "resource-pod" {
		t.Fatalf("ResourceEvents() page=%#v error=%v", eventPage, err)
	}
	if reader.eventCluster != "cluster-a" {
		t.Fatalf("ResourceEvents() cluster=%q, want cluster-a", reader.eventCluster)
	}
	if repository.stores < 5 {
		t.Fatalf("repository stores=%d, want each current projection persisted", repository.stores)
	}
	if len(configuration.results) == 0 || configuration.results[len(configuration.results)-1].State != string(ProviderAvailable) {
		t.Fatalf("provider observations = %#v", configuration.results)
	}
	probeDetail, err := service.Probe(context.Background())
	if err != nil || probeDetail != "Kubernetes API typed client 连接可用 · cluster-a · v1.36.1" {
		t.Fatalf("Probe() detail=%q error=%v", probeDetail, err)
	}
	if _, err := service.ProbeCluster(context.Background(), "another-cluster"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ProbeCluster() cluster mismatch error=%v, want ErrInvalid", err)
	}
}

func TestServiceReportsDisabledUnavailableAndScopeViolationsTruthfully(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 26, 5, 0, 0, 0, time.UTC)
	disabledConfiguration := &configurationStub{revision: testRevision(false)}
	repository := &repositoryStub{}
	disabled, err := NewService(disabledConfiguration, nil, repository, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	disabled.now = func() time.Time { return now }
	query := Query{ClusterID: "cluster-a", Namespace: "ops", From: now.Add(-time.Hour), To: now}
	topology, err := disabled.Topology(context.Background(), query)
	if err != nil {
		t.Fatalf("disabled Topology() error = %v", err)
	}
	if topology.ProviderState != ProviderDisabled || len(topology.Nodes) != 0 || repository.stores != 0 {
		t.Fatalf("disabled Topology() = %#v stores=%d", topology, repository.stores)
	}
	if _, err := disabled.Resource(context.Background(), "resource-pod", query); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("disabled Resource() error = %v, want ErrUnavailable", err)
	}
	if _, err := disabled.Topology(context.Background(), Query{ClusterID: "cluster-a", Namespace: "outside", From: query.From, To: query.To}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("outside-scope Topology() error = %v, want ErrInvalid", err)
	}

	enabledConfiguration := &configurationStub{revision: testRevision(true)}
	unavailable, err := NewService(enabledConfiguration, nil, repository, errors.New("worker gateway connection refused"))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	unavailable.now = func() time.Time { return now }
	topology, err = unavailable.Topology(context.Background(), query)
	if err != nil || topology.ProviderState != ProviderUnavailable || len(topology.Nodes) != 0 || topology.ProviderDetail != "Kubernetes Provider bootstrap 不可用" {
		t.Fatalf("unavailable Topology()=%#v error=%v", topology, err)
	}
}

func testRevision(kubernetesEnabled bool) settings.Revision {
	return settings.Revision{
		ID: "revision-1", Number: 1, Hash: "hash-1", Active: true,
		General:   settings.GeneralConfiguration{QueryMaxLookbackSeconds: 7200, QueryMaxResults: 100},
		Scope:     settings.OperationalScope{ID: "scope-1", Name: "local", ClusterID: "cluster-a", Environment: "local", Namespaces: []string{"ops", "demo"}},
		Providers: []settings.ProviderConfiguration{{Provider: settings.ProviderKubernetes, Enabled: kubernetesEnabled, TimeoutMS: 1000, MaxResults: 100}},
	}
}

func testResource(id, kind string, layer ResourceLayer, name string) Resource {
	return Resource{
		ID: id, SourceUID: id + "-uid", APIVersion: "v1", Kind: kind, Layer: layer, Namespace: "ops", Name: name,
		Health: ResourceHealth{State: HealthHealthy, Summary: kind + " ready"}, OwnerReferences: []ResourceReference{},
		Selector: map[string]string{}, Labels: map[string]string{}, Endpoints: []ResourceEndpoint{}, Ports: []ResourcePort{}, Conditions: []ResourceCondition{}, Addresses: []string{}, Links: []ContextLink{},
	}
}
