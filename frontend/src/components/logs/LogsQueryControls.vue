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
  text: string;
  traceID: string;
  levels: string[];
  expertQuery: string;
  from: string;
  to: string;
  limit: number;
  tail: boolean;
  validTimeRange: boolean;
  canRun: boolean;
  querying: boolean;
}>();

const emit = defineEmits<{
  "update:namespace": [value: string];
  "update:resourceID": [value: string];
  "update:mode": [value: TelemetryQueryMode];
  "update:text": [value: string];
  "update:traceID": [value: string];
  "update:expertQuery": [value: string];
  "update:from": [value: string];
  "update:to": [value: string];
  "update:limit": [value: number];
  "update:tail": [value: boolean];
  levelToggle: [level: string, checked: boolean];
  namespaceChange: [];
  resourceChange: [];
  preset: [minutes: number];
  run: [];
  cancel: [];
}>();

const namespaceItems = computed(() => props.namespaces.map((value) => ({ label: value, value })));
const resourceItems = computed(() => props.resources.map((resource) => ({
  label: `${resource.kind} · ${resource.name}`,
  value: resource.id,
})));
const limitItems = [1, 100, 200, 500, 1000].map((value) => ({ label: String(value), value }));
const levelItems = ["debug", "info", "warn", "error"];

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function numberValue(value: unknown): number {
  return typeof value === "number" ? value : Number(value) || 200;
}
</script>

<template>
  <section
    class="logs-query"
    aria-label="日志查询"
  >
    <div class="logs-query__context">
      <label>
        <span>Namespace</span>
        <USelect
          :model-value="namespace"
          :items="namespaceItems"
          value-key="value"
          label-key="label"
          aria-label="Namespace"
          :disabled="querying"
          @update:model-value="emit('update:namespace', stringValue($event))"
          @change="emit('namespaceChange')"
        />
      </label>
      <label class="logs-query__resource">
        <span>Workload</span>
        <USelect
          :model-value="resourceID"
          :items="resourceItems"
          value-key="value"
          label-key="label"
          aria-label="Workload"
          :disabled="querying"
          @update:model-value="emit('update:resourceID', stringValue($event))"
          @change="emit('resourceChange')"
        />
      </label>
      <div
        class="logs-query__mode"
        role="group"
        aria-label="日志查询模式"
      >
        <UButton
          :color="mode === 'guided' ? 'primary' : 'neutral'"
          :variant="mode === 'guided' ? 'soft' : 'ghost'"
          label="引导"
          :aria-pressed="mode === 'guided'"
          :disabled="querying"
          @click="emit('update:mode', 'guided')"
        />
        <UButton
          :color="mode === 'expert' ? 'primary' : 'neutral'"
          :variant="mode === 'expert' ? 'soft' : 'ghost'"
          label="Expert"
          :aria-pressed="mode === 'expert'"
          :disabled="querying"
          @click="emit('update:mode', 'expert')"
        />
      </div>
    </div>

    <div
      v-if="mode === 'guided'"
      class="logs-query__filters"
    >
      <label>
        <span>文本</span>
        <UInput
          :model-value="text"
          icon="i-lucide-search"
          placeholder="例如：timeout"
          aria-label="日志文本过滤"
          :disabled="querying"
          @update:model-value="emit('update:text', stringValue($event))"
        />
      </label>
      <label>
        <span>Trace ID</span>
        <UInput
          :model-value="traceID"
          icon="i-lucide-git-branch"
          placeholder="例如：038cbd20"
          aria-label="Trace ID"
          :disabled="querying"
          @update:model-value="emit('update:traceID', stringValue($event))"
        />
      </label>
      <fieldset>
        <legend>级别</legend>
        <UCheckbox
          v-for="level in levelItems"
          :key="level"
          :model-value="levels.includes(level)"
          :label="level.toUpperCase()"
          :disabled="querying"
          @update:model-value="emit('levelToggle', level, Boolean($event))"
        />
      </fieldset>
    </div>
    <label
      v-else
      class="logs-query__expert"
    >
      <span>Elasticsearch query clause</span>
      <UTextarea
        :model-value="expertQuery"
        :rows="3"
        autoresize
        spellcheck="false"
        aria-label="Elasticsearch query clause"
        :disabled="querying"
        @update:model-value="emit('update:expertQuery', stringValue($event))"
      />
    </label>

    <div class="logs-query__execution">
      <div
        class="logs-query__presets"
        role="group"
        aria-label="日志时间范围"
      >
        <UButton
          color="neutral"
          variant="ghost"
          size="sm"
          label="15m"
          :disabled="querying"
          @click="emit('preset', 15)"
        />
        <UButton
          color="neutral"
          variant="ghost"
          size="sm"
          label="1h"
          :disabled="querying"
          @click="emit('preset', 60)"
        />
        <UButton
          color="neutral"
          variant="ghost"
          size="sm"
          label="6h"
          :disabled="querying"
          @click="emit('preset', 360)"
        />
      </div>
      <label>
        <span>开始</span>
        <UInput
          :model-value="from"
          type="datetime-local"
          aria-label="日志开始时间"
          :disabled="querying"
          @update:model-value="emit('update:from', stringValue($event))"
        />
      </label>
      <label>
        <span>结束</span>
        <UInput
          :model-value="to"
          type="datetime-local"
          aria-label="日志结束时间"
          :disabled="querying"
          @update:model-value="emit('update:to', stringValue($event))"
        />
      </label>
      <label class="logs-query__limit">
        <span>上限</span>
        <USelect
          :model-value="limit"
          :items="limitItems"
          value-key="value"
          label-key="label"
          aria-label="日志结果上限"
          :disabled="querying"
          @update:model-value="emit('update:limit', numberValue($event))"
        />
      </label>
      <USwitch
        :model-value="tail"
        label="Tail（有界）"
        :disabled="querying"
        @update:model-value="emit('update:tail', Boolean($event))"
      />
      <div class="logs-query__actions">
        <UButton
          v-if="querying"
          color="error"
          variant="soft"
          icon="i-lucide-square"
          label="停止等待"
          @click="emit('cancel')"
        />
        <UButton
          color="primary"
          icon="i-lucide-play"
          label="执行查询"
          :loading="querying"
          :disabled="!canRun || !validTimeRange"
          @click="emit('run')"
        />
      </div>
    </div>

    <div class="logs-query__bounds">
      <span>Lookback ≤ {{ Math.round((catalog?.bounds.max_lookback_seconds ?? 0) / 3600) }}h</span>
      <span>Rows ≤ {{ catalog?.bounds.max_results ?? 0 }}</span>
      <span>Response ≤ {{ Math.round((catalog?.bounds.max_response_bytes ?? 0) / 1024) }} KiB</span>
      <span>Timeout {{ catalog?.bounds.timeout_ms ?? 0 }}ms</span>
    </div>
  </section>
