<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowLeft } from "@element-plus/icons-vue";
import { ElMessage, ElMessageBox } from "element-plus";

import { approveAction, executeAction, getAction, rejectAction } from "../api/actions";
import { formatTime } from "../utils/format";
import { statusTagType, riskTagType } from "../composables/useTagTypes";
import type { PendingAction } from "../types";
import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";

const route = useRoute();
const router = useRouter();
const action = ref<PendingAction | null>(null);
const loading = ref(false);
const acting = ref(false);
const error = ref("");

const approveDialogVisible = ref(false);
const rejectDialogVisible = ref(false);
const approveComment = ref("");
const rejectReason = ref("");

const actionID = computed(() => Number(route.params.id));

const stateKey = computed(() => {
  if (loading.value) return "loading";
  if (error.value) return "error";
  if (!action.value) return "empty";
  return "default";
});

function formatJSON(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2);
}

const resultSummary = computed(() => {
  const result = action.value?.result;
  if (!result) return [];
  return [
    ["目标", result.target],
    ["旧副本数", result.old_replicas],
    ["新副本数", result.new_replicas ?? result.replicas],
    ["Ready 副本", result.ready_replicas],
    ["旧重启标记", result.old_annotation],
    ["新重启标记", result.new_annotation],
    ["消息", result.message],
  ].filter((item): item is [string, string | number | boolean] => item[1] !== undefined && item[1] !== null && item[1] !== "");
});

async function loadAction() {
  if (!Number.isFinite(actionID.value) || actionID.value <= 0) {
    error.value = "无效动作 ID";
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    action.value = await getAction(actionID.value);
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载动作失败";
  } finally {
    loading.value = false;
  }
}

function openApproveDialog() {
  approveComment.value = "";
  approveDialogVisible.value = true;
}

function openRejectDialog() {
  rejectReason.value = "";
  rejectDialogVisible.value = true;
}

async function handleApprove() {
  if (!action.value) return;
  acting.value = true;
  try {
    action.value = await approveAction(action.value.id, approveComment.value);
    ElMessage.success("动作已批准");
    approveDialogVisible.value = false;
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "批准失败");
    await loadAction();
  } finally {
    acting.value = false;
  }
}

async function handleReject() {
  if (!rejectReason.value.trim()) {
    ElMessage.warning("请输入拒绝原因");
    return;
  }
  if (!action.value) return;
  acting.value = true;
  try {
    action.value = await rejectAction(action.value.id, rejectReason.value.trim());
    ElMessage.success("动作已拒绝");
    rejectDialogVisible.value = false;
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "拒绝失败");
    await loadAction();
  } finally {
    acting.value = false;
  }
}

async function handleExecute() {
  if (!action.value) return;
  try {
    await ElMessageBox.confirm("确认执行该动作？此操作不可撤销。", "执行确认", {
      confirmButtonText: "执行",
      cancelButtonText: "取消",
      type: "warning",
    });
  } catch {
    return;
  }
  acting.value = true;
  try {
    action.value = await executeAction(action.value.id);
    ElMessage.success("动作已执行");
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "执行失败");
    await loadAction();
  } finally {
    acting.value = false;
  }
}

onMounted(loadAction);
</script>

