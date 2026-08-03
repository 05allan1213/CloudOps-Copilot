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
const levelItems = [
  { label: "DEBUG", value: "debug" },
  { label: "INFO", value: "info" },
  { label: "WARN", value: "warn" },
  { label: "ERROR", value: "error" },
];
const presets = [
  { label: "15m", value: 15 },
  { label: "1h", value: 60 },
  { label: "6h", value: 360 },
];
const providerLabel = computed(() => {
  if (!props.catalog) return "正在确认 Elasticsearch";
  if (props.catalog.provider_state === "available") return "Elasticsearch 可用";
  if (props.catalog.provider_state === "partial") return "Elasticsearch 部分可用";
  return "Elasticsearch 不可用";
});
const levelSelectionLabel = computed(() => {
  if (!props.levels.length || props.levels.length === levelItems.length) return "全部级别";
  if (props.levels.length === 1) {
    return levelItems.find((item) => item.value === props.levels[0])?.label ?? props.levels[0]?.toUpperCase();
  }
  return `已选 ${props.levels.length} 个级别`;
});

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function numberValue(value: unknown): number {
  return typeof value === "number" ? value : Number(value) || 200;
}

function updateLevels(value: unknown): void {
  const selected = new Set(Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string" && levelItems.some((level) => level.value === item))
    : []);
  for (const level of levelItems) {
    const checked = selected.has(level.value);
    if (props.levels.includes(level.value) !== checked) emit("levelToggle", level.value, checked);
  }
}
</script>

<template>
  <section
    class="logs-command"
    aria-label="日志查询"
  >
    <header class="logs-command__header">
      <div>
        <h2>查询条件</h2>
        <p>{{ tail ? "按 Tail 语义读取当前有界日志窗口" : "按范围和条件检索历史日志" }}</p>
      </div>
      <span class="logs-command__provider">
        <span :class="{ 'is-ready': catalog?.provider_state === 'available' || catalog?.provider_state === 'partial' }" />
        {{ providerLabel }}
      </span>
    </header>
    <div class="logs-command__topline">
      <div class="logs-command__field">
        <span>查询模式</span>
        <div
          class="logs-command__stream-mode"
          role="group"
          aria-label="日志工作模式"
        >
          <UButton
            :color="!tail ? 'primary' : 'neutral'"
            :variant="!tail ? 'soft' : 'ghost'"
            icon="i-lucide-search"
            label="历史搜索"
            :aria-pressed="!tail"
            :disabled="querying"
            @click="emit('update:tail', false)"
          />
          <UButton
            :color="tail ? 'primary' : 'neutral'"
            :variant="tail ? 'soft' : 'ghost'"
            icon="i-lucide-radio-tower"
            label="Tail 快照"
            :aria-pressed="tail"
            :disabled="querying"
            @click="emit('update:tail', true)"
          />
        </div>
      </div>
      <label class="logs-command__field">
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
      <label class="logs-command__field">
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
      <div class="logs-command__field">
        <span>时间范围</span>
        <div
          class="logs-command__presets"
          role="group"
          aria-label="日志时间范围"
        >
          <UButton
            v-for="preset in presets"
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
    </div>

    <div class="logs-command__searchline">
      <label class="logs-command__field logs-command__query-field">
        <span>{{ mode === "guided" ? "日志内容" : "Query DSL" }}</span>
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
      </label>
      <UPopover
        class="logs-command__filters"
        :content="{ align: 'end', side: 'bottom', sideOffset: 8, collisionPadding: 16, sticky: 'always' }"
      >
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-sliders-horizontal"
          :label="levels.length ? `筛选 ${levels.length}` : '筛选'"
        />
        <template #content>
          <div class="logs-command__filter-content">
            <div
              class="logs-command__query-mode"
              role="group"
              aria-label="日志查询方式"
            >
              <span>查询方式</span>
              <div class="logs-command__query-segments">
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
            </div>
            <label
              class="logs-command__levels"
            >
              <span>日志级别</span>
              <USelectMenu
                :model-value="levels"
                :items="levelItems"
                value-key="value"
                label-key="label"
                multiple
                :search-input="false"
                :content="{ align: 'end', collisionPadding: 16 }"
                placeholder="全部级别"
                aria-label="日志级别"
                :disabled="querying || mode === 'expert'"
                @update:model-value="updateLevels"
              >
                <template #default>
                  <span>{{ levelSelectionLabel }}</span>
                </template>
              </USelectMenu>
            </label>
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
        :label="tail ? '执行 Tail 查询' : '搜索日志'"
        :loading="querying"
        :disabled="!canRun || !validTimeRange"
        @click="emit('run')"
      />
    </div>

    <span
      v-if="tail"
      class="logs-command__live-state"
    >
      <UIcon
        name="i-lucide-circle-dot-dashed"
        aria-hidden="true"
      />
      当前 API 返回一次有界 Tail 结果；持续 cursor/SSE 追加为 BACKEND_GAP。
    </span>
  </section>
</template>

