<script setup lang="ts">
import HostCard from "./HostCard.vue";
import type { Host } from "../types";

type HostStatus = "all" | "up" | "down";
type HostSort = "instance" | "cpu_desc" | "memory_desc";
type HostRisk = "all" | "high_cpu" | "high_memory";

const props = defineProps<{
  hosts: Host[];
  loading: boolean;
  hostSearchInput: string;
  appliedHostQuery: string;
  selectedHostStatus: HostStatus;
  selectedHostSort: HostSort;
  selectedHostRisk: HostRisk;
  hostViewSummary: string;
  hostFilterSummary: string[];
  hasActiveHostFilters: boolean;
}>();

const emit = defineEmits<{
  "update:hostSearchInput": [value: string];
  applySearch: [];
  statusChange: [value: HostStatus];
  sortChange: [value: HostSort];
  riskChange: [value: HostRisk];
  resetFilters: [];
}>();

function updateSearchInput(event: Event) {
  const target = event.target as HTMLInputElement;
  emit("update:hostSearchInput", target.value);
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
          style="color: var(--accent)"
        >
          <rect x="2" y="2" width="20" height="8" rx="2" />
          <rect x="2" y="14" width="20" height="8" rx="2" />
        </svg>
        <h2>主机指标</h2>
      </div>
      <div class="panel-actions panel-actions-wrap">
        <form class="search-form" @submit.prevent="emit('applySearch')">
          <input
            :value="props.hostSearchInput"
            type="text"
            class="search-input"
            placeholder="搜索主机名"
            @input="updateSearchInput"
          />
          <button type="submit" class="search-btn">
            搜索
          </button>
        </form>
        <div class="filter-group">
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedHostStatus === 'all' }"
            @click="emit('statusChange', 'all')"
          >
            全部
          </button>
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedHostStatus === 'up' }"
            @click="emit('statusChange', 'up')"
          >
            在线
          </button>
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedHostStatus === 'down' }"
            @click="emit('statusChange', 'down')"
          >
            离线
          </button>
        </div>
        <div class="filter-group">
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedHostSort === 'instance' }"
            @click="emit('sortChange', 'instance')"
          >
            名称
          </button>
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedHostSort === 'cpu_desc' }"
            @click="emit('sortChange', 'cpu_desc')"
          >
            CPU
          </button>
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedHostSort === 'memory_desc' }"
            @click="emit('sortChange', 'memory_desc')"
          >
            内存
          </button>
        </div>
        <div class="filter-group">
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedHostRisk === 'all' }"
            @click="emit('riskChange', 'all')"
          >
            全风险
          </button>
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedHostRisk === 'high_cpu' }"
            @click="emit('riskChange', 'high_cpu')"
          >
            高 CPU
          </button>
          <button
            type="button"
            class="filter-btn"
            :class="{ active: selectedHostRisk === 'high_memory' }"
            @click="emit('riskChange', 'high_memory')"
          >
            高内存
          </button>
        </div>
        <button
          v-if="hasActiveHostFilters"
          type="button"
          class="reset-btn"
          @click="emit('resetFilters')"
        >
          重置
        </button>
        <span class="panel-badge">WebSocket 实时推送</span>
      </div>
    </div>

    <div class="host-summary">
      <span class="host-summary-label">当前条件</span>
      <span class="host-summary-chip host-summary-chip-strong">
        {{ hostViewSummary }}
      </span>
      <span v-if="!hasActiveHostFilters" class="host-summary-chip">
        默认视图
      </span>
      <span
        v-for="item in hostFilterSummary"
        :key="item"
        class="host-summary-chip"
      >
        {{ item }}
      </span>
    </div>

    <div v-if="loading" class="hosts-grid">
      <div v-for="n in 3" :key="n" class="host-card skeleton">
        <div class="skeleton-header">
          <div class="skeleton-dot"></div>
          <div class="skeleton-line" style="width: 60%"></div>
        </div>
        <div class="skeleton-metric">
          <div class="skeleton-label"></div>
          <div class="skeleton-bar"></div>
          <div class="skeleton-value"></div>
        </div>
        <div class="skeleton-metric">
          <div class="skeleton-label"></div>
          <div class="skeleton-bar"></div>
          <div class="skeleton-value"></div>
        </div>
      </div>
    </div>

    <div v-else-if="hosts.length === 0" class="empty-state">
      <div class="empty-icon">
        <svg
          width="48"
          height="48"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
        >
          <rect x="2" y="2" width="20" height="8" rx="2" />
          <rect x="2" y="14" width="20" height="8" rx="2" />
        </svg>
      </div>
      <p>
        {{
          appliedHostQuery
            ? "没有匹配的主机"
            : selectedHostStatus === "all"
              ? "暂无主机数据"
              : "当前筛选条件下没有主机"
        }}
      </p>
      <p class="empty-sub">
        {{
          appliedHostQuery
            ? `没有匹配“${hostSearchInput.trim() || appliedHostQuery}”的主机`
            : selectedHostRisk === "high_cpu"
              ? "当前没有高 CPU 主机"
              : selectedHostRisk === "high_memory"
                ? "当前没有高内存主机"
            : selectedHostStatus === "all"
              ? "Prometheus 尚未发现任何主机"
              : selectedHostStatus === "up"
                ? "当前没有在线主机"
                : "当前没有离线主机"
        }}
      </p>
    </div>
    <div v-else class="hosts-grid">
      <HostCard
        v-for="host in hosts"
        :key="host.instance"
        :host="host"
      />
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
  gap: 0.5rem;
}

