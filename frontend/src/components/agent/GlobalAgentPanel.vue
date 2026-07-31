<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";

import { useAgentWorkspaceStore } from "../../stores/agentWorkspace";
import {
  AGENT_CONTEXT_EVENT,
  AGENT_OPEN_EVENT,
  shouldStopGlobalAgent,
  type AgentOpenRequest,
  type AgentPageContext,
} from "../../utils/agentContext";
import AgentConversation from "./AgentConversation.vue";
import AgentHistory from "./AgentHistory.vue";
import AgentInspector from "./AgentInspector.vue";

type PanelView = "history" | "conversation" | "inspector";

const route = useRoute();
const store = useAgentWorkspaceStore();
const open = ref(false);
const view = ref<PanelView>("conversation");
const pinned = ref(readPinned());
const returnFocus = ref<HTMLElement | null>(null);
const restoreFocusOnClose = ref(true);
const panelHeading = ref<HTMLElement | null>(null);
const panelSubtitle = computed(() => store.consultation?.title || store.selectedRun?.objective || "CloudOps Workspace");
const tabs = [
  { label: "记录", value: "history", icon: "i-lucide-history" },
  { label: "当前运行", value: "conversation", icon: "i-lucide-bot" },
  { label: "上下文", value: "inspector", icon: "i-lucide-panel-right" },
];
const slideoverUI = {
  overlay: "global-agent-slideover-overlay",
  content: "global-agent-slideover",
  header: "global-agent-slideover-header",
  body: "global-agent-slideover-body",
};

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
  const activeElement = document.activeElement;
  returnFocus.value = activeElement instanceof HTMLElement ? activeElement : null;
  restoreFocusOnClose.value = true;
  if (detail.context) store.setCurrentContext(detail.context);
  open.value = true;
  view.value = "conversation";
  void store.loadIndex().then(() => {
    if (shouldStopGlobalAgent(open.value, route.path)) {
      store.stopStream();
      return;
    }
    if (detail.consultationId) void store.selectConsultation(detail.consultationId);
    else if (store.selection === "consultation" && store.selectedID && store.streamState === "stopped") {
      void store.selectConsultation(store.selectedID);
    }
  });
}

function receiveContext(event: Event) {
  store.setCurrentContext((event as CustomEvent<AgentPageContext | null>).detail ?? null);
}

function changeView(value: string | number) {
  if (value === "history" || value === "conversation" || value === "inspector") view.value = value;
}

function focusPanelHeading() {
  void nextTick(() => panelHeading.value?.focus({ preventScroll: true }));
}

function restoreTriggerFocus() {
  if (restoreFocusOnClose.value && returnFocus.value?.isConnected) {
    returnFocus.value.focus({ preventScroll: true });
  }
  restoreFocusOnClose.value = true;
  returnFocus.value = null;
}

function enterFullWorkspace() {
  restoreFocusOnClose.value = false;
  open.value = false;
}

watch(() => route.fullPath, (value) => store.setRoute(value), { immediate: true });
watch([open, () => route.path], ([isOpen, path]) => {
  if (shouldStopGlobalAgent(isOpen, path)) store.stopStream();
});

onMounted(() => {
  window.addEventListener(AGENT_OPEN_EVENT, receiveOpen);
  window.addEventListener(AGENT_CONTEXT_EVENT, receiveContext);
});

onBeforeUnmount(() => {
  window.removeEventListener(AGENT_OPEN_EVENT, receiveOpen);
  window.removeEventListener(AGENT_CONTEXT_EVENT, receiveContext);
  store.stopStream();
});
</script>

<template>
  <USlideover
    v-model:open="open"
    title="Agent"
    :description="panelSubtitle"
    side="right"
    :modal="!pinned"
    :overlay="!pinned"
    :close="false"
    :ui="slideoverUI"
    @after:enter="focusPanelHeading"
    @after:leave="restoreTriggerFocus"
  >
    <template #title>
      <span
        ref="panelHeading"
        class="agent-panel-title"
        tabindex="-1"
      >
        <UIcon
          name="i-lucide-bot"
          aria-hidden="true"
        />
        Agent
      </span>
    </template>
    <template #description>
      <span class="agent-panel-subtitle">{{ panelSubtitle }}</span>
    </template>
    <template #actions>
      <UTooltip text="打开完整 Agent Workspace">
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-square-arrow-out-up-right"
          square
          to="/agent"
          aria-label="打开完整 Agent Workspace"
          @click="enterFullWorkspace"
        />
      </UTooltip>
      <UTooltip :text="pinned ? '取消固定 Agent 面板' : '固定 Agent 面板'">
        <UButton
          color="neutral"
          variant="ghost"
          :icon="pinned ? 'i-lucide-pin-off' : 'i-lucide-pin'"
          square
          :aria-label="pinned ? '取消固定 Agent 面板' : '固定 Agent 面板'"
          :aria-pressed="pinned"
          @click="togglePinned"
        />
      </UTooltip>
      <UTooltip text="关闭 Agent 面板">
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-x"
          square
          aria-label="关闭 Agent 面板"
          @click="open = false"
        />
      </UTooltip>
    </template>

    <template #body>
      <section
        class="global-agent-panel"
        data-testid="global-agent-drawer"
        aria-label="全局 Agent 面板内容"
      >
        <UTabs
          class="agent-panel-tabs"
          :model-value="view"
          :items="tabs"
          :content="false"
          color="primary"
          variant="link"
          size="sm"
          @update:model-value="changeView"
        />
        <div
          class="agent-panel-body"
          :data-view="view"
        >
          <AgentHistory
            v-if="view === 'history'"
            compact
          />
          <AgentConversation
            v-else-if="view === 'conversation'"
            compact
          />
          <AgentInspector
            v-else
            compact
          />
        </div>
      </section>
    </template>
  </USlideover>
</template>

<style scoped>
.agent-panel-title {
  display: inline-flex;
  align-items: center;
  gap: var(--co-space-2);
  color: var(--co-text-primary);
  font-size: 14px;
}

.agent-panel-title svg { width: 18px; height: 18px; color: var(--co-action-primary); }
.agent-panel-subtitle { display: block; max-width: 240px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.global-agent-panel { display: grid; height: 100%; min-width: 0; min-height: 0; grid-template-rows: auto minmax(0, 1fr); }
.agent-panel-tabs { border-bottom: 1px solid var(--co-border-default); }
.agent-panel-body { min-width: 0; min-height: 0; overflow: hidden; }
.agent-panel-body > * { height: 100%; }

:global(.global-agent-slideover) {
  width: min(560px, calc(100vw - var(--co-sidebar-rail-width)));
  max-width: none;
  border-left: 1px solid var(--co-border-default);
  background: var(--co-bg-overlay);
  box-shadow: var(--co-shadow-overlay);
  z-index: calc(var(--co-z-overlay) + 1);
}

:global(.global-agent-slideover-overlay) { z-index: var(--co-z-overlay); }

:global(.global-agent-slideover-header) {
  min-height: 58px;
  gap: var(--co-space-2);
  padding: 0 var(--co-space-3);
  border-bottom: 1px solid var(--co-border-default);
}

:global(.global-agent-slideover-body) {
  min-height: 0;
  padding: 0;
  overflow: hidden;
  overscroll-behavior: contain;
}
</style>
