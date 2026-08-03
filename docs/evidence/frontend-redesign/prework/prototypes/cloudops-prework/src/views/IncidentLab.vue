<script setup lang="ts">
import type { TableColumn } from "@nuxt/ui";
import { computed, h, nextTick, reactive, ref, resolveComponent, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { incidents, severityLabel, statusLabel, type IncidentRow, type IncidentSeverity, type IncidentStatus } from "../data";

const route = useRoute();
const router = useRouter();
const UBadge = resolveComponent("UBadge");
const UButton = resolveComponent("UButton");
const UCheckbox = resolveComponent("UCheckbox");

const search = ref(String(route.query.q ?? ""));
const status = ref(String(route.query.status ?? "all"));
const severity = ref(String(route.query.severity ?? "all"));
const sort = ref(String(route.query.sort ?? "updated-desc"));
const page = ref(Math.max(1, Number(route.query.page ?? 1) || 1));
const pageSize = 12;
const rowSelection = ref<Record<string, boolean>>({});
const inspectorDirty = ref(false);
const confirmCloseOpen = ref(false);
const openedFromList = ref(false);
const pendingSelection = ref("");
const temporaryFiltersExpanded = ref(false);

const statusItems = [
  { label: "全部状态", value: "all" },
  ...Object.entries(statusLabel).map(([value, label]) => ({ label, value })),
];
const severityItems = [
  { label: "全部级别", value: "all" },
  ...Object.entries(severityLabel).map(([value, label]) => ({ label, value })),
];
const sortItems = [
  { label: "最近更新", value: "updated-desc" },
  { label: "最早更新", value: "updated-asc" },
  { label: "级别优先", value: "severity" },
];

const selectedId = computed(() => String(route.params.incidentId ?? route.query.selected ?? route.query.incident ?? ""));
const selectedIncident = computed(() => incidents.find((item) => item.id === selectedId.value) ?? null);
const fullWorkspace = computed(() => route.name === "incident-detail");
const inspectorOpen = computed(() => !fullWorkspace.value && Boolean(selectedId.value));
const unavailableTarget = computed(() => {
  if (selectedIncident.value || !selectedId.value) return null;
  if (route.query.access === "denied" || selectedId.value === "inc-denied") return { title: "Permission Denied", description: "当前身份无权访问该 Incident；筛选、分页与 Scope 已保留。", icon: "i-lucide-shield-x", color: "error" as const };
  if (selectedId.value === "inc-deleted") return { title: "Incident 已删除", description: "目标已不存在；不会用缓存内容推断当前事实。", icon: "i-lucide-trash-2", color: "warning" as const };
  return { title: "Incident ID 无效", description: "链接目标无法解析；返回列表后仍保留当前查询上下文。", icon: "i-lucide-circle-off", color: "warning" as const };
});

const filtered = computed(() => {
  const query = search.value.trim().toLocaleLowerCase();
  const rows = incidents.filter((item) => {
    if (query && !`${item.summary} ${item.service} ${item.namespace} ${item.id}`.toLocaleLowerCase().includes(query)) return false;
    if (status.value !== "all" && item.status !== status.value) return false;
    if (severity.value !== "all" && item.severity !== severity.value) return false;
    return true;
  });
  const severityOrder: Record<IncidentSeverity, number> = { critical: 0, warning: 1, info: 2 };
  return [...rows].sort((left, right) => {
    if (sort.value === "updated-asc") return left.updatedAt.localeCompare(right.updatedAt);
    if (sort.value === "severity") return severityOrder[left.severity] - severityOrder[right.severity] || right.updatedAt.localeCompare(left.updatedAt);
    return right.updatedAt.localeCompare(left.updatedAt);
  });
});
const totalPages = computed(() => Math.max(1, Math.ceil(filtered.value.length / pageSize)));
const pageRows = computed(() => filtered.value.slice((page.value - 1) * pageSize, page.value * pageSize));
const selectedPageIndex = computed(() => pageRows.value.findIndex((item) => item.id === selectedId.value));
const previousIncident = computed(() => selectedPageIndex.value > 0 ? pageRows.value[selectedPageIndex.value - 1] : null);
const nextIncident = computed(() => selectedPageIndex.value >= 0 && selectedPageIndex.value < pageRows.value.length - 1 ? pageRows.value[selectedPageIndex.value + 1] : null);

function severityColor(value: IncidentSeverity) {
  return value === "critical" ? "error" : value === "warning" ? "warning" : "info";
}

function statusColor(value: IncidentStatus) {
  if (value === "closed") return "neutral";
  if (value === "verifying") return "success";
  if (value === "waiting") return "warning";
  return "info";
}

function openInspector(id: string) {
  if (inspectorDirty.value && selectedId.value && selectedId.value !== id) {
    pendingSelection.value = id;
    confirmCloseOpen.value = true;
    return;
  }
  const query: Record<string, string | string[] | null | undefined> = { ...route.query, selected: id };
  delete query.incident;
  if (selectedId.value) void router.replace({ name: "incidents", query });
  else {
    openedFromList.value = true;
    void router.push({ name: "incidents", query });
  }
}

async function restoreRowFocus(id: string) {
  await nextTick();
  document.querySelector<HTMLElement>(`[data-row-trigger="${CSS.escape(id)}"]`)?.focus({ preventScroll: true });
}

async function closeInspector() {
  const id = selectedId.value;
  inspectorDirty.value = false;
  if (openedFromList.value) {
    openedFromList.value = false;
    await router.back();
  } else {
    const query = { ...route.query };
    delete query.selected;
    delete query.incident;
    await router.replace({ name: "incidents", query });
  }
  await restoreRowFocus(id);
}

function requestInspectorState(open: boolean) {
  if (open) return;
  if (inspectorDirty.value) confirmCloseOpen.value = true;
  else void closeInspector();
}

function discardAndContinue() {
  confirmCloseOpen.value = false;
  inspectorDirty.value = false;
  if (pendingSelection.value) {
    const next = pendingSelection.value;
    pendingSelection.value = "";
    openInspector(next);
    return;
  }
  void closeInspector();
}

function openFullWorkspace() {
  if (!selectedIncident.value) return;
  void router.push({
    name: "incident-detail",
    params: { incidentId: selectedIncident.value.id },
    query: { tab: "evidence", from: "2026-07-30T07:00:00Z", to: "2026-07-30T08:00:00Z" },
  });
}

function syncQuery() {
  const query = { ...route.query };
  if (search.value) query.q = search.value;
  else delete query.q;
  if (status.value !== "all") query.status = status.value;
  else delete query.status;
  if (severity.value !== "all") query.severity = severity.value;
  else delete query.severity;
  if (sort.value !== "updated-desc") query.sort = sort.value;
  else delete query.sort;
  if (page.value > 1) query.page = String(page.value);
  else delete query.page;
  void router.replace({ query });
}

function resetFilters() {
  search.value = "";
  status.value = "all";
  severity.value = "all";
  sort.value = "updated-desc";
  page.value = 1;
}

watch([search, status, severity, sort], () => {
  page.value = 1;
  syncQuery();
});
watch(page, syncQuery);
watch(totalPages, (value) => { if (page.value > value) page.value = value; });
watch(() => route.query.selected, (next, previous) => {
  if (!next && previous) void restoreRowFocus(String(previous));
});
watch(() => route.query.q, (value) => { const next = String(value ?? ""); if (search.value !== next) search.value = next; });
watch(() => route.query.status, (value) => { const next = String(value ?? "all"); if (status.value !== next) status.value = next; });
watch(() => route.query.severity, (value) => { const next = String(value ?? "all"); if (severity.value !== next) severity.value = next; });
watch(() => route.query.sort, (value) => { const next = String(value ?? "updated-desc"); if (sort.value !== next) sort.value = next; });
watch(() => route.query.page, (value) => { const next = Math.max(1, Number(value ?? 1) || 1); if (page.value !== next) page.value = next; });

const columns: TableColumn<IncidentRow>[] = [
  {
    id: "select",
    header: ({ table }) => h(UCheckbox, {
      modelValue: table.getIsSomePageRowsSelected() ? "indeterminate" : table.getIsAllPageRowsSelected(),
      "onUpdate:modelValue": (value: boolean | "indeterminate") => table.toggleAllPageRowsSelected(Boolean(value)),
      "aria-label": "选择当前页全部 Incident",
    }),
    cell: ({ row }) => h(UCheckbox, {
      modelValue: row.getIsSelected(),
      "onUpdate:modelValue": (value: boolean | "indeterminate") => row.toggleSelected(Boolean(value)),
      "aria-label": `选择 ${row.original.id}`,
    }),
    meta: { class: { th: "w-10", td: "w-10" } },
  },
  {
    accessorKey: "severity",
    header: "级别",
    cell: ({ row }) => h(UBadge, { color: severityColor(row.original.severity), variant: "subtle", label: severityLabel[row.original.severity] }),
  },
  {
    accessorKey: "summary",
    header: "Incident",
    cell: ({ row }) => h("div", { class: "incident-cell" }, [
      h("strong", row.original.summary),
      h("small", `${row.original.id} · ${row.original.namespace}`),
    ]),
  },
  {
    accessorKey: "status",
    header: "状态",
    cell: ({ row }) => h(UBadge, { color: statusColor(row.original.status), variant: "soft", label: statusLabel[row.original.status] }),
  },
  { accessorKey: "service", header: "服务" },
  { accessorKey: "evidence", header: "Evidence" },
  {
    accessorKey: "updatedAt",
    header: "最近更新",
    cell: ({ row }) => new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit" }).format(new Date(row.original.updatedAt)),
  },
  {
    id: "inspect",
    header: "",
    cell: ({ row }) => h(UButton, {
      color: "neutral",
      variant: "ghost",
      square: true,
      icon: "i-lucide-panel-right-open",
      "aria-label": `检查 ${row.original.id}`,
      "data-row-trigger": row.original.id,
      "data-testid": `inspect-${row.original.id}`,
      onClick: () => openInspector(row.original.id),
    }),
  },
];
</script>

<template>
  <section class="workspace incident-lab" aria-labelledby="incident-lab-title">
    <header class="workspace-header">
      <div class="workspace-title">
        <h1 id="incident-lab-title" tabindex="-1">Incident 操作面</h1>
        <p>事故 Approval、Delivery 与 Verification 的唯一主入口；DevOps 只保留全局队列与技术明细。</p>
      </div>
      <div class="workspace-actions">
        <UButton color="neutral" variant="outline" icon="i-lucide-refresh-cw" label="刷新投影" />
        <UButton color="primary" variant="solid" icon="i-lucide-bot" label="只读调查" />
      </div>
    </header>

    <div v-if="route.query.compat === 'not-found'" class="status-panel is-warning" role="status">
      <UIcon name="i-lucide-route-off" class="size-5" aria-hidden="true" />
      <div><strong>旧入口已兼容跳转</strong><span>未知路径被带回 Incident，不会静默丢失当前 Scope。</span></div>
    </div>

    <section v-if="fullWorkspace && selectedIncident" class="content-band full-incident" aria-label="完整 Incident 工作页">
      <div class="section-band">
        <UButton color="neutral" variant="ghost" icon="i-lucide-arrow-left" label="返回 Incident 列表" @click="router.back()" />
        <div class="section-heading"><h2>{{ selectedIncident.summary }}</h2><UBadge :color="severityColor(selectedIncident.severity)" :label="severityLabel[selectedIncident.severity]" /></div>
        <code class="hash-value">{{ selectedIncident.hash }}</code>
      </div>
      <div class="section-band">
        <div class="state-grid" aria-label="事故执行状态">
          <div class="state-step"><strong><UIcon name="i-lucide-file-check-2" />Accepted</strong><small>Owner 已接受精确 hash</small></div>
          <div class="state-step"><strong><UIcon name="i-lucide-send" />Dispatched</strong><small>执行请求已派发</small></div>
          <div class="state-step is-current"><strong><UIcon name="i-lucide-scan-eye" />Observed</strong><small>等待当前 Provider 观察</small></div>
          <div class="state-step"><strong><UIcon name="i-lucide-badge-check" />Verified</strong><small>尚无当前 Verification 支持</small></div>
        </div>
      </div>
    </section>

    <template v-else-if="!fullWorkspace">
      <section class="toolbar-band" aria-label="Incident 筛选与排序">
        <div class="toolbar-group is-search"><span class="toolbar-label">搜索</span><UInput v-model="search" icon="i-lucide-search" placeholder="Incident、服务、Namespace 或 ID" data-testid="incident-search" /></div>
        <div class="toolbar-group"><span class="toolbar-label">状态</span><USelect v-model="status" :items="statusItems" value-key="value" data-testid="incident-status" /></div>
        <div class="toolbar-group"><span class="toolbar-label">级别</span><USelect v-model="severity" :items="severityItems" value-key="value" data-testid="incident-severity" /></div>
        <div class="toolbar-group"><span class="toolbar-label">排序</span><USelect v-model="sort" :items="sortItems" value-key="value" data-testid="incident-sort" /></div>
        <UTooltip text="临时筛选面板不会进入 URL">
          <UButton color="neutral" variant="outline" :icon="temporaryFiltersExpanded ? 'i-lucide-chevron-up' : 'i-lucide-sliders-horizontal'" label="临时条件" @click="temporaryFiltersExpanded = !temporaryFiltersExpanded" />
        </UTooltip>
        <UButton color="neutral" variant="ghost" icon="i-lucide-filter-x" label="清除" @click="resetFilters" />
      </section>
      <div v-if="temporaryFiltersExpanded" class="section-band" data-testid="temporary-filter-panel">
        <UAlert color="neutral" variant="subtle" icon="i-lucide-info" title="临时 UI 状态" description="本面板的展开状态不会写入 URL；筛选、排序与分页会写入。" />
      </div>
      <section class="content-band" aria-label="Incident 表格">
        <UTable v-model:row-selection="rowSelection" :data="pageRows" :columns="columns" sticky class="dense-table" data-testid="incident-table" />
        <div class="pagination-band">
          <span>{{ filtered.length }} 项 · 已选 {{ Object.values(rowSelection).filter(Boolean).length }} 项</span>
          <UPagination v-model:page="page" :total="filtered.length" :items-per-page="pageSize" :sibling-count="1" active-color="primary" />
        </div>
      </section>
    </template>
    <section v-else class="content-band unavailable-workspace" data-testid="incident-detail-unavailable">
      <UAlert :color="unavailableTarget?.color ?? 'warning'" variant="soft" :icon="unavailableTarget?.icon ?? 'i-lucide-circle-off'" :title="unavailableTarget?.title ?? 'Incident 不可用'" :description="unavailableTarget?.description ?? '返回 Incident 列表继续调查。'" />
      <UButton color="neutral" variant="outline" icon="i-lucide-arrow-left" label="返回 Incident 列表" @click="router.push({ name: 'incidents' })" />
    </section>

    <USlideover
      :open="inspectorOpen"
      :title="selectedIncident?.summary ?? 'Incident 不存在'"
      :description="selectedIncident ? `${selectedIncident.id} · ${selectedIncident.service}` : '目标可能已删除或无权访问'"
      :dismissible="!inspectorDirty"
      :close-icon="'i-lucide-x'"
      :ui="{ content: 'w-[min(560px,48vw)] max-w-none' }"
      data-testid="incident-inspector"
      @update:open="requestInspectorState"
      @close:prevent="confirmCloseOpen = true"
    >
      <template #body>
        <div v-if="selectedIncident" class="inspector-body">
          <div class="state-grid" aria-label="Incident 状态因果链">
            <div class="state-step"><strong><UIcon name="i-lucide-file-check-2" />Accepted</strong><small>hash 已锁定</small></div>
            <div class="state-step"><strong><UIcon name="i-lucide-send" />Dispatched</strong><small>已派发</small></div>
            <div class="state-step is-current"><strong><UIcon name="i-lucide-scan-eye" />Observed</strong><small>尚未闭合</small></div>
            <div class="state-step"><strong><UIcon name="i-lucide-badge-check" />Verified</strong><small>不可声明成功</small></div>
          </div>
          <dl>
            <div class="data-pair"><dt>Operational Scope</dt><dd>cloudops-local / {{ selectedIncident.namespace }} / {{ selectedIncident.service }}</dd></div>
            <div class="data-pair"><dt>Owner</dt><dd>{{ selectedIncident.owner }}</dd></div>
            <div class="data-pair"><dt>Evidence</dt><dd>{{ selectedIncident.evidence }} 项，来源与采集时间保持可见</dd></div>
            <div class="data-pair"><dt>Exact hash</dt><dd><code>{{ selectedIncident.hash }}</code></dd></div>
          </dl>
          <code class="hash-value">{{ selectedIncident.hash }}</code>
          <UCheckbox v-model="inspectorDirty" label="模拟 Inspector 中存在未保存备注" data-testid="inspector-dirty" />
        </div>
        <UAlert v-else :color="unavailableTarget?.color ?? 'warning'" variant="soft" :icon="unavailableTarget?.icon ?? 'i-lucide-circle-off'" :title="unavailableTarget?.title ?? '目标不可用'" :description="unavailableTarget?.description ?? '保留筛选、分页和 Scope，不推断目标事实。'" data-testid="incident-unavailable" />
      </template>
      <template #footer>
        <div class="inspector-footer">
          <div class="inspector-scan-actions" aria-label="快速检查 Incident">
            <UTooltip text="检查上一个 Incident">
              <UButton
                color="neutral"
                variant="ghost"
                square
                icon="i-lucide-chevron-up"
                aria-label="检查上一个 Incident"
                data-testid="inspector-previous"
                :disabled="!previousIncident"
                @click="previousIncident && openInspector(previousIncident.id)"
              />
            </UTooltip>
            <UTooltip text="检查下一个 Incident">
              <UButton
                color="neutral"
                variant="ghost"
                square
                icon="i-lucide-chevron-down"
                aria-label="检查下一个 Incident"
                data-testid="inspector-next"
                :disabled="!nextIncident"
                @click="nextIncident && openInspector(nextIncident.id)"
              />
            </UTooltip>
          </div>
          <UButton color="neutral" variant="outline" icon="i-lucide-x" label="关闭" @click="requestInspectorState(false)" />
          <UButton color="primary" icon="i-lucide-arrow-up-right" label="进入完整工作页" :disabled="!selectedIncident" @click="openFullWorkspace" />
        </div>
      </template>
    </USlideover>

    <UModal :open="confirmCloseOpen" title="放弃未保存的 Inspector 编辑？" description="关闭后仍会保留 URL 筛选、分页与 Scope。" :dismissible="false" :close="false" data-testid="dirty-close-modal">
      <template #body>
        <UAlert color="warning" variant="soft" icon="i-lucide-triangle-alert" title="未保存内容不会伪造后端 Draft" description="此原型只验证前端离开保护。" />
      </template>
      <template #footer>
        <div class="modal-actions">
          <UButton color="neutral" variant="outline" icon="i-lucide-arrow-left" label="继续编辑" data-testid="continue-editing" @click="confirmCloseOpen = false" />
          <UButton color="error" icon="i-lucide-trash-2" label="放弃并继续" data-testid="discard-inspector" @click="discardAndContinue" />
        </div>
      </template>
    </UModal>
  </section>
</template>

<style scoped>
.incident-lab { max-width: 1680px; margin: 0 auto; }
.incident-cell { display: grid; min-width: 260px; max-width: 620px; gap: 2px; }
.incident-cell strong { overflow: hidden; color: var(--co-text-primary); text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.incident-cell small { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; }
.dense-table :deep(th) { height: 36px; color: var(--co-text-secondary); background: var(--co-surface-muted); font-size: 10px; text-transform: uppercase; }
.dense-table :deep(td) { height: var(--co-row-height); padding-block: 5px; border-color: var(--co-border); font-size: 11px; }
.dense-table :deep(tr:hover td) { background: var(--co-selected); }
.inspector-body { display: grid; gap: var(--co-space-4); }
.inspector-footer, .modal-actions { display: flex; width: 100%; justify-content: flex-end; gap: var(--co-space-2); }
.inspector-scan-actions { display: flex; margin-right: auto; gap: 2px; }
.full-incident { display: grid; gap: 0; }
.unavailable-workspace { display: grid; gap: var(--co-space-3); padding: var(--co-space-4); }
@media (max-width: 1100px) {
  .incident-cell { min-width: 210px; }
  .dense-table :deep(th:nth-child(6)), .dense-table :deep(td:nth-child(6)) { display: none; }
}
</style>
