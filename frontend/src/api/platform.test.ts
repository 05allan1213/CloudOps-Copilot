import { describe, expect, it } from "vitest";

import { configurationDraft, type ConfigurationRevision } from "./platform";

describe("configurationDraft", () => {
  it("retains revision-bound Escalation Policies in an isolated draft", () => {
    const revision = {
      id: "11111111-1111-4111-8111-111111111111",
      number: 7,
      hash: "a".repeat(64),
      summary: "Active alert policy",
      general: {
        query_max_lookback_seconds: 3600,
        query_max_results: 1000,
        telemetry_retention_days: 7,
        browser_notifications_enabled: false,
        automatic_escalation_enabled: false,
      },
      scope: { name: "Local", cluster_id: "cloudops-local", environment: "local", namespaces: ["demo"], active: true },
      scopes: [{ name: "Local", cluster_id: "cloudops-local", environment: "local", namespaces: ["demo"], active: true }],
      providers: [],
      escalation_policies: [{
        id: "22222222-2222-4222-8222-222222222222",
        configuration_revision_id: "11111111-1111-4111-8111-111111111111",
        name: "Critical demo",
        enabled: true,
        severities: ["critical"],
        namespaces: ["demo"],
        label_matchers: { team: "platform" },
        minimum_firing_seconds: 300,
        minimum_recurrence_count: 1,
        create_incident: true,
      }],
      secret_references: [],
      created_by: "local-owner",
      created_at: "2026-07-27T03:00:00Z",
      active: true,
    } satisfies ConfigurationRevision;

    const draft = configurationDraft(revision);
    expect(draft.escalation_policies).toEqual(revision.escalation_policies);
    draft.escalation_policies[0].label_matchers.team = "changed";
    expect(revision.escalation_policies[0].label_matchers.team).toBe("platform");
  });
});
