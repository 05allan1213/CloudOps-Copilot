<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Search } from "@element-plus/icons-vue";

import PageHeader from "../components/common/PageHeader.vue";
import FilterPanel from "../components/common/FilterPanel.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import ResourceQuotaTable from "../components/k8s/ResourceQuotaTable.vue";
import LimitRangeTable from "../components/k8s/LimitRangeTable.vue";
import { fetchK8sResourceQuotas, fetchK8sLimitRanges } from "../api/k8s";
import { usePagination } from "../composables/usePagination";
import type { K8sResourceQuotaSummary, K8sLimitRangeSummary } from "../types";

const activeTab = ref("quota");

const quotas = ref<K8sResourceQuotaSummary[]>([]);
const limitranges = ref<K8sLimitRangeSummary[]>([]);
const loading = ref(false);
const error = ref("");

const selectedNamespace = ref("");
const searchInput = ref("");

const { page, pageSize, total, goToPage, resetPage } = usePagination(20);

const stateKey = computed(() => {
  if (loading.value) return "loading" as const;
  if (error.value) return "error" as const;
  const items = activeTab.value === "quota" ? quotas.value : limitranges.value;
  if (items.length === 0) return "empty" as const;
  return "default" as const;
});

async function loadData() {
  loading.value = true;
  error.value = "";
  try {
    if (activeTab.value === "quota") {
      const result = await fetchK8sResourceQuotas({
        namespace: selectedNamespace.value || undefined,
        search: searchInput.value || undefined,
        limit: pageSize.value,
      });
      quotas.value = result.items;
      total.value = result.total;
    } else {
      const result = await fetchK8sLimitRanges({
        namespace: selectedNamespace.value || undefined,
        search: searchInput.value || undefined,
        limit: pageSize.value,
      });
      limitranges.value = result.items;
      total.value = result.total;
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载配额数据失败";
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  resetPage();
  loadData();
}

function resetFilters() {
  selectedNamespace.value = "";
  searchInput.value = "";
  resetPage();
  loadData();
}

function handlePageChange(newPage: number) {
  goToPage(newPage);
  loadData();
}

function handleTabChange() {
  resetPage();
  loadData();
}

onMounted(loadData);
</script>

<template>
  <section class="k8s-quotas-page">
    <PageHeader title="Quotas" />

    <FilterPanel @search="applyFilters" @reset="resetFilters">
      <el-form-item label="命名空间">
        <el-select
          v-model="selectedNamespace"
          placeholder="全部命名空间"
          clearable
          style="width: 200px"
        />
      </el-form-item>
      <el-form-item label="搜索">
        <el-input
          v-model="searchInput"
          placeholder="搜索名称"
          :prefix-icon="Search"
          clearable
          style="width: 200px"
          @keyup.enter="applyFilters"
        />
      </el-form-item>
    </FilterPanel>

    <el-card shadow="never" class="panel">
      <template #header>
        <div class="panel-header">
          <div class="panel-title">
            <el-icon :size="18" color="var(--el-color-primary)"><Search /></el-icon>
            <span class="panel-title-text">资源配额</span>
          </div>
          <el-tabs v-model="activeTab" class="header-tabs" @tab-change="handleTabChange">
            <el-tab-pane label="ResourceQuota" name="quota" />
            <el-tab-pane label="LimitRange" name="limitrange" />
          </el-tabs>
        </div>
      </template>

      <StateWrapper :state="stateKey" :error-text="error" empty-text="暂无配额数据">
        <template #retry>
          <el-button type="primary" @click="loadData">重试</el-button>
        </template>

        <ResourceQuotaTable v-if="activeTab === 'quota'" :quotas="quotas" />
        <LimitRangeTable v-else :limitranges="limitranges" />

        <div class="pagination-wrap">
          <el-pagination
            v-model:current-page="page"
            :page-size="pageSize"
            :total="total"
            layout="total, prev, pager, next"
            background
            @current-change="handlePageChange"
          />
        </div>
      </StateWrapper>
    </el-card>
  </section>
</template>

<style scoped>
.k8s-quotas-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.panel :deep(.el-card__body) {
  padding: 16px 20px;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}

.panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.panel-title-text {
  font-size: 15px;
  font-weight: 600;
}

.header-tabs {
  flex: 1;
}

.pagination-wrap {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

@media (max-width: 768px) {
  .panel-header {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
