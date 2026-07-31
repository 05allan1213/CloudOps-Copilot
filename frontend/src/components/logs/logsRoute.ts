import type { TelemetryQueryMode } from "../../api/telemetry";

const allowedLevels = new Set(["debug", "info", "warn", "error"]);
const allowedLimits = new Set([1, 100, 200, 500, 1000]);

export interface LogsRouteState {
  cluster: string;
  namespace: string;
  resource: string;
  legacyWorkload: string;
  mode: TelemetryQueryMode;
  text: string;
  traceID: string;
  levels: string[];
  limit: number;
  tail: boolean;
  wrap: boolean;
  queryID: string;
  selectedEntryID: string;
  from: string;
  to: string;
}

function routeString(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === "string" ? value[0] : "";
  return typeof value === "string" ? value : "";
}

export function parseLogsRoute(query: Record<string, unknown>): LogsRouteState {
  const requestedLimit = Number(routeString(query.limit));
  return {
    cluster: routeString(query.cluster),
    namespace: routeString(query.namespace),
    resource: routeString(query.resource),
    legacyWorkload: routeString(query.workload),
    mode: routeString(query.mode) === "expert" ? "expert" : "guided",
    text: routeString(query.text),
    traceID: routeString(query.trace_id),
    levels: routeString(query.levels).split(",").filter((value) => allowedLevels.has(value)),
    limit: allowedLimits.has(requestedLimit) ? requestedLimit : 200,
    tail: routeString(query.tail) === "1",
    wrap: routeString(query.wrap) === "1",
    queryID: routeString(query.query),
    selectedEntryID: routeString(query.selected),
    from: routeString(query.from),
    to: routeString(query.to),
  };
}

export function buildLogsRouteQuery(state: Omit<LogsRouteState, "legacyWorkload">): Record<string, string> {
  const query: Record<string, string> = {
    cluster: state.cluster,
    namespace: state.namespace,
    resource: state.resource,
    mode: state.mode,
    text: state.text,
    trace_id: state.traceID,
    levels: state.levels.filter((value) => allowedLevels.has(value)).join(","),
    limit: String(allowedLimits.has(state.limit) ? state.limit : 200),
    tail: state.tail ? "1" : "",
    wrap: state.wrap ? "1" : "",
    query: state.queryID,
    selected: state.selectedEntryID,
    from: state.from,
    to: state.to,
  };
  return Object.fromEntries(Object.entries(query).filter(([, value]) => Boolean(value)));
}
