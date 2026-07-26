<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  Activity,
  Ban,
  BookMarked,
  CheckCircle2,
  Clipboard,
  ExternalLink,
  History,
  LineChart,
  LoaderCircle,
  Play,
  RefreshCw,
  Save,
  ShieldCheck,
  Square,
  TableProperties,
  TriangleAlert,
  Undo2,
} from "lucide-vue-next";
import { useRoute, useRouter } from "vue-router";

import { isApiError } from "../../api/client";
import { getResources, type KubernetesResource } from "../../api/infrastructure";
import {
  cancelMonitoringQuery,
  createQueryAuthorization,
  getMonitoringCatalog,
  getMonitoringQueries,
  getMonitoringQuery,
  getQueryAuthorizations,
  getQueryDefinitions,
  revokeQueryAuthorization,
  saveQueryDefinition,
  startMonitoringQuery,
  type MonitoringCatalog,
  type MonitoringContextLink,
  type QueryAuthorization,
  type QueryDefinition,
  type QueryExecution,
  type QueryMode,
  type QuerySeries,
} from "../../api/monitoring";
import { getBootstrap, type BootstrapSnapshot } from "../../api/platform";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";

interface RequestFailure {
  message: string;
  code: string;
  requestID: string;
  traceID: string;
  nextSteps: readonly string[];
}

interface TableRow {
  key: string;
  series: QuerySeries;
  label: string;
  latestValue: number | null;
  latestAt: string;
}

const route = useRoute();
const router = useRouter();
const bootstrap = ref<BootstrapSnapshot | null>(null);
const workloads = ref<KubernetesResource[]>([]);
const catalog = ref<MonitoringCatalog | null>(null);
const historyItems = ref<QueryExecution[]>([]);
const definitions = ref<QueryDefinition[]>([]);
const authorizations = ref<QueryAuthorization[]>([]);
const currentExecution = ref<QueryExecution | null>(null);
const selectedResourceID = ref(queryValue(route.query.resource));
const selectedNamespace = ref(queryValue(route.query.namespace));
const mode = ref<QueryMode>(queryValue(route.query.mode) === "expert" ? "expert" : "guided");
const guidedKey = ref(queryValue(route.query.metric));
const expertQuery = ref(queryValue(route.query.query));
const fromValue = ref(toLocalInput(new Date(Date.now() - 15 * 60_000)));
const toValue = ref(toLocalInput(new Date()));
const stepSeconds = ref(30);
const activeDefinitionID = ref("");
const loading = ref(true);
const catalogLoading = ref(false);
const queryRunning = ref(false);
const saving = ref(false);
const authorizationBusy = ref("");
const pageError = ref<RequestFailure | null>(null);
const queryError = ref<RequestFailure | null>(null);
const statusMessage = ref("");
const saveDialogOpen = ref(false);
const saveTitle = ref("");
const saveDescription = ref("");
const selectedManagementTab = ref<"definitions" | "authorizations">("definitions");
let mounted = true;
let requestGeneration = 0;

const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "medium" });
const timeFormatter = new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
const numberFormatter = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 5 });
const chartColors = ["#2f81f7", "#2da44e", "#d29922", "#bf3989", "#8250df", "#cf4a32", "#0891b2", "#57606a"];
const terminalStatuses = new Set(["succeeded", "failed", "cancelled"]);

const namespaces = computed(() => bootstrap.value?.active_scope.namespaces ?? []);
const namespaceWorkloads = computed(() => workloads.value.filter((item) => !selectedNamespace.value || item.namespace === selectedNamespace.value));
const selectedResource = computed(() => workloads.value.find((item) => item.id === selectedResourceID.value) ?? null);
const selectedCatalogEntry = computed(() => catalog.value?.queries.find((item) => item.key === guidedKey.value) ?? null);
const providerReady = computed(() => catalog.value?.provider_state === "available" || catalog.value?.provider_state === "partial");
const queryInFlight = computed(() => currentExecution.value?.status === "pending" || currentExecution.value?.status === "running");
const validTimeRange = computed(() => {
  const from = new Date(fromValue.value).getTime();
  const to = new Date(toValue.value).getTime();
  return Number.isFinite(from) && Number.isFinite(to) && from < to;
});
const canRun = computed(() => Boolean(selectedResource.value && providerReady.value && !queryRunning.value && validTimeRange.value));
const queryByteLength = computed(() => new TextEncoder().encode(expertQuery.value).length);
const providerLink = computed(() => currentExecution.value?.links.find((link) => link.kind === "provider" && link.provider === "grafana"));
const chartSeries = computed(() => currentExecution.value?.result?.series.filter((series) => series.points.length > 0) ?? []);
const tableRows = computed<TableRow[]>(() => chartSeries.value.map((series, index) => {
  const latest = series.points[series.points.length - 1];
  return {
    key: `${index}-${JSON.stringify(series.labels)}`,
    series,
    label: seriesLabel(series, index),
    latestValue: latest?.value ?? null,
    latestAt: latest?.timestamp ?? "",
  };
}));
const chartDomain = computed(() => {
  const points = chartSeries.value.flatMap((series) => series.points)
    .map((point) => ({ time: new Date(point.timestamp).getTime(), value: point.value }))
    .filter((point) => Number.isFinite(point.time) && Number.isFinite(point.value));
  if (!points.length) return null;
  let minTime = Math.min(...points.map((point) => point.time));
  let maxTime = Math.max(...points.map((point) => point.time));
  let minValue = Math.min(...points.map((point) => point.value));
  let maxValue = Math.max(...points.map((point) => point.value));
  if (minTime === maxTime) maxTime = minTime + 1000;
  if (minValue === maxValue) {
    const padding = Math.abs(minValue) * 0.1 || 1;
    minValue -= padding;
    maxValue += padding;
  }
  return { minTime, maxTime, minValue, maxValue };
});

function queryValue(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === "string" ? value[0] : "";
  return typeof value === "string" ? value : "";
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

function toLocalInput(value: Date): string {
  const offset = value.getTimezoneOffset() * 60_000;
  return new Date(value.getTime() - offset).toISOString().slice(0, 16);
}

function parseRouteTime(value: unknown): string {
  const raw = queryValue(value);
  if (!raw) return "";
  const parsed = new Date(raw);
  return Number.isNaN(parsed.getTime()) ? "" : toLocalInput(parsed);
}

function formatTime(value?: string): string {
  if (!value) return "无";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : dateFormatter.format(parsed);
}

function shortHash(value: string): string {
  return value.length > 18 ? `${value.slice(0, 14)}…` : value;
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KiB", "MiB"];
  let amount = value;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024;
    unit += 1;
  }
  return `${new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 }).format(amount)} ${units[unit]}`;
}

