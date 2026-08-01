<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import {
  getTopology,
  type InfrastructureQuery,
  type KubernetesResource,
  type ResourceLayer,
  type TopologyEdge,
  type TopologySnapshot,
} from "../../api/infrastructure";
import OperationsAtlas from "../../components/infrastructure/OperationsAtlas.vue";
import StructuredResourceView from "../../components/infrastructure/StructuredResourceView.vue";
import ApiErrorNotice from "../../components/workspace/ApiErrorNotice.vue";
import WorkspaceState from "../../components/workspace/WorkspaceState.vue";
import { useLatestAsync } from "../../composables/useLatestAsync";
import { useWorkspaceInspector } from "../../composables/useWorkspaceInspector";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";
import { resolveResourceSelection } from "../infrastructure/infrastructureModel";

const route = useRoute();
const router = useRouter();
const request = useLatestAsync<TopologySnapshot>();
const webglFailure = ref("");
const inspectorHeading = ref<HTMLElement | null>(null);
const modeControl = ref<HTMLElement | null>(null);
const inspector = useWorkspaceInspector({ selectedKey: "resource" });
const utcFormatter = new Intl.DateTimeFormat("zh-CN", {
  dateStyle: "medium",
  timeStyle: "medium",
  timeZone: "UTC",
});

