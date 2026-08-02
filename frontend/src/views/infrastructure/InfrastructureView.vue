<script setup lang="ts">
import type { LocationQueryRaw, RouteLocationRaw } from "vue-router";
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { apiErrorDetails } from "../../api/client";
import {
  getResource,
  getResourceEvents,
  getResources,
  getTopology,
  projectResolvedInfrastructureScope,
  type EventPage,
  type InfrastructureContextLink,
  type InfrastructureQuery,
  type KubernetesResource,
  type ResourceDetail,
  type ResourcePage,
  type TopologyEdge,
  type TopologySnapshot,
} from "../../api/infrastructure";
import { getBootstrap, type BootstrapSnapshot } from "../../api/platform";
import ApiErrorNotice from "../../components/workspace/ApiErrorNotice.vue";
import WorkspaceDenseList, { type DenseListSeverity } from "../../components/workspace/WorkspaceDenseList.vue";
import WorkspaceInspector, {
  type InspectorTargetState,
} from "../../components/workspace/WorkspaceInspector.vue";
import WorkspaceState from "../../components/workspace/WorkspaceState.vue";
import WorkspaceStatusRow from "../../components/workspace/WorkspaceStatusRow.vue";
import WorkspaceTechnicalDetails, { type TechnicalDetailField } from "../../components/workspace/WorkspaceTechnicalDetails.vue";
import { invalidateQueryDomain } from "../../composables/queryCache";
import { useWorkspaceInspector } from "../../composables/useWorkspaceInspector";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";
import {
  canReuseResolvedInfrastructureScope,
  infrastructureContextLocation,
  infrastructureResourceTypeItems,
  kindsForResourceType,
  queryValues,
  resourceTypeForKinds,
  sortResourcesByAttention,
  summarizeInfrastructureHealth,
  type InfrastructureResourceType,
} from "./infrastructureModel";

type ResourceRow = KubernetesResource & Record<string, unknown>;

const ALL_NAMESPACES_VALUE = "__all_namespaces__";
const ALL_KINDS_VALUE = "__all_kinds__";

interface ContextLinkRow {
  link: InfrastructureContextLink;
  location: RouteLocationRaw | null;
}

const route = useRoute();
const router = useRouter();
const resourceList = ref<HTMLElement | null>(null);
const inspector = useWorkspaceInspector({
  selectedKey: "resource",
  scrollElement: () => resourceList.value,
});
const bootstrap = ref<BootstrapSnapshot | null>(null);
const topology = ref<TopologySnapshot | null>(null);
const resourcePage = ref<ResourcePage | null>(null);
const detail = ref<ResourceDetail | null>(null);
const events = ref<EventPage | null>(null);
const loading = ref(false);
const detailLoading = ref(false);
const bootstrapError = shallowRef<unknown>(null);
const topologyError = shallowRef<unknown>(null);
const resourceError = shallowRef<unknown>(null);
const detailError = shallowRef<unknown>(null);
const eventError = shallowRef<unknown>(null);
const namespaceValue = ref(queryValue(route.query.namespace));
const searchValue = ref(queryValue(route.query.search));
const resourceType = ref<InfrastructureResourceType>(resourceTypeForKinds(queryValues(route.query.kind)));
type HealthFilter = "all" | "attention" | "healthy" | "unknown";
const healthFilterValues = new Set<HealthFilter>(["all", "attention", "healthy", "unknown"]);
const healthFilter = ref<HealthFilter>(healthFilterValue(route.query.health));
let controller: AbortController | undefined;
let requestToken = 0;
let previousWorkspaceSignature = "";
let previousSelection = "";
let reusableCanonicalRoute = "";

const dateFormatter = new Intl.DateTimeFormat("zh-CN", {
  dateStyle: "medium",
  timeStyle: "medium",
});
const resourceTypeValues = new Set(infrastructureResourceTypeItems.map((item) => item.value));
const resourceTabs = infrastructureResourceTypeItems.map((item) => ({
  ...item,
  icon: ({
    all: "i-lucide-boxes",
    namespace: "i-lucide-layers-3",
    service: "i-lucide-network",
    workload: "i-lucide-box",
    pod: "i-lucide-container",
    node: "i-lucide-server",
    gateway: "i-lucide-route",
  } satisfies Record<InfrastructureResourceType, string>)[item.value],
}));
const healthLabels: Record<KubernetesResource["health"]["state"], string> = {
  healthy: "健康",
  warning: "警告",
  critical: "故障",
  unknown: "未知",
};
const healthColors = {
  healthy: "success",
  warning: "warning",
  critical: "error",
  unknown: "neutral",
} as const;

const selectedResourceID = inspector.selectedID;
const namespaces = computed(() => (
  bootstrap.value?.active_scope.namespaces
  ?? topology.value?.scope.namespaces
  ?? resourcePage.value?.scope.namespaces
  ?? []
));
const namespaceItems = computed(() => [
  { label: "全部作用域 Namespace", value: ALL_NAMESPACES_VALUE },
  ...namespaces.value
    .filter((namespace) => namespace.trim().length > 0)
    .map((namespace) => ({ label: namespace, value: namespace })),
]);
const namespaceSelectValue = computed(() => namespaceValue.value || ALL_NAMESPACES_VALUE);
const resources = computed<ResourceRow[]>(() => (
  (resourcePage.value?.items ?? topology.value?.nodes ?? []) as ResourceRow[]
));
const healthSummary = computed(() => summarizeInfrastructureHealth(resources.value));
const visibleResources = computed<ResourceRow[]>(() => sortResourcesByAttention(resources.value)
  .filter((resource) => {
    if (healthFilter.value === "attention") return resource.health.state !== "healthy";
    if (healthFilter.value === "healthy") return resource.health.state === "healthy";
    if (healthFilter.value === "unknown") return resource.health.state === "unknown";
    return true;
  }) as ResourceRow[]);
