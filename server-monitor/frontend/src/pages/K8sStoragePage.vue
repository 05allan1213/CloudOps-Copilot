<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Search } from "@element-plus/icons-vue";

import PageHeader from "../components/common/PageHeader.vue";
import FilterPanel from "../components/common/FilterPanel.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import PVTable from "../components/k8s/PVTable.vue";
import PVCTable from "../components/k8s/PVCTable.vue";
import { fetchK8sPVs, fetchK8sPVCs } from "../api/k8s";
import { usePagination } from "../composables/usePagination";
import type { K8sPVSummary, K8sPVCSummary } from "../types";

const activeTab = ref("pv");

const pvs = ref<K8sPVSummary[]>([]);
const pvcs = ref<K8sPVCSummary[]>([]);
const loading = ref(false);
const error = ref("");

const selectedNamespace = ref("");
const selectedStatus = ref("");
const searchInput = ref("");

const { page, pageSize, total, goToPage, resetPage } = usePagination(20);

const stateKey = computed(() => {
  if (loading.value) return "loading" as const;
  if (error.value) return "error" as const;
  const items = activeTab.value === "pv" ? pvs.value : pvcs.value;
  if (items.length === 0) return "empty" as const;
  return "default" as const;
});

async function loadData() {
  loading.value = true;
  error.value = "";
  try {
    if (activeTab.value === "pv") {
      const result = await fetchK8sPVs({
        status: selectedStatus.value || undefined,
        search: searchInput.value || undefined,
        limit: pageSize.value,
      });
      pvs.value = result.items;
      total.value = result.total;
    } else {
      const result = await fetchK8sPVCs({
        namespace: selectedNamespace.value || undefined,
        status: selectedStatus.value || undefined,
        search: searchInput.value || undefined,
        limit: pageSize.value,
      });
      pvcs.value = result.items;
      total.value = result.total;
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载存储数据失败";
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
  selectedStatus.value = "";
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
  selectedNamespace.value = "";
  selectedStatus.value = "";
  searchInput.value = "";
  loadData();
}

onMounted(loadData);
</script>

<template>
  <section class="k8s-storage-page">
    <PageHeader title="存储" />

    <FilterPanel
      @search="applyFilters"
      @reset="resetFilters"
    >
      <el-form-item
        v-if="activeTab === 'pvc'"
        label="命名空间"
      >
        <el-select
          v-model="selectedNamespace"
          placeholder="全部命名空间"
          clearable
          style="width: 200px"
        />
      </el-form-item>
      <el-form-item label="状态">
        <el-select
          v-model="selectedStatus"
          placeholder="全部状态"
          clearable
          style="width: 160px"
        >
          <el-option
            v-if="activeTab === 'pv'"
            label="Bound"
            value="Bound"
          />
          <el-option
            v-if="activeTab === 'pv'"
            label="Available"
            value="Available"
          />
          <el-option
            v-if="activeTab === 'pv'"
            label="Released"
            value="Released"
          />
          <el-option
            v-if="activeTab === 'pv'"
            label="Failed"
            value="Failed"
          />
          <el-option
            v-if="activeTab === 'pvc'"
            label="Bound"
            value="Bound"
          />
          <el-option
            v-if="activeTab === 'pvc'"
            label="Pending"
            value="Pending"
          />
          <el-option
            v-if="activeTab === 'pvc'"
            label="Lost"
            value="Lost"
          />
        </el-select>
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
            <span class="panel-title-text">存储资源</span>
          </div>
          <el-tabs
            v-model="activeTab"
            class="header-tabs"
            @tab-change="handleTabChange"
          >
            <el-tab-pane
              label="PersistentVolumes"
              name="pv"
            />
            <el-tab-pane
              label="PersistentVolumeClaims"
              name="pvc"
            />
          </el-tabs>
        </div>
      </template>

      <StateWrapper
        :state="stateKey"
        :error-text="error"
        empty-text="暂无存储数据"
      >
        <template #retry>
          <el-button
            type="primary"
            @click="loadData"
          >
            重试
          </el-button>
        </template>

        <PVTable
          v-if="activeTab === 'pv'"
          :pvs="pvs"
        />
        <PVCTable
          v-else
          :pvcs="pvcs"
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
  </section>
</template>

<style scoped>
.k8s-storage-page {
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
