<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import {
  Monitor,
  Box,
  Grid,
  InfoFilled,
} from "@element-plus/icons-vue";

import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import EventTable from "../components/k8s/EventTable.vue";
import { fetchK8sOverview } from "../api/k8s";
import type { K8sClusterOverview } from "../types/k8s";

const overview = ref<K8sClusterOverview | null>(null);
const loading = ref(true);
const errorText = ref("");

const pageState = computed<"loading" | "error" | "default">(() => {
  if (loading.value) return "loading";
  if (errorText.value) return "error";
  return "default";
});

const subtitle = computed(() => {
  if (!overview.value?.collected_at) return "";
  return `采集于 ${new Date(overview.value.collected_at).toLocaleString()}`;
});

const nodeHealthRatio = computed(() => {
  if (!overview.value?.nodes_available) return 0;
  if (overview.value.nodes.total === 0) return 1;
  return overview.value.nodes.ready / overview.value.nodes.total;
});

const podHealthRatio = computed(() => {
  if (!overview.value) return 0;
  if (overview.value.pods.total === 0) return 1;
  return overview.value.pods.running / overview.value.pods.total;
});

const deploymentHealthRatio = computed(() => {
  if (!overview.value) return 0;
  if (overview.value.deployments.total === 0) return 1;
  return overview.value.deployments.available / overview.value.deployments.total;
});

async function loadOverview() {
  try {
    loading.value = true;
    errorText.value = "";
    overview.value = await fetchK8sOverview();
  } catch (err) {
    errorText.value = err instanceof Error ? err.message : "加载 K8s 集群概览失败";
  } finally {
    loading.value = false;
  }
}

onMounted(loadOverview);
</script>

