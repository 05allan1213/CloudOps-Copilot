<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import type { AgentContextInput } from "../../api/agent";
import AgentConversation from "../../components/agent/AgentConversation.vue";
import AgentHistory from "../../components/agent/AgentHistory.vue";
import AgentInspector from "../../components/agent/AgentInspector.vue";
import WorkspacePageFrame from "../../components/workspace/WorkspacePageFrame.vue";
import { useAgentWorkspaceStore } from "../../stores/agentWorkspace";
import { freeQueryContext, readAgentRouteSelection } from "../../utils/agentContext";

type EntryMode = "context" | "structured" | "free";

const route = useRoute();
const router = useRouter();
const store = useAgentWorkspaceStore();
const historyCollapsed = ref(false);
const inspectorCollapsed = ref(false);
const historyTouched = ref(false);
const inspectorTouched = ref(false);
const entryOpen = ref(false);
const entryMode = ref<EntryMode>("context");
const entryTitle = ref("");
const freeQuestion = ref("");

const currentEvidence = computed(() => store.selectedRun?.evidence_citations.length ?? store.activeSnapshot?.evidence_refs.length ?? 0);
const pendingAuthority = computed(() => (store.selectedRun?.action_cards.filter((item) => item.status === "proposed").length ?? 0)
  + (store.selectedRun?.operation_plans.filter((item) => item.status === "proposed").length ?? 0));
const activeRunStatus = computed(() => store.selectedRun?.outcome || store.selectedRun?.status || "ready");
const activeRunLabel = computed(() => ({
  ready: "Ready",
  pending: "Queued",
  running: "Investigating",
  completed: "Completed",
  diagnosed: "Diagnosed",
  insufficient: "Evidence limited",
  failed: "Failed",
  cancelled: "Cancelled",
} as Record<string, string>)[activeRunStatus.value] || activeRunStatus.value);
const activeTitle = computed(() => store.consultation?.title || store.selectedRun?.objective || "等待选择持久化 Agent 记录");
const workspacePulse = computed(() => store.creating || store.sending || activeRunStatus.value === "pending" || activeRunStatus.value === "running");
const contextInput = computed(() => store.currentContext?.input ?? null);
const activeScope = computed(() => store.activeSnapshot?.scope ?? store.consultation?.scope ?? null);
const activeResources = computed(() => store.activeSnapshot?.resource_refs ?? contextInput.value?.resource_refs ?? []);
const activeTime = computed(() => store.activeSnapshot?.time_range ?? (contextInput.value ? { from: contextInput.value.from, to: contextInput.value.to } : null));
const incidentIdentity = computed(() => store.selectedRun?.incident_id || stringQuery(route.query.incident));
const alertIdentity = computed(() => store.selectedRun?.alert_id || stringQuery(route.query.alert));
const serviceIdentity = computed(() => activeResources.value.map((item) => `${item.kind}/${item.name}`).join(", ") || "未提供");
const sourceLabel = computed(() => {
  if (incidentIdentity.value) return "Incident 上下文";
  if (alertIdentity.value) return "Alert 上下文";
  if (store.selection === "investigation") return "持久化 Investigation";
  if (store.currentContext) return "页面 Context Snapshot";
  return "持久化 Agent 记录";
});
const entryDefinition = computed(() => ({
  context: {
    title: "从当前上下文开始",
    description: "使用页面提供的真实 Scope、资源、时间范围和 Evidence 引用创建不可变 Snapshot。",
    icon: "i-lucide-link-2",
  },
  structured: {
    title: "结构化新建",
    description: "核对标题与已冻结上下文后创建 Consultation；不会补造资源或 Evidence。",
    icon: "i-lucide-list-plus",
  },
  free: {
    title: "自由查询",
    description: "保留真实 Snapshot，但明确标记无关联事件；回答仅受现有 Evidence 边界约束。",
    icon: "i-lucide-message-circle-question",
  },
})[entryMode.value]);
const gridClasses = computed(() => ({
  "is-history-collapsed": historyCollapsed.value,
  "is-inspector-collapsed": inspectorCollapsed.value,
}));

