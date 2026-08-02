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
const activeRunStatus = computed(() => store.selectedRun?.status || "ready");
const activeRunLabel = computed(() => ({
  ready: "未开始",
  pending: "准备中",
  running: "调查中",
  completed: "已完成",
  failed: "失败",
  cancelled: "已停止",
} as Record<string, string>)[activeRunStatus.value] || activeRunStatus.value);
const activeOutcome = computed(() => store.selectedRun?.outcome || "");
const activeOutcomeLabel = computed(() => ({
  diagnosed: "已定位根因",
  insufficient: "证据不足",
  completed: "已有结论",
  failed: "未形成结论",
  cancelled: "未形成结论",
} as Record<string, string>)[activeOutcome.value] || activeOutcome.value || "尚无结论");
const activeTitle = computed(() => store.consultation?.title || store.selectedRun?.objective || "等待选择持久化 Agent 记录");
const workspacePulse = computed(() => store.creating || store.sending || activeRunStatus.value === "pending" || activeRunStatus.value === "running");
const contextInput = computed(() => store.currentContext?.input ?? null);
const activeScope = computed(() => store.activeSnapshot?.scope ?? store.consultation?.scope ?? null);
const activeResources = computed(() => store.activeSnapshot?.resource_refs ?? contextInput.value?.resource_refs ?? []);
const activeTime = computed(() => store.activeSnapshot?.time_range ?? (contextInput.value ? { from: contextInput.value.from, to: contextInput.value.to } : null));
const incidentIdentity = computed(() => store.selectedRun?.incident_id || stringQuery(route.query.incident));
const alertIdentity = computed(() => store.selectedRun?.alert_id || stringQuery(route.query.alert));
const serviceIdentity = computed(() => activeResources.value.map((item) => `${item.kind}/${item.name}`).join(", ") || "未提供资源");
const scopeIdentity = computed(() => `${activeScope.value?.cluster_id || contextInput.value?.cluster_id || "未提供"} / ${activeScope.value?.environment || contextInput.value?.environment || "未提供"}`);
const eventIdentity = computed(() => incidentIdentity.value || alertIdentity.value || "无关联事件");
const evidenceBoundary = computed(() => `${contextInput.value?.evidence_refs.length ?? store.activeSnapshot?.evidence_refs.length ?? currentEvidence.value} Evidence · ${contextInput.value?.query_execution_refs.length ?? store.activeSnapshot?.query_execution_refs.length ?? 0} Query`);
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
      <div class="workspace-heading-main">
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
            </div>
            <p>{{ activeTitle }}</p>
          </div>
        </div>

        <div class="workspace-controls">
          <div
            class="workspace-status"
            role="status"
            aria-label="当前调查状态与诊断结论"
          >
            <span
              class="execution-status"
              :class="{ 'is-working': workspacePulse }"
            ><i aria-hidden="true" />调查{{ activeRunLabel }}</span>
            <span class="outcome-status">结论：{{ activeOutcomeLabel }}</span>
            <span class="workspace-counts">会话 {{ store.consultations.length + store.investigations.length }} · Evidence {{ currentEvidence }}<template v-if="pendingAuthority"> · 待授权 {{ pendingAuthority }}</template></span>
          </div>
          <div
            class="entry-actions"
            aria-label="Agent 新建入口"
          >
            <UButton
              color="primary"
              variant="soft"
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
        </div>
      </div>

      <UCollapsible
        class="agent-context-summary"
        data-testid="agent-context-strip"
      >
        <template #default="{ open }">
          <UButton
            class="context-summary-trigger"
            color="neutral"
            variant="ghost"
            block
            icon="i-lucide-waypoints"
            :trailing-icon="open ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
            :aria-label="`${open ? '收起' : '查看'}完整 Agent 上下文`"
          >
            <span class="context-summary-copy">
              <strong>{{ sourceLabel }}</strong>
              <span>{{ scopeIdentity }} · {{ serviceIdentity }} · {{ eventIdentity }} · {{ evidenceBoundary }}</span>
            </span>
            <span
              v-if="!store.contextReady"
              class="context-inline-notice"
            ><UIcon
              name="i-lucide-lock-keyhole"
              aria-hidden="true"
            />新建需从 Logs、Traces、Alert 或 Incident 恢复 Context</span>
            <span class="context-disclosure">{{ open ? "收起" : "查看上下文" }}</span>
          </UButton>
        </template>
        <template #content>
          <dl
            class="context-details"
            aria-label="完整 Agent 上下文"
          >
            <div><dt>来源</dt><dd>{{ sourceLabel }}</dd></div>
            <div><dt>Scope</dt><dd>{{ scopeIdentity }}</dd></div>
            <div>
              <dt>Incident / Alert</dt><dd translate="no">
                {{ eventIdentity }}
              </dd>
            </div>
            <div><dt>Resource</dt><dd>{{ serviceIdentity }}</dd></div>
            <div><dt>Time UTC</dt><dd>{{ formatUTC(activeTime?.from) }} → {{ formatUTC(activeTime?.to) }}</dd></div>
            <div><dt>Evidence boundary</dt><dd>{{ evidenceBoundary }}</dd></div>
          </dl>
          <p
            v-if="!store.contextReady"
            class="context-block-reason"
          >
            {{ store.contextBlockReason }}
          </p>
        </template>
      </UCollapsible>
    </header>

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
  grid-template-rows: auto minmax(0, 1fr);
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
  gap: 7px;
  padding: 10px clamp(18px, 2vw, 30px) 8px;
  background: transparent;
}
.workspace-heading-main { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: clamp(20px, 3vw, 48px); }

