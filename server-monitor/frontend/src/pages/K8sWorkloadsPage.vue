<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from "vue";
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
import { useK8sResourceList } from "../composables/useK8sResourceList";

const activeTab = ref("deployments");

const dep = reactive(
  useK8sResourceList<K8sDeploymentSummary>({
    fetchFn: (params) => fetchK8sDeployments(params),
    pageSize: 20,
  }),
);

const podPhase = ref("");
const pod = reactive(
  useK8sResourceList<K8sPodSummary>({
    fetchFn: (params) =>
      fetchK8sPods({ ...params, phase: podPhase.value || undefined }),
    pageSize: 20,
  }),
);

const ds = reactive(
  useK8sResourceList<K8sDaemonSetSummary>({
    fetchFn: (params) => fetchK8sDaemonSets(params),
    pageSize: 20,
  }),
);

const sts = reactive(
  useK8sResourceList<K8sStatefulSetSummary>({
    fetchFn: (params) => fetchK8sStatefulSets(params),
    pageSize: 20,
  }),
);

const jobStatus = ref("");
const job = reactive(
  useK8sResourceList<K8sJobSummary>({
    fetchFn: (params) =>
      fetchK8sJobs({ ...params, status: jobStatus.value || undefined }),
    pageSize: 20,
  }),
);

const yamlVisible = ref(false);
const yamlKind = ref("");
const yamlNamespace = ref("");
const yamlName = ref("");

const depState = computed<"loading" | "error" | "empty" | "default">(() => {
  if (dep.loading) return "loading";
  if (dep.error) return "error";
  if (dep.items.length === 0) return "empty";
  return "default";
});

const podState = computed<"loading" | "error" | "empty" | "default">(() => {
  if (pod.loading) return "loading";
  if (pod.error) return "error";
  if (pod.items.length === 0) return "empty";
  return "default";
});

const dsState = computed<"loading" | "error" | "empty" | "default">(() => {
  if (ds.loading) return "loading";
  if (ds.error) return "error";
  if (ds.items.length === 0) return "empty";
  return "default";
});

const stsState = computed<"loading" | "error" | "empty" | "default">(() => {
  if (sts.loading) return "loading";
  if (sts.error) return "error";
  if (sts.items.length === 0) return "empty";
  return "default";
});

const jobState = computed<"loading" | "error" | "empty" | "default">(() => {
  if (job.loading) return "loading";
  if (job.error) return "error";
  if (job.items.length === 0) return "empty";
  return "default";
});

function viewYaml(kind: string, namespace: string, name: string) {
  yamlKind.value = kind;
  yamlNamespace.value = namespace;
  yamlName.value = name;
  yamlVisible.value = true;
}

watch(podPhase, () => pod.applyFilters());
watch(jobStatus, () => job.applyFilters());

onMounted(() => {
  dep.loadResources();
  pod.loadResources();
});

watch(activeTab, () => {
  if (activeTab.value === "deployments" && dep.items.length === 0) dep.loadResources();
  if (activeTab.value === "pods" && pod.items.length === 0) pod.loadResources();
  if (activeTab.value === "daemonsets" && ds.items.length === 0) ds.loadResources();
  if (activeTab.value === "statefulsets" && sts.items.length === 0) sts.loadResources();
  if (activeTab.value === "jobs" && job.items.length === 0) job.loadResources();
});
</script>

