<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";

import { createActionsFromDiagnosis } from "../api/actions";
import { fetchDiagnosis, submitDiagnosisFeedback } from "../api/diagnosis";
import { formatTime } from "../utils/format";
import { useAuthStore } from "../stores/auth";
import type { DiagnosisReport } from "../types";

const route = useRoute();
const auth = useAuthStore();
const report = ref<DiagnosisReport | null>(null);
const loading = ref(false);
const creatingActions = ref(false);
const error = ref("");
const actionMessage = ref("");
const feedbackRating = ref<string | null>(null);
const feedbackComment = ref("");
const showCommentInput = ref(false);
const submitting = ref(false);
const feedbackSubmitted = ref(false);
const feedbackError = ref(false);

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

function formatPercent(value?: number) {
  return `${Math.round((value ?? 0) * 100)}%`;
}

function formatJSON(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2);
}

function formatScore(value?: number) {
  return (value ?? 0).toFixed(1);
}

function formatServicePorts(ports?: Array<{ port: number; target_port?: string }>) {
  return (ports ?? []).map((port) => `${port.port}${port.target_port ? `:${port.target_port}` : ""}`).join(", ") || "-";
}

function runbookMatches(runbook: { matched_alerts?: string[]; matched_keywords?: string[]; matched_metrics?: string[] }) {
  return [
    ...(runbook.matched_alerts ?? []),
    ...(runbook.matched_keywords ?? []),
    ...(runbook.matched_metrics ?? []),
  ];
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
    if (report.value.my_feedback) {
      feedbackRating.value = report.value.my_feedback.rating;
      feedbackComment.value = report.value.my_feedback.comment || "";
      feedbackSubmitted.value = true;
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载诊断报告失败";
  } finally {
    loading.value = false;
  }
}

