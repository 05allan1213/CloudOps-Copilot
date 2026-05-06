<script setup lang="ts">
import { computed } from "vue";
import { RouterLink } from "vue-router";

import type { Host } from "../types";

const props = defineProps<{
  host: Host;
}>();

const detailPath = computed(
  () => `/hosts/${encodeURIComponent(props.host.instance)}`,
);

function cpuColor(value: number): string {
  if (value >= 80) return "var(--danger)";
  if (value > 60) return "var(--warning)";
  return "var(--success)";
}

function memoryColor(value: number): string {
  if (value >= 85) return "var(--danger)";
  if (value > 70) return "var(--warning)";
  return "var(--success)";
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

  if (highCPU && highMemory) {
    return "both";
  }
  if (highCPU) {
    return "cpu";
  }
  if (highMemory) {
    return "memory";
  }

  return "normal";
}

function hostRiskClass(host: Host): string {
  switch (hostRiskVariant(host)) {
    case "cpu":
      return "host-risk-cpu";
    case "memory":
      return "host-risk-memory";
    case "both":
      return "host-risk-both";
    default:
      return "";
  }
}

function hostRiskLabel(host: Host): string {
  switch (hostRiskVariant(host)) {
    case "cpu":
      return "高 CPU";
    case "memory":
      return "高内存";
    case "both":
      return "双高风险";
    default:
      return "正常";
  }
}

function hostRiskHint(host: Host): string {
  switch (hostRiskVariant(host)) {
    case "cpu":
      return "CPU 已达到高风险阈值";
    case "memory":
      return "内存已达到高风险阈值";
    case "both":
      return "CPU 与内存都已达到高风险阈值";
    default:
      return "主机状态正常";
  }
}

function isHostUp(status: string): boolean {
  return status === "up";
}

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString("zh-CN");
  } catch {
    return iso;
  }
}
</script>

<template>
  <div class="host-card" :class="hostRiskClass(host)">
    <div class="host-header">
      <div class="host-name-row">
        <span
          class="status-dot"
          :class="isHostUp(host.status) ? 'dot-up' : 'dot-down'"
        ></span>
        <span class="host-name">{{ host.instance }}</span>
      </div>
      <span
        class="host-status"
        :class="isHostUp(host.status) ? 'status-up' : 'status-down'"
      >
        {{ isHostUp(host.status) ? "在线" : "离线" }}
      </span>
    </div>
    <div
      v-if="hostRiskVariant(host) !== 'normal'"
      class="host-risk-strip"
      :class="hostRiskClass(host)"
    >
      <span class="host-risk-badge" :class="hostRiskClass(host)">
        {{ hostRiskLabel(host) }}
      </span>
      <span class="host-risk-text">
        {{ hostRiskHint(host) }}
      </span>
    </div>
    <div class="host-metrics">
      <div class="metric-row">
        <div class="metric-label">CPU</div>
        <div class="metric-bar-bg">
          <div
            class="metric-bar-fill"
            :style="{
              width: Math.min(host.cpu, 100) + '%',
              background: cpuColor(host.cpu),
            }"
          />
        </div>
        <div class="metric-value" :style="{ color: cpuColor(host.cpu) }">
          {{ host.cpu.toFixed(1) }}%
        </div>
      </div>
      <div class="metric-row">
        <div class="metric-label">内存</div>
        <div class="metric-bar-bg">
          <div
            class="metric-bar-fill"
            :style="{
              width: Math.min(host.memory, 100) + '%',
              background: memoryColor(host.memory),
            }"
          />
        </div>
        <div
          class="metric-value"
          :style="{ color: memoryColor(host.memory) }"
        >
          {{ host.memory.toFixed(1) }}%
        </div>
      </div>
    </div>
    <div class="host-footer">
      <svg
        width="12"
        height="12"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
      >
        <circle cx="12" cy="12" r="10" />
        <polyline points="12 6 12 12 16 14" />
      </svg>
      最后采集: {{ formatTime(host.lastScrape) }}
      <RouterLink class="host-detail-link" :to="detailPath">
        详情
      </RouterLink>
    </div>
  </div>
