package promclient

import (
	"fmt"
	"strconv"
)

const (
	MetricCPUUsage        = "cpu_usage"
	MetricMemoryUsage     = "memory_usage"
	MetricMemoryTotal     = "memory_total"
	MetricMemoryAvailable = "memory_available"
	MetricDiskUsage       = "disk_usage"
	MetricNetworkRecv     = "network_recv"
	MetricNetworkSent     = "network_sent"
	MetricLoad1           = "load1"
	MetricLoad5           = "load5"
	MetricLoad15          = "load15"
	MetricProcessCount    = "process_count"
	MetricUptime          = "uptime"
	MetricHostList        = "host_list"
	MetricActiveAlerts    = "active_alerts"
)

const (
	queryHostUp      = `up{job="server-probe"}`
	queryCPUUsage    = "server_monitor_cpu_usage_percent"
	queryMemoryUsage = "server_monitor_memory_usage_percent"
)

type queryTemplate struct {
	requiresInstance bool
	build            func(instance string, params map[string]string) (string, error)
}

var queryTemplates = map[string]queryTemplate{
	MetricCPUUsage: {
		requiresInstance: true,
		build: func(instance string, params map[string]string) (string, error) {
			return withInstance("server_monitor_cpu_usage_percent", instance), nil
		},
	},
	MetricMemoryUsage: {
		requiresInstance: true,
		build: func(instance string, params map[string]string) (string, error) {
			return withInstance("server_monitor_memory_usage_percent", instance), nil
		},
	},
	MetricMemoryTotal: {
		requiresInstance: true,
		build: func(instance string, params map[string]string) (string, error) {
			return withInstance("server_monitor_memory_total_bytes", instance), nil
		},
	},
	MetricMemoryAvailable: {
		requiresInstance: true,
		build: func(instance string, params map[string]string) (string, error) {
			return withInstance("server_monitor_memory_available_bytes", instance), nil
		},
	},
	MetricDiskUsage: {
		requiresInstance: true,
		build: func(instance string, params map[string]string) (string, error) {
			mountpoint := params["mountpoint"]
			if mountpoint == "" {
				return fmt.Sprintf("max by (instance) (server_monitor_disk_usage_percent{instance=%s})", labelValue(instance)), nil
			}
			return fmt.Sprintf("server_monitor_disk_usage_percent{instance=%s,mountpoint=%s}", labelValue(instance), labelValue(mountpoint)), nil
		},
	},
	MetricNetworkRecv: {
		requiresInstance: true,
		build: func(instance string, params map[string]string) (string, error) {
			return fmt.Sprintf("rate(server_monitor_network_recv_bytes_total{instance=%s}[5m])", labelValue(instance)), nil
		},
	},
	MetricNetworkSent: {
		requiresInstance: true,
		build: func(instance string, params map[string]string) (string, error) {
			return fmt.Sprintf("rate(server_monitor_network_sent_bytes_total{instance=%s}[5m])", labelValue(instance)), nil
		},
	},
	MetricLoad1: {
		requiresInstance: true,
		build: func(instance string, params map[string]string) (string, error) {
			return withInstance("server_monitor_load1", instance), nil
		},
	},
	MetricLoad5: {
		requiresInstance: true,
		build: func(instance string, params map[string]string) (string, error) {
			return withInstance("server_monitor_load5", instance), nil
		},
	},
	MetricLoad15: {
		requiresInstance: true,
		build: func(instance string, params map[string]string) (string, error) {
			return withInstance("server_monitor_load15", instance), nil
		},
	},
	MetricProcessCount: {
		requiresInstance: true,
		build: func(instance string, params map[string]string) (string, error) {
			return withInstance("server_monitor_process_count", instance), nil
		},
	},
	MetricUptime: {
		requiresInstance: true,
		build: func(instance string, params map[string]string) (string, error) {
			return withInstance("server_monitor_uptime_seconds", instance), nil
		},
	},
	MetricHostList: {
		build: func(instance string, params map[string]string) (string, error) {
			return queryHostUp, nil
		},
	},
	MetricActiveAlerts: {
		build: func(instance string, params map[string]string) (string, error) {
			return `ALERTS{alertstate="firing"}`, nil
		},
	},
}

func BuildQuery(metric, instance string, params map[string]string) (string, error) {
	template, ok := queryTemplates[metric]
	if !ok {
		return "", fmt.Errorf("unknown metric: %s", metric)
	}
	if params == nil {
		params = map[string]string{}
	}
	if template.requiresInstance && instance == "" {
		return "", fmt.Errorf("instance is required for metric %s", metric)
	}

	return template.build(instance, params)
}

func withInstance(metricName, instance string) string {
	return fmt.Sprintf("%s{instance=%s}", metricName, labelValue(instance))
}

func labelValue(value string) string {
	return strconv.Quote(value)
}