async function createApprovalActions() {
  if (!report.value) return;
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

async function submitFeedback(rating: string) {
  feedbackRating.value = rating;
  submitting.value = true;
  feedbackError.value = false;
  try {
    await submitDiagnosisFeedback(report.value!.id, {
      rating: rating as "useful" | "not_useful",
      comment: feedbackComment.value || undefined,
    });
    feedbackSubmitted.value = true;
  } catch {
    feedbackError.value = true;
  } finally {
    submitting.value = false;
  }
}

function submitFeedbackWithComment() {
  if (!feedbackRating.value) return;
  submitFeedback(feedbackRating.value);
}

onMounted(loadReport);
</script>

<template>
  <section class="detail-page">
    <div v-if="loading" class="empty-line">加载中</div>
    <div v-else-if="error" class="message error">{{ error }}</div>
    <template v-else-if="report">
      <header class="detail-header">
        <div>
          <h2>#{{ report.id }} {{ report.alert_name || "诊断报告" }}</h2>
          <p>{{ report.target_name || "-" }} · {{ report.status }} · {{ formatTime(report.created_at) }}</p>
        </div>
        <div class="confidence">
          <strong>{{ formatPercent(report.confidence) }}</strong>
          <span>{{ report.confidence_level }}</span>
        </div>
      </header>

      <section v-if="report.status === 'completed'" class="feedback-section">
        <span class="feedback-label">这份诊断报告对您有帮助吗？</span>
        <button
          type="button"
          class="feedback-btn"
          :class="{ active: feedbackRating === 'useful' }"
          :disabled="submitting"
          @click="submitFeedback('useful')"
        >
          👍 有用
        </button>
        <button
          type="button"
          class="feedback-btn"
          :class="{ active: feedbackRating === 'not_useful' }"
          :disabled="submitting"
          @click="submitFeedback('not_useful')"
        >
          👎 没用
        </button>
        <button
          type="button"
          class="feedback-btn comment-btn"
          @click="showCommentInput = !showCommentInput"
        >
          💬 评论
        </button>
        <div v-if="showCommentInput" class="comment-input">
          <textarea
            v-model="feedbackComment"
            maxlength="500"
            placeholder="请输入您的反馈（可选，最多 500 字符）"
            rows="3"
          />
          <div class="comment-actions">
            <span class="char-count">{{ feedbackComment.length }}/500</span>
            <button
              type="button"
              class="submit-btn"
              :disabled="submitting || !feedbackRating"
              @click="submitFeedbackWithComment"
            >
              提交
            </button>
          </div>
        </div>
        <span v-if="feedbackSubmitted" class="feedback-thanks">感谢您的反馈！</span>
        <span v-if="feedbackError" class="feedback-error">反馈提交失败，请稍后重试</span>
      </section>

      <section class="summary-panel">
        <h3>摘要</h3>
        <p>{{ report.summary || "-" }}</p>
        <h3>根因假设</h3>
        <p>{{ report.root_cause || "-" }}</p>
      </section>

      <section class="grid-panels">
        <div class="panel">
          <h3>建议动作</h3>
          <div v-if="auth.isAdmin && actions.some((action) => action.requires_approval)" class="action-toolbar">
            <button type="button" :disabled="creatingActions" @click="createApprovalActions">
              创建审批动作
            </button>
            <span v-if="actionMessage">{{ actionMessage }}</span>
          </div>
          <div v-if="actions.length === 0" class="empty-line">暂无建议</div>
          <ul v-else class="action-list">
            <li v-for="action in actions" :key="`${action.type}-${action.description}`">
              <strong>{{ action.description }}</strong>
              <span>{{ action.type }} · {{ action.risk }} · {{ action.requires_approval ? "需审批" : "只读建议" }}</span>
            </li>
          </ul>
        </div>

        <div class="panel">
          <h3>指标证据</h3>
          <div v-if="metrics.length === 0" class="empty-line">暂无指标证据</div>
          <table v-else>
            <thead>
              <tr>
                <th>指标</th>
                <th>avg</th>
                <th>max</th>
                <th>last</th>
                <th>趋势</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="metric in metrics" :key="metric.name">
                <td>{{ metric.name }}</td>
                <td>{{ metric.avg.toFixed(2) }}</td>
                <td>{{ metric.max.toFixed(2) }}</td>
                <td>{{ metric.last.toFixed(2) }}</td>
                <td>{{ metric.trend }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="panel">
        <h3>规则分析</h3>
        <div v-if="ruleResults.length === 0" class="empty-line">暂无规则分析</div>
        <div v-else class="rule-list">
          <div v-for="rule in ruleResults" :key="rule.rule" class="rule-item" :class="{ passed: rule.passed }">
            <strong>{{ rule.rule }}</strong>
            <span>{{ rule.passed ? "命中" : "未命中" }}</span>
            <p>{{ rule.detail }}</p>
          </div>
        </div>
      </section>

      <section class="panel">
        <h3>K8s 证据</h3>
        <div v-if="!k8sEvidence?.enabled" class="empty-line">当前诊断未采集 K8s 证据。</div>
        <template v-else>
          <div class="k8s-head">
            <span>{{ k8sEvidence.namespace || "-" }}</span>
            <strong>{{ k8sEvidence.target_kind || "-" }} / {{ k8sEvidence.target_name || "-" }}</strong>
            <small>{{ formatTime(k8sEvidence.collected_at) }}</small>
          </div>
          <div v-if="k8sEvidence.deployments?.length" class="mini-table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Deployment</th>
                  <th>ready</th>
                  <th>updated</th>
                  <th>available</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="deployment in k8sEvidence.deployments" :key="deployment.name">
                  <td>{{ deployment.namespace }}/{{ deployment.name }}</td>
                  <td>{{ deployment.ready_replicas }}/{{ deployment.replicas }}</td>
                  <td>{{ deployment.updated_replicas }}</td>
                  <td>{{ deployment.available_replicas }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="k8sEvidence.pods?.length" class="mini-table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Pod</th>
                  <th>phase</th>
                  <th>ready</th>
                  <th>restarts</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="pod in k8sEvidence.pods" :key="pod.name">
                  <td>{{ pod.namespace }}/{{ pod.name }}</td>
                  <td>{{ pod.phase }}</td>
                  <td>{{ pod.ready_containers }}/{{ pod.total_containers }}</td>
                  <td>{{ pod.restart_count }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="k8sEvidence.services?.length" class="mini-table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Service</th>
                  <th>type</th>
                  <th>cluster IP</th>
                  <th>ports</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="service in k8sEvidence.services" :key="service.name">
                  <td>{{ service.namespace }}/{{ service.name }}</td>
                  <td>{{ service.type }}</td>
                  <td>{{ service.cluster_ip || "-" }}</td>
                  <td>{{ formatServicePorts(service.ports) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="k8sEvidence.nodes?.length" class="mini-table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Node</th>
                  <th>ready</th>
                  <th>kubelet</th>
                  <th>capacity</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="node in k8sEvidence.nodes" :key="node.name">
                  <td>{{ node.name }}</td>
                  <td>{{ node.ready ? "true" : "false" }}</td>
                  <td>{{ node.kubelet_version || "-" }}</td>
                  <td>{{ node.capacity?.cpu || "-" }} / {{ node.capacity?.memory || "-" }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <ul v-if="k8sEvidence.events?.length" class="error-list">
            <li v-for="event in k8sEvidence.events" :key="`${event.name}-${event.reason}`">
              {{ event.type || "Event" }} · {{ event.reason || "-" }} · {{ event.message || event.name }}
            </li>
          </ul>
          <details v-for="log in k8sEvidence.logs" :key="`${log.namespace}-${log.pod_name}-${log.container}`" class="runbook-snippet">
            <summary>{{ log.namespace }}/{{ log.pod_name }}{{ log.container ? ` · ${log.container}` : "" }} 日志</summary>
            <pre>{{ (log.lines || []).join("\n") }}</pre>
          </details>
          <ul v-if="k8sEvidence.errors?.length" class="error-list">
            <li v-for="item in k8sEvidence.errors" :key="`${item.source}-${item.error}`">
              {{ item.source }}：{{ item.error }}
            </li>
          </ul>
        </template>
      </section>

      <section class="panel">
        <h3>Runbook 命中</h3>
        <div v-if="runbooks.length === 0" class="empty-line">未命中匹配 Runbook，当前诊断仅基于告警、指标和规则分析。</div>
        <div v-else class="runbook-list">
          <article v-for="runbook in runbooks" :key="`${runbook.file}-${runbook.title}`" class="runbook-item">
            <div class="runbook-head">
              <strong>{{ runbook.title }}</strong>
              <span>{{ runbook.file }} · score {{ formatScore(runbook.score) }}</span>
            </div>
            <div v-if="runbookMatches(runbook).length" class="tag-row">
              <span v-for="match in runbookMatches(runbook)" :key="match" class="tag">{{ match }}</span>
            </div>
            <details class="runbook-snippet">
              <summary>查看片段</summary>
              <pre>{{ runbook.snippet }}</pre>
            </details>
          </article>
        </div>
      </section>

      <section v-if="collectionErrors.length" class="panel warning-panel">
        <h3>采集降级</h3>
        <ul class="error-list">
          <li v-for="item in collectionErrors" :key="`${item.source}-${item.error}`">
            {{ item.source }}：{{ item.error }}
          </li>
        </ul>
      </section>

      <details class="panel raw-panel">
        <summary>证据快照 JSON</summary>
        <pre>{{ formatJSON(report.evidence) }}</pre>
      </details>
    </template>
  </section>
</template>

<style scoped>
.detail-page {
  display: grid;
  gap: 1rem;
}

.detail-header,
.summary-panel,
.panel,
.message {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
}

.detail-header {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
}

h2,
h3,
p {
  margin: 0;
}

h2 {
  font-size: 1.2rem;
}

h3 {
  color: var(--text-secondary);
  font-size: 0.86rem;
  margin-bottom: 0.65rem;
}

.detail-header p,
.summary-panel p,
.action-list span,
.rule-item p {
  color: var(--text-muted);
  font-size: 0.82rem;
  margin-top: 0.35rem;
}

.confidence {
  display: grid;
  justify-items: end;
  gap: 0.2rem;
}

.confidence strong {
  color: var(--accent);
  font-size: 1.2rem;
}

.confidence span {
  color: var(--text-muted);
  font-size: 0.76rem;
}

.grid-panels {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr);
  gap: 1rem;
}

.action-list,
.error-list {
  display: grid;
  gap: 0.6rem;
  margin: 0;
  padding: 0;
  list-style: none;
}

.action-list li {
  display: grid;
  gap: 0.25rem;
  border-top: 1px solid var(--border-color);
  padding-top: 0.6rem;
}

.action-toolbar {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  margin-bottom: 0.75rem;
  flex-wrap: wrap;
}

.action-toolbar button {
  background: var(--accent);
  border: 0;
  border-radius: var(--radius-sm);
  color: white;
  cursor: pointer;
  min-height: 2rem;
  padding: 0 0.75rem;
}

.action-toolbar button:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.action-toolbar span {
  color: var(--text-muted);
  font-size: 0.8rem;
}

table {
  width: 100%;
  border-collapse: collapse;
}

.mini-table-wrap {
  margin-top: 0.75rem;
}

.k8s-head {
  display: flex;
  gap: 0.55rem;
  flex-wrap: wrap;
  color: var(--text-muted);
  font-size: 0.8rem;
}

.k8s-head strong {
  color: var(--text-secondary);
}

th,
td {
  border-top: 1px solid var(--border-color);
  padding: 0.58rem 0.45rem;
  text-align: left;
  font-size: 0.8rem;
}

th {
  color: var(--text-muted);
  font-size: 0.72rem;
}

.rule-list {
  display: grid;
  gap: 0.55rem;
}

.rule-item {
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.65rem;
}

.rule-item.passed {
  border-color: rgba(34, 197, 94, 0.35);
}

.rule-item span {
  float: right;
  color: var(--text-muted);
  font-size: 0.75rem;
}

.runbook-list {
  display: grid;
  gap: 0.7rem;
}

.runbook-item {
  border-top: 1px solid var(--border-color);
  display: grid;
  gap: 0.55rem;
  padding-top: 0.75rem;
}

.runbook-head {
  display: flex;
  justify-content: space-between;
  gap: 0.75rem;
}

.runbook-head span {
  color: var(--text-muted);
  font-size: 0.76rem;
  white-space: nowrap;
}

.tag-row {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}

.tag {
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  font-size: 0.72rem;
  padding: 0.18rem 0.45rem;
}

.runbook-snippet summary {
  cursor: pointer;
  color: var(--text-secondary);
  font-size: 0.78rem;
  font-weight: 700;
}

.runbook-snippet pre {
  max-height: 220px;
}

.warning-panel {
  border-color: rgba(245, 158, 11, 0.35);
}

.raw-panel summary {
  cursor: pointer;
  color: var(--text-secondary);
  font-weight: 700;
}

pre {
  max-height: 360px;
  overflow: auto;
  color: var(--text-secondary);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.empty-line {
  color: var(--text-muted);
  padding: 0.8rem 0;
  text-align: center;
}

.message.error {
  color: var(--danger);
  background: var(--danger-soft);
}

@media (max-width: 820px) {
  .detail-header,
  .grid-panels {
    grid-template-columns: 1fr;
  }

  .detail-header {
    display: grid;
  }

  .confidence {
    justify-items: start;
  }

  .runbook-head {
    display: grid;
  }

  .runbook-head span {
    white-space: normal;
  }
}

.feedback-section {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 0.75rem 1rem;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.feedback-label {
  color: var(--text-secondary);
  font-size: 0.84rem;
  margin-right: 0.25rem;
}

.feedback-btn {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 0.8rem;
  padding: 0.35rem 0.7rem;
  transition: border-color 0.15s, background 0.15s;
}

.feedback-btn:hover {
  border-color: var(--accent);
}

.feedback-btn.active {
  background: var(--accent);
  border-color: var(--accent);
  color: white;
}

.feedback-btn:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.comment-input {
  width: 100%;
  display: grid;
  gap: 0.4rem;
  margin-top: 0.35rem;
}

.comment-input textarea {
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  font-family: inherit;
  font-size: 0.82rem;
  padding: 0.5rem;
  resize: vertical;
}

.comment-actions {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.char-count {
  color: var(--text-muted);
  font-size: 0.75rem;
}

.submit-btn {
  background: var(--accent);
  border: 0;
  border-radius: var(--radius-sm);
  color: white;
  cursor: pointer;
  font-size: 0.8rem;
  padding: 0.35rem 0.9rem;
}

.submit-btn:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.feedback-thanks {
  color: #22c55e;
  font-size: 0.82rem;
}

.feedback-error {
  color: var(--danger);
  font-size: 0.82rem;
}
</style>
