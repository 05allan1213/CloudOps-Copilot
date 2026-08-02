<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { isApiError } from "../../api/client";
import { getResources, type KubernetesResource } from "../../api/infrastructure";
import { getBootstrap, type BootstrapSnapshot } from "../../api/platform";
import {
  createTelemetryConsultation,
  getLogEvidence,
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
import WorkspaceHeader from "../../components/workspace/WorkspaceHeader.vue";
import WorkspacePageFrame from "../../components/workspace/WorkspacePageFrame.vue";
import { invalidateQueryDomain } from "../../composables/queryCache";
import { useWorkspaceInspector } from "../../composables/useWorkspaceInspector";
import { resolveTelemetryResourceID } from "../../models/telemetry";
import { safeExternalURL } from "../../models/workbench";
import {
  openAgentPanel,
  publishAgentContext,
  shouldClearAgentContextOnUnmount,
  type AgentPageContext,
} from "../../utils/agentContext";
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
const logOverview = computed(() => ({
  errors: entries.value.filter((entry) => ["error", "fatal"].includes(entry.level?.toLowerCase() ?? "")).length,
  warnings: entries.value.filter((entry) => ["warn", "warning"].includes(entry.level?.toLowerCase() ?? "")).length,
  services: new Set(entries.value.map((entry) => entry.service || entry.resource.name).filter(Boolean)).size,
}));
const inspectedEntry = computed(() => (
  entries.value.find((entry) => entry.id === inspectedEntryID.value) ?? null
));
const inspectedEntryContext = computed(() => {
  const index = entries.value.findIndex((entry) => entry.id === inspectedEntryID.value);
  if (index < 0) return [];
  return entries.value.slice(Math.max(0, index - 2), Math.min(entries.value.length, index + 3));
});
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

function formatElapsed(createdAt: string, completedAt?: string): string {
  const started = new Date(createdAt).getTime();
  const finished = new Date(completedAt ?? "").getTime();
  if (!Number.isFinite(started) || !Number.isFinite(finished) || finished < started) return "耗时未知";
  const elapsed = finished - started;
  if (elapsed < 1000) return `${elapsed} ms`;
  return `${(elapsed / 1000).toFixed(elapsed < 10_000 ? 2 : 1)} s`;
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
  retainedEvidence.value = [];
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
      const [result, evidence] = await Promise.all([
        getLogQuery(parsedRoute.queryID, signal),
        getLogEvidence(parsedRoute.queryID, signal),
      ]);
      if (!mounted || signal.aborted || generation !== workspaceGeneration) return;
      applyQuery(result);
      retainedEvidence.value = evidence;
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
  invalidateQueryDomain(["platform", "infrastructure", "logs"]);
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

function prepareLogQuery(nextText: string, nextLevels: string[], minutes?: number) {
  mode.value = "guided";
  tail.value = false;
  textFilter.value = nextText;
  levels.value = [...nextLevels];
  if (minutes !== undefined) {
    const to = new Date();
    fromValue.value = toLocalInput(new Date(to.getTime() - minutes * 60_000));
    toValue.value = toLocalInput(to);
  }
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
    const [result, evidence] = await Promise.all([
      getLogQuery(id, signal),
      getLogEvidence(id, signal),
    ]);
    if (!mounted || signal.aborted || generation !== queryGeneration) return;
    applyQuery(result);
    retainedEvidence.value = evidence;
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
  if (shouldClearAgentContextOnUnmount(route.path)) publishAgentContext(null);
});
</script>

<template>
  <WorkspacePageFrame
    class="logs-workspace"
    width="full"
    aria-labelledby="logs-heading"
  >
    <WorkspaceHeader
      title="日志分析"
      :description="`${selectedResource ? `${selectedResource.kind} ${selectedResource.name} · ${selectedResource.namespace}` : '当前运行范围'} · 在时间流中定位异常并保留 Evidence`"
    >
      <template #actions>
        <UTooltip text="刷新日志工作区">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-refresh-cw"
            square
            aria-label="刷新日志工作区"
            :loading="loading"
            @click="refreshAll"
          />
        </UTooltip>
      </template>
    </WorkspaceHeader>

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
      <div class="logs-workspace__grid">
        <main class="logs-analysis">
          <aside
            class="logs-query-rail"
            aria-label="日志查询面板"
          >
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
          </aside>

          <section
            class="logs-stream-stage"
            aria-labelledby="logs-stream-heading"
          >
            <header class="logs-analysis__header">
              <div class="logs-analysis__identity">
                <h2 id="logs-stream-heading">
                  {{ currentQuery?.tail ? "Tail 查询结果" : "日志结果" }}
                </h2>
                <p v-if="currentQuery">
                  {{ currentQuery.result_count }} 条日志 · {{ formatElapsed(currentQuery.created_at, currentQuery.completed_at) }} · {{ formatBytes(currentQuery.response_bytes) }}
                </p>
                <p v-else>
                  运行查询后在此检查时间分布、日志原文与关联 Trace。
                </p>
              </div>
              <div class="logs-analysis__actions">
                <UButton
                  color="neutral"
                  variant="soft"
                  icon="i-lucide-save"
                  :label="selectedEntryIDs.size ? `保存 ${selectedEntryIDs.size} 条` : '保存 Evidence'"
                  :disabled="selectedEntryIDs.size === 0 || savingEvidence || currentQuery?.result_expired"
                  :loading="savingEvidence"
                  @click="retainSelectedEvidence"
                />
                <UButton
                  color="neutral"
                  variant="soft"
                  icon="i-lucide-bot"
                  label="交给 Agent"
                  :disabled="!canFreeze"
                  @click="openCurrentInAgent"
                />
                <UPopover :content="{ align: 'end', side: 'bottom', sideOffset: 8, collisionPadding: 16, sticky: 'always' }">
                  <UButton
                    color="neutral"
                    variant="ghost"
                    icon="i-lucide-history"
                    :label="`查询历史 ${historyItems.length}`"
                  />
                  <template #content>
                    <div class="logs-history-popover">
                      <LogsHistory
                        :items="historyItems"
                        :active-i-d="currentQuery?.id ?? ''"
                        @select="openHistory"
                      />
                    </div>
                  </template>
                </UPopover>
                <UPopover>
                  <UButton
                    color="neutral"
                    variant="ghost"
                    icon="i-lucide-settings-2"
                    label="视图设置"
                  />
                  <template #content>
                    <div class="logs-display-menu">
                      <USwitch
                        :model-value="wrapRows"
                        label="日志正文换行"
                        @update:model-value="updateWrap(Boolean($event))"
                      />
                      <span>关闭换行时，日志原文保留有界横向滚动。</span>
                    </div>
                  </template>
                </UPopover>
                <UPopover>
                  <UButton
                    color="neutral"
                    variant="ghost"
                    icon="i-lucide-ellipsis"
                    label="更多"
                  />
                  <template #content>
                    <div class="logs-more-menu">
                      <UButton
                        color="neutral"
                        variant="ghost"
                        icon="i-lucide-box"
                        label="打开关联 Workload"
                        :to="workloadLocation"
                      />
                      <UButton
                        v-if="kibanaURL"
                        color="neutral"
                        variant="ghost"
                        icon="i-lucide-external-link"
                        label="在 Kibana 中打开"
                        :to="kibanaURL"
                        target="_blank"
                        rel="noopener noreferrer"
                        external
                      />
                      <UButton
                        color="neutral"
                        variant="ghost"
                        icon="i-lucide-archive"
                        label="创建 Snapshot"
                        :disabled="!canFreeze || freezing"
                        :loading="freezing"
                        @click="freezeContext"
                      />
                    </div>
                  </template>
                </UPopover>
              </div>
            </header>

            <dl
              v-if="currentQuery"
              class="logs-result-metrics"
              aria-label="日志结果摘要"
            >
              <div><dt>日志</dt><dd>{{ currentQuery.result_count }}</dd></div>
              <div>
                <dt>错误</dt><dd :class="{ 'is-error': logOverview.errors > 0 }">
                  {{ logOverview.errors }}
                </dd>
              </div>
              <div><dt>警告</dt><dd>{{ logOverview.warnings }}</dd></div>
              <div><dt>服务</dt><dd>{{ logOverview.services }}</dd></div>
              <div><dt>采集时间</dt><dd>{{ formatTime(currentQuery.source.collected_at) }}</dd></div>
            </dl>

            <section
              v-if="currentQuery?.histogram.length"
              class="logs-distribution"
              aria-labelledby="logs-histogram-heading"
            >
              <header>
                <div>
                  <h3 id="logs-histogram-heading">
                    时间分布
                  </h3>
                </div>
                <span v-if="currentQuery?.histogram.length">{{ currentQuery.histogram.length }} 个时间桶 · 点击收窄范围</span>
                <span v-else>等待真实查询结果</span>
              </header>
              <div
                class="logs-distribution__buckets"
                :aria-label="`${currentQuery.histogram.length} 个日志时间桶`"
              >
                <UTooltip
                  v-for="bucket in currentQuery.histogram"
                  :key="bucket.from"
                  :text="`${formatTime(bucket.from)} · ${bucket.count} 条`"
                >
                  <UButton
                    class="logs-distribution__bucket"
                    color="primary"
                    variant="ghost"
                    :aria-label="`${formatTime(bucket.from)}，${bucket.count} 条日志`"
                    @click="selectBucket(bucket.from, bucket.to)"
                  >
                    <span :style="{ height: bucket.count ? `${Math.max(4, (bucket.count / maxHistogramCount) * 100)}%` : '0' }" />
                  </UButton>
                </UTooltip>
              </div>
            </section>

            <div
              v-if="currentQuery"
              class="logs-analysis__meta"
            >
              <span><b>{{ currentQuery.tail ? "TAIL" : "SEARCH" }}</b></span>
              <span>{{ currentQuery.time_range.from }} → {{ currentQuery.time_range.to }}</span>
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
              v-if="currentQuery?.status === 'failed'"
              kind="error"
              :title="currentQuery.error_code || '日志查询失败'"
              :description="currentQuery.error_detail || '服务端保留了失败的执行身份。'"
            />
            <VirtualLogList
              v-if="entries.length"
              :entries="entries"
              :wrap="wrapRows"
              :selected-i-ds="selectedEntryIDs"
              :inspected-i-d="inspectedEntryID"
              :query-identity="currentQuery?.id ?? ''"
              :follow="Boolean(currentQuery?.tail)"
              :highlight="mode === 'guided' ? textFilter : ''"
              @toggle="toggleEntry"
              @inspect="inspectEntry"
              @open-trace="openTrace"
              @copied="statusMessage = '完整日志原文已复制。'"
            />
            <section
              v-if="!currentQuery"
              class="logs-stream-empty"
              aria-live="polite"
            >
              <div class="logs-stream-empty__copy">
                <span><UIcon
                  name="i-lucide-text-search"
                  aria-hidden="true"
                /></span>
                <div>
                  <h3>尚未运行查询</h3>
                  <p>选择时间范围并输入查询条件后开始搜索。</p>
                </div>
              </div>
              <div class="logs-stream-empty__suggestions">
                <span>常用查询</span>
                <div>
                  <UButton
                    color="neutral"
                    variant="soft"
                    icon="i-lucide-circle-alert"
                    label="最近错误"
                    @click="prepareLogQuery('', ['error'], 15)"
                  />
                  <UButton
                    color="neutral"
                    variant="soft"
                    icon="i-lucide-clock-alert"
                    label="连接超时"
                    @click="prepareLogQuery('connection timeout', [], 60)"
                  />
                  <UButton
                    color="neutral"
                    variant="soft"
                    icon="i-lucide-server-off"
                    label="HTTP 5xx"
                    @click="prepareLogQuery('status=5', [], 60)"
                  />
                </div>
              </div>
            </section>
            <section
              v-else-if="currentQuery?.status === 'succeeded' && !currentQuery.result_expired && entries.length === 0"
              class="logs-stream-empty logs-stream-empty--no-results"
              aria-live="polite"
            >
              <div class="logs-stream-empty__copy">
                <span><UIcon
                  name="i-lucide-search-x"
                  aria-hidden="true"
                /></span>
                <div>
                  <h3>没有找到符合条件的日志</h3>
                  <p>当前范围：{{ currentQuery.resource.namespace }} / {{ currentQuery.resource.name }} / {{ formatTime(currentQuery.time_range.from) }} 至 {{ formatTime(currentQuery.time_range.to) }}</p>
                </div>
              </div>
              <div class="logs-stream-empty__suggestions">
                <span>建议调整</span>
                <div>
                  <UButton
                    color="neutral"
                    variant="ghost"
                    label="扩大到 1 小时"
                    @click="prepareLogQuery(textFilter, levels, 60)"
                  />
                  <UButton
                    color="neutral"
                    variant="ghost"
                    label="清除日志等级"
                    @click="prepareLogQuery(textFilter, [])"
                  />
                  <UButton
                    color="neutral"
                    variant="ghost"
                    label="清除关键词"
                    @click="prepareLogQuery('', levels)"
                  />
                </div>
              </div>
            </section>

            <div
              v-if="retainedEvidence.length || consultation"
              class="logs-context-status"
            >
              <UIcon
                name="i-lucide-archive"
                aria-hidden="true"
              />
              <div>
                <span>{{ retainedEvidence.length }} 条 Evidence · {{ consultation ? "Snapshot 已创建" : "可从更多操作创建 Snapshot" }}</span>
                <ul
                  v-if="retainedEvidence.length"
                  class="logs-evidence-list"
                  aria-label="日志 Evidence identities"
                >
                  <li
                    v-for="evidence in retainedEvidence"
                    :key="evidence.id"
                  >
                    <code>{{ evidence.id }}</code>
                  </li>
                </ul>
              </div>
            </div>
            <dl
              v-if="consultation"
              class="logs-snapshot__proof"
              data-testid="context-snapshot"
            >
              <div><dt>Consultation</dt><dd>{{ consultation.id }}</dd></div>
              <div><dt>Snapshot</dt><dd>{{ consultation.context_snapshot.id }}</dd></div>
              <div><dt>Content hash</dt><dd>{{ consultation.context_snapshot.content_hash }}</dd></div>
            </dl>
          </section>
        </main>
      </div>
    </template>
  </WorkspacePageFrame>

  <LogInspector
    :open="Boolean(inspectedEntryID)"
    :entry="inspectedEntry"
    :context-entries="inspectedEntryContext"
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
  width: 100%;
  max-width: 1600px;
  margin-inline: auto;
  padding: var(--co-space-5) clamp(var(--co-space-4), 2.5vw, var(--co-space-8)) var(--co-space-10);
  container-name: logs-workspace;
  container-type: inline-size;
}
.logs-workspace code { min-width: 0; overflow-wrap: anywhere; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 11px; }
.logs-workspace__grid { min-width: 0; }
.logs-analysis { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr); align-items: start; gap: var(--co-space-4); }
.logs-query-rail { position: static; display: grid; min-width: 0; gap: var(--co-space-3); }
.logs-stream-stage { display: grid; min-width: 0; align-content: start; gap: var(--co-space-3); padding: 0 var(--co-space-4) var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-bg-surface); box-shadow: var(--co-shadow-row); }
.logs-analysis__header { display: grid; min-width: 0; min-height: 76px; grid-template-columns: minmax(220px, 1fr) auto; align-items: center; gap: var(--co-space-4); padding: var(--co-space-3) 0; border-bottom: 1px solid var(--co-border-subtle); }
.logs-analysis__identity { display: grid; min-width: 0; gap: 3px; }
.logs-analysis__header h2 { margin: 0; font-size: 18px; }
.logs-analysis__header p { margin: 0; color: var(--co-text-secondary); font-size: 12px; }
.logs-analysis__actions { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); }
.logs-context-status > div { display: grid; min-width: 0; gap: var(--co-space-1); }
.logs-evidence-list { display: grid; min-width: 0; margin: 0; padding: 0; gap: 2px; list-style: none; }
.logs-display-menu { display: grid; min-width: 260px; gap: var(--co-space-2); padding: var(--co-space-3); }
.logs-display-menu span { max-width: 34ch; color: var(--co-text-muted); font-size: 12px; }
.logs-more-menu { display: grid; min-width: 250px; gap: 2px; padding: var(--co-space-2); }
.logs-more-menu :deep(button),
.logs-more-menu :deep(a) { width: 100%; justify-content: flex-start; }
.logs-result-metrics { display: grid; min-width: 0; grid-template-columns: repeat(4, minmax(90px, .48fr)) minmax(240px, 1fr); margin: 0; padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-subtle); }
.logs-result-metrics div { min-width: 0; padding: var(--co-space-1) var(--co-space-4); border-left: 1px solid var(--co-border-subtle); }
.logs-result-metrics div:first-child { padding-left: 0; border-left: 0; }
.logs-result-metrics dt { color: var(--co-text-muted); font-size: 12px; }
.logs-result-metrics dd { margin: 3px 0 0; color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 18px; font-weight: 750; font-variant-numeric: tabular-nums; }
.logs-result-metrics dd.is-error { color: var(--co-status-critical-fg); }
.logs-result-metrics div:last-child dd { overflow: hidden; font-size: 12px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.logs-distribution { display: grid; min-height: 76px; grid-template-columns: minmax(170px, auto) minmax(0, 1fr); overflow: hidden; border-radius: var(--co-radius-panel); background: var(--co-bg-canvas); }
.logs-distribution > header { display: flex; min-width: 0; align-items: flex-start; justify-content: center; flex-direction: column; gap: 2px; padding: var(--co-space-2) var(--co-space-3); border-right: 1px solid var(--co-border-subtle); }
.logs-distribution h3 { margin: 0; font-size: 14px; }
.logs-distribution header > span { max-width: 24ch; color: var(--co-text-muted); font-size: 12px; }
.logs-distribution__buckets { display: flex; height: 72px; align-items: end; gap: 3px; overflow: hidden; padding: var(--co-space-2) var(--co-space-3); }
.logs-distribution__bucket { position: relative; width: 100%; min-width: 7px; height: 100%; padding: 0; }
.logs-distribution__bucket span { position: absolute; right: 1px; bottom: 0; left: 1px; min-height: 4px; border-radius: var(--co-radius-control) var(--co-radius-control) 0 0; background: var(--co-status-success-fg); }
.logs-analysis__meta { display: flex; width: fit-content; max-width: 100%; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-2) var(--co-space-4); margin-bottom: var(--co-space-1); padding: 0 0 var(--co-space-2); border-bottom: 1px solid var(--co-border-subtle); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 11px; }
.logs-stream-empty { display: grid; min-height: 280px; place-content: center; justify-items: center; gap: var(--co-space-5); padding: var(--co-space-6) var(--co-space-5); text-align: center; }
.logs-stream-empty--no-results { min-height: 260px; }
.logs-stream-empty__copy { display: flex; min-width: 0; align-items: center; gap: var(--co-space-4); text-align: left; }
.logs-stream-empty__copy > span { display: grid; width: 48px; height: 48px; flex: 0 0 auto; place-items: center; border-radius: var(--co-radius-control); color: var(--co-text-muted); background: var(--co-bg-canvas); }
.logs-stream-empty__copy > span :deep(svg) { display: block; width: 22px; height: 22px; }
.logs-stream-empty h3 { margin: 0; font-size: 18px; }
.logs-stream-empty p { max-width: 52ch; margin: var(--co-space-2) 0 0; color: var(--co-text-secondary); font-size: 12px; line-height: 1.65; }
.logs-stream-empty__suggestions { display: grid; justify-items: center; gap: var(--co-space-2); }
.logs-stream-empty__suggestions > span { color: var(--co-text-muted); font-size: 12px; font-weight: 650; }
.logs-stream-empty__suggestions > div { display: flex; flex-wrap: wrap; justify-content: center; gap: var(--co-space-2); }
.logs-history-popover { box-sizing: border-box; width: min(420px, calc(100vw - 32px)); max-height: min(440px, calc(100dvh - 32px)); padding: var(--co-space-3); overflow: hidden; }
.logs-context-status { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); padding-top: var(--co-space-2); border-top: 1px solid var(--co-border-subtle); color: var(--co-text-secondary); font-size: 12px; }
.logs-context-status :deep(svg) { flex: 0 0 auto; color: var(--co-text-muted); }
.logs-snapshot__proof { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: 0; overflow: hidden; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); }
.logs-snapshot__proof div { min-width: 0; padding: var(--co-space-2); border-right: 1px solid var(--co-border-default); }
.logs-snapshot__proof div:last-child { border-right: 0; }
.logs-snapshot__proof dt { color: var(--co-text-muted); font-size: 10px; }
.logs-snapshot__proof dd { margin: var(--co-space-1) 0 0; overflow-wrap: anywhere; font-family: var(--co-font-mono); font-size: 10px; }