<template>
  <section class="action-detail-page">
    <PageHeader
      :title="`#${actionID} 动作详情`"
      subtitle="查看和管理审批动作"
    >
      <el-button
        text
        :icon="ArrowLeft"
        @click="router.push('/actions')"
      >
        返回动作列表
      </el-button>
    </PageHeader>

    <StateWrapper
      :state="stateKey"
      :error-text="error"
      empty-text="动作不存在"
    >
      <template #retry>
        <el-button
          type="primary"
          @click="loadAction"
        >
          重试
        </el-button>
      </template>

      <template v-if="action">
        <el-card
          shadow="never"
          class="action-header-card"
        >
          <div class="action-header">
            <div class="action-header-info">
              <h3>{{ action.action_type }}</h3>
              <div class="action-meta">
                <span>{{ action.namespace }}/{{ action.target_name }}</span>
                <el-tag
                  :type="statusTagType(action.status)"
                  size="small"
                >
                  {{ action.status }}
                </el-tag>
                <el-tag
                  :type="riskTagType(action.risk_level)"
                  size="small"
                  effect="plain"
                >
                  {{ action.risk_level }}
                </el-tag>
              </div>
            </div>
            <div class="action-buttons">
              <el-button
                v-if="action.status === 'pending'"
                type="success"
                :loading="acting"
                @click="openApproveDialog"
              >
                批准
              </el-button>
              <el-button
                v-if="action.status === 'pending'"
                type="danger"
                :loading="acting"
                @click="openRejectDialog"
              >
                拒绝
              </el-button>
              <el-button
                v-if="action.status === 'approved'"
                type="primary"
                :loading="acting"
                @click="handleExecute"
              >
                执行
              </el-button>
            </div>
          </div>
        </el-card>

        <el-card shadow="never">
          <el-descriptions
            :column="3"
            border
          >
            <el-descriptions-item label="风险级别">
              <el-tag
                :type="riskTagType(action.risk_level)"
                size="small"
                effect="plain"
              >
                {{ action.risk_level }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="来源">
              {{ action.requested_by }}
            </el-descriptions-item>
            <el-descriptions-item label="关联诊断">
              #{{ action.diagnosis_report_id || "-" }}
            </el-descriptions-item>
            <el-descriptions-item label="创建时间">
              {{ formatTime(action.created_at) }}
            </el-descriptions-item>
            <el-descriptions-item label="审批人">
              {{ action.approved_by || "-" }}
            </el-descriptions-item>
            <el-descriptions-item label="执行人">
              {{ action.executed_by || "-" }}
            </el-descriptions-item>
          </el-descriptions>
        </el-card>

        <el-card shadow="never">
          <template #header>
            <span class="card-title">参数</span>
          </template>
          <el-collapse>
            <el-collapse-item title="查看参数 JSON">
              <pre class="json-content">{{ formatJSON(action.params) }}</pre>
            </el-collapse-item>
          </el-collapse>
        </el-card>

        <el-card shadow="never">
          <template #header>
            <span class="card-title">执行结果</span>
          </template>
          <template v-if="resultSummary.length">
            <el-descriptions
              :column="3"
              border
              style="margin-bottom: 16px"
            >
              <el-descriptions-item
                v-for="[label, value] in resultSummary"
                :key="label"
                :label="label"
              >
                {{ value }}
              </el-descriptions-item>
            </el-descriptions>
          </template>
          <el-collapse>
            <el-collapse-item title="查看完整结果 JSON">
              <pre class="json-content">{{ formatJSON(action.result) }}</pre>
            </el-collapse-item>
          </el-collapse>
          <el-alert
            v-if="action.error_message"
            :title="action.error_message"
            type="error"
            show-icon
            :closable="false"
            style="margin-top: 12px"
          />
        </el-card>

        <el-dialog
          v-model="approveDialogVisible"
          title="批准动作"
          width="480px"
          :close-on-click-modal="false"
        >
          <el-form label-position="top">
            <el-form-item label="审批备注">
              <el-input
                v-model="approveComment"
                type="textarea"
                :rows="3"
                placeholder="请输入审批备注（可选）"
              />
            </el-form-item>
          </el-form>
          <template #footer>
            <el-button @click="approveDialogVisible = false">
              取消
            </el-button>
            <el-button
              type="success"
              :loading="acting"
              @click="handleApprove"
            >
              确认批准
            </el-button>
          </template>
        </el-dialog>

        <el-dialog
          v-model="rejectDialogVisible"
          title="拒绝动作"
          width="480px"
          :close-on-click-modal="false"
        >
          <el-form label-position="top">
            <el-form-item
              label="拒绝原因"
              required
            >
              <el-input
                v-model="rejectReason"
                type="textarea"
                :rows="3"
                placeholder="请输入拒绝原因（必填）"
              />
            </el-form-item>
          </el-form>
          <template #footer>
            <el-button @click="rejectDialogVisible = false">
              取消
            </el-button>
            <el-button
              type="danger"
              :loading="acting"
              @click="handleReject"
            >
              确认拒绝
            </el-button>
          </template>
        </el-dialog>
      </template>
    </StateWrapper>
  </section>
</template>

<style scoped>
.action-detail-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.action-header-card :deep(.el-card__body) {
  padding: 20px;
}

.action-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.action-header-info h3 {
  font-size: 16px;
  font-weight: 600;
  margin: 0 0 4px;
}

.action-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.action-buttons {
  display: flex;
  gap: 8px;
}

.card-title {
  font-size: 14px;
  font-weight: 600;
}

.json-content {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

@media (max-width: 768px) {
  .action-header {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
