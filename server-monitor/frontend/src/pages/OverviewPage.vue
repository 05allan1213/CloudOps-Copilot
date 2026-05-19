<script setup lang="ts">
import { computed } from "vue";
import { TrendCharts } from "@element-plus/icons-vue";

import HostResourceChart from "../components/HostResourceChart.vue";
import StatsRow from "../components/StatsRow.vue";
import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import { useMonitorStore } from "../stores/monitor";

const monitor = useMonitorStore();

const pageState = computed<"loading" | "error" | "default">(() => {
  if (monitor.loading) return "loading";
  if (monitor.hostsError || monitor.alertsError) return "error";
  return "default";
});

const errorText = computed(() =>
  [monitor.hostsError, monitor.alertsError, monitor.alertEventsError].filter(Boolean).join("; ") || "加载失败",
);
</script>

<template>
  <PageHeader
    title="总览"
    subtitle="刚刚更新"
  />

  <StateWrapper
    :state="pageState"
    :error-text="errorText"
  >
    <template #retry>
      <el-button
        type="primary"
        @click="monitor.refreshAll()"
      >
        重试
      </el-button>
    </template>

    <StatsRow
      :host-count="monitor.hosts.length"
      :host-count-label="monitor.hostCountLabel"
      :high-cpu-host-count="monitor.highCPUHostCount"
      :high-memory-host-count="monitor.highMemoryHostCount"
      :both-risk-host-count="monitor.bothRiskHostCount"
      :active-alert-count="monitor.alerts.length"
      :alert-event-count="monitor.alertEvents.length"
      :critical-count="monitor.criticalCount"
      :warning-count="monitor.warningCount"
      :info-count="monitor.infoCount"
    />

    <el-card
      shadow="never"
      class="chart-panel"
    >
      <template #header>
        <div class="chart-panel-header">
          <div class="chart-panel-title">
            <el-icon
              :size="18"
              color="var(--el-color-info)"
            >
              <TrendCharts />
            </el-icon>
            <span>资源分布</span>
          </div>
          <el-tag
            size="small"
            type="info"
          >
            ECharts
          </el-tag>
        </div>
      </template>
      <HostResourceChart :hosts="monitor.hosts" />
    </el-card>
  </StateWrapper>
</template>

<style scoped>
.chart-panel :deep(.el-card__body) {
  padding: 0 20px 20px;
}

.chart-panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.chart-panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 600;
}
</style>