</template>

<style scoped>
.host-card {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
  transition: all 0.2s ease;
  position: relative;
  overflow: hidden;
}

.host-card::before {
  content: "";
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 2px;
  background: linear-gradient(90deg, var(--accent), transparent);
  opacity: 0;
  transition: opacity 0.2s;
}

.host-card:hover::before {
  opacity: 1;
}

.host-card:hover {
  border-color: var(--border-hover);
  box-shadow: var(--shadow-md);
}

.host-card.host-risk-cpu {
  border-color: rgba(245, 158, 11, 0.36);
  box-shadow: 0 0 0 1px rgba(245, 158, 11, 0.08);
}

.host-card.host-risk-memory {
  border-color: rgba(6, 182, 212, 0.36);
  box-shadow: 0 0 0 1px rgba(6, 182, 212, 0.08);
}

.host-card.host-risk-both {
  border-color: rgba(239, 68, 68, 0.42);
  box-shadow: 0 0 0 1px rgba(239, 68, 68, 0.1);
}

.host-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1rem;
}

.host-name-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.dot-up {
  background: var(--success);
  box-shadow: 0 0 6px var(--success);
}

.dot-down {
  background: var(--danger);
  box-shadow: 0 0 6px var(--danger);
}

.host-name {
  font-weight: 600;
  font-size: 0.9rem;
}

.host-status {
  font-size: 0.7rem;
  font-weight: 600;
  padding: 0.2em 0.6em;
  border-radius: var(--radius-sm);
}

.status-up {
  background: var(--success-soft);
  color: var(--success);
}

.status-down {
  background: var(--danger-soft);
  color: var(--danger);
}

.host-risk-strip {
  display: flex;
  align-items: center;
  gap: 0.55rem;
  margin-bottom: 0.9rem;
  flex-wrap: wrap;
}

.host-risk-badge {
  display: inline-flex;
  align-items: center;
  border-radius: 999px;
  padding: 0.2rem 0.65rem;
  font-size: 0.68rem;
  font-weight: 700;
  letter-spacing: 0.02em;
  border: 1px solid transparent;
}

.host-risk-badge.host-risk-cpu {
  color: var(--warning);
  background: rgba(245, 158, 11, 0.12);
  border-color: rgba(245, 158, 11, 0.22);
}

.host-risk-badge.host-risk-memory {
  color: var(--info);
  background: rgba(6, 182, 212, 0.12);
  border-color: rgba(6, 182, 212, 0.22);
}

.host-risk-badge.host-risk-both {
  color: var(--danger);
  background: rgba(239, 68, 68, 0.12);
  border-color: rgba(239, 68, 68, 0.24);
}

.host-risk-text {
  font-size: 0.72rem;
  color: var(--text-secondary);
}

.host-metrics {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.metric-row {
  display: grid;
  grid-template-columns: 2.5rem 1fr 3.5rem;
  align-items: center;
  gap: 0.75rem;
}

.metric-label {
  font-size: 0.75rem;
  color: var(--text-muted);
  font-weight: 500;
}

.metric-bar-bg {
  height: 8px;
  background: rgba(255, 255, 255, 0.06);
  border-radius: 4px;
  overflow: hidden;
}

.metric-bar-fill {
  height: 100%;
  border-radius: 4px;
  transition: width 0.6s ease;
}

.metric-value {
  font-size: 0.8rem;
  font-weight: 600;
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.host-footer {
  margin-top: 0.875rem;
  padding-top: 0.75rem;
  border-top: 1px solid var(--border-color);
  font-size: 0.7rem;
  color: var(--text-muted);
  display: flex;
  align-items: center;
  gap: 0.35rem;
  flex-wrap: wrap;
}

.host-detail-link {
  margin-left: auto;
  color: var(--accent);
  font-weight: 700;
}

.host-detail-link:hover {
  color: var(--text-primary);
}
</style>
