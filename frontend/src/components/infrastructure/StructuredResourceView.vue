<script setup lang="ts">
import { computed, h, ref, resolveComponent } from "vue";

import type { KubernetesResource, ResourceLayer } from "../../api/infrastructure";
import DenseDataTable, { type DenseRowSeverity, type DenseTableColumn } from "../workspace/DenseDataTable.vue";

type ResourceRow = KubernetesResource & Record<string, unknown>;

const props = withDefaults(defineProps<{
  resources: KubernetesResource[];
  selectedId?: string;
}>(), {
  selectedId: "",
});
const emit = defineEmits<{
  select: [resource: KubernetesResource, trigger: HTMLElement | null];
}>();

const UBadge = resolveComponent("UBadge");
const search = ref("");
const layer = ref<ResourceLayer | "all">("all");
const layerItems: Array<{ label: string; value: ResourceLayer | "all" }> = [
  { label: "全部资源层", value: "all" },
  { label: "Namespace", value: "namespace" },
  { label: "Service", value: "service" },
  { label: "Workload", value: "workload" },
  { label: "Pod", value: "pod" },
  { label: "Node", value: "node" },
  { label: "Ingress / Gateway", value: "gateway" },
];
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

const filtered = computed<ResourceRow[]>(() => {
  const value = search.value.trim().toLocaleLowerCase();
  return props.resources.filter((resource) => {
    if (layer.value !== "all" && resource.layer !== layer.value) return false;
    if (!value) return true;
    return `${resource.kind} ${resource.namespace ?? ""} ${resource.name}`.toLocaleLowerCase().includes(value);
  }) as ResourceRow[];
});

const columns: DenseTableColumn<ResourceRow>[] = [
  { id: "kind", accessorKey: "kind", header: "Kind", label: "Kind", size: 118 },
  { id: "name", accessorKey: "name", header: "资源", label: "资源", size: 238 },
  { id: "namespace", accessorKey: "namespace", header: "Namespace", label: "Namespace", size: 170 },
  {
    id: "health",
    accessorKey: "health",
    header: "健康",
    label: "健康",
    size: 108,
    cell: ({ row }) => h(UBadge, {
      color: healthColors[row.original.health.state],
      variant: "subtle",
      label: healthLabels[row.original.health.state],
    }),
  },
  {
    id: "summary",
    accessorFn: (row) => row.health.summary,
    header: "状态摘要",
    label: "状态摘要",
    size: 300,
    optional: true,
  },
];

function severity(resource: ResourceRow): DenseRowSeverity {
  if (resource.health.state === "critical") return "critical";
  if (resource.health.state === "warning") return "warning";
  return resource.health.state === "unknown" ? "info" : "neutral";
}

function selectResource(resource: ResourceRow, trigger: HTMLElement | null) {
  emit("select", resource, trigger);
}
</script>

<template>
  <section
    class="structured-view"
    aria-label="Atlas 结构化资源视图"
    data-testid="atlas-structured-view"
  >
    <header class="structured-toolbar">
      <UInput
        v-model="search"
        class="structured-search"
        icon="i-lucide-search"
        name="atlas-resource-search"
        autocomplete="off"
        aria-label="搜索 Atlas 资源"
        placeholder="名称、Kind 或 Namespace"
      />
      <USelect
        v-model="layer"
        class="structured-layer"
        :items="layerItems"
        value-key="value"
        label-key="label"
        aria-label="筛选资源层"
      />
      <output aria-live="polite">{{ filtered.length }} / {{ resources.length }}</output>
    </header>

    <DenseDataTable
      :rows="filtered"
      :columns="columns"
      :row-key="(resource: ResourceRow) => resource.id"
      storage-key="atlas-structured-resources"
      caption="Operations Atlas 结构化资源"
      :critical-column-ids="['kind', 'name', 'health']"
      :selected-id="selectedId"
      empty="没有符合当前筛选条件的真实资源"
      :severity="severity"
      :copy-value="(resource: ResourceRow) => `${resource.kind}/${resource.namespace || 'cluster'}/${resource.name} (${resource.id})`"
      :virtualized="filtered.length > 250"
      @select="selectResource"
    />
  </section>
</template>

<style scoped>
.structured-view {
  display: grid;
  width: 100%;
  min-width: 0;
  min-height: 0;
  grid-template-rows: auto minmax(0, 1fr);
  color: var(--co-text-primary);
  background: var(--co-bg-canvas);
}

.structured-toolbar {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(240px, 1fr) minmax(180px, 240px) auto;
  align-items: center;
  gap: var(--co-space-3);
  min-height: 56px;
  padding: var(--co-space-2) var(--co-space-3);
  border-bottom: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}

.structured-search,
.structured-layer { min-width: 0; }
.structured-toolbar output {
  color: var(--co-text-muted);
  font-family: var(--co-font-mono);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

@media (max-width: 1024px) {
  .structured-toolbar { grid-template-columns: minmax(180px, 1fr) minmax(160px, 200px); }
  .structured-toolbar output { grid-column: 1 / -1; }
}
</style>
