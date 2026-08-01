<script setup lang="ts">
import { computed } from "vue";

import type { KubernetesResource } from "../../api/infrastructure";
import type { MonitoringCatalog, QueryMode } from "../../api/monitoring";

const props = defineProps<{
  namespaces: string[];
  resources: KubernetesResource[];
  catalog: MonitoringCatalog | null;
  namespace: string;
  resourceID: string;
  mode: QueryMode;
  guidedKey: string;
  expertQuery: string;
  from: string;
  to: string;
  stepSeconds: number;
  validTimeRange: boolean;
  canRun: boolean;
  running: boolean;
  queryInFlight: boolean;
}>();

const emit = defineEmits<{
  "update:namespace": [value: string];
  "update:resourceID": [value: string];
  "update:mode": [value: QueryMode];
  "update:guidedKey": [value: string];
  "update:expertQuery": [value: string];
  "update:from": [value: string];
  "update:to": [value: string];
  "update:stepSeconds": [value: number];
  namespaceChange: [];
  resourceChange: [];
  guidedChange: [];
  queryChange: [];
  preset: [minutes: number];
  run: [];
  cancel: [];
}>();

const namespaceItems = computed(() => props.namespaces.map((value) => ({ label: value, value })));
const resourceItems = computed(() => props.resources.map((resource) => ({
  label: `${resource.kind} · ${resource.name}`,
  value: resource.id,
})));
const queryItems = computed(() => (props.catalog?.queries ?? []).map((entry) => ({
  label: entry.title,
  value: entry.key,
})));
const stepItems = [
  { label: "15s", value: 15 },
  { label: "30s", value: 30 },
  { label: "1m", value: 60 },
  { label: "5m", value: 300 },
];
const selectedCatalogEntry = computed(() => props.catalog?.queries.find((item) => item.key === props.guidedKey));
const selectedResource = computed(() => props.resources.find((item) => item.id === props.resourceID));
const queryByteLength = computed(() => new TextEncoder().encode(props.expertQuery).length);

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function numberValue(value: unknown): number {
  return typeof value === "number" ? value : Number(value) || 30;
}

</script>

