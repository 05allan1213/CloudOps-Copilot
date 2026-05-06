<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";

import {
  createNotificationChannel,
  deleteNotificationChannel,
  fetchNotificationChannels,
  testNotificationChannel,
  updateNotificationChannel,
  type NotificationChannelRequest,
} from "../api/channels";
import type { NotificationChannel, NotificationChannelTestResult } from "../types";

const emptyForm: NotificationChannelRequest = {
  name: "",
  type: "webhook",
  url: "",
  enabled: true,
};

const channels = ref<NotificationChannel[]>([]);
const loading = ref(false);
const saving = ref(false);
const testingID = ref<number | null>(null);
const editingID = ref<number | null>(null);
const error = ref("");
const notice = ref("");
const testResult = ref<NotificationChannelTestResult | null>(null);
const form = reactive<NotificationChannelRequest>({ ...emptyForm });

function resetForm() {
  Object.assign(form, emptyForm);
  editingID.value = null;
  error.value = "";
}

function editChannel(channel: NotificationChannel) {
  editingID.value = channel.id;
  Object.assign(form, {
    name: channel.name,
    type: channel.type,
    url: channel.url,
    enabled: channel.enabled,
  });
}

async function loadChannels() {
  loading.value = true;
  error.value = "";
  try {
    channels.value = await fetchNotificationChannels();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载通知渠道失败";
  } finally {
    loading.value = false;
  }
}

async function saveChannel() {
  saving.value = true;
  error.value = "";
  notice.value = "";
  testResult.value = null;
  try {
    if (editingID.value) {
      await updateNotificationChannel(editingID.value, form);
      notice.value = "通知渠道已更新";
    } else {
      await createNotificationChannel(form);
      notice.value = "通知渠道已创建";
    }
    resetForm();
    await loadChannels();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "保存通知渠道失败";
  } finally {
    saving.value = false;
  }
}

async function removeChannel(channel: NotificationChannel) {
  if (!window.confirm(`删除通知渠道 ${channel.name}？`)) {
    return;
  }
  error.value = "";
  notice.value = "";
  testResult.value = null;
  try {
    await deleteNotificationChannel(channel.id);
    notice.value = "通知渠道已删除";
    await loadChannels();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "删除通知渠道失败";
  }
}

async function testChannel(channel: NotificationChannel) {
  testingID.value = channel.id;
  error.value = "";
  notice.value = "";
  testResult.value = null;
  try {
    testResult.value = await testNotificationChannel(channel.id);
    notice.value = "通知渠道连通性测试通过";
  } catch (err) {
    error.value = err instanceof Error ? err.message : "通知渠道测试失败";
  } finally {
    testingID.value = null;
  }
}

onMounted(loadChannels);
</script>

<template>
  <section class="manage-page">
    <header class="page-header">
      <div>
        <h2>通知渠道</h2>
        <p>当前阶段只维护 Webhook 配置和连通性测试，不发送真实告警通知。</p>
      </div>
    </header>

    <div v-if="error" class="message error">{{ error }}</div>
    <div v-if="notice" class="message success">{{ notice }}</div>
    <div v-if="testResult" class="test-result">
      HTTP {{ testResult.status_code ?? "-" }}，耗时 {{ testResult.latency_ms ?? 0 }}ms
    </div>

    <form class="form-panel" @submit.prevent="saveChannel">
      <div class="form-grid">
        <label>
          <span>名称</span>
          <input v-model.trim="form.name" required maxlength="128" />
        </label>
        <label>
          <span>类型</span>
          <select v-model="form.type">
            <option value="webhook">webhook</option>
          </select>
        </label>
        <label class="checkbox-field">
          <input v-model="form.enabled" type="checkbox" />
          <span>启用</span>
        </label>
      </div>
      <label>
        <span>Webhook URL</span>
        <input v-model.trim="form.url" required maxlength="512" placeholder="https://example.com/webhook" />
      </label>
      <div class="form-actions">
        <button class="primary-btn" type="submit" :disabled="saving">
          {{ saving ? "保存中" : editingID ? "更新渠道" : "创建渠道" }}
        </button>
        <button class="ghost-btn" type="button" @click="resetForm">清空</button>
      </div>
    </form>

    <div class="table-panel">
      <div v-if="loading" class="empty-line">加载中</div>
      <div v-else-if="channels.length === 0" class="empty-line">暂无通知渠道</div>
      <table v-else>
        <thead>
          <tr>
            <th>名称</th>
            <th>类型</th>
            <th>状态</th>
            <th>URL</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="channel in channels" :key="channel.id">
            <td>{{ channel.name }}</td>
            <td>{{ channel.type }}</td>
            <td>{{ channel.enabled ? "启用" : "停用" }}</td>
            <td class="url-cell">{{ channel.url }}</td>
            <td class="row-actions">
              <button type="button" @click="editChannel(channel)">编辑</button>
              <button type="button" :disabled="testingID === channel.id" @click="testChannel(channel)">
                {{ testingID === channel.id ? "测试中" : "测试" }}
              </button>
              <button type="button" class="danger-text" @click="removeChannel(channel)">删除</button>
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
.test-result,
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
select {
  width: 100%;
  color: var(--text-primary);
  background: rgba(11, 15, 23, 0.72);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.62rem 0.7rem;
}

input {
  cursor: text;
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
  flex-wrap: wrap;
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

.primary-btn:disabled,
.row-actions button:disabled {
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
.test-result {
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

.url-cell {
  max-width: 420px;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}

.empty-line {
  color: var(--text-muted);
  font-size: 0.86rem;
}

@media (max-width: 720px) {
  table {
    display: block;
    overflow-x: auto;
  }
}
</style>
