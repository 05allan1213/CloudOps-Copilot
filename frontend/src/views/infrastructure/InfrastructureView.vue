<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  Box,
  Boxes,
  Clock3,
  Network,
  RefreshCw,
  Search,
  Server,
  TriangleAlert,
} from "lucide-vue-next";
import { useRoute, useRouter } from "vue-router";

import { isApiError } from "../../api/client";
import {
  getResource,
  getResourceEvents,
  getResources,
  getTopology,
  type EventPage,
  type InfrastructureQuery,
  type KubernetesResource,
  type ResourceDetail,
  type ResourcePage,
  type TopologyEdge,
  type TopologySnapshot,
} from "../../api/infrastructure";
import { getBootstrap, type BootstrapSnapshot } from "../../api/platform";
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
const topology = ref<TopologySnapshot | null>(null);
const resourcePage = ref<ResourcePage | null>(null);
const detail = ref<ResourceDetail | null>(null);
const events = ref<EventPage | null>(null);
const loading = ref(false);
const detailLoading = ref(false);
const pageError = ref<RequestFailure | null>(null);
const detailError = ref<RequestFailure | null>(null);
const eventError = ref<RequestFailure | null>(null);
const namespaceValue = ref(queryValue(route.query.namespace));
const kindValue = ref(queryValue(route.query.kind));
const searchValue = ref(queryValue(route.query.search));
let controller: AbortController | undefined;
let requestToken = 0;
const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "medium" });

const selectedResourceID = computed(() => queryValue(route.query.resource));
const namespaces = computed(() => bootstrap.value?.active_scope.namespaces ?? topology.value?.scope.namespaces ?? []);
const availableKinds = computed(() => [...new Set((topology.value?.nodes ?? []).map((item) => item.kind))].sort());
const resources = computed(() => resourcePage.value?.items ?? []);
const providerReady = computed(() => topology.value?.provider_state === "available" || topology.value?.provider_state === "partial");
const selectedResource = computed(() => detail.value?.resource ?? null);
const relatedResources = computed(() => [...(detail.value?.related ?? [])].sort((left, right) => {
  const layerOrder = ["workload", "pod", "service", "gateway", "namespace", "node"];
  return layerOrder.indexOf(left.layer) - layerOrder.indexOf(right.layer) || left.name.localeCompare(right.name);
}));
const selectedEdges = computed(() => detail.value?.edges ?? []);
const resourceByID = computed(() => {
  const values = new Map<string, KubernetesResource>();
  for (const resource of topology.value?.nodes ?? []) values.set(resource.id, resource);
  return values;
});

function queryValue(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === "string" ? value[0] : "";
  return typeof value === "string" ? value : "";
}

function currentQuery(): InfrastructureQuery {
  const kind = queryValue(route.query.kind);
  return {
    cluster: queryValue(route.query.cluster) || undefined,
    namespace: queryValue(route.query.namespace) || undefined,
    kind: kind ? [kind] : undefined,
    search: queryValue(route.query.search) || undefined,
    from: queryValue(route.query.from) || undefined,
    to: queryValue(route.query.to) || undefined,
    limit: 500,
  };
}

