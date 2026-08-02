<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { isApiError } from "../../api/client";
import { getResources, type KubernetesResource } from "../../api/infrastructure";
import { getBootstrap, type BootstrapSnapshot } from "../../api/platform";
import {
  createTelemetryConsultation,
  getTraceDetail,
  getTraceSearch,
  getTraceSearches,
  getTracesCatalog,
  saveTraceEvidence,
  startTraceSearch,
  type Consultation,
  type TelemetryCatalog,
  type TelemetryEvidence,
  type TelemetryQueryMode,
  type TraceDetail,
  type TraceSearch,
  type TraceSpan,
  type TraceSummary,
} from "../../api/telemetry";
import TraceDetailWorkspace from "../../components/traces/TraceDetailWorkspace.vue";
import TraceSearchResults from "../../components/traces/TraceSearchResults.vue";
import TracesHistory from "../../components/traces/TracesHistory.vue";
import TracesQueryControls from "../../components/traces/TracesQueryControls.vue";
import {
  buildTracesRouteQuery,
  parseTracesRoute,
  type TracesRouteState,
} from "../../components/traces/tracesRoute";
import WorkspaceHeader from "../../components/workspace/WorkspaceHeader.vue";
import WorkspacePageFrame from "../../components/workspace/WorkspacePageFrame.vue";
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

type ResultsHandle = InstanceType<typeof TraceSearchResults>;

