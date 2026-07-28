<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  Archive,
  CheckCircle2,
  ChevronRight,
  ExternalLink,
  GitBranch,
  History,
  LoaderCircle,
  Play,
  RefreshCw,
  Save,
  ScanSearch,
  Server,
  Timer,
  TriangleAlert,
} from "lucide-vue-next";
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
import { waterfallPosition } from "../../models/telemetry";
import { openAgentPanel, publishAgentContext, type AgentPageContext } from "../../utils/agentContext";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";

interface RequestFailure {
  message: string;
  code: string;
  requestID: string;
  traceID: string;
}

const route = useRoute();
const router = useRouter();
const bootstrap = ref<BootstrapSnapshot | null>(null);
const workloads = ref<KubernetesResource[]>([]);
const catalog = ref<TelemetryCatalog | null>(null);
const historyItems = ref<TraceSearch[]>([]);
const currentSearch = ref<TraceSearch | null>(null);
const detail = ref<TraceDetail | null>(null);
const selectedResourceID = ref(queryValue(route.query.resource));
const selectedNamespace = ref(queryValue(route.query.namespace));
const mode = ref<TelemetryQueryMode>(queryValue(route.query.mode) === "expert" ? "expert" : "guided");
const serviceFilter = ref("");
const operationFilter = ref("");
const statusFilter = ref("");
const minDuration = ref<number | undefined>();
const maxDuration = ref<number | undefined>();
const expertQuery = ref("{}");
const fromValue = ref(toLocalInput(new Date(Date.now() - 15 * 60_000)));
const toValue = ref(toLocalInput(new Date()));
const limit = ref(100);
const selectedSpanIDs = ref(new Set<string>());
const inspectedSpan = ref<TraceSpan | null>(null);
const retainedEvidence = ref<TelemetryEvidence[]>([]);
const consultation = ref<Consultation | null>(null);
const loading = ref(true);
const searching = ref(false);
const detailLoading = ref(false);
const savingEvidence = ref(false);
const freezing = ref(false);
const pageError = ref<RequestFailure | null>(null);
const queryError = ref<RequestFailure | null>(null);
const statusMessage = ref("");
let controller: AbortController | undefined;
let mounted = true;

const allowedTraceLimits = [1, 50, 100, 200];

const namespaces = computed(() => bootstrap.value?.active_scope.namespaces ?? []);
const namespaceWorkloads = computed(() => workloads.value.filter((item) => !selectedNamespace.value || item.namespace === selectedNamespace.value));
const selectedResource = computed(() => workloads.value.find((item) => item.id === selectedResourceID.value) ?? null);
const providerReady = computed(() => catalog.value?.provider_state === "available" || catalog.value?.provider_state === "partial");
const validTimeRange = computed(() => {
  const from = new Date(fromValue.value).getTime();
  const to = new Date(toValue.value).getTime();
  return Number.isFinite(from) && Number.isFinite(to) && from < to;
});
const canSearch = computed(() => Boolean(selectedResource.value && providerReady.value && validTimeRange.value && !searching.value));
const canFreeze = computed(() => Boolean(detail.value?.query_id || currentSearch.value?.status === "succeeded"));
const workloadLocation = computed(() => ({ path: "/infrastructure", query: contextRouteQuery() }));
const tempoLink = computed(() => detail.value?.links.find((link) => link.provider === "tempo" && link.target === "external"));
const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "medium" });

function queryValue(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === "string" ? value[0] : "";
  return typeof value === "string" ? value : "";
}

function routeOptionalNumber(value: unknown): number | undefined {
  const text = queryValue(value);
  if (!text) return undefined;
  const parsed = Number(text);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined;
}

function routeLimit(value: unknown, fallback: number): number {
  const parsed = Number(queryValue(value));
  return allowedTraceLimits.includes(parsed) ? parsed : fallback;
}

function toLocalInput(value: Date): string {
  const offset = value.getTimezoneOffset() * 60_000;
  return new Date(value.getTime() - offset).toISOString().slice(0, 16);
}

function routeTime(value: unknown): string {
  const parsed = new Date(queryValue(value));
  return Number.isNaN(parsed.getTime()) ? "" : toLocalInput(parsed);
}

