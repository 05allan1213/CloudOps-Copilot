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
const presets = [
  { label: "15m", value: 15 },
  { label: "1h", value: 60 },
  { label: "6h", value: 360 },
];

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
    class="trace-discovery-command"
    aria-label="Trace 发现"
  >
    <header class="trace-discovery-command__heading">
      <div>
        <span aria-hidden="true">
          <UIcon name="i-lucide-waypoints" />
        </span>
        <div>
          <small>TRACE DISCOVERY</small>
          <strong>{{ mode === "guided" ? "服务与操作发现" : "TraceQL 检索" }}</strong>
        </div>
      </div>
      <span class="trace-discovery-command__provider">
        <span :class="{ 'is-ready': catalog?.provider_state === 'available' || catalog?.provider_state === 'partial' }" />
        Tempo {{ catalog?.provider_state === "available" ? "可用" : catalog?.provider_state === "partial" ? "部分可用" : "检查中" }}
      </span>
    </header>
    <div class="trace-discovery-command__topline">
      <div class="trace-discovery-command__scope">
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
      </div>
      <div
        class="trace-discovery-command__mode"
        role="group"
        aria-label="Trace 查询模式"
      >
        <UButton
          :color="mode === 'guided' ? 'primary' : 'neutral'"
          :variant="mode === 'guided' ? 'soft' : 'ghost'"
          label="服务发现"
          :aria-pressed="mode === 'guided'"
          :disabled="searching"
          @click="emit('update:mode', 'guided')"
        />
        <UButton
          :color="mode === 'expert' ? 'primary' : 'neutral'"
          :variant="mode === 'expert' ? 'soft' : 'ghost'"
          label="TraceQL"
          :aria-pressed="mode === 'expert'"
          :disabled="searching"
          @click="emit('update:mode', 'expert')"
        />
      </div>
      <div
        class="trace-discovery-command__presets"
        role="group"
        aria-label="Trace 时间范围"
      >
        <UButton
          v-for="preset in presets"
          :key="preset.value"
          color="neutral"
          variant="ghost"
          size="sm"
          :label="preset.label"
          :disabled="searching"
          @click="emit('preset', preset.value)"
        />
      </div>
    </div>

    <div class="trace-discovery-command__searchline">
      <template v-if="mode === 'guided'">
        <UInput
          :model-value="service"
          class="trace-discovery-command__service"
          icon="i-lucide-box"
          size="xl"
          placeholder="Service，例如 cloudops-api"
          aria-label="Service"
          :disabled="searching"
          @update:model-value="emit('update:service', stringValue($event))"
        />
        <UInput
          :model-value="operation"
          class="trace-discovery-command__operation"
          icon="i-lucide-route"
          size="xl"
          placeholder="Operation，例如 GET /readyz"
          aria-label="Operation"
          :disabled="searching"
          @update:model-value="emit('update:operation', stringValue($event))"
        />
      </template>
      <UInput
        v-else
        :model-value="expertQuery"
        class="trace-discovery-command__traceql"
        icon="i-lucide-braces"
        size="xl"
        placeholder="TraceQL span selector"
        aria-label="TraceQL span selector"
        :disabled="searching"
        @update:model-value="emit('update:expertQuery', stringValue($event))"
      />
      <UPopover class="trace-discovery-command__advanced">
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-sliders-horizontal"
          label="高级"
        />
        <template #content>
          <div class="trace-discovery-command__advanced-content">
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
            <label>
              <span>结果上限</span>
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
            <div class="trace-discovery-command__bounds">
              <span>Lookback ≤ {{ Math.round((catalog?.bounds.max_lookback_seconds ?? 0) / 3600) }}h</span>
              <span>Traces ≤ {{ catalog?.bounds.max_results ?? 0 }}</span>
              <span>Response ≤ {{ Math.round((catalog?.bounds.max_response_bytes ?? 0) / 1024) }} KiB</span>
              <span>Timeout {{ catalog?.bounds.timeout_ms ?? 0 }}ms</span>
            </div>
          </div>
        </template>
      </UPopover>
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
        icon="i-lucide-scan-search"
        label="发现 Trace"
        :loading="searching"
        :disabled="!canSearch || !validTimeRange"
        @click="emit('search')"
      />
    </div>
  </section>
</template>

