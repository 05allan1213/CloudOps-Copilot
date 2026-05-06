<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";

import {
  createAlertRule,
  deleteAlertRule,
  fetchAlertRules,
  syncAlertRules,
  updateAlertRule,
  type AlertRuleRequest,
} from "../api/alertRules";
import type { AlertRule, AlertRuleSyncResult } from "../types";

const emptyForm: AlertRuleRequest = {
  name: "",
  expr: "",
  duration: "2m",
  severity: "warning",
  summary: "",
  description: "",
  enabled: true,
};

const rules = ref<AlertRule[]>([]);
const loading = ref(false);
const saving = ref(false);
const syncing = ref(false);
const error = ref("");
const notice = ref("");
const editingID = ref<number | null>(null);
const syncResult = ref<AlertRuleSyncResult | null>(null);
const form = reactive<AlertRuleRequest>({ ...emptyForm });

function resetForm() {
  Object.assign(form, emptyForm);
  editingID.value = null;
  error.value = "";
}

function editRule(rule: AlertRule) {
  editingID.value = rule.id;
  Object.assign(form, {
    name: rule.name,
    expr: rule.expr,
    duration: rule.duration,
    severity: rule.severity,
    summary: rule.summary,
    description: rule.description,
    enabled: rule.enabled,
  });
}

async function loadRules() {
  loading.value = true;
  error.value = "";
  try {
    rules.value = await fetchAlertRules();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载告警规则失败";
  } finally {
    loading.value = false;
  }
}

async function saveRule() {
  saving.value = true;
  error.value = "";
  notice.value = "";
  try {
    if (editingID.value) {
      await updateAlertRule(editingID.value, form);
      notice.value = "告警规则已更新";
    } else {
      await createAlertRule(form);
      notice.value = "告警规则已创建";
    }
    resetForm();
    await loadRules();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "保存告警规则失败";
  } finally {
    saving.value = false;
  }
}

async function removeRule(rule: AlertRule) {
  if (!window.confirm(`删除告警规则 ${rule.name}？`)) {
    return;
  }
  error.value = "";
  notice.value = "";
  try {
    await deleteAlertRule(rule.id);
    notice.value = "告警规则已删除";
    await loadRules();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "删除告警规则失败";
  }
}

async function syncRules() {
  syncing.value = true;
  error.value = "";
  notice.value = "";
  syncResult.value = null;
  try {
    syncResult.value = await syncAlertRules();
    notice.value = "告警规则已同步到 Prometheus";
  } catch (err) {
    error.value = err instanceof Error ? err.message : "同步告警规则失败";
  } finally {
    syncing.value = false;
  }
}

onMounted(loadRules);
</script>