function statusLabel(status: QueryExecution["status"]): string {
  return ({ pending: "等待执行", running: "查询中", succeeded: "已完成", failed: "失败", cancelled: "已取消" })[status];
}

function providerStateLabel(state?: MonitoringCatalog["provider_state"]): string {
  return ({ available: "可用", partial: "部分可用", unavailable: "不可用", disabled: "已停用" } as Record<string, string>)[state ?? ""] ?? "检查中";
}

function authorizationState(item: QueryAuthorization): string {
  if (item.revoked_at) return "已撤销";
  if (item.consumed_execution_id) return "已使用";
  return "有效";
}

function seriesLabel(series: QuerySeries, index: number): string {
  const labels = Object.entries(series.labels)
    .filter(([key]) => key !== "__name__")
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, value]) => `${key}=${value}`);
  return labels.join(" · ") || series.labels.__name__ || `序列 ${index + 1}`;
}

function seriesPath(series: QuerySeries): string {
  const domain = chartDomain.value;
  if (!domain) return "";
  const left = 68;
  const top = 20;
  const width = 804;
  const height = 250;
  return series.points.map((point, index) => {
    const timestamp = new Date(point.timestamp).getTime();
    const x = left + ((timestamp - domain.minTime) / (domain.maxTime - domain.minTime)) * width;
    const y = top + (1 - ((point.value - domain.minValue) / (domain.maxValue - domain.minValue))) * height;
    return `${index === 0 ? "M" : "L"}${x.toFixed(2)},${y.toFixed(2)}`;
  }).join(" ");
}

function chartTime(value: number): string {
  return timeFormatter.format(new Date(value));
}

function selectPreset(minutes: number) {
  const to = new Date();
  const from = new Date(to.getTime() - minutes * 60_000);
  fromValue.value = toLocalInput(from);
  toValue.value = toLocalInput(to);
  clearDefinitionBinding();
}

async function syncRoute(extra: Record<string, string | undefined> = {}) {
  const query: Record<string, string> = {
    cluster: bootstrap.value?.active_scope.cluster_id ?? "",
    namespace: selectedNamespace.value,
    resource: selectedResourceID.value,
    mode: mode.value,
    from: validTimeRange.value ? new Date(fromValue.value).toISOString() : "",
    to: validTimeRange.value ? new Date(toValue.value).toISOString() : "",
  };
  if (mode.value === "guided" && guidedKey.value) query.metric = guidedKey.value;
  if (activeDefinitionID.value) query.definition = activeDefinitionID.value;
  for (const [key, value] of Object.entries(extra)) {
    if (value) query[key] = value;
    else delete query[key];
  }
  for (const key of Object.keys(query)) if (!query[key]) delete query[key];
  await router.replace({ path: "/monitoring", query });
}

function monitoringContext() {
  const resource = selectedResource.value;
  const clusterID = bootstrap.value?.active_scope.cluster_id;
  if (!resource || !clusterID || !resource.namespace) return null;
  return {
    cluster_id: clusterID,
    namespace: resource.namespace,
    resource: { id: resource.id, kind: resource.kind, namespace: resource.namespace, name: resource.name },
  };
}

async function loadWorkspace() {
  loading.value = true;
  pageError.value = null;
  const generation = ++requestGeneration;
  try {
    const snapshot = await getBootstrap();
    if (!mounted || generation !== requestGeneration) return;
    bootstrap.value = snapshot;
    selectedNamespace.value = queryValue(route.query.namespace) || snapshot.active_scope.namespaces[0] || "";
    const page = await getResources({
      cluster: snapshot.active_scope.cluster_id,
      kind: ["Deployment", "StatefulSet", "DaemonSet"],
      limit: 500,
    });
    if (!mounted || generation !== requestGeneration) return;
    workloads.value = page.items.filter((item) => item.layer === "workload");
    const requestedResource = queryValue(route.query.resource);
    selectedResourceID.value = workloads.value.some((item) => item.id === requestedResource)
      ? requestedResource
      : workloads.value.find((item) => item.namespace === selectedNamespace.value)?.id ?? workloads.value[0]?.id ?? "";
    const resource = selectedResource.value;
    if (resource?.namespace) selectedNamespace.value = resource.namespace;

    const routeFrom = parseRouteTime(route.query.from);
    const routeTo = parseRouteTime(route.query.to);
    if (routeFrom && routeTo) {
      fromValue.value = routeFrom;
      toValue.value = routeTo;
    }
    await Promise.all([loadCatalog(), loadManagement()]);
    const executionID = queryValue(route.query.execution);
    if (executionID) await openExecution(executionID, false);
    const definitionID = queryValue(route.query.definition);
    if (definitionID) {
      const definition = definitions.value.find((item) => item.id === definitionID);
      if (definition) await useDefinition(definition, false);
    }
  } catch (reason) {
    if (mounted && generation === requestGeneration) pageError.value = normalizeFailure(reason, "Monitoring Workspace 读取失败。 ");
  } finally {
    if (generation === requestGeneration) loading.value = false;
  }
}

async function loadCatalog() {
  const context = monitoringContext();
  if (!context) {
    catalog.value = null;
    historyItems.value = [];
    return;
  }
  catalogLoading.value = true;
  queryError.value = null;
  try {
    const [nextCatalog, nextHistory] = await Promise.all([
      getMonitoringCatalog(context),
      getMonitoringQueries({
        cluster_id: context.cluster_id,
        namespace: context.namespace,
        resource_id: context.resource.id,
        limit: 30,
      }),
    ]);
    catalog.value = nextCatalog;
    historyItems.value = nextHistory;
    if (!nextCatalog.queries.some((item) => item.key === guidedKey.value)) guidedKey.value = nextCatalog.queries[0]?.key ?? "";
    if (!expertQuery.value) expertQuery.value = nextCatalog.queries.find((item) => item.key === guidedKey.value)?.query ?? "";
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Prometheus 查询目录读取失败。 ");
  } finally {
    catalogLoading.value = false;
  }
}

async function loadManagement() {
  try {
    const [saved, grants] = await Promise.all([getQueryDefinitions(), getQueryAuthorizations()]);
    definitions.value = saved;
    authorizations.value = grants;
  } catch (reason) {
    pageError.value = normalizeFailure(reason, "Query Definition 与授权记录读取失败。 ");
  }
}

