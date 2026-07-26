<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";

import AppHeader from "./AppHeader.vue";
import AppSidebar from "./AppSidebar.vue";
import SidebarMenu from "./SidebarMenu.vue";

const route = useRoute();
const mobileNavigationOpen = ref(false);
const mobileNavigationTrigger = ref<HTMLElement | null>(null);

const pageTitle = computed(() =>
  typeof route.meta.title === "string" ? route.meta.title : "",
);
const isFullBleed = computed(() => route.meta.fullBleed === true);

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
        <RouterView
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
    title="CloudOps navigation"
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
  padding: clamp(16px, 2vw, 32px)
    max(clamp(16px, 2vw, 32px), env(safe-area-inset-right))
    max(clamp(16px, 2vw, 32px), env(safe-area-inset-bottom))
    max(clamp(16px, 2vw, 32px), env(safe-area-inset-left));
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
  padding: 0 max(var(--co-space-4), env(safe-area-inset-right)) 0 max(var(--co-space-4), env(safe-area-inset-left));
  border-bottom: 1px solid var(--co-border-default);
  color: var(--co-text-primary);
  font-weight: 700;
}

:global(.mobile-navigation .el-drawer__body) {
  padding: var(--co-space-4) max(var(--co-space-2), env(safe-area-inset-right)) max(var(--co-space-4), env(safe-area-inset-bottom)) max(var(--co-space-2), env(safe-area-inset-left));
  overscroll-behavior: contain;
}

:global(.mobile-navigation .el-drawer__close-btn) {
  width: 44px;
  height: 44px;
  border-radius: var(--co-radius-control);
}

@media (max-width: 767px) {
  .app-main {
    padding: var(--co-space-4)
      max(var(--co-space-4), env(safe-area-inset-right))
      max(var(--co-space-4), env(safe-area-inset-bottom))
      max(var(--co-space-4), env(safe-area-inset-left));
    overflow-x: hidden;
  }
}
</style>