</template>

<style scoped>
.logs-query {
  position: sticky;
  z-index: var(--co-z-sticky);
  top: 0;
  border-block: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}
.logs-query__context,
.logs-query__filters,
.logs-query__execution {
  display: grid;
  align-items: end;
  gap: var(--co-space-3);
  padding: var(--co-space-3) 0;
  border-bottom: 1px solid var(--co-border-subtle);
}
.logs-query__context { grid-template-columns: minmax(150px, 0.65fr) minmax(220px, 1.25fr) auto; }
.logs-query__filters { grid-template-columns: minmax(180px, 1fr) minmax(220px, 1.1fr) auto; }
.logs-query__execution { grid-template-columns: auto repeat(2, minmax(170px, 0.9fr)) 100px auto auto; }
.logs-query label,
.logs-query__expert { display: grid; min-width: 0; gap: var(--co-space-1); }
.logs-query label > span,
.logs-query__expert > span,
.logs-query legend { color: var(--co-text-muted); font-size: 11px; font-weight: 700; }
.logs-query__mode,
.logs-query__presets,
.logs-query__actions { display: flex; align-items: center; gap: var(--co-space-1); }
.logs-query__mode { align-self: end; }
.logs-query__filters fieldset { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-3); margin: 0; padding: var(--co-space-2); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); }
.logs-query__expert { padding: var(--co-space-3) 0; border-bottom: 1px solid var(--co-border-subtle); }
.logs-query__actions { justify-content: flex-end; }
.logs-query__bounds { display: flex; flex-wrap: wrap; gap: var(--co-space-1) var(--co-space-4); padding: var(--co-space-2) 0; color: var(--co-text-muted); font-size: 11px; }

@media (max-width: 1180px) {
  .logs-query { position: static; }
  .logs-query__execution { grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .logs-query__presets,
  .logs-query__actions { grid-column: span 2; }
}

@media (max-width: 1024px) {
  .logs-query__context,
  .logs-query__filters { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .logs-query__mode,
  .logs-query__filters fieldset { grid-column: 1 / -1; }
}
</style>
