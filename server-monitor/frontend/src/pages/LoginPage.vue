<script setup lang="ts">
import { computed, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { useAuthStore } from "../stores/auth";

const route = useRoute();
const router = useRouter();
const auth = useAuthStore();

const username = ref("");
const password = ref("");
const formError = ref("");

const redirectTarget = computed(() => {
  const redirect = route.query.redirect;
  if (typeof redirect !== "string" || !redirect.startsWith("/")) {
    return "/";
  }
  return redirect === "/login" ? "/" : redirect;
});

async function onSubmit() {
  formError.value = "";
  const nextUsername = username.value.trim();
  if (!nextUsername || !password.value) {
    formError.value = "请输入用户名和密码";
    return;
  }

  try {
    await auth.login(nextUsername, password.value);
    await router.replace(redirectTarget.value);
  } catch (err) {
    formError.value = err instanceof Error ? err.message : "登录失败";
  }
}
</script>

<template>
  <main class="login-page">
    <section class="login-shell" aria-label="登录">
      <div class="login-brand">
        <div class="login-logo"></div>
        <div>
          <h1>服务监控大屏</h1>
          <p>使用后台账号登录后继续查看主机指标与告警。</p>
        </div>
      </div>

      <form class="login-form" @submit.prevent="onSubmit">
        <label class="field">
          <span>用户名</span>
          <input
            v-model="username"
            autocomplete="username"
            autofocus
            type="text"
            placeholder="请输入用户名"
          />
        </label>

        <label class="field">
          <span>密码</span>
          <input
            v-model="password"
            autocomplete="current-password"
            type="password"
            placeholder="请输入密码"
          />
        </label>

        <p v-if="formError || auth.error" class="login-error">
          {{ formError || auth.error }}
        </p>

        <button class="login-submit" type="submit" :disabled="auth.loading">
          {{ auth.loading ? "登录中" : "登录" }}
        </button>
      </form>
    </section>
  </main>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 1.5rem;
}

.login-shell {
  width: min(100%, 420px);
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-md);
  padding: 1.5rem;
}

.login-brand {
  display: flex;
  gap: 0.875rem;
  align-items: center;
  margin-bottom: 1.25rem;
}

.login-logo {
  width: 42px;
  height: 42px;
  flex: 0 0 auto;
  border-radius: var(--radius-md);
  background: linear-gradient(135deg, var(--accent), var(--info));
  box-shadow: 0 0 16px var(--accent-glow);
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
  font-size: 1.3rem;
  line-height: 1.2;
  margin: 0;
}

.login-brand p {
  color: var(--text-muted);
  font-size: 0.82rem;
  line-height: 1.6;
  margin-top: 0.3rem;
}

.login-form {
  display: grid;
  gap: 0.9rem;
}

.field {
  display: grid;
  gap: 0.45rem;
}

.field span {
  color: var(--text-secondary);
  font-size: 0.78rem;
  font-weight: 700;
}

.field input {
  width: 100%;
  cursor: text;
  color: var(--text-primary);
  background: rgba(11, 15, 23, 0.72);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.75rem 0.8rem;
  transition: border-color 0.15s, box-shadow 0.15s;
}

.field input:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.field input::placeholder {
  color: var(--text-muted);
}

.login-error {
  color: var(--danger);
  background: var(--danger-soft);
  border: 1px solid rgba(239, 68, 68, 0.25);
  border-radius: var(--radius-sm);
  padding: 0.7rem 0.8rem;
  font-size: 0.82rem;
}

.login-submit {
  color: #fff;
  background: var(--accent);
  border-radius: var(--radius-sm);
  padding: 0.78rem 1rem;
  font-weight: 800;
  transition: opacity 0.15s, transform 0.15s;
}

.login-submit:hover:not(:disabled) {
  transform: translateY(-1px);
}

.login-submit:disabled {
  cursor: not-allowed;
  opacity: 0.65;
}
</style>
