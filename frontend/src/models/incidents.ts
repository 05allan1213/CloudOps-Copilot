import type {
  IncidentListQuery,
  IncidentSeverity,
  IncidentStatus,
  IncidentTimelineDTO,
  LoadState,
} from "../types/incidents";

export const incidentStatuses: IncidentStatus[] = [
  "DETECTED",
  "CORRELATING",
  "DIAGNOSING",
  "DIAGNOSIS_COMPLETED",
  "PLANNING_REMEDIATION",
  "AWAITING_APPROVAL",
  "APPLYING_CHANGE",
  "VERIFYING",
  "RESOLVED",
  "FAILED",
  "CLOSED_NO_ACTION",
];

export function incidentStatusLabel(value: string): string {
  const labels: Record<string, string> = {
    DETECTED: "Detected",
    CORRELATING: "Correlating",
    DIAGNOSING: "Investigating",
    DIAGNOSIS_COMPLETED: "Investigation complete",
    PLANNING_REMEDIATION: "Planning remediation",
    AWAITING_APPROVAL: "Awaiting approval",
    APPLYING_CHANGE: "Delivering change",
    VERIFYING: "Verifying recovery",
    RESOLVED: "Resolved",
    FAILED: "Failed",
    CLOSED_NO_ACTION: "Closed without action",
  };
  return labels[value] ?? "Unknown";
}

export function severityLabel(value: IncidentSeverity): string {
  return ({ critical: "Critical", warning: "Warning", info: "Info", unknown: "Unknown" })[value];
}

export function statusTone(value: string): "success" | "warning" | "danger" | "info" | "primary" {
  if (value === "RESOLVED" || value === "passed" || value === "completed" || value === "available" || value === "generated") return "success";
  if (value === "FAILED" || value === "failed" || value === "timed_out" || value === "conflict" || value === "malformed") return "danger";
  if (value === "AWAITING_APPROVAL" || value === "pending" || value === "running" || value === "unavailable" || value === "partial") return "warning";
  if (value === "unknown" || value === "not_started" || value === "not_generated" || value === "restricted" || value === "no_data") return "info";
  return "primary";
}

export function normalizeListQuery(query: Record<string, unknown>): IncidentListQuery {
  const result: IncidentListQuery = { page: positiveInt(query.page, 1), page_size: Math.min(positiveInt(query.page_size, 20), 50) };
  const stringKeys: Array<keyof IncidentListQuery> = ["status", "severity", "service", "environment", "namespace", "workload", "created_from", "created_to", "q"];
  for (const key of stringKeys) {
    const raw = query[key];
    const value = Array.isArray(raw) ? raw[0] : raw;
    if (typeof value === "string" && value.trim()) {
      (result as Record<string, string | number>)[key] = value.trim();
    }
  }
  return result;
}

export function serializeListQuery(query: IncidentListQuery): Record<string, string> {
  const result: Record<string, string> = {};
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "" && !(key === "page" && value === 1) && !(key === "page_size" && value === 20)) {
      result[key] = String(value);
    }
  }
  return result;
}

export function sortTimeline(items: IncidentTimelineDTO[]): IncidentTimelineDTO[] {
  return [...items].sort((left, right) => {
    const timeDelta = Date.parse(left.occurred_at) - Date.parse(right.occurred_at);
    return timeDelta !== 0 ? timeDelta : left.key.localeCompare(right.key);
  });
}

export function loadStateForStatus(status: number | null, fallback: LoadState = "error"): LoadState {
  if (status === 403) return "forbidden";
  if (status === 404) return "not_found";
  if (status === 503) return "unavailable";
  return fallback;
}

export function postmortemStateForStatus(status: number | null): LoadState {
  return status === 404 ? "not_generated" : loadStateForStatus(status);
}

export function factTone(classification: string): "fact" | "inference" | "unknown" {
  return classification === "fact" || classification === "inference" ? classification : "unknown";
}

export function verificationRequirementLabel(required: boolean): "Required" | "Optional" {
  return required ? "Required" : "Optional";
}

export function incidentDetailPath(publicID: string): string {
  return `/incidents/${encodeURIComponent(publicID)}`;
}

export function isCurrentRequest(identity: number, currentIdentity: number): boolean {
  return identity === currentIdentity;
}

function positiveInt(value: unknown, fallback: number): number {
  const raw = Array.isArray(value) ? value[0] : value;
  const parsed = typeof raw === "string" || typeof raw === "number" ? Number(raw) : Number.NaN;
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}
