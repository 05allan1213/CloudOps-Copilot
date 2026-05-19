<script setup lang="ts">
import { ref, computed, onMounted, watch } from "vue";
import { Search } from "@element-plus/icons-vue";

import PageHeader from "../components/common/PageHeader.vue";
import FilterPanel from "../components/common/FilterPanel.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import DeploymentTable from "../components/k8s/DeploymentTable.vue";
import PodTable from "../components/k8s/PodTable.vue";
import DaemonSetTable from "../components/k8s/DaemonSetTable.vue";
import StatefulSetTable from "../components/k8s/StatefulSetTable.vue";
import JobTable from "../components/k8s/JobTable.vue";
import YamlViewer from "../components/k8s/YamlViewer.vue";
import {
  fetchK8sDeployments,
  fetchK8sPods,
  fetchK8sDaemonSets,
  fetchK8sStatefulSets,
  fetchK8sJobs,
} from "../api/k8s";
import type {
  K8sDeploymentSummary,
  K8sPodSummary,
  K8sDaemonSetSummary,
  K8sStatefulSetSummary,
  K8sJobSummary,
} from "../types";

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

const daemonSets = ref<K8sDaemonSetSummary[]>([]);
const daemonSetsTotal = ref(0);
const daemonSetsLoading = ref(false);
const daemonSetsError = ref("");
const daemonSetNamespace = ref("");
const daemonSetSearch = ref("");
const daemonSetPage = ref(1);
const daemonSetPageSize = 20;

const statefulSets = ref<K8sStatefulSetSummary[]>([]);
const statefulSetsTotal = ref(0);
const statefulSetsLoading = ref(false);
const statefulSetsError = ref("");
const statefulSetNamespace = ref("");
const statefulSetSearch = ref("");
const statefulSetPage = ref(1);
const statefulSetPageSize = 20;

const jobs = ref<K8sJobSummary[]>([]);
const jobsTotal = ref(0);
const jobsLoading = ref(false);
const jobsError = ref("");
const jobNamespace = ref("");
const jobStatus = ref("");
const jobSearch = ref("");
const jobPage = ref(1);
const jobPageSize = 20;

const yamlVisible = ref(false);
const yamlKind = ref("");
const yamlNamespace = ref("");
const yamlName = ref("");

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

const daemonSetsState = computed<"loading" | "error" | "empty" | "default">(() => {
  if (daemonSetsLoading.value) return "loading";
  if (daemonSetsError.value) return "error";
  if (daemonSets.value.length === 0) return "empty";
  return "default";
});

const statefulSetsState = computed<"loading" | "error" | "empty" | "default">(() => {
  if (statefulSetsLoading.value) return "loading";
  if (statefulSetsError.value) return "error";
  if (statefulSets.value.length === 0) return "empty";
  return "default";
});

