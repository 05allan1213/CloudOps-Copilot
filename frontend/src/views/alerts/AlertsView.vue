<script setup lang="ts">
import type { TableColumn } from "@nuxt/ui";
import { computed, h, onBeforeUnmount, reactive, ref, watch } from "vue";
import type { LocationQueryRaw, RouteLocationRaw } from "vue-router";
import { useRoute, useRouter } from "vue-router";

import {
  acknowledgeAlert,
  alertCommandKey,
  alertInspectorHistory,
  alertListRouteQuery,
  canonicalAlertResourceQuery,
  createAlertSilence,
  expireAlertSilence,
  getAlert,
  isAlertPublicID,
  listAlerts,
  parseAlertListRouteQuery,
  reconcileAlertProbe,
  type AlertCommandResult,
  type AlertDetail,
  type AlertListQuery,
  type AlertListRouteState,
  type AlertRouteQuery,
  type AlertSeverity,
  type AlertStatus,
  type AlertView,
} from "../../api/alerts";
import { isApiError } from "../../api/client";
import AlertBadges from "../../components/alerts/AlertBadges.vue";
import ApiErrorNotice from "../../components/workspace/ApiErrorNotice.vue";
import ContextToolbar from "../../components/workspace/ContextToolbar.vue";
import DenseDataTable, { type DenseTableColumn } from "../../components/workspace/DenseDataTable.vue";
import WorkspaceHeader from "../../components/workspace/WorkspaceHeader.vue";
import WorkspaceInspector, { type InspectorTargetState } from "../../components/workspace/WorkspaceInspector.vue";
import WorkspaceState from "../../components/workspace/WorkspaceState.vue";
import { useWorkspaceInspector } from "../../composables/useWorkspaceInspector";

type AlertRow = AlertView & Record<string, unknown>;
type InspectorCommand = "acknowledge" | "silence" | "expire-silence";

interface DenseDataTableHandle {
  getRowElement: (rowID: string) => HTMLElement | null;
  getScrollElement: () => HTMLElement | null;
}

interface AlertCommandFeedback {
  label: string;
  receivedAt: string;
  result: AlertCommandResult<unknown>;
}

const route = useRoute();
const router = useRouter();
const initialRouteState = parseAlertListRouteQuery(route.query as unknown as AlertRouteQuery);
const filters = reactive<AlertListRouteState>({ ...initialRouteState });
const items = ref<AlertView[]>([]);
const nextCursor = ref("");
const pendingItems = ref<AlertView[]>([]);
const loading = ref(true);
const refreshing = ref(false);
const probing = ref(false);
const listError = ref<unknown>(null);
const denseTable = ref<DenseDataTableHandle | null>(null);
const inspectorDetail = ref<AlertDetail | null>(null);
const inspectorLoading = ref(false);
const inspectorError = ref<unknown>(null);
const inspectorCommand = ref<InspectorCommand | null>(null);
const inspectorCommandReason = ref("");
const inspectorSilenceDuration = ref(1800);
const inspectorCommandPending = ref(false);
const inspectorCommandError = ref<unknown>(null);
const inspectorCommandKey = ref("");
const inspectorExpectedVersion = ref(0);
const inspectorFeedback = ref<AlertCommandFeedback | null>(null);
let listController: AbortController | null = null;
let inspectorController: AbortController | null = null;
let probeController: AbortController | null = null;
let probeTimer: number | undefined;
let activeDataKey = "";

const inspector = useWorkspaceInspector({
  selectedKey: "selected",
  scrollElement: () => denseTable.value?.getScrollElement() ?? null,
  resolveTrigger: (rowID) => denseTable.value?.getRowElement(rowID) ?? null,
});

const statusItems: { label: string; value: AlertStatus | "all" }[] = [
  { label: "全部状态", value: "all" },
  { label: "触发中", value: "firing" },
  { label: "已恢复", value: "resolved" },
];
const severityItems: { label: string; value: AlertSeverity | "all" }[] = [
  { label: "全部级别", value: "all" },
  { label: "严重", value: "critical" },
  { label: "警告", value: "warning" },
  { label: "信息", value: "info" },
  { label: "未知", value: "unknown" },
];
const limitItems = [
  { label: "每页 25 条", value: 25 },
  { label: "每页 50 条", value: 50 },
  { label: "每页 100 条", value: 100 },
];
const silenceDurationItems = [
  { label: "5 分钟", value: 300 },
  { label: "15 分钟", value: 900 },
  { label: "30 分钟", value: 1800 },
  { label: "1 小时", value: 3600 },
  { label: "4 小时", value: 14400 },
  { label: "24 小时", value: 86400 },
];

const rows = computed(() => items.value as AlertRow[]);
const statusSelection = computed<AlertStatus | "all">({
  get: () => filters.status || "all",
  set: (value) => { filters.status = value === "all" ? "" : value; },
});
const severitySelection = computed<AlertSeverity | "all">({
  get: () => filters.severity || "all",
  set: (value) => { filters.severity = value === "all" ? "" : value; },
});
const firingCount = computed(() => items.value.filter((item) => item.status === "firing").length);
const acknowledgedCount = computed(() => items.value.filter((item) => item.acknowledgement).length);
const silencedCount = computed(() => items.value.filter((item) => item.silence?.status === "active").length);
const selectedListItem = computed(() => items.value.find((item) => item.id === inspector.selectedID.value) ?? null);
const selectedAlert = computed(() => inspectorDetail.value?.alert ?? selectedListItem.value);
const inspectorHistory = computed(() => alertInspectorHistory(inspectorDetail.value?.events ?? []));
const canAcknowledge = computed(() => selectedAlert.value?.status === "firing" && !selectedAlert.value.acknowledgement);
const canSilence = computed(() => selectedAlert.value?.status === "firing"
  && !["pending", "active"].includes(selectedAlert.value.silence?.status ?? ""));
