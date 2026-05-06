package collector

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v3/mem"
)

type MemoryCollector struct {
	hostname  string
	usage     *prometheus.GaugeVec
	total     *prometheus.GaugeVec
	available *prometheus.GaugeVec
}

func NewMemoryCollector(hostname string) *MemoryCollector {
	return &MemoryCollector{
		hostname: hostname,
		usage: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "server_monitor_memory_usage_percent",
			Help: "Current memory usage percentage.",
		}, []string{"instance"}),
		total: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "server_monitor_memory_total_bytes",
			Help: "Total system memory in bytes.",
		}, []string{"instance"}),
		available: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "server_monitor_memory_available_bytes",
			Help: "Available system memory in bytes.",
		}, []string{"instance"}),
	}
}

func (c *MemoryCollector) Name() string {
	return "memory"
}

func (c *MemoryCollector) Register(registry *prometheus.Registry) {
	registry.MustRegister(c.usage, c.total, c.available)
}

func (c *MemoryCollector) Update(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	virtualMemory, err := mem.VirtualMemory()
	if err != nil {
		return err
	}

	c.usage.WithLabelValues(c.hostname).Set(virtualMemory.UsedPercent)
	c.total.WithLabelValues(c.hostname).Set(float64(virtualMemory.Total))
	c.available.WithLabelValues(c.hostname).Set(float64(virtualMemory.Available))

	return nil
}
