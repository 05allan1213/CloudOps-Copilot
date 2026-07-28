<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  ArrowRight,
  Boxes,
  ListTree,
  LocateFixed,
  Network,
  RefreshCw,
  Settings,
  TriangleAlert,
} from "lucide-vue-next";
import { useRoute, useRouter } from "vue-router";

import { isApiError } from "../../api/client";
import type { KubernetesResource, ResourceLayer, TopologyEdge } from "../../api/infrastructure";
import { getOverview, type OverviewSnapshot } from "../../api/platform";
import OperationsAtlas from "../../components/infrastructure/OperationsAtlas.vue";
import StructuredResourceView from "../../components/infrastructure/StructuredResourceView.vue";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";

interface RequestFailure {
  message: string;
  code: string;
  requestID: string;
  traceID: string;
}

const route = useRoute();
const router = useRouter();
const snapshot = ref<OverviewSnapshot | null>(null);
const loading = ref(true);
const failure = ref<RequestFailure | null>(null);
const webglFailure = ref("");
let controller: AbortController | undefined;
const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "medium" });

const atlas = computed(() => snapshot.value?.atlas ?? null);
const selectedID = computed(() => queryValue(route.query.resource));
const selectedResource = computed(() => atlas.value?.nodes.find((item) => item.id === selectedID.value) ?? null);
const providerReady = computed(() => atlas.value?.provider_state === "available" || atlas.value?.provider_state === "partial");
const canvasAvailable = computed(() => providerReady.value && Boolean(atlas.value?.nodes.length));
const viewMode = computed<"canvas" | "structured">(() => {
  if (!canvasAvailable.value || webglFailure.value || queryValue(route.query.view) === "structured") return "structured";
  return "canvas";
});
const relationshipRows = computed(() => {
  if (!atlas.value || !selectedResource.value) return [];
  return atlas.value.edges
    .filter((edge) => edge.source_id === selectedResource.value?.id || edge.target_id === selectedResource.value?.id)
    .map((edge) => ({ edge, peer: atlas.value?.nodes.find((item) => item.id === (edge.source_id === selectedResource.value?.id ? edge.target_id : edge.source_id)) }))
    .filter((item): item is { edge: TopologyEdge; peer: KubernetesResource } => Boolean(item.peer));
});
const layerCounts = computed(() => {
  const counts: Record<ResourceLayer, number> = { namespace: 0, service: 0, workload: 0, pod: 0, node: 0, gateway: 0 };
  for (const resource of atlas.value?.nodes ?? []) counts[resource.layer]++;
  return counts;
});
const infrastructureTarget = computed(() => {
  const resource = selectedResource.value;
  const link = resource?.links.find((item) => item.kind === "internal" && item.availability === "available" && item.href.startsWith("/infrastructure?"));
  const query: Record<string, string> = {
    cluster: atlas.value?.scope.cluster_id ?? atlas.value?.source.cluster_id ?? "",
    resource: resource?.id ?? "",
  };
  if (resource?.namespace) query.namespace = resource.namespace;
  if (link?.from) query.from = link.from;
  if (link?.to) query.to = link.to;
  return { name: "infrastructure", query };
});

const layerLabels: Record<ResourceLayer, string> = {
  namespace: "Namespace",
  service: "Service",
  workload: "Workload",
  pod: "Pod",
  node: "Node",
  gateway: "Ingress / Gateway",
};

function queryValue(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === "string" ? value[0] : "";
  return typeof value === "string" ? value : "";
}

function formatTime(value?: string): string {
  if (!value) return "—";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : dateFormatter.format(parsed);
}

