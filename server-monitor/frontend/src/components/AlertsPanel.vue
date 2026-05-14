<script setup lang="ts">
import { RouterLink } from "vue-router";
import { Refresh } from "@element-plus/icons-vue";

import type { AlertRecord } from "../types";
import { formatTime } from "../utils/format";

type SeverityFilter = "all" | "critical" | "warning" | "info";

defineProps<{
  alerts: AlertRecord[];
  selectedSeverity: SeverityFilter;
  refreshing: boolean;
  error: string;
}>();

const emit = defineEmits<{
  severityChange: [value: SeverityFilter];
  refresh: [];
  diagnose: [alert: AlertRecord];
}>();

function severityTagType(severity: string | undefined): "danger" | "warning" | "info" | "" {
  switch (severity ?? "info") {
    case "critical":
      return "danger";
    case "warning":
      return "warning";
    default:
      return "info";
  }
}

function severityLabel(severity: string | undefined): string {
  switch (severity ?? "info") {
    case "critical":
      return "严重";
    case "warning":
      return "警告";
    default:
      return "提示";
  }
}

function diagnosisTagType(status?: string): "info" | "success" | "danger" | "" {
  switch (status) {
    case "completed":
      return "success";
    case "failed":
      return "danger";
    default:
      return "info";
  }
}

function diagnosisLabel(status?: string): string {
  switch (status) {
    case "pending":
      return "诊断等待中";
    case "running":
      return "自动诊断中";
    case "completed":
      return "自动诊断完成";
    case "failed":
      return "自动诊断失败";
    case "skipped":
      return "已跳过诊断";
    default:
      return "";
  }
}
</script>

<template>
  <el-card shadow="never" class="alerts-panel">
    <template #header>
      <div class="panel-header">
        <div class="panel-title">
          <el-icon :size="18" color="var(--el-color-warning)"><Warning /></el-icon>
          <span class="panel-title-text">告警列表</span>
        </div>
        <div class="panel-actions">
          <el-radio-group
            :model-value="selectedSeverity"
            size="small"
            @change="emit('severityChange', $event as SeverityFilter)"
          >
            <el-radio-button value="all">全部</el-radio-button>
            <el-radio-button value="critical">严重</el-radio-button>
            <el-radio-button value="warning">警告</el-radio-button>
            <el-radio-button value="info">提示</el-radio-button>
          </el-radio-group>
          <el-button
            :icon="Refresh"
            :loading="refreshing"
            size="small"
            @click="emit('refresh')"
          >
            刷新
          </el-button>
        </div>
      </div>
    </template>

    <el-alert
      v-if="error"
      :title="error"
      type="error"
      show-icon
      closable
      style="margin-bottom: 16px"
    />

    <el-empty
      v-else-if="alerts.length === 0"
      description="一切正常"
      image-size="48"
    >
      <template #image>
        <el-icon :size="48" color="var(--el-color-success)"><CircleCheckFilled /></el-icon>
      </template>
    </el-empty>

    <el-table
      v-else
      :data="alerts"
      stripe
      style="width: 100%"
      row-key="fingerprint"
    >
      <el-table-column label="级别" width="90" align="center">
        <template #default="{ row }">
          <el-tag :type="severityTagType(row.labels.severity)" size="small" effect="dark">
            {{ severityLabel(row.labels.severity) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="告警名称" min-width="160" prop="labels.alertname" show-overflow-tooltip>
        <template #default="{ row }">
          {{ row.labels.alertname || "未知告警" }}
        </template>
      </el-table-column>
      <el-table-column label="实例" min-width="140" show-overflow-tooltip>
        <template #default="{ row }">
          <span class="mono-text">{{ row.labels.instance || "" }}</span>
        </template>
      </el-table-column>
      <el-table-column label="摘要" min-width="200" show-overflow-tooltip>
        <template #default="{ row }">
          {{ row.annotations.summary || row.annotations.description || "" }}
        </template>
      </el-table-column>
      <el-table-column label="诊断状态" width="130" align="center">
        <template #default="{ row }">
          <el-tag
            v-if="row.diagnosisStatus"
            :type="diagnosisTagType(row.diagnosisStatus)"
            size="small"
          >
            {{ diagnosisLabel(row.diagnosisStatus) }}
          </el-tag>
          <span v-else class="text-muted">-</span>
        </template>
      </el-table-column>
      <el-table-column label="触发时间" width="170">
        <template #default="{ row }">
          {{ formatTime(row.startsAt) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="180" align="center">
        <template #default="{ row }">
          <el-button
            v-if="row.diagnosisReportId"
            type="primary"
            link
            size="small"
          >
            <RouterLink :to="`/diagnosis/${row.diagnosisReportId}`" class="link-text">
              查看诊断
            </RouterLink>
          </el-button>
          <el-button
            type="primary"
            link
            size="small"
            @click="emit('diagnose', row)"
          >
            {{ row.diagnosisStatus === "failed" ? "手动重试" : "生成诊断" }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>
  </el-card>
</template>

<style scoped>
.alerts-panel :deep(.el-card__body) {
  padding: 16px 20px;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}

.panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.panel-title-text {
  font-size: 15px;
  font-weight: 600;
}

.panel-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.mono-text {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.82rem;
}

.text-muted {
  color: var(--el-text-color-placeholder);
  font-size: 0.82rem;
}

.link-text {
  color: inherit;
  text-decoration: none;
}

@media (max-width: 768px) {
  .panel-header {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
