import type {
  IncidentListDirection,
  IncidentListQuery,
  IncidentListSort,
  IncidentSeverity,
  IncidentStatus,
  LoadState,
  ResourceView,
} from "../types/incidents";

export const incidentStatuses: IncidentStatus[] = [
  "detected",
  "investigating",
  "awaiting_approval",
  "delivering",
  "verifying",
  "resolved",
  "closed",
];

export function incidentStatusLabel(value: string): string {
  const labels: Record<string, string> = {
    detected: "已发现",
    investigating: "调查中",
    awaiting_approval: "等待决策",
    delivering: "执行中",
    verifying: "恢复验证中",
    resolved: "已恢复",
    closed: "已关闭",
  };
  return labels[value] ?? (value.replace(/_/g, " ") || "未知");
}

export function severityLabel(value: IncidentSeverity): string {
  return ({ critical: "严重", warning: "警告", info: "信息", unknown: "未知" })[value];
}

export function humanizeCode(value?: string): string {
  const normalized = value?.trim();
  if (!normalized) return "无阻塞原因";
  const labels: Record<string, string> = {
    pending: "等待执行",
    running: "执行中",
    passed: "已通过",
    failed: "未通过",
    timed_out: "已超时",
    inconclusive: "无明确结论",
    cancelled: "已取消",
    unavailable: "暂不可用",
    completed: "已完成",
    approved: "已批准",
    rejected: "已拒绝",
    valid: "有效",
    invalid: "无效",
    firing: "触发中",
    resolved: "已恢复",
    closed: "已关闭",
  };
  if (labels[normalized]) return labels[normalized];
  return normalized
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

export type StatusTone = "success" | "warning" | "danger" | "info" | "primary" | "inconclusive" | "neutral";

export function statusTone(value: string): StatusTone {
  const normalized = value.toLowerCase();
  if (["resolved", "passed", "completed", "approved", "delivered", "valid", "available"].includes(normalized)) return "success";
  if (["detected", "failed", "invalid", "rejected", "policy_rejected"].includes(normalized)) return "danger";
  if (["awaiting_approval", "pending", "timed_out", "partial", "unavailable"].includes(normalized)) return "warning";
  if (["inconclusive", "no_data"].includes(normalized)) return "inconclusive";
  if (["investigating", "running", "delivering", "verifying"].includes(normalized)) return "primary";
  if (["unknown", "cancelled", "superseded", "closed", "not_run", "not run"].includes(normalized)) return "neutral";
  return "primary";
}

export function normalizeListQuery(query: Record<string, unknown>): IncidentListQuery {
  const result: IncidentListQuery = { limit: Math.min(positiveInt(query.limit, 50), 100) };
  const status = normalizedString(query.status);
  const severity = normalizedString(query.severity);
  const service = normalizedString(query.service);
  const resource = normalizedString(query.resource);
  const alert = normalizedString(query.alert);
  const from = normalizedString(query.from);
  const to = normalizedString(query.to);
  const attention = normalizedString(query.attention);
  const cursor = normalizedString(query.cursor);
  const sort = normalizedString(query.sort);
  const direction = normalizedString(query.direction);
  const selected = normalizedString(query.selected);
  if (status && incidentStatuses.includes(status as IncidentStatus)) result.status = status as IncidentStatus;
  if (severity && ["critical", "warning", "info", "unknown"].includes(severity)) result.severity = severity as IncidentSeverity;
  if (service) result.service = service.slice(0, 255);
  if (resource) result.resource = resource.slice(0, 512);
  if (isPublicID(alert)) result.alert = alert;
  if (isRFC3339(from)) result.from = from;
  if (isRFC3339(to)) result.to = to;
  if (attention === "true" || attention === "false") result.attention = attention === "true";
  if (cursor && cursor.length <= 512) result.cursor = cursor;
  if (sort === "severity" || sort === "status" || sort === "updated") result.sort = sort as IncidentListSort;
  if (direction === "asc" || direction === "desc") result.direction = direction as IncidentListDirection;
  if (isPublicID(selected)) result.selected = selected;
  return result;
}

export function serializeListQuery(query: IncidentListQuery): Record<string, string> {
  const result: Record<string, string> = {};
  if (query.status) result.status = query.status;
  if (query.severity) result.severity = query.severity;
  if (query.service) result.service = query.service;
  if (query.attention !== undefined) result.attention = String(query.attention);
  if (query.resource) result.resource = query.resource;
  if (query.alert) result.alert = query.alert;
  if (query.from) result.from = query.from;
  if (query.to) result.to = query.to;
  if (query.cursor) result.cursor = query.cursor;
  if (query.limit && query.limit !== 50) result.limit = String(query.limit);
  if (query.sort && query.sort !== "updated") result.sort = query.sort;
  if (query.direction && query.direction !== "desc") result.direction = query.direction;
  if (query.selected) result.selected = query.selected;
  return result;
}

export function toIncidentListAPIQuery(query: IncidentListQuery): IncidentListQuery {
  const result: IncidentListQuery = {};
  if (query.status) result.status = query.status;
  if (query.severity) result.severity = query.severity;
  if (query.service) result.service = query.service;
  if (query.attention !== undefined) result.attention = query.attention;
  if (query.resource) result.resource = query.resource;
  if (query.alert) result.alert = query.alert;
  if (query.from) result.from = query.from;
  if (query.to) result.to = query.to;
  if (query.limit) result.limit = query.limit;
  if (query.cursor) result.cursor = query.cursor;
  return result;
}

export type IncidentInspectorFailureKind =
  | "ready"
  | "invalid"
  | "deleted"
  | "permission-denied"
  | "expired"
  | "error";

export function incidentInspectorFailureKind(
  incidentID: string,
  status: number | null = null,
  code = "",
): IncidentInspectorFailureKind {
  if (!isPublicID(incidentID)) return "invalid";
  if (status === null && !code) return "ready";
  if (status === 401 || status === 403) return "permission-denied";
  if (status === 404) return "deleted";
  if (status === 410 || code.toUpperCase().includes("EXPIRED")) return "expired";
  return "error";
}

export function loadStateForStatus(status: number | null, fallback: LoadState = "error"): LoadState {
  if (status === 403) return "forbidden";
  if (status === 404) return "not_found";
  if (status === 503) return "unavailable";
  return fallback;
}

export function incidentDetailPath(publicID: string): string {
  return `/incidents/${encodeURIComponent(publicID)}`;
}

export function isCurrentRequest(identity: number, currentIdentity: number): boolean {
  return identity === currentIdentity;
}

export function resourceTimestamp(item: ResourceView): string {
  return item.updated_at || item.created_at || "";
}

function normalizedString(value: unknown): string {
  const raw = Array.isArray(value) ? value[0] : value;
  return typeof raw === "string" ? raw.trim() : "";
}

function positiveInt(value: unknown, fallback: number): number {
  const raw = Array.isArray(value) ? value[0] : value;
  const parsed = typeof raw === "string" || typeof raw === "number" ? Number(raw) : Number.NaN;
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}

function isPublicID(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value);
}

function isRFC3339(value: string): boolean {
  return /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:\d{2})$/.test(value)
    && Number.isFinite(Date.parse(value));
}
