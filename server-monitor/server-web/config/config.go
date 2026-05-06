package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"server-monitor/pkg/configutil"
)

type Config struct {
	// ListenAddr HTTP 监听地址，格式为 host:port
	// 默认值：:8080
	ListenAddr string

	// PrometheusURL Prometheus 查询地址，格式为 http://host:port
	// 默认值：http://prometheus:9090
	PrometheusURL string

	// PrometheusReloadURL Prometheus 热重载地址，用于告警规则同步后触发配置重载
	// 默认值：基于 PrometheusURL 自动拼接 /-/reload
	PrometheusReloadURL string

	// AlertRulesFilePath 告警规则文件存储路径，为空时禁用规则同步功能
	// 默认值：空
	AlertRulesFilePath string

	// AlertRuleSyncEnabled 是否启用告警规则同步到 Prometheus
	// 默认值：true
	AlertRuleSyncEnabled bool

	// PromtoolPath promtool 可执行文件路径，用于校验告警规则语法
	// 默认值：promtool
	PromtoolPath string

	// AlertRuleSyncTimeout 告警规则同步操作超时时间
	// 默认值：10s
	AlertRuleSyncTimeout time.Duration

	// RequestTimeout Prometheus 查询请求超时时间
	// 默认值：5s
	RequestTimeout time.Duration

	// ReadyTimeout 就绪检查中各组件探测超时时间
	// 默认值：3s
	ReadyTimeout time.Duration

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
	// 默认值：5s
	ShutdownTimeout time.Duration

	// HostsBroadcastInterval 主机列表 WebSocket 广播间隔
	// 默认值：5s
	HostsBroadcastInterval time.Duration

	// HostsCacheTTL 主机列表缓存 TTL
	// 默认值：30s
	HostsCacheTTL time.Duration

	// DashboardOverviewTTL 仪表盘概览缓存 TTL
	// 默认值：10s
	DashboardOverviewTTL time.Duration

	// AlertEventDedupeTTL 告警事件去重窗口 TTL
	// 默认值：86400s（24 小时）
	AlertEventDedupeTTL time.Duration

	// AlertmanagerWebhookMaxBodyBytes Alertmanager Webhook 请求体最大字节数
	// 默认值：1048576（1MB）
	AlertmanagerWebhookMaxBodyBytes int64

	// CacheWriteTimeout 缓存写入操作超时时间
	// 默认值：3s
	CacheWriteTimeout time.Duration

	// GinMode Gin 框架运行模式，可选 debug / release / test
	// 默认值：debug
	GinMode string

	// TrustedProxies 受信任的反向代理 IP 列表，为空时不信任任何代理
	// 默认值：空
	TrustedProxies []string

	// CORSOrigins 允许的跨域来源列表，为空时使用默认策略
	// 默认值：空
	CORSOrigins []string

	// RateLimit 限流配置
	RateLimit RateLimitConfig

	// RedisAddr Redis 连接地址，格式为 host:port
	// 默认值：空（禁用 Redis）
	RedisAddr string

	// RedisPassword Redis 认证密码
	// 默认值：空
	// 敏感：是
	RedisPassword string

	// RedisDB Redis 数据库编号
	// 默认值：0
	RedisDB int

	// RedisStartupTimeout Redis 启动连接超时时间
	// 默认值：5s
	RedisStartupTimeout time.Duration

	// RedisDialTimeout Redis 拨号连接超时时间
	// 默认值：5s
	RedisDialTimeout time.Duration

	// RedisReadTimeout Redis 读取操作超时时间
	// 默认值：3s
	RedisReadTimeout time.Duration

	// RedisWriteTimeout Redis 写入操作超时时间
	// 默认值：3s
	RedisWriteTimeout time.Duration

	// RedisConnMaxLifetime Redis 连接最大存活时间
	// 默认值：1800s（30 分钟）
	RedisConnMaxLifetime time.Duration

	// RedisConnMaxIdleTime Redis 连接最大空闲时间
	// 默认值：300s（5 分钟）
	RedisConnMaxIdleTime time.Duration

	// MySQLHost MySQL 主机地址
	// 默认值：空（禁用 MySQL）
	MySQLHost string

	// MySQLPort MySQL 端口
	// 默认值：3306
	MySQLPort string

	// MySQLUser MySQL 用户名
	// 默认值：空
	MySQLUser string

	// MySQLPassword MySQL 密码
	// 默认值：空
	// 敏感：是
	MySQLPassword string

	// MySQLDatabase MySQL 数据库名
	// 默认值：空
	MySQLDatabase string

	// MySQLStartupTimeout MySQL 启动连接超时时间
	// 默认值：5s
	MySQLStartupTimeout time.Duration

	// MySQLPingTimeout MySQL 健康检查超时时间
	// 默认值：3s
	MySQLPingTimeout time.Duration

	// JWTSecret JWT 签名密钥，启用鉴权时必填且不少于 32 字节
	// 默认值：空
	// 敏感：是
	JWTSecret string

	// JWTExpireHours JWT 令牌过期时间（小时）
	// 默认值：24
	JWTExpireHours int

	// AuthEnabled 是否启用鉴权，生产环境必须开启
	// 默认值：true
	AuthEnabled bool

	// AdminPassword 初始管理员密码，仅在首次启动且无用户时自动创建 admin 账户
	// 默认值：空（不创建初始管理员）
	// 敏感：是
	AdminPassword string

	// StaticDir 前端静态文件目录，为空时不提供静态文件服务
	// 默认值：空
	StaticDir string

	// TraceOTLPEndpoint OpenTelemetry OTLP gRPC 导出端点，格式为 host:port
	// 默认值：空（禁用链路追踪）
	TraceOTLPEndpoint string

	// TraceSampleRate 链路追踪采样率，取值范围 [0, 1]
	// 默认值：1.0
	TraceSampleRate float64

	// KafkaBrokers Kafka Broker 地址列表，为空时禁用 Kafka 事件发送
	// 默认值：空
	KafkaBrokers []string

	// WSMaxConnections WebSocket 最大并发连接数，0 或负值使用默认值 1000
	// 默认值：1000
	WSMaxConnections int
}

