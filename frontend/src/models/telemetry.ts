export interface TelemetryResourceCandidate {
  id: string;
  kind: string;
  namespace?: string;
  name: string;
}

export function resolveTelemetryResourceID(
  resources: readonly TelemetryResourceCandidate[],
  resourceID: string,
  legacyWorkload: string,
  namespace = "",
): string {
  if (resourceID && resources.some((resource) => resource.id === resourceID)) return resourceID;
  if (!legacyWorkload) return "";
  const normalized = legacyWorkload.toLocaleLowerCase();
  return resources.find((resource) => {
    if (namespace && resource.namespace !== namespace) return false;
    const candidates = [
      resource.id,
      resource.name,
      `${resource.kind}/${resource.name}`,
      `${resource.kind.toLocaleLowerCase()}/${resource.namespace ?? ""}/${resource.name}`,
    ];
    return candidates.some((value) => value.toLocaleLowerCase() === normalized);
  })?.id ?? "";
}

export function logRawValue(message: string): string {
  return message;
}

export interface CopyableTraceSpan {
  span_id: string;
  parent_span_id?: string;
  service: string;
  name: string;
  kind?: string;
  start_time: string;
  duration_ms: number;
  status: string;
  critical_path: boolean;
  attributes: Record<string, string>;
  events: Array<{ name: string; timestamp: string; attributes: Record<string, string> }>;
}

export function traceSpanRawValue(span: CopyableTraceSpan): string {
  return JSON.stringify(span, null, 2);
}

export function traceServiceColor(service: string): string {
  const tokens = [
    "var(--co-action-primary)",
    "var(--co-status-info-fg)",
    "var(--co-status-inconclusive-fg)",
    "var(--co-text-secondary)",
  ];
  let hash = 0;
  for (const character of service) hash = ((hash << 5) - hash + character.charCodeAt(0)) | 0;
  return tokens[Math.abs(hash) % tokens.length] ?? tokens[0];
}

export interface WaterfallPosition {
  left: number;
  width: number;
}

export function waterfallPosition(
  traceStart: string,
  traceDurationMS: number,
  spanStart: string,
  spanDurationMS: number,
): WaterfallPosition {
  const origin = new Date(traceStart).getTime();
  const start = new Date(spanStart).getTime();
  const duration = Math.max(traceDurationMS, spanDurationMS, 0.001);
  const left = Number.isFinite(origin) && Number.isFinite(start)
    ? Math.max(0, Math.min(100, ((start - origin) / duration) * 100))
    : 0;
  const width = Math.max(0.35, Math.min(100 - left, (Math.max(0, spanDurationMS) / duration) * 100));
  return { left, width };
}
