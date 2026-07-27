<script setup lang="ts">
import { computed, onMounted } from "vue";
import { useRoute, useRouter } from "vue-router";

import IncidentFilterBar from "../../components/incidents/IncidentFilterBar.vue";
import IncidentTable from "../../components/incidents/IncidentTable.vue";
import StateBlock from "../../components/incidents/StateBlock.vue";
import { useIncidentList } from "../../composables/incidents/useIncidentList";
import { formatIncidentTime } from "../../utils/incidentTime";

const route = useRoute();
const router = useRouter();
const {
  filters,
  items,
  nextCursor,
  state,
  error,
  loading,
  loadingMore,
  lastUpdatedAt,
  hydratedFromCache,
  load,
  loadMore,
  syncURLAndLoad,
  reset,
} = useIncidentList(router, route.query);

const resultAnnouncement = computed(() => {
  if (loading.value && items.value.length > 0) return `正在更新 ${items.value.length} 条 Incident…`;
  if (state.value === "empty") return "没有符合条件的 Incident。";
  return `已加载 ${items.value.length} 条 Incident。`;
});

const refreshLabel = computed(() =>
  lastUpdatedAt.value ? formatIncidentTime(lastUpdatedAt.value) : "尚未刷新",
);
const hasActiveFilters = computed(() => Boolean(
  filters.status || filters.severity || filters.service || filters.attention !== undefined
  || filters.resource || filters.alert || filters.from || filters.to,
));

function recoverEmptyState() {
  return hasActiveFilters.value ? reset() : load(false);
}

onMounted(() => {
  if (!hydratedFromCache) void load(false);
});
</script>

<template>
  <section
    class="incident-list-view"
    aria-labelledby="incident-list-title"
  >
    <header class="page-header">
      <div class="page-heading">
        <p class="eyebrow">
          CloudOps 运维平台
        </p>
        <h1 id="incident-list-title">
          Incident
        </h1>
        <p>按生命周期、运行范围、Attention 与恢复状态协调响应。</p>
      </div>
      <dl class="page-facts">
        <div>
          <dt>已加载</dt>
          <dd>{{ items.length }}</dd>
        </div>
        <div>
          <dt>最近刷新</dt>
          <dd>{{ refreshLabel }}</dd>
        </div>
      </dl>
    </header>

    <IncidentFilterBar
      v-model:status="filters.status"
      v-model:severity="filters.severity"
      v-model:service="filters.service"
      v-model:attention="filters.attention"
      v-model:resource="filters.resource"
      v-model:alert="filters.alert"
      v-model:from="filters.from"
      v-model:to="filters.to"
      :loading="loading && !loadingMore"
      @apply="syncURLAndLoad"
      @reset="reset"
    />

    <div class="results-heading">
      <div>
        <h2 id="incident-results-title">
          Incident 列表
        </h2>
        <p>服务端默认按最近更新时间排序；表头排序仅作用于已加载结果。</p>
      </div>
      <span
        class="result-count"
        role="status"
        aria-live="polite"
      >
        <span
          v-if="loading"
          class="updating-dot"
          aria-hidden="true"
        />
        {{ resultAnnouncement }}
      </span>
    </div>

    <div
      v-if="state === 'loading' && items.length === 0"
      class="incident-skeleton"
      role="status"
      aria-live="polite"
      aria-label="正在加载 Incident"
    >
      <div class="skeleton-header" />
      <div
        v-for="index in 8"
        :key="index"
        class="skeleton-row"
      >
        <span
          v-for="cell in 6"
          :key="cell"
        />
      </div>
    </div>

    <StateBlock
      v-else-if="state === 'forbidden'"
      state="forbidden"
      :busy="loading"
      :message="error?.message"
      :request-i-d="error?.requestID"
      :trace-i-d="error?.traceID"
      primary-action-label="重试访问"
      @primary-action="load(false)"
    />

    <StateBlock
      v-else-if="state === 'error' || state === 'unavailable'"
      :state="state"
      :busy="loading"
      :message="error?.message"
      :request-i-d="error?.requestID"
      :trace-i-d="error?.traceID"
      primary-action-label="重试"
      @primary-action="load(false)"
    />

    <StateBlock
      v-else-if="state === 'empty'"
      state="empty"
      :busy="loading"
      :primary-action-label="hasActiveFilters ? '清除筛选' : '重试'"
      :secondary-action-label="hasActiveFilters ? '重试' : undefined"
      @primary-action="recoverEmptyState"
      @secondary-action="load(false)"
    />

    <template v-else>
      <StateBlock
        v-if="error"
        state="error"
        :busy="loading"
        title="更多结果加载失败"
        :message="error.message"
        :request-i-d="error.requestID"
        :trace-i-d="error.traceID"
        primary-action-label="重试"
        @primary-action="nextCursor ? loadMore() : load(false)"
      />
      <IncidentTable
        :items="items"
        :pending="loading"
        :next-cursor="nextCursor"
        :loading-more="loadingMore"
        @load-more="loadMore"
      />
    </template>
  </section>
