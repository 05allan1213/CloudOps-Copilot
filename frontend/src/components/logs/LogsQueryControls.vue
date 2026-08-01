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
const providerLabel = computed(() => {
  if (!props.catalog) return "正在确认 Elasticsearch";
  if (props.catalog.provider_state === "available") return "Elasticsearch 可用";
  if (props.catalog.provider_state === "partial") return "Elasticsearch 部分可用";
  return "Elasticsearch 不可用";
});

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function numberValue(value: unknown): number {
  return typeof value === "number" ? value : Number(value) || 200;
}
</script>

<template>
  <section
    class="logs-command"
    aria-label="日志查询"
  >
    <header class="logs-command__heading">
      <div>
        <span aria-hidden="true">
          <UIcon :name="tail ? 'i-lucide-radio-tower' : 'i-lucide-text-search'" />
        </span>
        <div>
          <small>LOG QUERY</small>
          <strong>{{ tail ? "实时追踪" : "历史检索" }}</strong>
        </div>
      </div>
      <span class="logs-command__provider">
        <span :class="{ 'is-ready': catalog?.provider_state === 'available' || catalog?.provider_state === 'partial' }" />
        {{ providerLabel }}
      </span>
    </header>
    <div class="logs-command__topline">
      <div
        class="logs-command__stream-mode"
        role="group"
        aria-label="日志工作模式"
      >
        <UButton
          :color="!tail ? 'primary' : 'neutral'"
          :variant="!tail ? 'solid' : 'ghost'"
          icon="i-lucide-search"
          label="搜索日志"
          :aria-pressed="!tail"
          :disabled="querying"
          @click="emit('update:tail', false)"
        />
        <UButton
          :color="tail ? 'primary' : 'neutral'"
          :variant="tail ? 'solid' : 'ghost'"
          icon="i-lucide-radio-tower"
          label="实时日志"
          :aria-pressed="tail"
          :disabled="querying"
          @click="emit('update:tail', true)"
        />
      </div>
      <div class="logs-command__scope">
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
      </div>
      <div
        v-if="!tail"
        class="logs-command__presets"
        role="group"
        aria-label="日志时间范围"
      >
        <UButton
          v-for="preset in [{ label: '15m', value: 15 }, { label: '1h', value: 60 }, { label: '6h', value: 360 }]"
          :key="preset.value"
          color="neutral"
          variant="ghost"
          size="sm"
          :label="preset.label"
          :disabled="querying"
          @click="emit('preset', preset.value)"
        />
      </div>
    </div>

    <div class="logs-command__searchline">
      <UInput
        v-if="mode === 'guided'"
        :model-value="text"
        class="logs-command__search"
        icon="i-lucide-search"
        size="xl"
        :placeholder="tail ? '筛选即将进入的日志，例如 error 或 timeout' : '搜索日志正文，例如 timeout、connection refused'"
        aria-label="日志文本过滤"
        :disabled="querying"
        @update:model-value="emit('update:text', stringValue($event))"
      />
      <UInput
        v-else
        :model-value="expertQuery"
        class="logs-command__search logs-command__search--code"
        icon="i-lucide-braces"
        size="xl"
        placeholder="Elasticsearch query clause"
        aria-label="Elasticsearch query clause"
        :disabled="querying"
        @update:model-value="emit('update:expertQuery', stringValue($event))"
      />
      <div
        class="logs-command__levels"
        role="group"
        aria-label="日志级别"
      >
        <UButton
          v-for="level in levelItems"
          :key="level"
          color="neutral"
          :variant="levels.includes(level) ? 'soft' : 'ghost'"
          size="sm"
          :label="level.toUpperCase()"
          :aria-pressed="levels.includes(level)"
          :disabled="querying || mode === 'expert'"
          @click="emit('levelToggle', level, !levels.includes(level))"
        />
      </div>
      <UPopover class="logs-command__advanced">
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-sliders-horizontal"
          label="高级"
        />
        <template #content>
          <div class="logs-command__advanced-content">
            <div
              class="logs-command__query-mode"
              role="group"
              aria-label="日志查询方式"
            >
              <span>查询方式</span>
              <UButton
                :color="mode === 'guided' ? 'primary' : 'neutral'"
                :variant="mode === 'guided' ? 'soft' : 'ghost'"
                label="字段搜索"
                :aria-pressed="mode === 'guided'"
                :disabled="querying"
                @click="emit('update:mode', 'guided')"
              />
              <UButton
                :color="mode === 'expert' ? 'primary' : 'neutral'"
                :variant="mode === 'expert' ? 'soft' : 'ghost'"
                label="Query DSL"
                :aria-pressed="mode === 'expert'"
                :disabled="querying"
                @click="emit('update:mode', 'expert')"
              />
            </div>
            <label>
              <span>Trace ID</span>
              <UInput
                :model-value="traceID"
                icon="i-lucide-git-branch"
                placeholder="可选"
                aria-label="Trace ID"
                :disabled="querying || mode === 'expert'"
                @update:model-value="emit('update:traceID', stringValue($event))"
              />
            </label>
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
            <label>
              <span>结果上限</span>
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
            <div class="logs-command__bounds">
              <span>Lookback ≤ {{ Math.round((catalog?.bounds.max_lookback_seconds ?? 0) / 3600) }}h</span>
              <span>Rows ≤ {{ catalog?.bounds.max_results ?? 0 }}</span>
              <span>Response ≤ {{ Math.round((catalog?.bounds.max_response_bytes ?? 0) / 1024) }} KiB</span>
              <span>Timeout {{ catalog?.bounds.timeout_ms ?? 0 }}ms</span>
            </div>
          </div>
        </template>
      </UPopover>
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
        :icon="tail ? 'i-lucide-radio-tower' : 'i-lucide-play'"
        :label="tail ? '开始追踪' : '搜索'"
        :loading="querying"
        :disabled="!canRun || !validTimeRange"
        @click="emit('run')"
      />
    </div>

    <span
      v-if="tail"
      class="logs-command__live-state"
    >
      <UIcon name="i-lucide-circle-dot-dashed" aria-hidden="true" />
      实时结果会持续追加；停止后保留当前缓冲区与查询身份。
    </span>
  </section>
