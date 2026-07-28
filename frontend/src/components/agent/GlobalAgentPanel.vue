<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Bot, History, PanelRight, Pin, PinOff, SquareArrowOutUpRight, X } from "lucide-vue-next";
import { useRoute } from "vue-router";

import { useAgentWorkspaceStore } from "../../stores/agentWorkspace";
import { AGENT_CONTEXT_EVENT, AGENT_OPEN_EVENT, type AgentOpenRequest, type AgentPageContext } from "../../utils/agentContext";
import AgentConversation from "./AgentConversation.vue";
import AgentHistory from "./AgentHistory.vue";
import AgentInspector from "./AgentInspector.vue";

type PanelView = "history" | "conversation" | "inspector";

const route = useRoute();
const store = useAgentWorkspaceStore();
const open = ref(false);
const view = ref<PanelView>("conversation");
const pinned = ref(readPinned());
const drawerSize = computed(() => "min(96vw, 560px)");

function readPinned(): boolean {
  try {
    return window.localStorage.getItem("cloudops.agent.pinned") === "true";
  } catch {
    return false;
  }
}

function togglePinned() {
  pinned.value = !pinned.value;
  try {
    window.localStorage.setItem("cloudops.agent.pinned", String(pinned.value));
  } catch {
    // Pinning still applies for the current browser lifecycle.
  }
}

function receiveOpen(event: Event) {
  const detail = (event as CustomEvent<AgentOpenRequest>).detail ?? {};
  if (detail.context) store.setCurrentContext(detail.context);
  open.value = true;
  view.value = "conversation";
  void store.loadIndex().then(() => {
    if (detail.consultationId) void store.selectConsultation(detail.consultationId);
  });
}

function receiveContext(event: Event) {
  store.setCurrentContext((event as CustomEvent<AgentPageContext | null>).detail ?? null);
}

watch(() => route.fullPath, (value) => store.setRoute(value), { immediate: true });
onMounted(() => {
  window.addEventListener(AGENT_OPEN_EVENT, receiveOpen);
  window.addEventListener(AGENT_CONTEXT_EVENT, receiveContext);
  void store.loadIndex();
});
onBeforeUnmount(() => {
  window.removeEventListener(AGENT_OPEN_EVENT, receiveOpen);
  window.removeEventListener(AGENT_CONTEXT_EVENT, receiveContext);
  store.stopStream();
});
</script>

<template>
  <el-drawer
    v-model="open"
    class="global-agent-drawer"
    direction="rtl"
    :size="drawerSize"
    :append-to-body="true"
    :lock-scroll="!pinned"
    :modal="!pinned"
    :close-on-click-modal="!pinned"
    :close-on-press-escape="true"
    :show-close="false"
    title="全局 Agent 面板"
  >
    <template #header>
      <div class="agent-drawer-header">
        <span class="drawer-mark"><Bot :size="18" aria-hidden="true" /></span>
        <div><strong>Agent</strong><small>{{ store.consultation?.title || store.selectedRun?.objective || "CloudOps Workspace" }}</small></div>
        <RouterLink to="/agent" aria-label="打开完整 Agent Workspace" title="完整 Workspace" @click="open = false"><SquareArrowOutUpRight :size="17" aria-hidden="true" /></RouterLink>
        <button type="button" :aria-label="pinned ? '取消固定 Agent 面板' : '固定 Agent 面板'" :title="pinned ? '取消固定' : '固定'" @click="togglePinned"><PinOff v-if="pinned" :size="17" aria-hidden="true" /><Pin v-else :size="17" aria-hidden="true" /></button>
        <button type="button" aria-label="关闭 Agent 面板" title="关闭" @click="open = false"><X :size="18" aria-hidden="true" /></button>
      </div>
    </template>

    <nav class="agent-drawer-tabs" aria-label="全局 Agent 面板视图" role="tablist">
      <button type="button" role="tab" :aria-selected="view === 'history'" @click="view = 'history'"><History :size="15" aria-hidden="true" />记录</button>
      <button type="button" role="tab" :aria-selected="view === 'conversation'" @click="view = 'conversation'"><Bot :size="15" aria-hidden="true" />当前运行</button>
      <button type="button" role="tab" :aria-selected="view === 'inspector'" @click="view = 'inspector'"><PanelRight :size="15" aria-hidden="true" />上下文</button>
    </nav>
    <div class="agent-drawer-body" :data-view="view">
      <AgentHistory v-if="view === 'history'" compact />
      <AgentConversation v-else-if="view === 'conversation'" compact />
      <AgentInspector v-else compact />
    </div>
  </el-drawer>
</template>

<style scoped>
.agent-drawer-header { display: grid; width: 100%; min-width: 0; grid-template-columns: 34px minmax(0, 1fr) repeat(3, 34px); align-items: center; gap: var(--co-space-2); }
.drawer-mark { display: grid; width: 34px; height: 34px; place-items: center; border: 1px solid var(--co-status-info-border); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-status-info-bg); }
.agent-drawer-header > div { display: grid; min-width: 0; gap: 2px; }
.agent-drawer-header strong, .agent-drawer-header small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.agent-drawer-header strong { color: var(--co-text-primary); font-size: 13px; }
.agent-drawer-header small { color: var(--co-text-secondary); font-size: 9px; }
.agent-drawer-header button, .agent-drawer-header a { display: grid; width: 34px; height: 34px; place-items: center; padding: 0; border: 1px solid transparent; border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: transparent; cursor: pointer; }
.agent-drawer-header button:hover, .agent-drawer-header a:hover { border-color: var(--co-border-default); color: var(--co-text-primary); background: var(--co-bg-hover); }
.agent-drawer-tabs { display: grid; height: 44px; grid-template-columns: repeat(3, minmax(0, 1fr)); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.agent-drawer-tabs button { display: flex; min-width: 0; align-items: center; justify-content: center; gap: 5px; padding: 0 var(--co-space-2); border: 0; border-bottom: 2px solid transparent; color: var(--co-text-secondary); background: transparent; cursor: pointer; font-size: 10px; font-weight: 750; }
.agent-drawer-tabs button:hover { color: var(--co-text-primary); background: var(--co-bg-hover); }
.agent-drawer-tabs button[aria-selected="true"] { border-bottom-color: var(--co-action-primary); color: var(--co-action-primary); }
.agent-drawer-body { height: calc(100dvh - 101px); min-width: 0; min-height: 0; overflow: hidden; }
.agent-drawer-body > * { height: 100%; }
:global(.global-agent-drawer.el-drawer) { background: var(--co-bg-surface); box-shadow: var(--co-shadow-overlay); }
:global(.global-agent-drawer .el-drawer__header) { min-height: 57px; margin: 0; padding: 0 var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
:global(.global-agent-drawer .el-drawer__body) { padding: 0; overflow: hidden; overscroll-behavior: contain; }
@media (max-width: 767px) { :global(.global-agent-drawer.el-drawer) { width: 100vw !important; max-width: none; } .agent-drawer-body { height: calc(100dvh - 101px - env(safe-area-inset-bottom)); } }
</style>