const canExpireSilence = computed(() => selectedAlert.value?.silence?.status === "active");
const inspectorTargetState = computed<InspectorTargetState>(() => {
  if (!inspector.selectedID.value || isAlertPublicID(inspector.selectedID.value)) {
    if (isApiError(inspectorError.value) && inspectorError.value.status === 403) return "permission-denied";
    if (isApiError(inspectorError.value) && inspectorError.value.status === 404) return "deleted";
    if (isApiError(inspectorError.value) && inspectorError.value.status === 410) return "expired";
    return "ready";
  }
  return "invalid";
});
const inspectorTargetDescription = computed(() => {
  if (inspectorTargetState.value === "invalid") return "链接中的 Alert ID 无法解析；不会自动选择第一行。";
  if (inspectorTargetState.value === "deleted") return "Alert 已不存在；筛选、游标和滚动位置保持不变。";
  if (inspectorTargetState.value === "permission-denied") return "当前身份无权读取此 Alert；不会用缓存内容推断当前事实。";
  if (inspectorTargetState.value === "expired") return "此 Alert 上下文已过期；请返回列表读取当前投影。";
  return "";
});
const inspectorCommandDefinition = computed(() => {
  if (inspectorCommand.value === "acknowledge") {
    return {
      title: "确认已知悉此 Alert",
      description: "记录当前 recurrence 已被 Owner 看到；不会创建 Silence、解决 Alert 或关闭 Incident。",
      confirmLabel: "记录 Acknowledge",
      color: "primary" as const,
      icon: "i-lucide-circle-check",
      needsReason: true,
    };
  }
  if (inspectorCommand.value === "silence") {
    return {
      title: "创建 Provider-backed Silence",
      description: "在选定时间内抑制匹配通知；Alert firing、acknowledgement 与 Incident 状态保持独立。",
      confirmLabel: "创建 Silence",
      color: "warning" as const,
      icon: "i-lucide-volume-x",
      needsReason: true,
    };
  }
  return {
    title: "提前结束当前 Silence",
    description: "Alertmanager 通知将恢复；Alert firing 状态不会因此改变。",
    confirmLabel: "结束 Silence",
    color: "warning" as const,
    icon: "i-lucide-volume-2",
    needsReason: false,
  };
});
const inspectorCommandReady = computed(() => Boolean(
  inspectorCommand.value
  && inspectorExpectedVersion.value > 0
  && inspectorCommandKey.value
  && (!inspectorCommandDefinition.value.needsReason || inspectorCommandReason.value.trim()),
));

const columns = [
  {
    id: "state",
    accessorKey: "status",
    label: "状态与级别",
    header: "状态与级别",
    size: 178,
    cell: ({ row }) => h(AlertBadges, {
      status: row.original.status,
      severity: row.original.severity,
    }),
  },
  {
    id: "alert",
    accessorKey: "summary",
    label: "Alert",
    header: "Alert",
    size: 340,
    cell: ({ row }) => h("div", { class: "alert-primary-cell", title: row.original.summary }, [
      h("strong", row.original.summary),
      h("span", `${row.original.category} · ${row.original.signal_count} Signals · recurrence ${row.original.recurrence_count}`),
    ]),
  },
  {
    id: "target",
    accessorKey: "target_name",
    label: "Scope 与目标",
    header: "Scope 与目标",
    size: 292,
    cell: ({ row }) => h("div", { class: "alert-target-cell" }, [
      h("strong", `${row.original.namespace}/${row.original.target_name}`),
      h("span", `${row.original.cluster} · ${row.original.target_kind} · ${row.original.service_name}`),
    ]),
  },
  {
    id: "facets",
    accessorKey: "version",
    label: "处置事实",
    header: "处置事实",
    size: 248,
    optional: true,
    cell: ({ row }) => h("span", { class: "alert-facet-cell" }, dispositionLabel(row.original)),
  },
  {
    id: "lastSeen",
    accessorKey: "last_seen_at",
    label: "最近 Signal",
    header: "最近 Signal",
    size: 176,
    optional: true,
    cell: ({ row }) => h("div", { class: "alert-time-cell" }, [
      h("time", {
        datetime: row.original.last_seen_at,
        title: formatUTC(row.original.last_seen_at),
        "aria-label": `${formatRelative(row.original.last_seen_at)}，${formatUTC(row.original.last_seen_at)}`,
      }, formatRelative(row.original.last_seen_at)),
      h("span", { class: "mono-text" }, `v${row.original.version}`),
    ]),
  },
  {
    id: "source",
    accessorKey: "source",
    label: "Provider",
    header: "Provider",
    size: 170,
    optional: true,
    cell: ({ row }) => h("div", { class: "alert-source-cell" }, [
      h("strong", row.original.source),
      h("span", row.original.migrated_legacy ? "Legacy ingress" : "Native Alert"),
    ]),
  },
] as (TableColumn<AlertRow> & DenseTableColumn<AlertRow>)[];