const jobsState = computed<"loading" | "error" | "empty" | "default">(() => {
  if (jobsLoading.value) return "loading";
  if (jobsError.value) return "error";
  if (jobs.value.length === 0) return "empty";
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

async function loadDaemonSets() {
  daemonSetsLoading.value = true;
  daemonSetsError.value = "";
  try {
    const result = await fetchK8sDaemonSets({
      namespace: daemonSetNamespace.value || undefined,
      search: daemonSetSearch.value || undefined,
      limit: daemonSetPageSize,
    });
    daemonSets.value = result.items;
    daemonSetsTotal.value = result.total;
  } catch (err) {
    daemonSetsError.value = err instanceof Error ? err.message : "加载 DaemonSets 失败";
  } finally {
    daemonSetsLoading.value = false;
  }
}

async function loadStatefulSets() {
  statefulSetsLoading.value = true;
  statefulSetsError.value = "";
  try {
    const result = await fetchK8sStatefulSets({
      namespace: statefulSetNamespace.value || undefined,
      search: statefulSetSearch.value || undefined,
      limit: statefulSetPageSize,
    });
    statefulSets.value = result.items;
    statefulSetsTotal.value = result.total;
  } catch (err) {
    statefulSetsError.value = err instanceof Error ? err.message : "加载 StatefulSets 失败";
  } finally {
    statefulSetsLoading.value = false;
  }
}

async function loadJobs() {
  jobsLoading.value = true;
  jobsError.value = "";
  try {
    const result = await fetchK8sJobs({
      namespace: jobNamespace.value || undefined,
      status: jobStatus.value || undefined,
      search: jobSearch.value || undefined,
      limit: jobPageSize,
    });
    jobs.value = result.items;
    jobsTotal.value = result.total;
  } catch (err) {
    jobsError.value = err instanceof Error ? err.message : "加载 Jobs 失败";
  } finally {
    jobsLoading.value = false;
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

function applyDaemonSetFilters() {
  daemonSetPage.value = 1;
  loadDaemonSets();
}

function resetDaemonSetFilters() {
  daemonSetNamespace.value = "";
  daemonSetSearch.value = "";
  daemonSetPage.value = 1;
  loadDaemonSets();
}

function applyStatefulSetFilters() {
  statefulSetPage.value = 1;
  loadStatefulSets();
}

function resetStatefulSetFilters() {
  statefulSetNamespace.value = "";
  statefulSetSearch.value = "";
  statefulSetPage.value = 1;
  loadStatefulSets();
}

function applyJobFilters() {
  jobPage.value = 1;
  loadJobs();
}

function resetJobFilters() {
  jobNamespace.value = "";
  jobStatus.value = "";
  jobSearch.value = "";
  jobPage.value = 1;
  loadJobs();
}

function handleDeploymentPageChange(page: number) {
  deploymentPage.value = page;
  loadDeployments();
}

function handlePodPageChange(page: number) {
  podPage.value = page;
  loadPods();
}

function handleDaemonSetPageChange(page: number) {
  daemonSetPage.value = page;
  loadDaemonSets();
}

function handleStatefulSetPageChange(page: number) {
  statefulSetPage.value = page;
  loadStatefulSets();
}

function handleJobPageChange(page: number) {
  jobPage.value = page;
  loadJobs();
}

function viewYaml(kind: string, namespace: string, name: string) {
  yamlKind.value = kind;
  yamlNamespace.value = namespace;
  yamlName.value = name;
  yamlVisible.value = true;
}

onMounted(() => {
  loadDeployments();
  loadPods();
});

watch(activeTab, () => {
  if (activeTab.value === "deployments" && deployments.value.length === 0) loadDeployments();
  if (activeTab.value === "pods" && pods.value.length === 0) loadPods();
  if (activeTab.value === "daemonsets" && daemonSets.value.length === 0) loadDaemonSets();
  if (activeTab.value === "statefulsets" && statefulSets.value.length === 0) loadStatefulSets();
  if (activeTab.value === "jobs" && jobs.value.length === 0) loadJobs();
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

          <DeploymentTable :deployments="deployments" @view-yaml="(d: K8sDeploymentSummary) => viewYaml('deployment', d.namespace, d.name)" />

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

          <PodTable :pods="pods" @view-yaml="(p: K8sPodSummary) => viewYaml('pod', p.namespace, p.name)" />

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

      <el-tab-pane label="DaemonSets" name="daemonsets">
        <FilterPanel @search="applyDaemonSetFilters" @reset="resetDaemonSetFilters">
          <el-form-item label="命名空间">
            <el-select
              v-model="daemonSetNamespace"
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
              v-model.trim="daemonSetSearch"
              placeholder="搜索 DaemonSet"
              :prefix-icon="Search"
              clearable
              style="width: 200px"
            />
          </el-form-item>
        </FilterPanel>

        <StateWrapper
          :state="daemonSetsState"
          :error-text="daemonSetsError"
          empty-text="暂无 DaemonSet"
        >
          <template #retry>
            <el-button type="primary" @click="loadDaemonSets">重试</el-button>
          </template>

          <DaemonSetTable :items="daemonSets" @view-yaml="(ds: K8sDaemonSetSummary) => viewYaml('daemonset', ds.namespace, ds.name)" />

          <div class="pagination-wrap">
            <el-pagination
              v-model:current-page="daemonSetPage"
              :page-size="daemonSetPageSize"
              :total="daemonSetsTotal"
              layout="total, prev, pager, next"
              background
              @current-change="handleDaemonSetPageChange"
            />
          </div>
        </StateWrapper>
      </el-tab-pane>

      <el-tab-pane label="StatefulSets" name="statefulsets">
        <FilterPanel @search="applyStatefulSetFilters" @reset="resetStatefulSetFilters">
          <el-form-item label="命名空间">
            <el-select
              v-model="statefulSetNamespace"
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
              v-model.trim="statefulSetSearch"
              placeholder="搜索 StatefulSet"
              :prefix-icon="Search"
              clearable
              style="width: 200px"
            />
          </el-form-item>
        </FilterPanel>

        <StateWrapper
          :state="statefulSetsState"
          :error-text="statefulSetsError"
          empty-text="暂无 StatefulSet"
        >
          <template #retry>
            <el-button type="primary" @click="loadStatefulSets">重试</el-button>
          </template>

          <StatefulSetTable :items="statefulSets" @view-yaml="(sts: K8sStatefulSetSummary) => viewYaml('statefulset', sts.namespace, sts.name)" />

          <div class="pagination-wrap">
            <el-pagination
              v-model:current-page="statefulSetPage"
              :page-size="statefulSetPageSize"
              :total="statefulSetsTotal"
              layout="total, prev, pager, next"
              background
              @current-change="handleStatefulSetPageChange"
            />
          </div>
        </StateWrapper>
      </el-tab-pane>

      <el-tab-pane label="Jobs" name="jobs">
        <FilterPanel @search="applyJobFilters" @reset="resetJobFilters">
          <el-form-item label="命名空间">
            <el-select
              v-model="jobNamespace"
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
            <el-radio-group v-model="jobStatus" size="small">
              <el-radio-button value="">全部</el-radio-button>
              <el-radio-button value="Running">Running</el-radio-button>
              <el-radio-button value="Completed">Completed</el-radio-button>
              <el-radio-button value="Failed">Failed</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="搜索">
            <el-input
              v-model.trim="jobSearch"
              placeholder="搜索 Job"
              :prefix-icon="Search"
              clearable
              style="width: 200px"
            />
          </el-form-item>
        </FilterPanel>

        <StateWrapper
          :state="jobsState"
          :error-text="jobsError"
          empty-text="暂无 Job"
        >
          <template #retry>
            <el-button type="primary" @click="loadJobs">重试</el-button>
          </template>

          <JobTable :items="jobs" @view-yaml="(j: K8sJobSummary) => viewYaml('job', j.namespace, j.name)" />

          <div class="pagination-wrap">
            <el-pagination
              v-model:current-page="jobPage"
              :page-size="jobPageSize"
              :total="jobsTotal"
              layout="total, prev, pager, next"
              background
              @current-change="handleJobPageChange"
            />
          </div>
        </StateWrapper>
      </el-tab-pane>
    </el-tabs>

    <YamlViewer
      v-model:visible="yamlVisible"
      :kind="yamlKind"
      :namespace="yamlNamespace"
      :name="yamlName"
    />
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
