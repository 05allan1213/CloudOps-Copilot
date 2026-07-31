<script setup lang="ts">
import { computed } from "vue";

import type { KubernetesResource } from "../../api/infrastructure";
import type { TelemetryCatalog, TelemetryQueryMode } from "../../api/telemetry";

const props = defineProps<{
  namespaces: string[];
  resources: KubernetesResource[];
  catalog: TelemetryCatalog | null;
  namespace: string;
  resourceID: string;
  mode: TelemetryQueryMode;
  service: string;
  operation: string;
  status: string;
  minDurationMS?: number;
  maxDurationMS?: number;
  expertQuery: string;
  from: string;
  to: string;
  limit: number;
  validTimeRange: boolean;
  canSearch: boolean;
  searching: boolean;
}>();

const emit = defineEmits<{
  "update:namespace": [value: string];
  "update:resourceID": [value: string];
  "update:mode": [value: TelemetryQueryMode];
  "update:service": [value: string];
  "update:operation": [value: string];
  "update:status": [value: string];
  "update:minDurationMS": [value: number | undefined];
  "update:maxDurationMS": [value: number | undefined];
  "update:expertQuery": [value: string];
  "update:from": [value: string];
  "update:to": [value: string];
  "update:limit": [value: number];
  namespaceChange: [];
  resourceChange: [];
  preset: [minutes: number];
  search: [];
  cancel: [];
}>();

const namespaceItems = computed(() => props.namespaces.map((value) => ({ label: value, value })));
const resourceItems = computed(() => props.resources.map((resource) => ({
  label: `${resource.kind} · ${resource.name}`,
  value: resource.id,
})));
const statusItems = [
  { label: "全部", value: "all" },
  { label: "Error", value: "error" },
  { label: "OK", value: "ok" },
];
const limitItems = [1, 50, 100, 200].map((value) => ({ label: String(value), value }));

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function numberValue(value: unknown, fallback: number): number {
  return typeof value === "number" ? value : Number(value) || fallback;
}

function optionalNumber(value: unknown): number | undefined {
  if (value === "" || value === null || value === undefined) return undefined;
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined;
}

function statusValue(value: unknown): string {
  const selected = stringValue(value);
  return selected === "all" ? "" : selected;
}
</script>