const kindOptions = computed(() => [
  { label: "全部 Kubernetes Kind", value: ALL_KINDS_VALUE },
  ...[...new Set((topology.value?.nodes ?? resources.value).map((resource) => resource.kind))]
    .filter((kind) => kind.trim().length > 0)
    .sort()
    .map((kind) => ({ label: kind, value: kind })),
]);
const selectedKind = computed(() => queryValues(route.query.kind).length === 1 ? queryValues(route.query.kind)[0] : ALL_KINDS_VALUE);
const timeWindowItems = [
  { label: "最近 1 小时", value: "1h" },
  { label: "最近 6 小时", value: "6h" },
  { label: "最近 24 小时", value: "24h" },
];
const selectedTimeWindow = computed(() => {
  const from = new Date(queryValue(route.query.from)).getTime();
  const to = new Date(queryValue(route.query.to)).getTime();
  if (!Number.isFinite(from) || !Number.isFinite(to)) return "1h";
  const hours = Math.round((to - from) / 3_600_000);
  return hours === 6 ? "6h" : hours === 24 ? "24h" : "1h";
});
const hasResourceFilters = computed(() => Boolean(
  namespaceValue.value
  || searchValue.value.trim()
  || resourceType.value !== "all"
  || healthFilter.value !== "all"
  || selectedKind.value !== ALL_KINDS_VALUE,
));
const providerState = computed(() => topology.value?.provider_state ?? resourcePage.value?.provider_state);
const providerReady = computed(() => providerState.value === "available" || providerState.value === "partial");
const providerIdentity = computed(() => topology.value?.source.identity ?? resourcePage.value?.source.identity ?? "未读取");
const clusterID = computed(() => (
  bootstrap.value?.active_scope.cluster_id
  || topology.value?.scope.cluster_id
  || resourcePage.value?.scope.cluster_id
  || queryValue(route.query.cluster)
  || "未读取"
));
const collectedAt = computed(() => topology.value?.collected_at ?? resourcePage.value?.collected_at ?? "");
const partialProjection = computed(() => Boolean(
  topology.value?.partial
  || topology.value?.truncated
  || resourcePage.value?.partial
  || resourcePage.value?.truncated,
));
const staleProjection = computed(() => (
  topology.value?.freshness.state === "stale" || resourcePage.value?.freshness.state === "stale"
));
const selectedResource = computed(() => detail.value?.resource ?? null);
const atlasTarget = computed<RouteLocationRaw>(() => ({
  name: "atlas",
  query: {
    cluster: queryValue(route.query.cluster) || undefined,
    namespace: queryValue(route.query.namespace) || undefined,
    resource: selectedResourceID.value || undefined,
  },
}));
const relatedResources = computed(() => [...(detail.value?.related ?? [])].sort((left, right) => {
  const layerOrder = ["namespace", "gateway", "service", "workload", "pod", "node"];
  return layerOrder.indexOf(left.layer) - layerOrder.indexOf(right.layer)
    || left.name.localeCompare(right.name);
}));
const selectedEdges = computed(() => detail.value?.edges ?? []);
const resourceByID = computed(() => {
  const values = new Map<string, KubernetesResource>();
  for (const resource of topology.value?.nodes ?? []) values.set(resource.id, resource);
  for (const resource of resourcePage.value?.items ?? []) values.set(resource.id, resource);
  for (const resource of detail.value?.related ?? []) values.set(resource.id, resource);
  if (selectedResource.value) values.set(selectedResource.value.id, selectedResource.value);
  return values;
});
const targetError = computed(() => (
  detailError.value ? apiErrorDetails(detailError.value, "资源详情读取失败。") : null
));
const inspectorTargetState = computed<InspectorTargetState>(() => {
  const status = targetError.value?.status;
  const code = targetError.value?.code.toLocaleUpperCase() ?? "";
  if (status === 401) return "expired";
  if (status === 403 || code.includes("FORBIDDEN") || code.includes("PERMISSION")) return "permission-denied";
  if (status === 404 || code.includes("NOT_FOUND")) return "deleted";
  if (status === 400 || status === 422 || code.includes("INVALID")) return "invalid";
  return "ready";
});
const inspectorTargetDescription = computed(() => {
  const failure = targetError.value;
  if (!failure) return "";
  const identity = [failure.code, failure.requestID ? `Request ${failure.requestID}` : ""].filter(Boolean).join(" · ");
  return identity ? `${failure.message} (${identity})` : failure.message;
});
const contextLinks = computed<ContextLinkRow[]>(() => (
  (selectedResource.value?.links ?? [])
    .filter((link) => link.kind === "internal" && link.target === "current")
    .map((link) => ({ link, location: infrastructureContextLocation(link) }))
));
const projectionIssues = computed(() => topology.value?.issues ?? []);
const resourceTechnicalFields = computed<TechnicalDetailField[]>(() => selectedResource.value ? [
  { label: "Resource ID", value: selectedResource.value.id, code: true, copyValue: selectedResource.value.id },
  { label: "Source UID", value: selectedResource.value.source_uid, code: true, copyValue: selectedResource.value.source_uid },
  { label: "Resource version", value: selectedResource.value.resource_version, code: true, copyValue: selectedResource.value.resource_version },
  { label: "Generation", value: selectedResource.value.generation, code: true },
  { label: "Snapshot", value: detail.value?.snapshot_id, code: true, copyValue: detail.value?.snapshot_id },
  { label: "Provider", value: detail.value?.source.identity || providerIdentity.value, code: true },
] : []);

function queryValue(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === "string" ? value[0] : "";
  return typeof value === "string" ? value : "";
}

function healthFilterValue(value: unknown): HealthFilter {
  const candidate = queryValue(value) as HealthFilter;
  return healthFilterValues.has(candidate) ? candidate : "all";
}

function currentQuery(): InfrastructureQuery {
  const kinds = queryValues(route.query.kind);
  return {
    cluster: queryValue(route.query.cluster) || undefined,
    namespace: queryValue(route.query.namespace) || undefined,
    kind: kinds.length ? kinds : undefined,
    search: queryValue(route.query.search) || undefined,
    cursor: queryValue(route.query.cursor) || undefined,
    from: queryValue(route.query.from) || undefined,
    to: queryValue(route.query.to) || undefined,
    limit: 500,
  };
}

function ensureTimeRange(): boolean {
  if (queryValue(route.query.from) && queryValue(route.query.to)) return true;
  const to = new Date();
  const from = new Date(to.getTime() - 60 * 60 * 1000);
  void router.replace({
    query: {
      ...route.query,
      from: queryValue(route.query.from) || from.toISOString(),
      to: queryValue(route.query.to) || to.toISOString(),
    },
  });
  return false;
}

