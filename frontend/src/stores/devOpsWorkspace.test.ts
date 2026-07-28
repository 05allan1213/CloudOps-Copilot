import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  getAgentInvestigations,
  proposeOperationPlan,
  type OperationPlan,
  type OperationPlanProposalInput,
} from "../api/agent";
import { getDevOpsWorkspace, type DevOpsWorkspace } from "../api/devops";
import { getResources, type ResourcePage } from "../api/infrastructure";
import { useDevOpsWorkspaceStore } from "./devOpsWorkspace";

vi.mock("../api/devops", () => ({
  executeActionCard: vi.fn(),
  executeOperationPlan: vi.fn(),
  getDevOpsWorkspace: vi.fn(),
}));
vi.mock("../api/agent", () => ({
  authorizeActionCard: vi.fn(),
  authorizeOperationPlan: vi.fn(),
  getAgentInvestigations: vi.fn(),
  proposeOperationPlan: vi.fn(),
}));
vi.mock("../api/infrastructure", () => ({
  getResources: vi.fn(),
}));

const getWorkspaceMock = vi.mocked(getDevOpsWorkspace);
const getResourcesMock = vi.mocked(getResources);
const getInvestigationsMock = vi.mocked(getAgentInvestigations);
const proposeOperationPlanMock = vi.mocked(proposeOperationPlan);

const workspace = { collected_at: "2026-07-28T00:00:00Z" } as DevOpsWorkspace;
const resources = {
  scope: { name: "Local", cluster_id: "cloudops-local", environment: "local", namespaces: ["demo"], active: true },
  provider_state: "available",
  source: { provider: "kubernetes", cluster_id: "cloudops-local", identity: "kubernetes://cloudops-local", collected_at: "2026-07-28T00:00:00Z" },
  freshness: { state: "fresh", fresh_until: "2026-07-28T00:01:00Z", age_seconds: 0 },
  items: [],
  partial: false,
  truncated: false,
  collected_at: "2026-07-28T00:00:00Z",
} satisfies ResourcePage;

describe("DevOps Workspace store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    getWorkspaceMock.mockResolvedValue(workspace);
    getResourcesMock.mockResolvedValue(resources);
    getInvestigationsMock.mockResolvedValue([]);
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

    getWorkspaceMock.mockResolvedValueOnce(workspace);
    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.workspace).toStrictEqual(workspace);
  });

  it("keeps the durable workspace available while failing Scenario planning closed", async () => {
    getResourcesMock.mockRejectedValueOnce(new Error("Kubernetes unavailable"));
    const store = useDevOpsWorkspaceStore();

    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.workspace).toStrictEqual(workspace);
    expect(store.scenarioResources).toBeNull();
    expect(store.scenarioPlanningError).toContain("Kubernetes Deployment projection");
  });

  it("proposes one exact Scenario scale plan and then refreshes durable state", async () => {
    const input = {
      run_id: "11111111-1111-4111-8111-111111111111",
      action_type: "kubernetes.deployment.scale",
      target: {
        cluster_id: "cloudops-local",
        environment: "local",
        namespace: "demo",
        workload_kind: "Deployment",
        workload_name: "cloudops-scenario-fault",
        scenario_id: "scenario-20260728000000-deadbeef",
      },
      parameters: { replicas: 0 },
      intended_state: { replicas: 0 },
      preconditions: [
        { type: "deployment.replicas", expected_replicas: 1 },
        { type: "deployment.resource_version", expected_resource_version: "42" },
        { type: "local.change_freeze", expected_enabled: false, expected_version: 0 },
      ],
      risk: "Scale only the bounded Scenario fault workload.",
      verification_intent: { type: "kubernetes.deployment.scale", expected_replicas: 0 },
      expires_at: "2026-07-28T01:00:00Z",
    } satisfies OperationPlanProposalInput;
    const plan = { id: "22222222-2222-4222-8222-222222222222", status: "proposed" } as OperationPlan;
    proposeOperationPlanMock.mockResolvedValueOnce(plan);
    const store = useDevOpsWorkspaceStore();

    const result = await store.proposeScenarioPlan(input);

    expect(result).toStrictEqual(plan);
    expect(proposeOperationPlanMock).toHaveBeenCalledWith(input);
    expect(getWorkspaceMock).toHaveBeenCalledOnce();
    expect(store.notice).toContain("immutable Operation Plan");
  });
});