function formatTime(value?: string): string {
  if (!value) return "无";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : dateFormatter.format(parsed);
}

function formatDuration(value: number): string {
  if (!Number.isFinite(value)) return "无";
  if (value < 1) return `${value.toFixed(3)} ms`;
  if (value < 1000) return `${value.toFixed(value < 10 ? 2 : 1)} ms`;
  return `${(value / 1000).toFixed(2)} s`;
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
  if (!isApiError(reason)) return { message: fallback, code: "REQUEST_FAILED", requestID: "", traceID: "" };
  return { message: reason.message, code: reason.code || "REQUEST_FAILED", requestID: reason.requestID, traceID: reason.traceID };
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
  const current = detail.value ?? currentSearch.value;
  const queryID = detail.value?.query_id || currentSearch.value?.id;
  if (!current || !queryID || (currentSearch.value?.status && currentSearch.value.status !== "succeeded")) return null;
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
        trace_id: detail.value?.trace_id,
        query_hash: currentSearch.value?.query_hash,
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

function contextRouteQuery(): Record<string, string> {
  return {
    cluster: bootstrap.value?.active_scope.cluster_id ?? "",
    namespace: selectedNamespace.value,
    resource: selectedResourceID.value,
    from: validTimeRange.value ? new Date(fromValue.value).toISOString() : "",
    to: validTimeRange.value ? new Date(toValue.value).toISOString() : "",
  };
}

async function syncRoute(searchID?: string, traceID?: string) {
  const query: Record<string, string> = { ...contextRouteQuery(), mode: mode.value };
  if (serviceFilter.value) query.service = serviceFilter.value;
  if (operationFilter.value) query.operation = operationFilter.value;
  if (statusFilter.value) query.status = statusFilter.value;
  if (minDuration.value !== undefined) query.min_duration_ms = String(minDuration.value);
  if (maxDuration.value !== undefined) query.max_duration_ms = String(maxDuration.value);
  query.limit = String(limit.value);
  if (searchID) query.search = searchID;
  if (traceID) query.trace_id = traceID;
  for (const key of Object.keys(query)) if (!query[key]) delete query[key];
  await router.replace({ path: "/traces", query });
}

async function loadCatalogAndHistory(signal?: AbortSignal) {
  const context = telemetryContext();
  if (!context) return;
  const [nextCatalog, nextHistory] = await Promise.all([
    getTracesCatalog(context, signal),
    getTraceSearches({ cluster_id: context.cluster_id, namespace: context.namespace, resource_id: context.resource.id, limit: 30 }, signal),
  ]);
  catalog.value = nextCatalog;
  historyItems.value = nextHistory;
}

async function loadWorkspace() {
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  pageError.value = null;
  currentSearch.value = null;
  detail.value = null;
  try {
    const snapshot = await getBootstrap(controller.signal);
    if (!mounted) return;
    bootstrap.value = snapshot;
    selectedNamespace.value = queryValue(route.query.namespace) || snapshot.active_scope.namespaces[0] || "";
    const page = await getResources({ cluster: snapshot.active_scope.cluster_id, kind: ["Deployment", "StatefulSet", "DaemonSet"], limit: 500 }, controller.signal);
    workloads.value = page.items.filter((item) => item.layer === "workload");
    const requested = queryValue(route.query.resource);
    selectedResourceID.value = workloads.value.some((item) => item.id === requested)
      ? requested
      : workloads.value.find((item) => item.namespace === selectedNamespace.value)?.id ?? workloads.value[0]?.id ?? "";
    if (selectedResource.value?.namespace) selectedNamespace.value = selectedResource.value.namespace;
    const from = routeTime(route.query.from);
    const to = routeTime(route.query.to);
    if (from && to) [fromValue.value, toValue.value] = [from, to];
    serviceFilter.value = queryValue(route.query.service);
    operationFilter.value = queryValue(route.query.operation);
    statusFilter.value = ["error", "ok"].includes(queryValue(route.query.status)) ? queryValue(route.query.status) : "";
    minDuration.value = routeOptionalNumber(route.query.min_duration_ms);
    maxDuration.value = routeOptionalNumber(route.query.max_duration_ms);
    limit.value = routeLimit(route.query.limit, 100);
    await loadCatalogAndHistory(controller.signal);
    const searchID = queryValue(route.query.search);
    if (searchID) await openHistory(searchID, false);
    const traceID = queryValue(route.query.trace_id);
    if (traceID) await openTrace(traceID, searchID || undefined, false);
  } catch (reason) {
    if (!controller.signal.aborted) pageError.value = normalizeFailure(reason, "Traces Workspace 读取失败。");
  } finally {
    if (!controller.signal.aborted) loading.value = false;
  }
}

async function refreshAll() {
  statusMessage.value = "";
  queryError.value = null;
  await loadWorkspace();
}

async function changeNamespace() {
  selectedResourceID.value = namespaceWorkloads.value[0]?.id ?? "";
  currentSearch.value = null;
  detail.value = null;
  retainedEvidence.value = [];
  consultation.value = null;
  await loadCatalogAndHistory();
  await syncRoute();
}

async function changeResource() {
  if (selectedResource.value?.namespace) selectedNamespace.value = selectedResource.value.namespace;
  currentSearch.value = null;
  detail.value = null;
  retainedEvidence.value = [];
  consultation.value = null;
  await loadCatalogAndHistory();
  await syncRoute();
}

function selectPreset(minutes: number) {
  const to = new Date();
  const from = new Date(to.getTime() - minutes * 60_000);
  fromValue.value = toLocalInput(from);
  toValue.value = toLocalInput(to);
}

async function searchTraces() {
  const context = telemetryContext();
  if (!context || !canSearch.value) return;
  searching.value = true;
  queryError.value = null;
  statusMessage.value = "";
  detail.value = null;
  selectedSpanIDs.value = new Set();
  retainedEvidence.value = [];
  consultation.value = null;
  try {
    const result = await startTraceSearch({
      ...context,
      mode: mode.value,
      query: mode.value === "expert" ? expertQuery.value : undefined,
      filter: mode.value === "guided" ? {
        service: serviceFilter.value.trim() || undefined,
        operation: operationFilter.value.trim() || undefined,
        status: statusFilter.value || undefined,
        min_duration_ms: minDuration.value || undefined,
        max_duration_ms: maxDuration.value || undefined,
      } : {},
      from: new Date(fromValue.value).toISOString(),
      to: new Date(toValue.value).toISOString(),
      limit: limit.value,
    });
    currentSearch.value = result;
    historyItems.value = [result, ...historyItems.value.filter((item) => item.id !== result.id)].slice(0, 30);
    await syncRoute(result.id);
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Tempo Trace 搜索失败。");
  } finally {
    searching.value = false;
  }
}

async function openHistory(id: string, updateRoute = true) {
  queryError.value = null;
  detail.value = null;
  try {
    const result = await getTraceSearch(id);
    currentSearch.value = result;
    mode.value = result.mode;
    expertQuery.value = result.query;
    fromValue.value = toLocalInput(new Date(result.time_range.from));
    toValue.value = toLocalInput(new Date(result.time_range.to));
    if (updateRoute) await syncRoute(result.id);
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Trace 搜索历史读取失败。");
  }
}

async function openTrace(trace: TraceSummary | string, searchID = currentSearch.value?.id, updateRoute = true) {
  const traceID = typeof trace === "string" ? trace : trace.trace_id;
  const context = telemetryContext();
  if (!context || detailLoading.value) return;
  detailLoading.value = true;
  queryError.value = null;
  selectedSpanIDs.value = new Set();
  inspectedSpan.value = null;
  retainedEvidence.value = [];
  consultation.value = null;
  try {
    detail.value = await getTraceDetail(traceID, searchID
      ? { search_id: searchID }
      : { context, from: new Date(fromValue.value).toISOString(), to: new Date(toValue.value).toISOString() });
    if (detail.value.spans.length) inspectedSpan.value = detail.value.spans[0];
    if (updateRoute) await syncRoute(searchID, traceID);
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Trace detail 读取失败。");
  } finally {
    detailLoading.value = false;
  }
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

function spanStyle(span: TraceSpan) {
  const value = detail.value
    ? waterfallPosition(detail.value.start_time, detail.value.duration_ms, span.start_time, span.duration_ms)
    : { left: 0, width: 0.35 };
  return { left: `${value.left}%`, width: `${value.width}%` };
}

async function retainSelectedEvidence() {
  const current = detail.value;
  if (!current || selectedSpanIDs.value.size === 0 || savingEvidence.value) return;
  savingEvidence.value = true;
  queryError.value = null;
  try {
    const evidence = await saveTraceEvidence(current.query_id, current.trace_id, [...selectedSpanIDs.value]);
    retainedEvidence.value = [evidence, ...retainedEvidence.value];
    statusMessage.value = `已保留 ${evidence.item_count} 个 span Evidence。`;
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Trace Evidence 保存失败。");
  } finally {
    savingEvidence.value = false;
  }
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

function receiveScopeChange() {
  void loadWorkspace();
}

onMounted(() => {
  window.addEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, receiveScopeChange);
  void loadWorkspace();
});

watch([() => route.fullPath, currentSearch, detail, retainedEvidence], publishCurrentAgentContext, { flush: "post" });

onBeforeUnmount(() => {
  mounted = false;
  controller?.abort();
  window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, receiveScopeChange);
  publishAgentContext(null);
});
</script>

<template>
  <section class="telemetry-workspace traces-workspace" aria-labelledby="traces-heading">
    <header class="telemetry-heading">
      <div>
        <div class="telemetry-heading__line"><ScanSearch :size="20" aria-hidden="true" /><h1 id="traces-heading">链路</h1><span class="provider-state" :data-state="catalog?.provider_state ?? 'checking'"><span aria-hidden="true" />Tempo {{ providerStateLabel(catalog?.provider_state) }}</span></div>
        <p>{{ bootstrap?.active_scope.cluster_id || "活动集群" }} / {{ selectedNamespace || "Namespace" }}<span v-if="selectedResource"> / {{ selectedResource.kind }} {{ selectedResource.name }}</span></p>
      </div>
      <button class="icon-button" type="button" title="刷新链路工作区" aria-label="刷新链路工作区" :disabled="loading" @click="refreshAll"><RefreshCw :size="18" :class="{ spinning: loading }" aria-hidden="true" /></button>
    </header>

    <div v-if="pageError" class="telemetry-notice is-error" role="alert"><TriangleAlert :size="18" aria-hidden="true" /><div><strong>{{ pageError.code }}</strong><span>{{ pageError.message }}</span></div></div>
    <div v-if="queryError" class="telemetry-notice is-error" role="alert" aria-live="assertive"><TriangleAlert :size="18" aria-hidden="true" /><div><strong>{{ queryError.code }}</strong><span>{{ queryError.message }}</span><small v-if="queryError.requestID">Request {{ queryError.requestID }} · Trace {{ queryError.traceID || "无" }}</small></div></div>
    <div v-if="statusMessage" class="telemetry-notice is-success" role="status" aria-live="polite"><CheckCircle2 :size="18" aria-hidden="true" /><span>{{ statusMessage }}</span></div>
    <div v-if="loading" class="telemetry-loading" role="status"><LoaderCircle :size="22" class="spinning" aria-hidden="true" />正在读取活动 Scope 与真实 Workload…</div>

    <template v-else>
      <section class="telemetry-query-band" aria-label="Trace 搜索">
        <div class="context-controls">
          <label><span>Namespace</span><select v-model="selectedNamespace" name="traces-namespace" autocomplete="off" @change="changeNamespace"><option v-for="namespace in namespaces" :key="namespace" :value="namespace">{{ namespace }}</option></select></label>
          <label class="workload-control"><span>Workload</span><select v-model="selectedResourceID" name="traces-workload" autocomplete="off" @change="changeResource"><option v-for="resource in namespaceWorkloads" :key="resource.id" :value="resource.id">{{ resource.kind }} · {{ resource.name }}</option></select></label>
          <div class="field-group"><span>查询模式</span><div class="segmented-control" role="group" aria-label="Trace 查询模式"><button type="button" :aria-pressed="mode === 'guided'" @click="mode = 'guided'">引导</button><button type="button" :aria-pressed="mode === 'expert'" @click="mode = 'expert'">Expert</button></div></div>
        </div>

        <div v-if="mode === 'guided'" class="guided-grid trace-guided-grid">
          <label><span>Service</span><input v-model="serviceFilter" name="trace-service" type="search" autocomplete="off" placeholder="例如：cloudops-api…" /></label>
          <label><span>Operation</span><input v-model="operationFilter" name="trace-operation" type="search" autocomplete="off" placeholder="例如：GET /readyz…" /></label>
          <label><span>Status</span><select v-model="statusFilter" name="trace-status" autocomplete="off"><option value="">全部</option><option value="error">Error</option><option value="ok">OK</option></select></label>
          <label><span>最短耗时（ms）</span><input v-model.number="minDuration" name="trace-min-duration" type="number" inputmode="numeric" autocomplete="off" min="0" /></label>
          <label><span>最长耗时（ms）</span><input v-model.number="maxDuration" name="trace-max-duration" type="number" inputmode="numeric" autocomplete="off" min="0" /></label>
        </div>
        <label v-else class="expert-editor"><span>TraceQL span selector</span><textarea v-model="expertQuery" name="trace-expert-query" autocomplete="off" rows="5" spellcheck="false" /></label>

        <div class="time-controls">
          <div class="preset-control" role="group" aria-label="Trace 时间范围快捷选择"><button type="button" @click="selectPreset(15)">15m</button><button type="button" @click="selectPreset(60)">1h</button><button type="button" @click="selectPreset(360)">6h</button></div>
          <label><span>开始</span><input v-model="fromValue" name="traces-from" type="datetime-local" autocomplete="off" /></label>
          <label><span>结束</span><input v-model="toValue" name="traces-to" type="datetime-local" autocomplete="off" /></label>
          <label><span>上限</span><select v-model.number="limit" name="traces-limit" autocomplete="off"><option :value="1">1</option><option :value="50">50</option><option :value="100">100</option><option :value="200">200</option></select></label>
        </div>

        <div class="query-actions">
          <div class="bound-summary"><span>Lookback ≤ {{ Math.round((catalog?.bounds.max_lookback_seconds ?? 0) / 3600) }}h</span><span>Traces ≤ {{ catalog?.bounds.max_results ?? 0 }}</span><span>Response ≤ {{ formatBytes(catalog?.bounds.max_response_bytes ?? 0) }}</span><span>Timeout {{ catalog?.bounds.timeout_ms ?? 0 }}ms</span></div>
          <button class="command-button is-primary" type="button" :disabled="!canSearch" @click="searchTraces"><LoaderCircle v-if="searching" :size="17" class="spinning" aria-hidden="true" /><Play v-else :size="17" aria-hidden="true" />搜索 Trace</button>
        </div>
        <div v-if="catalog && !providerReady" class="provider-unavailable" role="status"><TriangleAlert :size="18" aria-hidden="true" /><div><strong>Tempo {{ providerStateLabel(catalog.provider_state) }}</strong><span>{{ catalog.provider_detail }}</span><small>{{ catalog.source.identity || "当前 Configuration Revision 没有可用端点" }}</small></div></div>
      </section>

      <div class="telemetry-result-grid">
        <main class="telemetry-result-column">
          <section class="result-header"><div><span class="section-kicker">Trace Search</span><h2>搜索结果</h2></div><div class="result-actions"><RouterLink class="command-button" :to="workloadLocation"><Server :size="16" aria-hidden="true" />Workload</RouterLink></div></section>
          <div v-if="currentSearch" class="execution-meta"><span><b>{{ currentSearch.result_count }}</b> traces</span><span><b>{{ formatBytes(currentSearch.response_bytes) }}</b></span><span>采集 {{ formatTime(currentSearch.source.collected_at) }}</span><span v-if="currentSearch.partial" class="is-warning">部分结果</span><span v-if="currentSearch.truncated" class="is-warning">已截断</span><span v-if="currentSearch.stale" class="is-warning">已陈旧</span></div>
          <div v-if="currentSearch?.truncated" class="telemetry-notice is-warning" role="status"><TriangleAlert :size="18" aria-hidden="true" /><span>结果达到当前上限；请收窄时间或 TraceQL 条件。</span></div>
          <div v-if="currentSearch?.result_expired" class="telemetry-notice is-warning" role="status"><History :size="18" aria-hidden="true" /><span>Provider Trace summaries 已过期，仅保留执行元数据。请重新搜索。</span></div>
          <div v-if="!currentSearch && !detailLoading && !detail" class="empty-result"><ScanSearch :size="30" aria-hidden="true" /><strong>尚无 Trace 搜索</strong><span>选择真实 Workload 与时间范围后搜索。</span></div>
          <div v-else-if="currentSearch?.status === 'succeeded' && !currentSearch.result_expired && currentSearch.traces.length === 0 && !detail" class="empty-result" data-testid="traces-empty"><ScanSearch :size="30" aria-hidden="true" /><strong>此范围没有 Trace</strong><span>资源与时间上下文保持不变。</span></div>

          <section v-if="currentSearch?.traces.length" class="trace-list" aria-label="Trace 搜索结果">
            <button v-for="trace in currentSearch.traces" :key="trace.trace_id" class="trace-summary-row" :class="{ active: detail?.trace_id === trace.trace_id }" type="button" @click="openTrace(trace)"><div><strong>{{ trace.root_service }} · {{ trace.root_operation }}</strong><code>{{ trace.trace_id }}</code><small>{{ formatTime(trace.start_time) }}</small></div><span>{{ formatDuration(trace.duration_ms) }}</span><span>{{ trace.span_count }} spans</span><span :class="{ 'error-count': trace.error_span_count > 0 }">{{ trace.error_span_count }} errors</span><ChevronRight :size="16" aria-hidden="true" /></button>
          </section>

          <div v-if="detailLoading" class="telemetry-loading" role="status"><LoaderCircle :size="22" class="spinning" aria-hidden="true" />正在读取 Tempo Trace detail…</div>
          <template v-else-if="detail">
            <section class="trace-detail-heading"><div><span class="section-kicker">Trace detail</span><h2>{{ detail.root_service }} · {{ detail.root_operation }}</h2><p>{{ detail.trace_id }}</p></div><div class="result-actions"><button class="command-button" type="button" :disabled="selectedSpanIDs.size === 0 || savingEvidence" @click="retainSelectedEvidence"><Save :size="16" aria-hidden="true" />保存 Evidence</button><a v-if="tempoLink" class="command-button" :href="tempoLink.href" target="_blank" rel="noopener noreferrer"><ExternalLink :size="16" aria-hidden="true" />Tempo</a></div></section>
            <div class="execution-meta"><span><b>{{ detail.spans.length }}</b> spans</span><span><b>{{ formatDuration(detail.duration_ms) }}</b></span><span><b>{{ formatBytes(detail.response_bytes) }}</b></span><span v-if="detail.partial" class="is-warning">部分结果</span><span v-if="detail.truncated" class="is-warning">已截断</span></div>
            <div class="waterfall-scroll" data-testid="trace-waterfall">
              <div class="waterfall" role="list" aria-label="Trace waterfall">
                <div class="waterfall-header" aria-hidden="true"><span>Span</span><span>0 → {{ formatDuration(detail.duration_ms) }}</span><span>耗时</span></div>
                <div v-for="span in detail.spans" :key="span.span_id" class="waterfall-row" :class="{ active: inspectedSpan?.span_id === span.span_id }" :style="{ '--span-depth': span.depth }" role="listitem">
                  <label class="span-label"><input type="checkbox" autocomplete="off" :checked="selectedSpanIDs.has(span.span_id)" :aria-label="`选择 span ${span.name}`" @change="toggleSpan(span.span_id)" /><span class="span-copy"><strong>{{ span.name }}</strong><small>{{ span.service }}</small></span></label>
                  <button class="waterfall-inspect" type="button" :aria-label="`检查 span ${span.name}`" @click="inspectSpan(span)"><span class="span-track" aria-hidden="true"><i class="span-bar" :class="{ 'is-error': span.status === 'error', 'is-critical': span.critical_path }" :style="spanStyle(span)" /></span><span class="span-duration">{{ formatDuration(span.duration_ms) }}</span></button>
                </div>
              </div>
            </div>

            <section class="snapshot-bar" aria-labelledby="trace-context-heading"><div><Archive :size="18" aria-hidden="true" /><div><h2 id="trace-context-heading">冻结上下文</h2><span>{{ retainedEvidence.length }} 条 Evidence · Trace detail execution {{ detail.query_id }}</span></div></div><button class="command-button is-primary" type="button" :disabled="!canFreeze || freezing" @click="freezeContext"><LoaderCircle v-if="freezing" :size="16" class="spinning" aria-hidden="true" /><Archive v-else :size="16" aria-hidden="true" />创建 Snapshot</button></section>
            <dl v-if="consultation" class="snapshot-proof" data-testid="context-snapshot"><div><dt>Consultation</dt><dd>{{ consultation.id }}</dd></div><div><dt>Snapshot</dt><dd>{{ consultation.context_snapshot.id }}</dd></div><div><dt>Content hash</dt><dd>{{ consultation.context_snapshot.content_hash }}</dd></div></dl>
          </template>
        </main>

        <aside class="telemetry-inspector" aria-label="Span 详情与历史">
          <section>
            <div class="section-title"><div><GitBranch :size="17" aria-hidden="true" /><h2>Span 详情</h2></div><span v-if="inspectedSpan?.critical_path">Critical path</span></div>
            <div v-if="!inspectedSpan" class="aside-empty">选择 waterfall 中的 span 查看属性与事件。</div>
            <template v-else>
              <dl class="field-list"><div><dt>span_id</dt><dd>{{ inspectedSpan.span_id }}</dd></div><div><dt>parent</dt><dd>{{ inspectedSpan.parent_span_id || "root" }}</dd></div><div><dt>service</dt><dd>{{ inspectedSpan.service }}</dd></div><div><dt>kind</dt><dd>{{ inspectedSpan.kind || "unspecified" }}</dd></div><div><dt>status</dt><dd>{{ inspectedSpan.status }}</dd></div><div v-for="(value, key) in inspectedSpan.attributes" :key="key"><dt>{{ key }}</dt><dd>{{ value }}</dd></div></dl>
              <div v-if="inspectedSpan.events?.length" class="span-events"><div class="section-title"><div><Timer :size="15" aria-hidden="true" /><h2>Events</h2></div></div><dl class="field-list"><div v-for="event in inspectedSpan.events" :key="`${event.timestamp}:${event.name}`"><dt>{{ formatTime(event.timestamp) }}</dt><dd>{{ event.name }}</dd></div></dl></div>
            </template>
          </section>
          <section>
            <div class="section-title"><div><History :size="17" aria-hidden="true" /><h2>搜索历史</h2></div><span>{{ historyItems.length }}</span></div>
            <div v-if="historyItems.length === 0" class="aside-empty">尚无持久化执行元数据。</div>
            <button v-for="item in historyItems" :key="item.id" class="history-row" :class="{ active: currentSearch?.id === item.id }" type="button" @click="openHistory(item.id)"><span>{{ item.mode }} · {{ item.result_count }} traces</span><small>{{ formatTime(item.created_at) }}<template v-if="item.result_expired"> · 结果已过期</template></small></button>
          </section>
        </aside>
      </div>
    </template>
  </section>
</template>

<style scoped lang="scss">
@use "../../styles/telemetry-workspace";

.trace-guided-grid { grid-template-columns: 1.2fr 1.2fr 0.7fr 0.8fr 0.8fr; }
.span-label { display: flex; align-items: center; gap: 7px; }
.span-label input { width: 16px; height: 16px; flex: 0 0 16px; accent-color: var(--co-action-primary); }
.span-events { margin-top: 14px; }

@media (max-width: 1100px) {
  .trace-guided-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}

@media (max-width: 700px) {
  .trace-guided-grid { grid-template-columns: 1fr; }
}
</style>
