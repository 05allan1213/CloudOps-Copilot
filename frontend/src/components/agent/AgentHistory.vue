<script setup lang="ts">
import { computed } from "vue";

import { useAgentWorkspaceStore, type AgentSelection } from "../../stores/agentWorkspace";

const props = defineProps<{ compact?: boolean; collapsed?: boolean }>();
const emit = defineEmits<{ "toggle-collapse": [] }>();
const store = useAgentWorkspaceStore();
const headingID = computed(() => props.compact ? "global-agent-history-heading" : "agent-history-heading");
const dateFormatter = new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
const tabs = computed(() => [
  { label: `会话 ${store.consultations.length}`, value: "consultation", icon: "i-lucide-message-square-text" },
  { label: `调查 ${store.investigations.length}`, value: "investigation", icon: "i-lucide-search" },
]);

function formatTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "时间未知" : dateFormatter.format(date);
}

function statusColor(status: string): "success" | "info" | "warning" | "error" | "neutral" {
  if (status === "completed" || status === "diagnosed" || status === "open") return "success";
  if (status === "running" || status === "pending") return "info";
  if (status === "insufficient") return "warning";
  if (status === "failed" || status === "cancelled") return "error";
  return "neutral";
}

async function selectType(value: string | number) {
  if (value !== "consultation" && value !== "investigation") return;
  const type = value as AgentSelection;
  if (type === "consultation") {
    const id = store.consultations[0]?.id;
    if (id) await store.selectConsultation(id);
    else store.selection = type;
    return;
  }
  const id = store.investigations[0]?.id;
  if (id) await store.selectInvestigation(id);
  else store.selection = type;
}
</script>

<template>
  <aside
    v-if="collapsed && !compact"
    class="agent-history-rail"
    aria-label="已折叠 Agent History"
  >
    <UTooltip
      text="展开 Agent History"
      :content="{ side: 'right' }"
    >
      <UButton
        color="neutral"
        variant="ghost"
        icon="i-lucide-panel-left-open"
        square
        size="xs"
        aria-label="展开 Agent History"
        data-testid="agent-history-expand"
        @click="emit('toggle-collapse')"
      />
    </UTooltip>
    <span aria-hidden="true">History</span>
  </aside>

  <section
    v-else
    class="agent-history"
    :class="{ 'is-compact': compact }"
    :aria-labelledby="headingID"
  >
    <header>
      <div>
        <span class="section-kicker">History</span>
        <h2 :id="headingID">
          Agent 记录
        </h2>
      </div>
      <div class="history-actions">
        <UTooltip text="刷新 Agent 记录">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-refresh-cw"
            square
            :loading="store.loading"
            aria-label="刷新 Agent 记录"
            @click="store.loadIndex(true)"
          />
        </UTooltip>
        <UTooltip
          v-if="!compact"
          text="折叠 Agent History"
        >
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-panel-left-close"
            square
            aria-label="折叠 Agent History"
            data-testid="agent-history-collapse"
            @click="emit('toggle-collapse')"
          />
        </UTooltip>
      </div>
    </header>

    <UTabs
      class="history-tabs"
      :model-value="store.selection"
      :items="tabs"
      :content="false"
      color="primary"
      variant="pill"
      size="xs"
      @update:model-value="selectType"
    />

    <div
      class="history-list"
      role="tabpanel"
      :aria-busy="store.loading"
    >
      <template v-if="store.selection === 'consultation'">
        <UButton
          v-for="item in store.consultations"
          :key="item.id"
          color="neutral"
          variant="ghost"
          class="history-item"
          :class="{ active: store.selectedID === item.id }"
          :aria-pressed="store.selectedID === item.id"
          @click="store.selectConsultation(item.id)"
        >
          <span class="history-icon"><UIcon
            name="i-lucide-message-square-text"
            aria-hidden="true"
          /></span>
          <span class="history-copy">
            <strong>{{ item.title }}</strong>
            <small>{{ item.scope.cluster_id }} · {{ item.message_count }} 条消息</small>
          </span>
          <span class="history-meta">
            <small>{{ formatTime(item.updated_at) }}</small>
            <UBadge
              :color="statusColor(item.active_run?.status || item.status)"
              variant="subtle"
              size="sm"
              :label="item.active_run?.status || item.status"
            />
          </span>
        </UButton>
        <div
          v-if="!store.consultations.length && !store.loading"
          class="history-empty"
        >
          <UIcon
            name="i-lucide-message-square-text"
            aria-hidden="true"
          />
          <strong>尚无 Consultation</strong>
        </div>
      </template>

      <template v-else>
        <UButton
          v-for="item in store.investigations"
          :key="item.id"
          color="neutral"
          variant="ghost"
          class="history-item"
          :class="{ active: store.selectedID === item.id }"
          :aria-pressed="store.selectedID === item.id"
          @click="store.selectInvestigation(item.id)"
        >
          <span class="history-icon"><UIcon
            name="i-lucide-bot"
            aria-hidden="true"
          /></span>
          <span class="history-copy">
            <strong>{{ item.objective }}</strong>
            <small>{{ item.subject_type }} · {{ item.evidence_count }} Evidence</small>
          </span>
          <span class="history-meta">
            <small>{{ formatTime(item.updated_at) }}</small>
            <UBadge
              :color="statusColor(item.outcome || item.status)"
              variant="subtle"
              size="sm"
              :label="item.outcome || item.status"
            />
          </span>
        </UButton>
        <div
          v-if="!store.investigations.length && !store.loading"
          class="history-empty"
        >
          <UIcon
            name="i-lucide-bot"
            aria-hidden="true"
          />
          <strong>尚无 Investigation</strong>
        </div>
      </template>
    </div>
  </section>
