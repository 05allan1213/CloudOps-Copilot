<script setup lang="ts">
import type { AlertEvent } from "../types";
import { formatTime } from "../utils/format";
import { severityTagType } from "../composables/useTagTypes";

type EventStatusFilter = "all" | "firing" | "resolved";
type SeverityFilter = "all" | "critical" | "warning" | "info";

defineProps<{
  events: AlertEvent[];
  selectedStatus: EventStatusFilter;
  selectedSeverity: SeverityFilter;
  error: string;
}>();

const emit = defineEmits<{
  statusChange: [value: EventStatusFilter];
  severityChange: [value: SeverityFilter];
}>();

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

function statusTagType(status: AlertEvent["status"]): "danger" | "success" | "" {
  return status === "resolved" ? "success" : "danger";
}

function statusLabel(status: AlertEvent["status"]): string {
  return status === "resolved" ? "恢复" : "触发";
}
</script>

<template>
  <el-card
    shadow="never"
    class="events-panel"
  >
    <template #header>
      <div class="panel-header">
        <div class="panel-title">
          <el-icon
            :size="18"
            color="#818cf8"
          >
            <TrendCharts />
          </el-icon>
          <span class="panel-title-text">最近事件</span>
          <el-tag
            size="small"
            effect="plain"
            class="event-badge"
          >
            Webhook 历史流
          </el-tag>
        </div>
        <div class="panel-actions">
          <el-radio-group
            :model-value="selectedStatus"
            size="small"
            @change="emit('statusChange', $event as EventStatusFilter)"
          >
            <el-radio-button value="all">
              全部状态
            </el-radio-button>
            <el-radio-button value="firing">
              触发
            </el-radio-button>
            <el-radio-button value="resolved">
              恢复
            </el-radio-button>
          </el-radio-group>
          <el-radio-group
            :model-value="selectedSeverity"
            size="small"
            @change="emit('severityChange', $event as SeverityFilter)"
          >
            <el-radio-button value="all">
              全部级别
            </el-radio-button>
            <el-radio-button value="critical">
              严重
            </el-radio-button>
            <el-radio-button value="warning">
              警告
            </el-radio-button>
            <el-radio-button value="info">
              提示
            </el-radio-button>
          </el-radio-group>
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
      v-else-if="events.length === 0"
      description="暂无最近事件"
    />

    <el-table
      v-else
      :data="events"
      stripe
      style="width: 100%"
      :row-key="(row: AlertEvent) => `${row.fingerprint}-${row.receivedAt}-${row.status}`"
    >
      <el-table-column
        label="级别"
        width="90"
        align="center"
      >
        <template #default="{ row }">
          <el-tag
            :type="severityTagType(row.labels.severity)"
            size="small"
            effect="dark"
          >
            {{ severityLabel(row.labels.severity) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        label="状态"
        width="90"
        align="center"
      >
        <template #default="{ row }">
          <el-tag
            :type="statusTagType(row.status)"
            size="small"
          >
            {{ statusLabel(row.status) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        label="告警名称"
        min-width="160"
        show-overflow-tooltip
      >
        <template #default="{ row }">
          {{ row.labels.alertname || "未知事件" }}
        </template>
      </el-table-column>
      <el-table-column
        label="实例"
        min-width="140"
        show-overflow-tooltip
      >
        <template #default="{ row }">
          <span class="mono-text">{{ row.labels.instance || "" }}</span>
        </template>
      </el-table-column>
      <el-table-column
        label="摘要"
        min-width="200"
        show-overflow-tooltip
      >
        <template #default="{ row }">
          {{ row.annotations.summary || row.annotations.description || "" }}
        </template>
      </el-table-column>
      <el-table-column
        label="接收时间"
        width="170"
      >
        <template #default="{ row }">
          {{ formatTime(row.receivedAt) }}
        </template>
      </el-table-column>
    </el-table>
  </el-card>
</template>

<style scoped>
.events-panel :deep(.el-card__body) {
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

.event-badge {
  color: #818cf8;
  background: rgba(129, 140, 248, 0.1);
  border-color: rgba(129, 140, 248, 0.25);
}

.panel-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.mono-text {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 13px;
}

@media (max-width: 768px) {
  .panel-header {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