function stringQuery(value: unknown): string {
  const candidate = Array.isArray(value) ? value[0] : value;
  return typeof candidate === "string" ? candidate : "";
}

function formatUTC(value?: string): string {
  if (!value) return "未提供";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toISOString();
}

function selectionFromRoute() {
  return readAgentRouteSelection(route.query as Record<string, unknown>);
}

async function loadWorkspace() {
  const selection = selectionFromRoute();
  await store.loadIndex(false, selection.investigationID, selection.consultationID);
}

function applyResponsiveDefaults() {
  if (typeof window === "undefined") return;
  if (!historyTouched.value) historyCollapsed.value = window.innerWidth < 1540;
  if (!inspectorTouched.value) inspectorCollapsed.value = window.innerWidth < 1180;
}

function toggleHistory() {
  historyTouched.value = true;
  historyCollapsed.value = !historyCollapsed.value;
}

function toggleInspector() {
  inspectorTouched.value = true;
  inspectorCollapsed.value = !inspectorCollapsed.value;
}

function openEntry(mode: EntryMode) {
  entryMode.value = mode;
  entryTitle.value = mode === "free" ? "无关联事件查询" : contextInput.value?.title || "CloudOps Consultation";
  freeQuestion.value = "";
  entryOpen.value = true;
}

function updateEntryOpen(value: boolean) {
  if (!store.creating) entryOpen.value = value;
}

function entryContext(): AgentContextInput | null {
  const current = store.currentContext;
  if (!current) return null;
  const context = entryMode.value === "free" ? freeQueryContext(current) : current;
  return {
    ...context.input,
    title: entryTitle.value.trim(),
    filters: entryMode.value === "free"
      ? { ...context.input.filters, free_query: freeQuestion.value.trim(), unassociated_event: true }
      : context.input.filters,
  };
}

async function submitEntry() {
  const input = entryContext();
  if (!input || entryTitle.value.trim().length < 2 || (entryMode.value === "free" && !freeQuestion.value.trim())) return;
  const created = await store.createConsultation(input, entryMode.value);
  if (!created) return;
  if (entryMode.value === "free") await store.sendMessage(freeQuestion.value);
  if (!store.error) entryOpen.value = false;
}

watch(() => route.fullPath, (value) => store.setRoute(value), { immediate: true });
watch(
  () => [route.query.consultation, route.query.investigation, route.query.run],
  () => {
    if (!store.loaded) return;
    const selection = selectionFromRoute();
    void store.selectFromRoute(selection.consultationID, selection.investigationID);
  },
);
watch(
  () => [store.selection, store.selectedID] as const,
  ([selection, id]) => {
    if (!store.loaded || !id || route.path !== "/agent") return;
    const routeSelection = selectionFromRoute();
    if ((selection === "consultation" && routeSelection.consultationID === id)
      || (selection === "investigation" && stringQuery(route.query.investigation) === id && !route.query.run && !route.query.consultation)) return;
    const query = { ...route.query };
    delete query.run;
    if (selection === "consultation") {
      query.consultation = id;
      delete query.investigation;
    } else {
      query.investigation = id;
      delete query.consultation;
    }
    void router.replace({ query });
  },
);

onMounted(() => {
  applyResponsiveDefaults();
  window.addEventListener("resize", applyResponsiveDefaults);
  void loadWorkspace();
});

onBeforeUnmount(() => {
  window.removeEventListener("resize", applyResponsiveDefaults);
  store.teardown();
});
</script>

