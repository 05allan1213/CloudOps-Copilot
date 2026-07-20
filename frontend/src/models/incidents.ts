import type {
  IncidentListQuery,
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
    detected: "Detected",
    investigating: "Investigating",
    awaiting_approval: "Awaiting approval",
    delivering: "Delivering",
    verifying: "Verifying recovery",
    resolved: "Resolved",
    closed: "Closed",
  };
  return labels[value] ?? (value.replace(/_/g, " ") || "Unknown");
}

export function severityLabel(value: IncidentSeverity): string {
  return ({ critical: "Critical", warning: "Warning", info: "Info", unknown: "Unknown" })[value];
}

export function statusTone(value: string): "success" | "warning" | "danger" | "info" | "primary" {
  const normalized = value.toLowerCase();
  if (["resolved", "passed", "completed", "approved", "delivered", "valid"].includes(normalized)) return "success";
  if (["failed", "timed_out", "invalid", "rejected", "policy_rejected"].includes(normalized)) return "danger";
  if (["awaiting_approval", "pending", "running", "delivering", "verifying", "inconclusive", "unavailable"].includes(normalized)) return "warning";
  if (["unknown", "cancelled", "superseded", "closed"].includes(normalized)) return "info";
  return "primary";
}

export function normalizeListQuery(query: Record<string, unknown>): IncidentListQuery {
  const result: IncidentListQuery = { limit: Math.min(positiveInt(query.limit, 50), 100) };
  const status = normalizedString(query.status);
  const severity = normalizedString(query.severity);
  const service = normalizedString(query.service);
  if (status && incidentStatuses.includes(status as IncidentStatus)) result.status = status as IncidentStatus;
  if (severity && ["critical", "warning", "info", "unknown"].includes(severity)) result.severity = severity as IncidentSeverity;
  if (service) result.service = service.slice(0, 255);
  return result;
}

export function serializeListQuery(query: IncidentListQuery): Record<string, string> {
  const result: Record<string, string> = {};
  if (query.status) result.status = query.status;
  if (query.severity) result.severity = query.severity;
  if (query.service) result.service = query.service;
  if (query.limit && query.limit !== 50) result.limit = String(query.limit);
  return result;
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
