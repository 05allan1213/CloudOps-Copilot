<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { RouterLink, RouterView, useRoute, useRouter } from "vue-router";

import { useAlertsWebSocket } from "./composables/useAlertsWebSocket";
import { useAuthStore } from "./stores/auth";
import { useMonitorStore } from "./stores/monitor";

const monitor = useMonitorStore();
const auth = useAuthStore();
const route = useRoute();
const router = useRouter();
const beijingTime = ref("");
const beijingTimer = ref<number | null>(null);
const updateAgoTimer = ref<number | null>(null);
const isFullscreen = ref(false);
const fullscreenError = ref("");
const liveDataStarted = ref(false);

const { connectionState, connect, disconnect } = useAlertsWebSocket(
  monitor.applyIncomingAlert,
  monitor.applyIncomingHosts,
);

const connectionLabel = computed(() => {
  switch (connectionState.value) {
    case "connected":
      return "实时连接";
    case "connecting":
      return "连接中";
    case "disconnected":
      return "离线";
  }
});

const isPublicRoute = computed(() => Boolean(route.meta.public));
const shouldUseLiveData = computed(() => !isPublicRoute.value && auth.isAuthenticated);

watch(
  () => monitor.alerts.length,
  (newLen) => {
    document.title =
      newLen > 0 ? `(${newLen}) 服务监控大屏` : "服务监控大屏";
  },
);

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

function toggleFullscreen() {
  fullscreenError.value = "";
  if (!document.fullscreenElement) {
    document.documentElement.requestFullscreen().catch((err) => {
      fullscreenError.value = "无法进入全屏模式";
      console.warn("Fullscreen request failed:", err);
    });
  } else {
    document.exitFullscreen().catch((err) => {
      fullscreenError.value = "无法退出全屏模式";
      console.warn("Exit fullscreen failed:", err);
    });
  }
}

function onFullscreenChange() {
  isFullscreen.value = !!document.fullscreenElement;
}

function onKeydown(e: KeyboardEvent) {
  if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) {
    return;
  }
  if (e.target instanceof HTMLElement && e.target.isContentEditable) {
    return;
  }
  if (e.ctrlKey || e.altKey || e.metaKey) {
    return;
  }
  if (e.key === "r" || e.key === "R") {
    e.preventDefault();
    monitor.refreshAll();
  } else if (e.key === "f" || e.key === "F") {
    e.preventDefault();
    toggleFullscreen();
  }
}

function startLiveData() {
  if (liveDataStarted.value) {
    return;
  }
  monitor.refreshAll();
  connect();
  liveDataStarted.value = true;
}

function stopLiveData() {
  if (!liveDataStarted.value) {
    return;
  }
  disconnect();
  liveDataStarted.value = false;
}

async function logout() {
  auth.logout();
  stopLiveData();
  await router.push("/login");
}

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
  window.addEventListener("keydown", onKeydown);
  document.addEventListener("fullscreenchange", onFullscreenChange);
});

onBeforeUnmount(() => {
  stopLiveData();
  if (beijingTimer.value !== null) clearInterval(beijingTimer.value);
  if (updateAgoTimer.value !== null) clearInterval(updateAgoTimer.value);
  monitor.clearToastTimers();
  window.removeEventListener("keydown", onKeydown);
  document.removeEventListener("fullscreenchange", onFullscreenChange);
});
</script>

