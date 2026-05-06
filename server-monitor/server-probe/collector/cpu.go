package collector

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v3/cpu"
	"go.uber.org/zap"
)

type CPUCollector struct {
	usage    *prometheus.GaugeVec
	hostname string
}

const cpuSampleInterval = 100 * time.Millisecond

func NewCPUCollector(hostname string) *CPUCollector {
	return &CPUCollector{
		usage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "server_monitor_cpu_usage_percent",
				Help: "CPU usage percentage",
			},
			[]string{"instance"},
		),
		hostname: hostname,
	}
}

func (c *CPUCollector) Name() string {
	return "cpu"
}

func (c *CPUCollector) Register(reg *prometheus.Registry) {
	reg.MustRegister(c.usage)
}

func (c *CPUCollector) Update(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	percentages, err := cpu.PercentWithContext(ctx, cpuSampleInterval, false)
	if err != nil {
		return err
	}
	if len(percentages) == 0 {
		zap.L().Warn("collector cpu percent returned empty slice")
		return nil
	}

	c.usage.WithLabelValues(c.hostname).Set(percentages[0])
	return nil
}
