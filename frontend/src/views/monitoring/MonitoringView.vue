<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
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
} from "../../api/monitoring";
import { getBootstrap, type BootstrapSnapshot } from "../../api/platform";
import MonitoringAssets from "../../components/monitoring/MonitoringAssets.vue";
import MonitoringDialogs, {
  type MonitoringConfirmation,
  type MonitoringConfirmationKind,
} from "../../components/monitoring/MonitoringDialogs.vue";
import MonitoringHistory from "../../components/monitoring/MonitoringHistory.vue";
import MonitoringQueryControls from "../../components/monitoring/MonitoringQueryControls.vue";
import MonitoringResult from "../../components/monitoring/MonitoringResult.vue";
import WorkspacePageFrame from "../../components/workspace/WorkspacePageFrame.vue";
import {
  buildMonitoringRouteQuery,
  parseMonitoringRoute,
} from "../../components/monitoring/monitoringRoute";
import { safeExternalURL } from "../../models/workbench";
import {
  openAgentPanel,
  publishAgentContext,
  type AgentPageContext,
} from "../../utils/agentContext";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";

interface RequestFailure {
  message: string;
  code: string;
  requestID: string;
  traceID: string;
  nextSteps: readonly string[];
}

type ConfirmationTarget =
  | { kind: "authorize-once"; execution: QueryExecution }
  | { kind: "authorize-definition"; definition: QueryDefinition }
  | { kind: "revoke"; authorization: QueryAuthorization };

const route = useRoute();
const router = useRouter();
const initialRoute = parseMonitoringRoute(route.query);
const bootstrap = ref<BootstrapSnapshot | null>(null);
const workloads = ref<KubernetesResource[]>([]);
const catalog = ref<MonitoringCatalog | null>(null);
const historyItems = ref<QueryExecution[]>([]);
const definitions = ref<QueryDefinition[]>([]);
const authorizations = ref<QueryAuthorization[]>([]);
const currentExecution = ref<QueryExecution | null>(null);
const selectedResourceID = ref(initialRoute.resource);
const selectedNamespace = ref(initialRoute.namespace);
const mode = ref<QueryMode>(initialRoute.mode);
const guidedKey = ref(initialRoute.metric);
const expertQuery = ref(initialRoute.promql);
const fromValue = ref(toLocalInput(new Date(Date.now() - 15 * 60_000)));
const toValue = ref(toLocalInput(new Date()));
const stepSeconds = ref(30);
const activeDefinitionID = ref("");
const cursorTimestamp = ref<number | null>(null);
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
const confirmationTarget = ref<ConfirmationTarget | null>(null);
let mounted = true;
let workspaceGeneration = 0;
let queryGeneration = 0;
let workspaceController: AbortController | undefined;
let queryController: AbortController | undefined;

const terminalStatuses = new Set<QueryExecution["status"]>(["succeeded", "failed", "cancelled"]);
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
const canRun = computed(() => Boolean(
  selectedResource.value
  && providerReady.value
  && !queryRunning.value
  && !queryInFlight.value
  && validTimeRange.value
  && (mode.value === "guided" ? guidedKey.value : expertQuery.value.trim()),
));
const compactQueryErrorMessage = computed(() => {
  const error = queryError.value;
  if (!error) return "";
  return /network error/i.test(error.message)
    ? "网络请求未完成，请检查 Prometheus 连接后重试。"
    : error.message;
});
const queryErrorReason = computed(() => {
  const message = queryError.value?.message ?? "";
  return /network error/i.test(message) ? message : "";
});
const providerLink = computed(() => {
  const link = currentExecution.value?.links.find((item) => item.kind === "provider" && item.provider === "grafana");
  const href = safeExternalURL(link?.href);
  return link && href ? { ...link, href } : undefined;
});
const confirmation = computed<MonitoringConfirmation | null>(() => {
  const target = confirmationTarget.value;
  if (!target) return null;
  if (target.kind === "authorize-once") {
    return {
      kind: target.kind,
      title: "授权一次精确查询",
      description: "服务端将授权 Agent 使用这次成功执行的精确 query hash 一次。",
      target: `${target.execution.id} / ${target.execution.query_hash}`,
      effect: "只允许消费一次；后续查询变更不会继承此授权。",
      authority: `Configuration Revision ${target.execution.configuration_revision_id}`,
      confirmLabel: "创建一次性授权",
    };
  }
  if (target.kind === "authorize-definition") {
    return {
      kind: target.kind,
      title: "授权 Query Definition",
      description: "服务端将按已保存的 revision 与边界授权 Agent 复用该定义。",
      target: `${target.definition.title} / revision ${target.definition.revision}`,
      effect: "授权持续作用于该定义身份，直到明确撤销；不会授权未保存的查询变更。",
      authority: `${target.definition.id} / ${target.definition.content_hash}`,
      confirmLabel: "授权此 revision",
    };
  }
  return {
    kind: target.kind,
    title: "撤销 Agent 查询授权",
    description: "撤销后 Agent 不能再发起新的授权查询；已运行的执行不会被取消。",
    target: `${target.authorization.id} / ${target.authorization.query_hash}`,
    effect: "阻止后续消费；该审计记录仍会保留，撤销不能通过前端恢复。",
    authority: `${target.authorization.configuration_revision_id} / ${target.authorization.mode}`,
    confirmLabel: "确认撤销",
  };
});

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

