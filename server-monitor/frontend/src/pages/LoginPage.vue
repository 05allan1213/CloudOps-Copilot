<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage, type FormInstance, type FormRules } from "element-plus";
import { Key, User } from "@element-plus/icons-vue";

import { useAuthStore } from "../stores/auth";

const route = useRoute();
const router = useRouter();
const auth = useAuthStore();

const formRef = ref<FormInstance>();
const form = reactive({
  username: "",
  password: "",
});
const formError = ref("");

const rules: FormRules = {
  username: [{ required: true, message: "请输入用户名", trigger: "blur" }],
  password: [{ required: true, message: "请输入密码", trigger: "blur" }],
};

const redirectTarget = computed(() => {
  const redirect = route.query.redirect;
  if (typeof redirect !== "string" || !redirect.startsWith("/")) {
    return "/";
  }
  return redirect === "/login" ? "/" : redirect;
});

async function onSubmit() {
  if (!formRef.value) return;
  const valid = await formRef.value.validate().catch(() => false);
  if (!valid) return;

  formError.value = "";
  try {
    await auth.login(form.username.trim(), form.password);
    await router.replace(redirectTarget.value);
  } catch (err) {
    formError.value = err instanceof Error ? err.message : "登录失败";
  }
}
</script>

<template>
  <main class="login-page">
    <el-card class="login-card" shadow="always">
      <div class="login-brand">
        <div class="login-logo"></div>
        <div>
          <h1>服务监控大屏</h1>
          <p>使用后台账号登录后继续查看主机指标与告警。</p>
        </div>
      </div>

      <el-alert
        v-if="formError || auth.error"
        :title="formError || auth.error || ''"
        type="error"
        show-icon
        :closable="true"
        class="login-alert"
      />

      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-position="top"
        :disabled="auth.loading"
        @submit.prevent="onSubmit"
      >
        <el-form-item label="用户名" prop="username">
          <el-input
            v-model="form.username"
            autocomplete="username"
            placeholder="请输入用户名"
            :prefix-icon="User"
            size="large"
          />
        </el-form-item>

        <el-form-item label="密码" prop="password">
          <el-input
            v-model="form.password"
            type="password"
            autocomplete="current-password"
            placeholder="请输入密码"
            :prefix-icon="Key"
            size="large"
            show-password
          />
        </el-form-item>

        <el-form-item>
          <el-button
            type="primary"
            size="large"
            :loading="auth.loading"
            class="login-submit"
            @click="onSubmit"
          >
            {{ auth.loading ? "登录中" : "登录" }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </main>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
}

.login-card {
  width: min(100%, 420px);
}

.login-card :deep(.el-card__body) {
  padding: 24px;
}

.login-brand {
  display: flex;
  gap: 14px;
  align-items: center;
  margin-bottom: 20px;
}

.login-logo {
  width: 42px;
  height: 42px;
  flex: 0 0 auto;
  border-radius: var(--cloudops-radius-md);
  background: linear-gradient(135deg, var(--el-color-primary), var(--el-color-info));
  box-shadow: 0 0 16px rgba(59, 130, 246, 0.3);
  position: relative;
}

.login-logo::after {
  content: "";
  position: absolute;
  inset: 8px;
  border: 2px solid rgba(255, 255, 255, 0.4);
  border-radius: 4px;
}

.login-brand h1 {
  font-size: 21px;
  line-height: 1.2;
  margin: 0;
}

.login-brand p {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.6;
  margin-top: 5px;
}

.login-alert {
  margin-bottom: 16px;
}

.login-submit {
  width: 100%;
}
</style>
