<script setup lang="ts">
import { computed, markRaw, nextTick, onBeforeUnmount, onMounted, ref, type Component } from "vue";
import {
  Gauge,
  GitPullRequest,
  Logs,
  ScanSearch,
  Server,
  Settings,
} from "lucide-vue-next";
import { useRoute } from "vue-router";

import {
  getNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  openNotificationStream,
  type OwnerNotification,
} from "../../api/notifications";
import { isApiError } from "../../api/client";
import { getBootstrap, type BootstrapSnapshot } from "../../api/platform";
import { mobileMoreNavigation, type NavigationIcon } from "../../navigation";
import AppHeader from "./AppHeader.vue";
import AppSidebar from "./AppSidebar.vue";
import MobileBottomNav from "./MobileBottomNav.vue";
import NotificationInbox from "./NotificationInbox.vue";

const route = useRoute();
const sidebarCollapsed = ref(readCollapsedPreference());
const notificationsOpen = ref(false);
const moreOpen = ref(false);
const notificationTrigger = ref<HTMLElement | null>(null);
const moreTrigger = ref<HTMLElement | null>(null);
const notifications = ref<OwnerNotification[]>([]);
const unreadCount = ref(0);
const notificationLoading = ref(false);
const notificationError = ref("");
const bootstrap = ref<BootstrapSnapshot | null>(null);
const streamState = ref<"connected" | "reconnecting" | "stopped">("stopped");
let closeNotificationStream: (() => void) | undefined;
let streamStartedAt = 0;

const pageTitle = computed(() => (typeof route.meta.title === "string" ? route.meta.title : ""));
const isFullBleed = computed(() => route.meta.fullBleed === true);
const moreIcons: Partial<Record<NavigationIcon, Component>> = {
  Server: markRaw(Server),
  Gauge: markRaw(Gauge),
  Logs: markRaw(Logs),
  ScanSearch: markRaw(ScanSearch),
  GitPullRequest: markRaw(GitPullRequest),
  Settings: markRaw(Settings),
};

function readCollapsedPreference(): boolean {
  try {
    return window.localStorage.getItem("cloudops.sidebar.collapsed") === "true";
  } catch {
    return false;
  }
}

function toggleSidebar() {
  sidebarCollapsed.value = !sidebarCollapsed.value;
  try {
    window.localStorage.setItem("cloudops.sidebar.collapsed", String(sidebarCollapsed.value));
  } catch {
    // The visual state remains usable when browser storage is unavailable.
  }
}

function openNotifications(trigger: HTMLElement) {
  notificationTrigger.value = trigger;
  notificationsOpen.value = true;
}

function openMore(trigger: HTMLElement) {
  moreTrigger.value = trigger;
  moreOpen.value = true;
}

function restoreNotificationFocus() {
  notificationTrigger.value?.focus();
}

function restoreMoreFocus() {
  moreTrigger.value?.focus();
}

async function refreshNotifications() {
  notificationLoading.value = true;
  notificationError.value = "";
  try {
    const page = await getNotifications();
    notifications.value = page.items;
    unreadCount.value = page.unread_count;
  } catch (error) {
    notificationError.value = isApiError(error)
      ? `通知读取失败（${error.code || "REQUEST_FAILED"}）：${error.message}`
      : "通知读取失败，请检查本地 API。";
  } finally {
    notificationLoading.value = false;
  }
}

async function refreshBootstrap() {
  try {
    bootstrap.value = await getBootstrap();
  } catch {
    bootstrap.value = null;
  }
}

function receiveNotification(item: OwnerNotification) {
  streamState.value = "connected";
  const index = notifications.value.findIndex((candidate) => candidate.id === item.id);
  if (index >= 0) notifications.value.splice(index, 1, item);
  else {
    notifications.value.unshift(item);
    mirrorOwnerNotification(item);
  }
  unreadCount.value = notifications.value.reduce((count, candidate) => count + (candidate.read ? 0 : 1), 0);
}