<template>
  <WorkspacePageFrame
    as="section"
    width="full"
    class="agent-workspace-page"
    aria-labelledby="agent-workspace-heading"
    data-testid="agent-workspace"
  >
    <header class="workspace-heading">
      <div class="workspace-title">
        <span class="workspace-mark"><UIcon
          name="i-lucide-bot"
          aria-hidden="true"
        /></span>
        <div>
          <span class="section-kicker">CloudOps Intelligence / Agent workspace</span>
          <div class="workspace-title-line">
            <h1
              id="agent-workspace-heading"
              tabindex="-1"
            >
              Agent 调查工作台
            </h1>
            <span
              class="workspace-live"
              :class="{ 'is-working': workspacePulse }"
              role="status"
            ><i aria-hidden="true" />{{ activeRunLabel }}</span>
          </div>
          <p>{{ activeTitle }}</p>
        </div>
      </div>
      <dl class="workspace-stats" aria-label="Agent 工作台实时指标">
        <div><dt><UIcon name="i-lucide-message-square-text" aria-hidden="true" /> Sessions</dt><dd>{{ store.consultations.length + store.investigations.length }}</dd></div>
        <div><dt><UIcon name="i-lucide-fingerprint" aria-hidden="true" /> Evidence</dt><dd>{{ currentEvidence }}</dd></div>
        <div :class="{ 'has-attention': pendingAuthority > 0 }"><dt><UIcon name="i-lucide-shield-check" aria-hidden="true" /> Authority</dt><dd>{{ pendingAuthority }}</dd></div>
      </dl>
      <div
        class="entry-actions"
        aria-label="Agent 新建入口"
      >
        <UButton
          color="primary"
          icon="i-lucide-link-2"
          label="基于上下文"
          :disabled="!store.contextReady"
          @click="openEntry('context')"
        />
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-list-plus"
          label="新建调查"
          :disabled="!store.contextReady"
          @click="openEntry('structured')"
        />
        <UTooltip text="无关联事件；回答仍受真实 Snapshot 与 Evidence 限制">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-message-circle-question"
            label="自由查询"
            :disabled="!store.contextReady"
            @click="openEntry('free')"
          />
        </UTooltip>
      </div>
    </header>

    <section
      class="agent-context-strip"
      aria-label="当前 Agent 上下文"
      data-testid="agent-context-strip"
    >
      <div>
        <UIcon
          name="i-lucide-waypoints"
          aria-hidden="true"
        /><span>来源</span><strong>{{ sourceLabel }}</strong>
      </div>
      <div>
        <UIcon
          name="i-lucide-scan-search"
          aria-hidden="true"
        /><span>Scope</span><strong>{{ activeScope?.cluster_id || contextInput?.cluster_id || "未提供" }} / {{ activeScope?.environment || contextInput?.environment || "未提供" }}</strong>
      </div>
      <div>
        <UIcon
          name="i-lucide-siren"
          aria-hidden="true"
        /><span>Incident / Alert</span><strong translate="no">{{ incidentIdentity || alertIdentity || "无关联事件" }}</strong>
      </div>
      <div>
        <UIcon
          name="i-lucide-box"
          aria-hidden="true"
        /><span>Service / Resource</span><strong>{{ serviceIdentity }}</strong>
      </div>
      <div>
        <UIcon
          name="i-lucide-clock-3"
          aria-hidden="true"
        /><span>Time UTC</span><strong>{{ formatUTC(activeTime?.from) }} → {{ formatUTC(activeTime?.to) }}</strong>
      </div>
      <div>
        <UIcon
          name="i-lucide-file-check-2"
          aria-hidden="true"
        /><span>Evidence boundary</span><strong>{{ contextInput?.evidence_refs.length ?? store.activeSnapshot?.evidence_refs.length ?? currentEvidence }} retained · {{ contextInput?.query_execution_refs.length ?? store.activeSnapshot?.query_execution_refs.length ?? 0 }} queries</strong>
      </div>
    </section>

    <UAlert
      v-if="!store.contextReady"
      class="context-blocked"
      color="neutral"
      variant="soft"
      icon="i-lucide-shield-alert"
      title="新建入口保持阻止"
      :description="store.contextBlockReason"
    />

    <div
      class="workspace-grid"
      :class="gridClasses"
    >
      <AgentHistory
        class="workspace-history"
        :collapsed="historyCollapsed"
        @toggle-collapse="toggleHistory"
      />
      <AgentConversation class="workspace-conversation" />
      <AgentInspector
        class="workspace-inspector"
        :collapsed="inspectorCollapsed"
        @toggle-collapse="toggleInspector"
      />
    </div>

    <UModal
      :open="entryOpen"
      :title="entryDefinition.title"
      :description="entryDefinition.description"
      :close="false"
      :dismissible="!store.creating"
      :ui="{ content: 'agent-entry-modal', body: 'agent-entry-modal-body', footer: 'agent-entry-modal-footer' }"
      @update:open="updateEntryOpen"
    >
      <template #body>
        <div class="entry-modal-content">
          <UAlert
            :color="entryMode === 'free' ? 'warning' : 'info'"
            variant="soft"
            :icon="entryDefinition.icon"
            :title="entryMode === 'free' ? '无关联事件' : '不可变 Context Snapshot'"
            :description="entryMode === 'free' ? '不会推断 Incident/Alert 关系，也不会超出下面列出的 Query/Evidence。' : '创建后 Scope、资源、时间与引用作为一个新的持久化 Snapshot。'"
          />
          <UFormField
            label="Consultation 标题"
            name="agent_entry_title"
            required
          >
            <UInput
              v-model="entryTitle"
              name="agent_entry_title"
              autocomplete="off"
              :maxlength="128"
              placeholder="2–128 个字符"
              autofocus
            />
          </UFormField>
          <UFormField
            v-if="entryMode === 'free'"
            label="查询内容"
            name="agent_free_query"
            required
          >
            <UTextarea
              v-model="freeQuestion"
              name="agent_free_query"
              autocomplete="off"
              :maxlength="16000"
              :rows="5"
              placeholder="仅询问当前真实 Snapshot 可支持的问题"
            />
          </UFormField>
          <dl class="entry-context-facts">
            <div><dt>Scope</dt><dd>{{ contextInput?.cluster_id }} / {{ contextInput?.environment }}</dd></div>
            <div><dt>Namespace</dt><dd>{{ contextInput?.namespaces.join(", ") }}</dd></div>
            <div><dt>Resource</dt><dd>{{ contextInput?.resource_refs.map((item) => `${item.kind}/${item.name}`).join(", ") }}</dd></div>
            <div><dt>Time UTC</dt><dd>{{ formatUTC(contextInput?.from) }} → {{ formatUTC(contextInput?.to) }}</dd></div>
            <div><dt>References</dt><dd>{{ contextInput?.query_execution_refs.length ?? 0 }} query · {{ contextInput?.evidence_refs.length ?? 0 }} Evidence</dd></div>
          </dl>
        </div>
      </template>
      <template #footer>
        <div class="entry-modal-actions">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-x"
            label="取消"
            :disabled="store.creating"
            @click="entryOpen = false"
          />
          <UButton
            color="primary"
            icon="i-lucide-check"
            :label="entryMode === 'free' ? '创建并提交查询' : '创建 Consultation'"
            :loading="store.creating"
            :disabled="!store.contextReady || entryTitle.trim().length < 2 || (entryMode === 'free' && !freeQuestion.trim())"
            @click="submitEntry"
          />
        </div>
      </template>
    </UModal>
  </WorkspacePageFrame>
