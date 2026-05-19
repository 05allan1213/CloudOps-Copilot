<script setup lang="ts">
import { onMounted, ref, reactive, computed } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import type { FormInstance, FormRules } from "element-plus";

import { deleteUser, fetchUsers, register } from "../api/auth";
import type { AuthUser } from "../types";
import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";

const users = ref<AuthUser[]>([]);
const loading = ref(false);
const creating = ref(false);
const dialogVisible = ref(false);
const formRef = ref<FormInstance>();

const form = reactive({
  username: "",
  password: "",
  role: "viewer",
});

const stateKey = computed(() => {
  if (loading.value) return "loading";
  if (users.value.length === 0) return "empty";
  return "default";
});

const formRules: FormRules = {
  username: [
    { required: true, message: "请输入用户名", trigger: "blur" },
    { min: 3, max: 64, message: "用户名长度 3-64 字符", trigger: "blur" },
    { pattern: /^[a-zA-Z0-9_]+$/, message: "仅允许字母、数字和下划线", trigger: "blur" },
  ],
  password: [
    { required: true, message: "请输入密码", trigger: "blur" },
    { min: 8, message: "密码至少 8 个字符", trigger: "blur" },
  ],
  role: [{ required: true, message: "请选择角色", trigger: "change" }],
};

function resetForm() {
  form.username = "";
  form.password = "";
  form.role = "viewer";
  formRef.value?.resetFields();
}

function openDialog() {
  resetForm();
  dialogVisible.value = true;
}

function closeDialog() {
  dialogVisible.value = false;
  resetForm();
}

async function loadUsers() {
  loading.value = true;
  try {
    users.value = await fetchUsers();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "加载用户列表失败");
  } finally {
    loading.value = false;
  }
}

async function handleRegister() {
  const valid = await formRef.value?.validate().catch(() => false);
  if (!valid) return;

  creating.value = true;
  try {
    await register({ username: form.username, password: form.password, role: form.role });
    ElMessage.success("用户创建成功");
    dialogVisible.value = false;
    resetForm();
    await loadUsers();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "创建用户失败");
  } finally {
    creating.value = false;
  }
}

async function handleDelete(user: AuthUser) {
  try {
    await ElMessageBox.confirm(`确认删除用户「${user.username}」？此操作不可撤销。`, "删除确认", {
      confirmButtonText: "删除",
      cancelButtonText: "取消",
      type: "warning",
    });
  } catch {
    return;
  }
  try {
    await deleteUser(user.id);
    ElMessage.success("用户已删除");
    await loadUsers();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "删除用户失败");
  }
}

function roleTagType(role: string) {
  return role === "admin" ? "danger" : "info";
}

onMounted(loadUsers);
</script>

<template>
  <section class="users-page">
    <PageHeader
      title="用户管理"
      subtitle="管理系统用户和角色"
    >
      <el-button
        type="primary"
        @click="openDialog"
      >
        创建用户
      </el-button>
    </PageHeader>

    <el-dialog
      v-model="dialogVisible"
      title="创建用户"
      width="480px"
      :close-on-click-modal="false"
      @close="closeDialog"
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="formRules"
        label-width="80px"
        label-position="top"
        @submit.prevent="handleRegister"
      >
        <el-form-item
          label="用户名"
          prop="username"
        >
          <el-input
            v-model.trim="form.username"
            maxlength="64"
            placeholder="3-64 字符，字母数字下划线"
          />
        </el-form-item>
        <el-form-item
          label="密码"
          prop="password"
        >
          <el-input
            v-model="form.password"
            type="password"
            show-password
            placeholder="至少 8 个字符"
          />
        </el-form-item>
        <el-form-item
          label="角色"
          prop="role"
        >
          <el-select
            v-model="form.role"
            style="width: 100%"
          >
            <el-option
              label="viewer"
              value="viewer"
            />
            <el-option
              label="admin"
              value="admin"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="closeDialog">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="creating"
          @click="handleRegister"
        >
          {{ creating ? "创建中" : "创建" }}
        </el-button>
      </template>
    </el-dialog>

    <StateWrapper
      :state="stateKey"
      empty-text="暂无用户"
    >
      <template #retry>
        <el-button
          type="primary"
          @click="loadUsers"
        >
          重试
        </el-button>
      </template>
      <el-card shadow="never">
        <el-table
          :data="users"
          stripe
          style="width: 100%"
        >
          <el-table-column
            prop="id"
            label="ID"
            width="80"
            align="center"
          />
          <el-table-column
            prop="username"
            label="用户名"
            min-width="160"
          />
          <el-table-column
            label="角色"
            width="120"
            align="center"
          >
            <template #default="{ row }">
              <el-tag
                :type="roleTagType(row.role)"
                size="small"
                effect="plain"
              >
                {{ row.role }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            label="操作"
            width="120"
            align="center"
          >
            <template #default="{ row }">
              <el-button
                size="small"
                text
                type="danger"
                @click="handleDelete(row)"
              >
                删除
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-card>
    </StateWrapper>
  </section>
</template>

<style scoped>
.users-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
</style>
