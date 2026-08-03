<script setup lang="ts">
import type { QueryExecution } from "../../api/monitoring";

defineProps<{
  items: QueryExecution[];
  activeID: string;
}>();

const emit = defineEmits<{
  select: [id: string];
}>();

const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "short" });

function formatTime(value: string): string {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : dateFormatter.format(parsed);
}

function statusLabel(status: QueryExecution["status"]): string {
  return ({ pending: "等待", running: "查询中", succeeded: "完成", failed: "失败", cancelled: "取消" })[status];
}
</script>

<template>
  <aside
    class="monitoring-history"
    aria-labelledby="monitoring-history-title"
  >
    <header>
      <h2 id="monitoring-history-title">
        查询历史
      </h2>
      <UBadge
        color="neutral"
        variant="soft"
        :label="String(items.length)"
      />
    </header>
    <WorkspaceState
      v-if="!items.length"
      kind="empty"
      title="暂无执行记录"
      description="当前 Workload 尚未产生查询历史。"
    />
    <div
      v-else
      class="monitoring-history__list"
    >
      <UButton
        v-for="item in items"
        :key="item.id"
        class="monitoring-history__item"
        :class="{ 'is-active': activeID === item.id }"
        color="neutral"
        variant="ghost"
        :aria-current="activeID === item.id ? 'true' : undefined"
        @click="emit('select', item.id)"
      >
        <span class="monitoring-history__copy">
          <span><b>{{ item.mode === "guided" ? "可视化" : "PromQL" }}</b><i :data-status="item.status">{{ statusLabel(item.status) }}</i></span>
          <code>{{ item.catalog_key || item.query }}</code>
          <small>{{ formatTime(item.created_at) }} · {{ item.actor }}</small>
        </span>
      </UButton>
    </div>
  </aside>
</template>

<style scoped>
.monitoring-history { display: grid; min-width: 0; max-height: min(336px, calc(var(--reka-popover-content-available-height, 360px) - 24px), calc(100dvh - 56px)); grid-template-rows: auto minmax(0, 1fr); align-self: start; overflow: hidden; }
.monitoring-history > header { display: flex; min-height: 44px; align-items: center; justify-content: space-between; gap: var(--co-space-2); }
.monitoring-history h2 { margin: 0; font-size: 14px; }
.monitoring-history__list { display: grid; min-height: 0; max-height: min(280px, calc(var(--reka-popover-content-available-height, 360px) - 76px), calc(100dvh - 108px)); overflow-x: hidden; overflow-y: auto; overscroll-behavior: contain; gap: 6px; padding-right: 4px; scrollbar-gutter: stable; }
.monitoring-history__item { width: 100%; min-height: 60px; justify-content: stretch; padding: 8px 10px; border: 1px solid transparent; border-radius: var(--co-radius-frame); background: var(--co-bg-surface); box-shadow: var(--co-shadow-row); }
.monitoring-history__item.is-active { border-color: var(--co-action-primary); box-shadow: var(--co-shadow-subtle); background: var(--co-bg-active); }
.monitoring-history__copy { display: grid; width: 100%; min-width: 0; gap: 3px; text-align: left; }
.monitoring-history__copy > span { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-2); }
.monitoring-history__copy b { font-size: 11px; }
.monitoring-history__copy i { color: var(--co-text-muted); font-size: 10px; font-style: normal; }
.monitoring-history__copy i[data-status="succeeded"] { color: var(--co-status-success-fg); }
.monitoring-history__copy i[data-status="failed"],
.monitoring-history__copy i[data-status="cancelled"] { color: var(--co-status-critical-fg); }
.monitoring-history__copy code { overflow: hidden; color: var(--co-text-secondary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.monitoring-history__copy small { color: var(--co-text-muted); font-size: 10px; }
</style>