function normalizeFailure(error: unknown, fallback: string): RequestFailure {
  if (isApiError(error)) {
    return {
      message: error.message,
      code: error.code || "REQUEST_FAILED",
      requestID: error.requestID,
      traceID: error.traceID,
    };
  }
  return { message: error instanceof Error ? error.message : fallback, code: "REQUEST_FAILED", requestID: "", traceID: "" };
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

async function loadWorkspace() {
  if (!ensureTimeRange()) return;
  const token = ++requestToken;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  detailLoading.value = Boolean(selectedResourceID.value);
  pageError.value = null;
  detailError.value = null;
  eventError.value = null;
  detail.value = null;
  events.value = null;
  const query = currentQuery();
  try {
    const [nextBootstrap, nextTopology, nextPage] = await Promise.all([
      getBootstrap(controller.signal),
      getTopology(query, controller.signal),
      getResources(query, controller.signal),
    ]);
    if (token !== requestToken) return;
    bootstrap.value = nextBootstrap;
    topology.value = nextTopology;
    resourcePage.value = nextPage;

    if (!queryValue(route.query.cluster) && nextBootstrap.active_scope.cluster_id) {
      void router.replace({ query: { ...route.query, cluster: nextBootstrap.active_scope.cluster_id } });
      return;
    }
    if (!selectedResourceID.value || (nextTopology.provider_state !== "available" && nextTopology.provider_state !== "partial")) return;

    const [nextDetail, nextEvents] = await Promise.allSettled([
      getResource(selectedResourceID.value, query, controller.signal),
      getResourceEvents(selectedResourceID.value, query, controller.signal),
    ]);
    if (token !== requestToken) return;
    if (nextDetail.status === "fulfilled") detail.value = nextDetail.value;
    else detailError.value = normalizeFailure(nextDetail.reason, "资源详情读取失败。");
    if (nextEvents.status === "fulfilled") events.value = nextEvents.value;
    else eventError.value = normalizeFailure(nextEvents.reason, "资源 Event 读取失败。");
  } catch (error) {
    if (controller.signal.aborted || token !== requestToken) return;
    pageError.value = normalizeFailure(error, "基础设施投影读取失败。");
  } finally {
    if (token === requestToken) {
      loading.value = false;
      detailLoading.value = false;
    }
  }
}

function pushQuery(changes: Record<string, string | undefined>) {
  const query = { ...route.query };
  for (const [key, value] of Object.entries(changes)) {
    if (value) query[key] = value;
    else delete query[key];
  }
  delete query.cursor;
  void router.push({ name: "infrastructure", query });
}

function applyNamespace() {
  pushQuery({ namespace: namespaceValue.value || undefined, resource: undefined });
}

function applyFilters() {
  pushQuery({
    kind: kindValue.value || undefined,
    search: searchValue.value.trim() || undefined,
    resource: undefined,
  });
}

function selectResource(resource: KubernetesResource) {
  const changes: Record<string, string | undefined> = { resource: resource.id };
  if (resource.namespace) changes.namespace = resource.namespace;
  pushQuery(changes);
}

function selectResourceID(id: string | undefined) {
  if (!id) return;
  const resource = resourceByID.value.get(id);
  if (resource) selectResource(resource);
  else pushQuery({ resource: id });
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

function formatTime(value?: string): string {
  if (!value) return "—";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : dateFormatter.format(parsed);
}

watch(() => route.fullPath, () => {
  namespaceValue.value = queryValue(route.query.namespace);
  kindValue.value = queryValue(route.query.kind);
  searchValue.value = queryValue(route.query.search);
  void loadWorkspace();
}, { immediate: true });

function handleOperationalScopeChanged() {
  topology.value = null;
  resourcePage.value = null;
  detail.value = null;
  events.value = null;
  void loadWorkspace();
}

onMounted(() => window.addEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged));
onBeforeUnmount(() => {
  window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged);
  requestToken++;
  controller?.abort();
});
</script>

