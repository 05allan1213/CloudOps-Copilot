<script setup lang="ts">
import { computed } from "vue";
import ContextToolbar from "../workspace/ContextToolbar.vue";
import { incidentStatusLabel, incidentStatuses, severityLabel } from "../../models/incidents";
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

const allValue = "__all__";
const statusItems = [
  { label: "全部状态", value: allValue },
  ...incidentStatuses.map((value) => ({ label: incidentStatusLabel(value), value })),
];
const severityItems = [
  { label: "全部级别", value: allValue },
  ...(["critical", "warning", "info", "unknown"] as const).map((value) => ({
    label: severityLabel(value),
    value,
  })),
];
const attentionItems = [
  { label: "全部 Attention", value: allValue },
  { label: "需要 Attention", value: "true" },
  { label: "无需 Attention", value: "false" },
];

const statusModel = computed({
  get: () => props.status ?? allValue,
  set: (value: string) => emit("update:status", value === allValue ? undefined : value as IncidentStatus),
});
const severityModel = computed({
  get: () => props.severity ?? allValue,
  set: (value: string) => emit("update:severity", value === allValue ? undefined : value as IncidentSeverity),
});
const attentionModel = computed({
  get: () => props.attention === undefined ? allValue : String(props.attention),
  set: (value: string) => emit("update:attention", value === allValue ? undefined : value === "true"),
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
  <ContextToolbar label="Incident 筛选与查询">
    <template #filters>
      <form
        id="incident-filter-form"
        class="filter-form"
        data-testid="incident-filter-form"
        @submit.prevent="emit('apply')"
      >
        <UFormField label="状态">
          <USelect
            v-model="statusModel"
            :items="statusItems"
            value-key="value"
            aria-label="状态"
          />
        </UFormField>
        <UFormField label="级别">
          <USelect
            v-model="severityModel"
            :items="severityItems"
            value-key="value"
            aria-label="级别"
          />
        </UFormField>
        <UFormField label="Attention">
          <USelect
            v-model="attentionModel"
            :items="attentionItems"
            value-key="value"
            aria-label="Attention"
          />
        </UFormField>
        <UFormField label="服务">
          <UInput
            v-model="serviceModel"
            name="service"
            placeholder="checkout-api"
            aria-label="服务"
          />
        </UFormField>
        <UFormField label="资源">
          <UInput
            v-model="resourceModel"
            name="resource"
            placeholder="deployment/api"
            aria-label="资源"
          />
        </UFormField>
        <UFormField label="Alert UUID">
          <UInput
            v-model="alertModel"
            name="alert"
            placeholder="public Alert ID"
            aria-label="Alert UUID"
          />
        </UFormField>
        <UFormField label="从">
          <UInput
            v-model="fromModel"
            type="datetime-local"
            name="from"
            aria-label="从"
          />
        </UFormField>
        <UFormField label="到">
          <UInput
            v-model="toModel"
            type="datetime-local"
            name="to"
            aria-label="到"
          />
        </UFormField>
      </form>
    </template>
    <template #secondary>
      <span class="filter-contract">URL 已同步</span>
    </template>
    <template #primary>
      <UButton
        type="submit"
        form="incident-filter-form"
        data-testid="incident-filter-apply"
        color="primary"
        icon="i-lucide-search"
        :loading="loading"
        label="应用筛选"
      />
      <UButton
        type="button"
        color="neutral"
        variant="ghost"
        icon="i-lucide-filter-x"
        label="清除"
        @click="emit('reset')"
      />
    </template>
  </ContextToolbar>
</template>

<style scoped>
.filter-form { display: flex; min-width: 0; flex: 1 1 760px; flex-wrap: wrap; align-items: flex-end; gap: var(--co-space-2); }
.filter-form :deep(.u-form-field) { min-width: 112px; }
.filter-form :deep(.u-input), .filter-form :deep(.u-select) { min-width: 112px; }
.filter-contract { color: var(--co-text-muted); font-size: 11px; white-space: nowrap; }
@media (max-width: 767px) {
  .filter-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); width: 100%; }
  .filter-form :deep(.u-form-field), .filter-form :deep(.u-input), .filter-form :deep(.u-select) { min-width: 0; width: 100%; }
}
</style>