<style scoped>
.logs-command {
  display: grid;
  min-width: 0;
  gap: var(--co-space-4);
  padding: var(--co-space-4);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-frame);
  background: var(--co-bg-surface);
  box-shadow: var(--co-shadow-row);
}
.logs-command__header { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-4); padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-subtle); }
.logs-command__header > div { min-width: 0; }
.logs-command__header h2 { margin: 0; color: var(--co-text-primary); font-size: 17px; }
.logs-command__header p { margin: 3px 0 0; color: var(--co-text-muted); font-size: 12px; }
.logs-command__topline { display: grid; min-width: 0; grid-template-columns: minmax(190px, .8fr) minmax(170px, .72fr) minmax(240px, 1.15fr) minmax(170px, .68fr); align-items: end; gap: var(--co-space-3); }
.logs-command__field { display: grid; min-width: 0; grid-template-rows: 18px 40px; align-content: end; gap: 6px; }
.logs-command__field > span,
.logs-command__filter-content label > span,
.logs-command__query-mode > span,
.logs-command__levels > span { min-width: 0; color: var(--co-text-secondary); font-size: 12px; font-weight: 650; line-height: 18px; }
.logs-command__stream-mode,
.logs-command__presets { display: grid; width: 100%; min-width: 0; height: 40px; align-items: stretch; gap: 0; padding: 2px; overflow: hidden; border-radius: var(--co-radius-pill); background: var(--co-bg-canvas); }
.logs-command__stream-mode { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.logs-command__presets { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.logs-command__stream-mode :deep(button),
.logs-command__presets :deep(button) { width: 100%; min-width: 0; height: 100%; min-height: 0; justify-content: center; padding-inline: 8px; }
.logs-command__provider { display: flex; min-width: 0; align-items: center; justify-content: flex-start; gap: var(--co-space-2); overflow: hidden; color: var(--co-text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.logs-command__provider > span { width: 7px; height: 7px; flex: 0 0 auto; border-radius: var(--co-radius-pill); background: var(--co-text-muted); }
.logs-command__provider > span.is-ready { background: var(--co-status-success-fg); box-shadow: 0 0 0 4px var(--co-status-success-bg); }
.logs-command__searchline { display: grid; min-width: 0; grid-template-columns: minmax(320px, 1fr) auto auto auto; align-items: end; gap: var(--co-space-2); }
.logs-command__query-field { min-width: 0; }
.logs-command__search { width: 100%; min-width: 0; }
.logs-command__search--code :deep(input) { font-family: var(--co-font-mono); font-size: 11px; }
.logs-command__filters,
.logs-command__searchline > :deep(button) { align-self: end; }
.logs-command__filters { display: flex; min-width: 0; }
.logs-command__filters :deep(button),
.logs-command__searchline > :deep(button) { height: 40px; }
.logs-command__live-state { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); padding-top: var(--co-space-1); border-top: 1px solid var(--co-border-subtle); color: var(--co-status-success-fg); font-size: 12px; }
.logs-command__filter-content { display: grid; box-sizing: border-box; width: min(680px, calc(100vw - 32px)); min-width: 0; max-height: min(520px, calc(100dvh - 32px)); grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: end; gap: var(--co-space-3); padding: var(--co-space-4); overflow-x: hidden; overflow-y: auto; overscroll-behavior: contain; border-radius: var(--co-radius-panel); background: var(--co-bg-overlay); box-shadow: var(--co-shadow-overlay); scrollbar-gutter: stable; }
.logs-command__filter-content label,
.logs-command__query-mode { display: grid; min-width: 0; grid-template-rows: 18px 40px; align-content: end; gap: 6px; }
.logs-command__query-segments { display: grid; box-sizing: border-box; min-width: 0; height: 40px; grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: stretch; gap: 0; padding: 2px; overflow: hidden; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); background: var(--co-bg-canvas); }
.logs-command__query-segments :deep(button) { width: 100%; min-width: 0; height: 100%; min-height: 0; justify-content: center; padding-inline: 8px; }
.logs-command__levels :deep([role="combobox"]) { height: 40px; justify-content: space-between; }
.logs-command__levels :deep([role="combobox"] > span) { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.logs-command__filter-content :deep(input),
.logs-command__filter-content :deep(button),
.logs-command__filter-content :deep([role="combobox"]) { width: 100%; min-width: 0; max-width: 100%; }
.logs-command__bounds { display: flex; min-width: 0; grid-column: 1 / -1; flex-wrap: wrap; gap: var(--co-space-2) var(--co-space-4); padding-top: var(--co-space-2); border-top: 1px solid var(--co-border-subtle); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 11px; }
.logs-command :deep(input),
.logs-command :deep(button),
.logs-command :deep([role="combobox"]) { border-radius: var(--co-radius-control); }
.logs-command__field > :deep([role="combobox"]),
.logs-command__field > :deep(input) { height: 40px; }

@media (max-width: 1180px) {
  .logs-command__topline { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 1024px) {
  .logs-command { padding: var(--co-space-3); }
  .logs-command__searchline { grid-template-columns: minmax(0, 1fr) auto auto; }
}

@media (max-width: 700px) {
  .logs-command__filter-content { width: calc(100vw - 24px); max-height: calc(100dvh - 24px); grid-template-columns: minmax(0, 1fr); padding: var(--co-space-3); }
  .logs-command__bounds { grid-column: 1; }
}

@container logs-workspace (max-width: 620px) {
  .logs-command__header { align-items: flex-start; }
  .logs-command__topline { grid-template-columns: minmax(0, 1fr); }
  .logs-command__searchline { grid-template-columns: minmax(0, 1fr) auto; }
  .logs-command__query-field { grid-column: 1 / -1; }
  .logs-command__filter-content { grid-template-columns: minmax(0, 1fr); }
}

@media (prefers-reduced-motion: reduce) {
  .logs-command * { scroll-behavior: auto; }
}
</style>
