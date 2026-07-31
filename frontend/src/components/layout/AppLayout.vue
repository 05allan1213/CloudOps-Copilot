<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { isApiError } from "../../api/client";
import {
  getNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  openNotificationStream,
  type OwnerNotification,
} from "../../api/notifications";
import {
  activateScope,
  getBootstrap,
  getScopes,
  type BootstrapSnapshot,
  type OperationalScope,
} from "../../api/platform";
import { dispatchOperationalScopeChange, queryForScopeChange } from "../../utils/operationalScope";
import { openAgentPanel } from "../../utils/agentContext";
import GlobalAgentPanel from "../agent/GlobalAgentPanel.vue";
import AppHeader from "./AppHeader.vue";
import AppSidebar from "./AppSidebar.vue";
import NotificationInbox from "./NotificationInbox.vue";

const route = useRoute();
const router = useRouter();
const sidebarCollapsed = ref(readCollapsedPreference());
const compactDesktop = ref(readCompactDesktop());
const notificationsOpen = ref(false);
const notificationTrigger = ref<HTMLElement | null>(null);
const notifications = ref<OwnerNotification[]>([]);
const unreadCount = ref(0);
const notificationLoading = ref(false);
const notificationError = ref("");
const bootstrap = ref<BootstrapSnapshot | null>(null);
const scopes = ref<OperationalScope[]>([]);
const selectedScopeID = ref("");
const scopeSwitching = ref(false);
const scopeSwitchError = ref("");
const streamState = ref<"connected" | "reconnecting" | "stopped">("stopped");
let compactDesktopQuery: MediaQueryList | undefined;
let closeNotificationStream: (() => void) | undefined;
let streamStartedAt = 0;
let headingObserver: MutationObserver | undefined;
let headingObserverTimeout: number | undefined;

const pageTitle = computed(() => (typeof route.meta.title === "string" ? route.meta.title : ""));
const isFullBleed = computed(() => route.meta.fullBleed === true);
const sidebarRail = computed(() => sidebarCollapsed.value || compactDesktop.value);
const notificationSlideoverUI = {
  overlay: "notification-slideover-overlay",
  content: "notification-slideover",
  header: "notification-slideover-header",
  body: "notification-slideover-body",
};

function readCollapsedPreference(): boolean {
  try {
    return typeof window !== "undefined" && window.localStorage.getItem("cloudops.sidebar.collapsed") === "true";
  } catch {
    return false;
  }
}

function readCompactDesktop(): boolean {
  return typeof window !== "undefined" && window.matchMedia("(max-width: 1199px)").matches;
}

function updateCompactDesktop(event: MediaQueryListEvent | MediaQueryList) {
  compactDesktop.value = event.matches;
}

