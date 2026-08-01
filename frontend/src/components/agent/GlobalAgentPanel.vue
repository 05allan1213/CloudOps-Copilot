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

const MIN_DOCK_WIDTH = 400;
const MAX_DOCK_WIDTH = 620;
const route = useRoute();
const store = useAgentWorkspaceStore();
const open = ref(false);
const view = ref<PanelView>("conversation");
const returnFocus = ref<HTMLElement | null>(null);
const panelHeading = ref<HTMLElement | null>(null);
const dockWidth = ref(readDockWidth());
let resizing = false;

const panelSubtitle = computed(() => store.consultation?.title || store.selectedRun?.objective || "当前页面的运维协作空间");
const runStatus = computed(() => store.selectedRun?.outcome || store.selectedRun?.status || "");
const isRunning = computed(() => runStatus.value === "pending" || runStatus.value === "running");
const contextLabel = computed(() => {
  const snapshot = store.activeSnapshot;
  const context = store.currentContext?.input;
  const cluster = snapshot?.scope.cluster_id || context?.cluster_id;
  const environment = snapshot?.scope.environment || context?.environment;
  return cluster ? `${cluster} · ${environment || "unknown"}` : "等待页面上下文";
});
const tabs = [
  { label: "对话", value: "conversation", icon: "i-lucide-message-square-text" },
  { label: "记录", value: "history", icon: "i-lucide-history" },
  { label: "证据", value: "inspector", icon: "i-lucide-fingerprint" },
];
const dockStyle = computed(() => ({ "--agent-dock-current-width": `${dockWidth.value}px` }));

function readDockWidth(): number {
  try {
    const value = Number(window.localStorage.getItem("cloudops.agent.dock-width"));
    if (Number.isFinite(value)) return Math.min(MAX_DOCK_WIDTH, Math.max(MIN_DOCK_WIDTH, value));
  } catch {
    // The default width remains deterministic when browser storage is unavailable.
  }
  return 480;
}

function rememberDockWidth() {
  try {
    window.localStorage.setItem("cloudops.agent.dock-width", String(dockWidth.value));
  } catch {
    // Resizing still applies for the current browser lifecycle.
  }
}

async function openPanel(request: AgentOpenRequest = {}) {
  const activeElement = document.activeElement;
  returnFocus.value = activeElement instanceof HTMLElement ? activeElement : null;
  if (request.context) store.setCurrentContext(request.context);
  open.value = true;
  view.value = "conversation";
  await nextTick();
  panelHeading.value?.focus({ preventScroll: true });
  await store.loadIndex();
  if (shouldStopGlobalAgent(open.value, route.path)) {
    store.stopStream();
    return;
  }
  if (request.consultationId) await store.selectConsultation(request.consultationId);
  else if (store.selection === "consultation" && store.selectedID && store.streamState === "stopped") {
    await store.selectConsultation(store.selectedID);
  }
}

function receiveOpen(event: Event) {
  void openPanel((event as CustomEvent<AgentOpenRequest>).detail ?? {});
}

function receiveContext(event: Event) {
  store.setCurrentContext((event as CustomEvent<AgentPageContext | null>).detail ?? null);
}

function changeView(value: string | number) {
  if (value === "history" || value === "conversation" || value === "inspector") view.value = value;
}

function closePanel(restoreFocus = true) {
  open.value = false;
  store.stopStream();
  if (restoreFocus) void nextTick(() => returnFocus.value?.isConnected && returnFocus.value.focus({ preventScroll: true }));
  returnFocus.value = null;
}

function enterFullWorkspace() {
  closePanel(false);
}

function resizeDock(event: PointerEvent) {
  if (!resizing) return;
  const available = window.innerWidth - Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--co-sidebar-rail-width") || "60") - 420;
  const max = Math.max(MIN_DOCK_WIDTH, Math.min(MAX_DOCK_WIDTH, available));
  dockWidth.value = Math.min(max, Math.max(MIN_DOCK_WIDTH, window.innerWidth - event.clientX));
}

function stopResize() {
  if (!resizing) return;
  resizing = false;
  document.body.classList.remove("is-resizing-agent-dock");
  window.removeEventListener("pointermove", resizeDock);
  window.removeEventListener("pointerup", stopResize);
  rememberDockWidth();
}

