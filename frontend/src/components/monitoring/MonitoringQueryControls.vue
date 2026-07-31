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

function inputValue(event: Event): string {
  return (event.currentTarget as HTMLInputElement | HTMLTextAreaElement).value;
}
</script>

<template>
  <section
    class="monitoring-query"
    aria-label="监控查询"
  >
    <div class="monitoring-query__context">
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
          label="引导"
          :aria-pressed="mode === 'guided'"
          :disabled="queryInFlight"
          @click="emit('update:mode', 'guided')"
        />
        <UButton
          :color="mode === 'expert' ? 'primary' : 'neutral'"
          :variant="mode === 'expert' ? 'soft' : 'ghost'"
          label="Expert"
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
      <label
        v-else
        class="monitoring-query__promql"
      >
        <span>PromQL <b>{{ queryByteLength }} / 8192 bytes</b></span>
        <textarea
          :value="expertQuery"
          name="expert-promql"
          rows="2"
          spellcheck="false"
          autocomplete="off"
          :disabled="queryInFlight"
          @input="emit('update:expertQuery', inputValue($event)); emit('queryChange')"
        />
      </label>
    </div>

    <div class="monitoring-query__time">
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
      <label>
        <span>开始</span>
        <input
          :value="from"
          type="datetime-local"
          name="monitoring-from"
          autocomplete="off"
          :disabled="queryInFlight"
          @input="emit('update:from', inputValue($event))"
          @change="emit('queryChange')"
        >
      </label>
      <label>
        <span>结束</span>
        <input
          :value="to"
          type="datetime-local"
          name="monitoring-to"
          autocomplete="off"
          :disabled="queryInFlight"
          @input="emit('update:to', inputValue($event))"
          @change="emit('queryChange')"
        >
      </label>
      <label class="monitoring-query__step">
        <span>Step</span>
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
  </section>
</template>

<style scoped>
.monitoring-query {
  position: sticky;
  top: var(--co-header-height);
  z-index: calc(var(--co-z-header) - 1);
  min-width: 0;
  border-block: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}
.monitoring-query__context,
.monitoring-query__time {
  display: flex;
  min-width: 0;
  align-items: end;
  gap: var(--co-space-2);
  padding: var(--co-space-2) var(--co-space-3);
}
.monitoring-query__context { border-bottom: 1px solid var(--co-border-subtle); }
.monitoring-query label { display: grid; min-width: 0; gap: 3px; }
.monitoring-query label > span { color: var(--co-text-muted); font-size: 10px; font-weight: 700; }
.monitoring-query label > span b { float: right; margin-left: var(--co-space-2); font-weight: 500; font-variant-numeric: tabular-nums; }
.monitoring-query__context > label { flex: 0 1 180px; }
.monitoring-query__context > .monitoring-query__resource { flex-basis: 260px; }
.monitoring-query__context > .monitoring-query__definition,
.monitoring-query__context > .monitoring-query__promql { flex: 1 1 360px; }
.monitoring-query__mode,
.monitoring-query__presets { display: inline-flex; flex: 0 0 auto; gap: 1px; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); }
.monitoring-query__promql textarea,
.monitoring-query__time input {
  width: 100%;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
  background: var(--co-bg-canvas);
  color: var(--co-text-primary);
}
.monitoring-query__promql textarea { min-height: 48px; resize: vertical; padding: var(--co-space-2); font-family: var(--co-font-mono); font-size: 12px; line-height: 1.35; }
.monitoring-query__time input { min-height: 32px; padding: 4px var(--co-space-2); font-size: 12px; }
.monitoring-query__time > label { flex: 0 1 178px; }
.monitoring-query__time > .monitoring-query__step { flex-basis: 92px; }
.monitoring-query__bounds { display: grid; min-width: 160px; flex: 1 1 260px; gap: 2px; color: var(--co-text-secondary); font-size: 11px; }
.monitoring-query__bounds span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.monitoring-query__bounds small { color: var(--co-text-muted); font-variant-numeric: tabular-nums; }

@media (max-width: 1180px) {
  .monitoring-query { position: static; }
  .monitoring-query__context,
  .monitoring-query__time { flex-wrap: wrap; }
  .monitoring-query__context > .monitoring-query__definition,
  .monitoring-query__context > .monitoring-query__promql { flex-basis: 100%; }
  .monitoring-query__bounds { order: 3; flex-basis: 100%; }
}
</style>