async function refreshAll() {
  statusMessage.value = "";
  await Promise.all([loadCatalog(), loadManagement()]);
}

async function changeNamespace() {
  const next = namespaceWorkloads.value[0];
  selectedResourceID.value = next?.id ?? "";
  currentExecution.value = null;
  activeDefinitionID.value = "";
  await loadCatalog();
  await syncRoute({ execution: undefined, definition: undefined });
}

async function changeResource() {
  const resource = selectedResource.value;
  if (resource) selectedNamespace.value = resource.namespace ?? "";
  currentExecution.value = null;
  activeDefinitionID.value = "";
  await loadCatalog();
  await syncRoute({ execution: undefined, definition: undefined });
}

function changeMode(next: QueryMode) {
  if (mode.value === next) return;
  mode.value = next;
  if (next === "expert" && !expertQuery.value) expertQuery.value = selectedCatalogEntry.value?.query ?? "";
  clearDefinitionBinding();
  void syncRoute({ execution: undefined });
}

function selectGuidedQuery() {
  const entry = selectedCatalogEntry.value;
  if (entry) expertQuery.value = entry.query;
  clearDefinitionBinding();
  void syncRoute({ execution: undefined });
}

function clearDefinitionBinding() {
  activeDefinitionID.value = "";
}

async function runQuery() {
  const context = monitoringContext();
  if (!context || !validTimeRange.value || !canRun.value) return;
  queryRunning.value = true;
  queryError.value = null;
  statusMessage.value = "";
  const generation = ++requestGeneration;
  try {
    const execution = await startMonitoringQuery({
      ...context,
      mode: mode.value,
      catalog_key: mode.value === "guided" ? guidedKey.value : undefined,
      query: mode.value === "expert" ? expertQuery.value : undefined,
      from: new Date(fromValue.value).toISOString(),
      to: new Date(toValue.value).toISOString(),
      step_seconds: stepSeconds.value,
      query_definition_id: activeDefinitionID.value || undefined,
    });
    if (!mounted || generation !== requestGeneration) return;
    currentExecution.value = execution;
    await syncRoute({ execution: execution.id });
    await pollExecution(execution.id, generation);
  } catch (reason) {
    if (mounted && generation === requestGeneration) queryError.value = normalizeFailure(reason, "查询执行失败。 ");
  } finally {
    if (generation === requestGeneration) queryRunning.value = false;
  }
}

async function pollExecution(id: string, generation: number) {
  for (let attempt = 0; attempt < 120 && mounted && generation === requestGeneration; attempt += 1) {
    const execution = await getMonitoringQuery(id);
    if (!mounted || generation !== requestGeneration) return;
    currentExecution.value = execution;
    if (terminalStatuses.has(execution.status)) {
      await reloadHistory();
      return;
    }
    await new Promise((resolve) => window.setTimeout(resolve, 250));
  }
  if (mounted && generation === requestGeneration) {
    queryError.value = normalizeFailure(null, "查询仍在运行，请从历史记录重新打开。 ");
  }
}

async function reloadHistory() {
  const context = monitoringContext();
  if (!context) return;
  historyItems.value = await getMonitoringQueries({
    cluster_id: context.cluster_id,
    namespace: context.namespace,
    resource_id: context.resource.id,
    limit: 30,
  });
}

async function stopQuery() {
  const execution = currentExecution.value;
  if (!execution || !queryInFlight.value) return;
  try {
    currentExecution.value = await cancelMonitoringQuery(execution.id);
    requestGeneration += 1;
    queryRunning.value = false;
    await reloadHistory();
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "查询取消失败。 ");
  }
}

async function openExecution(id: string, updateRoute = true) {
  queryError.value = null;
  try {
    const execution = await getMonitoringQuery(id);
    currentExecution.value = execution;
    mode.value = execution.mode;
    guidedKey.value = execution.catalog_key ?? guidedKey.value;
    expertQuery.value = execution.query;
    fromValue.value = toLocalInput(new Date(execution.time_range.from));
    toValue.value = toLocalInput(new Date(execution.time_range.to));
    stepSeconds.value = execution.bounds.step_seconds;
    activeDefinitionID.value = execution.query_definition_id ?? "";
    if (updateRoute) await syncRoute({ execution: execution.id });
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Query Execution 读取失败。 ");
  }
}

function openSaveDialog() {
  const execution = currentExecution.value;
  if (!execution || execution.status !== "succeeded") return;
  saveTitle.value = `${execution.resource.name} ${mode.value === "guided" ? selectedCatalogEntry.value?.title ?? "指标查询" : "PromQL"}`;
  saveDescription.value = "";
  saveDialogOpen.value = true;
}

async function saveDefinition() {
  const execution = currentExecution.value;
  if (!execution || !saveTitle.value.trim() || saving.value) return;
  saving.value = true;
  queryError.value = null;
  try {
    const definition = await saveQueryDefinition({
      query_execution_id: execution.id,
      previous_query_definition_id: execution.query_definition_id || undefined,
      title: saveTitle.value.trim(),
      description: saveDescription.value.trim() || undefined,
    });
    definitions.value = [definition, ...definitions.value.filter((item) => item.id !== definition.id)];
    activeDefinitionID.value = definition.id;
    saveDialogOpen.value = false;
    statusMessage.value = `Query Definition 已保存为 revision ${definition.revision}。`;
    await syncRoute({ definition: definition.id, execution: execution.id });
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Query Definition 保存失败。 ");
  } finally {
    saving.value = false;
  }
}

async function useDefinition(definition: QueryDefinition, updateRoute = true) {
  const resource = workloads.value.find((item) => item.id === definition.resource.id);
  if (!resource) {
    queryError.value = normalizeFailure(null, "该 Query Definition 的 Workload 不在当前活动 Scope 中。 ");
    return;
  }
  selectedResourceID.value = resource.id;
  selectedNamespace.value = resource.namespace ?? "";
  mode.value = definition.mode;
  guidedKey.value = definition.catalog_key ?? guidedKey.value;
  expertQuery.value = definition.query;
  activeDefinitionID.value = definition.id;
  currentExecution.value = null;
  await loadCatalog();
  if (updateRoute) await syncRoute({ definition: definition.id, execution: undefined });
}

