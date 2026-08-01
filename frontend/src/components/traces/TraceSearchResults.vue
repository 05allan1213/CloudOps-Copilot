<script setup lang="ts">
import { nextTick, ref } from "vue";

import type { TraceSummary } from "../../api/telemetry";

defineProps<{
  traces: TraceSummary[];
  activeTraceID: string;
}>();

const emit = defineEmits<{
  open: [trace: TraceSummary, scrollTop: number];
}>();

const viewport = ref<HTMLElement | null>(null);
const copiedTraceID = ref("");

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
        <span>
          <strong>{{ trace.root_service }} · {{ trace.root_operation }}</strong>
          <code>{{ trace.trace_id }}</code>
          <small>{{ formatTime(trace.start_time) }}</small>
        </span>
        <b>{{ formatDuration(trace.duration_ms) }}</b>
        <b>{{ trace.span_count }} spans</b>
        <b :class="{ 'is-error': trace.error_span_count > 0 }">{{ trace.error_span_count }} errors</b>
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
.trace-search-results { max-height: 560px; overflow-y: auto; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); }
.trace-summary-row { display: grid; grid-template-columns: minmax(0, 1fr) 34px; align-items: center; border-bottom: 1px solid var(--co-border-subtle); }
.trace-summary-row:hover,
.trace-summary-row.is-active { background: var(--co-bg-hover); }
.trace-summary-row.is-active { box-shadow: inset var(--co-severity-marker-width) 0 0 var(--co-action-primary); }
.trace-summary-row__open { display: grid; min-height: 66px; grid-template-columns: minmax(0, 1fr) auto auto auto 20px; justify-content: stretch; gap: var(--co-space-4); padding: var(--co-space-2); text-align: left; }
.trace-summary-row__open > span { display: grid; min-width: 0; gap: 2px; }
.trace-summary-row__open strong,
.trace-summary-row__open code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.trace-summary-row__open code,
.trace-summary-row__open small { color: var(--co-text-muted); font-size: 10px; }
.trace-summary-row__open > b { align-self: center; color: var(--co-text-secondary); font-size: 11px; font-variant-numeric: tabular-nums; }
.trace-summary-row__open > b.is-error { color: var(--co-status-critical-fg); }

@media (max-width: 1024px) {
  .trace-summary-row__open { grid-template-columns: minmax(0, 1fr) auto auto 20px; }
  .trace-summary-row__open > b:nth-of-type(3) { display: none; }
}
</style>
