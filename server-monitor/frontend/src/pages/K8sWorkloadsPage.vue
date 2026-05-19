<script setup lang="ts">
import { ref, computed, onMounted, watch } from "vue";
import { Search } from "@element-plus/icons-vue";

import PageHeader from "../components/common/PageHeader.vue";
import FilterPanel from "../components/common/FilterPanel.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import DeploymentTable from "../components/k8s/DeploymentTable.vue";
import PodTable from "../components/k8s/PodTable.vue";
import { fetchK8sDeployments, fetchK8sPods } from "../api/k8s";
import type { K8sDeploymentSummary, K8sPodSummary } from "../types";

const activeTab = ref("deployments");

const deployments = ref<K8sDeploymentSummary[]>([]);
const deploymentsTotal = ref(0);
const deploymentsLoading = ref(false);
const deploymentsError = ref("");
const deploymentNamespace = ref("");
const deploymentSearch = ref("");
const deploymentPage = ref(1);
const deploymentPageSize = 20;

const pods = ref<K8sPodSummary[]>([]);
const podsTotal = ref(0);
const podsLoading = ref(false);
const podsError = ref("");
const podNamespace = ref("");
const podPhase = ref("");
const podSearch = ref("");
const podPage = ref(1);
const podPageSize = 20;

const deploymentsState = computed<"loading" | "error" | "empty" | "default">(() => {
  if (deploymentsLoading.value) return "loading";
  if (deploymentsError.value) return "error";
  if (deployments.value.length === 0) return "empty";
  return "default";
});

const podsState = computed<"loading" | "error" | "empty" | "default">(() => {
  if (podsLoading.value) return "loading";
  if (podsError.value) return "error";
  if (pods.value.length === 0) return "empty";
  return "default";
});

async function loadDeployments() {
  deploymentsLoading.value = true;
  deploymentsError.value = "";
  try {
    const result = await fetchK8sDeployments({
      namespace: deploymentNamespace.value || undefined,
      search: deploymentSearch.value || undefined,
      limit: deploymentPageSize,
    });
    deployments.value = result.items;
    deploymentsTotal.value = result.total;
  } catch (err) {
    deploymentsError.value = err instanceof Error ? err.message : "加载 Deployments 失败";
  } finally {
    deploymentsLoading.value = false;
  }
}

async function loadPods() {
  podsLoading.value = true;
  podsError.value = "";
  try {
    const result = await fetchK8sPods({
      namespace: podNamespace.value || undefined,
      phase: podPhase.value || undefined,
      search: podSearch.value || undefined,
      limit: podPageSize,
    });
    pods.value = result.items;
    podsTotal.value = result.total;
  } catch (err) {
    podsError.value = err instanceof Error ? err.message : "加载 Pods 失败";
  } finally {
    podsLoading.value = false;
  }
}

function applyDeploymentFilters() {
  deploymentPage.value = 1;
  loadDeployments();
}

function resetDeploymentFilters() {
  deploymentNamespace.value = "";
  deploymentSearch.value = "";
  deploymentPage.value = 1;
  loadDeployments();
}

function applyPodFilters() {
  podPage.value = 1;
  loadPods();
}

function resetPodFilters() {
  podNamespace.value = "";
  podPhase.value = "";
  podSearch.value = "";
  podPage.value = 1;
  loadPods();
}

function handleDeploymentPageChange(page: number) {
  deploymentPage.value = page;
  loadDeployments();
}

function handlePodPageChange(page: number) {
  podPage.value = page;
  loadPods();
}

onMounted(() => {
  loadDeployments();
  loadPods();
});

watch(activeTab, () => {
  if (activeTab.value === "deployments" && deployments.value.length === 0) {
    loadDeployments();
  }
  if (activeTab.value === "pods" && pods.value.length === 0) {
    loadPods();
  }
});
</script>

<template>
  <section class="workloads-page">
    <PageHeader title="Workloads" />

    <el-tabs v-model="activeTab">
      <el-tab-pane label="Deployments" name="deployments">
        <FilterPanel @search="applyDeploymentFilters" @reset="resetDeploymentFilters">
          <el-form-item label="命名空间">
            <el-select
              v-model="deploymentNamespace"
              placeholder="全部"
              clearable
              style="width: 160px"
            >
              <el-option label="default" value="default" />
              <el-option label="kube-system" value="kube-system" />
              <el-option label="kube-public" value="kube-public" />
            </el-select>
          </el-form-item>
          <el-form-item label="搜索">
            <el-input
              v-model.trim="deploymentSearch"
              placeholder="搜索 Deployment"
              :prefix-icon="Search"
              clearable
              style="width: 200px"
            />
          </el-form-item>
        </FilterPanel>

        <StateWrapper
          :state="deploymentsState"
          :error-text="deploymentsError"
          empty-text="暂无 Deployment"
        >
          <template #retry>
            <el-button type="primary" @click="loadDeployments">重试</el-button>
          </template>

          <DeploymentTable :deployments="deployments" />

          <div class="pagination-wrap">
            <el-pagination
              v-model:current-page="deploymentPage"
              :page-size="deploymentPageSize"
              :total="deploymentsTotal"
              layout="total, prev, pager, next"
              background
              @current-change="handleDeploymentPageChange"
            />
          </div>
        </StateWrapper>
      </el-tab-pane>

      <el-tab-pane label="Pods" name="pods">
        <FilterPanel @search="applyPodFilters" @reset="resetPodFilters">
          <el-form-item label="命名空间">
            <el-select
              v-model="podNamespace"
              placeholder="全部"
              clearable
              style="width: 160px"
            >
              <el-option label="default" value="default" />
              <el-option label="kube-system" value="kube-system" />
              <el-option label="kube-public" value="kube-public" />
            </el-select>
          </el-form-item>
          <el-form-item label="状态">
            <el-radio-group v-model="podPhase" size="small">
              <el-radio-button value="">全部</el-radio-button>
              <el-radio-button value="Running">Running</el-radio-button>
              <el-radio-button value="Pending">Pending</el-radio-button>
              <el-radio-button value="Failed">Failed</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="搜索">
            <el-input
              v-model.trim="podSearch"
              placeholder="搜索 Pod"
              :prefix-icon="Search"
              clearable
              style="width: 200px"
            />
          </el-form-item>
        </FilterPanel>

        <StateWrapper
          :state="podsState"
          :error-text="podsError"
          empty-text="暂无 Pod"
        >
          <template #retry>
            <el-button type="primary" @click="loadPods">重试</el-button>
          </template>

          <PodTable :pods="pods" />

          <div class="pagination-wrap">
            <el-pagination
              v-model:current-page="podPage"
              :page-size="podPageSize"
              :total="podsTotal"
              layout="total, prev, pager, next"
              background
              @current-change="handlePodPageChange"
            />
          </div>
        </StateWrapper>
      </el-tab-pane>
    </el-tabs>
  </section>
</template>

<style scoped>
.workloads-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.pagination-wrap {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