function beginResize(event: PointerEvent) {
  resizing = true;
  (event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
  document.body.classList.add("is-resizing-agent-dock");
  window.addEventListener("pointermove", resizeDock);
  window.addEventListener("pointerup", stopResize);
}

watch(() => route.fullPath, (value) => {
  store.setRoute(value);
  if (route.path === "/agent" && open.value) closePanel(false);
}, { immediate: true });

watch(open, (isOpen) => {
  if (shouldStopGlobalAgent(isOpen, route.path)) store.stopStream();
});

onMounted(() => {
  window.addEventListener(AGENT_OPEN_EVENT, receiveOpen);
  window.addEventListener(AGENT_CONTEXT_EVENT, receiveContext);
});

onBeforeUnmount(() => {
  stopResize();
  window.removeEventListener(AGENT_OPEN_EVENT, receiveOpen);
  window.removeEventListener(AGENT_CONTEXT_EVENT, receiveContext);
  store.stopStream();
});
</script>

<template>
  <div
    class="agent-dock-slot"
    :class="{ 'is-open': open && route.path !== '/agent' }"
    :style="dockStyle"
  >
    <Transition name="agent-dock">
      <aside
        v-if="open && route.path !== '/agent'"
        class="global-agent-dock"
        data-testid="global-agent-drawer"
        aria-label="全局 Agent 协作面板"
      >
        <button
          class="dock-resize-handle"
          type="button"
          aria-label="调整 Agent Dock 宽度"
          title="拖动调整 Agent Dock 宽度"
          @pointerdown="beginResize"
        >
          <span />
        </button>

        <header class="agent-dock-header">
          <span class="agent-symbol" aria-hidden="true">
            <UIcon name="i-lucide-sparkles" />
          </span>
          <div class="agent-dock-title">
            <span>CloudOps Agent</span>
            <strong ref="panelHeading" tabindex="-1">{{ panelSubtitle }}</strong>
          </div>
          <span
            class="agent-live-state"
            :class="{ 'is-running': isRunning }"
            role="status"
          >
            <i aria-hidden="true" />
            {{ isRunning ? "Working" : store.streamState === "connected" ? "Live" : "Ready" }}
          </span>
          <div class="agent-dock-actions">
            <UTooltip text="打开完整 Agent 工作台">
              <UButton
                color="neutral"
                variant="ghost"
                icon="i-lucide-maximize-2"
                square
                to="/agent"
                aria-label="打开完整 Agent 工作台"
                @click="enterFullWorkspace"
              />
            </UTooltip>
            <UTooltip text="关闭 Agent Dock">
              <UButton
                color="neutral"
                variant="ghost"
                icon="i-lucide-x"
                square
                aria-label="关闭 Agent Dock"
                @click="closePanel()"
              />
            </UTooltip>
          </div>
        </header>

        <div class="agent-context-line">
          <UIcon name="i-lucide-waypoints" aria-hidden="true" />
          <span>{{ contextLabel }}</span>
          <small>{{ store.contextReady ? "Evidence boundary retained" : "Read-only context" }}</small>
        </div>

        <UTabs
          class="agent-dock-tabs"
          :model-value="view"
          :items="tabs"
          :content="false"
          color="neutral"
          variant="pill"
          size="sm"
          @update:model-value="changeView"
        />

        <div class="agent-dock-body" :data-view="view">
          <AgentHistory v-if="view === 'history'" compact />
          <AgentConversation v-else-if="view === 'conversation'" compact />
          <AgentInspector v-else compact />
        </div>
      </aside>
    </Transition>

    <UTooltip
      text="打开 CloudOps Agent"
      :content="{ side: 'left' }"
    >
      <Transition name="agent-launcher">
        <UButton
          v-if="!open && route.path !== '/agent'"
          class="agent-launcher"
          color="neutral"
          variant="solid"
          icon="i-lucide-bot"
          square
          aria-label="打开 CloudOps Agent"
          @click="openPanel()"
        />
      </Transition>
    </UTooltip>
  </div>
</template>

<style scoped>
.agent-dock-slot {
  position: relative;
  z-index: calc(var(--co-z-header) + 2);
  width: 0;
  height: 100dvh;
  flex: 0 0 auto;
  transition: width 320ms cubic-bezier(.23, 1, .32, 1);
}
.agent-dock-slot.is-open { width: var(--agent-dock-current-width, var(--co-agent-dock-width)); }

.global-agent-dock {
  position: absolute;
  inset: 0 0 0 auto;
  display: grid;
  width: var(--agent-dock-current-width, var(--co-agent-dock-width));
  min-width: 0;
  min-height: 0;
  grid-template-rows: auto auto auto minmax(0, 1fr);
  overflow: hidden;
  border-left: 1px solid color-mix(in srgb, var(--co-border-strong) 72%, transparent);
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--co-bg-floating) 96%, transparent), var(--co-bg-canvas));
  box-shadow: -18px 0 52px rgb(45 39 33 / 13%);
}

.dock-resize-handle {
  position: absolute;
  top: 0;
  bottom: 0;
  left: -5px;
  z-index: 4;
  width: 10px;
  padding: 0;
  border: 0;
  background: transparent;
  cursor: col-resize;
}
.dock-resize-handle span {
  position: absolute;
  top: 50%;
  left: 3px;
  width: 3px;
  height: 54px;
  border-radius: 999px;
  background: var(--co-border-strong);
  opacity: 0;
  transform: translateY(-50%);
  transition: opacity var(--co-motion-fast) var(--co-ease-out);
}
.dock-resize-handle:hover span,
.dock-resize-handle:focus-visible span { opacity: 1; }