@media (max-width: 1024px) {
  .logs-workspace { padding-inline: var(--co-space-4); }
  .logs-analysis__header { grid-template-columns: minmax(0, 1fr); align-items: flex-start; padding-block: var(--co-space-3); }
  .logs-analysis__actions { justify-content: flex-start; }
  .logs-result-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .logs-result-metrics div:nth-child(odd) { padding-left: 0; border-left: 0; }
  .logs-result-metrics div:last-child { grid-column: 1 / -1; padding-top: var(--co-space-3); border-top: 1px solid var(--co-border-subtle); }
  .logs-distribution { grid-template-columns: minmax(0, 1fr); }
  .logs-distribution > header { border-right: 0; border-bottom: 1px solid var(--co-border-subtle); }
}

@container logs-workspace (max-width: 900px) {
  .logs-analysis__header { grid-template-columns: minmax(0, 1fr); align-items: flex-start; }
  .logs-analysis__actions { justify-content: flex-start; }
  .logs-distribution { grid-template-columns: minmax(0, 1fr); }
  .logs-distribution > header { border-right: 0; border-bottom: 1px solid var(--co-border-subtle); }
}

@container logs-workspace (max-width: 620px) {
  .logs-stream-stage { padding-inline: var(--co-space-3); }
  .logs-result-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .logs-stream-empty { min-height: 260px; padding-inline: var(--co-space-3); }
  .logs-stream-empty__copy { align-items: flex-start; }
  .logs-snapshot__proof { grid-template-columns: minmax(0, 1fr); }
  .logs-snapshot__proof div { border-right: 0; border-bottom: 1px solid var(--co-border-default); }
  .logs-snapshot__proof div:last-child { border-bottom: 0; }
}

@media (prefers-reduced-motion: reduce) {
  .logs-workspace * { scroll-behavior: auto; }
}

</style>
