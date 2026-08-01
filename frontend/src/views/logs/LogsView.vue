<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { isApiError } from "../../api/client";
import { getResources, type KubernetesResource } from "../../api/infrastructure";
import { getBootstrap, type BootstrapSnapshot } from "../../api/platform";
import {
  createTelemetryConsultation,
  getLogQueries,
  getLogQuery,
  getLogsCatalog,
  saveLogEvidence,
  startLogQuery,
  type Consultation,
  type LogEntry,
  type LogQuery,
  type TelemetryCatalog,
  type TelemetryEvidence,
  type TelemetryQueryMode,
} from "../../api/telemetry";
import LogInspector from "../../components/logs/LogInspector.vue";
import LogsHistory from "../../components/logs/LogsHistory.vue";
import LogsQueryControls from "../../components/logs/LogsQueryControls.vue";
import { buildLogsRouteQuery, parseLogsRoute } from "../../components/logs/logsRoute";
import VirtualLogList from "../../components/logs/VirtualLogList.vue";
import WorkspacePageFrame from "../../components/workspace/WorkspacePageFrame.vue";
import WorkspaceStatusRow from "../../components/workspace/WorkspaceStatusRow.vue";
import { useWorkspaceInspector } from "../../composables/useWorkspaceInspector";
import { resolveTelemetryResourceID } from "../../models/telemetry";
import { safeExternalURL } from "../../models/workbench";
import { openAgentPanel, publishAgentContext, type AgentPageContext } from "../../utils/agentContext";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";

interface RequestFailure {
  status: number | null;
  message: string;
  code: string;
  requestID: string;
  traceID: string;
  nextSteps: readonly string[];
}

const route = useRoute();
const router = useRouter();
const initialRoute = parseLogsRoute(route.query as Record<string, unknown>);
const {
  selectedID: inspectedEntryID,
  triggerElement: inspectorTrigger,
  open: openLogInspector,
  close: closeLogInspector,
} = useWorkspaceInspector({
  resolveTrigger(selectedID) {
    if (typeof document === "undefined") return null;
    return [...document.querySelectorAll<HTMLElement>("[data-log-entry-id]")]
      .find((element) => element.dataset.logEntryId === selectedID) ?? null;
  },
});
const bootstrap = ref<BootstrapSnapshot | null>(null);
const workloads = ref<KubernetesResource[]>([]);
const catalog = ref<TelemetryCatalog | null>(null);
const historyItems = ref<LogQuery[]>([]);
const currentQuery = ref<LogQuery | null>(null);
const selectedResourceID = ref(initialRoute.resource);
const selectedNamespace = ref(initialRoute.namespace);
const mode = ref<TelemetryQueryMode>(initialRoute.mode);
const textFilter = ref(initialRoute.text);
const traceFilter = ref(initialRoute.traceID);
const levels = ref<string[]>(initialRoute.levels);
const expertQuery = ref('{"match_all":{}}');
const fromValue = ref(routeTime(initialRoute.from) || toLocalInput(new Date(Date.now() - 15 * 60_000)));
const toValue = ref(routeTime(initialRoute.to) || toLocalInput(new Date()));
const limit = ref(initialRoute.limit);
const tail = ref(initialRoute.tail);
const wrapRows = ref(initialRoute.wrap);
const selectedEntryIDs = ref(new Set<string>());
const retainedEvidence = ref<TelemetryEvidence[]>([]);
const consultation = ref<Consultation | null>(null);
const loading = ref(true);
const querying = ref(false);
const savingEvidence = ref(false);
const freezing = ref(false);
const pageError = ref<RequestFailure | null>(null);
const queryError = ref<RequestFailure | null>(null);
const statusMessage = ref("");
let mounted = true;
let routeReady = false;
let routeMutationDepth = 0;
let workspaceGeneration = 0;
let queryGeneration = 0;
let workspaceController: AbortController | undefined;
let queryController: AbortController | undefined;
let lastWorkspaceRouteSignature = logsWorkspaceRouteSignature(route.query as Record<string, unknown>);

