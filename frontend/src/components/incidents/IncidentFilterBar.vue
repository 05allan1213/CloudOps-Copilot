<script setup lang="ts">
import { computed } from "vue";
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
const quickItems = [
  { label: "全部", value: "all", icon: "i-lucide-list-filter" },
  { label: "调查中", value: "investigating", icon: "i-lucide-search-check" },
  { label: "需关注", value: "attention", icon: "i-lucide-triangle-alert" },
  { label: "已关闭", value: "closed", icon: "i-lucide-circle-check" },
];
const activeAdvancedCount = computed(() => [
  props.severity,
  props.attention === undefined ? undefined : String(props.attention),
  props.resource,
  props.alert,
  props.from,
  props.to,
].filter(Boolean).length);

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
const quickModel = computed({
  get: () => {
    if (props.attention === true && !props.status) return "attention";
    if (props.status === "investigating" && props.attention === undefined) return "investigating";
    if (props.status === "closed" && props.attention === undefined) return "closed";
    if (!props.status && props.attention === undefined) return "all";
    return "custom";
  },
  set: (value: string) => {
    emit("update:attention", value === "attention" ? true : undefined);
    emit("update:status", value === "investigating" || value === "closed" ? value : undefined);
  },
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
  <section
    class="incident-commandbar"
    aria-label="Incident 筛选与查询"
  >
    <form
      id="incident-filter-form"
      class="filter-form"
      data-testid="incident-filter-form"
      @submit.prevent="emit('apply')"
    >
      <UTabs
        v-model="quickModel"
        class="incident-status-tabs"
        :items="quickItems"
        :content="false"
        color="primary"
        variant="pill"
        size="sm"
        aria-label="Incident 高频筛选"
      />
      <UInput
        v-model="serviceModel"
        class="incident-primary-search"
        name="service"
        icon="i-lucide-search"
        placeholder="搜索服务或响应对象"
        aria-label="搜索服务"
      />
      <UCollapsible class="incident-advanced-filters">
        <template #default="{ open }">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-sliders-horizontal"
            :trailing-icon="open ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
            :label="activeAdvancedCount ? `高级 · ${activeAdvancedCount}` : '高级'"
            :aria-label="`${open ? '收起' : '展开'} Incident 高级筛选`"
          />
        </template>
        <template #content>
          <div class="incident-advanced-grid">
            <UFormField label="生命周期状态">
              <USelect
                v-model="statusModel"
                :items="statusItems"
                value-key="value"
                aria-label="Incident 生命周期状态"
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
          </div>
        </template>
      </UCollapsible>
      <UButton
        type="submit"
        form="incident-filter-form"
        data-testid="incident-filter-apply"
        color="primary"
        icon="i-lucide-search"
        :loading="loading"
        label="应用"
      />
      <UButton
        type="button"
        color="neutral"
        variant="ghost"
        icon="i-lucide-filter-x"
        square
        aria-label="清除 Incident 筛选"
        @click="emit('reset')"
      />
    </form>
  </section>
</template>

<style scoped>
.incident-commandbar { min-width: 0; padding: var(--co-space-2); border: 1px solid var(--co-border-subtle); border-radius: var(--co-radius-frame); background: color-mix(in srgb, var(--co-bg-surface) 88%, var(--co-bg-canvas)); }
.filter-form { display: grid; min-width: 0; grid-template-columns: minmax(300px, auto) minmax(220px, 1fr) auto auto auto; align-items: center; gap: var(--co-space-2); }
.incident-status-tabs,
.incident-primary-search { min-width: 0; }
.incident-primary-search { width: 100%; }
.incident-advanced-filters { position: relative; }
.incident-advanced-grid { position: absolute; z-index: var(--co-z-popover); top: calc(100% + var(--co-space-2)); right: 0; display: grid; width: min(660px, calc(100vw - 64px)); grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--co-space-3); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-overlay); box-shadow: var(--co-shadow-overlay); }
.incident-advanced-grid :deep(.u-input), .incident-advanced-grid :deep(.u-select) { width: 100%; }

@media (max-width: 1180px) {
  .filter-form { grid-template-columns: minmax(280px, 1fr) minmax(220px, 1fr) auto auto auto; }
  .incident-status-tabs { grid-column: 1 / -1; }
}

@media (max-width: 1024px) {
  .filter-form { grid-template-columns: minmax(0, 1fr) auto auto auto; }
  .incident-status-tabs { grid-column: 1 / -1; }
  .incident-advanced-grid { position: static; width: 100%; grid-template-columns: repeat(2, minmax(0, 1fr)); margin-top: var(--co-space-2); box-shadow: none; }
  .incident-advanced-filters { position: static; }
}
</style>