function dispositionLabel(item: AlertView): string {
  const facets = [];
  if (item.acknowledgement) facets.push("已 Acknowledge");
  if (item.silence?.status === "active") facets.push("Silence 生效中");
  if (item.incident_links.length) facets.push(`${item.incident_links.length} 个 Incident`);
  if (item.investigations.length) facets.push(`${item.investigations.length} 个 Investigation`);
  return facets.length ? facets.join(" · ") : "尚未处置";
}

function formatUTC(value?: string): string {
  if (!value) return "无";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toISOString();
}

function formatRelative(value: string): string {
  const timestamp = new Date(value).getTime();
  if (Number.isNaN(timestamp)) return value;
  const seconds = Math.round((timestamp - Date.now()) / 1000);
  const formatter = new Intl.RelativeTimeFormat("zh-CN", { numeric: "auto" });
  if (Math.abs(seconds) < 60) return formatter.format(seconds, "second");
  const minutes = Math.round(seconds / 60);
  if (Math.abs(minutes) < 60) return formatter.format(minutes, "minute");
  const hours = Math.round(minutes / 60);
  if (Math.abs(hours) < 24) return formatter.format(hours, "hour");
  return formatter.format(Math.round(hours / 24), "day");
}

function currentListQuery(state = parseAlertListRouteQuery(route.query as unknown as AlertRouteQuery)): AlertListQuery {
  return {
    status: state.status || undefined,
    severity: state.severity || undefined,
    namespace: state.namespace || undefined,
    search: state.search || undefined,
    incident: state.incident || undefined,
    cursor: state.cursor || undefined,
    limit: state.limit,
  };
}

function dataKey(state: AlertListRouteState): string {
  return JSON.stringify({
    status: state.status,
    severity: state.severity,
    namespace: state.namespace,
    search: state.search,
    incident: state.incident,
    cursor: state.cursor,
    limit: state.limit,
  });
}

async function synchronizeRoute() {
  const nextState = parseAlertListRouteQuery(route.query as unknown as AlertRouteQuery);
  const canonical = alertListRouteQuery(nextState, route.query as unknown as AlertRouteQuery);
  const canonicalLocation = { path: route.path, query: canonical as LocationQueryRaw, hash: route.hash };
  if (router.resolve(canonicalLocation).fullPath !== route.fullPath) {
    await router.replace(canonicalLocation);
    return;
  }
  Object.assign(filters, nextState);
  const nextDataKey = dataKey(nextState);
  if (nextDataKey === activeDataKey) return;
  activeDataKey = nextDataKey;
  pendingItems.value = [];
  await loadList(false);
}

async function loadList(preserve: boolean) {
  listController?.abort();
  probeController?.abort();
  const requestController = new AbortController();
  listController = requestController;
  if (preserve && items.value.length) refreshing.value = true;
  else {
    loading.value = true;
    items.value = [];
  }
  listError.value = null;
  try {
    const page = await listAlerts(currentListQuery(), requestController.signal);
    if (listController !== requestController) return;
    items.value = page.items;
    nextCursor.value = page.next_cursor || "";
    pendingItems.value = [];
    scheduleProbe();
  } catch (error) {
    if (requestController.signal.aborted || listController !== requestController) return;
    listError.value = error;
  } finally {
    if (listController === requestController) {
      loading.value = false;
      refreshing.value = false;
    }
  }
}

function scheduleProbe() {
  if (probeTimer !== undefined) window.clearInterval(probeTimer);
  probeTimer = undefined;
  const state = parseAlertListRouteQuery(route.query as unknown as AlertRouteQuery);
  if (state.cursor) return;
  probeTimer = window.setInterval(() => void probeForNewRows(), 30_000);
}

async function probeForNewRows() {
  if (probing.value || loading.value || refreshing.value) return;
  const state = parseAlertListRouteQuery(route.query as unknown as AlertRouteQuery);
  if (state.cursor) return;
  probeController?.abort();
  const requestController = new AbortController();
  probeController = requestController;
  probing.value = true;
  try {
    const page = await listAlerts({ ...currentListQuery(state), cursor: undefined }, requestController.signal);
    if (probeController !== requestController) return;
    const reconciliation = reconcileAlertProbe(items.value, page.items);
    items.value = reconciliation.items;
    pendingItems.value = reconciliation.pendingItems;
  } catch {
    // A failed background probe never replaces the current readable projection.
  } finally {
    if (probeController === requestController) probing.value = false;
  }
}

async function applyFilters() {
  await router.push({
    path: route.path,
    query: alertListRouteQuery({ ...filters, cursor: "", selected: "" }, route.query as unknown as AlertRouteQuery) as LocationQueryRaw,
  });
}

async function clearFilters() {
  filters.status = "";
  filters.severity = "";
  filters.namespace = "";
  filters.search = "";
  await applyFilters();
}

async function clearIncidentFilter() {
  filters.incident = "";
  await applyFilters();
}