const namespaces = computed(() => bootstrap.value?.active_scope.namespaces ?? []);
const namespaceWorkloads = computed(() => workloads.value.filter((item) => (
  !selectedNamespace.value || item.namespace === selectedNamespace.value
)));
const selectedResource = computed(() => (
  workloads.value.find((item) => item.id === selectedResourceID.value) ?? null
));
const providerReady = computed(() => (
  catalog.value?.provider_state === "available" || catalog.value?.provider_state === "partial"
));
const validTimeRange = computed(() => {
  const from = new Date(fromValue.value).getTime();
  const to = new Date(toValue.value).getTime();
  return Number.isFinite(from) && Number.isFinite(to) && from < to;
});
const canRun = computed(() => Boolean(
  selectedResource.value
  && providerReady.value
  && validTimeRange.value
  && !querying.value
  && (mode.value === "guided" || expertQuery.value.trim()),
));
const entries = computed(() => currentQuery.value?.entries ?? []);
const inspectedEntry = computed(() => (
  entries.value.find((entry) => entry.id === inspectedEntryID.value) ?? null
));
const maxHistogramCount = computed(() => Math.max(
  1,
  ...(currentQuery.value?.histogram ?? []).map((bucket) => bucket.count),
));
const canFreeze = computed(() => (
  currentQuery.value?.status === "succeeded" && !currentQuery.value.result_expired
));
const workloadLocation = computed(() => ({
  path: "/infrastructure",
  query: {
    cluster: bootstrap.value?.active_scope.cluster_id ?? "",
    namespace: selectedNamespace.value,
    resource: selectedResourceID.value,
    from: validTimeRange.value ? new Date(fromValue.value).toISOString() : "",
    to: validTimeRange.value ? new Date(toValue.value).toISOString() : "",
  },
}));
const kibanaURL = computed(() => {
  const link = currentQuery.value?.links.find((item) => (
    item.provider === "kibana" && item.availability === "available"
  ));
  return safeExternalURL(link?.href);
});

const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "medium" });

function toLocalInput(value: Date): string {
  const offset = value.getTimezoneOffset() * 60_000;
  return new Date(value.getTime() - offset).toISOString().slice(0, 16);
}

function routeTime(value: string): string {
  const parsed = new Date(value);
  return value && !Number.isNaN(parsed.getTime()) ? toLocalInput(parsed) : "";
}

function formatTime(value?: string): string {
  if (!value) return "无";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : dateFormatter.format(parsed);
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value < 1) return "0 B";
  if (value < 1024) return `${value} B`;
  return `${(value / 1024).toFixed(value < 10240 ? 1 : 0)} KiB`;
}

function providerStateLabel(state?: TelemetryCatalog["provider_state"]): string {
  return ({ available: "可用", partial: "部分可用", unavailable: "不可用", disabled: "已停用" } as Record<string, string>)[state ?? ""] ?? "检查中";
}

function normalizeFailure(reason: unknown, fallback: string): RequestFailure {
  if (!isApiError(reason)) {
    return { status: null, message: fallback, code: "REQUEST_FAILED", requestID: "", traceID: "", nextSteps: [] };
  }
  return {
    status: reason.status,
    message: reason.message,
    code: reason.code || "REQUEST_FAILED",
    requestID: reason.requestID,
    traceID: reason.traceID,
    nextSteps: reason.nextSteps,
  };
}

function telemetryContext() {
  const resource = selectedResource.value;
  const clusterID = bootstrap.value?.active_scope.cluster_id;
  if (!resource || !resource.namespace || !clusterID) return null;
  return {
    cluster_id: clusterID,
    namespace: resource.namespace,
    resource: { id: resource.id, kind: resource.kind, namespace: resource.namespace, name: resource.name },
  };
}

function currentAgentContext(): AgentPageContext | null {
  const query = currentQuery.value;
  if (!query || query.status !== "succeeded" || query.result_expired) return null;
  return {
    route: route.fullPath,
    input: {
      title: `${query.resource.name} 日志上下文`,
      cluster_id: query.scope.cluster_id,
      environment: query.scope.environment,
      namespaces: [...query.scope.namespaces],
      resource_refs: [query.resource],
      filters: {
        workspace: "logs",
        provider: query.provider,
        mode: query.mode,
        query_hash: query.query_hash,
        levels: [...levels.value],
        text: textFilter.value,
        trace_id: traceFilter.value,
      },
      from: query.time_range.from,
      to: query.time_range.to,
      query_definition_refs: [],
      query_execution_refs: [query.id],
      evidence_refs: retainedEvidence.value.map((item) => item.id),
    },
  };
}

function logsWorkspaceRouteSignature(query: Record<string, unknown>): string {
  const parsed = parseLogsRoute(query);
  return JSON.stringify({ ...parsed, selectedEntryID: "" });
}

function publishCurrentAgentContext() {
  publishAgentContext(currentAgentContext());
}

