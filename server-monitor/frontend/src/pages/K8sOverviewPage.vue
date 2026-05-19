<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import {
  Monitor,
  CircleCheck,
  CircleClose,
  Box,
  Select,
  Warning,
  CircleCloseFilled,
  WarningFilled,
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

    <div class="stats-row">
      <el-row
        v-if="overview?.nodes_available"
        :gutter="16"
        class="stats-section"
      >
        <el-col
          :xs="12"
          :sm="8"
        >
          <el-card
            shadow="hover"
            class="stat-card"
          >
            <div class="stat-inner">
              <el-icon
                class="stat-icon stat-icon--primary"
                :size="28"
              >
                <Monitor />
              </el-icon>
              <el-statistic
                :value="overview.nodes.total"
                class="stat-value"
              >
                <template #title>
                  <span class="stat-label">Node 总数</span>
                </template>
              </el-statistic>
            </div>
          </el-card>
        </el-col>
        <el-col
          :xs="12"
          :sm="8"
        >
          <el-card
            shadow="hover"
            class="stat-card"
          >
            <div class="stat-inner">
              <el-icon
                class="stat-icon stat-icon--success"
                :size="28"
              >
                <CircleCheck />
              </el-icon>
              <el-statistic
                :value="overview.nodes.ready"
                class="stat-value stat-value--success"
              >
                <template #title>
                  <span class="stat-label">Ready</span>
                </template>
              </el-statistic>
            </div>
          </el-card>
        </el-col>
        <el-col
          :xs="12"
          :sm="8"
        >
          <el-card
            shadow="hover"
            class="stat-card"
          >
            <div class="stat-inner">
              <el-icon
                class="stat-icon stat-icon--danger"
                :size="28"
              >
                <CircleClose />
              </el-icon>
              <el-statistic
                :value="overview.nodes.not_ready"
                class="stat-value stat-value--danger"
              >
                <template #title>
                  <span class="stat-label">NotReady</span>
                </template>
              </el-statistic>
            </div>
          </el-card>
        </el-col>
      </el-row>

      <el-row
        :gutter="16"
        class="stats-section"
      >
        <el-col
          :xs="12"
          :sm="6"
        >
          <el-card
            shadow="hover"
            class="stat-card"
          >
            <div class="stat-inner">
              <el-icon
                class="stat-icon stat-icon--primary"
                :size="28"
              >
                <Box />
              </el-icon>
              <el-statistic
                :value="overview?.pods.total ?? 0"
                class="stat-value"
              >
                <template #title>
                  <span class="stat-label">Pod 总数</span>
                </template>
              </el-statistic>
            </div>
          </el-card>
        </el-col>
        <el-col
          :xs="12"
          :sm="6"
        >
          <el-card
            shadow="hover"
            class="stat-card"
          >
            <div class="stat-inner">
              <el-icon
                class="stat-icon stat-icon--success"
                :size="28"
              >
                <Select />
              </el-icon>
              <el-statistic
                :value="overview?.pods.running ?? 0"
                class="stat-value stat-value--success"
              >
                <template #title>
                  <span class="stat-label">Running</span>
                </template>
              </el-statistic>
            </div>
          </el-card>
        </el-col>
        <el-col
          :xs="12"
          :sm="6"
        >
          <el-card
            shadow="hover"
            class="stat-card"
          >
            <div class="stat-inner">
              <el-icon
                class="stat-icon stat-icon--warning"
                :size="28"
              >
                <Warning />
              </el-icon>
              <el-statistic
                :value="overview?.pods.pending ?? 0"
                class="stat-value stat-value--warning"
              >
                <template #title>
                  <span class="stat-label">Pending</span>
                </template>
              </el-statistic>
            </div>
          </el-card>
        </el-col>
        <el-col
          :xs="12"
          :sm="6"
        >
          <el-card
            shadow="hover"
            class="stat-card"
          >
            <div class="stat-inner">
              <el-icon
                class="stat-icon stat-icon--danger"
                :size="28"
              >
                <CircleCloseFilled />
              </el-icon>
              <el-statistic
                :value="overview?.pods.failed ?? 0"
                class="stat-value stat-value--danger"
              >
                <template #title>
                  <span class="stat-label">Failed</span>
                </template>
              </el-statistic>
            </div>
          </el-card>
        </el-col>
      </el-row>

      <el-row
        :gutter="16"
        class="stats-section"
      >
        <el-col
          :xs="12"
          :sm="8"
        >
          <el-card
            shadow="hover"
            class="stat-card"
          >
            <div class="stat-inner">
              <el-icon
                class="stat-icon stat-icon--primary"
                :size="28"
              >
                <Grid />
              </el-icon>
              <el-statistic
                :value="overview?.deployments.total ?? 0"
                class="stat-value"
              >
                <template #title>
                  <span class="stat-label">Deployment 总数</span>
                </template>
              </el-statistic>
            </div>
          </el-card>
        </el-col>
        <el-col
          :xs="12"
          :sm="8"
        >
          <el-card
            shadow="hover"
            class="stat-card"
          >
            <div class="stat-inner">
              <el-icon
                class="stat-icon stat-icon--success"
                :size="28"
              >
                <Select />
              </el-icon>
              <el-statistic
                :value="overview?.deployments.available ?? 0"
                class="stat-value stat-value--success"
              >
                <template #title>
                  <span class="stat-label">Available</span>
                </template>
              </el-statistic>
            </div>
          </el-card>
        </el-col>
        <el-col
          :xs="12"
          :sm="8"
        >
          <el-card
            shadow="hover"
            class="stat-card"
          >
            <div class="stat-inner">
              <el-icon
                class="stat-icon stat-icon--warning"
                :size="28"
              >
                <WarningFilled />
              </el-icon>
              <el-statistic
                :value="overview?.deployments.unavailable ?? 0"
                class="stat-value stat-value--warning"
              >
                <template #title>
                  <span class="stat-label">Unavailable</span>
                </template>
              </el-statistic>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </div>

    <el-card
      v-if="overview?.nodes_available"
      shadow="never"
      style="margin-bottom: 24px"
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
        <el-descriptions-item label="Node 总数">
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
            {{ overview.host_coverage.uncovered_nodes }}
          </el-tag>
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <el-card shadow="never">
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
.stats-row {
  margin-bottom: 24px;
}

.stats-section {
  margin-bottom: 12px;
}

.stats-section:last-child {
  margin-bottom: 0;
}

.stat-card {
  height: 100%;
}

.stat-card :deep(.el-card__body) {
  padding: 20px;
}

.stat-inner {
  display: flex;
  align-items: center;
  gap: 14px;
}

.stat-value :deep(.el-statistic__number) {
  font-size: 24px;
  font-weight: 700;
}

.stat-value--success :deep(.el-statistic__number) {
  color: var(--el-color-success);
}

.stat-value--warning :deep(.el-statistic__number) {
  color: var(--el-color-warning);
}

.stat-value--danger :deep(.el-statistic__number) {
  color: var(--el-color-danger);
}

.stat-value--info :deep(.el-statistic__number) {
  color: var(--el-color-info);
}

.stat-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
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
</style>
