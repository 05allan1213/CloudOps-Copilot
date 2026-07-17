package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"server-monitor/pkg/configutil"
	"server-web/internal/verification"
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

	// IncidentAggregationWindow V2 告警聚合到同一 Incident 的时间窗口
	// 默认值：14400s（4 小时）
	IncidentAggregationWindow time.Duration

	// IncidentAgentEnabled opts into the durable Phase 2 runtime. It is disabled by default.
	IncidentAgentEnabled            bool
	IncidentAgentWorkerID           string
	IncidentAgentPollInterval       time.Duration
	IncidentAgentLeaseDuration      time.Duration
	IncidentAgentHeartbeatPeriod    time.Duration
	IncidentAgentMaxSteps           int
	IncidentAgentMaxToolCalls       int
	IncidentAgentMaxModelCalls      int
	IncidentAgentTokenBudget        int64
	IncidentAgentMaxEvidenceItems   int
	IncidentAgentMaxRuntime         time.Duration
	IncidentAgentToolTimeout        time.Duration
	IncidentAgentMaxEvidenceBytes   int
	IncidentAgentMaxCheckpointBytes int
	IncidentAgentMaxStepRetries     int

	// FastDemo enables the disposable, controlled-direct V2 demo path only.
	FastDemoEnabled          bool
	FastDemoRevision         string
	FastDemoCluster          string
	FastDemoNamespace        string
	FastDemoWorkload         string
	FastDemoRecoveryReplicas int

	// Phase 3 change intelligence is read-only and disabled by default.
	ChangeIntelligenceEnabled bool
	ChangeLookback            time.Duration
	ChangeMaxCandidates       int
	ChangeServiceMappingsJSON string
	GitHubEnabled             bool
	GitHubAPIBaseURL          string
	GitHubAppID               int64
	GitHubInstallationID      int64
	GitHubPrivateKeyFile      string
	GitHubTokenFile           string
	GitHubAllowedOwners       []string
	GitHubAllowedRepositories []string
	GitHubAllowedBranches     []string
	GitHubAllowedPaths        []string
	GitHubDeniedPathPatterns  []string
	GitHubMaxDiffFiles        int
	GitHubMaxDiffBytes        int
	GitHubTimeout             time.Duration
	GitHubMaxRetries          int
	ArgoCDEnabled             bool
	ArgoCDServer              string
	ArgoCDTokenFile           string
	ArgoCDAllowedApplications []string
	ArgoCDAllowedProjects     []string
	ArgoCDTimeout             time.Duration
	ArgoCDMaxResources        int
	ArgoCDMaxDiffBytes        int
	ImageRevisionRequired     bool
	ImageAllowedRegistries    []string
	RegistryMetadataEnabled   bool
	RegistryBaseURL           string
	RegistryAllowedHosts      []string
	RegistryAllowedRepos      []string
	RegistryAllowedAuthRealms []string
	RegistryAllowedRedirects  []string
	OCIAllowedSources         []string
	RegistryBearerTokenFile   string
	RegistryUsernameFile      string
	RegistryPasswordFile      string
	RegistryTimeout           time.Duration
	RegistryMaxRetries        int
	RegistryManifestMaxBytes  int64
	RegistryConfigMaxBytes    int64
	RegistryCacheTTL          time.Duration
	RegistryCacheMaxItems     int

	// Phase 4 human-approved GitOps remediation is independently default-off.
	RemediationEnabled             bool
	GitOpsPREnabled                bool
	GitHubWriteEnabled             bool
	RemediationAllowedOperations   []string
	RemediationMaxPatchBytes       int
	RemediationMaxFiles            int
	RemediationMaxRisk             string
	RemediationMinReplicas         int
	RemediationMaxReplicas         int
	RemediationHPATargets          []string
	GitOpsBaseBranchesJSON         string
	GitHubWriteAPIBaseURL          string
	GitHubWriteAppID               int64
	GitHubWriteInstallationID      int64
	GitHubWritePrivateKeyFile      string
	GitHubWriteTokenFile           string
	GitHubWriteAllowedRepositories []string
	GitHubWriteAllowedBaseBranches []string
	GitHubWriteAllowedPaths        []string
	GitHubWriteTimeout             time.Duration
	GitHubWriteMaxResponseBytes    int64
	GitHubWriteMaxContentBytes     int
	RemediationWorkerID            string
	RemediationPollInterval        time.Duration
	RemediationLeaseDuration       time.Duration

	// Phase 5 delivery observation and deterministic recovery verification are independently default-off.
	DeliveryTrackingEnabled       bool
	VerificationEnabled           bool
	DeliveryWorkerID              string
	VerificationWorkerID          string
	DeliveryPollInterval          time.Duration
	DeliveryTimeout               time.Duration
	VerificationTimeout           time.Duration
	VerificationStabilityWindow   time.Duration
	VerificationLeaseDuration     time.Duration
	VerificationMaxAttempts       int
	VerificationProfilesJSON      string
	ObservabilityPrometheusURL    string
	ObservabilityLokiURL          string
	ObservabilityTempoURL         string
	ObservabilityPromTokenFile    string
	ObservabilityLokiTokenFile    string
	ObservabilityTempoTokenFile   string
	ObservabilityLokiTenant       string
	ObservabilityRequestTimeout   time.Duration
	ObservabilityMaxLookback      time.Duration
	ObservabilityMaxResponseBytes int64
	ObservabilityMaxSamples       int
	ObservabilityMaxSeries        int
	ObservabilityMaxTraces        int
	ObservabilityMaxRetries       int

	// GlobalMaxBodyBytes 全局 API 请求体最大字节数
	// 默认值：2097152（2MB）
	GlobalMaxBodyBytes int64

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

	// CopilotLLMClassifyThreshold 规则分类置信度低于该值时走 LLM 分类主路径
	// 默认值：0.9
	CopilotLLMClassifyThreshold float64

	// CopilotSummaryEnabled 是否启用 Copilot 工具结果 LLM 摘要
	// 默认值：true
	CopilotSummaryEnabled bool

	// CopilotSummaryTimeout Copilot 摘要单次 LLM 调用超时时间
	// 默认值：8s
	CopilotSummaryTimeout time.Duration

	// CopilotSummaryMaxPromptBytes Copilot 摘要 prompt 最大字节数
	// 默认值：16384
	CopilotSummaryMaxPromptBytes int

	// CopilotChatHistoryMaxRounds Copilot 传给 LLM 的历史对话轮数
	// 默认值：10
	CopilotChatHistoryMaxRounds int

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

	// K8SAPIEnabled 是否启用 K8s HTTP API 和前端页面
	// 默认值：false
	K8SAPIEnabled bool

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

	K8sCacheTTL     time.Duration
	K8sListCacheTTL time.Duration

	// K8sEventWatchInterval K8s Event WebSocket 推送轮询间隔
	// 默认值：5s
	K8sEventWatchInterval time.Duration

	K8SClusters []K8SClusterConfig

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