function providerStateLabel(state?: MonitoringCatalog["provider_state"]): string {
  return ({ available: "可用", partial: "部分可用", unavailable: "不可用", disabled: "已停用" } as Record<string, string>)[state ?? ""] ?? "检查中";
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

function currentAgentContext(): AgentPageContext | null {
  const execution = currentExecution.value;
  if (!execution || execution.status !== "succeeded" || execution.result_expired) return null;
  return {
    route: route.fullPath,
    input: {
      title: `${execution.resource.name} Monitoring 查询上下文`,
      cluster_id: execution.scope.cluster_id,
      environment: execution.scope.environment,
      namespaces: [...execution.scope.namespaces],
      resource_refs: [execution.resource],
      filters: {
        workspace: "monitoring",
        provider: execution.provider,
        mode: execution.mode,
        query: execution.query,
        query_hash: execution.query_hash,
      },
      from: execution.time_range.from,
      to: execution.time_range.to,
      query_definition_refs: execution.query_definition_id ? [execution.query_definition_id] : [],
      query_execution_refs: [execution.id],
      evidence_refs: [],
    },
  };
}

function publishCurrentAgentContext() {
  publishAgentContext(currentAgentContext());
}

function routeState(execution = currentExecution.value?.id ?? "") {
  return {
    cluster: bootstrap.value?.active_scope.cluster_id ?? "",
    namespace: selectedNamespace.value,
    resource: selectedResourceID.value,
    mode: mode.value,
    metric: guidedKey.value,
    promql: expertQuery.value,
    from: validTimeRange.value ? new Date(fromValue.value).toISOString() : "",
    to: validTimeRange.value ? new Date(toValue.value).toISOString() : "",
    execution,
    definition: activeDefinitionID.value,
  };
}

async function syncRoute(overrides: Partial<{ execution: string; definition: string }> = {}) {
  const state = routeState(overrides.execution ?? currentExecution.value?.id ?? "");
  if (overrides.definition !== undefined) state.definition = overrides.definition;
  await router.replace({ path: "/monitoring", query: buildMonitoringRouteQuery(state) });
}

async function loadCatalog(signal?: AbortSignal) {
  const context = monitoringContext();
  if (!context) {
    catalog.value = null;
    historyItems.value = [];
    return;
  }
  catalogLoading.value = true;
  try {
    const [nextCatalog, nextHistory] = await Promise.all([
      getMonitoringCatalog(context, signal),
      getMonitoringQueries({
        cluster_id: context.cluster_id,
        namespace: context.namespace,
        resource_id: context.resource.id,
        limit: 30,
      }, signal),
    ]);
    if (signal?.aborted) return;
    catalog.value = nextCatalog;
    historyItems.value = nextHistory.filter((item) => item.provider === "prometheus");
    if (!nextCatalog.queries.some((item) => item.key === guidedKey.value)) guidedKey.value = nextCatalog.queries[0]?.key ?? "";
    if (!expertQuery.value) expertQuery.value = nextCatalog.queries.find((item) => item.key === guidedKey.value)?.query ?? "";
  } catch (reason) {
    if (!signal?.aborted) queryError.value = normalizeFailure(reason, "Prometheus 查询目录读取失败。");
  } finally {
    if (!signal?.aborted) catalogLoading.value = false;
  }
}

async function loadManagement(signal?: AbortSignal) {
  try {
    const [saved, grants] = await Promise.all([
      getQueryDefinitions(50, signal),
      getQueryAuthorizations(50, signal),
    ]);
    if (signal?.aborted) return;
    definitions.value = saved;
    authorizations.value = grants;
  } catch (reason) {
    if (!signal?.aborted) pageError.value = normalizeFailure(reason, "Query Definition 与授权记录读取失败。");
  }
}

async function applyExecution(execution: QueryExecution, updateRoute: boolean) {
  currentExecution.value = execution;
  mode.value = execution.mode;
  guidedKey.value = execution.catalog_key ?? guidedKey.value;
  expertQuery.value = execution.query;
  fromValue.value = toLocalInput(new Date(execution.time_range.from));
  toValue.value = toLocalInput(new Date(execution.time_range.to));
  stepSeconds.value = execution.bounds.step_seconds;
  activeDefinitionID.value = execution.query_definition_id ?? "";
  cursorTimestamp.value = null;
  if (updateRoute) await syncRoute({ execution: execution.id, definition: activeDefinitionID.value });
}

async function loadExecution(id: string, updateRoute: boolean, signal: AbortSignal, generation: number) {
  const execution = await getMonitoringQuery(id, signal);
  if (!mounted || signal.aborted || generation !== queryGeneration) return;
  await applyExecution(execution, updateRoute);
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
  currentExecution.value = null;
  try {
    const snapshot = await getBootstrap(signal);
    if (!mounted || signal.aborted || generation !== workspaceGeneration) return;
    bootstrap.value = snapshot;
    const parsedRoute = parseMonitoringRoute(route.query);
    selectedNamespace.value = parsedRoute.namespace || snapshot.active_scope.namespaces[0] || "";
    const page = await getResources({
      cluster: snapshot.active_scope.cluster_id,
      kind: ["Deployment", "StatefulSet", "DaemonSet"],
      limit: 500,
    }, signal);
    if (!mounted || signal.aborted || generation !== workspaceGeneration) return;
    workloads.value = page.items.filter((item) => item.layer === "workload");
    selectedResourceID.value = workloads.value.some((item) => item.id === parsedRoute.resource)
      ? parsedRoute.resource
      : workloads.value.find((item) => item.namespace === selectedNamespace.value)?.id ?? workloads.value[0]?.id ?? "";
    if (selectedResource.value?.namespace) selectedNamespace.value = selectedResource.value.namespace;
    mode.value = parsedRoute.mode;
    guidedKey.value = parsedRoute.metric;
    expertQuery.value = parsedRoute.promql;
    if (parsedRoute.from && parsedRoute.to) {
      fromValue.value = toLocalInput(new Date(parsedRoute.from));
      toValue.value = toLocalInput(new Date(parsedRoute.to));
    }
    await Promise.all([loadCatalog(signal), loadManagement(signal)]);
    if (parsedRoute.execution) {
      queryController = new AbortController();
      const querySignal = queryController.signal;
      const queryRequestGeneration = ++queryGeneration;
      await loadExecution(parsedRoute.execution, false, querySignal, queryRequestGeneration);
    } else if (parsedRoute.definition) {
      const definition = definitions.value.find((item) => item.id === parsedRoute.definition);
      if (definition) await useDefinition(definition, false);
    } else {
      const recentExecution = historyItems.value.find((item) => item.status === "succeeded" && !item.result_expired);
      if (recentExecution) {
        queryController = new AbortController();
        const querySignal = queryController.signal;
        const queryRequestGeneration = ++queryGeneration;
        await loadExecution(recentExecution.id, false, querySignal, queryRequestGeneration);
      }
    }
    if (!signal.aborted) await syncRoute();
  } catch (reason) {
    if (!signal.aborted && mounted) pageError.value = normalizeFailure(reason, "Monitoring Workspace 读取失败。");
  } finally {
    if (!signal.aborted && generation === workspaceGeneration) loading.value = false;
  }
}

async function refreshAll() {
  statusMessage.value = "";
  queryError.value = null;
  const controller = new AbortController();
  await Promise.all([loadCatalog(controller.signal), loadManagement(controller.signal)]);
}

async function changeNamespace() {
  queryController?.abort();
  queryGeneration += 1;
  selectedResourceID.value = namespaceWorkloads.value[0]?.id ?? "";
  currentExecution.value = null;
  activeDefinitionID.value = "";
  cursorTimestamp.value = null;
  await loadCatalog();
  await syncRoute({ execution: "", definition: "" });
}

async function changeResource() {
  queryController?.abort();
  queryGeneration += 1;
  if (selectedResource.value?.namespace) selectedNamespace.value = selectedResource.value.namespace;
  currentExecution.value = null;
  activeDefinitionID.value = "";
  cursorTimestamp.value = null;
  await loadCatalog();
  await syncRoute({ execution: "", definition: "" });
}

function clearDefinitionBinding() {
  activeDefinitionID.value = "";
}

function changeMode(next: QueryMode) {
  if (mode.value === next) return;
  mode.value = next;
  if (next === "expert" && !expertQuery.value) expertQuery.value = selectedCatalogEntry.value?.query ?? "";
  clearDefinitionBinding();
  currentExecution.value = null;
  cursorTimestamp.value = null;
  void syncRoute({ execution: "", definition: "" });
}

function selectGuidedQuery() {
  const entry = selectedCatalogEntry.value;
  if (entry) expertQuery.value = entry.query;
  markQueryChanged();
}

function markQueryChanged() {
  clearDefinitionBinding();
  currentExecution.value = null;
  cursorTimestamp.value = null;
  void syncRoute({ execution: "", definition: "" });
}

function selectPreset(minutes: number) {
  const to = new Date();
  fromValue.value = toLocalInput(new Date(to.getTime() - minutes * 60_000));
  toValue.value = toLocalInput(to);
  markQueryChanged();
}

async function waitForPoll(signal: AbortSignal) {
  await new Promise<void>((resolve) => {
    const timer = window.setTimeout(resolve, 250);
    signal.addEventListener("abort", () => {
      window.clearTimeout(timer);
      resolve();
    }, { once: true });
  });
}

async function reloadHistory(signal?: AbortSignal) {
  const context = monitoringContext();
  if (!context) return;
  const items = await getMonitoringQueries({
    cluster_id: context.cluster_id,
    namespace: context.namespace,
    resource_id: context.resource.id,
    limit: 30,
  }, signal);
  historyItems.value = items.filter((item) => item.provider === "prometheus");
}

async function pollExecution(id: string, generation: number, signal: AbortSignal) {
  for (let attempt = 0; attempt < 120 && mounted && !signal.aborted && generation === queryGeneration; attempt += 1) {
    const execution = await getMonitoringQuery(id, signal);
    if (!mounted || signal.aborted || generation !== queryGeneration) return;
    currentExecution.value = execution;
    if (terminalStatuses.has(execution.status)) {
      await reloadHistory(signal);
      return;
    }
    await waitForPoll(signal);
  }
  if (mounted && !signal.aborted && generation === queryGeneration) {
    queryError.value = normalizeFailure(null, "查询仍在运行，请从历史记录重新打开。");
  }
}

async function runQuery() {
  const context = monitoringContext();
  if (!context || !canRun.value) return;
  queryController?.abort();
  queryController = new AbortController();
  const signal = queryController.signal;
  const generation = ++queryGeneration;
  queryRunning.value = true;
  queryError.value = null;
  statusMessage.value = "";
  cursorTimestamp.value = null;
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
    if (!mounted || signal.aborted || generation !== queryGeneration) return;
    await applyExecution(execution, true);
    await pollExecution(execution.id, generation, signal);
  } catch (reason) {
    if (!signal.aborted && mounted && generation === queryGeneration) queryError.value = normalizeFailure(reason, "查询执行失败。");
  } finally {
    if (generation === queryGeneration) queryRunning.value = false;
  }
}

