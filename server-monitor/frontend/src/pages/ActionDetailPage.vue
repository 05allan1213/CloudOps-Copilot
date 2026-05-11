<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { approveAction, executeAction, getAction, rejectAction } from "../api/actions";
import type { PendingAction } from "../types";

const route = useRoute();
const router = useRouter();
const action = ref<PendingAction | null>(null);
const loading = ref(false);
const acting = ref(false);
const error = ref("");

const actionID = computed(() => Number(route.params.id));

function formatTime(value?: string) {
  if (!value) return "-";
  return new Date(value).toLocaleString("zh-CN", { hour12: false });
}

function formatJSON(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2);
}

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

async function approve() {
  const comment = window.prompt("审批备注", "");
  if (comment === null || !action.value) return;
  await run(() => approveAction(action.value!.id, comment));
}

async function reject() {
  const reason = window.prompt("拒绝原因", "");
  if (!reason || !action.value) return;
  await run(() => rejectAction(action.value!.id, reason));
}

async function execute() {
  if (!action.value || !window.confirm("确认执行该动作？")) return;
  await run(() => executeAction(action.value!.id));
}

async function run(fn: () => Promise<PendingAction>) {
  acting.value = true;
  error.value = "";
  try {
    action.value = await fn();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "操作失败";
    await loadAction();
  } finally {
    acting.value = false;
  }
}

onMounted(loadAction);
</script>

<template>
  <section class="detail-page">
    <button class="secondary-btn back-btn" type="button" @click="router.push('/actions')">
      返回动作列表
    </button>
    <div v-if="loading" class="empty-line">加载中</div>
    <div v-else-if="error" class="message error">{{ error }}</div>
    <template v-if="action">
      <header class="detail-header">
        <div>
          <h2>#{{ action.id }} {{ action.action_type }}</h2>
          <p>{{ action.namespace }}/{{ action.target_name }} · {{ action.status }}</p>
        </div>
        <div class="button-row">
          <button v-if="action.status === 'pending'" type="button" :disabled="acting" @click="approve">批准</button>
          <button v-if="action.status === 'pending'" class="secondary-btn" type="button" :disabled="acting" @click="reject">拒绝</button>
          <button v-if="action.status === 'approved'" type="button" :disabled="acting" @click="execute">执行</button>
        </div>
      </header>

      <section class="info-grid">
        <div><span>风险</span><strong>{{ action.risk_level }}</strong></div>
        <div><span>来源</span><strong>{{ action.requested_by }}</strong></div>
        <div><span>关联诊断</span><strong>#{{ action.diagnosis_report_id || "-" }}</strong></div>
        <div><span>创建时间</span><strong>{{ formatTime(action.created_at) }}</strong></div>
        <div><span>审批人</span><strong>{{ action.approved_by || "-" }}</strong></div>
        <div><span>执行人</span><strong>{{ action.executed_by || "-" }}</strong></div>
      </section>

      <section class="panel">
        <h3>参数</h3>
        <pre>{{ formatJSON(action.params) }}</pre>
      </section>

      <section class="panel">
        <h3>执行结果</h3>
        <pre>{{ formatJSON(action.result) }}</pre>
        <p v-if="action.error_message" class="error-text">{{ action.error_message }}</p>
      </section>
    </template>
  </section>
</template>

<style scoped>
.detail-page {
  display: grid;
  gap: 1rem;
}

.detail-header,
.info-grid,
.panel,
.message {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
}

.detail-header,
.button-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.info-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 0.85rem;
}

.info-grid div {
  display: grid;
  gap: 0.25rem;
}

.info-grid span,
p {
  color: var(--text-muted);
  font-size: 0.82rem;
  margin: 0;
}

h2,
h3 {
  margin: 0;
}

h3 {
  color: var(--text-secondary);
  font-size: 0.88rem;
  margin-bottom: 0.6rem;
}

pre {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

button,
.secondary-btn {
  background: var(--accent);
  border: 0;
  border-radius: var(--radius-sm);
  color: white;
  cursor: pointer;
  min-height: 2.2rem;
  padding: 0 0.8rem;
}

.secondary-btn {
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
  color: var(--text-primary);
}

.back-btn {
  justify-self: start;
}

.error,
.error-text {
  color: var(--danger);
}

.empty-line {
  color: var(--text-muted);
}
</style>
