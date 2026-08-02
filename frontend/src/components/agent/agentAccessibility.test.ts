import { createPinia } from "pinia";
import { renderToString } from "@vue/server-renderer";
import UApp from "@nuxt/ui/components/App.vue";
import { createSSRApp, defineComponent } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { describe, expect, it, vi } from "vitest";

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
      content: "## Bounded result\n\n- Evidence retained\n- `Provider writes`: not run",
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
      components: { AgentConversation, AgentHistory, AgentInspector, UApp },
      template: `
        <UApp>
          <main>
            <AgentHistory />
            <AgentConversation />
            <AgentInspector />
            <AgentHistory compact />
            <AgentConversation compact />
            <AgentInspector compact />
          </main>
        </UApp>
      `,
    });
    const pinia = createPinia();
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
    });
    await router.push("/agent");
    await router.isReady();
    const app = createSSRApp(root).use(pinia).use(router);
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
    expect(html).toContain("<h2>Bounded result</h2>");
    expect(html).toContain("<ul>");
  });

  it("honors an exact Investigation Context Link selection", async () => {
    const pinia = createPinia();
    const store = useAgentWorkspaceStore(pinia);
    const run = agentFixture().active_run!;
    store.investigations = [run];
    store.selection = "investigation";
    store.selectedID = run.id;
    store.investigation = run;

    await expect(store.selectInvestigationFromRoute(run.id)).resolves.toBe(true);
    await expect(store.selectInvestigationFromRoute("missing-run")).resolves.toBe(false);
    expect(store.error).toContain("不在当前持久化索引中");
  });

  it("honors a preferred Investigation after the global Agent panel preloads the index", async () => {
    const pinia = createPinia();
    const store = useAgentWorkspaceStore(pinia);
    const selectFromRoute = vi.spyOn(store, "selectInvestigationFromRoute").mockResolvedValue(true);
    store.loaded = true;

    await store.loadIndex(false, "run-from-context-link");

    expect(selectFromRoute).toHaveBeenCalledOnce();
    expect(selectFromRoute).toHaveBeenCalledWith("run-from-context-link");
  });
});
