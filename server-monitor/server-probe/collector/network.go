package collector

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	gopsnet "github.com/shirou/gopsutil/v3/net"
	"go.uber.org/zap"
)

type NetworkCollector struct {
	hostname      string
	recvBytes     *prometheus.CounterVec
	sentBytes     *prometheus.CounterVec
	lastRecvBytes map[string]uint64
	lastSentBytes map[string]uint64
}

func NewNetworkCollector(hostname string) *NetworkCollector {
	return &NetworkCollector{
		hostname: hostname,
		recvBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "server_monitor_network_recv_bytes_total",
			Help: "Network received bytes observed by the probe.",
		}, []string{"instance", "interface"}),
		sentBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "server_monitor_network_sent_bytes_total",
			Help: "Network sent bytes observed by the probe.",
		}, []string{"instance", "interface"}),
		lastRecvBytes: make(map[string]uint64),
		lastSentBytes: make(map[string]uint64),
	}
}

func (c *NetworkCollector) Name() string {
	return "network"
}

func (c *NetworkCollector) Register(registry *prometheus.Registry) {
	registry.MustRegister(c.recvBytes, c.sentBytes)
}

func (c *NetworkCollector) Update(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	counters, err := gopsnet.IOCounters(true)
	if err != nil {
		return err
	}

	for _, stat := range counters {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.addCounterDelta(c.recvBytes, c.lastRecvBytes, stat.Name, stat.BytesRecv)
		c.addCounterDelta(c.sentBytes, c.lastSentBytes, stat.Name, stat.BytesSent)
	}

	return nil
}

func (c *NetworkCollector) addCounterDelta(counter *prometheus.CounterVec, previous map[string]uint64, label string, current uint64) {
	last, ok := previous[label]
	previous[label] = current
	if !ok {
		counter.WithLabelValues(c.hostname, label).Add(float64(current))
		return
	}
	if current < last {
		zap.L().Warn("network counter reset detected",
			zap.String("interface", label),
			zap.Uint64("previous", last),
			zap.Uint64("current", current),
		)
		counter.WithLabelValues(c.hostname, label).Add(float64(current))
		return
	}
	counter.WithLabelValues(c.hostname, label).Add(float64(current - last))
}
