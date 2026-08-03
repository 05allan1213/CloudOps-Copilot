package observability

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

type cancellationStore struct {
	Store
	mu     sync.Mutex
	status ExecutionStatus
}

func (s *cancellationStore) Cancel(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = ExecutionCancelled
	return nil
}

func (s *cancellationStore) Execution(context.Context, string) (Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Execution{ID: "execution-1", Status: s.status}, nil
}

func (s *cancellationStore) MarkRunning(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = ExecutionRunning
	return nil
}

func (s *cancellationStore) Complete(context.Context, string, ProviderQueryResult, error) error {
	return nil
}

type cancellationProvider struct {
	called chan struct{}
}

type cancellationRevisionStore struct{}

func (cancellationRevisionStore) ActiveRevision(context.Context) (settings.Revision, error) {
	return testRevision(), nil
}

func (cancellationRevisionStore) Revision(context.Context, string) (settings.Revision, error) {
	return testRevision(), nil
}

func (p *cancellationProvider) Catalog(context.Context, ProviderCatalogRequest) (ProviderCatalog, error) {
	return ProviderCatalog{}, nil
}

func (p *cancellationProvider) Query(context.Context, ProviderQueryRequest) (ProviderQueryResult, error) {
	close(p.called)
	return ProviderQueryResult{}, nil
}

func TestOwnerQueryCancellationWindowStopsProviderLaunch(t *testing.T) {
	store := &cancellationStore{status: ExecutionPending}
	provider := &cancellationProvider{called: make(chan struct{})}
	service := &Service{
		store: store, revisions: cancellationRevisionStore{}, provider: provider,
		semaphore: make(chan struct{}, MaximumConcurrent), results: make(map[string]QueryResult),
		cancels: make(map[string]context.CancelFunc),
	}
	service.launch("execution-1", PreparedQuery{Bounds: QueryBounds{TimeoutMS: 1_000}}, time.Second)

	execution, err := service.Cancel(context.Background(), "execution-1")
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if execution.Status != ExecutionCancelled {
		t.Fatalf("Cancel() status = %q, want %q", execution.Status, ExecutionCancelled)
	}
	service.wg.Wait()
	select {
	case <-provider.called:
		t.Fatal("Provider Query was called after cancellation")
	default:
	}
}