</template>

<style scoped>
.agent-history {
  display: grid;
  min-width: 0;
  min-height: 0;
  grid-template-columns: minmax(0, 1fr);
  grid-template-rows: auto auto minmax(0, 1fr);
  border-right: 1px solid var(--co-border-default);
  background: linear-gradient(180deg, color-mix(in srgb, var(--co-bg-surface) 72%, var(--co-bg-floating)), var(--co-bg-surface));
}
.agent-history > header {
  display: flex;
  width: 100%;
  min-width: 0;
  min-height: 70px;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-2);
  padding: 12px 12px 9px 15px;
  border-bottom: 1px solid color-mix(in srgb, var(--co-border-default) 72%, transparent);
  background: color-mix(in srgb, var(--co-bg-floating) 36%, transparent);
}
.agent-history > header > div:first-child { min-width: 0; }
.agent-history h2 { margin: 2px 0 0; font-size: 15px; font-weight: 720; letter-spacing: 0; }
.section-kicker { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; letter-spacing: .1em; text-transform: uppercase; }
.history-actions { display: flex; flex: 0 0 auto; align-items: center; gap: 2px; }
.history-actions :deep(button) { width: 30px; min-width: 30px; height: 30px; border-radius: 8px; }
.history-tabs { margin: 9px 10px 8px; padding: 3px; border: 1px solid var(--co-border-default); border-radius: 10px; background: color-mix(in srgb, var(--co-bg-floating) 66%, transparent); }
.history-list { min-height: 0; padding: 2px 8px 12px; overflow-y: auto; overscroll-behavior: contain; }

.history-item {
  position: relative;
  display: grid;
  width: 100%;
  min-width: 0;
  min-height: 66px;
  grid-template-columns: 30px minmax(0, 1fr) auto;
  align-items: center;
  gap: 9px;
  margin-bottom: 4px;
  padding: 8px 8px;
  border: 1px solid transparent;
  border-radius: 9px;
  color: var(--co-text-secondary);
  text-align: left;
  transition: border-color var(--co-motion-fast) var(--co-ease-out), background var(--co-motion-fast) var(--co-ease-out), transform var(--co-motion-fast) var(--co-ease-out);
}

.history-item::before { position: absolute; top: 10px; bottom: 10px; left: -1px; width: 2px; border-radius: 999px; background: transparent; content: ""; }
.history-item:hover { border-color: color-mix(in srgb, var(--co-border-strong) 72%, transparent); background: color-mix(in srgb, var(--co-bg-floating) 58%, transparent); transform: translateX(1px); }
.history-item.active { border-color: var(--co-border-strong); color: var(--co-text-primary); background: var(--co-bg-floating); box-shadow: 0 8px 22px rgb(52 46 39 / 7%); }
.history-item.active::before { background: var(--co-viz-live); }
.history-icon { display: grid; width: 30px; height: 30px; place-items: center; border: 1px solid var(--co-border-default); border-radius: 9px; color: var(--co-text-secondary); background: color-mix(in srgb, var(--co-bg-floating) 80%, transparent); }
.history-item.active .history-icon { border-color: color-mix(in srgb, var(--co-viz-live) 35%, var(--co-border-default)); color: var(--co-status-success-fg); background: var(--co-viz-live-soft); }
.history-icon :deep(svg) { width: 14px; height: 14px; }
.history-copy, .history-meta { display: grid; min-width: 0; gap: 3px; }
.history-copy strong { overflow: hidden; font-size: 11px; font-weight: 680; line-height: 1.35; text-overflow: ellipsis; white-space: nowrap; }
.history-copy small, .history-meta small { overflow: hidden; color: var(--co-text-muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.history-meta { justify-items: end; }
.history-empty { display: grid; min-height: 230px; place-items: center; align-content: center; gap: 9px; color: var(--co-text-muted); font-size: 10px; text-align: center; }
.history-empty :deep(svg) { width: 34px; height: 34px; padding: 8px; border: 1px solid var(--co-border-default); border-radius: 11px; color: var(--co-text-secondary); background: var(--co-bg-floating); box-shadow: 0 10px 24px rgb(52 46 39 / 7%); }
.is-compact > header { min-height: 58px; }
.is-compact .history-tabs { margin-top: 7px; }
.is-compact .history-item { margin-bottom: 2px; border-radius: 8px; }

.agent-history-rail { display: grid; min-width: 0; min-height: 0; grid-template-rows: 42px 1fr; justify-items: center; padding-top: 5px; border-right: 1px solid var(--co-border-default); background: color-mix(in srgb, var(--co-bg-surface) 82%, var(--co-bg-floating)); }
.agent-history-rail > span { align-self: start; margin-top: var(--co-space-2); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; letter-spacing: .1em; text-transform: uppercase; writing-mode: vertical-rl; }
</style>
