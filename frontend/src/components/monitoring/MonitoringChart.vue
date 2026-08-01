<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";

import type { QuerySeries } from "../../api/monitoring";
import {
  monitoringChartTheme,
  monitoringSeriesLabel,
  projectMonitoringSeries,
} from "./monitoringChart";

const props = withDefaults(defineProps<{
  series: QuerySeries[];
  maxPoints?: number;
}>(), {
  maxPoints: 2400,
});

const emit = defineEmits<{
  cursor: [timestampSeconds: number | null];
  rangeChange: [range: { from: number; to: number } | null];
}>();

const root = ref<HTMLElement | null>(null);
const host = ref<HTMLElement | null>(null);
const hoverIndex = ref<number | null>(null);
const tooltipLeft = ref(0);
const tooltipTop = ref(0);
const projection = computed(() => projectMonitoringSeries(props.series, props.maxPoints));
const tooltipRows = computed(() => {
  const index = hoverIndex.value;
  if (index === null) return [];
  return props.series.map((item, seriesIndex) => ({
    label: monitoringSeriesLabel(item, seriesIndex),
    value: projection.value.data[seriesIndex + 1]?.[index] ?? null,
  }));
});
const tooltipTimestamp = computed(() => {
  const index = hoverIndex.value;
  const timestamp = index === null ? null : projection.value.timestamps[index];
  return timestamp == null ? "" : new Date(timestamp * 1000).toLocaleString("zh-CN");
});
const keyboardSummary = computed(() => tooltipRows.value.length
  ? `${tooltipTimestamp.value}，${tooltipRows.value.map((row) => `${row.label} ${row.value ?? "无值"}`).join("，")}`
  : "");

let chart: uPlot | null = null;
let resizeObserver: ResizeObserver | null = null;
let themeObserver: MutationObserver | null = null;
let rebuilding = false;

function theme() {
  const styles = getComputedStyle(root.value ?? document.documentElement);
  return monitoringChartTheme((name) => styles.getPropertyValue(name));
}

function chartHeight(): number {
  const viewport = window.innerHeight;
  return Math.max(320, Math.min(420, viewport - 470));
}

function updateCursor(self: uPlot) {
  const index = self.cursor.idx;
  if (index === null || index === undefined || !Number.isFinite(index)) {
    hoverIndex.value = null;
    emit("cursor", null);
    return;
  }
  hoverIndex.value = index;
  tooltipLeft.value = Math.min(Math.max(12, (self.cursor.left ?? 0) + 18), Math.max(12, self.width - 280));
  tooltipTop.value = Math.max(12, (self.cursor.top ?? 0) + 12);
  emit("cursor", projection.value.timestamps[index] ?? null);
}

function updateRange(self: uPlot, scaleKey: string) {
  if (scaleKey !== "x" || self.status === 0) return;
  const scale = self.scales.x;
  if (typeof scale.min !== "number" || typeof scale.max !== "number") return;
  emit("rangeChange", { from: scale.min, to: scale.max });
}

function createChart() {
  const target = host.value;
  if (!target || !projection.value.timestamps.length) return;
  chart?.destroy();
  target.replaceChildren();
  const colors = theme();
  const labels = props.series.map(monitoringSeriesLabel);
  const options: uPlot.Options = {
    width: Math.max(320, target.clientWidth),
    height: chartHeight(),
    padding: [12, 12, 0, 0],
    legend: { show: false },
    cursor: {
      drag: { x: true, y: false, setScale: true },
      points: { show: false },
      x: true,
      y: false,
    },
    scales: { x: { time: true } },
    axes: [
      {
        stroke: colors.text,
        grid: { stroke: colors.grid, width: 1 },
        ticks: { stroke: colors.border, width: 1 },
        values: (_self, values) => values.map((value) => new Date(value * 1000).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })),
      },
      {
        stroke: colors.text,
        grid: { stroke: colors.grid, width: 1 },
        ticks: { stroke: colors.border, width: 1 },
        size: 68,
      },
    ],
    series: [
      { label: "时间" },
      ...labels.map((label, index) => ({
        label,
        stroke: colors.series[index % colors.series.length],
        width: 2,
        spanGaps: false,
        points: { show: false },
      })),
    ],
    hooks: {
      setCursor: [updateCursor],
      setScale: [updateRange],
    },
  };
  chart = new uPlot(options, projection.value.data, target);
  chart.over.style.cursor = "crosshair";
}

async function rebuildChart() {
  if (rebuilding) return;
  rebuilding = true;
  await nextTick();
  createChart();
  rebuilding = false;
}

function moveKeyboardCursor(event: KeyboardEvent) {
  if (!projection.value.timestamps.length || !chart) return;
  const lastIndex = projection.value.timestamps.length - 1;
  let nextIndex = hoverIndex.value ?? lastIndex;
  if (event.key === "ArrowLeft") nextIndex = Math.max(0, nextIndex - 1);
  else if (event.key === "ArrowRight") nextIndex = Math.min(lastIndex, nextIndex + 1);
  else if (event.key === "Home") nextIndex = 0;
  else if (event.key === "End") nextIndex = lastIndex;
  else return;
  event.preventDefault();
  hoverIndex.value = nextIndex;
  const left = chart.valToPos(projection.value.timestamps[nextIndex] ?? 0, "x");
  chart.setCursor({ left, top: Math.max(0, chart.bbox.height / uPlot.pxRatio / 2) });
  emit("cursor", projection.value.timestamps[nextIndex] ?? null);
}

