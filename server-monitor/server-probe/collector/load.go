package collector

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v3/load"
)

type LoadCollector struct {
	hostname string
	load1    *prometheus.GaugeVec
	load5    *prometheus.GaugeVec
	load15   *prometheus.GaugeVec
}

func NewLoadCollector(hostname string) *LoadCollector {
	return &LoadCollector{
		hostname: hostname,
		load1: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "server_monitor_load1",
			Help: "System load average over 1 minute.",
		}, []string{"instance"}),
		load5: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "server_monitor_load5",
			Help: "System load average over 5 minutes.",
		}, []string{"instance"}),
		load15: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "server_monitor_load15",
			Help: "System load average over 15 minutes.",
		}, []string{"instance"}),
	}
}

func (c *LoadCollector) Name() string {
	return "load"
}

func (c *LoadCollector) Register(registry *prometheus.Registry) {
	registry.MustRegister(c.load1, c.load5, c.load15)
}

func (c *LoadCollector) Update(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	avg, err := load.Avg()
	if err != nil {
		return err
	}

	c.load1.WithLabelValues(c.hostname).Set(avg.Load1)
	c.load5.WithLabelValues(c.hostname).Set(avg.Load5)
	c.load15.WithLabelValues(c.hostname).Set(avg.Load15)

	return nil
}