function routeState(
  queryID = currentQuery.value?.id ?? "",
  selectedEntryID = inspectedEntryID.value,
) {
  return {
    cluster: bootstrap.value?.active_scope.cluster_id ?? "",
    namespace: selectedNamespace.value,
    resource: selectedResourceID.value,
    mode: mode.value,
    text: textFilter.value,
    traceID: traceFilter.value,
    levels: levels.value,
    limit: limit.value,
    tail: tail.value,
    wrap: wrapRows.value,
    queryID,
    selectedEntryID,
    from: validTimeRange.value ? new Date(fromValue.value).toISOString() : "",
    to: validTimeRange.value ? new Date(toValue.value).toISOString() : "",
  };
}

async function syncRoute(
  queryID = currentQuery.value?.id ?? "",
  selectedEntryID = inspectedEntryID.value,
) {
  routeMutationDepth += 1;
  try {
    await router.replace({ path: "/logs", query: buildLogsRouteQuery(routeState(queryID, selectedEntryID)) });
  } finally {
    await nextTick();
    routeMutationDepth -= 1;
  }
}

async function loadCatalogAndHistory(signal?: AbortSignal) {
  const context = telemetryContext();
  if (!context) {
    catalog.value = null;
    historyItems.value = [];
    return;
  }
  const [nextCatalog, nextHistory] = await Promise.all([
    getLogsCatalog(context, signal),
    getLogQueries({
      cluster_id: context.cluster_id,
      namespace: context.namespace,
      resource_id: context.resource.id,
      limit: 30,
    }, signal),
  ]);
  if (signal?.aborted) return;
  catalog.value = nextCatalog;
  historyItems.value = nextHistory;
}

function applyQuery(result: LogQuery) {
  currentQuery.value = result;
  mode.value = result.mode;
  expertQuery.value = result.query;
  tail.value = result.tail;
  fromValue.value = toLocalInput(new Date(result.time_range.from));
  toValue.value = toLocalInput(new Date(result.time_range.to));
  selectedEntryIDs.value = new Set();
  consultation.value = null;
}

async function loadWorkspace() {
  workspaceController?.abort();
  queryController?.abort();
  workspaceController = new AbortController();
  const signal = workspaceController.signal;
  const generation = ++workspaceGeneration;
  queryGeneration += 1;
  loading.value = true;
  pageError.value = null;
  queryError.value = null;
  currentQuery.value = null;
  selectedEntryIDs.value = new Set();
  retainedEvidence.value = [];
  consultation.value = null;
  try {
    const snapshot = await getBootstrap(signal);
    if (!mounted || signal.aborted || generation !== workspaceGeneration) return;
    bootstrap.value = snapshot;
    const parsedRoute = parseLogsRoute(route.query as Record<string, unknown>);
    selectedNamespace.value = parsedRoute.namespace || snapshot.active_scope.namespaces[0] || "";
    const page = await getResources({
      cluster: snapshot.active_scope.cluster_id,
      kind: ["Deployment", "StatefulSet", "DaemonSet"],
      limit: 500,
    }, signal);
    if (!mounted || signal.aborted || generation !== workspaceGeneration) return;
    workloads.value = page.items.filter((item) => item.layer === "workload");
    const resolved = resolveTelemetryResourceID(
      workloads.value,
      parsedRoute.resource,
      parsedRoute.legacyWorkload,
      selectedNamespace.value,
    );
    selectedResourceID.value = resolved
      || workloads.value.find((item) => item.namespace === selectedNamespace.value)?.id
      || workloads.value[0]?.id
      || "";
    if (selectedResource.value?.namespace) selectedNamespace.value = selectedResource.value.namespace;
    mode.value = parsedRoute.mode;
    textFilter.value = parsedRoute.text;
    traceFilter.value = parsedRoute.traceID;
    levels.value = parsedRoute.levels;
    limit.value = parsedRoute.limit;
    tail.value = parsedRoute.tail;
    wrapRows.value = parsedRoute.wrap;
    const from = routeTime(parsedRoute.from);
    const to = routeTime(parsedRoute.to);
    if (from && to) [fromValue.value, toValue.value] = [from, to];
    await loadCatalogAndHistory(signal);
    if (!mounted || signal.aborted || generation !== workspaceGeneration) return;
    if (parsedRoute.queryID) {
      const result = await getLogQuery(parsedRoute.queryID, signal);
      if (!mounted || signal.aborted || generation !== workspaceGeneration) return;
      applyQuery(result);
    }
    if (parsedRoute.legacyWorkload || parsedRoute.resource !== selectedResourceID.value) {
      await syncRoute(parsedRoute.queryID, parsedRoute.selectedEntryID);
    }
  } catch (reason) {
    if (!signal.aborted && mounted && generation === workspaceGeneration) {
      pageError.value = normalizeFailure(reason, "Logs Workspace 读取失败。");
    }
  } finally {
    if (!signal.aborted && generation === workspaceGeneration) loading.value = false;
  }
}

