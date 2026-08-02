import { describe, expect, it } from "vitest";

import type { ConfigurationRevision, ProviderHealth } from "../../api/platform";
import {
  buildSectionConfigurationDraft,
  classifySettingsApplyOutcome,
  createPersistedSettingsDrafts,
  createSettingsSectionDrafts,
  isSettingsSectionDirty,
  parsePersistedSettingsDrafts,
  persistedSettingsDraftConflicts,
  rebaseSettingsSection,
  restorePersistedSettingsDrafts,
  sanitizeSecretReferences,
  sectionChangedInRevision,
  settingsSectionChanges,
  settingsSectionFingerprint,
  validateSettingsSectionLocally,
  type ScopeSectionValue,
} from "./settingsDraft";

function revision(overrides: Partial<ConfigurationRevision> = {}): ConfigurationRevision {
  return {
    id: "11111111-1111-4111-8111-111111111111",
    number: 12,
    hash: "a".repeat(64),
    summary: "Active configuration",
    general: {
      query_max_lookback_seconds: 3600,
      query_max_results: 1000,
      telemetry_retention_days: 7,
      browser_notifications_enabled: false,
      automatic_escalation_enabled: false,
    },
    scope: {
      id: "22222222-2222-4222-8222-222222222222",
      name: "Local",
      cluster_id: "cloudops-local",
      environment: "local",
      namespaces: ["demo"],
      active: true,
    },
    scopes: [{
      id: "22222222-2222-4222-8222-222222222222",
      name: "Local",
      cluster_id: "cloudops-local",
      environment: "local",
      namespaces: ["demo"],
      active: true,
    }],
    providers: [
      { provider: "llm", enabled: true, endpoint: "https://llm.example/v1", model: "model-a", timeout_ms: 10000, max_results: 200, context_link_base: "" },
      { provider: "kubernetes", enabled: true, endpoint: "", model: "", timeout_ms: 10000, max_results: 200, context_link_base: "" },
    ],
    escalation_policies: [],
    secret_references: [{
      provider: "llm",
      purpose: "api_key",
      secret_version_id: "33333333-3333-4333-8333-333333333333",
      state: "configured",
      fingerprint: "sha256:visible-metadata-only",
    }],
    created_by: "local-owner",
    created_at: "2026-07-31T08:00:00Z",
    active: true,
    ...overrides,
  };
}