async function loadWorkspace(background = false) {
  if (!ensureTimeRange()) return;
  const token = ++requestToken;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  detailLoading.value = Boolean(selectedResourceID.value);
  bootstrapError.value = null;
  topologyError.value = null;
  resourceError.value = null;
  detailError.value = null;
  eventError.value = null;
  detail.value = null;
  events.value = null;
  if (!background) {
    topology.value = null;
    resourcePage.value = null;
  }

  const query = currentQuery();
  try {
    const [nextBootstrap, nextTopology, nextPage] = await Promise.allSettled([
      getBootstrap(controller.signal),
      getTopology(query, controller.signal),
      getResources(query, controller.signal),
    ]);
    if (controller.signal.aborted || token !== requestToken) return;

    if (nextBootstrap.status === "fulfilled") bootstrap.value = nextBootstrap.value;
    else bootstrapError.value = nextBootstrap.reason;
    if (nextTopology.status === "fulfilled") topology.value = nextTopology.value;
    else topologyError.value = nextTopology.reason;
    if (nextPage.status === "fulfilled") resourcePage.value = nextPage.value;
    else resourceError.value = nextPage.reason;

    const resolvedCluster = nextBootstrap.status === "fulfilled"
      ? nextBootstrap.value.active_scope.cluster_id
      : nextTopology.status === "fulfilled"
        ? nextTopology.value.scope.cluster_id
        : nextPage.status === "fulfilled"
          ? nextPage.value.scope.cluster_id
          : "";
    if (!queryValue(route.query.cluster) && resolvedCluster) {
      const normalizedLocation = { query: { ...route.query, cluster: resolvedCluster } };
      if (canReuseResolvedInfrastructureScope(resolvedCluster, [
        nextBootstrap.status === "fulfilled" ? nextBootstrap.value.active_scope.cluster_id : undefined,
        nextTopology.status === "fulfilled" ? nextTopology.value.scope.cluster_id : undefined,
        nextPage.status === "fulfilled" ? nextPage.value.scope.cluster_id : undefined,
      ])) {
        projectResolvedInfrastructureScope(query, resolvedCluster);
        reusableCanonicalRoute = router.resolve(normalizedLocation).fullPath;
      }
      void router.replace(normalizedLocation);
      return;
    }

    const resolvedProviderState = nextTopology.status === "fulfilled"
      ? nextTopology.value.provider_state
      : nextPage.status === "fulfilled"
        ? nextPage.value.provider_state
        : undefined;
    if (
      !selectedResourceID.value
      || (resolvedProviderState !== "available" && resolvedProviderState !== "partial")
    ) return;

    const [nextDetail, nextEvents] = await Promise.allSettled([
      getResource(selectedResourceID.value, query, controller.signal),
      getResourceEvents(selectedResourceID.value, query, controller.signal),
    ]);
    if (controller.signal.aborted || token !== requestToken) return;
    if (nextDetail.status === "fulfilled") detail.value = nextDetail.value;
    else detailError.value = nextDetail.reason;
    if (nextEvents.status === "fulfilled") events.value = nextEvents.value;
    else eventError.value = nextEvents.reason;
  } finally {
    if (token === requestToken) {
      loading.value = false;
      detailLoading.value = false;
    }
  }
}

function refreshWorkspace() {
  invalidateQueryDomain("infrastructure");
  void loadWorkspace(true);
}

function updateQuery(changes: LocationQueryRaw) {
  const query: LocationQueryRaw = { ...route.query };
  for (const [key, value] of Object.entries(changes)) {
    if (value === undefined || value === null || value === "" || (Array.isArray(value) && !value.length)) {
      delete query[key];
    } else {
      query[key] = value;
    }
  }
  delete query.cursor;
  void router.push({ name: "infrastructure", query });
}

function changeNamespace(value: string | number) {
  const selected = String(value);
  namespaceValue.value = selected === ALL_NAMESPACES_VALUE ? "" : selected;
  updateQuery({ namespace: namespaceValue.value || undefined, resource: undefined });
}

function changeResourceType(value: string | number) {
  const next = String(value) as InfrastructureResourceType;
  if (!resourceTypeValues.has(next)) return;
  resourceType.value = next;
  updateQuery({ kind: kindsForResourceType(next), resource: undefined });
}

function changeKind(value: string | number) {
  const selected = String(value);
  const kind = selected === ALL_KINDS_VALUE ? "" : selected;
  resourceType.value = resourceTypeForKinds(kind ? [kind] : []);
  updateQuery({ kind: kind || undefined, resource: undefined });
}

function changeHealthFilter(value: HealthFilter) {
  healthFilter.value = value;
  updateQuery({ health: value === "all" ? undefined : value, resource: undefined });
}

function changeTimeWindow(value: string | number) {
  const hours = Number.parseInt(String(value), 10);
  if (![1, 6, 24].includes(hours)) return;
  const to = new Date();
  const from = new Date(to.getTime() - hours * 3_600_000);
  updateQuery({ from: from.toISOString(), to: to.toISOString(), resource: undefined });
}

function applySearch() {
  updateQuery({ search: searchValue.value.trim() || undefined, resource: undefined });
}

function resetFilters() {
  namespaceValue.value = "";
  searchValue.value = "";
  resourceType.value = "all";
  healthFilter.value = "all";
  updateQuery({ namespace: undefined, kind: undefined, search: undefined, health: undefined, resource: undefined });
}

function openNextPage() {
  if (!resourcePage.value?.next_cursor) return;
  void router.push({ name: "infrastructure", query: { ...route.query, cursor: resourcePage.value.next_cursor, resource: undefined } });
}

function selectResource(resource: ResourceRow, trigger: HTMLElement | null) {
  void inspector.open(resource.id, trigger);
}

function selectResourceByID(id?: string) {
  if (!id) return;
  void inspector.open(id);
}

function closeInspector() {
  void inspector.close();
}

function handleInspectorOpen(value: boolean) {
  if (!value) closeInspector();
}

function edgePeer(edge: TopologyEdge): KubernetesResource | undefined {
  const selectedID = selectedResource.value?.id;
  return resourceByID.value.get(edge.source_id === selectedID ? edge.target_id : edge.source_id);
}

function relationLabel(relation: TopologyEdge["relation"]): string {
  return ({
    contains: "包含",
    owns: "拥有",
    selects: "选择",
    routes_to: "路由到",
    scheduled_on: "调度到",
    backend_ref: "后端引用",
  } satisfies Record<TopologyEdge["relation"], string>)[relation];
}

function severity(resource: ResourceRow): DenseListSeverity {
  if (resource.health.state === "critical") return "critical";
  if (resource.health.state === "warning") return "warning";
  return resource.health.state === "unknown" ? "info" : "neutral";
}

function resourceReadyLabel(resource: ResourceRow): string {
  if (resource.workload) return `Ready ${resource.workload.ready_replicas}/${resource.workload.desired_replicas}`;
  if (resource.endpoints.length) {
    const ready = resource.endpoints.filter((endpoint) => endpoint.ready !== false).length;
    return `Ready ${ready}/${resource.endpoints.length}`;
  }
  const readyCondition = resource.conditions.find((condition) => condition.type.toLowerCase() === "ready");
  return readyCondition ? `Ready ${readyCondition.status}` : "Ready 未报告";
}

function providerStateLabel(): string {
  return ({
    available: "可用",
    partial: "部分可用",
    unavailable: "不可用",
    disabled: "已停用",
  } as Record<string, string>)[providerState.value ?? ""] ?? "检查中";
}

function contextLinkIcon(link: InfrastructureContextLink): string {
  if (link.href.startsWith("/monitoring")) return "i-lucide-chart-no-axes-combined";
  if (link.href.startsWith("/logs")) return "i-lucide-logs";
  if (link.href.startsWith("/traces")) return "i-lucide-git-branch";
  return "i-lucide-search";
}

function formatTime(value?: string): string {
  if (!value) return "未报告";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : dateFormatter.format(parsed);
}