function toggleSidebar() {
  if (compactDesktop.value) return;
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

function focusNotificationHeading() {
  void nextTick(() => {
    const heading = document.querySelector<HTMLElement>("#notification-inbox-title");
    if (!heading) return;
    heading.setAttribute("tabindex", "-1");
    heading.focus({ preventScroll: true });
  });
}

function restoreNotificationFocus() {
  if (notificationTrigger.value?.isConnected) notificationTrigger.value.focus({ preventScroll: true });
  notificationTrigger.value = null;
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

async function readPlatformContext(): Promise<BootstrapSnapshot> {
  const [bootstrapResult, scopesResult] = await Promise.allSettled([getBootstrap(), getScopes()]);
  if (bootstrapResult.status === "rejected") throw bootstrapResult.reason;
  const next = bootstrapResult.value;
  const registeredScopes = scopesResult.status === "fulfilled" && scopesResult.value.length
    ? scopesResult.value
    : next.active_revision.scopes;
  bootstrap.value = next;
  scopes.value = registeredScopes;
  selectedScopeID.value = next.active_scope.id
    ?? registeredScopes.find((scope) => scope.cluster_id === next.active_scope.cluster_id)?.id
    ?? "";
  return next;
}

async function refreshBootstrap(): Promise<BootstrapSnapshot | null> {
  try {
    return await readPlatformContext();
  } catch {
    if (!bootstrap.value) bootstrap.value = null;
    return null;
  }
}

function describeScopeSwitchError(reason: unknown): string {
  if (!isApiError(reason)) return "活动集群切换失败，请检查 Kubernetes Provider 与 API 状态。";
  const next = reason.nextSteps.length ? `；下一步：${reason.nextSteps.join("；")}` : "";
  return `${reason.code || "SCOPE_ACTIVATION_FAILED"}：${reason.message}${next}`;
}

async function pushScopeContext(scope: OperationalScope) {
  await router.push({
    path: route.path,
    query: queryForScopeChange(route.query, scope.cluster_id),
    hash: route.hash,
  });
}

function applyLocalScope(scope: OperationalScope) {
  const activeScope = { ...scope, active: true };
  scopes.value = scopes.value.map((item) => ({ ...item, active: item.id === activeScope.id }));
  selectedScopeID.value = activeScope.id ?? "";
  if (bootstrap.value) {
    bootstrap.value = {
      ...bootstrap.value,
      active_scope: activeScope,
      active_revision: {
        ...bootstrap.value.active_revision,
        scope: activeScope,
        scopes: scopes.value,
      },
    };
  }
}

async function changeActiveScope(scopeID: string) {
  if (scopeSwitching.value || !scopeID || scopeID === bootstrap.value?.active_scope.id) return;
  const previousScope = bootstrap.value?.active_scope;
  selectedScopeID.value = scopeID;
  scopeSwitching.value = true;
  scopeSwitchError.value = "";
  let activatedScope: OperationalScope | null = null;
  try {
    activatedScope = await activateScope(scopeID);
    const next = await readPlatformContext();
    await pushScopeContext(next.active_scope);
    dispatchOperationalScopeChange(next.active_scope);
  } catch (reason) {
    if (!activatedScope) {
      selectedScopeID.value = previousScope?.id ?? "";
      scopeSwitchError.value = describeScopeSwitchError(reason);
      return;
    }
    applyLocalScope(activatedScope);
    await pushScopeContext(activatedScope);
    dispatchOperationalScopeChange(activatedScope);
    scopeSwitchError.value = "活动集群已切换，但 Bootstrap 状态刷新失败；请重试刷新。";
  } finally {
    scopeSwitching.value = false;
  }
}

async function handleConfigurationApplied() {
  const previousCluster = bootstrap.value?.active_scope.cluster_id;
  const next = await refreshBootstrap();
  if (!next) return;
  if (previousCluster !== next.active_scope.cluster_id) await pushScopeContext(next.active_scope);
  dispatchOperationalScopeChange(next.active_scope);
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
  if (!enabled || !document.hidden || (item.severity !== "P1" && item.severity !== "P2")
      || !Number.isFinite(createdAt) || createdAt < streamStartedAt || !("Notification" in window)
      || window.Notification.permission !== "granted") return;
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

function stopHeadingObserver() {
  headingObserver?.disconnect();
  headingObserver = undefined;
  if (headingObserverTimeout !== undefined) window.clearTimeout(headingObserverTimeout);
  headingObserverTimeout = undefined;
}

function focusRouteHeading() {
  stopHeadingObserver();
  void nextTick(() => {
    const focusHeading = () => {
      const heading = document.querySelector<HTMLElement>("#main-content > :not(.fade-leave-active):not(.fade-leave-to) h1");
      if (!heading) return false;
      if (!heading.hasAttribute("tabindex")) heading.setAttribute("tabindex", "-1");
      heading.focus({ preventScroll: true });
      return true;
    };
    if (focusHeading()) return;
    const main = document.querySelector<HTMLElement>("#main-content");
    if (!main) return;
    headingObserver = new MutationObserver(() => {
      if (focusHeading()) stopHeadingObserver();
    });
    headingObserver.observe(main, { childList: true, subtree: true });
    headingObserverTimeout = window.setTimeout(stopHeadingObserver, 2_000);
  });
}

watch(() => route.path, () => focusRouteHeading(), { flush: "post" });

onMounted(async () => {
  compactDesktopQuery = window.matchMedia("(max-width: 1199px)");
  updateCompactDesktop(compactDesktopQuery);
  compactDesktopQuery.addEventListener("change", updateCompactDesktop);
  await Promise.all([refreshNotifications(), refreshBootstrap()]);
  streamStartedAt = Date.now();
  streamState.value = "connected";
  closeNotificationStream = openNotificationStream(receiveNotification, () => {
    streamState.value = "reconnecting";
  }, () => {
    streamState.value = "connected";
  });
  window.addEventListener("cloudops:configuration-applied", handleConfigurationApplied);
});

onBeforeUnmount(() => {
  stopHeadingObserver();
  compactDesktopQuery?.removeEventListener("change", updateCompactDesktop);
  closeNotificationStream?.();
  streamState.value = "stopped";
  window.removeEventListener("cloudops:configuration-applied", handleConfigurationApplied);
});
</script>

<template>
  <a
    class="skip-link"
    href="#main-content"
  >跳到主要内容</a>
  <div class="app-shell">
    <AppSidebar
      :collapsed="sidebarRail"
      :collapse-locked="compactDesktop"
      :active-scope="bootstrap?.active_scope"
      :scopes="scopes"
      :selected-scope-id="selectedScopeID"
      :scope-switching="scopeSwitching"
      @toggle="toggleSidebar"
      @change-scope="changeActiveScope"
      @open-agent="openAgentPanel()"
    />
    <div class="app-frame">
      <AppHeader
        :page-title="pageTitle"
        :unread-count="unreadCount"
        :provider-health="bootstrap?.provider_health"
        :scenario-state="bootstrap?.scenario_state ?? 'inactive'"
        @open-notifications="openNotifications"
      />
      <UAlert
        v-if="scopeSwitchError"
        class="scope-switch-alert"
        color="error"
        variant="soft"
        icon="i-lucide-triangle-alert"
        title="运行范围切换异常"
        :description="scopeSwitchError"
        :close="{ 'aria-label': '关闭集群切换错误' }"
        role="alert"
        aria-live="polite"
        @update:open="scopeSwitchError = ''"
      />
      <main
        id="main-content"
        class="app-main"
        data-testid="app-main"
        tabindex="-1"
        :class="{ 'app-main--full-bleed': isFullBleed }"
      >
        <RouterView v-slot="{ Component }">
          <Transition
            name="fade"
            @after-enter="focusRouteHeading"
          >
            <div
              :key="route.path"
              class="route-ui-boundary"
            >
              <component :is="Component" />
            </div>
          </Transition>
        </RouterView>
      </main>
    </div>
  </div>

  <GlobalAgentPanel />

  <USlideover
    v-model:open="notificationsOpen"
    title="Owner 通知"
    description="实时事件、待处理状态与可信上下文入口"
    side="right"
    :close="{ 'aria-label': '关闭通知收件箱' }"
    :ui="notificationSlideoverUI"
    @after:enter="focusNotificationHeading"
    @after:leave="restoreNotificationFocus"
  >
    <template #body>
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
    </template>
  </USlideover>
</template>

<style scoped>
.skip-link {
  position: fixed;
  top: var(--co-space-2);
  left: var(--co-space-2);
  z-index: var(--co-z-skip-link);
  padding: var(--co-space-3) var(--co-space-4);
  border-radius: var(--co-radius-control);
  color: var(--co-text-on-action);
  background: var(--co-action-primary);
  box-shadow: var(--co-shadow-overlay);
  transform: translateY(calc(-100% - var(--co-space-4)));
  transition: transform var(--co-motion-fast) var(--co-ease-out);
}

.skip-link:focus-visible { transform: translateY(0); }

.scope-switch-alert {
  position: fixed;
  top: calc(var(--co-header-height) + var(--co-space-3));
  right: var(--co-space-4);
  z-index: var(--co-z-overlay);
  width: min(440px, calc(100vw - 32px));
  box-shadow: var(--co-shadow-overlay);
}

.app-shell {
  display: flex;
  min-width: 0;
  min-height: 100dvh;
  align-items: flex-start;
  background: var(--co-bg-canvas);
}

.app-frame { min-width: 0; min-height: 100dvh; flex: 1; }

.app-main {
  min-width: 0;
  min-height: calc(100dvh - var(--co-header-height));
  padding: clamp(16px, 2vw, 32px) max(clamp(16px, 2vw, 32px), env(safe-area-inset-right))
    max(clamp(24px, 3vw, 48px), env(safe-area-inset-bottom))
    max(clamp(16px, 2vw, 32px), env(safe-area-inset-left));
  background: var(--co-bg-canvas);
}

.app-main--full-bleed {
  height: calc(100dvh - var(--co-header-height));
  padding: 0;
  overflow: hidden;
}

.route-ui-boundary { min-width: 0; min-height: 100%; isolation: isolate; }
.app-main--full-bleed .route-ui-boundary { height: 100%; }

:global(.notification-slideover) {
  width: min(440px, calc(100vw - var(--co-sidebar-rail-width)));
  max-width: none;
  border-left: 1px solid var(--co-border-default);
  background: var(--co-bg-overlay);
  box-shadow: var(--co-shadow-overlay);
  z-index: calc(var(--co-z-overlay) + 1);
}

:global(.notification-slideover-overlay) { z-index: var(--co-z-overlay); }

:global(.notification-slideover-header) {
  min-height: var(--co-header-height);
  padding: 0 var(--co-space-4);
  border-bottom: 1px solid var(--co-border-default);
}

:global(.notification-slideover-body) {
  padding: var(--co-space-5);
  overflow-y: auto;
  overscroll-behavior: contain;
}
</style>