async function stopQuery() {
  const execution = currentExecution.value;
  if (!execution || !queryInFlight.value) return;
  queryController?.abort();
  queryGeneration += 1;
  queryRunning.value = false;
  try {
    currentExecution.value = await cancelMonitoringQuery(execution.id);
    await reloadHistory();
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "查询取消失败。");
  }
}

async function openExecution(id: string, updateRoute = true) {
  queryController?.abort();
  queryController = new AbortController();
  const signal = queryController.signal;
  const generation = ++queryGeneration;
  queryError.value = null;
  queryRunning.value = false;
  try {
    await loadExecution(id, updateRoute, signal, generation);
    if (currentExecution.value?.id === id && queryInFlight.value) {
      queryRunning.value = true;
      await pollExecution(id, generation, signal);
    }
  } catch (reason) {
    if (!signal.aborted) queryError.value = normalizeFailure(reason, "Query Execution 读取失败。");
  } finally {
    if (generation === queryGeneration) queryRunning.value = false;
  }
}

function openSaveDialog() {
  const execution = currentExecution.value;
  if (!execution || execution.status !== "succeeded") return;
  saveTitle.value = `${execution.resource.name} ${execution.mode === "guided" ? selectedCatalogEntry.value?.title ?? "指标查询" : "PromQL"}`;
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
    await syncRoute({ execution: execution.id, definition: definition.id });
  } catch (reason) {
    queryError.value = normalizeFailure(reason, "Query Definition 保存失败。");
  } finally {
    saving.value = false;
  }
}