</template>

<style scoped>
.agent-workspace-page {
  position: relative;
  display: grid;
  height: calc(100dvh - var(--co-header-height));
  min-width: 0;
  min-height: 620px;
  grid-template-rows: auto auto auto minmax(0, 1fr);
  overflow: hidden;
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--co-bg-canvas) 90%, var(--co-viz-live) 2%) 0, var(--co-bg-canvas) 260px);
}
.agent-workspace-page::before {
  position: absolute;
  inset: 0 0 auto;
  height: 310px;
  z-index: 0;
  background-image:
    linear-gradient(to right, color-mix(in srgb, var(--co-border-default) 58%, transparent) 1px, transparent 1px),
    linear-gradient(to bottom, color-mix(in srgb, var(--co-border-default) 58%, transparent) 1px, transparent 1px);
  background-size: 64px 64px;
  opacity: .22;
  mask-image: linear-gradient(to bottom, #000, transparent 92%);
  pointer-events: none;
  content: "";
}

.workspace-heading {
  position: relative;
  z-index: 1;
  display: grid;
  min-width: 0;
  min-height: 112px;
  grid-template-columns: minmax(320px, 1fr) auto auto;
  align-items: center;
  gap: clamp(18px, 2.2vw, 34px);
  padding: 13px clamp(18px, 2vw, 30px) 14px;
  background: transparent;
}

.workspace-title { display: flex; min-width: 0; align-items: center; gap: 14px; }
.workspace-mark {
  position: relative;
  display: grid;
  width: 52px;
  height: 52px;
  flex: 0 0 52px;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--co-ink-action) 34%, var(--co-border-default));
  border-radius: 15px;
  color: var(--co-bg-canvas);
  background: var(--co-ink-action);
  box-shadow: 0 13px 30px color-mix(in srgb, var(--co-ink-action) 20%, transparent);
}
.workspace-mark::after {
  position: absolute;
  right: -3px;
  bottom: -3px;
  width: 10px;
  height: 10px;
  border: 2px solid var(--co-bg-canvas);
  border-radius: 50%;
  background: var(--co-viz-live);
  content: "";
}
.workspace-mark :deep(svg) { width: 23px; height: 23px; }
.section-kicker { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; letter-spacing: .1em; text-transform: uppercase; }
.workspace-title-line { display: flex; min-width: 0; align-items: center; gap: 11px; }
.workspace-title h1 { margin: 1px 0 0; color: var(--co-text-primary); font-size: 24px; font-weight: 760; letter-spacing: 0; }
.workspace-title p { max-width: 560px; margin: 3px 0 0; overflow: hidden; color: var(--co-text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.workspace-live { display: inline-flex; flex: 0 0 auto; align-items: center; gap: 6px; color: var(--co-status-success-fg); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; letter-spacing: .07em; text-transform: uppercase; }
.workspace-live i { width: 6px; height: 6px; border-radius: 50%; background: var(--co-viz-live); box-shadow: 0 0 0 4px var(--co-viz-live-soft); }
.workspace-live.is-working { color: var(--co-status-warning-fg); }
.workspace-live.is-working i { background: var(--co-viz-amber); box-shadow: 0 0 0 4px color-mix(in srgb, var(--co-viz-amber) 14%, transparent); animation: workspace-pulse 1.8s ease-in-out infinite; }
.workspace-stats {
  display: flex;
  min-width: 0;
  align-items: stretch;
  margin: 0;
  padding: 5px;
  border: 1px solid color-mix(in srgb, var(--co-border-default) 76%, transparent);
  border-radius: 13px;
  background: color-mix(in srgb, var(--co-bg-floating) 78%, transparent);
  box-shadow: var(--co-shadow-floating);
  backdrop-filter: blur(var(--co-glass-blur));
}
.workspace-stats div { display: grid; min-width: 76px; gap: 2px; padding: 5px 11px; border-right: 1px solid var(--co-border-default); }
.workspace-stats div:last-child { border-right: 0; }
.workspace-stats dt { display: flex; align-items: center; gap: 5px; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 700; text-transform: uppercase; }
.workspace-stats dt :deep(svg) { width: 11px; height: 11px; }
.workspace-stats dd { margin: 0; color: var(--co-text-primary); font-size: 16px; font-weight: 760; line-height: 1.1; font-variant-numeric: tabular-nums; }
.workspace-stats .has-attention dd { color: var(--co-status-warning-fg); }
.entry-actions { display: flex; min-width: 0; align-items: center; justify-content: flex-end; gap: var(--co-space-2); }
.entry-actions :deep(button) { min-height: 38px; border-radius: 11px; }

.agent-context-strip {
  position: relative;
  z-index: 1;
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  margin: 0 clamp(18px, 2vw, 30px) 12px;
  gap: 8px;
  overflow: visible;
  border: 0;
  background: transparent;
  box-shadow: none;
}

.agent-context-strip > div { display: grid; min-width: 0; min-height: 48px; grid-template-columns: auto minmax(0, 1fr); gap: 2px 8px; padding: 8px 10px; border: 1px solid color-mix(in srgb, var(--co-border-default) 88%, transparent); border-radius: 10px; background: color-mix(in srgb, var(--co-bg-floating) 72%, transparent); }
.agent-context-strip :deep(svg) { grid-row: 1 / 3; align-self: center; width: 14px; height: 14px; color: var(--co-viz-live); }
.agent-context-strip span { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 750; letter-spacing: .04em; text-transform: uppercase; }
.agent-context-strip strong { min-width: 0; overflow: hidden; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 9px; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
.context-blocked { position: relative; z-index: 1; margin: 0 clamp(18px, 2vw, 30px) 12px; border-radius: 10px; }

.workspace-grid {
  position: relative;
  z-index: 1;
  display: grid;
  min-width: 0;
  min-height: 0;
  grid-template-columns: 248px minmax(420px, 1fr) 368px;
  margin: 0 14px 14px;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--co-border-strong) 76%, transparent);
  border-radius: 14px;
  background: var(--co-bg-floating);
  box-shadow: var(--co-shadow-panel), inset 0 2px 0 color-mix(in srgb, var(--co-viz-live) 28%, transparent);
}

.workspace-grid.is-history-collapsed { grid-template-columns: 34px minmax(420px, 1fr) 368px; }
.workspace-grid.is-inspector-collapsed { grid-template-columns: 248px minmax(420px, 1fr) 34px; }
.workspace-grid.is-history-collapsed.is-inspector-collapsed { grid-template-columns: 34px minmax(420px, 1fr) 34px; }
.workspace-history, .workspace-conversation, .workspace-inspector { min-width: 0; min-height: 0; }

.entry-modal-content { display: grid; min-width: 0; gap: var(--co-space-4); }
.entry-context-facts { display: grid; gap: var(--co-space-1); margin: 0; padding-top: var(--co-space-2); border-top: 1px solid var(--co-border-default); }
.entry-context-facts div { display: grid; min-width: 0; grid-template-columns: 104px minmax(0, 1fr); gap: var(--co-space-3); padding: var(--co-space-1) 0; }
.entry-context-facts dt { color: var(--co-text-muted); font-size: 11px; }
.entry-context-facts dd { min-width: 0; margin: 0; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 10px; overflow-wrap: anywhere; }
.entry-modal-actions { display: flex; width: 100%; justify-content: flex-end; gap: var(--co-space-2); }

:global(.agent-entry-modal) { width: min(640px, calc(100vw - 32px)); }
:global(.agent-entry-modal-body) { min-height: 0; overflow-y: auto; }
:global(.agent-entry-modal-footer) { flex: 0 0 auto; }

@media (max-width: 1600px) {
  .workspace-heading { grid-template-columns: minmax(320px, 1fr) auto; }
  .workspace-stats { display: none; }
  .workspace-heading { gap: var(--co-space-3); }
  .entry-actions :deep(.truncate) { max-width: 112px; }
}

@media (max-width: 1180px) {
  .workspace-heading { grid-template-columns: minmax(280px, 1fr) auto; }
  .agent-context-strip { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}

@keyframes workspace-pulse { 50% { opacity: .42; } }
@media (prefers-reduced-motion: reduce) { .workspace-live.is-working i { animation: none; } }
</style>