function providerStateLabel(): string {
  return ({
    available: "可用",
    partial: "部分可用",
    unavailable: "不可用",
    disabled: "已停用",
  } as const)[atlas.value?.provider_state ?? "unavailable"];
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

async function loadOverview() {
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  failure.value = null;
  try {
    snapshot.value = await getOverview(controller.signal);
  } catch (error) {
    if (controller.signal.aborted) return;
    if (isApiError(error)) {
      failure.value = {
        message: error.message,
        code: error.code || "REQUEST_FAILED",
        requestID: error.requestID,
        traceID: error.traceID,
      };
    } else {
      failure.value = { message: error instanceof Error ? error.message : "Overview API 读取失败。", code: "REQUEST_FAILED", requestID: "", traceID: "" };
    }
  } finally {
    if (!controller.signal.aborted) loading.value = false;
  }
}

function setView(mode: "canvas" | "structured") {
  if (mode === "canvas" && !canvasAvailable.value) return;
  const query = { ...route.query };
  if (mode === "structured") query.view = "structured";
  else {
    delete query.view;
    webglFailure.value = "";
  }
  void router.push({ name: "overview", query });
}

function selectResource(resource: KubernetesResource) {
  void router.push({ name: "overview", query: { ...route.query, resource: resource.id } });
}

function handleWebGLUnavailable(reason: string) {
  webglFailure.value = reason || "浏览器未能初始化 WebGL。";
}

function handleOperationalScopeChanged() {
  snapshot.value = null;
  webglFailure.value = "";
  void loadOverview();
}

onMounted(() => {
  window.addEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged);
  void loadOverview();
});
onBeforeUnmount(() => {
  window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged);
  controller?.abort();
});
</script>