function resetRange() {
  const timestamps = projection.value.timestamps;
  if (!chart || timestamps.length < 2) return;
  chart.setScale("x", { min: timestamps[0] ?? 0, max: timestamps[timestamps.length - 1] ?? 0 });
  emit("rangeChange", null);
}

onMounted(() => {
  void rebuildChart();
  resizeObserver = new ResizeObserver(() => {
    if (!chart || !host.value) return;
    chart.setSize({ width: Math.max(320, host.value.clientWidth), height: chartHeight() });
  });
  if (host.value) resizeObserver.observe(host.value);
  themeObserver = new MutationObserver(() => void rebuildChart());
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "data-theme"] });
});

watch(() => props.series, () => void rebuildChart(), { deep: true });

onBeforeUnmount(() => {
  resizeObserver?.disconnect();
  themeObserver?.disconnect();
  chart?.destroy();
  chart = null;
});
</script>

<template>
  <section
    ref="root"
    class="monitoring-chart"
    aria-labelledby="monitoring-chart-title"
  >
    <header class="monitoring-chart__header">
      <div>
        <h3 id="monitoring-chart-title">
          时序图
        </h3>
        <span>
          {{ series.length }} series · {{ projection.renderedTimestampCount }} timestamps
          <template v-if="projection.downsampled"> / {{ projection.sourceTimestampCount }}</template>
        </span>
      </div>
      <UButton
        color="neutral"
        variant="ghost"
        size="sm"
        icon="i-lucide-scan-line"
        label="重置范围"
        @click="resetRange"
      />
    </header>
    <div
      class="monitoring-chart__surface"
      role="application"
      tabindex="0"
      :aria-label="`${series.length} 条 Prometheus 时序`"
      @keydown="moveKeyboardCursor"
    >
      <div
        ref="host"
        class="monitoring-chart__host"
      />
      <div
        v-if="hoverIndex !== null"
        class="monitoring-chart__tooltip"
        :style="{ left: `${tooltipLeft}px`, top: `${tooltipTop}px` }"
        aria-hidden="true"
      >
        <strong>{{ tooltipTimestamp }}</strong>
        <span
          v-for="row in tooltipRows"
          :key="row.label"
        >
          <i aria-hidden="true" />
          <b>{{ row.label }}</b>
          <code>{{ row.value ?? "无" }}</code>
        </span>
      </div>
    </div>
    <p
      class="sr-only"
      aria-live="polite"
    >
      {{ keyboardSummary }}
    </p>
  </section>
</template>

<style>
.monitoring-chart {
  min-width: 0;
  border-bottom: 1px solid var(--co-border-default);
}

.monitoring-chart__header {
  display: flex;
  min-height: 48px;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
}

.monitoring-chart__header h3 { margin: 0; font-size: 14px; }
.monitoring-chart__header span { color: var(--co-text-muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.monitoring-chart__surface {
  position: relative;
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-frame);
  background: var(--co-bg-canvas);
  outline: none;
}
.monitoring-chart__surface:focus-visible { box-shadow: inset 0 0 0 2px var(--co-focus-ring); }
.monitoring-chart__host { width: 100%; min-height: 320px; }
.monitoring-chart__host .uplot { max-width: 100%; color: var(--co-text-secondary); font-family: var(--co-font-sans); }
.monitoring-chart__host .u-select { background: color-mix(in srgb, var(--co-action-primary) 16%, transparent); }
.monitoring-chart__host .u-cursor-x { border-color: var(--co-focus-ring); }
.monitoring-chart__tooltip {
  position: absolute;
  z-index: 2;
  display: grid;
  width: min(260px, calc(100% - 24px));
  max-height: 180px;
  gap: 4px;
  padding: var(--co-space-2) var(--co-space-3);
  overflow: auto;
  border: 1px solid var(--co-border-strong);
  border-radius: var(--co-radius-control);
  background: var(--co-bg-overlay);
  box-shadow: var(--co-shadow-overlay);
  color: var(--co-text-primary);
  pointer-events: none;
}
.monitoring-chart__tooltip > strong { font-size: 11px; font-variant-numeric: tabular-nums; }
.monitoring-chart__tooltip > span { display: grid; grid-template-columns: 6px minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-2); font-size: 11px; }
.monitoring-chart__tooltip i { width: 6px; height: 6px; border-radius: 50%; background: var(--co-action-primary); }
.monitoring-chart__tooltip b { min-width: 0; overflow: hidden; font-family: var(--co-font-mono); font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.monitoring-chart__tooltip code { font-variant-numeric: tabular-nums; }

@media (prefers-reduced-motion: reduce) {
  .monitoring-chart * { scroll-behavior: auto; }
}
</style>