async function useDefinition(definition: QueryDefinition, updateRoute = true) {
  const resource = workloads.value.find((item) => item.id === definition.resource.id);
  if (!resource) {
    queryError.value = normalizeFailure(null, "该 Query Definition 的 Workload 不在当前活动 Scope 中。");
    return;
  }
  selectedResourceID.value = resource.id;
  selectedNamespace.value = resource.namespace ?? "";
  mode.value = definition.mode;
  guidedKey.value = definition.catalog_key ?? guidedKey.value;
  expertQuery.value = definition.query;
  activeDefinitionID.value = definition.id;
  currentExecution.value = null;
  cursorTimestamp.value = null;
  await loadCatalog();
  if (updateRoute) await syncRoute({ execution: "", definition: definition.id });
}

function requestConfirmation(kind: MonitoringConfirmationKind, value: QueryExecution | QueryDefinition | QueryAuthorization) {
  if (kind === "authorize-once") confirmationTarget.value = { kind, execution: value as QueryExecution };
  else if (kind === "authorize-definition") confirmationTarget.value = { kind, definition: value as QueryDefinition };
  else confirmationTarget.value = { kind, authorization: value as QueryAuthorization };
}

async function confirmAuthorization() {
  const target = confirmationTarget.value;
  if (!target || authorizationBusy.value) return;
  const identity = target.kind === "authorize-once"
    ? target.execution.id
    : target.kind === "authorize-definition" ? target.definition.id : target.authorization.id;
  authorizationBusy.value = identity;
  queryError.value = null;
  try {
    if (target.kind === "authorize-once") {
      const authorization = await createQueryAuthorization({ mode: "run_once", query_execution_id: target.execution.id });
      authorizations.value.unshift(authorization);
      statusMessage.value = "已创建精确的一次性 Agent 查询授权。";
    } else if (target.kind === "authorize-definition") {
      const authorization = await createQueryAuthorization({ mode: "definition", query_definition_id: target.definition.id });
      authorizations.value.unshift(authorization);
      statusMessage.value = `已授权 Agent 使用 ${target.definition.title} revision ${target.definition.revision}。`;
    } else {
      await revokeQueryAuthorization(target.authorization.id);
      authorizations.value = authorizations.value.map((candidate) => candidate.id === target.authorization.id
        ? { ...candidate, revoked_at: new Date().toISOString() }
        : candidate);
      statusMessage.value = "Agent 查询授权已撤销。";
    }
    confirmationTarget.value = null;
  } catch (reason) {
    const fallback = target.kind === "revoke" ? "Agent 查询授权撤销失败。" : "Agent 查询授权创建失败。";
    queryError.value = normalizeFailure(reason, fallback);
  } finally {
    authorizationBusy.value = "";
  }
}