<template>
  <article class="overview-atlas" data-testid="overview-operations-atlas">
    <header class="atlas-toolbar">
      <div class="atlas-heading">
        <p class="eyebrow">Live Operations Atlas</p>
        <h1>集群运行图谱</h1>
        <p v-if="atlas">{{ atlas.scope.cluster_id }} · {{ atlas.scope.namespaces.join(", ") || "无 Namespace" }}</p>
        <p v-else>从当前活动 Operational Scope 读取。</p>
      </div>
      <div v-if="atlas" class="atlas-facts" aria-label="Atlas 投影摘要">
        <span class="provider-state" :class="`is-${atlas.provider_state}`">Kubernetes {{ providerStateLabel() }}</span>
        <span>{{ atlas.nodes.length }} nodes</span>
        <span>{{ atlas.edges.length }} edges</span>
        <time :datetime="atlas.collected_at">{{ formatTime(atlas.collected_at) }}</time>
      </div>
      <div class="toolbar-actions">
        <div class="view-switch" aria-label="Atlas 视图">
          <button type="button" :aria-pressed="viewMode === 'canvas'" :disabled="!canvasAvailable" @click="setView('canvas')"><LocateFixed :size="16" aria-hidden="true" />Canvas</button>
          <button type="button" :aria-pressed="viewMode === 'structured'" @click="setView('structured')"><ListTree :size="16" aria-hidden="true" />结构化</button>
        </div>
        <button class="refresh-button" type="button" :disabled="loading" aria-label="刷新 Operations Atlas" @click="loadOverview"><RefreshCw :size="17" aria-hidden="true" /></button>
      </div>
    </header>

    <section class="atlas-stage">
      <div class="atlas-surface" :class="{ 'is-structured': viewMode === 'structured' }">
        <div v-if="loading && !snapshot" class="surface-state" role="status">
          <RefreshCw class="loading-icon" :size="26" aria-hidden="true" />
          <strong>正在读取当前集群投影</strong>
          <span>Kubernetes typed reader</span>
        </div>

        <div v-else-if="failure" class="surface-state is-error" role="alert">
          <TriangleAlert :size="28" aria-hidden="true" />
          <strong>Overview API 不可用</strong>
          <p>{{ failure.message }}</p>
          <small class="mono-text">{{ failure.code }}<template v-if="failure.requestID"> · Request {{ failure.requestID }}</template><template v-if="failure.traceID"> · Trace {{ failure.traceID }}</template></small>
          <button type="button" @click="loadOverview">重试</button>
        </div>

        <template v-else-if="atlas">
          <div v-if="!providerReady" class="structured-shell">
            <div class="provider-notice" role="status" data-testid="provider-unavailable-state">
              <TriangleAlert :size="20" aria-hidden="true" />
              <div>
                <strong>Kubernetes Provider {{ providerStateLabel() }}</strong>
                <p>{{ atlas.provider_detail }}。Canvas 未创建，且没有注入装饰性节点。</p>
              </div>
              <RouterLink to="/settings#providers"><Settings :size="16" aria-hidden="true" />检查设置</RouterLink>
            </div>
            <StructuredResourceView :resources="atlas.nodes" :selected-id="selectedID" @select="selectResource" />
          </div>

          <div v-else-if="!atlas.nodes.length" class="structured-shell">
            <div class="provider-notice is-empty" role="status">
              <Boxes :size="20" aria-hidden="true" />
              <div><strong>当前 Scope 没有资源节点</strong><p>这是 typed reader 的真实空结果；Canvas 未创建。</p></div>
            </div>
            <StructuredResourceView :resources="atlas.nodes" :selected-id="selectedID" @select="selectResource" />
          </div>

          <div v-else-if="viewMode === 'structured'" class="structured-shell">
            <div v-if="webglFailure" class="provider-notice" role="status" data-testid="webgl-fallback-state">
              <TriangleAlert :size="20" aria-hidden="true" />
              <div><strong>已切换到结构化 fallback</strong><p>WebGL 初始化失败：{{ webglFailure }}</p></div>
            </div>
            <StructuredResourceView :resources="atlas.nodes" :selected-id="selectedID" @select="selectResource" />
          </div>

          <template v-else>
            <OperationsAtlas :snapshot="atlas" :selected-id="selectedID" @select="selectResource" @unavailable="handleWebGLUnavailable" />
            <div class="canvas-context" aria-hidden="true">
              <span class="mono-text">{{ atlas.source.identity }}</span>
              <span>{{ atlas.freshness.state }} · Snapshot {{ atlas.id || "not persisted" }}</span>
            </div>
          </template>
        </template>
      </div>

      <aside class="atlas-inspector" aria-labelledby="atlas-inspector-title">
        <template v-if="selectedResource">
          <header>
            <span class="resource-layer" :class="`is-${selectedResource.layer}`">{{ selectedResource.kind }}</span>
            <span class="health-state" :class="`is-${selectedResource.health.state}`">{{ selectedResource.health.state }}</span>
            <h2 id="atlas-inspector-title">{{ selectedResource.name }}</h2>
            <p class="mono-text">{{ selectedResource.namespace || "cluster-scoped" }} · {{ selectedResource.api_version }}</p>
          </header>
          <dl class="resource-facts">
            <div><dt>状态</dt><dd>{{ selectedResource.status || "未报告" }}</dd></div>
            <div><dt>健康</dt><dd>{{ selectedResource.health.summary }}</dd></div>
            <div><dt>Node</dt><dd class="mono-text">{{ selectedResource.node_name || "—" }}</dd></div>
            <div><dt>邻接关系</dt><dd>{{ relationshipRows.length }}</dd></div>
          </dl>
          <section class="relationship-section">
            <h3>结构事实</h3>
            <ul v-if="relationshipRows.length">
              <li v-for="item in relationshipRows" :key="item.edge.id">
                <button type="button" @click="selectResource(item.peer)">
                  <span>{{ relationLabel(item.edge.relation) }}</span>
                  <strong>{{ item.peer.kind }} / {{ item.peer.name }}</strong>
                  <small>{{ item.edge.source_fact }}</small>
                </button>
              </li>
            </ul>
            <p v-else>当前投影没有与该资源相连的结构事实。</p>
          </section>
          <RouterLink class="open-resource" data-testid="open-infrastructure-resource" :to="infrastructureTarget">
            在基础设施中打开<ArrowRight :size="17" aria-hidden="true" />
          </RouterLink>
        </template>

        <template v-else>
          <header>
            <Network :size="24" aria-hidden="true" />
            <h2 id="atlas-inspector-title">当前集群投影</h2>
            <p class="mono-text">{{ atlas?.source.identity || "Kubernetes source unavailable" }}</p>
          </header>
          <dl v-if="atlas" class="projection-facts">
            <div><dt>Provider</dt><dd>{{ providerStateLabel() }}</dd></div>
            <div><dt>Freshness</dt><dd>{{ atlas.freshness.state }} · {{ atlas.freshness.age_seconds }}s</dd></div>
            <div><dt>Revision</dt><dd class="mono-text">{{ atlas.configuration_revision_id }}</dd></div>
            <div><dt>Snapshot</dt><dd class="mono-text">{{ atlas.id || "not persisted" }}</dd></div>
            <div><dt>Content hash</dt><dd class="mono-text">{{ atlas.content_hash || "not available" }}</dd></div>
          </dl>
          <section v-if="atlas?.nodes.length" class="layer-legend" aria-label="真实资源层计数">
            <h3>资源层</h3>
            <ul>
              <li v-for="(label, layer) in layerLabels" :key="layer" :class="`is-${layer}`"><span>{{ label }}</span><strong>{{ layerCounts[layer] }}</strong></li>
            </ul>
          </section>
          <p v-if="atlas?.issues.length" class="issue-summary"><TriangleAlert :size="16" aria-hidden="true" />{{ atlas.issues.length }} 个 Provider issue；在基础设施工作区查看范围化结果。</p>
          <p v-else-if="canvasAvailable" class="selection-help">Atlas 投影已就绪。</p>
          <RouterLink class="open-resource" to="/infrastructure">打开基础设施<ArrowRight :size="17" aria-hidden="true" /></RouterLink>
        </template>
      </aside>
    </section>
  </article>