const atlas = computed(() => request.data.value);
const selectedID = inspector.selectedID;
const selectedResource = computed(() => (
  atlas.value && selectedID.value
    ? resolveResourceSelection(atlas.value.nodes, selectedID.value) ?? null
    : null
));
const selectedTargetInvalid = computed(() => Boolean(selectedID.value && atlas.value && !selectedResource.value));
const providerReady = computed(() => atlas.value?.provider_state === "available" || atlas.value?.provider_state === "partial");
const canvasAvailable = computed(() => providerReady.value && Boolean(atlas.value?.nodes.length) && !webglFailure.value);
const viewMode = computed<"canvas" | "structured">(() => (
  queryValue(route.query.view) === "structured" || !canvasAvailable.value ? "structured" : "canvas"
));
const relationshipRows = computed(() => {
  if (!atlas.value || !selectedResource.value) return [];
  return atlas.value.edges
    .filter((edge) => edge.source_id === selectedResource.value?.id || edge.target_id === selectedResource.value?.id)
    .map((edge) => ({
      edge,
      peer: atlas.value?.nodes.find((item) => item.id === (
        edge.source_id === selectedResource.value?.id ? edge.target_id : edge.source_id
      )),
    }))
    .filter((item): item is { edge: TopologyEdge; peer: KubernetesResource } => Boolean(item.peer));
});
const layerCounts = computed(() => {
  const counts: Record<ResourceLayer, number> = {
    namespace: 0,
    service: 0,
    workload: 0,
    pod: 0,
    node: 0,
    gateway: 0,
  };
  for (const resource of atlas.value?.nodes ?? []) counts[resource.layer] += 1;
  return counts;
});
const infrastructureTarget = computed(() => {
  const resource = selectedResource.value;
  const query: Record<string, string> = {
    cluster: atlas.value?.scope.cluster_id ?? atlas.value?.source.cluster_id ?? "",
    resource: resource?.id ?? "",
  };
  if (resource?.namespace) query.namespace = resource.namespace;
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

function queryValue(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === "string" ? value[0] : "";
  return typeof value === "string" ? value : "";
}

function formatUTC(value?: string): string {
  if (!value) return "未报告";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : `${utcFormatter.format(parsed)} UTC`;
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

async function loadAtlas(background = false) {
  const query: InfrastructureQuery = {
    cluster: queryValue(route.query.cluster) || undefined,
    namespace: queryValue(route.query.namespace) || undefined,
    from: queryValue(route.query.from) || undefined,
    to: queryValue(route.query.to) || undefined,
  };
  await request.run(({ signal }) => getTopology(query, signal), { background });
}

function setView(mode: "canvas" | "structured") {
  if (mode === "canvas" && !providerReady.value) return;
  const query = { ...route.query };
  if (mode === "structured") query.view = "structured";
  else {
    delete query.view;
    webglFailure.value = "";
  }
  void router.push({ name: "atlas", query });
}

function defaultCanvasTrigger(): HTMLElement | null {
  return modeControl.value?.querySelector<HTMLElement>("button") ?? null;
}

function selectResource(resource: KubernetesResource, trigger: HTMLElement | null = null) {
  void inspector.open(resource.id, trigger ?? defaultCanvasTrigger());
}

function closeInspector() {
  void inspector.close();
}

function handleWebGLUnavailable(reason: string) {
  webglFailure.value = reason || "浏览器未能初始化 WebGL。";
}

function handleOperationalScopeChanged() {
  webglFailure.value = "";
  void loadAtlas(true);
}

watch(selectedID, async (value) => {
  if (!value) return;
  await nextTick();
  inspectorHeading.value?.focus({ preventScroll: true });
});

onMounted(() => {
  window.addEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged);
  void loadAtlas();
});
watch(() => route.fullPath, (current, previous) => {
  if (current === previous) return;
  void loadAtlas(Boolean(request.data.value));
});
onBeforeUnmount(() => window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged));
</script>

<template>
  <article
    class="atlas-view"
    data-testid="atlas-workspace"
  >
    <header class="atlas-header">
      <div class="atlas-heading">
        <span class="atlas-eyebrow">Operations Atlas</span>
        <h1 tabindex="-1">
          运行拓扑
        </h1>
        <p v-if="atlas">
          <span class="mono-text">{{ atlas.scope.cluster_id }}</span>
          <span>{{ atlas.scope.namespaces.join(", ") || "无 Namespace" }}</span>
        </p>
        <p v-else>
          从当前 Operational Scope 读取 Kubernetes topology。
        </p>
      </div>

      <div
        v-if="atlas"
        class="atlas-facts"
        aria-label="Atlas 投影摘要"
      >
        <UBadge
          :color="atlas.provider_state === 'available' ? 'success' : atlas.provider_state === 'partial' ? 'warning' : 'error'"
          variant="subtle"
          :label="`Kubernetes ${providerStateLabel()}`"
        />
        <span>{{ atlas.nodes.length }} nodes</span>
        <span>{{ atlas.edges.length }} edges</span>
        <time :datetime="atlas.collected_at">{{ formatUTC(atlas.collected_at) }}</time>
      </div>

      <div class="atlas-actions">
        <div
          ref="modeControl"
          class="atlas-mode-control"
          role="group"
          aria-label="Atlas 视图模式"
        >
          <UButton
            color="neutral"
            :variant="viewMode === 'canvas' ? 'solid' : 'ghost'"
            icon="i-lucide-orbit"
            label="3D"
            :disabled="!providerReady || !atlas?.nodes.length"
            aria-label="切换到 3D Atlas"
            @click="setView('canvas')"
          />
          <UButton
            color="neutral"
            :variant="viewMode === 'structured' ? 'solid' : 'ghost'"
            icon="i-lucide-list-tree"
            label="结构化"
            aria-label="切换到结构化资源视图"
            @click="setView('structured')"
          />
        </div>
        <UTooltip text="刷新当前拓扑投影">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-refresh-cw"
            square
            aria-label="刷新当前拓扑投影"
            :loading="request.refreshing.value"
            :disabled="request.loading.value"
            @click="loadAtlas(true)"
          />
        </UTooltip>
      </div>
    </header>

    <UAlert
      v-if="atlas?.partial"
      class="atlas-partial"
      color="warning"
      variant="soft"
      icon="i-lucide-split"
      title="当前 topology 仅部分可用"
      :description="atlas.provider_detail"
      role="status"
    />

    <section
      class="atlas-stage"
      :class="{ 'has-inspector': Boolean(selectedID) }"
    >
      <div
        class="atlas-surface"
        :class="{ 'is-structured': viewMode === 'structured' }"
      >
        <div
          v-if="request.loading.value && !request.data.value"
          class="atlas-request-state"
        >
          <WorkspaceState
            kind="loading"
            title="正在读取真实拓扑"
            description="从 Overview typed projection 获取当前 Kubernetes Provider 事实。"
          />
        </div>

        <div
          v-else-if="request.error.value && !atlas"
          class="atlas-request-state"
        >
          <ApiErrorNotice
            :error="request.error.value"
            title="Atlas 读取失败"
            retryable
            @retry="loadAtlas()"
          />
        </div>

        <template v-else-if="atlas">
          <div
            v-if="!providerReady"
            class="atlas-state-shell"
          >
            <WorkspaceState
              kind="error"
              :title="`Kubernetes Provider ${providerStateLabel()}`"
              :description="`${atlas.provider_detail}。没有创建装饰性节点。`"
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
            <StructuredResourceView
              :resources="atlas.nodes"
              :selected-id="selectedID"
              @select="selectResource"
            />
          </div>

          <div
            v-else-if="!atlas.nodes.length"
            class="atlas-state-shell"
          >
            <WorkspaceState
              kind="empty"
              title="当前 Scope 没有资源节点"
              description="这是 typed Kubernetes reader 的真实空结果；Canvas 未创建。"
            />
            <StructuredResourceView
              :resources="atlas.nodes"
              :selected-id="selectedID"
              @select="selectResource"
            />
          </div>

          <div
            v-else-if="viewMode === 'structured'"
            class="structured-shell"
          >
            <UAlert
              v-if="webglFailure"
              color="warning"
              variant="soft"
              icon="i-lucide-triangle-alert"
              title="已切换到结构化等价视图"
              :description="webglFailure"
              role="status"
            />
            <StructuredResourceView
              :resources="atlas.nodes"
              :selected-id="selectedID"
              @select="selectResource"
            />
          </div>

          <template v-else>
            <OperationsAtlas
              :snapshot="atlas"
              :selected-id="selectedID"
              @select="selectResource"
              @unavailable="handleWebGLUnavailable"
            />
            <div
              class="canvas-context"
              aria-hidden="true"
            >
              <span class="mono-text">{{ atlas.source.identity }}</span>
              <span>{{ atlas.freshness.state }} · Snapshot {{ atlas.id || "not persisted" }}</span>
            </div>
          </template>
        </template>
      </div>

      <aside
        v-if="selectedID"
        class="atlas-inspector"
        aria-labelledby="atlas-inspector-title"
      >
        <header>
          <div>
            <span class="atlas-eyebrow">Resource Inspector</span>
            <h2
              id="atlas-inspector-title"
              ref="inspectorHeading"
              tabindex="-1"
            >
              {{ selectedResource?.name || "资源目标不可用" }}
            </h2>
            <p class="mono-text">
              {{ selectedResource?.id || selectedID }}
            </p>
          </div>
          <UTooltip text="关闭 Inspector">
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-x"
              square
              aria-label="关闭 Atlas Inspector"
              @click="closeInspector"
            />
          </UTooltip>
        </header>

        <WorkspaceState
          v-if="selectedTargetInvalid"
          kind="invalid"
          :description="`当前 topology 中不存在资源 ${selectedID}。选择保持在 URL 中，未自动替换。`"
        >
          <template #actions>
            <UButton
              color="warning"
              variant="soft"
              icon="i-lucide-x"
              label="清除无效选择"
              @click="closeInspector"
            />
          </template>
        </WorkspaceState>

        <template v-else-if="selectedResource">
          <div class="resource-status-line">
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
            <div><dt>邻接关系</dt><dd>{{ relationshipRows.length }}</dd></div>
          </dl>

          <section class="inspector-section">
            <h3>结构事实</h3>
            <ul v-if="relationshipRows.length">
              <li
                v-for="item in relationshipRows"
                :key="item.edge.id"
              >
                <UButton
                  color="neutral"
                  variant="ghost"
                  block
                  class="relationship-link"
                  @click="selectResource(item.peer)"
                >
                  <span>{{ relationLabel(item.edge.relation) }}</span>
                  <strong>{{ item.peer.kind }} / {{ item.peer.name }}</strong>
                  <small>{{ item.edge.source_fact }}</small>
                </UButton>
              </li>
            </ul>
            <p v-else>
              当前投影没有与该资源相连的结构事实。
            </p>
          </section>

          <UButton
            color="primary"
            icon="i-lucide-arrow-right"
            trailing
            block
            label="在基础设施中打开"
            :to="infrastructureTarget"
          />
        </template>

        <section
          v-if="atlas"
          class="inspector-section projection-section"
        >
          <h3>投影身份</h3>
          <dl>
            <div><dt>Provider</dt><dd>{{ providerStateLabel() }}</dd></div>
            <div>
              <dt>Revision</dt><dd class="mono-text">
                {{ atlas.configuration_revision_id }}
              </dd>
            </div>
            <div>
              <dt>Snapshot</dt><dd class="mono-text">
                {{ atlas.id || "not persisted" }}
              </dd>
            </div>
            <div>
              <dt>Content hash</dt><dd class="mono-text">
                {{ atlas.content_hash || "not available" }}
              </dd>
            </div>
          </dl>
          <ul
            class="layer-counts"
            aria-label="资源层计数"
          >
            <li
              v-for="(label, layer) in layerLabels"
              :key="layer"
            >
              <span>{{ label }}</span><strong>{{ layerCounts[layer] }}</strong>
            </li>
          </ul>
        </section>
      </aside>
    </section>
  </article>
</template>

<style scoped>
.atlas-view {
  display: grid;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  grid-template-rows: auto auto minmax(0, 1fr);
  overflow: hidden;
  color: var(--co-text-primary);
  background: var(--co-bg-canvas);
}

.atlas-header {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(260px, 1fr) auto auto;
  align-items: center;
  gap: var(--co-space-4);
  min-height: 76px;
  padding: var(--co-space-3) var(--co-space-5);
  border-bottom: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}

.atlas-heading { min-width: 0; }
.atlas-heading h1 { margin: 0; font-size: 20px; line-height: 1.3; }
.atlas-heading p {
  display: flex;
  min-width: 0;
  gap: var(--co-space-2);
  margin: var(--co-space-1) 0 0;
  overflow: hidden;
  color: var(--co-text-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
}
.atlas-eyebrow {
  display: block;
  margin-bottom: var(--co-space-1);
  color: var(--co-text-muted);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}
.atlas-facts,
.atlas-actions,
.atlas-mode-control {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: var(--co-space-2);
}
.atlas-facts {
  flex-wrap: wrap;
  justify-content: flex-end;
  color: var(--co-text-muted);
  font-family: var(--co-font-mono);
  font-size: 10px;
}
.atlas-facts > span,
.atlas-facts time { padding-left: var(--co-space-2); border-left: 1px solid var(--co-border-default); }
.atlas-mode-control {
  padding: 2px;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
  background: var(--co-bg-canvas);
}
.atlas-partial { border-radius: 0; }
.atlas-stage {
  display: grid;
  min-width: 0;
  min-height: 0;
  grid-row: 3;
  grid-template-columns: minmax(0, 1fr);
  overflow: hidden;
  transition: grid-template-columns var(--co-motion-standard) var(--co-ease-out);
}
.atlas-stage.has-inspector { grid-template-columns: minmax(0, 1fr) min(460px, 38%); }
.atlas-surface {
  position: relative;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  background: var(--co-bg-canvas);
}
.atlas-surface.is-structured { overflow: auto; }
.atlas-request-state { margin: var(--co-space-4); }
.atlas-state-shell,
.structured-shell {
  display: grid;
  width: 100%;
  min-height: 100%;
  grid-template-rows: auto minmax(0, 1fr);
}
.canvas-context {
  position: absolute;
  right: var(--co-space-3);
  bottom: var(--co-space-3);
  left: var(--co-space-3);
  display: flex;
  min-width: 0;
  justify-content: space-between;
  gap: var(--co-space-3);
  padding: var(--co-space-2) var(--co-space-3);
  border: 1px solid var(--co-border-default);
  color: var(--co-text-secondary);
  background: color-mix(in srgb, var(--co-bg-surface) 88%, transparent);
  font-size: 10px;
  pointer-events: none;
}
.canvas-context span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.atlas-inspector {
  min-width: 0;
  min-height: 0;
  padding: var(--co-space-4);
  overflow-y: auto;
  overscroll-behavior: contain;
  border-left: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}
.atlas-inspector > header {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--co-space-3);
  padding-bottom: var(--co-space-3);
  border-bottom: 1px solid var(--co-border-default);
}
.atlas-inspector > header > div { min-width: 0; }
.atlas-inspector h2 { margin: 0; overflow-wrap: anywhere; font-size: 16px; line-height: 1.35; }
.atlas-inspector header p { margin: var(--co-space-1) 0 0; color: var(--co-text-muted); overflow-wrap: anywhere; font-size: 10px; }
.resource-status-line { display: flex; flex-wrap: wrap; gap: var(--co-space-2); margin-top: var(--co-space-4); }
.resource-facts,
.projection-section dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  margin: var(--co-space-3) 0;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
}
.resource-facts div,
.projection-section dl div { min-width: 0; padding: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.resource-facts div:nth-child(odd),
.projection-section dl div:nth-child(odd) { border-right: 1px solid var(--co-border-default); }
.resource-facts dt,
.projection-section dt { color: var(--co-text-muted); font-size: 10px; }
.resource-facts dd,
.projection-section dd { margin: var(--co-space-1) 0 0; overflow-wrap: anywhere; font-size: 11px; font-weight: 700; }
.inspector-section { margin: var(--co-space-4) 0; }
.inspector-section h3 { margin: 0 0 var(--co-space-2); font-size: 13px; }
.inspector-section > p { color: var(--co-text-muted); font-size: 11px; }
.inspector-section ul { display: grid; gap: var(--co-space-2); margin: 0; padding: 0; list-style: none; }
.relationship-link { display: grid; min-width: 0; grid-template-columns: 1fr; justify-items: start; text-align: left; }
.relationship-link span,
.relationship-link small { color: var(--co-text-muted); overflow-wrap: anywhere; font-size: 10px; }
.relationship-link strong { overflow-wrap: anywhere; font-size: 11px; }
.projection-section { padding-top: var(--co-space-4); border-top: 1px solid var(--co-border-default); }
.layer-counts { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.layer-counts li {
  display: flex;
  justify-content: space-between;
  gap: var(--co-space-2);
  padding: var(--co-space-2);
  border-left: var(--co-severity-marker-width) solid var(--co-action-primary);
  background: var(--co-bg-subtle);
  font-size: 11px;
}

@media (max-width: 1100px) {
  .atlas-header { grid-template-columns: minmax(220px, 1fr) auto; }
  .atlas-facts { grid-column: 1 / -1; grid-row: 2; justify-content: flex-start; }
  .atlas-actions { grid-column: 2; grid-row: 1; }
  .atlas-stage.has-inspector { grid-template-columns: minmax(0, 1fr) min(400px, 42%); }
}

@media (prefers-reduced-motion: reduce) {
  .atlas-stage { transition: none; }
}
</style>
