<script setup lang="ts">
import { computed, onMounted, watch } from "vue";
import { Bot, Database, History, PanelRight, ShieldCheck } from "lucide-vue-next";
import { useRoute, useRouter } from "vue-router";

import AgentConversation from "../../components/agent/AgentConversation.vue";
import AgentHistory from "../../components/agent/AgentHistory.vue";
import AgentInspector from "../../components/agent/AgentInspector.vue";
import { useAgentWorkspaceStore } from "../../stores/agentWorkspace";

type MobilePanel = "history" | "conversation" | "inspector";

const route = useRoute();
const router = useRouter();
const store = useAgentWorkspaceStore();
const activePanel = computed<MobilePanel>(() => {
  const value = route.query.agent_view;
  return value === "history" || value === "inspector" ? value : "conversation";
});
const currentEvidence = computed(() => store.selectedRun?.evidence_citations.length ?? 0);
const pendingAuthority = computed(() => (store.selectedRun?.action_cards.filter((item) => item.status === "proposed").length ?? 0)
  + (store.selectedRun?.operation_plans.filter((item) => item.status === "proposed").length ?? 0));

function selectPanel(panel: MobilePanel) {
  void router.replace({ query: { ...route.query, agent_view: panel === "conversation" ? undefined : panel } });
}

function queryValue(value: unknown): string {
  const raw = Array.isArray(value) ? value[0] : value;
  return typeof raw === "string" ? raw : "";
}

async function loadWorkspace() {
  await store.loadIndex(false, queryValue(route.query.investigation));
}

watch(() => route.fullPath, (value) => store.setRoute(value), { immediate: true });
watch(() => route.query.investigation, (value) => {
  if (store.loaded) void store.selectInvestigationFromRoute(queryValue(value));
});
watch(() => store.loaded, (loaded) => {
  if (loaded) void store.selectInvestigationFromRoute(queryValue(route.query.investigation));
}, { immediate: true });
onMounted(() => void loadWorkspace());
</script>

<template>
  <section class="agent-workspace" aria-labelledby="agent-workspace-heading">
    <header class="workspace-heading">
      <div class="workspace-title">
        <span class="workspace-mark"><Bot :size="20" aria-hidden="true" /></span>
        <div><span class="section-kicker">CloudOps Agent</span><h1 id="agent-workspace-heading">Agent Workspace</h1></div>
      </div>
      <dl class="workspace-stats">
        <div><dt>Consultations</dt><dd>{{ store.consultations.length }}</dd></div>
        <div><dt>Investigations</dt><dd>{{ store.investigations.length }}</dd></div>
        <div><dt>Current Evidence</dt><dd>{{ currentEvidence }}</dd></div>
        <div><dt>Pending Authority</dt><dd>{{ pendingAuthority }}</dd></div>
      </dl>
    </header>

    <nav class="mobile-agent-tabs" aria-label="Agent Workspace 视图" role="tablist">
      <button type="button" role="tab" :aria-selected="activePanel === 'history'" @click="selectPanel('history')"><History :size="16" aria-hidden="true" />记录</button>
      <button type="button" role="tab" :aria-selected="activePanel === 'conversation'" @click="selectPanel('conversation')"><Bot :size="16" aria-hidden="true" />当前运行</button>
      <button type="button" role="tab" :aria-selected="activePanel === 'inspector'" @click="selectPanel('inspector')"><PanelRight :size="16" aria-hidden="true" />证据与权限</button>
    </nav>

    <div class="workspace-grid" :data-mobile-panel="activePanel">
      <AgentHistory class="workspace-history" />
      <AgentConversation class="workspace-conversation" />
      <AgentInspector class="workspace-inspector" />
    </div>

    <div class="workspace-contract" aria-hidden="true"><Database :size="14" />Durable MySQL<ShieldCheck :size="14" />Exact Authority</div>
  </section>
</template>

<style scoped>
.agent-workspace { position: relative; display: grid; height: calc(100dvh - var(--co-header-height)); min-width: 0; min-height: 620px; grid-template-rows: 72px minmax(0, 1fr); overflow: hidden; background: var(--co-bg-canvas); }
.workspace-heading { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-5); padding: 0 var(--co-space-5); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.workspace-title { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); }
.workspace-mark { display: grid; width: 38px; height: 38px; flex: 0 0 38px; place-items: center; border: 1px solid var(--co-status-info-border); border-radius: var(--co-radius-panel); color: var(--co-action-primary); background: var(--co-status-info-bg); }
.section-kicker { color: var(--co-text-secondary); font-size: 10px; font-weight: 800; text-transform: uppercase; }
.workspace-title h1 { margin: 1px 0 0; font-size: 18px; letter-spacing: 0; }
.workspace-stats { display: flex; min-width: 0; align-items: center; gap: var(--co-space-5); margin: 0; }
.workspace-stats div { display: grid; min-width: 70px; gap: 2px; }
.workspace-stats dt { color: var(--co-text-secondary); font-size: 9px; text-transform: uppercase; }
.workspace-stats dd { margin: 0; color: var(--co-text-primary); font-size: 14px; font-weight: 800; font-variant-numeric: tabular-nums; }
.workspace-grid { display: grid; min-width: 0; min-height: 0; grid-template-columns: minmax(240px, 290px) minmax(420px, 1fr) minmax(300px, 350px); }
.mobile-agent-tabs { display: none; }
.workspace-contract { position: absolute; right: var(--co-space-3); bottom: var(--co-space-2); display: flex; align-items: center; gap: 5px; pointer-events: none; color: var(--co-text-secondary); font-size: 8px; }
@media (max-width: 1180px) { .workspace-stats div:nth-child(-n+2) { display: none; } .workspace-grid { grid-template-columns: 240px minmax(380px, 1fr) 300px; } }
@media (max-width: 900px) { .workspace-heading { padding-inline: var(--co-space-4); } .workspace-stats { gap: var(--co-space-3); } .workspace-grid { grid-template-columns: 220px minmax(360px, 1fr); } .workspace-inspector { display: none; } }
@media (max-width: 767px) {
  .agent-workspace { height: calc(100dvh - var(--co-header-height) - 58px - env(safe-area-inset-bottom)); min-height: 0; grid-template-rows: 58px 46px minmax(0, 1fr); }
  .workspace-heading { min-height: 58px; }
  .workspace-mark { width: 32px; height: 32px; flex-basis: 32px; }
  .workspace-title h1 { font-size: 15px; }
  .workspace-stats { display: none; }
  .mobile-agent-tabs { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
  .mobile-agent-tabs button { display: flex; min-width: 0; align-items: center; justify-content: center; gap: 5px; padding: 0 4px; border: 0; border-bottom: 2px solid transparent; color: var(--co-text-secondary); background: transparent; cursor: pointer; font-size: 10px; font-weight: 750; }
  .mobile-agent-tabs button:hover { color: var(--co-text-primary); background: var(--co-bg-hover); }
  .mobile-agent-tabs button[aria-selected="true"] { border-bottom-color: var(--co-action-primary); color: var(--co-action-primary); }
  .workspace-grid { display: block; min-height: 0; overflow: hidden; }
  .workspace-history, .workspace-conversation, .workspace-inspector { display: none; height: 100%; }
  .workspace-grid[data-mobile-panel="history"] .workspace-history, .workspace-grid[data-mobile-panel="conversation"] .workspace-conversation, .workspace-grid[data-mobile-panel="inspector"] .workspace-inspector { display: grid; }
  .workspace-grid[data-mobile-panel="inspector"] .workspace-inspector { display: block; }
  .workspace-contract { display: none; }
}
</style>
