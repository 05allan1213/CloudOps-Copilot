<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Search } from "@element-plus/icons-vue";

import PageHeader from "../components/common/PageHeader.vue";
import FilterPanel from "../components/common/FilterPanel.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import HPATable from "../components/k8s/HPATable.vue";
import { fetchK8sHPAs } from "../api/k8s";
import { usePagination } from "../composables/usePagination";
import type { K8sHPASummary } from "../types";

const hpas = ref<K8sHPASummary[]>([]);
const loading = ref(false);
const error = ref("");

const selectedNamespace = ref("");
const searchInput = ref("");

const { page, pageSize, total, goToPage, resetPage } = usePagination(20);

const stateKey = computed(() => {
  if (loading.value) return "loading" as const;
  if (error.value) return "error" as const;
  if (hpas.value.length === 0) return "empty" as const;
  return "default" as const;
});

async function loadHPAs() {
  loading.value = true;
  error.value = "";
  try {
    const result = await fetchK8sHPAs({
      namespace: selectedNamespace.value || undefined,
      search: searchInput.value || undefined,
      limit: pageSize.value,
    });
    hpas.value = result.items;
    total.value = result.total;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载 HPA 数据失败";
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  resetPage();
  loadHPAs();
}

function resetFilters() {
  selectedNamespace.value = "";
  searchInput.value = "";
  resetPage();
  loadHPAs();
}

function handlePageChange(newPage: number) {
  goToPage(newPage);
  loadHPAs();
}

onMounted(loadHPAs);
</script>

<template>
  <section class="k8s-hpa-page">
    <PageHeader title="HPA" />

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
          placeholder="搜索 HPA 名称"
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
            <span class="panel-title-text">HorizontalPodAutoscaler 列表</span>
          </div>
          <div class="panel-actions">
            <el-tag size="small" type="info">共 {{ total }} 条</el-tag>
          </div>
        </div>
      </template>

      <StateWrapper :state="stateKey" :error-text="error" empty-text="暂无 HPA 数据">
        <template #retry>
          <el-button type="primary" @click="loadHPAs">重试</el-button>
        </template>

        <HPATable :hpas="hpas" />

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
.k8s-hpa-page {
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

.panel-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
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

  .panel-actions {
    width: 100%;
  }
}
</style>