type RateLimitConfig struct {
	// Enabled 是否启用限流
	// 默认值：false
	Enabled bool

	// Requests 限流窗口内允许的最大请求数
	// 默认值：120
	Requests int64

	// Window 限流滑动窗口时长
	// 默认值：60s
	Window time.Duration

	// OperationTimeout 限流 Redis 操作超时时间
	// 默认值：500ms
	OperationTimeout time.Duration
}

func Load() Config {
	prometheusURL := configutil.String("PROMETHEUS_URL", "http://prometheus:9090")
	return Config{
		ListenAddr:                      configutil.String("LISTEN_ADDR", ":8080"),
		PrometheusURL:                   prometheusURL,
		PrometheusReloadURL:             configutil.NonEmptyString("PROMETHEUS_RELOAD_URL", strings.TrimRight(prometheusURL, "/")+"/-/reload"),
		AlertRulesFilePath:              configutil.String("ALERT_RULES_FILE_PATH", ""),
		AlertRuleSyncEnabled:            configutil.Bool("ALERT_RULE_SYNC_ENABLED", true),
		PromtoolPath:                    configutil.String("PROMTOOL_PATH", "promtool"),
		AlertRuleSyncTimeout:            configutil.DurationSeconds("ALERT_RULE_SYNC_TIMEOUT_SECONDS", 10),
		RequestTimeout:                  configutil.DurationSeconds("REQUEST_TIMEOUT_SECONDS", 5),
		ReadyTimeout:                    configutil.DurationSeconds("READY_TIMEOUT_SECONDS", 3),
		HTTPReadHeaderTimeout:           configutil.DurationSeconds("HTTP_READ_HEADER_TIMEOUT_SECONDS", 5),
		HTTPReadTimeout:                 configutil.DurationSeconds("HTTP_READ_TIMEOUT_SECONDS", 15),
		HTTPWriteTimeout:                configutil.DurationSeconds("HTTP_WRITE_TIMEOUT_SECONDS", 30),
		HTTPIdleTimeout:                 configutil.DurationSeconds("HTTP_IDLE_TIMEOUT_SECONDS", 120),
		ShutdownTimeout:                 configutil.DurationSeconds("SHUTDOWN_TIMEOUT_SECONDS", 5),
		HostsBroadcastInterval:          configutil.DurationSeconds("HOSTS_BROADCAST_INTERVAL_SECONDS", 5),
		HostsCacheTTL:                   configutil.DurationSeconds("HOSTS_CACHE_TTL_SECONDS", 30),
		DashboardOverviewTTL:            configutil.DurationSeconds("DASHBOARD_OVERVIEW_TTL_SECONDS", 10),
		AlertEventDedupeTTL:             configutil.DurationSeconds("ALERT_EVENT_DEDUPE_TTL_SECONDS", 86400),
		AlertmanagerWebhookMaxBodyBytes: int64(configutil.PositiveInt("ALERTMANAGER_WEBHOOK_MAX_BODY_BYTES", 1048576)),
		CacheWriteTimeout:               configutil.DurationSeconds("CACHE_WRITE_TIMEOUT_SECONDS", 3),
		GinMode:                         configutil.String("GIN_MODE", "debug"),
		TrustedProxies:                  configutil.List("TRUSTED_PROXIES"),
		CORSOrigins:                     configutil.List("CORS_ALLOWED_ORIGINS"),
		RateLimit: RateLimitConfig{
			Enabled:          configutil.Bool("RATE_LIMIT_ENABLED", false),
			Requests:         int64(configutil.PositiveInt("RATE_LIMIT_REQUESTS", 120)),
			Window:           configutil.DurationSeconds("RATE_LIMIT_WINDOW_SECONDS", 60),
			OperationTimeout: configutil.DurationMilliseconds("RATE_LIMIT_OPERATION_TIMEOUT_MILLISECONDS", 500),
		},
		RedisAddr:            configutil.String("REDIS_ADDR", ""),
		RedisPassword:        configutil.String("REDIS_PASSWORD", ""),
		RedisDB:              configutil.NonNegativeInt("REDIS_DB", 0),
		RedisStartupTimeout:  configutil.DurationSeconds("REDIS_STARTUP_TIMEOUT_SECONDS", 5),
		RedisDialTimeout:     configutil.DurationSeconds("REDIS_DIAL_TIMEOUT_SECONDS", 5),
		RedisReadTimeout:     configutil.DurationSeconds("REDIS_READ_TIMEOUT_SECONDS", 3),
		RedisWriteTimeout:    configutil.DurationSeconds("REDIS_WRITE_TIMEOUT_SECONDS", 3),
		RedisConnMaxLifetime: configutil.DurationSeconds("REDIS_CONN_MAX_LIFETIME_SECONDS", 1800),
		RedisConnMaxIdleTime: configutil.DurationSeconds("REDIS_CONN_MAX_IDLE_TIME_SECONDS", 300),
		MySQLHost:            configutil.String("MYSQL_HOST", ""),
		MySQLPort:            configutil.String("MYSQL_PORT", "3306"),
		MySQLUser:            configutil.String("MYSQL_USER", ""),
		MySQLPassword:        configutil.String("MYSQL_PASSWORD", ""),
		MySQLDatabase:        configutil.String("MYSQL_DATABASE", ""),
		MySQLStartupTimeout:  configutil.DurationSeconds("MYSQL_STARTUP_TIMEOUT_SECONDS", 5),
		MySQLPingTimeout:     configutil.DurationSeconds("MYSQL_PING_TIMEOUT_SECONDS", 3),
		JWTSecret:            configutil.String("JWT_SECRET", ""),
		JWTExpireHours:       configutil.PositiveInt("JWT_EXPIRE_HOURS", 24),
		AuthEnabled:          configutil.Bool("AUTH_ENABLED", true),
		AdminPassword:        configutil.String("ADMIN_PASSWORD", ""),
		StaticDir:            configutil.String("STATIC_DIR", ""),
		TraceOTLPEndpoint:    configutil.NonEmptyString("TRACE_OTLP_ENDPOINT", ""),
		TraceSampleRate:      configutil.FloatRange("TRACE_SAMPLE_RATE", 1.0, 0, 1),
		KafkaBrokers:         configutil.List("KAFKA_BROKERS"),
		WSMaxConnections:     configutil.PositiveInt("WS_MAX_CONNECTIONS", 1000),
	}
}

