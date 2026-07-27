import { createPinia } from "pinia";
import { renderToString } from "@vue/server-renderer";
import { createSSRApp, defineComponent } from "vue";
import { describe, expect, it } from "vitest";

import type { AgentContextSnapshot, AgentRun, ConsultationDetail } from "../../api/agent";
import { useAgentWorkspaceStore } from "../../stores/agentWorkspace";
import AgentConversation from "./AgentConversation.vue";
import AgentHistory from "./AgentHistory.vue";
import AgentInspector from "./AgentInspector.vue";

const observedAt = "2026-07-27T06:00:00Z";

function agentFixture(): ConsultationDetail {
  const scope = {
    name: "Local CloudOps",
    cluster_id: "cloudops-local",
    environment: "local",
    namespaces: ["cloudops-system"],
    active: true,
  };
  const snapshot: AgentContextSnapshot = {
    id: "snapshot-1",
    consultation_id: "consultation-1",
    subject_type: "consultation",
    configuration_revision_id: "configuration-1",
    scope,
    resource_refs: [],
    filters: {},
    time_range: { from: observedAt, to: observedAt },
    query_definition_refs: [],
    query_execution_refs: [],
    evidence_refs: [],
    content_hash: "snapshot-hash",
    created_at: observedAt,
  };
  const run: AgentRun = {
    id: "run-1",
    subject_type: "consultation",
    consultation_id: "consultation-1",
    configuration_revision_id: "configuration-1",
    context_snapshot_id: snapshot.id,
    status: "completed",
    outcome: "insufficient",
    uncertainty: "high",
    objective: "Inspect the bounded operational context",
    answer: "The model provider is disabled.",
    prompt_version: "agent-workspace/v1",
    failure_code: "MODEL_PROVIDER_DISABLED",
    started_at: observedAt,
    completed_at: observedAt,
    created_at: observedAt,
    updated_at: observedAt,
    evidence_count: 0,
    steps: [{
      id: "step-1",
      sequence: 1,
      type: "tool",
      tool: "logs.query",
      target: "cloudops-api",
      scope: {},
      status: "completed",
      result_summary: "Bounded query completed",
      duration_ms: 12,
      started_at: observedAt,
      finished_at: observedAt,
      created_at: observedAt,
    }],
    evidence_citations: [],
    guidance_citations: [],
    action_cards: [],
    operation_plans: [],
  };

  return {
    id: "consultation-1",
    title: "Logs Consultation",
    status: "open",
    active_snapshot_id: snapshot.id,
    active_run: run,
    scope,
    message_count: 1,
    created_at: observedAt,
    updated_at: observedAt,
    snapshots: [snapshot],
    messages: [{
      id: "message-1",
      consultation_id: "consultation-1",
      run_id: run.id,
      context_snapshot_id: snapshot.id,
      sequence: 1,
      role: "assistant",
      content: "Bounded result",
      status: "completed",
      created_at: observedAt,
      completed_at: observedAt,
      evidence_citations: [],
      guidance_citations: [],
    }],
  };
}

describe("Agent Workspace accessibility IDs", () => {
  it("keeps full and compact Agent surfaces uniquely labelled", async () => {
    const root = defineComponent({
      components: { AgentConversation, AgentHistory, AgentInspector },
      template: `
        <main>
          <AgentHistory />
          <AgentConversation />
          <AgentInspector />
          <AgentHistory compact />
          <AgentConversation compact />
          <AgentInspector compact />
        </main>
      `,
    });
    const pinia = createPinia();
    const app = createSSRApp(root).use(pinia);
    const store = useAgentWorkspaceStore(pinia);
    store.selectedID = "consultation-1";
    store.selection = "consultation";
    store.consultation = agentFixture();
    store.loaded = true;

    const html = await renderToString(app);
    const ids = [...html.matchAll(/\sid="([^"]+)"/g)].map((match) => match[1]);
    const idSet = new Set(ids);
    const references = [
      ...html.matchAll(/\saria-labelledby="([^"]+)"/g),
      ...html.matchAll(/<label[^>]+\sfor="([^"]+)"/g),
    ].flatMap((match) => match[1].split(/\s+/));

    expect(ids.length).toBe(idSet.size);
    expect([...idSet]).toEqual(expect.arrayContaining([
      "agent-history-heading",
      "global-agent-history-heading",
      "agent-conversation-heading",
      "global-agent-conversation-heading",
      "agent-conversation-consultation-tools-heading",
      "global-agent-conversation-consultation-tools-heading",
      "agent-conversation-message",
      "global-agent-conversation-message",
    ]));
    for (const reference of references) expect(idSet.has(reference), `${reference} must resolve to an element`).toBe(true);
  });
});