</template>

<style scoped>
.logs-command {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(150px, .28fr) minmax(0, 1.72fr);
  grid-template-areas:
    "identity scope"
    "search search";
  align-items: center;
  gap: var(--co-space-3) var(--co-space-4);
  padding: var(--co-space-3) var(--co-space-4);
  border: 1px solid var(--co-border-subtle);
  border-radius: var(--co-radius-frame);
  background: color-mix(in srgb, var(--co-bg-surface) 78%, var(--co-bg-canvas));
  box-shadow: var(--co-shadow-row);
}
.logs-command__heading { display: grid; min-width: 0; grid-area: identity; gap: var(--co-space-2); }
.logs-command__heading > div { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); }
.logs-command__heading > div > span { display: grid; width: 38px; height: 38px; flex: 0 0 auto; place-items: center; border: 1px solid var(--co-border-subtle); border-radius: var(--co-radius-control); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.logs-command__heading > div > div { display: grid; min-width: 0; gap: 2px; }
.logs-command__heading small { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; }
.logs-command__heading strong { color: var(--co-text-primary); font-size: 14px; }
.logs-command__topline { display: grid; min-width: 0; grid-area: scope; grid-template-columns: minmax(180px, auto) minmax(430px, 1fr) minmax(168px, auto); align-items: center; gap: var(--co-space-2); }
.logs-command__stream-mode { display: flex; align-items: center; gap: var(--co-space-1); padding: 3px; border-radius: var(--co-radius-pill); background: var(--co-bg-canvas); }
.logs-command__stream-mode :deep(button) { min-width: 0; min-height: 34px; flex: 1; }
.logs-command__scope { display: grid; min-width: 0; grid-template-columns: minmax(180px, .78fr) minmax(240px, 1.22fr); gap: var(--co-space-2); }
.logs-command__provider { display: flex; min-width: 0; align-items: center; justify-content: flex-start; gap: var(--co-space-2); overflow: hidden; color: var(--co-text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.logs-command__provider > span { width: 7px; height: 7px; flex: 0 0 auto; border-radius: var(--co-radius-pill); background: var(--co-text-muted); }
.logs-command__provider > span.is-ready { background: var(--co-status-success-fg); box-shadow: 0 0 0 4px var(--co-status-success-bg); }
.logs-command__searchline { display: grid; min-width: 0; grid-area: search; grid-template-columns: minmax(260px, 1fr) minmax(286px, auto) auto auto auto; align-items: center; gap: var(--co-space-2); padding: var(--co-space-2); border-radius: var(--co-radius-panel); background: color-mix(in srgb, var(--co-bg-canvas) 68%, transparent); }
.logs-command__search { min-width: 0; }
.logs-command__search--code :deep(input) { font-family: var(--co-font-mono); font-size: 11px; }
.logs-command__levels,
.logs-command__presets { min-width: 0; align-items: center; gap: 2px; padding: 3px; border-radius: var(--co-radius-control); background: var(--co-bg-canvas); }
.logs-command__levels { display: flex; flex-wrap: nowrap; }
.logs-command__presets { display: flex; flex-wrap: wrap; }
.logs-command__levels :deep(button),
.logs-command__presets :deep(button) { min-width: 0; }
.logs-command__levels :deep(button) { flex: 1; justify-content: center; font-size: 11px; }
.logs-command__levels :deep(button),
.logs-command__presets :deep(button) { padding-inline: 8px; }
.logs-command__presets :deep(button) { flex: 1; }
.logs-command__live-state { display: flex; min-width: 0; align-items: center; gap: var(--co-space-1); padding-inline: var(--co-space-2); color: var(--co-status-success-fg); font-size: 10px; }
.logs-command__advanced-content { display: grid; width: min(920px, calc(100vw - 48px)); min-width: 0; grid-template-columns: minmax(220px, 1fr) minmax(180px, 0.9fr) repeat(2, minmax(180px, 0.9fr)) minmax(110px, 0.45fr); align-items: end; gap: var(--co-space-3); padding: var(--co-space-4); border-radius: var(--co-radius-panel); background: var(--co-bg-overlay); box-shadow: var(--co-shadow-overlay); }
.logs-command__advanced-content label { display: grid; min-width: 0; gap: var(--co-space-1); }
.logs-command__advanced-content label > span,
.logs-command__query-mode > span { color: var(--co-text-muted); font-size: 10px; font-weight: 800; }
.logs-command__query-mode { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-1); }
.logs-command__query-mode > span { width: 100%; }
.logs-command__bounds { display: flex; min-width: 0; grid-column: 1 / -1; flex-wrap: wrap; gap: var(--co-space-1) var(--co-space-4); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; }
.logs-command :deep(input),
.logs-command :deep(button),
.logs-command :deep([role="combobox"]) { border-radius: var(--co-radius-control); }

@media (max-width: 1180px) {
  .logs-command { grid-template-columns: minmax(0, 1fr); grid-template-areas: "identity" "scope" "search"; }
  .logs-command__heading { grid-template-columns: minmax(0, 1fr) auto; align-items: center; }
  .logs-command__topline { grid-template-columns: minmax(180px, auto) minmax(360px, 1fr) minmax(160px, auto); }
  .logs-command__searchline { grid-template-columns: minmax(0, 1fr) auto auto; }
  .logs-command__levels { grid-column: 1 / -1; }
  .logs-command__advanced-content { width: min(620px, calc(100vw - 48px)); grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 1024px) {
  .logs-command { padding: var(--co-space-3); }
  .logs-command__heading { grid-template-columns: minmax(0, 1fr); align-items: flex-start; }
  .logs-command__topline { grid-template-columns: minmax(0, 1fr); }
  .logs-command__scope { grid-template-columns: minmax(0, 0.8fr) minmax(0, 1.2fr); }
  .logs-command__searchline { grid-template-columns: minmax(0, 1fr) auto; }
  .logs-command__searchline > :first-child { grid-column: 1 / -1; }
  .logs-command__levels { grid-column: 1 / -1; }
}

@container logs-workspace (max-width: 900px) {
  .logs-command { grid-template-columns: minmax(0, 1fr); grid-template-areas: "identity" "scope" "search"; padding: var(--co-space-3); }
  .logs-command__heading { grid-template-columns: minmax(0, 1fr) auto; align-items: center; }
  .logs-command__topline { grid-template-columns: minmax(170px, auto) minmax(0, 1fr); }
  .logs-command__scope { grid-column: 1 / -1; grid-template-columns: minmax(150px, .8fr) minmax(0, 1.2fr); }
  .logs-command__presets { min-width: 168px; }
  .logs-command__searchline { grid-template-columns: minmax(0, 1fr) auto auto; }
  .logs-command__searchline > :first-child,
  .logs-command__levels { grid-column: 1 / -1; }
}

@container logs-workspace (max-width: 620px) {
  .logs-command__heading { grid-template-columns: minmax(0, 1fr); }
  .logs-command__topline { grid-template-columns: minmax(0, 1fr); }
  .logs-command__scope { grid-template-columns: minmax(0, 1fr); }
  .logs-command__searchline { grid-template-columns: minmax(0, 1fr) auto; }
  .logs-command__advanced { justify-self: start; }
}

@media (prefers-reduced-motion: reduce) {
  .logs-command * { scroll-behavior: auto; }
}
</style>