async function copyQuery() {
  const query = mode.value === "expert" ? expertQuery.value : selectedCatalogEntry.value?.query;
  if (!query) return;
  try {
    await navigator.clipboard.writeText(query);
    statusMessage.value = "PromQL 已复制。";
  } catch {
    queryError.value = normalizeFailure(null, "浏览器未允许复制 PromQL。");
  }
}

function linkTarget(link?: MonitoringContextLink): "_blank" | "_self" {
  return link?.target === "external" ? "_blank" : "_self";
}

function openExecutionInAgent() {
  const context = currentAgentContext();
  if (!context) {
    statusMessage.value = "请先完成一个仍保留结果的查询，再关联 Agent 调查。";
    return;
  }
  openAgentPanel({ context });
}

function receiveScopeChange() {
  void loadWorkspace();
}

onMounted(() => {
  window.addEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, receiveScopeChange);
  void loadWorkspace();
});

watch([() => route.fullPath, currentExecution], publishCurrentAgentContext, { flush: "post" });

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
    class="monitoring-workspace"
    width="full"
    aria-labelledby="monitoring-heading"
  >
    <WorkspaceHeader
      title="监控"
      eyebrow="Observability / Metrics"
      heading-id="monitoring-heading"
      :description="`${selectedResource ? `${selectedResource.kind} ${selectedResource.name}` : '当前运行范围'} · 以真实指标值、趋势与异常时间窗口为中心`"
    >
      <template #actions>
        <UTooltip text="刷新监控工作区">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-refresh-cw"
            square
            aria-label="刷新监控工作区"
            :loading="loading || catalogLoading"
            @click="refreshAll"
          />
        </UTooltip>
      </template>
    </WorkspaceHeader>

    <WorkspaceState
      v-if="pageError"
      kind="error"
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
    <section
      v-if="queryError"
      class="monitoring-query-error"
      role="alert"
      aria-live="assertive"
    >
      <UIcon
        name="i-lucide-circle-x"
        aria-hidden="true"
      />
      <div>
        <strong>查询失败</strong>
        <span>{{ compactQueryErrorMessage }}</span>
        <small>
          错误码：{{ queryError.code }}
          <template v-if="queryErrorReason"> · {{ queryErrorReason }}</template>
          <template v-if="queryError.requestID"> · Request {{ queryError.requestID }}</template>
          <template v-if="queryError.traceID"> · Trace {{ queryError.traceID }}</template>
        </small>
      </div>
      <UButton
        v-if="queryErrorReason && canRun"
        color="neutral"
        variant="outline"
        size="sm"
        icon="i-lucide-rotate-cw"
        label="重试"
        :loading="queryRunning"
        @click="runQuery"
      />
    </section>
    <UAlert
      v-if="statusMessage"
      color="success"
      variant="soft"
      icon="i-lucide-circle-check"
      title="操作已完成"
      :description="statusMessage"
      role="status"
    />

    <WorkspaceState
      v-if="loading"
      kind="loading"
      title="正在读取活动 Scope"
      description="加载真实 Workload、Prometheus 查询目录与历史身份。"
    />

    <template v-else>
      <UAlert
        v-if="catalog && !providerReady"
        color="error"
        variant="soft"
        icon="i-lucide-ban"
        :title="`Prometheus ${providerStateLabel(catalog.provider_state)}`"
        :description="`${catalog.provider_detail} · ${catalog.source.identity || '当前 Configuration Revision 没有可用采集端点'}`"
      />

      <section
        class="monitoring-workspace__query"
        aria-label="指标查询工具栏"
      >
        <MonitoringQueryControls
          :namespaces="namespaces"
          :resources="namespaceWorkloads"
          :catalog="catalog"
          :namespace="selectedNamespace"
          :resource-i-d="selectedResourceID"
          :mode="mode"
          :guided-key="guidedKey"
          :expert-query="expertQuery"
          :from="fromValue"
          :to="toValue"
          :step-seconds="stepSeconds"
          :valid-time-range="validTimeRange"
          :can-run="canRun"
          :running="queryRunning"
          :query-in-flight="queryInFlight"
          @update:namespace="selectedNamespace = $event"
          @update:resource-i-d="selectedResourceID = $event"
          @update:mode="changeMode"
          @update:guided-key="guidedKey = $event"
          @update:expert-query="expertQuery = $event"
          @update:from="fromValue = $event"
          @update:to="toValue = $event"
          @update:step-seconds="stepSeconds = $event"
          @namespace-change="changeNamespace"
          @resource-change="changeResource"
          @guided-change="selectGuidedQuery"
          @query-change="markQueryChanged"
          @preset="selectPreset"
          @copy-query="copyQuery"
          @run="runQuery"
          @cancel="stopQuery"
        />
      </section>

      <div class="monitoring-workspace__grid">
        <MonitoringResult
          :execution="currentExecution"
          :cursor-timestamp="cursorTimestamp"
          :metric-title="selectedCatalogEntry?.title || (mode === 'expert' ? 'PromQL 分析' : '指标趋势')"
          :resource-label="selectedResource ? `${selectedResource.kind} ${selectedResource.name}` : '当前 Scope'"
          @cursor="cursorTimestamp = $event"
        >
          <template #actions>
            <UButton
              color="neutral"
              variant="soft"
              icon="i-lucide-save"
              label="保存定义"
              :disabled="currentExecution?.status !== 'succeeded'"
              @click="openSaveDialog"
            />
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-shield-check"
              label="授权一次"
              :disabled="currentExecution?.status !== 'succeeded'"
              :loading="authorizationBusy === currentExecution?.id"
              @click="currentExecution && requestConfirmation('authorize-once', currentExecution)"
            />
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-bot"
              label="关联 Agent"
              :disabled="currentExecution?.status !== 'succeeded' || currentExecution?.result_expired"
              @click="openExecutionInAgent"
            />
            <UButton
              v-if="providerLink?.availability === 'available'"
              color="neutral"
              variant="ghost"
              icon="i-lucide-external-link"
              label="Grafana"
              :to="providerLink.href"
              :target="linkTarget(providerLink)"
              rel="noopener noreferrer"
              external
            />
            <UPopover :content="{ align: 'end', side: 'bottom', sideOffset: 8, collisionPadding: 16, sticky: 'always' }">
              <UButton
                color="neutral"
                variant="ghost"
                icon="i-lucide-history"
                :label="`查询历史 ${historyItems.length}`"
              />
              <template #content>
                <div class="monitoring-history-popover">
                  <MonitoringHistory
                    :items="historyItems"
                    :active-i-d="currentExecution?.id ?? ''"
                    @select="openExecution"
                  />
                </div>
              </template>
            </UPopover>
          </template>
        </MonitoringResult>
      </div>

      <MonitoringAssets
        :definitions="definitions"
        :authorizations="authorizations"
        :busy-i-d="authorizationBusy"
        @load-definition="useDefinition"
        @authorize-definition="requestConfirmation('authorize-definition', $event)"
        @revoke-authorization="requestConfirmation('revoke', $event)"
      />
    </template>
  </WorkspacePageFrame>

  <MonitoringDialogs
    :save-open="saveDialogOpen"
    :save-title="saveTitle"
    :save-description="saveDescription"
    :saving="saving"
    :confirmation="confirmation"
    :confirming="Boolean(authorizationBusy)"
    @update:save-open="saveDialogOpen = $event"
    @update:save-title="saveTitle = $event"
    @update:save-description="saveDescription = $event"
    @save="saveDefinition"
    @close-confirmation="confirmationTarget = null"
    @confirm="confirmAuthorization"
  />
