<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { RouterLink } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { Refresh } from "@element-plus/icons-vue";

import {
  approveAction,
  executeAction,
  listActions,
  rejectAction,
} from "../api/actions";
import { formatTime } from "../utils/format";
import { usePagination } from "../composables/usePagination";
import FilterPanel from "../components/common/FilterPanel.vue";
import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";
import type { PendingAction } from "../types";

const actions = ref<PendingAction[]>([]);
const loading = ref(false);
const actingID = ref<number | null>(null);
const error = ref("");
const filters = reactive({
  status: "",
  risk_level: "",
  action_type: "",
});

const { page, pageSize, total, totalPages, goToPage, resetPage } = usePagination(50);

const stateKey = computed(() => {
  if (loading.value) return "loading" as const;
  if (error.value) return "error" as const;
  if (actions.value.length === 0) return "empty" as const;
  return "default" as const;
});

const approveDialogVisible = ref(false);
const rejectDialogVisible = ref(false);
const currentAction = ref<PendingAction | null>(null);
const approveComment = ref("");
const rejectReason = ref("");

function targetOf(action: PendingAction) {
  return `${action.namespace || "-"}/${action.target_name || "-"}`;
}

function riskTagType(level: string): "danger" | "warning" | "success" | "" {
  switch (level) {
    case "high":
      return "danger";
    case "medium":
      return "warning";
    case "low":
      return "success";
    default:
      return "";
  }
}

function statusTagType(status: string): "warning" | "success" | "danger" | "info" | "" {
  switch (status) {
    case "pending":
      return "warning";
    case "approved":
      return "info";
    case "executed":
      return "success";
    case "rejected":
    case "failed":
      return "danger";
    default:
      return "";
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
    error.value = err instanceof Error ? err.message : "加载待审批动作失败";
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

function openApproveDialog(action: PendingAction) {
  currentAction.value = action;
  approveComment.value = "";
  approveDialogVisible.value = true;
}

function openRejectDialog(action: PendingAction) {
  currentAction.value = action;
  rejectReason.value = "";
  rejectDialogVisible.value = true;
}

async function confirmApprove() {
  if (!currentAction.value) return;
  const id = currentAction.value.id;
  actingID.value = id;
  approveDialogVisible.value = false;
  try {
    await approveAction(id, approveComment.value);
    ElMessage.success("审批通过");
    await loadActions();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "审批失败";
  } finally {
    actingID.value = null;
  }
}

async function confirmReject() {
  if (!currentAction.value) return;
  if (!rejectReason.value.trim()) {
    ElMessage.warning("请输入拒绝原因");
    return;
  }
  const id = currentAction.value.id;
  actingID.value = id;
  rejectDialogVisible.value = false;
  try {
    await rejectAction(id, rejectReason.value);
    ElMessage.success("已拒绝");
    await loadActions();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "拒绝失败";
  } finally {
    actingID.value = null;
  }
}

async function confirmExecute(action: PendingAction) {
  try {
    await ElMessageBox.confirm(
      `确认执行 ${action.action_type} ${targetOf(action)}？`,
      "执行确认",
      {
        confirmButtonText: "执行",
        cancelButtonText: "取消",
        type: "warning",
      },
    );
    actingID.value = action.id;
    try {
      await executeAction(action.id);
      ElMessage.success("执行成功");
      await loadActions();
    } catch (err) {
      error.value = err instanceof Error ? err.message : "执行失败";
    } finally {
      actingID.value = null;
    }
  } catch {
    // cancelled
  }
}

onMounted(loadActions);
</script>

<template>
  <section class="actions-page">
    <PageHeader title="动作审批" subtitle="诊断建议生成的待审批动作，写操作默认需要 admin 审批。">
      <template #default>
        <el-button :icon="Refresh" :loading="loading" @click="loadActions">刷新</el-button>
      </template>
    </PageHeader>

    <FilterPanel @search="applyFilters" @reset="resetFilters">
      <el-form-item label="状态">
        <el-select v-model="filters.status" placeholder="全部状态" clearable style="width: 140px">
          <el-option label="pending" value="pending" />
          <el-option label="approved" value="approved" />
          <el-option label="rejected" value="rejected" />
          <el-option label="executed" value="executed" />
          <el-option label="failed" value="failed" />
        </el-select>
      </el-form-item>
      <el-form-item label="风险级别">
        <el-select v-model="filters.risk_level" placeholder="全部风险" clearable style="width: 140px">
          <el-option label="low" value="low" />
          <el-option label="medium" value="medium" />
          <el-option label="high" value="high" />
        </el-select>
      </el-form-item>
      <el-form-item label="动作类型">
        <el-input v-model.trim="filters.action_type" placeholder="action_type" clearable style="width: 160px" />
      </el-form-item>
    </FilterPanel>

    <StateWrapper :state="stateKey" :error-text="error" empty-text="暂无动作">
      <template #retry>
        <el-button type="primary" @click="loadActions">重试</el-button>
      </template>

      <el-table :data="actions" stripe style="width: 100%">
        <el-table-column label="ID" width="100">
          <template #default="{ row }">
            <RouterLink class="detail-link" :to="`/actions/${row.id}`">#{{ row.id }}</RouterLink>
          </template>
        </el-table-column>
        <el-table-column label="动作类型" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="mono-text">{{ row.action_type }}</span>
          </template>
        </el-table-column>
        <el-table-column label="目标" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">
            {{ targetOf(row) }}
          </template>
        </el-table-column>
        <el-table-column label="风险级别" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="riskTagType(row.risk_level)" size="small" effect="dark">
              {{ row.risk_level }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)" size="small">
              {{ row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="来源" width="100" prop="requested_by" show-overflow-tooltip />
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" align="center">
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'pending'"
              type="primary"
              link
              size="small"
              :loading="actingID === row.id"
              @click="openApproveDialog(row)"
            >
              批准
            </el-button>
            <el-button
              v-if="row.status === 'pending'"
              type="danger"
              link
              size="small"
              :loading="actingID === row.id"
              @click="openRejectDialog(row)"
            >
              拒绝
            </el-button>
            <el-button
              v-if="row.status === 'approved'"
              type="warning"
              link
              size="small"
              :loading="actingID === row.id"
              @click="confirmExecute(row)"
            >
              执行
            </el-button>
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

    <el-dialog v-model="approveDialogVisible" title="审批通过" width="460px">
      <el-form label-position="top">
        <el-form-item label="审批备注">
          <el-input
            v-model="approveComment"
            type="textarea"
            :rows="3"
            placeholder="可选：输入审批备注"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="approveDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmApprove">确认批准</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="rejectDialogVisible" title="拒绝动作" width="460px">
      <el-form label-position="top">
        <el-form-item label="拒绝原因" required>
          <el-input
            v-model="rejectReason"
            type="textarea"
            :rows="3"
            placeholder="必填：输入拒绝原因"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="rejectDialogVisible = false">取消</el-button>
        <el-button type="danger" @click="confirmReject">确认拒绝</el-button>
      </template>
    </el-dialog>
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
