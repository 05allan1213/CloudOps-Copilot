<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { RouterView, useRoute } from "vue-router";

import { useAlertsWebSocket } from "./composables/useAlertsWebSocket";
import { useAuthStore } from "./stores/auth";
import { useMonitorStore } from "./stores/monitor";
import AppLayout from "./components/layout/AppLayout.vue";

const monitor = useMonitorStore();
const auth = useAuthStore();
const route = useRoute();
const isFullscreen = ref(false);
const fullscreenError = ref("");
const liveDataStarted = ref(false);

const { connect, disconnect } = useAlertsWebSocket(
  monitor.applyIncomingAlert,
  monitor.applyIncomingHosts,
  monitor.applyIncomingDiagnosisUpdate,
  monitor.applyIncomingActionUpdate,
);

const isPublicRoute = computed(() => Boolean(route.meta.public));
const shouldUseLiveData = computed(() => !isPublicRoute.value && auth.isAuthenticated);

watch(
  () => monitor.alerts.length,
  (newLen) => {
    document.title =
      newLen > 0 ? `(${newLen}) CloudOps Monitor` : "CloudOps Monitor";
  },
);

function toggleFullscreen() {
  fullscreenError.value = "";
  if (!document.fullscreenElement) {
    document.documentElement.requestFullscreen().catch(() => {
      fullscreenError.value = "无法进入全屏模式";
    });
  } else {
    document.exitFullscreen().catch(() => {
      fullscreenError.value = "无法退出全屏模式";
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
  window.addEventListener("keydown", onKeydown);
  document.addEventListener("fullscreenchange", onFullscreenChange);
});

onBeforeUnmount(() => {
  stopLiveData();
  window.removeEventListener("keydown", onKeydown);
  document.removeEventListener("fullscreenchange", onFullscreenChange);
});
</script>

<template>
  <RouterView v-if="isPublicRoute" />
  <AppLayout v-else>
    <RouterView />
  </AppLayout>
</template>
