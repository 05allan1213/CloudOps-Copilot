<script setup lang="ts">
import type { TableColumn } from "@nuxt/ui";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";

import { createMonitoringFixture } from "../fixtures";

interface MetricRow {
  metric: string;
  value: string;
  unit: string;
  provider: string;
  state: string;
}

const rawFixture = createMonitoringFixture();
const chartHost = ref<HTMLDivElement | null>(null);
const chartSurface = ref<HTMLElement | null>(null);
const resolution = ref<"raw" | "30s">("raw");
const activeIndex = ref(rawFixture.pointCount - 1);
const renderMs = ref(0);
const visiblePoints = ref(rawFixture.pointCount);
const chartState = ref<"ready" | "empty">("ready");
const tooltipLeft = ref(0);
const tooltipTop = ref(0);
let chart: uPlot | null = null;
let resizeObserver: ResizeObserver | null = null;

const resolutionItems = [
  { label: "原始 5s", value: "raw" },
  { label: "降采样 30s", value: "30s" },
];

function downsample(values: Array<number | null>, factor: number) {
  const result: Array<number | null> = [];
  for (let index = 0; index < values.length; index += factor) {
    const bucket = values.slice(index, index + factor).filter((value): value is number => value !== null);
    result.push(bucket.length ? Number((bucket.reduce((sum, value) => sum + value, 0) / bucket.length).toFixed(3)) : null);
  }
  return result;
}

const chartData = computed<uPlot.AlignedData>(() => {
  if (chartState.value === "empty") return [[], [], [], []];
  if (resolution.value === "raw") return [rawFixture.timestamps, rawFixture.cpu, rawFixture.latency, rawFixture.errors];
  const factor = 6;
  return [
    rawFixture.timestamps.filter((_, index) => index % factor === 0),
    downsample(rawFixture.cpu, factor),
    downsample(rawFixture.latency, factor),
    downsample(rawFixture.errors, factor),
  ];
});

const selectedTimestamp = computed(() => {
  const values = chartData.value[0];
  if (!values.length) return "无数据";
  const index = Math.min(activeIndex.value, values.length - 1);
  return new Date(Number(values[index]) * 1000).toISOString();
});

function valueAt(seriesIndex: number, digits: number) {
  const values = chartData.value[seriesIndex];
  if (!values?.length) return "-";
  const value = values[Math.min(activeIndex.value, values.length - 1)];
  return typeof value === "number" ? value.toFixed(digits) : "缺测";
}

const synchronizedRows = computed<MetricRow[]>(() => [
  { metric: "CPU 使用率", value: valueAt(1, 2), unit: "%", provider: "Prometheus A", state: valueAt(1, 2) === "缺测" ? "空点" : "可用" },
  { metric: "P95 延迟", value: valueAt(2, 2), unit: "ms", provider: "Prometheus A", state: valueAt(2, 2) === "缺测" ? "空点" : "可用" },
  { metric: "错误率", value: valueAt(3, 3), unit: "%", provider: "Prometheus B", state: "Partial" },
]);

const tableColumns: TableColumn<MetricRow>[] = [
  { accessorKey: "metric", header: "指标" },
  { accessorKey: "value", header: "值" },
  { accessorKey: "unit", header: "单位" },
  { accessorKey: "provider", header: "Provider" },
  { accessorKey: "state", header: "状态" },
];