watch(() => route.fullPath, () => {
  namespaceValue.value = queryValue(route.query.namespace);
  searchValue.value = queryValue(route.query.search);
  resourceType.value = resourceTypeForKinds(queryValues(route.query.kind));
  healthFilter.value = healthFilterValue(route.query.health);
  const workspaceSignature = JSON.stringify({ query: currentQuery(), health: healthFilter.value });
  const selection = selectedResourceID.value;
  const selectionOnly = previousWorkspaceSignature === workspaceSignature
    && previousSelection !== selection;
  const closedOnly = selectionOnly
    && Boolean(previousSelection)
    && !selection;
  previousWorkspaceSignature = workspaceSignature;
  previousSelection = selection;
  if (reusableCanonicalRoute === route.fullPath) {
    reusableCanonicalRoute = "";
    return;
  }
  reusableCanonicalRoute = "";
  if (closedOnly) {
    detail.value = null;
    events.value = null;
    detailError.value = null;
    eventError.value = null;
    detailLoading.value = false;
    return;
  }
  void loadWorkspace(selectionOnly);
}, { immediate: true });

function handleOperationalScopeChanged() {
  bootstrap.value = null;
  topology.value = null;
  resourcePage.value = null;
  detail.value = null;
  events.value = null;
  void loadWorkspace();
}

onMounted(() => window.addEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged));
onBeforeUnmount(() => {
  window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged);
  requestToken += 1;
  controller?.abort();
});
</script>

