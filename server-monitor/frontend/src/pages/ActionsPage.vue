<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { RouterLink } from "vue-router";

import {
  approveAction,
  executeAction,
  listActions,
  rejectAction,
} from "../api/actions";
import { formatTime } from "../utils/format";
import type { PendingAction } from "../types";

const actions = ref<PendingAction[]>([]);
const loading = ref(false);
const actingID = ref<number | null>(null);
const error = ref("");
const total = ref(0);
const page = ref(1);
const pageSize = 50;
const filters = reactive({
  status: "",
  risk_level: "",
  action_type: "",
});

function targetOf(action: PendingAction) {
  return `${action.namespace || "-"}/${action.target_name || "-"}`;
}

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)));

function goToPage(p: number) {
  if (p < 1 || p > totalPages.value) return;
  page.value = p;
  loadActions();
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
      page_size: pageSize,
    });
    actions.value = response.items;
    total.value = response.total ?? 0;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载待审批动作失败";
  } finally {
    loading.value = false;
  }
}

async function approve(action: PendingAction) {
  const comment = window.prompt(`审批通过 ${action.action_type} ${targetOf(action)}`, "");
  if (comment === null) return;
  await runAction(action.id, () => approveAction(action.id, comment));
}

async function reject(action: PendingAction) {
  const reason = window.prompt(`拒绝 ${action.action_type} ${targetOf(action)}`, "");
  if (!reason) return;
  await runAction(action.id, () => rejectAction(action.id, reason));
}

async function execute(action: PendingAction) {
  if (!window.confirm(`确认执行 ${action.action_type} ${targetOf(action)}？`)) return;
  await runAction(action.id, () => executeAction(action.id));
}

async function runAction(id: number, fn: () => Promise<PendingAction>) {
  actingID.value = id;
  error.value = "";
  try {
    await fn();
    await loadActions();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "操作失败";
    await loadActions();
  } finally {
    actingID.value = null;
  }
}

onMounted(loadActions);
</script>

<template>
  <section class="actions-page">
    <header class="page-header">
      <div>
        <h2>动作审批</h2>
        <p>诊断建议生成的待审批动作，写操作默认需要 admin 审批。</p>
      </div>
      <button class="secondary-btn" type="button" :disabled="loading" @click="loadActions">
        刷新
      </button>
    </header>

    <section class="toolbar">
      <select v-model="filters.status" @change="loadActions">
        <option value="">全部状态</option>
        <option value="pending">pending</option>
        <option value="approved">approved</option>
        <option value="rejected">rejected</option>
        <option value="executed">executed</option>
        <option value="failed">failed</option>
      </select>
      <select v-model="filters.risk_level" @change="loadActions">
        <option value="">全部风险</option>
        <option value="medium">medium</option>
        <option value="high">high</option>
        <option value="low">low</option>
      </select>
      <input
        v-model.trim="filters.action_type"
        placeholder="action_type"
        @keydown.enter="loadActions"
      />
      <button class="secondary-btn" type="button" @click="loadActions">筛选</button>
    </section>

    <div v-if="error" class="message error">{{ error }}</div>
    <div v-if="loading" class="empty-line">加载中</div>

    <div v-else class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>ID</th>
            <th>动作</th>
            <th>目标</th>
            <th>风险</th>
            <th>状态</th>
            <th>来源</th>
            <th>创建时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="action in actions" :key="action.id" :class="{ pending: action.status === 'pending' }">
            <td>
              <RouterLink :to="`/actions/${action.id}`">#{{ action.id }}</RouterLink>
            </td>
            <td class="mono-cell">{{ action.action_type }}</td>
            <td>{{ targetOf(action) }}</td>
            <td>{{ action.risk_level }}</td>
            <td>
              <span class="status-chip" :class="`status-${action.status}`">{{ action.status }}</span>
            </td>
            <td>{{ action.requested_by }}</td>
            <td>{{ formatTime(action.created_at) }}</td>
            <td class="actions-cell">
              <button
                v-if="action.status === 'pending'"
                type="button"
                :disabled="actingID === action.id"
                @click="approve(action)"
              >
                批准
              </button>
              <button
                v-if="action.status === 'pending'"
                type="button"
                :disabled="actingID === action.id"
                @click="reject(action)"
              >
                拒绝
              </button>
              <button
                v-if="action.status === 'approved'"
                type="button"
                :disabled="actingID === action.id"
                @click="execute(action)"
              >
                执行
              </button>
            </td>
          </tr>
          <tr v-if="actions.length === 0">
            <td colspan="8" class="empty-line">暂无动作</td>
          </tr>
        </tbody>
      </table>
      <div v-if="totalPages > 1" class="pagination">
        <button type="button" :disabled="page <= 1" @click="goToPage(page - 1)">上一页</button>
        <span class="page-info">{{ page }} / {{ totalPages }}</span>
        <button type="button" :disabled="page >= totalPages" @click="goToPage(page + 1)">下一页</button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.actions-page {
  display: grid;
  gap: 1rem;
}

.page-header,
.toolbar,
.message,
.table-wrap {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
}

.page-header,
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
}

h2,
p {
  margin: 0;
}

.page-header p {
  color: var(--text-muted);
  font-size: 0.84rem;
  margin-top: 0.35rem;
}

select,
input {
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  color: var(--text-primary);
  min-height: 2.25rem;
  padding: 0 0.7rem;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  border-bottom: 1px solid var(--border-color);
  padding: 0.65rem;
  text-align: left;
  vertical-align: top;
}

th {
  color: var(--text-secondary);
  font-size: 0.78rem;
}

.mono-cell {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  max-width: 15rem;
  overflow-wrap: anywhere;
}

.status-chip {
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.15rem 0.45rem;
}

.status-pending,
.status-approved {
  color: var(--warning);
}

.status-executed {
  color: var(--success);
}

.status-failed,
.status-rejected {
  color: var(--danger);
}

.actions-cell {
  display: flex;
  gap: 0.4rem;
  flex-wrap: wrap;
}

button,
.secondary-btn {
  background: var(--accent);
  border: 0;
  border-radius: var(--radius-sm);
  color: white;
  cursor: pointer;
  min-height: 2rem;
  padding: 0 0.75rem;
}

.secondary-btn,
.actions-cell button:nth-child(2) {
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
  color: var(--text-primary);
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.error {
  color: var(--danger);
}

.empty-line {
  color: var(--text-muted);
  text-align: center;
}

.pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  padding-top: 0.75rem;
  border-top: 1px solid var(--border-color);
  margin-top: 0.5rem;
}

.page-info {
  font-size: 0.85rem;
  color: var(--text-secondary);
  min-width: 5rem;
  text-align: center;
}

.pagination button {
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
  color: var(--text-primary);
  font-size: 0.8rem;
  padding: 0.3rem 0.7rem;
  min-height: auto;
}
</style>
