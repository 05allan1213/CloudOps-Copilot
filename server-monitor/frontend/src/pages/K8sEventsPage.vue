<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Search } from "@element-plus/icons-vue";

import PageHeader from "../components/common/PageHeader.vue";
import FilterPanel from "../components/common/FilterPanel.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import EventTable from "../components/k8s/EventTable.vue";
import { fetchK8sEvents } from "../api/k8s";
import { usePagination } from "../composables/usePagination";
import { useK8sEventsWebSocket } from "../composables/useK8sEventsWebSocket";
import type { K8sEventSummary } from "../types";

const events = ref<K8sEventSummary[]>([]);
const loading = ref(false);
const error = ref("");

const selectedType = ref("all");
const selectedNamespace = ref("");
const searchInput = ref("");

const { page, pageSize, total, goToPage, resetPage } = usePagination(20);

const { connectionState, connect: connectWs } = useK8sEventsWebSocket((event: K8sEventSummary) => {
  events.value.unshift(event);
  total.value += 1;
});

const stateKey = computed(() => {
  if (loading.value) return "loading" as const;
  if (error.value) return "error" as const;
  if (events.value.length === 0) return "empty" as const;
  return "default" as const;
});

async function loadEvents() {
  loading.value = true;
  error.value = "";
  try {
    const result = await fetchK8sEvents({
      namespace: selectedNamespace.value || undefined,
      type: selectedType.value !== "all" ? selectedType.value : undefined,
      search: searchInput.value || undefined,
      limit: pageSize.value,
    });
    events.value = result.items;
    total.value = result.total;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载 Events 失败";
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  resetPage();
  loadEvents();
}

function resetFilters() {
  selectedType.value = "all";
  selectedNamespace.value = "";
  searchInput.value = "";
  resetPage();
  loadEvents();
}

function handlePageChange(newPage: number) {
  goToPage(newPage);
  loadEvents();
}

onMounted(() => {
  loadEvents();
  connectWs();
});
</script>

<template>
  <section class="k8s-events-page">
    <PageHeader title="事件" />

    <FilterPanel
      @search="applyFilters"
      @reset="resetFilters"
    >
      <el-form-item label="类型">
        <el-radio-group
          v-model="selectedType"
          size="small"
        >
          <el-radio-button value="all">
            全部
          </el-radio-button>
          <el-radio-button value="Warning">
            Warning
          </el-radio-button>
          <el-radio-button value="Normal">
            Normal
          </el-radio-button>
        </el-radio-group>
      </el-form-item>
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
          placeholder="搜索 Event 名称"
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
            <span class="panel-title-text">事件列表</span>
          </div>
          <div class="panel-actions">
            <el-tag
              v-if="connectionState === 'connected'"
              size="small"
              type="success"
            >
              实时
            </el-tag>
            <el-tag
              v-else-if="connectionState === 'connecting'"
              size="small"
              type="warning"
            >
              连接中
            </el-tag>
            <el-tag
              v-else
              size="small"
              type="info"
            >
              离线
            </el-tag>
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
        empty-text="暂无 Event 数据"
      >
        <template #retry>
          <el-button
            type="primary"
            @click="loadEvents"
          >
            重试
          </el-button>
        </template>

        <EventTable :events="events" />

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
.k8s-events-page {
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
