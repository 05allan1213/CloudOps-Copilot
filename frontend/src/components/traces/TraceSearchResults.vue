<script setup lang="ts">
import { computed, nextTick, ref } from "vue";

import type { TraceSummary } from "../../api/telemetry";

const props = defineProps<{
  traces: TraceSummary[];
  activeTraceID: string;
}>();

const emit = defineEmits<{
  open: [trace: TraceSummary, scrollTop: number];
}>();

const viewport = ref<HTMLElement | null>(null);
const copiedTraceID = ref("");
const maxDuration = computed(() => Math.max(1, ...props.traces.map((trace) => trace.duration_ms)));

const dateFormatter = new Intl.DateTimeFormat("zh-CN", {
  dateStyle: "medium",
  timeStyle: "medium",
});

function formatTime(value: string): string {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : dateFormatter.format(parsed);
}

function formatDuration(value: number): string {
  if (!Number.isFinite(value)) return "无";
  if (value < 1) return `${value.toFixed(3)} ms`;
  if (value < 1000) return `${value.toFixed(value < 10 ? 2 : 1)} ms`;
  return `${(value / 1000).toFixed(2)} s`;
}

function durationWidth(value: number): string {
  return `${Math.max(4, (value / maxDuration.value) * 100)}%`;
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

async function copyTraceID(traceID: string) {
  try {
    await navigator.clipboard.writeText(traceID);
  } catch {
    fallbackCopy(traceID);
  }
  copiedTraceID.value = traceID;
  window.setTimeout(() => { if (copiedTraceID.value === traceID) copiedTraceID.value = ""; }, 1200);
}

function openTrace(trace: TraceSummary) {
  emit("open", trace, viewport.value?.scrollTop ?? 0);
}

async function restoreScroll(scrollTop: number) {
  await nextTick();
  if (viewport.value) viewport.value.scrollTop = scrollTop;
}

defineExpose({ restoreScroll });
</script>

<template>
  <section
    ref="viewport"
    class="trace-search-results"
    aria-label="Trace 搜索结果"
    data-testid="trace-search-results"
  >
    <article
      v-for="trace in traces"
      :key="trace.trace_id"
      class="trace-summary-row"
      :class="{ 'is-active': activeTraceID === trace.trace_id }"
    >
      <UButton
        class="trace-summary-row__open"
        color="neutral"
        variant="ghost"
        :aria-label="`打开 Trace ${trace.trace_id}`"
        @click="openTrace(trace)"
      >
        <span
          class="trace-summary-row__signal"
          :class="{ 'has-error': trace.error_span_count > 0 }"
        >
          <UIcon
            :name="trace.error_span_count ? 'i-lucide-circle-alert' : 'i-lucide-route'"
            aria-hidden="true"
          />
        </span>
        <span class="trace-summary-row__copy">
          <span class="trace-summary-row__path">
            <strong>{{ trace.root_service }}</strong>
            <UIcon name="i-lucide-arrow-right" aria-hidden="true" />
            <span>{{ trace.root_operation }}</span>
          </span>
          <small>{{ trace.resource.kind }} · {{ trace.resource.namespace }}/{{ trace.resource.name }}</small>
        </span>
        <span class="trace-summary-row__metrics">
          <b>{{ formatDuration(trace.duration_ms) }}</b>
          <i aria-hidden="true"><span :style="{ width: durationWidth(trace.duration_ms) }" /></i>
          <small>{{ trace.span_count }} Span · <em :class="{ 'is-error': trace.error_span_count > 0 }">{{ trace.error_span_count }} 错误</em></small>
        </span>
        <time :datetime="trace.start_time">{{ formatTime(trace.start_time) }}</time>
        <UIcon
          name="i-lucide-chevron-right"
          aria-hidden="true"
        />
      </UButton>
      <UTooltip text="复制 Trace ID">
        <UButton
          color="neutral"
          variant="ghost"
          :icon="copiedTraceID === trace.trace_id ? 'i-lucide-check' : 'i-lucide-copy'"
          square
          :aria-label="`复制 Trace ID ${trace.trace_id}`"
          @click="copyTraceID(trace.trace_id)"
        />
      </UTooltip>
    </article>
  </section>
</template>

<style scoped>
.trace-search-results { display: grid; max-height: 590px; overflow-y: auto; gap: var(--co-space-2); padding: 2px; }
.trace-summary-row { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr) 34px; align-items: center; overflow: hidden; border: 1px solid var(--co-border-subtle); border-radius: var(--co-radius-frame); background: color-mix(in srgb, var(--co-bg-surface) 86%, var(--co-bg-canvas)); box-shadow: var(--co-shadow-row); transition: border-color var(--co-motion-fast) var(--co-ease-out), background var(--co-motion-fast) var(--co-ease-out), box-shadow var(--co-motion-fast) var(--co-ease-out); }
.trace-summary-row:hover,
.trace-summary-row.is-active { z-index: 1; border-color: var(--co-border-default); background: var(--co-bg-hover); box-shadow: var(--co-shadow-section); }
.trace-summary-row.is-active { box-shadow: inset 0 0 0 1px var(--co-action-primary); }
.trace-summary-row__open { display: grid; min-height: 74px; grid-template-columns: 38px minmax(0, 1fr) auto 116px 18px; align-items: center; justify-content: stretch; gap: var(--co-space-3); padding: var(--co-space-3); text-align: left; }
.trace-summary-row__signal { display: grid; width: 34px; height: 34px; place-items: center; border: 1px solid var(--co-status-success-border); border-radius: var(--co-radius-control); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.trace-summary-row__signal.has-error { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.trace-summary-row__copy { display: grid; min-width: 0; gap: 4px; }
.trace-summary-row__path { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); }
.trace-summary-row__path svg { flex: 0 0 auto; color: var(--co-text-muted); }
.trace-summary-row__copy strong,
.trace-summary-row__path > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.trace-summary-row__copy strong { font-size: 12px; }
.trace-summary-row__path > span { color: var(--co-text-secondary); font-size: 11px; }
.trace-summary-row__copy small { color: var(--co-text-muted); font-size: 9px; }
.trace-summary-row__metrics { display: grid; min-width: 76px; gap: 2px; }
.trace-summary-row__metrics b { font-family: var(--co-font-mono); font-size: 15px; font-variant-numeric: tabular-nums; }
.trace-summary-row__metrics i { display: block; width: 92px; height: 4px; overflow: hidden; border-radius: var(--co-radius-pill); background: var(--co-bg-subtle); }
.trace-summary-row__metrics i span { display: block; height: 100%; border-radius: inherit; background: var(--co-status-success-fg); }
.trace-summary-row__metrics small { color: var(--co-text-muted); font-size: 9px; white-space: nowrap; }
.trace-summary-row__metrics em { font-style: normal; }
.trace-summary-row__metrics em.is-error { color: var(--co-status-critical-fg); }
.trace-summary-row__open > time { color: var(--co-text-muted); font-size: 9px; text-align: right; font-variant-numeric: tabular-nums; }

@media (max-width: 1024px) {
  .trace-summary-row__open { grid-template-columns: 34px minmax(0, 1fr) auto 18px; }
  .trace-summary-row__open > time { display: none; }
}

@media (prefers-reduced-motion: reduce) {
  .trace-summary-row { transition: none; }
}
</style>
