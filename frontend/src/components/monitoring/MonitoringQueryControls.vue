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
          <strong>指标查询</strong>
          <small>{{ catalog?.provider_detail || "等待 Prometheus 查询目录" }}</small>
        </div>
      </div>
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
        color="error"
        variant="outline"
        icon="i-lucide-square"
        label="取消"
        @click="emit('cancel')"
      />
      <UButton
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
  border-block: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}
.monitoring-query__primary,
.monitoring-query__time {
  display: flex;
  min-width: 0;
  align-items: end;
  gap: var(--co-space-2);
  padding: var(--co-space-2) var(--co-space-3);
}
.monitoring-query__primary { position: sticky; top: var(--co-header-height); z-index: calc(var(--co-z-header) - 1); background: var(--co-bg-surface); }
.monitoring-query__identity { display: flex; min-width: 180px; flex: 0 1 220px; align-items: center; gap: var(--co-space-2); }
.monitoring-query__identity > div { display: grid; min-width: 0; gap: 1px; }
.monitoring-query__identity strong { font-size: 12px; }
.monitoring-query__identity small { overflow: hidden; color: var(--co-text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.monitoring-query__icon { display: grid; width: var(--co-status-icon-size); height: var(--co-status-icon-size); flex: 0 0 auto; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-status-info-fg); background: var(--co-bg-floating); }
.monitoring-query label { display: grid; min-width: 0; gap: 3px; }
.monitoring-query label > span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; }
.monitoring-query label > span b { float: right; margin-left: var(--co-space-2); font-weight: 500; font-variant-numeric: tabular-nums; }
.monitoring-query__primary > label { flex: 0 1 150px; }
.monitoring-query__primary > .monitoring-query__resource { flex-basis: 220px; }
.monitoring-query__primary > .monitoring-query__definition { flex: 1 1 240px; }
.monitoring-query__mode,
.monitoring-query__presets { display: inline-flex; flex: 0 0 auto; gap: 1px; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); }
.monitoring-query__promql { padding: var(--co-space-2) var(--co-space-3); border-top: 1px solid var(--co-border-subtle); }
.monitoring-query__promql :deep(textarea) { font-family: var(--co-font-mono); font-size: 12px; }
.monitoring-query__advanced { border-top: 1px solid var(--co-border-subtle); }
.monitoring-query__advanced > :deep(button) { min-height: var(--co-control-height); justify-content: flex-start; border-radius: 0; }
.monitoring-query__time > label { flex: 0 1 178px; }
.monitoring-query__time > .monitoring-query__step { flex-basis: 92px; }
.monitoring-query__bounds { display: grid; min-width: 160px; flex: 1 1 260px; gap: 2px; color: var(--co-text-secondary); font-size: 11px; }
.monitoring-query__bounds span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.monitoring-query__bounds small { color: var(--co-text-muted); font-variant-numeric: tabular-nums; }

@media (max-width: 1180px) {
  .monitoring-query__primary,
  .monitoring-query__time { flex-wrap: wrap; }
  .monitoring-query__primary { position: static; }
  .monitoring-query__identity { flex-basis: 100%; }
  .monitoring-query__primary > .monitoring-query__definition { flex-basis: 220px; }
  .monitoring-query__bounds { order: 3; flex-basis: 100%; }
}

@media (prefers-reduced-motion: reduce) {
  .monitoring-query * { scroll-behavior: auto; }
}
</style>
