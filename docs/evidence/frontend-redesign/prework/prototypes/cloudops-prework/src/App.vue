<script setup lang="ts">
import type { NavigationMenuItem } from "@nuxt/ui";
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

const route = useRoute();
const router = useRouter();
const sidebarOpen = ref(false);
const storedTheme = localStorage.getItem("cloudops-prework-theme");
const theme = ref<"light" | "dark">(
  storedTheme === "light" || storedTheme === "dark"
    ? storedTheme
    : window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light",
);
const routeTitle = computed(() => String(route.meta.title ?? "CloudOps"));

const primaryNavigation: NavigationMenuItem[] = [
  { label: "事件", icon: "i-lucide-siren", to: "/incidents" },
  { label: "监控", icon: "i-lucide-chart-no-axes-combined", to: "/monitoring" },
  { label: "Operations Atlas", icon: "i-lucide-orbit", to: "/atlas" },
  { label: "Agent", icon: "i-lucide-bot", to: "/agent" },
];

const secondaryNavigation: NavigationMenuItem[] = [
  { label: "设置", icon: "i-lucide-settings-2", to: "/settings" },
  { label: "异常状态", icon: "i-lucide-triangle-alert", to: "/states" },
  { label: "大数据边界", icon: "i-lucide-rows-3", to: "/scale" },
];

function applyTheme(value: "light" | "dark") {
  document.documentElement.classList.toggle("dark", value === "dark");
  document.documentElement.dataset.theme = value;
  document.documentElement.style.colorScheme = value;
  localStorage.setItem("cloudops-prework-theme", value);
  window.dispatchEvent(new CustomEvent("cloudops-prework-theme", { detail: value }));
}

function toggleTheme() {
  theme.value = theme.value === "light" ? "dark" : "light";
}

watch(theme, applyTheme, { immediate: true });
async function focusRouteHeading() {
  await nextTick();
  document.querySelector<HTMLElement>("#main-content h1")?.focus({ preventScroll: true });
}

watch(() => route.path, focusRouteHeading);
onMounted(focusRouteHeading);
</script>

<template>
  <UApp :toaster="{ position: 'top-right' }">
    <a class="skip-link" href="#main-content">跳到主要内容</a>
    <UDashboardGroup class="prototype-shell">
      <UDashboardSidebar v-model:open="sidebarOpen" collapsible resizable class="prototype-sidebar" :min-size="15" :max-size="19">
        <template #header="{ collapsed: isCollapsed }">
          <RouterLink class="brand-link" to="/incidents" aria-label="CloudOps 前置原型主页">
            <span class="brand-mark" aria-hidden="true">CO</span>
            <span v-if="!isCollapsed" class="brand-copy"><strong>CloudOps</strong><small>前置验证原型</small></span>
          </RouterLink>
        </template>
        <template #default="{ collapsed: isCollapsed }">
          <UNavigationMenu :items="primaryNavigation" orientation="vertical" :collapsed="isCollapsed" tooltip />
          <div class="sidebar-spacer" />
          <UNavigationMenu :items="secondaryNavigation" orientation="vertical" :collapsed="isCollapsed" tooltip />
        </template>
        <template #footer="{ collapsed: isCollapsed }">
          <div class="owner-chip" :class="{ 'is-collapsed': isCollapsed }">
            <UIcon name="i-lucide-shield-check" class="size-4" aria-hidden="true" />
            <span v-if="!isCollapsed"><strong>本地 Owner</strong><small>只读原型数据</small></span>
          </div>
        </template>
      </UDashboardSidebar>

      <UDashboardPanel class="prototype-panel">
        <UDashboardNavbar class="prototype-navbar">
          <template #left>
            <span class="navbar-route-label">{{ routeTitle }}</span>
          </template>
          <template #right>
            <UBadge color="success" variant="subtle" icon="i-lucide-radio" label="只读 Fixture" />
            <UTooltip :text="theme === 'light' ? '切换深色主题' : '切换浅色主题'">
              <UButton
                color="neutral"
                variant="ghost"
                square
                :icon="theme === 'light' ? 'i-lucide-moon' : 'i-lucide-sun'"
                :aria-label="theme === 'light' ? '切换深色主题' : '切换浅色主题'"
                data-testid="theme-toggle"
                @click="toggleTheme"
              />
            </UTooltip>
            <UTooltip text="通知仅展示，不写入">
              <UButton color="neutral" variant="ghost" square icon="i-lucide-bell" aria-label="查看通知" />
            </UTooltip>
          </template>
        </UDashboardNavbar>
        <UDashboardToolbar class="context-toolbar" data-testid="context-toolbar">
          <div class="scope-chain">
            <span><UIcon name="i-lucide-server" aria-hidden="true" />cloudops-local</span>
            <span><UIcon name="i-lucide-box" aria-hidden="true" />demo</span>
            <span><UIcon name="i-lucide-clock-3" aria-hidden="true" />最近 1 小时</span>
          </div>
          <div class="provider-chain">
            <span class="status-dot is-success" />9/9 Provider 可用
          </div>
        </UDashboardToolbar>
        <main id="main-content" class="prototype-main" tabindex="-1">
          <RouterView />
        </main>
      </UDashboardPanel>
    </UDashboardGroup>
  </UApp>
</template>