<template>
  <PageHeader
    title="K8s 集群"
    :subtitle="subtitle"
  />

  <StateWrapper
    :state="pageState"
    :error-text="errorText"
  >
    <template #retry>
      <el-button
        type="primary"
        @click="loadOverview"
      >
        重试
      </el-button>
    </template>

    <el-alert
      v-if="overview?.truncated"
      title="数据量较大，部分结果已被截断"
      type="warning"
      show-icon
      closable
      style="margin-bottom: 16px"
    />

    <div class="overview-grid">
      <div
        v-if="overview?.nodes_available"
        class="health-section"
      >
        <div class="health-header">
          <div class="health-title-group">
            <el-icon
              class="health-icon"
              :size="20"
            >
              <Monitor />
            </el-icon>
            <span class="health-title">节点</span>
          </div>
          <el-tag
            :type="nodeHealthRatio >= 1 ? 'success' : nodeHealthRatio >= 0.5 ? 'warning' : 'danger'"
            size="small"
            round
          >
            {{ overview.nodes.ready }}/{{ overview.nodes.total }}
          </el-tag>
        </div>
        <div class="health-bar">
          <div
            class="health-bar-fill health-bar--success"
            :style="{ width: `${nodeHealthRatio * 100}%` }"
          />
        </div>
        <div class="health-stats">
          <div class="health-stat">
            <span class="health-stat-value">{{ overview.nodes.total }}</span>
            <span class="health-stat-label">总数</span>
          </div>
          <div class="health-stat">
            <span class="health-stat-value text-success">{{ overview.nodes.ready }}</span>
            <span class="health-stat-label">就绪</span>
          </div>
          <div class="health-stat">
            <span class="health-stat-value text-danger">{{ overview.nodes.not_ready }}</span>
            <span class="health-stat-label">未就绪</span>
          </div>
        </div>
      </div>

      <div class="health-section">
        <div class="health-header">
          <div class="health-title-group">
            <el-icon
              class="health-icon"
              :size="20"
            >
              <Box />
            </el-icon>
            <span class="health-title">Pod</span>
          </div>
          <el-tag
            :type="podHealthRatio >= 1 ? 'success' : podHealthRatio >= 0.8 ? 'warning' : 'danger'"
            size="small"
            round
          >
            {{ overview?.pods.running ?? 0 }}/{{ overview?.pods.total ?? 0 }}
          </el-tag>
        </div>
        <div class="health-bar">
          <div
            class="health-bar-fill health-bar--success"
            :style="{ width: `${podHealthRatio * 100}%` }"
          />
        </div>
        <div class="health-stats">
          <div class="health-stat">
            <span class="health-stat-value">{{ overview?.pods.total ?? 0 }}</span>
            <span class="health-stat-label">总数</span>
          </div>
          <div class="health-stat">
            <span class="health-stat-value text-success">{{ overview?.pods.running ?? 0 }}</span>
            <span class="health-stat-label">运行中</span>
          </div>
          <div class="health-stat">
            <span class="health-stat-value text-warning">{{ overview?.pods.pending ?? 0 }}</span>
            <span class="health-stat-label">等待中</span>
          </div>
          <div class="health-stat">
            <span class="health-stat-value text-danger">{{ overview?.pods.failed ?? 0 }}</span>
            <span class="health-stat-label">失败</span>
          </div>
        </div>
      </div>

      <div class="health-section">
        <div class="health-header">
          <div class="health-title-group">
            <el-icon
              class="health-icon"
              :size="20"
            >
              <Grid />
            </el-icon>
            <span class="health-title">Deployment</span>
          </div>
          <el-tag
            :type="deploymentHealthRatio >= 1 ? 'success' : deploymentHealthRatio >= 0.8 ? 'warning' : 'danger'"
            size="small"
            round
          >
            {{ overview?.deployments.available ?? 0 }}/{{ overview?.deployments.total ?? 0 }}
          </el-tag>
        </div>
        <div class="health-bar">
          <div
            class="health-bar-fill health-bar--success"
            :style="{ width: `${deploymentHealthRatio * 100}%` }"
          />
        </div>
        <div class="health-stats">
          <div class="health-stat">
            <span class="health-stat-value">{{ overview?.deployments.total ?? 0 }}</span>
            <span class="health-stat-label">总数</span>
          </div>
          <div class="health-stat">
            <span class="health-stat-value text-success">{{ overview?.deployments.available ?? 0 }}</span>
            <span class="health-stat-label">可用</span>
          </div>
          <div class="health-stat">
            <span class="health-stat-value text-warning">{{ overview?.deployments.unavailable ?? 0 }}</span>
            <span class="health-stat-label">不可用</span>
          </div>
        </div>
      </div>
    </div>

    <el-card
      v-if="overview?.nodes_available"
      shadow="never"
      class="info-card"
    >
      <template #header>
        <div class="section-header">
          <div class="section-title">
            <el-icon
              :size="18"
              color="var(--el-color-info)"
            >
              <InfoFilled />
            </el-icon>
            <span>主机覆盖</span>
          </div>
        </div>
      </template>
      <el-descriptions
        :column="3"
        border
      >
        <el-descriptions-item label="节点总数">
          {{ overview.host_coverage.total_nodes }}
        </el-descriptions-item>
        <el-descriptions-item label="已覆盖">
          <el-tag
            type="success"
            size="small"
          >
            {{ overview.host_coverage.covered_nodes }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="未覆盖">
          <el-tag
            v-if="overview.host_coverage.uncovered_nodes > 0"
            type="danger"
            size="small"
          >
            {{ overview.host_coverage.uncovered_nodes }}
          </el-tag>
          <el-tag
            v-else
            type="success"
            size="small"
          >
            0
          </el-tag>
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <el-card
      shadow="never"
      class="info-card"
    >
      <template #header>
        <div class="section-header">
          <div class="section-title">
            <el-icon
              :size="18"
              color="var(--el-color-info)"
            >
              <InfoFilled />
            </el-icon>
            <span>最近事件</span>
          </div>
        </div>
      </template>
      <EventTable :events="overview?.recent_events ?? []" />
    </el-card>
  </StateWrapper>
</template>

<style scoped>
.overview-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 16px;
  margin-bottom: 24px;
}

.health-section {
  background: var(--cloudops-bg-card);
  border: 1px solid var(--cloudops-border-color);
  border-radius: var(--cloudops-radius-md);
  padding: 20px;
  transition: border-color 0.2s;
}

.health-section:hover {
  border-color: var(--cloudops-border-hover);
}

.health-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 14px;
}

.health-title-group {
  display: flex;
  align-items: center;
  gap: 8px;
}

.health-icon {
  color: var(--cloudops-accent);
}

.health-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--cloudops-text-primary);
}

.health-bar {
  height: 6px;
  background: var(--el-fill-color);
  border-radius: 3px;
  overflow: hidden;
  margin-bottom: 16px;
}

.health-bar-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.4s ease;
}

.health-bar--success {
  background: var(--cloudops-success);
}

.health-stats {
  display: flex;
  gap: 24px;
}

.health-stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.health-stat-value {
  font-size: 20px;
  font-weight: 700;
  color: var(--cloudops-text-primary);
  font-variant-numeric: tabular-nums;
}

.health-stat-label {
  font-size: 12px;
  color: var(--cloudops-text-muted);
  font-weight: 500;
  letter-spacing: 0.02em;
}

.text-success {
  color: var(--cloudops-success);
}

.text-warning {
  color: var(--cloudops-warning);
}

.text-danger {
  color: var(--cloudops-danger);
}

.info-card {
  margin-bottom: 24px;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 600;
}

@media (max-width: 768px) {
  .overview-grid {
    grid-template-columns: 1fr;
  }

  .health-stats {
    gap: 16px;
  }
}
</style>
