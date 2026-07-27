<script setup lang="ts">
import { computed } from "vue";
import { Bot, MessageSquareText, RefreshCw, Search } from "lucide-vue-next";

import { useAgentWorkspaceStore } from "../../stores/agentWorkspace";

const props = defineProps<{ compact?: boolean }>();
const store = useAgentWorkspaceStore();
const headingID = computed(() => props.compact ? "global-agent-history-heading" : "agent-history-heading");
const dateFormatter = new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });

function formatTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "时间未知" : dateFormatter.format(date);
}

async function selectType(type: "consultation" | "investigation") {
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
  <section class="agent-history" :class="{ 'is-compact': compact }" :aria-labelledby="headingID">
    <header>
      <div>
        <span class="section-kicker">History</span>
        <h2 :id="headingID">Agent 记录</h2>
      </div>
      <button type="button" aria-label="刷新 Agent 记录" title="刷新" :disabled="store.loading" @click="store.loadIndex(true)">
        <RefreshCw :size="16" aria-hidden="true" />
      </button>
    </header>

    <div class="history-segments" role="tablist" aria-label="Agent 记录类型">
      <button type="button" role="tab" :aria-selected="store.selection === 'consultation'" @click="selectType('consultation')">
        <MessageSquareText :size="15" aria-hidden="true" />Consultation
        <span>{{ store.consultations.length }}</span>
      </button>
      <button type="button" role="tab" :aria-selected="store.selection === 'investigation'" @click="selectType('investigation')">
        <Search :size="15" aria-hidden="true" />Investigation
        <span>{{ store.investigations.length }}</span>
      </button>
    </div>

    <div class="history-list" role="tabpanel">
      <template v-if="store.selection === 'consultation'">
        <button
          v-for="item in store.consultations"
          :key="item.id"
          type="button"
          class="history-item"
          :class="{ active: store.selectedID === item.id }"
          @click="store.selectConsultation(item.id)"
        >
          <span class="history-icon"><MessageSquareText :size="16" aria-hidden="true" /></span>
          <span class="history-copy"><strong>{{ item.title }}</strong><small>{{ item.scope.cluster_id }} · {{ item.message_count }} 条消息</small></span>
          <span class="history-meta"><small>{{ formatTime(item.updated_at) }}</small><i :data-status="item.active_run?.status || item.status">{{ item.active_run?.status || item.status }}</i></span>
        </button>
        <div v-if="!store.consultations.length && !store.loading" class="history-empty">
          <MessageSquareText :size="24" aria-hidden="true" /><strong>尚无 Consultation</strong>
        </div>
      </template>
      <template v-else>
        <button
          v-for="item in store.investigations"
          :key="item.id"
          type="button"
          class="history-item"
          :class="{ active: store.selectedID === item.id }"
          @click="store.selectInvestigation(item.id)"
        >
          <span class="history-icon"><Bot :size="16" aria-hidden="true" /></span>
          <span class="history-copy"><strong>{{ item.objective }}</strong><small>{{ item.subject_type }} · {{ item.evidence_count }} Evidence</small></span>
          <span class="history-meta"><small>{{ formatTime(item.updated_at) }}</small><i :data-status="item.status">{{ item.outcome || item.status }}</i></span>
        </button>
        <div v-if="!store.investigations.length && !store.loading" class="history-empty">
          <Bot :size="24" aria-hidden="true" /><strong>尚无 Investigation</strong>
        </div>
      </template>
    </div>
  </section>
</template>

<style scoped>
.agent-history { display: grid; min-width: 0; min-height: 0; grid-template-rows: auto auto minmax(0, 1fr); border-right: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.agent-history > header { display: flex; min-height: 66px; align-items: center; justify-content: space-between; gap: var(--co-space-3); padding: var(--co-space-3) var(--co-space-4); border-bottom: 1px solid var(--co-border-default); }
.agent-history h2 { margin: 2px 0 0; font-size: 15px; letter-spacing: 0; }
.section-kicker { color: var(--co-text-secondary); font-size: 10px; font-weight: 800; text-transform: uppercase; }
.agent-history > header button { display: grid; width: 36px; height: 36px; place-items: center; padding: 0; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-surface); cursor: pointer; }
.agent-history > header button:hover { color: var(--co-text-primary); background: var(--co-bg-hover); }
.history-segments { display: grid; grid-template-columns: 1fr 1fr; gap: 2px; margin: var(--co-space-3); padding: 3px; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); background: var(--co-bg-subtle); }
.history-segments button { display: flex; min-width: 0; min-height: 34px; align-items: center; justify-content: center; gap: 5px; padding: 0 7px; border: 0; border-radius: 4px; color: var(--co-text-secondary); background: transparent; cursor: pointer; font-size: 11px; font-weight: 750; }
.history-segments button:hover { color: var(--co-text-primary); background: var(--co-bg-hover); }
.history-segments button[aria-selected="true"] { color: var(--co-text-primary); background: var(--co-bg-surface); box-shadow: 0 1px 2px rgb(0 0 0 / 12%); }
.history-segments span { font-variant-numeric: tabular-nums; }
.history-list { min-height: 0; overflow-y: auto; overscroll-behavior: contain; }
.history-item { display: grid; width: 100%; min-width: 0; grid-template-columns: 30px minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-2); padding: var(--co-space-3) var(--co-space-4); border: 0; border-left: 3px solid transparent; border-bottom: 1px solid var(--co-border-default); color: var(--co-text-secondary); background: transparent; cursor: pointer; text-align: left; }
.history-item:hover { background: var(--co-bg-hover); }
.history-item.active { border-left-color: var(--co-action-primary); color: var(--co-text-primary); background: var(--co-bg-active); }
.history-icon { display: grid; width: 30px; height: 30px; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-surface); }
.history-copy, .history-meta { display: grid; min-width: 0; gap: 4px; }
.history-copy strong { overflow: hidden; font-size: 12px; line-height: 1.35; text-overflow: ellipsis; white-space: nowrap; }
.history-copy small, .history-meta small { overflow: hidden; color: var(--co-text-secondary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.history-meta { justify-items: end; }
.history-meta i { padding: 2px 5px; border-radius: 4px; color: var(--co-text-secondary); background: var(--co-bg-subtle); font-size: 9px; font-style: normal; text-transform: uppercase; }
.history-meta i[data-status="running"], .history-meta i[data-status="pending"] { color: var(--co-status-info-fg); background: var(--co-status-info-bg); }
.history-meta i[data-status="failed"], .history-meta i[data-status="cancelled"] { color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.history-empty { display: grid; min-height: 180px; place-items: center; align-content: center; gap: var(--co-space-2); color: var(--co-text-secondary); font-size: 12px; }
.is-compact > header { min-height: 56px; }
@media (max-width: 767px) { .agent-history { border-right: 0; } }
</style>
