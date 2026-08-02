<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
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
import AlertQueue from "../../components/alerts/AlertQueue.vue";
import ApiErrorNotice from "../../components/workspace/ApiErrorNotice.vue";
import WorkspaceHeader from "../../components/workspace/WorkspaceHeader.vue";
import WorkspaceInspector, { type InspectorTargetState } from "../../components/workspace/WorkspaceInspector.vue";
import WorkspacePageFrame from "../../components/workspace/WorkspacePageFrame.vue";
import WorkspaceState from "../../components/workspace/WorkspaceState.vue";
import { invalidateQueryDomain } from "../../composables/queryCache";
import { useWorkspaceInspector } from "../../composables/useWorkspaceInspector";
import { usePageVisibility } from "../../composables/usePageVisibility";

type InspectorCommand = "acknowledge" | "silence" | "expire-silence";

interface AlertQueueHandle {
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
const alertQueue = ref<AlertQueueHandle | null>(null);
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
const { visible } = usePageVisibility();

const inspector = useWorkspaceInspector({
  selectedKey: "selected",
  scrollElement: () => alertQueue.value?.getScrollElement() ?? null,
  resolveTrigger: (rowID) => alertQueue.value?.getRowElement(rowID) ?? null,
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

const statusSelection = computed<AlertStatus | "all">({
  get: () => filters.status || "all",
  set: (value) => { filters.status = value === "all" ? "" : value; },
});
const severitySelection = computed<AlertSeverity | "all">({
  get: () => filters.severity || "all",
  set: (value) => { filters.severity = value === "all" ? "" : value; },
});
const firingCount = computed(() => items.value.filter((item) => item.status === "firing").length);
const criticalCount = computed(() => items.value.filter((item) => item.severity === "critical").length);
const needsAttentionCount = computed(() => items.value.filter((item) => (
  item.status === "firing" && !item.acknowledgement && item.silence?.status !== "active"
)).length);
const silencedCount = computed(() => items.value.filter((item) => item.silence?.status === "active").length);
const queueItems = computed(() => [...items.value].sort((left, right) => {
  if (left.status !== right.status) return left.status === "firing" ? -1 : 1;
  const severityOrder: Record<AlertSeverity, number> = { critical: 0, warning: 1, info: 2, unknown: 3 };
  const severityDelta = severityOrder[left.severity] - severityOrder[right.severity];
  if (severityDelta) return severityDelta;
  return Date.parse(right.last_seen_at) - Date.parse(left.last_seen_at);
}));
const activeFilterLabel = computed(() => {
  const labels: string[] = [];
  if (filters.status) labels.push(filters.status === "firing" ? "触发中" : "已恢复");
  if (filters.severity) labels.push(({ critical: "严重", warning: "警告", info: "信息" } as Record<string, string>)[filters.severity] ?? filters.severity);
  if (filters.namespace) labels.push(filters.namespace);
  if (filters.search) labels.push(`“${filters.search}”`);
  return labels.length ? labels.join(" · ") : "全部当前告警";
});
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

function formatUTC(value?: string): string {
  if (!value) return "无";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toISOString();
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
  if (preserve) invalidateQueryDomain("alerts");
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
  if (state.cursor || !visible.value) return;
  probeTimer = window.setInterval(() => void probeForNewRows(), 30_000);
}

function pauseProbe() {
  if (probeTimer !== undefined) window.clearInterval(probeTimer);
  probeTimer = undefined;
  probeController?.abort();
  probeController = null;
  probing.value = false;
}

async function probeForNewRows() {
  if (!visible.value || probing.value || loading.value || refreshing.value) return;
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

async function applyQuickFilter(kind: "firing" | "critical") {
  filters.status = kind === "firing" ? "firing" : "";
  filters.severity = kind === "critical" ? "critical" : "";
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

function openRow(row: AlertView, trigger: HTMLElement | null) {
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

async function loadInspector(selectedID: string, force = false) {
  if (force) invalidateQueryDomain("alerts");
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

watch(() => route.fullPath, () => void synchronizeRoute(), { immediate: true });
watch(() => inspector.selectedID.value, (selectedID) => void loadInspector(selectedID), { immediate: true });
watch(visible, (isVisible) => {
  if (!isVisible) {
    pauseProbe();
    return;
  }
  scheduleProbe();
  if (activeDataKey) void probeForNewRows();
});
onBeforeUnmount(() => {
  listController?.abort();
  inspectorController?.abort();
  pauseProbe();
});
</script>

<template>
  <WorkspacePageFrame
    as="article"
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

    <section class="alerts-signal-deck" aria-labelledby="alert-attention-heading">
      <div class="alerts-signal-deck__main">
        <div class="alert-attention__lead">
          <span class="alert-attention__signal" aria-hidden="true">
            <UIcon name="i-lucide-siren" />
          </span>
          <div>
            <span class="alert-attention__eyebrow">Live signal / Alert lifecycle</span>
            <h2 id="alert-attention-heading">
              {{ firingCount ? `${firingCount} 条告警正在触发` : "当前没有触发中的告警" }}
            </h2>
            <p>
              <template v-if="needsAttentionCount">
                {{ needsAttentionCount }} 条尚未处置，{{ silencedCount }} 条静默中；先处理严重且未关联 Incident 的对象。
              </template>
              <template v-else>
                当前投影没有未确认且未静默的 firing Alert，{{ silencedCount }} 条处于静默。
              </template>
            </p>
          </div>
        </div>
        <div class="alert-signal-track" aria-hidden="true">
          <span class="is-critical" :style="{ flexGrow: Math.max(criticalCount, 1) }" />
          <span class="is-warning" :style="{ flexGrow: Math.max(needsAttentionCount - criticalCount, 1) }" />
          <span class="is-quiet" :style="{ flexGrow: Math.max(items.length - firingCount, 1) }" />
        </div>
      </div>
      <div class="alert-attention__facets" aria-label="告警摘要与快速筛选">
        <UButton class="alert-attention__facet" color="neutral" variant="ghost" @click="applyQuickFilter('firing')">
          <span><small>触发中</small><b>{{ firingCount }}</b></span>
        </UButton>
        <UButton class="alert-attention__facet" color="neutral" variant="ghost" @click="applyQuickFilter('critical')">
          <span><small>严重</small><b class="is-critical">{{ criticalCount }}</b></span>
        </UButton>
        <div class="alert-attention__facet is-static">
          <span><small>待处置</small><b class="is-warning">{{ needsAttentionCount }}</b></span>
        </div>
        <div class="alert-attention__facet is-static">
          <span><small>已静默</small><b>{{ silencedCount }}</b></span>
        </div>
      </div>
    </section>

    <UForm
      class="alert-commandbar"
      :state="filters"
      aria-label="Alert 筛选与列表操作"
      @submit="applyFilters"
    >
      <UTabs
        v-model="statusSelection"
        class="alert-status-tabs"
        :items="statusItems"
        :content="false"
        color="primary"
        variant="pill"
        size="sm"
        aria-label="Alert 状态"
      />
      <UInput
        v-model="filters.search"
        class="alert-search-field"
        icon="i-lucide-search"
        maxlength="255"
        autocomplete="off"
        aria-label="搜索告警或目标"
        placeholder="搜索告警、服务或对象"
      />
      <UCollapsible class="alert-advanced-filters">
        <template #default="{ open }">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-sliders-horizontal"
            :trailing-icon="open ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
            label="高级筛选"
            :aria-label="`${open ? '收起' : '展开'} Alert 高级筛选`"
          />
        </template>
        <template #content>
          <div class="alert-advanced-grid">
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
          </div>
        </template>
      </UCollapsible>
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
      <UButton
        color="primary"
        icon="i-lucide-search"
        label="应用"
        type="submit"
      />
    </UForm>

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
      <section
        v-else
        class="alert-queue-workspace"
        aria-labelledby="alert-queue-heading"
      >
        <header>
          <div>
            <span>处置队列</span>
            <h2 id="alert-queue-heading">
              告警处置队列
            </h2>
          </div>
          <p>{{ items.length }} 条 · {{ activeFilterLabel }} · firing 与高严重度优先</p>
        </header>
        <AlertQueue
          ref="alertQueue"
          :items="queueItems"
          :selected-id="inspector.selectedID.value"
          @select="openRow"
        />
      </section>
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
            @click="loadInspector(inspector.selectedID.value, true)"
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
        @retry="loadInspector(inspector.selectedID.value, true)"
      />
      <template v-else-if="inspectorDetail && selectedAlert">
        <AlertBadges
          :status="selectedAlert.status"
          :severity="selectedAlert.severity"
        />
        <section class="alert-inspector-current" aria-labelledby="alert-current-state">
          <span aria-hidden="true">
            <UIcon :name="selectedAlert.status === 'firing' ? 'i-lucide-siren' : 'i-lucide-circle-check'" />
          </span>
          <div>
            <h3 id="alert-current-state">
              {{ selectedAlert.status === "firing" ? "此告警仍在触发" : "此告警已经恢复" }}
            </h3>
            <p>
              {{ selectedAlert.acknowledgement ? "Owner 已知悉" : "尚未确认" }} ·
              {{ selectedAlert.silence?.status === "active" ? "通知静默中" : "通知未静默" }} ·
              {{ selectedAlert.incident_links.length ? `${selectedAlert.incident_links.length} 个关联 Incident` : "尚未关联 Incident" }}
            </p>
          </div>
        </section>

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

        <UCollapsible class="alert-technical-details">
          <template #default="{ open }">
            <UButton
              color="neutral"
              variant="ghost"
              block
              icon="i-lucide-braces"
              label="技术身份与完整时间"
              :trailing-icon="open ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
            />
          </template>
          <template #content>
            <dl class="alert-inspector-facts">
              <div><dt>Alert ID</dt><dd><code translate="no">{{ selectedAlert.id }}</code></dd></div>
              <div><dt>Provider</dt><dd>{{ selectedAlert.source }}</dd></div>
              <div><dt>当前版本</dt><dd><code translate="no">v{{ selectedAlert.version }}</code></dd></div>
              <div><dt>最近 Signal UTC</dt><dd><time :datetime="selectedAlert.last_seen_at">{{ formatUTC(selectedAlert.last_seen_at) }}</time></dd></div>
              <div><dt>Operational Scope</dt><dd><code translate="no">{{ selectedAlert.context_link.operational_scope_id || "未投影" }}</code></dd></div>
              <div><dt>来源</dt><dd>{{ selectedAlert.migrated_legacy ? "Legacy automatic ingress" : "Native Alert" }}</dd></div>
            </dl>
          </template>
        </UCollapsible>
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
  </WorkspacePageFrame>
</template>

<style scoped>
.alerts-workspace {
  display: grid;
  min-width: 0;
  gap: var(--co-space-4);
  container-name: alerts-workspace;
  container-type: inline-size;
}

.alerts-signal-deck {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr) minmax(360px, auto);
  align-items: center;
  gap: var(--co-space-6);
  padding: var(--co-space-6);
  border-radius: var(--co-radius-frame);
  background: color-mix(in srgb, var(--co-bg-surface) 82%, var(--co-bg-canvas));
  box-shadow: var(--co-shadow-row);
}
.alerts-signal-deck__main { display: grid; min-width: 0; gap: var(--co-space-4); }
.alert-attention__lead { display: flex; min-width: 0; align-items: center; gap: var(--co-space-4); }
.alert-attention__signal { display: grid; width: 58px; height: 58px; flex: 0 0 auto; place-items: center; border: 1px solid color-mix(in srgb, var(--co-status-critical-border) 56%, transparent); border-radius: var(--co-radius-panel); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); font-size: 23px; }
.alert-attention__lead > div { min-width: 0; }
.alert-attention__eyebrow,
.alert-queue-workspace > header span { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; font-weight: 800; text-transform: uppercase; }
.alerts-signal-deck h2 { margin: 4px 0 0; font-size: clamp(22px, 2vw, 30px); line-height: 1.15; letter-spacing: 0; }
.alerts-signal-deck p { max-width: 66ch; margin: var(--co-space-2) 0 0; color: var(--co-text-secondary); font-size: 11px; line-height: 1.6; }
.alert-signal-track { display: flex; min-width: 0; height: 6px; gap: 3px; overflow: hidden; border-radius: var(--co-radius-pill); background: var(--co-bg-canvas); }
.alert-signal-track span { min-width: 8px; border-radius: inherit; }
.alert-signal-track .is-critical { background: var(--co-status-critical-fg); }
.alert-signal-track .is-warning { background: var(--co-status-warning-fg); }
.alert-signal-track .is-quiet { background: var(--co-border-strong); }
.alert-attention__facets { display: grid; min-width: 0; grid-template-columns: repeat(4, minmax(84px, 1fr)); align-items: stretch; gap: 1px; overflow: hidden; border-radius: var(--co-radius-panel); background: var(--co-border-subtle); }
.alert-attention__facets :deep(.alert-attention__facet) { display: flex; min-width: 84px; min-height: 72px; align-items: center; padding: var(--co-space-3); border: 0; border-radius: 0; background: var(--co-bg-canvas); }
.alert-attention__facets :deep(.alert-attention__facet:first-child) { border-radius: var(--co-radius-panel) 0 0 var(--co-radius-panel); }
.alert-attention__facets :deep(.alert-attention__facet:last-child) { border-radius: 0 var(--co-radius-panel) var(--co-radius-panel) 0; }
.alert-attention__facets :deep(.alert-attention__facet.is-static) { cursor: default; }
.alert-attention__facets :deep(button:hover) { background: var(--co-bg-hover); transform: none; }
.alert-attention__facets span { display: grid; justify-items: start; gap: var(--co-space-2); text-align: left; }
.alert-attention__facets b { font-family: var(--co-font-mono); font-size: 20px; font-variant-numeric: tabular-nums; }
.alert-attention__facets small { color: var(--co-text-muted); font-size: 10px; }
.alert-attention__facets .is-critical { color: var(--co-status-critical-fg); }
.alert-attention__facets .is-warning { color: var(--co-status-warning-fg); }

.alert-commandbar {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(350px, auto) minmax(260px, 1fr) auto auto auto;
  align-items: center;
  gap: var(--co-space-2);
  padding: 6px 8px;
  border: 1px solid var(--co-border-subtle);
  border-radius: var(--co-radius-frame);
  background: color-mix(in srgb, var(--co-bg-surface) 88%, var(--co-bg-canvas));
  box-shadow: var(--co-shadow-row);
}
.alert-commandbar :deep(input),
.alert-commandbar :deep(button),
.alert-commandbar :deep([role="tablist"]) { border-radius: var(--co-radius-control); }
.alert-status-tabs { min-width: 0; }
.alert-advanced-filters { position: relative; }
.alert-advanced-grid { position: absolute; z-index: var(--co-z-popover); top: calc(100% + var(--co-space-2)); right: 0; display: grid; width: min(520px, calc(100vw - 64px)); grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--co-space-3); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-overlay); box-shadow: var(--co-shadow-overlay); }

.alert-queue-workspace { display: grid; min-width: 0; gap: var(--co-space-3); }
.alert-queue-workspace > header { display: flex; min-width: 0; align-items: end; justify-content: space-between; gap: var(--co-space-4); }
.alert-queue-workspace h2 { margin: 2px 0 0; font-size: 17px; }
.alert-queue-workspace p { margin: 0; color: var(--co-text-muted); font-size: 11px; text-align: right; }

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

.alert-inspector-current { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); padding: var(--co-space-3); border-radius: var(--co-radius-overlay); background: var(--co-bg-subtle); }
.alert-inspector-current > span { display: grid; width: 42px; height: 42px; flex: 0 0 auto; place-items: center; border-radius: var(--co-radius-overlay); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.alert-inspector-current > div { min-width: 0; }
.alert-inspector-current h3 { margin: 0; font-size: 14px; }
.alert-inspector-current p { margin: 3px 0 0; color: var(--co-text-muted); font-size: 11px; overflow-wrap: anywhere; }

.alert-inspector-section {
  display: grid;
  min-width: 0;
  gap: var(--co-space-3);
  padding-top: var(--co-space-3);
  border-top: 1px solid var(--co-border-default);
}
.alert-inspector-section h3 { margin: 0; font-size: 14px; }
.alert-inspector-actions { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-2); }
.alert-link-stack { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr); gap: var(--co-space-2); }
.alert-inspector-history { display: grid; margin: 0; padding: 0; list-style: none; }
.alert-inspector-history li { display: grid; min-width: 0; gap: 2px; padding: var(--co-space-2) 0; border-bottom: 1px solid var(--co-border-subtle); }
.alert-inspector-history strong,
.alert-inspector-history span { overflow-wrap: anywhere; }
.alert-inspector-history span,
.alert-inspector-history time,
.alert-empty-line { color: var(--co-text-muted); font-size: 11px; }
.alert-inspector-history time { font-family: var(--co-font-mono); }
.alert-empty-line { margin: 0; }
.alert-technical-details { overflow: hidden; border-radius: var(--co-radius-overlay); background: var(--co-bg-subtle); }
.alert-technical-details > :deep(button) { justify-content: flex-start; border-radius: var(--co-radius-overlay); }
.alert-technical-details .alert-inspector-facts { padding: 0 var(--co-space-3) var(--co-space-3); }

.alert-command-dialog { display: grid; min-width: 0; gap: var(--co-space-4); }
.alert-command-actions { display: flex; width: 100%; justify-content: flex-end; gap: var(--co-space-2); }

@media (max-width: 1180px) {
  .alerts-signal-deck { grid-template-columns: minmax(0, 1fr); }
  .alert-attention__facets { justify-content: flex-start; }
  .alert-commandbar { grid-template-columns: minmax(0, 1fr) minmax(240px, 1fr) auto auto; }
  .alert-commandbar > :last-child { grid-column: auto; }
}

@media (max-width: 1024px) {
  .alerts-signal-deck { grid-template-columns: minmax(0, 1fr); padding: var(--co-space-5); }
  .alert-attention__facets { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .alert-commandbar { grid-template-columns: minmax(0, 1fr) auto auto; }
  .alert-status-tabs,
  .alert-search-field { grid-column: 1 / -1; }
  .alert-advanced-filters { grid-column: 1 / -1; }
  .alert-advanced-grid { position: static; width: 100%; grid-template-columns: repeat(2, minmax(0, 1fr)); margin-top: var(--co-space-2); box-shadow: none; }
  .alert-queue-workspace > header { align-items: flex-start; flex-direction: column; }
  .alert-queue-workspace p { text-align: left; }
}

@container alerts-workspace (max-width: 900px) {
  .alerts-signal-deck { grid-template-columns: minmax(0, 1fr); padding: var(--co-space-4); }
  .alert-attention__facets { grid-column: 1; }
  .alert-commandbar { grid-template-columns: minmax(0, 1fr) auto auto auto; }
  .alert-status-tabs,
  .alert-search-field { grid-column: 1 / -1; }
  .alert-queue-workspace > header { align-items: flex-start; flex-direction: column; }
  .alert-queue-workspace p { text-align: left; }
}

@container alerts-workspace (max-width: 680px) {
  .alert-attention__facets :deep(.alert-attention__facet) { min-width: calc(50% - var(--co-space-1)); }
  .alert-commandbar { grid-template-columns: minmax(0, 1fr) auto auto; }
  .alerts-signal-deck h2 { font-size: 22px; }
}

@container alerts-workspace (max-width: 520px) {
  .alert-advanced-filters { grid-column: 1 / -1; }
  .alert-advanced-grid { position: static; width: 100%; grid-template-columns: repeat(2, minmax(0, 1fr)); margin-top: var(--co-space-2); box-shadow: none; }
}

@media (prefers-reduced-motion: reduce) {
  .alert-attention__facets :deep(button:hover) { transform: none; }
}
</style>
