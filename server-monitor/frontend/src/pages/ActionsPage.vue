<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { RouterLink } from "vue-router";
import { Refresh } from "@element-plus/icons-vue";

import { listActions } from "../api/actions";
import { formatTime } from "../utils/format";
import { usePagination } from "../composables/usePagination";
import { riskTagType, statusTagType } from "../composables/useTagTypes";
import FilterPanel from "../components/common/FilterPanel.vue";
import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import type { PendingAction } from "../types";

const actions = ref<PendingAction[]>([]);
const loading = ref(false);
const error = ref("");
const filters = reactive({
  status: "",
  risk_level: "",
  action_type: "",
});

const { page, pageSize, total, goToPage, resetPage } = usePagination(50);

const stateKey = computed(() => {
  if (loading.value) return "loading" as const;
  if (error.value) return "error" as const;
  if (actions.value.length === 0) return "empty" as const;
  return "default" as const;
});

function targetOf(action: PendingAction) {
  return `${action.namespace || "-"}/${action.target_name || "-"}`;
}

function riskLabel(level: string): string {
  switch (level) {
    case "low": return "低";
    case "medium": return "中";
    case "high": return "高";
    default: return level;
  }
}

function statusLabel(status: string): string {
  switch (status) {
    case "pending": return "待审批";
    case "approved": return "已审批";
    case "rejected": return "已拒绝";
    case "executed": return "已执行";
    case "failed": return "失败";
    default: return status;
  }
}

async function loadActions() {
  loading.value = true;
  error.value = "";
  try {
    const response = await listActions({
      status: filters.status || undefined,
      risk_level: filters.risk_level || undefined,
      action_type: filters.action_type || undefined,
      page: page.value,
      page_size: pageSize.value,
    });
    actions.value = response.items;
    total.value = response.total ?? 0;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "Legacy 动作历史不可用";
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  resetPage();
  loadActions();
}

function resetFilters() {
  filters.status = "";
  filters.risk_level = "";
  filters.action_type = "";
  resetPage();
  loadActions();
}

function handlePageChange(newPage: number) {
  goToPage(newPage);
  loadActions();
}

onMounted(loadActions);
</script>

<template>
  <section class="actions-page">
    <PageHeader
      title="动作历史（Deprecated）"
      subtitle="该页面仅保留兼容性历史查看；V2 基线不允许在此审批或执行 Kubernetes 写操作。"
    >
      <template #default>
        <el-button
          :icon="Refresh"
          :loading="loading"
          @click="loadActions"
        >
          刷新
        </el-button>
      </template>
    </PageHeader>

    <el-alert
      title="Legacy direct action execution is frozen. Use Incident Workbench remediation, approval and Verification."
      type="warning"
      show-icon
      :closable="false"
    />

    <FilterPanel
      @search="applyFilters"
      @reset="resetFilters"
    >
      <el-form-item label="状态">
        <el-select
          v-model="filters.status"
          placeholder="全部状态"
          clearable
          style="width: 140px"
        >
          <el-option
            label="待审批"
            value="pending"
          />
          <el-option
            label="已审批"
            value="approved"
          />
          <el-option
            label="已拒绝"
            value="rejected"
          />
          <el-option
            label="已执行"
            value="executed"
          />
          <el-option
            label="失败"
            value="failed"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="风险级别">
        <el-select
          v-model="filters.risk_level"
          placeholder="全部风险"
          clearable
          style="width: 140px"
        >
          <el-option
            label="低"
            value="low"
          />
          <el-option
            label="中"
            value="medium"
          />
          <el-option
            label="高"
            value="high"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="动作类型">
        <el-input
          v-model.trim="filters.action_type"
          placeholder="action_type"
          clearable
          style="width: 160px"
        />
      </el-form-item>
    </FilterPanel>

    <StateWrapper
      :state="stateKey"
      :error-text="error"
      empty-text="暂无动作"
    >
      <template #retry>
        <el-button
          type="primary"
          @click="loadActions"
        >
          重试
        </el-button>
      </template>

      <el-table
        :data="actions"
        stripe
        style="width: 100%"
      >
        <el-table-column
          label="ID"
          width="100"
        >
          <template #default="{ row }">
            <RouterLink
              class="detail-link"
              :to="`/actions/${row.id}`"
            >
              #{{ row.id }}
            </RouterLink>
          </template>
        </el-table-column>
        <el-table-column
          label="动作类型"
          min-width="140"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <span class="mono-text">{{ row.action_type }}</span>
          </template>
        </el-table-column>
        <el-table-column
          label="目标"
          min-width="140"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ targetOf(row) }}
          </template>
        </el-table-column>
        <el-table-column
          label="风险级别"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="riskTagType(row.risk_level)"
              size="small"
              effect="dark"
            >
              {{ riskLabel(row.risk_level) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="状态"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="statusTagType(row.status)"
              size="small"
            >
              {{ statusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="来源"
          width="100"
          prop="requested_by"
          show-overflow-tooltip
        />
        <el-table-column
          label="创建时间"
          width="170"
        >
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="120"
          align="center"
        >
          <template #default="{ row }">
            <RouterLink :to="`/actions/${row.id}`">
              查看历史
            </RouterLink>
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
.actions-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.detail-link {
  color: var(--el-color-primary);
  font-weight: 600;
  text-decoration: none;
}

</style>
