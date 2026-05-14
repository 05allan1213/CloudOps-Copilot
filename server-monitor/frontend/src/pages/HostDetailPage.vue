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
import { useRouter } from "vue-router";
import { ArrowLeft } from "@element-plus/icons-vue";

import { fetchHostMetrics } from "../api/hosts";
import { useTheme } from "../composables/useTheme";
import StateWrapper from "../components/common/StateWrapper.vue";
import { useMonitorStore } from "../stores/monitor";
import type { HostMetricsResponse, RangeSeries } from "../types";

echarts.use([LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer]);

const props = defineProps<{
  instance: string;
}>();

type RangeOption = "15m" | "1h" | "6h" | "24h";

const router = useRouter();
const monitor = useMonitorStore();
const { isDark, getEChartsTheme } = useTheme();

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
const pageState = computed<"loading" | "error" | "default">(() => {
  if (loading.value) return "loading";
  if (error.value) return "error";
  return "default";
});

const rangeOptions: { label: string; value: RangeOption }[] = [
  { label: "15 分钟", value: "15m" },
  { label: "1 小时", value: "1h" },
  { label: "6 小时", value: "6h" },
  { label: "24 小时", value: "24h" },
];

const metricCards = computed(() => [
  { label: "CPU", value: formatPercent(latestMetricValue("cpu")), icon: "cpu" },
  { label: "内存", value: formatPercent(latestMetricValue("memory")), icon: "memory" },
  { label: "磁盘", value: formatPercent(latestMetricValue("disk")), icon: "disk" },
  { label: "接收速率", value: formatBytesPerSecond(latestMetricValue("network_recv")), icon: "download" },
  { label: "发送速率", value: formatBytesPerSecond(latestMetricValue("network_sent")), icon: "upload" },
  { label: "Load 1m", value: formatNumber(latestMetricValue("load1")), icon: "load" },
  { label: "进程数", value: formatNumber(latestMetricValue("process_count")), icon: "process" },
  { label: "运行时间", value: formatUptime(latestMetricValue("uptime")), icon: "uptime" },
]);

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

watch(metrics, () => {
  renderChart();
});

watch(isDark, () => {
  renderChart();
});

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

  chart = echarts.init(chartEl.value);
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

    const theme = getEChartsTheme(isDark.value);
    const xValues = timeAxisValues();

    chart.setOption({
      backgroundColor: theme.backgroundColor,
      color: ["#f59e0b", "#06b6d4", "#ef4444"],
      grid: {
        left: 42,
        right: 20,
        top: 42,
        bottom: 42,
      },
      legend: {
        top: 0,
        right: 0,
        textStyle: theme.legend.textStyle,
      },
      tooltip: {
        trigger: "axis",
        backgroundColor: theme.tooltip.backgroundColor,
        borderColor: theme.tooltip.borderColor,
        textStyle: theme.tooltip.textStyle,
        valueFormatter: (value: unknown) => {
          const num = typeof value === "number" ? value : Number(value);
          return Number.isFinite(num) ? `${roundMetric(num)}%` : "--";
        },
      },
      xAxis: {
        type: "category",
        data: xValues,
        axisLabel: theme.xAxis.axisLabel,
        axisLine: theme.xAxis.axisLine,
      },
      yAxis: {
        type: "value",
        min: 0,
        max: 100,
        axisLabel: {
          ...theme.yAxis.axisLabel,
          formatter: "{value}%",
        },
        splitLine: theme.yAxis.splitLine,
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
</script>

<template>
  <div class="detail-header">
    <div>
      <el-button
        type="primary"
        link
        :icon="ArrowLeft"
        @click="router.push('/hosts')"
      >
        返回主机列表
      </el-button>
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
    <el-radio-group v-model="selectedRange" size="small">
      <el-radio-button
        v-for="option in rangeOptions"
        :key="option.value"
        :value="option.value"
      >
        {{ option.label }}
      </el-radio-button>
    </el-radio-group>
  </div>

  <el-row :gutter="12" class="metric-grid">
    <el-col
      v-for="card in metricCards"
      :key="card.label"
      :xs="12"
      :sm="6"
    >
      <el-card shadow="never" class="metric-card">
        <el-statistic :title="card.label" :value="card.value" />
      </el-card>
    </el-col>
  </el-row>

  <StateWrapper
    :state="pageState === 'default' && !hasPercentSeries ? 'empty' : pageState"
    empty-text="暂无趋势数据"
    :error-text="error"
  >
    <template #retry>
      <el-button type="primary" @click="loadMetrics()">重试</el-button>
    </template>

    <el-card shadow="never" class="chart-panel">
      <template #header>
        <div class="chart-panel-header">
          <span class="chart-panel-title">资源趋势</span>
          <el-tag size="small" type="info">{{ selectedRange }}</el-tag>
        </div>
      </template>
      <div ref="chartEl" class="chart-canvas"></div>
    </el-card>
  </StateWrapper>
</template>

<style scoped>
.detail-header {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  align-items: flex-start;
  margin-bottom: 1rem;
}

.detail-header h2 {
  margin: 0.5rem 0 0;
  font-size: 1.2rem;
}

.detail-header p {
  margin-top: 0.35rem;
  color: var(--el-text-color-secondary);
  font-size: 0.82rem;
}

.metric-grid {
  margin-bottom: 1rem;
}

.metric-card :deep(.el-card__body) {
  padding: 14px;
}

.metric-card :deep(.el-statistic__number) {
  font-size: 1.05rem;
  font-variant-numeric: tabular-nums;
}

.chart-panel :deep(.el-card__body) {
  padding: 0 20px 20px;
}

.chart-panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.chart-panel-title {
  font-size: 15px;
  font-weight: 600;
}

.chart-canvas {
  height: 340px;
  width: 100%;
}

@media (max-width: 768px) {
  .detail-header {
    flex-direction: column;
  }
}
</style>
