<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";

import { redirectToOAuth } from "../../api/client";
import StateBlock from "../incidents/StateBlock.vue";
import AppHeader from "./AppHeader.vue";
import AppSidebar from "./AppSidebar.vue";
import SidebarMenu from "./SidebarMenu.vue";
import { useAuthStore } from "../../stores/auth";

const route = useRoute();
const auth = useAuthStore();
const mobileNavigationOpen = ref(false);
const mobileNavigationTrigger = ref<HTMLElement | null>(null);

const pageTitle = computed(() =>
  typeof route.meta.title === "string" ? route.meta.title : "",
);
const isFullBleed = computed(() => route.meta.fullBleed === true);
const authBoundaryState = computed(() => auth.error?.status === 403 ? "forbidden" : "unavailable");

function openMobileNavigation(trigger?: HTMLElement) {
  mobileNavigationTrigger.value = trigger ?? null;
  mobileNavigationOpen.value = true;
}

function closeMobileNavigation() {
  mobileNavigationOpen.value = false;
}

function restoreMobileNavigationFocus() {
  mobileNavigationTrigger.value?.focus();
}

async function retrySession() {
  try {
    await auth.loadSession(true);
  } catch {
    // The boundary retains the normalized problem and recovery actions.
  }
}

watch(
  () => route.path,
  () => {
    closeMobileNavigation();
  },
);
</script>

<template>
  <a
    class="skip-link"
    href="#incident-content"
  >
    Skip to incident content
  </a>
  <div class="app-shell">
    <AppSidebar />
    <div class="app-frame">
      <AppHeader
        :page-title="pageTitle"
        @open-navigation="openMobileNavigation"
      />
      <main
        id="incident-content"
        class="app-main"
        :class="{ 'app-main--full-bleed': isFullBleed }"
        tabindex="-1"
      >
        <section
          v-if="auth.loading && !auth.initialized && !auth.error"
          class="auth-boundary"
          aria-labelledby="auth-boundary-title"
        >
          <h1
            id="auth-boundary-title"
            class="visually-hidden"
          >
            CloudOps Incident Agent session
          </h1>
          <StateBlock
            state="loading"
            :heading-level="2"
            title="Establishing GitHub session"
            message="Checking the trusted session and issuing a short-lived CSRF token."
          />
        </section>
        <section
          v-else-if="auth.error"
          class="auth-boundary"
          aria-labelledby="auth-boundary-title"
        >
          <h1
            id="auth-boundary-title"
            class="visually-hidden"
          >
            CloudOps Incident Agent access
          </h1>
          <StateBlock
            :state="authBoundaryState"
            :heading-level="2"
            :title="authBoundaryState === 'forbidden' ? 'Workbench access is forbidden' : 'Workbench session unavailable'"
            :message="auth.error.message"
            :request-i-d="auth.error.requestID"
            :trace-i-d="auth.error.traceID"
            primary-action-label="Retry Session"
            secondary-action-label="Re-authenticate"
            @primary-action="retrySession"
            @secondary-action="redirectToOAuth"
          />
        </section>
        <RouterView
          v-else
          v-slot="{ Component }"
        >
          <Transition
            name="fade"
            mode="out-in"
          >
            <component
              :is="Component"
              :key="route.path"
            />
          </Transition>
        </RouterView>
      </main>
    </div>
  </div>

  <el-drawer
    v-model="mobileNavigationOpen"
    class="mobile-navigation"
    direction="ltr"
    size="min(86vw, 320px)"
    title="CloudOps Incident Agent navigation"
    :append-to-body="true"
    :lock-scroll="true"
    :modal="true"
    :close-on-click-modal="true"
    :close-on-press-escape="true"
    @closed="restoreMobileNavigationFocus"
  >
    <div class="mobile-navigation-context">
      <span
        class="environment-dot"
        aria-hidden="true"
      />
      <span>Demo / kind environment</span>
    </div>
    <SidebarMenu
      variant="drawer"
      @navigate="closeMobileNavigation"
    />
  </el-drawer>
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

.skip-link:focus-visible {
  transform: translateY(0);
}

.app-shell {
  display: flex;
  height: 100dvh;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  background: var(--co-bg-canvas);
}

.app-frame {
  display: flex;
  flex: 1;
  height: 100dvh;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  flex-direction: column;
}

.app-main {
  flex: 1;
  min-width: 0;
  padding: clamp(16px, 2vw, 32px);
  overflow: auto;
  overscroll-behavior: contain;
  background: var(--co-bg-canvas);
  scroll-padding-top: calc(var(--co-header-height) + var(--co-space-4));
}

.app-main:focus-visible {
  outline: 3px solid var(--co-focus-ring);
  outline-offset: -3px;
}

.app-main--full-bleed {
  padding: 0;
  overflow: hidden;
}

.auth-boundary {
  display: grid;
  width: min(100%, 760px);
  min-height: 40vh;
  align-content: center;
  margin: 0 auto;
}

.mobile-navigation-context {
  display: flex;
  align-items: center;
  gap: var(--co-space-2);
  margin: 0 var(--co-space-2) var(--co-space-4);
  padding: var(--co-space-3);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  color: var(--co-text-secondary);
  background: var(--co-bg-subtle);
  font-size: 13px;
}

.environment-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--co-status-info-fg);
  box-shadow: 0 0 0 3px var(--co-status-info-bg);
}

:global(.mobile-navigation.el-drawer) {
  background: var(--co-bg-surface);
}

:global(.mobile-navigation .el-drawer__header) {
  min-height: var(--co-header-height);
  margin: 0;
  padding: 0 var(--co-space-4);
  border-bottom: 1px solid var(--co-border-default);
  color: var(--co-text-primary);
  font-weight: 700;
}

:global(.mobile-navigation .el-drawer__body) {
  padding: var(--co-space-4) var(--co-space-2);
  overscroll-behavior: contain;
}

:global(.mobile-navigation .el-drawer__close-btn) {
  width: 44px;
  height: 44px;
  border-radius: var(--co-radius-control);
}

@media (max-width: 767px) {
  .app-main {
    padding: var(--co-space-4);
    overflow-x: hidden;
  }
}
</style>
