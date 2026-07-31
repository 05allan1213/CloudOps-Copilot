<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import ContextToolbar from "../../components/workspace/ContextToolbar.vue";
import WorkspaceHeader from "../../components/workspace/WorkspaceHeader.vue";
import WorkspaceInspector from "../../components/workspace/WorkspaceInspector.vue";
import IncidentFilterBar from "../../components/incidents/IncidentFilterBar.vue";
import IncidentInspector from "../../components/incidents/IncidentInspector.vue";
import IncidentTable from "../../components/incidents/IncidentTable.vue";
import StateBlock from "../../components/incidents/StateBlock.vue";
import { useIncidentList } from "../../composables/incidents/useIncidentList";
import { useWorkspaceInspector } from "../../composables/useWorkspaceInspector";
import type { IncidentListDirection, IncidentListSort, IncidentView } from "../../types/incidents";
import { formatIncidentTime } from "../../utils/incidentTime";

const route = useRoute();
const router = useRouter();
const incidentTable = ref<{
  getRowElement: (incidentID: string) => HTMLElement | null;
  getScrollElement: () => HTMLElement | null;
} | null>(null);
const {
  selectedID,
  triggerElement: inspectorTrigger,
  open: openInspector,
  close: closeInspector,
} = useWorkspaceInspector({
  scrollElement: () => incidentTable.value?.getScrollElement() ?? null,
  resolveTrigger: (incidentID) => incidentTable.value?.getRowElement(incidentID) ?? null,
});
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
  updatePresentation,
  reset,
} = useIncidentList(router, route.query);

const hasActiveFilters = computed(() => Boolean(
  filters.status || filters.severity || filters.service || filters.attention !== undefined
  || filters.resource || filters.alert || filters.from || filters.to,
));
const resultAnnouncement = computed(() => {
  if (loading.value && items.value.length > 0) return `正在更新 ${items.value.length} 条 Incident…`;
  if (state.value === "empty") return "没有符合条件的 Incident。";
  return `已加载 ${items.value.length} 条 Incident。`;
});
const refreshLabel = computed(() => lastUpdatedAt.value ? formatIncidentTime(lastUpdatedAt.value) : "尚未刷新");
const sortKey = computed<IncidentListSort>(() => filters.sort ?? "updated");
const sortDirection = computed<IncidentListDirection>(() => filters.direction ?? "desc");

function recoverEmptyState() {
  return hasActiveFilters.value ? reset() : load(false);
}

function selectIncident(incident: IncidentView, trigger: HTMLElement | null) {
  void openInspector(incident.id, trigger);
}

function handleInspectorOpenChange(open: boolean) {
  if (!open) void closeInspector();
}

function setSort(value: IncidentListSort) {
  void updatePresentation(value, sortDirection.value);
}

function setDirection(value: IncidentListDirection) {
  void updatePresentation(sortKey.value, value);
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
    <WorkspaceHeader
      heading-id="incident-list-title"
      eyebrow="CloudOps 运维平台"
      title="Incident"
      description="按生命周期、运行范围、Attention 与恢复证明协调响应。"
    >
      <template #context>
        <div
          class="header-context-facts"
          aria-label="Incident 列表摘要"
        >
          <span><strong>{{ items.length }}</strong> 已加载</span>
          <span>最近刷新 {{ refreshLabel }}</span>
        </div>
      </template>
      <template #actions>
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-refresh-cw"
          :loading="loading && !loadingMore"
          label="刷新"
          @click="load(false)"
        />
      </template>
    </WorkspaceHeader>

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

    <ContextToolbar label="Incident 列表排序与状态">
      <template #filters>
        <div class="results-heading">
          <div>
            <h2 id="incident-results-title">
              Incident 列表
            </h2>
            <p>固定关键列，次要列偏好保存在本地；排序状态写入 URL。</p>
          </div>
          <span
            class="result-count"
            role="status"
            aria-live="polite"
          >{{ resultAnnouncement }}</span>
        </div>
      </template>
      <template #secondary>
        <span class="sort-contract">排序：{{ sortKey }} / {{ sortDirection === "asc" ? "升序" : "降序" }}</span>
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-arrow-up-a-z"
          label="级别"
          @click="setSort('severity')"
        />
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-arrow-down-up"
          label="状态"
          @click="setSort('status')"
        />
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-clock-3"
          label="更新时间"
          @click="setSort('updated')"
        />
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-arrow-up-down"
          label="切换方向"
          @click="setDirection(sortDirection === 'asc' ? 'desc' : 'asc')"
        />
      </template>
    </ContextToolbar>

    <div
      v-if="state === 'loading' && items.length === 0"
      class="incident-skeleton"
      role="status"
      aria-live="polite"
      aria-label="正在加载 Incident"
    >
      <USkeleton
        v-for="index in 8"
        :key="index"
        class="skeleton-row"
      />
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
        ref="incidentTable"
        :items="items"
        :pending="loading"
        :next-cursor="nextCursor"
        :loading-more="loadingMore"
        :sort="sortKey"
        :direction="sortDirection"
        :selected-id="selectedID"
        @update:sort="setSort"
        @update:direction="setDirection"
        @select="selectIncident"
        @load-more="loadMore"
      />
    </template>

    <WorkspaceInspector
      :open="Boolean(selectedID)"
      title="Incident Inspector"
      description="当前 Cycle 的只读摘要与生命周期状态。"
      :trigger="inspectorTrigger"
      @update:open="handleInspectorOpenChange"
    >
      <IncidentInspector
        v-if="selectedID"
        :incident-i-d="selectedID"
      />
      <template #footer>
        <UButton
          v-if="selectedID"
          color="primary"
          icon="i-lucide-arrow-up-right"
          label="打开完整 Incident 详情"
          :to="{ name: 'incident-detail', params: { incidentId: selectedID }, query: { from: 'incidents' } }"
        />
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-x"
          label="关闭"
          @click="closeInspector()"
        />
      </template>
    </WorkspaceInspector>
  </section>
</template>

<style scoped>
.incident-list-view { display: grid; width: min(100%, var(--co-content-max-width)); min-width: 0; margin: 0 auto; gap: var(--co-space-4); }
.header-context-facts { display: flex; flex-wrap: wrap; gap: var(--co-space-3); color: var(--co-text-secondary); font-size: 11px; }
.header-context-facts strong { color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 16px; }
.results-heading { display: flex; min-width: 0; width: 100%; align-items: center; justify-content: space-between; gap: var(--co-space-4); }
.results-heading h2, .results-heading p { margin: 0; }
.results-heading h2 { font-size: 16px; }
.results-heading p { margin-top: 2px; color: var(--co-text-muted); font-size: 11px; }
.result-count, .sort-contract { color: var(--co-text-muted); font-size: 11px; white-space: nowrap; }
.incident-skeleton { display: grid; gap: 1px; padding: var(--co-space-3); border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.skeleton-row { height: var(--co-table-row-height); }
@media (max-width: 767px) {
  .results-heading { align-items: flex-start; flex-direction: column; }
  .header-context-facts { display: grid; }
}
</style>
