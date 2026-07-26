<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  Archive,
  CheckCircle2,
  ChevronRight,
  ExternalLink,
  FileSearch,
  History,
  LoaderCircle,
  Logs,
  Play,
  RefreshCw,
  Save,
  Server,
  TextWrap,
  TriangleAlert,
} from "lucide-vue-next";
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
import { virtualWindow } from "../../models/telemetry";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";

interface RequestFailure {
  message: string;
  code: string;
  requestID: string;
  traceID: string;
  nextSteps: readonly string[];
}

const route = useRoute();
const router = useRouter();
const bootstrap = ref<BootstrapSnapshot | null>(null);
const workloads = ref<KubernetesResource[]>([]);
const catalog = ref<TelemetryCatalog | null>(null);
const historyItems = ref<LogQuery[]>([]);
const currentQuery = ref<LogQuery | null>(null);
const selectedResourceID = ref(queryValue(route.query.resource));
const selectedNamespace = ref(queryValue(route.query.namespace));
const mode = ref<TelemetryQueryMode>(queryValue(route.query.mode) === "expert" ? "expert" : "guided");
const textFilter = ref(queryValue(route.query.text));
const traceFilter = ref(queryValue(route.query.trace_id));
const levels = ref<string[]>([]);
const expertQuery = ref('{"match_all":{}}');
const fromValue = ref(toLocalInput(new Date(Date.now() - 15 * 60_000)));
const toValue = ref(toLocalInput(new Date()));
const limit = ref(200);
const tail = ref(false);
const wrapRows = ref(false);
const scrollTop = ref(0);
const selectedEntryIDs = ref(new Set<string>());
const inspectedEntry = ref<LogEntry | null>(null);
const retainedEvidence = ref<TelemetryEvidence[]>([]);
const consultation = ref<Consultation | null>(null);
const loading = ref(true);
const querying = ref(false);
const savingEvidence = ref(false);
const freezing = ref(false);
const pageError = ref<RequestFailure | null>(null);
const queryError = ref<RequestFailure | null>(null);
const statusMessage = ref("");
let controller: AbortController | undefined;
let mounted = true;

const rowViewportHeight = 520;
const allowedLogLevels = ["debug", "info", "warn", "error"];
const allowedLogLimits = [1, 100, 200, 500, 1000];
const rowHeight = computed(() => (wrapRows.value ? 92 : 54));
const namespaces = computed(() => bootstrap.value?.active_scope.namespaces ?? []);
const namespaceWorkloads = computed(() => workloads.value.filter((item) => !selectedNamespace.value || item.namespace === selectedNamespace.value));
const selectedResource = computed(() => workloads.value.find((item) => item.id === selectedResourceID.value) ?? null);
const providerReady = computed(() => catalog.value?.provider_state === "available" || catalog.value?.provider_state === "partial");
const validTimeRange = computed(() => {
  const from = new Date(fromValue.value).getTime();
  const to = new Date(toValue.value).getTime();
  return Number.isFinite(from) && Number.isFinite(to) && from < to;
});
const canRun = computed(() => Boolean(selectedResource.value && providerReady.value && validTimeRange.value && !querying.value));
const entries = computed(() => currentQuery.value?.entries ?? []);
const windowState = computed(() => virtualWindow(entries.value.length, scrollTop.value, rowViewportHeight, rowHeight.value));
const visibleEntries = computed(() => entries.value.slice(windowState.value.start, windowState.value.end));
const maxHistogramCount = computed(() => Math.max(1, ...(currentQuery.value?.histogram ?? []).map((bucket) => bucket.count)));
const workloadLocation = computed(() => ({
  path: "/infrastructure",
  query: contextRouteQuery(),
}));
const canFreeze = computed(() => currentQuery.value?.status === "succeeded" && !currentQuery.value.result_expired);

const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "medium" });

function queryValue(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === "string" ? value[0] : "";
  return typeof value === "string" ? value : "";
}

function routeList(value: unknown, allowed: readonly string[]): string[] {
  return queryValue(value).split(",").filter((item) => allowed.includes(item));
}

function routeLimit(value: unknown, allowed: readonly number[], fallback: number): number {
  const parsed = Number(queryValue(value));
  return allowed.includes(parsed) ? parsed : fallback;
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

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value < 1) return "0 B";
  if (value < 1024) return `${value} B`;
  return `${(value / 1024).toFixed(value < 10240 ? 1 : 0)} KiB`;
}

