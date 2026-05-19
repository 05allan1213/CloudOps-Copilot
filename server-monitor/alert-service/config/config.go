package config

import (
	"fmt"
	"time"

	"server-monitor/pkg/configutil"
)

type Config struct {
	// ListenAddr HTTP 监听地址，格式为 host:port
	// 默认值：:8081
	ListenAddr string

	// HTTPReadHeaderTimeout HTTP 服务器读取请求头超时时间
	// 默认值：5s
	HTTPReadHeaderTimeout time.Duration

	// HTTPReadTimeout HTTP 服务器读取请求体超时时间
	// 默认值：15s
	HTTPReadTimeout time.Duration

	// HTTPWriteTimeout HTTP 服务器写入响应超时时间
	// 默认值：30s
	HTTPWriteTimeout time.Duration

	// HTTPIdleTimeout HTTP 长连接空闲超时时间
	// 默认值：120s
	HTTPIdleTimeout time.Duration

	// ShutdownTimeout 优雅关闭总超时时间
	// 默认值：10s
	ShutdownTimeout time.Duration

	// KafkaBrokers Kafka Broker 地址列表
	// 默认值：kafka:9092
	KafkaBrokers []string

	// KafkaGroupID Kafka 消费者组 ID
	// 默认值：alert-service
	KafkaGroupID string

	// RedisAddr Redis 连接地址，格式为 host:port
	// 默认值：redis:6379
	RedisAddr string

	// RedisPassword Redis 认证密码
	// 默认值：空
	// 敏感：是
	RedisPassword string

	// RedisDialTimeout Redis 连接超时时间
	// 默认值：3s
	RedisDialTimeout time.Duration

	// RedisReadTimeout Redis 读取超时时间
	// 默认值：2s
	RedisReadTimeout time.Duration

	// RedisWriteTimeout Redis 写入超时时间
	// 默认值：2s
	RedisWriteTimeout time.Duration

	// ReadinessTimeout 就绪检查超时时间
	// 默认值：1s
	ReadinessTimeout time.Duration

	// ShutdownPhaseCount 优雅关闭阶段数量，用于计算每阶段超时
	// 默认值：3
	ShutdownPhaseCount int

	// TraceOTLPEndpoint OpenTelemetry OTLP gRPC 导出端点，格式为 host:port
	// 默认值：jaeger:4317
	TraceOTLPEndpoint string

	// TraceSampleRate 链路追踪采样率，取值范围 [0, 1]
	// 默认值：1.0
	TraceSampleRate float64
}

func Load() Config {
	return Config{
		ListenAddr:            configutil.String("LISTEN_ADDR", ":8081"),
		HTTPReadHeaderTimeout: configutil.DurationSeconds("HTTP_READ_HEADER_TIMEOUT_SECONDS", 5),
		HTTPReadTimeout:       configutil.DurationSeconds("HTTP_READ_TIMEOUT_SECONDS", 15),
		HTTPWriteTimeout:      configutil.DurationSeconds("HTTP_WRITE_TIMEOUT_SECONDS", 30),
		HTTPIdleTimeout:       configutil.DurationSeconds("HTTP_IDLE_TIMEOUT_SECONDS", 120),
		ShutdownTimeout:       configutil.DurationSeconds("SHUTDOWN_TIMEOUT_SECONDS", 10),
		KafkaBrokers:          configutil.ListWithFallback("KAFKA_BROKERS", []string{"kafka:9092"}),
		KafkaGroupID:          configutil.NonEmptyString("KAFKA_GROUP_ID", "alert-service"),
		RedisAddr:             configutil.NonEmptyString("REDIS_ADDR", "redis:6379"),
		RedisPassword:         configutil.String("REDIS_PASSWORD", ""),
		RedisDialTimeout:      configutil.DurationSeconds("REDIS_DIAL_TIMEOUT_SECONDS", 3),
		RedisReadTimeout:      configutil.DurationSeconds("REDIS_READ_TIMEOUT_SECONDS", 2),
		RedisWriteTimeout:     configutil.DurationSeconds("REDIS_WRITE_TIMEOUT_SECONDS", 2),
		ReadinessTimeout:      configutil.DurationSeconds("READINESS_TIMEOUT_SECONDS", 1),
		ShutdownPhaseCount:    configutil.PositiveInt("SHUTDOWN_PHASE_COUNT", 3),
		TraceOTLPEndpoint:     configutil.NonEmptyString("TRACE_OTLP_ENDPOINT", "jaeger:4317"),
		TraceSampleRate:       configutil.FloatRange("TRACE_SAMPLE_RATE", 1.0, 0, 1),
	}
}

func (c Config) Validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("LISTEN_ADDR is required")
	}
	if err := configutil.ValidateHostPort("LISTEN_ADDR", c.ListenAddr); err != nil {
		return err
	}
	if len(c.KafkaBrokers) == 0 {
		return fmt.Errorf("KAFKA_BROKERS is required")
	}
	for _, broker := range c.KafkaBrokers {
		if err := configutil.ValidateHostPort("KAFKA_BROKERS", broker); err != nil {
			return err
		}
	}
	if c.KafkaGroupID == "" {
		return fmt.Errorf("KAFKA_GROUP_ID is required")
	}
	if c.RedisAddr == "" {
		return fmt.Errorf("REDIS_ADDR is required")
	}
	if err := configutil.ValidateHostPort("REDIS_ADDR", c.RedisAddr); err != nil {
		return err
	}
	if c.TraceOTLPEndpoint != "" {
		if err := configutil.ValidateHostPort("TRACE_OTLP_ENDPOINT", c.TraceOTLPEndpoint); err != nil {
			return err
		}
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT_SECONDS must be positive, got %v", c.ShutdownTimeout)
	}
	return nil
}
