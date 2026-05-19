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
import { fetchK8sNodeByInstance } from "../api/k8s";
import { useTheme } from "../composables/useTheme";
import StateWrapper from "../components/common/StateWrapper.vue";
import { useMonitorStore } from "../stores/monitor";
import type { HostMetricsResponse, RangeSeries, K8sNodeSummary } from "../types";

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
const k8sNode = ref<K8sNodeSummary | null>(null);
const k8sLoading = ref(false);
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
  loadK8sNode();
});

onBeforeUnmount(() => {
  resizeObserver?.disconnect();
  chart?.dispose();
  chart = null;
  k8sAbortController?.abort();
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

let k8sAbortController: AbortController | null = null;

async function loadK8sNode() {
  if (!monitor.k8sNodesEnabled) return;
  if (k8sAbortController) {
    k8sAbortController.abort();
  }
  const controller = new AbortController();
  k8sAbortController = controller;

  k8sLoading.value = true;
  try {
    k8sNode.value = await fetchK8sNodeByInstance(
      instanceName.value,
      controller.signal,
    );
  } catch {
    if (!controller.signal.aborted) {
      k8sNode.value = null;
    }
  } finally {
    if (k8sAbortController === controller) {
      k8sLoading.value = false;
      k8sAbortController = null;
    }
  }
}

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
      color: theme.chartColors,
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

  <el-card v-if="monitor.k8sNodesEnabled && k8sNode" shadow="never" class="k8s-node-card">
    <template #header>
      <div class="k8s-node-header">
        <span class="k8s-node-title">K8s Node</span>
        <el-button type="primary" link size="small" @click="router.push(`/k8s/nodes/${encodeURIComponent(k8sNode!.name)}`)">
          查看详情
        </el-button>
      </div>
    </template>
    <div class="k8s-node-info">
      <div class="k8s-node-item">
        <span class="k8s-node-label">名称</span>
        <span class="k8s-node-value">{{ k8sNode.name }}</span>
      </div>
      <div class="k8s-node-item">
        <span class="k8s-node-label">状态</span>
        <el-tag :type="k8sNode.ready ? 'success' : 'danger'" size="small">
          {{ k8sNode.ready ? "Ready" : "NotReady" }}
        </el-tag>
      </div>
      <div v-if="k8sNode.roles?.length" class="k8s-node-item">
        <span class="k8s-node-label">角色</span>
        <span class="k8s-node-value">{{ k8sNode.roles.join(", ") }}</span>
      </div>
      <div v-if="k8sNode.kubelet_version" class="k8s-node-item">
        <span class="k8s-node-label">Kubelet</span>
        <span class="k8s-node-value">{{ k8sNode.kubelet_version }}</span>
      </div>
      <div v-if="k8sNode.capacity?.cpu" class="k8s-node-item">
        <span class="k8s-node-label">CPU 容量</span>
        <span class="k8s-node-value">{{ k8sNode.capacity.cpu }}</span>
      </div>
      <div v-if="k8sNode.capacity?.memory" class="k8s-node-item">
        <span class="k8s-node-label">内存容量</span>
        <span class="k8s-node-value">{{ k8sNode.capacity.memory }}</span>
      </div>
    </div>
  </el-card>

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
  gap: 16px;
  align-items: flex-start;
  margin-bottom: 16px;
}

.detail-header h2 {
  margin: 8px 0 0;
  font-size: 19px;
}

.detail-header p {
  margin-top: 6px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.metric-grid {
  margin-bottom: 16px;
}

.metric-card :deep(.el-card__body) {
  padding: 20px;
}

.metric-card :deep(.el-statistic__number) {
  font-size: 17px;
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

.k8s-node-card {
  margin-top: 16px;
}

.k8s-node-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.k8s-node-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--cloudops-text-primary);
}

.k8s-node-info {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 12px;
}

.k8s-node-item {
  display: flex;
  align-items: center;
  gap: 8px;
}

.k8s-node-label {
  font-size: 13px;
  color: var(--cloudops-text-secondary);
  min-width: 70px;
}

.k8s-node-value {
  font-size: 13px;
  color: var(--cloudops-text-primary);
}
</style>