</template>

<style scoped>
.overview-atlas { display: grid; width: 100%; height: 100%; min-width: 0; min-height: 0; grid-template-rows: auto minmax(0, 1fr); overflow: hidden; color: var(--co-text-primary); background: var(--co-bg-canvas); }
.atlas-toolbar { display: grid; min-width: 0; grid-template-columns: minmax(260px, 1fr) auto auto; align-items: center; gap: var(--co-space-4); min-height: 86px; padding: var(--co-space-3) var(--co-space-5); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.atlas-heading { min-width: 0; }
.atlas-heading h1 { margin: 0; font-size: clamp(22px, 2.4vw, 29px); line-height: 1.15; }
.atlas-heading > p:last-child { margin: var(--co-space-1) 0 0; overflow: hidden; color: var(--co-text-muted); text-overflow: ellipsis; white-space: nowrap; font-size: 11px; }
.eyebrow { margin: 0 0 2px; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; font-weight: 850; letter-spacing: 0.07em; text-transform: uppercase; }
.atlas-facts { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; }
.atlas-facts > span:not(.provider-state), .atlas-facts time { padding-left: var(--co-space-2); border-left: 1px solid var(--co-border-default); }
.provider-state { padding: 3px 8px; border: 1px solid var(--co-status-neutral-border); border-radius: var(--co-radius-pill); color: var(--co-status-neutral-fg); background: var(--co-status-neutral-bg); font-family: var(--co-font-sans); font-weight: 800; }
.provider-state.is-available { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.provider-state.is-partial, .provider-state.is-unavailable { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.toolbar-actions { display: flex; align-items: center; gap: var(--co-space-2); }
.view-switch { display: inline-flex; padding: 3px; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); background: var(--co-bg-canvas); }
.view-switch button, .refresh-button { display: inline-flex; min-height: 38px; align-items: center; justify-content: center; gap: 6px; border: 0; border-radius: 3px; color: var(--co-text-secondary); background: transparent; cursor: pointer; font-size: 11px; font-weight: 800; }
.view-switch button { padding: 0 var(--co-space-3); }
.view-switch button[aria-pressed="true"] { color: var(--co-text-primary); background: var(--co-bg-active); box-shadow: inset 0 0 0 1px var(--co-status-info-border); }
.view-switch button:hover, .refresh-button:hover { color: var(--co-text-primary); background: var(--co-bg-hover); }
.view-switch button:disabled, .refresh-button:disabled { cursor: not-allowed; opacity: 0.45; }
.refresh-button { width: 42px; border: 1px solid var(--co-border-default); }
.atlas-stage { display: grid; min-width: 0; min-height: 0; grid-template-columns: minmax(0, 1fr) 332px; overflow: hidden; }
.atlas-surface { position: relative; min-width: 0; min-height: 0; overflow: hidden; background: #090c10; }
.atlas-surface.is-structured { background: var(--co-bg-canvas); }
.canvas-context { position: absolute; right: max(var(--co-space-3), env(safe-area-inset-right)); bottom: max(var(--co-space-3), env(safe-area-inset-bottom)); left: max(var(--co-space-3), env(safe-area-inset-left)); display: flex; min-width: 0; justify-content: space-between; gap: var(--co-space-3); padding: var(--co-space-2) var(--co-space-3); color: #aab7c4; background: rgb(9 12 16 / 82%); font-size: 10px; pointer-events: none; }
.canvas-context span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.structured-shell { display: grid; width: 100%; height: 100%; min-height: 0; grid-template-rows: auto minmax(0, 1fr); }
.structured-shell > :only-child { grid-row: 1 / -1; }
.provider-notice { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-3); padding: var(--co-space-3) var(--co-space-4); border-bottom: 1px solid var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.provider-notice.is-empty { border-color: var(--co-status-neutral-border); color: var(--co-status-neutral-fg); background: var(--co-status-neutral-bg); }
.provider-notice p { margin: 2px 0 0; color: inherit; opacity: 0.86; font-size: 11px; }
.provider-notice a { display: inline-flex; min-height: 40px; align-items: center; gap: var(--co-space-2); padding: 0 var(--co-space-3); border: 1px solid currentcolor; border-radius: var(--co-radius-control); font-size: 11px; font-weight: 800; }
.surface-state { display: grid; width: 100%; height: 100%; min-height: 280px; place-content: center; justify-items: center; gap: var(--co-space-2); padding: var(--co-space-6); color: #aab7c4; text-align: center; background: #090c10; }
.surface-state span, .surface-state p { max-width: 520px; margin: 0; color: #7d8a97; }
.surface-state.is-error { color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.surface-state.is-error p { color: inherit; }
.surface-state button { min-height: 42px; margin-top: var(--co-space-2); padding: 0 var(--co-space-4); border: 1px solid currentcolor; border-radius: var(--co-radius-control); color: inherit; background: transparent; cursor: pointer; font-weight: 800; }
.loading-icon { animation: spin 1.2s linear infinite; }
.atlas-inspector { min-width: 0; min-height: 0; padding: var(--co-space-5); overflow-y: auto; overscroll-behavior: contain; border-left: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.atlas-inspector > header { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-2); padding-bottom: var(--co-space-4); border-bottom: 1px solid var(--co-border-default); }
.atlas-inspector > header > svg { grid-column: 1 / -1; color: var(--co-action-primary); }
.atlas-inspector h2 { grid-column: 1 / -1; margin: 0; overflow-wrap: anywhere; font-size: 20px; }
.atlas-inspector header p { grid-column: 1 / -1; margin: 0; color: var(--co-text-muted); overflow-wrap: anywhere; font-size: 10px; }
.resource-layer, .health-state { padding: 3px 7px; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-pill); color: var(--co-text-secondary); font-size: 10px; font-weight: 850; text-transform: uppercase; }
.resource-layer.is-workload { border-color: #8b7cf6; color: #a99dfd; }
.resource-layer.is-pod { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); }
.resource-layer.is-service { border-color: #24c7d9; color: #24c7d9; }
.resource-layer.is-node { border-color: #e7a441; color: #e7a441; }
.resource-layer.is-gateway { border-color: #e460a8; color: #e460a8; }
.health-state { justify-self: end; }
.health-state.is-healthy { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.health-state.is-warning { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.health-state.is-critical { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.resource-facts, .projection-facts { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); margin: var(--co-space-4) 0 0; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); }
.resource-facts div, .projection-facts div { min-width: 0; padding: var(--co-space-3); border-right: 1px solid var(--co-border-default); border-bottom: 1px solid var(--co-border-default); }
.resource-facts dt, .projection-facts dt { color: var(--co-text-muted); font-size: 10px; }
.resource-facts dd, .projection-facts dd { margin: var(--co-space-1) 0 0; overflow-wrap: anywhere; font-size: 11px; font-weight: 750; }
.projection-facts { grid-template-columns: 1fr; }
.relationship-section, .layer-legend { margin-top: var(--co-space-5); }
.relationship-section h3, .layer-legend h3 { margin: 0 0 var(--co-space-2); font-size: 13px; }
.relationship-section ul, .layer-legend ul { display: grid; gap: var(--co-space-2); margin: 0; padding: 0; list-style: none; }
.relationship-section li button { display: grid; width: 100%; min-height: 58px; gap: 2px; padding: var(--co-space-2) var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); text-align: left; background: var(--co-bg-canvas); cursor: pointer; }
.relationship-section li button:hover { border-color: var(--co-border-strong); background: var(--co-bg-hover); }
.relationship-section span, .relationship-section small { color: var(--co-text-muted); overflow-wrap: anywhere; font-size: 10px; }
.relationship-section strong { overflow-wrap: anywhere; font-size: 11px; }
.relationship-section > p, .selection-help { color: var(--co-text-muted); font-size: 11px; }
.layer-legend li { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-3); padding: var(--co-space-2) var(--co-space-3); border-left: 3px solid var(--co-border-strong); color: var(--co-text-secondary); background: var(--co-bg-subtle); font-size: 11px; }
.layer-legend li.is-workload { border-color: #8b7cf6; }
.layer-legend li.is-pod { border-color: #52d273; }
.layer-legend li.is-service { border-color: #24c7d9; }
.layer-legend li.is-node { border-color: #e7a441; }
.layer-legend li.is-gateway { border-color: #e460a8; }
.issue-summary { display: flex; align-items: flex-start; gap: var(--co-space-2); color: var(--co-status-warning-fg); font-size: 11px; }
.open-resource { display: flex; width: 100%; min-height: 44px; align-items: center; justify-content: center; gap: var(--co-space-2); margin-top: var(--co-space-5); padding: 0 var(--co-space-4); border: 1px solid var(--co-action-primary); border-radius: var(--co-radius-control); color: var(--co-text-on-action); background: var(--co-action-primary); cursor: pointer; font-weight: 800; }
.open-resource:hover { background: var(--co-action-hover); }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 1100px) {
  .atlas-toolbar { grid-template-columns: minmax(240px, 1fr) auto; }
  .atlas-facts { grid-column: 1 / -1; grid-row: 2; justify-content: flex-start; }
  .toolbar-actions { grid-column: 2; grid-row: 1; }
  .atlas-stage { grid-template-columns: minmax(0, 1fr) 290px; }
}
@media (max-width: 767px) {
  .overview-atlas { height: 100%; overflow-y: auto; overscroll-behavior: contain; }
  .atlas-toolbar { grid-template-columns: minmax(0, 1fr); gap: var(--co-space-3); padding: var(--co-space-3) max(var(--co-space-4), env(safe-area-inset-right)) var(--co-space-3) max(var(--co-space-4), env(safe-area-inset-left)); }
  .atlas-heading > p:last-child { white-space: normal; }
  .atlas-facts { grid-column: 1; grid-row: auto; justify-content: flex-start; }
  .toolbar-actions { grid-column: 1; grid-row: auto; justify-content: space-between; }
  .view-switch { flex: 1; }
  .view-switch button { flex: 1; }
  .atlas-stage { display: grid; min-height: 720px; grid-template-columns: 1fr; grid-template-rows: minmax(360px, 52dvh) auto; overflow: visible; }
  .atlas-surface { min-height: 360px; }
  .atlas-inspector { min-height: 360px; padding-right: max(var(--co-space-5), env(safe-area-inset-right)); padding-bottom: max(var(--co-space-5), env(safe-area-inset-bottom)); padding-left: max(var(--co-space-5), env(safe-area-inset-left)); overflow: visible; border-top: 1px solid var(--co-border-default); border-left: 0; }
  .provider-notice { grid-template-columns: auto minmax(0, 1fr); }
  .provider-notice a { grid-column: 1 / -1; justify-content: center; }
  .canvas-context { align-items: flex-start; flex-direction: column; }
}
@media (max-width: 380px) {
  .atlas-facts time { display: none; }
  .atlas-inspector { padding: var(--co-space-4); }
  .resource-facts { grid-template-columns: 1fr; }
}
@media (prefers-reduced-motion: reduce) { .loading-icon { animation: none; } }
</style>
