<script setup lang="ts">
import { computed } from "vue";
import { FilterX, Search } from "lucide-vue-next";

import { incidentStatuses, incidentStatusLabel, severityLabel } from "../../models/incidents";
import type { IncidentSeverity, IncidentStatus } from "../../types/incidents";

const props = defineProps<{
  status?: IncidentStatus;
  severity?: IncidentSeverity;
  service?: string;
  attention?: boolean;
  resource?: string;
  alert?: string;
  from?: string;
  to?: string;
  loading?: boolean;
}>();

const emit = defineEmits<{
  "update:status": [value: IncidentStatus | undefined];
  "update:severity": [value: IncidentSeverity | undefined];
  "update:service": [value: string | undefined];
  "update:attention": [value: boolean | undefined];
  "update:resource": [value: string | undefined];
  "update:alert": [value: string | undefined];
  "update:from": [value: string | undefined];
  "update:to": [value: string | undefined];
  apply: [];
  reset: [];
}>();

const severities: IncidentSeverity[] = ["critical", "warning", "info", "unknown"];
const statusModel = computed({
  get: () => props.status ?? "",
  set: (value: string) => emit("update:status", (value || undefined) as IncidentStatus | undefined),
});
const severityModel = computed({
  get: () => props.severity ?? "",
  set: (value: string) => emit("update:severity", (value || undefined) as IncidentSeverity | undefined),
});
const attentionModel = computed({
  get: () => props.attention === undefined ? "" : String(props.attention),
  set: (value: string) => emit("update:attention", value === "" ? undefined : value === "true"),
});
const serviceModel = textModel(() => props.service, (value) => emit("update:service", value));
const resourceModel = textModel(() => props.resource, (value) => emit("update:resource", value));
const alertModel = textModel(() => props.alert, (value) => emit("update:alert", value));
const fromModel = computed({
  get: () => toLocalDateTime(props.from),
  set: (value: string) => emit("update:from", toRFC3339(value)),
});
const toModel = computed({
  get: () => toLocalDateTime(props.to),
  set: (value: string) => emit("update:to", toRFC3339(value)),
});
const hasFilters = computed(() => Boolean(
  props.status || props.severity || props.service || props.attention !== undefined
  || props.resource || props.alert || props.from || props.to,
));

function textModel(read: () => string | undefined, write: (value: string | undefined) => void) {
  return computed({
    get: () => read() ?? "",
    set: (value: string) => write(value.trimStart() || undefined),
  });
}

function toRFC3339(value: string): string | undefined {
  if (!value) return undefined;
  const parsed = new Date(value);
  return Number.isFinite(parsed.getTime()) ? parsed.toISOString() : undefined;
}

function toLocalDateTime(value?: string): string {
  if (!value) return "";
  const parsed = new Date(value);
  if (!Number.isFinite(parsed.getTime())) return "";
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())}T${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`;
}
</script>

<template>
  <form class="filter-bar" aria-labelledby="incident-filters-title" @submit.prevent="$emit('apply')">
    <div class="filter-heading">
      <div><h2 id="incident-filters-title">筛选 Incident</h2><p id="incident-filter-help">筛选条件与 URL 保持同步。</p></div>
      <span class="filter-contract">URL 已同步</span>
    </div>

    <div class="filter-grid">
      <label><span>状态</span><select v-model="statusModel" name="status" autocomplete="off"><option value="">全部状态</option><option v-for="option in incidentStatuses" :key="option" :value="option">{{ incidentStatusLabel(option) }}</option></select></label>
      <label><span>级别</span><select v-model="severityModel" name="severity" autocomplete="off"><option value="">全部级别</option><option v-for="option in severities" :key="option" :value="option">{{ severityLabel(option) }}</option></select></label>
      <label><span>Attention</span><select v-model="attentionModel" name="attention" autocomplete="off"><option value="">全部</option><option value="true">需要关注</option><option value="false">无需关注</option></select></label>
      <label><span>服务</span><input v-model="serviceModel" name="service" type="text" maxlength="255" autocomplete="off" spellcheck="false" placeholder="例如：checkout-api…"></label>
      <label><span>资源</span><input v-model="resourceModel" name="resource" type="text" maxlength="512" autocomplete="off" spellcheck="false" placeholder="资源 ID 或精确名称…"></label>
      <label><span>Alert</span><input v-model="alertModel" name="alert" type="text" maxlength="36" autocomplete="off" spellcheck="false" placeholder="Alert public UUID…"></label>
      <label><span>开始时间</span><input v-model="fromModel" name="from" type="datetime-local" autocomplete="off"></label>
      <label><span>结束时间</span><input v-model="toModel" name="to" type="datetime-local" autocomplete="off"></label>
    </div>

    <div class="filter-actions">
      <button type="submit" class="primary-action" :disabled="loading"><Search :size="16" aria-hidden="true" />{{ loading ? "正在查询…" : "查询" }}</button>
      <button type="button" class="secondary-action" :disabled="!hasFilters || loading" @click="$emit('reset')"><FilterX :size="16" aria-hidden="true" />清除筛选</button>
    </div>
  </form>
</template>

<style scoped>
.filter-bar { display: grid; min-width: 0; gap: var(--co-space-4); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.filter-heading, .filter-actions { display: flex; align-items: center; }
.filter-heading { justify-content: space-between; gap: var(--co-space-4); }
.filter-heading h2, .filter-heading p { margin: 0; }
.filter-heading h2 { font-size: 16px; text-wrap: balance; }
.filter-heading p { margin-top: 2px; color: var(--co-text-secondary); font-size: 12px; }
.filter-contract { flex: 0 0 auto; padding: 3px 8px; border: 1px solid var(--co-status-neutral-border); border-radius: var(--co-radius-pill); color: var(--co-status-neutral-fg); background: var(--co-status-neutral-bg); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.filter-grid { display: grid; grid-template-columns: repeat(4, minmax(150px, 1fr)); min-width: 0; gap: var(--co-space-3); }
.filter-grid label { display: grid; min-width: 0; gap: 5px; color: var(--co-text-secondary); font-size: 11px; font-weight: 700; }
.filter-grid input, .filter-grid select { width: 100%; min-width: 0; min-height: 40px; padding: 0 var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background-color: var(--co-bg-surface); font: inherit; font-weight: 500; }
.filter-grid input:hover, .filter-grid select:hover { border-color: var(--co-border-strong); }
.filter-grid input:focus-visible, .filter-grid select:focus-visible, button:focus-visible { outline: 2px solid var(--co-action-primary); outline-offset: 2px; }
.filter-actions { flex-wrap: wrap; gap: var(--co-space-2); }
.primary-action, .secondary-action { display: inline-flex; min-height: 42px; align-items: center; justify-content: center; gap: 7px; padding: 0 var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); cursor: pointer; font-weight: 750; }
.primary-action { border-color: var(--co-action-primary); color: var(--co-text-on-action); background: var(--co-action-primary); }
.secondary-action { color: var(--co-text-primary); background: var(--co-bg-surface); }
button:hover { border-color: var(--co-border-strong); }
button:disabled { cursor: not-allowed; opacity: .55; }
@media (max-width: 1050px) { .filter-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 600px) {
  .filter-contract { display: none; }
  .filter-grid { grid-template-columns: minmax(0, 1fr); }
  .filter-grid label { font-size: 12px; }
  .filter-grid input, .filter-grid select { min-height: 44px; font-size: 16px; }
  .filter-actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .primary-action, .secondary-action { width: 100%; padding-inline: var(--co-space-2); }
}
</style>
