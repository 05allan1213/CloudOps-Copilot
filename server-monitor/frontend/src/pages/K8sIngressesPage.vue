<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Search } from "@element-plus/icons-vue";

import PageHeader from "../components/common/PageHeader.vue";
import FilterPanel from "../components/common/FilterPanel.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import IngressTable from "../components/k8s/IngressTable.vue";
import { fetchK8sIngresses } from "../api/k8s";
import { usePagination } from "../composables/usePagination";
import type { K8sIngressSummary } from "../types";

const ingresses = ref<K8sIngressSummary[]>([]);
const loading = ref(false);
const error = ref("");

const selectedNamespace = ref("");
const searchInput = ref("");

const { page, pageSize, total, goToPage, resetPage } = usePagination(20);

const stateKey = computed(() => {
  if (loading.value) return "loading" as const;
  if (error.value) return "error" as const;
  if (ingresses.value.length === 0) return "empty" as const;
  return "default" as const;
});

async function loadIngresses() {
  loading.value = true;
  error.value = "";
  try {
    const result = await fetchK8sIngresses({
      namespace: selectedNamespace.value || undefined,
      search: searchInput.value || undefined,
      limit: pageSize.value,
    });
    ingresses.value = result.items;
    total.value = result.total;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载 Ingress 失败";
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  resetPage();
  loadIngresses();
}

function resetFilters() {
  selectedNamespace.value = "";
  searchInput.value = "";
  resetPage();
  loadIngresses();
}

function handlePageChange(newPage: number) {
  goToPage(newPage);
  loadIngresses();
}

onMounted(loadIngresses);
</script>

<template>
  <section class="k8s-ingresses-page">
    <PageHeader title="Ingress" />

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
          placeholder="搜索 Ingress 名称"
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
            <span class="panel-title-text">Ingress 列表</span>
          </div>
          <div class="panel-actions">
            <el-tag size="small" type="info">共 {{ total }} 条</el-tag>
          </div>
        </div>
      </template>

      <StateWrapper :state="stateKey" :error-text="error" empty-text="暂无 Ingress 数据">
        <template #retry>
          <el-button type="primary" @click="loadIngresses">重试</el-button>
        </template>

        <IngressTable :ingresses="ingresses" />

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
.k8s-ingresses-page {
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
