<script setup lang="ts">
import { useVirtualizer } from "@tanstack/vue-virtual";
import type { ComponentPublicInstance, CSSProperties } from "vue";
import { computed, ref } from "vue";

import type { TraceDetail, TraceSpan } from "../../api/telemetry";
import { traceServiceColor, traceSpanRawValue, waterfallPosition } from "../../models/telemetry";

const props = defineProps<{
  detail: TraceDetail;
  selectedIDs: Set<string>;
  inspectedID: string;
}>();

const emit = defineEmits<{
  toggle: [spanID: string];
  inspect: [span: TraceSpan];
}>();

const viewport = ref<HTMLDivElement | null>(null);
const copiedSpanID = ref("");

const virtualizer = useVirtualizer<HTMLDivElement, HTMLDivElement>(computed(() => ({
  count: props.detail.spans.length,
  getScrollElement: () => viewport.value,
  estimateSize: () => 48,
  overscan: 10,
  getItemKey: (index: number) => props.detail.spans[index]?.span_id ?? index,
})));
const virtualRows = computed(() => virtualizer.value.getVirtualItems());
const totalSize = computed(() => virtualizer.value.getTotalSize());
const viewportHeight = computed(() => Math.min(590, Math.max(144, props.detail.spans.length * 48)));

function formatDuration(value: number): string {
  if (!Number.isFinite(value)) return "无";
  if (value < 1) return `${value.toFixed(3)} ms`;
  if (value < 1000) return `${value.toFixed(value < 10 ? 2 : 1)} ms`;
  return `${(value / 1000).toFixed(2)} s`;
}

function rowStyle(span: TraceSpan, start: number): CSSProperties {
  return {
    transform: `translateY(${start}px)`,
    "--span-depth": span.depth,
    "--span-service-color": traceServiceColor(span.service),
  } as CSSProperties;
}

function barStyle(span: TraceSpan): CSSProperties {
  const position = waterfallPosition(
    props.detail.start_time,
    props.detail.duration_ms,
    span.start_time,
    span.duration_ms,
  );
  return { left: `${position.left}%`, width: `${position.width}%` };
}

function measureRow(element: Element | ComponentPublicInstance | null) {
  const resolved = element instanceof Element ? element : element?.$el;
  if (resolved instanceof HTMLDivElement) virtualizer.value.measureElement(resolved);
}

function fallbackCopy(value: string) {
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.append(textarea);
  textarea.select();
  document.execCommand("copy");
  textarea.remove();
}

async function copySpan(span: TraceSpan) {
  const value = traceSpanRawValue(span);
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    fallbackCopy(value);
  }
  copiedSpanID.value = span.span_id;
  window.setTimeout(() => { if (copiedSpanID.value === span.span_id) copiedSpanID.value = ""; }, 1200);
}
</script>

<template>
  <section
    class="trace-waterfall"
    aria-labelledby="trace-waterfall-heading"
    data-testid="trace-waterfall"
  >
    <header>
      <h3 id="trace-waterfall-heading">
        Span 瀑布
      </h3>
      <span>0 → {{ formatDuration(detail.duration_ms) }}</span>
    </header>
    <div
      ref="viewport"
      class="trace-waterfall__viewport"
      role="list"
      :aria-label="`${detail.spans.length} 个 Span`"
      :aria-setsize="detail.spans.length"
      :data-rendered-count="virtualRows.length"
      :style="{ height: `${viewportHeight}px` }"
    >
      <div
        class="trace-waterfall__spacer"
        :style="{ height: `${totalSize}px` }"
      >
        <article
          v-for="virtualRow in virtualRows"
          :key="String(virtualRow.key)"
          :ref="measureRow"
          class="trace-span-row"
          :class="{ 'is-active': inspectedID === detail.spans[virtualRow.index]?.span_id }"
          role="listitem"
          :aria-posinset="virtualRow.index + 1"
          :aria-setsize="detail.spans.length"
          :data-index="virtualRow.index"
          :style="rowStyle(detail.spans[virtualRow.index], virtualRow.start)"
        >
          <UCheckbox
            :model-value="selectedIDs.has(detail.spans[virtualRow.index].span_id)"
            :aria-label="`选择 span ${detail.spans[virtualRow.index].name} 作为 Evidence`"
            @update:model-value="emit('toggle', detail.spans[virtualRow.index].span_id)"
          />
          <div class="trace-span-row__identity">
            <span aria-hidden="true" />
            <strong>{{ detail.spans[virtualRow.index].name }}</strong>
            <small>{{ detail.spans[virtualRow.index].service }} · {{ detail.spans[virtualRow.index].parent_span_id || "root" }}</small>
          </div>
          <UButton
            class="trace-span-row__inspect"
            color="neutral"
            variant="ghost"
            :aria-label="`检查 span ${detail.spans[virtualRow.index].name}`"
            @click="emit('inspect', detail.spans[virtualRow.index])"
          >
            <span
              class="trace-span-row__track"
              aria-hidden="true"
            >
              <i
                class="trace-span-row__bar"
                :class="{
                  'is-error': detail.spans[virtualRow.index].status === 'error',
                  'is-critical': detail.spans[virtualRow.index].critical_path,
                }"
                :style="barStyle(detail.spans[virtualRow.index])"
              />
            </span>
            <span class="trace-span-row__duration">{{ formatDuration(detail.spans[virtualRow.index].duration_ms) }}</span>
          </UButton>
          <UTooltip text="复制完整 Span">
            <UButton
              color="neutral"
              variant="ghost"
              :icon="copiedSpanID === detail.spans[virtualRow.index].span_id ? 'i-lucide-check' : 'i-lucide-copy'"
              square
              :aria-label="`复制完整 span ${detail.spans[virtualRow.index].name}`"
              @click="copySpan(detail.spans[virtualRow.index])"
            />
          </UTooltip>
        </article>
      </div>
    </div>
  </section>
