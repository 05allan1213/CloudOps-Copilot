<script setup lang="ts">
import type { AlertEvent } from "../types";

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

function eventStatusLabel(status: AlertEvent["status"]): string {
  return status === "resolved" ? "恢复" : "触发";
}

function eventStatusClass(status: AlertEvent["status"]): string {
  return status === "resolved" ? "event-status-resolved" : "event-status-firing";
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
          style="color: #818cf8"
        >
          <path d="M3 12h4l3 8 4-16 3 8h4" />
        </svg>
        <h2>最近事件</h2>
      </div>
      <div class="panel-actions panel-actions-wrap">
        <div class="filter-group">
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedStatus === 'all' }"
            @click="emit('statusChange', 'all')"
          >
            全部状态
          </button>
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedStatus === 'firing' }"
            @click="emit('statusChange', 'firing')"
          >
            触发
          </button>
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedStatus === 'resolved' }"
            @click="emit('statusChange', 'resolved')"
          >
            恢复
          </button>
        </div>
        <div class="filter-group">
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedSeverity === 'all' }"
            @click="emit('severityChange', 'all')"
          >
            全部级别
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
        <span class="panel-badge event-badge">Webhook 历史流</span>
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

    <div v-else-if="events.length === 0" class="empty-state">
      <div class="empty-icon">
        <svg
          width="48"
          height="48"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
        >
          <path d="M3 12h4l3 8 4-16 3 8h4" />
        </svg>
      </div>
      <p>暂无最近事件</p>
      <p class="empty-sub">新告警或恢复事件会在这里按时间倒序展示</p>
    </div>

    <div v-else class="event-list">
      <div
        v-for="event in events"
        :key="`${event.fingerprint}-${event.receivedAt}-${event.status}`"
        class="event-card"
        :class="severityClass(event.labels.severity)"
      >
        <div class="event-top">
          <div class="event-top-left">
            <span
              class="alert-severity"
              :class="severityClass(event.labels.severity)"
            >
              {{ severityLabel(event.labels.severity) }}
            </span>
            <span class="event-status" :class="eventStatusClass(event.status)">
              {{ eventStatusLabel(event.status) }}
            </span>
          </div>
          <span class="alert-time">{{ formatTime(event.receivedAt) }}</span>
        </div>
        <div class="event-body">
          <div class="alert-name">
            {{ event.labels.alertname || "未知事件" }}
          </div>
          <div class="alert-instance">
            {{ event.labels.instance || "" }}
          </div>
          <p class="alert-summary">
            {{ event.annotations.summary || event.annotations.description || "" }}
          </p>
          <p class="event-meta">开始时间 {{ formatTime(event.startsAt) }}</p>
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

.panel-badge {
  font-size: 0.7rem;
  font-weight: 500;
  color: var(--accent);
  background: var(--accent-soft);
  padding: 0.2rem 0.6rem;
  border-radius: var(--radius-sm);
  border: 1px solid rgba(59, 130, 246, 0.2);
}

.event-badge {
  color: #818cf8;
  background: rgba(99, 102, 241, 0.12);
  border-color: rgba(129, 140, 248, 0.22);
}

.panel-actions {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.panel-actions-wrap {
  flex-wrap: wrap;
  justify-content: flex-end;
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

.event-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 0.75rem;
}

.event-card {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
  transition: all 0.2s ease;
  border-left: 3px solid transparent;
}

.event-card:hover {
  border-color: var(--border-hover);
  box-shadow: var(--shadow-md);
}

.event-card.severity-critical {
  border-left-color: var(--danger);
}

.event-card.severity-warning {
  border-left-color: var(--warning);
}

.event-card.severity-info {
  border-left-color: var(--info);
}

.event-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 0.65rem;
}

.event-top-left {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.45rem;
}

.event-status {
  font-size: 0.68rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  padding: 0.2em 0.55em;
  border-radius: var(--radius-sm);
}

.event-status-firing {
  background: rgba(239, 68, 68, 0.1);
  color: var(--danger);
}

.event-status-resolved {
  background: rgba(34, 197, 94, 0.12);
  color: var(--success);
}

.event-meta {
  font-size: 0.72rem;
  color: var(--text-muted);
  margin-top: 0.45rem;
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

  .panel-actions-wrap {
    justify-content: flex-start;
  }
}
</style>
