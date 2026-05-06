package collector

import (
	"context"
	"errors"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v3/disk"
	"go.uber.org/zap"
)

type DiskCollector struct {
	hostname       string
	usage          *prometheus.GaugeVec
	total          *prometheus.GaugeVec
	free           *prometheus.GaugeVec
	readBytes      *prometheus.CounterVec
	writeBytes     *prometheus.CounterVec
	lastReadBytes  map[string]uint64
	lastWriteBytes map[string]uint64
}

func NewDiskCollector(hostname string) *DiskCollector {
	return &DiskCollector{
		hostname: hostname,
		usage: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "server_monitor_disk_usage_percent",
			Help: "Disk usage percentage by mountpoint.",
		}, []string{"instance", "mountpoint"}),
		total: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "server_monitor_disk_total_bytes",
			Help: "Disk total bytes by mountpoint.",
		}, []string{"instance", "mountpoint"}),
		free: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "server_monitor_disk_free_bytes",
			Help: "Disk free bytes by mountpoint.",
		}, []string{"instance", "mountpoint"}),
		readBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "server_monitor_disk_read_bytes_total",
			Help: "Disk read bytes observed by the probe.",
		}, []string{"instance", "device"}),
		writeBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "server_monitor_disk_write_bytes_total",
			Help: "Disk write bytes observed by the probe.",
		}, []string{"instance", "device"}),
		lastReadBytes:  make(map[string]uint64),
		lastWriteBytes: make(map[string]uint64),
	}
}

func (c *DiskCollector) Name() string {
	return "disk"
}

func (c *DiskCollector) Register(registry *prometheus.Registry) {
	registry.MustRegister(c.usage, c.total, c.free, c.readBytes, c.writeBytes)
}

func (c *DiskCollector) Update(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var errs []error

	partitions, err := disk.Partitions(false)
	if err != nil {
		errs = append(errs, err)
	} else {
		for _, partition := range partitions {
			if err := ctx.Err(); err != nil {
				return errors.Join(append(errs, err)...)
			}
			if !shouldCollectDiskPartition(partition) {
				continue
			}
			usage, err := disk.Usage(partition.Mountpoint)
			if err != nil {
				zap.L().Warn("disk usage collect failed",
					zap.String("mountpoint", partition.Mountpoint),
					zap.Error(err),
				)
				errs = append(errs, err)
				continue
			}
			c.usage.WithLabelValues(c.hostname, partition.Mountpoint).Set(usage.UsedPercent)
			c.total.WithLabelValues(c.hostname, partition.Mountpoint).Set(float64(usage.Total))
			c.free.WithLabelValues(c.hostname, partition.Mountpoint).Set(float64(usage.Free))
		}
	}

	if err := ctx.Err(); err != nil {
		return errors.Join(append(errs, err)...)
	}

	ioCounters, err := disk.IOCounters()
	if err != nil {
		errs = append(errs, err)
	} else {
		for device, stat := range ioCounters {
			if err := ctx.Err(); err != nil {
				return errors.Join(append(errs, err)...)
			}
			c.addCounterDelta(c.readBytes, c.lastReadBytes, device, stat.ReadBytes)
			c.addCounterDelta(c.writeBytes, c.lastWriteBytes, device, stat.WriteBytes)
		}
	}

	return errors.Join(errs...)
}

func (c *DiskCollector) addCounterDelta(counter *prometheus.CounterVec, previous map[string]uint64, label string, current uint64) {
	last, ok := previous[label]
	previous[label] = current
	if !ok {
		counter.WithLabelValues(c.hostname, label).Add(float64(current))
		return
	}
	if current < last {
		zap.L().Warn("disk counter reset detected",
			zap.String("device", label),
			zap.Uint64("previous", last),
			zap.Uint64("current", current),
		)
		counter.WithLabelValues(c.hostname, label).Add(float64(current))
		return
	}
	counter.WithLabelValues(c.hostname, label).Add(float64(current - last))
}

func shouldCollectDiskPartition(partition disk.PartitionStat) bool {
	if partition.Mountpoint == "/" {
		return true
	}

	switch strings.ToLower(partition.Fstype) {
	case "", "autofs", "binfmt_misc", "bpf", "cgroup", "cgroup2", "configfs", "debugfs", "devpts", "devtmpfs", "fusectl", "hugetlbfs", "mqueue", "nsfs", "proc", "pstore", "securityfs", "selinuxfs", "squashfs", "sysfs", "tmpfs", "tracefs":
		return false
	default:
		return true
	}
}
