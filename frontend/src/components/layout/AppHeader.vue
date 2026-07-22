<script setup lang="ts">
import { computed, ref } from "vue";
import {
  ArrowRight,
  Connection,
  DataAnalysis,
  Menu,
  Moon,
  Sunny,
  SwitchButton,
  UserFilled,
} from "@element-plus/icons-vue";

import { useTheme } from "../../composables/useTheme";
import { useAuthStore } from "../../stores/auth";

defineProps<{
  pageTitle: string;
}>();

const emit = defineEmits<{
  openNavigation: [trigger?: HTMLElement];
}>();

const auth = useAuthStore();
const { isDark, toggleTheme } = useTheme();
const mobileNavigationButton = ref<HTMLButtonElement | null>(null);
const environmentLabel = import.meta.env.VITE_ENVIRONMENT_LABEL || "Demo / kind";
const themeActionLabel = computed(() =>
  isDark.value ? "Switch to light theme" : "Switch to dark theme",
);

function requestMobileNavigation() {
  emit("openNavigation", mobileNavigationButton.value ?? undefined);
}

function handleUserCommand(command: string) {
  if (command === "sign-out") auth.signOut();
}
</script>

<template>
  <header class="app-header">
    <div class="header-leading">
      <button
        ref="mobileNavigationButton"
        type="button"
        class="icon-button mobile-navigation-trigger"
        aria-label="Open primary navigation"
        title="Open primary navigation"
        @click="requestMobileNavigation"
      >
        <el-icon :size="20">
          <Menu />
        </el-icon>
      </button>

      <RouterLink
        class="product-mark"
        to="/incidents"
        aria-label="CloudOps Incident Agent home"
      >
        <span
          class="product-icon"
          aria-hidden="true"
        >
          <el-icon :size="20">
            <DataAnalysis />
          </el-icon>
        </span>
        <span class="product-name product-name--long">CloudOps Incident Agent</span>
        <span class="product-name product-name--short">CloudOps</span>
      </RouterLink>

      <nav
        class="breadcrumb"
        aria-label="Breadcrumb"
      >
        <RouterLink to="/incidents">
          Incidents
        </RouterLink>
        <el-icon
          :size="14"
          aria-hidden="true"
        >
          <ArrowRight />
        </el-icon>
        <span aria-current="page">{{ pageTitle || "Workbench" }}</span>
      </nav>
    </div>

    <div class="header-actions">
      <span class="environment-boundary">
        <el-icon
          :size="16"
          aria-hidden="true"
        >
          <Connection />
        </el-icon>
        {{ environmentLabel }}
      </span>

      <button
        type="button"
        class="icon-button"
        :aria-label="themeActionLabel"
        :title="themeActionLabel"
        @click="toggleTheme"
      >
        <el-icon :size="20">
          <Sunny v-if="isDark" />
          <Moon v-else />
        </el-icon>
      </button>

      <el-dropdown
        trigger="click"
        placement="bottom-end"
        @command="handleUserCommand"
      >
        <button
          type="button"
          class="user-trigger"
          :aria-label="`Open account menu for ${auth.actor?.login || 'GitHub session'}`"
        >
          <span
            class="user-avatar"
            aria-hidden="true"
          >
            <el-icon :size="18">
              <UserFilled />
            </el-icon>
          </span>
          <span class="user-copy">
            <strong>{{ auth.actor?.login || "GitHub session" }}</strong>
            <small>{{ auth.actor?.role || "viewer" }}</small>
          </span>
        </button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item disabled>
              {{ auth.actor?.login || "GitHub session" }} · {{ auth.actor?.role || "viewer" }}
            </el-dropdown-item>
            <el-dropdown-item
              command="sign-out"
              divided
            >
              <el-icon aria-hidden="true">
                <SwitchButton />
              </el-icon>
              Sign out
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
  </header>
</template>

<style scoped>
.app-header {
  position: relative;
  z-index: var(--co-z-header);
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: var(--co-header-height);
  padding: 0 max(var(--co-space-4), env(safe-area-inset-right)) 0 var(--co-space-5);
  border-bottom: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}

.header-leading,
.header-actions,
.product-mark,
.breadcrumb,
.environment-boundary,
.user-trigger {
  display: flex;
  align-items: center;
}

.header-leading {
  min-width: 0;
  gap: var(--co-space-4);
}

.header-actions {
  flex: 0 0 auto;
  gap: var(--co-space-2);
}

.product-mark {
  min-width: 0;
  min-height: 44px;
  gap: var(--co-space-2);
  color: var(--co-text-primary);
}

.product-icon,
.user-avatar {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
  color: var(--co-action-primary);
  background: var(--co-bg-subtle);
}

.product-icon {
  width: 32px;
  height: 32px;
}

.product-name {
  overflow: hidden;
  font-size: 14px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.product-name--short,
.icon-button.mobile-navigation-trigger {
  display: none;
}

.breadcrumb {
  min-width: 0;
  gap: var(--co-space-2);
  padding-left: var(--co-space-4);
  border-left: 1px solid var(--co-border-default);
  color: var(--co-text-muted);
  font-size: 13px;
  white-space: nowrap;
}

.breadcrumb a {
  color: var(--co-action-primary);
}

.breadcrumb a:hover {
  text-decoration: underline;
  text-underline-offset: 3px;
}

.environment-boundary {
  min-height: 32px;
  gap: var(--co-space-2);
  padding: 0 var(--co-space-3);
  border: 1px solid var(--co-status-info-border);
  border-radius: var(--co-radius-pill);
  color: var(--co-status-info-fg);
  background: var(--co-status-info-bg);
  font-size: 12px;
  font-weight: 650;
  white-space: nowrap;
}

.icon-button,
.user-trigger {
  border: 1px solid transparent;
  color: var(--co-text-secondary);
  background: transparent;
  cursor: pointer;
  transition: color var(--co-motion-fast) var(--co-ease-out),
    border-color var(--co-motion-fast) var(--co-ease-out),
    background-color var(--co-motion-fast) var(--co-ease-out);
}

.icon-button {
  display: grid;
  width: 44px;
  height: 44px;
  padding: 0;
  place-items: center;
  border-radius: var(--co-radius-control);
}

.icon-button:hover,
.user-trigger:hover {
  border-color: var(--co-border-default);
  color: var(--co-text-primary);
  background: var(--co-bg-hover);
}

.user-trigger {
  min-height: 44px;
  gap: var(--co-space-2);
  padding: var(--co-space-1) var(--co-space-2);
  border-radius: var(--co-radius-control);
}

.user-avatar {
  width: 32px;
  height: 32px;
}

.user-copy {
  display: grid;
  min-width: 0;
  text-align: left;
}

.user-copy strong {
  max-width: 150px;
  overflow: hidden;
  color: var(--co-text-primary);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-copy small {
  color: var(--co-text-muted);
  font-size: 11px;
  line-height: 1.3;
  text-transform: capitalize;
}

@media (max-width: 1120px) {
  .product-name--long {
    display: none;
  }

  .product-name--short {
    display: inline;
  }
}

@media (max-width: 900px) {
  .breadcrumb,
  .user-copy {
    display: none;
  }
}

@media (max-width: 767px) {
  .app-header {
    padding-left: max(var(--co-space-2), env(safe-area-inset-left));
  }

  .icon-button.mobile-navigation-trigger {
    display: grid;
  }

  .product-icon {
    display: none;
  }

  :global(.el-button) {
    min-height: 44px;
  }
}

@media (max-width: 560px) {
  .environment-boundary {
    display: none;
  }
}
</style>