<template>
  <RouterView v-if="isPublicRoute" />
  <div v-else class="app-container">
    <!-- Toast Notifications -->
    <div class="toast-container">
      <transition-group name="toast">
        <div
          v-for="toast in monitor.toasts"
          :key="toast.id"
          class="toast"
          :class="monitor.severityClass(toast.severity)"
        >
          <span class="toast-severity">
            {{ monitor.severityLabel(toast.severity) }}
          </span>
          <span class="toast-message">{{ toast.message }}</span>
        </div>
      </transition-group>
    </div>

    <!-- Header -->
    <header class="header">
      <div class="header-left">
        <div class="logo">
          <div class="logo-icon"></div>
          <div class="logo-text">
            <h1>服务监控大屏</h1>
            <p class="logo-sub">实时主机指标与告警推送</p>
          </div>
        </div>
      </div>
      <div class="header-right">
        <div class="update-ago">{{ monitor.updateAgo }}</div>
        <div class="clock">
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <circle cx="12" cy="12" r="10" />
            <polyline points="12 6 12 12 16 14" />
          </svg>
          <span>{{ beijingTime }}</span>
        </div>
        <button class="fullscreen-btn" title="全屏 (F)" @click="toggleFullscreen">
          <svg
            v-if="!isFullscreen"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3" />
          </svg>
          <svg
            v-else
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <path d="M8 3v3a2 2 0 0 1-2 2H3m18 0h-3a2 2 0 0 1-2-2V3m0 18v-3a2 2 0 0 1 2-2h3M3 16h3a2 2 0 0 1 2 2v3" />
          </svg>
        </button>
        <span v-if="fullscreenError" class="fullscreen-error">{{ fullscreenError }}</span>
        <div class="ws-status" :class="'ws-' + connectionState">
          <span class="ws-dot"></span>
          <span>{{ connectionLabel }}</span>
        </div>
        <div class="auth-user">
          <span class="auth-name">{{ auth.user?.username }}</span>
          <span class="auth-role">{{ auth.user?.role }}</span>
        </div>
        <button class="logout-btn" type="button" @click="logout">
          退出
        </button>
      </div>
    </header>

    <nav class="route-tabs" aria-label="页面导航">
      <RouterLink to="/" class="route-tab" exact-active-class="active">
        总览
      </RouterLink>
      <RouterLink to="/hosts" class="route-tab" exact-active-class="active">
        主机
      </RouterLink>
      <RouterLink to="/status" class="route-tab" exact-active-class="active">
        状态
      </RouterLink>
      <RouterLink to="/alerts" class="route-tab" exact-active-class="active">
        告警
      </RouterLink>
      <RouterLink to="/alert-histories" class="route-tab" exact-active-class="active">
        历史
      </RouterLink>
      <RouterLink
        v-if="auth.isAdmin"
        to="/settings"
        class="route-tab"
        exact-active-class="active"
      >
        设置
      </RouterLink>
    </nav>

    <RouterView />
  </div>
</template>

<style scoped>
.app-container {
  max-width: 1200px;
  margin: 0 auto;
  padding: 1.5rem;
  min-height: 100vh;
}

/* Toast Notifications */
.toast-container {
  position: fixed;
  top: 1rem;
  right: 1rem;
  z-index: 9999;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  pointer-events: none;
}

.toast {
  pointer-events: auto;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 0.75rem 1rem;
  min-width: 280px;
  max-width: 400px;
  box-shadow: var(--shadow-md);
  display: flex;
  align-items: center;
  gap: 0.5rem;
  backdrop-filter: blur(8px);
}

.toast.severity-critical {
  border-left: 3px solid var(--danger);
}

.toast.severity-warning {
  border-left: 3px solid var(--warning);
}

.toast.severity-info {
  border-left: 3px solid var(--info);
}

.toast-severity {
  font-size: 0.7rem;
  font-weight: 700;
  padding: 0.15em 0.4em;
  border-radius: 4px;
  flex-shrink: 0;
}

.toast.severity-critical .toast-severity {
  background: var(--danger-soft);
  color: var(--danger);
}

.toast.severity-warning .toast-severity {
  background: var(--warning-soft);
  color: var(--warning);
}

.toast.severity-info .toast-severity {
  background: var(--info-soft);
  color: var(--info);
}

.toast-message {
  font-size: 0.8rem;
  color: var(--text-secondary);
}

.toast-enter-active,
.toast-leave-active {
  transition: all 0.3s ease;
}

.toast-enter-from {
  opacity: 0;
  transform: translateX(100%);
}

.toast-leave-to {
  opacity: 0;
  transform: translateX(100%);
}

/* Header */
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1.5rem;
  padding-bottom: 1rem;
  border-bottom: 1px solid var(--border-color);
}

