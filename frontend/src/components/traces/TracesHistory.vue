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
            <b>{{ item.mode }} · {{ item.result_count }} traces</b>
            <i :data-status="item.status">{{ item.status }}</i>
          </span>
          <code>{{ item.query }}</code>
          <small>{{ formatTime(item.created_at) }}<template v-if="item.result_expired"> · 结果已过期</template></small>
        </span>
      </UButton>
    </div>
  </aside>
</template>

<style scoped>
.traces-history { min-width: 0; align-self: start; border-top: 1px solid var(--co-border-default); }
.traces-history > header { display: flex; min-height: 52px; align-items: center; justify-content: space-between; gap: var(--co-space-2); }
.traces-history h2 { margin: 0; font-size: 14px; }
.traces-history__list { max-height: 620px; overflow-y: auto; border: 1px solid var(--co-border-subtle); border-radius: var(--co-radius-frame); }
.traces-history__item { width: 100%; min-height: 66px; justify-content: stretch; border-radius: 0; border-bottom: 1px solid var(--co-border-subtle); }
.traces-history__item.is-active { box-shadow: inset var(--co-severity-marker-width) 0 0 var(--co-action-primary); background: var(--co-bg-active); }
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