</template>

<style scoped>
.monitoring-workspace {
  padding: var(--co-space-5) clamp(var(--co-space-4), 2.5vw, var(--co-space-8)) var(--co-space-10);
}
.monitoring-workspace code { min-width: 0; overflow-wrap: anywhere; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 11px; }
.monitoring-workspace__grid { display: grid; grid-template-columns: minmax(0, 1fr); }
.monitoring-workspace__query { min-width: 0; margin-bottom: var(--co-space-3); }
.monitoring-query-error { display: grid; box-sizing: border-box; min-height: 72px; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-3); padding: 12px 16px; border: 1px solid color-mix(in srgb, var(--co-status-critical-fg) 18%, transparent); border-radius: var(--co-radius-frame); background: color-mix(in srgb, var(--co-status-critical-bg) 58%, transparent); color: var(--co-status-critical-fg); }
.monitoring-query-error > svg { width: 18px; height: 18px; align-self: start; margin-top: 2px; }
.monitoring-query-error > div { display: grid; min-width: 0; gap: 2px; }
.monitoring-query-error strong { color: var(--co-text-primary); font-size: 12px; }
.monitoring-query-error span { color: var(--co-text-secondary); font-size: 11px; line-height: 1.45; }
.monitoring-query-error small { overflow-wrap: anywhere; color: var(--co-status-critical-fg); font-family: var(--co-font-mono); font-size: 9px; opacity: .78; }
.monitoring-history-popover { box-sizing: border-box; width: min(360px, var(--reka-popover-content-available-width, 360px), calc(100vw - 32px)); max-height: min(360px, var(--reka-popover-content-available-height, 360px), calc(100dvh - 32px)); overflow: hidden; padding: var(--co-space-3); }

@media (max-width: 1024px) {
  .monitoring-workspace { padding-inline: var(--co-space-4); }
  .monitoring-workspace__grid { grid-template-columns: minmax(0, 1fr); }
}

@media (max-width: 640px) {
  .monitoring-query-error { grid-template-columns: auto minmax(0, 1fr); }
  .monitoring-query-error > button { grid-column: 2; justify-self: start; }
}

@media (prefers-reduced-motion: reduce) {
  .monitoring-workspace * { scroll-behavior: auto; }
}
</style>