.workspace-title { display: flex; min-width: 0; align-items: center; gap: 14px; }
.workspace-mark {
  position: relative;
  display: grid;
  width: 42px;
  height: 42px;
  flex: 0 0 42px;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--co-ink-action) 34%, var(--co-border-default));
  border-radius: 12px;
  color: var(--co-status-success-fg);
  background: color-mix(in srgb, var(--co-viz-live-soft) 72%, var(--co-bg-floating));
  box-shadow: 0 8px 20px rgb(52 46 39 / 7%);
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
.workspace-mark :deep(svg) { width: 20px; height: 20px; }
.section-kicker { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; letter-spacing: .1em; text-transform: uppercase; }
.workspace-title-line { display: flex; min-width: 0; align-items: center; gap: 11px; }
.workspace-title h1 { margin: 1px 0 0; color: var(--co-text-primary); font-size: 22px; font-weight: 760; letter-spacing: 0; }
.workspace-title p { max-width: 560px; margin: 3px 0 0; overflow: hidden; color: var(--co-text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.workspace-controls { display: flex; min-width: 0; align-items: center; justify-content: flex-end; gap: clamp(16px, 2vw, 28px); }
.workspace-status { display: grid; min-width: 0; grid-template-columns: auto auto; align-items: center; gap: 2px 8px; }
.execution-status, .outcome-status { display: inline-flex; align-items: center; gap: 6px; font-size: 10px; font-weight: 720; white-space: nowrap; }
.execution-status { color: var(--co-status-success-fg); }
.execution-status i { width: 6px; height: 6px; border-radius: 50%; background: var(--co-viz-live); box-shadow: 0 0 0 3px var(--co-viz-live-soft); }
.execution-status.is-working { color: var(--co-status-warning-fg); }
.execution-status.is-working i { background: var(--co-viz-amber); box-shadow: 0 0 0 3px color-mix(in srgb, var(--co-viz-amber) 14%, transparent); animation: workspace-pulse var(--co-motion-pulse-cycle) var(--co-ease-signal) infinite; }
.outcome-status { color: var(--co-text-secondary); }
.workspace-counts { grid-column: 1 / -1; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; white-space: nowrap; }
.entry-actions { display: flex; min-width: 0; align-items: center; justify-content: flex-end; gap: var(--co-space-2); }
.entry-actions :deep(button) { min-height: 34px; border-radius: 9px; }

.agent-context-summary { min-width: 0; overflow: hidden; border-top: 1px solid color-mix(in srgb, var(--co-border-default) 76%, transparent); }
.context-summary-trigger { min-height: 34px; justify-content: flex-start; padding-inline: 3px; border-radius: 0; text-align: left; }
.context-summary-trigger :deep(svg) { flex: 0 0 auto; color: var(--co-status-success-fg); }
.context-summary-copy { display: flex; min-width: 0; flex: 1 1 auto; align-items: center; gap: 8px; overflow: hidden; }
.context-summary-copy strong { flex: 0 0 auto; color: var(--co-text-primary); font-size: 9px; }
.context-summary-copy > span { min-width: 0; overflow: hidden; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.context-inline-notice { display: inline-flex; max-width: 340px; flex: 0 1 auto; align-items: center; gap: 5px; overflow: hidden; color: var(--co-status-warning-fg); font-size: 8px; text-overflow: ellipsis; white-space: nowrap; }
.context-inline-notice :deep(svg) { width: 12px; height: 12px; color: currentColor; }
.context-disclosure { flex: 0 0 auto; color: var(--co-text-muted); font-size: 8px; }
.context-details { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0 18px; margin: 0; padding: 7px 8px 10px 28px; border-top: 1px solid color-mix(in srgb, var(--co-border-default) 62%, transparent); }
.context-details > div { display: grid; min-width: 0; grid-template-columns: 92px minmax(0, 1fr); gap: 8px; padding: 4px 0; }
.context-details dt { color: var(--co-text-muted); font-size: 8px; }
.context-details dd { min-width: 0; margin: 0; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 8px; overflow-wrap: anywhere; }
.context-block-reason { margin: 0 8px 8px 28px; color: var(--co-status-warning-fg); font-size: 9px; }

.workspace-grid {
  position: relative;
  z-index: 1;
  display: grid;
  min-width: 0;
  min-height: 0;
  grid-template-columns: 232px minmax(420px, 1fr) 348px;
  margin: 0 14px 14px;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--co-border-strong) 76%, transparent);
  border-radius: 14px;
  background: var(--co-bg-floating);
  box-shadow: var(--co-shadow-panel), inset 0 2px 0 color-mix(in srgb, var(--co-viz-live) 28%, transparent);
}

.workspace-grid.is-history-collapsed { grid-template-columns: 34px minmax(420px, 1fr) 348px; }
.workspace-grid.is-inspector-collapsed { grid-template-columns: 232px minmax(420px, 1fr) 34px; }
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
  .entry-actions :deep(.truncate) { max-width: 112px; }
}

@media (max-width: 1180px) {
  .workspace-status { display: none; }
  .context-inline-notice { display: none; }
  .context-details { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 767px) {
  .workspace-grid,
  .workspace-grid.is-history-collapsed,
  .workspace-grid.is-inspector-collapsed,
  .workspace-grid.is-history-collapsed.is-inspector-collapsed {
    grid-template-columns: 34px minmax(0, 1fr) 34px;
    margin-inline: 8px;
  }
}

@keyframes workspace-pulse { 50% { opacity: .42; } }
@media (prefers-reduced-motion: reduce) { .execution-status.is-working i { animation: none; } }
</style>