<style scoped>
.trace-discovery-command { display: grid; min-width: 0; grid-template-columns: minmax(150px, .28fr) minmax(0, 1.72fr); grid-template-areas: "identity scope" "search search"; align-items: center; gap: var(--co-space-3) var(--co-space-4); padding: var(--co-space-3) var(--co-space-4); border: 1px solid var(--co-border-subtle); border-radius: var(--co-radius-frame); background: color-mix(in srgb, var(--co-bg-surface) 78%, var(--co-bg-canvas)); box-shadow: var(--co-shadow-row); }
.trace-discovery-command__heading { display: grid; min-width: 0; grid-area: identity; gap: var(--co-space-2); }
.trace-discovery-command__heading > div { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); }
.trace-discovery-command__heading > div > span { display: grid; width: 38px; height: 38px; flex: 0 0 auto; place-items: center; border: 1px solid var(--co-status-success-border); border-radius: var(--co-radius-control); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.trace-discovery-command__heading > div > div { display: grid; min-width: 0; gap: 2px; }
.trace-discovery-command__heading small { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; }
.trace-discovery-command__heading strong { color: var(--co-text-primary); font-size: 14px; }
.trace-discovery-command__topline { display: grid; min-width: 0; grid-area: scope; grid-template-columns: minmax(430px, 1fr) minmax(180px, auto) minmax(168px, auto); align-items: center; gap: var(--co-space-2); }
.trace-discovery-command__scope { display: grid; min-width: 0; grid-template-columns: minmax(180px, .78fr) minmax(240px, 1.22fr); gap: var(--co-space-2); }
.trace-discovery-command__provider { display: flex; min-width: 0; align-items: center; justify-content: flex-start; gap: var(--co-space-2); color: var(--co-text-muted); font-size: 10px; white-space: nowrap; }
.trace-discovery-command__provider > span { width: 7px; height: 7px; border-radius: var(--co-radius-pill); background: var(--co-text-muted); }
.trace-discovery-command__provider > span.is-ready { background: var(--co-status-success-fg); box-shadow: 0 0 0 4px var(--co-status-success-bg); }
.trace-discovery-command__searchline { display: grid; min-width: 0; grid-area: search; grid-template-columns: minmax(220px, 1fr) minmax(220px, 1fr) auto auto auto; align-items: center; gap: var(--co-space-2); padding: var(--co-space-2); border-radius: var(--co-radius-panel); background: color-mix(in srgb, var(--co-bg-canvas) 68%, transparent); }
.trace-discovery-command__service,
.trace-discovery-command__operation { min-width: 0; }
.trace-discovery-command__traceql { min-width: 0; grid-column: 1 / 3; }
.trace-discovery-command__traceql :deep(input) { font-family: var(--co-font-mono); font-size: 11px; }
.trace-discovery-command__mode,
.trace-discovery-command__presets { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: 2px; padding: 3px; border-radius: var(--co-radius-control); background: var(--co-bg-canvas); }
.trace-discovery-command__mode :deep(button),
.trace-discovery-command__presets :deep(button) { min-width: 0; flex: 1; }
.trace-discovery-command__mode :deep(button),
.trace-discovery-command__presets :deep(button) { padding-inline: 8px; }
.trace-discovery-command__advanced-content { display: grid; width: min(920px, calc(100vw - 48px)); min-width: 0; grid-template-columns: repeat(3, minmax(130px, 0.6fr)) repeat(2, minmax(180px, 1fr)) minmax(110px, 0.45fr); align-items: end; gap: var(--co-space-3); padding: var(--co-space-4); border-radius: var(--co-radius-overlay); background: var(--co-bg-overlay); box-shadow: var(--co-shadow-overlay); }
.trace-discovery-command__advanced-content label { display: grid; min-width: 0; gap: var(--co-space-1); }
.trace-discovery-command__advanced-content label > span { color: var(--co-text-muted); font-size: 10px; font-weight: 800; }
.trace-discovery-command__bounds { display: flex; min-width: 0; grid-column: 1 / -1; flex-wrap: wrap; gap: var(--co-space-1) var(--co-space-4); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; }
.trace-discovery-command :deep(input),
.trace-discovery-command :deep(button),
.trace-discovery-command :deep([role="combobox"]) { border-radius: var(--co-radius-control); }

@media (max-width: 1180px) {
  .trace-discovery-command { grid-template-columns: minmax(0, 1fr); grid-template-areas: "identity" "scope" "search"; }
  .trace-discovery-command__heading { grid-template-columns: minmax(0, 1fr) auto; align-items: center; }
  .trace-discovery-command__topline { grid-template-columns: minmax(360px, 1fr) minmax(180px, auto) minmax(160px, auto); }
  .trace-discovery-command__searchline { grid-template-columns: minmax(220px, 1fr) minmax(220px, 1fr) auto auto; }
  .trace-discovery-command__advanced-content { width: min(680px, calc(100vw - 48px)); grid-template-columns: repeat(3, minmax(0, 1fr)); }
}

@media (max-width: 1024px) {
  .trace-discovery-command { padding: var(--co-space-3); }
  .trace-discovery-command__heading { grid-template-columns: minmax(0, 1fr); align-items: flex-start; }
  .trace-discovery-command__topline { grid-template-columns: minmax(0, 1fr); }
  .trace-discovery-command__scope { grid-template-columns: minmax(0, 0.8fr) minmax(0, 1.2fr); }
  .trace-discovery-command__searchline { grid-template-columns: minmax(0, 1fr) auto; }
  .trace-discovery-command__service,
  .trace-discovery-command__operation,
  .trace-discovery-command__traceql { grid-column: 1 / -1; }
}

@container traces-workspace (max-width: 900px) {
  .trace-discovery-command { grid-template-columns: minmax(0, 1fr); grid-template-areas: "identity" "scope" "search"; padding: var(--co-space-3); }
  .trace-discovery-command__heading { grid-template-columns: minmax(0, 1fr) auto; align-items: center; }
  .trace-discovery-command__topline { grid-template-columns: minmax(0, 1fr) minmax(180px, auto); }
  .trace-discovery-command__scope { grid-column: 1 / -1; grid-template-columns: minmax(150px, .8fr) minmax(0, 1.2fr); }
  .trace-discovery-command__presets { min-width: 168px; }
  .trace-discovery-command__searchline { grid-template-columns: repeat(2, minmax(0, 1fr)) auto auto; }
}

@container traces-workspace (max-width: 620px) {
  .trace-discovery-command__heading { grid-template-columns: minmax(0, 1fr); }
  .trace-discovery-command__topline { grid-template-columns: minmax(0, 1fr); }
  .trace-discovery-command__scope { grid-template-columns: minmax(0, 1fr); }
  .trace-discovery-command__searchline { grid-template-columns: minmax(0, 1fr) auto; }
  .trace-discovery-command__service,
  .trace-discovery-command__operation,
  .trace-discovery-command__traceql { grid-column: 1 / -1; }
}

@media (prefers-reduced-motion: reduce) {
  .trace-discovery-command * { scroll-behavior: auto; }
}
</style>
