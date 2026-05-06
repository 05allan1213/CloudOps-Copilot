<script setup lang="ts">
import type { AlertRecord } from "../types";

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
}>();

function severityClass(severity: string | undefined): string {
  switch (severity ?? "info") {
    case "critical":
      return "severity-critical";
    case "warning":
      return "severity-warning";
    default:
      return "severity-info";
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

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString("zh-CN");
  } catch {
    return iso;
  }
}
</script>

<template>
  <section class="panel">
    <div class="panel-header">
      <div class="panel-title">
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          style="color: var(--warning)"
        >
          <path
            d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
          />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
        <h2>告警列表</h2>
      </div>
      <div class="panel-actions">
        <div class="filter-group">
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedSeverity === 'all' }"
            @click="emit('severityChange', 'all')"
          >
            全部
          </button>
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedSeverity === 'critical' }"
            @click="emit('severityChange', 'critical')"
          >
            严重
          </button>
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedSeverity === 'warning' }"
            @click="emit('severityChange', 'warning')"
          >
            警告
          </button>
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedSeverity === 'info' }"
            @click="emit('severityChange', 'info')"
          >
            提示
          </button>
        </div>
        <button
          type="button"
          class="refresh-btn"
          :disabled="refreshing"
          @click="emit('refresh')"
        >
          <svg
            v-if="!refreshing"
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <polyline points="23 4 23 10 17 10" />
            <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10" />
          </svg>
          <span v-else class="spin"></span>
          {{ refreshing ? "刷新中..." : "刷新" }}
        </button>
      </div>
    </div>

    <div v-if="error" class="error-banner">
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
      >
        <circle cx="12" cy="12" r="10" />
        <line x1="15" y1="9" x2="9" y2="15" />
        <line x1="9" y1="9" x2="15" y2="15" />
      </svg>
      {{ error }}
    </div>

    <div v-else-if="alerts.length === 0" class="empty-state">
      <div class="empty-icon" style="color: var(--success)">
        <svg
          width="48"
          height="48"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
        >
          <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
          <polyline points="22 4 12 14.01 9 11.01" />
        </svg>
      </div>
      <p style="color: var(--success); font-weight: 600">一切正常</p>
      <p class="empty-sub">当前无活跃告警</p>
    </div>

    <div v-else class="alert-list">
      <div
        v-for="alert in alerts"
        :key="alert.fingerprint"
        class="alert-card"
        :class="severityClass(alert.labels.severity)"
      >
        <div class="alert-top">
          <span
            class="alert-severity"
            :class="severityClass(alert.labels.severity)"
          >
            {{ severityLabel(alert.labels.severity) }}
          </span>
          <span class="alert-time">{{ formatTime(alert.startsAt) }}</span>
        </div>
        <div class="alert-body">
          <div class="alert-name">
            {{ alert.labels.alertname || "未知告警" }}
          </div>
          <div class="alert-instance">
            {{ alert.labels.instance || "" }}
          </div>
          <p class="alert-summary">
            {{ alert.annotations.summary || alert.annotations.description || "" }}
          </p>
          <p
            v-if="alert.annotations.description && alert.annotations.summary"
            class="alert-desc"
          >
            {{ alert.annotations.description }}
          </p>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.panel {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  padding: 1.25rem 1.5rem;
  margin-bottom: 1.5rem;
  backdrop-filter: blur(8px);
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1.25rem;
  flex-wrap: wrap;
  gap: 0.75rem;
}

.panel-title {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.panel-title h2 {
  font-size: 1rem;
  font-weight: 600;
  margin: 0;
}

.panel-actions {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.filter-group {
  display: flex;
  gap: 0.25rem;
  background: var(--bg-secondary);
  padding: 0.25rem;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-color);
}

.filter-btn {
  font-size: 0.75rem;
  font-weight: 500;
  padding: 0.35em 0.75em;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  transition: all 0.15s;
}

.filter-btn:hover {
  color: var(--text-secondary);
}

.filter-btn.active {
  background: var(--accent-soft);
  color: var(--accent);
  font-weight: 600;
}

.refresh-btn {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  font-size: 0.75rem;
  font-weight: 500;
  padding: 0.4rem 0.75rem;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-color);
  color: var(--text-secondary);
  background: var(--bg-secondary);
  transition: all 0.15s;
}

.refresh-btn:hover:not(:disabled) {
  border-color: var(--accent);
  color: var(--accent);
}

.refresh-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.spin {
  width: 14px;
  height: 14px;
  border: 2px solid var(--border-color);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.empty-state {
  text-align: center;
  padding: 3rem 0;
  color: var(--text-muted);
}

.empty-icon {
  margin-bottom: 1rem;
  color: var(--text-muted);
}

.empty-sub {
  font-size: 0.8rem;
  margin-top: 0.35rem;
}

.error-banner {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  background: var(--danger-soft);
  border: 1px solid rgba(239, 68, 68, 0.25);
  border-radius: var(--radius-sm);
  padding: 0.75rem 1rem;
  color: var(--danger);
  font-size: 0.85rem;
}

.alert-list {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.alert-card {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
  transition: all 0.2s ease;
  border-left: 3px solid transparent;
}

.alert-card:hover {
  border-color: var(--border-hover);
  box-shadow: var(--shadow-md);
}

.alert-card.severity-critical {
  border-left-color: var(--danger);
}

.alert-card.severity-warning {
  border-left-color: var(--warning);
}

.alert-card.severity-info {
  border-left-color: var(--info);
}

.alert-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.5rem;
}

.alert-severity {
  font-size: 0.7rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  padding: 0.2em 0.6em;
  border-radius: var(--radius-sm);
}

.alert-severity.severity-critical {
  background: var(--danger-soft);
  color: var(--danger);
}

.alert-severity.severity-warning {
  background: var(--warning-soft);
  color: var(--warning);
}

.alert-severity.severity-info {
  background: var(--info-soft);
  color: var(--info);
}

.alert-time {
  font-size: 0.7rem;
  color: var(--text-muted);
}

.alert-name {
  font-weight: 600;
  font-size: 0.9rem;
  margin-bottom: 0.15rem;
}

.alert-instance {
  font-size: 0.75rem;
  color: var(--text-muted);
  margin-bottom: 0.5rem;
}

.alert-summary {
  font-size: 0.8rem;
  color: var(--text-secondary);
  margin: 0;
}

.alert-desc {
  font-size: 0.75rem;
  color: var(--text-muted);
  margin: 0.35rem 0 0;
}

@media (max-width: 768px) {
  .panel-header {
    flex-direction: column;
    align-items: flex-start;
  }

  .panel-actions {
    width: 100%;
    justify-content: space-between;
  }
}
</style>
