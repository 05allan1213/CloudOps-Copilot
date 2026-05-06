<script setup lang="ts">
import { BarChart } from "echarts/charts";
import {
  GridComponent,
  LegendComponent,
  TooltipComponent,
} from "echarts/components";
import * as echarts from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";

import type { Host } from "../types";

echarts.use([BarChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer]);

const props = defineProps<{
  hosts: Host[];
}>();

const chartEl = ref<HTMLDivElement | null>(null);
let chart: echarts.ECharts | null = null;
let resizeObserver: ResizeObserver | null = null;
let resizeDebounceTimer: number | null = null;

type TooltipItem = {
  axisValueLabel?: string;
  marker?: string;
  seriesName?: string;
  value?: string | number;
};

const chartHosts = computed(() =>
  [...props.hosts]
    .sort((a, b) => Math.max(b.cpu, b.memory) - Math.max(a.cpu, a.memory))
    .slice(0, 12),
);

const hasData = computed(() => chartHosts.value.length > 0);

onMounted(() => {
  initChart();
});

onBeforeUnmount(() => {
  resizeObserver?.disconnect();
  chart?.dispose();
  chart = null;
});

watch(
  chartHosts,
  () => {
    renderChart();
  },
  { deep: true },
);

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

    const textColor = cssVar("--text-secondary", "#9ca3af");
    const axisColor = cssVar("--border-color", "rgba(75, 85, 99, 0.35)");

    chart.setOption({
      backgroundColor: "transparent",
      color: [cssVar("--warning", "#f59e0b"), cssVar("--info", "#06b6d4")],
      grid: {
        left: 36,
        right: 18,
        top: 44,
        bottom: 48,
      },
      legend: {
        top: 0,
        right: 0,
        textStyle: {
          color: textColor,
        },
      },
      tooltip: {
        trigger: "axis",
        axisPointer: {
          type: "shadow",
        },
        formatter: formatTooltip,
      },
      xAxis: {
        type: "category",
        data: chartHosts.value.map((host) => host.instance),
        axisLabel: {
          color: textColor,
          interval: 0,
          overflow: "truncate",
          width: 92,
        },
        axisLine: {
          lineStyle: {
            color: axisColor,
          },
        },
        axisTick: {
          show: false,
        },
      },
      yAxis: {
        type: "value",
        min: 0,
        max: 100,
        axisLabel: {
          color: textColor,
          formatter: "{value}%",
        },
        splitLine: {
          lineStyle: {
            color: axisColor,
          },
        },
      },
      series: [
        {
          name: "CPU",
          type: "bar",
          data: chartHosts.value.map((host) => roundMetric(host.cpu)),
          barMaxWidth: 22,
          itemStyle: {
            borderRadius: [4, 4, 0, 0],
          },
        },
        {
          name: "内存",
          type: "bar",
          data: chartHosts.value.map((host) => roundMetric(host.memory)),
          barMaxWidth: 22,
          itemStyle: {
            borderRadius: [4, 4, 0, 0],
          },
        },
      ],
    });
  });
}

function roundMetric(value: number): number {
  return Number(value.toFixed(1));
}

function cssVar(name: string, fallback: string): string {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return value || fallback;
}

function escapeHtml(text: string): string {
  const div = document.createElement("div");
  div.textContent = text;
  return div.innerHTML;
}

function formatTooltip(params: TooltipItem | TooltipItem[]): string {
  const items = Array.isArray(params) ? params : [params];
  const title = items[0]?.axisValueLabel ?? "";
  const rows = items.map((item) => {
    const value = item.value ?? "--";
    return `${item.marker}${item.seriesName}: ${value}%`;
  });
  return [escapeHtml(title), ...rows].join("<br />");
}
</script>

<template>
  <div class="host-resource-chart">
    <div v-if="!hasData" class="chart-empty">
      暂无主机指标
    </div>
    <div ref="chartEl" class="chart-canvas" :class="{ hidden: !hasData }"></div>
  </div>
</template>

<style scoped>
.host-resource-chart {
  min-height: 320px;
  position: relative;
}

.chart-canvas {
  width: 100%;
  height: 320px;
}

.chart-canvas.hidden {
  visibility: hidden;
}

.chart-empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  font-size: 0.9rem;
}
</style>