<template>
  <section
    class="traces-query"
    aria-label="Trace 搜索"
  >
    <div class="traces-query__context">
      <label>
        <span>Namespace</span>
        <USelect
          :model-value="namespace"
          :items="namespaceItems"
          value-key="value"
          label-key="label"
          aria-label="Namespace"
          :disabled="searching"
          @update:model-value="emit('update:namespace', stringValue($event))"
          @change="emit('namespaceChange')"
        />
      </label>
      <label class="traces-query__resource">
        <span>Workload</span>
        <USelect
          :model-value="resourceID"
          :items="resourceItems"
          value-key="value"
          label-key="label"
          aria-label="Workload"
          :disabled="searching"
          @update:model-value="emit('update:resourceID', stringValue($event))"
          @change="emit('resourceChange')"
        />
      </label>
      <div
        class="traces-query__mode"
        role="group"
        aria-label="Trace 查询模式"
      >
        <UButton
          :color="mode === 'guided' ? 'primary' : 'neutral'"
          :variant="mode === 'guided' ? 'soft' : 'ghost'"
          label="引导"
          :aria-pressed="mode === 'guided'"
          :disabled="searching"
          @click="emit('update:mode', 'guided')"
        />
        <UButton
          :color="mode === 'expert' ? 'primary' : 'neutral'"
          :variant="mode === 'expert' ? 'soft' : 'ghost'"
          label="Expert"
          :aria-pressed="mode === 'expert'"
          :disabled="searching"
          @click="emit('update:mode', 'expert')"
        />
      </div>
    </div>

    <div
      v-if="mode === 'guided'"
      class="traces-query__filters"
    >
      <label>
        <span>Service</span>
        <UInput
          :model-value="service"
          icon="i-lucide-box"
          placeholder="例如：cloudops-api"
          aria-label="Service"
          :disabled="searching"
          @update:model-value="emit('update:service', stringValue($event))"
        />
      </label>
      <label>
        <span>Operation</span>
        <UInput
          :model-value="operation"
          icon="i-lucide-route"
          placeholder="例如：GET /readyz"
          aria-label="Operation"
          :disabled="searching"
          @update:model-value="emit('update:operation', stringValue($event))"
        />
      </label>
      <label>
        <span>Status</span>
        <USelect
          :model-value="status || 'all'"
          :items="statusItems"
          value-key="value"
          label-key="label"
          aria-label="Trace Status"
          :disabled="searching"
          @update:model-value="emit('update:status', statusValue($event))"
        />
      </label>
      <label>
        <span>最短耗时（ms）</span>
        <UInput
          :model-value="minDurationMS"
          type="number"
          min="0"
          aria-label="最短耗时"
          :disabled="searching"
          @update:model-value="emit('update:minDurationMS', optionalNumber($event))"
        />
      </label>
      <label>
        <span>最长耗时（ms）</span>
        <UInput
          :model-value="maxDurationMS"
          type="number"
          min="0"
          aria-label="最长耗时"
          :disabled="searching"
          @update:model-value="emit('update:maxDurationMS', optionalNumber($event))"
        />
      </label>
    </div>
    <label
      v-else
      class="traces-query__expert"
    >
      <span>TraceQL span selector</span>
      <UTextarea
        :model-value="expertQuery"
        :rows="3"
        autoresize
        spellcheck="false"
        aria-label="TraceQL span selector"
        :disabled="searching"
        @update:model-value="emit('update:expertQuery', stringValue($event))"
      />
    </label>

    <div class="traces-query__execution">
      <div
        class="traces-query__presets"
        role="group"
        aria-label="Trace 时间范围"
      >
        <UButton
          color="neutral"
          variant="ghost"
          size="sm"
          label="15m"
          :disabled="searching"
          @click="emit('preset', 15)"
        />
        <UButton
          color="neutral"
          variant="ghost"
          size="sm"
          label="1h"
          :disabled="searching"
          @click="emit('preset', 60)"
        />
        <UButton
          color="neutral"
          variant="ghost"
          size="sm"
          label="6h"
          :disabled="searching"
          @click="emit('preset', 360)"
        />
      </div>
      <label>
        <span>开始</span>
        <UInput
          :model-value="from"
          type="datetime-local"
          aria-label="Trace 开始时间"
          :disabled="searching"
          @update:model-value="emit('update:from', stringValue($event))"
        />
      </label>
      <label>
        <span>结束</span>
        <UInput
          :model-value="to"
          type="datetime-local"
          aria-label="Trace 结束时间"
          :disabled="searching"
          @update:model-value="emit('update:to', stringValue($event))"
        />
      </label>
      <label class="traces-query__limit">
        <span>上限</span>
        <USelect
          :model-value="limit"
          :items="limitItems"
          value-key="value"
          label-key="label"
          aria-label="Trace 结果上限"
          :disabled="searching"
          @update:model-value="emit('update:limit', numberValue($event, 100))"
        />
      </label>
      <div class="traces-query__actions">
        <UButton
          v-if="searching"
          color="error"
          variant="soft"
          icon="i-lucide-square"
          label="停止等待"
          @click="emit('cancel')"
        />
        <UButton
          color="primary"
          icon="i-lucide-play"
          label="搜索 Trace"
          :loading="searching"
          :disabled="!canSearch || !validTimeRange"
          @click="emit('search')"
        />
      </div>
    </div>

    <div class="traces-query__bounds">
      <span>Lookback ≤ {{ Math.round((catalog?.bounds.max_lookback_seconds ?? 0) / 3600) }}h</span>
      <span>Traces ≤ {{ catalog?.bounds.max_results ?? 0 }}</span>
      <span>Response ≤ {{ Math.round((catalog?.bounds.max_response_bytes ?? 0) / 1024) }} KiB</span>
      <span>Timeout {{ catalog?.bounds.timeout_ms ?? 0 }}ms</span>
    </div>
  </section>
</template>

<style scoped>
.traces-query { border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.traces-query__context,
.traces-query__filters,
.traces-query__execution {
  display: grid;
  align-items: end;
  gap: var(--co-space-3);
  padding: var(--co-space-3) 0;
  border-bottom: 1px solid var(--co-border-subtle);
}
.traces-query__context { grid-template-columns: minmax(150px, 0.65fr) minmax(220px, 1.25fr) auto; }
.traces-query__filters { grid-template-columns: 1.1fr 1.1fr 0.65fr 0.7fr 0.7fr; }
.traces-query__execution { grid-template-columns: auto repeat(2, minmax(170px, 0.9fr)) 100px auto; }
.traces-query label,
.traces-query__expert { display: grid; min-width: 0; gap: var(--co-space-1); }
.traces-query label > span,
.traces-query__expert > span { color: var(--co-text-muted); font-size: 11px; font-weight: 700; }
.traces-query__mode,
.traces-query__presets,
.traces-query__actions { display: flex; align-items: center; gap: var(--co-space-1); }
.traces-query__expert { padding: var(--co-space-3) 0; border-bottom: 1px solid var(--co-border-subtle); }
.traces-query__actions { justify-content: flex-end; }
.traces-query__bounds { display: flex; flex-wrap: wrap; gap: var(--co-space-1) var(--co-space-4); padding: var(--co-space-2) 0; color: var(--co-text-muted); font-size: 11px; }

@media (max-width: 1180px) {
  .traces-query__filters { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .traces-query__execution { grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .traces-query__presets,
  .traces-query__actions { grid-column: span 2; }
}

@media (max-width: 1024px) {
  .traces-query__context,
  .traces-query__filters { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .traces-query__mode { grid-column: 1 / -1; }
}
</style>
