<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from "vue";
import { useRouter } from "vue-router";
import * as echarts from "echarts/core";
import { GraphChart } from "echarts/charts";
import { TooltipComponent, LegendComponent } from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import PageHeader from "../components/common/PageHeader.vue";
import { fetchK8sTopology } from "../api/k8s";
import { useTheme } from "../composables/useTheme";
import type { K8sTopologyNode, K8sTopologyEdge } from "../types";

echarts.use([GraphChart, TooltipComponent, LegendComponent, CanvasRenderer]);

const router = useRouter();
const { isDark } = useTheme();

const namespace = ref("");
const loading = ref(false);
const error = ref("");
const topologyNodes = ref<K8sTopologyNode[]>([]);
const topologyEdges = ref<K8sTopologyEdge[]>([]);

let chart: echarts.ECharts | null = null;
const chartEl = ref<HTMLDivElement>();

const kindColors: Record<string, string> = {
  Node: "#67C23A",
  Deployment: "#409EFF",
  Pod: "#E6A23C",
  Service: "#F56C6C",
  DaemonSet: "#909399",
  StatefulSet: "#9B59B6",
};

const edgeStyles: Record<string, { color: string; width: number; type?: string }> = {
  scheduled: { color: "#67C23A", width: 1.5 },
  owns: { color: "#409EFF", width: 2 },
  selects: { color: "#F56C6C", width: 1, type: "dashed" },
};

async function loadTopology() {
  loading.value = true;
  error.value = "";
  try {
    const data = await fetchK8sTopology(namespace.value || undefined);
    topologyNodes.value = data.nodes;
    topologyEdges.value = data.edges;
    renderChart();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载拓扑数据失败";
  } finally {
    loading.value = false;
  }
}

function renderChart() {
  if (!chartEl.value) return;

  if (!chart) {
    chart = echarts.init(chartEl.value);
    chart.on("click", (params: { data?: unknown }) => {
      const data = params.data as { detailPath?: string } | undefined;
      if (data?.detailPath) {
        router.push(data.detailPath);
      }
    });
  }

  const categories = [
    { name: "Node" },
    { name: "Deployment" },
    { name: "Pod" },
    { name: "Service" },
    { name: "DaemonSet" },
    { name: "StatefulSet" },
  ];

  const nodes = topologyNodes.value.map((n) => {
    const catIndex = categories.findIndex((c) => c.name === n.kind);
    return {
      id: n.id,
      name: n.name,
      category: catIndex >= 0 ? catIndex : 0,
      symbolSize: n.kind === "Node" ? 40 : n.kind === "Pod" ? 20 : 30,
      itemStyle: { color: kindColors[n.kind] || "#909399" },
      detailPath: n.detail_path,
      label: {
        show: true,
        fontSize: 10,
        formatter: n.namespace ? `{name|${n.name}}\n{ns|${n.namespace}}` : "{name|" + n.name + "}",
        rich: {
          name: { fontSize: 10, lineHeight: 14 },
          ns: { fontSize: 8, color: "#999", lineHeight: 12 },
        },
      },
    };
  });

  const edges = topologyEdges.value.map((e) => {
    const style = edgeStyles[e.type] || { color: "#ccc", width: 1 };
    return {
      source: e.source,
      target: e.target,
      lineStyle: {
        color: style.color,
        width: style.width,
        type: style.type as "solid" | "dashed" | "dotted",
        curveness: 0.2,
      },
    };
  });

  chart.setOption({
    tooltip: {
      trigger: "item",
      formatter: (params: { dataType?: string; data?: { id?: string }; dataIndex?: number }) => {
        if (params.dataType === "node") {
          const n = topologyNodes.value.find((n) => n.id === params.data?.id);
          if (n) {
            return `<b>${n.kind}</b>: ${n.name}<br/>${n.namespace ? "Namespace: " + n.namespace + "<br/>" : ""}Status: ${n.status || "-"}`;
          }
        }
        if (params.dataType === "edge") {
          const e = topologyEdges.value[params.dataIndex ?? 0];
          if (e) return `${e.source} → ${e.target}<br/>Type: ${e.type}`;
        }
        return "";
      },
    },
    legend: {
      data: categories.map((c) => c.name),
      textStyle: { color: isDark.value ? "#ccc" : "#333" },
    },
    series: [
      {
        type: "graph",
        layout: "force",
        data: nodes,
        links: edges,
        categories,
        roam: true,
        draggable: true,
        force: {
          repulsion: 200,
          edgeLength: [80, 200],
          gravity: 0.1,
        },
        emphasis: {
          focus: "adjacency",
          lineStyle: { width: 3 },
        },
      },
    ],
  }, true);
}

function handleResize() {
  chart?.resize();
}

watch(isDark, () => {
  renderChart();
});

watch(namespace, () => {
  loadTopology();
});

onMounted(() => {
  loadTopology();
  window.addEventListener("resize", handleResize);
});

onBeforeUnmount(() => {
  window.removeEventListener("resize", handleResize);
  chart?.dispose();
  chart = null;
});
</script>

<template>
  <section class="topology-page">
    <PageHeader title="资源拓扑" />

    <div class="toolbar">
      <el-select
        v-model="namespace"
        placeholder="全部命名空间"
        clearable
        style="width: 200px"
      >
        <el-option
          label="全部命名空间"
          value=""
        />
        <el-option
          label="default"
          value="default"
        />
        <el-option
          label="kube-system"
          value="kube-system"
        />
        <el-option
          label="kube-public"
          value="kube-public"
        />
      </el-select>
      <el-button
        :loading="loading"
        type="primary"
        plain
        @click="loadTopology"
      >
        刷新
      </el-button>
    </div>

    <el-alert
      v-if="error"
      :title="error"
      type="error"
      show-icon
      :closable="false"
      style="margin-bottom: 16px"
    />

    <div
      ref="chartEl"
      v-loading="loading"
      class="chart-container"
    />

    <div class="legend-hint">
      <el-tag
        size="small"
        type="success"
      >
        Node
      </el-tag>
      <el-tag
        size="small"
        type="primary"
      >
        Deployment
      </el-tag>
      <el-tag
        size="small"
        type="warning"
      >
        Pod
      </el-tag>
      <el-tag
        size="small"
        type="danger"
      >
        Service
      </el-tag>
      <span class="edge-hint">— scheduled &nbsp;&nbsp;— owns &nbsp;&nbsp;┈ selects</span>
    </div>
  </section>
</template>

<style scoped>
.topology-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.toolbar {
  display: flex;
  gap: 12px;
  align-items: center;
}

.chart-container {
  width: 100%;
  height: 600px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
}

.legend-hint {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

.edge-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-left: 8px;
}
</style>
