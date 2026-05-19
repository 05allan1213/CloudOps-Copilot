<script setup lang="ts">
import { Monitor, Cpu, Memo, WarningFilled, Bell, CircleCloseFilled, Warning, InfoFilled } from "@element-plus/icons-vue";

defineProps<{
  hostCount: number;
  hostCountLabel: string;
  highCpuHostCount: number;
  highMemoryHostCount: number;
  bothRiskHostCount: number;
  activeAlertCount: number;
  alertEventCount: number;
  criticalCount: number;
  warningCount: number;
  infoCount: number;
}>();
</script>

<template>
  <div class="stats-row">
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
              <Monitor />
            </el-icon>
            <el-statistic
              :value="hostCount"
              class="stat-value"
            >
              <template #title>
                <span class="stat-label">{{ hostCountLabel }}</span>
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
              <Cpu />
            </el-icon>
            <el-statistic
              :value="highCpuHostCount"
              class="stat-value stat-value--warning"
            >
              <template #title>
                <span class="stat-label">高 CPU</span>
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
              class="stat-icon stat-icon--info"
              :size="28"
            >
              <Memo />
            </el-icon>
            <el-statistic
              :value="highMemoryHostCount"
              class="stat-value stat-value--info"
            >
              <template #title>
                <span class="stat-label">高内存</span>
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
              <WarningFilled />
            </el-icon>
            <el-statistic
              :value="bothRiskHostCount"
              class="stat-value stat-value--danger"
            >
              <template #title>
                <span class="stat-label">双高风险</span>
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
              <Bell />
            </el-icon>
            <el-statistic
              :value="activeAlertCount"
              class="stat-value"
            >
              <template #title>
                <span class="stat-label">活跃告警</span>
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
              :value="criticalCount"
              class="stat-value stat-value--danger"
            >
              <template #title>
                <span class="stat-label">严重</span>
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
              :value="warningCount"
              class="stat-value stat-value--warning"
            >
              <template #title>
                <span class="stat-label">警告</span>
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
              class="stat-icon stat-icon--info"
              :size="28"
            >
              <InfoFilled />
            </el-icon>
            <el-statistic
              :value="infoCount"
              class="stat-value stat-value--info"
            >
              <template #title>
                <span class="stat-label">提示</span>
              </template>
            </el-statistic>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
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
</style>
