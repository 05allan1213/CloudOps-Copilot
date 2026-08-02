import type { TelemetryQueryMode } from "../../api/telemetry";

const allowedLimits = new Set([1, 50, 100, 200]);
const allowedStatuses = new Set(["error", "ok"]);

export interface TracesRouteState {
  cluster: string;
  namespace: string;
  resource: string;
  legacyWorkload: string;
  mode: TelemetryQueryMode;
  service: string;
  operation: string;
  status: string;
  minDurationMS?: number;
  maxDurationMS?: number;
  limit: number;
  searchID: string;
  traceID: string;
  evidenceQueryID: string;
  from: string;
  to: string;
}

function routeString(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === "string" ? value[0] : "";
  return typeof value === "string" ? value : "";
}

function routeDuration(value: unknown): number | undefined {
  const text = routeString(value);
  if (!text) return undefined;
  const parsed = Number(text);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined;
}

export function parseTracesRoute(query: Record<string, unknown>): TracesRouteState {
  const requestedLimit = Number(routeString(query.limit));
  const requestedStatus = routeString(query.status);
  return {
    cluster: routeString(query.cluster),
    namespace: routeString(query.namespace),
    resource: routeString(query.resource),
    legacyWorkload: routeString(query.workload),
    mode: routeString(query.mode) === "expert" ? "expert" : "guided",
    service: routeString(query.service),
    operation: routeString(query.operation),
    status: allowedStatuses.has(requestedStatus) ? requestedStatus : "",
    minDurationMS: routeDuration(query.min_duration_ms),
    maxDurationMS: routeDuration(query.max_duration_ms),
    limit: allowedLimits.has(requestedLimit) ? requestedLimit : 100,
    searchID: routeString(query.search),
    traceID: routeString(query.trace_id),
    evidenceQueryID: routeString(query.evidence_query),
    from: routeString(query.from),
    to: routeString(query.to),
  };
}

export function buildTracesRouteQuery(state: Omit<TracesRouteState, "legacyWorkload">): Record<string, string> {
  const query: Record<string, string> = {
    cluster: state.cluster,
    namespace: state.namespace,
    resource: state.resource,
    mode: state.mode,
    service: state.service,
    operation: state.operation,
    status: allowedStatuses.has(state.status) ? state.status : "",
    min_duration_ms: state.minDurationMS === undefined ? "" : String(Math.max(0, state.minDurationMS)),
    max_duration_ms: state.maxDurationMS === undefined ? "" : String(Math.max(0, state.maxDurationMS)),
    limit: String(allowedLimits.has(state.limit) ? state.limit : 100),
    search: state.searchID,
    trace_id: state.traceID,
    evidence_query: state.evidenceQueryID,
    from: state.from,
    to: state.to,
  };
  return Object.fromEntries(Object.entries(query).filter(([, value]) => Boolean(value)));
}
