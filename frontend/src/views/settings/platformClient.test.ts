import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  applyConfiguration,
  createSecret,
  getSettings,
  getStorageStatus,
  testProvider,
  validateSettings,
  type ConfigurationDraft,
  type ProviderConfiguration,
} from "../../api/platform";

const client = vi.hoisted(() => ({
  getJSON: vi.fn(),
  postJSON: vi.fn(),
}));

vi.mock("../../api/client", () => client);

const draft = { summary: "Scoped change" } as ConfigurationDraft;
const provider: ProviderConfiguration = {
  provider: "prometheus",
  enabled: true,
  endpoint: "http://prometheus:9090",
  model: "",
  timeout_ms: 5000,
  max_results: 200,
  context_link_base: "http://127.0.0.1:18081",
};

describe("Settings typed platform client", () => {
  beforeEach(() => {
    client.getJSON.mockReset();
    client.postJSON.mockReset();
  });

  it("uses the canonical read endpoints", async () => {
    await getSettings();
    await getStorageStatus();

    expect(client.getJSON).toHaveBeenNthCalledWith(1, "/api/v1/settings", { signal: undefined });
    expect(client.getJSON).toHaveBeenNthCalledWith(2, "/api/v1/storage-status", { signal: undefined });
  });

  it("preserves validation identity and exact write payloads", async () => {
    await validateSettings(draft);
    await applyConfiguration("validation-1", draft, { id: "revision-1", hash: "a".repeat(64) });
    await testProvider(provider, [], "cluster-a");
    await createSecret({ provider: "prometheus", purpose: "token", value: "write-only" });

    expect(client.postJSON).toHaveBeenNthCalledWith(1, "/api/v1/settings/validate", draft, { timeout: 70_000 });
    expect(client.postJSON).toHaveBeenNthCalledWith(2, "/api/v1/configuration-revisions", {
      validation_id: "validation-1",
      expected_active_revision_id: "revision-1",
      expected_active_revision_hash: "a".repeat(64),
      draft,
    });
    expect(client.postJSON).toHaveBeenNthCalledWith(3, "/api/v1/providers/prometheus/tests", {
      configuration: provider,
      secret_references: [],
      cluster_id: "cluster-a",
    });
    expect(client.postJSON).toHaveBeenNthCalledWith(4, "/api/v1/secrets", {
      provider: "prometheus",
      purpose: "token",
      value: "write-only",
    });
  });
});