describe("Settings section drafts", () => {
  it("applies only the selected section over its exact base revision", () => {
    const sections = createSettingsSectionDrafts(revision());
    const system = sections.system;
    (system.value as ConfigurationRevision["general"]).query_max_results = 2500;
    system.summary = "Raise the query result bound";
    const provider = (sections.providers.value as ConfigurationRevision["providers"])[0];
    provider.endpoint = "https://unapplied.example/v1";

    const payload = buildSectionConfigurationDraft(system);

    expect(payload.general.query_max_results).toBe(2500);
    expect(payload.providers[0].endpoint).toBe("https://llm.example/v1");
    expect(payload.summary).toBe("Raise the query result bound");
    expect(isSettingsSectionDirty(system)).toBe(true);
    expect(settingsSectionChanges(system)).toContain("查询结果上限：1000 -> 2500");
  });

  it("preserves the chosen default Scope without mutating another section", () => {
    const sections = createSettingsSectionDrafts(revision());
    const scopes = sections.scopes.value as ScopeSectionValue;
    scopes.scopes.push({ name: "Staging", cluster_id: "staging", environment: "staging", namespaces: ["apps"], active: false });
    scopes.defaultIndex = 1;
    sections.scopes.summary = "Select the staging Scope";

    const payload = buildSectionConfigurationDraft(sections.scopes);

    expect(payload.scope.cluster_id).toBe("staging");
    expect(payload.scopes).toHaveLength(2);
    expect(payload.general.query_max_results).toBe(1000);
  });

  it("detects same-section concurrent revisions and rebases only after an explicit choice", () => {
    const section = createSettingsSectionDrafts(revision()).providers;
    (section.value as ConfigurationRevision["providers"])[0].timeout_ms = 15000;
    section.summary = "Increase the LLM timeout";
    const latest = revision({
      id: "44444444-4444-4444-8444-444444444444",
      number: 13,
      providers: revision().providers.map((provider) => provider.provider === "llm"
        ? { ...provider, model: "model-b" }
        : provider),
    });

    expect(sectionChangedInRevision(section, latest)).toBe(true);
    const preserved = rebaseSettingsSection(section, latest, true);
    expect(preserved.baseRevisionID).toBe(latest.id);
    expect((preserved.value as ConfigurationRevision["providers"])[0].timeout_ms).toBe(15000);
    expect(preserved.summary).toBe("Increase the LLM timeout");
    const discarded = rebaseSettingsSection(section, latest, false);
    expect((discarded.value as ConfigurationRevision["providers"])[0].model).toBe("model-b");
    expect(discarded.summary).toBe("");
  });

  it("never includes secret values or display-only fingerprints in an apply payload", () => {
    const section = createSettingsSectionDrafts(revision())["secret-references"];
    section.summary = "Rotate the LLM credential reference";
    const payload = buildSectionConfigurationDraft(section);
    const encoded = JSON.stringify(payload);

    expect(payload.secret_references).toEqual([{
      provider: "llm",
      purpose: "api_key",
      secret_version_id: "33333333-3333-4333-8333-333333333333",
    }]);
    expect(encoded).not.toContain("fingerprint");
    expect(encoded).not.toContain("secret_value");
    expect(sanitizeSecretReferences(revision().secret_references)[0]).not.toHaveProperty("state");
  });

  it("keeps local validation scoped and blocks a missing publication summary", () => {
    const section = createSettingsSectionDrafts(revision()).system;
    (section.value as ConfigurationRevision["general"]).query_max_results = 0;

    expect(validateSettingsSectionLocally(section)).toEqual(expect.arrayContaining([
      { name: "system.summary", message: "发布摘要需为 3 至 255 个字符。" },
      { name: "system.query_max_results", message: "查询结果上限必须在 1 至 10000 之间。" },
    ]));
  });

  it("protects summary-only edits and invalidates validation identity when the summary changes", () => {
    const section = createSettingsSectionDrafts(revision()).system;
    const initialFingerprint = settingsSectionFingerprint(section);

    section.summary = "Owner audit note";

    expect(isSettingsSectionDirty(section)).toBe(true);
    expect(settingsSectionChanges(section)).toEqual([]);
    expect(settingsSectionFingerprint(section)).not.toBe(initialFingerprint);
  });

  it("persists only allowlisted non-sensitive draft fields for 24 hours", () => {
    const sections = createSettingsSectionDrafts(revision());
    const provider = (sections.providers.value as ConfigurationRevision["providers"])[0] as ConfigurationRevision["providers"][number] & { secret_value?: string; access_token?: string };
    provider.timeout_ms = 15000;
    provider.secret_value = "must-not-persist";
    provider.access_token = "must-not-persist-either";
    sections.providers.summary = "Increase provider timeout";
    const payload = createPersistedSettingsDrafts(sections, 1_000);
    const encoded = JSON.stringify(payload);

    expect(Object.keys(payload.sections)).toEqual(["providers"]);
    expect(encoded).not.toContain("must-not-persist");
    expect(parsePersistedSettingsDrafts(encoded, 1_000 + 23 * 60 * 60 * 1000)?.status).toBe("fresh");
    expect(parsePersistedSettingsDrafts(encoded, 1_000 + 25 * 60 * 60 * 1000)?.status).toBe("expired");
  });

  it("rejects damaged persisted shapes and strips unknown fields when parsing", () => {
    const sections = createSettingsSectionDrafts(revision());
    (sections.providers.value as ConfigurationRevision["providers"])[0].timeout_ms = 15000;
    sections.providers.summary = "Increase provider timeout";
    const raw = createPersistedSettingsDrafts(sections, 1_000) as unknown as {
      sections: { providers: { value: Array<Record<string, unknown>>; baseDraft: { providers: Array<Record<string, unknown>> } } };
    };
    raw.sections.providers.value[0].access_token = "must-not-survive-parse";
    raw.sections.providers.baseDraft.providers[0].secret_value = "must-not-survive-parse";

    const parsed = parsePersistedSettingsDrafts(JSON.stringify(raw), 1_000);
    expect(JSON.stringify(parsed)).not.toContain("must-not-survive-parse");

    raw.sections.providers.value[0].provider = "unknown-provider";
    expect(parsePersistedSettingsDrafts(JSON.stringify(raw), 1_000)).toBeNull();
  });

  it("restores the exact saved base and reports a newer active Revision as a conflict", () => {
    const sections = createSettingsSectionDrafts(revision());
    (sections.system.value as ConfigurationRevision["general"]).query_max_results = 2500;
    sections.system.summary = "Raise query result bound";
    const payload = createPersistedSettingsDrafts(sections, 1_000);
    const newer = revision({ id: "44444444-4444-4444-8444-444444444444", number: 13, hash: "b".repeat(64) });
    const restored = restorePersistedSettingsDrafts(newer, payload);

    expect(restored.system.baseRevisionID).toBe(revision().id);
    expect((restored.system.value as ConfigurationRevision["general"]).query_max_results).toBe(2500);
    expect(persistedSettingsDraftConflicts(payload, newer)).toBe(true);
  });
});