async function refreshAll() {
  statusMessage.value = "";
  await loadWorkspace();
}

async function changeNamespace() {
  selectedResourceID.value = namespaceWorkloads.value[0]?.id ?? "";
  await syncRoute("", "");
  await loadWorkspace();
}

async function changeResource() {
  if (selectedResource.value?.namespace) selectedNamespace.value = selectedResource.value.namespace;
  await syncRoute("", "");
  await loadWorkspace();
}

function changeMode(value: TelemetryQueryMode) {
  mode.value = value;
  void syncRoute("", "");
}

function selectPreset(minutes: number) {
  const to = new Date();
  fromValue.value = toLocalInput(new Date(to.getTime() - minutes * 60_000));
  toValue.value = toLocalInput(to);
  currentQuery.value = null;
  selectedEntryIDs.value = new Set();
  void syncRoute("", "");
}

function selectBucket(from: string, to: string) {
  fromValue.value = toLocalInput(new Date(from));
  toValue.value = toLocalInput(new Date(to));
  currentQuery.value = null;
  selectedEntryIDs.value = new Set();
  void syncRoute("", "");
}

function toggleLevel(level: string, checked: boolean) {
  const next = new Set(levels.value);
  if (checked) next.add(level);
  else next.delete(level);
  levels.value = [...next];
}

function updateTail(value: boolean) {
  tail.value = value;
  void syncRoute(currentQuery.value?.id ?? "");
}

function updateWrap(value: boolean) {
  wrapRows.value = value;
  void syncRoute(currentQuery.value?.id ?? "");
}

async function runQuery() {
  const context = telemetryContext();
  if (!context || !canRun.value) return;
  queryController?.abort();
  queryController = new AbortController();
  const signal = queryController.signal;
  const generation = ++queryGeneration;
  querying.value = true;
  queryError.value = null;
  statusMessage.value = "";
  try {
    const result = await startLogQuery({
      ...context,
      mode: mode.value,
      query: mode.value === "expert" ? expertQuery.value : undefined,
      filter: mode.value === "guided" ? {
        text: textFilter.value.trim() || undefined,
        levels: levels.value.length ? levels.value : undefined,
        trace_id: traceFilter.value.trim() || undefined,
      } : {},
      from: new Date(fromValue.value).toISOString(),
      to: new Date(toValue.value).toISOString(),
      limit: limit.value,
      tail: tail.value,
    }, signal);
    if (!mounted || signal.aborted || generation !== queryGeneration) return;
    applyQuery(result);
    historyItems.value = [result, ...historyItems.value.filter((item) => item.id !== result.id)].slice(0, 30);
    await syncRoute(result.id, "");
  } catch (reason) {
    if (!signal.aborted && mounted && generation === queryGeneration) {
      queryError.value = normalizeFailure(reason, "Elasticsearch 查询失败。");
    }
  } finally {
    if (generation === queryGeneration) querying.value = false;
  }
}

function cancelQueryRequest() {
  if (!querying.value) return;
  queryController?.abort();
  queryGeneration += 1;
  querying.value = false;
  statusMessage.value = "已停止等待当前请求；若服务端已经接受执行，可稍后从查询历史重新读取。";
}

async function openHistory(id: string) {
  queryController?.abort();
  queryController = new AbortController();
  const signal = queryController.signal;
  const generation = ++queryGeneration;
  querying.value = true;
  queryError.value = null;
  try {
    const result = await getLogQuery(id, signal);
    if (!mounted || signal.aborted || generation !== queryGeneration) return;
    applyQuery(result);
    await syncRoute(result.id, "");
  } catch (reason) {
    if (!signal.aborted && generation === queryGeneration) {
      queryError.value = normalizeFailure(reason, "日志查询历史读取失败。");
    }
  } finally {
    if (generation === queryGeneration) querying.value = false;
  }
}

function toggleEntry(id: string) {
  const next = new Set(selectedEntryIDs.value);
  if (next.has(id)) next.delete(id);
  else if (next.size < 32) next.add(id);
  selectedEntryIDs.value = next;
}