<template>
  <article class="infrastructure-view" data-testid="infrastructure-workspace">
    <header class="workspace-header">
      <div>
        <p class="eyebrow">Infrastructure Workspace</p>
        <h1>基础设施资源</h1>
        <p>{{ bootstrap?.active_scope.cluster_id || "活动集群读取中" }} · {{ namespaces.join(", ") || "无 Namespace" }}</p>
      </div>
      <button type="button" :disabled="loading" @click="loadWorkspace">
        <RefreshCw :size="17" aria-hidden="true" />
        {{ loading ? "正在刷新" : "刷新当前投影" }}
      </button>
    </header>

    <section class="scope-bar" aria-label="Operational Scope 与资源筛选">
      <div class="scope-identity">
        <Server :size="18" aria-hidden="true" />
        <span>活动集群</span>
        <strong class="mono-text">{{ bootstrap?.active_scope.cluster_id || topology?.source.cluster_id || "读取中" }}</strong>
      </div>
      <label>
        <span>Namespace</span>
        <select v-model="namespaceValue" name="resource-namespace" autocomplete="off" data-testid="namespace-selector" @change="applyNamespace">
          <option value="">全部作用域 Namespace</option>
          <option v-for="namespace in namespaces" :key="namespace" :value="namespace">{{ namespace }}</option>
        </select>
      </label>
      <form class="filters" role="search" @submit.prevent="applyFilters">
        <label>
          <span>Kind</span>
          <select v-model="kindValue" name="resource-kind" autocomplete="off">
            <option value="">全部 Kind</option>
            <option v-for="kind in availableKinds" :key="kind" :value="kind">{{ kind }}</option>
          </select>
        </label>
        <label class="search-field">
          <span>资源搜索</span>
          <span class="search-input"><Search :size="16" aria-hidden="true" /><input v-model="searchValue" name="resource-search" type="search" autocomplete="off" aria-label="资源搜索" placeholder="名称、Kind 或 Namespace…" /></span>
        </label>
        <button type="submit">应用筛选</button>
      </form>
    </section>

    <section v-if="pageError" class="request-state is-error" role="alert">
      <TriangleAlert :size="22" aria-hidden="true" />
      <div>
        <strong>基础设施 API 不可用</strong>
        <p>{{ pageError.message }}</p>
        <small class="mono-text">{{ pageError.code }}<template v-if="pageError.requestID"> · Request {{ pageError.requestID }}</template><template v-if="pageError.traceID"> · Trace {{ pageError.traceID }}</template></small>
      </div>
      <button type="button" @click="loadWorkspace">重试</button>
    </section>

    <section v-else-if="topology && !providerReady" class="request-state is-unavailable" role="status" data-testid="provider-unavailable-state">
      <TriangleAlert :size="22" aria-hidden="true" />
      <div>
        <strong>Kubernetes Provider {{ topology.provider_state === "disabled" ? "已停用" : "不可用" }}</strong>
        <p>{{ topology.provider_detail }}。当前投影包含 0 个真实资源，不会填充演示拓扑。</p>
        <small class="mono-text">{{ topology.source.identity }} · {{ formatTime(topology.collected_at) }}</small>
      </div>
      <RouterLink to="/settings#providers">检查 Provider 设置</RouterLink>
    </section>

    <section v-else class="workspace-grid" :aria-busy="loading">
      <section class="resource-browser" aria-labelledby="resource-list-title">
        <header>
          <div>
            <p class="eyebrow">Typed resources</p>
            <h2 id="resource-list-title">资源浏览器</h2>
          </div>
          <output aria-live="polite">{{ resources.length }} 个资源</output>
        </header>
        <div v-if="loading && !resourcePage" class="loading-state" role="status">正在读取当前 Kubernetes API…</div>
        <ul v-else-if="resources.length" class="resource-list" data-testid="resource-list">
          <li v-for="resource in resources" :key="resource.id">
            <button
              type="button"
              :class="{ 'is-selected': resource.id === selectedResourceID }"
              :data-resource-id="resource.id"
              @click="selectResource(resource)"
            >
              <span class="kind-mark" :class="`is-${resource.layer}`"><Box :size="16" aria-hidden="true" />{{ resource.kind }}</span>
              <span class="resource-name"><strong>{{ resource.name }}</strong><small class="mono-text">{{ resource.namespace || "cluster" }}</small></span>
              <span class="health-chip" :class="`is-${resource.health.state}`">{{ resource.health.state }}</span>
              <small class="resource-summary">{{ resource.health.summary }}</small>
            </button>
          </li>
        </ul>
        <div v-else class="empty-state">
          <Boxes :size="28" aria-hidden="true" />
          <strong>没有符合筛选条件的真实资源</strong>
          <p>当前过滤条件未匹配资源。</p>
        </div>
      </section>

      <aside class="resource-inspector" aria-labelledby="resource-detail-title">
        <div v-if="detailLoading" class="loading-state" role="status">正在读取资源详情与 Event…</div>
        <section v-else-if="detailError" class="inspector-state" role="alert">
          <TriangleAlert :size="24" aria-hidden="true" />
          <h2 id="resource-detail-title">资源详情不可用</h2>
          <p>{{ detailError.message }}</p>
          <small class="mono-text">{{ detailError.code }}<template v-if="detailError.requestID"> · Request {{ detailError.requestID }}</template></small>
        </section>
        <section v-else-if="!selectedResource" class="inspector-state">
          <Network :size="26" aria-hidden="true" />
          <h2 id="resource-detail-title">选择一个资源</h2>
          <p>详情、真实关系和近期 Kubernetes Event 会在这里保持同一 URL 上下文。</p>
        </section>
        <div v-else class="detail-content" data-testid="resource-detail" :data-resource-id="selectedResource.id">
          <header class="detail-header">
            <div>
              <span class="kind-mark" :class="`is-${selectedResource.layer}`">{{ selectedResource.kind }}</span>
              <h2 id="resource-detail-title">{{ selectedResource.name }}</h2>
              <p class="mono-text">{{ selectedResource.namespace || "cluster-scoped" }} · {{ selectedResource.api_version }}</p>
            </div>
            <span class="health-chip" :class="`is-${selectedResource.health.state}`">{{ selectedResource.health.state }}</span>
          </header>

          <dl class="identity-grid">
            <div><dt>状态</dt><dd>{{ selectedResource.status || "未报告" }}</dd></div>
            <div><dt>健康摘要</dt><dd>{{ selectedResource.health.summary }}</dd></div>
            <div><dt>创建时间</dt><dd>{{ formatTime(selectedResource.created_at) }}</dd></div>
            <div><dt>调度 Node</dt><dd class="mono-text">{{ selectedResource.node_name || "—" }}</dd></div>
          </dl>

          <section class="detail-section" aria-labelledby="related-title">
            <header><Network :size="17" aria-hidden="true" /><h3 id="related-title">真实拓扑关系</h3></header>
            <ul v-if="relatedResources.length" class="related-list">
              <li v-for="resource in relatedResources" :key="resource.id">
                <button type="button" :data-related-resource-id="resource.id" @click="selectResource(resource)">
                  <span>{{ resource.kind }}</span><strong>{{ resource.name }}</strong><small>{{ resource.namespace || "cluster" }}</small>
                </button>
              </li>
            </ul>
            <p v-else class="section-empty">当前结构事实没有相邻资源。</p>
            <ul v-if="selectedEdges.length" class="edge-list">
              <li v-for="edge in selectedEdges" :key="edge.id">
                <button type="button" @click="selectResourceID(edgePeer(edge)?.id)">{{ relationLabel(edge.relation) }} · {{ edgePeer(edge)?.kind }} / {{ edgePeer(edge)?.name }}</button>
                <small>{{ edge.source_fact }}</small>
              </li>
            </ul>
          </section>

          <section v-if="selectedResource.owner_references.length" class="detail-section">
            <header><Boxes :size="17" aria-hidden="true" /><h3>Owner References</h3></header>
            <ul class="compact-list">
              <li v-for="owner in selectedResource.owner_references" :key="`${owner.kind}:${owner.namespace}:${owner.name}`">
                <button v-if="owner.id" type="button" @click="selectResourceID(owner.id)">{{ owner.kind }} / {{ owner.name }}</button>
                <span v-else>{{ owner.kind }} / {{ owner.name }}</span>
              </li>
            </ul>
          </section>

          <section v-if="Object.keys(selectedResource.selector).length || Object.keys(selectedResource.labels).length" class="detail-section split-section">
            <div v-if="Object.keys(selectedResource.selector).length">
              <h3>Selectors</h3>
              <ul class="tag-list"><li v-for="(value, key) in selectedResource.selector" :key="key" class="mono-text">{{ key }}={{ value }}</li></ul>
            </div>
            <div v-if="Object.keys(selectedResource.labels).length">
              <h3>Labels</h3>
              <ul class="tag-list"><li v-for="(value, key) in selectedResource.labels" :key="key" class="mono-text">{{ key }}={{ value }}</li></ul>
            </div>
          </section>

          <section v-if="selectedResource.ports.length || selectedResource.endpoints.length || selectedResource.addresses.length" class="detail-section split-section">
            <div v-if="selectedResource.ports.length">
              <h3>Ports</h3>
              <ul class="compact-list"><li v-for="port in selectedResource.ports" :key="`${port.name}:${port.protocol}:${port.port}`"><span class="mono-text">{{ port.name || "port" }} · {{ port.protocol }} {{ port.port }}<template v-if="port.target_port"> → {{ port.target_port }}</template></span></li></ul>
            </div>
            <div v-if="selectedResource.endpoints.length || selectedResource.addresses.length">
              <h3>Endpoints / Addresses</h3>
              <ul class="compact-list">
                <li v-for="endpoint in selectedResource.endpoints" :key="`${endpoint.address}:${endpoint.target_ref}`"><span class="mono-text">{{ endpoint.address }}<template v-if="endpoint.target_ref"> · {{ endpoint.target_ref }}</template></span></li>
                <li v-for="address in selectedResource.addresses" :key="address"><span class="mono-text">{{ address }}</span></li>
              </ul>
            </div>
          </section>

          <section v-if="selectedResource.conditions.length" class="detail-section">
            <h3>Conditions</h3>
            <ul class="condition-list">
              <li v-for="condition in selectedResource.conditions" :key="`${condition.type}:${condition.last_transition_time}`">
                <strong>{{ condition.type }} · {{ condition.status }}</strong>
                <span>{{ condition.reason || "无 reason" }}<template v-if="condition.message"> — {{ condition.message }}</template></span>
                <time v-if="condition.last_transition_time" :datetime="condition.last_transition_time">{{ formatTime(condition.last_transition_time) }}</time>
              </li>
            </ul>
          </section>

          <section class="detail-section" data-testid="resource-events">
            <header><Clock3 :size="17" aria-hidden="true" /><h3>近期 Kubernetes Events</h3></header>
            <div v-if="eventError" class="inline-error" role="status">
              <strong>Event API 不可用</strong><span>{{ eventError.message }}</span><small class="mono-text">{{ eventError.code }}<template v-if="eventError.requestID"> · Request {{ eventError.requestID }}</template></small>
            </div>
            <ol v-else-if="events?.items.length" class="event-list">
              <li v-for="event in events.items" :key="event.id" :class="{ 'is-warning': event.type === 'Warning' }">
                <div><strong>{{ event.reason || event.type || "Event" }}</strong><span>{{ event.resource_kind }} / {{ event.resource_name }}</span></div>
                <p>{{ event.message || "事件未提供 message" }}</p>
                <time :datetime="event.observed_at">{{ formatTime(event.observed_at) }}</time>
              </li>
            </ol>
            <p v-else class="section-empty">当前 typed Event 查询未返回事件。</p>
          </section>
        </div>
      </aside>
    </section>

    <footer v-if="topology" class="projection-footer">
      <span>{{ topology.nodes.length }} nodes · {{ topology.edges.length }} edges</span>
      <span class="mono-text">Snapshot {{ topology.id || "not persisted" }} · {{ topology.content_hash || "no hash" }}</span>
      <time :datetime="topology.collected_at">{{ formatTime(topology.collected_at) }}</time>
    </footer>
  </article>