describe("Settings apply truth", () => {
  const health = (overrides: Partial<ProviderHealth> = {}): ProviderHealth => ({
    provider: "llm",
    state: "available",
    detail: "Provider probe succeeded",
    updated_at: "2026-07-31T08:02:00Z",
    ...overrides,
  });

  it("does not style an accepted Revision as applied success", () => {
    const outcome = classifySettingsApplyOutcome(revision({
      worker_boundary: {
        task_id: "task-1",
        revision_id: "11111111-1111-4111-8111-111111111111",
        status: "ready",
      },
    }), [health(), health({ provider: "kubernetes" })]);

    expect(outcome.state).toBe("accepted");
    expect(outcome.title).toBe("Revision 已接收");
  });

  it("reports itemized partial Provider results after Worker success", () => {
    const outcome = classifySettingsApplyOutcome(revision({
      worker_boundary: {
        task_id: "task-1",
        revision_id: "11111111-1111-4111-8111-111111111111",
        status: "succeeded",
        observed_hash: "a".repeat(64),
        observed_at: "2026-07-31T08:01:00Z",
      },
    }), [
      health(),
      health({ provider: "kubernetes", state: "partial", detail: "One registered Scope is unavailable" }),
    ]);

    expect(outcome.state).toBe("partial");
    expect(outcome.items.find((item) => item.id === "kubernetes")).toMatchObject({
      state: "partial",
      detail: "One registered Scope is unavailable",
    });
  });

  it("does not reuse stale Provider health or a mismatched Worker hash as success", () => {
    const mismatch = classifySettingsApplyOutcome(revision({
      worker_boundary: {
        task_id: "task-2",
        revision_id: "11111111-1111-4111-8111-111111111111",
        status: "succeeded",
        observed_hash: "b".repeat(64),
      },
    }), [health()]);

    expect(mismatch.state).toBe("partial");
    expect(mismatch.items.find((item) => item.id === "worker")?.state).toBe("partial");

    const staleHealth = classifySettingsApplyOutcome(revision({
      worker_boundary: {
        task_id: "task-3",
        revision_id: "11111111-1111-4111-8111-111111111111",
        status: "succeeded",
        observed_hash: "a".repeat(64),
      },
    }), [health({ configuration_revision_id: "99999999-9999-4999-8999-999999999999" })]);

    expect(staleHealth.state).toBe("partial");
    expect(staleHealth.items.find((item) => item.id === "llm")?.state).toBe("unknown");
  });
});
