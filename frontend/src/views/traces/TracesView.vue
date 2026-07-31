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
  <section
    class="traces-workspace"
    aria-labelledby="traces-heading"
  >
    <WorkspaceHeader
      heading-id="traces-heading"
      eyebrow="Telemetry / Tempo"
      title="链路"
      description="真实 Scope 的有界 Trace 搜索、语义瀑布与 Span Evidence 上下文。"
    >
      <template #context>
        <UBadge
          color="neutral"
          variant="soft"
          :icon="providerReady ? 'i-lucide-circle-check' : 'i-lucide-circle-alert'"
          :label="`Tempo ${providerStateLabel(catalog?.provider_state)}`"
        />
        <code>{{ bootstrap?.active_scope.cluster_id || "活动集群" }} / {{ selectedNamespace || "Namespace" }}</code>
      </template>
      <template #actions>
        <UTooltip text="刷新 Traces 工作区">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-refresh-cw"
            square
            aria-label="刷新 Traces 工作区"
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

      <div class="traces-workspace__grid">
        <main class="traces-results">
          <header class="traces-results__header">
            <div>
              <span>Trace Search</span>
              <h2>搜索结果</h2>
            </div>
            <div>
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
                v-if="tempoURL"
                color="neutral"
                variant="outline"
                icon="i-lucide-external-link"
                label="Tempo"
                :to="tempoURL"
                target="_blank"
                rel="noopener noreferrer"
                external
              />
            </div>
          </header>

          <div
            v-if="currentSearch"
            class="traces-results__meta"
          >
            <span><b>{{ currentSearch.result_count }}</b> traces</span>
            <span><b>{{ formatBytes(currentSearch.response_bytes) }}</b></span>
            <span>采集 {{ formatTime(currentSearch.source.collected_at) }}</span>
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
            description="仅保留 Search Execution 审计元数据；请重新搜索。"
          >
            <template #actions>
              <UButton
                color="primary"
                icon="i-lucide-play"
                label="按当前条件重新搜索"
                :disabled="!canSearch"
                @click="searchTraces"
              />
            </template>
          </WorkspaceState>
          <WorkspaceState
            v-if="!currentSearch"
            kind="empty"
            title="尚无 Trace 搜索"
            description="选择真实 Workload 与时间范围后搜索。"
          />
          <WorkspaceState
            v-else-if="currentSearch.status === 'succeeded' && !currentSearch.result_expired && !currentSearch.traces.length"
            kind="empty"
            title="此范围没有 Trace"
            description="资源、过滤条件与时间上下文保持不变。"
          />
          <WorkspaceState
            v-else-if="currentSearch.status === 'failed' || currentSearch.status === 'cancelled'"
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
        </main>

        <TracesHistory
          :items="historyItems"
          :active-i-d="currentSearch?.id ?? ''"
          @select="openHistory"
        />
      </div>
    </template>
  </section>
</template>

<style scoped>
.traces-workspace {
  width: min(100%, 1680px);
  margin: 0 auto;
  padding: var(--co-space-5) clamp(var(--co-space-4), 2.5vw, var(--co-space-8)) var(--co-space-10);
}
.traces-workspace code { min-width: 0; overflow-wrap: anywhere; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 11px; }
.traces-workspace__grid { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr) minmax(240px, 290px); gap: var(--co-space-6); margin-top: var(--co-space-4); }
.traces-results { min-width: 0; }
.traces-results__header { display: flex; min-height: 54px; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.traces-results__header > div:first-child span { color: var(--co-text-muted); font-size: 10px; font-weight: 750; text-transform: uppercase; }
.traces-results__header h2 { margin: 2px 0 0; font-size: 17px; }
.traces-results__header > div:last-child { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); }
.traces-results__meta { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-2) var(--co-space-4); padding: var(--co-space-2) 0 var(--co-space-3); border-bottom: 1px solid var(--co-border-default); color: var(--co-text-secondary); font-size: 11px; }

@media (max-width: 1024px) {
  .traces-workspace__grid { grid-template-columns: minmax(0, 1fr); }
  .traces-results__header { align-items: flex-start; flex-direction: column; }
  .traces-results__header > div:last-child { justify-content: flex-start; }
}
</style>
