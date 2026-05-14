<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Refresh } from "@element-plus/icons-vue";

import { fetchDashboardOverview } from "../api/hosts";
import { fetchHealthz, fetchReadyz } from "../api/status";
import { formatTime } from "../utils/format";
import type {
  ApiResponse,
  DashboardOverview,
  HealthStatus,
  ReadyStatus,
} from "../types";
import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";

const loading = ref(true);
const health = ref<ApiResponse<HealthStatus> | null>(null);
const ready = ref<ApiResponse<ReadyStatus> | null>(null);
const overview = ref<DashboardOverview | null>(null);
const error = ref("");

const serviceReady = computed(() => ready.value?.data?.ready === true);
const serviceHealthy = computed(() => health.value?.data?.healthy === true);
const dependencies = computed(() => ready.value?.data?.dependencies ?? {});

const stateKey = computed(() => {
  if (loading.value) return "loading";
  return "default";
});

onMounted(() => {
  loadStatus();
});

async function loadStatus() {
  loading.value = true;
  error.value = "";
  try {
    const [healthResult, readyResult, overviewResult] = await Promise.allSettled([
      fetchHealthz(),
      fetchReadyz(),
      fetchDashboardOverview(),
    ]);

    if (healthResult.status === "fulfilled") {
      health.value = healthResult.value;
    }
    if (readyResult.status === "fulfilled") {
      ready.value = readyResult.value;
    }
    if (overviewResult.status === "fulfilled") {
      overview.value = overviewResult.value;
    }

    const failedNames: string[] = [];
    if (healthResult.status === "rejected") failedNames.push("健康检查");
    if (readyResult.status === "rejected") failedNames.push("就绪检查");
    if (overviewResult.status === "rejected") failedNames.push("监控概览");
    if (failedNames.length > 0) {
      error.value = `以下接口暂时不可用: ${failedNames.join("、")}`;
    }
  } finally {
    loading.value = false;
  }
}

function depTagType(value: string | undefined) {
  switch (value) {
    case "ok":
      return "success";
    case "disabled":
      return "info";
    case "unreachable":
      return "danger";
    default:
      return "info";
  }
}

function depLabel(value: string | undefined): string {
  switch (value) {
    case "ok":
      return "正常";
    case "disabled":
      return "未启用";
    case "unreachable":
      return "不可达";
    default:
      return "--";
  }
}

function formatPercent(value: number | undefined): string {
  return value === undefined ? "--" : `${value.toFixed(1)}%`;
}
</script>

<template>
  <section class="status-page">
    <PageHeader title="系统状态" subtitle="服务健康、依赖就绪与监控概览">
      <el-button :icon="Refresh" :loading="loading" @click="loadStatus">刷新</el-button>
    </PageHeader>

    <el-alert
      v-if="error"
      :title="error"
      type="warning"
      show-icon
      closable
      style="margin-bottom: 16px"
    />

    <StateWrapper :state="stateKey" empty-text="暂无状态数据">
      <template #retry>
        <el-button type="primary" @click="loadStatus">重试</el-button>
      </template>

      <el-row :gutter="16" class="status-row">
        <el-col :xs="12" :sm="6">
          <el-card shadow="hover" class="status-card">
            <div class="status-card-inner">
              <span class="status-label">健康检查</span>
              <el-tag :type="serviceHealthy ? 'success' : 'danger'" size="large" effect="dark">
                {{ serviceHealthy ? "正常" : "异常" }}
              </el-tag>
            </div>
          </el-card>
        </el-col>
        <el-col :xs="12" :sm="6">
          <el-card shadow="hover" class="status-card">
            <div class="status-card-inner">
              <span class="status-label">就绪检查</span>
              <el-tag :type="serviceReady ? 'success' : 'danger'" size="large" effect="dark">
                {{ serviceReady ? "正常" : "异常" }}
              </el-tag>
            </div>
          </el-card>
        </el-col>
        <el-col :xs="12" :sm="6">
          <el-card shadow="hover" class="status-card">
            <div class="status-card-inner">
              <span class="status-label">Prometheus</span>
              <el-tag :type="depTagType(dependencies.prometheus)" size="large" effect="dark">
                {{ depLabel(dependencies.prometheus) }}
              </el-tag>
            </div>
          </el-card>
        </el-col>
        <el-col :xs="12" :sm="6">
          <el-card shadow="hover" class="status-card">
            <div class="status-card-inner">
              <span class="status-label">Redis</span>
              <el-tag :type="depTagType(dependencies.redis)" size="large" effect="dark">
                {{ depLabel(dependencies.redis) }}
              </el-tag>
            </div>
          </el-card>
        </el-col>
      </el-row>

      <el-card shadow="never" class="overview-card">
        <template #header>
          <div class="overview-header">
            <span class="overview-title">监控概览</span>
            <el-tag size="small" type="info" effect="plain">
              {{ loading ? "更新中" : formatTime(overview?.generated_at) }}
            </el-tag>
          </div>
        </template>
        <el-row :gutter="16">
          <el-col :xs="12" :sm="8" :md="4">
            <el-statistic title="主机总数" :value="overview?.total_hosts ?? '--'" />
          </el-col>
          <el-col :xs="12" :sm="8" :md="4">
            <el-statistic title="健康主机" :value="overview?.healthy_hosts ?? '--'" />
          </el-col>
          <el-col :xs="12" :sm="8" :md="4">
            <el-statistic title="离线主机" :value="overview?.down_hosts ?? '--'" />
          </el-col>
          <el-col :xs="12" :sm="8" :md="4">
            <el-statistic title="活跃告警" :value="overview?.active_alerts ?? '--'" />
          </el-col>
          <el-col :xs="12" :sm="8" :md="4">
            <el-statistic title="平均 CPU" :value="formatPercent(overview?.avg_cpu)" />
          </el-col>
          <el-col :xs="12" :sm="8" :md="4">
            <el-statistic title="平均内存" :value="formatPercent(overview?.avg_memory)" />
          </el-col>
        </el-row>
      </el-card>
    </StateWrapper>
  </section>
</template>

<style scoped>
.status-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.status-row {
  margin-bottom: 0;
}

.status-card {
  height: 100%;
}

.status-card :deep(.el-card__body) {
  padding: 20px;
}

.status-card-inner {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
}

.status-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
}

.overview-card :deep(.el-card__body) {
  padding: 20px;
}

.overview-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.overview-title {
  font-size: 15px;
  font-weight: 600;
}

@media (max-width: 768px) {
  .status-card-inner {
    flex-direction: row;
    justify-content: space-between;
  }
}
</style>