async function loadNextPage() {
  if (!nextCursor.value) return;
  const state = parseAlertListRouteQuery(route.query as unknown as AlertRouteQuery);
  await router.push({
    path: route.path,
    query: alertListRouteQuery({ ...state, cursor: nextCursor.value, selected: "" }, route.query as unknown as AlertRouteQuery) as LocationQueryRaw,
  });
}

async function returnToFirstPage() {
  const state = parseAlertListRouteQuery(route.query as unknown as AlertRouteQuery);
  await router.push({
    path: route.path,
    query: alertListRouteQuery({ ...state, cursor: "", selected: "" }, route.query as unknown as AlertRouteQuery) as LocationQueryRaw,
  });
}

function openRow(row: AlertRow, trigger: HTMLElement | null) {
  void inspector.open(row.id, trigger);
}

function requestInspectorOpen(value: boolean) {
  if (!value) void inspector.close();
}

function fullDetailLocation(item: AlertView): RouteLocationRaw {
  const state = parseAlertListRouteQuery(route.query as unknown as AlertRouteQuery);
  const query = canonicalAlertResourceQuery(alertListRouteQuery(
    { ...state, selected: "" },
    route.query as unknown as AlertRouteQuery,
  ));
  query.cluster = item.cluster;
  query.namespace = item.namespace;
  query.from = item.starts_at;
  query.to = item.resolved_at || new Date().toISOString();
  query.resource = item.target_name;
  return { name: "alert-detail", params: { alertId: item.id }, query: query as LocationQueryRaw };
}

function openFullDetail() {
  if (selectedAlert.value) void inspector.openFull(fullDetailLocation(selectedAlert.value));
}

async function loadInspector(selectedID: string) {
  inspectorController?.abort();
  inspectorDetail.value = null;
  inspectorError.value = null;
  inspectorFeedback.value = null;
  closeInspectorCommand();
  if (!selectedID || !isAlertPublicID(selectedID)) return;
  const requestController = new AbortController();
  inspectorController = requestController;
  inspectorLoading.value = true;
  try {
    const next = await getAlert(selectedID, requestController.signal);
    if (inspectorController !== requestController) return;
    inspectorDetail.value = next;
    items.value = items.value.map((item) => item.id === selectedID ? next.alert : item);
  } catch (error) {
    if (requestController.signal.aborted || inspectorController !== requestController) return;
    inspectorError.value = error;
  } finally {
    if (inspectorController === requestController) inspectorLoading.value = false;
  }
}

function openInspectorCommand(command: InspectorCommand) {
  if (!selectedAlert.value) return;
  inspectorCommand.value = command;
  inspectorCommandReason.value = command === "acknowledge"
    ? "Owner 已看到并开始 triage"
    : command === "silence"
      ? "Owner triage 期间抑制重复 Provider 通知"
      : "";
  inspectorCommandError.value = null;
  inspectorExpectedVersion.value = selectedAlert.value.version;
  const resourceID = command === "expire-silence"
    ? selectedAlert.value.silence?.id ?? selectedAlert.value.id
    : selectedAlert.value.id;
  inspectorCommandKey.value = alertCommandKey(command, resourceID);
}

function closeInspectorCommand() {
  if (inspectorCommandPending.value) return;
  inspectorCommand.value = null;
  inspectorCommandError.value = null;
}

function updateInspectorCommandOpen(value: boolean) {
  if (!value) closeInspectorCommand();
}

async function runInspectorCommand() {
  const current = selectedAlert.value;
  const command = inspectorCommand.value;
  if (!current || !command || !inspectorCommandReady.value || inspectorCommandPending.value) return;
  inspectorCommandPending.value = true;
  inspectorCommandError.value = null;
  try {
    const options = { idempotencyKey: inspectorCommandKey.value };
    const result = command === "acknowledge"
      ? await acknowledgeAlert(current.id, inspectorExpectedVersion.value, inspectorCommandReason.value.trim(), options)
      : command === "silence"
        ? await createAlertSilence(current.id, inspectorExpectedVersion.value, inspectorSilenceDuration.value, inspectorCommandReason.value.trim(), options)
        : await expireAlertSilence(current.silence!.id, inspectorExpectedVersion.value, options);
    inspectorFeedback.value = {
      label: command === "acknowledge" ? "Acknowledge 已返回" : command === "silence" ? "Silence 创建请求已返回" : "Silence 结束请求已返回",
      receivedAt: new Date().toISOString(),
      result,
    };
    inspectorCommand.value = null;
    await loadInspector(current.id);
    inspectorFeedback.value = {
      label: command === "acknowledge" ? "Acknowledge 已返回" : command === "silence" ? "Silence 创建请求已返回" : "Silence 结束请求已返回",
      receivedAt: new Date().toISOString(),
      result,
    };
  } catch (error) {
    inspectorCommandError.value = error;
  } finally {
    inspectorCommandPending.value = false;
  }
}

function copyRow(item: AlertRow): string {
  return JSON.stringify({
    id: item.id,
    summary: item.summary,
    status: item.status,
    severity: item.severity,
    cluster: item.cluster,
    namespace: item.namespace,
    target: `${item.target_kind}/${item.target_name}`,
    fingerprint: item.fingerprint,
    correlation_key: item.correlation_key,
    version: item.version,
    last_seen_at: item.last_seen_at,
  }, null, 2);
}