function providerStateLabel(state?: TelemetryCatalog["provider_state"]): string {
  return ({ available: "可用", partial: "部分可用", unavailable: "不可用", disabled: "已停用" } as Record<string, string>)[state ?? ""] ?? "检查中";
}

function levelLabel(value?: string): string {
  return value ? value.toUpperCase() : "INFO";
}

function normalizeFailure(reason: unknown, fallback: string): RequestFailure {
  if (!isApiError(reason)) return { message: fallback, code: "REQUEST_FAILED", requestID: "", traceID: "", nextSteps: [] };
  return {
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

function contextRouteQuery(from = fromValue.value, to = toValue.value): Record<string, string> {
  return {
    cluster: bootstrap.value?.active_scope.cluster_id ?? "",
    namespace: selectedNamespace.value,
    resource: selectedResourceID.value,
    from: from ? new Date(from).toISOString() : "",
    to: to ? new Date(to).toISOString() : "",
  };
}

async function syncRoute(queryID?: string) {
  const query: Record<string, string> = { ...contextRouteQuery(), mode: mode.value };
  if (textFilter.value) query.text = textFilter.value;
  if (traceFilter.value) query.trace_id = traceFilter.value;
  if (levels.value.length) query.levels = levels.value.join(",");
  query.limit = String(limit.value);
  if (tail.value) query.tail = "1";
  if (wrapRows.value) query.wrap = "1";
  if (queryID) query.query = queryID;
  for (const key of Object.keys(query)) if (!query[key]) delete query[key];
  await router.replace({ path: "/logs", query });
}

async function loadCatalogAndHistory(signal?: AbortSignal) {
  const context = telemetryContext();
  if (!context) return;
  const [nextCatalog, nextHistory] = await Promise.all([
    getLogsCatalog(context, signal),
    getLogQueries({ cluster_id: context.cluster_id, namespace: context.namespace, resource_id: context.resource.id, limit: 30 }, signal),
  ]);
  catalog.value = nextCatalog;
  historyItems.value = nextHistory;
}

async function loadWorkspace() {
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  pageError.value = null;
  currentQuery.value = null;
  selectedEntryIDs.value = new Set();
  inspectedEntry.value = null;
  try {
    const snapshot = await getBootstrap(controller.signal);
    if (!mounted) return;
    bootstrap.value = snapshot;
    selectedNamespace.value = queryValue(route.query.namespace) || snapshot.active_scope.namespaces[0] || "";
    const page = await getResources({
      cluster: snapshot.active_scope.cluster_id,
      kind: ["Deployment", "StatefulSet", "DaemonSet"],
      limit: 500,
    }, controller.signal);
    workloads.value = page.items.filter((item) => item.layer === "workload");
    const requested = queryValue(route.query.resource);
    selectedResourceID.value = workloads.value.some((item) => item.id === requested)
      ? requested
      : workloads.value.find((item) => item.namespace === selectedNamespace.value)?.id ?? workloads.value[0]?.id ?? "";
    if (selectedResource.value?.namespace) selectedNamespace.value = selectedResource.value.namespace;
    const from = routeTime(route.query.from);
    const to = routeTime(route.query.to);
    if (from && to) [fromValue.value, toValue.value] = [from, to];
    textFilter.value = queryValue(route.query.text);
    traceFilter.value = queryValue(route.query.trace_id);
    levels.value = routeList(route.query.levels, allowedLogLevels);
    limit.value = routeLimit(route.query.limit, allowedLogLimits, 200);
    tail.value = queryValue(route.query.tail) === "1";
    wrapRows.value = queryValue(route.query.wrap) === "1";
    await loadCatalogAndHistory(controller.signal);
    const queryID = queryValue(route.query.query);
    if (queryID) await openHistory(queryID, false);
  } catch (reason) {
    if (!controller.signal.aborted) pageError.value = normalizeFailure(reason, "Logs Workspace 读取失败。");
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
  currentQuery.value = null;
  retainedEvidence.value = [];
  consultation.value = null;
  await loadCatalogAndHistory();
  await syncRoute();
}

async function changeResource() {
  if (selectedResource.value?.namespace) selectedNamespace.value = selectedResource.value.namespace;
  currentQuery.value = null;
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

function selectBucket(from: string, to: string) {
  fromValue.value = toLocalInput(new Date(from));
  toValue.value = toLocalInput(new Date(to));
  void syncRoute();
}

async function runQuery() {
  const context = telemetryContext();
  if (!context || !canRun.value) return;
  querying.value = true;
  queryError.value = null;
  statusMessage.value = "";
  selectedEntryIDs.value = new Set();
  inspectedEntry.value = null;
  consultation.value = null;
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
    });
    currentQuery.value = result;
    historyItems.value = [result, ...historyItems.value.filter((item) => item.id !== result.id)].slice(0, 30);
    await syncRoute(result.id);
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Elasticsearch 查询失败。");
  } finally {
    querying.value = false;
  }
}

async function openHistory(id: string, updateRoute = true) {
  queryError.value = null;
  selectedEntryIDs.value = new Set();
  inspectedEntry.value = null;
  try {
    const result = await getLogQuery(id);
    currentQuery.value = result;
    mode.value = result.mode;
    expertQuery.value = result.query;
    tail.value = result.tail;
    fromValue.value = toLocalInput(new Date(result.time_range.from));
    toValue.value = toLocalInput(new Date(result.time_range.to));
    if (updateRoute) await syncRoute(result.id);
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "日志查询历史读取失败。");
  }
}

