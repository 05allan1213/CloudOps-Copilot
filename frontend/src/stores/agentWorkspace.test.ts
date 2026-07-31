import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AgentContextInput, AgentRun, AgentStreamEvent, ConsultationDetail } from "../api/agent";

const mocks = vi.hoisted(() => ({
  attachAgentSnapshot: vi.fn(),
  authorizeActionCard: vi.fn(),
  authorizeOperationPlan: vi.fn(),
  cancelAgentConsultation: vi.fn(),
  cancelAgentInvestigation: vi.fn(),
  createAgentConsultation: vi.fn(),
  createKnowledgeItem: vi.fn(),
  getAgentConsultation: vi.fn(),
  getAgentConsultations: vi.fn(),
  getAgentInvestigation: vi.fn(),
  getAgentInvestigations: vi.fn(),
  getKnowledgeItems: vi.fn(),
  getOperationPlans: vi.fn(),
  getRunbookGuidance: vi.fn(),
  openAgentEventStream: vi.fn(),
  sendAgentMessage: vi.fn(),
  updateKnowledgeItem: vi.fn(),
  streamEvent: undefined as ((event: AgentStreamEvent) => void) | undefined,
  streamError: undefined as (() => void) | undefined,
  streamOpen: undefined as (() => void) | undefined,
  streamClose: vi.fn(),
}));

vi.mock("../api/agent", async (importOriginal) => ({
  ...await importOriginal<typeof import("../api/agent")>(),
  attachAgentSnapshot: mocks.attachAgentSnapshot,
  authorizeActionCard: mocks.authorizeActionCard,
  authorizeOperationPlan: mocks.authorizeOperationPlan,
  cancelAgentConsultation: mocks.cancelAgentConsultation,
  cancelAgentInvestigation: mocks.cancelAgentInvestigation,
  createAgentConsultation: mocks.createAgentConsultation,
  createKnowledgeItem: mocks.createKnowledgeItem,
  getAgentConsultation: mocks.getAgentConsultation,
  getAgentConsultations: mocks.getAgentConsultations,
  getAgentInvestigation: mocks.getAgentInvestigation,
  getAgentInvestigations: mocks.getAgentInvestigations,
  getKnowledgeItems: mocks.getKnowledgeItems,
  getOperationPlans: mocks.getOperationPlans,
  getRunbookGuidance: mocks.getRunbookGuidance,
  openAgentEventStream: mocks.openAgentEventStream,
  sendAgentMessage: mocks.sendAgentMessage,
  updateKnowledgeItem: mocks.updateKnowledgeItem,
}));

import { useAgentWorkspaceStore } from "./agentWorkspace";

const observedAt = "2026-07-31T06:00:00Z";

function runFixture(): AgentRun {
  return {
    id: "run-1",
    subject_type: "consultation",
    consultation_id: "consultation-1",
    configuration_revision_id: "configuration-1",
    context_snapshot_id: "snapshot-1",
    status: "running",
    uncertainty: "bounded",
    objective: "Inspect current Evidence",
    prompt_version: "agent/v1",
    created_at: observedAt,
    updated_at: observedAt,
    evidence_count: 0,
    steps: [],
    evidence_citations: [],
    guidance_citations: [],
    action_cards: [],
    operation_plans: [],
  };
}

function consultationFixture(): ConsultationDetail {
  const scope = { name: "CloudOps", cluster_id: "cloudops-local", environment: "local", namespaces: ["cloudops-system"], active: true };
  return {
    id: "consultation-1",
    title: "Current Evidence",
    status: "open",
    active_snapshot_id: "snapshot-1",
    active_run: runFixture(),
    scope,
    message_count: 0,
    created_at: observedAt,
    updated_at: observedAt,
    snapshots: [{
      id: "snapshot-1",
      consultation_id: "consultation-1",
      subject_type: "consultation",
      configuration_revision_id: "configuration-1",
      scope,
      resource_refs: [{ id: "resource-1", kind: "Deployment", namespace: "cloudops-system", name: "cloudops-api" }],
      filters: {},
      time_range: { from: "2026-07-31T05:00:00Z", to: observedAt },
      query_definition_refs: [],
      query_execution_refs: ["query-1"],
      evidence_refs: [],
      content_hash: "snapshot-hash",
      created_at: observedAt,
    }],
    messages: [],
  };
}

