<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { useAlertsWebSocket } from "../../composables/useAlertsWebSocket";
import { useTheme } from "../../composables/useTheme";
import { useAuthStore } from "../../stores/auth";
import { useMonitorStore } from "../../stores/monitor";

defineProps<{
  pageTitle: string;
}>();

const auth = useAuthStore();
const monitor = useMonitorStore();
const route = useRoute();
const router = useRouter();
const { isDark, toggleTheme } = useTheme();

const beijingTime = ref("");
const beijingTimer = ref<number | null>(null);
const updateAgoTimer = ref<number | null>(null);
const liveDataStarted = ref(false);

const { connectionState, connect, disconnect } = useAlertsWebSocket(
  monitor.applyIncomingAlert,
  monitor.applyIncomingHosts,
  monitor.applyIncomingDiagnosisUpdate,
  monitor.applyIncomingActionUpdate,
);

const connectionLabel = computed(() => {
  switch (connectionState.value) {
    case "connected":
      return "已连接";
    case "connecting":
      return "连接中";
    case "disconnected":
      return "离线";
  }
});

const connectionType = computed(() => {
  switch (connectionState.value) {
    case "connected":
      return "success";
    case "connecting":
      return "warning";
    case "disconnected":
      return "danger";
  }
});

const shouldUseLiveData = computed(() => auth.isAuthenticated);

function updateBeijingTime() {
  beijingTime.value = new Date().toLocaleString("zh-CN", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

function startLiveData() {
  if (liveDataStarted.value) return;
  monitor.refreshAll();
  connect();
  liveDataStarted.value = true;
}

function stopLiveData() {
  if (!liveDataStarted.value) return;
  disconnect();
  liveDataStarted.value = false;
}

async function logout() {
  auth.logout();
  stopLiveData();
  await router.push("/login");
}

import { watch } from "vue";

watch(
  shouldUseLiveData,
  (enabled) => {
    if (enabled) {
      startLiveData();
    } else {
      stopLiveData();
    }
  },
  { immediate: true },
);

onMounted(() => {
  updateBeijingTime();
  monitor.updateAgoText();
  beijingTimer.value = window.setInterval(updateBeijingTime, 1000);
  updateAgoTimer.value = window.setInterval(monitor.updateAgoText, 5000);
});

onBeforeUnmount(() => {
  stopLiveData();
  if (beijingTimer.value !== null) clearInterval(beijingTimer.value);
  if (updateAgoTimer.value !== null) clearInterval(updateAgoTimer.value);
});
</script>

<template>
  <el-header class="app-header" height="56px">
    <div class="header-left">
      <h2 class="header-page-title">{{ pageTitle }}</h2>
      <span class="header-update-ago">{{ monitor.updateAgo }}</span>
    </div>
    <div class="header-right">
      <el-tag
        :type="connectionType"
        size="small"
        effect="dark"
        round
        class="ws-tag"
      >
        <span class="ws-dot" :class="'ws-' + connectionState"></span>
        {{ connectionLabel }}
      </el-tag>
      <span class="header-clock">{{ beijingTime }}</span>
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
        <el-tag size="small" effect="plain" round>{{ auth.user?.role }}</el-tag>
      </div>
      <el-button size="small" text @click="logout">
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
  .header-clock {
    display: none;
  }
}
</style>
