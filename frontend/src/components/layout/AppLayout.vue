<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, ref, watch, type Component as VueComponent } from "vue";
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
import { invalidateQueryDomain, setQueryCacheContext } from "../../composables/queryCache";
import {
  activateWorkspaceSectionReveals,
  consumeWorkspaceRouteEntrance,
} from "../../composables/workspaceMotion";
import {
  AGENT_CONTEXT_EVENT,
  AGENT_OPEN_EVENT,
  type AgentOpenRequest,
  type AgentPageContext,
} from "../../utils/agentContext";
import { dispatchOperationalScopeChange, queryForScopeChange } from "../../utils/operationalScope";
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
const online = ref(typeof navigator === "undefined" || navigator.onLine);
const routeBoundary = ref<HTMLElement | null>(null);
const routeFirstVisit = ref(consumeWorkspaceRouteEntrance(route.path));
const agentPanelRequested = ref(false);
let agentPanelModule: Promise<{ default: VueComponent }> | undefined;
const loadAgentPanel = () => (agentPanelModule ??= import("../agent/GlobalAgentPanel.vue"));
const LazyGlobalAgentPanel = defineAsyncComponent(() => loadAgentPanel().then((module) => module.default));
let pendingAgentOpen: AgentOpenRequest | null = null;
let latestAgentContext: AgentPageContext | null = null;
let compactDesktopQuery: MediaQueryList | undefined;
let closeNotificationStream: (() => void) | undefined;
let streamStartedAt = 0;
let headingObserver: MutationObserver | undefined;
let headingObserverTimeout: number | undefined;
let disposeSectionReveals: (() => void) | undefined;

const isFullBleed = computed(() => route.meta.fullBleed === true);
const isFixedWorkspace = computed(() => route.meta.fixedWorkspace === true);
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

