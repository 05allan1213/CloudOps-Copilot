<script setup lang="ts">
import type { TableColumn } from "@nuxt/ui";
import { computed, h, nextTick, reactive, ref, resolveComponent, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { ApiError } from "../../../src/api/client";
import ApiErrorNotice from "../../../src/components/workspace/ApiErrorNotice.vue";
import ContextToolbar from "../../../src/components/workspace/ContextToolbar.vue";
import DenseDataTable, { type DenseTableColumn } from "../../../src/components/workspace/DenseDataTable.vue";
import RealtimeTrustStatus from "../../../src/components/workspace/RealtimeTrustStatus.vue";
import RiskConfirmation from "../../../src/components/workspace/RiskConfirmation.vue";
import WorkspaceHeader from "../../../src/components/workspace/WorkspaceHeader.vue";
import WorkspaceInspector, { type InspectorTargetState } from "../../../src/components/workspace/WorkspaceInspector.vue";
import WorkspaceOperationProgress from "../../../src/components/workspace/WorkspaceOperationProgress.vue";
import WorkspaceState from "../../../src/components/workspace/WorkspaceState.vue";
import type {
  RealtimeTrustState,
  RiskConfirmationFacts,
  RiskConfirmationKind,
  WorkspaceStateKind,
} from "../../../src/components/workspace/workspacePresentation";
import { useTheme } from "../../../src/composables/useTheme";
import { useLatestAsync } from "../../../src/composables/useLatestAsync";
import { useWorkspaceInspector } from "../../../src/composables/useWorkspaceInspector";
import {
  createWorkspaceQueryCodec,
  enumQueryField,
  integerQueryField,
  stringQueryField,
} from "../../../src/composables/useWorkspaceQuery";
import { createWorkspaceRows, type WorkspaceFixtureRow } from "./fixtureData";

interface FixtureQueryState {
  search: string;
  sort: "updated-desc" | "updated-asc";
  page: number;
  tab: "resources" | "states";
  selected: string;
}

interface DenseDataTableHandle {
  getRowElement: (rowID: string) => HTMLElement | null;
  getScrollElement: () => HTMLElement | null;
}

const route = useRoute();
const router = useRouter();
const UBadge = resolveComponent("UBadge");
const rows = shallowRef(createWorkspaceRows());
const queryCodec = createWorkspaceQueryCodec<FixtureQueryState>({
  search: stringQueryField("search"),
  sort: enumQueryField("sort", ["updated-desc", "updated-asc"], "updated-desc", ["order"]),
  page: integerQueryField("page", { defaultValue: 1, min: 1, max: 500 }),
  tab: enumQueryField("tab", ["resources", "states"], "resources"),
  selected: stringQueryField("selected", { aliases: ["resource"] }),
}, { transientKeys: ["hover", "menu", "columns", "panel"] });
const queryState = reactive(queryCodec.decode(route.query));
const temporaryPanelOpen = ref(false);
const stateKind = ref<WorkspaceStateKind>("partial");
const denseTable = ref<DenseDataTableHandle | null>(null);
const realtimeState = ref<RealtimeTrustState>("connecting");
const newItems = ref(17);
const inspectorDirty = ref(false);
const dirtyCloseOpen = ref(false);
const riskOpen = ref(false);
const riskKind = ref<RiskConfirmationKind>("acknowledgement");
const riskFacts = ref<RiskConfirmationFacts>({
  target: "deployment/cloudops-api",
  effect: "记录 Owner 已知悉当前只读告警。",
});
const updatedRows = ref(0);
const projectionRevision = ref(12);
const projectionRefresh = useLatestAsync<string>();
projectionRefresh.data.value = `Provider projection revision ${projectionRevision.value}`;
const submitAttempt = useLatestAsync<void>();
const draftInput = ref("scope=cluster/local resource=deployment/cloudops-api");
const operationActive = ref(true);
const { isDark, toggleTheme } = useTheme();
const inspector = useWorkspaceInspector({
  selectedKey: "selected",
  legacySelectedKeys: ["resource"],
  scrollElement: () => denseTable.value?.getScrollElement() ?? null,
  resolveTrigger: (rowID) => denseTable.value?.getRowElement(rowID) ?? null,
});

const search = computed({
  get: () => queryState.search,
  set: (value: string) => { queryState.search = value; queryState.page = 1; syncQuery(); },
});
const sort = computed({
  get: () => queryState.sort,
  set: (value: FixtureQueryState["sort"]) => { queryState.sort = value; syncQuery(); },
});
const tab = computed({
  get: () => queryState.tab,
  set: (value: FixtureQueryState["tab"]) => { queryState.tab = value; syncQuery(); },
});
const filteredRows = computed(() => {
  const normalized = queryState.search.trim().toLocaleLowerCase();
  const source = normalized
    ? rows.value.filter((row) => `${row.id} ${row.resource} ${row.namespace} ${row.provider}`.toLocaleLowerCase().includes(normalized))
    : rows.value;
  return queryState.sort === "updated-asc" ? source : [...source].reverse();
});
const selectedRow = computed(() => rows.value.find((row) => row.id === inspector.selectedID.value) ?? null);
const targetState = computed<InspectorTargetState>(() => {
  if (!inspector.selectedID.value || selectedRow.value) return "ready";
  if (inspector.selectedID.value === "resource-deleted") return "deleted";
  if (inspector.selectedID.value === "resource-denied") return "permission-denied";
  if (inspector.selectedID.value === "resource-expired") return "expired";
  return "invalid";
});
const targetDescription = computed(() => {
  if (targetState.value === "deleted") return "目标已删除；列表筛选、分页和 Scope 保持不变。";
  if (targetState.value === "permission-denied") return "当前身份缺少 resource.read；不会自动选择第一行。";
  if (targetState.value === "expired") return "当前 authority 已过期，旧 hash 不会继续使用。";
  if (targetState.value === "invalid") return "链接中的资源 ID 无法解析；不会推断替代目标。";
  return "";
});
const fullPage = computed(() => route.name === "workspace-full");
const fullPageID = computed(() => String(route.params.id ?? ""));
const visibleStateKinds: { label: string; value: WorkspaceStateKind }[] = [
  { label: "Loading", value: "loading" },
  { label: "Empty", value: "empty" },
  { label: "Error", value: "error" },
  { label: "Partial", value: "partial" },
  { label: "Stale", value: "stale" },
  { label: "Disconnected", value: "disconnected" },
  { label: "Permission Denied", value: "permission-denied" },
  { label: "Expired", value: "expired" },
];
const sortItems = [
  { label: "最近更新", value: "updated-desc" },
  { label: "最早更新", value: "updated-asc" },
];
const tabItems = [
  { label: "资源表格", value: "resources", icon: "i-lucide-table-properties" },
  { label: "异常状态", value: "states", icon: "i-lucide-triangle-alert" },
];
const syntheticError = new ApiError(
  "Kubernetes Provider 返回的对象版本与当前读取上下文不一致；已加载数据保持可见。",
  409,
  "PROVIDER_REVISION_CONFLICT_WITH_A_VERY_LONG_ERROR_CODE",
  "req-8d9112caa4104ea3b75cc36cb9f75931",
  "4d5f196cf4c548b78acc66e80b915c91",
  false,
  ["刷新当前 Provider 投影", "核对 Operational Scope 后重试只读请求"],
);

function statusColor(value: WorkspaceFixtureRow["status"]) {
  if (value === "available") return "success";
  if (value === "partial") return "warning";
  return "neutral";
}

const columns: DenseTableColumn<WorkspaceFixtureRow>[] = [
  {
    id: "severity",
    accessorKey: "severity",
    label: "级别",
    header: "级别",
    size: 88,
    cell: ({ row }) => h(UBadge, {
      color: row.original.severity === "critical" ? "error" : row.original.severity === "warning" ? "warning" : "info",
      variant: "soft",
      label: row.original.severityLabel,
    }),
  } as TableColumn<WorkspaceFixtureRow> & DenseTableColumn<WorkspaceFixtureRow>,
  {
    id: "resource",
    accessorKey: "resource",
    label: "资源",
    header: "资源",
    size: 300,
    cell: ({ row }) => h("div", { class: "fixture-resource-cell" }, [
      h("strong", row.original.resource),
      h("code", row.original.id),
    ]),
  },
  {
    id: "status",
    accessorKey: "status",
    label: "状态",
    header: "状态",
    size: 112,
    cell: ({ row }) => h(UBadge, {
      color: statusColor(row.original.status),
      variant: "soft",
      label: row.original.status === "available" ? "可用" : row.original.status === "partial" ? "部分结果" : "Stale",
    }),
  },
  { id: "namespace", accessorKey: "namespace", label: "Namespace", header: "Namespace", size: 144 },
  { id: "provider", accessorKey: "provider", label: "Provider", header: "Provider", size: 144, optional: true },
  { id: "owner", accessorKey: "owner", label: "Owner", header: "Owner", size: 128, optional: true },
  {
    id: "updatedAt",
    accessorKey: "updatedAt",
    label: "精确 UTC",
    header: "精确 UTC",
    size: 216,
    optional: true,
    cell: ({ row }) => h("time", { datetime: row.original.updatedAt, class: "fixture-mono" }, row.original.updatedAt),
  },
  {
    id: "exactHash",
    accessorKey: "exactHash",
    label: "Exact hash",
    header: "Exact hash",
    size: 360,
    optional: true,
    cell: ({ row }) => h("code", { class: "fixture-long-hash", title: row.original.exactHash }, row.original.exactHash),
  },
];

function syncQuery() {
  void router.replace({
    path: route.path,
    query: queryCodec.encode(queryState, route.query),
    hash: route.hash,
  });
}

function changePage(delta: number) {
  queryState.page = Math.min(500, Math.max(1, queryState.page + delta));
  syncQuery();
}

function openRow(row: WorkspaceFixtureRow, trigger: HTMLElement | null) {
  void inspector.open(row.id, trigger);
}

function openUnavailable(id: string) {
  void inspector.open(id, document.activeElement instanceof HTMLElement ? document.activeElement : null);
}

function requestInspectorOpen(value: boolean) {
  if (!value) void inspector.close();
}

async function discardInspectorChanges() {
  dirtyCloseOpen.value = false;
  inspectorDirty.value = false;
  await nextTick();
  await inspector.close();
}

function openFullWorkspace() {
  if (!selectedRow.value) return;
  void inspector.openFull({
    name: "workspace-full",
    params: { id: selectedRow.value.id },
    query: { search: queryState.search || undefined, sort: queryState.sort },
  });
}

function configureRisk(kind: RiskConfirmationKind) {
  riskKind.value = kind;
  riskFacts.value = kind === "acknowledgement"
    ? { target: "alert/cloudops-api", effect: "记录 Owner 已知悉当前只读告警。" }
    : kind === "configuration"
      ? { target: "provider/prometheus", effect: "将查询超时调整为 15 秒。", recovery: "可恢复为当前 Revision 12。" }
      : kind === "approval"
        ? { target: "deployment/cloudops-api", effect: "批准候选版本进入交付流程。", authority: "owner-local", exactHash: "sha256:43c5e6ab8" }
        : kind === "rollback"
          ? { target: "deployment/cloudops-api", effect: "回滚到上一稳定版本。", authority: "owner-local", version: "revision-12", recovery: "回滚后仍需独立 Verification。" }
          : { target: "operation/op-20260731", effect: "立即停止当前 Provider 操作。", authority: "owner-local", version: "attempt-7", irreversible: "已完成的 Provider side effect 不会自动撤销。" };
  riskOpen.value = true;
}

function updateExistingRow() {
  const first = rows.value[0];
  if (!first) return;
  rows.value = [{ ...first, status: first.status === "available" ? "partial" : "available" }, ...rows.value.slice(1)];
  updatedRows.value += 1;
}

function confirmRisk() {
  riskOpen.value = false;
}

function fixtureDelay(signal: AbortSignal, milliseconds: number): Promise<void> {
  return new Promise((resolve, reject) => {
    const onAbort = () => {
      window.clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    };
    const timer = window.setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, milliseconds);
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

async function refreshProjection() {
  await projectionRefresh.run(async ({ signal }) => {
    await fixtureDelay(signal, 500);
    projectionRevision.value += 1;
    return `Provider projection revision ${projectionRevision.value}`;
  }, { background: true });
}

async function submitFixtureDraft() {
  await submitAttempt.run(async ({ signal }) => {
    await fixtureDelay(signal, 500);
    throw new Error("模拟提交失败；输入和当前投影保持不变。");
  });
}

watch(() => route.query, (query) => Object.assign(queryState, queryCodec.decode(query)));
</script>

<template>
  <UApp>
    <a
      class="fixture-skip-link"
      href="#workspace-fixture-main"
    >跳到主要内容</a>
    <div class="workspace-fixture-shell">
      <header class="fixture-topbar">
        <div>
          <strong>CloudOps</strong>
          <span>Workspace foundation fixture</span>
        </div>
        <UButton
          color="neutral"
          variant="ghost"
          :icon="isDark ? 'i-lucide-sun' : 'i-lucide-moon'"
          :label="isDark ? '浅色' : '深色'"
          :aria-label="isDark ? '切换浅色主题' : '切换深色主题'"
          @click="toggleTheme"
        />
      </header>
      <main
        id="workspace-fixture-main"
        :class="{ 'has-workspace-inspector': !fullPage && Boolean(inspector.selectedID.value) }"
        tabindex="-1"
      >
        <template v-if="fullPage">
          <WorkspaceHeader
            eyebrow="完整工作页 push contract"
            :title="`资源完整工作页 · ${fullPageID}`"
            description="完整调查使用 push；浏览器 Back 返回列表与 Inspector 上下文。"
          >
            <template #actions>
              <UButton
                color="neutral"
                variant="outline"
                icon="i-lucide-arrow-left"
                label="返回"
                @click="router.back()"
              />
            </template>
          </WorkspaceHeader>
          <section
            class="fixture-band fixture-full-page"
            aria-label="完整资源事实"
          >
            <h2>当前事实</h2>
            <dl>
              <div><dt>Resource ID</dt><dd>{{ fullPageID }}</dd></div>
              <div><dt>Observed UTC</dt><dd>2026-07-31T08:00:00Z</dd></div>
              <div><dt>状态</dt><dd>Observed，不推断 Verified</dd></div>
            </dl>
          </section>
        </template>

        <template v-else>
          <WorkspaceHeader
            eyebrow="共享 Workspace 基础"
            title="资源可信度与 Inspector"
            description="专用 fixture 导入真实生产组件；不注册任何正式 Workspace route。"
          >
            <template #context>
              <UBadge
                color="neutral"
                variant="soft"
                icon="i-lucide-server"
                label="cluster/local"
              />
              <UBadge
                color="info"
                variant="soft"
                icon="i-lucide-database"
                label="20,000 rows"
              />
              <span class="fixture-update-count">existing rows updated {{ updatedRows }}</span>
            </template>
            <template #actions>
              <UButton
                color="neutral"
                variant="outline"
                icon="i-lucide-refresh-cw"
                label="就地更新现有行"
                @click="updateExistingRow"
              />
              <UButton
                color="primary"
                icon="i-lucide-search"
                label="只读调查"
              />
            </template>
          </WorkspaceHeader>

          <ContextToolbar
            label="资源筛选与操作"
            tabbed
          >
            <template #tabs>
              <UTabs
                v-model="tab"
                :items="tabItems"
                value-key="value"
                size="sm"
              />
            </template>
            <template #filters>
              <UInput
                v-model="search"
                class="fixture-search"
                icon="i-lucide-search"
                placeholder="资源、Namespace、Provider"
                aria-label="搜索资源"
              />
              <USelect
                v-model="sort"
                :items="sortItems"
                value-key="value"
                aria-label="排序"
              />
              <div
                class="fixture-page-stepper"
                aria-label="分页"
              >
                <UButton
                  color="neutral"
                  variant="ghost"
                  icon="i-lucide-chevron-left"
                  square
                  aria-label="上一页"
                  :disabled="queryState.page === 1"
                  @click="changePage(-1)"
                />
                <span>第 {{ queryState.page }} 页</span>
                <UButton
                  color="neutral"
                  variant="ghost"
                  icon="i-lucide-chevron-right"
                  square
                  aria-label="下一页"
                  @click="changePage(1)"
                />
              </div>
            </template>
            <template #secondary>
              <UButton
                color="neutral"
                variant="ghost"
                icon="i-lucide-sliders-horizontal"
                label="临时面板"
                :aria-expanded="temporaryPanelOpen"
                @click="temporaryPanelOpen = !temporaryPanelOpen"
              />
            </template>
            <template #primary>
              <UButton
                color="primary"
                icon="i-lucide-panel-right-open"
                label="打开异常目标"
                @click="openUnavailable('resource-denied')"
              />
            </template>
          </ContextToolbar>

          <WorkspaceState
            v-if="temporaryPanelOpen"
            kind="empty"
            title="临时 UI 状态"
            description="面板展开、列偏好、hover 和菜单状态不写入 URL。"
          />

          <RealtimeTrustStatus
            :state="realtimeState"
            cursor="cursor-1042"
            last-continuous-at="2026-07-31T08:00:00Z"
            detail="新行由用户主控加载；现有行就地更新。"
            :new-items="newItems"
            @load-new="newItems = 0"
          />

          <section
            class="fixture-fault-controls"
            aria-label="SSE 故障展示"
          >
            <UButton
              color="info"
              variant="outline"
              icon="i-lucide-loader-circle"
              label="Connecting"
              @click="realtimeState = 'connecting'"
            />
            <UButton
              color="success"
              variant="outline"
              icon="i-lucide-radio"
              label="Live"
              @click="realtimeState = 'live'"
            />
            <UButton
              color="warning"
              variant="outline"
              icon="i-lucide-refresh-cw"
              label="Reconnecting"
              @click="realtimeState = 'reconnecting'"
            />
            <UButton
              color="warning"
              variant="outline"
              icon="i-lucide-unplug"
              label="Disconnected"
              @click="realtimeState = 'disconnected'"
            />
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-cloud-off"
              label="Stale"
              @click="realtimeState = 'stale'"
            />
            <UButton
              color="error"
              variant="outline"
              icon="i-lucide-history"
              label="Cursor expired"
              @click="realtimeState = 'cursor-expired'"
            />
            <UButton
              color="info"
              variant="outline"
              icon="i-lucide-list-restart"
              label="Resyncing"
              @click="realtimeState = 'resyncing'"
            />
            <UButton
              color="error"
              variant="outline"
              icon="i-lucide-circle-x"
              label="Resync failed"
              @click="realtimeState = 'resync-failed'"
            />
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-power"
              label="Stopped"
              @click="realtimeState = 'stopped'"
            />
          </section>

          <template v-if="tab === 'resources'">
            <DenseDataTable
              ref="denseTable"
              :rows="filteredRows"
              :columns="columns"
              :row-key="(row: WorkspaceFixtureRow) => row.id"
              storage-key="workspace-foundation-fixture"
              caption="20,000 行资源可信度 fixture"
              :critical-column-ids="['severity', 'resource', 'status']"
              :selected-id="inspector.selectedID.value"
              :severity="(row: WorkspaceFixtureRow) => row.severity"
              :copy-value="(row: WorkspaceFixtureRow) => row.fullValue"
              virtualized
              @select="openRow"
            />
          </template>

          <section
            v-else
            class="fixture-state-grid"
            aria-label="异常状态展示组合"
          >
            <nav aria-label="异常状态选择">
              <USelect
                v-model="stateKind"
                :items="visibleStateKinds"
                value-key="value"
                aria-label="展示状态"
              />
              <UButton
                color="neutral"
                variant="ghost"
                icon="i-lucide-circle-off"
                label="Invalid target"
                @click="openUnavailable('resource-not-valid')"
              />
              <UButton
                color="neutral"
                variant="ghost"
                icon="i-lucide-trash-2"
                label="Deleted target"
                @click="openUnavailable('resource-deleted')"
              />
              <UButton
                color="neutral"
                variant="ghost"
                icon="i-lucide-key-round"
                label="Expired target"
                @click="openUnavailable('resource-expired')"
              />
            </nav>
            <div class="fixture-state-content">
              <WorkspaceState :kind="stateKind" />
              <ApiErrorNotice
                :error="syntheticError"
                retryable
              />
            </div>
          </section>

          <section
            class="fixture-async-band"
            aria-labelledby="fixture-async-title"
          >
            <div class="fixture-async-heading">
              <span>Async lifecycle presentation</span>
              <h2 id="fixture-async-title">
                异步反馈与上下文保留
              </h2>
            </div>
            <div class="fixture-async-grid">
              <div class="fixture-async-cell">
                <span>当前投影保持可见</span>
                <strong>{{ projectionRefresh.data.value }}</strong>
                <span
                  v-if="projectionRefresh.refreshing.value"
                  role="status"
                >后台刷新中，现有内容保持可读</span>
                <UButton
                  color="neutral"
                  variant="outline"
                  icon="i-lucide-refresh-cw"
                  label="后台刷新"
                  :loading="projectionRefresh.refreshing.value"
                  :disabled="projectionRefresh.refreshing.value"
                  @click="refreshProjection"
                />
              </div>
              <div class="fixture-async-cell">
                <label for="fixture-submit-draft">待提交输入</label>
                <UInput
                  id="fixture-submit-draft"
                  v-model="draftInput"
                  class="fixture-async-input"
                  aria-label="待提交输入"
                />
                <UButton
                  color="primary"
                  icon="i-lucide-send"
                  label="模拟提交失败"
                  :loading="submitAttempt.loading.value"
                  :disabled="submitAttempt.loading.value"
                  @click="submitFixtureDraft"
                />
                <ApiErrorNotice
                  v-if="submitAttempt.error.value"
                  :error="submitAttempt.error.value"
                  title="提交失败，输入已保留"
                />
              </div>
              <div class="fixture-async-cell">
                <WorkspaceOperationProgress
                  v-if="operationActive"
                  stage="等待 Provider observed"
                  :elapsed-seconds="42"
                  description="Accepted 不等于 Verified；仅在领域契约允许时显示取消。"
                  cancellable
                  @cancel="operationActive = false"
                />
                <WorkspaceState
                  v-else
                  kind="partial"
                  title="取消已请求"
                  description="当前 fixture 只验证呈现，不推断 Provider 已停止。"
                />
              </div>
            </div>
          </section>

          <section
            class="fixture-risk-band"
            aria-labelledby="fixture-risk-title"
          >
            <div>
              <span>Risk confirmation compositions</span>
              <h2 id="fixture-risk-title">
                按后果分级确认
              </h2>
            </div>
            <div>
              <UButton
                color="neutral"
                variant="outline"
                label="Acknowledgement"
                @click="configureRisk('acknowledgement')"
              />
              <UButton
                color="warning"
                variant="outline"
                label="Configuration"
                @click="configureRisk('configuration')"
              />
              <UButton
                color="warning"
                variant="outline"
                label="Approval"
                @click="configureRisk('approval')"
              />
              <UButton
                color="error"
                variant="outline"
                label="Rollback"
                @click="configureRisk('rollback')"
              />
              <UButton
                color="error"
                label="Forced termination"
                @click="configureRisk('forced-termination')"
              />
            </div>
          </section>
        </template>
      </main>
    </div>

    <WorkspaceInspector
      :open="!fullPage && Boolean(inspector.selectedID.value)"
      :title="selectedRow?.resource ?? '资源目标不可用'"
      :description="selectedRow ? `${selectedRow.id} · ${selectedRow.provider}` : inspector.selectedID.value"
      :target-state="targetState"
      :target-description="targetDescription"
      :dirty="inspectorDirty"
      :trigger="inspector.triggerElement.value"
      @update:open="requestInspectorOpen"
      @close-prevent="dirtyCloseOpen = true"
    >
      <template v-if="selectedRow">
        <dl class="fixture-inspector-facts">
          <div><dt>Operational Scope</dt><dd>cluster/local · {{ selectedRow.namespace }}</dd></div>
          <div><dt>Provider</dt><dd>{{ selectedRow.provider }}</dd></div>
          <div><dt>Observed UTC</dt><dd>{{ selectedRow.updatedAt }}</dd></div>
          <div><dt>Exact hash</dt><dd>{{ selectedRow.exactHash }}</dd></div>
        </dl>
        <UButton
          color="neutral"
          variant="outline"
          :icon="inspectorDirty ? 'i-lucide-file-warning' : 'i-lucide-file-pen-line'"
          :label="inspectorDirty ? '存在未应用编辑' : '模拟未应用编辑'"
          @click="inspectorDirty = !inspectorDirty"
        />
      </template>
      <template #recovery>
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-arrow-left"
          label="关闭并保留列表"
          @click="inspector.close"
        />
      </template>
      <template #footer>
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-x"
          label="关闭"
          @click="requestInspectorOpen(false)"
        />
        <UButton
          color="primary"
          icon="i-lucide-arrow-up-right"
          label="进入完整工作页"
          :disabled="!selectedRow"
          @click="openFullWorkspace"
        />
      </template>
    </WorkspaceInspector>

    <UModal
      :open="dirtyCloseOpen"
      title="放弃未应用的 Inspector 编辑？"
      description="关闭后仍保留筛选、分页、滚动和触发元素 Focus。"
      :dismissible="false"
      :close="false"
    >
      <template #body>
        <WorkspaceState
          kind="partial"
          title="前端本地编辑尚未应用"
          description="不会伪造后端 Draft 或静默丢弃输入。"
        />
      </template>
      <template #footer>
        <div class="fixture-modal-actions">
          <UButton
            color="neutral"
            variant="outline"
            label="继续编辑"
            @click="dirtyCloseOpen = false"
          />
          <UButton
            color="error"
            icon="i-lucide-trash-2"
            label="放弃并关闭"
            @click="discardInspectorChanges"
          />
        </div>
      </template>
    </UModal>

    <RiskConfirmation
      :open="riskOpen"
      :kind="riskKind"
      :facts="riskFacts"
      @update:open="riskOpen = $event"
      @confirm="confirmRisk"
    />
  </UApp>
</template>

<style>
.workspace-fixture-shell { min-height: 100dvh; background: var(--co-bg-canvas); }
.fixture-topbar {
  position: sticky;
  top: 0;
  z-index: var(--co-z-header);
  display: flex;
  min-height: var(--co-header-height);
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
  padding: 0 var(--co-space-4);
  border-bottom: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}
.fixture-topbar > div { display: grid; }
.fixture-topbar strong { font-size: 13px; }
.fixture-topbar span { color: var(--co-text-muted); font-size: 10px; }
.workspace-fixture-shell main {
  min-width: 0;
  max-width: var(--co-content-max-width);
  margin: 0 auto;
  padding: var(--co-space-5);
  transition: margin-right var(--co-motion-standard) var(--co-ease-out);
}
.workspace-fixture-shell main.has-workspace-inspector {
  max-width: none;
  margin-right: var(--co-inspector-max-width);
  margin-left: 0;
}
.fixture-skip-link {
  position: fixed;
  top: var(--co-space-2);
  left: var(--co-space-2);
  z-index: var(--co-z-skip-link);
  padding: var(--co-space-2) var(--co-space-3);
  color: var(--co-text-on-action);
  background: var(--co-action-primary);
  transform: translateY(-180%);
}
.fixture-skip-link:focus { transform: translateY(0); }
.fixture-search { min-width: min(320px, 100%); flex: 1 1 280px; }
.fixture-page-stepper { display: flex; align-items: center; gap: var(--co-space-1); color: var(--co-text-secondary); font-size: 11px; }
.fixture-update-count { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; }
.fixture-fault-controls,
.fixture-risk-band,
.fixture-risk-band > div:last-child,
.fixture-modal-actions { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-2); }
.fixture-fault-controls { padding: var(--co-space-2) var(--co-space-3); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-subtle); }
.fixture-async-band { border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.fixture-async-heading { padding: var(--co-space-3) var(--co-space-4); }
.fixture-async-heading span,
.fixture-async-cell > span,
.fixture-async-cell > label { color: var(--co-text-muted); font-size: 10px; }
.fixture-async-heading h2 { margin: var(--co-space-1) 0 0; font-size: 15px; }
.fixture-async-grid { display: grid; min-width: 0; grid-template-columns: repeat(2, minmax(0, 1fr)); border-top: 1px solid var(--co-border-default); }
.fixture-async-cell { display: flex; min-width: 0; flex-wrap: wrap; align-content: start; align-items: center; gap: var(--co-space-2); padding: var(--co-space-3) var(--co-space-4); }
.fixture-async-cell:nth-child(even) { border-left: 1px solid var(--co-border-default); }
.fixture-async-cell:last-child { grid-column: 1 / -1; border-top: 1px solid var(--co-border-default); }
.fixture-async-cell strong { min-width: 0; overflow-wrap: anywhere; }
.fixture-async-input { min-width: min(260px, 100%); flex: 1 1 240px; }
.fixture-state-grid { display: grid; min-width: 0; grid-template-columns: 220px minmax(0, 1fr); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.fixture-state-grid nav { display: flex; min-width: 0; flex-direction: column; gap: var(--co-space-2); padding: var(--co-space-3); border-right: 1px solid var(--co-border-default); }
.fixture-state-content { display: grid; min-width: 0; align-content: start; gap: var(--co-space-4); padding: var(--co-space-4); }
.fixture-risk-band { justify-content: space-between; padding: var(--co-space-4) 0; border-bottom: 1px solid var(--co-border-default); }
.fixture-risk-band span { color: var(--co-text-muted); font-size: 10px; text-transform: uppercase; }
.fixture-risk-band h2 { margin: var(--co-space-1) 0 0; font-size: 15px; }
.fixture-resource-cell { display: grid; min-width: 0; width: 100%; gap: 2px; }
.fixture-resource-cell strong { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fixture-resource-cell code,
.fixture-mono,
.fixture-long-hash { font-family: var(--co-font-mono); font-size: 10px; }
.fixture-resource-cell code { color: var(--co-text-muted); }
.fixture-long-hash { display: block; max-width: var(--co-inspector-width); overflow: hidden; text-overflow: ellipsis; }
.fixture-inspector-facts,
.fixture-full-page dl { display: grid; margin: 0; gap: var(--co-space-1); }
.fixture-inspector-facts div,
.fixture-full-page dl div { display: grid; grid-template-columns: 132px minmax(0, 1fr); gap: var(--co-space-2); padding: var(--co-space-2) 0; border-bottom: 1px solid var(--co-border-default); }
.fixture-inspector-facts dt,
.fixture-full-page dt {
  min-width: 0;
  color: var(--co-text-muted);
  overflow-wrap: anywhere;
}
.fixture-inspector-facts dd,
.fixture-full-page dd { min-width: 0; margin: 0; overflow-wrap: anywhere; font-family: var(--co-font-mono); }
.fixture-band { min-width: 0; padding: var(--co-space-4); border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.fixture-full-page h2 { margin: 0 0 var(--co-space-3); font-size: 15px; }
.fixture-modal-actions { width: 100%; justify-content: flex-end; }

@media (max-width: 1024px) {
  .workspace-fixture-shell main { padding: var(--co-space-4); }
  .workspace-fixture-shell main.has-workspace-inspector { margin-right: var(--co-inspector-width); }
  .fixture-state-grid { grid-template-columns: 1fr; }
  .fixture-state-grid nav { flex-direction: row; overflow-x: auto; border-right: 0; border-bottom: 1px solid var(--co-border-default); }
  .fixture-async-grid { grid-template-columns: 1fr; }
  .fixture-async-cell:nth-child(even) { border-left: 0; border-top: 1px solid var(--co-border-default); }
  .fixture-async-cell:last-child { grid-column: auto; }
  .fixture-risk-band { align-items: flex-start; flex-direction: column; }
}
</style>

<style>
* { box-sizing: border-box; }
html, body, #app { min-width: 0; min-height: 100%; margin: 0; }
body { color: var(--co-text-primary); background: var(--co-bg-canvas); font-family: var(--co-font-sans); letter-spacing: 0; }
button, input, select, textarea { font: inherit; }
button:focus-visible, a:focus-visible, input:focus-visible, select:focus-visible, textarea:focus-visible, [tabindex]:focus-visible {
  outline: 2px solid var(--co-focus-ring);
  outline-offset: 2px;
}
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { scroll-behavior: auto; transition-duration: 0.01ms; animation-duration: 0.01ms; animation-iteration-count: 1; }
}
</style>