func (c Config) Validate() error {
	if c.AuthEnabled && len(strings.TrimSpace(c.JWTSecret)) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 bytes when auth is enabled, got %d", len(strings.TrimSpace(c.JWTSecret)))
	}
	if c.ListenAddr == "" {
		return fmt.Errorf("LISTEN_ADDR is required")
	}
	if err := validateListenAddr("LISTEN_ADDR", c.ListenAddr); err != nil {
		return err
	}
	if c.PrometheusURL == "" {
		return fmt.Errorf("PROMETHEUS_URL is required")
	}
	if err := validateHTTPURL("PROMETHEUS_URL", c.PrometheusURL); err != nil {
		return err
	}
	if c.PrometheusReloadURL != "" {
		if err := validateHTTPURL("PROMETHEUS_RELOAD_URL", c.PrometheusReloadURL); err != nil {
			return err
		}
	}
	if c.RedisAddr != "" {
		if err := validateHostPort("REDIS_ADDR", c.RedisAddr); err != nil {
			return err
		}
	}
	if err := validatePort("MYSQL_PORT", c.MySQLPort); err != nil {
		return err
	}
	if c.TraceOTLPEndpoint != "" {
		if err := validateHostPort("TRACE_OTLP_ENDPOINT", c.TraceOTLPEndpoint); err != nil {
			return err
		}
	}
	for _, broker := range c.KafkaBrokers {
		if err := validateHostPort("KAFKA_BROKERS", broker); err != nil {
			return err
		}
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT_SECONDS must be positive, got %v", c.ShutdownTimeout)
	}
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("REQUEST_TIMEOUT_SECONDS must be positive, got %v", c.RequestTimeout)
	}
	if c.JWTExpireHours <= 0 {
		return fmt.Errorf("JWT_EXPIRE_HOURS must be positive, got %d", c.JWTExpireHours)
	}
	if c.RateLimit.Enabled {
		if c.RateLimit.Requests <= 0 {
			return fmt.Errorf("RATE_LIMIT_REQUESTS must be positive when rate limit is enabled, got %d", c.RateLimit.Requests)
		}
		if c.RateLimit.Window <= 0 {
			return fmt.Errorf("RATE_LIMIT_WINDOW_SECONDS must be positive when rate limit is enabled, got %v", c.RateLimit.Window)
		}
	}
	return nil
}

func validateHTTPURL(name, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be a valid http or https URL", name)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use http or https scheme, got %q", name, parsed.Scheme)
	}
	if parsed.Port() != "" {
		if err := validatePort(name, parsed.Port()); err != nil {
			return err
		}
	}
	return nil
}

func validateListenAddr(name, raw string) error {
	return validateHostPort(name, raw)
}

func validateHostPort(name, raw string) error {
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return fmt.Errorf("%s must use host:port format: %w", name, err)
	}
	if strings.TrimSpace(host) != host || strings.TrimSpace(port) != port {
		return fmt.Errorf("%s must not contain surrounding spaces", name)
	}
	return validatePort(name, port)
}

func validatePort(name, raw string) error {
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%s port must be in range 1-65535, got %q", name, raw)
	}
	return nil
}
