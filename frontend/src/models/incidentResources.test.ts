import { describe, expect, it } from "vitest";

import {
  evidenceStateDefinition,
  isChangeContext,
  isKubernetesContext,
  resourceProvenance,
} from "./incidentResources";
import type { ResourceView } from "../types/incidents";

function resource(overrides: Partial<ResourceView> = {}): ResourceView {
  return {
    id: "2e082375-0185-4538-a722-e52e91d0cc15",
    kind: "evidence",
    migrated_legacy: false,
    migrated_legacy_context: false,
    ...overrides,
  };
}

describe("generic Incident Resource presentation boundaries", () => {
  it("keeps Evidence absence and invalidity semantics distinct", () => {
    expect(evidenceStateDefinition("available").label).toBe("Available");
    expect(evidenceStateDefinition("partial").label).toBe("Partial");
    expect(evidenceStateDefinition("no_data").label).toBe("No Data");
    expect(evidenceStateDefinition("unavailable").label).toBe("Unavailable");
    expect(evidenceStateDefinition("invalid").label).toBe("Invalid");
    expect(evidenceStateDefinition("superseded").label).toBe("Superseded");
  });

  it("does not infer Kubernetes or change identity from summary text", () => {
    const summaryOnly = resource({ summary: "Deployment changed in Kubernetes" });
    expect(isKubernetesContext(summaryOnly)).toBe(false);
    expect(isChangeContext(summaryOnly)).toBe(false);
    expect(isKubernetesContext(resource({ kind: "kubernetes_snapshot" }))).toBe(true);
    expect(isChangeContext(resource({ kind: "deployment_revision" }))).toBe(true);
  });

  it("renders legacy provenance without treating it as current native authority", () => {
    expect(resourceProvenance(resource())).toBe("V3 native projection");
    expect(resourceProvenance(resource({ migrated_legacy: true }))).toBe("Migrated legacy record");
    expect(resourceProvenance(resource({ migrated_legacy_context: true }))).toBe("Legacy context · audit-only");
  });
});
