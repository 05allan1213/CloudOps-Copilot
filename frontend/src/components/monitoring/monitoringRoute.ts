import type { QueryMode } from "../../api/monitoring";

export interface MonitoringRouteState {
  cluster: string;
  namespace: string;
  resource: string;
  mode: QueryMode;
  metric: string;
  promql: string;
  from: string;
  to: string;
  execution: string;
  definition: string;
}
type RouteQueryLike = Record<string, unknown>;

export function routeString(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === "string" ? value[0] : "";
  return typeof value === "string" ? value : "";
}

export function normalizeRouteTime(value: unknown): string {
  const raw = routeString(value);
  if (!raw) return "";
  const parsed = new Date(raw);
  return Number.isNaN(parsed.getTime()) ? "" : parsed.toISOString();
}

export function parseMonitoringRoute(query: RouteQueryLike): MonitoringRouteState {
  return {
    cluster: routeString(query.cluster),
    namespace: routeString(query.namespace),
    resource: routeString(query.resource),
    mode: routeString(query.mode) === "expert" ? "expert" : "guided",
    metric: routeString(query.metric),
    promql: routeString(query.query),
    from: normalizeRouteTime(query.from),
    to: normalizeRouteTime(query.to),
    execution: routeString(query.execution),
    definition: routeString(query.definition),
  };
}

export function buildMonitoringRouteQuery(state: MonitoringRouteState): Record<string, string> {
  const query: Record<string, string> = {
    cluster: state.cluster,
    namespace: state.namespace,
    resource: state.resource,
    mode: state.mode,
    from: normalizeRouteTime(state.from),
    to: normalizeRouteTime(state.to),
  };
  if (state.mode === "guided" && state.metric) query.metric = state.metric;
  if (state.mode === "expert" && state.promql.trim()) query.query = state.promql.trim();
  if (state.execution) query.execution = state.execution;
  if (state.definition) query.definition = state.definition;
  return Object.fromEntries(Object.entries(query).filter(([, value]) => Boolean(value)));
}
