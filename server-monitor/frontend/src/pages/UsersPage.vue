<script setup lang="ts">
import { onMounted, ref } from "vue";

import { deleteUser, fetchUsers, register } from "../api/auth";
import type { AuthUser } from "../types";

const users = ref<AuthUser[]>([]);
const loading = ref(false);
const error = ref("");
const showForm = ref(false);
const formError = ref("");

const form = ref({
  username: "",
  password: "",
  role: "viewer",
});

async function loadUsers() {
  loading.value = true;
  error.value = "";
  try {
    users.value = await fetchUsers();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载用户列表失败";
  } finally {
    loading.value = false;
  }
}

async function handleRegister() {
  formError.value = "";
  try {
    await register(form.value);
    showForm.value = false;
    form.value = { username: "", password: "", role: "viewer" };
    await loadUsers();
  } catch (err) {
    formError.value = err instanceof Error ? err.message : "创建用户失败";
  }
}

async function handleDelete(id: number) {
  try {
    await deleteUser(id);
    await loadUsers();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "删除用户失败";
  }
}

function cancelForm() {
  showForm.value = false;
  form.value = { username: "", password: "", role: "viewer" };
  formError.value = "";
}

onMounted(() => {
  loadUsers();
});
</script>

<template>
  <section class="users-page">
    <header class="page-header">
      <div>
        <h2>用户管理</h2>
        <p>管理系统用户和角色。</p>
      </div>
      <button v-if="!showForm" class="primary-btn" @click="showForm = true">创建用户</button>
    </header>

    <form v-if="showForm" class="form-panel" @submit.prevent="handleRegister">
      <label>
        <span>用户名</span>
        <input v-model.trim="form.username" required minlength="3" maxlength="64" pattern="[a-zA-Z0-9_]+" />
      </label>
      <label>
        <span>密码</span>
        <input v-model="form.password" type="password" required minlength="8" />
      </label>
      <label>
        <span>角色</span>
        <select v-model="form.role">
          <option value="viewer">viewer</option>
          <option value="admin">admin</option>
        </select>
      </label>
      <div class="form-actions">
        <button class="primary-btn" type="submit">创建</button>
        <button class="ghost-btn" type="button" @click="cancelForm">取消</button>
      </div>
      <div v-if="formError" class="message error">{{ formError }}</div>
    </form>

    <div v-if="error" class="message error">{{ error }}</div>

    <div class="table-panel">
      <div v-if="loading" class="empty-line">加载中</div>
      <div v-else-if="users.length === 0" class="empty-line">暂无用户</div>
      <table v-else>
        <thead>
          <tr>
            <th>ID</th>
            <th>用户名</th>
            <th>角色</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="user in users" :key="user.id">
            <td>{{ user.id }}</td>
            <td>{{ user.username }}</td>
            <td>{{ user.role }}</td>
            <td>
              <button class="ghost-btn danger-btn" @click="handleDelete(user.id)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.users-page {
  display: grid;
  gap: 1rem;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
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
.message {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1rem;
}

.form-panel {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 0.85rem;
  align-items: end;
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

select {
  cursor: pointer;
}

.form-actions {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}

.primary-btn,
.ghost-btn {
  border-radius: var(--radius-sm);
  padding: 0.55rem 0.8rem;
  font-weight: 800;
}

.primary-btn {
  color: #fff;
  background: var(--accent);
}

.ghost-btn {
  color: var(--text-secondary);
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
}

.danger-btn {
  color: var(--danger);
  border-color: rgba(239, 68, 68, 0.3);
}

.danger-btn:hover {
  background: rgba(239, 68, 68, 0.1);
}

.message.error {
  color: var(--danger);
  border-color: rgba(239, 68, 68, 0.3);
  grid-column: 1 / -1;
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
  font-size: 0.82rem;
}

th {
  color: var(--text-muted);
  font-size: 0.72rem;
}

.empty-line {
  color: var(--text-muted);
  font-size: 0.86rem;
}
</style>
