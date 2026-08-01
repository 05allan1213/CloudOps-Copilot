<script setup lang="ts">
import type { TraceSearch } from "../../api/telemetry";

defineProps<{
  items: TraceSearch[];
  activeID: string;
}>();

const emit = defineEmits<{
  select: [searchID: string];
}>();

const dateFormatter = new Intl.DateTimeFormat("zh-CN", {
  dateStyle: "short",
  timeStyle: "short",
});

function formatTime(value: string): string {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : dateFormatter.format(parsed);
}

function modeLabel(mode: TraceSearch["mode"]): string {
  return mode === "expert" ? "TraceQL" : "服务发现";
}

function statusLabel(status: TraceSearch["status"]): string {
  return ({ pending: "等待", running: "搜索中", succeeded: "完成", failed: "失败", cancelled: "已取消" })[status];
}
</script>

<template>
  <aside
    class="traces-history"
    aria-labelledby="traces-history-heading"
  >
    <header>
      <h2 id="traces-history-heading">
        搜索历史
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
      title="尚无搜索历史"
      description="Trace Search 身份会由服务端保留。"
    />
    <div
      v-else
      class="traces-history__list"
    >
      <UButton
        v-for="item in items"
        :key="item.id"
        class="traces-history__item"
        :class="{ 'is-active': activeID === item.id }"
        color="neutral"
        variant="ghost"
        :aria-current="activeID === item.id ? 'true' : undefined"
        @click="emit('select', item.id)"
      >
        <span class="traces-history__copy">
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
.traces-history { min-width: 0; }
.traces-history > header { display: flex; min-height: 48px; align-items: center; justify-content: space-between; gap: var(--co-space-2); }
.traces-history h2 { margin: 0; font-size: 14px; }
.traces-history__list { display: grid; max-height: min(60vh, 480px); overflow: auto; gap: var(--co-space-2); }
.traces-history__item { width: 100%; min-height: 66px; justify-content: stretch; border: 1px solid transparent; border-radius: var(--co-radius-frame); background: var(--co-bg-surface); box-shadow: var(--co-shadow-row); }
.traces-history__item.is-active { border-color: var(--co-action-primary); box-shadow: var(--co-shadow-subtle); background: var(--co-bg-active); }
.traces-history__copy { display: grid; width: 100%; min-width: 0; gap: 3px; text-align: left; }
.traces-history__copy > span { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-2); }
.traces-history__copy b { font-size: 11px; }
.traces-history__copy i { color: var(--co-text-muted); font-size: 10px; font-style: normal; }
.traces-history__copy i[data-status="succeeded"] { color: var(--co-status-success-fg); }
.traces-history__copy i[data-status="failed"],
.traces-history__copy i[data-status="cancelled"] { color: var(--co-status-critical-fg); }
.traces-history__copy code { overflow: hidden; color: var(--co-text-secondary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.traces-history__copy small { color: var(--co-text-muted); font-size: 10px; }

</style>