async function authorizeOnce() {
  const execution = currentExecution.value;
  if (!execution || execution.status !== "succeeded") return;
  authorizationBusy.value = execution.id;
  try {
    const authorization = await createQueryAuthorization({ mode: "run_once", query_execution_id: execution.id });
    authorizations.value.unshift(authorization);
    selectedManagementTab.value = "authorizations";
    statusMessage.value = "已创建精确的一次性 Agent 查询授权。";
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Agent 查询授权创建失败。 ");
  } finally {
    authorizationBusy.value = "";
  }
}

async function authorizeDefinition(definition: QueryDefinition) {
  authorizationBusy.value = definition.id;
  try {
    const authorization = await createQueryAuthorization({ mode: "definition", query_definition_id: definition.id });
    authorizations.value.unshift(authorization);
    selectedManagementTab.value = "authorizations";
    statusMessage.value = `已授权 Agent 使用 ${definition.title} revision ${definition.revision}。`;
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Definition 授权创建失败。 ");
  } finally {
    authorizationBusy.value = "";
  }
}

async function revokeAuthorization(item: QueryAuthorization) {
  if (item.revoked_at || !window.confirm("撤销这条 Agent 查询授权？")) return;
  authorizationBusy.value = item.id;
  try {
    await revokeQueryAuthorization(item.id);
    authorizations.value = authorizations.value.map((candidate) => candidate.id === item.id
      ? { ...candidate, revoked_at: new Date().toISOString() }
      : candidate);
    statusMessage.value = "Agent 查询授权已撤销。";
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Agent 查询授权撤销失败。 ");
  } finally {
    authorizationBusy.value = "";
  }
}

async function copyQuery() {
  const query = currentExecution.value?.query || expertQuery.value;
  if (!query) return;
  try {
    await navigator.clipboard.writeText(query);
    statusMessage.value = "PromQL 已复制。";
  } catch {
    queryError.value = normalizeFailure(null, "浏览器未允许复制 PromQL。 ");
  }
}

function linkTarget(link?: MonitoringContextLink): "_blank" | "_self" {
  return link?.target === "external" ? "_blank" : "_self";
}

function receiveScopeChange() {
  currentExecution.value = null;
  void loadWorkspace();
}

onMounted(() => {
  window.addEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, receiveScopeChange);
  void loadWorkspace();
});

onBeforeUnmount(() => {
  mounted = false;
  requestGeneration += 1;
  window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, receiveScopeChange);
});
</script>