<template>
  <section class="workloads-page">
    <PageHeader title="工作负载" />

    <el-tabs v-model="activeTab">
      <el-tab-pane
        label="Deployments"
        name="deployments"
      >
        <FilterPanel
          @search="dep.applyFilters"
          @reset="dep.resetFilters"
        >
          <el-form-item label="命名空间">
            <el-select
              v-model="dep.namespace"
              placeholder="全部"
              clearable
              style="width: 160px"
            >
              <el-option
                label="default"
                value="default"
              />
              <el-option
                label="kube-system"
                value="kube-system"
              />
              <el-option
                label="kube-public"
                value="kube-public"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="搜索">
            <el-input
              v-model.trim="dep.searchText"
              placeholder="搜索 Deployment"
              :prefix-icon="Search"
              clearable
              style="width: 200px"
            />
          </el-form-item>
        </FilterPanel>

        <StateWrapper
          :state="depState"
          :error-text="dep.error"
          empty-text="暂无 Deployment"
        >
          <template #retry>
            <el-button
              type="primary"
              @click="dep.loadResources"
            >
              重试
            </el-button>
          </template>

          <DeploymentTable
            :deployments="dep.items"
            @view-yaml="(d: K8sDeploymentSummary) => viewYaml('deployment', d.namespace, d.name)"
          />

          <div class="pagination-wrap">
            <el-pagination
              v-model:current-page="dep.page"
              :page-size="dep.pageSize"
              :total="dep.total"
              layout="total, prev, pager, next"
              background
              @current-change="dep.handlePageChange"
            />
          </div>
        </StateWrapper>
      </el-tab-pane>

      <el-tab-pane
        label="Pods"
        name="pods"
      >
        <FilterPanel
          @search="pod.applyFilters"
          @reset="pod.resetFilters"
        >
          <el-form-item label="命名空间">
            <el-select
              v-model="pod.namespace"
              placeholder="全部"
              clearable
              style="width: 160px"
            >
              <el-option
                label="default"
                value="default"
              />
              <el-option
                label="kube-system"
                value="kube-system"
              />
              <el-option
                label="kube-public"
                value="kube-public"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="状态">
            <el-radio-group
              v-model="podPhase"
              size="small"
            >
              <el-radio-button value="">
                全部
              </el-radio-button>
              <el-radio-button value="Running">
                运行中
              </el-radio-button>
              <el-radio-button value="Pending">
                等待中
              </el-radio-button>
              <el-radio-button value="Failed">
                失败
              </el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="搜索">
            <el-input
              v-model.trim="pod.searchText"
              placeholder="搜索 Pod"
              :prefix-icon="Search"
              clearable
              style="width: 200px"
            />
          </el-form-item>
        </FilterPanel>

        <StateWrapper
          :state="podState"
          :error-text="pod.error"
          empty-text="暂无 Pod"
        >
          <template #retry>
            <el-button
              type="primary"
              @click="pod.loadResources"
            >
              重试
            </el-button>
          </template>

          <PodTable
            :pods="pod.items"
            @view-yaml="(p: K8sPodSummary) => viewYaml('pod', p.namespace, p.name)"
          />

          <div class="pagination-wrap">
            <el-pagination
              v-model:current-page="pod.page"
              :page-size="pod.pageSize"
              :total="pod.total"
              layout="total, prev, pager, next"
              background
              @current-change="pod.handlePageChange"
            />
          </div>
        </StateWrapper>
      </el-tab-pane>

      <el-tab-pane
        label="DaemonSets"
        name="daemonsets"
      >
        <FilterPanel
          @search="ds.applyFilters"
          @reset="ds.resetFilters"
        >
          <el-form-item label="命名空间">
            <el-select
              v-model="ds.namespace"
              placeholder="全部"
              clearable
              style="width: 160px"
            >
              <el-option
                label="default"
                value="default"
              />
              <el-option
                label="kube-system"
                value="kube-system"
              />
              <el-option
                label="kube-public"
                value="kube-public"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="搜索">
            <el-input
              v-model.trim="ds.searchText"
              placeholder="搜索 DaemonSet"
              :prefix-icon="Search"
              clearable
              style="width: 200px"
            />
          </el-form-item>
        </FilterPanel>

        <StateWrapper
          :state="dsState"
          :error-text="ds.error"
          empty-text="暂无 DaemonSet"
        >
          <template #retry>
            <el-button
              type="primary"
              @click="ds.loadResources"
            >
              重试
            </el-button>
          </template>

          <DaemonSetTable
            :items="ds.items"
            @view-yaml="(d: K8sDaemonSetSummary) => viewYaml('daemonset', d.namespace, d.name)"
          />

          <div class="pagination-wrap">
            <el-pagination
              v-model:current-page="ds.page"
              :page-size="ds.pageSize"
              :total="ds.total"
              layout="total, prev, pager, next"
              background
              @current-change="ds.handlePageChange"
            />
          </div>
        </StateWrapper>
      </el-tab-pane>

      <el-tab-pane
        label="StatefulSets"
        name="statefulsets"
      >
        <FilterPanel
          @search="sts.applyFilters"
          @reset="sts.resetFilters"
        >
          <el-form-item label="命名空间">
            <el-select
              v-model="sts.namespace"
              placeholder="全部"
              clearable
              style="width: 160px"
            >
              <el-option
                label="default"
                value="default"
              />
              <el-option
                label="kube-system"
                value="kube-system"
              />
              <el-option
                label="kube-public"
                value="kube-public"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="搜索">
            <el-input
              v-model.trim="sts.searchText"
              placeholder="搜索 StatefulSet"
              :prefix-icon="Search"
              clearable
              style="width: 200px"
            />
          </el-form-item>
        </FilterPanel>

        <StateWrapper
          :state="stsState"
          :error-text="sts.error"
          empty-text="暂无 StatefulSet"
        >
          <template #retry>
            <el-button
              type="primary"
              @click="sts.loadResources"
            >
              重试
            </el-button>
          </template>

          <StatefulSetTable
            :items="sts.items"
            @view-yaml="(s: K8sStatefulSetSummary) => viewYaml('statefulset', s.namespace, s.name)"
          />

          <div class="pagination-wrap">
            <el-pagination
              v-model:current-page="sts.page"
              :page-size="sts.pageSize"
              :total="sts.total"
              layout="total, prev, pager, next"
              background
              @current-change="sts.handlePageChange"
            />
          </div>
        </StateWrapper>
      </el-tab-pane>

      <el-tab-pane
        label="Jobs"
        name="jobs"
      >
        <FilterPanel
          @search="job.applyFilters"
          @reset="job.resetFilters"
        >
          <el-form-item label="命名空间">
            <el-select
              v-model="job.namespace"
              placeholder="全部"
              clearable
              style="width: 160px"
            >
              <el-option
                label="default"
                value="default"
              />
              <el-option
                label="kube-system"
                value="kube-system"
              />
              <el-option
                label="kube-public"
                value="kube-public"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="状态">
            <el-radio-group
              v-model="jobStatus"
              size="small"
            >
              <el-radio-button value="">
                全部
              </el-radio-button>
              <el-radio-button value="Running">
                运行中
              </el-radio-button>
              <el-radio-button value="Completed">
                已完成
              </el-radio-button>
              <el-radio-button value="Failed">
                失败
              </el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="搜索">
            <el-input
              v-model.trim="job.searchText"
              placeholder="搜索 Job"
              :prefix-icon="Search"
              clearable
              style="width: 200px"
            />
          </el-form-item>
        </FilterPanel>

        <StateWrapper
          :state="jobState"
          :error-text="job.error"
          empty-text="暂无 Job"
        >
          <template #retry>
            <el-button
              type="primary"
              @click="job.loadResources"
            >
              重试
            </el-button>
          </template>

          <JobTable
            :items="job.items"
            @view-yaml="(j: K8sJobSummary) => viewYaml('job', j.namespace, j.name)"
          />

          <div class="pagination-wrap">
            <el-pagination
              v-model:current-page="job.page"
              :page-size="job.pageSize"
              :total="job.total"
              layout="total, prev, pager, next"
              background
              @current-change="job.handlePageChange"
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

</style>