function mirrorOwnerNotification(item: OwnerNotification) {
  const createdAt = new Date(item.created_at).getTime();
  const enabled = bootstrap.value?.active_revision.general.browser_notifications_enabled === true;
  if (!enabled || !document.hidden || (item.severity !== "P1" && item.severity !== "P2") ||
      !Number.isFinite(createdAt) || createdAt < streamStartedAt || !("Notification" in window) ||
      window.Notification.permission !== "granted") return;
  new window.Notification(`CloudOps ${item.severity}`, {
    body: item.reason,
    tag: `cloudops-${item.source_type}-${item.source_id}-${item.source_state}`,
  });
}

async function readNotification(item: OwnerNotification) {
  if (item.read) return;
  try {
    await markNotificationRead(item.id);
    item.read = true;
    unreadCount.value = Math.max(0, unreadCount.value - 1);
  } catch (error) {
    notificationError.value = isApiError(error)
      ? `通知状态更新失败（${error.code || "REQUEST_FAILED"}）：${error.message}`
      : "通知状态更新失败。";
  }
}

async function readAllNotifications() {
  try {
    await markAllNotificationsRead();
    for (const item of notifications.value) item.read = true;
    unreadCount.value = 0;
  } catch (error) {
    notificationError.value = isApiError(error)
      ? `批量更新失败（${error.code || "REQUEST_FAILED"}）：${error.message}`
      : "批量更新通知失败。";
  }
}

function focusRouteHeading() {
  void nextTick(() => {
    const heading = document.querySelector<HTMLElement>("#main-content h1");
    if (!heading) return;
    if (!heading.hasAttribute("tabindex")) heading.setAttribute("tabindex", "-1");
    heading.focus({ preventScroll: true });
  });
}

onMounted(async () => {
  await Promise.all([refreshNotifications(), refreshBootstrap()]);
  streamStartedAt = Date.now();
  streamState.value = "connected";
  closeNotificationStream = openNotificationStream(receiveNotification, () => {
    streamState.value = "reconnecting";
  }, () => {
    streamState.value = "connected";
  });
  window.addEventListener("cloudops:configuration-applied", refreshBootstrap);
});

onBeforeUnmount(() => {
  closeNotificationStream?.();
  streamState.value = "stopped";
  window.removeEventListener("cloudops:configuration-applied", refreshBootstrap);
});
</script>

<template>
  <a class="skip-link" href="#main-content">跳到主要内容</a>
  <div class="app-shell">
    <AppSidebar :collapsed="sidebarCollapsed" @toggle="toggleSidebar" />
    <div class="app-frame">
      <AppHeader
        :page-title="pageTitle"
        :unread-count="unreadCount"
        :active-scope="bootstrap?.active_scope"
        :provider-health="bootstrap?.provider_health"
        @open-notifications="openNotifications"
      />
      <main id="main-content" class="app-main" :class="{ 'app-main--full-bleed': isFullBleed }">
        <RouterView v-slot="{ Component }">
          <Transition name="fade" mode="out-in" @after-enter="focusRouteHeading">
            <component :is="Component" :key="route.path" />
          </Transition>
        </RouterView>
      </main>
    </div>
  </div>

  <MobileBottomNav @open-more="openMore" />

  <el-drawer
    v-model="notificationsOpen"
    class="notification-drawer"
    direction="rtl"
    size="min(94vw, 440px)"
    title="Owner 通知"
    :append-to-body="true"
    :lock-scroll="true"
    :close-on-click-modal="true"
    :close-on-press-escape="true"
    @closed="restoreNotificationFocus"
  >
    <NotificationInbox
      :items="notifications"
      :unread-count="unreadCount"
      :loading="notificationLoading"
      :error="notificationError"
      :stream-state="streamState"
      @refresh="refreshNotifications"
      @read="readNotification"
      @read-all="readAllNotifications"
      @navigate="notificationsOpen = false"
    />
  </el-drawer>

  <el-drawer
    v-model="moreOpen"
    class="more-workspaces-sheet"
    direction="btt"
    size="min(72dvh, 520px)"
    title="更多工作区"
    :append-to-body="true"
    :lock-scroll="true"
    :close-on-click-modal="true"
    :close-on-press-escape="true"
    @closed="restoreMoreFocus"
  >
    <nav class="more-workspaces" aria-label="更多工作区">
      <RouterLink
        v-for="item in mobileMoreNavigation"
        :key="item.index"
        :to="item.index"
        @click="moreOpen = false"
      >
        <component :is="moreIcons[item.icon]" :size="21" aria-hidden="true" />
        <span>{{ item.title }}</span>
      </RouterLink>
    </nav>
  </el-drawer>