function inspectEntry(entry: LogEntry, trigger: HTMLElement) {
  void openLogInspector(entry.id, trigger);
}

function closeInspector(value: boolean) {
  if (!value) void closeLogInspector();
}

function openTrace(entry: LogEntry) {
  if (!entry.trace_id) return;
  const at = new Date(entry.timestamp).getTime();
  const from = Number.isFinite(at) ? new Date(at - 5 * 60_000).toISOString() : new Date(fromValue.value).toISOString();
  const to = Number.isFinite(at) ? new Date(at + 5 * 60_000).toISOString() : new Date(toValue.value).toISOString();
  void router.push({
    path: "/traces",
    query: {
      cluster: bootstrap.value?.active_scope.cluster_id ?? "",
      namespace: selectedNamespace.value,
      resource: selectedResourceID.value,
      from,
      to,
      trace_id: entry.trace_id,
    },
  });
}

async function retainSelectedEvidence() {
  const query = currentQuery.value;
  if (!query || selectedEntryIDs.value.size === 0 || savingEvidence.value) return;
  savingEvidence.value = true;
  queryError.value = null;
  try {
    const evidence = await saveLogEvidence(query.id, [...selectedEntryIDs.value]);
    retainedEvidence.value = [evidence, ...retainedEvidence.value];
    statusMessage.value = `已保留 ${evidence.item_count} 条日志 Evidence。`;
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Evidence 保存失败。");
  } finally {
    savingEvidence.value = false;
  }
}

function openCurrentInAgent() {
  const context = currentAgentContext();
  if (!context) {
    statusMessage.value = "请先完成一个仍保留结果的查询，再关联或新建 Agent 调查。";
    return;
  }
  openAgentPanel({ context });
}

async function freezeContext() {
  const query = currentQuery.value;
  const resource = telemetryContext()?.resource;
  const scope = bootstrap.value?.active_scope;
  if (!query || !resource || !scope || !canFreeze.value || freezing.value) return;
  freezing.value = true;
  queryError.value = null;
  try {
    const context = currentAgentContext();
    consultation.value = await createTelemetryConsultation({
      title: `${resource.name} 日志上下文`,
      cluster_id: scope.cluster_id,
      environment: scope.environment,
      namespaces: [resource.namespace],
      resource_refs: [resource],
      filters: context?.input.filters,
      from: query.time_range.from,
      to: query.time_range.to,
      query_definition_refs: context?.input.query_definition_refs,
      query_execution_refs: [query.id],
      evidence_refs: retainedEvidence.value.map((item) => item.id),
    });
    statusMessage.value = "当前日志查询已冻结为不可变 Context Snapshot。";
    if (context) openAgentPanel({ consultationId: consultation.value.id, context });
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Context Snapshot 创建失败。");
  } finally {
    freezing.value = false;
  }
}

function receiveScopeChange() {
  void loadWorkspace();
}

onMounted(async () => {
  window.addEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, receiveScopeChange);
  await loadWorkspace();
  if (mounted) routeReady = true;
});

watch(() => route.fullPath, () => {
  const nextSignature = logsWorkspaceRouteSignature(route.query as Record<string, unknown>);
  const selectionOnly = nextSignature === lastWorkspaceRouteSignature;
  lastWorkspaceRouteSignature = nextSignature;
  publishCurrentAgentContext();
  if (routeReady && routeMutationDepth === 0 && !selectionOnly) void loadWorkspace();
});
watch([currentQuery, retainedEvidence], publishCurrentAgentContext, { flush: "post" });

onBeforeUnmount(() => {
  mounted = false;
  workspaceGeneration += 1;
  queryGeneration += 1;
  workspaceController?.abort();
  queryController?.abort();
  window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, receiveScopeChange);
  publishAgentContext(null);
});
</script>

