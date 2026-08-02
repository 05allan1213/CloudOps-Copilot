import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  authorizeOperationPlan,
  getAgentInvestigations,
  proposeOperationPlan,
  type AgentRun,
  type OperationPlan,
  type OperationPlanProposalInput,
} from "../api/agent";
import {
  executeOperationPlan,
  getDevOpsWorkspace,
  type DevOpsWorkspace,
  type OperationExecution,
} from "../api/devops";
import { getResources, type ResourcePage } from "../api/infrastructure";
import {
  classifyDevOpsRun,
  classifyDevOpsSubject,
  incidentStageHref,
  useDevOpsWorkspaceStore,
} from "./devOpsWorkspace";

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
const authorizeOperationPlanMock = vi.mocked(authorizeOperationPlan);
const executeOperationPlanMock = vi.mocked(executeOperationPlan);

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

function investigation(
  id: string,
  subjectType: AgentRun["subject_type"],
  incidentID = "",
): AgentRun {
  return {
    id,
    subject_type: subjectType,
    ...(incidentID ? { incident_id: incidentID } : {}),
    configuration_revision_id: "revision-1",
    context_snapshot_id: "context-1",
    status: "completed",
    uncertainty: "low",
    objective: "Focused ownership fixture",
    prompt_version: "test/v1",
    created_at: "2026-07-28T00:00:00Z",
    updated_at: "2026-07-28T00:00:00Z",
    evidence_count: 1,
    steps: [],
    evidence_citations: [],
    guidance_citations: [],
    action_cards: [],
    operation_plans: [],
  };
}

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
    expect(store.scenarioPlanningError).toContain("Kubernetes Deployment 投影");
  });

  it("classifies Incident ownership from both execution and Agent run facts", () => {
    const subject = { id: "plan-1", run_id: "run-1" } as OperationPlan;
    const execution = {
      id: "execution-1",
      subject_id: subject.id,
      incident_id: "incident-from-execution",
    } as OperationExecution;

    expect(classifyDevOpsSubject(subject, [execution], [])).toMatchObject({
      kind: "incident",
      incidentID: "incident-from-execution",
    });
    expect(classifyDevOpsSubject(subject, [], [investigation("run-1", "incident", "incident-from-run")])).toMatchObject({
      kind: "incident",
      incidentID: "incident-from-run",
    });
    expect(classifyDevOpsRun("run-1", [investigation("run-1", "consultation")])).toMatchObject({
      kind: "non_incident",
      incidentID: "",
    });
    expect(classifyDevOpsRun("missing", [])).toMatchObject({ kind: "unknown" });
  });

  it("builds stable Incident stage links without accepting an empty identity", () => {
    expect(incidentStageHref("incident/with space", "approval")).toBe("/incidents/incident%2Fwith%20space#approval");
    expect(incidentStageHref("incident-1", "delivery")).toBe("/incidents/incident-1#delivery");
    expect(incidentStageHref("incident-1", "verification")).toBe("/incidents/incident-1#verification");
    expect(incidentStageHref("  ", "verification")).toBe("");
  });

  it("blocks Incident-owned authorization and execution before an API mutation", async () => {
    const plan = {
      id: "plan-incident",
      run_id: "run-incident",
      content_hash: "a".repeat(64),
    } as OperationPlan;
    const store = useDevOpsWorkspaceStore();
    store.workspace = { executions: [] } as unknown as DevOpsWorkspace;
    store.investigations = [investigation("run-incident", "incident", "incident-1")];

    await store.authorizePlan(plan, "Owner review");
    const execution = await store.executePlan(plan);

    expect(authorizeOperationPlanMock).not.toHaveBeenCalled();
    expect(executeOperationPlanMock).not.toHaveBeenCalled();
    expect(execution).toBeNull();
    expect(store.failure?.code).toBe("INCIDENT_OWNED_OPERATION");
    expect(store.error).toContain("Incident 生命周期");
  });

  it("blocks unknown ownership but retains a proven non-incident Plan path", async () => {
    const plan = {
      id: "plan-global",
      run_id: "run-global",
      content_hash: "b".repeat(64),
    } as OperationPlan;
    const store = useDevOpsWorkspaceStore();
    store.workspace = { executions: [] } as unknown as DevOpsWorkspace;

    await store.authorizePlan(plan, "Unknown run");
    expect(authorizeOperationPlanMock).not.toHaveBeenCalled();
    expect(store.failure?.code).toBe("DEVOPS_OWNERSHIP_UNKNOWN");

    store.investigations = [investigation("run-global", "consultation")];
    authorizeOperationPlanMock.mockResolvedValueOnce(plan);
    await store.authorizePlan(plan, "Reviewed global operation");

    expect(authorizeOperationPlanMock).toHaveBeenCalledWith(plan.id, plan.content_hash, "Reviewed global operation");
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
    store.investigations = [investigation(input.run_id, "alert")];

    const result = await store.proposeScenarioPlan(input);

    expect(result).toStrictEqual(plan);
    expect(proposeOperationPlanMock).toHaveBeenCalledWith(input);
    expect(getWorkspaceMock).toHaveBeenCalledOnce();
    expect(store.notice).toContain("不可变 Operation Plan");
  });
});
