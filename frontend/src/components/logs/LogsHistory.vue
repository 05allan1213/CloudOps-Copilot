<script setup lang="ts">
import type { LogQuery } from "../../api/telemetry";

defineProps<{
  items: LogQuery[];
  activeID: string;
}>();

const emit = defineEmits<{
  select: [queryID: string];
}>();

const dateFormatter = new Intl.DateTimeFormat("zh-CN", {
  dateStyle: "short",
  timeStyle: "short",
});

function formatTime(value: string): string {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : dateFormatter.format(parsed);
}

function modeLabel(mode: LogQuery["mode"]): string {
  return mode === "expert" ? "Query DSL" : "日志搜索";
}

function statusLabel(status: LogQuery["status"]): string {
  return ({ pending: "等待", running: "查询中", succeeded: "完成", failed: "失败", cancelled: "已取消" })[status];
}
</script>

<template>
  <aside
    class="logs-history"
    aria-labelledby="logs-history-heading"
  >
    <header>
      <h2 id="logs-history-heading">
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
      title="尚无查询历史"
      description="成功或失败的执行身份会由服务端保留。"
    />
    <div
      v-else
      class="logs-history__list"
    >
      <UButton
        v-for="item in items"
        :key="item.id"
        class="logs-history__item"
        :class="{ 'is-active': activeID === item.id }"
        color="neutral"
        variant="ghost"
        :aria-current="activeID === item.id ? 'true' : undefined"
        @click="emit('select', item.id)"
      >
        <span class="logs-history__copy">
          <span>
            <b>{{ modeLabel(item.mode) }} · {{ item.result_count }} 条</b>
            <i :data-status="item.status">{{ statusLabel(item.status) }}</i>
          </span>
          <code>{{ item.query }}</code>
          <small>{{ formatTime(item.created_at) }}<template v-if="item.result_expired"> · 结果已过期</template></small>
        </span>
      </UButton>
    </div>
  </aside>
</template>

<style scoped>
.logs-history { min-width: 0; align-self: start; }
.logs-history > header { display: flex; min-height: 52px; align-items: center; justify-content: space-between; gap: var(--co-space-2); }
.logs-history h2 { margin: 0; font-size: 14px; }
.logs-history__list { display: grid; max-height: 620px; overflow-y: auto; gap: 7px; }
.logs-history__item { width: 100%; min-height: 66px; justify-content: stretch; border: 1px solid transparent; border-radius: var(--co-radius-frame); background: var(--co-bg-surface); box-shadow: var(--co-shadow-row); }
.logs-history__item.is-active { border-color: var(--co-action-primary); box-shadow: var(--co-shadow-subtle); background: var(--co-bg-active); }
.logs-history__copy { display: grid; width: 100%; min-width: 0; gap: 3px; text-align: left; }
.logs-history__copy > span { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-2); }
.logs-history__item b { font-size: 11px; }
.logs-history__item i { color: var(--co-text-muted); font-size: 10px; font-style: normal; }
.logs-history__item i[data-status="succeeded"] { color: var(--co-status-success-fg); }
.logs-history__item i[data-status="failed"],
.logs-history__item i[data-status="cancelled"] { color: var(--co-status-critical-fg); }
.logs-history__item code { overflow: hidden; color: var(--co-text-secondary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.logs-history__item small { color: var(--co-text-muted); font-size: 10px; }
</style>
