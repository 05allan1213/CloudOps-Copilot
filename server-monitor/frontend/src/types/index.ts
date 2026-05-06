export interface AlertRecord {
  status: "firing" | "resolved";
  fingerprint: string;
  labels: Record<string, string>;
  annotations: Record<string, string>;
  startsAt: string;
  endsAt: string;
  generatorURL?: string;
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

export interface DashboardOverview {
  total_hosts: number;
  healthy_hosts: number;
  down_hosts: number;
  active_alerts: number;
  avg_cpu: number;
  avg_memory: number;
  generated_at: string;
  alert_degraded?: boolean;
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
