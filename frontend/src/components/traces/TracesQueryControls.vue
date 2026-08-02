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
const providerLabel = computed(() => {
  if (!props.catalog) return "正在确认 Tempo";
  if (props.catalog.provider_state === "available") return "Tempo 可用";
  if (props.catalog.provider_state === "partial") return "Tempo 部分可用";
  return "Tempo 不可用";
});

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
    <header class="trace-discovery-command__header">
      <div>
        <h2>查询条件</h2>
        <p>{{ mode === "guided" ? "按服务与操作发现调用链" : "使用 TraceQL 精确检索调用链" }}</p>
      </div>
      <span class="trace-discovery-command__provider">
        <span :class="{ 'is-ready': catalog?.provider_state === 'available' || catalog?.provider_state === 'partial' }" />
        {{ providerLabel }}
      </span>
    </header>
    <div class="trace-discovery-command__topline">
      <div class="trace-discovery-command__field">
        <span>查询模式</span>
        <div
          class="trace-discovery-command__mode"
          role="group"
          aria-label="Trace 查询模式"
        >
          <UButton
            :color="mode === 'guided' ? 'primary' : 'neutral'"
            :variant="mode === 'guided' ? 'soft' : 'ghost'"
            icon="i-lucide-waypoints"
            label="服务发现"
            :aria-pressed="mode === 'guided'"
            :disabled="searching"
            @click="emit('update:mode', 'guided')"
          />
          <UButton
            :color="mode === 'expert' ? 'primary' : 'neutral'"
            :variant="mode === 'expert' ? 'soft' : 'ghost'"
            icon="i-lucide-braces"
            label="TraceQL"
            :aria-pressed="mode === 'expert'"
            :disabled="searching"
            @click="emit('update:mode', 'expert')"
          />
        </div>
      </div>
      <label class="trace-discovery-command__field">
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
      <label class="trace-discovery-command__field">
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
      <div class="trace-discovery-command__field">
        <span>时间范围</span>
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
    </div>

    <div class="trace-discovery-command__searchline">
      <template v-if="mode === 'guided'">
        <label class="trace-discovery-command__field trace-discovery-command__service">
          <span>Service</span>
          <UInput
            :model-value="service"
            icon="i-lucide-box"
            size="xl"
            placeholder="例如 cloudops-api"
            aria-label="Service"
            :disabled="searching"
            @update:model-value="emit('update:service', stringValue($event))"
          />
        </label>
        <label class="trace-discovery-command__field trace-discovery-command__operation">
          <span>Operation</span>
          <UInput
            :model-value="operation"
            icon="i-lucide-route"
            size="xl"
            placeholder="例如 GET /readyz"
            aria-label="Operation"
            :disabled="searching"
            @update:model-value="emit('update:operation', stringValue($event))"
          />
        </label>
      </template>
      <label
        v-else
        class="trace-discovery-command__field trace-discovery-command__traceql"
      >
        <span>TraceQL</span>
        <UInput
          :model-value="expertQuery"
          icon="i-lucide-braces"
          size="xl"
          placeholder="TraceQL span selector"
          aria-label="TraceQL span selector"
          :disabled="searching"
          @update:model-value="emit('update:expertQuery', stringValue($event))"
        />
      </label>
      <UPopover
        class="trace-discovery-command__filters"
        :content="{ align: 'end', side: 'bottom', sideOffset: 8, collisionPadding: 16, sticky: 'always' }"
      >
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-sliders-horizontal"
          :label="status || minDurationMS !== undefined || maxDurationMS !== undefined ? '筛选 已启用' : '筛选'"
        />
        <template #content>
          <div class="trace-discovery-command__filter-content">
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
.trace-discovery-command { display: grid; min-width: 0; gap: var(--co-space-4); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-bg-surface); box-shadow: var(--co-shadow-row); }
.trace-discovery-command__header { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-4); padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-subtle); }
.trace-discovery-command__header > div { min-width: 0; }
.trace-discovery-command__header h2 { margin: 0; color: var(--co-text-primary); font-size: 17px; }
.trace-discovery-command__header p { margin: 3px 0 0; color: var(--co-text-muted); font-size: 12px; }
.trace-discovery-command__topline { display: grid; min-width: 0; grid-template-columns: minmax(190px, .8fr) minmax(170px, .72fr) minmax(240px, 1.15fr) minmax(170px, .68fr); align-items: end; gap: var(--co-space-3); }
.trace-discovery-command__field { display: grid; min-width: 0; grid-template-rows: 18px 40px; align-content: end; gap: 6px; }
.trace-discovery-command__field > span,
.trace-discovery-command__filter-content label > span { min-width: 0; color: var(--co-text-secondary); font-size: 12px; font-weight: 650; line-height: 18px; }
.trace-discovery-command__provider { display: flex; min-width: 0; align-items: center; justify-content: flex-start; gap: var(--co-space-2); overflow: hidden; color: var(--co-text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.trace-discovery-command__provider > span { width: 7px; height: 7px; flex: 0 0 auto; border-radius: var(--co-radius-pill); background: var(--co-text-muted); }
.trace-discovery-command__provider > span.is-ready { background: var(--co-status-success-fg); box-shadow: 0 0 0 4px var(--co-status-success-bg); }
.trace-discovery-command__searchline { display: grid; min-width: 0; grid-template-columns: minmax(220px, .82fr) minmax(280px, 1.18fr) auto auto auto; align-items: end; gap: var(--co-space-2); }
.trace-discovery-command__service,
.trace-discovery-command__operation { min-width: 0; }
.trace-discovery-command__traceql { min-width: 0; grid-column: 1 / 3; }
.trace-discovery-command__traceql :deep(input) { font-family: var(--co-font-mono); font-size: 12px; }
.trace-discovery-command__mode,
.trace-discovery-command__presets { display: grid; width: 100%; min-width: 0; height: 40px; align-items: stretch; gap: 0; padding: 2px; overflow: hidden; border-radius: var(--co-radius-pill); background: var(--co-bg-canvas); }
.trace-discovery-command__mode { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.trace-discovery-command__presets { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.trace-discovery-command__mode :deep(button),
.trace-discovery-command__presets :deep(button) { width: 100%; min-width: 0; height: 100%; min-height: 0; justify-content: center; padding-inline: 8px; }
.trace-discovery-command__filters,
.trace-discovery-command__searchline > :deep(button) { align-self: end; }
.trace-discovery-command__filters { display: flex; min-width: 0; }
.trace-discovery-command__filters :deep(button),
.trace-discovery-command__searchline > :deep(button) { height: 40px; }
.trace-discovery-command__filter-content { display: grid; box-sizing: border-box; width: min(680px, calc(100vw - 32px)); min-width: 0; max-height: min(520px, calc(100dvh - 32px)); grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: end; gap: var(--co-space-3); padding: var(--co-space-4); overflow-x: hidden; overflow-y: auto; overscroll-behavior: contain; border-radius: var(--co-radius-panel); background: var(--co-bg-overlay); box-shadow: var(--co-shadow-overlay); scrollbar-gutter: stable; }
.trace-discovery-command__filter-content label { display: grid; min-width: 0; gap: var(--co-space-2); }
.trace-discovery-command__filter-content :deep(input),
.trace-discovery-command__filter-content :deep(button),
.trace-discovery-command__filter-content :deep([role="combobox"]) { width: 100%; min-width: 0; max-width: 100%; }
.trace-discovery-command__bounds { display: flex; min-width: 0; grid-column: 1 / -1; flex-wrap: wrap; gap: var(--co-space-2) var(--co-space-4); padding-top: var(--co-space-2); border-top: 1px solid var(--co-border-subtle); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 11px; }
.trace-discovery-command :deep(input),
.trace-discovery-command :deep(button),
.trace-discovery-command :deep([role="combobox"]) { border-radius: var(--co-radius-control); }
.trace-discovery-command__field > :deep([role="combobox"]),
.trace-discovery-command__field > :deep(input) { height: 40px; }

@media (max-width: 1180px) {
  .trace-discovery-command__topline { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 1024px) {
  .trace-discovery-command { padding: var(--co-space-3); }
  .trace-discovery-command__searchline { grid-template-columns: minmax(0, 1fr) auto; }
  .trace-discovery-command__service,
  .trace-discovery-command__operation,
  .trace-discovery-command__traceql { grid-column: 1 / -1; }
}

@media (max-width: 700px) {
  .trace-discovery-command__filter-content { width: calc(100vw - 24px); max-height: calc(100dvh - 24px); grid-template-columns: minmax(0, 1fr); padding: var(--co-space-3); }
  .trace-discovery-command__bounds { grid-column: 1; }
}

@container traces-workspace (max-width: 620px) {
  .trace-discovery-command__header { align-items: flex-start; }
  .trace-discovery-command__topline { grid-template-columns: minmax(0, 1fr); }
  .trace-discovery-command__searchline { grid-template-columns: minmax(0, 1fr) auto; }
  .trace-discovery-command__service,
  .trace-discovery-command__operation,
  .trace-discovery-command__traceql { grid-column: 1 / -1; }
  .trace-discovery-command__filter-content { grid-template-columns: minmax(0, 1fr); }
}

@media (prefers-reduced-motion: reduce) {
  .trace-discovery-command * { scroll-behavior: auto; }
}
</style>
