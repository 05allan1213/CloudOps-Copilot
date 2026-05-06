<script setup lang="ts">
import { LineChart } from "echarts/charts";
import {
  GridComponent,
  LegendComponent,
  TooltipComponent,
} from "echarts/components";
import * as echarts from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { RouterLink } from "vue-router";

import { fetchHostMetrics } from "../api/hosts";
import { useMonitorStore } from "../stores/monitor";
import type { HostMetricsResponse, RangeSeries } from "../types";

echarts.use([LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer]);

const props = defineProps<{
  instance: string;
}>();

type RangeOption = "15m" | "1h" | "6h" | "24h";

const monitor = useMonitorStore();
const selectedRange = ref<RangeOption>("1h");
const metrics = ref<HostMetricsResponse | null>(null);
const loading = ref(true);
const error = ref("");
const chartEl = ref<HTMLDivElement | null>(null);
let chart: echarts.ECharts | null = null;
let resizeObserver: ResizeObserver | null = null;

const instanceName = computed(() => props.instance);
const currentHost = computed(() =>
  monitor.hosts.find((host) => host.instance === instanceName.value),
);
const hasPercentSeries = computed(() =>
  ["cpu", "memory", "disk"].some((name) => firstSeries(name)?.values.length),
);
const rangeOptions: { label: string; value: RangeOption }[] = [
  { label: "15 分钟", value: "15m" },
  { label: "1 小时", value: "1h" },
  { label: "6 小时", value: "6h" },
  { label: "24 小时", value: "24h" },
];

onMounted(() => {
  initChart();
  loadMetrics();
});

onBeforeUnmount(() => {
  resizeObserver?.disconnect();
  chart?.dispose();
  chart = null;
});

watch(selectedRange, () => {
  loadMetrics();
});

watch(
  metrics,
  () => {
    renderChart();
  },
);

let abortController: AbortController | null = null;
let metricsRequestId = 0;

async function loadMetrics() {
  if (abortController) {
    abortController.abort();
  }
  const requestId = ++metricsRequestId;
  const controller = new AbortController();
  abortController = controller;

  loading.value = true;
  error.value = "";
  try {
    const data = await fetchHostMetrics(
      instanceName.value,
      { range: selectedRange.value },
      controller.signal,
    );
    if (requestId !== metricsRequestId) {
      return;
    }
    metrics.value = data;
  } catch (err) {
    if (requestId !== metricsRequestId || controller.signal.aborted) {
      return;
    }
    error.value = err instanceof Error ? err.message : "加载主机详情失败";
    metrics.value = null;
  } finally {
    if (requestId === metricsRequestId) {
      loading.value = false;
      abortController = null;
    }
  }
}

let resizeDebounceTimer: number | null = null;

function initChart() {
  if (!chartEl.value) {
    return;
  }

  chart = echarts.init(chartEl.value, "dark");
  resizeObserver = new ResizeObserver(() => {
    if (resizeDebounceTimer !== null) {
      clearTimeout(resizeDebounceTimer);
    }
    resizeDebounceTimer = window.setTimeout(() => {
      chart?.resize();
      resizeDebounceTimer = null;
    }, 100);
  });
  resizeObserver.observe(chartEl.value);
  renderChart();
}