<template>
  <WorkspacePageFrame
    class="logs-workspace"
    width="full"
    aria-labelledby="logs-heading"
  >
    <header class="logs-workspace__heading">
      <div>
        <span>可观测性</span>
        <h1 id="logs-heading">
          日志
        </h1>
        <p>{{ selectedResource ? `${selectedResource.kind} ${selectedResource.name}` : "当前运行范围" }} · 搜索日志、追踪上下文并保留 Evidence</p>
      </div>
      <UTooltip text="刷新日志工作区">
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-refresh-cw"
          square
          aria-label="刷新日志工作区"
          :loading="loading"
          @click="refreshAll"
        />
      </UTooltip>
    </header>

    <WorkspaceStatusRow
      :tone="providerReady ? 'success' : catalog ? 'error' : 'neutral'"
      :icon="providerReady ? 'i-lucide-scroll-text' : 'i-lucide-circle-alert'"
      :title="`Elasticsearch ${providerStateLabel(catalog?.provider_state)}`"
      :description="catalog?.provider_detail || '正在确认当前 Configuration Revision 的日志端点'"
      :badge="currentQuery?.tail ? '实时模式' : ''"
      :busy="loading"
    >
      <template #meta>
        {{ bootstrap?.active_scope.cluster_id || "活动集群" }} / {{ selectedNamespace || "Namespace" }}
      </template>
    </WorkspaceStatusRow>

    <WorkspaceState
      v-if="pageError"
      :kind="pageError.status === 403 ? 'permission-denied' : 'error'"
      :title="pageError.code"
      :description="pageError.message"
      :request-i-d="pageError.requestID"
      :trace-i-d="pageError.traceID"
      :next-steps="pageError.nextSteps"
    >
      <template #actions>
        <UButton
          color="error"
          variant="soft"
          icon="i-lucide-rotate-cw"
          label="重试"
          @click="loadWorkspace"
        />
      </template>
    </WorkspaceState>
    <WorkspaceState
      v-if="queryError"
      :kind="queryError.status === 403 ? 'permission-denied' : 'error'"
      :title="queryError.code"
      :description="queryError.message"
      :request-i-d="queryError.requestID"
      :trace-i-d="queryError.traceID"
      :next-steps="queryError.nextSteps"
    />
    <UAlert
      v-if="statusMessage"
      color="success"
      variant="soft"
      icon="i-lucide-circle-check"
      title="状态更新"
      :description="statusMessage"
      role="status"
    />

    <WorkspaceState
      v-if="loading"
      kind="loading"
      title="正在读取活动 Scope"
      description="加载真实 Workload、Elasticsearch 查询目录与历史身份。"
    />

    <template v-else>
      <LogsQueryControls
        :namespaces="namespaces"
        :resources="namespaceWorkloads"
        :catalog="catalog"
        :namespace="selectedNamespace"
        :resource-i-d="selectedResourceID"
        :mode="mode"
        :text="textFilter"
        :trace-i-d="traceFilter"
        :levels="levels"
        :expert-query="expertQuery"
        :from="fromValue"
        :to="toValue"
        :limit="limit"
        :tail="tail"
        :valid-time-range="validTimeRange"
        :can-run="canRun"
        :querying="querying"
        @update:namespace="selectedNamespace = $event"
        @update:resource-i-d="selectedResourceID = $event"
        @update:mode="changeMode"
        @update:text="textFilter = $event"
        @update:trace-i-d="traceFilter = $event"
        @update:expert-query="expertQuery = $event"
        @update:from="fromValue = $event"
        @update:to="toValue = $event"
        @update:limit="limit = $event"
        @update:tail="updateTail"
        @level-toggle="toggleLevel"
        @namespace-change="changeNamespace"
        @resource-change="changeResource"
        @preset="selectPreset"
        @run="runQuery"
        @cancel="cancelQueryRequest"
      />

      <UAlert
        v-if="catalog && !providerReady"
        color="error"
        variant="soft"
        icon="i-lucide-ban"
        :title="`Elasticsearch ${providerStateLabel(catalog.provider_state)}`"
        :description="`${catalog.provider_detail} · ${catalog.source.identity || '当前 Configuration Revision 没有可用采集端点'}`"
      />

      <section
        v-if="currentQuery?.histogram.length"
        class="logs-histogram"
        aria-labelledby="logs-histogram-heading"
      >
        <header>
          <h2 id="logs-histogram-heading">
            日志分布
          </h2>
          <span>{{ currentQuery.histogram.length }} 个时间桶</span>
        </header>
        <div :aria-label="`${currentQuery.histogram.length} 个日志时间桶`">
          <UTooltip
            v-for="bucket in currentQuery.histogram"
            :key="bucket.from"
            :text="`${formatTime(bucket.from)} · ${bucket.count} 条`"
          >
            <UButton
              class="logs-histogram__bucket"
              color="primary"
              variant="ghost"
              :aria-label="`${formatTime(bucket.from)}，${bucket.count} 条日志`"
              @click="selectBucket(bucket.from, bucket.to)"
            >
              <span :style="{ height: `${Math.max(4, (bucket.count / maxHistogramCount) * 100)}%` }" />
            </UButton>
          </UTooltip>
        </div>
      </section>

      <div class="logs-workspace__grid">
        <main class="logs-results">
          <header class="logs-results__header">
            <div>
              <span>Query Execution</span>
              <h2>日志结果</h2>
            </div>
            <div>
              <USwitch
                :model-value="wrapRows"
                label="换行"
                @update:model-value="updateWrap(Boolean($event))"
              />
              <UButton
                color="neutral"
                variant="outline"
                icon="i-lucide-save"
                label="保存 Evidence"
                :disabled="selectedEntryIDs.size === 0 || savingEvidence || currentQuery?.result_expired"
                :loading="savingEvidence"
                @click="retainSelectedEvidence"
              />
              <UButton
                color="neutral"
                variant="outline"
                icon="i-lucide-bot"
                label="关联 Agent"
                :disabled="!canFreeze"
                @click="openCurrentInAgent"
              />
              <UButton
                color="neutral"
                variant="outline"
                icon="i-lucide-box"
                label="Workload"
                :to="workloadLocation"
              />
              <UButton
                v-if="kibanaURL"
                color="neutral"
                variant="outline"
                icon="i-lucide-external-link"
                label="Kibana"
                :to="kibanaURL"
                target="_blank"
                rel="noopener noreferrer"
                external
              />
            </div>
          </header>

          <div
            v-if="currentQuery"
            class="logs-results__meta"
          >
            <span><b>{{ currentQuery.result_count }}</b> rows</span>
            <span><b>{{ formatBytes(currentQuery.response_bytes) }}</b></span>
            <span>采集 {{ formatTime(currentQuery.source.collected_at) }}</span>
            <UBadge
              v-if="currentQuery.tail"
              color="info"
              variant="soft"
              label="Tail"
            />
            <UBadge
              v-if="currentQuery.truncated"
              color="warning"
              variant="soft"
              label="已截断"
            />
          </div>
          <WorkspaceState
            v-if="currentQuery?.partial"
            kind="partial"
            title="Provider 仅返回部分日志"
            description="可用日志继续显示；Evidence 会保留当前 partial 与 truncated 事实。"
          />
          <WorkspaceState
            v-if="currentQuery?.stale"
            kind="stale"
            title="日志结果已陈旧"
            description="当前内容仍可检查，但不声明它代表最新 Provider 状态。"
          />
          <WorkspaceState
            v-if="currentQuery?.result_expired"
            kind="expired"
            title="Provider 日志结果已过期"
            description="仅保留 Query Execution 审计元数据；请重新执行后再保存 Evidence。"
          />
          <WorkspaceState
            v-if="!currentQuery"
            kind="empty"
            title="尚无日志查询"
            description="选择真实 Workload 与时间范围后执行。"
          />
          <WorkspaceState
            v-else-if="currentQuery.status === 'succeeded' && !currentQuery.result_expired && !entries.length"
            kind="empty"
            title="此范围没有日志"
            description="资源、过滤条件与时间上下文保持不变。"
          />
          <WorkspaceState
            v-else-if="currentQuery.status === 'failed'"
            kind="error"
            :title="currentQuery.error_code || '日志查询失败'"
            :description="currentQuery.error_detail || '服务端保留了失败的执行身份。'"
          />
          <VirtualLogList
            v-else-if="entries.length"
            :entries="entries"
            :wrap="wrapRows"
            :selected-i-ds="selectedEntryIDs"
            :inspected-i-d="inspectedEntryID"
            @toggle="toggleEntry"
            @inspect="inspectEntry"
            @open-trace="openTrace"
            @copied="statusMessage = '完整日志原文已复制。'"
          />

          <section
            class="logs-snapshot"
            aria-labelledby="logs-context-heading"
          >
            <div>
              <UIcon
                name="i-lucide-archive"
                aria-hidden="true"
              />
              <div>
                <h2 id="logs-context-heading">
                  冻结上下文
                </h2>
                <span>{{ retainedEvidence.length }} 条 Evidence · {{ currentQuery ? 1 : 0 }} 个 Query Execution</span>
              </div>
            </div>
            <UButton
              color="primary"
              icon="i-lucide-archive"
              label="创建 Snapshot"
              :disabled="!canFreeze || freezing"
              :loading="freezing"
              @click="freezeContext"
            />
          </section>
          <dl
            v-if="consultation"
            class="logs-snapshot__proof"
            data-testid="context-snapshot"
          >
            <div><dt>Consultation</dt><dd>{{ consultation.id }}</dd></div>
            <div><dt>Snapshot</dt><dd>{{ consultation.context_snapshot.id }}</dd></div>
            <div><dt>Content hash</dt><dd>{{ consultation.context_snapshot.content_hash }}</dd></div>
          </dl>
        </main>

        <LogsHistory
          :items="historyItems"
          :active-i-d="currentQuery?.id ?? ''"
          @select="openHistory"
        />
      </div>
    </template>
  </WorkspacePageFrame>

  <LogInspector
    :open="Boolean(inspectedEntryID)"
    :entry="inspectedEntry"
    :target-i-d="inspectedEntryID"
    :trigger="inspectorTrigger"
    :selected="Boolean(inspectedEntry && selectedEntryIDs.has(inspectedEntry.id))"
    @update:open="closeInspector"
    @toggle-evidence="toggleEntry"
    @open-trace="openTrace"
  />