watch(() => route.fullPath, () => void synchronizeRoute(), { immediate: true });
watch(() => inspector.selectedID.value, (selectedID) => void loadInspector(selectedID), { immediate: true });
onBeforeUnmount(() => {
  listController?.abort();
  inspectorController?.abort();
  probeController?.abort();
  if (probeTimer !== undefined) window.clearInterval(probeTimer);
});
</script>

<template>
  <article
    class="alerts-workspace"
    data-testid="alerts-list-route"
  >
    <WorkspaceHeader
      title="告警"
      eyebrow="CloudOps Alerts"
      description="按真实 Alert lifecycle 快速扫描、处置并进入关联的 Incident 或 Agent 上下文。"
    >
      <template #actions>
        <UTooltip text="刷新当前 Alert 投影">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-refresh-cw"
            square
            aria-label="刷新告警"
            :loading="refreshing"
            :disabled="loading || refreshing"
            @click="loadList(true)"
          />
        </UTooltip>
      </template>
    </WorkspaceHeader>

    <UAlert
      v-if="filters.incident"
      color="info"
      variant="soft"
      icon="i-lucide-link-2"
      title="当前列表来自一个 Incident 上下文"
      :description="`仅显示与 Incident ${filters.incident} 关联的 Alert。`"
    >
      <template #actions>
        <UButton
          color="info"
          variant="outline"
          icon="i-lucide-arrow-up-right"
          label="打开 Incident"
          :to="{ name: 'incident-detail', params: { incidentId: filters.incident } }"
        />
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-filter-x"
          label="移除 Incident 筛选"
          @click="clearIncidentFilter"
        />
      </template>
    </UAlert>

    <dl
      class="alert-summary-strip"
      aria-label="当前 Alert 摘要"
    >
      <div><dt>当前页</dt><dd>{{ items.length }}</dd></div>
      <div>
        <dt>触发中</dt><dd class="is-critical">
          {{ firingCount }}
        </dd>
      </div>
      <div><dt>已 Acknowledge</dt><dd>{{ acknowledgedCount }}</dd></div>
      <div><dt>Silence 生效中</dt><dd>{{ silencedCount }}</dd></div>
    </dl>

    <ContextToolbar label="Alert 筛选与列表操作">
      <template #filters>
        <UForm
          class="alert-filter-form"
          :state="filters"
          @submit="applyFilters"
        >
          <UFormField
            label="状态"
            name="status"
          >
            <USelect
              v-model="statusSelection"
              :items="statusItems"
              value-key="value"
              aria-label="Alert 状态"
            />
          </UFormField>
          <UFormField
            label="级别"
            name="severity"
          >
            <USelect
              v-model="severitySelection"
              :items="severityItems"
              value-key="value"
              aria-label="Alert 级别"
            />
          </UFormField>
          <UFormField
            label="Namespace"
            name="namespace"
          >
            <UInput
              v-model="filters.namespace"
              icon="i-lucide-box"
              maxlength="255"
              autocomplete="off"
              placeholder="例如 demo"
            />
          </UFormField>
          <UFormField
            label="搜索"
            name="search"
            class="alert-search-field"
          >
            <UInput
              v-model="filters.search"
              icon="i-lucide-search"
              maxlength="255"
              autocomplete="off"
              placeholder="摘要、目标或服务"
            />
          </UFormField>
          <UFormField
            label="每页"
            name="limit"
          >
            <USelect
              v-model="filters.limit"
              :items="limitItems"
              value-key="value"
              aria-label="每页 Alert 数量"
            />
          </UFormField>
        </UForm>
      </template>
      <template #secondary>
        <UTooltip text="清除状态、级别、Namespace 和搜索">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-filter-x"
            square
            aria-label="清除筛选"
            @click="clearFilters"
          />
        </UTooltip>
      </template>
      <template #primary>
        <UButton
          color="primary"
          icon="i-lucide-search"
          label="查询"
          @click="applyFilters"
        />
      </template>
    </ContextToolbar>

    <UAlert
      v-if="pendingItems.length"
      data-testid="alerts-new-row-control"
      color="info"
      variant="soft"
      icon="i-lucide-list-plus"
      :title="`${pendingItems.length} 条新 Alert 等待加载`"
      description="现有行已就地更新；新行不会在扫描期间自动插入。"
    >
      <template #actions>
        <UButton
          color="info"
          variant="solid"
          icon="i-lucide-list-restart"
          label="立即加载"
          @click="loadList(false)"
        />
      </template>
    </UAlert>

    <ApiErrorNotice
      v-if="listError && !items.length"
      :error="listError"
      fallback="Alert 列表读取失败。"
      title="Alert API 不可用"
      retryable
      @retry="loadList(false)"
    />
    <WorkspaceState
      v-else-if="loading && !items.length"
      kind="loading"
      title="正在读取 Alert lifecycle"
      description="筛选、游标与 Inspector URL 保持稳定。"
    />
    <template v-else>
      <ApiErrorNotice
        v-if="listError"
        :error="listError"
        fallback="后台刷新失败；当前 Alert 投影保持可读。"
        title="刷新失败"
        retryable
        @retry="loadList(true)"
      />
      <WorkspaceState
        v-if="!items.length"
        kind="empty"
        title="没有匹配的 Alert"
        description="当前状态、级别、Namespace、搜索和 Incident 条件下没有领域记录。"
      >
        <template #actions>
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-filter-x"
            label="清除筛选"
            @click="clearFilters"
          />
        </template>
      </WorkspaceState>
      <DenseDataTable
        v-else
        ref="denseTable"
        :rows="rows"
        :columns="columns"
        :row-key="(row: AlertRow) => row.id"
        storage-key="alerts-triage"
        caption="Alert triage 列表；选择行打开 Inspector"
        :critical-column-ids="['state', 'alert', 'target']"
        :selected-id="inspector.selectedID.value"
        :severity="(row: AlertRow) => row.severity === 'critical' ? 'critical' : row.severity === 'warning' ? 'warning' : row.severity === 'info' ? 'info' : 'neutral'"
        :copy-value="copyRow"
        @select="openRow"
      />
      <nav
        v-if="items.length"
        class="alert-pagination"
        aria-label="Alert cursor 分页"
      >
        <UButton
          v-if="filters.cursor"
          color="neutral"
          variant="outline"
          icon="i-lucide-list-start"
          label="返回首屏"
          @click="returnToFirstPage"
        />
        <code
          v-if="filters.cursor"
          translate="no"
        >cursor {{ filters.cursor }}</code>
        <UButton
          v-if="nextCursor"
          color="neutral"
          variant="outline"
          trailing-icon="i-lucide-arrow-right"
          label="加载下一页"
          @click="loadNextPage"
        />
      </nav>
    </template>

    <WorkspaceInspector
      :open="Boolean(inspector.selectedID.value)"
      :title="selectedAlert?.summary ?? 'Alert 目标不可用'"
      :description="selectedAlert ? `${selectedAlert.namespace}/${selectedAlert.target_name} · v${selectedAlert.version}` : inspector.selectedID.value"
      :target-state="inspectorTargetState"
      :target-description="inspectorTargetDescription"
      :trigger="inspector.triggerElement.value"
      @update:open="requestInspectorOpen"
    >
      <template #actions>
        <UTooltip text="刷新 Inspector">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-refresh-cw"
            square
            aria-label="刷新 Alert Inspector"
            :loading="inspectorLoading"
            :disabled="!isAlertPublicID(inspector.selectedID.value)"
            @click="loadInspector(inspector.selectedID.value)"
          />
        </UTooltip>
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
      <WorkspaceState
        v-if="inspectorLoading"
        kind="loading"
        title="正在读取 Alert 快照"
        description="列表筛选、游标和滚动位置保持不变。"
      />
      <ApiErrorNotice
        v-else-if="inspectorError && inspectorTargetState === 'ready'"
        :error="inspectorError"
        fallback="Alert Inspector 读取失败。"
        retryable
        @retry="loadInspector(inspector.selectedID.value)"
      />
      <template v-else-if="inspectorDetail && selectedAlert">
        <AlertBadges
          :status="selectedAlert.status"
          :severity="selectedAlert.severity"
        />
        <dl class="alert-inspector-facts">
          <div><dt>Alert ID</dt><dd><code translate="no">{{ selectedAlert.id }}</code></dd></div>
          <div><dt>Provider</dt><dd>{{ selectedAlert.source }}</dd></div>
          <div><dt>当前版本</dt><dd><code translate="no">v{{ selectedAlert.version }}</code></dd></div>
          <div><dt>最近 Signal UTC</dt><dd><time :datetime="selectedAlert.last_seen_at">{{ formatUTC(selectedAlert.last_seen_at) }}</time></dd></div>
          <div><dt>Operational Scope</dt><dd><code translate="no">{{ selectedAlert.context_link.operational_scope_id || "未投影" }}</code></dd></div>
          <div><dt>来源</dt><dd>{{ selectedAlert.migrated_legacy ? "Legacy automatic ingress" : "Native Alert" }}</dd></div>
        </dl>

        <UAlert
          v-if="inspectorFeedback"
          color="info"
          variant="soft"
          icon="i-lucide-receipt-text"
          :title="inspectorFeedback.label"
          description="命令响应不等于 Incident 恢复或 Verification 通过。"
        >
          <template #description>
            <dl class="alert-command-identity">
              <div><dt>HTTP</dt><dd>{{ inspectorFeedback.result.httpStatus }}</dd></div>
              <div><dt>Expected version</dt><dd>{{ inspectorFeedback.result.expectedVersion }}</dd></div>
              <div><dt>Idempotent replay</dt><dd>{{ inspectorFeedback.result.idempotentReplay ? "YES" : "NO" }}</dd></div>
              <div><dt>Request ID</dt><dd>{{ inspectorFeedback.result.requestID || "未返回" }}</dd></div>
              <div><dt>Trace ID</dt><dd>{{ inspectorFeedback.result.traceID || "未返回" }}</dd></div>
              <div><dt>Idempotency Key</dt><dd>{{ inspectorFeedback.result.idempotencyKey }}</dd></div>
              <div><dt>客户端收到 UTC</dt><dd>{{ formatUTC(inspectorFeedback.receivedAt) }}</dd></div>
            </dl>
          </template>
        </UAlert>

        <section
          class="alert-inspector-section"
          aria-labelledby="alert-inspector-actions"
        >
          <h3 id="alert-inspector-actions">
            Alert 本地处置
          </h3>
          <div class="alert-inspector-actions">
            <UButton
              color="primary"
              variant="soft"
              icon="i-lucide-circle-check"
              label="Acknowledge"
              :disabled="!canAcknowledge"
              @click="openInspectorCommand('acknowledge')"
            />
            <UButton
              color="warning"
              variant="soft"
              icon="i-lucide-volume-x"
              label="创建 Silence"
              :disabled="!canSilence"
              @click="openInspectorCommand('silence')"
            />
            <UButton
              color="warning"
              variant="outline"
              icon="i-lucide-volume-2"
              label="结束 Silence"
              :disabled="!canExpireSilence"
              @click="openInspectorCommand('expire-silence')"
            />
          </div>
        </section>

        <section
          class="alert-inspector-section"
          aria-labelledby="alert-inspector-history"
        >
          <h3 id="alert-inspector-history">
            状态历史
          </h3>
          <ol
            v-if="inspectorHistory.length"
            class="alert-inspector-history"
          >
            <li
              v-for="event in inspectorHistory"
              :key="event.id"
            >
              <strong>{{ event.summary }}</strong>
              <span>{{ event.type }} · {{ event.actor_type }}/{{ event.actor_id }}</span>
              <time :datetime="event.occurred_at">{{ formatUTC(event.occurred_at) }}</time>
            </li>
          </ol>
          <p
            v-else
            class="alert-empty-line"
          >
            当前投影没有 Alert event。
          </p>
        </section>

        <section
          class="alert-inspector-section"
          aria-labelledby="alert-inspector-incidents"
        >
          <h3 id="alert-inspector-incidents">
            关联 Incident
          </h3>
          <div
            v-if="selectedAlert.incident_links.length"
            class="alert-link-stack"
          >
            <UButton
              v-for="link in selectedAlert.incident_links"
              :key="link.id"
              color="neutral"
              variant="outline"
              icon="i-lucide-siren"
              :label="`Incident ${link.incident_id.slice(0, 12)} · ${link.incident_status}`"
              :to="{ name: 'incident-detail', params: { incidentId: link.incident_id } }"
            />
          </div>
          <p
            v-else
            class="alert-empty-line"
          >
            尚未关联 Incident；创建与关联操作在完整详情页执行。
          </p>
        </section>

        <section
          class="alert-inspector-section"
          aria-labelledby="alert-inspector-provider"
        >
          <h3 id="alert-inspector-provider">
            Provider 与恢复动作
          </h3>
          <div class="alert-link-stack">
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-settings-2"
              label="Alertmanager 配置"
              to="/settings#providers"
            />
            <UButton
              v-for="run in selectedAlert.investigations"
              :key="run.id"
              color="neutral"
              variant="outline"
              icon="i-lucide-bot"
              :label="`Investigation ${run.id.slice(0, 12)} · ${run.status}`"
              :to="{ name: 'agent', query: { investigation: run.id } }"
            />
          </div>
        </section>
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
          label="打开完整详情"
          :disabled="!selectedAlert"
          @click="openFullDetail"
        />
      </template>
    </WorkspaceInspector>

    <UModal
      :open="Boolean(inspectorCommand)"
      :title="inspectorCommandDefinition.title"
      :description="inspectorCommandDefinition.description"
      :dismissible="!inspectorCommandPending"
      :close="!inspectorCommandPending"
      @update:open="updateInspectorCommandOpen"
    >
      <template #body>
        <div class="alert-command-dialog">
          <UAlert
            :color="inspectorCommandDefinition.color === 'primary' ? 'info' : inspectorCommandDefinition.color"
            variant="soft"
            :icon="inspectorCommandDefinition.icon"
            :title="inspectorCommandDefinition.title"
            :description="inspectorCommandDefinition.description"
          />
          <dl>
            <div><dt>Target</dt><dd>{{ selectedAlert?.target_kind }}/{{ selectedAlert?.namespace }}/{{ selectedAlert?.target_name }}</dd></div>
            <div><dt>Expected version</dt><dd><code translate="no">{{ inspectorExpectedVersion }}</code></dd></div>
            <div><dt>Idempotency Key</dt><dd><code translate="no">{{ inspectorCommandKey }}</code></dd></div>
            <div v-if="inspectorCommand === 'silence'">
              <dt>恢复</dt><dd>到期自动解除，也可提前结束；不会改变 firing 状态。</dd>
            </div>
            <div v-if="inspectorCommand === 'expire-silence'">
              <dt>后果</dt><dd>Provider 通知恢复；当前 Alert 与 Incident 状态保持不变。</dd>
            </div>
          </dl>
          <UForm
            :state="{ reason: inspectorCommandReason, duration: inspectorSilenceDuration }"
            @submit="runInspectorCommand"
          >
            <UFormField
              v-if="inspectorCommand === 'silence'"
              label="Silence 时长"
              name="duration"
            >
              <USelect
                v-model="inspectorSilenceDuration"
                :items="silenceDurationItems"
                value-key="value"
              />
            </UFormField>
            <UFormField
              v-if="inspectorCommandDefinition.needsReason"
              label="审计原因"
              name="reason"
              required
            >
              <UTextarea
                v-model="inspectorCommandReason"
                :rows="4"
                maxlength="1024"
                autoresize
              />
            </UFormField>
          </UForm>
          <ApiErrorNotice
            v-if="inspectorCommandError"
            :error="inspectorCommandError"
            fallback="Alert 命令失败；输入与 Idempotency Key 已保留。"
            title="命令未完成"
          />
        </div>
      </template>
      <template #footer>
        <div class="alert-command-actions">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-arrow-left"
            label="取消"
            :disabled="inspectorCommandPending"
            @click="closeInspectorCommand"
          />
          <UButton
            :color="inspectorCommandDefinition.color"
            :icon="inspectorCommandDefinition.icon"
            :label="inspectorCommandDefinition.confirmLabel"
            :loading="inspectorCommandPending"
            :disabled="!inspectorCommandReady"
            @click="runInspectorCommand"
          />
        </div>
      </template>
    </UModal>
  </article>
