package config

import (
	"fmt"
	"regexp"
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

	// CopilotEnabled 是否启用 Copilot API
	// 默认值：true
	CopilotEnabled bool

	// CopilotToolRegistryEnabled 是否启用 Copilot Tool Registry 执行路径
	// 默认值：true
	CopilotToolRegistryEnabled bool

	// CopilotToolDefaultTimeout Copilot 工具默认执行超时时间
	// 默认值：30s
	CopilotToolDefaultTimeout time.Duration

	// CopilotToolLogArgs 是否在工具调用日志中记录脱敏后的明文参数
	// 默认值：false
	CopilotToolLogArgs bool

	CopilotToolsClassifyEnabled bool `env:"COPILOT_TOOLS_CLASSIFY_ENABLED" envDefault:"false"`
	CopilotMultiIntentEnabled   bool `env:"COPILOT_MULTI_INTENT_ENABLED" envDefault:"false"`
	CopilotMultiIntentMax       int  `env:"COPILOT_MULTI_INTENT_MAX" envDefault:"3"`

	// CopilotSummaryEnabled 是否启用 Copilot 工具结果 LLM 摘要
	// 默认值：true
	CopilotSummaryEnabled bool

	// CopilotSummaryTimeout Copilot 摘要单次 LLM 调用超时时间
	// 默认值：8s
	CopilotSummaryTimeout time.Duration

	// CopilotSummaryMaxPromptBytes Copilot 摘要 prompt 最大字节数
	// 默认值：16384
	CopilotSummaryMaxPromptBytes int

	// RunbookDir Runbook Markdown 目录，为空时禁用 Runbook 检索
	// 默认值：../runbooks
	RunbookDir string

	// RunbookMaxFiles 最大 Runbook 文件数量
	// 默认值：100
	RunbookMaxFiles int

	// RunbookMaxFileBytes 单个 Runbook Markdown 最大字节数
	// 默认值：65536
	RunbookMaxFileBytes int64

	// RunbookSearchTopN 诊断默认注入 Runbook 片段数量
	// 默认值：2
	RunbookSearchTopN int

	RunbookBM25Weight float64
	RunbookBM25K1     float64
	RunbookBM25B      float64

	EmbeddingAPIURL            string
	EmbeddingAPIKey            string
	EmbeddingModel             string
	EmbeddingTimeout           time.Duration
	EmbeddingIndexBuildTimeout time.Duration
	EmbeddingDims              int
	RunbookRRFK                int

	RerankerEnabled bool
	RerankerTopN    int
	RerankerTimeout time.Duration

	// LLMAPIKey LLM API Key，用于后续 LLM 兜底
	// 默认值：空（禁用 LLM 调用）
	// 敏感：是
	LLMAPIKey string

	// LLMAPIURL OpenAI 兼容 Chat Completions 地址
	// 默认值：https://api.deepseek.com/v1/chat/completions
	LLMAPIURL string

	// LLMModel LLM 模型名称
	// 默认值：deepseek-chat
	LLMModel string

	// LLMTimeout LLM 请求超时时间
	// 默认值：60s
	LLMTimeout time.Duration

	// LLMMaxTokens LLM 单次响应最大 token 数
	// 默认值：800
	LLMMaxTokens int

	// DiagnosisLLMTimeout 诊断总结 LLM 请求超时时间
	// 默认值：15s
	DiagnosisLLMTimeout time.Duration

	// DiagnosisEnabled 是否启用告警自动诊断 Worker
	// 默认值：false
	DiagnosisEnabled bool

	// DiagnosisWorkerCount 自动诊断 Worker 并发数量
	// 默认值：1
	DiagnosisWorkerCount int

	// DiagnosisKafkaGroupID 自动诊断 Kafka Consumer Group ID
	// 默认值：diagnosis-worker
	DiagnosisKafkaGroupID string

	// DiagnosisTaskTTL 自动诊断 Redis 去重任务 TTL
	// 默认值：1800s（30 分钟）
	DiagnosisTaskTTL time.Duration

	// DiagnosisTaskTimeout 单次自动诊断总超时
	// 默认值：120s
	DiagnosisTaskTimeout time.Duration

	// DiagnosisRetryableErrors 临时错误是否不提交 Kafka offset
	// 默认值：true
	DiagnosisRetryableErrors bool

	// DiagnosisStatusPushEnabled 是否推送 diagnosis_update WebSocket 消息
	// 默认值：true
	DiagnosisStatusPushEnabled bool

	FeedbackEnabled          bool
	FeedbackCommentMaxLength int

	// ActionApprovalEnabled 是否启用动作审批 API
	// 默认值：true
	ActionApprovalEnabled bool

	// ActionExecutionEnabled 是否允许真实执行白名单动作
	// 默认值：false
	ActionExecutionEnabled bool

	// ActionMaxReplicas scale_deployment 允许的最大副本数
	// 默认值：10
	ActionMaxReplicas int

	// ActionPendingTTL 待审批动作建议过期时间
	// 默认值：24h
	ActionPendingTTL time.Duration

	// ActionOperationEventsEnabled 是否发布 operation-events
	// 默认值：true
	ActionOperationEventsEnabled bool

	// ActionStatusPushEnabled 是否推送 action 状态 WebSocket 消息
	// 默认值：true
	ActionStatusPushEnabled bool

	// K8SEnabled 是否启用 K8s 只读工具和诊断证据采集
	// 默认值：false
	K8SEnabled bool

	// K8SWriteEnabled 是否启用审批后的真实 K8s 写操作
	// 默认值：false
	K8SWriteEnabled bool

	// K8SNodesEnabled 是否启用集群级 Node 查询工具
	// 默认值：false
	K8SNodesEnabled bool

	// K8SInCluster 是否优先使用集群内 ServiceAccount 配置
	// 默认值：true
	K8SInCluster bool

	// K8SKubeconfig 本地或 Compose 模式使用的 kubeconfig 路径
	// 默认值：空
	// 敏感：路径本身不敏感，文件内容敏感
	K8SKubeconfig string

	// K8SAllowedNamespaces 允许访问的 namespace，空环境变量默认只允许 default
	// 默认值：default
	K8SAllowedNamespaces []string

	// K8SDefaultNamespace 未指定 namespace 时使用的默认 namespace
	// 默认值：default
	K8SDefaultNamespace string

	// K8SRequestTimeout 单次 K8s API 调用超时时间
	// 默认值：10s
	K8SRequestTimeout time.Duration

	// K8SLogTailLines k8s.get_logs 默认日志行数
	// 默认值：100
	K8SLogTailLines int

	// K8SLogMaxBytes k8s.get_logs 单次最大返回字节数
	// 默认值：32768
	K8SLogMaxBytes int

	// K8SEventLimit k8s.get_events 默认返回条数
	// 默认值：50
	K8SEventLimit int

	// CopilotSessionTTL Copilot Redis 会话 TTL
	// 默认值：7200s（2 小时）
	CopilotSessionTTL time.Duration

	// CopilotMaxMessageLength Copilot 单条消息最大字符数
	// 默认值：2000
	CopilotMaxMessageLength int

	// CopilotMaxSessionMessages Copilot 单会话保留消息数
	// 默认值：50
	CopilotMaxSessionMessages int

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
		CopilotEnabled:               configutil.Bool("COPILOT_ENABLED", true),
		CopilotToolRegistryEnabled:   configutil.Bool("COPILOT_TOOL_REGISTRY_ENABLED", true),
		CopilotToolDefaultTimeout:    configutil.DurationSeconds("COPILOT_TOOL_DEFAULT_TIMEOUT_SECONDS", 30),
		CopilotToolLogArgs:           configutil.Bool("COPILOT_TOOL_LOG_ARGS", false),
		CopilotToolsClassifyEnabled:  configutil.Bool("COPILOT_TOOLS_CLASSIFY_ENABLED", false),
		CopilotMultiIntentEnabled:    configutil.Bool("COPILOT_MULTI_INTENT_ENABLED", false),
		CopilotMultiIntentMax:        configutil.PositiveInt("COPILOT_MULTI_INTENT_MAX", 3),
		CopilotSummaryEnabled:        configutil.Bool("COPILOT_SUMMARY_ENABLED", true),
		CopilotSummaryTimeout:        configutil.DurationSeconds("COPILOT_SUMMARY_TIMEOUT_SECONDS", 8),
		CopilotSummaryMaxPromptBytes: configutil.PositiveInt("COPILOT_SUMMARY_MAX_PROMPT_BYTES", 16384),
		RunbookDir:                   configutil.String("RUNBOOK_DIR", "../runbooks"),
		RunbookMaxFiles:              configutil.PositiveInt("RUNBOOK_MAX_FILES", 100),
		RunbookMaxFileBytes:          int64(configutil.PositiveInt("RUNBOOK_MAX_FILE_BYTES", 65536)),
		RunbookSearchTopN:            configutil.PositiveInt("RUNBOOK_SEARCH_TOP_N", 2),
		RunbookBM25Weight:            configutil.FloatRange("RUNBOOK_BM25_WEIGHT", 0.3, 0, 1),
		RunbookBM25K1:                configutil.FloatRange("RUNBOOK_BM25_K1", 1.2, 0, 10),
		RunbookBM25B:                 configutil.FloatRange("RUNBOOK_BM25_B", 0.75, 0, 1),
		EmbeddingAPIURL:              configutil.String("EMBEDDING_API_URL", ""),
		EmbeddingAPIKey:              configutil.String("EMBEDDING_API_KEY", ""),
		EmbeddingModel:               configutil.String("EMBEDDING_MODEL", ""),
		EmbeddingTimeout:             configutil.DurationSeconds("EMBEDDING_TIMEOUT_SECONDS", 10),
		EmbeddingIndexBuildTimeout:   configutil.DurationSeconds("EMBEDDING_INDEX_BUILD_TIMEOUT_SECONDS", 30),
		EmbeddingDims:                configutil.NonNegativeInt("EMBEDDING_DIMS", 0),
		RunbookRRFK:                  configutil.PositiveInt("RUNBOOK_RRF_K", 60),
		RerankerEnabled:              configutil.Bool("RERANKER_ENABLED", false),
		RerankerTopN:                 configutil.PositiveInt("RERANKER_TOP_N", 2),
		RerankerTimeout:              configutil.DurationSeconds("RERANKER_TIMEOUT_SECONDS", 10),
		LLMAPIKey:                    configutil.String("LLM_API_KEY", ""),
		LLMAPIURL:                    configutil.String("LLM_API_URL", "https://api.deepseek.com/v1/chat/completions"),
		LLMModel:                     configutil.String("LLM_MODEL", "deepseek-chat"),
		LLMTimeout:                   configutil.DurationSeconds("LLM_TIMEOUT_SECONDS", 60),
		LLMMaxTokens:                 configutil.PositiveInt("LLM_MAX_TOKENS", 800),
		DiagnosisLLMTimeout:          configutil.DurationSeconds("DIAGNOSIS_LLM_TIMEOUT_SECONDS", 15),
		DiagnosisEnabled:             configutil.Bool("DIAGNOSIS_ENABLED", false),
		DiagnosisWorkerCount:         configutil.PositiveInt("DIAGNOSIS_WORKER_COUNT", 1),
		DiagnosisKafkaGroupID:        configutil.String("DIAGNOSIS_KAFKA_GROUP_ID", "diagnosis-worker"),
		DiagnosisTaskTTL:             configutil.DurationSeconds("DIAGNOSIS_TASK_TTL_SECONDS", 1800),
		DiagnosisTaskTimeout:         configutil.DurationSeconds("DIAGNOSIS_TASK_TIMEOUT_SECONDS", 120),
		DiagnosisRetryableErrors:     configutil.Bool("DIAGNOSIS_RETRYABLE_ERRORS", true),
		DiagnosisStatusPushEnabled:   configutil.Bool("DIAGNOSIS_STATUS_PUSH_ENABLED", true),
		FeedbackEnabled:              configutil.Bool("FEEDBACK_ENABLED", true),
		FeedbackCommentMaxLength:     configutil.PositiveInt("FEEDBACK_COMMENT_MAX_LENGTH", 500),
		ActionApprovalEnabled:        configutil.Bool("ACTION_APPROVAL_ENABLED", true),
		ActionExecutionEnabled:       configutil.Bool("ACTION_EXECUTION_ENABLED", false),
		ActionMaxReplicas:            configutil.PositiveInt("ACTION_MAX_REPLICAS", 10),
		ActionPendingTTL:             time.Duration(configutil.PositiveInt("ACTION_PENDING_TTL_HOURS", 24)) * time.Hour,
		ActionOperationEventsEnabled: configutil.Bool("ACTION_OPERATION_EVENTS_ENABLED", true),
		ActionStatusPushEnabled:      configutil.Bool("ACTION_STATUS_PUSH_ENABLED", true),
		K8SEnabled:                   configutil.Bool("K8S_ENABLED", false),
		K8SWriteEnabled:              configutil.Bool("K8S_WRITE_ENABLED", false),
		K8SNodesEnabled:              configutil.Bool("K8S_NODES_ENABLED", false),
		K8SInCluster:                 configutil.Bool("K8S_IN_CLUSTER", true),
		K8SKubeconfig:                configutil.String("K8S_KUBECONFIG", ""),
		K8SAllowedNamespaces:         defaultList(configutil.List("K8S_ALLOWED_NAMESPACES"), []string{"default"}),
		K8SDefaultNamespace:          configutil.String("K8S_DEFAULT_NAMESPACE", "default"),
		K8SRequestTimeout:            configutil.DurationSeconds("K8S_REQUEST_TIMEOUT_SECONDS", 10),
		K8SLogTailLines:              configutil.PositiveInt("K8S_LOG_TAIL_LINES", 100),
		K8SLogMaxBytes:               configutil.PositiveInt("K8S_LOG_MAX_BYTES", 32768),
		K8SEventLimit:                configutil.PositiveInt("K8S_EVENT_LIMIT", 50),
		CopilotSessionTTL:            configutil.DurationSeconds("COPILOT_SESSION_TTL_SECONDS", 7200),
		CopilotMaxMessageLength:      configutil.PositiveInt("COPILOT_MAX_MESSAGE_LENGTH", 2000),
		CopilotMaxSessionMessages:    configutil.PositiveInt("COPILOT_MAX_SESSION_MESSAGES", 50),
		RedisAddr:                    configutil.String("REDIS_ADDR", ""),
		RedisPassword:                configutil.String("REDIS_PASSWORD", ""),
		RedisDB:                      configutil.NonNegativeInt("REDIS_DB", 0),
		RedisStartupTimeout:          configutil.DurationSeconds("REDIS_STARTUP_TIMEOUT_SECONDS", 5),
		RedisDialTimeout:             configutil.DurationSeconds("REDIS_DIAL_TIMEOUT_SECONDS", 5),
		RedisReadTimeout:             configutil.DurationSeconds("REDIS_READ_TIMEOUT_SECONDS", 3),
		RedisWriteTimeout:            configutil.DurationSeconds("REDIS_WRITE_TIMEOUT_SECONDS", 3),
		RedisConnMaxLifetime:         configutil.DurationSeconds("REDIS_CONN_MAX_LIFETIME_SECONDS", 1800),
		RedisConnMaxIdleTime:         configutil.DurationSeconds("REDIS_CONN_MAX_IDLE_TIME_SECONDS", 300),
		MySQLHost:                    configutil.String("MYSQL_HOST", ""),
		MySQLPort:                    configutil.String("MYSQL_PORT", "3306"),
		MySQLUser:                    configutil.String("MYSQL_USER", ""),
		MySQLPassword:                configutil.String("MYSQL_PASSWORD", ""),
		MySQLDatabase:                configutil.String("MYSQL_DATABASE", ""),
		MySQLStartupTimeout:          configutil.DurationSeconds("MYSQL_STARTUP_TIMEOUT_SECONDS", 5),
		MySQLPingTimeout:             configutil.DurationSeconds("MYSQL_PING_TIMEOUT_SECONDS", 3),
		JWTSecret:                    configutil.String("JWT_SECRET", ""),
		JWTExpireHours:               configutil.PositiveInt("JWT_EXPIRE_HOURS", 24),
		AuthEnabled:                  configutil.Bool("AUTH_ENABLED", true),
		AdminPassword:                configutil.String("ADMIN_PASSWORD", ""),
		StaticDir:                    configutil.String("STATIC_DIR", ""),
		TraceOTLPEndpoint:            configutil.NonEmptyString("TRACE_OTLP_ENDPOINT", ""),
		TraceSampleRate:              configutil.FloatRange("TRACE_SAMPLE_RATE", 1.0, 0, 1),
		KafkaBrokers:                 configutil.List("KAFKA_BROKERS"),
		WSMaxConnections:             configutil.PositiveInt("WS_MAX_CONNECTIONS", 1000),
	}
}

