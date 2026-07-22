import type { IncidentStatus, RemediationPlanView, ResolutionReportView, VerificationRunView } from "../types/incidents";

export interface PlanDecisionAvailability {
  available: boolean;
  expired: boolean;
  stale: boolean;
  reason: string;
}

export function planDecisionAvailability(
  plan: RemediationPlanView,
  options: {
    isOperator: boolean;
    incidentVersion: number;
    incidentStatus: IncidentStatus;
    nowMs?: number;
    conflict?: boolean;
  },
): PlanDecisionAvailability {
  const expiry = Date.parse(plan.expires_at);
  const expired = !Number.isFinite(expiry) || expiry <= (options.nowMs ?? Date.now());
  const stale = plan.incident_version + 1 !== options.incidentVersion;
  const conflict = options.conflict ?? false;
  let reason = "";
  if (!options.isOperator) reason = "Viewer access is read-only. An operator must submit the exact decision.";
  else if (plan.decision) reason = "This immutable Plan already has a persisted Decision.";
  else if (conflict) reason = "The server rejected the previous payload as stale or conflicting. Refresh this Plan before deciding again.";
  else if (expired) reason = "This Plan has expired or has no valid expiry. Refresh for a current server-generated Plan.";
  else if (stale) reason = "This Plan is bound to an older Incident version. Refresh before deciding.";
  else if (options.incidentStatus !== "awaiting_approval") reason = `The Incident is ${options.incidentStatus.replace(/_/g, " ")} and is not awaiting approval.`;
  else if (plan.status !== "awaiting_approval") reason = `This Plan is ${plan.status.replace(/_/g, " ")} and is not awaiting a Decision.`;
  return {
    available: options.isOperator
      && !plan.decision
      && !conflict
      && !expired
      && !stale
      && options.incidentStatus === "awaiting_approval"
      && plan.status === "awaiting_approval",
    expired,
    stale,
    reason,
  };
}

export type VerificationDisplayState =
  | "not_run"
  | "pending"
  | "running"
  | "passed"
  | "failed"
  | "timed_out"
  | "inconclusive"
  | "cancelled"
  | "unavailable";

export function verificationDisplayState(status?: string): VerificationDisplayState {
  switch (status) {
    case "pending":
    case "running":
    case "passed":
    case "failed":
    case "timed_out":
    case "inconclusive":
    case "cancelled":
      return status;
    case "unavailable":
      return "unavailable";
    default:
      return "not_run";
  }
}

export function verificationStateLabel(status?: string): string {
  const labels: Record<VerificationDisplayState, string> = {
    not_run: "NOT RUN",
    pending: "Pending",
    running: "Running",
    passed: "Passed",
    failed: "Failed",
    timed_out: "Timed out",
    inconclusive: "Inconclusive",
    cancelled: "Cancelled",
    unavailable: "Unavailable",
  };
  return labels[verificationDisplayState(status)];
}

export function latestVerificationRun(runs: VerificationRunView[]): VerificationRunView | null {
  return [...runs].sort((left, right) => {
    if (right.attempt !== left.attempt) return right.attempt - left.attempt;
    return Date.parse(right.updated_at || right.created_at) - Date.parse(left.updated_at || left.created_at);
  })[0] ?? null;
}

export function canExposeResolutionReport(runs: VerificationRunView[]): boolean {
  return latestVerificationRun(runs)?.status === "passed";
}

export interface StabilityProgress {
  elapsedMs: number;
  requiredMs: number;
  percent: number;
  label: string;
  source: "persisted_success_since" | "persisted_completion" | "not_projected";
}

export function stabilityProgress(run: VerificationRunView, nowMs = Date.now()): StabilityProgress {
  const requiredMs = Math.max(0, run.common_window.stability_window_ms);
  const completedAt = Date.parse(run.common_window.completed_at || "");
  const successSince = Date.parse(run.common_window.success_since || "");
  let elapsedMs = 0;
  let source: StabilityProgress["source"] = "not_projected";
  if (Number.isFinite(completedAt)) {
    elapsedMs = requiredMs;
    source = "persisted_completion";
  } else if (Number.isFinite(successSince)) {
    elapsedMs = Math.max(0, Math.min(requiredMs, nowMs - successSince));
    source = "persisted_success_since";
  }
  const percent = requiredMs === 0 ? 0 : Math.round((elapsedMs / requiredMs) * 100);
  return {
    elapsedMs,
    requiredMs,
    percent,
    label: `${Math.round(elapsedMs / 1000)}s / ${Math.round(requiredMs / 1000)}s required`,
    source,
  };
}

export function resolutionReportIsConsistent(
  runs: VerificationRunView[],
  report: ResolutionReportView | null,
): boolean {
  return Boolean(report) && canExposeResolutionReport(runs);
}