function operationalScopeIdentity(scope: OperationalScope): string {
  return [scope.id, scope.cluster_id, scope.environment, [...scope.namespaces].sort().join(",")]
    .filter(Boolean)
    .join(":");
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
  invalidateQueryDomain("notifications");
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
  setQueryCacheContext({ operationalScope: operationalScopeIdentity(next.active_scope) });
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
  setQueryCacheContext({ operationalScope: operationalScopeIdentity(activeScope) });
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

function startRoutePresentation() {
  focusRouteHeading();
  disposeSectionReveals?.();
  disposeSectionReveals = routeBoundary.value
    ? activateWorkspaceSectionReveals(routeBoundary.value, route.path, routeFirstVisit.value)
    : undefined;
}

function stopRoutePresentation() {
  disposeSectionReveals?.();
  disposeSectionReveals = undefined;
}

function captureAgentContext(event: Event) {
  latestAgentContext = (event as CustomEvent<AgentPageContext | null>).detail ?? null;
}

function requestAgentPanel(event: Event) {
  if (agentPanelRequested.value) return;
  const request = (event as CustomEvent<AgentOpenRequest>).detail ?? {};
  pendingAgentOpen = { ...request, context: request.context ?? latestAgentContext ?? undefined };
  void loadAgentPanel().then(() => { agentPanelRequested.value = true; });
}

function replayPendingAgentEvent() {
  if (latestAgentContext) {
    window.dispatchEvent(new CustomEvent(AGENT_CONTEXT_EVENT, { detail: latestAgentContext }));
  }
  if (pendingAgentOpen) {
    const request = pendingAgentOpen;
    pendingAgentOpen = null;
    window.dispatchEvent(new CustomEvent(AGENT_OPEN_EVENT, { detail: request }));
  }
}

function updateConnectivity() {
  online.value = navigator.onLine;
}

watch(() => route.path, (path) => {
  stopRoutePresentation();
  routeFirstVisit.value = consumeWorkspaceRouteEntrance(path);
}, { flush: "sync" });

onMounted(async () => {
  compactDesktopQuery = window.matchMedia("(max-width: 1199px)");
  updateCompactDesktop(compactDesktopQuery);
  compactDesktopQuery.addEventListener("change", updateCompactDesktop);
  window.addEventListener("online", updateConnectivity);
  window.addEventListener("offline", updateConnectivity);
  window.addEventListener(AGENT_CONTEXT_EVENT, captureAgentContext);
  window.addEventListener(AGENT_OPEN_EVENT, requestAgentPanel);
  await Promise.all([refreshNotifications(), refreshBootstrap()]);
  streamStartedAt = Date.now();
  streamState.value = "connected";
  closeNotificationStream = openNotificationStream(receiveNotification, () => {
    streamState.value = "reconnecting";
  }, () => {
    streamState.value = "connected";
  });
  window.addEventListener("cloudops:configuration-applied", handleConfigurationApplied);
  await nextTick();
  startRoutePresentation();
});

onBeforeUnmount(() => {
  stopHeadingObserver();
  stopRoutePresentation();
  compactDesktopQuery?.removeEventListener("change", updateCompactDesktop);
  closeNotificationStream?.();
  streamState.value = "stopped";
  window.removeEventListener("cloudops:configuration-applied", handleConfigurationApplied);
  window.removeEventListener("online", updateConnectivity);
  window.removeEventListener("offline", updateConnectivity);
  window.removeEventListener(AGENT_CONTEXT_EVENT, captureAgentContext);
  window.removeEventListener(AGENT_OPEN_EVENT, requestAgentPanel);
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
      @toggle="toggleSidebar"
    />
    <div class="app-frame">
      <div
        class="shell-top-wash"
        aria-hidden="true"
      />
      <AppHeader
        :unread-count="unreadCount"
        :provider-health="bootstrap?.provider_health"
        :scenario-state="bootstrap?.scenario_state ?? 'inactive'"
        :active-scope="bootstrap?.active_scope"
        :scopes="scopes"
        :selected-scope-id="selectedScopeID"
        :scope-switching="scopeSwitching"
        @open-notifications="openNotifications"
        @change-scope="changeActiveScope"
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
      <UAlert
        v-if="!online"
        class="offline-alert"
        color="warning"
        variant="soft"
        icon="i-lucide-wifi-off"
        title="当前处于离线只读状态"
        description="已加载内容保持可读；所有写请求由客户端阻止，恢复网络后需重新读取服务端状态。"
        role="status"
      />
      <main
        id="main-content"
        class="app-main"
        data-testid="app-main"
        tabindex="-1"
        :class="{
          'app-main--full-bleed': isFullBleed,
          'app-main--fixed-workspace': isFixedWorkspace,
        }"
      >
        <RouterView v-slot="{ Component }">
          <Transition
            :css="false"
            @before-leave="stopRoutePresentation"
            @after-enter="startRoutePresentation"
          >
            <div
              ref="routeBoundary"
              :key="route.path"
              class="route-ui-boundary"
              :data-route-motion="routeFirstVisit ? 'first-visit' : 'cached-return'"
            >
              <component :is="Component" />
            </div>
          </Transition>
        </RouterView>
      </main>
    </div>
    <component
      :is="LazyGlobalAgentPanel"
      v-if="agentPanelRequested"
      @vue:mounted="replayPendingAgentEvent"
    />
  </div>

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

.scope-switch-alert,
.offline-alert {
  position: fixed;
  top: calc(var(--co-header-height) + var(--co-space-3));
  right: var(--co-space-4);
  z-index: var(--co-z-overlay);
  width: min(440px, calc(100vw - 32px));
  box-shadow: var(--co-shadow-overlay);
}

.offline-alert { top: calc(var(--co-header-height) + 92px); }

.app-shell {
  position: relative;
  display: flex;
  width: 100%;
  height: 100dvh;
  min-width: 0;
  min-height: 0;
  align-items: flex-start;
  overflow: hidden;
  background: var(--co-bg-canvas);
}

.app-frame {
  position: relative;
  display: flex;
  height: 100dvh;
  min-width: 0;
  min-height: 0;
  flex: 1 1 auto;
  overflow: hidden;
  flex-direction: column;
}

.shell-top-wash {
  position: absolute;
  top: 0;
  right: 0;
  left: 0;
  z-index: 0;
  height: 170px;
  pointer-events: none;
  background: linear-gradient(
    180deg,
    color-mix(in srgb, var(--co-bg-canvas) 78%, var(--co-ink-action) 8%) 0%,
    color-mix(in srgb, var(--co-bg-canvas) 62%, transparent) 42%,
    transparent 100%
  );
  backdrop-filter: blur(18px);
  mask-image: linear-gradient(to bottom, #000 0%, rgb(0 0 0 / 70%) 48%, transparent 100%);
}

.app-main {
  position: relative;
  z-index: 1;
  min-width: 0;
  min-height: 0;
  flex: 1 1 auto;
  padding: var(--co-page-gutter) max(var(--co-page-gutter), env(safe-area-inset-right))
    max(var(--co-page-end-space), env(safe-area-inset-bottom))
    max(var(--co-page-gutter), env(safe-area-inset-left));
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
  background: transparent;
}

.app-main--full-bleed {
  height: auto;
  padding: 0;
  overflow: hidden;
}

.route-ui-boundary { min-width: 0; min-height: 100%; isolation: isolate; }
.app-main--full-bleed .route-ui-boundary { height: 100%; }
.app-main--fixed-workspace { overflow: hidden; }
.app-main--fixed-workspace .route-ui-boundary { height: 100%; min-height: 0; }

:global(.notification-slideover) {
  width: min(440px, calc(100vw - var(--co-sidebar-rail-width)));
  max-width: none;
  overflow: hidden;
  border-left: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-overlay) 0 0 var(--co-radius-overlay);
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
