import type { ResourceView } from "../types/incidents";

export interface EvidenceStateDefinition {
  label: string;
  description: string;
}

export function resourceKindLabel(kind: string): string {
  const normalized = kind.trim();
  if (!normalized) return "Persisted Resource";
  return normalized
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

export function resourceStatusLabel(status?: string): string {
  const normalized = status?.trim();
  if (!normalized) return "Not projected";
  return normalized
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

export function resourceTimestamp(item: ResourceView): string {
  return item.updated_at || item.created_at || "";
}

export function isKubernetesContext(item: ResourceView): boolean {
  return resourceIdentity(item).some((value) => /(kubernetes|k8s|workload|deployment|replicaset|pod|service|endpoint|rollout)/i.test(value));
}

export function isChangeContext(item: ResourceView): boolean {
  return resourceIdentity(item).some((value) => /(change|deploy|revision|commit|gitops|image|release|argo)/i.test(value));
}

export function isAgentActivity(item: ResourceView): boolean {
  return resourceIdentity(item).some((value) => /(agent|investigation|diagnosis|hypothesis|step|tool)/i.test(value));
}

export function deterministicResourceSummary(item: ResourceView): string {
  return item.summary?.trim() || "No bounded deterministic summary was projected.";
}

export function resourceProvenance(item: ResourceView): string {
  if (item.migrated_legacy_context) return "Legacy context · audit-only";
  if (item.migrated_legacy) return "Migrated legacy record";
  return "Native projection";
}

export function evidenceStateDefinition(status?: string): EvidenceStateDefinition {
  const normalized = status?.trim().toLowerCase() || "unknown";
  const definitions: Record<string, EvidenceStateDefinition> = {
    available: {
      label: "Available",
      description: "A bounded Evidence projection is available; trust and claim authority are not projected by this contract.",
    },
    partial: {
      label: "Partial",
      description: "Only part of the bounded Evidence result is available. Completeness is not asserted.",
    },
    no_data: {
      label: "No Data",
      description: "The persisted result contains no source data. This state is not treated as a passing result.",
    },
    unavailable: {
      label: "Unavailable",
      description: "The source or projection was unavailable, so the browser cannot assert an Evidence result.",
    },
    invalid: {
      label: "Invalid",
      description: "The persisted Evidence item did not satisfy its server-side validation boundary.",
    },
    superseded: {
      label: "Superseded",
      description: "A newer persisted Evidence item replaced this record for current use.",
    },
    unknown: {
      label: "Not Projected",
      description: "The current Resource contract did not project an Evidence state.",
    },
  };
  return definitions[normalized] ?? {
    label: resourceStatusLabel(normalized),
    description: "This server-projected Evidence state is displayed without browser reinterpretation.",
  };
}

function resourceIdentity(item: ResourceView): string[] {
  return [item.kind, item.status ?? ""];
}