function contextFixture(): AgentContextInput {
  return {
    title: "Current Evidence",
    cluster_id: "cloudops-local",
    environment: "local",
    namespaces: ["cloudops-system"],
    resource_refs: [{ id: "resource-1", kind: "Deployment", namespace: "cloudops-system", name: "cloudops-api" }],
    from: "2026-07-31T05:00:00Z",
    to: observedAt,
    query_definition_refs: [],
    query_execution_refs: ["query-1"],
    evidence_refs: [],
  };
}

function streamEvent(id: string, delta: string): AgentStreamEvent {
  return {
    id,
    run_id: "run-1",
    consultation_id: "consultation-1",
    sequence: Number(id.replace(/\D/g, "")) || 1,
    type: "answer.delta",
    payload: { delta },
    created_at: observedAt,
  };
}

beforeEach(() => {
  setActivePinia(createPinia());
  vi.clearAllMocks();
  mocks.getAgentConsultations.mockResolvedValue([]);
  mocks.getAgentInvestigations.mockResolvedValue([]);
  mocks.getKnowledgeItems.mockResolvedValue([]);
  mocks.getRunbookGuidance.mockResolvedValue([]);
  mocks.getOperationPlans.mockResolvedValue([]);
  mocks.openAgentEventStream.mockImplementation((_id, onEvent, onError, onOpen) => {
    mocks.streamEvent = onEvent;
    mocks.streamError = onError;
    mocks.streamOpen = onOpen;
    return mocks.streamClose;
  });
});

afterEach(() => {
  useAgentWorkspaceStore().teardown();
  vi.useRealTimers();
});

describe("Agent Workspace store", () => {
  it("deduplicates a bounded stream, reports reconnect state, and tears down ownership", () => {
    vi.useFakeTimers();
    const store = useAgentWorkspaceStore();
    store.selection = "consultation";
    store.selectedID = "consultation-1";
    store.consultation = consultationFixture();
    vi.spyOn(store, "refreshSelection").mockResolvedValue();

    store.startStream("consultation-1");
    expect(store.streamState).toBe("connecting");
    mocks.streamOpen?.();
    expect(store.streamState).toBe("connected");

    const event = streamEvent("event-1", "bounded ");
    mocks.streamEvent?.(event);
    mocks.streamEvent?.(event);
    expect(store.liveAnswer).toBe("bounded ");
    expect(store.streamCursor).toBe("event-1");
    expect(store.duplicateEvents).toBe(1);

    mocks.streamError?.();
    mocks.streamError?.();
    mocks.streamError?.();
    expect(store.streamState).toBe("disconnected");
    expect(store.streamReconnects).toBe(3);

    store.teardown();
    vi.runAllTimers();
    expect(mocks.streamClose).toHaveBeenCalledOnce();
    expect(store.refreshSelection).not.toHaveBeenCalled();
    expect(store.streamState).toBe("stopped");
  });

  it("reuses the same Idempotency-Key when identical message content is retried", async () => {
    const store = useAgentWorkspaceStore();
    store.selection = "consultation";
    store.selectedID = "consultation-1";
    store.consultation = consultationFixture();
    vi.spyOn(store, "refreshSelection").mockResolvedValue();
    mocks.sendAgentMessage
      .mockRejectedValueOnce(new Error("temporary failure"))
      .mockResolvedValueOnce({ message: {}, run: runFixture() });

    await store.sendMessage("inspect the current evidence");
    const firstKey = mocks.sendAgentMessage.mock.calls[0][2];
    expect(store.pendingMessageContent).toBe("inspect the current evidence");
    await store.sendMessage("  inspect the current evidence  ");

    expect(mocks.sendAgentMessage).toHaveBeenCalledTimes(2);
    expect(mocks.sendAgentMessage.mock.calls[1][2]).toBe(firstKey);
    expect(store.pendingMessageIdempotencyKey).toBe("");
  });

  it("fails closed before creating a Consultation without Query or Evidence references", async () => {
    const store = useAgentWorkspaceStore();
    const context = { ...contextFixture(), query_execution_refs: [], evidence_refs: [] };

    await expect(store.createConsultation(context, "free")).resolves.toBe(false);

    expect(mocks.createAgentConsultation).not.toHaveBeenCalled();
    expect(store.failure?.code).toBe("CONTEXT_NOT_READY");
    expect(store.error).toContain("query execution");
  });
});