.panel-actions-wrap {
  flex-wrap: wrap;
}

.panel-badge {
  font-size: 0.7rem;
  font-weight: 500;
  color: var(--accent);
  background: var(--accent-soft);
  padding: 0.2rem 0.6rem;
  border-radius: var(--radius-sm);
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

.reset-btn {
  font-size: 0.75rem;
  font-weight: 600;
  padding: 0.35rem 0.65rem;
  border-radius: var(--radius-sm);
  background: rgba(148, 163, 184, 0.12);
  color: var(--text-secondary);
  transition: all 0.15s ease;
}

.reset-btn:hover {
  color: var(--text-primary);
  background: rgba(148, 163, 184, 0.18);
}

.host-summary {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin-bottom: 1rem;
}

.host-summary-label {
  font-size: 0.72rem;
  color: var(--text-muted);
  font-weight: 600;
}

.host-summary-chip {
  display: inline-flex;
  align-items: center;
  min-height: 1.55rem;
  padding: 0.2rem 0.55rem;
  border-radius: 999px;
  background: rgba(148, 163, 184, 0.12);
  border: 1px solid rgba(148, 163, 184, 0.18);
  color: var(--text-secondary);
  font-size: 0.72rem;
  font-weight: 600;
}

.host-summary-chip-strong {
  background: var(--accent-soft);
  border-color: rgba(59, 130, 246, 0.2);
  color: var(--accent);
}

.search-form {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.25rem;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-color);
  background: var(--bg-secondary);
}

.search-input {
  min-width: 11rem;
  padding: 0.35rem 0.5rem;
  color: var(--text-primary);
  cursor: text;
}

.search-input::placeholder {
  color: var(--text-muted);
}

.search-btn {
  padding: 0.35rem 0.75rem;
  border-radius: var(--radius-sm);
  background: var(--accent-soft);
  color: var(--accent);
  font-size: 0.75rem;
  font-weight: 600;
  transition: all 0.15s ease;
}

.search-btn:hover {
  background: rgba(59, 130, 246, 0.18);
}

.skeleton {
  animation: skeleton-pulse 1.5s ease-in-out infinite;
}

@keyframes skeleton-pulse {
  0%,
  100% {
    opacity: 0.6;
  }
  50% {
    opacity: 1;
  }
}

.skeleton-header {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 1rem;
}

.skeleton-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--border-color);
}

.skeleton-line {
  height: 16px;
  background: var(--border-color);
  border-radius: 4px;
}

.skeleton-metric {
  display: grid;
  grid-template-columns: 2.5rem 1fr 3.5rem;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 0.75rem;
}

.skeleton-label {
  height: 12px;
  background: var(--border-color);
  border-radius: 3px;
}

.skeleton-bar {
  height: 8px;
  background: var(--border-color);
  border-radius: 4px;
}

.skeleton-value {
  height: 12px;
  background: var(--border-color);
  border-radius: 3px;
}

.host-card {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
}

.hosts-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 1rem;
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

  .search-form {
    width: 100%;
  }

  .search-input {
    min-width: 0;
    flex: 1;
  }

  .hosts-grid {
    grid-template-columns: 1fr;
  }
}
</style>
