<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from "vue";
import { useRouter } from "vue-router";
import { ArrowLeft } from "@element-plus/icons-vue";

import StateWrapper from "../components/common/StateWrapper.vue";
import NodeHostAssociation from "../components/k8s/NodeHostAssociation.vue";
import PodTable from "../components/k8s/PodTable.vue";
import EventTable from "../components/k8s/EventTable.vue";
import { fetchK8sNodeDetail } from "../api/k8s";
import type { K8sNodeDetail } from "../types/k8s";

const props = defineProps<{
  name: string;
}>();

const router = useRouter();
const detail = ref<K8sNodeDetail | null>(null);
const loading = ref(true);
const error = ref("");

const nodeInfo = computed(() => detail.value?.node ?? null);
const isReady = computed(() => nodeInfo.value?.ready ?? false);
const roles = computed(() => (nodeInfo.value?.roles ?? []).join(", ") || "-");
const kubeletVersion = computed(() => nodeInfo.value?.kubelet_version || "-");
const cpuCapacity = computed(() => nodeInfo.value?.capacity?.cpu || "-");
const memoryCapacity = computed(() => nodeInfo.value?.capacity?.memory || "-");
const hostAssociation = computed(() => {
  if (!detail.value) return undefined;
  return {
    online: detail.value.host_online,
    last_scrape: detail.value.last_scrape,
  };
});
const pods = computed(() => detail.value?.pods ?? []);
const events = computed(() => detail.value?.events ?? []);

const pageState = computed<"loading" | "error" | "default">(() => {
  if (loading.value) return "loading";
  if (error.value) return "error";
  return "default";
});

const podsState = computed<"loading" | "empty" | "default">(() => {
  if (loading.value) return "loading";
  if (pods.value.length === 0) return "empty";
  return "default";
});

const eventsState = computed<"loading" | "empty" | "default">(() => {
  if (loading.value) return "loading";
  if (events.value.length === 0) return "empty";
  return "default";
});

let requestId = 0;
let abortController: AbortController | null = null;

async function loadDetail() {
  const id = ++requestId;
  if (abortController) {
    abortController.abort();
  }
  const controller = new AbortController();
  abortController = controller;

  try {
    loading.value = true;
    error.value = "";
    const data = await fetchK8sNodeDetail(props.name, controller.signal);
    if (id === requestId) {
      detail.value = data;
    }
  } catch (e: any) {
    if (id === requestId && !controller.signal.aborted) {
      error.value = e.message || "加载失败";
    }
  } finally {
    if (id === requestId) {
      loading.value = false;
      abortController = null;
    }
  }
}

onMounted(loadDetail);

onUnmounted(() => {
  abortController?.abort();
  requestId++;
});
</script>

<template>
  <div class="detail-header">
    <div>
      <el-button
        type="primary"
        link
        :icon="ArrowLeft"
        @click="router.push('/k8s/nodes')"
      >
        返回节点列表
      </el-button>
      <h2>{{ name }}</h2>
      <p>
        <el-tag
          v-if="!loading && !error"
          :type="isReady ? 'success' : 'danger'"
          size="small"
        >
          {{ isReady ? "Ready" : "NotReady" }}
        </el-tag>
      </p>
    </div>
  </div>

  <StateWrapper
    :state="pageState"
    :error-text="error"
    empty-text="暂无节点数据"
  >
    <template #retry>
      <el-button type="primary" @click="loadDetail">重试</el-button>
    </template>

    <el-row :gutter="12" class="metric-grid">
      <el-col :xs="12" :sm="8" :md="4">
        <el-card shadow="never" class="metric-card">
          <el-statistic title="状态">
            <template #default>
              <el-tag :type="isReady ? 'success' : 'danger'" size="small">
                {{ isReady ? "Ready" : "NotReady" }}
              </el-tag>
            </template>
          </el-statistic>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="8" :md="4">
        <el-card shadow="never" class="metric-card">
          <el-statistic title="角色" :value="roles" />
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="8" :md="4">
        <el-card shadow="never" class="metric-card">
          <el-statistic title="Kubelet 版本" :value="kubeletVersion" />
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="8" :md="4">
        <el-card shadow="never" class="metric-card">
          <el-statistic title="CPU 容量" :value="cpuCapacity" />
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="8" :md="4">
        <el-card shadow="never" class="metric-card">
          <el-statistic title="内存容量" :value="memoryCapacity" />
        </el-card>
      </el-col>
    </el-row>

    <NodeHostAssociation
      :association="hostAssociation"
      :node-name="name"
      style="margin-bottom: 16px"
    />

    <el-card shadow="never" style="margin-bottom: 16px">
      <template #header>
        <span class="section-title">运行中的 Pod</span>
      </template>
      <StateWrapper :state="podsState" empty-text="该节点暂无运行中的 Pod">
        <PodTable :pods="pods" />
      </StateWrapper>
    </el-card>

    <el-card shadow="never">
      <template #header>
        <span class="section-title">相关事件</span>
      </template>
      <StateWrapper :state="eventsState" empty-text="暂无相关事件">
        <EventTable :events="events" />
      </StateWrapper>
    </el-card>
  </StateWrapper>
</template>

<style scoped>
.detail-header {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
  margin-bottom: 16px;
}

.detail-header h2 {
  margin: 8px 0 0;
  font-size: 19px;
}

.detail-header p {
  margin-top: 6px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.metric-grid {
  margin-bottom: 16px;
}

.metric-card :deep(.el-card__body) {
  padding: 20px;
}

.metric-card :deep(.el-statistic__number) {
  font-size: 17px;
  font-variant-numeric: tabular-nums;
}

.section-title {
  font-size: 15px;
  font-weight: 600;
}

@media (max-width: 768px) {
  .detail-header {
    flex-direction: column;
  }
}
</style>