function token(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function chartOptions(width: number, height: number): uPlot.Options {
  return {
    width,
    height,
    padding: [12, 10, 0, 0],
    legend: { show: false },
    cursor: {
      drag: { x: true, y: false, setScale: true },
      points: { size: 7, width: 2 },
    },
    scales: {
      x: { time: true },
      cpu: { range: [0, 100] },
      latency: { auto: true },
      errors: { range: [0, 8] },
    },
    axes: [
      { stroke: token("--co-text-muted"), grid: { stroke: token("--co-border") }, ticks: { stroke: token("--co-border-strong") }, font: "10px system-ui" },
      { scale: "cpu", label: "%", side: 3, size: 48, stroke: token("--co-text-muted"), grid: { stroke: token("--co-border") }, ticks: { stroke: token("--co-border-strong") }, font: "10px system-ui" },
      { scale: "latency", label: "ms", side: 1, size: 52, stroke: token("--co-text-muted"), grid: { show: false }, ticks: { stroke: token("--co-border-strong") }, font: "10px system-ui" },
    ],
    series: [
      {},
      { label: "CPU", scale: "cpu", stroke: token("--co-action"), width: 2, spanGaps: false },
      { label: "P95", scale: "latency", stroke: token("--co-warning-fg"), width: 1.6, spanGaps: false },
      { label: "Errors", scale: "errors", stroke: token("--co-critical-fg"), width: 1.6, spanGaps: false },
    ],
    hooks: {
      setCursor: [
        (instance) => {
          if (instance.cursor.idx === null || instance.cursor.idx === undefined) return;
          activeIndex.value = instance.cursor.idx;
          tooltipLeft.value = Math.min(Number(instance.cursor.left ?? 0) + 68, Math.max(80, instance.width - 190));
          tooltipTop.value = Math.max(14, Number(instance.cursor.top ?? 0) + 8);
        },
      ],
      setScale: [
        (instance, key) => {
          if (key !== "x") return;
          const min = Number(instance.scales.x?.min ?? 0);
          const max = Number(instance.scales.x?.max ?? 0);
          const values = chartData.value[0];
          visiblePoints.value = values.filter((value) => Number(value) >= min && Number(value) <= max).length;
        },
      ],
    },
  };
}

function destroyChart() {
  chart?.destroy();
  chart = null;
}

function buildChart() {
  const host = chartHost.value;
  if (!host) return;
  destroyChart();
  host.replaceChildren();
  const started = performance.now();
  const width = Math.max(320, Math.floor(host.clientWidth));
  const height = Math.max(300, Math.min(430, Math.floor(window.innerHeight * 0.48)));
  chart = new uPlot(chartOptions(width, height), chartData.value, host);
  activeIndex.value = Math.max(0, chartData.value[0].length - 1);
  visiblePoints.value = chartData.value[0].length;
  renderMs.value = Number((performance.now() - started).toFixed(1));
}

function setRange(minutes: number | "all") {
  const times = chartData.value[0];
  if (!chart || !times.length) return;
  const max = Number(times[times.length - 1]);
  const min = minutes === "all" ? Number(times[0]) : max - minutes * 60;
  chart.setScale("x", { min, max });
}

function moveCursor(delta: number) {
  const times = chartData.value[0];
  if (!chart || !times.length) return;
  activeIndex.value = Math.max(0, Math.min(times.length - 1, activeIndex.value + delta));
  chart.setLegend({ idx: activeIndex.value });
  chart.setCursor({ left: chart.valToPos(Number(times[activeIndex.value]), "x"), top: chart.bbox.height / uPlot.pxRatio / 2 });
}

function handleChartKey(event: KeyboardEvent) {
  if (event.key === "ArrowLeft") moveCursor(-1);
  else if (event.key === "ArrowRight") moveCursor(1);
  else if (event.key === "Home") moveCursor(-rawFixture.pointCount);
  else if (event.key === "End") moveCursor(rawFixture.pointCount);
  else return;
  event.preventDefault();
}

function toggleEmpty() {
  chartState.value = chartState.value === "ready" ? "empty" : "ready";
}

function rebuildFromTheme() {
  void nextTick(buildChart);
}

watch([resolution, chartState], () => void nextTick(buildChart));

onMounted(() => {
  buildChart();
  resizeObserver = new ResizeObserver(() => {
    const host = chartHost.value;
    if (!chart || !host) return;
    chart.setSize({ width: Math.max(320, Math.floor(host.clientWidth)), height: chart.height });
  });
  if (chartHost.value) resizeObserver.observe(chartHost.value);
  window.addEventListener("cloudops-prework-theme", rebuildFromTheme);
});

onBeforeUnmount(() => {
  resizeObserver?.disconnect();
  resizeObserver = null;
  window.removeEventListener("cloudops-prework-theme", rebuildFromTheme);
  destroyChart();
});
</script>

<template>
  <section class="workspace monitoring-lab" aria-labelledby="monitoring-title">
    <header class="workspace-header">
      <div class="workspace-title">
        <h1 id="monitoring-title" tabindex="-1">Monitoring Renderer</h1>
        <p>Prometheus 规模多序列 · 精确 UTC · Provider 部分结果</p>
      </div>
      <div class="workspace-actions">
        <UBadge color="neutral" variant="subtle" icon="i-lucide-database" :label="`${chartData[0].length.toLocaleString()} 点`" />
        <UBadge color="info" variant="subtle" icon="i-lucide-timer" :label="`${renderMs} ms`" data-testid="monitor-render-ms" />
      </div>
    </header>

    <section class="toolbar-band monitoring-toolbar" aria-label="监控图表范围">
      <div class="toolbar-group"><span class="toolbar-label">Resolution</span><USelect v-model="resolution" :items="resolutionItems" value-key="value" data-testid="monitor-resolution" /></div>
      <div class="range-buttons" role="group" aria-label="时间范围">
        <UButton color="neutral" variant="outline" label="15m" @click="setRange(15)" />
        <UButton color="neutral" variant="outline" label="1h" @click="setRange(60)" />
        <UButton color="neutral" variant="outline" label="全部" @click="setRange('all')" />
      </div>
      <UTooltip text="上一个采样点"><UButton color="neutral" variant="ghost" square icon="i-lucide-chevron-left" aria-label="上一个采样点" @click="moveCursor(-1)" /></UTooltip>
      <UTooltip text="下一个采样点"><UButton color="neutral" variant="ghost" square icon="i-lucide-chevron-right" aria-label="下一个采样点" @click="moveCursor(1)" /></UTooltip>
      <UButton color="neutral" variant="ghost" :icon="chartState === 'ready' ? 'i-lucide-chart-no-axes-column-decreasing' : 'i-lucide-chart-no-axes-combined'" :label="chartState === 'ready' ? '空结果' : '恢复数据'" data-testid="toggle-monitor-empty" @click="toggleEmpty" />
      <span class="visible-count">视窗 {{ visiblePoints.toLocaleString() }} 点</span>
    </section>

    <UAlert color="warning" variant="soft" icon="i-lucide-split" title="Partial Provider" description="Prometheus A 连续；Prometheus B 在 00:27:30Z–00:31:35Z 返回空点。现有数据保持可读，未声明完整。" data-testid="monitor-partial" />

    <section class="chart-band" aria-label="Prometheus 时序图">
      <div
        ref="chartSurface"
        class="chart-surface"
        role="application"
        tabindex="0"
        aria-label="多序列时序图，使用左右方向键检查采样点"
        data-testid="monitor-chart-surface"
        @keydown="handleChartKey"
      >
        <div v-if="chartState === 'ready'" ref="chartHost" class="chart-host" data-testid="monitor-chart" />
        <UEmpty v-else icon="i-lucide-chart-no-axes-column-decreasing" title="当前范围没有采样" description="Scope、时间范围和 Provider 部分状态仍然保留。" data-testid="monitor-empty" />
        <div v-if="chartState === 'ready'" class="chart-tooltip" :style="{ left: `${tooltipLeft}px`, top: `${tooltipTop}px` }" aria-hidden="true">
          <strong>{{ selectedTimestamp.slice(11, 19) }} UTC</strong>
          <span>CPU {{ valueAt(1, 2) }}%</span>
          <span>P95 {{ valueAt(2, 2) }}ms</span>
          <span>Error {{ valueAt(3, 3) }}%</span>
        </div>
      </div>
    </section>

    <section class="synchronized-band" aria-labelledby="synchronized-title">
      <div class="section-heading"><h2 id="synchronized-title">{{ selectedTimestamp }}</h2><UBadge color="neutral" variant="outline" icon="i-lucide-table-properties" label="同步数据表" /></div>
      <UTable :data="synchronizedRows" :columns="tableColumns" class="metric-table" data-testid="monitor-sync-table" />
    </section>
  </section>
</template>

<style scoped>
.monitoring-lab { max-width: 1680px; margin: 0 auto; }
.monitoring-toolbar { align-items: center; }
.range-buttons { display: flex; gap: 4px; }
.visible-count { margin-left: auto; color: var(--co-text-muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.chart-band { min-width: 0; margin-top: var(--co-space-3); border-block: 1px solid var(--co-border); background: var(--co-surface); }
.chart-surface { position: relative; min-width: 0; min-height: 340px; padding: var(--co-space-3); overflow: hidden; }
.chart-host { width: 100%; min-width: 0; }
.chart-host :deep(.uplot) { color: var(--co-text-secondary); background: transparent; font-family: var(--co-font-sans); }
.chart-host :deep(canvas) { display: block; }
.chart-tooltip { position: absolute; z-index: 4; display: grid; min-width: 148px; gap: 2px; padding: 7px 9px; border: 1px solid var(--co-border-strong); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-overlay); box-shadow: var(--co-shadow-overlay); pointer-events: none; font-size: 10px; font-variant-numeric: tabular-nums; }
.chart-tooltip strong { color: var(--co-text-primary); }
.synchronized-band { min-width: 0; padding: var(--co-space-4); border-bottom: 1px solid var(--co-border); background: var(--co-surface); }
.metric-table :deep(th), .metric-table :deep(td) { height: 34px; padding-block: 4px; font-size: 11px; }
@media (max-width: 1100px) {
  .visible-count { width: 100%; margin-left: 0; }
  .chart-surface { min-height: 320px; padding-inline: var(--co-space-2); }
}
</style>