</template>

<style scoped>
.incident-list-view {
  display: grid;
  width: min(100%, var(--co-content-max-width));
  min-width: 0;
  margin: 0 auto;
  gap: var(--co-space-5);
}

.page-header {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-6);
  padding: var(--co-space-4) var(--co-space-5);
  border-bottom: 1px solid var(--co-border-default);
}

.page-heading {
  min-width: 0;
}

.eyebrow,
.page-heading h1,
.page-heading > p,
.page-facts,
.results-heading h2,
.results-heading p {
  margin: 0;
}

.eyebrow {
  color: var(--co-action-primary);
  font-size: 11px;
  font-weight: 750;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.page-heading h1 {
  margin-top: 2px;
  color: var(--co-text-primary);
  font-size: 24px;
  line-height: 1.2;
}

.page-heading > p {
  max-width: 72ch;
  margin-top: var(--co-space-1);
  color: var(--co-text-secondary);
  font-size: 13px;
}

.page-facts {
  display: flex;
  flex: 0 0 auto;
  align-items: stretch;
  gap: var(--co-space-4);
}

.page-facts div {
  display: grid;
  min-width: 96px;
  align-content: center;
  gap: 2px;
  padding-left: var(--co-space-4);
  border-left: 1px solid var(--co-border-default);
}

.page-facts dt {
  color: var(--co-text-muted);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.page-facts dd {
  max-width: 200px;
  margin: 0;
  color: var(--co-text-primary);
  font-size: 13px;
  font-variant-numeric: tabular-nums;
}

.page-facts div:first-child dd {
  font-family: var(--co-font-mono);
  font-size: 18px;
  font-weight: 750;
}

.results-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: var(--co-space-4);
}

.results-heading h2 {
  font-size: 18px;
}

.results-heading p {
  margin-top: 2px;
  color: var(--co-text-muted);
  font-size: 12px;
}

.result-count {
  display: inline-flex;
  min-height: 28px;
  flex: 0 0 auto;
  align-items: center;
  gap: var(--co-space-2);
  padding: 3px 10px;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-pill);
  color: var(--co-text-secondary);
  background: var(--co-bg-surface);
  font-size: 12px;
}

.updating-dot {
  width: 8px;
  height: 8px;
  border: 2px solid var(--co-status-info-border);
  border-top-color: var(--co-status-info-fg);
  border-radius: 50%;
  animation: updating-rotation 1s linear infinite;
}

.incident-skeleton {
  overflow: hidden;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-surface);
}

.skeleton-header,
.skeleton-row {
  display: grid;
  grid-template-columns: 0.8fr 2fr 1fr 1.4fr 0.8fr 1.2fr;
  gap: var(--co-space-4);
  padding: var(--co-space-3) var(--co-space-4);
}

.skeleton-header {
  height: 42px;
  background: var(--co-bg-subtle);
}

.skeleton-row {
  min-height: 60px;
  align-items: center;
  border-top: 1px solid var(--co-border-default);
}

.skeleton-row span {
  height: 12px;
  border-radius: var(--co-radius-control);
  background: linear-gradient(90deg, var(--co-bg-subtle), var(--co-bg-hover), var(--co-bg-subtle));
  animation: skeleton-pulse 1.4s ease-in-out infinite;
}

@keyframes updating-rotation {
  to { transform: rotate(360deg); }
}

@keyframes skeleton-pulse {
  50% { opacity: 0.55; }
}

@media (prefers-reduced-motion: reduce) {
  .updating-dot,
  .skeleton-row span {
    animation: none;
  }
}

@media (max-width: 767px) {
  .page-header,
  .results-heading {
    align-items: flex-start;
    flex-direction: column;
  }

  .page-header {
    gap: var(--co-space-4);
    padding: var(--co-space-2) 0 var(--co-space-4);
  }

  .page-heading h1 {
    font-size: 22px;
  }

  .page-heading > p {
    font-size: 14px;
  }

  .page-facts {
    width: 100%;
  }

  .page-facts div {
    min-width: 0;
    flex: 1;
    padding-left: var(--co-space-3);
  }

  .page-facts div:first-child {
    padding-left: 0;
    border-left: 0;
  }

  .page-facts dd {
    overflow-wrap: anywhere;
  }

  .result-count {
    min-height: 32px;
  }

  .skeleton-header {
    display: none;
  }

  .skeleton-row {
    grid-template-columns: 1fr 1fr;
    min-height: 132px;
  }

  .skeleton-row span:nth-child(n + 5) {
    display: none;
  }
}
</style>