</template>

<style scoped>
.alerts-workspace {
  display: grid;
  width: 100%;
  min-width: 0;
  gap: var(--co-space-4);
}

.alert-summary-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin: 0;
  border-block: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}

.alert-summary-strip div {
  min-width: 0;
  padding: var(--co-space-3) var(--co-space-4);
  border-right: 1px solid var(--co-border-default);
}

.alert-summary-strip div:last-child { border-right: 0; }
.alert-summary-strip dt { color: var(--co-text-muted); font-size: 11px; }
.alert-summary-strip dd {
  margin: var(--co-space-1) 0 0;
  font-family: var(--co-font-mono);
  font-size: 18px;
  font-variant-numeric: tabular-nums;
  font-weight: 800;
}
.alert-summary-strip .is-critical { color: var(--co-status-critical-fg); }

.alert-filter-form {
  display: grid;
  width: 100%;
  min-width: 0;
  grid-template-columns: 138px 138px minmax(150px, 0.7fr) minmax(220px, 1fr) 132px;
  align-items: end;
  gap: var(--co-space-2);
}

.alert-primary-cell,
.alert-target-cell,
.alert-time-cell,
.alert-source-cell {
  display: grid;
  min-width: 0;
  gap: 2px;
}

.alert-primary-cell strong,
.alert-target-cell strong,
.alert-source-cell strong {
  min-width: 0;
  overflow: hidden;
  color: var(--co-text-primary);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.alert-primary-cell span,
.alert-target-cell span,
.alert-time-cell span,
.alert-source-cell span,
.alert-facet-cell { color: var(--co-text-muted); font-size: 11px; }
.alert-facet-cell { white-space: nowrap; }
.alert-time-cell time { color: var(--co-text-primary); font-variant-numeric: tabular-nums; }

.alert-pagination {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: var(--co-space-2);
}

.alert-pagination code {
  max-width: min(52ch, 100%);
  overflow: hidden;
  color: var(--co-text-muted);
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.alert-inspector-facts,
.alert-command-identity,
.alert-command-dialog dl {
  display: grid;
  min-width: 0;
  margin: 0;
  gap: var(--co-space-1);
}

.alert-inspector-facts div,
.alert-command-identity div,
.alert-command-dialog dl div {
  display: grid;
  min-width: 0;
  grid-template-columns: 128px minmax(0, 1fr);
  gap: var(--co-space-2);
  padding: var(--co-space-2) 0;
  border-bottom: 1px solid var(--co-border-default);
}

.alert-inspector-facts dt,
.alert-command-identity dt,
.alert-command-dialog dt { color: var(--co-text-muted); font-size: 11px; }
.alert-inspector-facts dd,
.alert-command-identity dd,
.alert-command-dialog dd { min-width: 0; margin: 0; overflow-wrap: anywhere; }
.alert-command-identity dd { font-family: var(--co-font-mono); font-size: 10px; }

.alert-inspector-section {
  display: grid;
  min-width: 0;
  gap: var(--co-space-3);
  padding-top: var(--co-space-3);
  border-top: 1px solid var(--co-border-default);
}
.alert-inspector-section h3 { margin: 0; font-size: 14px; }
.alert-inspector-actions,
.alert-link-stack { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-2); }
.alert-link-stack { display: grid; }
.alert-inspector-history { display: grid; margin: 0; padding: 0; list-style: none; }
.alert-inspector-history li { display: grid; min-width: 0; gap: 2px; padding: var(--co-space-2) 0; border-bottom: 1px solid var(--co-border-subtle); }
.alert-inspector-history strong,
.alert-inspector-history span { overflow-wrap: anywhere; }
.alert-inspector-history span,
.alert-inspector-history time,
.alert-empty-line { color: var(--co-text-muted); font-size: 11px; }
.alert-inspector-history time { font-family: var(--co-font-mono); }
.alert-empty-line { margin: 0; }

.alert-command-dialog { display: grid; min-width: 0; gap: var(--co-space-4); }
.alert-command-actions { display: flex; width: 100%; justify-content: flex-end; gap: var(--co-space-2); }

@media (max-width: 1180px) {
  .alert-filter-form { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .alert-search-field { grid-column: span 2; }
}

@media (max-width: 1024px) {
  .alert-summary-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .alert-summary-strip div:nth-child(2) { border-right: 0; }
  .alert-summary-strip div:nth-child(-n+2) { border-bottom: 1px solid var(--co-border-default); }
  .alert-filter-form { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .alert-search-field { grid-column: 1 / -1; }
}
</style>
