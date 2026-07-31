<script setup lang="ts">
import { computed, h, ref, resolveComponent } from "vue";
import { BellRing, ShieldCheck } from "lucide-vue-next";

import DenseDataTable, { type DenseTableColumn } from "../workspace/DenseDataTable.vue";
import { humanizeCode, incidentStatusLabel } from "../../models/incidents";
import type { IncidentListDirection, IncidentListSort, IncidentView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";
import AttentionFlag from "./AttentionFlag.vue";
import IncidentStatusBadge from "./IncidentStatusBadge.vue";
import SeverityBadge from "./SeverityBadge.vue";

type TableIncident = IncidentView & Record<string, unknown>;

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

const UButton = resolveComponent("UButton");
const denseTable = ref<{
  getRowElement: (rowID: string) => HTMLElement | null;
  getScrollElement: () => HTMLElement | null;
} | null>(null);
const rows = computed(() => props.items as TableIncident[]);
const severityOrder: Record<string, number> = { critical: 4, warning: 3, info: 2, unknown: 1 };

const sortedRows = computed(() => [...rows.value].sort((left, right) => {
  const comparison = compareRows(left, right, props.sort);
  if (comparison !== 0) return props.direction === "asc" ? comparison : -comparison;
  return left.id.localeCompare(right.id);
}));

const columns = computed<DenseTableColumn<TableIncident>[]>(() => [
  {
    id: "severity",
    accessorKey: "severity",
    label: "级别",
    header: () => sortControl("级别", "severity"),
    size: 112,
    cell: ({ row }) => h(SeverityBadge, { severity: row.original.severity }),
  },
  {
    id: "identity",
    accessorKey: "summary",
    label: "Incident",
    header: "Incident",
    size: 330,
    cell: ({ row }) => h("div", { class: "incident-table-identity" }, [
      h("strong", { "data-testid": "incident-row-summary" }, row.original.summary || "Incident Cycle 进行中"),
      h("span", {}, `${compactID(row.original.id)} · Cycle ${row.original.cycle} · v${row.original.version}`),
    ]),
  },
  {
    id: "scope",
    accessorKey: "operational_context",
    label: "Scope / Alert",
    header: "Scope / Alert",
    size: 245,
    cell: ({ row }) => h("div", { class: "incident-table-stack" }, [
      h("strong", {}, `${row.original.operational_context.namespace}/${row.original.operational_context.resource.name}`),
      h("span", {}, `${row.original.operational_context.cluster} · ${row.original.operational_context.service}`),
      h("span", { class: "incident-table-inline" }, [h(BellRing, { size: 13, "aria-hidden": "true" }), `${row.original.related_alert_count} Alert`]),
    ]),
  },
  {
    id: "status",
    accessorKey: "status",
    label: "状态 / Attention",
    header: () => sortControl("状态 / Attention", "status"),
    size: 210,
    cell: ({ row }) => h("div", { class: "incident-table-stack" }, [
      h("div", { class: "incident-table-inline" }, [h(IncidentStatusBadge, { status: row.original.status }), h("span", {}, incidentStatusLabel(row.original.status))]),
      h("div", { class: "incident-table-inline" }, [h(AttentionFlag, { active: row.original.attention.required }), row.original.attention.required ? h("span", {}, humanizeCode(row.original.attention.reason_code)) : h("span", {}, "无需 Attention")]),
    ]),
  },
  {
    id: "recovery",
    accessorKey: "recovery",
    label: "恢复",
    header: "恢复",
    size: 215,
    optional: true,
    cell: ({ row }) => h("div", { class: "incident-table-stack" }, [
      h("strong", { class: "incident-table-inline" }, [h(ShieldCheck, { size: 15, "aria-hidden": "true" }), recoveryLabel(row.original.recovery.state)]),
      h("span", {}, `${row.original.recovery.verification_attempts} 次尝试 · ${row.original.recovery.failed_verification_count} 次失败`),
      h("span", {}, row.original.recovery.resolution_report_id ? "ResolutionReport 已生成" : "尚无 ResolutionReport"),
    ]),
  },
  {
    id: "updated",
    accessorKey: "updated_at",
    label: "更新",
    header: () => sortControl("更新", "updated"),
    size: 160,
    cell: ({ row }) => h("time", { datetime: row.original.updated_at }, formatIncidentTime(row.original.updated_at)),
  },
]);

function compareRows(left: TableIncident, right: TableIncident, key: IncidentListSort): number {
  if (key === "severity") return (severityOrder[left.severity] ?? 0) - (severityOrder[right.severity] ?? 0);
  if (key === "status") return left.status.localeCompare(right.status);
  return dateValue(left.updated_at) - dateValue(right.updated_at);
}

function sortControl(label: string, key: IncidentListSort) {
  const active = props.sort === key;
  const icon = active && props.direction === "asc" ? "i-lucide-arrow-up-a-z" : "i-lucide-arrow-down-a-z";
  return h(UButton, {
    color: "neutral",
    variant: "ghost",
    size: "xs",
    label,
    icon,
    "aria-label": `按${label}排序${active ? `，当前${props.direction === "asc" ? "升序" : "降序"}` : ""}`,
    onClick: () => setSort(key),
  });
}

function setSort(key: IncidentListSort) {
  if (props.sort === key) emit("update:direction", props.direction === "asc" ? "desc" : "asc");
  else {
    emit("update:sort", key);
    emit("update:direction", key === "status" ? "asc" : "desc");
  }
}

function dateValue(value?: string): number {
  const parsed = Date.parse(value || "");
  return Number.isFinite(parsed) ? parsed : 0;
}

function compactID(value: string): string {
  return value.length > 18 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value;
}

function recoveryLabel(value: IncidentView["recovery"]["state"]): string {
  return ({
    not_started: "尚未验证",
    awaiting_verification: "等待验证",
    verifying: "验证中",
    investigate: "返回调查",
    recovered: "已证明恢复",
  })[value];
}

function severityTone(value: IncidentView["severity"]): "critical" | "warning" | "info" | "neutral" {
  return value === "critical" ? "critical" : value === "warning" ? "warning" : value === "info" ? "info" : "neutral";
}

function onSelect(incident: TableIncident, trigger: HTMLElement | null) {
  emit("select", incident, trigger);
}

function getRowElement(incidentID: string) {
  return denseTable.value?.getRowElement(incidentID) ?? null;
}

function getScrollElement() {
  return denseTable.value?.getScrollElement() ?? null;
}

defineExpose({ getRowElement, getScrollElement });
</script>

<template>
  <section
    class="incident-results"
    :aria-busy="pending"
    aria-labelledby="incident-results-title"
    data-testid="incident-results"
  >
    <DenseDataTable
      ref="denseTable"
      :rows="sortedRows"
      :columns="columns"
      :row-key="(row) => row.id"
      storage-key="incidents"
      caption="当前 Incident Cycle；选择行打开只读 Inspector。"
      :critical-column-ids="['severity', 'identity', 'status']"
      :selected-id="selectedId"
      :severity="(row) => severityTone(row.severity)"
      @select="onSelect"
    />
    <footer class="results-footer">
      <p>列表字段来自当前 Cycle 持久化投影；选择 Incident 后在 Inspector 查看摘要。</p>
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
.incident-results { min-width: 0; overflow: hidden; border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.results-footer { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-4); padding: var(--co-space-3); }
.results-footer p { margin: 0; color: var(--co-text-muted); font-size: 11px; }
.incident-table-identity, .incident-table-stack { display: grid; min-width: 0; gap: 3px; }
.incident-table-identity strong, .incident-table-stack strong { color: var(--co-text-primary); font-size: 12px; overflow-wrap: anywhere; }
.incident-table-identity span, .incident-table-stack span { color: var(--co-text-muted); font-size: 11px; overflow-wrap: anywhere; }
span.incident-table-inline { display: inline-flex; align-items: center; gap: 5px; }
time { color: var(--co-text-secondary); font-size: 11px; font-variant-numeric: tabular-nums; }
@media (max-width: 767px) {
  .results-footer { align-items: stretch; flex-direction: column; }
  .results-footer :deep(button) { width: 100%; justify-content: center; }
}
</style>