<template>
  <section class="monitoring-workspace" aria-labelledby="monitoring-heading">
    <header class="workspace-heading">
      <div>
        <div class="heading-line">
          <Activity :size="20" aria-hidden="true" />
          <h1 id="monitoring-heading">监控</h1>
          <span class="provider-state" :data-state="catalog?.provider_state ?? 'checking'">
            <span aria-hidden="true" />
            Prometheus {{ providerStateLabel(catalog?.provider_state) }}
          </span>
        </div>
        <p>
          {{ bootstrap?.active_scope.cluster_id || "活动集群" }}
          <span aria-hidden="true">/</span>
          {{ selectedNamespace || "Namespace" }}
          <span v-if="selectedResource">/ {{ selectedResource.kind }} {{ selectedResource.name }}</span>
        </p>
      </div>
      <button class="icon-button" type="button" title="刷新监控数据" aria-label="刷新监控数据" :disabled="loading || catalogLoading" @click="refreshAll">
        <RefreshCw :size="18" :class="{ spinning: loading || catalogLoading }" aria-hidden="true" />
      </button>
    </header>

    <div v-if="pageError" class="notice notice--error" role="alert">
      <TriangleAlert :size="18" aria-hidden="true" />
      <div>
        <strong>{{ pageError.code }}</strong>
        <span>{{ pageError.message }}</span>
      </div>
    </div>
    <div v-if="queryError" class="notice notice--error" role="alert" aria-live="assertive">
      <TriangleAlert :size="18" aria-hidden="true" />
      <div>
        <strong>{{ queryError.code }}</strong>
        <span>{{ queryError.message }}</span>
        <span v-for="step in queryError.nextSteps" :key="step">{{ step }}</span>
        <small v-if="queryError.requestID">Request {{ queryError.requestID }} · Trace {{ queryError.traceID || "无" }}</small>
      </div>
    </div>
    <div v-if="statusMessage" class="notice notice--success" role="status" aria-live="polite">
      <CheckCircle2 :size="18" aria-hidden="true" />
      <span>{{ statusMessage }}</span>
    </div>

    <div v-if="loading" class="workspace-loading" role="status">
      <LoaderCircle :size="22" class="spinning" aria-hidden="true" />
      <span>正在读取活动 Scope 与真实 Workload…</span>
    </div>

    <template v-else>
      <section class="query-band" aria-label="监控查询">
        <div class="context-controls">
          <label>
            <span>Namespace</span>
            <select v-model="selectedNamespace" name="monitoring-namespace" autocomplete="off" @change="changeNamespace">
              <option v-for="namespace in namespaces" :key="namespace" :value="namespace">{{ namespace }}</option>
            </select>
          </label>
          <label class="workload-control">
            <span>Workload</span>
            <select v-model="selectedResourceID" name="monitoring-workload" autocomplete="off" @change="changeResource">
              <option v-for="resource in namespaceWorkloads" :key="resource.id" :value="resource.id">
                {{ resource.kind }} · {{ resource.name }}
              </option>
            </select>
          </label>
          <div class="field-group">
            <span>查询模式</span>
            <div class="segmented-control" role="group" aria-label="查询模式">
              <button type="button" :aria-pressed="mode === 'guided'" @click="changeMode('guided')">引导</button>
              <button type="button" :aria-pressed="mode === 'expert'" @click="changeMode('expert')">Expert</button>
            </div>
          </div>
        </div>

        <div class="query-editor">
          <label v-if="mode === 'guided'" class="query-field">
            <span>指标视图</span>
            <select v-model="guidedKey" name="guided-query" autocomplete="off" @change="selectGuidedQuery">
              <option v-for="entry in catalog?.queries ?? []" :key="entry.key" :value="entry.key">{{ entry.title }}</option>
            </select>
            <small>{{ selectedCatalogEntry?.description || "当前 Scope 没有可用的引导查询。" }}</small>
          </label>
          <label v-else class="query-field expert-field">
            <span>PromQL <b>{{ queryByteLength }} / 8192 bytes</b></span>
            <textarea v-model="expertQuery" name="expert-promql" rows="4" spellcheck="false" @input="clearDefinitionBinding" />
          </label>

          <div class="time-controls">
            <div class="preset-control" role="group" aria-label="时间范围快捷选择">
              <button type="button" @click="selectPreset(15)">15m</button>
              <button type="button" @click="selectPreset(60)">1h</button>
              <button type="button" @click="selectPreset(360)">6h</button>
            </div>
            <label>
              <span>开始</span>
              <input v-model="fromValue" type="datetime-local" name="monitoring-from" @change="clearDefinitionBinding" />
            </label>
            <label>
              <span>结束</span>
              <input v-model="toValue" type="datetime-local" name="monitoring-to" @change="clearDefinitionBinding" />
            </label>
            <label class="step-control">
              <span>Step</span>
              <select v-model.number="stepSeconds" name="monitoring-step" autocomplete="off" @change="clearDefinitionBinding">
                <option :value="15">15s</option>
                <option :value="30">30s</option>
                <option :value="60">1m</option>
                <option :value="300">5m</option>
              </select>
            </label>
          </div>
        </div>

        <div class="query-actions">
          <div class="bound-summary" aria-label="查询边界">
            <span>Lookback ≤ {{ Math.round((catalog?.bounds.max_lookback_seconds ?? 0) / 3600) }}h</span>
            <span>Series ≤ {{ catalog?.bounds.max_series ?? 0 }}</span>
            <span>Samples ≤ {{ catalog?.bounds.max_samples ?? 0 }}</span>
            <span>Timeout {{ catalog?.bounds.timeout_ms ?? 0 }}ms</span>
          </div>
          <button v-if="queryInFlight" class="command-button command-button--danger" type="button" @click="stopQuery">
            <Square :size="17" aria-hidden="true" />
            取消
          </button>
          <button class="command-button command-button--primary" type="button" :disabled="!canRun" @click="runQuery">
            <LoaderCircle v-if="queryRunning" :size="17" class="spinning" aria-hidden="true" />
            <Play v-else :size="17" aria-hidden="true" />
            执行查询
          </button>
        </div>

        <div v-if="catalog && !providerReady" class="provider-unavailable" role="status">
          <Ban :size="18" aria-hidden="true" />
          <div>
            <strong>Prometheus {{ providerStateLabel(catalog.provider_state) }}</strong>
            <span>{{ catalog.provider_detail }}</span>
            <small>{{ catalog.source.identity || "当前 Configuration Revision 没有可用的采集端点" }}</small>
          </div>
        </div>
      </section>

      <div class="workspace-grid">
        <main class="result-column">
          <section class="result-header" aria-labelledby="query-result-heading">
            <div>
              <span class="section-kicker">Query Execution</span>
              <h2 id="query-result-heading">查询结果</h2>
            </div>
            <div v-if="currentExecution" class="result-actions">
              <span class="execution-status" :data-status="currentExecution.status">
                <LoaderCircle v-if="queryInFlight" :size="15" class="spinning" aria-hidden="true" />
                {{ statusLabel(currentExecution.status) }}
              </span>
              <button class="icon-button" type="button" title="复制 PromQL" aria-label="复制 PromQL" @click="copyQuery">
                <Clipboard :size="17" aria-hidden="true" />
              </button>
              <button class="command-button" type="button" :disabled="currentExecution.status !== 'succeeded'" @click="openSaveDialog">
                <Save :size="16" aria-hidden="true" />
                保存定义
              </button>
              <button class="command-button" type="button" :disabled="currentExecution.status !== 'succeeded' || authorizationBusy === currentExecution.id" @click="authorizeOnce">
                <ShieldCheck :size="16" aria-hidden="true" />
                授权一次
              </button>
              <a
                v-if="providerLink?.availability === 'available'"
                class="command-button"
                :href="providerLink.href"
                :target="linkTarget(providerLink)"
                rel="noopener noreferrer"
              >
                <ExternalLink :size="16" aria-hidden="true" />
                Grafana
              </a>
            </div>
          </section>

          <div v-if="!currentExecution" class="empty-result">
            <LineChart :size="30" aria-hidden="true" />
            <strong>尚无查询结果</strong>
          </div>

          <template v-else>
            <div class="execution-meta">
              <span><b>{{ currentExecution.series_count }}</b> series</span>
              <span><b>{{ currentExecution.sample_count }}</b> samples</span>
              <span><b>{{ formatBytes(currentExecution.response_bytes) }}</b></span>
              <span>Revision <b>{{ shortHash(currentExecution.configuration_revision_id) }}</b></span>
              <span>采集 {{ formatTime(currentExecution.source.collected_at) }}</span>
              <span v-if="currentExecution.partial" class="meta-warning">部分结果</span>
              <span v-if="currentExecution.truncated" class="meta-warning">已截断</span>
            </div>

            <div v-if="currentExecution.status === 'failed'" class="notice notice--error" role="alert">
              <TriangleAlert :size="18" aria-hidden="true" />
              <div>
                <strong>{{ currentExecution.error_code || "QUERY_FAILED" }}</strong>
                <span>{{ currentExecution.error_detail || "Prometheus 查询失败。" }}</span>
              </div>
            </div>
            <div v-else-if="currentExecution.result_expired" class="notice notice--warning" role="status">
              <History :size="18" aria-hidden="true" />
              <span>该执行的完整遥测结果未长期保留；审计元数据仍可用。</span>
            </div>

            <section v-if="chartSeries.length" class="chart-section" aria-labelledby="chart-heading">
              <div class="section-title-row">
                <div>
                  <LineChart :size="18" aria-hidden="true" />
                  <h3 id="chart-heading">时序图</h3>
                </div>
                <span>{{ currentExecution.result?.result_type }}</span>
              </div>
              <div class="chart-frame">
                <svg viewBox="0 0 900 310" role="img" :aria-label="`${chartSeries.length} 条真实 Prometheus 时序`" preserveAspectRatio="none">
                  <g class="chart-grid" aria-hidden="true">
                    <line v-for="index in 5" :key="`h-${index}`" x1="68" x2="872" :y1="20 + (index - 1) * 62.5" :y2="20 + (index - 1) * 62.5" />
                    <line v-for="index in 5" :key="`v-${index}`" :x1="68 + (index - 1) * 201" :x2="68 + (index - 1) * 201" y1="20" y2="270" />
                  </g>
                  <g v-if="chartDomain" class="chart-axis" aria-hidden="true">
                    <text x="60" y="27" text-anchor="end">{{ numberFormatter.format(chartDomain.maxValue) }}</text>
                    <text x="60" y="274" text-anchor="end">{{ numberFormatter.format(chartDomain.minValue) }}</text>
                    <text x="68" y="297">{{ chartTime(chartDomain.minTime) }}</text>
                    <text x="872" y="297" text-anchor="end">{{ chartTime(chartDomain.maxTime) }}</text>
                  </g>
                  <path
                    v-for="(series, index) in chartSeries"
                    :key="tableRows[index]?.key"
                    class="series-path"
                    :stroke="chartColors[index % chartColors.length]"
                    :d="seriesPath(series)"
                    vector-effect="non-scaling-stroke"
                  />
                </svg>
              </div>
              <div class="chart-legend" aria-label="时序图图例">
                <span v-for="(row, index) in tableRows" :key="row.key">
                  <i :style="{ backgroundColor: chartColors[index % chartColors.length] }" aria-hidden="true" />
                  {{ row.label }}
                </span>
              </div>
            </section>

            <section v-if="tableRows.length" class="table-section" aria-labelledby="table-heading">
              <div class="section-title-row">
                <div>
                  <TableProperties :size="18" aria-hidden="true" />
                  <h3 id="table-heading">序列表</h3>
                </div>
                <span>{{ tableRows.length }} rows</span>
              </div>
              <div class="result-table-wrap">
                <table>
                  <thead>
                    <tr><th>Labels</th><th>最新值</th><th>时间</th><th>Samples</th></tr>
                  </thead>
                  <tbody>
                    <tr v-for="row in tableRows" :key="row.key">
                      <td class="series-label">{{ row.label }}</td>
                      <td class="numeric-cell">{{ row.latestValue === null ? "无" : numberFormatter.format(row.latestValue) }}</td>
                      <td>{{ formatTime(row.latestAt) }}</td>
                      <td class="numeric-cell">{{ row.series.points.length }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </section>

            <section class="audit-section" aria-labelledby="audit-heading">
              <div class="section-title-row">
                <div>
                  <History :size="18" aria-hidden="true" />
                  <h3 id="audit-heading">执行审计</h3>
                </div>
                <span>{{ currentExecution.id }}</span>
              </div>
              <ol>
                <li v-for="event in currentExecution.events" :key="event.id">
                  <span>{{ event.type }}</span>
                  <small>{{ event.actor }} · {{ formatTime(event.created_at) }}</small>
                  <p v-if="event.detail">{{ event.detail }}</p>
                </li>
              </ol>
            </section>
          </template>
        </main>

        <aside class="history-column" aria-labelledby="history-heading">
          <div class="section-title-row">
            <div>
              <History :size="18" aria-hidden="true" />
              <h2 id="history-heading">查询历史</h2>
            </div>
            <span>{{ historyItems.length }}</span>
          </div>
          <div v-if="!historyItems.length" class="history-empty">当前 Workload 暂无执行记录</div>
          <button
            v-for="item in historyItems"
            :key="item.id"
            class="history-item"
            :class="{ active: currentExecution?.id === item.id }"
            type="button"
            @click="openExecution(item.id)"
          >
            <span class="history-item-top">
              <b>{{ item.mode === "guided" ? "引导" : "Expert" }}</b>
              <i :data-status="item.status">{{ statusLabel(item.status) }}</i>
            </span>
            <span>{{ item.catalog_key || item.query }}</span>
            <small>{{ formatTime(item.created_at) }} · {{ item.actor }}</small>
          </button>
        </aside>
      </div>

      <section class="management-section" aria-labelledby="management-heading">
        <div class="management-heading-row">
          <div>
            <BookMarked :size="19" aria-hidden="true" />
            <h2 id="management-heading">查询资产</h2>
          </div>
          <div class="segmented-control" role="tablist" aria-label="查询资产视图">
            <button type="button" role="tab" :aria-selected="selectedManagementTab === 'definitions'" :aria-pressed="selectedManagementTab === 'definitions'" @click="selectedManagementTab = 'definitions'">已保存</button>
            <button type="button" role="tab" :aria-selected="selectedManagementTab === 'authorizations'" :aria-pressed="selectedManagementTab === 'authorizations'" @click="selectedManagementTab = 'authorizations'">Agent 授权</button>
          </div>
        </div>

        <div v-if="selectedManagementTab === 'definitions'" class="asset-list" role="tabpanel">
          <div v-if="!definitions.length" class="asset-empty">暂无 Query Definition</div>
          <article v-for="definition in definitions" :key="definition.id" class="asset-row">
            <div>
              <strong>{{ definition.title }}</strong>
              <span>{{ definition.resource.name }} · revision {{ definition.revision }} · {{ definition.mode }}</span>
              <code>{{ definition.query }}</code>
            </div>
            <div class="asset-actions">
              <button class="command-button" type="button" @click="useDefinition(definition)">
                <Undo2 :size="16" aria-hidden="true" />
                载入
              </button>
              <button class="command-button" type="button" :disabled="authorizationBusy === definition.id" @click="authorizeDefinition(definition)">
                <ShieldCheck :size="16" aria-hidden="true" />
                授权 Agent
              </button>
            </div>
          </article>
        </div>

        <div v-else class="asset-list" role="tabpanel">
          <div v-if="!authorizations.length" class="asset-empty">暂无 Agent Query Authorization</div>
          <article v-for="authorization in authorizations" :key="authorization.id" class="asset-row authorization-row">
            <div>
              <strong>{{ authorization.mode === "run_once" ? "一次性精确查询" : "Query Definition 授权" }}</strong>
              <span>{{ authorization.resource.name }} · {{ authorizationState(authorization) }} · {{ authorization.query_mode }}</span>
              <code>{{ authorization.query_hash }}</code>
            </div>
            <button class="command-button command-button--danger" type="button" :disabled="Boolean(authorization.revoked_at) || authorizationBusy === authorization.id" @click="revokeAuthorization(authorization)">
              <Ban :size="16" aria-hidden="true" />
              撤销
            </button>
          </article>
        </div>
      </section>
    </template>
  </section>

  <el-dialog v-model="saveDialogOpen" title="保存 Query Definition" width="min(520px, calc(100vw - 32px))" append-to-body>
    <form class="save-form" @submit.prevent="saveDefinition">
      <label>
        <span>名称</span>
        <input v-model="saveTitle" name="query-definition-title" autocomplete="off" required maxlength="128" />
      </label>
      <label>
        <span>说明</span>
        <textarea v-model="saveDescription" name="query-definition-description" rows="3" maxlength="512" />
      </label>
      <div class="dialog-actions">
        <button class="command-button" type="button" @click="saveDialogOpen = false">取消</button>
        <button class="command-button command-button--primary" type="submit" :disabled="saving || !saveTitle.trim()">
          <LoaderCircle v-if="saving" :size="16" class="spinning" aria-hidden="true" />
          <Save v-else :size="16" aria-hidden="true" />
          保存
        </button>
      </div>
    </form>
  </el-dialog>
</template>

<style scoped>
.monitoring-workspace {
  width: min(100%, 1680px);
  margin: 0 auto;
  padding: 24px clamp(16px, 2.5vw, 36px) 56px;
}

.workspace-heading,
.heading-line,
.query-actions,
.result-header,
.result-actions,
.section-title-row,
.section-title-row > div,
.management-heading-row,
.management-heading-row > div,
.asset-actions,
.dialog-actions {
  display: flex;
  align-items: center;
}

.workspace-heading {
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 20px;
}

.heading-line {
  gap: 10px;
}

.heading-line h1,
.section-title-row h2,
.section-title-row h3,
.management-heading-row h2 {
  margin: 0;
}

.heading-line h1 {
  font-size: 24px;
  line-height: 1.2;
}

.workspace-heading p {
  margin: 7px 0 0;
  color: var(--co-text-secondary);
  font-size: 13px;
}

.provider-state {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-height: 28px;
  padding: 3px 9px;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
  color: var(--co-text-secondary);
  font-size: 12px;
}

.provider-state > span {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--co-text-muted);
}