.agent-dock-header {
  display: grid;
  min-width: 0;
  min-height: 72px;
  grid-template-columns: auto minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--co-border-default);
  background: color-mix(in srgb, var(--co-bg-floating) 82%, transparent);
  backdrop-filter: blur(var(--co-glass-blur));
}
.agent-symbol {
  display: grid;
  width: 38px;
  height: 38px;
  place-items: center;
  border: 1px solid var(--co-border-strong);
  border-radius: 12px;
  color: var(--co-bg-canvas);
  background: var(--co-ink-action);
  box-shadow: 0 10px 24px color-mix(in srgb, var(--co-ink-action) 20%, transparent);
}
.agent-symbol :deep(svg) { width: 18px; height: 18px; }
.agent-dock-title { display: grid; min-width: 0; gap: 2px; }
.agent-dock-title span {
  color: var(--co-text-muted);
  font-family: var(--co-font-mono);
  font-size: 8px;
  font-weight: 800;
  letter-spacing: .11em;
  text-transform: uppercase;
}
.agent-dock-title strong {
  overflow: hidden;
  color: var(--co-text-primary);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.agent-live-state {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--co-text-muted);
  font-family: var(--co-font-mono);
  font-size: 8px;
  font-weight: 800;
  text-transform: uppercase;
}
.agent-live-state i,
.launcher-state {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--co-viz-live);
  box-shadow: 0 0 0 4px var(--co-viz-live-soft);
}
.agent-live-state.is-running i,
.launcher-state.is-running {
  background: var(--co-viz-amber);
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--co-viz-amber) 14%, transparent);
  animation: agent-status-breathe 1.8s ease-in-out infinite;
}
.agent-dock-actions { display: flex; align-items: center; gap: 2px; }
.agent-dock-actions :deep(button),
.agent-dock-actions :deep(a) { width: 34px; min-width: 34px; height: 34px; border-radius: 9px; }

.agent-context-line {
  display: grid;
  min-width: 0;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--co-border-default);
  color: var(--co-text-secondary);
  background: color-mix(in srgb, var(--co-bg-surface) 72%, transparent);
  font-size: 10px;
}
.agent-context-line :deep(svg) { width: 14px; height: 14px; color: var(--co-viz-live); }
.agent-context-line span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.agent-context-line small { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; }

.agent-dock-tabs {
  padding: 8px 12px;
  border-bottom: 1px solid var(--co-border-default);
  background: color-mix(in srgb, var(--co-bg-floating) 66%, transparent);
}
.agent-dock-body { min-width: 0; min-height: 0; overflow: hidden; }
.agent-dock-body > * { height: 100%; }

.agent-launcher {
  position: fixed;
  right: 22px;
  bottom: 20px;
  z-index: calc(var(--co-z-overlay) - 1);
  display: inline-flex;
  width: 44px;
  min-width: 44px;
  height: 44px;
  min-height: 44px;
  align-items: center;
  justify-content: center;
  gap: 0;
  padding: 0 !important;
  border: 1px solid color-mix(in srgb, var(--co-text-primary) 28%, transparent);
  border-radius: 12px;
  color: var(--co-bg-canvas);
  background: var(--co-ink-action);
  box-shadow: 0 13px 32px color-mix(in srgb, var(--co-ink-action) 24%, transparent);
}
.agent-launcher:hover { transform: translateY(-2px); box-shadow: 0 17px 38px color-mix(in srgb, var(--co-ink-action) 28%, transparent); }
.agent-launcher > * { margin: 0 !important; }
.agent-launcher :deep(svg) { width: 20px; height: 20px; }

.agent-dock-enter-active,
.agent-dock-leave-active { transition: opacity 220ms ease, transform 320ms cubic-bezier(.23, 1, .32, 1); }
.agent-dock-enter-from,
.agent-dock-leave-to { opacity: 0; transform: translateX(28px); }
.agent-launcher-enter-active,
.agent-launcher-leave-active { transition: opacity 180ms ease, transform 220ms cubic-bezier(.23, 1, .32, 1); }
.agent-launcher-enter-from,
.agent-launcher-leave-to { opacity: 0; transform: translateY(10px) scale(.96); }

@keyframes agent-status-breathe {
  50% { opacity: .45; }
}
:global(body.is-resizing-agent-dock) { cursor: col-resize; user-select: none; }
@media (prefers-reduced-motion: reduce) {
  .agent-dock-slot,
  .agent-dock-enter-active,
  .agent-dock-leave-active,
  .agent-launcher-enter-active,
  .agent-launcher-leave-active { transition: none; }
  .agent-live-state.is-running i,
  .launcher-state.is-running { animation: none; }
}
</style>
