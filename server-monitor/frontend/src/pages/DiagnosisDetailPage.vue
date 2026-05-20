<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { ElMessageBox } from "element-plus";

import { createActionsFromDiagnosis } from "../api/actions";
import { fetchDiagnosis } from "../api/diagnosis";
import { formatTime } from "../utils/format";
import { useAuthStore } from "../stores/auth";
import StateWrapper from "../components/common/StateWrapper.vue";
import DiagnosisSummary from "../components/diagnosis/DiagnosisSummary.vue";
import MetricEvidence from "../components/diagnosis/MetricEvidence.vue";
import K8sEvidence from "../components/diagnosis/K8sEvidence.vue";
import RuleAnalysis from "../components/diagnosis/RuleAnalysis.vue";
import RunbookHits from "../components/diagnosis/RunbookHits.vue";
import FeedbackSection from "../components/diagnosis/FeedbackSection.vue";
import type { DiagnosisReport } from "../types";

const route = useRoute();
const auth = useAuthStore();
const report = ref<DiagnosisReport | null>(null);
const loading = ref(false);
const creatingActions = ref(false);
const error = ref("");
const actionMessage = ref("");

const stateKey = computed(() => {
  if (loading.value) return "loading" as const;
  if (error.value) return "error" as const;
  return "default" as const;
});

const metrics = computed(() => report.value?.evidence?.metrics ?? []);
const ruleResults = computed(() => report.value?.rule_analysis?.results ?? []);
const actions = computed(() => report.value?.recommended_actions ?? []);
const collectionErrors = computed(() => report.value?.evidence?.collection_errors ?? []);
const k8sEvidence = computed(() => report.value?.evidence?.k8s);
const runbooks = computed(() => {
  const direct = report.value?.runbooks ?? [];
  if (direct.length > 0) return direct;
  return report.value?.evidence?.runbooks ?? [];
});

function formatJSON(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2);
}

function statusLabel(status: string) {
  switch (status) {
    case "pending": return "等待中";
    case "running": return "诊断中";
    case "completed": return "已完成";
    case "failed": return "失败";
    default: return status || "-";
  }
}

async function loadReport() {
  const id = Number(route.params.id);
  if (!Number.isFinite(id) || id <= 0) {
    error.value = "无效诊断报告 ID";
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    report.value = await fetchDiagnosis(id);
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载诊断报告失败";
  } finally {
    loading.value = false;
  }
}

async function createApprovalActions() {
  if (!report.value) return;
  try {
    await ElMessageBox.confirm(
      "确认创建审批动作？将根据建议动作生成待审批记录。",
      "创建确认",
      { confirmButtonText: "创建", cancelButtonText: "取消", type: "warning" },
    );
  } catch {
    return;
  }
  creatingActions.value = true;
  actionMessage.value = "";
  error.value = "";
  try {
    const selectedTypes = actions.value
      .filter((action) => action.requires_approval)
      .map((action) => action.type);
    const result = await createActionsFromDiagnosis(report.value.id, selectedTypes);
    actionMessage.value = `已创建 ${result.created.length} 条，跳过 ${result.skipped.length} 条`;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "创建待审批动作失败";
  } finally {
    creatingActions.value = false;
  }
}

onMounted(loadReport);
</script>

<template>
  <section class="detail-page">
    <StateWrapper
      :state="stateKey"
      :error-text="error"
      empty-text="诊断报告不存在"
    >
      <template #retry>
        <el-button
          type="primary"
          @click="loadReport"
        >
          重试
        </el-button>
      </template>

      <template v-if="report">
        <el-card shadow="never">
          <el-descriptions
            :column="3"
            border
          >
            <el-descriptions-item label="ID">
              #{{ report.id }}
            </el-descriptions-item>
            <el-descriptions-item label="告警名">
              {{ report.alert_name || "-" }}
            </el-descriptions-item>
            <el-descriptions-item label="目标">
              {{ report.target_name || "-" }}
            </el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag
                :type="report.status === 'completed' ? 'success' : report.status === 'failed' ? 'danger' : 'info'"
                size="small"
              >
                {{ statusLabel(report.status) }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="置信度">
              <el-progress
                :percentage="Math.round(report.confidence * 100)"
                :stroke-width="14"
                :text-inside="true"
                style="width: 160px"
              />
            </el-descriptions-item>
            <el-descriptions-item label="创建时间">
              {{ formatTime(report.created_at) }}
            </el-descriptions-item>
          </el-descriptions>
        </el-card>

        <FeedbackSection
          v-if="report.status === 'completed'"
          :report-id="report.id"
          :existing-feedback="report.my_feedback"
        />

        <DiagnosisSummary
          :summary="report.summary"
          :root-cause="report.root_cause"
        />

        <div class="grid-panels">
          <el-card shadow="never">
            <template #header>
              <div class="panel-header-row">
                <span class="card-title">建议动作</span>
                <el-button
                  v-if="auth.isAdmin && actions.some((a) => a.requires_approval)"
                  type="primary"
                  size="small"
                  :loading="creatingActions"
                  @click="createApprovalActions"
                >
                  创建审批动作
                </el-button>
              </div>
            </template>
            <div
              v-if="actionMessage"
              class="action-message"
            >
              {{ actionMessage }}
            </div>
            <el-empty
              v-if="actions.length === 0"
              description="暂无建议"
              :image-size="32"
            />
            <ul
              v-else
              class="action-list"
            >
              <li
                v-for="action in actions"
                :key="`${action.type}-${action.description}`"
              >
                <strong>{{ action.description }}</strong>
                <span>{{ action.type }} · {{ action.risk }} · {{ action.requires_approval ? "需审批" : "只读建议" }}</span>
              </li>
            </ul>
          </el-card>

          <MetricEvidence :metrics="metrics" />
        </div>

        <RuleAnalysis :rule-results="ruleResults" />

        <K8sEvidence
          v-if="k8sEvidence"
          :k8s-evidence="k8sEvidence"
        />

        <RunbookHits :runbooks="runbooks" />

        <el-alert
          v-if="collectionErrors.length"
          type="warning"
          show-icon
          :closable="false"
        >
          <template #title>
            采集降级
          </template>
          <ul class="error-list">
            <li
              v-for="item in collectionErrors"
              :key="`${item.source}-${item.error}`"
            >
              {{ item.source }}：{{ item.error }}
            </li>
          </ul>
        </el-alert>

        <el-collapse>
          <el-collapse-item
            name="evidence-json"
            title="证据快照 JSON"
          >
            <pre class="json-content">{{ formatJSON(report.evidence) }}</pre>
          </el-collapse-item>
        </el-collapse>
      </template>
    </StateWrapper>
  </section>
</template>

<style scoped>
.detail-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.grid-panels {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr);
  gap: 16px;
}

.panel-header-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}

.action-message {
  color: var(--el-color-success);
  font-size: 13px;
  margin-bottom: 8px;
}

.action-list {
  display: grid;
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.action-list li {
  display: grid;
  gap: 4px;
  border-top: 1px solid var(--el-border-color-lighter);
  padding-top: 10px;
}

.action-list span {
  color: var(--el-text-color-placeholder);
  font-size: 12px;
}

.error-list {
  margin: 4px 0 0;
  padding-left: 18px;
  font-size: 13px;
}

.json-content {
  max-height: 360px;
  overflow: auto;
  color: var(--el-text-color-secondary);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 12px;
  margin: 0;
}

@media (max-width: 820px) {
  .grid-panels {
    grid-template-columns: 1fr;
  }
}
</style>