.provider-state[data-state="available"] > span { background: #2da44e; }
.provider-state[data-state="partial"] > span { background: #d29922; }
.provider-state[data-state="unavailable"] > span,
.provider-state[data-state="disabled"] > span { background: #cf4a32; }

.icon-button,
.command-button,
.segmented-control button,
.preset-control button,
.history-item {
  border: 1px solid var(--co-border-default);
  color: var(--co-text-primary);
  background: var(--co-bg-surface);
  cursor: pointer;
}

.icon-button {
  display: inline-grid;
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  place-items: center;
  padding: 0;
  border-radius: var(--co-radius-control);
}

.icon-button:hover,
.command-button:hover:not(:disabled),
.segmented-control button:hover,
.preset-control button:hover,
.history-item:hover {
  border-color: var(--co-border-strong);
  background: var(--co-bg-hover);
}

.command-button {
  display: inline-flex;
  min-height: 40px;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 7px 13px;
  border-radius: var(--co-radius-control);
  font-weight: 650;
  font-size: 13px;
}

.command-button--primary {
  border-color: var(--co-action-primary);
  background: var(--co-action-primary);
  color: var(--co-text-on-action);
}

.command-button--danger { color: var(--co-status-critical-fg); }

button:disabled,
a[aria-disabled="true"] {
  cursor: not-allowed;
  opacity: 0.5;
}

.notice {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin: 0 0 14px;
  padding: 11px 13px;
  border: 1px solid var(--co-border-default);
  border-left-width: 3px;
  border-radius: var(--co-radius-control);
  background: var(--co-bg-surface);
  font-size: 13px;
}

.notice > div {
  display: grid;
  gap: 2px;
}

.notice strong { display: block; }
.notice small { color: var(--co-text-muted); }
.notice--error { border-left-color: var(--co-status-critical-fg); }
.notice--error > svg { color: var(--co-status-critical-fg); }
.notice--success { border-left-color: var(--co-status-success-fg); }
.notice--success > svg { color: var(--co-status-success-fg); }
.notice--warning { border-left-color: var(--co-status-warning-fg); }

.workspace-loading,
.empty-result {
  display: grid;
  min-height: 280px;
  place-content: center;
  justify-items: center;
  gap: 10px;
  color: var(--co-text-secondary);
}

.query-band {
  border-block: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}

.context-controls,
.query-editor,
.time-controls {
  display: grid;
  align-items: end;
  gap: 12px;
}

.context-controls {
  grid-template-columns: minmax(150px, 0.7fr) minmax(240px, 1.4fr) auto;
  padding: 16px;
  border-bottom: 1px solid var(--co-border-default);
}

.query-editor {
  grid-template-columns: minmax(280px, 1fr) minmax(540px, 1.7fr);
  padding: 16px;
}

.time-controls {
  grid-template-columns: auto minmax(160px, 1fr) minmax(160px, 1fr) minmax(90px, 0.5fr);
}

label,
.field-group,
.query-field {
  display: grid;
  gap: 6px;
  min-width: 0;
}

label > span,
.field-group > span,
.query-field > span {
  color: var(--co-text-secondary);
  font-size: 12px;
  font-weight: 650;
}

.query-field > span {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.query-field b {
  color: var(--co-text-muted);
  font-weight: 500;
}

select,
input,
textarea {
  width: 100%;
  min-height: 40px;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
  color: var(--co-text-primary);
  background: var(--co-bg-canvas);
}

select,
input { padding: 7px 10px; }
textarea {
  resize: vertical;
  padding: 10px 12px;
  font-family: var(--co-font-mono);
  line-height: 1.55;
}

.query-field small { color: var(--co-text-muted); }

.segmented-control,
.preset-control {
  display: inline-grid;
  grid-auto-flow: column;
  grid-auto-columns: minmax(56px, 1fr);
  overflow: hidden;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
}

.segmented-control button,
.preset-control button {
  min-height: 40px;
  padding: 6px 12px;
  border: 0;
  border-right: 1px solid var(--co-border-default);
  font-size: 13px;
}

.segmented-control button:last-child,
.preset-control button:last-child { border-right: 0; }

.segmented-control button[aria-pressed="true"] {
  background: var(--co-action-primary);
  color: var(--co-text-on-action);
}

.preset-control button { min-width: 48px; }

.query-actions {
  justify-content: flex-end;
  gap: 10px;
  padding: 12px 16px 16px;
}

.bound-summary {
  display: flex;
  flex: 1;
  flex-wrap: wrap;
  gap: 6px 14px;
  color: var(--co-text-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.provider-unavailable {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 13px 16px;
  border-top: 1px solid var(--co-border-default);
  color: var(--co-status-critical-fg);
}

.provider-unavailable > div { display: grid; }
.provider-unavailable span,
.provider-unavailable small { color: var(--co-text-secondary); }

.workspace-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(260px, 320px);
  gap: 24px;
  margin-top: 24px;
}

.result-column,
.history-column,
.management-section {
  min-width: 0;
  border-top: 1px solid var(--co-border-default);
}

.result-header,
.section-title-row,
.management-heading-row {
  min-height: 58px;
  justify-content: space-between;
  gap: 14px;
  padding: 10px 0;
}

.section-kicker {
  display: block;
  color: var(--co-text-muted);
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
}

.result-header h2,
.section-title-row h2,
.management-heading-row h2 {
  font-size: 17px;
}

.section-title-row h3 { font-size: 15px; }
.section-title-row > div,
.management-heading-row > div { gap: 8px; }
.section-title-row > span { color: var(--co-text-muted); font-size: 12px; }

.result-actions {
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}

.execution-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 30px;
  padding: 3px 9px;
  border-radius: var(--co-radius-control);
  background: var(--co-bg-subtle);
  color: var(--co-text-secondary);
  font-size: 12px;
  font-weight: 700;
}

.execution-status[data-status="succeeded"] { color: var(--co-status-success-fg); }
.execution-status[data-status="failed"],
.execution-status[data-status="cancelled"] { color: var(--co-status-critical-fg); }

.execution-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 20px;
  padding: 10px 0 14px;
  border-bottom: 1px solid var(--co-border-default);
  color: var(--co-text-secondary);
  font-size: 12px;
}

.meta-warning { color: var(--co-status-warning-fg); font-weight: 700; }

.chart-section,
.table-section,
.audit-section {
  padding: 14px 0 20px;
  border-bottom: 1px solid var(--co-border-default);
}

.chart-frame {
  width: 100%;
  aspect-ratio: 2.9 / 1;
  min-height: 260px;
  max-height: 390px;
  overflow: hidden;
  border: 1px solid var(--co-border-default);
  background: var(--co-bg-canvas);
}

.chart-frame svg { width: 100%; height: 100%; }
.chart-grid line { stroke: var(--co-border-default); stroke-width: 1; }
.chart-axis text { fill: var(--co-text-muted); font-size: 11px; }
.series-path { fill: none; stroke-width: 2; stroke-linejoin: round; stroke-linecap: round; }

.chart-legend {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
  padding-top: 10px;
  color: var(--co-text-secondary);
  font-size: 11px;
}

.chart-legend span {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 7px;
  overflow-wrap: anywhere;
}

.chart-legend i { width: 18px; height: 3px; flex: 0 0 18px; }

.result-table-wrap { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; font-size: 12px; }
th, td { padding: 10px 9px; border-bottom: 1px solid var(--co-border-default); text-align: left; }
th { color: var(--co-text-muted); font-weight: 650; }
.series-label { max-width: 520px; overflow-wrap: anywhere; font-family: var(--co-font-mono); }
.numeric-cell { text-align: right; font-variant-numeric: tabular-nums; }

.audit-section ol {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.audit-section li {
  min-width: 0;
  padding: 10px 12px;
  border-left: 2px solid var(--co-border-strong);
  background: var(--co-bg-surface);
}

.audit-section li span,
.audit-section li small { display: block; }
.audit-section li span { font-weight: 700; }
.audit-section li small,
.audit-section li p { color: var(--co-text-muted); font-size: 11px; }
.audit-section li p { margin: 5px 0 0; }

.history-column {
  align-self: start;
  max-height: 760px;
  overflow-y: auto;
}

.history-empty,
.asset-empty {
  padding: 24px 4px;
  color: var(--co-text-muted);
  font-size: 13px;
}

.history-item {
  display: grid;
  width: 100%;
  gap: 7px;
  padding: 11px 12px;
  border-width: 0 0 1px;
  text-align: left;
}

.history-item.active {
  border-left: 3px solid var(--co-action-primary);
  background: var(--co-bg-subtle);
}

.history-item-top {
  display: flex;
  justify-content: space-between;
  gap: 10px;
}

.history-item-top i { color: var(--co-text-muted); font-size: 11px; font-style: normal; }
.history-item-top i[data-status="succeeded"] { color: var(--co-status-success-fg); }
.history-item-top i[data-status="failed"] { color: var(--co-status-critical-fg); }
.history-item > span:not(.history-item-top) { overflow: hidden; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.history-item small { color: var(--co-text-muted); }

.management-section { margin-top: 28px; }
.management-heading-row { flex-wrap: wrap; }
.asset-list { border-top: 1px solid var(--co-border-default); }
.asset-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 18px;
  padding: 14px 4px;
  border-bottom: 1px solid var(--co-border-default);
}

.asset-row > div:first-child { display: grid; min-width: 0; gap: 3px; }
.asset-row span { color: var(--co-text-secondary); font-size: 12px; }
.asset-row code { overflow: hidden; color: var(--co-text-muted); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.asset-actions { gap: 8px; }

.save-form { display: grid; gap: 16px; }
.dialog-actions { justify-content: flex-end; gap: 8px; padding-top: 4px; }

.spinning { animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

@media (max-width: 1180px) {
  .query-editor { grid-template-columns: 1fr; }
  .workspace-grid { grid-template-columns: minmax(0, 1fr) 280px; }
}

@media (max-width: 920px) {
  .context-controls { grid-template-columns: 1fr 1.5fr; }
  .field-group { grid-column: 1 / -1; }
  .workspace-grid { grid-template-columns: 1fr; }
  .history-column { max-height: 420px; }
  .chart-frame { min-height: 230px; }
}

@media (max-width: 700px) {
  .monitoring-workspace { padding: 18px 14px 104px; }
  .heading-line { flex-wrap: wrap; }
  .context-controls,
  .query-editor { grid-template-columns: 1fr; padding: 14px 12px; }
  .time-controls { grid-template-columns: 1fr 1fr; }
  .preset-control { grid-column: 1 / -1; }
  .query-actions { align-items: stretch; flex-direction: column; }
  .bound-summary { order: 3; }
  .query-actions .command-button { width: 100%; }
  .result-header { align-items: flex-start; flex-direction: column; }
  .result-actions { width: 100%; justify-content: flex-start; }
  .chart-frame { min-height: 210px; aspect-ratio: 1.65 / 1; }
  .asset-row { grid-template-columns: 1fr; }
  .asset-actions { flex-wrap: wrap; }
  .authorization-row > button { width: 100%; }
}

@media (max-width: 420px) {
  .time-controls { grid-template-columns: 1fr; }
  .preset-control { grid-column: auto; }
  .workspace-heading { align-items: flex-start; }
  .provider-state { max-width: 100%; }
  .result-actions .command-button { flex: 1 1 130px; }
  .result-actions .icon-button { flex: 0 0 40px; }
}
</style>
