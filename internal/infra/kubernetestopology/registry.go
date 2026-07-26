package kubernetestopology

import (
	"context"
	"errors"
	"strings"

	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
)

const maximumReaders = 10

// Registry dispatches each request to one bootstrap-owned Kubernetes client.
// Cluster identity is always explicit; there is no implicit default fallback.
type Registry struct {
	readers map[string]*Reader
}

func NewRegistry(readers ...*Reader) (*Registry, error) {
	if len(readers) == 0 || len(readers) > maximumReaders {
		return nil, errors.New("Kubernetes topology reader registry requires 1 to 10 clients")
	}
	registry := &Registry{readers: make(map[string]*Reader, len(readers))}
	for _, reader := range readers {
		if reader == nil {
			return nil, errors.New("Kubernetes topology reader registry contains a nil client")
		}
		if _, exists := registry.readers[reader.clusterID]; exists {
			return nil, errors.New("Kubernetes topology reader registry contains a duplicate cluster identity")
		}
		registry.readers[reader.clusterID] = reader
	}
	return registry, nil
}

func (r *Registry) Probe(ctx context.Context, clusterID string) (infrastructure.ProviderSource, error) {
	reader, err := r.reader(clusterID)
	if err != nil {
		return infrastructure.ProviderSource{}, err
	}
	return reader.Probe(ctx, clusterID)
}

func (r *Registry) Read(ctx context.Context, request infrastructure.ReadRequest) (infrastructure.Projection, error) {
	reader, err := r.reader(request.ClusterID)
	if err != nil {
		return infrastructure.Projection{}, err
	}
	return reader.Read(ctx, request)
}

func (r *Registry) Events(ctx context.Context, clusterID string, resource infrastructure.Resource, limit int) ([]infrastructure.Event, bool, error) {
	reader, err := r.reader(clusterID)
	if err != nil {
		return nil, false, err
	}
	return reader.Events(ctx, clusterID, resource, limit)
}

func (r *Registry) reader(clusterID string) (*Reader, error) {
	if r == nil {
		return nil, errors.New("Kubernetes topology reader registry is unavailable")
	}
	reader := r.readers[strings.TrimSpace(clusterID)]
	if reader == nil {
		return nil, errors.New("requested cluster is not registered by the Kubernetes topology reader")
	}
	return reader, nil
}

var _ infrastructure.Reader = (*Registry)(nil)
