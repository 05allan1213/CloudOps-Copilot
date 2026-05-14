<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { RouterLink } from "vue-router";

import { fetchDiagnosisList, type DiagnosisQuery } from "../api/diagnosis";
import { formatTime } from "../utils/format";
import { usePagination } from "../composables/usePagination";
import FilterPanel from "../components/common/FilterPanel.vue";
import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import type { DiagnosisListResponse } from "../types";

const reports = ref<DiagnosisListResponse>({
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
});
const loading = ref(false);
const error = ref("");
const filters = reactive<DiagnosisQuery>({
  status: "",
  trigger_type: "",
  page: 1,
  page_size: 20,
});

const { page, pageSize, total, goToPage, resetPage } = usePagination(20);

const stateKey = computed(() => {
  if (loading.value) return "loading" as const;
  if (error.value) return "error" as const;
  if (reports.value.items.length === 0) return "empty" as const;
  return "default" as const;
});

function statusTagType(value: string): "info" | "success" | "danger" | "warning" | "" {
  switch (value) {
    case "pending":
      return "info";
    case "running":
      return "warning";
    case "completed":
      return "success";
    case "failed":
      return "danger";
    default:
      return "";
  }
}

function statusLabel(value: string) {
  switch (value) {
    case "pending":
      return "等待中";
    case "running":
      return "诊断中";
    case "completed":
      return "已完成";
    case "failed":
      return "失败";
    default:
      return value || "-";
  }
}

function triggerTagType(value: string): "" | "primary" | "success" | "warning" {
  switch (value) {
    case "manual":
      return "primary";
    case "chat":
      return "success";
    case "auto":
      return "warning";
    default:
      return "";
  }
}

function triggerLabel(value: string) {
  switch (value) {
    case "manual":
      return "手动";
    case "chat":
      return "对话";
    case "auto":
      return "自动";
    default:
      return value || "-";
  }
}

function formatPercent(value: number) {
  return `${Math.round(value * 100)}%`;
}

async function loadReports() {
  loading.value = true;
  error.value = "";
  try {
    filters.page = page.value;
    filters.page_size = pageSize.value;
    reports.value = await fetchDiagnosisList(filters);
    total.value = reports.value.total;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载诊断报告失败";
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  resetPage();
  loadReports();
}

function resetFilters() {
  filters.status = "";
  filters.trigger_type = "";
  resetPage();
  loadReports();
}

function handlePageChange(newPage: number) {
  goToPage(newPage);
  loadReports();
}

onMounted(loadReports);
</script>

<template>
  <section class="diagnosis-page">
    <PageHeader title="诊断报告" subtitle="查看手动或 Copilot 触发的告警诊断结果。" />

    <FilterPanel @search="applyFilters" @reset="resetFilters">
      <el-form-item label="状态">
        <el-select v-model="filters.status" placeholder="全部" clearable style="width: 140px">
          <el-option label="pending" value="pending" />
          <el-option label="running" value="running" />
          <el-option label="completed" value="completed" />
          <el-option label="failed" value="failed" />
        </el-select>
      </el-form-item>
      <el-form-item label="来源">
        <el-select v-model="filters.trigger_type" placeholder="全部" clearable style="width: 140px">
          <el-option label="手动" value="manual" />
          <el-option label="对话" value="chat" />
          <el-option label="自动" value="auto" />
        </el-select>
      </el-form-item>
    </FilterPanel>

    <StateWrapper :state="stateKey" :error-text="error" empty-text="暂无诊断报告">
      <template #retry>
        <el-button type="primary" @click="loadReports">重试</el-button>
      </template>

      <el-table :data="reports.items" stripe style="width: 100%">
        <el-table-column label="ID" width="100">
          <template #default="{ row }">
            <RouterLink class="detail-link" :to="`/diagnosis/${row.id}`">#{{ row.id }}</RouterLink>
          </template>
        </el-table-column>
        <el-table-column label="告警名" min-width="140" prop="alert_name" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.alert_name || "-" }}
          </template>
        </el-table-column>
        <el-table-column label="目标" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="mono-text">{{ row.target_name || "-" }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)" size="small">
              {{ statusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="来源" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="triggerTagType(row.trigger_type)" size="small" effect="plain">
              {{ triggerLabel(row.trigger_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="置信度" width="90" align="center">
          <template #default="{ row }">
            {{ formatPercent(row.confidence) }}
          </template>
        </el-table-column>
        <el-table-column label="摘要" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.summary || "-" }}
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
      </el-table>

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
  </section>
</template>

<style scoped>
.diagnosis-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.detail-link {
  color: var(--el-color-primary);
  font-weight: 600;
  text-decoration: none;
}

.mono-text {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.82rem;
}

.pagination-wrap {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
