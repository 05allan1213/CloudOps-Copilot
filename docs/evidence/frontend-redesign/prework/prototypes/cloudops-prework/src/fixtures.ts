export interface AtlasNodeFixture {
  id: string;
  kind: "cluster" | "namespace" | "service" | "workload";
  name: string;
  namespace: string;
  status: "healthy" | "warning" | "critical" | "unknown";
  x: number;
  y: number;
  z: number;
  parentIndex: number | null;
}

export interface ScaleRowFixture {
  id: string;
  time: string;
  source: string;
  level: "INFO" | "WARN" | "ERROR";
  message: string;
  fullValue: string;
}

export function createMonitoringFixture(pointCount = 7200) {
  const timestamps: number[] = [];
  const cpu: Array<number | null> = [];
  const latency: Array<number | null> = [];
  const errors: Array<number | null> = [];
  const start = Date.parse("2026-07-30T00:00:00Z") / 1000;

  for (let index = 0; index < pointCount; index += 1) {
    const incidentPulse = index > 4380 && index < 4920 ? Math.sin(((index - 4380) / 540) * Math.PI) : 0;
    const missing = index >= 3210 && index < 3260;
    timestamps.push(start + index * 5);
    cpu.push(missing ? null : Number((47 + Math.sin(index / 91) * 8 + Math.cos(index / 37) * 3 + incidentPulse * 31).toFixed(2)));
    latency.push(missing ? null : Number((122 + Math.sin(index / 73) * 18 + Math.cos(index / 19) * 7 + incidentPulse * 245).toFixed(2)));
    errors.push(missing ? null : Number((0.22 + Math.abs(Math.sin(index / 113)) * 0.3 + incidentPulse * 4.8).toFixed(3)));
  }

  return { timestamps, cpu, latency, errors, pointCount, missingPoints: 50 };
}

export function createAtlasNodes(count = 200): AtlasNodeFixture[] {
  const kinds: AtlasNodeFixture["kind"][] = ["cluster", "namespace", "service", "workload"];
  const statuses: AtlasNodeFixture["status"][] = ["healthy", "healthy", "healthy", "warning", "critical", "unknown"];

  return Array.from({ length: count }, (_, index) => {
    const ring = Math.floor(index / 25);
    const angle = ((index % 25) / 25) * Math.PI * 2 + ring * 0.31;
    const radius = 3.2 + ring * 1.18;
    return {
      id: `resource-${String(index + 1).padStart(3, "0")}`,
      kind: kinds[index % kinds.length],
      name: index === 17 ? "checkout-api-with-a-very-long-kubernetes-resource-name" : `cloudops-${kinds[index % kinds.length]}-${String(index + 1).padStart(3, "0")}`,
      namespace: index % 4 === 0 ? "cloudops-system" : "demo",
      status: statuses[index % statuses.length],
      x: Math.cos(angle) * radius,
      y: (ring - 3.5) * 0.78 + Math.sin(index * 0.47) * 0.35,
      z: Math.sin(angle) * radius,
      parentIndex: index === 0 ? null : Math.max(0, Math.floor((index - 1) / 4)),
    };
  });
}
export function createScaleRows(kind: "logs" | "traces" | "timeline" | "table"): ScaleRowFixture[] {
  const counts = { logs: 10000, traces: 2500, timeline: 5000, table: 20000 } as const;
  const sources = kind === "traces" ? ["gateway", "checkout", "payments", "mysql"] : ["cloudops-api", "otel-collector", "worker", "prometheus"];
  const base = Date.parse("2026-07-30T06:00:00Z");

  return Array.from({ length: counts[kind] }, (_, index) => {
    const id = `${kind.slice(0, 2)}-${String(index + 1).padStart(6, "0")}`;
    const traceId = `${(index + 4096).toString(16).padStart(8, "0")}${"a7d90f1e".repeat(3)}`.slice(0, 32);
    const source = sources[index % sources.length];
    const level: ScaleRowFixture["level"] = index % 97 === 0 ? "ERROR" : index % 19 === 0 ? "WARN" : "INFO";
    const message = kind === "traces"
      ? `${source}.operation.${index % 31} completed in ${12 + (index % 840)}ms with parent span ${traceId.slice(0, 16)}`
      : kind === "timeline"
        ? `Incident stage ${index % 12} observed Provider revision ${13 + (index % 7)} without changing the reader position`
        : kind === "table"
          ? `Namespace demo resource ${id} current projection and Provider-backed status`
          : `request completed route=/api/v3/work/${index % 42} request_id=req-${traceId.slice(0, 12)} trace_id=${traceId}`;
    return {
      id,
      time: new Date(base + index * 700).toISOString(),
      source,
      level,
      message,
      fullValue: `${new Date(base + index * 700).toISOString()} ${level} source=${source} ${message} exact_hash=${traceId}${traceId}`,
    };
  });
}