function onVirtualScroll(event: Event) {
  scrollTop.value = (event.currentTarget as HTMLElement).scrollTop;
}

function toggleEntry(id: string) {
  const next = new Set(selectedEntryIDs.value);
  if (next.has(id)) next.delete(id);
  else if (next.size < 32) next.add(id);
  selectedEntryIDs.value = next;
}

function inspectEntry(entry: LogEntry) {
  inspectedEntry.value = entry;
}

function openTrace(entry: LogEntry) {
  const exact = entry.links.find((link) => link.provider === "tempo" && link.availability === "available");
  if (exact?.href.startsWith("/")) {
    void router.push(exact.href);
    return;
  }
  if (!entry.trace_id) return;
  const at = new Date(entry.timestamp).getTime();
  void router.push({
    path: "/traces",
    query: {
      ...contextRouteQuery(toLocalInput(new Date(at - 5 * 60_000)), toLocalInput(new Date(at + 5 * 60_000))),
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

async function freezeContext() {
  const query = currentQuery.value;
  const resource = telemetryContext()?.resource;
  const scope = bootstrap.value?.active_scope;
  if (!query || !resource || !scope || !canFreeze.value || freezing.value) return;
  freezing.value = true;
  queryError.value = null;
  try {
    consultation.value = await createTelemetryConsultation({
      title: `${resource.name} 日志上下文`,
      cluster_id: scope.cluster_id,
      environment: scope.environment,
      namespaces: [resource.namespace],
      resource_refs: [resource],
      from: query.time_range.from,
      to: query.time_range.to,
      query_execution_refs: [query.id],
      evidence_refs: retainedEvidence.value.map((item) => item.id),
    });
    statusMessage.value = "当前日志查询已冻结为不可变 Context Snapshot。";
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

onBeforeUnmount(() => {
  mounted = false;
  controller?.abort();
  window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, receiveScopeChange);
});
</script>

<template>
  <section class="telemetry-workspace logs-workspace" aria-labelledby="logs-heading">
    <header class="telemetry-heading">
      <div>
        <div class="telemetry-heading__line">
          <Logs :size="20" aria-hidden="true" />
          <h1 id="logs-heading">日志</h1>
          <span class="provider-state" :data-state="catalog?.provider_state ?? 'checking'">
            <span aria-hidden="true" />Elasticsearch {{ providerStateLabel(catalog?.provider_state) }}
          </span>
        </div>
        <p>{{ bootstrap?.active_scope.cluster_id || "活动集群" }} / {{ selectedNamespace || "Namespace" }}<span v-if="selectedResource"> / {{ selectedResource.kind }} {{ selectedResource.name }}</span></p>
      </div>
      <button class="icon-button" type="button" title="刷新日志工作区" aria-label="刷新日志工作区" :disabled="loading" @click="refreshAll">
        <RefreshCw :size="18" :class="{ spinning: loading }" aria-hidden="true" />
      </button>
    </header>

    <div v-if="pageError" class="telemetry-notice is-error" role="alert"><TriangleAlert :size="18" aria-hidden="true" /><div><strong>{{ pageError.code }}</strong><span>{{ pageError.message }}</span></div></div>
    <div v-if="queryError" class="telemetry-notice is-error" role="alert" aria-live="assertive">
      <TriangleAlert :size="18" aria-hidden="true" />
      <div><strong>{{ queryError.code }}</strong><span>{{ queryError.message }}</span><small v-if="queryError.requestID">Request {{ queryError.requestID }} · Trace {{ queryError.traceID || "无" }}</small></div>
    </div>
    <div v-if="statusMessage" class="telemetry-notice is-success" role="status" aria-live="polite"><CheckCircle2 :size="18" aria-hidden="true" /><span>{{ statusMessage }}</span></div>

    <div v-if="loading" class="telemetry-loading" role="status"><LoaderCircle :size="22" class="spinning" aria-hidden="true" />正在读取活动 Scope 与真实 Workload…</div>

    <template v-else>
      <section class="telemetry-query-band" aria-label="日志查询">
        <div class="context-controls">
          <label><span>Namespace</span><select v-model="selectedNamespace" name="logs-namespace" @change="changeNamespace"><option v-for="namespace in namespaces" :key="namespace" :value="namespace">{{ namespace }}</option></select></label>
          <label class="workload-control"><span>Workload</span><select v-model="selectedResourceID" name="logs-workload" @change="changeResource"><option v-for="resource in namespaceWorkloads" :key="resource.id" :value="resource.id">{{ resource.kind }} · {{ resource.name }}</option></select></label>
          <div class="field-group"><span>查询模式</span><div class="segmented-control" role="group" aria-label="日志查询模式"><button type="button" :aria-pressed="mode === 'guided'" @click="mode = 'guided'">引导</button><button type="button" :aria-pressed="mode === 'expert'" @click="mode = 'expert'">Expert</button></div></div>
        </div>

        <div v-if="mode === 'guided'" class="guided-grid">
          <label><span>文本</span><input v-model="textFilter" name="logs-text" type="search" autocomplete="off" placeholder="例如：timeout…" /></label>
          <label><span>trace_id</span><input v-model="traceFilter" class="mono-text" name="logs-trace-id" type="text" inputmode="text" autocomplete="off" spellcheck="false" placeholder="例如：038cbd20…" /></label>
          <fieldset class="level-filter"><legend>级别</legend><label v-for="value in allowedLogLevels" :key="value"><input v-model="levels" name="logs-level" type="checkbox" :value="value" />{{ value.toUpperCase() }}</label></fieldset>
        </div>
        <label v-else class="expert-editor"><span>Elasticsearch query clause</span><textarea v-model="expertQuery" name="logs-expert-query" rows="5" spellcheck="false" /></label>

        <div class="time-controls">
          <div class="preset-control" role="group" aria-label="日志时间范围快捷选择"><button type="button" @click="selectPreset(15)">15m</button><button type="button" @click="selectPreset(60)">1h</button><button type="button" @click="selectPreset(360)">6h</button></div>
          <label><span>开始</span><input v-model="fromValue" name="logs-from" type="datetime-local" autocomplete="off" /></label>
          <label><span>结束</span><input v-model="toValue" name="logs-to" type="datetime-local" autocomplete="off" /></label>
          <label><span>上限</span><select v-model.number="limit" name="logs-limit"><option :value="1">1</option><option :value="100">100</option><option :value="200">200</option><option :value="500">500</option><option :value="1000">1000</option></select></label>
        </div>

        <div class="query-actions">
          <div class="bound-summary"><span>Lookback ≤ {{ Math.round((catalog?.bounds.max_lookback_seconds ?? 0) / 3600) }}h</span><span>Rows ≤ {{ catalog?.bounds.max_results ?? 0 }}</span><span>Response ≤ {{ formatBytes(catalog?.bounds.max_response_bytes ?? 0) }}</span><span>Timeout {{ catalog?.bounds.timeout_ms ?? 0 }}ms</span></div>
          <label class="binary-control"><input v-model="tail" name="logs-tail" type="checkbox" />Tail（有界）</label>
          <button class="command-button is-primary" type="button" :disabled="!canRun" @click="runQuery"><LoaderCircle v-if="querying" :size="17" class="spinning" aria-hidden="true" /><Play v-else :size="17" aria-hidden="true" />执行查询</button>
        </div>

        <div v-if="catalog && !providerReady" class="provider-unavailable" role="status"><TriangleAlert :size="18" aria-hidden="true" /><div><strong>Elasticsearch {{ providerStateLabel(catalog.provider_state) }}</strong><span>{{ catalog.provider_detail }}</span><small>{{ catalog.source.identity || "当前 Configuration Revision 没有可用端点" }}</small></div></div>
      </section>

      <section v-if="currentQuery?.histogram.length" class="histogram-section" aria-labelledby="logs-histogram-heading">
        <div class="section-title"><div><FileSearch :size="17" aria-hidden="true" /><h2 id="logs-histogram-heading">日志分布</h2></div><span>选择柱体收窄时间范围</span></div>
        <div class="histogram" :aria-label="`${currentQuery.histogram.length} 个日志时间桶`">
          <button v-for="bucket in currentQuery.histogram" :key="bucket.from" type="button" :title="`${formatTime(bucket.from)} · ${bucket.count} 条`" :aria-label="`${formatTime(bucket.from)}，${bucket.count} 条日志`" @click="selectBucket(bucket.from, bucket.to)"><span :style="{ height: `${Math.max(4, (bucket.count / maxHistogramCount) * 100)}%` }" /><i>{{ bucket.count }}</i></button>
        </div>
      </section>

      <div class="telemetry-result-grid">
        <main class="telemetry-result-column">
          <section class="result-header">
            <div><span class="section-kicker">Query Execution</span><h2>日志结果</h2></div>
            <div class="result-actions">
              <label class="binary-control"><input v-model="wrapRows" name="logs-wrap" type="checkbox" /><TextWrap :size="15" aria-hidden="true" />换行</label>
              <button class="command-button" type="button" :disabled="selectedEntryIDs.size === 0 || savingEvidence" @click="retainSelectedEvidence"><Save :size="16" aria-hidden="true" />保存 Evidence</button>
              <RouterLink class="command-button" :to="workloadLocation"><Server :size="16" aria-hidden="true" />Workload</RouterLink>
            </div>
          </section>

          <div v-if="currentQuery" class="execution-meta"><span><b>{{ currentQuery.result_count }}</b> rows</span><span><b>{{ formatBytes(currentQuery.response_bytes) }}</b></span><span>采集 {{ formatTime(currentQuery.source.collected_at) }}</span><span v-if="currentQuery.tail">Tail</span><span v-if="currentQuery.partial" class="is-warning">部分结果</span><span v-if="currentQuery.truncated" class="is-warning">已截断</span><span v-if="currentQuery.stale" class="is-warning">已陈旧</span></div>
          <div v-if="currentQuery?.truncated" class="telemetry-notice is-warning" role="status"><TriangleAlert :size="18" aria-hidden="true" /><span>结果达到当前上限；请收窄时间或过滤条件后重新查询。</span></div>
          <div v-if="currentQuery?.result_expired" class="telemetry-notice is-warning" role="status"><History :size="18" aria-hidden="true" /><span>Provider 行已过期，仅保留 Query Execution 审计元数据。请重新执行后再保存 Evidence。</span></div>
          <div v-if="!currentQuery" class="empty-result"><Logs :size="30" aria-hidden="true" /><strong>尚无日志查询</strong><span>选择真实 Workload 与时间范围后执行。</span></div>
          <div v-else-if="currentQuery.status === 'succeeded' && !currentQuery.result_expired && entries.length === 0" class="empty-result" data-testid="logs-empty"><FileSearch :size="30" aria-hidden="true" /><strong>此范围没有日志</strong><span>资源与时间上下文保持不变。</span></div>

          <div v-else-if="entries.length" class="virtual-log-list" :class="{ 'is-wrapped': wrapRows }" :style="{ height: `${rowViewportHeight}px` }" data-testid="virtual-log-list" role="list" aria-label="虚拟化日志行" @scroll="onVirtualScroll">
            <div class="virtual-spacer" :style="{ height: `${windowState.totalHeight}px` }">
              <div class="virtual-window" :style="{ transform: `translateY(${windowState.offset}px)` }">
                <article v-for="entry in visibleEntries" :key="entry.id" class="log-row" :class="{ 'is-inspected': inspectedEntry?.id === entry.id }" :style="{ height: `${rowHeight}px` }" role="listitem">
                  <label class="row-selector" @click.stop><input type="checkbox" :checked="selectedEntryIDs.has(entry.id)" :aria-label="`选择 ${formatTime(entry.timestamp)} 的日志`" @change="toggleEntry(entry.id)" /></label>
                  <button class="log-row-inspect" type="button" :aria-label="`检查 ${formatTime(entry.timestamp)} 的日志`" @click="inspectEntry(entry)">
                    <time :datetime="entry.timestamp">{{ formatTime(entry.timestamp) }}</time>
                    <span class="level-mark" :data-level="entry.level || 'info'">{{ levelLabel(entry.level) }}</span>
                    <code>{{ entry.message }}</code>
                  </button>
                  <button v-if="entry.trace_id" class="trace-link" type="button" :title="`打开 Trace ${entry.trace_id}`" :aria-label="`打开 Trace ${entry.trace_id}`" @click.stop="openTrace(entry)"><ChevronRight :size="17" aria-hidden="true" /></button>
                </article>
              </div>
            </div>
          </div>

          <section class="snapshot-bar" aria-labelledby="logs-context-heading">
            <div><Archive :size="18" aria-hidden="true" /><div><h2 id="logs-context-heading">冻结上下文</h2><span>{{ retainedEvidence.length }} 条 Evidence · {{ currentQuery ? 1 : 0 }} 个 Query Execution</span></div></div>
            <button class="command-button is-primary" type="button" :disabled="!canFreeze || freezing" @click="freezeContext"><LoaderCircle v-if="freezing" :size="16" class="spinning" aria-hidden="true" /><Archive v-else :size="16" aria-hidden="true" />创建 Snapshot</button>
          </section>
          <dl v-if="consultation" class="snapshot-proof" data-testid="context-snapshot"><div><dt>Consultation</dt><dd>{{ consultation.id }}</dd></div><div><dt>Snapshot</dt><dd>{{ consultation.context_snapshot.id }}</dd></div><div><dt>Content hash</dt><dd>{{ consultation.context_snapshot.content_hash }}</dd></div></dl>
        </main>

        <aside class="telemetry-inspector" aria-label="日志字段与历史">
          <section>
            <div class="section-title"><div><FileSearch :size="17" aria-hidden="true" /><h2>字段检查</h2></div></div>
            <div v-if="!inspectedEntry" class="aside-empty">选择一条日志查看已投影字段。</div>
            <template v-else>
              <dl class="field-list"><div><dt>timestamp</dt><dd>{{ formatTime(inspectedEntry.timestamp) }}</dd></div><div><dt>service</dt><dd>{{ inspectedEntry.service || "无" }}</dd></div><div><dt>trace_id</dt><dd>{{ inspectedEntry.trace_id || "无" }}</dd></div><div><dt>span_id</dt><dd>{{ inspectedEntry.span_id || "无" }}</dd></div><div v-for="(value, key) in inspectedEntry.attributes" :key="key"><dt>{{ key }}</dt><dd>{{ value }}</dd></div></dl>
              <button v-if="inspectedEntry.trace_id" class="command-button" type="button" @click="openTrace(inspectedEntry)">打开 Trace<ChevronRight :size="16" aria-hidden="true" /></button>
            </template>
          </section>
          <section>
            <div class="section-title"><div><History :size="17" aria-hidden="true" /><h2>查询历史</h2></div><span>{{ historyItems.length }}</span></div>
            <div v-if="historyItems.length === 0" class="aside-empty">尚无持久化执行元数据。</div>
            <button v-for="item in historyItems" :key="item.id" class="history-row" :class="{ active: currentQuery?.id === item.id }" type="button" @click="openHistory(item.id)"><span>{{ item.mode }} · {{ item.result_count }} rows</span><small>{{ formatTime(item.created_at) }}<template v-if="item.result_expired"> · 结果已过期</template></small></button>
          </section>
          <a v-if="currentQuery?.links.find((link) => link.provider === 'kibana')" class="provider-link" :href="currentQuery.links.find((link) => link.provider === 'kibana')?.href" target="_blank" rel="noopener noreferrer"><ExternalLink :size="16" aria-hidden="true" />在 Kibana 打开精确查询</a>
        </aside>
      </div>
    </template>
  </section>
</template>

<style scoped lang="scss">
@use "../../styles/telemetry-workspace";
</style>
