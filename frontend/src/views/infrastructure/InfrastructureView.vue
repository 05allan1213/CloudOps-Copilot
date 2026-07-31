<script setup lang="ts">
import type { LocationQueryRaw, RouteLocationRaw } from "vue-router";
import { computed, h, onBeforeUnmount, onMounted, ref, resolveComponent, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { apiErrorDetails } from "../../api/client";
import {
  getResource,
  getResourceEvents,
  getResources,
  getTopology,
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
import DenseDataTable, {
  type DenseRowSeverity,
  type DenseTableColumn,
} from "../../components/workspace/DenseDataTable.vue";
import ApiErrorNotice from "../../components/workspace/ApiErrorNotice.vue";
import WorkspaceInspector, {
  type InspectorTargetState,
} from "../../components/workspace/WorkspaceInspector.vue";
import WorkspaceState from "../../components/workspace/WorkspaceState.vue";
import { useWorkspaceInspector } from "../../composables/useWorkspaceInspector";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";
import {
  infrastructureContextLocation,
  infrastructureResourceTypeItems,
  kindsForResourceType,
  queryValues,
  resourceTypeForKinds,
  type InfrastructureResourceType,
} from "./infrastructureModel";

type ResourceRow = KubernetesResource & Record<string, unknown>;

const ALL_NAMESPACES_VALUE = "__all_namespaces__";

interface ContextLinkRow {
  link: InfrastructureContextLink;
  location: RouteLocationRaw | null;
}

const route = useRoute();
const router = useRouter();
const inspector = useWorkspaceInspector({ selectedKey: "resource" });
const UBadge = resolveComponent("UBadge");
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
let controller: AbortController | undefined;
let requestToken = 0;

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
  ...namespaces.value.map((namespace) => ({ label: namespace, value: namespace })),
]);
const namespaceSelectValue = computed(() => namespaceValue.value || ALL_NAMESPACES_VALUE);
const resources = computed<ResourceRow[]>(() => (
  (resourcePage.value?.items ?? topology.value?.nodes ?? []) as ResourceRow[]
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

const columns: DenseTableColumn<ResourceRow>[] = [
  { id: "kind", accessorKey: "kind", header: "Kind", label: "Kind", size: 126 },
  { id: "name", accessorKey: "name", header: "资源", label: "资源", size: 252 },
  { id: "namespace", accessorKey: "namespace", header: "Namespace", label: "Namespace", size: 176 },
  {
    id: "health",
    accessorKey: "health",
    header: "健康",
    label: "健康",
    size: 104,
    cell: ({ row }) => h(UBadge, {
      color: healthColors[row.original.health.state],
      variant: "subtle",
      label: healthLabels[row.original.health.state],
    }),
  },
  { id: "status", accessorKey: "status", header: "状态", label: "状态", size: 148 },
  {
    id: "node",
    accessorKey: "node_name",
    header: "Node",
    label: "Node",
    size: 180,
    optional: true,
  },
  {
    id: "created",
    accessorFn: (row) => formatTime(row.created_at),
    header: "创建时间",
    label: "创建时间",
    size: 190,
    optional: true,
  },
  {
    id: "summary",
    accessorFn: (row) => row.health.summary,
    header: "状态摘要",
    label: "状态摘要",
    size: 320,
    optional: true,
  },
];

function queryValue(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === "string" ? value[0] : "";
  return typeof value === "string" ? value : "";
}

function currentQuery(): InfrastructureQuery {
  const kinds = queryValues(route.query.kind);
  return {
    cluster: queryValue(route.query.cluster) || undefined,
    namespace: queryValue(route.query.namespace) || undefined,
    kind: kinds.length ? kinds : undefined,
    search: queryValue(route.query.search) || undefined,
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
      void router.replace({ query: { ...route.query, cluster: resolvedCluster } });
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

function applySearch() {
  updateQuery({ search: searchValue.value.trim() || undefined, resource: undefined });
}

function resetFilters() {
  namespaceValue.value = "";
  searchValue.value = "";
  resourceType.value = "all";
  updateQuery({ namespace: undefined, kind: undefined, search: undefined, resource: undefined });
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

function severity(resource: ResourceRow): DenseRowSeverity {
  if (resource.health.state === "critical") return "critical";
  if (resource.health.state === "warning") return "warning";
  return resource.health.state === "unknown" ? "info" : "neutral";
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

function copyResource(resource: ResourceRow): string {
  return `${resource.kind}/${resource.namespace || "cluster"}/${resource.name} (${resource.id})`;
}

watch(() => route.fullPath, () => {
  namespaceValue.value = queryValue(route.query.namespace);
  searchValue.value = queryValue(route.query.search);
  resourceType.value = resourceTypeForKinds(queryValues(route.query.kind));
  void loadWorkspace();
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
      <UTooltip text="刷新当前资源投影">
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-refresh-cw"
          square
          aria-label="刷新基础设施资源"
          :loading="loading"
          @click="loadWorkspace(true)"
        />
      </UTooltip>
    </header>

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
        variant="link"
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
            color="primary"
            icon="i-lucide-list-filter"
            label="筛选"
          />
        </form>
        <UTooltip text="清除 Namespace、资源类型与搜索条件">
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
    </section>

    <div class="projection-states">
      <ApiErrorNotice
        v-if="bootstrapError"
        :error="bootstrapError"
        title="Operational Scope 读取失败"
        retryable
        @retry="loadWorkspace(true)"
      />
      <ApiErrorNotice
        v-if="topologyError"
        :error="topologyError"
        title="Topology 投影读取失败"
        retryable
        @retry="loadWorkspace(true)"
      />
      <ApiErrorNotice
        v-if="resourceError"
        :error="resourceError"
        title="资源列表读取失败"
        retryable
        @retry="loadWorkspace(true)"
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

    <WorkspaceState
      v-if="loading && !topology && !resourcePage && !topologyError && !resourceError"
      kind="loading"
      title="正在读取 Kubernetes 资源"
      description="正在获取当前 Scope 的资源列表与 topology 投影。"
    />

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
      class="resource-table-shell"
      :aria-busy="loading"
    >
      <header class="resource-table-heading">
        <div>
          <span class="infrastructure-eyebrow">Typed Kubernetes resources</span>
          <h2>资源清单</h2>
        </div>
        <div class="resource-table-facts">
          <span>{{ topology?.nodes.length ?? resources.length }} nodes</span>
          <span>{{ topology?.edges.length ?? 0 }} edges</span>
          <time
            v-if="collectedAt"
            :datetime="collectedAt"
          >{{ formatTime(collectedAt) }}</time>
        </div>
      </header>
      <DenseDataTable
        :rows="resources"
        :columns="columns"
        :row-key="(resource: ResourceRow) => resource.id"
        storage-key="infrastructure-resources"
        caption="基础设施资源清单"
        :critical-column-ids="['kind', 'name', 'health']"
        :selected-id="selectedResourceID"
        empty="当前 Scope 与筛选条件下没有真实资源"
        :severity="severity"
        :copy-value="copyResource"
        :virtualized="resources.length > 250"
        @select="selectResource"
      />
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
        @retry="loadWorkspace(true)"
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
            <dt>资源 ID</dt><dd class="mono-text">
              {{ selectedResource.id }}
            </dd>
          </div>
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
          <div>
            <dt>Provider</dt><dd class="mono-text">
              {{ detail?.source.identity || providerIdentity }}
            </dd>
          </div>
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
            @retry="loadWorkspace(true)"
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
  padding-bottom: var(--co-space-4);
  border-bottom: 1px solid var(--co-border-default);
}

.infrastructure-heading { min-width: 0; }
.infrastructure-heading h1,
.resource-table-heading h2 { margin: 0; }
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

.resource-controls {
  display: grid;
  min-width: 0;
  border-block: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}
.resource-type-tabs { min-width: 0; overflow-x: auto; border-bottom: 1px solid var(--co-border-subtle); }
.resource-filter-row {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(220px, 300px) minmax(320px, 1fr) auto;
  align-items: center;
  gap: var(--co-space-3);
  padding: var(--co-space-3);
}
.namespace-select,
.resource-search { min-width: 0; }
.resource-search {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) auto;
  gap: var(--co-space-2);
}

.projection-states { display: grid; min-width: 0; gap: var(--co-space-2); }
.resource-table-shell {
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-surface);
}
.resource-table-heading {
  display: flex;
  min-width: 0;
  min-height: 58px;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
  padding: var(--co-space-2) var(--co-space-3);
  border-bottom: 1px solid var(--co-border-default);
}
.resource-table-heading h2 { font-size: 16px; line-height: 1.35; }
.resource-table-facts {
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
.projection-issues { border-top: 1px solid var(--co-border-default); }
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

@media (max-width: 1100px) {
  .infrastructure-header { grid-template-columns: minmax(240px, 1fr) auto; }
  .projection-identity { grid-row: 2; justify-content: flex-start; }
  .infrastructure-header > :last-child { grid-column: 2; grid-row: 1 / 3; }
  .resource-filter-row { grid-template-columns: minmax(190px, 260px) minmax(260px, 1fr) auto; }
}

@media (max-width: 760px) {
  .infrastructure-header { grid-template-columns: minmax(0, 1fr) auto; }
  .projection-identity { grid-column: 1 / -1; }
  .resource-filter-row { grid-template-columns: minmax(0, 1fr) auto; }
  .namespace-select { grid-column: 1 / -1; }
  .resource-search { grid-template-columns: minmax(0, 1fr) auto; }
  .resource-facts { grid-template-columns: minmax(0, 1fr); }
  .workload-facts { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .projection-issues li { grid-template-columns: minmax(0, 1fr); }
}
</style>
