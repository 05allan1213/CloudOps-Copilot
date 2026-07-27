import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { getDevOpsWorkspace, type DevOpsWorkspace } from "../api/devops";
import { useDevOpsWorkspaceStore } from "./devOpsWorkspace";

vi.mock("../api/devops", () => ({
  executeActionCard: vi.fn(),
  executeOperationPlan: vi.fn(),
  getDevOpsWorkspace: vi.fn(),
}));

const getWorkspaceMock = vi.mocked(getDevOpsWorkspace);

describe("DevOps Workspace store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
  });

  it("leaves loading state after an aborted request and permits a later refresh", async () => {
    const controller = new AbortController();
    getWorkspaceMock.mockImplementationOnce((signal) => new Promise((_, reject) => {
      signal?.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
    }));
    const store = useDevOpsWorkspaceStore();

    const abortedLoad = store.load(false, controller.signal);
    await Promise.resolve();
    expect(store.loading).toBe(true);
    controller.abort();
    await abortedLoad;

    expect(store.loading).toBe(false);
    expect(store.error).toBe("");

    const workspace = { collected_at: "2026-07-28T00:00:00Z" } as DevOpsWorkspace;
    getWorkspaceMock.mockResolvedValueOnce(workspace);
    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.workspace).toStrictEqual(workspace);
  });
});
