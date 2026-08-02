<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import WorkspaceHeader from "../../components/workspace/WorkspaceHeader.vue";
import WorkspaceInspector from "../../components/workspace/WorkspaceInspector.vue";
import WorkspacePageFrame from "../../components/workspace/WorkspacePageFrame.vue";
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
const criticalCount = computed(() => items.value.filter((item) => item.severity === "critical" && item.status !== "closed").length);
const attentionCount = computed(() => items.value.filter((item) => item.attention.required).length);
const activeCount = computed(() => items.value.filter((item) => !["resolved", "closed"].includes(item.status)).length);
const recoveredCount = computed(() => items.value.filter((item) => item.recovery.state === "recovered").length);
const sortItems: { label: string; value: IncidentListSort }[] = [
  { label: "最近更新", value: "updated" },
  { label: "严重度", value: "severity" },
  { label: "生命周期状态", value: "status" },
];

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
  <WorkspacePageFrame
    as="section"
    class="incident-list-view"
    aria-labelledby="incident-list-title"
  >
    <WorkspaceHeader
      heading-id="incident-list-title"
      eyebrow="CloudOps 运维平台"
      title="Incident"
      description="按生命周期、运行范围、关注结论与恢复验证协调响应。"
    >
      <template #context>
        <div
          class="header-context-facts"
          aria-label="Incident 列表摘要"
        >
          <span>当前页 {{ items.length }} 条</span>
          <span>最近刷新：{{ refreshLabel }}</span>
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

    <section
      class="incident-attention"
      aria-labelledby="incident-attention-heading"
    >
      <div class="incident-attention__lead">
        <span aria-hidden="true">
          <UIcon :name="criticalCount ? 'i-lucide-triangle-alert' : 'i-lucide-shield-check'" />
        </span>
        <div>
          <small>当前响应态势</small>
          <h2 id="incident-attention-heading">
            <template v-if="criticalCount">{{ criticalCount }} 条严重 Incident 需要优先判断</template>
            <template v-else-if="attentionCount">{{ attentionCount }} 条 Incident 需要负责人关注</template>
            <template v-else>当前响应队列没有高风险阻塞</template>
          </h2>
          <p>{{ activeCount }} 条仍在生命周期中，{{ recoveredCount }} 条已完成恢复验证。</p>
        </div>
      </div>
      <dl aria-label="Incident 工作队列摘要">
        <div><dt>严重</dt><dd class="is-critical">{{ criticalCount }}</dd><small>当前页</small></div>
        <div><dt>处理中</dt><dd>{{ activeCount }}</dd><small>未恢复</small></div>
        <div><dt>需要关注</dt><dd class="is-warning">{{ attentionCount }}</dd><small>待判断</small></div>
        <div><dt>恢复已验证</dt><dd class="is-success">{{ recoveredCount }}</dd><small>可收敛</small></div>
      </dl>
    </section>

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

    <section
      class="incident-queue-heading"
      aria-labelledby="incident-results-title"
    >
      <div class="results-heading">
        <span class="incident-queue-icon" aria-hidden="true"><UIcon name="i-lucide-list-checks" /></span>
        <div>
          <small>当前处置队列</small>
          <h2 id="incident-results-title">处置队列</h2>
          <p>选择一项查看当前结论、生命周期阻塞和下一步。</p>
        </div>
      </div>
      <div class="incident-queue-tools">
        <span
          class="result-count"
          role="status"
          aria-live="polite"
        >{{ resultAnnouncement }}</span>
        <USelect
          :model-value="sortKey"
          :items="sortItems"
          value-key="value"
          icon="i-lucide-arrow-down-up"
          aria-label="Incident 排序字段"
          @update:model-value="setSort"
        />
        <UTooltip :text="sortDirection === 'asc' ? '当前升序，点击切换为降序' : '当前降序，点击切换为升序'">
          <UButton
            color="neutral"
            variant="ghost"
            :icon="sortDirection === 'asc' ? 'i-lucide-arrow-up' : 'i-lucide-arrow-down'"
            square
            aria-label="切换排序方向"
            @click="setDirection(sortDirection === 'asc' ? 'desc' : 'asc')"
          />
        </UTooltip>
      </div>
    </section>

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
      title="Incident 生命周期"
      description="当前结论、生命周期阻塞与下一步。"
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
  </WorkspacePageFrame>
</template>

<style scoped>
.incident-list-view { display: grid; min-width: 0; gap: var(--co-space-4); }
.header-context-facts { display: flex; flex-wrap: wrap; gap: var(--co-space-3); color: var(--co-text-muted); font-size: 10px; }
.incident-attention { display: grid; min-width: 0; grid-template-columns: minmax(0, 1.15fr) minmax(420px, .85fr); align-items: center; gap: var(--co-space-5); padding-bottom: var(--co-space-3); border-bottom: 1px solid color-mix(in srgb, var(--co-status-critical-border) 28%, var(--co-border-subtle)); }
.incident-attention__lead { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); }
.incident-attention__lead > span { display: grid; width: 46px; height: 46px; flex: 0 0 auto; place-items: center; border-radius: var(--co-radius-panel); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); font-size: 19px; }
.incident-attention__lead > div { min-width: 0; }
.incident-attention__lead small { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; font-weight: 800; text-transform: uppercase; }
.incident-attention__lead h2 { margin: 3px 0 0; font-size: 19px; line-height: 1.3; }
.incident-attention__lead p { margin: var(--co-space-1) 0 0; color: var(--co-text-secondary); font-size: 11px; }
.incident-attention dl { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; gap: var(--co-space-2); }
.incident-attention dl div { display: grid; min-width: 0; gap: 1px; padding: var(--co-space-2) var(--co-space-3); border-radius: var(--co-radius-panel); background: var(--co-bg-subtle); }
.incident-attention dt, .incident-attention small { color: var(--co-text-muted); font-size: 9px; }
.incident-attention dd { margin: 0; font-family: var(--co-font-mono); font-size: 18px; font-weight: 800; font-variant-numeric: tabular-nums; }
.incident-attention .is-critical { color: var(--co-status-critical-fg); }
.incident-attention .is-warning { color: var(--co-status-warning-fg); }
.incident-attention .is-success { color: var(--co-status-success-fg); }
.incident-queue-heading { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-4); padding: var(--co-space-2) 0; border-bottom: 1px solid var(--co-border-subtle); }
.results-heading { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); }
.incident-queue-icon { display: grid; width: 38px; height: 38px; flex: 0 0 auto; place-items: center; border-radius: var(--co-radius-panel); color: var(--co-status-info-fg); background: var(--co-status-info-bg); }
.results-heading small { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; font-weight: 800; text-transform: uppercase; }
.results-heading h2, .results-heading p { margin: 0; }
.results-heading h2 { margin-top: 2px; font-size: 17px; }
.results-heading p { margin-top: 2px; color: var(--co-text-muted); font-size: 11px; }
.result-count { color: var(--co-text-muted); font-size: 11px; white-space: nowrap; }
.incident-queue-tools { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); }
.incident-skeleton { display: grid; gap: 1px; padding: var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-bg-surface); }
.skeleton-row { height: var(--co-table-row-height); }
@media (max-width: 1024px) {
  .incident-attention { grid-template-columns: minmax(0, 1fr); }
  .incident-attention dl { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .incident-queue-heading { align-items: flex-start; flex-direction: column; }
  .incident-queue-tools { justify-content: flex-start; }
  .header-context-facts { display: grid; }
}
</style>
