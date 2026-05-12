<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";

import { listAuditLogs } from "../api/auditLogs";
import { formatTime } from "../utils/format";
import type { AuditLog } from "../types";

const logs = ref<AuditLog[]>([]);
const loading = ref(false);
const error = ref("");
const total = ref(0);
const page = ref(1);
const pageSize = 50;
const filters = reactive({
  action: "",
  result: "",
  actor: "",
});
const actionOptions = [
  { value: "", label: "全部动作" },
  { value: "action.create_pending", label: "创建待审批" },
  { value: "action.approve", label: "审批通过" },
  { value: "action.reject", label: "审批拒绝" },
  { value: "action.execute", label: "执行动作" },
  { value: "k8s.restart_deployment", label: "K8s 重启 Deployment" },
  { value: "k8s.scale_deployment", label: "K8s 扩缩 Deployment" },
];

function requestActionType(log: AuditLog): string {
  const request = log.request;
  if (!request || typeof request.action_type !== "string") {
    return "-";
  }
  return request.action_type;
}

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)));

function goToPage(p: number) {
  if (p < 1 || p > totalPages.value) return;
  page.value = p;
  loadLogs();
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
      page_size: pageSize,
    });
    logs.value = response.items;
    total.value = response.total ?? 0;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载审计日志失败";
  } finally {
    loading.value = false;
  }
}

onMounted(loadLogs);
</script>

<template>
  <section class="audit-page">
    <header class="page-header">
      <div>
        <h2>审计日志</h2>
        <p>动作创建、审批、拒绝、执行和权限拒绝的可追溯记录。</p>
      </div>
      <button type="button" @click="loadLogs">刷新</button>
    </header>

    <section class="toolbar">
      <select v-model="filters.action" @change="loadLogs">
        <option v-for="option in actionOptions" :key="option.value" :value="option.value">
          {{ option.label }}
        </option>
      </select>
      <select v-model="filters.result" @change="loadLogs">
        <option value="">全部结果</option>
        <option value="success">success</option>
        <option value="failure">failure</option>
        <option value="denied">denied</option>
        <option value="timeout">timeout</option>
      </select>
      <input v-model.trim="filters.actor" placeholder="actor" @keydown.enter="loadLogs" />
      <button type="button" @click="loadLogs">筛选</button>
    </section>

    <div v-if="error" class="message error">{{ error }}</div>
    <div v-if="loading" class="empty-line">加载中</div>

    <div v-else class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>时间</th>
            <th>操作者</th>
            <th>动作</th>
            <th>动作类型</th>
            <th>资源</th>
            <th>结果</th>
            <th>trace_id</th>
            <th>错误</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="log in logs" :key="log.id">
            <td>{{ formatTime(log.created_at) }}</td>
            <td>{{ log.actor }} · {{ log.actor_role }}</td>
            <td class="mono-cell">{{ log.action }}</td>
            <td class="mono-cell">{{ requestActionType(log) }}</td>
            <td>{{ log.resource_type }} #{{ log.resource_id }}</td>
            <td :class="`result-${log.result}`">{{ log.result }}</td>
            <td class="mono-cell">{{ log.trace_id || "-" }}</td>
            <td class="error-cell">{{ log.error_message || "-" }}</td>
          </tr>
          <tr v-if="logs.length === 0">
            <td colspan="8" class="empty-line">暂无审计日志</td>
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
.audit-page {
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

input,
select {
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  color: var(--text-primary);
  min-height: 2.25rem;
  padding: 0 0.7rem;
}

button {
  background: var(--accent);
  border: 0;
  border-radius: var(--radius-sm);
  color: white;
  cursor: pointer;
  min-height: 2.2rem;
  padding: 0 0.8rem;
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

.mono-cell,
.error-cell {
  overflow-wrap: anywhere;
}

.result-success {
  color: var(--success);
}

.result-failure,
.result-denied {
  color: var(--danger);
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