</template>

<style scoped>
.skip-link { position: fixed; top: var(--co-space-2); left: var(--co-space-2); z-index: var(--co-z-skip-link); padding: var(--co-space-3) var(--co-space-4); border-radius: var(--co-radius-control); color: var(--co-text-on-action); background: var(--co-action-primary); box-shadow: var(--co-shadow-overlay); transform: translateY(calc(-100% - var(--co-space-4))); transition: transform var(--co-motion-fast) var(--co-ease-out); }
.skip-link:focus-visible { transform: translateY(0); }
.app-shell { display: flex; min-width: 0; min-height: 100dvh; align-items: flex-start; background: var(--co-bg-canvas); }
.app-frame { flex: 1; min-width: 0; min-height: 100dvh; }
.app-main { min-width: 0; min-height: calc(100dvh - var(--co-header-height)); padding: clamp(16px, 2vw, 32px) max(clamp(16px, 2vw, 32px), env(safe-area-inset-right)) max(clamp(24px, 3vw, 48px), env(safe-area-inset-bottom)) max(clamp(16px, 2vw, 32px), env(safe-area-inset-left)); background: var(--co-bg-canvas); }
.app-main--full-bleed { height: calc(100dvh - var(--co-header-height)); padding: 0; overflow: hidden; }
.more-workspaces { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-3); }
.more-workspaces a { display: flex; min-width: 0; min-height: 64px; align-items: center; gap: var(--co-space-3); padding: 0 var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); color: var(--co-text-secondary); background: var(--co-bg-surface); font-weight: 700; }
.more-workspaces a:hover { border-color: var(--co-border-strong); color: var(--co-text-primary); background: var(--co-bg-hover); }
.more-workspaces a.router-link-active { border-color: var(--co-status-info-border); color: var(--co-action-primary); background: var(--co-bg-active); }
:global(.notification-drawer.el-drawer), :global(.more-workspaces-sheet.el-drawer) { background: var(--co-bg-surface); }
:global(.notification-drawer .el-drawer__header), :global(.more-workspaces-sheet .el-drawer__header) { min-height: var(--co-header-height); margin: 0; padding: 0 max(var(--co-space-4), env(safe-area-inset-right)) 0 max(var(--co-space-4), env(safe-area-inset-left)); border-bottom: 1px solid var(--co-border-default); color: var(--co-text-primary); font-weight: 750; }
:global(.notification-drawer .el-drawer__body) { padding: var(--co-space-5); overscroll-behavior: contain; }
:global(.more-workspaces-sheet .el-drawer__body) { padding: var(--co-space-5) max(var(--co-space-4), env(safe-area-inset-right)) max(var(--co-space-5), env(safe-area-inset-bottom)) max(var(--co-space-4), env(safe-area-inset-left)); overscroll-behavior: contain; }
:global(.notification-drawer .el-drawer__close-btn), :global(.more-workspaces-sheet .el-drawer__close-btn) { width: 44px; height: 44px; border-radius: var(--co-radius-control); }
@media (max-width: 767px) {
  .app-main { min-height: calc(100dvh - var(--co-header-height)); padding: var(--co-space-4) max(var(--co-space-4), env(safe-area-inset-right)) calc(82px + env(safe-area-inset-bottom)) max(var(--co-space-4), env(safe-area-inset-left)); overflow-x: hidden; }
  .app-main--full-bleed { height: calc(100dvh - var(--co-header-height) - 58px - env(safe-area-inset-bottom)); padding: 0; }
  :global(.notification-drawer .el-drawer__body) { padding: var(--co-space-4); }
}
@media (max-width: 359px) { .more-workspaces { grid-template-columns: 1fr; } }
</style>
