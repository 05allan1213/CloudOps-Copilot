<script setup lang="ts">
import { computed, onMounted, ref } from "vue";

import { fetchDashboardOverview } from "../api/hosts";
import { fetchHealthz, fetchReadyz } from "../api/status";
import type {
  ApiResponse,
  DashboardOverview,
  HealthStatus,
  ReadyStatus,
} from "../types";

const loading = ref(true);
const health = ref<ApiResponse<HealthStatus> | null>(null);
const ready = ref<ApiResponse<ReadyStatus> | null>(null);
const overview = ref<DashboardOverview | null>(null);
const error = ref("");

const serviceReady = computed(() => ready.value?.data?.ready === true);
const serviceHealthy = computed(() => health.value?.data?.healthy === true);
const dependencies = computed(() => ready.value?.data?.dependencies ?? {});

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

    const failed = [healthResult, readyResult, overviewResult].filter(
      (result) => result.status === "rejected",
    );
    if (failed.length > 0) {
      const failedNames: string[] = [];
      if (healthResult.status === "rejected") failedNames.push("健康检查");
      if (readyResult.status === "rejected") failedNames.push("就绪检查");
      if (overviewResult.status === "rejected") failedNames.push("监控概览");
      error.value = `以下接口暂时不可用: ${failedNames.join("、")}`;
    }
  } finally {
    loading.value = false;
  }
}

function statusLabel(value: boolean): string {
  return value ? "正常" : "异常";
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

function formatTime(value: string | undefined): string {
  if (!value) {
    return "--";
  }
  return new Date(value).toLocaleString("zh-CN");
}
</script>

<template>
  <section class="status-header">
    <div>
      <h2>系统状态</h2>
      <p>服务健康、依赖就绪与监控概览</p>
    </div>
    <button type="button" class="refresh-btn" :disabled="loading" @click="loadStatus">
      刷新
    </button>
  </section>

  <div v-if="error" class="status-message">{{ error }}</div>

  <section class="status-grid">
    <div class="status-card">
      <span>健康检查</span>
      <strong :class="serviceHealthy ? 'ok' : 'bad'">
        {{ statusLabel(serviceHealthy) }}
      </strong>
    </div>
    <div class="status-card">
      <span>就绪检查</span>
      <strong :class="serviceReady ? 'ok' : 'bad'">
        {{ statusLabel(serviceReady) }}
      </strong>
    </div>
    <div class="status-card">
      <span>Prometheus</span>
      <strong :class="dependencies.prometheus === 'ok' ? 'ok' : 'bad'">
        {{ depLabel(dependencies.prometheus) }}
      </strong>
    </div>
    <div class="status-card">
      <span>Redis</span>
      <strong :class="dependencies.redis === 'ok' ? 'ok' : 'muted'">
        {{ depLabel(dependencies.redis) }}
      </strong>
    </div>
  </section>

  <section class="panel">
    <div class="panel-header">
      <div class="panel-title">
        <h2>监控概览</h2>
      </div>
      <span class="panel-badge">
        {{ loading ? "更新中" : formatTime(overview?.generated_at) }}
      </span>
    </div>
    <div class="overview-grid">
      <div class="overview-item">
        <span>主机总数</span>
        <strong>{{ overview?.total_hosts ?? "--" }}</strong>
      </div>
      <div class="overview-item">
        <span>健康主机</span>
        <strong>{{ overview?.healthy_hosts ?? "--" }}</strong>
      </div>
      <div class="overview-item">
        <span>离线主机</span>
        <strong>{{ overview?.down_hosts ?? "--" }}</strong>
      </div>
      <div class="overview-item">
        <span>活跃告警</span>
        <strong>{{ overview?.active_alerts ?? "--" }}</strong>
      </div>
      <div class="overview-item">
        <span>平均 CPU</span>
        <strong>{{ formatPercent(overview?.avg_cpu) }}</strong>
      </div>
      <div class="overview-item">
        <span>平均内存</span>
        <strong>{{ formatPercent(overview?.avg_memory) }}</strong>
      </div>
    </div>
  </section>
</template>

<style scoped>
.status-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1rem;
}

.status-header h2 {
  margin: 0;
  font-size: 1.2rem;
}

.status-header p {
  margin-top: 0.35rem;
  color: var(--text-muted);
  font-size: 0.82rem;
}

.refresh-btn {
  color: var(--accent);
  background: var(--accent-soft);
  border-radius: var(--radius-sm);
  padding: 0.45rem 0.8rem;
  font-size: 0.78rem;
  font-weight: 700;
}

.refresh-btn:disabled {
  color: var(--text-muted);
  cursor: default;
}

.status-message {
  margin-bottom: 1rem;
  color: var(--warning);
  background: var(--warning-soft);
  border: 1px solid rgba(245, 158, 11, 0.24);
  border-radius: var(--radius-md);
  padding: 0.75rem 1rem;
  font-size: 0.82rem;
}

.status-grid,
.overview-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 0.75rem;
}

.status-grid {
  margin-bottom: 1rem;
}

.status-card,
.overview-item {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 0.9rem;
}

.status-card span,
.overview-item span {
  display: block;
  color: var(--text-muted);
  font-size: 0.72rem;
  font-weight: 700;
  margin-bottom: 0.45rem;
}

.status-card strong,
.overview-item strong {
  font-size: 1.05rem;
  font-variant-numeric: tabular-nums;
}

.ok {
  color: var(--success);
}

.bad {
  color: var(--danger);
}

.muted {
  color: var(--text-secondary);
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
  gap: 1rem;
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

@media (max-width: 768px) {
  .status-header,
  .panel-header {
    flex-direction: column;
  }
}
</style>