<template>
  <article
    class="infrastructure-view"
    data-testid="infrastructure-workspace"
  >
    <header class="infrastructure-header">
      <div class="infrastructure-heading">
        <span class="infrastructure-eyebrow">Infrastructure Workspace</span>
        <h1 tabindex="-1">
          基础设施资源
        </h1>
        <p>
          <span class="mono-text">{{ clusterID }}</span>
          <span>{{ bootstrap?.active_scope.environment || "环境未报告" }}</span>
          <span>{{ namespaces.length }} Namespaces</span>
        </p>
      </div>
      <div class="projection-identity">
        <UBadge
          :color="providerReady ? 'success' : 'warning'"
          variant="subtle"
          icon="i-lucide-plug-zap"
          :label="`Kubernetes ${providerStateLabel()}`"
        />
        <span
          class="mono-text"
          :title="providerIdentity"
        >{{ providerIdentity }}</span>
      </div>
      <div class="infrastructure-actions">
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-orbit"
          label="打开 Atlas"
          :to="atlasTarget"
        />
        <UTooltip text="刷新当前资源投影">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-refresh-cw"
            square
            aria-label="刷新基础设施资源"
            :loading="loading"
            @click="refreshWorkspace"
          />
        </UTooltip>
      </div>
    </header>

    <section
      class="resource-posture"
      aria-label="基础设施健康摘要"
    >
      <div class="posture-copy">
        <span class="infrastructure-eyebrow">Current posture</span>
        <strong>{{ healthSummary.attention ? `${healthSummary.attention} 个资源需要关注` : "当前资源未报告异常" }}</strong>
        <small>{{ resources.length }} 个筛选后资源 · {{ topology?.edges.length ?? 0 }} 条真实关系</small>
      </div>
      <div class="posture-metrics">
        <UButton
          color="neutral"
          variant="ghost"
          class="posture-metric"
          :class="{ 'is-active': healthFilter === 'all' }"
          :aria-pressed="healthFilter === 'all'"
          @click="changeHealthFilter('all')"
        >
          <span>全部</span><strong>{{ healthSummary.total }}</strong>
        </UButton>
        <UButton
          color="error"
          variant="ghost"
          class="posture-metric is-critical"
          :class="{ 'is-active': healthFilter === 'attention' }"
          :aria-pressed="healthFilter === 'attention'"
          @click="changeHealthFilter('attention')"
        >
          <span>需关注</span><strong>{{ healthSummary.attention }}</strong>
        </UButton>
        <UButton
          color="success"
          variant="ghost"
          class="posture-metric is-healthy"
          :class="{ 'is-active': healthFilter === 'healthy' }"
          :aria-pressed="healthFilter === 'healthy'"
          @click="changeHealthFilter('healthy')"
        >
          <span>健康</span><strong>{{ healthSummary.healthy }}</strong>
        </UButton>
        <UButton
          color="neutral"
          variant="ghost"
          class="posture-metric"
          :class="{ 'is-active': healthFilter === 'unknown' }"
          :aria-pressed="healthFilter === 'unknown'"
          @click="changeHealthFilter('unknown')"
        >
          <span>未知</span><strong>{{ healthSummary.unknown }}</strong>
        </UButton>
      </div>
    </section>

    <section
      class="resource-controls"
      aria-label="资源筛选"
    >
      <UTabs
        class="resource-type-tabs"
        :model-value="resourceType"
        :items="resourceTabs"
        :content="false"
        color="primary"
        variant="pill"
        size="sm"
        @update:model-value="changeResourceType"
      />
      <div class="resource-filter-row">
        <USelect
          class="namespace-select"
          :model-value="namespaceSelectValue"
          :items="namespaceItems"
          value-key="value"
          label-key="label"
          icon="i-lucide-layers-3"
          aria-label="筛选 Namespace"
          @update:model-value="changeNamespace"
        />
        <form
          class="resource-search"
          role="search"
          @submit.prevent="applySearch"
        >
          <UInput
            v-model="searchValue"
            icon="i-lucide-search"
            name="infrastructure-search"
            type="search"
            autocomplete="off"
            aria-label="搜索资源"
            placeholder="名称、Kind 或 Namespace"
          />
          <UButton
            type="submit"
            :color="hasResourceFilters ? 'primary' : 'neutral'"
            :variant="hasResourceFilters ? 'soft' : 'outline'"
            icon="i-lucide-list-filter"
            label="筛选"
          />
        </form>
        <div class="filter-actions">
          <UPopover>
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-sliders-horizontal"
              label="高级筛选"
            />
            <template #content>
              <div class="advanced-filters">
                <UFormField label="精确 Kind">
                  <USelect
                    :model-value="selectedKind"
                    :items="kindOptions"
                    value-key="value"
                    label-key="label"
                    aria-label="按精确 Kubernetes Kind 筛选"
                    @update:model-value="changeKind"
                  />
                </UFormField>
                <UFormField label="事件时间范围">
                  <USelect
                    :model-value="selectedTimeWindow"
                    :items="timeWindowItems"
                    value-key="value"
                    label-key="label"
                    aria-label="选择资源事件时间范围"
                    @update:model-value="changeTimeWindow"
                  />
                </UFormField>
                <p>时间范围同时作用于资源详情的 Kubernetes Events，并保持在 URL 中。</p>
              </div>
            </template>
          </UPopover>
          <UTooltip text="清除 Namespace、资源类型、健康与搜索条件">
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-filter-x"
              square
              aria-label="清除资源筛选"
              @click="resetFilters"
            />
          </UTooltip>
        </div>
      </div>
    </section>

    <div class="projection-states">
      <ApiErrorNotice
        v-if="bootstrapError"
        :error="bootstrapError"
        title="Operational Scope 读取失败"
        retryable
        @retry="refreshWorkspace"
      />
      <ApiErrorNotice
        v-if="topologyError"
        :error="topologyError"
        title="Topology 投影读取失败"
        retryable
        @retry="refreshWorkspace"
      />
      <ApiErrorNotice
        v-if="resourceError"
        :error="resourceError"
        title="资源列表读取失败"
        retryable
        @retry="refreshWorkspace"
      />
      <WorkspaceState
        v-if="partialProjection"
        kind="partial"
        title="基础设施投影仅部分可用"
        :description="topology?.provider_detail || 'Provider 返回了部分或截断结果。'"
      />
      <WorkspaceState
        v-if="staleProjection"
        kind="stale"
        :description="`当前投影采集于 ${formatTime(collectedAt)}，不再声明为实时事实。`"
      />
    </div>

    <section
      v-if="loading && !topology && !resourcePage && !topologyError && !resourceError"
      class="resource-loading-stage"
      role="status"
      aria-label="正在读取 Kubernetes 资源"
    >
      <header>
        <span aria-hidden="true"><UIcon name="i-lucide-loader-circle" /></span>
        <div>
          <h2>正在读取 Kubernetes 资源</h2>
          <p>当前 Scope 的资源列表与 topology 投影正在同步。</p>
        </div>
      </header>
      <div class="resource-loading-stage__body">
        <div class="resource-loading-stage__rows">
          <div
            v-for="index in 4"
            :key="index"
          >
            <USkeleton class="resource-loading-stage__kind" />
            <span>
              <USkeleton class="resource-loading-stage__title" />
              <USkeleton class="resource-loading-stage__meta" />
            </span>
            <USkeleton class="resource-loading-stage__state" />
          </div>
        </div>
        <aside aria-hidden="true">
          <USkeleton class="resource-loading-stage__relation" />
          <USkeleton class="resource-loading-stage__relation is-short" />
          <USkeleton class="resource-loading-stage__relation" />
        </aside>
      </div>
    </section>

    <WorkspaceState
      v-else-if="providerState && !providerReady"
      kind="error"
      :title="`Kubernetes Provider ${providerStateLabel()}`"
      :description="`${topology?.provider_detail || '当前 Provider 没有返回可用资源。'} 未填充演示资源。`"
    >
      <template #actions>
        <UButton
          color="error"
          variant="soft"
          icon="i-lucide-settings"
          label="检查 Provider 设置"
          to="/settings#providers"
        />
      </template>
    </WorkspaceState>

    <section
      v-else
      ref="resourceList"
      class="resource-list-shell"
      :aria-busy="loading"
    >
      <header class="resource-list-heading">
        <div>
          <span class="infrastructure-eyebrow">Attention first</span>
          <h2>资源工作队列</h2>
        </div>
        <div class="resource-list-facts">
          <span>{{ visibleResources.length }} / {{ resources.length }} resources</span>
          <span>{{ topology?.edges.length ?? 0 }} edges</span>
          <time
            v-if="collectedAt"
            :datetime="collectedAt"
          >{{ formatTime(collectedAt) }}</time>
        </div>
      </header>
      <WorkspaceStatusRow
        v-if="loading && resources.length"
        title="正在刷新资源投影"
        description="保留当前列表，完成后按健康状态重新排序。"
        tone="info"
        busy
      />
      <WorkspaceDenseList
        :items="visibleResources"
        :item-key="(resource: ResourceRow) => resource.id"
        label="基础设施资源工作队列"
        :selected-key="selectedResourceID"
        empty="当前 Scope 与筛选条件下没有真实资源"
        :severity="severity"
        @select="selectResource"
      >
        <template #leading="{ item }">
          <UBadge
            color="neutral"
            variant="outline"
            :label="item.kind"
          />
        </template>
        <template #title="{ item }">
          {{ item.name }}
        </template>
        <template #description="{ item }">
          {{ item.namespace || "cluster-scoped" }} · {{ item.kind }} · {{ resourceReadyLabel(item) }}
        </template>
        <template #meta="{ item }">
          {{ item.status || item.node_name || "状态未报告" }}
        </template>
        <template #trailing="{ item }">
          <span
            class="resource-health-conclusion"
            :class="{ 'is-healthy': item.health.state === 'healthy' }"
          >
            <small>{{ item.health.summary }}</small>
            <UBadge
              :color="healthColors[item.health.state]"
              variant="subtle"
              :label="healthLabels[item.health.state]"
            />
          </span>
          <UIcon
            name="i-lucide-chevron-right"
            aria-hidden="true"
          />
        </template>
      </WorkspaceDenseList>
      <div
        v-if="resourcePage?.next_cursor"
        class="resource-pagination"
      >
        <span>当前页结束，下一 Cursor 可用</span>
        <UButton
          color="neutral"
          variant="outline"
          trailing-icon="i-lucide-arrow-right"
          label="加载下一页"
          @click="openNextPage"
        />
      </div>
      <details
        v-if="projectionIssues.length"
        class="projection-issues"
      >
        <summary>{{ projectionIssues.length }} 个 Provider issue</summary>
        <ul>
          <li
            v-for="issue in projectionIssues"
            :key="`${issue.namespace}:${issue.operation}:${issue.code}`"
          >
            <strong>{{ issue.code }}</strong>
            <span>{{ issue.namespace || "cluster" }} · {{ issue.operation }}</span>
            <p>{{ issue.detail }}</p>
          </li>
        </ul>
      </details>
    </section>

    <WorkspaceInspector
      :open="Boolean(selectedResourceID)"
      :title="selectedResource?.name || '资源 Inspector'"
      :description="selectedResource?.id || selectedResourceID"
      :target-state="inspectorTargetState"
      :target-description="inspectorTargetDescription"
      :trigger="inspector.triggerElement.value"
      @update:open="handleInspectorOpen"
    >
      <WorkspaceState
        v-if="detailLoading"
        kind="loading"
        title="正在读取资源详情"
        description="资源身份、结构事实与 Kubernetes Events 正在并行读取。"
      />

      <ApiErrorNotice
        v-else-if="detailError && inspectorTargetState === 'ready'"
        :error="detailError"
        title="资源详情读取失败"
        retryable
        @retry="refreshWorkspace"
      />

      <WorkspaceState
        v-else-if="!selectedResource"
        kind="empty"
        title="资源详情尚不可用"
        description="列表与当前 Query 保持不变。"
      />

      <template v-else>
        <div class="resource-inspector-status">
          <UBadge
            color="neutral"
            variant="outline"
            :label="selectedResource.kind"
          />
          <UBadge
            :color="healthColors[selectedResource.health.state]"
            variant="subtle"
            :label="healthLabels[selectedResource.health.state]"
          />
          <UBadge
            v-if="detail?.partial"
            color="warning"
            variant="soft"
            label="Partial"
          />
          <UBadge
            v-if="detail?.freshness.state === 'stale'"
            color="neutral"
            variant="soft"
            label="Stale"
          />
        </div>

        <dl class="resource-facts">
          <div>
            <dt>Namespace</dt><dd class="mono-text">
              {{ selectedResource.namespace || "cluster-scoped" }}
            </dd>
          </div>
          <div>
            <dt>API Version</dt><dd class="mono-text">
              {{ selectedResource.api_version }}
            </dd>
          </div>
          <div><dt>状态</dt><dd>{{ selectedResource.status || "未报告" }}</dd></div>
          <div><dt>健康摘要</dt><dd>{{ selectedResource.health.summary }}</dd></div>
          <div>
            <dt>Node</dt><dd class="mono-text">
              {{ selectedResource.node_name || "未绑定" }}
            </dd>
          </div>
          <div><dt>创建时间</dt><dd>{{ formatTime(selectedResource.created_at) }}</dd></div>
        </dl>

        <section
          v-if="selectedResource.workload"
          class="inspector-section"
        >
          <h3>Workload 状态</h3>
          <dl class="workload-facts">
            <div><dt>Desired</dt><dd>{{ selectedResource.workload.desired_replicas }}</dd></div>
            <div><dt>Updated</dt><dd>{{ selectedResource.workload.updated_replicas }}</dd></div>
            <div><dt>Ready</dt><dd>{{ selectedResource.workload.ready_replicas }}</dd></div>
            <div><dt>Available</dt><dd>{{ selectedResource.workload.available_replicas }}</dd></div>
          </dl>
        </section>

        <section class="inspector-section">
          <h3>真实拓扑关系</h3>
          <ul
            v-if="relatedResources.length"
            class="relationship-list"
          >
            <li
              v-for="resource in relatedResources"
              :key="resource.id"
            >
              <UButton
                color="neutral"
                variant="ghost"
                block
                class="relationship-button"
                @click="selectResourceByID(resource.id)"
              >
                <span>{{ resource.kind }}</span>
                <strong>{{ resource.name }}</strong>
                <small>{{ resource.namespace || "cluster" }}</small>
              </UButton>
            </li>
          </ul>
          <p
            v-else
            class="inspector-empty"
          >
            当前投影没有相邻资源。
          </p>
          <ul
            v-if="selectedEdges.length"
            class="edge-list"
          >
            <li
              v-for="edge in selectedEdges"
              :key="edge.id"
            >
              <UButton
                color="neutral"
                variant="link"
                :label="`${relationLabel(edge.relation)} · ${edgePeer(edge)?.kind || '资源'} / ${edgePeer(edge)?.name || '不可用'}`"
                @click="selectResourceByID(edgePeer(edge)?.id)"
              />
              <small>{{ edge.source_fact }}</small>
            </li>
          </ul>
        </section>

        <section
          v-if="contextLinks.length"
          class="inspector-section"
        >
          <h3>继续调查</h3>
          <nav
            class="context-links"
            aria-label="资源 Context Links"
          >
            <UButton
              v-for="item in contextLinks"
              :key="`${item.link.label}:${item.link.href}`"
              color="neutral"
              variant="outline"
              :icon="contextLinkIcon(item.link)"
              :label="item.link.label"
              :to="item.location || undefined"
              :disabled="!item.location"
            />
          </nav>
        </section>

        <section
          v-if="selectedResource.conditions.length"
          class="inspector-section"
        >
          <h3>Conditions</h3>
          <ul class="condition-list">
            <li
              v-for="condition in selectedResource.conditions"
              :key="`${condition.type}:${condition.last_transition_time}`"
            >
              <strong>{{ condition.type }} · {{ condition.status }}</strong>
              <span>{{ condition.reason || "无 reason" }}</span>
              <p v-if="condition.message">
                {{ condition.message }}
              </p>
              <time
                v-if="condition.last_transition_time"
                :datetime="condition.last_transition_time"
              >{{ formatTime(condition.last_transition_time) }}</time>
            </li>
          </ul>
        </section>

        <section
          class="inspector-section"
          data-testid="resource-events"
        >
          <h3>近期 Kubernetes Events</h3>
          <ApiErrorNotice
            v-if="eventError"
            :error="eventError"
            title="Event 读取失败"
            retryable
            @retry="refreshWorkspace"
          />
          <WorkspaceState
            v-else-if="events?.partial || events?.truncated"
            kind="partial"
            title="Event 仅部分可用"
            description="可用 Event 保持显示。"
          />
          <ol
            v-if="events?.items.length"
            class="event-list"
          >
            <li
              v-for="event in events.items"
              :key="event.id"
              :class="{ 'is-warning': event.type === 'Warning' }"
            >
              <div>
                <strong>{{ event.reason || event.type || "Event" }}</strong>
                <time :datetime="event.observed_at">{{ formatTime(event.observed_at) }}</time>
              </div>
              <p>{{ event.message || "Event 未提供 message" }}</p>
              <span class="mono-text">{{ event.resource_kind }} / {{ event.resource_name }}</span>
            </li>
          </ol>
          <p
            v-else-if="!eventError"
            class="inspector-empty"
          >
            当前时间范围没有 Event。
          </p>
        </section>

        <WorkspaceTechnicalDetails
          :fields="resourceTechnicalFields"
          description="完整资源身份、版本、标签、端点与端口"
        >
          <dl class="technical-resource-groups">
            <div>
              <dt>Labels</dt><dd class="mono-text">
                {{ JSON.stringify(selectedResource.labels) }}
              </dd>
            </div>
            <div>
              <dt>Selector</dt><dd class="mono-text">
                {{ JSON.stringify(selectedResource.selector) }}
              </dd>
            </div>
            <div>
              <dt>Endpoints</dt><dd class="mono-text">
                {{ JSON.stringify(selectedResource.endpoints) }}
              </dd>
            </div>
            <div>
              <dt>Ports</dt><dd class="mono-text">
                {{ JSON.stringify(selectedResource.ports) }}
              </dd>
            </div>
            <div>
              <dt>Addresses</dt><dd class="mono-text">
                {{ selectedResource.addresses.join(", ") || "未提供" }}
              </dd>
            </div>
          </dl>
        </WorkspaceTechnicalDetails>
      </template>

      <template #footer>
        <span class="inspector-time">{{ formatTime(detail?.collected_at || collectedAt) }}</span>
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-x"
          label="关闭"
          @click="closeInspector"
        />
      </template>
    </WorkspaceInspector>
  </article>