type K8SClusterConfig struct {
	Name              string
	Kubeconfig        string
	InCluster         bool
	AllowedNamespaces []string
	DefaultNamespace  string
	RequestTimeout    time.Duration
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

type EffectiveChangeConfig struct {
	Enabled               bool          `json:"enabled"`
	Lookback              time.Duration `json:"lookback"`
	MaxCandidates         int           `json:"max_candidates"`
	MappingCount          int           `json:"mapping_count"`
	GitHubEnabled         bool          `json:"github_enabled"`
	GitHubAPIBaseURL      string        `json:"github_api_base_url,omitempty"`
	GitHubRepositoryCount int           `json:"github_repository_count"`
	ArgoCDEnabled         bool          `json:"argocd_enabled"`
	ArgoCDServer          string        `json:"argocd_server,omitempty"`
	ArgoApplicationCount  int           `json:"argocd_application_count"`
	ImageRevisionRequired bool          `json:"image_revision_required"`
	RegistryEnabled       bool          `json:"registry_metadata_enabled"`
	RegistryBaseURL       string        `json:"registry_base_url,omitempty"`
	RegistryHostCount     int           `json:"registry_host_count"`
	RegistryRepoCount     int           `json:"registry_repository_count"`
	RegistrySourceCount   int           `json:"oci_source_repository_count"`
	RegistryTimeout       time.Duration `json:"registry_timeout"`
	RegistryMaxRetries    int           `json:"registry_max_retries"`
	RegistryManifestLimit int64         `json:"registry_manifest_max_bytes"`
	RegistryConfigLimit   int64         `json:"registry_config_max_bytes"`
	RegistryCacheTTL      time.Duration `json:"registry_cache_ttl"`
	RegistryCacheMaxItems int           `json:"registry_cache_max_items"`
}

// EffectiveChangeConfig returns only non-secret startup diagnostics. Credential values and paths are omitted.
func (c Config) EffectiveChangeConfig() EffectiveChangeConfig {
	mappingCount := 0
	var mappings map[string]json.RawMessage
	if json.Unmarshal([]byte(c.ChangeServiceMappingsJSON), &mappings) == nil {
		mappingCount = len(mappings)
	}
	return EffectiveChangeConfig{Enabled: c.ChangeIntelligenceEnabled, Lookback: c.ChangeLookback, MaxCandidates: c.ChangeMaxCandidates, MappingCount: mappingCount, GitHubEnabled: c.GitHubEnabled, GitHubAPIBaseURL: c.GitHubAPIBaseURL, GitHubRepositoryCount: len(c.GitHubAllowedRepositories), ArgoCDEnabled: c.ArgoCDEnabled, ArgoCDServer: c.ArgoCDServer, ArgoApplicationCount: len(c.ArgoCDAllowedApplications), ImageRevisionRequired: c.ImageRevisionRequired, RegistryEnabled: c.RegistryMetadataEnabled, RegistryBaseURL: c.RegistryBaseURL, RegistryHostCount: len(c.RegistryAllowedHosts), RegistryRepoCount: len(c.RegistryAllowedRepos), RegistrySourceCount: len(c.OCIAllowedSources), RegistryTimeout: c.RegistryTimeout, RegistryMaxRetries: c.RegistryMaxRetries, RegistryManifestLimit: c.RegistryManifestMaxBytes, RegistryConfigLimit: c.RegistryConfigMaxBytes, RegistryCacheTTL: c.RegistryCacheTTL, RegistryCacheMaxItems: c.RegistryCacheMaxItems}
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
		IncidentAggregationWindow:       configutil.DurationSeconds("INCIDENT_AGGREGATION_WINDOW_SECONDS", 14400),
		IncidentAgentEnabled:            configutil.Bool("INCIDENT_AGENT_ENABLED", false),
		IncidentAgentWorkerID:           configutil.String("INCIDENT_AGENT_WORKER_ID", configutil.String("HOSTNAME", "server-web-agent-worker")),
		IncidentAgentPollInterval:       configutil.DurationMilliseconds("INCIDENT_AGENT_POLL_INTERVAL_MILLISECONDS", 1000),
		IncidentAgentLeaseDuration:      configutil.DurationSeconds("INCIDENT_AGENT_LEASE_SECONDS", 30),
		IncidentAgentHeartbeatPeriod:    configutil.DurationSeconds("INCIDENT_AGENT_HEARTBEAT_SECONDS", 10),
		IncidentAgentMaxSteps:           configutil.PositiveInt("INCIDENT_AGENT_MAX_STEPS", 12),
		IncidentAgentMaxToolCalls:       configutil.PositiveInt("INCIDENT_AGENT_MAX_TOOL_CALLS", 6),
		IncidentAgentMaxModelCalls:      configutil.PositiveInt("INCIDENT_AGENT_MAX_MODEL_CALLS", 8),
		IncidentAgentTokenBudget:        int64(configutil.PositiveInt("INCIDENT_AGENT_TOKEN_BUDGET", 12000)),
		IncidentAgentMaxEvidenceItems:   configutil.PositiveInt("INCIDENT_AGENT_MAX_EVIDENCE_ITEMS", 12),
		IncidentAgentMaxRuntime:         configutil.DurationSeconds("INCIDENT_AGENT_MAX_RUNTIME_SECONDS", 120),
		IncidentAgentToolTimeout:        configutil.DurationSeconds("INCIDENT_AGENT_TOOL_TIMEOUT_SECONDS", 15),
		IncidentAgentMaxEvidenceBytes:   configutil.PositiveInt("INCIDENT_AGENT_MAX_EVIDENCE_BYTES", 16384),
		IncidentAgentMaxCheckpointBytes: configutil.PositiveInt("INCIDENT_AGENT_MAX_CHECKPOINT_BYTES", 32768),
		IncidentAgentMaxStepRetries:     configutil.NonNegativeInt("INCIDENT_AGENT_MAX_STEP_RETRIES", 1),
		FastDemoEnabled:                 configutil.Bool("FAST_DEMO_ENABLED", false),
		FastDemoRevision:                configutil.String("FAST_DEMO_REVISION", ""),
		FastDemoCluster:                 configutil.String("FAST_DEMO_CLUSTER", "kind-cloudops-demo"),
		FastDemoNamespace:               configutil.String("FAST_DEMO_NAMESPACE", "default"),
		FastDemoWorkload:                configutil.String("FAST_DEMO_WORKLOAD", "cloudops-demo-workload"),
		FastDemoRecoveryReplicas:        configutil.PositiveInt("FAST_DEMO_RECOVERY_REPLICAS", 2),
		ChangeIntelligenceEnabled:       configutil.Bool("CHANGE_INTELLIGENCE_ENABLED", false),
		ChangeLookback:                  configutil.DurationSeconds("CHANGE_LOOKBACK", 86400),
		ChangeMaxCandidates:             configutil.PositiveInt("CHANGE_MAX_CANDIDATES", 10),
		ChangeServiceMappingsJSON:       configutil.String("CHANGE_SERVICE_MAPPINGS_JSON", "{}"),
		GitHubEnabled:                   configutil.Bool("GITHUB_ENABLED", false),
		GitHubAPIBaseURL:                configutil.String("GITHUB_API_BASE_URL", "https://api.github.com"),
		GitHubAppID:                     int64(configutil.NonNegativeInt("GITHUB_APP_ID", 0)),
		GitHubInstallationID:            int64(configutil.NonNegativeInt("GITHUB_INSTALLATION_ID", 0)),
		GitHubPrivateKeyFile:            configutil.String("GITHUB_PRIVATE_KEY_FILE", ""),
		GitHubTokenFile:                 configutil.String("GITHUB_TOKEN_FILE", ""),
		GitHubAllowedOwners:             configutil.List("GITHUB_ALLOWED_OWNERS"),
		GitHubAllowedRepositories:       configutil.List("GITHUB_ALLOWED_REPOSITORIES"),
		GitHubAllowedBranches:           configutil.List("GITHUB_ALLOWED_BRANCHES"),
		GitHubAllowedPaths:              configutil.List("GITHUB_ALLOWED_PATHS"),
		GitHubDeniedPathPatterns:        configutil.List("GITHUB_DENIED_PATH_PATTERNS"),
		GitHubMaxDiffFiles:              configutil.PositiveInt("GITHUB_MAX_DIFF_FILES", 100),
		GitHubMaxDiffBytes:              configutil.PositiveInt("GITHUB_MAX_DIFF_BYTES", 131072),
		GitHubTimeout:                   configutil.DurationSeconds("GITHUB_TIMEOUT", 10),
		GitHubMaxRetries:                configutil.NonNegativeInt("GITHUB_MAX_RETRIES", 1),
		ArgoCDEnabled:                   configutil.Bool("ARGOCD_ENABLED", false),
		ArgoCDServer:                    configutil.String("ARGOCD_SERVER", ""),
		ArgoCDTokenFile:                 configutil.String("ARGOCD_TOKEN_FILE", ""),
		ArgoCDAllowedApplications:       configutil.List("ARGOCD_ALLOWED_APPLICATIONS"),
		ArgoCDAllowedProjects:           configutil.List("ARGOCD_ALLOWED_PROJECTS"),
		ArgoCDTimeout:                   configutil.DurationSeconds("ARGOCD_TIMEOUT", 10),
		ArgoCDMaxResources:              configutil.PositiveInt("ARGOCD_MAX_RESOURCES", 100),
		ArgoCDMaxDiffBytes:              configutil.PositiveInt("ARGOCD_MAX_DIFF_BYTES", 131072),
		ImageRevisionRequired:           configutil.Bool("IMAGE_REVISION_REQUIRED", false),
		ImageAllowedRegistries:          configutil.List("IMAGE_ALLOWED_REGISTRIES"),
		RegistryMetadataEnabled:         configutil.Bool("REGISTRY_METADATA_ENABLED", false),
		RegistryBaseURL:                 configutil.String("REGISTRY_BASE_URL", ""),
		RegistryAllowedHosts:            configutil.List("REGISTRY_ALLOWED_HOSTS"),
		RegistryAllowedRepos:            configutil.List("REGISTRY_ALLOWED_REPOSITORIES"),
		RegistryAllowedAuthRealms:       configutil.List("REGISTRY_ALLOWED_AUTH_REALM_HOSTS"),
		RegistryAllowedRedirects:        configutil.List("REGISTRY_ALLOWED_REDIRECT_HOSTS"),
		OCIAllowedSources:               configutil.List("OCI_ALLOWED_SOURCE_REPOSITORIES"),
		RegistryBearerTokenFile:         configutil.String("REGISTRY_BEARER_TOKEN_FILE", ""),
		RegistryUsernameFile:            configutil.String("REGISTRY_USERNAME_FILE", ""),
		RegistryPasswordFile:            configutil.String("REGISTRY_PASSWORD_FILE", ""),
		RegistryTimeout:                 configutil.DurationSeconds("REGISTRY_TIMEOUT", 10),
		RegistryMaxRetries:              configutil.NonNegativeInt("REGISTRY_MAX_RETRIES", 1),
		RegistryManifestMaxBytes:        int64(configutil.PositiveInt("REGISTRY_MANIFEST_MAX_BYTES", 4194304)),
		RegistryConfigMaxBytes:          int64(configutil.PositiveInt("REGISTRY_CONFIG_MAX_BYTES", 1048576)),
		RegistryCacheTTL:                configutil.DurationSeconds("REGISTRY_CACHE_TTL_SECONDS", 300),
		RegistryCacheMaxItems:           configutil.PositiveInt("REGISTRY_CACHE_MAX_ITEMS", 256),
		RemediationEnabled:              configutil.Bool("REMEDIATION_ENABLED", false),
		GitOpsPREnabled:                 configutil.Bool("GITOPS_PR_ENABLED", false),
		GitHubWriteEnabled:              configutil.Bool("GITHUB_WRITE_ENABLED", false),
		RemediationAllowedOperations:    configutil.List("REMEDIATION_ALLOWED_OPERATIONS"),
		RemediationMaxPatchBytes:        configutil.PositiveInt("REMEDIATION_MAX_PATCH_BYTES", 16384),
		RemediationMaxFiles:             configutil.PositiveInt("REMEDIATION_MAX_FILES", 1),
		RemediationMaxRisk:              configutil.String("REMEDIATION_MAX_RISK", "medium"),
		RemediationMinReplicas:          configutil.NonNegativeInt("REMEDIATION_MIN_REPLICAS", 1),
		RemediationMaxReplicas:          configutil.PositiveInt("REMEDIATION_MAX_REPLICAS", 20),
		RemediationHPATargets:           configutil.List("REMEDIATION_HPA_TARGETS"),
		GitOpsBaseBranchesJSON:          configutil.String("GITOPS_BASE_BRANCHES_JSON", "{}"),
		GitHubWriteAPIBaseURL:           configutil.String("GITHUB_WRITE_API_BASE_URL", "https://api.github.com"),
		GitHubWriteAppID:                int64(configutil.NonNegativeInt("GITHUB_WRITE_APP_ID", 0)),
		GitHubWriteInstallationID:       int64(configutil.NonNegativeInt("GITHUB_WRITE_INSTALLATION_ID", 0)),
		GitHubWritePrivateKeyFile:       configutil.String("GITHUB_WRITE_PRIVATE_KEY_FILE", ""),
		GitHubWriteTokenFile:            configutil.String("GITHUB_WRITE_TOKEN_FILE", ""),
		GitHubWriteAllowedRepositories:  configutil.List("GITHUB_WRITE_ALLOWED_REPOSITORIES"),
		GitHubWriteAllowedBaseBranches:  configutil.List("GITHUB_WRITE_ALLOWED_BASE_BRANCHES"),
		GitHubWriteAllowedPaths:         configutil.List("GITHUB_WRITE_ALLOWED_PATHS"),
		GitHubWriteTimeout:              configutil.DurationSeconds("GITHUB_WRITE_TIMEOUT_SECONDS", 10),
		GitHubWriteMaxResponseBytes:     int64(configutil.PositiveInt("GITHUB_WRITE_MAX_RESPONSE_BYTES", 1048576)),
		GitHubWriteMaxContentBytes:      configutil.PositiveInt("GITHUB_WRITE_MAX_CONTENT_BYTES", 131072),
		RemediationWorkerID:             configutil.String("REMEDIATION_WORKER_ID", configutil.String("HOSTNAME", "server-web-remediation-worker")),
		RemediationPollInterval:         configutil.DurationMilliseconds("REMEDIATION_POLL_INTERVAL_MILLISECONDS", 1000),
		RemediationLeaseDuration:        configutil.DurationSeconds("REMEDIATION_LEASE_SECONDS", 30),
		DeliveryTrackingEnabled:         configutil.Bool("DELIVERY_TRACKING_ENABLED", false),
		VerificationEnabled:             configutil.Bool("VERIFICATION_ENABLED", false),
		DeliveryWorkerID:                configutil.String("DELIVERY_WORKER_ID", configutil.String("HOSTNAME", "server-web-delivery-worker")),
		VerificationWorkerID:            configutil.String("VERIFICATION_WORKER_ID", configutil.String("HOSTNAME", "server-web-verification-worker")),
		DeliveryPollInterval:            configutil.DurationSeconds("DELIVERY_POLL_INTERVAL_SECONDS", 10),
		DeliveryTimeout:                 configutil.DurationSeconds("DELIVERY_TIMEOUT_SECONDS", 3600),
		VerificationTimeout:             configutil.DurationSeconds("VERIFICATION_TIMEOUT_SECONDS", 1800),
		VerificationStabilityWindow:     configutil.DurationSeconds("VERIFICATION_STABILITY_WINDOW_SECONDS", 120),
		VerificationLeaseDuration:       configutil.DurationSeconds("VERIFICATION_LEASE_SECONDS", 30),
		VerificationMaxAttempts:         configutil.PositiveInt("VERIFICATION_MAX_ATTEMPTS", 180),
		VerificationProfilesJSON:        configutil.String("VERIFICATION_PROFILES_JSON", `{"profiles":[]}`),
		ObservabilityPrometheusURL:      configutil.String("OBSERVABILITY_PROMETHEUS_URL", "https://prometheus.invalid"),
		ObservabilityLokiURL:            configutil.String("OBSERVABILITY_LOKI_URL", "https://loki.invalid"),
		ObservabilityTempoURL:           configutil.String("OBSERVABILITY_TEMPO_URL", "https://tempo.invalid"),
		ObservabilityPromTokenFile:      configutil.String("OBSERVABILITY_PROMETHEUS_TOKEN_FILE", ""),
		ObservabilityLokiTokenFile:      configutil.String("OBSERVABILITY_LOKI_TOKEN_FILE", ""),
		ObservabilityTempoTokenFile:     configutil.String("OBSERVABILITY_TEMPO_TOKEN_FILE", ""),
		ObservabilityLokiTenant:         configutil.String("OBSERVABILITY_LOKI_TENANT", ""),
		ObservabilityRequestTimeout:     configutil.DurationSeconds("OBSERVABILITY_REQUEST_TIMEOUT_SECONDS", 10),
		ObservabilityMaxLookback:        configutil.DurationSeconds("OBSERVABILITY_MAX_LOOKBACK_SECONDS", 3600),
		ObservabilityMaxResponseBytes:   int64(configutil.PositiveInt("OBSERVABILITY_MAX_RESPONSE_BYTES", 262144)),
		ObservabilityMaxSamples:         configutil.PositiveInt("OBSERVABILITY_MAX_SAMPLES", 1000),
		ObservabilityMaxSeries:          configutil.PositiveInt("OBSERVABILITY_MAX_SERIES", 20),
		ObservabilityMaxTraces:          configutil.PositiveInt("OBSERVABILITY_MAX_TRACES", 100),
		ObservabilityMaxRetries:         configutil.NonNegativeInt("OBSERVABILITY_MAX_RETRIES", 1),
		GlobalMaxBodyBytes:              int64(configutil.PositiveInt("GLOBAL_MAX_BODY_BYTES", 2097152)),
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
		CopilotLLMClassifyThreshold:  configutil.FloatRange("COPILOT_LLM_CLASSIFY_THRESHOLD", 0.9, 0, 1),
		CopilotSummaryEnabled:        configutil.Bool("COPILOT_SUMMARY_ENABLED", true),
		CopilotSummaryTimeout:        configutil.DurationSeconds("COPILOT_SUMMARY_TIMEOUT_SECONDS", 8),
		CopilotSummaryMaxPromptBytes: configutil.PositiveInt("COPILOT_SUMMARY_MAX_PROMPT_BYTES", 16384),
		CopilotChatHistoryMaxRounds:  configutil.PositiveInt("COPILOT_CHAT_HISTORY_MAX_ROUNDS", 10),
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
		K8SAPIEnabled:                configutil.Bool("K8S_API_ENABLED", false),
		K8SInCluster:                 configutil.Bool("K8S_IN_CLUSTER", true),
		K8SKubeconfig:                configutil.String("K8S_KUBECONFIG", ""),
		K8SAllowedNamespaces:         defaultList(configutil.List("K8S_ALLOWED_NAMESPACES"), []string{"default"}),
		K8SDefaultNamespace:          configutil.String("K8S_DEFAULT_NAMESPACE", "default"),
		K8SRequestTimeout:            configutil.DurationSeconds("K8S_REQUEST_TIMEOUT_SECONDS", 10),
		K8SLogTailLines:              configutil.PositiveInt("K8S_LOG_TAIL_LINES", 100),
		K8SLogMaxBytes:               configutil.PositiveInt("K8S_LOG_MAX_BYTES", 32768),
		K8SEventLimit:                configutil.PositiveInt("K8S_EVENT_LIMIT", 50),
		K8sCacheTTL:                  configutil.DurationSeconds("K8S_CACHE_TTL_SECONDS", 30),
		K8sListCacheTTL:              configutil.DurationSeconds("K8S_LIST_CACHE_TTL_SECONDS", 15),
		K8sEventWatchInterval:        configutil.DurationSeconds("K8S_EVENT_WATCH_INTERVAL_SECONDS", 5),
		K8SClusters:                  parseK8SClusters(configutil.String("K8S_CLUSTERS", "")),
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
	if c.IncidentAggregationWindow < time.Minute || c.IncidentAggregationWindow > 24*time.Hour {
		return fmt.Errorf("INCIDENT_AGGREGATION_WINDOW_SECONDS must be in range 60-86400, got %v", c.IncidentAggregationWindow)
	}
	if c.IncidentAgentEnabled {
		if workerID := strings.TrimSpace(c.IncidentAgentWorkerID); workerID == "" || len(workerID) > 128 {
			return fmt.Errorf("INCIDENT_AGENT_WORKER_ID must contain 1-128 bytes when INCIDENT_AGENT_ENABLED is true")
		}
		if !c.CopilotEnabled || !c.CopilotToolRegistryEnabled {
			return fmt.Errorf("COPILOT_ENABLED and COPILOT_TOOL_REGISTRY_ENABLED must be true when INCIDENT_AGENT_ENABLED is true")
		}
		if (!c.FastDemoEnabled && strings.TrimSpace(c.LLMAPIKey) == "") || strings.TrimSpace(c.MySQLHost) == "" {
			return fmt.Errorf("LLM_API_KEY and MySQL configuration are required when INCIDENT_AGENT_ENABLED is true outside fast demo mode")
		}
		if c.IncidentAgentPollInterval <= 0 || c.IncidentAgentLeaseDuration <= 0 || c.IncidentAgentHeartbeatPeriod <= 0 || c.IncidentAgentHeartbeatPeriod >= c.IncidentAgentLeaseDuration || c.IncidentAgentMaxRuntime <= 0 || c.IncidentAgentToolTimeout <= 0 {
			return fmt.Errorf("incident agent timing configuration is invalid")
		}
		if c.IncidentAgentMaxSteps <= 0 || c.IncidentAgentMaxToolCalls <= 0 || c.IncidentAgentMaxModelCalls <= 0 || c.IncidentAgentTokenBudget <= 0 || c.IncidentAgentMaxEvidenceItems <= 0 || c.IncidentAgentMaxEvidenceBytes < 256 || c.IncidentAgentMaxCheckpointBytes < 1024 || c.IncidentAgentMaxStepRetries < 0 {
			return fmt.Errorf("incident agent budget configuration is invalid")
		}
	}
	if c.FastDemoEnabled {
		if !c.IncidentAgentEnabled || !c.K8SEnabled || !c.K8SWriteEnabled || strings.TrimSpace(c.MySQLHost) == "" {
			return fmt.Errorf("FAST_DEMO_ENABLED requires incident Agent, Kubernetes read/write and MySQL")
		}
		if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(strings.ToLower(strings.TrimSpace(c.FastDemoRevision))) {
			return fmt.Errorf("FAST_DEMO_REVISION must be an exact 40-character commit SHA")
		}
		if strings.TrimSpace(c.FastDemoCluster) == "" || strings.TrimSpace(c.FastDemoNamespace) == "" || strings.TrimSpace(c.FastDemoWorkload) == "" || c.FastDemoRecoveryReplicas < 1 || c.FastDemoRecoveryReplicas > c.ActionMaxReplicas {
			return fmt.Errorf("fast demo target or recovery replica configuration is invalid")
		}
	}
	if c.ChangeIntelligenceEnabled {
		if strings.TrimSpace(c.MySQLHost) == "" {
			return fmt.Errorf("MySQL configuration is required when CHANGE_INTELLIGENCE_ENABLED is true")
		}
		if c.ChangeLookback < time.Minute || c.ChangeLookback > 30*24*time.Hour || c.ChangeMaxCandidates < 1 || c.ChangeMaxCandidates > 50 {
			return fmt.Errorf("change intelligence lookback or candidate limit is invalid")
		}
		var mappings map[string]json.RawMessage
		if json.Unmarshal([]byte(c.ChangeServiceMappingsJSON), &mappings) != nil || len(mappings) == 0 {
			return fmt.Errorf("CHANGE_SERVICE_MAPPINGS_JSON must contain at least one service mapping")
		}
	}
	if c.GitHubEnabled {
		if !c.ChangeIntelligenceEnabled || !strings.HasPrefix(c.GitHubAPIBaseURL, "https://") || len(c.GitHubAllowedOwners) == 0 || len(c.GitHubAllowedRepositories) == 0 {
			return fmt.Errorf("GitHub change integration requires change intelligence, HTTPS and owner/repository allowlists")
		}
		appAuth := c.GitHubAppID > 0 && c.GitHubInstallationID > 0 && strings.TrimSpace(c.GitHubPrivateKeyFile) != ""
		fileAuth := strings.TrimSpace(c.GitHubTokenFile) != ""
		if appAuth == fileAuth {
			return fmt.Errorf("configure exactly one GitHub App or token-file authentication mode")
		}
		if c.GitHubMaxRetries < 0 || c.GitHubMaxRetries > 3 || c.GitHubMaxDiffFiles > 500 || c.GitHubMaxDiffBytes > 2*1024*1024 || c.GitHubTimeout <= 0 {
			return fmt.Errorf("GitHub change integration limits are invalid")
		}
	}
	if c.ArgoCDEnabled {
		if !c.ChangeIntelligenceEnabled || !strings.HasPrefix(c.ArgoCDServer, "https://") || strings.TrimSpace(c.ArgoCDTokenFile) == "" || len(c.ArgoCDAllowedApplications) == 0 || len(c.ArgoCDAllowedProjects) == 0 {
			return fmt.Errorf("argocd change integration requires change intelligence, HTTPS, token file and allowlists")
		}
		if c.ArgoCDTimeout <= 0 || c.ArgoCDMaxResources < 1 || c.ArgoCDMaxResources > 500 || c.ArgoCDMaxDiffBytes < 1024 || c.ArgoCDMaxDiffBytes > 2*1024*1024 {
			return fmt.Errorf("argocd change integration limits are invalid")
		}
	}
	if c.ImageRevisionRequired && len(c.ImageAllowedRegistries) == 0 {
		return fmt.Errorf("IMAGE_ALLOWED_REGISTRIES is required when IMAGE_REVISION_REQUIRED is true")
	}
	if c.RegistryMetadataEnabled {
		if !c.ChangeIntelligenceEnabled || !c.ImageRevisionRequired {
			return fmt.Errorf("REGISTRY_METADATA_ENABLED requires CHANGE_INTELLIGENCE_ENABLED and IMAGE_REVISION_REQUIRED")
		}
		base, baseErr := url.Parse(strings.TrimSpace(c.RegistryBaseURL))
		if baseErr != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") || len(c.RegistryAllowedHosts) == 0 || len(c.RegistryAllowedRepos) == 0 || len(c.OCIAllowedSources) == 0 {
			return fmt.Errorf("registry metadata requires a fixed HTTPS base and host, repository, and OCI source allowlists")
		}
		baseAllowed := false
		for _, host := range c.RegistryAllowedHosts {
			host = strings.TrimSpace(host)
			if host == "" || strings.ContainsAny(host, "*/?#@") {
				return fmt.Errorf("registry host allowlists must contain exact host names")
			}
			baseAllowed = baseAllowed || strings.EqualFold(host, base.Host)
		}
		if !baseAllowed {
			return fmt.Errorf("REGISTRY_BASE_URL host must be explicitly allowlisted")
		}
		for _, host := range append(append([]string{}, c.RegistryAllowedAuthRealms...), c.RegistryAllowedRedirects...) {
			if host = strings.TrimSpace(host); host == "" || strings.ContainsAny(host, "*/?#@") {
				return fmt.Errorf("registry realm and redirect allowlists must contain exact host names")
			}
		}
		for _, repository := range c.RegistryAllowedRepos {
			if repository = strings.Trim(strings.TrimSpace(repository), "/"); repository == "" || strings.Contains(repository, "*") || !strings.Contains(repository, "/") {
				return fmt.Errorf("registry repository allowlist must contain exact namespace/repository names")
			}
		}
		for _, source := range c.OCIAllowedSources {
			parsed, sourceErr := url.Parse(strings.TrimSpace(source))
			if sourceErr != nil || parsed == nil {
				return fmt.Errorf("OCI source repository allowlist must contain exact HTTPS repositories")
			}
			sourcePath := strings.Trim(strings.TrimSuffix(parsed.Path, ".git"), "/")
			if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Contains(parsed.Path, "*") || strings.Count(sourcePath, "/") != 1 {
				return fmt.Errorf("OCI source repository allowlist must contain exact HTTPS repositories")
			}
		}
		basicAny := strings.TrimSpace(c.RegistryUsernameFile) != "" || strings.TrimSpace(c.RegistryPasswordFile) != ""
		basicComplete := strings.TrimSpace(c.RegistryUsernameFile) != "" && strings.TrimSpace(c.RegistryPasswordFile) != ""
		if basicAny && !basicComplete {
			return fmt.Errorf("registry basic authentication requires username and password file references")
		}
		if strings.TrimSpace(c.RegistryBearerTokenFile) != "" && basicAny {
			return fmt.Errorf("registry bearer and basic authentication modes are mutually exclusive")
		}
		if c.RegistryTimeout <= 0 || c.RegistryTimeout > 30*time.Second || c.RegistryMaxRetries < 0 || c.RegistryMaxRetries > 3 || c.RegistryManifestMaxBytes < 1024 || c.RegistryManifestMaxBytes > 4*1024*1024 || c.RegistryConfigMaxBytes < 1024 || c.RegistryConfigMaxBytes > 1024*1024 || c.RegistryCacheTTL <= 0 || c.RegistryCacheTTL > time.Hour || c.RegistryCacheMaxItems < 1 || c.RegistryCacheMaxItems > 2048 {
			return fmt.Errorf("registry metadata limits are invalid")
		}
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
	if c.K8SAPIEnabled && !c.K8SEnabled {
		return fmt.Errorf("K8S_ENABLED must be true when K8S_API_ENABLED is true")
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
	if _, ok := allowed[c.K8SDefaultNamespace]; !ok && !slices.Contains(c.K8SAllowedNamespaces, "*") {
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
	if c.K8sCacheTTL < 5*time.Second || c.K8sCacheTTL > 300*time.Second {
		return fmt.Errorf("K8S_CACHE_TTL_SECONDS must be in range 5-300, got %v", c.K8sCacheTTL)
	}
	if c.K8sListCacheTTL < 5*time.Second || c.K8sListCacheTTL > 300*time.Second {
		return fmt.Errorf("K8S_LIST_CACHE_TTL_SECONDS must be in range 5-300, got %v", c.K8sListCacheTTL)
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
	if c.CopilotChatHistoryMaxRounds <= 0 {
		return fmt.Errorf("COPILOT_CHAT_HISTORY_MAX_ROUNDS must be positive, got %d", c.CopilotChatHistoryMaxRounds)
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
	if c.GlobalMaxBodyBytes < 65536 || c.GlobalMaxBodyBytes > 16777216 {
		return fmt.Errorf("GLOBAL_MAX_BODY_BYTES must be in range 65536-16777216, got %d", c.GlobalMaxBodyBytes)
	}
	if c.GitOpsPREnabled && !c.RemediationEnabled {
		return fmt.Errorf("GITOPS_PR_ENABLED requires REMEDIATION_ENABLED")
	}
	if c.GitHubWriteEnabled && (!c.RemediationEnabled || !c.GitOpsPREnabled) {
		return fmt.Errorf("GITHUB_WRITE_ENABLED requires REMEDIATION_ENABLED and GITOPS_PR_ENABLED")
	}
	if c.RemediationEnabled {
		if !c.ChangeIntelligenceEnabled || !c.RegistryMetadataEnabled || !c.GitOpsPREnabled || !c.GitHubWriteEnabled || strings.TrimSpace(c.MySQLHost) == "" {
			return fmt.Errorf("remediation requires change intelligence, registry metadata, GitOps PR, GitHub write and MySQL")
		}
		if len(c.RemediationAllowedOperations) == 0 || len(c.GitHubWriteAllowedRepositories) == 0 || len(c.GitHubWriteAllowedBaseBranches) == 0 || len(c.GitHubWriteAllowedPaths) == 0 {
			return fmt.Errorf("remediation operation, repository, base branch and path allowlists are required")
		}
		for _, operation := range c.RemediationAllowedOperations {
			if operation != "rollback_image" && operation != "set_replicas" {
				return fmt.Errorf("unsupported REMEDIATION_ALLOWED_OPERATIONS value %q", operation)
			}
		}
		if c.RemediationMaxFiles != 1 || c.RemediationMaxPatchBytes < 256 || c.RemediationMaxPatchBytes > 131072 || (c.RemediationMaxRisk != "low" && c.RemediationMaxRisk != "medium" && c.RemediationMaxRisk != "high") || c.RemediationMinReplicas < 0 || c.RemediationMaxReplicas < c.RemediationMinReplicas {
			return fmt.Errorf("remediation policy limits are invalid")
		}
		var branches map[string]string
		if json.Unmarshal([]byte(c.GitOpsBaseBranchesJSON), &branches) != nil || len(branches) == 0 {
			return fmt.Errorf("GITOPS_BASE_BRANCHES_JSON must contain repository to branch mappings")
		}
		for repository, branch := range branches {
			if !slices.Contains(c.GitHubWriteAllowedRepositories, repository) || !slices.Contains(c.GitHubWriteAllowedBaseBranches, branch) {
				return fmt.Errorf("GitOps base branch mapping is outside write allowlists")
			}
		}
		base, err := url.Parse(strings.TrimSpace(c.GitHubWriteAPIBaseURL))
		if err != nil || base.Scheme != "https" || base.Host == "" {
			return fmt.Errorf("GITHUB_WRITE_API_BASE_URL must be fixed HTTPS")
		}
		appAuth := c.GitHubWriteAppID > 0 && c.GitHubWriteInstallationID > 0 && strings.TrimSpace(c.GitHubWritePrivateKeyFile) != ""
		fileAuth := strings.TrimSpace(c.GitHubWriteTokenFile) != ""
		if appAuth == fileAuth {
			return fmt.Errorf("configure exactly one isolated GitHub write App or token-file authentication mode")
		}
		if (c.GitHubWritePrivateKeyFile != "" && c.GitHubWritePrivateKeyFile == c.GitHubPrivateKeyFile) || (c.GitHubWriteTokenFile != "" && c.GitHubWriteTokenFile == c.GitHubTokenFile) {
			return fmt.Errorf("GitHub write credentials must not reuse Phase 3 read credential files")
		}
		if c.GitHubWriteTimeout <= 0 || c.GitHubWriteMaxResponseBytes < 1024 || c.GitHubWriteMaxResponseBytes > 2*1024*1024 || c.GitHubWriteMaxContentBytes < c.RemediationMaxPatchBytes || strings.TrimSpace(c.RemediationWorkerID) == "" || len(c.RemediationWorkerID) > 128 || c.RemediationPollInterval <= 0 || c.RemediationLeaseDuration <= c.RemediationPollInterval {
			return fmt.Errorf("GitHub write or remediation worker limits are invalid")
		}
	}
	if c.DeliveryTrackingEnabled {
		if !c.RemediationEnabled || !c.GitHubEnabled || !c.ArgoCDEnabled || !c.K8SEnabled || strings.TrimSpace(c.MySQLHost) == "" {
			return fmt.Errorf("delivery tracking requires remediation, GitHub read, Argo CD read, Kubernetes read and MySQL")
		}
	}
	if c.VerificationEnabled && !c.DeliveryTrackingEnabled {
		return fmt.Errorf("VERIFICATION_ENABLED requires DELIVERY_TRACKING_ENABLED")
	}
	var profiles verification.Profiles
	decoder := json.NewDecoder(strings.NewReader(c.VerificationProfilesJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profiles); err != nil || profiles.Validate() != nil {
		return fmt.Errorf("VERIFICATION_PROFILES_JSON is invalid")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("VERIFICATION_PROFILES_JSON is invalid")
	}
	if len(profiles.Items) > 0 && !c.VerificationEnabled {
		return fmt.Errorf("non-empty VERIFICATION_PROFILES_JSON requires VERIFICATION_ENABLED")
	}
	if c.DeliveryTrackingEnabled || c.VerificationEnabled {
		if strings.TrimSpace(c.DeliveryWorkerID) == "" || len(c.DeliveryWorkerID) > 128 || strings.TrimSpace(c.VerificationWorkerID) == "" || len(c.VerificationWorkerID) > 128 {
			return fmt.Errorf("delivery and verification worker IDs must contain 1-128 bytes")
		}
		if c.DeliveryPollInterval < time.Second || c.DeliveryPollInterval > time.Minute || c.DeliveryTimeout < time.Minute || c.DeliveryTimeout > 24*time.Hour || c.VerificationTimeout < time.Minute || c.VerificationTimeout > 24*time.Hour || c.VerificationStabilityWindow < c.DeliveryPollInterval || c.VerificationStabilityWindow > c.VerificationTimeout || c.VerificationLeaseDuration <= c.DeliveryPollInterval || c.VerificationMaxAttempts < 1 || c.VerificationMaxAttempts > 10000 {
			return fmt.Errorf("delivery and verification timing configuration is invalid")
		}
		if len(profiles.Items) > 0 {
			for name, raw := range map[string]string{"OBSERVABILITY_PROMETHEUS_URL": c.ObservabilityPrometheusURL, "OBSERVABILITY_LOKI_URL": c.ObservabilityLokiURL, "OBSERVABILITY_TEMPO_URL": c.ObservabilityTempoURL} {
				u, err := url.Parse(strings.TrimSpace(raw))
				if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" {
					return fmt.Errorf("%s must be fixed HTTPS", name)
				}
			}
			if c.ObservabilityRequestTimeout < time.Second || c.ObservabilityRequestTimeout > time.Minute || c.ObservabilityMaxLookback < time.Minute || c.ObservabilityMaxLookback > 24*time.Hour || c.ObservabilityMaxResponseBytes < 1024 || c.ObservabilityMaxResponseBytes > 1024*1024 || c.ObservabilityMaxSamples < 1 || c.ObservabilityMaxSamples > 10000 || c.ObservabilityMaxSeries < 1 || c.ObservabilityMaxSeries > 100 || c.ObservabilityMaxTraces < 1 || c.ObservabilityMaxTraces > 1000 || c.ObservabilityMaxRetries < 0 || c.ObservabilityMaxRetries > 2 {
				return fmt.Errorf("observability verification limits are invalid")
			}
		}
	}
	return nil
}

var k8sNamespacePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

func checkK8SNamespace(name, namespace string) error {
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if namespace == "*" {
		return nil
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

func parseK8SClusters(raw string) []K8SClusterConfig {
	if raw == "" {
		return nil
	}
	var clusters []K8SClusterConfig
	if err := json.Unmarshal([]byte(raw), &clusters); err != nil {
		return nil
	}
	return clusters
}
