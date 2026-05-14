<script setup lang="ts">
import { computed } from "vue";
import { useRouter } from "vue-router";

import type { Host } from "../types";
import { formatTime } from "../utils/format";

const props = defineProps<{
  host: Host;
}>();

const router = useRouter();

const detailPath = computed(
  () => `/hosts/${encodeURIComponent(props.host.instance)}`,
);

function cpuColor(value: number): string {
  if (value >= 80) return "#ef4444";
  if (value > 60) return "#f59e0b";
  return "#22c55e";
}

function memoryColor(value: number): string {
  if (value >= 85) return "#ef4444";
  if (value > 70) return "#f59e0b";
  return "#22c55e";
}

function isHighCPU(host: Host): boolean {
  return host.cpu >= 80;
}

function isHighMemory(host: Host): boolean {
  return host.memory >= 85;
}

function hostRiskVariant(host: Host): "normal" | "cpu" | "memory" | "both" {
  const highCPU = isHighCPU(host);
  const highMemory = isHighMemory(host);
  if (highCPU && highMemory) return "both";
  if (highCPU) return "cpu";
  if (highMemory) return "memory";
  return "normal";
}

function riskTagType(host: Host): "danger" | "warning" | "info" | "" {
  switch (hostRiskVariant(host)) {
    case "both": return "danger";
    case "cpu": return "warning";
    case "memory": return "info";
    default: return "";
  }
}

function riskLabel(host: Host): string {
  switch (hostRiskVariant(host)) {
    case "both": return "双高风险";
    case "cpu": return "高 CPU";
    case "memory": return "高内存";
    default: return "正常";
  }
}
</script>

<template>
  <el-card
    shadow="hover"
    class="host-card"
    :class="{
      'host-card--cpu': hostRiskVariant(host) === 'cpu',
      'host-card--memory': hostRiskVariant(host) === 'memory',
      'host-card--both': hostRiskVariant(host) === 'both',
    }"
  >
    <div class="host-header">
      <span class="host-name">{{ host.instance }}</span>
      <el-tag :type="host.status === 'up' ? 'success' : 'danger'" size="small">
        {{ host.status === "up" ? "在线" : "离线" }}
      </el-tag>
    </div>

    <div v-if="hostRiskVariant(host) !== 'normal'" class="host-risk">
      <el-tag :type="riskTagType(host)" size="small" effect="dark">
        {{ riskLabel(host) }}
      </el-tag>
    </div>

    <div class="host-metrics">
      <div class="metric-item">
        <span class="metric-label">CPU</span>
        <el-progress
          :percentage="Number(host.cpu.toFixed(1))"
          :color="cpuColor(host.cpu)"
          :stroke-width="8"
          :format="(val: number) => val.toFixed(1) + '%'"
        />
      </div>
      <div class="metric-item">
        <span class="metric-label">内存</span>
        <el-progress
          :percentage="Number(host.memory.toFixed(1))"
          :color="memoryColor(host.memory)"
          :stroke-width="8"
          :format="(val: number) => val.toFixed(1) + '%'"
        />
      </div>
    </div>

    <div class="host-footer">
      <span class="host-time">最后采集: {{ formatTime(host.lastScrape) }}</span>
      <el-button type="primary" link size="small" @click="router.push(detailPath)">
        详情
      </el-button>
    </div>
  </el-card>
</template>

<style scoped>
.host-card {
  transition: all 0.2s ease;
}

.host-card:hover {
  transform: translateY(-1px);
}

.host-card--cpu {
  border-color: rgba(245, 158, 11, 0.36);
}

.host-card--memory {
  border-color: rgba(6, 182, 212, 0.36);
}

.host-card--both {
  border-color: rgba(239, 68, 68, 0.42);
}

.host-card :deep(.el-card__body) {
  padding: 16px;
}

.host-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.host-name {
  font-weight: 600;
  font-size: 0.9rem;
}

.host-risk {
  margin-bottom: 12px;
}

.host-metrics {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.metric-item {
  display: flex;
  align-items: center;
  gap: 12px;
}

.metric-label {
  font-size: 0.75rem;
  color: var(--el-text-color-secondary);
  font-weight: 500;
  min-width: 32px;
}

.metric-item :deep(.el-progress) {
  flex: 1;
}

.host-footer {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.host-time {
  font-size: 0.7rem;
  color: var(--el-text-color-secondary);
}
</style>
