<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Search } from "@element-plus/icons-vue";

import { useAlertsWebSocket } from "../../composables/useAlertsWebSocket";
import { useTheme } from "../../composables/useTheme";
import { useK8sCluster } from "../../composables/useK8sCluster";
import { useAuthStore } from "../../stores/auth";
import { useMonitorStore } from "../../stores/monitor";
import GlobalSearchDialog from "../common/GlobalSearchDialog.vue";

defineProps<{
  pageTitle: string;
}>();

const auth = useAuthStore();
const monitor = useMonitorStore();
const route = useRoute();
const router = useRouter();
const { isDark, toggleTheme } = useTheme();
const { clusters, currentCluster, setCluster } = useK8sCluster();

const beijingTime = ref("");
const beijingTimer = ref<number | null>(null);
const updateAgoTimer = ref<number | null>(null);
const liveDataStarted = ref(false);
const searchVisible = ref(false);

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

function handleSearchKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key === "k") {
    e.preventDefault();
    searchVisible.value = true;
  }
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
  document.addEventListener("keydown", handleSearchKeydown);
});

onBeforeUnmount(() => {
  stopLiveData();
  if (beijingTimer.value !== null) clearInterval(beijingTimer.value);
  if (updateAgoTimer.value !== null) clearInterval(updateAgoTimer.value);
  document.removeEventListener("keydown", handleSearchKeydown);
});
</script>

<template>
  <el-header class="app-header" height="56px">
    <div class="header-left">
      <h2 class="header-page-title">{{ pageTitle }}</h2>
      <span class="header-update-ago">{{ monitor.updateAgo }}</span>
    </div>
    <div class="header-right">
      <el-select
        v-if="clusters.length > 1"
        v-model="currentCluster"
        size="small"
        style="width: 140px"
        @change="setCluster"
      >
        <el-option
          v-for="c in clusters"
          :key="c"
          :label="c"
          :value="c"
        />
      </el-select>
      <el-button
        size="small"
        :icon="Search"
        @click="searchVisible = true"
      >
        <span class="search-btn-text">搜索</span>
        <span class="search-btn-shortcut">Ctrl+K</span>
      </el-button>
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
  <GlobalSearchDialog v-model="searchVisible" />
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
