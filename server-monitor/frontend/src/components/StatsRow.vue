<script setup lang="ts">
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
  <section class="stats-row">
    <div class="stat-card">
      <div
        class="stat-icon"
        style="background: var(--accent-soft); color: var(--accent)"
      >
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <rect x="2" y="2" width="20" height="8" rx="2" />
          <rect x="2" y="14" width="20" height="8" rx="2" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value">{{ hostCount }}</span>
        <span class="stat-label">{{ hostCountLabel }}</span>
      </div>
    </div>
    <div class="stat-card">
      <div
        class="stat-icon"
        style="background: var(--warning-soft); color: var(--warning)"
      >
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M12 3v10" />
          <path d="M8 13h8" />
          <path d="M5.6 19h12.8" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value" style="color: var(--warning)">{{ highCpuHostCount }}</span>
        <span class="stat-label">高 CPU</span>
      </div>
    </div>
    <div class="stat-card">
      <div
        class="stat-icon"
        style="background: var(--info-soft); color: var(--info)"
      >
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <rect x="4" y="7" width="16" height="10" rx="2" />
          <path d="M8 11h8" />
          <path d="M8 14h5" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value" style="color: var(--info)">{{ highMemoryHostCount }}</span>
        <span class="stat-label">高内存</span>
      </div>
    </div>
    <div class="stat-card">
      <div
        class="stat-icon"
        style="background: var(--danger-soft); color: var(--danger)"
      >
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M6 6l12 12" />
          <path d="M18 6L6 18" />
          <circle cx="12" cy="12" r="9" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value" style="color: var(--danger)">{{ bothRiskHostCount }}</span>
        <span class="stat-label">双高风险</span>
      </div>
    </div>
    <div class="stat-card">
      <div
        class="stat-icon"
        style="background: var(--info-soft); color: var(--info)"
      >
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path
            d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
          />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value">{{ activeAlertCount }}</span>
        <span class="stat-label">活跃告警</span>
      </div>
    </div>
    <div class="stat-card">
      <div
        class="stat-icon"
        style="background: rgba(99, 102, 241, 0.12); color: #818cf8"
      >
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M3 12h4l3 8 4-16 3 8h4" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value" style="color: #818cf8">{{ alertEventCount }}</span>
        <span class="stat-label">最近事件</span>
      </div>
    </div>
    <div class="stat-card">
      <div
        class="stat-icon"
        style="background: var(--danger-soft); color: var(--danger)"
      >
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <circle cx="12" cy="12" r="10" />
          <line x1="15" y1="9" x2="9" y2="15" />
          <line x1="9" y1="9" x2="15" y2="15" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value" style="color: var(--danger)">{{ criticalCount }}</span>
        <span class="stat-label">严重</span>
      </div>
    </div>
    <div class="stat-card">
      <div
        class="stat-icon"
        style="background: var(--warning-soft); color: var(--warning)"
      >
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path
            d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
          />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value" style="color: var(--warning)">{{ warningCount }}</span>
        <span class="stat-label">警告</span>
      </div>
    </div>
    <div class="stat-card">
      <div
        class="stat-icon"
        style="background: rgba(6, 182, 212, 0.12); color: var(--info)"
      >
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="16" x2="12" y2="12" />
          <line x1="12" y1="8" x2="12.01" y2="8" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value" style="color: var(--info)">{{ infoCount }}</span>
        <span class="stat-label">提示</span>
      </div>
    </div>
  </section>
</template>

<style scoped>
.stats-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 1rem;
  margin-bottom: 1.5rem;
}

.stat-card {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
  display: flex;
  align-items: center;
  gap: 0.875rem;
  transition: all 0.2s ease;
}

.stat-card:hover {
  border-color: var(--border-hover);
  transform: translateY(-1px);
  box-shadow: var(--shadow-md);
}

.stat-icon {
  width: 40px;
  height: 40px;
  border-radius: var(--radius-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.stat-info {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
}

.stat-value {
  font-size: 1.5rem;
  font-weight: 700;
  line-height: 1;
  color: var(--text-primary);
}

.stat-label {
  font-size: 0.7rem;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

@media (max-width: 768px) {
  .stats-row {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (max-width: 480px) {
  .stats-row {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