</template>

<style scoped>
.trace-waterfall { min-width: 0; overflow: hidden; border: 1px solid color-mix(in srgb, var(--co-code-text) 14%, transparent); border-radius: var(--co-radius-frame); background: var(--co-code-bg); color: var(--co-code-text); box-shadow: var(--co-shadow-row); }
.trace-waterfall > header { display: flex; min-height: 48px; align-items: center; justify-content: space-between; gap: var(--co-space-3); padding: 0 var(--co-space-4); border-bottom: 1px solid color-mix(in srgb, var(--co-code-text) 10%, transparent); }
.trace-waterfall h3 { margin: 0; font-size: 13px; }
.trace-waterfall header span { color: color-mix(in srgb, var(--co-code-text) 56%, transparent); font-size: 10px; }
.trace-waterfall :deep(button) { color: var(--co-code-text); }
.trace-waterfall__viewport { max-height: min(58vh, 590px); min-height: 144px; overflow: auto; overscroll-behavior: contain; contain: layout paint; }
.trace-waterfall__spacer { position: relative; min-width: 640px; }
.trace-span-row {
  position: absolute;
  inset: 0 0 auto;
  display: grid;
  width: 100%;
  min-width: 640px;
  min-height: 48px;
  grid-template-columns: 28px minmax(190px, 0.38fr) minmax(320px, 1fr) 34px;
  align-items: center;
  gap: var(--co-space-2);
  padding: 0 var(--co-space-2);
  border-bottom: 1px solid color-mix(in srgb, var(--co-code-text) 9%, transparent);
  background: transparent;
}
.trace-span-row:hover,
.trace-span-row.is-active { background: color-mix(in srgb, var(--co-code-text) 7%, transparent); }
.trace-span-row.is-active { box-shadow: inset var(--co-severity-marker-width) 0 0 var(--span-service-color); }
.trace-span-row__identity {
  display: grid;
  min-width: 0;
  grid-template-columns: 8px minmax(0, 1fr);
  column-gap: var(--co-space-2);
  padding-left: calc(var(--span-depth, 0) * 12px);
}
.trace-span-row__identity > span { width: 7px; height: 7px; align-self: center; border-radius: 50%; background: var(--span-service-color); }
.trace-span-row__identity strong,
.trace-span-row__identity small { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.trace-span-row__identity strong { color: var(--co-code-text); font-size: 11px; }
.trace-span-row__identity small { grid-column: 2; color: color-mix(in srgb, var(--co-code-text) 52%, transparent); font-family: var(--co-font-mono); font-size: 9px; }
.trace-span-row__inspect { display: grid; min-width: 0; grid-template-columns: minmax(240px, 1fr) 64px; justify-content: stretch; gap: var(--co-space-2); padding: 0; }
.trace-span-row__track { position: relative; height: 20px; border-inline: 1px solid color-mix(in srgb, var(--co-code-text) 12%, transparent); border-radius: var(--co-radius-control); background: color-mix(in srgb, var(--co-code-text) 6%, transparent); }
.trace-span-row__bar { position: absolute; top: 4px; height: 12px; min-width: 2px; border-radius: 2px; background: var(--span-service-color); }
.trace-span-row__bar.is-error { background: var(--co-status-critical-fg); }
.trace-span-row__bar.is-critical { box-shadow: inset 0 -3px 0 var(--co-status-warning-fg); }
.trace-span-row__duration { color: var(--co-code-text); font-size: 10px; text-align: right; font-variant-numeric: tabular-nums; }
</style>