</template>

<style scoped>
.infrastructure-view {
  display: grid;
  width: min(100%, var(--co-content-max-width));
  min-width: 0;
  margin: 0 auto;
  gap: var(--co-space-4);
}

.infrastructure-header {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(280px, 1fr) minmax(220px, auto) auto;
  align-items: center;
  gap: var(--co-space-4);
  padding-bottom: var(--co-space-3);
}

.infrastructure-heading { min-width: 0; }
.infrastructure-heading h1,
.resource-list-heading h2 { margin: 0; }
.infrastructure-heading h1 { font-size: 24px; line-height: 1.3; }
.infrastructure-heading p {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: var(--co-space-2);
  margin: var(--co-space-1) 0 0;
  color: var(--co-text-muted);
  font-size: 11px;
}
.infrastructure-heading p span:not(:last-child)::after { margin-left: var(--co-space-2); content: "·"; }
.infrastructure-eyebrow {
  display: block;
  margin-bottom: var(--co-space-1);
  color: var(--co-text-muted);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0;
  text-transform: uppercase;
}

.projection-identity {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: flex-end;
  gap: var(--co-space-2);
}
.projection-identity > span {
  min-width: 0;
  max-width: 260px;
  overflow: hidden;
  color: var(--co-text-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 10px;
}
.infrastructure-actions,
.filter-actions {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: flex-end;
  gap: var(--co-space-2);
}

.resource-posture {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(260px, 1fr) minmax(460px, 1.25fr);
  align-items: stretch;
  overflow: hidden;
  gap: 0;
  padding: var(--co-space-2) var(--co-space-3);
  border-radius: var(--co-radius-frame);
  background: var(--co-bg-surface);
}
.posture-copy {
  display: grid;
  min-width: 0;
  align-content: center;
  gap: 2px;
  padding: var(--co-space-3) var(--co-space-4);
}
.posture-copy strong { font-size: 15px; line-height: 1.35; }
.posture-copy small { color: var(--co-text-muted); font-size: 11px; }
.posture-metrics {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(4, minmax(92px, 1fr));
  gap: var(--co-space-2);
}
.posture-metric {
  min-width: 0;
  min-height: 66px;
  border: 1px solid transparent;
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-subtle);
}
.posture-metric :deep(span) { display: grid; min-width: 0; justify-items: start; gap: 2px; }
.posture-metric span { color: var(--co-text-muted); font-size: 10px; }
.posture-metric strong { color: var(--co-text-primary); font-size: 21px; font-weight: 800; font-variant-numeric: tabular-nums; }
.posture-metric.is-active { border-color: var(--co-border-default); background: color-mix(in srgb, var(--co-bg-active) 76%, var(--co-bg-surface)); box-shadow: none; }

