<script setup lang="ts">
import { useRouter } from "vue-router";

import { useTheme } from "../../composables/useTheme";
import { useAuthStore } from "../../stores/auth";

defineProps<{
  pageTitle: string;
}>();

const auth = useAuthStore();
const router = useRouter();
const { isDark, toggleTheme } = useTheme();

async function logout() {
  auth.logout();
  await router.push("/login");
}
</script>

<template>
  <el-header
    class="app-header"
    height="56px"
  >
    <div class="header-left">
      <h2 class="header-page-title">
        {{ pageTitle }}
      </h2>
    </div>
    <div class="header-right">
      <el-tag
        type="success"
        size="small"
        effect="plain"
        round
      >
        Incident V2
      </el-tag>
      <el-switch
        :model-value="isDark"
        inline-prompt
        active-text="暗"
        inactive-text="亮"
        aria-label="切换暗亮主题"
        @change="toggleTheme"
      />
      <div class="header-user">
        <span class="header-username">{{ auth.user?.username }}</span>
        <el-tag
          size="small"
          effect="plain"
          round
        >
          {{ auth.user?.role }}
        </el-tag>
      </div>
      <el-button
        size="small"
        text
        @click="logout"
      >
        退出
      </el-button>
    </div>
  </el-header>
</template>

<style scoped>
.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  background: var(--cloudops-bg-secondary);
  border-bottom: 1px solid var(--cloudops-border-color);
  height: var(--cloudops-header-height);
  flex-shrink: 0;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.header-page-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--cloudops-text-primary);
  margin: 0;
}

.header-update-ago {
  font-size: 12px;
  color: var(--cloudops-text-muted);
}

.header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.search-btn-text {
  margin-right: 6px;
}

.search-btn-shortcut {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  background: var(--el-fill-color);
  padding: 1px 5px;
  border-radius: 3px;
}

.ws-tag {
  display: flex;
  align-items: center;
  gap: 6px;
}

.ws-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  display: inline-block;
}

.ws-dot.ws-connected {
  background: var(--cloudops-success);
  box-shadow: 0 0 4px var(--cloudops-success);
}

.ws-dot.ws-connecting {
  background: var(--cloudops-warning);
  animation: pulse 1.5s infinite;
}

.ws-dot.ws-disconnected {
  background: var(--cloudops-danger);
}

.header-clock {
  font-size: 13px;
  font-weight: 500;
  color: var(--cloudops-text-secondary);
  font-variant-numeric: tabular-nums;
}

.header-user {
  display: flex;
  align-items: center;
  gap: 6px;
}

.header-username {
  font-size: 13px;
  font-weight: 600;
  color: var(--cloudops-text-primary);
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

@media (max-width: 768px) {
  .header-update-ago,
  .header-clock,
  .search-btn-text,
  .search-btn-shortcut {
    display: none;
  }
}
</style>