<template>
  <section
    class="monitoring-query"
    aria-label="监控查询"
  >
    <div class="monitoring-query__primary">
      <div class="monitoring-query__identity">
        <span
          class="monitoring-query__icon"
          aria-hidden="true"
        ><UIcon name="i-lucide-chart-no-axes-combined" /></span>
        <div>
          <strong>{{ mode === "guided" ? selectedCatalogEntry?.title || "指标画布" : "PromQL 分析" }}</strong>
          <small>{{ selectedResource ? `${selectedResource.kind} ${selectedResource.name}` : "当前 Scope" }} · {{ catalog?.provider_state === "available" ? "Prometheus 可用" : catalog?.provider_state === "partial" ? "Prometheus 部分可用" : "正在确认 Provider" }}</small>
        </div>
      </div>
      <label class="monitoring-query__resource">
        <span>Workload</span>
        <USelect
          :model-value="resourceID"
          :items="resourceItems"
          value-key="value"
          label-key="label"
          aria-label="Workload"
          :disabled="queryInFlight"
          @update:model-value="emit('update:resourceID', stringValue($event))"
          @change="emit('resourceChange')"
        />
      </label>
      <div
        class="monitoring-query__mode"
        role="group"
        aria-label="查询模式"
      >
        <UButton
          :color="mode === 'guided' ? 'primary' : 'neutral'"
          :variant="mode === 'guided' ? 'soft' : 'ghost'"
          label="可视化查询"
          :aria-pressed="mode === 'guided'"
          :disabled="queryInFlight"
          @click="emit('update:mode', 'guided')"
        />
        <UButton
          :color="mode === 'expert' ? 'primary' : 'neutral'"
          :variant="mode === 'expert' ? 'soft' : 'ghost'"
          label="PromQL"
          :aria-pressed="mode === 'expert'"
          :disabled="queryInFlight"
          @click="emit('update:mode', 'expert')"
        />
      </div>
      <label
        v-if="mode === 'guided'"
        class="monitoring-query__definition"
      >
        <span>指标视图</span>
        <USelect
          :model-value="guidedKey"
          :items="queryItems"
          value-key="value"
          label-key="label"
          aria-label="指标视图"
          :disabled="queryInFlight"
          @update:model-value="emit('update:guidedKey', stringValue($event))"
          @change="emit('guidedChange')"
        />
      </label>
      <div
        class="monitoring-query__presets"
        role="group"
        aria-label="时间范围"
      >
        <UButton
          color="neutral"
          variant="ghost"
          size="sm"
          label="15m"
          :disabled="queryInFlight"
          @click="emit('preset', 15)"
        />
        <UButton
          color="neutral"
          variant="ghost"
          size="sm"
          label="1h"
          :disabled="queryInFlight"
          @click="emit('preset', 60)"
        />
        <UButton
          color="neutral"
          variant="ghost"
          size="sm"
          label="6h"
          :disabled="queryInFlight"
          @click="emit('preset', 360)"
        />
      </div>
      <UButton
        v-if="queryInFlight"
        class="monitoring-query__cancel"
        color="error"
        variant="outline"
        icon="i-lucide-square"
        label="取消"
        @click="emit('cancel')"
      />
      <UButton
        class="monitoring-query__run"
        color="primary"
        icon="i-lucide-play"
        label="执行查询"
        :loading="running"
        :disabled="!canRun || !validTimeRange"
        @click="emit('run')"
      />
    </div>

    <label
      v-if="mode === 'expert'"
      class="monitoring-query__promql"
    >
      <span>PromQL <b>{{ queryByteLength }} / 8192 bytes</b></span>
      <UTextarea
        :model-value="expertQuery"
        :rows="2"
        autoresize
        name="expert-promql"
        spellcheck="false"
        autocomplete="off"
        aria-label="PromQL"
        :disabled="queryInFlight"
        @update:model-value="emit('update:expertQuery', stringValue($event)); emit('queryChange')"
      />
    </label>

    <UCollapsible class="monitoring-query__advanced">
      <template #default="{ open }">
        <UButton
          color="neutral"
          variant="ghost"
          block
          icon="i-lucide-sliders-horizontal"
          label="高级时间与采样参数"
          :trailing-icon="open ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
        />
      </template>
      <template #content>
        <div class="monitoring-query__time">
          <label>
            <span>Namespace</span>
            <USelect
              :model-value="namespace"
              :items="namespaceItems"
              value-key="value"
              label-key="label"
              aria-label="Namespace"
              :disabled="queryInFlight"
              @update:model-value="emit('update:namespace', stringValue($event))"
              @change="emit('namespaceChange')"
            />
          </label>
          <label>
            <span>开始</span>
            <UInput
              :model-value="from"
              type="datetime-local"
              name="monitoring-from"
              autocomplete="off"
              aria-label="监控开始时间"
              :disabled="queryInFlight"
              @update:model-value="emit('update:from', stringValue($event))"
              @change="emit('queryChange')"
            />
          </label>
          <label>
            <span>结束</span>
            <UInput
              :model-value="to"
              type="datetime-local"
              name="monitoring-to"
              autocomplete="off"
              aria-label="监控结束时间"
              :disabled="queryInFlight"
              @update:model-value="emit('update:to', stringValue($event))"
              @change="emit('queryChange')"
            />
          </label>
          <label class="monitoring-query__step">
            <span>采样间隔</span>
            <USelect
              :model-value="stepSeconds"
              :items="stepItems"
              value-key="value"
              label-key="label"
              aria-label="查询 Step"
              :disabled="queryInFlight"
              @update:model-value="emit('update:stepSeconds', numberValue($event))"
              @change="emit('queryChange')"
            />
          </label>
          <div
            class="monitoring-query__bounds"
            aria-label="查询边界"
          >
            <span>{{ selectedCatalogEntry?.description || catalog?.provider_detail || "等待查询目录" }}</span>
            <small>
              Lookback ≤ {{ Math.round((catalog?.bounds.max_lookback_seconds ?? 0) / 3600) }}h ·
              Series ≤ {{ catalog?.bounds.max_series ?? 0 }} · Samples ≤ {{ catalog?.bounds.max_samples ?? 0 }}
            </small>
          </div>
        </div>
      </template>
    </UCollapsible>
  </section>
</template>