.resource-controls {
  display: grid;
  min-width: 0;
  gap: var(--co-space-2);
  padding: var(--co-space-2);
  border-radius: var(--co-radius-frame);
  background: color-mix(in srgb, var(--co-bg-surface) 78%, transparent);
}
.resource-type-tabs { min-width: 0; padding: 2px; overflow-x: auto; border: 1px solid var(--co-border-subtle); border-radius: var(--co-radius-control); background: var(--co-bg-canvas); }
.resource-type-tabs :deep(button) { min-height: 38px; padding-inline: 12px; border-radius: var(--co-radius-control); }
.resource-type-tabs :deep(svg),
.resource-filter-row :deep(svg) { width: 16px; height: 16px; }
.resource-filter-row {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(220px, 300px) minmax(320px, 1fr) auto;
  align-items: center;
  gap: var(--co-space-3);
  padding: 0;
}
.namespace-select,
.resource-search { min-width: 0; }
.resource-search {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) auto;
  gap: var(--co-space-2);
}
.resource-filter-row :deep(input),
.resource-filter-row :deep([role="combobox"]),
.resource-filter-row :deep(button) { min-height: 38px; border-radius: var(--co-radius-control); }
.resource-search > :deep(button),
.filter-actions :deep(button) { padding-inline: 12px; }
.advanced-filters {
  display: grid;
  width: min(360px, calc(100vw - 32px));
  gap: var(--co-space-3);
  padding: var(--co-space-3);
}
.advanced-filters p { margin: 0; color: var(--co-text-muted); font-size: 10px; line-height: 1.45; }

