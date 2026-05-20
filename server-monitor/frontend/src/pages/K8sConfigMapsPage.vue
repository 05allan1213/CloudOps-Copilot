<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Search } from "@element-plus/icons-vue";

import PageHeader from "../components/common/PageHeader.vue";
import FilterPanel from "../components/common/FilterPanel.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import ConfigMapTable from "../components/k8s/ConfigMapTable.vue";
import { fetchK8sConfigMaps } from "../api/k8s";
import { usePagination } from "../composables/usePagination";
import type { K8sConfigMapSummary } from "../types";

const configmaps = ref<K8sConfigMapSummary[]>([]);
const loading = ref(false);
const error = ref("");

const selectedNamespace = ref("");
const searchInput = ref("");
const detailDialogVisible = ref(false);
const selectedConfigMap = ref<K8sConfigMapSummary | null>(null);

const { page, pageSize, total, goToPage, resetPage } = usePagination(20);

const stateKey = computed(() => {
  if (loading.value) return "loading" as const;
  if (error.value) return "error" as const;
  if (configmaps.value.length === 0) return "empty" as const;
  return "default" as const;
});

async function loadConfigMaps() {
  loading.value = true;
  error.value = "";
  try {
    const result = await fetchK8sConfigMaps({
      namespace: selectedNamespace.value || undefined,
      search: searchInput.value || undefined,
      limit: pageSize.value,
    });
    configmaps.value = result.items;
    total.value = result.total;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载 ConfigMaps 失败";
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  resetPage();
  loadConfigMaps();
}

function resetFilters() {
  selectedNamespace.value = "";
  searchInput.value = "";
  resetPage();
  loadConfigMaps();
}

function handlePageChange(newPage: number) {
  goToPage(newPage);
  loadConfigMaps();
}

function viewDetail(cm: K8sConfigMapSummary) {
  selectedConfigMap.value = cm;
  detailDialogVisible.value = true;
}

onMounted(loadConfigMaps);
</script>

<template>
  <section class="k8s-configmaps-page">
    <PageHeader title="配置项" />

    <FilterPanel
      @search="applyFilters"
      @reset="resetFilters"
    >
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
          placeholder="搜索 ConfigMap 名称"
          :prefix-icon="Search"
          clearable
          style="width: 200px"
          @keyup.enter="applyFilters"
        />
      </el-form-item>
    </FilterPanel>

    <el-card
      shadow="never"
      class="panel"
    >
      <template #header>
        <div class="panel-header">
          <div class="panel-title">
            <el-icon
              :size="18"
              color="var(--el-color-primary)"
            >
              <Search />
            </el-icon>
            <span class="panel-title-text">配置项列表</span>
          </div>
          <div class="panel-actions">
            <el-tag
              size="small"
              type="info"
            >
              共 {{ total }} 条
            </el-tag>
          </div>
        </div>
      </template>

      <StateWrapper
        :state="stateKey"
        :error-text="error"
        empty-text="暂无 ConfigMap 数据"
      >
        <template #retry>
          <el-button
            type="primary"
            @click="loadConfigMaps"
          >
            重试
          </el-button>
        </template>

        <ConfigMapTable
          :configmaps="configmaps"
          @view="viewDetail"
        />

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

    <el-dialog
      v-model="detailDialogVisible"
      :title="selectedConfigMap ? `ConfigMap: ${selectedConfigMap.name}` : 'ConfigMap 详情'"
      width="700px"
      destroy-on-close
    >
      <template v-if="selectedConfigMap">
        <el-descriptions
          :column="2"
          border
          size="small"
          class="cm-descriptions"
        >
          <el-descriptions-item label="命名空间">
            {{ selectedConfigMap.namespace }}
          </el-descriptions-item>
          <el-descriptions-item label="名称">
            {{ selectedConfigMap.name }}
          </el-descriptions-item>
        </el-descriptions>
        <div class="cm-data-section">
          <h4 class="cm-data-title">
            Data
          </h4>
          <div
            v-for="key in selectedConfigMap.data_keys"
            :key="key"
            class="cm-data-row"
          >
            <span class="cm-data-key">{{ key }}</span>
            <pre class="cm-data-value">{{ selectedConfigMap.data[key] }}</pre>
          </div>
          <el-empty
            v-if="!selectedConfigMap.data_keys?.length"
            description="无数据"
            :image-size="60"
          />
        </div>
      </template>
    </el-dialog>
  </section>
</template>

<style scoped>
.k8s-configmaps-page {
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

.cm-descriptions {
  margin-bottom: 16px;
}

.cm-data-section {
  margin-top: 8px;
}

.cm-data-title {
  font-size: 14px;
  font-weight: 600;
  margin-bottom: 8px;
  color: var(--el-text-color-primary);
}

.cm-data-row {
  display: flex;
  gap: 12px;
  margin-bottom: 8px;
  align-items: flex-start;
}

.cm-data-key {
  min-width: 120px;
  font-weight: 500;
  color: var(--el-text-color-regular);
  font-size: 13px;
}

.cm-data-value {
  flex: 1;
  margin: 0;
  padding: 8px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  font-size: 12px;
  font-family: monospace;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 200px;
  overflow-y: auto;
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
