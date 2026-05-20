<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { Search } from "@element-plus/icons-vue";

import FilterPanel from "../components/common/FilterPanel.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import NodeTable from "../components/k8s/NodeTable.vue";
import { fetchK8sNodes } from "../api/k8s";
import type { K8sNodeWithHost, K8sNodeQuery } from "../types/k8s";

type NodeStatus = "" | "Ready" | "NotReady";

const nodes = ref<K8sNodeWithHost[]>([]);
const total = ref(0);
const loading = ref(true);
const errorText = ref("");

const statusFilter = ref<NodeStatus>("");
const roleFilter = ref("");
const searchInput = ref("");
const currentPage = ref(1);
const pageSize = ref(20);

const pageState = computed<"loading" | "error" | "default">(() => {
  if (loading.value) return "loading";
  if (errorText.value) return "error";
  return "default";
});

async function loadNodes() {
  try {
    loading.value = true;
    errorText.value = "";
    const query: K8sNodeQuery = {
      limit: pageSize.value,
    };
    if (statusFilter.value) {
      query.status = statusFilter.value;
    }
    if (roleFilter.value) {
      query.role = roleFilter.value;
    }
    if (searchInput.value) {
      query.search = searchInput.value;
    }
    const result = await fetchK8sNodes(query);
    nodes.value = result.items;
    total.value = result.total;
  } catch (err) {
    errorText.value = err instanceof Error ? err.message : "加载节点列表失败";
  } finally {
    loading.value = false;
  }
}

function handleSearch() {
  currentPage.value = 1;
  loadNodes();
}

function handleReset() {
  statusFilter.value = "";
  roleFilter.value = "";
  searchInput.value = "";
  currentPage.value = 1;
  loadNodes();
}

function handlePageChange(page: number) {
  currentPage.value = page;
  loadNodes();
}

onMounted(loadNodes);
</script>

<template>
  <FilterPanel
    @search="handleSearch"
    @reset="handleReset"
  >
    <el-form-item label="状态">
      <el-radio-group
        v-model="statusFilter"
        size="small"
      >
        <el-radio-button value="">
          全部
        </el-radio-button>
        <el-radio-button value="Ready">
          就绪
        </el-radio-button>
        <el-radio-button value="NotReady">
          未就绪
        </el-radio-button>
      </el-radio-group>
    </el-form-item>
    <el-form-item label="角色">
      <el-select
        v-model="roleFilter"
        placeholder="全部角色"
        clearable
        style="width: 200px"
      >
        <el-option
          value="control-plane"
          label="控制平面"
        />
        <el-option
          value="worker"
          label="工作节点"
        />
      </el-select>
    </el-form-item>
    <el-form-item label="搜索">
      <el-input
        v-model="searchInput"
        placeholder="搜索节点名称"
        :prefix-icon="Search"
        clearable
        style="width: 200px"
        @keyup.enter="handleSearch"
      />
    </el-form-item>
  </FilterPanel>

  <StateWrapper
    :state="pageState"
    :error-text="errorText"
  >
    <template #retry>
      <el-button
        type="primary"
        @click="loadNodes"
      >
        重试
      </el-button>
    </template>

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
            <span class="panel-title-text">节点列表</span>
          </div>
          <div class="panel-actions">
            <el-tag
              size="small"
              type="info"
            >
              共 {{ total }} 个节点
            </el-tag>
          </div>
        </div>
      </template>

      <NodeTable :nodes="nodes" />

      <div
        v-if="total > pageSize"
        class="pagination-wrapper"
      >
        <el-pagination
          layout="total, prev, pager, next"
          :total="total"
          :page-size="pageSize"
          :current-page="currentPage"
          @current-change="handlePageChange"
        />
      </div>
    </el-card>
  </StateWrapper>
</template>

<style scoped>
.panel :deep(.el-card__body) {
  padding: 16px 20px;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

</style>