.logo {
  display: flex;
  align-items: center;
  gap: 0.875rem;
}

.logo-icon {
  width: 40px;
  height: 40px;
  border-radius: var(--radius-md);
  background: linear-gradient(135deg, var(--accent), #6366f1);
  box-shadow: 0 0 16px var(--accent-glow);
  position: relative;
}

.logo-icon::after {
  content: "";
  position: absolute;
  inset: 8px;
  border: 2px solid rgba(255, 255, 255, 0.4);
  border-radius: 4px;
}

.logo-text h1 {
  font-size: 1.25rem;
  font-weight: 700;
  margin: 0;
  letter-spacing: -0.02em;
}

.logo-sub {
  font-size: 0.75rem;
  color: var(--text-muted);
  margin: 0.15rem 0 0;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.update-ago {
  font-size: 0.7rem;
  color: var(--text-muted);
  font-weight: 500;
}

.clock {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.85rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: var(--text-secondary);
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  padding: 0.4rem 0.75rem;
  border-radius: var(--radius-sm);
}

.clock svg {
  color: var(--accent);
}

.fullscreen-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-color);
  color: var(--text-muted);
  background: var(--bg-card);
  transition: all 0.15s;
}

.fullscreen-btn:hover {
  border-color: var(--accent);
  color: var(--accent);
}

.fullscreen-error {
  font-size: 0.7rem;
  color: var(--warning);
  background: var(--warning-soft);
  padding: 0.2rem 0.5rem;
  border-radius: var(--radius-sm);
}

.ws-status {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.75rem;
  font-weight: 600;
  padding: 0.35rem 0.75rem;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-color);
}

.ws-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
}

.ws-connected {
  background: var(--success-soft);
  border-color: rgba(34, 197, 94, 0.3);
  color: var(--success);
}

.ws-connected .ws-dot {
  background: var(--success);
  box-shadow: 0 0 6px var(--success);
}

.ws-connecting {
  background: var(--warning-soft);
  border-color: rgba(245, 158, 11, 0.3);
  color: var(--warning);
}

.ws-connecting .ws-dot {
  background: var(--warning);
  animation: pulse 1.5s infinite;
}

.ws-disconnected {
  background: var(--danger-soft);
  border-color: rgba(239, 68, 68, 0.3);
  color: var(--danger);
}

.ws-disconnected .ws-dot {
  background: var(--danger);
}

.auth-user {
  display: grid;
  gap: 0.1rem;
  min-width: 88px;
  padding: 0.32rem 0.65rem;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-card);
}

.auth-name {
  color: var(--text-primary);
  font-size: 0.78rem;
  font-weight: 700;
  line-height: 1.1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.auth-role {
  color: var(--text-muted);
  font-size: 0.66rem;
  font-weight: 700;
  line-height: 1.1;
  text-transform: uppercase;
}

.logout-btn {
  color: var(--text-secondary);
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.43rem 0.7rem;
  font-size: 0.75rem;
  font-weight: 700;
  transition: border-color 0.15s, color 0.15s;
}

.logout-btn:hover {
  border-color: var(--danger);
  color: var(--danger);
}

.route-tabs {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  margin-bottom: 1.5rem;
  border-bottom: 1px solid var(--border-color);
}

.route-tab {
  color: var(--text-muted);
  font-size: 0.85rem;
  font-weight: 600;
  padding: 0.7rem 0.9rem;
  border-bottom: 2px solid transparent;
  transition: color 0.15s, border-color 0.15s;
}

.route-tab:hover {
  color: var(--text-secondary);
}

.route-tab.active {
  color: var(--accent);
  border-bottom-color: var(--accent);
}

@keyframes pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.4;
  }
}

/* Responsive */
@media (max-width: 768px) {
  .app-container {
    padding: 1rem;
  }

  .header {
    flex-direction: column;
    align-items: flex-start;
    gap: 1rem;
  }

  .header-right {
    flex-wrap: wrap;
    width: 100%;
  }

  .toast-container {
    left: 1rem;
    right: 1rem;
  }

  .toast {
    max-width: 100%;
    min-width: auto;
  }
}

</style>