<style scoped>
.monitoring-query {
  min-width: 0;
  display: grid;
  gap: var(--co-space-2);
}
.monitoring-query__primary,
.monitoring-query__time {
  min-width: 0;
  align-items: end;
  gap: var(--co-space-2);
  padding: var(--co-space-3) var(--co-space-4);
}
.monitoring-query__primary { display: grid; grid-template-columns: minmax(150px, 0.65fr) minmax(150px, 0.65fr) minmax(180px, 0.9fr) auto auto auto; border-radius: var(--co-radius-frame); background: color-mix(in srgb, var(--co-bg-surface) 82%, var(--co-bg-canvas)); box-shadow: var(--co-shadow-row); }
.monitoring-query__identity { display: flex; min-width: 0; grid-column: 1 / 4; grid-row: 1; align-items: center; gap: var(--co-space-2); }
.monitoring-query__identity > div { display: grid; min-width: 0; gap: 1px; }
.monitoring-query__identity strong { font-size: 12px; }
.monitoring-query__identity small { overflow: hidden; color: var(--co-text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.monitoring-query__icon { display: grid; width: var(--co-status-icon-size); height: var(--co-status-icon-size); flex: 0 0 auto; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-status-info-fg); background: var(--co-bg-floating); }
.monitoring-query label { display: grid; min-width: 0; gap: 3px; }
.monitoring-query label > span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; }
.monitoring-query label > span b { float: right; margin-left: var(--co-space-2); font-weight: 500; font-variant-numeric: tabular-nums; }
.monitoring-query__primary > .monitoring-query__resource { grid-column: 1 / 3; grid-row: 2; }
.monitoring-query__primary > .monitoring-query__definition { grid-column: 3; grid-row: 2; }
.monitoring-query__mode,
.monitoring-query__presets { display: inline-flex; flex: 0 0 auto; gap: 1px; padding: 2px; border-radius: var(--co-radius-control); background: var(--co-bg-canvas); }
.monitoring-query__mode { grid-column: 4 / 7; grid-row: 1; justify-self: end; }
.monitoring-query__presets { grid-column: 4; grid-row: 2; }
.monitoring-query__cancel { grid-column: 5; grid-row: 2; }
.monitoring-query__run { grid-column: 6; grid-row: 2; }
.monitoring-query__promql { padding: var(--co-space-3) var(--co-space-4); border-radius: var(--co-radius-frame); background: color-mix(in srgb, var(--co-bg-surface) 82%, var(--co-bg-canvas)); box-shadow: var(--co-shadow-row); }
.monitoring-query__promql :deep(textarea) { font-family: var(--co-font-mono); font-size: 12px; }
.monitoring-query__advanced { overflow: hidden; border-radius: var(--co-radius-panel); background: color-mix(in srgb, var(--co-bg-surface) 62%, transparent); }
.monitoring-query__advanced > :deep(button) { min-height: var(--co-control-height); justify-content: flex-start; border-radius: var(--co-radius-panel); }
.monitoring-query__time { display: grid; grid-template-columns: minmax(140px, 0.7fr) repeat(2, minmax(170px, 0.9fr)) minmax(92px, 0.45fr) minmax(200px, 1fr); }
.monitoring-query__bounds { display: grid; min-width: 160px; flex: 1 1 260px; gap: 2px; color: var(--co-text-secondary); font-size: 11px; }
.monitoring-query__bounds span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.monitoring-query__bounds small { color: var(--co-text-muted); font-variant-numeric: tabular-nums; }

@media (max-width: 1180px) {
  .monitoring-query__primary { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto auto; }
  .monitoring-query__identity { grid-column: 1 / 3; }
  .monitoring-query__mode { grid-column: 3 / 5; }
  .monitoring-query__primary > .monitoring-query__resource { grid-column: 1 / 3; }
  .monitoring-query__primary > .monitoring-query__definition { grid-column: 3; }
  .monitoring-query__presets { grid-column: 1 / 3; grid-row: 3; justify-self: start; }
  .monitoring-query__cancel { grid-column: 3; grid-row: 3; }
  .monitoring-query__run { grid-column: 4; grid-row: 3; }
  .monitoring-query__time { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .monitoring-query__bounds { grid-column: 1 / -1; }
}

@media (max-width: 1024px) {
  .monitoring-query__primary { grid-template-columns: minmax(0, 1fr) auto; }
  .monitoring-query__identity { grid-column: 1; }
  .monitoring-query__mode { grid-column: 2; }
  .monitoring-query__primary > .monitoring-query__resource,
  .monitoring-query__primary > .monitoring-query__definition { grid-column: 1 / -1; grid-row: auto; }
  .monitoring-query__presets { grid-column: 1; grid-row: auto; }
  .monitoring-query__cancel,
  .monitoring-query__run { grid-column: auto; grid-row: auto; }
}

@media (prefers-reduced-motion: reduce) {
  .monitoring-query * { scroll-behavior: auto; }
}
</style>
