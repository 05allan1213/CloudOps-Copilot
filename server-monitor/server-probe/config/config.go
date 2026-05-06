package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"server-monitor/pkg/configutil"
)

type Config struct {
	// ListenAddr HTTP 监听地址，格式为 host:port
	// 默认值：:9090
	ListenAddr string

	// MetricsPath Prometheus 指标暴露路径，必须以 / 开头
	// 默认值：/metrics
	MetricsPath string

	// ScrapeInterval 指标采集间隔
	// 默认值：5s
	ScrapeInterval time.Duration

	// PromHTTPMaxRequestsInFlight Prometheus HTTP Handler 最大并发请求数
	// 默认值：5
	PromHTTPMaxRequestsInFlight int

	// PromHTTPTimeout Prometheus HTTP Handler 单请求超时时间
	// 默认值：5s
	PromHTTPTimeout time.Duration

	// HTTPReadTimeout HTTP 服务器读取请求体超时时间
	// 默认值：10s
	HTTPReadTimeout time.Duration

	// HTTPWriteTimeout HTTP 服务器写入响应超时时间
	// 默认值：10s
	HTTPWriteTimeout time.Duration

	// HTTPIdleTimeout HTTP 长连接空闲超时时间
	// 默认值：60s
	HTTPIdleTimeout time.Duration

	// ShutdownTimeout 优雅关闭总超时时间
	// 默认值：5s
	ShutdownTimeout time.Duration

	// Hostname 探针标识的主机名，用于指标标签
	// 默认值：自动获取系统主机名，获取失败时为 unknown
	Hostname string

	// HostProc 宿主机 /proc 文件系统挂载路径，用于采集主机指标
	// 默认值：空（使用本机 /proc）
	HostProc string

	// HostSys 宿主机 /sys 文件系统挂载路径，用于采集主机指标
	// 默认值：空（使用本机 /sys）
	HostSys string

	// TraceOTLPEndpoint OpenTelemetry OTLP gRPC 导出端点，格式为 host:port
	// 默认值：空（禁用链路追踪）
	TraceOTLPEndpoint string

	// TraceSampleRate 链路追踪采样率，取值范围 [0, 1]
	// 默认值：1.0
	TraceSampleRate float64
}

func Load() Config {
	return Config{
		ListenAddr:                  configutil.NonEmptyString("LISTEN_ADDR", ":9090"),
		MetricsPath:                 getEnvPath("METRICS_PATH", "/metrics"),
		ScrapeInterval:              configutil.DurationSeconds("SCRAPE_INTERVAL", 5),
		PromHTTPMaxRequestsInFlight: configutil.PositiveInt("PROMHTTP_MAX_REQUESTS_IN_FLIGHT", 5),
		PromHTTPTimeout:             configutil.DurationSeconds("PROMHTTP_TIMEOUT", 5),
		HTTPReadTimeout:             configutil.DurationSeconds("HTTP_READ_TIMEOUT", 10),
		HTTPWriteTimeout:            configutil.DurationSeconds("HTTP_WRITE_TIMEOUT", 10),
		HTTPIdleTimeout:             configutil.DurationSeconds("HTTP_IDLE_TIMEOUT", 60),
		ShutdownTimeout:             configutil.DurationSeconds("SHUTDOWN_TIMEOUT", 5),
		Hostname:                    configutil.String("HOSTNAME", getHostname()),
		HostProc:                    configutil.String("HOST_PROC", ""),
		HostSys:                     configutil.String("HOST_SYS", ""),
		TraceOTLPEndpoint:           configutil.NonEmptyString("TRACE_OTLP_ENDPOINT", ""),
		TraceSampleRate:             configutil.FloatRange("TRACE_SAMPLE_RATE", 1.0, 0, 1),
	}
}

func getHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	if h != "" {
		return h
	}
	return "unknown"
}

func getEnvPath(key, fallback string) string {
	value := configutil.NonEmptyString(key, fallback)
	if !strings.HasPrefix(value, "/") {
		return fallback
	}
	return value
}

func (c Config) Validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("LISTEN_ADDR is required")
	}
	if err := validateHostPort("LISTEN_ADDR", c.ListenAddr); err != nil {
		return err
	}
	if c.MetricsPath == "" {
		return fmt.Errorf("METRICS_PATH is required")
	}
	if !strings.HasPrefix(c.MetricsPath, "/") {
		return fmt.Errorf("METRICS_PATH must start with /")
	}
	if c.TraceOTLPEndpoint != "" {
		if err := validateHostPort("TRACE_OTLP_ENDPOINT", c.TraceOTLPEndpoint); err != nil {
			return err
		}
	}
	if c.ScrapeInterval <= 0 {
		return fmt.Errorf("SCRAPE_INTERVAL must be positive, got %v", c.ScrapeInterval)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT must be positive, got %v", c.ShutdownTimeout)
	}
	return nil
}

func validateHostPort(name, raw string) error {
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return fmt.Errorf("%s must use host:port format: %w", name, err)
	}
	if strings.TrimSpace(host) != host || strings.TrimSpace(port) != port {
		return fmt.Errorf("%s must not contain surrounding spaces", name)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("%s port must be in range 1-65535, got %q", name, port)
	}
	return nil
}
