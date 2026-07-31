export interface WorkspaceFixtureRow extends Record<string, unknown> {
  id: string;
  severity: "critical" | "warning" | "info";
  severityLabel: string;
  resource: string;
  namespace: string;
  status: "available" | "partial" | "stale";
  provider: string;
  owner: string;
  updatedAt: string;
  exactHash: string;
  fullValue: string;
}

export function createWorkspaceRows(count = 20_000): WorkspaceFixtureRow[] {
  const base = Date.parse("2026-07-31T00:00:00Z");
  const providers = ["Kubernetes", "Prometheus", "Tempo", "Elasticsearch"];
  return Array.from({ length: count }, (_, index) => {
    const suffix = String(index + 1).padStart(5, "0");
    const severity = index % 71 === 0 ? "critical" : index % 13 === 0 ? "warning" : "info";
    const status = index % 97 === 0 ? "stale" : index % 19 === 0 ? "partial" : "available";
    const trace = `${(index + 4096).toString(16).padStart(8, "0")}${"a7d90f1e".repeat(3)}`.slice(0, 32);
    const resource = index === 17
      ? "deployment/cloudops-api-with-a-very-long-kubernetes-resource-name-for-overflow-validation"
      : `deployment/cloudops-resource-${suffix}`;
    const updatedAt = new Date(base + index * 1_000).toISOString();
    const exactHash = `${trace}${trace}`;
    return {
      id: `resource-${suffix}`,
      severity,
      severityLabel: severity === "critical" ? "严重" : severity === "warning" ? "警告" : "信息",
      resource,
      namespace: index % 3 === 0 ? "cloudops-system" : "demo",
      status,
      provider: providers[index % providers.length],
      owner: index % 4 === 0 ? "平台值班" : "应用值班",
      updatedAt,
      exactHash,
      fullValue: `${updatedAt} provider=${providers[index % providers.length]} namespace=${index % 3 === 0 ? "cloudops-system" : "demo"} resource=${resource} status=${status} exact_hash=${exactHash}`,
    };
  });
}