function renderChart() {
  void nextTick(() => {
    if (!chart) {
      return;
    }

    const xValues = timeAxisValues();
    chart.setOption({
      backgroundColor: "transparent",
      color: [
        cssVar("--warning", "#f59e0b"),
        cssVar("--info", "#06b6d4"),
        cssVar("--danger", "#ef4444"),
      ],
      grid: {
        left: 42,
        right: 20,
        top: 42,
        bottom: 42,
      },
      legend: {
        top: 0,
        right: 0,
        textStyle: {
          color: cssVar("--text-secondary", "#9ca3af"),
        },
      },
      tooltip: {
        trigger: "axis",
        valueFormatter: (value: unknown) => {
          const num = typeof value === "number" ? value : Number(value);
          return Number.isFinite(num) ? `${roundMetric(num)}%` : "--";
        },
      },
      xAxis: {
        type: "category",
        data: xValues,
        axisLabel: {
          color: cssVar("--text-secondary", "#9ca3af"),
        },
        axisLine: {
          lineStyle: {
            color: cssVar("--border-color", "rgba(75, 85, 99, 0.35)"),
          },
        },
      },
      yAxis: {
        type: "value",
        min: 0,
        max: 100,
        axisLabel: {
          color: cssVar("--text-secondary", "#9ca3af"),
          formatter: "{value}%",
        },
        splitLine: {
          lineStyle: {
            color: cssVar("--border-color", "rgba(75, 85, 99, 0.35)"),
          },
        },
      },
      series: [
        lineSeries("CPU", "cpu"),
        lineSeries("内存", "memory"),
        lineSeries("磁盘", "disk"),
      ].filter((item) => item.data.length > 0),
    });
  });
}

function lineSeries(name: string, metricName: string) {
  return {
    name,
    type: "line",
    smooth: true,
    showSymbol: false,
    data: metricValues(metricName),
  };
}

function timeAxisValues(): string[] {
  const series = firstSeries("cpu") ?? firstSeries("memory") ?? firstSeries("disk");
  return (
    series?.values.map((point) =>
      new Date(point.timestamp).toLocaleTimeString("zh-CN", {
        hour: "2-digit",
        minute: "2-digit",
      }),
    ) ?? []
  );
}

function metricValues(metricName: string): number[] {
  return firstSeries(metricName)?.values.map((point) => roundMetric(point.value)) ?? [];
}

function firstSeries(metricName: string): RangeSeries | undefined {
  return metrics.value?.metrics[metricName]?.[0];
}

function latestMetricValue(metricName: string): number | null {
  const values = firstSeries(metricName)?.values ?? [];
  const last = values[values.length - 1];
  return last ? last.value : null;
}

function formatPercent(value: number | null): string {
  return value === null ? "--" : `${roundMetric(value).toFixed(1)}%`;
}

function formatNumber(value: number | null): string {
  return value === null ? "--" : roundMetric(value).toString();
}

function formatBytesPerSecond(value: number | null): string {
  if (value === null) {
    return "--";
  }
  if (value >= 1024 * 1024) {
    return `${(value / 1024 / 1024).toFixed(2)} MB/s`;
  }
  if (value >= 1024) {
    return `${(value / 1024).toFixed(1)} KB/s`;
  }
  return `${value.toFixed(0)} B/s`;
}

function formatUptime(value: number | null): string {
  if (value === null) {
    return "--";
  }
  const days = Math.floor(value / 86400);
  const hours = Math.floor((value % 86400) / 3600);
  if (days > 0) {
    return `${days}天 ${hours}小时`;
  }
  return `${hours}小时`;
}

function roundMetric(value: number): number {
  return Number(value.toFixed(1));
}

function cssVar(name: string, fallback: string): string {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return value || fallback;
}
</script>