</template>

<style scoped>
.infrastructure-view { display: grid; width: min(100%, var(--co-content-max-width)); min-width: 0; margin: 0 auto; gap: var(--co-space-4); }
.workspace-header { display: flex; min-width: 0; align-items: flex-end; justify-content: space-between; gap: var(--co-space-5); padding-bottom: var(--co-space-4); border-bottom: 1px solid var(--co-border-default); }
.workspace-header h1, .resource-browser h2, .detail-header h2, .inspector-state h2 { margin: 0; }
.workspace-header h1 { font-size: clamp(25px, 3vw, 32px); }
.workspace-header p:not(.eyebrow) { max-width: 760px; margin: var(--co-space-2) 0 0; color: var(--co-text-secondary); }
.eyebrow { margin: 0 0 var(--co-space-1); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 11px; font-weight: 800; letter-spacing: 0.06em; text-transform: uppercase; }
button, .request-state a { min-height: 42px; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-surface); cursor: pointer; font-weight: 700; }
button:hover, .request-state a:hover { border-color: var(--co-border-strong); background: var(--co-bg-hover); }
button:disabled { cursor: wait; opacity: 0.6; }
.workspace-header > button { display: inline-flex; flex: 0 0 auto; align-items: center; gap: var(--co-space-2); padding: 0 var(--co-space-4); }
.scope-bar { display: grid; min-width: 0; grid-template-columns: minmax(210px, 0.7fr) minmax(180px, 0.6fr) minmax(420px, 1.7fr); align-items: end; gap: var(--co-space-3); padding: var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.scope-identity { display: grid; min-width: 0; grid-template-columns: auto minmax(0, 1fr); gap: 0 var(--co-space-2); padding: var(--co-space-2); }
.scope-identity svg { grid-row: 1 / 3; align-self: center; color: var(--co-action-primary); }
.scope-identity span, label > span { color: var(--co-text-muted); font-size: 11px; font-weight: 750; }
.scope-identity strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
label { display: grid; min-width: 0; gap: var(--co-space-1); }
select, input { width: 100%; min-width: 0; min-height: 42px; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-canvas); }
select { padding: 0 var(--co-space-3); }
.filters { display: grid; min-width: 0; grid-template-columns: minmax(130px, 0.55fr) minmax(200px, 1fr) auto; align-items: end; gap: var(--co-space-2); }
.filters > button { padding: 0 var(--co-space-4); }
.search-input { display: flex; min-width: 0; min-height: 42px; align-items: center; gap: var(--co-space-2); padding: 0 var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-muted); background: var(--co-bg-canvas); }
.search-input:focus-within { border-color: var(--co-focus-ring); box-shadow: 0 0 0 2px color-mix(in srgb, var(--co-focus-ring) 32%, transparent); }
.search-input input { min-height: auto; padding: 0; border: 0; outline: 0; }
.workspace-grid { display: grid; min-width: 0; min-height: 620px; grid-template-columns: minmax(320px, 0.78fr) minmax(0, 1.45fr); overflow: hidden; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.resource-browser, .resource-inspector { min-width: 0; min-height: 0; }
.resource-browser { display: grid; grid-template-rows: auto minmax(0, 1fr); border-right: 1px solid var(--co-border-default); }
.resource-browser > header { display: flex; align-items: end; justify-content: space-between; gap: var(--co-space-3); padding: var(--co-space-4); border-bottom: 1px solid var(--co-border-default); }
.resource-browser h2 { font-size: 18px; }
.resource-browser output { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 11px; }
.resource-list { min-height: 0; margin: 0; padding: 0; overflow-y: auto; overscroll-behavior: contain; list-style: none; }
.resource-list li { content-visibility: auto; contain-intrinsic-size: 78px; }
.resource-list button { display: grid; width: 100%; min-height: 76px; grid-template-columns: minmax(100px, auto) minmax(120px, 1fr) auto; align-items: center; gap: var(--co-space-2); padding: var(--co-space-3) var(--co-space-4); border: 0; border-bottom: 1px solid var(--co-border-default); border-radius: 0; text-align: left; background: transparent; }
.resource-list button.is-selected { background: var(--co-bg-active); box-shadow: inset 3px 0 var(--co-action-primary); }
.kind-mark { display: inline-flex; min-width: 0; align-items: center; gap: 5px; color: var(--co-text-secondary); font-size: 11px; font-weight: 800; }
.kind-mark.is-workload { color: #a99dfd; }
.kind-mark.is-pod { color: var(--co-status-success-fg); }
.kind-mark.is-service { color: #24c7d9; }
.kind-mark.is-node { color: #e7a441; }
.kind-mark.is-gateway { color: #e460a8; }
.resource-name { display: grid; min-width: 0; }
.resource-name strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
.resource-name small { overflow: hidden; color: var(--co-text-muted); text-overflow: ellipsis; white-space: nowrap; }
.resource-summary { grid-column: 2 / -1; color: var(--co-text-secondary); overflow-wrap: anywhere; }
.health-chip { padding: 2px 7px; border: 1px solid var(--co-status-neutral-border); border-radius: var(--co-radius-pill); color: var(--co-status-neutral-fg); background: var(--co-status-neutral-bg); font-size: 10px; font-weight: 850; text-transform: uppercase; }
.health-chip.is-healthy { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.health-chip.is-warning { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.health-chip.is-critical { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.resource-inspector { overflow-y: auto; overscroll-behavior: contain; background: var(--co-bg-canvas); }
.detail-content { display: grid; gap: var(--co-space-3); padding: var(--co-space-4); }
.detail-header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-3); padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.detail-header > div { min-width: 0; }
.detail-header h2 { margin-top: var(--co-space-1); overflow-wrap: anywhere; font-size: 22px; }
.detail-header p { margin: var(--co-space-1) 0 0; color: var(--co-text-muted); overflow-wrap: anywhere; font-size: 11px; }
.identity-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); margin: 0; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); }
.identity-grid div { min-width: 0; padding: var(--co-space-3); border-right: 1px solid var(--co-border-default); border-bottom: 1px solid var(--co-border-default); }
.identity-grid dt { color: var(--co-text-muted); font-size: 11px; }
.identity-grid dd { margin: var(--co-space-1) 0 0; overflow-wrap: anywhere; font-size: 12px; font-weight: 700; }
.detail-section { min-width: 0; padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); background: var(--co-bg-surface); }
.detail-section > header { display: flex; align-items: center; gap: var(--co-space-2); }
.detail-section h3 { margin: 0 0 var(--co-space-3); font-size: 14px; }
.detail-section > header h3 { margin: 0; }
.related-list, .compact-list, .tag-list, .condition-list, .edge-list, .event-list { margin: var(--co-space-3) 0 0; padding: 0; list-style: none; }
.related-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-2); }
.related-list button { display: grid; width: 100%; min-width: 0; min-height: 58px; grid-template-columns: auto minmax(0, 1fr); align-items: center; gap: 1px var(--co-space-2); padding: var(--co-space-2) var(--co-space-3); text-align: left; }
.related-list button span, .related-list button small { color: var(--co-text-muted); font-size: 10px; }
.related-list button strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.related-list button small { grid-column: 2; }
.edge-list { display: grid; gap: var(--co-space-2); padding-top: var(--co-space-3); border-top: 1px solid var(--co-border-default); }
.edge-list li { display: grid; gap: 2px; }
.edge-list button { min-height: 32px; padding: 0; border: 0; color: var(--co-action-primary); text-align: left; background: transparent; }
.edge-list small, .section-empty { color: var(--co-text-muted); overflow-wrap: anywhere; }
.split-section { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-4); }
.tag-list { display: flex; flex-wrap: wrap; gap: var(--co-space-2); }
.tag-list li { max-width: 100%; padding: 3px 7px; overflow-wrap: anywhere; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-subtle); font-size: 10px; }
.compact-list { display: grid; gap: var(--co-space-2); }
.compact-list li { min-width: 0; overflow-wrap: anywhere; font-size: 11px; }
.compact-list button { min-height: 32px; padding: 0; border: 0; color: var(--co-action-primary); text-align: left; background: transparent; }
.condition-list { display: grid; gap: var(--co-space-2); }
.condition-list li { display: grid; gap: 2px; padding: var(--co-space-2); border-left: 2px solid var(--co-border-strong); }
.condition-list strong, .condition-list span { overflow-wrap: anywhere; font-size: 11px; }
.condition-list span, .condition-list time { color: var(--co-text-muted); }
.event-list { display: grid; gap: var(--co-space-2); }
.event-list li { display: grid; gap: var(--co-space-1); padding: var(--co-space-3); border-left: 3px solid var(--co-status-info-border); background: var(--co-bg-subtle); }
.event-list li.is-warning { border-left-color: var(--co-status-warning-border); }
.event-list li > div { display: flex; justify-content: space-between; gap: var(--co-space-2); }
.event-list span, .event-list time { color: var(--co-text-muted); font-size: 10px; }
.event-list p { margin: 0; overflow-wrap: anywhere; font-size: 12px; }
.inline-error { display: grid; gap: var(--co-space-1); margin-top: var(--co-space-3); padding: var(--co-space-3); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.request-state { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-3); min-height: 120px; padding: var(--co-space-5); border: 1px solid var(--co-status-warning-border); border-radius: var(--co-radius-panel); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.request-state.is-error { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.request-state p { margin: var(--co-space-1) 0; overflow-wrap: anywhere; }
.request-state a, .request-state button { display: inline-flex; align-items: center; padding: 0 var(--co-space-4); }
.loading-state, .empty-state, .inspector-state { display: grid; min-height: 220px; place-content: center; justify-items: center; gap: var(--co-space-2); padding: var(--co-space-5); color: var(--co-text-muted); text-align: center; }
.empty-state p, .inspector-state p { max-width: 420px; margin: 0; }
.projection-footer { display: flex; min-width: 0; flex-wrap: wrap; justify-content: space-between; gap: var(--co-space-2); color: var(--co-text-muted); font-size: 10px; }
.projection-footer span { min-width: 0; overflow-wrap: anywhere; }
@media (max-width: 1120px) {
  .scope-bar { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .filters { grid-column: 1 / -1; }
  .workspace-grid { grid-template-columns: minmax(280px, 0.72fr) minmax(0, 1.2fr); }
  .related-list, .split-section { grid-template-columns: 1fr; }
}
@media (max-width: 767px) {
  .workspace-header { align-items: flex-start; flex-direction: column; }
  .workspace-header > button { width: 100%; justify-content: center; }
  .scope-bar, .filters { grid-template-columns: 1fr; }
  .filters { grid-column: auto; }
  .filters > button { width: 100%; }
  select, input { font-size: 16px; }
  .workspace-grid { min-height: 0; grid-template-columns: 1fr; overflow: visible; }
  .resource-browser { max-height: 500px; border-right: 0; border-bottom: 1px solid var(--co-border-default); }
  .resource-list { max-height: 390px; }
  .resource-inspector { overflow: visible; }
  .resource-list button { grid-template-columns: minmax(86px, auto) minmax(0, 1fr) auto; }
  .detail-content { padding: var(--co-space-3); }
  .identity-grid { grid-template-columns: 1fr; }
  .request-state { grid-template-columns: auto minmax(0, 1fr); }
  .request-state a, .request-state button { grid-column: 1 / -1; justify-content: center; }
  .event-list li > div { align-items: flex-start; flex-direction: column; }
}
@media (max-width: 380px) {
  .resource-list button { grid-template-columns: minmax(0, 1fr) auto; }
  .kind-mark { grid-column: 1 / -1; }
  .resource-summary { grid-column: 1 / -1; }
}
</style>