const route = useRoute();
const router = useRouter();
const initialRoute = parseTracesRoute(route.query as Record<string, unknown>);
const bootstrap = ref<BootstrapSnapshot | null>(null);
const workloads = ref<KubernetesResource[]>([]);
const catalog = ref<TelemetryCatalog | null>(null);
const historyItems = ref<TraceSearch[]>([]);
const currentSearch = ref<TraceSearch | null>(null);
const detail = ref<TraceDetail | null>(null);
const selectedResourceID = ref(initialRoute.resource);
const selectedNamespace = ref(initialRoute.namespace);
const mode = ref<TelemetryQueryMode>(initialRoute.mode);
const serviceFilter = ref(initialRoute.service);
const operationFilter = ref(initialRoute.operation);
const statusFilter = ref(initialRoute.status);
const minDuration = ref<number | undefined>(initialRoute.minDurationMS);
const maxDuration = ref<number | undefined>(initialRoute.maxDurationMS);
const expertQuery = ref("{}");
const fromValue = ref(routeTime(initialRoute.from) || toLocalInput(new Date(Date.now() - 15 * 60_000)));
const toValue = ref(routeTime(initialRoute.to) || toLocalInput(new Date()));
const limit = ref(initialRoute.limit);
const selectedSpanIDs = ref(new Set<string>());
const inspectedSpan = ref<TraceSpan | null>(null);
const retainedEvidence = ref<TelemetryEvidence[]>([]);
const consultation = ref<Consultation | null>(null);
const resultsList = ref<ResultsHandle | null>(null);
const loading = ref(true);
const searching = ref(false);
const detailLoading = ref(false);
const savingEvidence = ref(false);
const freezing = ref(false);
const pageError = ref<RequestFailure | null>(null);
const queryError = ref<RequestFailure | null>(null);
const statusMessage = ref("");
let mounted = true;
let routeReady = false;
let routeMutationDepth = 0;
let workspaceGeneration = 0;
let searchGeneration = 0;
let detailGeneration = 0;
let listScrollTop = 0;
let workspaceController: AbortController | undefined;
let searchController: AbortController | undefined;
let detailController: AbortController | undefined;

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
const canSearch = computed(() => Boolean(
  selectedResource.value
  && providerReady.value
  && validTimeRange.value
  && !searching.value
  && (mode.value === "guided" || expertQuery.value.trim()),
));
const canFreeze = computed(() => Boolean(
  detail.value?.query_id
  || (currentSearch.value?.status === "succeeded" && !currentSearch.value.result_expired),
));
const traceOverview = computed(() => {
  const traces = currentSearch.value?.traces ?? [];
  const slowest = traces.reduce<TraceSummary | null>((selected, trace) => (
    !selected || trace.duration_ms > selected.duration_ms ? trace : selected
  ), null);
  return {
    total: traces.length,
    services: new Set(traces.map((trace) => trace.root_service).filter(Boolean)).size,
    errorTraces: traces.filter((trace) => trace.error_span_count > 0).length,
    spans: traces.reduce((total, trace) => total + trace.span_count, 0),
    slowest,
  };
});
const detailRouteActive = computed(() => Boolean(
  parseTracesRoute(route.query as Record<string, unknown>).traceID,
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
const tempoURL = computed(() => {
  const links = detail.value?.links ?? currentSearch.value?.links ?? [];
  const link = links.find((item) => (
    item.provider === "tempo"
    && item.target === "external"
    && item.availability === "available"
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

function formatDuration(value: number): string {
  if (!Number.isFinite(value)) return "无";
  if (value < 1) return `${value.toFixed(3)} ms`;
  if (value < 1000) return `${value.toFixed(value < 10 ? 2 : 1)} ms`;
  return `${(value / 1000).toFixed(2)} s`;
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
    resource: {
      id: resource.id,
      kind: resource.kind,
      namespace: resource.namespace,
      name: resource.name,
    },
  };
}

function currentAgentContext(): AgentPageContext | null {
  const current = detail.value ?? currentSearch.value;
  const queryID = detail.value?.query_id || currentSearch.value?.id;
  if (!current || !queryID || currentSearch.value?.result_expired) return null;
  return {
    route: route.fullPath,
    input: {
      title: `${current.resource.name} Trace 上下文`,
      cluster_id: current.scope.cluster_id,
      environment: current.scope.environment,
      namespaces: [...current.scope.namespaces],
      resource_refs: [current.resource],
      filters: {
        workspace: "traces",
        provider: "tempo",
        mode: mode.value,
        trace_id: detail.value?.trace_id,
        query_hash: currentSearch.value?.query_hash,
        service: serviceFilter.value,
        operation: operationFilter.value,
        status: statusFilter.value,
      },
      from: current.time_range.from,
      to: current.time_range.to,
      query_definition_refs: [],
      query_execution_refs: [queryID],
      evidence_refs: retainedEvidence.value.map((item) => item.id),
    },
  };
}

function publishCurrentAgentContext() {
  publishAgentContext(currentAgentContext());
}

function routeState(searchID = currentSearch.value?.id ?? "", traceID = detail.value?.trace_id ?? "") {
  return {
    cluster: bootstrap.value?.active_scope.cluster_id ?? "",
    namespace: selectedNamespace.value,
    resource: selectedResourceID.value,
    mode: mode.value,
    service: serviceFilter.value,
    operation: operationFilter.value,
    status: statusFilter.value,
    minDurationMS: minDuration.value,
    maxDurationMS: maxDuration.value,
    limit: limit.value,
    searchID,
    traceID,
    from: validTimeRange.value ? new Date(fromValue.value).toISOString() : "",
    to: validTimeRange.value ? new Date(toValue.value).toISOString() : "",
  };
}

async function syncRoute(
  searchID = currentSearch.value?.id ?? "",
  traceID = detail.value?.trace_id ?? "",
  navigation: "replace" | "push" = "replace",
) {
  routeMutationDepth += 1;
  try {
    const location = { path: "/traces", query: buildTracesRouteQuery(routeState(searchID, traceID)) };
    if (navigation === "push") await router.push(location);
    else await router.replace(location);
  } finally {
    await nextTick();
    routeMutationDepth -= 1;
  }
}

function applyRouteFilters(parsed: TracesRouteState) {
  mode.value = parsed.mode;
  serviceFilter.value = parsed.service;
  operationFilter.value = parsed.operation;
  statusFilter.value = parsed.status;
  minDuration.value = parsed.minDurationMS;
  maxDuration.value = parsed.maxDurationMS;
  limit.value = parsed.limit;
  const from = routeTime(parsed.from);
  const to = routeTime(parsed.to);
  if (from && to) [fromValue.value, toValue.value] = [from, to];
}

async function loadCatalogAndHistory(signal?: AbortSignal) {
  const context = telemetryContext();
  if (!context) {
    catalog.value = null;
    historyItems.value = [];
    return;
  }
  const [nextCatalog, nextHistory] = await Promise.all([
    getTracesCatalog(context, signal),
    getTraceSearches({
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

function resetDetail() {
  detailController?.abort();
  detailGeneration += 1;
  detailLoading.value = false;
  detail.value = null;
  inspectedSpan.value = null;
  selectedSpanIDs.value = new Set();
  retainedEvidence.value = [];
  consultation.value = null;
}

function applySearch(result: TraceSearch) {
  currentSearch.value = result;
  mode.value = result.mode;
  expertQuery.value = result.query;
  fromValue.value = toLocalInput(new Date(result.time_range.from));
  toValue.value = toLocalInput(new Date(result.time_range.to));
  resetDetail();
}

function applyDetail(result: TraceDetail) {
  detail.value = result;
  selectedSpanIDs.value = new Set();
  inspectedSpan.value = result.spans[0] ?? null;
  retainedEvidence.value = [];
  consultation.value = null;
}

function traceDetailOptions(searchID?: string) {
  const context = telemetryContext();
  if (searchID) return { search_id: searchID };
  if (!context) return null;
  return {
    context,
    from: new Date(fromValue.value).toISOString(),
    to: new Date(toValue.value).toISOString(),
  };
}

async function loadWorkspace() {
  workspaceController?.abort();
  searchController?.abort();
  detailController?.abort();
  workspaceController = new AbortController();
  const signal = workspaceController.signal;
  const generation = ++workspaceGeneration;
  searchGeneration += 1;
  detailGeneration += 1;
  loading.value = true;
  searching.value = false;
  detailLoading.value = false;
  pageError.value = null;
  queryError.value = null;
  currentSearch.value = null;
  detail.value = null;
  selectedSpanIDs.value = new Set();
  inspectedSpan.value = null;
  retainedEvidence.value = [];
  consultation.value = null;
  try {
    const snapshot = await getBootstrap(signal);
    if (!mounted || signal.aborted || generation !== workspaceGeneration) return;
    bootstrap.value = snapshot;
    const parsed = parseTracesRoute(route.query as Record<string, unknown>);
    selectedNamespace.value = parsed.namespace || snapshot.active_scope.namespaces[0] || "";
    const page = await getResources({
      cluster: snapshot.active_scope.cluster_id,
      kind: ["Deployment", "StatefulSet", "DaemonSet"],
      limit: 500,
    }, signal);
    if (!mounted || signal.aborted || generation !== workspaceGeneration) return;
    workloads.value = page.items.filter((item) => item.layer === "workload");
    const resolved = resolveTelemetryResourceID(
      workloads.value,
      parsed.resource,
      parsed.legacyWorkload,
      selectedNamespace.value,
    );
    selectedResourceID.value = resolved
      || workloads.value.find((item) => item.namespace === selectedNamespace.value)?.id
      || workloads.value[0]?.id
      || "";
    if (selectedResource.value?.namespace) selectedNamespace.value = selectedResource.value.namespace;
    applyRouteFilters(parsed);
    await loadCatalogAndHistory(signal);
    if (!mounted || signal.aborted || generation !== workspaceGeneration) return;

    if (parsed.searchID) {
      try {
        const result = await getTraceSearch(parsed.searchID, signal);
        if (!mounted || signal.aborted || generation !== workspaceGeneration) return;
        applySearch(result);
      } catch (reason) {
        if (!signal.aborted) queryError.value = normalizeFailure(reason, "Trace 搜索历史读取失败。");
      }
    }
    if (parsed.traceID) {
      const options = traceDetailOptions(parsed.searchID || undefined);
      if (options) {
        detailLoading.value = true;
        try {
          const result = await getTraceDetail(parsed.traceID, options, signal);
          if (!mounted || signal.aborted || generation !== workspaceGeneration) return;
          applyDetail(result);
        } catch (reason) {
          if (!signal.aborted) queryError.value = normalizeFailure(reason, "Trace detail 读取失败。");
        } finally {
          if (!signal.aborted && generation === workspaceGeneration) detailLoading.value = false;
        }
      }
    }
    if (parsed.legacyWorkload || parsed.resource !== selectedResourceID.value) {
      await syncRoute(parsed.searchID, parsed.traceID);
    }
  } catch (reason) {
    if (!signal.aborted && mounted && generation === workspaceGeneration) {
      pageError.value = normalizeFailure(reason, "Traces Workspace 读取失败。");
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
  currentSearch.value = null;
  void syncRoute("", "");
}

function selectPreset(minutes: number) {
  const to = new Date();
  fromValue.value = toLocalInput(new Date(to.getTime() - minutes * 60_000));
  toValue.value = toLocalInput(to);
  currentSearch.value = null;
  void syncRoute("", "");
}

function prepareTraceShortcut(kind: "errors" | "slow" | "recent" | "wider" | "clear-operation" | "clear-duration") {
  if (kind === "errors") {
    mode.value = "guided";
    statusFilter.value = "error";
    minDuration.value = undefined;
    maxDuration.value = undefined;
  } else if (kind === "slow") {
    mode.value = "guided";
    statusFilter.value = "";
    minDuration.value = 1000;
    maxDuration.value = undefined;
  } else if (kind === "clear-operation") {
    operationFilter.value = "";
  } else if (kind === "clear-duration") {
    minDuration.value = undefined;
    maxDuration.value = undefined;
  }
  if (kind === "errors" || kind === "recent") {
    const to = new Date();
    fromValue.value = toLocalInput(new Date(to.getTime() - 15 * 60_000));
    toValue.value = toLocalInput(to);
  } else if (kind === "slow" || kind === "wider") {
    const to = new Date();
    fromValue.value = toLocalInput(new Date(to.getTime() - 60 * 60_000));
    toValue.value = toLocalInput(to);
  }
  currentSearch.value = null;
  void syncRoute("", "");
}

async function searchTraces() {
  const context = telemetryContext();
  if (!context || !canSearch.value) return;
  searchController?.abort();
  searchController = new AbortController();
  const signal = searchController.signal;
  const generation = ++searchGeneration;
  searching.value = true;
  queryError.value = null;
  statusMessage.value = "";
  try {
    const result = await startTraceSearch({
      ...context,
      mode: mode.value,
      query: mode.value === "expert" ? expertQuery.value : undefined,
      filter: mode.value === "guided" ? {
        service: serviceFilter.value.trim() || undefined,
        operation: operationFilter.value.trim() || undefined,
        status: statusFilter.value || undefined,
        min_duration_ms: minDuration.value,
        max_duration_ms: maxDuration.value,
      } : {},
      from: new Date(fromValue.value).toISOString(),
      to: new Date(toValue.value).toISOString(),
      limit: limit.value,
    }, signal);
    if (!mounted || signal.aborted || generation !== searchGeneration) return;
    applySearch(result);
    historyItems.value = [result, ...historyItems.value.filter((item) => item.id !== result.id)].slice(0, 30);
    listScrollTop = 0;
    await syncRoute(result.id, "");
  } catch (reason) {
    if (!signal.aborted && mounted && generation === searchGeneration) {
      queryError.value = normalizeFailure(reason, "Tempo Trace 搜索失败。");
    }
  } finally {
    if (generation === searchGeneration) searching.value = false;
  }
}

function cancelSearchRequest() {
  if (!searching.value) return;
  searchController?.abort();
  searchGeneration += 1;
  searching.value = false;
  statusMessage.value = "已停止等待当前请求；若服务端已经接受搜索，可稍后从历史重新读取。";
}

async function openHistory(id: string, updateRoute = true) {
  searchController?.abort();
  searchController = new AbortController();
  const signal = searchController.signal;
  const generation = ++searchGeneration;
  searching.value = true;
  queryError.value = null;
  try {
    const result = await getTraceSearch(id, signal);
    if (!mounted || signal.aborted || generation !== searchGeneration) return;
    applySearch(result);
    listScrollTop = 0;
    if (updateRoute) await syncRoute(result.id, "");
  } catch (reason) {
    if (!signal.aborted && generation === searchGeneration) {
      queryError.value = normalizeFailure(reason, "Trace 搜索历史读取失败。");
    }
  } finally {
    if (generation === searchGeneration) searching.value = false;
  }
}

async function openTrace(trace: TraceSummary, scrollTop: number) {
  listScrollTop = scrollTop;
  await openTraceID(trace.trace_id, currentSearch.value?.id, true);
}

async function openTraceID(traceID: string, searchID?: string, updateRoute = false) {
  const options = traceDetailOptions(searchID);
  if (!options) return;
  detailController?.abort();
  detailController = new AbortController();
  const signal = detailController.signal;
  const generation = ++detailGeneration;
  detailLoading.value = true;
  queryError.value = null;
  detail.value = null;
  inspectedSpan.value = null;
  selectedSpanIDs.value = new Set();
  retainedEvidence.value = [];
  consultation.value = null;
  if (updateRoute) await syncRoute(searchID ?? "", traceID, "push");
  try {
    const result = await getTraceDetail(traceID, options, signal);
    if (!mounted || signal.aborted || generation !== detailGeneration) return;
    applyDetail(result);
  } catch (reason) {
    if (!signal.aborted && mounted && generation === detailGeneration) {
      queryError.value = normalizeFailure(reason, "Trace detail 读取失败。");
    }
  } finally {
    if (generation === detailGeneration) detailLoading.value = false;
  }
}

async function restoreResultsScroll() {
  await nextTick();
  await resultsList.value?.restoreScroll(listScrollTop);
}

function returnFromDetail() {
  const back = window.history.state?.back;
  if (typeof back === "string" && back) {
    router.back();
    return;
  }
  resetDetail();
  void syncRoute(currentSearch.value?.id ?? "", "").then(restoreResultsScroll);
}

function toggleSpan(spanID: string) {
  const next = new Set(selectedSpanIDs.value);
  if (next.has(spanID)) next.delete(spanID);
  else if (next.size < 32) next.add(spanID);
  selectedSpanIDs.value = next;
}

function inspectSpan(span: TraceSpan) {
  inspectedSpan.value = span;
}

async function retainSelectedEvidence() {
  const current = detail.value;
  if (!current || selectedSpanIDs.value.size === 0 || savingEvidence.value) return;
  savingEvidence.value = true;
  queryError.value = null;
  try {
    const evidence = await saveTraceEvidence(current.query_id, current.trace_id, [...selectedSpanIDs.value]);
    retainedEvidence.value = [evidence, ...retainedEvidence.value];
    statusMessage.value = `已保留 ${evidence.item_count} 个 Span Evidence。`;
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Trace Evidence 保存失败。");
  } finally {
    savingEvidence.value = false;
  }
}

function openCurrentInAgent() {
  const context = currentAgentContext();
  if (!context) {
    statusMessage.value = "请先打开一个仍保留结果的 Trace，再关联或新建 Agent 调查。";
    return;
  }
  openAgentPanel({ context });
}

async function freezeContext() {
  const resource = telemetryContext()?.resource;
  const scope = bootstrap.value?.active_scope;
  const queryID = detail.value?.query_id || currentSearch.value?.id;
  const range = detail.value?.time_range || currentSearch.value?.time_range;
  if (!resource || !scope || !queryID || !range || !canFreeze.value || freezing.value) return;
  freezing.value = true;
  queryError.value = null;
  try {
    const context = currentAgentContext();
    consultation.value = await createTelemetryConsultation({
      title: `${resource.name} Trace 上下文`,
      cluster_id: scope.cluster_id,
      environment: scope.environment,
      namespaces: [resource.namespace],
      resource_refs: [resource],
      filters: context?.input.filters,
      from: range.from,
      to: range.to,
      query_definition_refs: context?.input.query_definition_refs,
      query_execution_refs: [queryID],
      evidence_refs: retainedEvidence.value.map((item) => item.id),
    });
    statusMessage.value = "当前 Trace 上下文已冻结为不可变 Context Snapshot。";
    if (context) openAgentPanel({ consultationId: consultation.value.id, context });
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Context Snapshot 创建失败。");
  } finally {
    freezing.value = false;
  }
}

async function applyRouteNavigation() {
  const parsed = parseTracesRoute(route.query as Record<string, unknown>);
  const resolved = resolveTelemetryResourceID(
    workloads.value,
    parsed.resource,
    parsed.legacyWorkload,
    parsed.namespace,
  );
  const activeCluster = bootstrap.value?.active_scope.cluster_id ?? "";
  const contextChanged = (
    (parsed.cluster && parsed.cluster !== activeCluster)
    || parsed.namespace !== selectedNamespace.value
    || (resolved && resolved !== selectedResourceID.value)
    || Boolean(parsed.legacyWorkload)
  );
  if (contextChanged) {
    await loadWorkspace();
    return;
  }
  applyRouteFilters(parsed);
  if (parsed.searchID && parsed.searchID !== currentSearch.value?.id) {
    await openHistory(parsed.searchID, false);
  } else if (!parsed.searchID && currentSearch.value) {
    currentSearch.value = null;
  }
  if (parsed.traceID) {
    if (parsed.traceID !== detail.value?.trace_id) {
      await openTraceID(parsed.traceID, parsed.searchID || currentSearch.value?.id, false);
    }
  } else {
    resetDetail();
    await restoreResultsScroll();
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
  publishCurrentAgentContext();
  if (routeReady && routeMutationDepth === 0) void applyRouteNavigation();
});
watch([currentSearch, detail, retainedEvidence], publishCurrentAgentContext, { flush: "post" });

onBeforeUnmount(() => {
  mounted = false;
  workspaceGeneration += 1;
  searchGeneration += 1;
  detailGeneration += 1;
  workspaceController?.abort();
  searchController?.abort();
  detailController?.abort();
  window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, receiveScopeChange);
  publishAgentContext(null);
});
</script>

<template>
  <WorkspacePageFrame
    class="traces-workspace"
    width="full"
    aria-labelledby="traces-heading"
  >
    <WorkspaceHeader
      title="链路分析"
      :description="`${selectedResource ? `${selectedResource.kind} ${selectedResource.name} · ${selectedResource.namespace}` : '当前运行范围'} · 沿关键路径定位慢 Span 与错误服务`"
    >
      <template #actions>
        <UTooltip text="刷新链路工作区">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-refresh-cw"
            square
            aria-label="刷新链路工作区"
            :loading="loading"
            @click="refreshAll"
          />
        </UTooltip>
      </template>
    </WorkspaceHeader>

    <WorkspaceState
      v-if="pageError"
      :kind="pageError.status === 401 || pageError.status === 403 ? 'permission-denied' : 'error'"
      :title="pageError.code"
      :description="pageError.message"
      :request-i-d="pageError.requestID"
      :trace-i-d="pageError.traceID"
      :next-steps="pageError.nextSteps"
    >
      <template #actions>
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-refresh-cw"
          label="重试"
          @click="refreshAll"
        />
      </template>
    </WorkspaceState>
    <WorkspaceState
      v-if="queryError"
      :kind="queryError.status === 401 || queryError.status === 403 ? 'permission-denied' : 'error'"
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
      description="加载真实 Workload、Tempo 查询目录与历史身份。"
    />

    <template v-else-if="detailRouteActive">
      <WorkspaceState
        v-if="detailLoading"
        kind="loading"
        title="正在读取 Tempo Trace detail"
        description="保留 Trace、Search 与 Configuration Revision 身份。"
      />
      <TraceDetailWorkspace
        v-else-if="detail"
        :detail="detail"
        :selected-i-ds="selectedSpanIDs"
        :inspected-span="inspectedSpan"
        :retained-evidence-count="retainedEvidence.length"
        :consultation="consultation"
        :saving-evidence="savingEvidence"
        :freezing="freezing"
        :can-freeze="canFreeze"
        :search-stale="Boolean(currentSearch?.stale)"
        :workload-location="workloadLocation"
        @back="returnFromDetail"
        @toggle="toggleSpan"
        @inspect="inspectSpan"
        @save-evidence="retainSelectedEvidence"
        @open-agent="openCurrentInAgent"
        @freeze="freezeContext"
      />
      <WorkspaceState
        v-else
        kind="empty"
        title="Trace detail 当前不可用"
        description="可返回搜索结果或刷新当前深链。"
      >
        <template #actions>
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-arrow-left"
            label="返回"
            @click="returnFromDetail"
          />
          <UButton
            color="primary"
            icon="i-lucide-refresh-cw"
            label="重试"
            @click="refreshAll"
          />
        </template>
      </WorkspaceState>
    </template>

    <template v-else>
      <section
        class="trace-discovery-stage"
        aria-labelledby="trace-results-heading"
      >
        <div class="trace-workbench">
          <aside
            class="trace-query-rail"
            aria-label="Trace 查询面板"
          >
            <TracesQueryControls
              :namespaces="namespaces"
              :resources="namespaceWorkloads"
              :catalog="catalog"
              :namespace="selectedNamespace"
              :resource-i-d="selectedResourceID"
              :mode="mode"
              :service="serviceFilter"
              :operation="operationFilter"
              :status="statusFilter"
              :min-duration-m-s="minDuration"
              :max-duration-m-s="maxDuration"
              :expert-query="expertQuery"
              :from="fromValue"
              :to="toValue"
              :limit="limit"
              :valid-time-range="validTimeRange"
              :can-search="canSearch"
              :searching="searching"
              @update:namespace="selectedNamespace = $event"
              @update:resource-i-d="selectedResourceID = $event"
              @update:mode="changeMode"
              @update:service="serviceFilter = $event"
              @update:operation="operationFilter = $event"
              @update:status="statusFilter = $event"
              @update:min-duration-m-s="minDuration = $event"
              @update:max-duration-m-s="maxDuration = $event"
              @update:expert-query="expertQuery = $event"
              @update:from="fromValue = $event"
              @update:to="toValue = $event"
              @update:limit="limit = $event"
              @namespace-change="changeNamespace"
              @resource-change="changeResource"
              @preset="selectPreset"
              @search="searchTraces"
              @cancel="cancelSearchRequest"
            />

            <UAlert
              v-if="catalog && !providerReady"
              color="error"
              variant="soft"
              icon="i-lucide-ban"
              :title="`Tempo ${providerStateLabel(catalog.provider_state)}`"
              :description="`${catalog.provider_detail} · ${catalog.source.identity || '当前 Configuration Revision 没有可用采集端点'}`"
            />
          </aside>

          <section
            class="trace-results-panel"
            aria-labelledby="trace-results-heading"
          >
            <header class="trace-results-header">
              <div>
                <h2 id="trace-results-heading">
                  Trace 结果
                </h2>
                <p v-if="currentSearch">
                  {{ currentSearch.result_count }} 条 Trace · {{ formatElapsed(currentSearch.created_at, currentSearch.completed_at) }} · {{ formatBytes(currentSearch.response_bytes) }}
                </p>
                <p v-else>
                  运行查询后在此比较耗时、Span 数量、错误服务并进入瀑布图。
                </p>
              </div>
              <div class="trace-results-actions">
                <UTooltip text="打开 Trace 并选择 Span 后保存">
                  <UButton
                    color="neutral"
                    variant="soft"
                    icon="i-lucide-save"
                    label="保存 Evidence"
                    disabled
                  />
                </UTooltip>
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
                    <div class="trace-history-popover">
                      <TracesHistory
                        :items="historyItems"
                        :active-i-d="currentSearch?.id ?? ''"
                        @select="openHistory"
                      />
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
                    <div class="trace-more-menu">
                      <UButton
                        color="neutral"
                        variant="ghost"
                        icon="i-lucide-archive"
                        label="创建 Snapshot"
                        :disabled="!canFreeze || freezing"
                        :loading="freezing"
                        @click="freezeContext"
                      />
                      <UButton
                        color="neutral"
                        variant="ghost"
                        icon="i-lucide-box"
                        label="打开关联 Workload"
                        :to="workloadLocation"
                      />
                      <UButton
                        v-if="tempoURL"
                        color="neutral"
                        variant="ghost"
                        icon="i-lucide-external-link"
                        label="在 Tempo 中打开"
                        :to="tempoURL"
                        target="_blank"
                        rel="noopener noreferrer"
                        external
                      />
                      <UButton
                        color="neutral"
                        variant="ghost"
                        icon="i-lucide-chart-no-axes-combined"
                        label="查看关联指标"
                        :to="{ path: '/monitoring', query: workloadLocation.query }"
                      />
                      <UButton
                        color="neutral"
                        variant="ghost"
                        icon="i-lucide-logs"
                        label="查看关联日志"
                        :to="{ path: '/logs', query: workloadLocation.query }"
                      />
                    </div>
                  </template>
                </UPopover>
              </div>
            </header>

            <div
              v-if="currentSearch"
              class="trace-results-overview"
            >
              <dl
                class="trace-results-metrics"
                aria-label="Trace 结果摘要"
              >
                <div><dt>Trace</dt><dd>{{ traceOverview.total }}</dd></div>
                <div><dt>服务</dt><dd>{{ traceOverview.services }}</dd></div>
                <div>
                  <dt>错误链路</dt><dd :class="{ 'is-error': traceOverview.errorTraces > 0 }">
                    {{ traceOverview.errorTraces }}
                  </dd>
                </div>
                <div><dt>Span</dt><dd>{{ traceOverview.spans }}</dd></div>
              </dl>
              <section
                v-if="traceOverview.slowest"
                class="trace-results-slowest"
              >
                <div>
                  <span>最慢链路</span>
                  <strong>{{ traceOverview.slowest.root_service }} · {{ traceOverview.slowest.root_operation }}</strong>
                  <p>{{ formatDuration(traceOverview.slowest.duration_ms) }} · {{ traceOverview.slowest.span_count }} Span · {{ traceOverview.slowest.error_span_count }} 错误</p>
                </div>
                <UButton
                  color="neutral"
                  variant="soft"
                  trailing-icon="i-lucide-arrow-right"
                  label="分析"
                  @click="openTrace(traceOverview.slowest, 0)"
                />
              </section>
            </div>

            <div
              v-if="currentSearch"
              class="trace-results-meta"
            >
              <span><b>{{ currentSearch.mode === "expert" ? "TRACEQL" : "DISCOVERY" }}</b></span>
              <span>{{ currentSearch.time_range.from }} → {{ currentSearch.time_range.to }}</span>
              <UBadge
                v-if="currentSearch.truncated"
                color="warning"
                variant="soft"
                label="已截断"
              />
            </div>
            <WorkspaceState
              v-if="currentSearch?.partial"
              kind="partial"
              title="Tempo 仅返回部分 Trace summaries"
              description="可用结果继续显示；请收窄时间范围或过滤条件后重试。"
            />
            <WorkspaceState
              v-if="currentSearch?.stale"
              kind="stale"
              title="Trace 搜索结果已陈旧"
              description="当前内容仍可检查，但不声明它代表最新 Provider 状态。"
            />
            <WorkspaceState
              v-if="currentSearch?.result_expired"
              kind="expired"
              title="Provider Trace summaries 已过期"
              description="仅保留 Search Execution 审计元数据；请在查询面板重新执行。"
            />
            <WorkspaceState
              v-if="currentSearch?.status === 'failed' || currentSearch?.status === 'cancelled'"
              kind="error"
              :title="currentSearch.error_code || `Trace 搜索${currentSearch.status === 'cancelled' ? '已取消' : '失败'}`"
              :description="currentSearch.error_detail || '服务端保留了此次 Search Execution 身份。'"
            />
            <TraceSearchResults
              v-if="currentSearch?.traces.length"
              ref="resultsList"
              :traces="currentSearch.traces"
              active-trace-i-d=""
              @open="openTrace"
            />
            <section
              v-if="!currentSearch"
              class="trace-results-empty"
              aria-live="polite"
            >
              <div class="trace-results-empty__copy">
                <span><UIcon
                  name="i-lucide-scan-search"
                  aria-hidden="true"
                /></span>
                <div>
                  <h3>尚未发现 Trace</h3>
                  <p>选择服务和操作，或使用 TraceQL 查询链路。</p>
                </div>
              </div>
              <div class="trace-results-empty__suggestions">
                <span>常用条件</span>
                <div>
                  <UButton
                    color="neutral"
                    variant="soft"
                    icon="i-lucide-circle-alert"
                    label="错误 Trace"
                    @click="prepareTraceShortcut('errors')"
                  />
                  <UButton
                    color="neutral"
                    variant="soft"
                    icon="i-lucide-timer"
                    label="耗时 > 1s"
                    @click="prepareTraceShortcut('slow')"
                  />
                  <UButton
                    color="neutral"
                    variant="soft"
                    icon="i-lucide-clock-3"
                    label="最近 15 分钟"
                    @click="prepareTraceShortcut('recent')"
                  />
                </div>
              </div>
            </section>
            <section
              v-else-if="currentSearch?.status === 'succeeded' && !currentSearch.result_expired && currentSearch.traces.length === 0"
              class="trace-results-empty trace-results-empty--no-results"
              aria-live="polite"
            >
              <div class="trace-results-empty__copy">
                <span><UIcon
                  name="i-lucide-search-x"
                  aria-hidden="true"
                /></span>
                <div>
                  <h3>没有找到符合条件的 Trace</h3>
                  <p>当前范围：{{ currentSearch.resource.namespace }} / {{ currentSearch.resource.name }} / {{ formatTime(currentSearch.time_range.from) }} 至 {{ formatTime(currentSearch.time_range.to) }}</p>
                </div>
              </div>
              <div class="trace-results-empty__suggestions">
                <span>建议调整</span>
                <div>
                  <UButton
                    color="neutral"
                    variant="ghost"
                    label="扩大到 1 小时"
                    @click="prepareTraceShortcut('wider')"
                  />
                  <UButton
                    color="neutral"
                    variant="ghost"
                    label="清除 Operation"
                    @click="prepareTraceShortcut('clear-operation')"
                  />
                  <UButton
                    color="neutral"
                    variant="ghost"
                    label="清除耗时筛选"
                    @click="prepareTraceShortcut('clear-duration')"
                  />
                </div>
              </div>
            </section>
          </section>
        </div>
      </section>
    </template>
  </WorkspacePageFrame>
</template>

<style scoped>
.traces-workspace {
  width: 100%;
  max-width: 1600px;
  margin-inline: auto;
  padding: var(--co-space-5) clamp(var(--co-space-4), 2.5vw, var(--co-space-8)) var(--co-space-10);
  container-name: traces-workspace;
  container-type: inline-size;
}
.traces-workspace code { min-width: 0; overflow-wrap: anywhere; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 11px; }
.trace-discovery-stage { min-width: 0; }
.trace-workbench { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr); align-items: start; gap: var(--co-space-4); }
.trace-query-rail { position: static; display: grid; min-width: 0; gap: var(--co-space-3); }
.trace-results-panel { display: grid; min-width: 0; align-content: start; gap: var(--co-space-3); padding: 0 var(--co-space-4) var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-bg-surface); box-shadow: var(--co-shadow-row); }
.trace-results-header { display: grid; min-width: 0; min-height: 76px; grid-template-columns: minmax(220px, 1fr) auto; align-items: center; gap: var(--co-space-4); padding: var(--co-space-3) 0; border-bottom: 1px solid var(--co-border-subtle); }
.trace-results-header > div:first-child { display: grid; min-width: 0; gap: 3px; }
.trace-results-header h2 { margin: 0; font-size: 18px; }
.trace-results-header p { margin: 0; color: var(--co-text-secondary); font-size: 12px; }
.trace-results-actions { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); }
.trace-more-menu { display: grid; min-width: 250px; gap: 2px; padding: var(--co-space-2); }
.trace-more-menu :deep(button),
.trace-more-menu :deep(a) { width: 100%; justify-content: flex-start; }
.trace-results-overview { display: grid; min-width: 0; grid-template-columns: minmax(360px, .9fr) minmax(320px, 1.1fr); align-items: center; padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-subtle); }
.trace-results-metrics { display: grid; min-width: 0; grid-template-columns: repeat(4, minmax(80px, 1fr)); margin: 0; }
.trace-results-metrics div { min-width: 0; padding: var(--co-space-1) var(--co-space-4); border-left: 1px solid var(--co-border-subtle); }
.trace-results-metrics div:first-child { padding-left: 0; border-left: 0; }
.trace-results-metrics dt { color: var(--co-text-muted); font-size: 12px; }
.trace-results-metrics dd { margin: 3px 0 0; color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 18px; font-weight: 750; font-variant-numeric: tabular-nums; }
.trace-results-metrics dd.is-error { color: var(--co-status-critical-fg); }
.trace-results-slowest { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-3); padding-left: var(--co-space-4); border-left: 1px solid var(--co-border-subtle); }
.trace-results-slowest > div { display: grid; min-width: 0; gap: 3px; }
.trace-results-slowest span { color: var(--co-text-muted); font-size: 12px; font-weight: 650; }
.trace-results-slowest strong { overflow: hidden; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.trace-results-slowest p { margin: 0; color: var(--co-text-muted); font-size: 12px; }
.trace-results-meta { display: flex; width: fit-content; max-width: 100%; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-2) var(--co-space-4); padding-bottom: var(--co-space-2); border-bottom: 1px solid var(--co-border-subtle); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 11px; }
.trace-results-empty { display: grid; min-height: 280px; place-content: center; justify-items: center; gap: var(--co-space-5); padding: var(--co-space-6) var(--co-space-5); text-align: center; }
.trace-results-empty--no-results { min-height: 260px; }
.trace-results-empty__copy { display: flex; min-width: 0; align-items: center; gap: var(--co-space-4); text-align: left; }
.trace-results-empty__copy > span { display: grid; width: 48px; height: 48px; flex: 0 0 auto; place-items: center; border-radius: var(--co-radius-control); color: var(--co-text-muted); background: var(--co-bg-canvas); }
.trace-results-empty__copy > span :deep(svg) { display: block; width: 22px; height: 22px; }
.trace-results-empty h3 { margin: 0; font-size: 18px; }
.trace-results-empty p { max-width: 58ch; margin: var(--co-space-2) 0 0; color: var(--co-text-secondary); font-size: 12px; line-height: 1.65; }
.trace-results-empty__suggestions { display: grid; justify-items: center; gap: var(--co-space-2); }
.trace-results-empty__suggestions > span { color: var(--co-text-muted); font-size: 12px; font-weight: 650; }
.trace-results-empty__suggestions > div { display: flex; flex-wrap: wrap; justify-content: center; gap: var(--co-space-2); }
.trace-history-popover { box-sizing: border-box; width: min(420px, calc(100vw - 32px)); max-height: min(440px, calc(100dvh - 32px)); padding: var(--co-space-3); overflow: hidden; }
@media (max-width: 1024px) {
  .traces-workspace { padding-inline: var(--co-space-4); }
  .trace-results-header { grid-template-columns: minmax(0, 1fr); align-items: flex-start; padding-block: var(--co-space-3); }
  .trace-results-actions { justify-content: flex-start; }
  .trace-results-overview { grid-template-columns: minmax(0, 1fr); }
  .trace-results-slowest { margin-top: var(--co-space-3); padding-top: var(--co-space-3); padding-left: 0; border-top: 1px solid var(--co-border-subtle); border-left: 0; }
}

@container traces-workspace (max-width: 900px) {
  .trace-results-header { grid-template-columns: minmax(0, 1fr); align-items: flex-start; }
  .trace-results-actions { justify-content: flex-start; }
  .trace-results-overview { grid-template-columns: minmax(0, 1fr); }
  .trace-results-slowest { margin-top: var(--co-space-3); padding-top: var(--co-space-3); padding-left: 0; border-top: 1px solid var(--co-border-subtle); border-left: 0; }
}

@container traces-workspace (max-width: 620px) {
  .trace-results-panel { padding-inline: var(--co-space-3); }
  .trace-results-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .trace-results-metrics div:nth-child(odd) { padding-left: 0; border-left: 0; }
  .trace-results-slowest { align-items: flex-start; flex-direction: column; }
  .trace-results-empty { min-height: 260px; padding-inline: var(--co-space-3); }
  .trace-results-empty__copy { align-items: flex-start; }
}

@media (prefers-reduced-motion: reduce) {
  .traces-workspace * { scroll-behavior: auto; }
}
</style>
