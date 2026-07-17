<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowLeft } from "@element-plus/icons-vue";

import { getAction } from "../api/actions";
import { formatTime } from "../utils/format";
import { statusTagType, riskTagType } from "../composables/useTagTypes";
import type { PendingAction } from "../types";
import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";

const route = useRoute();
const router = useRouter();
const action = ref<PendingAction | null>(null);
const loading = ref(false);
const error = ref("");

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

onMounted(loadAction);
</script>

<template>
  <section class="action-detail-page">
    <PageHeader
      :title="`#${actionID} 动作详情`"
      subtitle="只读兼容性历史；Legacy direct action execution 已冻结。"
    >
      <el-button
        text
        :icon="ArrowLeft"
        @click="router.push('/actions')"
      >
        返回动作列表
      </el-button>
    </PageHeader>

    <el-alert
      title="此页面不会批准、拒绝或执行动作。V2 写操作必须经过 Incident remediation、Approval 与 Verification。"
      type="warning"
      show-icon
      :closable="false"
    />

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
