package collector

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

type Collector interface {
	Name() string
	Register(registry *prometheus.Registry)
	Update(ctx context.Context) error
}
