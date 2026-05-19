export interface AlertRecord {
  status: "firing" | "resolved";
  fingerprint: string;
  labels: Record<string, string>;
  annotations: Record<string, string>;
  startsAt: string;
  endsAt: string;
  generatorURL?: string;
  diagnosisStatus?: DiagnosisUpdate["status"];
  diagnosisReportId?: number;
  diagnosisError?: string;
}

export interface AlertEvent {
  status: "firing" | "resolved";
  fingerprint: string;
  labels: Record<string, string>;
  annotations: Record<string, string>;
  startsAt: string;
  endsAt: string;
  generatorURL?: string;
  receivedAt: string;
}

export interface Host {
  instance: string;
  cpu: number;
  memory: number;
  status: string;
  lastScrape: string;
}

export interface AuthUser {
  id: number;
  username: string;
  role: "admin" | "viewer" | string;
}

export interface LoginResponse {
  token: string;
  expires_at: string;
  user: AuthUser;
}

export interface HostGroupMember {
  id?: number;
  group_id?: number;
  instance: string;
  created_at?: string;
}

export interface HostGroup {
  id: number;
  name: string;
  description: string;
  member_count: number;
  members?: HostGroupMember[];
  created_at: string;
  updated_at: string;
}

export interface AlertRule {
  id: number;
  name: string;
  expr: string;
  duration: string;
  severity: "critical" | "warning" | "info" | string;
  summary: string;
  description: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface AlertRuleSyncResult {
  success: boolean;
  rule_count: number;
  file_path?: string;
  synced_at?: string;
  reload_url?: string;
  promtool?: string;
  error?: string;
  restored?: boolean;
  reloaded: boolean;
  validated: boolean;
  rendered_to?: string;
}

export interface NotificationChannel {
  id: number;
  name: string;
  type: "webhook" | string;
  url: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface NotificationChannelTestResult {
  success: boolean;
  latency_ms?: number;
  status_code?: number;
  error?: string;
}

export interface AlertHistory {
  id: number;
  fingerprint: string;
  alert_name: string;
  instance: string;
  severity: "critical" | "warning" | "info" | string;
  status: "firing" | "resolved" | string;
  summary: string;
  labels_json: string;
  fired_at: string;
  resolved_at?: string;
  created_at: string;
}

export interface AlertHistoryListResponse {
  items: AlertHistory[];
  total: number;
  page: number;
  page_size: number;
}

export interface RangePoint {
  timestamp: string;
  value: number;
}

export interface RangeSeries {
  metric: Record<string, string>;
  values: RangePoint[];
}

export interface HostMetricsResponse {
  instance: string;
  range: string;
  stepSeconds: number;
  metrics: Record<string, RangeSeries[]>;
}

export interface CopilotToolCall {
  name: string;
  status: "success" | "error" | string;
  error?: string;
  result?: unknown;
}

export interface DiagnosisRequest {
  fingerprint?: string;
  alert_history_id?: number;
  alert_name?: string;
  instance?: string;
  target_kind?: string;
  target_name?: string;
  trigger_type?: "manual" | "chat" | "auto" | string;
}

export interface DiagnosisUpdate {
  fingerprint: string;
  alert_name: string;
  instance: string;
  status: "pending" | "running" | "completed" | "failed" | "skipped" | string;
  trigger_type: "auto" | string;
  report_id?: number;
  summary?: string;
  error?: string;
  updated_at: string;
}

export interface DiagnosisMetricEvidence {
  name: string;
  source: string;
  window: string;
  avg: number;
  max: number;
  last: number;
  trend: string;
  collected_at?: string;
}

export interface K8sPodSummary {
  namespace: string;
  name: string;
  phase: string;
  ready_containers: number;
  total_containers: number;
  restart_count: number;
  node_name?: string;
  pod_ip?: string;
  owner_kind?: string;
  owner_name?: string;
  labels?: Record<string, string>;
  start_time?: string;
  collected_at?: string;
}

export interface K8sDeploymentSummary {
  namespace: string;
  name: string;
  selector?: Record<string, string>;
  replicas: number;
  ready_replicas: number;
  updated_replicas: number;
  available_replicas: number;
  strategy?: string;
  collected_at?: string;
}

export interface K8sServiceSummary {
  namespace: string;
  name: string;
  selector?: Record<string, string>;
  type: string;
  cluster_ip?: string;
  ports?: Array<{ name?: string; protocol: string; port: number; target_port?: string }>;
  collected_at?: string;
}

export interface K8sIngressPath {
  host: string;
  path: string;
  path_type: string;
  backend: string;
}

export interface K8sIngressTLS {
  hosts: string[];
  secret_name: string;
}

export interface K8sIngressSummary {
  namespace: string;
  name: string;
  hosts: string[];
  paths: K8sIngressPath[];
  tls: K8sIngressTLS[];
  age: number;
  collected_at?: string;
}

export interface K8sNodeSummary {
  name: string;
  ready: boolean;
  roles?: string[];
  kubelet_version?: string;
  capacity?: { cpu?: string; memory?: string };
  collected_at?: string;
}

export interface K8sEventSummary {
  namespace?: string;
  name: string;
  type?: string;
  reason?: string;
  message?: string;
  involved_kind?: string;
  involved_name?: string;
  count?: number;
  last_seen?: string;
  collected_at?: string;
}

export interface K8sLogSnippet {
  namespace: string;
  pod_name: string;
  container?: string;
  lines: string[];
  truncated: boolean;
  collected_at?: string;
}

export interface K8sConfigMapSummary {
  namespace: string;
  name: string;
  data_keys: string[];
  data: Record<string, string>;
  age: number;
  collected_at?: string;
}

export interface K8sPVSummary {
  name: string;
  capacity: string;
  access_modes: string[];
  status: string;
  claim_ref?: string;
  storage_class?: string;
  age: string;
  collected_at?: string;
}

export interface K8sPVCSummary {
  namespace: string;
  name: string;
  storage_class?: string;
  volume_name?: string;
  access_modes: string[];
  status: string;
  age: string;
  collected_at?: string;
}

export interface K8sResourceQuantity {
  value: string;
}

export interface K8sResourceQuotaSummary {
  namespace: string;
  name: string;
  hard: Record<string, K8sResourceQuantity>;
  used: Record<string, K8sResourceQuantity>;
  age: number;
  collected_at?: string;
}

export interface K8sLimitRangeItem {
  type: string;
  min?: string;
  max?: string;
  default?: string;
}

export interface K8sLimitRangeSummary {
  namespace: string;
  name: string;
  limits: K8sLimitRangeItem[];
  age: number;
  collected_at?: string;
}

export interface K8sHPASummary {
  namespace: string;
  name: string;
  reference: string;
  min_replicas: number;
  max_replicas: number;
  current_replicas: number;
  target_utilization: string;
  age: number;
  collected_at?: string;
}

export interface K8sDaemonSetSummary {
  namespace: string;
  name: string;
  desired: number;
  current: number;
  ready: number;
  updated: number;
  node_selector: string;
  age: number;
  collected_at?: string;
}

export interface K8sStatefulSetSummary {
  namespace: string;
  name: string;
  replicas_ready: number;
  replicas_desired: number;
  service_name: string;
  age: number;
  collected_at?: string;
}

export interface K8sJobSummary {
  namespace: string;
  name: string;
  completions: string;
  duration: string;
  status: string;
  age: number;
  collected_at?: string;
}

export interface K8sTopologyNode {
  id: string;
  kind: string;
  name: string;
  namespace?: string;
  status?: string;
  detail_path?: string;
}

export interface K8sTopologyEdge {
  source: string;
  target: string;
  type: string;
}

export interface K8sTopologyData {
  nodes: K8sTopologyNode[];
  edges: K8sTopologyEdge[];
}

export interface K8sEvidence {
  enabled: boolean;
  namespace?: string;
  target_kind?: string;
  target_name?: string;
  pods?: K8sPodSummary[];
  deployments?: K8sDeploymentSummary[];
  services?: K8sServiceSummary[];
  nodes?: K8sNodeSummary[];
  events?: K8sEventSummary[];
  logs?: K8sLogSnippet[];
  errors?: Array<{ source: string; error: string }>;
  collected_at?: string;
}

export interface DiagnosisRuleResult {
  rule: string;
  passed: boolean;
  detail: string;
  evidence_refs: string[];
}

export interface DiagnosisRuleAnalysis {
  summary: string;
  confidence: number;
  confidence_level: string;
  results: DiagnosisRuleResult[];
  next_steps: string[];
}

export interface DiagnosisRecommendedAction {
  type: string;
  description: string;
  risk: string;
  requires_approval: boolean;
}

export type ActionStatus =
  | "pending"
  | "approved"
  | "rejected"
  | "executing"
  | "executed"
  | "failed"
  | "cancelled"
  | string;

export type RiskLevel = "low" | "medium" | "high" | string;

export interface PendingAction {
  id: number;
  diagnosis_report_id: number;
  action_type: string;
  target_kind: string;
  target_name: string;
  namespace: string;
  params?: Record<string, unknown>;
  risk_level: RiskLevel;
  status: ActionStatus;
  requested_by: string;
  approved_by?: number;
  executed_by?: number;
  result?: Record<string, unknown>;
  error_message?: string;
  created_at: string;
  approved_at?: string;
  executed_at?: string;
  updated_at: string;
}

export interface ActionUpdate {
  action_id: number;
  diagnosis_report_id?: number;
  action_type?: string;
  target?: string;
  target_name?: string;
  risk_level?: string;
  requested_by?: string;
  status?: ActionStatus;
  result?: string;
  updated_at?: string;
}

export interface PendingActionListResponse {
  items: PendingAction[];
  total: number;
  page: number;
  page_size: number;
}

export interface CreatePendingActionResult {
  created: PendingAction[];
  skipped: Array<{ action_type: string; reason: string }>;
}

export interface AuditLog {
  id: number;
  actor: string;
  actor_role: string;
  action: string;
  resource_type: string;
  resource_id: string;
  request?: Record<string, unknown>;
  result: "success" | "failure" | "denied" | "timeout" | string;
  error_message?: string;
  trace_id?: string;
  created_at: string;
}

export interface AuditLogListResponse {
  items: AuditLog[];
  total: number;
  page: number;
  page_size: number;
}

export interface RunbookEvidence {
  title: string;
  file: string;
  score: number;
  matched_alerts?: string[];
  matched_keywords?: string[];
  matched_metrics?: string[];
  snippet: string;
  source?: string;
  collected_at?: string;
}

export interface DiagnosisEvidence {
  alert_context?: Record<string, unknown>;
  active_alerts?: Record<string, unknown>[];
  metrics?: DiagnosisMetricEvidence[];
  history?: Record<string, unknown>[];
  runbooks?: RunbookEvidence[];
  k8s?: K8sEvidence;
  collection_errors?: Array<{ source: string; error: string }>;
  collected_at?: string;
}

export interface DiagnosisReport {
  id: number;
  alert_history_id: number;
  fingerprint: string;
  alert_name: string;
  target_kind: string;
  target_name: string;
  namespace: string;
  severity: "critical" | "warning" | "info" | string;
  status: "pending" | "running" | "completed" | "failed" | string;
  summary: string;
  root_cause: string;
  evidence?: DiagnosisEvidence;
  runbooks?: RunbookEvidence[];
  recommended_actions?: DiagnosisRecommendedAction[];
  rule_analysis?: DiagnosisRuleAnalysis;
  confidence: number;
  confidence_level: string;
  llm_prompt_hash: string;
  llm_model: string;
  trigger_type: string;
  created_by: number;
  created_at: string;
  updated_at: string;
  my_feedback?: { rating: string; comment?: string };
}

export interface DiagnosisFeedback {
  id: number;
  diagnosis_id: number;
  rating: "useful" | "not_useful";
  comment?: string;
  created_by: number;
  created_at: string;
}

export interface DiagnosisListResponse {
  items: DiagnosisReport[];
  total: number;
  page: number;
  page_size: number;
}

export interface CopilotChatRequest {
  message: string;
  session_id?: string;
}

export interface CopilotChatResponse {
  session_id: string;
  reply: string;
  intent: string;
  confidence: number;
  tool_calls: CopilotToolCall[];
  suggestions: CopilotSuggestion[];
}

export interface CopilotSuggestion {
  text: string;
  action?: string;
  intent?: string;
  params?: Record<string, string>;
}

export interface CopilotSession {
  id: string;
  title: string;
  updated_at: string;
}

export interface CopilotMessage {
  role: "user" | "assistant" | string;
  content: string;
  created_at: string;
}

export interface CopilotLocalMessage extends CopilotMessage {
  intent?: string;
  confidence?: number;
  tool_calls?: CopilotToolCall[];
  suggestions?: CopilotSuggestion[];
}

export interface DashboardOverview {
  total_hosts: number;
  healthy_hosts: number;
  down_hosts: number;
  active_alerts: number;
  avg_cpu: number;
  avg_memory: number;
  generated_at: string;
  alert_degraded?: boolean;
  k8s_api_enabled?: boolean;
  k8s_nodes_enabled?: boolean;
  copilot_enabled?: boolean;
}

export interface DependencyStatus {
  prometheus?: string;
  redis?: string;
}

export interface HealthStatus {
  healthy?: boolean;
}

export interface ReadyStatus {
  ready?: boolean;
  dependencies?: DependencyStatus;
}

export interface ApiResponse<T> {
  status: string;
  data?: T;
  error?: string;
}