.projection-states { display: grid; min-width: 0; gap: var(--co-space-2); }
.resource-loading-stage { display: grid; min-height: 260px; gap: var(--co-space-4); padding: var(--co-space-4); border: 1px solid var(--co-border-subtle); border-radius: var(--co-radius-frame); background: color-mix(in srgb, var(--co-bg-surface) 82%, var(--co-bg-canvas)); }
.resource-loading-stage > header { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); }
.resource-loading-stage > header > span { display: grid; width: 38px; height: 38px; flex: 0 0 auto; place-items: center; border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-surface); }
.resource-loading-stage > header svg { animation: resource-loading-spin var(--co-spinner-duration) linear infinite; }
.resource-loading-stage h2 { margin: 0; font-size: 15px; }
.resource-loading-stage p { margin: 2px 0 0; color: var(--co-text-muted); font-size: 11px; }
.resource-loading-stage__body { display: grid; min-width: 0; grid-template-columns: minmax(0, 1.5fr) minmax(220px, .5fr); gap: var(--co-space-4); }
.resource-loading-stage__rows { display: grid; gap: var(--co-space-2); }
.resource-loading-stage__rows > div { display: grid; min-width: 0; grid-template-columns: 72px minmax(0, 1fr) 82px; align-items: center; gap: var(--co-space-3); padding: var(--co-space-3); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.resource-loading-stage__rows > div > span { display: grid; min-width: 0; gap: var(--co-space-2); }
.resource-loading-stage__kind { width: 62px; height: 24px; border-radius: var(--co-radius-control); }
.resource-loading-stage__title { width: min(260px, 72%); height: 12px; }
.resource-loading-stage__meta { width: min(390px, 88%); height: 9px; }
.resource-loading-stage__state { width: 72px; height: 24px; border-radius: var(--co-radius-pill); }
.resource-loading-stage aside { display: grid; align-content: center; gap: var(--co-space-3); padding: var(--co-space-4); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.resource-loading-stage__relation { width: 100%; height: 30px; border-radius: var(--co-radius-control); }
.resource-loading-stage__relation.is-short { width: 68%; justify-self: end; }
@keyframes resource-loading-spin { to { transform: rotate(360deg); } }
.resource-list-shell {
  min-width: 0;
  overflow: visible;
}
.resource-list-shell :deep(.workspace-dense-list) { margin-top: var(--co-space-2); }
.resource-list-shell :deep(.workspace-dense-list-item--critical + .workspace-dense-list-item--neutral),
.resource-list-shell :deep(.workspace-dense-list-item--warning + .workspace-dense-list-item--neutral),
.resource-list-shell :deep(.workspace-dense-list-item--info + .workspace-dense-list-item--neutral) { margin-top: var(--co-space-1); }
.resource-list-shell :deep(.workspace-dense-list-item--neutral .workspace-dense-list-meta) { color: var(--co-text-muted); opacity: .72; }
.resource-health-conclusion { display: grid; min-width: 0; justify-items: end; gap: 3px; }
.resource-health-conclusion small { max-width: 28ch; overflow: hidden; color: var(--co-text-secondary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.resource-health-conclusion.is-healthy { opacity: .68; }
.resource-list-heading {
  display: flex;
  min-width: 0;
  min-height: 58px;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
  padding: var(--co-space-3) var(--co-space-4);
  border-radius: var(--co-radius-frame);
  background: color-mix(in srgb, var(--co-bg-surface) 74%, transparent);
}
.resource-list-heading h2 { font-size: 16px; line-height: 1.35; }
.resource-list-facts {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: var(--co-space-3);
  color: var(--co-text-muted);
  font-family: var(--co-font-mono);
  font-size: 10px;
  font-variant-numeric: tabular-nums;
}
.resource-pagination {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
  margin-top: var(--co-space-2);
  padding: var(--co-space-2) var(--co-space-3);
  border-radius: var(--co-radius-overlay);
  background: var(--co-bg-surface);
  color: var(--co-text-muted);
  font-size: 10px;
}
.projection-issues { margin-top: var(--co-space-2); overflow: hidden; border-radius: var(--co-radius-overlay); background: var(--co-bg-surface); }
.projection-issues summary {
  padding: var(--co-space-3);
  color: var(--co-status-warning-fg);
  cursor: pointer;
  font-weight: 700;
}
.projection-issues ul { display: grid; margin: 0; padding: 0 var(--co-space-3) var(--co-space-3); gap: var(--co-space-2); list-style: none; }
.projection-issues li { display: grid; grid-template-columns: 120px minmax(160px, 0.5fr) minmax(260px, 1fr); gap: var(--co-space-2); font-size: 11px; }
.projection-issues p { margin: 0; overflow-wrap: anywhere; }

.resource-inspector-status { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-2); }
.resource-facts,
.workload-facts { display: grid; min-width: 0; margin: 0; gap: var(--co-space-2); }
.resource-facts { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.workload-facts { grid-template-columns: repeat(4, minmax(0, 1fr)); }
.resource-facts div,
.workload-facts div {
  min-width: 0;
  padding: var(--co-space-2);
  border-left: var(--co-severity-marker-width) solid var(--co-border-strong);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-subtle);
}
.resource-facts dt,
.workload-facts dt { color: var(--co-text-muted); font-size: 10px; }
.resource-facts dd,
.workload-facts dd { min-width: 0; margin: var(--co-space-1) 0 0; overflow-wrap: anywhere; font-size: 12px; }
.workload-facts dd { font-size: 16px; font-weight: 700; font-variant-numeric: tabular-nums; }

.inspector-section { display: grid; min-width: 0; gap: var(--co-space-2); }
.inspector-section h3 { margin: 0; font-size: 13px; line-height: 1.35; }
.relationship-list,
.edge-list,
.condition-list,
.event-list { display: grid; min-width: 0; margin: 0; padding: 0; gap: var(--co-space-1); list-style: none; }
.relationship-button {
  display: grid;
  min-width: 0;
  grid-template-columns: 92px minmax(0, 1fr);
  justify-items: start;
  text-align: left;
}
.relationship-list strong,
.relationship-list small { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.relationship-list small { grid-column: 2; color: var(--co-text-muted); }
.edge-list li { display: grid; min-width: 0; }
.edge-list small { padding: 0 var(--co-space-3) var(--co-space-1); color: var(--co-text-muted); overflow-wrap: anywhere; }
.context-links { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-2); }
.condition-list li,
.event-list li {
  display: grid;
  min-width: 0;
  gap: var(--co-space-1);
  padding: var(--co-space-2) var(--co-space-3);
  border-left: var(--co-severity-marker-width) solid var(--co-border-strong);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-subtle);
}
.event-list li.is-warning { border-left-color: var(--co-status-warning-fg); }
.condition-list span,
.condition-list time,
.event-list span,
.event-list time { color: var(--co-text-muted); font-size: 10px; }
.condition-list p,
.event-list p { margin: 0; overflow-wrap: anywhere; font-size: 12px; }
.event-list li > div { display: flex; min-width: 0; justify-content: space-between; gap: var(--co-space-2); }
.inspector-empty { margin: 0; color: var(--co-text-muted); font-size: 12px; }
.inspector-time { margin-right: auto; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; }
.technical-resource-groups { display: grid; min-width: 0; margin: 0; gap: var(--co-space-2); }
.technical-resource-groups div { display: grid; min-width: 0; gap: 2px; }
.technical-resource-groups dt { color: var(--co-text-muted); font-size: 10px; }
.technical-resource-groups dd { min-width: 0; margin: 0; overflow-wrap: anywhere; font-size: 10px; }

@media (max-width: 1100px) {
  .infrastructure-header { grid-template-columns: minmax(240px, 1fr) auto; }
  .projection-identity { grid-row: 2; justify-content: flex-start; }
  .infrastructure-actions { grid-column: 2; grid-row: 1 / 3; }
  .resource-posture { grid-template-columns: minmax(240px, .8fr) minmax(420px, 1.2fr); }
  .resource-filter-row { grid-template-columns: minmax(190px, 260px) minmax(260px, 1fr) auto; }
}

@media (max-width: 760px) {
  .infrastructure-header { grid-template-columns: minmax(0, 1fr) auto; }
  .projection-identity { grid-column: 1 / -1; }
  .infrastructure-actions { grid-column: 2; grid-row: 1; }
  .infrastructure-actions > :deep(a) { display: none; }
  .resource-posture { grid-template-columns: minmax(0, 1fr); }
  .posture-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .resource-filter-row { grid-template-columns: minmax(0, 1fr) auto; }
  .namespace-select { grid-column: 1 / -1; }
  .resource-search { grid-template-columns: minmax(0, 1fr) auto; }
  .resource-facts { grid-template-columns: minmax(0, 1fr); }
  .workload-facts { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .projection-issues li { grid-template-columns: minmax(0, 1fr); }
  .resource-list-heading { align-items: flex-start; flex-direction: column; }
  .resource-list-facts { justify-content: flex-start; }
  .resource-health-conclusion small { max-width: 18ch; }
  .resource-loading-stage__body { grid-template-columns: minmax(0, 1fr); }
  .resource-loading-stage aside { display: none; }
}

@media (max-width: 520px) {
  .resource-filter-row { grid-template-columns: minmax(0, 1fr); }
  .namespace-select,
  .resource-search,
  .filter-actions { grid-column: 1; }
  .filter-actions { justify-content: flex-start; }
}

@media (prefers-reduced-motion: reduce) {
  .posture-metric { transition: none; }
  .resource-loading-stage > header svg { animation: none; }
}
</style>