<template>
  <section class="detail-header">
    <div>
      <RouterLink to="/hosts" class="back-link">返回主机列表</RouterLink>
      <h2>{{ instanceName }}</h2>
      <p>
        {{
          currentHost
            ? currentHost.status === "up"
              ? "当前在线"
              : "当前离线"
            : "主机指标趋势"
        }}
      </p>
    </div>
    <div class="range-tabs">
      <button
        v-for="option in rangeOptions"
        :key="option.value"
        type="button"
        class="range-btn"
        :class="{ active: selectedRange === option.value }"
        @click="selectedRange = option.value"
      >
        {{ option.label }}
      </button>
    </div>
  </section>

  <section class="metric-grid">
    <div class="metric-card">
      <span>CPU</span>
      <strong>{{ formatPercent(latestMetricValue("cpu")) }}</strong>
    </div>
    <div class="metric-card">
      <span>内存</span>
      <strong>{{ formatPercent(latestMetricValue("memory")) }}</strong>
    </div>
    <div class="metric-card">
      <span>磁盘</span>
      <strong>{{ formatPercent(latestMetricValue("disk")) }}</strong>
    </div>
    <div class="metric-card">
      <span>接收速率</span>
      <strong>{{ formatBytesPerSecond(latestMetricValue("network_recv")) }}</strong>
    </div>
    <div class="metric-card">
      <span>发送速率</span>
      <strong>{{ formatBytesPerSecond(latestMetricValue("network_sent")) }}</strong>
    </div>
    <div class="metric-card">
      <span>Load 1m</span>
      <strong>{{ formatNumber(latestMetricValue("load1")) }}</strong>
    </div>
    <div class="metric-card">
      <span>进程数</span>
      <strong>{{ formatNumber(latestMetricValue("process_count")) }}</strong>
    </div>
    <div class="metric-card">
      <span>运行时间</span>
      <strong>{{ formatUptime(latestMetricValue("uptime")) }}</strong>
    </div>
  </section>

  <section class="panel">
    <div class="panel-header">
      <div class="panel-title">
        <h2>资源趋势</h2>
      </div>
      <span class="panel-badge">{{ selectedRange }}</span>
    </div>
    <div v-if="loading" class="chart-state">加载中</div>
    <div v-else-if="error" class="chart-state chart-error">{{ error }}</div>
    <div v-else-if="!hasPercentSeries" class="chart-state">暂无趋势数据</div>
    <div ref="chartEl" class="chart-canvas" :class="{ hidden: loading || error || !hasPercentSeries }"></div>
  </section>
</template>

<style scoped>
.detail-header {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  align-items: flex-start;
  margin-bottom: 1rem;
}

.back-link {
  display: inline-flex;
  margin-bottom: 0.5rem;
  color: var(--accent);
  font-size: 0.78rem;
  font-weight: 700;
}

.detail-header h2 {
  margin: 0;
  font-size: 1.2rem;
}

.detail-header p {
  margin-top: 0.35rem;
  color: var(--text-muted);
  font-size: 0.82rem;
}

.range-tabs {
  display: flex;
  gap: 0.25rem;
  padding: 0.25rem;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  flex-wrap: wrap;
}

.range-btn {
  color: var(--text-muted);
  border-radius: var(--radius-sm);
  padding: 0.35rem 0.65rem;
  font-size: 0.75rem;
  font-weight: 700;
}

.range-btn.active {
  color: var(--accent);
  background: var(--accent-soft);
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 0.75rem;
  margin-bottom: 1rem;
}

.metric-card {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 0.9rem;
}

.metric-card span {
  display: block;
  color: var(--text-muted);
  font-size: 0.72rem;
  font-weight: 700;
  margin-bottom: 0.45rem;
}

.metric-card strong {
  font-size: 1.05rem;
  font-variant-numeric: tabular-nums;
}

.panel {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  padding: 1.25rem 1.5rem;
  margin-bottom: 1.5rem;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1rem;
}

.panel-title h2 {
  font-size: 1rem;
  margin: 0;
}

.panel-badge {
  color: var(--accent);
  background: var(--accent-soft);
  border-radius: var(--radius-sm);
  padding: 0.2rem 0.6rem;
  font-size: 0.7rem;
  font-weight: 700;
}

.chart-canvas {
  height: 340px;
  width: 100%;
}

.chart-canvas.hidden {
  visibility: hidden;
  height: 0;
}

.chart-state {
  min-height: 180px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  font-size: 0.9rem;
}

.chart-error {
  color: var(--danger);
}

@media (max-width: 768px) {
  .detail-header {
    flex-direction: column;
  }
}
</style>
