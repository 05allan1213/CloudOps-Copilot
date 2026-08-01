<script setup lang="ts">
import { computed, ref } from "vue";

import WorkspaceDenseList from "../workspace/WorkspaceDenseList.vue";
import { humanizeCode, incidentStatusLabel } from "../../models/incidents";
import type { IncidentListDirection, IncidentListSort, IncidentView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import AttentionFlag from "./AttentionFlag.vue";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import SeverityBadge from "./SeverityBadge.vue";

const props = withDefaults(defineProps<{
  items: IncidentView[];
  pending?: boolean;
  nextCursor?: string;
  loadingMore?: boolean;
  sort?: IncidentListSort;
  direction?: IncidentListDirection;
  selectedId?: string;
}>(), {
  pending: false,
  nextCursor: "",
  loadingMore: false,
  sort: "updated",
  direction: "desc",
  selectedId: "",
});

const emit = defineEmits<{
  loadMore: [];
  "update:sort": [value: IncidentListSort];
  "update:direction": [value: IncidentListDirection];
  select: [incident: IncidentView, trigger: HTMLElement | null];
}>();

const root = ref<HTMLElement | null>(null);
const severityOrder: Record<string, number> = { critical: 4, warning: 3, info: 2, unknown: 1 };
const sortedRows = computed(() => [...props.items].sort((left, right) => {
  const comparison = compareRows(left, right, props.sort);
  if (comparison !== 0) return props.direction === "asc" ? comparison : -comparison;
  return left.id.localeCompare(right.id);
}));

function compareRows(left: IncidentView, right: IncidentView, key: IncidentListSort): number {
  if (key === "severity") return (severityOrder[left.severity] ?? 0) - (severityOrder[right.severity] ?? 0);
  if (key === "status") return left.status.localeCompare(right.status);
  return dateValue(left.updated_at) - dateValue(right.updated_at);
}

function dateValue(value?: string): number {
  const parsed = Date.parse(value || "");
  return Number.isFinite(parsed) ? parsed : 0;
}

function severityTone(value: IncidentView): "critical" | "warning" | "info" | "neutral" {
  if (value.severity === "critical") return "critical";
  if (value.severity === "warning") return "warning";
  if (value.severity === "info") return "info";
  return "neutral";
}

function recoveryLabel(value: IncidentView["recovery"]["state"]): string {
  return ({
    not_started: "尚未验证",
    awaiting_verification: "等待验证",
    verifying: "验证中",
    investigate: "返回调查",
    recovered: "恢复已证明",
  })[value];
}

function getRowElement(incidentID: string): HTMLElement | null {
  const escaped = typeof CSS === "undefined" ? incidentID : CSS.escape(incidentID);
  return root.value?.querySelector<HTMLElement>(`[data-incident-id="${escaped}"]`)?.closest<HTMLElement>("button") ?? null;
}

function getScrollElement(): HTMLElement | null {
  return root.value;
}

defineExpose({ getRowElement, getScrollElement });
</script>

<template>
  <section
    ref="root"
    class="incident-results"
    :aria-busy="pending"
    aria-labelledby="incident-results-title"
    data-testid="incident-results"
  >
    <WorkspaceDenseList
      :items="sortedRows"
      :item-key="(item) => item.id"
      :selected-key="selectedId"
      :severity="severityTone"
      label="Incident 工作队列；选择一项打开生命周期 Inspector"
      @select="(item, trigger) => emit('select', item, trigger)"
    >
      <template #leading="{ item }">
        <div class="incident-row-badges">
          <SeverityBadge :severity="item.severity" />
          <IncidentStatusBadge :status="item.status" />
        </div>
      </template>
      <template #title="{ item }">
        <span
          :data-incident-id="item.id"
          data-testid="incident-row-summary"
        >{{ item.summary || "Incident Cycle 进行中" }}</span>
      </template>
      <template #description="{ item }">
        <span class="incident-row-stage">
          <AttentionFlag :active="item.attention.required" />
          {{ incidentStatusLabel(item.status) }}<template v-if="item.attention.required"> · {{ humanizeCode(item.attention.reason_code) }}</template>
        </span>
        <span>{{ item.operational_context.cluster }} · {{ item.operational_context.namespace }}/{{ item.operational_context.service }} · {{ item.related_alert_count }} Alerts</span>
      </template>
      <template #meta="{ item }">
        <span class="incident-row-meta">
          <strong>{{ recoveryLabel(item.recovery.state) }}</strong>
          <time :datetime="item.updated_at">{{ formatIncidentTime(item.updated_at) }}</time>
        </span>
      </template>
      <template #trailing>
        <UIcon
          name="i-lucide-chevron-right"
          aria-hidden="true"
        />
      </template>
    </WorkspaceDenseList>
    <footer class="results-footer">
      <p>当前 Cycle 的持久化投影；完整技术身份与处置操作在 Inspector 和详情中。</p>
      <UButton
        v-if="nextCursor"
        color="neutral"
        variant="outline"
        icon="i-lucide-chevrons-down"
        :loading="loadingMore"
        :disabled="loadingMore"
        :label="loadingMore ? '正在加载…' : '加载更多 Incident'"
        @click="emit('loadMore')"
      />
    </footer>
  </section>
</template>

<style scoped>
.incident-results { min-width: 0; overflow: visible; }
.incident-row-badges { display: grid; width: 98px; justify-items: start; gap: 3px; }
.incident-row-stage { display: inline-flex; min-width: 0; align-items: center; gap: var(--co-space-1); }
.incident-row-meta { display: grid; min-width: 118px; justify-items: end; gap: 2px; }
.incident-row-meta strong { color: var(--co-text-secondary); font-size: 10px; }
.incident-row-meta time { color: var(--co-text-muted); font-size: 10px; font-variant-numeric: tabular-nums; white-space: nowrap; }
.incident-results :deep(.workspace-dense-list-copy small) { display: grid; gap: 2px; }
.results-footer { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-4); margin-top: var(--co-space-2); padding: var(--co-space-3) var(--co-space-4); border-radius: var(--co-radius-overlay); background: var(--co-bg-surface); }
.results-footer p { margin: 0; color: var(--co-text-muted); font-size: 11px; }

@media (max-width: 1024px) {
  .incident-row-meta { min-width: 94px; }
}

@media (prefers-reduced-motion: reduce) {
  .incident-results { scroll-behavior: auto; }
}
</style>