func (c *Config) Validate() error {
	if c.AuthEnabled && len(strings.TrimSpace(c.JWTSecret)) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 bytes when auth is enabled, got %d", len(strings.TrimSpace(c.JWTSecret)))
	}
	if c.ListenAddr == "" {
		return fmt.Errorf("LISTEN_ADDR is required")
	}
	if err := configutil.ValidateListenAddr("LISTEN_ADDR", c.ListenAddr); err != nil {
		return err
	}
	if c.PrometheusURL == "" {
		return fmt.Errorf("PROMETHEUS_URL is required")
	}
	if err := configutil.ValidateHTTPURL("PROMETHEUS_URL", c.PrometheusURL); err != nil {
		return err
	}
	if c.PrometheusReloadURL != "" {
		if err := configutil.ValidateHTTPURL("PROMETHEUS_RELOAD_URL", c.PrometheusReloadURL); err != nil {
			return err
		}
	}
	if c.LLMAPIURL != "" {
		if err := configutil.ValidateHTTPURL("LLM_API_URL", c.LLMAPIURL); err != nil {
			return err
		}
	}
	if c.RedisAddr != "" {
		if err := configutil.ValidateHostPort("REDIS_ADDR", c.RedisAddr); err != nil {
			return err
		}
	}
	if err := configutil.ValidatePort("MYSQL_PORT", c.MySQLPort); err != nil {
		return err
	}
	if c.TraceOTLPEndpoint != "" {
		if err := configutil.ValidateHostPort("TRACE_OTLP_ENDPOINT", c.TraceOTLPEndpoint); err != nil {
			return err
		}
	}
	for _, broker := range c.KafkaBrokers {
		if err := configutil.ValidateHostPort("KAFKA_BROKERS", broker); err != nil {
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
	if c.LLMTimeout <= 0 {
		return fmt.Errorf("LLM_TIMEOUT_SECONDS must be positive, got %v", c.LLMTimeout)
	}
	if c.LLMMaxTokens <= 0 {
		return fmt.Errorf("LLM_MAX_TOKENS must be positive, got %d", c.LLMMaxTokens)
	}
	if c.DiagnosisLLMTimeout <= 0 {
		return fmt.Errorf("DIAGNOSIS_LLM_TIMEOUT_SECONDS must be positive, got %v", c.DiagnosisLLMTimeout)
	}
	if c.DiagnosisWorkerCount <= 0 || c.DiagnosisWorkerCount > 8 {
		return fmt.Errorf("DIAGNOSIS_WORKER_COUNT must be in range 1-8, got %d", c.DiagnosisWorkerCount)
	}
	if c.DiagnosisTaskTTL <= 0 {
		return fmt.Errorf("DIAGNOSIS_TASK_TTL_SECONDS must be positive, got %v", c.DiagnosisTaskTTL)
	}
	if c.DiagnosisTaskTimeout <= 0 {
		return fmt.Errorf("DIAGNOSIS_TASK_TIMEOUT_SECONDS must be positive, got %v", c.DiagnosisTaskTimeout)
	}
	if c.DiagnosisTaskTTL <= c.DiagnosisTaskTimeout {
		return fmt.Errorf("DIAGNOSIS_TASK_TTL_SECONDS must be greater than DIAGNOSIS_TASK_TIMEOUT_SECONDS")
	}
	if c.DiagnosisTaskTimeout <= c.DiagnosisLLMTimeout {
		return fmt.Errorf("DIAGNOSIS_TASK_TIMEOUT_SECONDS must be greater than DIAGNOSIS_LLM_TIMEOUT_SECONDS")
	}
	if c.DiagnosisEnabled {
		if !c.CopilotEnabled {
			return fmt.Errorf("COPILOT_ENABLED must be true when DIAGNOSIS_ENABLED is true")
		}
		if len(c.KafkaBrokers) == 0 {
			return fmt.Errorf("KAFKA_BROKERS is required when DIAGNOSIS_ENABLED is true")
		}
		if strings.TrimSpace(c.DiagnosisKafkaGroupID) == "" {
			return fmt.Errorf("DIAGNOSIS_KAFKA_GROUP_ID is required when DIAGNOSIS_ENABLED is true")
		}
		if strings.TrimSpace(c.RedisAddr) == "" {
			return fmt.Errorf("REDIS_ADDR is required when DIAGNOSIS_ENABLED is true")
		}
	}
	if c.FeedbackCommentMaxLength < 100 || c.FeedbackCommentMaxLength > 2000 {
		return fmt.Errorf("FEEDBACK_COMMENT_MAX_LENGTH must be in range 100-2000, got %d", c.FeedbackCommentMaxLength)
	}
	if c.ActionMaxReplicas < 1 || c.ActionMaxReplicas > 100 {
		return fmt.Errorf("ACTION_MAX_REPLICAS must be in range 1-100, got %d", c.ActionMaxReplicas)
	}
	if c.ActionPendingTTL < time.Hour || c.ActionPendingTTL > 168*time.Hour {
		return fmt.Errorf("ACTION_PENDING_TTL_HOURS must be in range 1-168, got %v", c.ActionPendingTTL)
	}
	if c.K8SWriteEnabled {
		if !c.K8SEnabled {
			return fmt.Errorf("K8S_ENABLED must be true when K8S_WRITE_ENABLED is true")
		}
		if !c.ActionApprovalEnabled {
			return fmt.Errorf("ACTION_APPROVAL_ENABLED must be true when K8S_WRITE_ENABLED is true")
		}
		if !c.ActionExecutionEnabled {
			return fmt.Errorf("ACTION_EXECUTION_ENABLED must be true when K8S_WRITE_ENABLED is true")
		}
	}
	if c.K8SNodesEnabled && !c.K8SEnabled {
		return fmt.Errorf("K8S_ENABLED must be true when K8S_NODES_ENABLED is true")
	}
	if err := checkK8SNamespace("K8S_DEFAULT_NAMESPACE", c.K8SDefaultNamespace); err != nil {
		return err
	}
	allowed := map[string]struct{}{}
	for _, namespace := range c.K8SAllowedNamespaces {
		if err := checkK8SNamespace("K8S_ALLOWED_NAMESPACES", namespace); err != nil {
			return err
		}
		allowed[namespace] = struct{}{}
	}
	if _, ok := allowed[c.K8SDefaultNamespace]; !ok {
		return fmt.Errorf("K8S_DEFAULT_NAMESPACE must be included in K8S_ALLOWED_NAMESPACES")
	}
	if c.K8SRequestTimeout < time.Second || c.K8SRequestTimeout > 60*time.Second {
		return fmt.Errorf("K8S_REQUEST_TIMEOUT_SECONDS must be in range 1-60, got %v", c.K8SRequestTimeout)
	}
	if c.K8SLogTailLines < 1 || c.K8SLogTailLines > 1000 {
		return fmt.Errorf("K8S_LOG_TAIL_LINES must be in range 1-1000, got %d", c.K8SLogTailLines)
	}
	if c.K8SLogMaxBytes < 1024 || c.K8SLogMaxBytes > 262144 {
		return fmt.Errorf("K8S_LOG_MAX_BYTES must be in range 1024-262144, got %d", c.K8SLogMaxBytes)
	}
	if c.K8SEventLimit < 1 || c.K8SEventLimit > 200 {
		return fmt.Errorf("K8S_EVENT_LIMIT must be in range 1-200, got %d", c.K8SEventLimit)
	}
	if c.CopilotSessionTTL <= 0 {
		return fmt.Errorf("COPILOT_SESSION_TTL_SECONDS must be positive, got %v", c.CopilotSessionTTL)
	}
	if c.CopilotMaxMessageLength <= 0 {
		return fmt.Errorf("COPILOT_MAX_MESSAGE_LENGTH must be positive, got %d", c.CopilotMaxMessageLength)
	}
	if c.CopilotMaxSessionMessages <= 0 {
		return fmt.Errorf("COPILOT_MAX_SESSION_MESSAGES must be positive, got %d", c.CopilotMaxSessionMessages)
	}
	if c.CopilotToolDefaultTimeout <= 0 {
		return fmt.Errorf("COPILOT_TOOL_DEFAULT_TIMEOUT_SECONDS must be positive, got %v", c.CopilotToolDefaultTimeout)
	}
	if c.CopilotSummaryTimeout <= 0 {
		return fmt.Errorf("COPILOT_SUMMARY_TIMEOUT_SECONDS must be positive, got %v", c.CopilotSummaryTimeout)
	}
	if c.CopilotSummaryMaxPromptBytes <= 0 {
		return fmt.Errorf("COPILOT_SUMMARY_MAX_PROMPT_BYTES must be positive, got %d", c.CopilotSummaryMaxPromptBytes)
	}
	if c.CopilotMultiIntentMax < 1 {
		c.CopilotMultiIntentMax = 3
	}
	if c.CopilotMultiIntentMax > 5 {
		c.CopilotMultiIntentMax = 5
	}
	if c.RunbookMaxFiles <= 0 {
		return fmt.Errorf("RUNBOOK_MAX_FILES must be positive, got %d", c.RunbookMaxFiles)
	}
	if c.RunbookMaxFileBytes <= 0 {
		return fmt.Errorf("RUNBOOK_MAX_FILE_BYTES must be positive, got %d", c.RunbookMaxFileBytes)
	}
	if c.RunbookSearchTopN <= 0 || c.RunbookSearchTopN > 5 {
		return fmt.Errorf("RUNBOOK_SEARCH_TOP_N must be in range 1-5, got %d", c.RunbookSearchTopN)
	}
	if c.EmbeddingAPIURL != "" {
		if err := configutil.ValidateHTTPURL("EMBEDDING_API_URL", c.EmbeddingAPIURL); err != nil {
			return err
		}
	}
	if c.EmbeddingTimeout < time.Second || c.EmbeddingTimeout > 60*time.Second {
		return fmt.Errorf("EMBEDDING_TIMEOUT_SECONDS must be in range 1-60, got %v", c.EmbeddingTimeout)
	}
	if c.EmbeddingIndexBuildTimeout < time.Second || c.EmbeddingIndexBuildTimeout > 300*time.Second {
		return fmt.Errorf("EMBEDDING_INDEX_BUILD_TIMEOUT_SECONDS must be in range 1-300, got %v", c.EmbeddingIndexBuildTimeout)
	}
	if c.EmbeddingDims < 0 || c.EmbeddingDims > 4096 {
		return fmt.Errorf("EMBEDDING_DIMS must be in range 0-4096, got %d", c.EmbeddingDims)
	}
	if c.RunbookRRFK < 1 || c.RunbookRRFK > 200 {
		return fmt.Errorf("RUNBOOK_RRF_K must be in range 1-200, got %d", c.RunbookRRFK)
	}
	if c.RerankerTopN < 1 || c.RerankerTopN > 5 {
		return fmt.Errorf("RERANKER_TOP_N must be in range 1-5, got %d", c.RerankerTopN)
	}
	if c.RerankerTimeout < time.Second || c.RerankerTimeout > 30*time.Second {
		return fmt.Errorf("RERANKER_TIMEOUT_SECONDS must be in range 1-30, got %v", c.RerankerTimeout)
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

var k8sNamespacePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

func checkK8SNamespace(name, namespace string) error {
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(namespace) > 63 || !k8sNamespacePattern.MatchString(namespace) {
		return fmt.Errorf("%s contains invalid namespace %q", name, namespace)
	}
	return nil
}

func defaultList(values, defaults []string) []string {
	if len(values) > 0 {
		return values
	}
	return defaults
}
