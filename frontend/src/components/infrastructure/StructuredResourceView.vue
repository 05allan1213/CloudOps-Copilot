<script setup lang="ts">
import { computed, ref } from "vue";
import { Search } from "lucide-vue-next";

import type { KubernetesResource, ResourceLayer } from "../../api/infrastructure";

const props = withDefaults(defineProps<{
  resources: KubernetesResource[];
  selectedId?: string;
  compact?: boolean;
}>(), { selectedId: "", compact: false });
const emit = defineEmits<{ select: [resource: KubernetesResource] }>();

const search = ref("");
const layer = ref<ResourceLayer | "all">("all");
const layerLabels: Record<ResourceLayer, string> = {
  namespace: "Namespace",
  service: "Service",
  workload: "Workload",
  pod: "Pod",
  node: "Node",
  gateway: "Ingress / Gateway",
};

const filtered = computed(() => {
  const value = search.value.trim().toLocaleLowerCase();
  return props.resources.filter((resource) => {
    if (layer.value !== "all" && resource.layer !== layer.value) return false;
    if (!value) return true;
    return `${resource.kind} ${resource.namespace ?? ""} ${resource.name}`.toLocaleLowerCase().includes(value);
  });
});
</script>

<template>
  <section class="structured-view" :class="{ 'is-compact': compact }" aria-label="Atlas 结构化资源视图" data-testid="atlas-structured-view">
    <header>
      <label>
        <span>搜索资源</span>
        <span class="search-control"><Search :size="16" aria-hidden="true" /><input v-model="search" name="atlas-resource-search" type="search" autocomplete="off" placeholder="名称、Kind 或 Namespace" /></span>
      </label>
      <label>
        <span>资源层</span>
        <select v-model="layer" name="atlas-resource-layer" autocomplete="off">
          <option value="all">全部资源层</option>
          <option v-for="(label, value) in layerLabels" :key="value" :value="value">{{ label }}</option>
        </select>
      </label>
      <output aria-live="polite">{{ filtered.length }} / {{ resources.length }}</output>
    </header>

    <ul v-if="filtered.length" class="resource-list">
      <li v-for="resource in filtered" :key="resource.id">
        <button type="button" :class="{ 'is-selected': resource.id === selectedId }" @click="emit('select', resource)">
          <span class="resource-kind">{{ resource.kind }}</span>
          <strong>{{ resource.name }}</strong>
          <span class="resource-namespace mono-text">{{ resource.namespace || "cluster" }}</span>
          <span class="health-state" :class="`is-${resource.health.state}`">{{ resource.health.state }}</span>
          <small>{{ resource.health.summary }}</small>
        </button>
      </li>
    </ul>
    <p v-else class="empty-state">没有符合当前筛选条件的真实资源。</p>
  </section>
</template>

<style scoped>
.structured-view { display: grid; min-height: 0; grid-template-rows: auto 1fr; color: var(--co-text-primary); background: var(--co-bg-canvas); }
.structured-view > header { display: grid; grid-template-columns: minmax(220px, 1fr) minmax(160px, 220px) auto; align-items: end; gap: var(--co-space-3); padding: var(--co-space-4); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
label { display: grid; gap: var(--co-space-1); color: var(--co-text-muted); font-size: 11px; font-weight: 700; }
.search-control { display: flex; min-height: 42px; align-items: center; gap: var(--co-space-2); padding: 0 var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-muted); background: var(--co-bg-canvas); }
.search-control:focus-within { border-color: var(--co-focus-ring); box-shadow: 0 0 0 2px color-mix(in srgb, var(--co-focus-ring) 32%, transparent); }
input, select { width: 100%; min-height: 42px; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-canvas); font: inherit; }
input { min-width: 0; min-height: auto; padding: 0; border: 0; outline: 0; }
select { padding: 0 var(--co-space-3); }
output { padding-bottom: var(--co-space-3); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 11px; }
.resource-list { min-height: 0; margin: 0; padding: var(--co-space-3); overflow-y: auto; overscroll-behavior: contain; list-style: none; }
.resource-list li { content-visibility: auto; contain-intrinsic-size: 68px; }
.resource-list button { display: grid; width: 100%; min-height: 64px; grid-template-columns: 96px minmax(140px, 1fr) minmax(90px, 0.7fr) auto; align-items: center; gap: var(--co-space-2); padding: var(--co-space-2) var(--co-space-3); border: 0; border-bottom: 1px solid var(--co-border-default); color: var(--co-text-primary); text-align: left; background: transparent; cursor: pointer; }
.resource-list button:hover { background: var(--co-bg-hover); }
.resource-list button.is-selected { background: var(--co-bg-active); box-shadow: inset 3px 0 var(--co-action-primary); }
.resource-kind, .resource-namespace { overflow: hidden; color: var(--co-text-muted); text-overflow: ellipsis; white-space: nowrap; font-size: 11px; }
.resource-list strong { overflow-wrap: anywhere; font-size: 13px; }
.resource-list small { grid-column: 2 / -1; color: var(--co-text-secondary); overflow-wrap: anywhere; }
.health-state { padding: 2px 7px; border: 1px solid var(--co-status-neutral-border); border-radius: var(--co-radius-pill); color: var(--co-status-neutral-fg); font-size: 10px; font-weight: 800; }
.health-state.is-healthy { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); }
.health-state.is-warning { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); }
.health-state.is-critical { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); }
.empty-state { display: grid; min-height: 180px; place-items: center; margin: 0; padding: var(--co-space-5); color: var(--co-text-muted); }
.is-compact > header { grid-template-columns: 1fr; }
.is-compact output { padding: 0; }
.is-compact .resource-list button { grid-template-columns: 80px minmax(100px, 1fr) auto; }
.is-compact .resource-namespace { display: none; }
@media (max-width: 720px) {
  .structured-view > header { grid-template-columns: 1fr; }
  output { padding: 0; }
  .resource-list button { grid-template-columns: 74px minmax(100px, 1fr) auto; }
  .resource-namespace { display: none; }
}
</style>