<template>
  <section class="manage-page">
    <header class="page-header">
      <div>
        <h2>告警规则</h2>
        <p>保存到 MySQL 后，可手动同步到 Prometheus rules 文件。</p>
      </div>
      <button class="primary-btn" type="button" :disabled="syncing" @click="syncRules">
        {{ syncing ? "同步中" : "同步规则" }}
      </button>
    </header>

    <div v-if="error" class="message error">{{ error }}</div>
    <div v-if="notice" class="message success">{{ notice }}</div>
    <div v-if="syncResult" class="sync-result">
      已校验 {{ syncResult.validated ? "通过" : "未通过" }}，规则数 {{ syncResult.rule_count }}，Reload {{ syncResult.reloaded ? "成功" : "未执行" }}
    </div>

    <form class="form-panel" @submit.prevent="saveRule">
      <div class="form-grid">
        <label>
          <span>名称</span>
          <input v-model.trim="form.name" required maxlength="128" />
        </label>
        <label>
          <span>持续时间</span>
          <input v-model.trim="form.duration" required placeholder="2m" />
        </label>
        <label>
          <span>级别</span>
          <select v-model="form.severity">
            <option value="critical">critical</option>
            <option value="warning">warning</option>
            <option value="info">info</option>
          </select>
        </label>
        <label class="checkbox-field">
          <input v-model="form.enabled" type="checkbox" />
          <span>启用</span>
        </label>
      </div>
      <label>
        <span>PromQL</span>
        <textarea v-model.trim="form.expr" required rows="3" />
      </label>
      <label>
        <span>摘要</span>
        <input v-model.trim="form.summary" maxlength="512" />
      </label>
      <label>
        <span>描述</span>
        <textarea v-model.trim="form.description" rows="2" />
      </label>
      <div class="form-actions">
        <button class="primary-btn" type="submit" :disabled="saving">
          {{ saving ? "保存中" : editingID ? "更新规则" : "创建规则" }}
        </button>
        <button class="ghost-btn" type="button" @click="resetForm">清空</button>
      </div>
    </form>

    <div class="table-panel">
      <div v-if="loading" class="empty-line">加载中</div>
      <div v-else-if="rules.length === 0" class="empty-line">暂无告警规则</div>
      <table v-else>
        <thead>
          <tr>
            <th>名称</th>
            <th>级别</th>
            <th>状态</th>
            <th>表达式</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="rule in rules" :key="rule.id">
            <td>{{ rule.name }}</td>
            <td>{{ rule.severity }}</td>
            <td>{{ rule.enabled ? "启用" : "停用" }}</td>
            <td class="mono-cell">{{ rule.expr }}</td>
            <td class="row-actions">
              <button type="button" @click="editRule(rule)">编辑</button>
              <button type="button" class="danger-text" @click="removeRule(rule)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.manage-page {
  display: grid;
  gap: 1rem;
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}

.page-header h2 {
  font-size: 1.25rem;
  margin: 0;
}

.page-header p {
  color: var(--text-muted);
  font-size: 0.82rem;
  margin-top: 0.3rem;
}

.form-panel,
.table-panel,
.sync-result,
.message {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
}

.form-panel {
  display: grid;
  gap: 0.85rem;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 0.85rem;
}

label {
  display: grid;
  gap: 0.4rem;
  color: var(--text-secondary);
  font-size: 0.78rem;
  font-weight: 700;
}

input,
textarea,
select {
  width: 100%;
  cursor: text;
  color: var(--text-primary);
  background: rgba(11, 15, 23, 0.72);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.62rem 0.7rem;
}

select,
.checkbox-field input {
  cursor: pointer;
}

.checkbox-field {
  align-content: end;
  grid-template-columns: auto 1fr;
  align-items: center;
}

.checkbox-field input {
  width: 16px;
  height: 16px;
}

.form-actions,
.row-actions {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}

.primary-btn,
.ghost-btn,
.row-actions button {
  border-radius: var(--radius-sm);
  padding: 0.55rem 0.8rem;
  font-weight: 800;
}

.primary-btn {
  color: #fff;
  background: var(--accent);
}

.primary-btn:disabled {
  cursor: not-allowed;
  opacity: 0.65;
}

.ghost-btn,
.row-actions button {
  color: var(--text-secondary);
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
}

.danger-text {
  color: var(--danger) !important;
}

.message.error {
  color: var(--danger);
  border-color: rgba(239, 68, 68, 0.3);
}

.message.success,
.sync-result {
  color: var(--success);
  border-color: rgba(34, 197, 94, 0.3);
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  border-bottom: 1px solid var(--border-color);
  padding: 0.75rem 0.5rem;
  text-align: left;
  vertical-align: top;
  font-size: 0.82rem;
}

th {
  color: var(--text-muted);
  font-size: 0.72rem;
}

.mono-cell {
  max-width: 420px;
  color: var(--text-secondary);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  overflow-wrap: anywhere;
}

.empty-line {
  color: var(--text-muted);
  font-size: 0.86rem;
}

@media (max-width: 720px) {
  .page-header {
    align-items: flex-start;
    flex-direction: column;
  }

  table {
    display: block;
    overflow-x: auto;
  }
}
</style>
