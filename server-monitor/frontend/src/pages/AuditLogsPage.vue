<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { Refresh } from "@element-plus/icons-vue";

import { listAuditLogs } from "../api/auditLogs";
import { formatTime } from "../utils/format";
import { usePagination } from "../composables/usePagination";
import FilterPanel from "../components/common/FilterPanel.vue";
import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import type { AuditLog } from "../types";

const logs = ref<AuditLog[]>([]);
const loading = ref(false);
const error = ref("");
const filters = reactive({
  action: "",
  result: "",
  actor: "",
});

const { page, pageSize, total, goToPage, resetPage } = usePagination(50);

const actionOptions = [
  { value: "", label: "全部动作" },
  { value: "action.create_pending", label: "创建待审批" },
  { value: "action.approve", label: "审批通过" },
  { value: "action.reject", label: "审批拒绝" },
  { value: "action.execute", label: "执行动作" },
  { value: "k8s.restart_deployment", label: "K8s 重启 Deployment" },
  { value: "k8s.scale_deployment", label: "K8s 扩缩 Deployment" },
];

const stateKey = computed(() => {
  if (loading.value) return "loading" as const;
  if (error.value) return "error" as const;
  if (logs.value.length === 0) return "empty" as const;
  return "default" as const;
});

function requestActionType(log: AuditLog): string {
  const request = log.request;
  if (!request || typeof request.action_type !== "string") {
    return "-";
  }
  return request.action_type;
}

function resultTagType(result: string): "success" | "danger" | "warning" | "info" | "" {
  switch (result) {
    case "success":
      return "success";
    case "failure":
      return "danger";
    case "denied":
      return "warning";
    case "timeout":
      return "info";
    default:
      return "";
  }
}

async function loadLogs() {
  loading.value = true;
  error.value = "";
  try {
    const response = await listAuditLogs({
      action: filters.action || undefined,
      result: filters.result || undefined,
      actor: filters.actor || undefined,
      page: page.value,
      page_size: pageSize.value,
    });
    logs.value = response.items;
    total.value = response.total ?? 0;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载审计日志失败";
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  resetPage();
  loadLogs();
}

function resetFilters() {
  filters.action = "";
  filters.result = "";
  filters.actor = "";
  resetPage();
  loadLogs();
}

function handlePageChange(newPage: number) {
  goToPage(newPage);
  loadLogs();
}

onMounted(loadLogs);
</script>

<template>
  <section class="audit-page">
    <PageHeader
      title="审计日志"
      subtitle="动作创建、审批、拒绝、执行和权限拒绝的可追溯记录。"
    >
      <template #default>
        <el-button
          :icon="Refresh"
          :loading="loading"
          @click="loadLogs"
        >
          刷新
        </el-button>
      </template>
    </PageHeader>

    <FilterPanel
      @search="applyFilters"
      @reset="resetFilters"
    >
      <el-form-item label="动作">
        <el-select
          v-model="filters.action"
          placeholder="全部动作"
          clearable
          style="width: 180px"
        >
          <el-option
            v-for="option in actionOptions"
            :key="option.value"
            :label="option.label"
            :value="option.value"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="结果">
        <el-select
          v-model="filters.result"
          placeholder="全部结果"
          clearable
          style="width: 140px"
        >
          <el-option
            label="success"
            value="success"
          />
          <el-option
            label="failure"
            value="failure"
          />
          <el-option
            label="denied"
            value="denied"
          />
          <el-option
            label="timeout"
            value="timeout"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="操作者">
        <el-input
          v-model.trim="filters.actor"
          placeholder="actor"
          clearable
          style="width: 160px"
        />
      </el-form-item>
    </FilterPanel>

    <StateWrapper
      :state="stateKey"
      :error-text="error"
      empty-text="暂无审计日志"
    >
      <template #retry>
        <el-button
          type="primary"
          @click="loadLogs"
        >
          重试
        </el-button>
      </template>

      <el-table
        :data="logs"
        stripe
        style="width: 100%"
      >
        <el-table-column
          label="时间"
          width="170"
        >
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作者"
          min-width="120"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ row.actor }} · {{ row.actor_role }}
          </template>
        </el-table-column>
        <el-table-column
          label="动作"
          min-width="160"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <span class="mono-text">{{ row.action }}</span>
          </template>
        </el-table-column>
        <el-table-column
          label="动作类型"
          min-width="120"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <span class="mono-text">{{ requestActionType(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column
          label="资源"
          min-width="140"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ row.resource_type }} #{{ row.resource_id }}
          </template>
        </el-table-column>
        <el-table-column
          label="结果"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="resultTagType(row.result)"
              size="small"
            >
              {{ row.result }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="trace_id"
          min-width="120"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <span class="mono-text">{{ row.trace_id || "-" }}</span>
          </template>
        </el-table-column>
        <el-table-column
          label="错误"
          min-width="140"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <span
              v-if="row.error_message"
              class="error-text"
            >{{ row.error_message }}</span>
            <span
              v-else
              class="text-muted"
            >-</span>
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
.audit-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.error-text {
  color: var(--el-color-danger);
  font-size: 13px;
}

.text-muted {
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}

</style>
