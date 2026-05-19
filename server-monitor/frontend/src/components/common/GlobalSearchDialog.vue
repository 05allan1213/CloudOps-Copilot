<script setup lang="ts">
import { ref, computed, watch } from "vue";
import { useRouter } from "vue-router";
import { Search } from "@element-plus/icons-vue";

import { useMonitorStore } from "../../stores/monitor";
import { fetchHosts } from "../../api/hosts";
import { fetchK8sNodes, fetchK8sPods, fetchK8sDeployments, fetchK8sServices } from "../../api/k8s";
import type { Host } from "../../types";
import type { K8sNodeSummary, K8sPodSummary, K8sDeploymentSummary, K8sServiceSummary } from "../../types";

interface SearchResultItem {
  label: string;
  path: string;
  detail?: string;
}

interface SearchResultGroup {
  type: string;
  label: string;
  items: SearchResultItem[];
}

const props = defineProps<{
  modelValue: boolean;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: boolean];
}>();

const router = useRouter();
const monitor = useMonitorStore();

const query = ref("");
const searching = ref(false);
const results = ref<SearchResultGroup[]>([]);

const visible = computed({
  get: () => props.modelValue,
  set: (val) => emit("update:modelValue", val),
});

let searchTimer: ReturnType<typeof setTimeout> | null = null;

watch(query, () => {
  if (searchTimer) clearTimeout(searchTimer);
  const q = query.value.trim();
  if (!q) {
    results.value = [];
    return;
  }
  searchTimer = setTimeout(() => doSearch(q), 300);
});

async function doSearch(q: string) {
  searching.value = true;
  const groups: SearchResultGroup[] = [];

  try {
    const hostPromise = fetchHosts({ q }).catch(() => []);
    const k8sPromises = monitor.k8sApiEnabled
      ? [
          fetchK8sNodes({ search: q, limit: 10 }).catch(() => ({ items: [], total: 0 })),
          fetchK8sPods({ search: q, limit: 10 }).catch(() => ({ items: [], total: 0 })),
          fetchK8sDeployments({ search: q, limit: 10 }).catch(() => ({ items: [], total: 0 })),
          fetchK8sServices({ search: q, limit: 10 }).catch(() => ({ items: [], total: 0 })),
        ]
      : [Promise.resolve({ items: [], total: 0 }), Promise.resolve({ items: [], total: 0 }), Promise.resolve({ items: [], total: 0 }), Promise.resolve({ items: [], total: 0 })];

    const [hosts, nodesResult, podsResult, deploymentsResult, servicesResult] = await Promise.all([
      hostPromise,
      ...k8sPromises,
    ]);

    const hostItems: SearchResultItem[] = (hosts as Host[]).map((h) => ({
      label: h.instance,
      path: `/hosts/${encodeURIComponent(h.instance)}`,
      detail: h.status === "up" ? "在线" : "离线",
    }));
    if (hostItems.length > 0) {
      groups.push({ type: "host", label: "主机", items: hostItems.slice(0, 10) });
    }

    if (monitor.k8sApiEnabled) {
      const nodeItems: SearchResultItem[] = (nodesResult as { items: K8sNodeSummary[] }).items.map((n) => ({
        label: n.name,
        path: `/k8s/nodes/${encodeURIComponent(n.name)}`,
        detail: n.ready ? "Ready" : "NotReady",
      }));
      if (nodeItems.length > 0) {
        groups.push({ type: "node", label: "Nodes", items: nodeItems });
      }

      const podItems: SearchResultItem[] = (podsResult as { items: K8sPodSummary[] }).items.map((p) => ({
        label: `${p.namespace}/${p.name}`,
        path: "/k8s/workloads",
        detail: p.phase,
      }));
      if (podItems.length > 0) {
        groups.push({ type: "pod", label: "Pods", items: podItems });
      }

      const deployItems: SearchResultItem[] = (deploymentsResult as { items: K8sDeploymentSummary[] }).items.map((d) => ({
        label: `${d.namespace}/${d.name}`,
        path: "/k8s/workloads",
        detail: `${d.ready_replicas}/${d.replicas} ready`,
      }));
      if (deployItems.length > 0) {
        groups.push({ type: "deployment", label: "Deployments", items: deployItems });
      }

      const svcItems: SearchResultItem[] = (servicesResult as { items: K8sServiceSummary[] }).items.map((s) => ({
        label: `${s.namespace}/${s.name}`,
        path: "/k8s/services",
        detail: s.type,
      }));
      if (svcItems.length > 0) {
        groups.push({ type: "service", label: "Services", items: svcItems });
      }
    }
  } catch {
    // ignore search errors
  } finally {
    searching.value = false;
  }

  results.value = groups;
}

function navigateTo(item: SearchResultItem) {
  visible.value = false;
  query.value = "";
  results.value = [];
  router.push(item.path);
}

function handleClose() {
  visible.value = false;
  query.value = "";
  results.value = [];
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :show-close="false"
    width="560px"
    top="80px"
    class="global-search-dialog"
    @close="handleClose"
  >
    <div class="search-input-wrap">
      <el-input
        v-model="query"
        placeholder="搜索主机、节点、Pod、Deployment、Service..."
        :prefix-icon="Search"
        size="large"
        clearable
        autofocus
      />
    </div>
    <div v-if="searching" class="search-loading">
      <el-icon class="is-loading" :size="16"><Search /></el-icon>
      <span>搜索中...</span>
    </div>
    <div v-else-if="results.length === 0 && query.trim()" class="search-empty">
      <span>未找到匹配结果</span>
    </div>
    <div v-else class="search-results">
      <div v-for="group in results" :key="group.type" class="search-group">
        <div class="search-group-label">{{ group.label }}</div>
        <div
          v-for="item in group.items"
          :key="item.label + item.path"
          class="search-item"
          @click="navigateTo(item)"
        >
          <span class="search-item-label">{{ item.label }}</span>
          <span v-if="item.detail" class="search-item-detail">{{ item.detail }}</span>
        </div>
      </div>
    </div>
  </el-dialog>
</template>

<style scoped>
.search-input-wrap {
  margin: -10px 0 0;
}

.search-loading,
.search-empty {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 20px 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  justify-content: center;
}

.search-results {
  max-height: 400px;
  overflow-y: auto;
  margin-top: 8px;
}

.search-group {
  margin-bottom: 8px;
}

.search-group-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  padding: 4px 0;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.search-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s;
}

.search-item:hover {
  background: var(--el-fill-color-light);
}

.search-item-label {
  font-size: 13px;
  color: var(--el-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.search-item-detail {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  flex-shrink: 0;
  margin-left: 12px;
}
</style>