</template>

<style scoped>
.logs-workspace {
  padding: var(--co-space-5) clamp(var(--co-space-4), 2.5vw, var(--co-space-8)) var(--co-space-10);
}
.logs-workspace code { min-width: 0; overflow-wrap: anywhere; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 11px; }
.logs-workspace__heading { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--co-space-4); padding-bottom: var(--co-space-4); }
.logs-workspace__heading > div { min-width: 0; }
.logs-workspace__heading span { color: var(--co-text-muted); font-size: 11px; }
.logs-workspace__heading h1 { margin: 3px 0 0; font-size: 24px; line-height: 1.2; }
.logs-workspace__heading p { margin: var(--co-space-1) 0 0; color: var(--co-text-secondary); font-size: 12px; overflow-wrap: anywhere; }
.logs-workspace__grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(240px, 290px); gap: var(--co-space-6); margin-top: var(--co-space-4); }
.logs-results { min-width: 0; }
.logs-results__header { display: flex; min-height: 54px; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.logs-results__header > div:first-child span { color: var(--co-text-muted); font-size: 10px; font-weight: 750; text-transform: uppercase; }
.logs-results__header h2 { margin: 2px 0 0; font-size: 17px; }
.logs-results__header > div:last-child { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); }
.logs-results__meta { display: flex; flex-wrap: wrap; align-items: center; gap: var(--co-space-2) var(--co-space-4); padding: var(--co-space-2) 0 var(--co-space-3); border-bottom: 1px solid var(--co-border-default); color: var(--co-text-secondary); font-size: 11px; }
.logs-histogram { padding: var(--co-space-3) 0; border-bottom: 1px solid var(--co-border-default); }
.logs-histogram > header { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.logs-histogram h2 { margin: 0; font-size: 14px; }
.logs-histogram header span { color: var(--co-text-muted); font-size: 11px; }
.logs-histogram > div { display: flex; height: 80px; align-items: end; gap: 2px; overflow: hidden; border-bottom: 1px solid var(--co-border-strong); }
.logs-histogram__bucket { position: relative; width: 100%; min-width: 7px; height: 100%; padding: 0; }
.logs-histogram__bucket span { position: absolute; right: 1px; bottom: 0; left: 1px; min-height: 4px; background: var(--co-action-primary); }
.logs-snapshot { display: flex; min-height: 66px; align-items: center; justify-content: space-between; gap: var(--co-space-4); padding: var(--co-space-3) 0; border-block: 1px solid var(--co-border-default); }
.logs-snapshot > div { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); }
.logs-snapshot h2 { margin: 0; font-size: 14px; }
.logs-snapshot span { color: var(--co-text-muted); font-size: 11px; }
.logs-snapshot__proof { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: 0; border-bottom: 1px solid var(--co-border-default); }
.logs-snapshot__proof div { min-width: 0; padding: var(--co-space-2); border-right: 1px solid var(--co-border-default); }
.logs-snapshot__proof div:last-child { border-right: 0; }
.logs-snapshot__proof dt { color: var(--co-text-muted); font-size: 10px; }
.logs-snapshot__proof dd { margin: var(--co-space-1) 0 0; overflow-wrap: anywhere; font-family: var(--co-font-mono); font-size: 10px; }

@media (max-width: 1024px) {
  .logs-workspace { padding-inline: var(--co-space-4); }
  .logs-workspace__grid { grid-template-columns: minmax(0, 1fr); }
  .logs-results__header { align-items: flex-start; flex-direction: column; }
  .logs-results__header > div:last-child { justify-content: flex-start; }
}

@media (prefers-reduced-motion: reduce) {
  .logs-workspace * { scroll-behavior: auto; }
}

</style>
